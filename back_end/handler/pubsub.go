package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"cloud.google.com/go/pubsub"
)

var (
	pubsubClient *pubsub.Client
	pubsubOnce   sync.Once
	pubsubTopic  *pubsub.Topic
)

// JobEvent represents a lifecycle event published to the Pub/Sub topic.
type JobEvent struct {
	EventType string            `json:"event_type"` // e.g. EventJobSubmitted, EventJobProvisioning, etc. See events.go
	JobID     string            `json:"job_id"`
	TraceID   string            `json:"trace_id,omitempty"`
	SpanID    string            `json:"span_id,omitempty"`
	Timestamp string            `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"` // Additional context (user_id, phone_model, etc.)
}

// initPubSub lazily initializes the Pub/Sub client and topic.
// Returns false if Pub/Sub is not configured (graceful degradation for local dev).
func initPubSub() bool {
	pubsubOnce.Do(func() {
		projectID := os.Getenv("GCP_PROJECT_ID")
		topicName := os.Getenv("PUBSUB_TOPIC")

		if projectID == "" || topicName == "" {
			logJSON("DEBUG", "PUBSUB_TOPIC or GCP_PROJECT_ID not set, Pub/Sub event publishing disabled", "", "", "PubSub")
			return
		}

		ctx := context.Background()
		client, err := pubsub.NewClient(ctx, projectID)
		if err != nil {
			logJSON("ERROR", fmt.Sprintf("Failed to initialize Pub/Sub client: %v", err), "", "", "PubSub")
			return
		}
		pubsubClient = client
		pubsubTopic = client.Topic(topicName)
		// Enable message ordering by job_id for consistent event sequencing
		pubsubTopic.EnableMessageOrdering = true
	})

	return pubsubClient != nil && pubsubTopic != nil
}

// PublishJobEvent publishes a job lifecycle event to the hexforge-job-events Pub/Sub topic.
// This is fire-and-forget: failures are logged but never block the main pipeline.
// Uses context.Background() with a timeout instead of the request context to prevent
// silent publish failures when the Cloud Function's HTTP handler returns before the
// publish confirmation arrives.
func PublishJobEvent(ctx context.Context, eventType, jobID, traceID, spanID string, metadata map[string]string) {
	if !initPubSub() {
		return
	}

	event := JobEvent{
		EventType: eventType,
		JobID:     jobID,
		TraceID:   traceID,
		SpanID:    spanID,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Metadata:  metadata,
	}

	data, err := json.Marshal(event)
	if err != nil {
		logJSON("ERROR", fmt.Sprintf("Failed to marshal Pub/Sub event: %v", err), traceID, spanID, "PubSub")
		return
	}

	result := pubsubTopic.Publish(ctx, &pubsub.Message{
		Data:        data,
		OrderingKey: jobID,
		Attributes: map[string]string{
			"event_type": eventType,
			"job_id":     jobID,
			"trace_id":   traceID,
			"span_id":    spanID,
		},
	})

	// Non-blocking: we log failures but don't retry or block the handler.
	// CRITICAL: Use context.Background() with a timeout instead of the request context.
	// The HTTP request context is cancelled when the Cloud Function handler returns,
	// which would silently drop the publish confirmation goroutine.
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := result.Get(bgCtx)
		if err != nil {
			logJSON("WARNING", fmt.Sprintf("Failed to publish Pub/Sub event %s for job %s: %v", eventType, jobID, err), traceID, spanID, "PubSub")
		}
	}()
}
