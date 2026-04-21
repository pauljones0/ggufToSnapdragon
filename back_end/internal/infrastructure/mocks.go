package infrastructure

import (
	"context"
	"sync"
)

// MockEventPublisher implements domain.EventPublisher for testing.
type MockEventPublisher struct {
	mu              sync.Mutex
	PublishedEvents []struct {
		EventType string
		JobID     string
		TraceID   string
		SpanID    string
		Metadata  map[string]string
	}
}

func NewMockEventPublisher() *MockEventPublisher {
	return &MockEventPublisher{}
}

func (m *MockEventPublisher) PublishJobEvent(ctx context.Context, eventType string, jobID string, traceID string, spanID string, metadata map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PublishedEvents = append(m.PublishedEvents, struct {
		EventType string
		JobID     string
		TraceID   string
		SpanID    string
		Metadata  map[string]string
	}{
		EventType: eventType,
		JobID:     jobID,
		TraceID:   traceID,
		SpanID:    spanID,
		Metadata:  metadata,
	})
	return nil
}

// MockCloudTasksClient implements domain.CloudTasksClient for testing.
// Set ReturnError to a non-nil error to simulate enqueue failures.
type MockCloudTasksClient struct {
	mu           sync.Mutex
	ReturnError  error
	EnqueuedJobs []struct {
		JobID   string
		TraceID string
	}
}

func NewMockCloudTasksClient() *MockCloudTasksClient {
	return &MockCloudTasksClient{}
}

// NewMockCloudTasksClientWithError creates a client that always returns the given error on EnqueueJob.
func NewMockCloudTasksClientWithError(err error) *MockCloudTasksClient {
	return &MockCloudTasksClient{ReturnError: err}
}

func (m *MockCloudTasksClient) EnqueueJob(ctx context.Context, jobID string, traceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ReturnError != nil {
		return m.ReturnError
	}
	m.EnqueuedJobs = append(m.EnqueuedJobs, struct {
		JobID   string
		TraceID string
	}{
		JobID:   jobID,
		TraceID: traceID,
	})
	return nil
}
