package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
)

// DeadLetterHandler is invoked when a Cloud Task exhausts all retries.
// It gracefully marks the job as Failed and releases the FinOps spend lock
// so the queue isn't permanently blocked by a single poisoned task.
func DeadLetterHandler(w http.ResponseWriter, r *http.Request) {
	// Generate a unique span ID for this handler invocation
	spanID := NewSpanID()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload QueueTaskPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	traceID := payload.TraceID

	// Structured labels for all log entries in this handler
	labels := map[string]string{"job_id": payload.JobID}

	logJSON("CRITICAL", fmt.Sprintf("Dead-Letter Queue: Cloud Tasks exhausted all retries for job %s", payload.JobID), traceID, spanID, "DeadLetterHandler", labels)

	// 1. Fetch the job record to calculate the cost to decrement
	docRef := firestoreClient.Collection("Jobs").Doc(payload.JobID)
	jobDoc, err := docRef.Get(ctx)
	if err != nil {
		logJSON("ERROR", fmt.Sprintf("DLQ: Failed to fetch job %s: %v", payload.JobID, err), traceID, spanID, "DeadLetterHandler", labels)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	var jobRecord JobRecord
	if err := jobDoc.DataTo(&jobRecord); err != nil {
		logJSON("ERROR", fmt.Sprintf("DLQ: Failed to parse job record %s: %v", payload.JobID, err), traceID, spanID, "DeadLetterHandler", labels)
		http.Error(w, "Failed to parse job", http.StatusInternalServerError)
		return
	}

	// 2. Mark the job as Failed
	_, err = docRef.Update(ctx, []firestore.Update{
		{Path: "status", Value: "Failed"},
		{Path: "error_message", Value: "Cloud Tasks DLQ: Max retries exhausted. The FinOps budget cap was never freed within the retry window."},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
	if err != nil {
		logJSON("ERROR", fmt.Sprintf("DLQ: Failed to mark job %s as Failed: %v", payload.JobID, err), traceID, spanID, "DeadLetterHandler", labels)
		http.Error(w, "Failed to update job", http.StatusInternalServerError)
		return
	}

	// 3. Decrement the FinOps spend lock if the job was in a provisioning state
	// (it may still hold a lock increment from the QueueManager transaction)
	if jobRecord.Status == "Provisioning" || jobRecord.Status == "Downloading" || jobRecord.Status == "Compiling" || jobRecord.Status == "Uploading" {
		estimatedJobCost := EstimateJobCost(jobRecord.TargetVCpu, jobRecord.TargetRAM)

		lockRef := firestoreClient.Collection("System").Doc("ConcurrencyLock")
		_, lockErr := lockRef.Update(ctx, []firestore.Update{
			{Path: "active_spend_rate_cents_per_hr", Value: firestore.Increment(-estimatedJobCost)},
		})
		if lockErr != nil {
			logJSON("CRITICAL", fmt.Sprintf("DLQ: Failed to decrement FinOps lock for job %s (cost: %.2f): %v", payload.JobID, estimatedJobCost, lockErr), traceID, spanID, "DeadLetterHandler", labels)
		} else {
			logJSON("INFO", fmt.Sprintf("DLQ: Decremented FinOps lock by %.2f cents/hr for job %s", estimatedJobCost, payload.JobID), traceID, spanID, "DeadLetterHandler", labels)
		}
	}

	// 4. Publish lifecycle event to Pub/Sub for downstream consumers
	PublishJobEvent(ctx, EventJobDeadLettered, payload.JobID, traceID, spanID, map[string]string{
		"previous_status": jobRecord.Status,
		"dlq_reason":      "Cloud Tasks max retries exhausted",
	})

	logJSON("INFO", fmt.Sprintf("DLQ: Successfully handled dead-letter for job %s", payload.JobID), traceID, spanID, "DeadLetterHandler", labels)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"dead_letter_handled"}`))
}
