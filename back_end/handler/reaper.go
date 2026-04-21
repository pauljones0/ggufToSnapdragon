package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

// StaleJobReaper acts as a garbage collector for jobs stuck in the cloud batch environment.
func StaleJobReaper(w http.ResponseWriter, r *http.Request) {
	// Generate a unique span ID for this handler invocation
	spanID := NewSpanID()

	ctx := context.Background()

	threshold := time.Now().UTC().Add(-4 * time.Hour)
	jobsCol := firestoreClient.Collection("Jobs")

	statesToCheck := []string{"Provisioning", "Downloading", "Compiling", "Uploading"}
	totalReaped := 0

	// Note: We check all active states that a worker might get stuck in
	for _, state := range statesToCheck {
		iter := jobsCol.Where("status", "==", state).
			Where("updated_at", "<", threshold).
			Documents(ctx)

		docs, err := iter.GetAll()
		if err != nil {
			logJSON("ERROR", fmt.Sprintf("Reaper failed to fetch docs for %s: %v", state, err), "", spanID, "StaleJobReaper")
			continue
		}

		for _, doc := range docs {
			// Extract trace_id and target_ram from the job document for logging and cost calculation
			var jobRecord JobRecord
			traceID := ""
			labels := map[string]string{"job_id": doc.Ref.ID}
			if parseErr := doc.DataTo(&jobRecord); parseErr == nil {
				traceID = jobRecord.TraceID
			}

			// Use a transaction to reap the job and decrement the lock atomically
			err := firestoreClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
				docSnap, err := tx.Get(doc.Ref)
				if err != nil {
					return err
				}

				if err := docSnap.DataTo(&jobRecord); err != nil {
					return fmt.Errorf("failed to parse job record: %w", err)
				}

				// Re-verify state and threshold inside transaction
				updatedAt, _ := docSnap.Data()["updated_at"].(time.Time)
				if updatedAt.After(threshold) {
					return fmt.Errorf("job updated recently, skipping")
				}

				// 1. Reap the job
				err = tx.Update(doc.Ref, []firestore.Update{
					{Path: "status", Value: "Failed"},
					{Path: "error_message", Value: "Ghost Job Reaper: Terminated stuck worker instance (>4h elapsed)."},
					{Path: "updated_at", Value: time.Now().UTC()},
				})
				if err != nil {
					return err
				}

				// 2. Decrement FinOps budget lock using the shared cost estimator
				estimatedJobCost := EstimateJobCost(jobRecord.TargetVCpu, jobRecord.TargetRAM)
				lockRef := firestoreClient.Collection("System").Doc("ConcurrencyLock")

				return tx.Update(lockRef, []firestore.Update{
					{Path: "active_spend_rate_cents_per_hr", Value: firestore.Increment(-estimatedJobCost)},
				})
			})

			if err != nil {
				if strings.Contains(err.Error(), "job updated recently") {
					continue
				}
				logJSON("ERROR", fmt.Sprintf("Reaper aborted transaction for job %s: %v", doc.Ref.ID, err), traceID, spanID, "StaleJobReaper", labels)
			} else {
				logJSON("WARNING", fmt.Sprintf("Reaped ghost job %s from %s state", doc.Ref.ID, state), traceID, spanID, "StaleJobReaper", labels)
				totalReaped++

				// Publish lifecycle event to Pub/Sub for downstream consumers
				PublishJobEvent(ctx, EventJobReaped, doc.Ref.ID, traceID, spanID, map[string]string{
					"previous_status": state,
				})
			}
		}
	}

	logJSON("INFO", fmt.Sprintf("StaleJobReaper completed: reaped %d ghost jobs", totalReaped), "", spanID, "StaleJobReaper")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"status":"success", "reaped": %d}`, totalReaped)))
}
