package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

// UpdateJobStatusRequest is the payload from the worker node daemon.
type UpdateJobStatusRequest struct {
	JobID        string `json:"job_id"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
	TraceID      string `json:"trace_id,omitempty"`
}

// UpdateJobStatus allows the worker node to update the status of an active job.
func UpdateJobStatus(w http.ResponseWriter, r *http.Request) {
	// Authentication is handled exclusively and securely via Google Cloud IAM.
	// We deploy this Cloud Function without the `--allow-unauthenticated` flag, meaning GCP's API Gateway
	// natively intercepts the request, validates the Service Account OIDC token, and blocks invalid requests
	// before they even reach our Go code.

	// Generate a unique span ID for this handler invocation
	spanID := NewSpanID()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 2. Parse Request
	var req UpdateJobStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.JobID == "" || req.Status == "" {
		http.Error(w, "Missing required fields (job_id, status)", http.StatusBadRequest)
		return
	}

	traceID := req.TraceID

	// Structured labels for all log entries in this handler
	labels := map[string]string{"job_id": req.JobID, "status": req.Status}

	// 3. Update Firestore Document
	ctx := context.Background()
	jobsCol := firestoreClient.Collection("Jobs")
	docRef := jobsCol.Doc(req.JobID)

	updates := []firestore.Update{
		{Path: "status", Value: req.Status},
		{Path: "updated_at", Value: time.Now().UTC()},
	}

	if req.ErrorMessage != "" {
		updates = append(updates, firestore.Update{Path: "error_message", Value: req.ErrorMessage})
	}

	_, err := docRef.Update(ctx, updates)
	if err != nil {
		logJSON("ERROR", fmt.Sprintf("Failed to update job status for %s: %v", req.JobID, err), traceID, spanID, "UpdateJobStatus", labels)
		http.Error(w, "Failed to update internal job state", http.StatusInternalServerError)
		return
	}

	logJSON("INFO", fmt.Sprintf("Updated job %s to status %s", req.JobID, req.Status), traceID, spanID, "UpdateJobStatus", labels)

	// Publish lifecycle event to Pub/Sub for ALL status transitions (Provisioning, Downloading, Compiling, Uploading, Completed, Failed).
	// Normalized to lowercase for consistent event naming across the pipeline.
	eventType := fmt.Sprintf("job.%s", strings.ToLower(req.Status))
	PublishJobEvent(ctx, eventType, req.JobID, traceID, spanID, nil)

	// If the job has reached a terminal state, decrement the FinOps budget lock.
	// We read the job's target_ram from Firestore to calculate the exact cost that QueueManager originally incremented.
	if req.Status == "Completed" || req.Status == "Failed" {
		// Fetch the job's target_ram for accurate cost calculation
		jobDoc, fetchErr := docRef.Get(ctx)
		if fetchErr != nil {
			logJSON("CRITICAL", fmt.Sprintf("Failed to fetch job record for FinOps lock decrement on job %s: %v", req.JobID, fetchErr), traceID, spanID, "UpdateJobStatus", labels)
		} else {
			var jobRecord JobRecord
			if parseErr := jobDoc.DataTo(&jobRecord); parseErr != nil {
				logJSON("CRITICAL", fmt.Sprintf("Failed to parse job record for FinOps lock decrement on job %s: %v", req.JobID, parseErr), traceID, spanID, "UpdateJobStatus", labels)
			} else {
				estimatedJobCost := EstimateJobCost(jobRecord.TargetVCpu, jobRecord.TargetRAM)

				lockRef := firestoreClient.Collection("System").Doc("ConcurrencyLock")
				_, lockErr := lockRef.Update(ctx, []firestore.Update{
					{Path: "active_spend_rate_cents_per_hr", Value: firestore.Increment(-estimatedJobCost)},
				})
				if lockErr != nil {
					logJSON("CRITICAL", fmt.Sprintf("Failed to decrement FinOps budget lock for job %s (cost: %.2f): %v", req.JobID, estimatedJobCost, lockErr), traceID, spanID, "UpdateJobStatus", labels)
				} else {
					logJSON("INFO", fmt.Sprintf("Decremented FinOps budget lock by %.2f cents/hr following terminal state %s for job %s", estimatedJobCost, req.Status, req.JobID), traceID, spanID, "UpdateJobStatus", labels)
				}
			}
		}
	}

	// 4. Respond
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}
