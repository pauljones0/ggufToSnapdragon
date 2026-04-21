package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"cloud.google.com/go/firestore"
	batch "google.golang.org/api/batch/v1"
)

type QueueTaskPayload struct {
	JobID   string `json:"job_id"`
	TraceID string `json:"trace_id"`
}

// QueueManager acts as the asynchronous backend orchestrator linking Cloud Tasks to Google Cloud Batch.
// It triggers via HTTP POST from Google Cloud Tasks.
func QueueManager(w http.ResponseWriter, r *http.Request) {
	// Generate a unique span ID for this handler invocation
	spanID := NewSpanID()

	var payload QueueTaskPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	projectID := os.Getenv("GCP_PROJECT_ID")
	region := os.Getenv("GCP_REGION") // Expected to be set (e.g. us-central1)
	zone := os.Getenv("GCP_ZONE")     // Expected to be set (e.g. us-central1-a)
	checkpointBucket := os.Getenv("CHECKPOINT_BUCKET")

	// Structured labels for all log entries in this handler
	labels := map[string]string{"job_id": payload.JobID}

	if projectID == "" || region == "" || zone == "" || checkpointBucket == "" {
		logJSON("ERROR", "GCP_PROJECT_ID, GCP_REGION, GCP_ZONE, or CHECKPOINT_BUCKET missing from environment", payload.TraceID, spanID, "QueueManager", labels)
		http.Error(w, "missing env", http.StatusInternalServerError)
		return
	}

	var jobRecord JobRecord
	var jobIDStr string
	var batchErr error
	var err error

	// Use shared FinOps constants from finops.go
	var estimatedJobCost float64

	// Fetch the specific 'Queued' job and Lock Concurrency in an atomic Transaction
	err = firestoreClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// --- A. Fetch Specific Queued Job ---
		targetJobDoc, err := tx.Get(firestoreClient.Collection("Jobs").Doc(payload.JobID))
		if err != nil || !targetJobDoc.Exists() {
			return fmt.Errorf("job not found or unavailable")
		}

		if err := targetJobDoc.DataTo(&jobRecord); err != nil {
			return fmt.Errorf("failed to parse job record: %w", err)
		}

		if jobRecord.Status != "Queued" {
			return fmt.Errorf("job is already processing or finished")
		}

		// Calculate job cost using shared FinOps module
		estimatedJobCost = EstimateJobCost(jobRecord.TargetVCpu, jobRecord.TargetRAM)

		// --- B. Check and Increment FinOps Lock ---
		lockRef := firestoreClient.Collection("System").Doc("ConcurrencyLock")
		lockDoc, err := tx.Get(lockRef)

		activeSpendRate := 0.0
		if err == nil && lockDoc.Exists() {
			if val, ok := lockDoc.Data()["active_spend_rate_cents_per_hr"]; ok {
				// Firestore numeric values can sometimes unmarshal as int64 or float64.
				// We must defensively coerce them to float64 to prevent panic or budget calculation drift.
				switch v := val.(type) {
				case float64:
					activeSpendRate = v
				case int64:
					activeSpendRate = float64(v)
				case int:
					activeSpendRate = float64(v)
				case float32:
					activeSpendRate = float64(v)
				default:
					logJSON("WARNING", fmt.Sprintf("Unexpected type %T for active_spend_rate_cents_per_hr", v), payload.TraceID, spanID, "QueueManager", labels)
				}
			}
		}

		if activeSpendRate+estimatedJobCost > MaxBudgetCentsPerHour {
			return fmt.Errorf("finops budget limit reached: current spend %.2f + job cost %.2f > budget %.2f", activeSpendRate, estimatedJobCost, MaxBudgetCentsPerHour)
		}

		jobIDStr = fmt.Sprintf("hexforge-job-%s", strings.ToLower(jobRecord.JobID))

		// Lock job by updating to Provisioning
		err = tx.Update(targetJobDoc.Ref, []firestore.Update{
			{Path: "status", Value: "Provisioning"},
		})
		if err != nil {
			return err
		}

		// Increment the active spend rate lock
		if lockDoc.Exists() {
			err = tx.Update(lockRef, []firestore.Update{
				{Path: "active_spend_rate_cents_per_hr", Value: firestore.Increment(estimatedJobCost)},
			})
		} else {
			// Create lock document if it doesn't exist
			err = tx.Set(lockRef, map[string]interface{}{
				"active_spend_rate_cents_per_hr": estimatedJobCost,
			})
		}
		return err
	})

	if err != nil {
		if strings.Contains(err.Error(), "finops budget limit") {
			logJSON("WARNING", fmt.Sprintf("Queue Manager hit budget lock: %v. Returning 429 for Cloud Tasks Backoff.", err), payload.TraceID, spanID, "QueueManager", labels)
			http.Error(w, "FinOps Budget Limit Reached", http.StatusTooManyRequests)
			return
		}
		if strings.Contains(err.Error(), "job is already") || strings.Contains(err.Error(), "job not found") {
			logJSON("INFO", fmt.Sprintf("Queue Manager skipped duplicate/invalid execution: %v", err), payload.TraceID, spanID, "QueueManager", labels)
			w.WriteHeader(http.StatusOK)
			return
		}
		logJSON("ERROR", fmt.Sprintf("Queue Manager transaction aborted/failed: %v", err), payload.TraceID, spanID, "QueueManager", labels)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	logJSON("INFO", fmt.Sprintf("Queue Manager locked and is dispatching Cloud Batch Job %s for Job Record %s", jobIDStr, jobRecord.JobID), payload.TraceID, spanID, "QueueManager", labels)

	// 4. Initialize Cloud Batch API
	batchService, err := batch.NewService(ctx)
	if err != nil {
		// Rollback both job status and lock on failure
		_, rollbackErr := firestoreClient.Collection("Jobs").Doc(jobRecord.JobID).Update(ctx, []firestore.Update{{Path: "status", Value: "Queued"}})
		_, lockErr := firestoreClient.Collection("System").Doc("ConcurrencyLock").Update(ctx, []firestore.Update{{Path: "active_spend_rate_cents_per_hr", Value: firestore.Increment(-estimatedJobCost)}})

		if rollbackErr != nil || lockErr != nil {
			logJSON("CRITICAL", fmt.Sprintf("Failed to init batch service: %v. AND failed to rollback status/lock. RollbackErr: %v, LockErr: %v", err, rollbackErr, lockErr), payload.TraceID, spanID, "QueueManager", labels)
		} else {
			logJSON("ERROR", fmt.Sprintf("failed to init batch service: %v", err), payload.TraceID, spanID, "QueueManager", labels)
		}
		http.Error(w, "Failed to init batch service", http.StatusInternalServerError)
		return
	}

	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)

	// Since we are compiling models, Local NVMe SSD is added for swap space
	// We use the custom Base Image that Packer built.
	batchJob := &batch.Job{
		TaskGroups: []*batch.TaskGroup{
			{
				TaskSpec: &batch.TaskSpec{
					Runnables: []*batch.Runnable{
						{
							Script: &batch.Script{
								Path: "/opt/hexforge/batch_entrypoint.sh",
							},
						},
					},
					ComputeResource: &batch.ComputeResource{
						CpuMilli:  int64(jobRecord.TargetVCpu) * 1000,
						MemoryMib: int64(jobRecord.TargetRAM * 1024), // Extracted from Hardware Profile dynamically
					},
					Environment: &batch.Environment{
						Variables: map[string]string{
							"JOB_ID":               jobRecord.JobID,
							"HF_URL":               jobRecord.HFUrl,
							"DSP_ARCH":             jobRecord.DSPArch,
							"CHIPSET":              jobRecord.Chipset,
							"TARGET_SWAP":          fmt.Sprintf("%d", jobRecord.TargetSwap),
							"API_BASE_URL":         fmt.Sprintf("https://%s-%s.cloudfunctions.net", region, projectID),
							"CHECKPOINT_BUCKET":    checkpointBucket,
							"TRACE_ID":             payload.TraceID,
							"SPAN_ID":              spanID,
							"GCP_PROJECT_ID":       projectID,
							"PUBSUB_TOPIC":         os.Getenv("PUBSUB_TOPIC"),
							"NATIVE_INT4":          boolToNum(jobRecord.NativeINT4Support),
							"HAS_HMX":              boolToNum(jobRecord.HasHMX),
							"NEEDS_LOGITS_OFFLOAD": boolToNum(jobRecord.NeedsLogitsOffload),
							"NEEDS_FASTRPC_FIX":    boolToNum(jobRecord.NeedsFastRPCFix),
							"MAX_SESSION_MEM_GB":   fmt.Sprintf("%.2f", jobRecord.MaxSessionMemoryGB),
							"MOE_CAPABLE":          boolToNum(jobRecord.MoECapable),
							"KV_OFFSET_MB":         fmt.Sprintf("%d", jobRecord.KVOffsetBufferMB),
							"VTCM_SIZE_KB":         fmt.Sprintf("%d", jobRecord.VTCMSizeKB),
							"HTP_GENERATION":       fmt.Sprintf("%d", jobRecord.HTPGeneration),
							"MMAP_BUDGET_MB":       fmt.Sprintf("%d", jobRecord.MmapBudgetMB),
							"SPECULATIVE_MODE":     "SSD-Q1", // Default to architectural recommendation
							"EXPORT_TOKENIZER":     "1",      // Enable by default for standalone deployment
						},
						SecretVariables: map[string]string{
							"HF_TOKEN": fmt.Sprintf("projects/%s/secrets/hf-token/versions/latest", projectID),
						},
					},
					LifecyclePolicies: []*batch.LifecyclePolicy{
						{
							// Action to take if a Spot VM is naturally preempted
							Action: "RETRY_TASK",
							ActionCondition: &batch.ActionCondition{
								ExitCodes: []int64{50001}, // 50001 is the standard Cloud Batch infrastructure failure / preemption code
							},
						},
					},
				},
				TaskCount: 1,
			},
		},
		AllocationPolicy: &batch.AllocationPolicy{
			Instances: []*batch.InstancePolicyOrTemplate{
				{
					Policy: &batch.InstancePolicy{
						MachineType: fmt.Sprintf("custom-%d-%d", jobRecord.TargetVCpu, jobRecord.TargetRAM*1024),
						BootDisk: &batch.Disk{
							Image:  "projects/" + projectID + "/global/images/family/hexforge-base",
							SizeGb: int64(150),
							Type:   "pd-ssd",
						},
						ProvisioningModel: "SPOT", // Slash compute costs by 60-80%
					},
					InstallGpuDrivers: false,
				},
			},
			Location: &batch.LocationPolicy{
				AllowedLocations: []string{fmt.Sprintf("zones/%s", zone)},
			},
			ServiceAccount: &batch.ServiceAccount{
				Email: fmt.Sprintf("hexforge-worker-sa@%s.iam.gserviceaccount.com", projectID),
			},
			Network: &batch.NetworkPolicy{
				NetworkInterfaces: []*batch.NetworkInterface{
					{
						Network:             fmt.Sprintf("projects/%s/global/networks/hexforge-vpc", projectID),
						Subnetwork:          fmt.Sprintf("projects/%s/regions/%s/subnetworks/hexforge-subnet", projectID, region),
						NoExternalIpAddress: true,
					},
				},
			},
		},
		LogsPolicy: &batch.LogsPolicy{
			Destination: "CLOUD_LOGGING",
		},
	}

	_, batchErr = batchService.Projects.Locations.Jobs.Create(parent, batchJob).JobId(jobIDStr).Do()
	if batchErr != nil {
		// Rollback to Queued so another instance can retry
		_, rollbackErr := firestoreClient.Collection("Jobs").Doc(jobRecord.JobID).Update(ctx, []firestore.Update{{Path: "status", Value: "Queued"}})
		_, lockErr := firestoreClient.Collection("System").Doc("ConcurrencyLock").Update(ctx, []firestore.Update{{Path: "active_spend_rate_cents_per_hr", Value: firestore.Increment(-estimatedJobCost)}})
		if rollbackErr != nil || lockErr != nil {
			logJSON("CRITICAL", fmt.Sprintf("Failed to submit Batch Job: %v. AND failed to rollback Firestore status/lock. RollbackErr: %v, LockErr: %v", batchErr, rollbackErr, lockErr), payload.TraceID, spanID, "QueueManager", labels)
		} else {
			logJSON("ERROR", fmt.Sprintf("failed to submit Cloud Batch Job: %v", batchErr), payload.TraceID, spanID, "QueueManager", labels)
		}
		http.Error(w, "Batch submission failed", http.StatusInternalServerError)
		return
	}

	logJSON("INFO", fmt.Sprintf("Successfully dispatched Google Cloud Batch Job %s", jobIDStr), payload.TraceID, spanID, "QueueManager", labels)

	// Publish lifecycle event to Pub/Sub for downstream consumers
	PublishJobEvent(ctx, EventJobProvisioning, jobRecord.JobID, payload.TraceID, spanID, map[string]string{
		"batch_job_id": jobIDStr,
		"target_ram":   fmt.Sprintf("%d", jobRecord.TargetRAM),
		"target_vcpu":  fmt.Sprintf("%d", jobRecord.TargetVCpu),
	})

	w.WriteHeader(http.StatusOK)
}

func boolToNum(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
