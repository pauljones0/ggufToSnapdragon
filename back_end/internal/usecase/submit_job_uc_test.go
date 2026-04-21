package usecase

import (
	"context"
	"fmt"
	"testing"

	"hexforge-backend/internal/domain"
	"hexforge-backend/internal/infrastructure"
)

// newTestUseCase returns a fresh set of mocks and a use‑case wired to them.
// Tests that need to mutate mock state should capture the returned pointers.
func newTestUseCase() (*infrastructure.MockJobRepository, *infrastructure.MockEventPublisher, *infrastructure.MockCloudTasksClient, *SubmitJobUseCase) {
	repo := infrastructure.NewMockJobRepository()
	bus := infrastructure.NewMockEventPublisher()
	tasks := infrastructure.NewMockCloudTasksClient()
	uc := NewSubmitJobUseCase(repo, bus, tasks)
	return repo, bus, tasks, uc
}

func TestSubmitJobUseCase_Execute(t *testing.T) {
	ctx := context.Background()

	// ── Persona 1: Core Happy Path ─────────────────────────────────────────────

	t.Run("Valid_Job", func(t *testing.T) {
		_, bus, tasks, uc := newTestUseCase()
		req := domain.JobRequest{
			UserID:     "user1",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/author/model-3b",
		}

		record, err := uc.Execute(ctx, req, "trace123", "span456")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if record.Status != "Queued" {
			t.Errorf("expected Queued, got %s", record.Status)
		}
		if record.EstimatedINT8TOPS == 0 {
			t.Errorf("expected non-zero EstimatedINT8TOPS")
		}
		if record.MemBandwidthGBps == 0 {
			t.Errorf("expected non-zero MemBandwidthGBps")
		}
		if len(bus.PublishedEvents) != 1 {
			t.Errorf("expected 1 event, got %d", len(bus.PublishedEvents))
		}
		if len(tasks.EnqueuedJobs) != 1 {
			t.Errorf("expected 1 enqueued task, got %d", len(tasks.EnqueuedJobs))
		}
	})

	t.Run("Cache_Hit", func(t *testing.T) {
		repo, bus, _, uc := newTestUseCase()
		// Seed a completed job for the same URL + DSP arch
		_ = repo.SaveJob(ctx, &domain.JobRecord{
			JobID:   "cached-job",
			UserID:  "other-user",
			HFUrl:   "https://huggingface.co/author/model-3b",
			DSPArch: "v75", // Samsung Galaxy S24 Ultra maps to v75
			Status:  "Completed",
		})

		req := domain.JobRequest{
			UserID:     "user-cache",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/author/model-3b",
		}
		record, err := uc.Execute(ctx, req, "trace-cache", "span-cache")
		if err != nil {
			t.Fatalf("expected no error on cache hit, got %v", err)
		}
		if record.Status != "Completed" {
			t.Errorf("expected Completed (cache hit), got %s", record.Status)
		}
		// Cache hits bypass the task queue — no events should be published
		if len(bus.PublishedEvents) != 0 {
			t.Errorf("expected 0 events for cache hit, got %d", len(bus.PublishedEvents))
		}
	})

	t.Run("Enqueue_Failure_Does_Not_Publish_Event", func(t *testing.T) {
		repo := infrastructure.NewMockJobRepository()
		bus := infrastructure.NewMockEventPublisher()
		failingTasks := infrastructure.NewMockCloudTasksClientWithError(fmt.Errorf("cloud tasks unavailable"))
		uc := NewSubmitJobUseCase(repo, bus, failingTasks)

		req := domain.JobRequest{
			UserID:     "user-fail",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/author/model-3b",
		}
		record, err := uc.Execute(ctx, req, "trace-fail", "span-fail")
		if err != nil {
			t.Fatalf("enqueue failure should not surface as an error to the caller, got %v", err)
		}
		if record.Status != "Queued" {
			t.Errorf("expected Queued, got %s", record.Status)
		}
		// No event must be emitted when the task queue call fails
		if len(bus.PublishedEvents) != 0 {
			t.Errorf("expected 0 events when enqueue fails, got %d", len(bus.PublishedEvents))
		}
	})

	t.Run("Queue_Full", func(t *testing.T) {
		repo, _, _, uc := newTestUseCase()
		// Fill the queue to capacity
		for i := 0; i < 50; i++ {
			_ = repo.SaveJob(ctx, &domain.JobRecord{
				JobID:  fmt.Sprintf("dummy-%d", i),
				UserID: fmt.Sprintf("filler-%d", i),
				Status: "Queued",
			})
		}

		req := domain.JobRequest{
			UserID:     "lateuser",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/author/model-3b",
		}
		_, err := uc.Execute(ctx, req, "traceQ", "spanQ")
		if err == nil {
			t.Fatal("expected queue-full error, got nil")
		}
	})

	t.Run("User_Already_Has_Active_Job", func(t *testing.T) {
		repo, _, _, uc := newTestUseCase()
		_ = repo.SaveJob(ctx, &domain.JobRecord{
			JobID:  "existing-job",
			UserID: "busyuser",
			Status: "Queued",
		})

		req := domain.JobRequest{
			UserID:     "busyuser",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/author/model-3b",
		}
		_, err := uc.Execute(ctx, req, "traceU", "spanU")
		if err == nil {
			t.Fatal("expected already-active-job error, got nil")
		}
	})

	// ── Persona 1: Negative / Validation Paths ────────────────────────────────

	t.Run("Blacklisted_URL", func(t *testing.T) {
		_, _, _, uc := newTestUseCase()
		req := domain.JobRequest{
			UserID:     "badactor",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/unfiltered-malware-models/bad",
		}
		_, err := uc.Execute(ctx, req, "trace2", "span2")
		if err == nil {
			t.Fatal("expected error for blacklisted repository")
		}
	})

	t.Run("Exceeds_Hardware_Limit", func(t *testing.T) {
		_, _, _, uc := newTestUseCase()
		req := domain.JobRequest{
			UserID:     "user3",
			PhoneModel: "OnePlus 9 Pro", // v68, Max 3B
			HFUrl:      "https://huggingface.co/author/model-7b",
		}
		_, err := uc.Execute(ctx, req, "trace3", "span3")
		if err == nil {
			t.Fatal("expected error for exceeding hardware limit")
		}
	})

	t.Run("Unknown_Phone_Model", func(t *testing.T) {
		_, _, _, uc := newTestUseCase()
		req := domain.JobRequest{
			UserID:     "user4",
			PhoneModel: "Nokia 3310",
			HFUrl:      "https://huggingface.co/author/model-3b",
		}
		_, err := uc.Execute(ctx, req, "trace4", "span4")
		if err == nil {
			t.Fatal("expected error for unknown phone model")
		}
	})

	t.Run("No_Size_In_URL", func(t *testing.T) {
		_, _, _, uc := newTestUseCase()
		req := domain.JobRequest{
			UserID:     "user5",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/author/some-model-no-size",
		}
		_, err := uc.Execute(ctx, req, "trace5", "span5")
		if err == nil {
			t.Fatal("expected error for missing size in URL")
		}
	})

	// ── Persona 2: Security — Shell Injection ─────────────────────────────────

	t.Run("Shell_Injection_Semicolon", func(t *testing.T) {
		_, _, _, uc := newTestUseCase()
		req := domain.JobRequest{
			UserID:     "attacker",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/author/model-3b;rm -rf /",
		}
		_, err := uc.Execute(ctx, req, "traceSec1", "spanSec1")
		if err == nil {
			t.Fatal("expected error for shell injection character ';'")
		}
	})

	t.Run("Shell_Injection_Backtick", func(t *testing.T) {
		_, _, _, uc := newTestUseCase()
		req := domain.JobRequest{
			UserID:     "attacker",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/author/model-3b`whoami`",
		}
		_, err := uc.Execute(ctx, req, "traceSec2", "spanSec2")
		if err == nil {
			t.Fatal("expected error for shell injection character '`'")
		}
	})

	t.Run("Shell_Injection_Dollar_Sign", func(t *testing.T) {
		_, _, _, uc := newTestUseCase()
		req := domain.JobRequest{
			UserID:     "attacker",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/author/model-3b$(evil)",
		}
		_, err := uc.Execute(ctx, req, "traceSec3", "spanSec3")
		if err == nil {
			t.Fatal("expected error for shell injection character '$'")
		}
	})

	t.Run("Shell_Injection_Literal_Newline", func(t *testing.T) {
		_, _, _, uc := newTestUseCase()
		req := domain.JobRequest{
			UserID:     "attacker",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/author/model-3b\nevil",
		}
		_, err := uc.Execute(ctx, req, "traceSec4", "spanSec4")
		if err == nil {
			t.Fatal("expected error for literal newline in URL")
		}
	})

	t.Run("Shell_Injection_Literal_Tab", func(t *testing.T) {
		_, _, _, uc := newTestUseCase()
		req := domain.JobRequest{
			UserID:     "attacker",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/author/model-3b\tevil",
		}
		_, err := uc.Execute(ctx, req, "traceSec5", "spanSec5")
		if err == nil {
			t.Fatal("expected error for literal tab in URL")
		}
	})

	t.Run("Second_Blacklisted_Repo", func(t *testing.T) {
		_, _, _, uc := newTestUseCase()
		req := domain.JobRequest{
			UserID:     "attacker2",
			PhoneModel: "Samsung Galaxy S24 Ultra",
			HFUrl:      "https://huggingface.co/poc-malicious-gguf/model-3b",
		}
		_, err := uc.Execute(ctx, req, "traceSec6", "spanSec6")
		if err == nil {
			t.Fatal("expected error for second blacklisted repository 'poc-malicious-gguf'")
		}
	})
}
