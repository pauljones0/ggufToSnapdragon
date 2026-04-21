package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hexforge-backend/models"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var firestoreClient *firestore.Client

// init initializes the global Firestore client.
// Note: logJSON is not used here because GCP_PROJECT_ID may not be set during cold-start init.
func init() {
	ctx := context.Background()
	// Assumes standard GCP environment where credentials are automatically loaded
	client, err := firestore.NewClient(ctx, firestore.DetectProjectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"severity":"CRITICAL","message":"Failed to initialize Firestore client: %v"}`+"\n", err)
		os.Exit(1)
	}
	firestoreClient = client
}

// JobRequest represents the incoming JSON payload from the Next.js frontend.
type JobRequest struct {
	UserID     string `json:"user_id"`
	PhoneModel string `json:"phone_model"`
	HFUrl      string `json:"hf_url"`
}

// JobRecord represents the document schema stored in Firestore.
type JobRecord struct {
	JobID                 string    `firestore:"job_id"`
	UserID                string    `firestore:"user_id"`
	HFUrl                 string    `firestore:"hf_url"`
	ParameterSizeBillions int       `firestore:"parameter_size_billions"`
	PhoneModel            string    `firestore:"phone_model"`
	Chipset               string    `firestore:"chipset"`
	DSPArch               string    `firestore:"dsp_arch"`
	TargetRAM             int       `firestore:"target_ram"`
	TargetSwap            int       `firestore:"target_swap"`
	TargetVCpu            int       `firestore:"target_vcpu"`
	NativeINT4Support     bool      `firestore:"native_int4_support"`
	HasHMX                bool      `firestore:"has_hmx"`
	NeedsLogitsOffload    bool      `firestore:"needs_logits_offload"`
	MaxSessionMemoryGB    float64   `firestore:"max_session_memory_gb"`
	MoECapable            bool      `firestore:"moe_capable"`
	KVOffsetBufferMB      int       `firestore:"kv_offset_buffer_mb"`
	VTCMSizeKB            int       `firestore:"vtcm_size_kb"`
	HTPGeneration         int       `firestore:"htp_generation"`
	MmapBudgetMB          int       `firestore:"mmap_budget_mb"`
	Status                string    `firestore:"status"`
	ErrorMessage          string    `firestore:"error_message,omitempty"`
	NeedsFastRPCFix       bool      `firestore:"needs_fastrpc_fix"`
	TraceID               string    `firestore:"trace_id,omitempty"`
	CreatedAt             time.Time `firestore:"created_at"`
	UpdatedAt             time.Time `firestore:"updated_at"`
}

// SubmitJob is the HTTP Cloud Function entry point.
func SubmitJob(w http.ResponseWriter, r *http.Request) {
	// Generate a unique span ID for this handler invocation
	spanID := NewSpanID()

	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Trace-ID")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Parse Request
	var req JobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.UserID == "" || req.PhoneModel == "" || req.HFUrl == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	traceID := r.Header.Get("X-Trace-ID")
	if traceID == "" {
		traceID = uuid.New().String()
	}

	// Parse GCP native trace context for span correlation
	cloudTraceCtx := r.Header.Get("X-Cloud-Trace-Context")
	gcpTraceID, _ := ParseCloudTraceContext(cloudTraceCtx)
	if gcpTraceID != "" && traceID == "" {
		traceID = gcpTraceID
	}

	// Structured labels for all log entries in this request
	jobLabels := map[string]string{"user_id": req.UserID}

	// 1.2 JWT Validation for API Hardening
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Unauthorized: Missing or invalid token", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")

	authCtx := context.Background()
	app, appErr := firebase.NewApp(authCtx, nil)
	if appErr != nil {
		logJSON("ERROR", fmt.Sprintf("error initializing firebase app: %v", appErr), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "Internal configuration error", http.StatusInternalServerError)
		return
	}
	authClient, authErr := app.Auth(authCtx)
	if authErr != nil {
		logJSON("ERROR", fmt.Sprintf("error getting Auth client: %v", authErr), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "Internal configuration error", http.StatusInternalServerError)
		return
	}

	token, tokenErr := authClient.VerifyIDToken(authCtx, idToken)
	if tokenErr != nil {
		logJSON("WARNING", fmt.Sprintf("Invalid authentication token: %v", tokenErr), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "Invalid authentication token", http.StatusUnauthorized)
		return
	}

	// Override user ID with trusted Firebase UID to prevent queue flooding
	req.UserID = token.UID
	jobLabels["user_id"] = req.UserID

	// 1.5 Strict URL Input Sanitization
	parsedUrl, err := url.ParseRequestURI(req.HFUrl)
	if err != nil || parsedUrl.Scheme != "https" || parsedUrl.Host != "huggingface.co" {
		logJSON("WARNING", fmt.Sprintf("Security Alert: Invalid HF URL Prefix or Format: %s", req.HFUrl), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "Error: URL must strictly be a valid https://huggingface.co/ URL", http.StatusBadRequest)
		return
	}

	// Block injection characters since the URL reaches `huggingface-cli` via bash args inside the sandbox.
	// We include single quotes to prevent escaping out of double quotes dynamically.
	if strings.ContainsAny(req.HFUrl, ";&|`$<>\\'\"\\n\\r\\t") {
		logJSON("WARNING", fmt.Sprintf("Security Alert: Potential shell injection detected in URL: %s", req.HFUrl), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "Error: Invalid characters in URL.", http.StatusBadRequest)
		return
	}

	// URL Blacklist for malicious/abusive repositories
	maliciousRepos := []string{
		"unfiltered-malware-models",
		"poc-malicious-gguf",
	}
	for _, blacklisted := range maliciousRepos {
		if strings.Contains(strings.ToLower(req.HFUrl), strings.ToLower(blacklisted)) {
			logJSON("WARNING", fmt.Sprintf("Security Alert: Blocked access to blacklisted repository: %s", req.HFUrl), traceID, spanID, "SubmitJob", jobLabels)
			http.Error(w, "Error: The requested repository is restricted for security reasons.", http.StatusForbidden)
			return
		}
	}

	logJSON("INFO", fmt.Sprintf("Received job request for phone %s with URL %s", req.PhoneModel, req.HFUrl), traceID, spanID, "SubmitJob", jobLabels)

	// 2. Validate Hugging Face Format and Extract Parameter Size
	// Note: Our defense mechanism drops gracefully without exposing specific size complaints.
	paramsSizeRe := regexp.MustCompile(`(?i)([\d\.]+)b`)
	matches := paramsSizeRe.FindStringSubmatch(req.HFUrl)
	var paramSizeBillions float64

	if len(matches) < 2 {
		logJSON("WARNING", fmt.Sprintf("Regex mismatch for B size in URL: %s", req.HFUrl), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "Error: Incompatible model architecture or unrecognized GGUF structural footprint.", http.StatusBadRequest)
		return
	}

	sizeFloat, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		logJSON("ERROR", fmt.Sprintf("Failed to convert regex match to float: %v", err), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "Error: Incompatible model architecture or unrecognized GGUF structural footprint.", http.StatusBadRequest)
		return
	}
	paramSizeBillions = sizeFloat

	// 3. Hardware Profile Verification
	deviceDict := models.GetDeviceDictionary()
	deviceConfig, exists := deviceDict[req.PhoneModel]

	dspArch := ""
	chipset := ""
	if !exists {
		// If "Other" or unknown, we default to the lowest common denominator (v68 / 3B limits)
		// Or we could reject. The user asked for "Other" to allow manual entry.
		// For v1, if they supplied "Other", we assume a safe baseline or reject.
		// For simplicity, we drop it if it's not strictly known to avoid OOM crashes on the VM.
		logJSON("WARNING", fmt.Sprintf("Unrecognized device model: %s", req.PhoneModel), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "Error: Unrecognized Snapdragon component. Please select a supported model.", http.StatusBadRequest)
		return
	} else {
		dspArch = deviceConfig.HexagonDSPArchitecture
		chipset = deviceConfig.Chipset
	}

	profiles := models.GetHardwareProfiles()
	profile, ok := profiles[dspArch]
	if !ok {
		logJSON("ERROR", fmt.Sprintf("Unrecognized DSP Arch mapped: %s", dspArch), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "Internal Configuration Error", http.StatusInternalServerError)
		return
	}

	if paramSizeBillions > float64(profile.MaxNPUOnlyModelSizeB) {
		logJSON("WARNING", fmt.Sprintf("Rejection: Model %fB exceeds threshold %dB for arch %s", paramSizeBillions, profile.MaxNPUOnlyModelSizeB, dspArch), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, fmt.Sprintf("Error: Model size strictly exceeds %dB parameter limit for %s architecture.", profile.MaxNPUOnlyModelSizeB, dspArch), http.StatusBadRequest)
		return
	}

	targetVCpu := profile.RecommendedVCpuCount
	if targetVCpu == 0 {
		targetVCpu = StandardVCpuCount // Fallback to global standard if profile is missing it
	}

	// Hugging Face File Size HEAD Validation (Anti-gaming check)
	// Models are roughly ~1GB per Billion parameters (INT4/INT8 quantization variations).
	// We allow a strict 15% physical wiggle room across the raw max size before rejecting it to prevent OOM
	// The user cannot rename a 70B model URL to 3B to bypass this.
	hfUrlSafe := strings.ReplaceAll(req.HFUrl, " ", "%20")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// [SRE Telemetry] Generate a sub-span specifically for the external HF HTTP request
	spanID_hf := NewSpanID()
	logJSON("INFO", fmt.Sprintf("Initiating HEAD request to Hugging Face to validate size: %s", hfUrlSafe), traceID, spanID_hf, "SubmitJob_HFValid", jobLabels)

	hfStartTime := time.Now()
	headResp, err := client.Head(hfUrlSafe)
	hfDuration := time.Since(hfStartTime).Milliseconds()

	if err != nil || headResp.StatusCode != 200 {
		logJSON("WARNING", fmt.Sprintf("Failed HF HEAD request check: %v (took %dms)", err, hfDuration), traceID, spanID_hf, "SubmitJob_HFValid", jobLabels)
		http.Error(w, "Error: Could not validate Hugging Face URL. Ensure the repository is public and points to a file.", http.StatusBadRequest)
		return
	}

	contentLengthBytes := headResp.ContentLength
	logJSON("INFO", fmt.Sprintf("HF HEAD check complete. Content length: %d bytes. Took: %dms", contentLengthBytes, hfDuration), traceID, spanID_hf, "SubmitJob_HFValid", jobLabels)
	maxAllowedBytes := int64(profile.MaxNPUOnlyModelSizeB) * 1024 * 1024 * 1024 // ~1GB per Billion Params
	maxAllowedBytesWiggle := int64(float64(maxAllowedBytes) * 1.15)             // 15% tolerance

	if contentLengthBytes > maxAllowedBytesWiggle {
		logJSON("WARNING", fmt.Sprintf("OOM Guard rejection: Physical file size %d bytes exceeds physical constraints for %dB model", contentLengthBytes, profile.MaxNPUOnlyModelSizeB), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, fmt.Sprintf("Error: The raw file size (%.2f GB) exceeds the physical RAM limits for %dB models on this device.", float64(contentLengthBytes)/(1024*1024*1024), profile.MaxNPUOnlyModelSizeB), http.StatusBadRequest)
		return
	}

	// 4. Create Job Record
	ctx := context.Background()
	jobsCol := firestoreClient.Collection("Jobs")

	// Global Queue Flooding Protection (Target: Max 50 active Queued items)
	globalQueueIter := jobsCol.Where("status", "==", "Queued").Limit(50).Documents(ctx)
	globalQueueDocs, _ := globalQueueIter.GetAll()
	if len(globalQueueDocs) >= 50 {
		logJSON("WARNING", fmt.Sprintf("System Alert: Queue-flooding mitigation active. Rejecting request from user %s", req.UserID), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "The HexForge compilation queue is currently full. Please try again later.", http.StatusServiceUnavailable)
		return
	}

	// Check user quota (1 concurrent per user)
	iter := jobsCol.Where("user_id", "==", req.UserID).
		Where("status", "in", []string{"Queued", "Provisioning", "Downloading", "Compiling", "Uploading"}).
		Documents(ctx)

	docs, err := iter.GetAll()
	if err != nil {
		logJSON("ERROR", fmt.Sprintf("Failed to query existing jobs: %v", err), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if len(docs) >= 1 {
		http.Error(w, "You already have an active job in the pipeline. Please wait for it to finish.", http.StatusTooManyRequests)
		return
	}

	// Cache Deduplication Check: Has this EXACT model + architecture been successfully compiled already?
	cacheIter := jobsCol.Where("hf_url", "==", req.HFUrl).
		Where("dsp_arch", "==", dspArch).
		Where("status", "==", "Completed").
		Limit(1).
		Documents(ctx)

	cacheDocs, err := cacheIter.GetAll()
	if err == nil && len(cacheDocs) > 0 {
		logJSON("INFO", fmt.Sprintf("Deduplication cache hit! Bypassing queue for url %s", req.HFUrl), traceID, spanID, "SubmitJob", jobLabels)

		// Map the new job identically to bypass
		newDoc := jobsCol.NewDoc()
		record := JobRecord{
			JobID:                 newDoc.ID,
			UserID:                req.UserID,
			HFUrl:                 req.HFUrl,
			ParameterSizeBillions: int(paramSizeBillions),
			PhoneModel:            req.PhoneModel,
			Chipset:               chipset,
			DSPArch:               dspArch,
			TargetRAM:             profile.RecommendedHostRAMGB,
			TargetSwap:            profile.RecommendedHostSwapGB,
			TargetVCpu:            targetVCpu,
			NativeINT4Support:     profile.NativeINT4Support,
			HasHMX:                profile.HasHMX,
			NeedsLogitsOffload:    profile.NeedsLogitsOffload,
			NeedsFastRPCFix:       profile.NeedsFastRPCFix,
			MaxSessionMemoryGB:    profile.MaxSessionMemoryGB,
			MoECapable:            profile.MoECapable,
			KVOffsetBufferMB:      profile.KVOffsetBufferMB,
			VTCMSizeKB:            profile.VTCMSizeKB,
			HTPGeneration:         profile.HTPGeneration,
			MmapBudgetMB:          profile.MmapBudgetMB,
			Status:                "Completed",
			CreatedAt:             time.Now().UTC(),
			UpdatedAt:             time.Now().UTC(),
		}

		_, _ = newDoc.Set(ctx, record)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"job_id": record.JobID,
			"status": "Completed",
		})
		return
	}

	// Construct New Job
	newDoc := jobsCol.NewDoc()
	record := JobRecord{
		JobID:                 newDoc.ID,
		UserID:                req.UserID,
		HFUrl:                 req.HFUrl,
		ParameterSizeBillions: int(paramSizeBillions),
		PhoneModel:            req.PhoneModel,
		Chipset:               chipset,
		DSPArch:               dspArch,
		TargetRAM:             profile.RecommendedHostRAMGB,
		TargetSwap:            profile.RecommendedHostSwapGB,
		TargetVCpu:            targetVCpu,
		NativeINT4Support:     profile.NativeINT4Support,
		HasHMX:                profile.HasHMX,
		NeedsLogitsOffload:    profile.NeedsLogitsOffload,
		NeedsFastRPCFix:       profile.NeedsFastRPCFix,
		MaxSessionMemoryGB:    profile.MaxSessionMemoryGB,
		MoECapable:            profile.MoECapable,
		KVOffsetBufferMB:      profile.KVOffsetBufferMB,
		VTCMSizeKB:            profile.VTCMSizeKB,
		HTPGeneration:         profile.HTPGeneration,
		MmapBudgetMB:          profile.MmapBudgetMB,
		Status:                "Queued",
		CreatedAt:             time.Now().UTC(),
		UpdatedAt:             time.Now().UTC(),
	}

	record.TraceID = traceID

	_, err = newDoc.Set(ctx, record)
	if err != nil {
		logJSON("ERROR", fmt.Sprintf("Failed to insert new job: %v", err), traceID, spanID, "SubmitJob", jobLabels)
		http.Error(w, "Failed to queue job", http.StatusInternalServerError)
		return
	}

	// 5. Dispatch Cloud Task (with idempotent task name to prevent duplicates)
	projectID := os.Getenv("GCP_PROJECT_ID")
	region := os.Getenv("GCP_REGION")
	jobLabels["job_id"] = record.JobID

	if projectID != "" && region != "" {
		client, taskErr := cloudtasks.NewClient(ctx)
		if taskErr == nil {
			defer client.Close()
			queuePath := fmt.Sprintf("projects/%s/locations/%s/queues/hexforge-job-queue", projectID, region)

			payload, _ := json.Marshal(map[string]string{
				"job_id":   record.JobID,
				"trace_id": traceID,
			})

			queueManagerURL := fmt.Sprintf("https://%s-%s.cloudfunctions.net/QueueManager", region, projectID)
			functionsSAEmail := os.Getenv("FUNCTIONS_SA_EMAIL")
			if functionsSAEmail == "" {
				functionsSAEmail = fmt.Sprintf("hexforge-functions-sa@%s.iam.gserviceaccount.com", projectID)
			}

			// Deterministic task name from job_id prevents duplicate task creation on retries.
			// Cloud Tasks will reject CreateTask if a task with this name already exists.
			taskName := fmt.Sprintf("%s/tasks/hexforge-%s", queuePath, record.JobID)

			reqTask := &taskspb.CreateTaskRequest{
				Parent: queuePath,
				Task: &taskspb.Task{
					Name: taskName,
					MessageType: &taskspb.Task_HttpRequest{
						HttpRequest: &taskspb.HttpRequest{
							HttpMethod: taskspb.HttpMethod_POST,
							Url:        queueManagerURL,
							Headers: map[string]string{
								"Content-Type": "application/json",
							},
							Body: payload,
							AuthorizationHeader: &taskspb.HttpRequest_OidcToken{
								OidcToken: &taskspb.OidcToken{
									ServiceAccountEmail: functionsSAEmail,
									Audience:            queueManagerURL,
								},
							},
						},
					},
					ScheduleTime: timestamppb.Now(),
				},
			}

			_, taskErr = client.CreateTask(ctx, reqTask)
			if taskErr != nil {
				logJSON("ERROR", fmt.Sprintf("Failed to enqueue Cloud Task for job %s: %v", record.JobID, taskErr), traceID, spanID, "SubmitJob", jobLabels)
			} else {
				logJSON("INFO", fmt.Sprintf("Successfully enqueued idempotent Cloud Task %s for job %s", taskName, record.JobID), traceID, spanID, "SubmitJob", jobLabels)

				PublishJobEvent(ctx, EventJobSubmitted, record.JobID, traceID, spanID, map[string]string{
					"user_id":     req.UserID,
					"phone_model": req.PhoneModel,
					"hf_url":      req.HFUrl,
					"target_vcpu": strconv.Itoa(targetVCpu),
				})
			}
		} else {
			logJSON("ERROR", fmt.Sprintf("Failed to initialize Cloud Tasks client: %v", taskErr), traceID, spanID, "SubmitJob", jobLabels)
		}
	} else {
		logJSON("WARNING", "GCP_PROJECT_ID or GCP_REGION not set, skipping Cloud Tasks dispatch", traceID, spanID, "SubmitJob", jobLabels)
	}

	// 6. Respond
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"job_id": record.JobID,
		"status": "Queued",
	})
}
