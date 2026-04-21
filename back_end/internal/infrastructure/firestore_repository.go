package infrastructure

import (
	"context"
	"fmt"
	"time"

	"hexforge-backend/internal/domain"

	"cloud.google.com/go/firestore"
)

type FirestoreJobRepository struct {
	Client *firestore.Client
}

func NewFirestoreJobRepository(client *firestore.Client) *FirestoreJobRepository {
	return &FirestoreJobRepository{Client: client}
}

func (r *FirestoreJobRepository) GetJob(ctx context.Context, jobID string) (*domain.JobRecord, error) {
	doc, err := r.Client.Collection("Jobs").Doc(jobID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var job domain.JobRecord
	if err := doc.DataTo(&job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *FirestoreJobRepository) SaveJob(ctx context.Context, job *domain.JobRecord) error {
	_, err := r.Client.Collection("Jobs").Doc(job.JobID).Set(ctx, job)
	return err
}

func (r *FirestoreJobRepository) UpdateJobStatus(ctx context.Context, jobID string, status string) error {
	_, err := r.Client.Collection("Jobs").Doc(jobID).Update(ctx, []firestore.Update{
		{Path: "status", Value: status},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
	return err
}

func (r *FirestoreJobRepository) GetActiveJobCountForUser(ctx context.Context, userID string) (int, error) {
	iter := r.Client.Collection("Jobs").
		Where("user_id", "==", userID).
		Where("status", "in", []string{"Queued", "Provisioning", "Downloading", "Compiling", "Uploading"}).
		Documents(ctx)
	docs, err := iter.GetAll()
	if err != nil {
		return 0, err
	}
	return len(docs), nil
}

func (r *FirestoreJobRepository) GetGlobalQueuedCount(ctx context.Context, limit int) (int, error) {
	iter := r.Client.Collection("Jobs").Where("status", "==", "Queued").Limit(limit).Documents(ctx)
	docs, err := iter.GetAll()
	if err != nil {
		return 0, err
	}
	return len(docs), nil
}

func (r *FirestoreJobRepository) FindCompletedCache(ctx context.Context, hfURL, dspArch string) (*domain.JobRecord, error) {
	iter := r.Client.Collection("Jobs").
		Where("hf_url", "==", hfURL).
		Where("dsp_arch", "==", dspArch).
		Where("status", "==", "Completed").
		Limit(1).
		Documents(ctx)

	docs, err := iter.GetAll()
	if err != nil || len(docs) == 0 {
		return nil, fmt.Errorf("not found in cache")
	}

	var job domain.JobRecord
	if err := docs[0].DataTo(&job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *FirestoreJobRepository) ExecuteConcurrencyLock(ctx context.Context, jobID string, estimatedCost float64, maxBudget float64) (string, error) {
	var jobIDStr string
	err := r.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		targetJobDoc, err := tx.Get(r.Client.Collection("Jobs").Doc(jobID))
		if err != nil || !targetJobDoc.Exists() {
			return fmt.Errorf("job not found")
		}

		var jobRecord domain.JobRecord
		if err := targetJobDoc.DataTo(&jobRecord); err != nil {
			return err
		}

		if jobRecord.Status != "Queued" {
			return fmt.Errorf("job already processing")
		}

		lockRef := r.Client.Collection("System").Doc("ConcurrencyLock")
		lockDoc, err := tx.Get(lockRef)

		activeSpendRate := 0.0
		if err == nil && lockDoc.Exists() {
			if val, ok := lockDoc.Data()["active_spend_rate_cents_per_hr"]; ok {
				switch v := val.(type) {
				case float64:
					activeSpendRate = v
				case int64:
					activeSpendRate = float64(v)
				case int:
					activeSpendRate = float64(v)
				case float32:
					activeSpendRate = float64(v)
				}
			}
		}

		if activeSpendRate+estimatedCost > maxBudget {
			return fmt.Errorf("finops budget limit reached")
		}

		jobIDStr = fmt.Sprintf("hexforge-job-%s", jobRecord.JobID)

		err = tx.Update(targetJobDoc.Ref, []firestore.Update{
			{Path: "status", Value: "Provisioning"},
		})
		if err != nil {
			return err
		}

		if lockDoc.Exists() {
			err = tx.Update(lockRef, []firestore.Update{
				{Path: "active_spend_rate_cents_per_hr", Value: firestore.Increment(estimatedCost)},
			})
		} else {
			err = tx.Set(lockRef, map[string]interface{}{
				"active_spend_rate_cents_per_hr": estimatedCost,
			})
		}
		return err
	})
	return jobIDStr, err
}

func (r *FirestoreJobRepository) DecrementConcurrencyLock(ctx context.Context, estimatedCost float64) error {
	_, err := r.Client.Collection("System").Doc("ConcurrencyLock").Update(ctx, []firestore.Update{
		{Path: "active_spend_rate_cents_per_hr", Value: firestore.Increment(-estimatedCost)},
	})
	return err
}

func (r *FirestoreJobRepository) GetStaleJobs(ctx context.Context, ageHours int) ([]domain.JobRecord, error) {
	threshold := time.Now().UTC().Add(-time.Duration(ageHours) * time.Hour)
	activeStatuses := []string{"Provisioning", "Downloading", "Compiling", "Uploading"}

	var staleJobs []domain.JobRecord
	for _, status := range activeStatuses {
		iter := r.Client.Collection("Jobs").
			Where("status", "==", status).
			Where("updated_at", "<", threshold).
			Documents(ctx)

		docs, err := iter.GetAll()
		if err != nil {
			return nil, err
		}

		for _, doc := range docs {
			var job domain.JobRecord
			if err := doc.DataTo(&job); err == nil {
				staleJobs = append(staleJobs, job)
			}
		}
	}
	return staleJobs, nil
}
