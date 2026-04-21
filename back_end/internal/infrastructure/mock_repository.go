package infrastructure

import (
	"context"
	"fmt"
	"sync"
	"time"

	"hexforge-backend/internal/domain"
)

// MockJobRepository implements domain.JobRepository for testing.
type MockJobRepository struct {
	mu              sync.Mutex
	Jobs            map[string]*domain.JobRecord
	ConcurrencyLock float64
}

func NewMockJobRepository() *MockJobRepository {
	return &MockJobRepository{
		Jobs: make(map[string]*domain.JobRecord),
	}
}

func (m *MockJobRepository) GetJob(ctx context.Context, jobID string) (*domain.JobRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job, ok := m.Jobs[jobID]; ok {
		return job, nil
	}
	return nil, fmt.Errorf("job not found")
}

func (m *MockJobRepository) SaveJob(ctx context.Context, job *domain.JobRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Jobs[job.JobID] = job
	return nil
}

func (m *MockJobRepository) UpdateJobStatus(ctx context.Context, jobID string, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job, ok := m.Jobs[jobID]; ok {
		job.Status = status
		job.UpdatedAt = time.Now().UTC()
		return nil
	}
	return fmt.Errorf("job not found")
}

func (m *MockJobRepository) GetActiveJobCountForUser(ctx context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, job := range m.Jobs {
		if job.UserID == userID && job.Status != "Completed" && job.Status != "Failed" {
			count++
		}
	}
	return count, nil
}

func (m *MockJobRepository) GetGlobalQueuedCount(ctx context.Context, limit int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, job := range m.Jobs {
		if job.Status == "Queued" {
			count++
		}
	}
	if count > limit {
		return limit, nil
	}
	return count, nil
}

func (m *MockJobRepository) FindCompletedCache(ctx context.Context, hfURL, dspArch string) (*domain.JobRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.Jobs {
		if job.Status == "Completed" && job.HFUrl == hfURL && job.DSPArch == dspArch {
			return job, nil
		}
	}
	return nil, fmt.Errorf("not found in cache")
}

func (m *MockJobRepository) ExecuteConcurrencyLock(ctx context.Context, jobID string, estimatedCost float64, maxBudget float64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Jobs[jobID]; !ok {
		return "", fmt.Errorf("job not found")
	}
	if m.ConcurrencyLock+estimatedCost > maxBudget {
		return "", fmt.Errorf("finops budget limit reached")
	}
	m.ConcurrencyLock += estimatedCost
	m.Jobs[jobID].Status = "Provisioning"
	return "hexforge-task-" + jobID, nil
}

func (m *MockJobRepository) DecrementConcurrencyLock(ctx context.Context, estimatedCost float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ConcurrencyLock -= estimatedCost
	if m.ConcurrencyLock < 0 {
		m.ConcurrencyLock = 0
	}
	return nil
}

func (m *MockJobRepository) GetStaleJobs(ctx context.Context, ageHours int) ([]domain.JobRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Dummy implementation for brevity
	return []domain.JobRecord{}, nil
}
