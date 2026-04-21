package domain

import (
	"context"
)

// JobRepository defines the persistence interface for Job records.
type JobRepository interface {
	GetJob(ctx context.Context, jobID string) (*JobRecord, error)
	GetActiveJobCountForUser(ctx context.Context, userID string) (int, error)
	GetGlobalQueuedCount(ctx context.Context, limit int) (int, error)
	FindCompletedCache(ctx context.Context, hfURL, dspArch string) (*JobRecord, error)
	SaveJob(ctx context.Context, job *JobRecord) error
	UpdateJobStatus(ctx context.Context, jobID string, status string) error
	ExecuteConcurrencyLock(ctx context.Context, jobID string, estimatedJobCost float64, maxBudgetCents float64) (string, error)
	DecrementConcurrencyLock(ctx context.Context, estimatedJobCost float64) error
	GetStaleJobs(ctx context.Context, ageHours int) ([]JobRecord, error)
}

// EventPublisher defines the message bus interface for lifecycle events.
type EventPublisher interface {
	PublishJobEvent(ctx context.Context, eventType string, jobID string, traceID string, spanID string, metadata map[string]string) error
}

// CloudTasksClient defines the background job submission interface.
type CloudTasksClient interface {
	EnqueueJob(ctx context.Context, jobID string, traceID string) error
}

// HFValidator defines the external validation of Hugging Face repositories.
type HFValidator interface {
	ValidateFileSize(ctx context.Context, hfURL string, maxAllowedBytes int64) error
}
