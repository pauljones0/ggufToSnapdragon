package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"hexforge-backend/internal/domain"

	"cloud.google.com/go/pubsub"
)

var (
	pubsubClient *pubsub.Client
	pubsubTopic  *pubsub.Topic
	psOnce       sync.Once
)

type JobEvent struct {
	EventType string            `json:"event_type"`
	JobID     string            `json:"job_id"`
	TraceID   string            `json:"trace_id"`
	SpanID    string            `json:"span_id"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type PubSubPublisher struct {
	Client *pubsub.Client
	Topic  *pubsub.Topic
}

func NewPubSubPublisher(projectID string, topicName string) (*PubSubPublisher, error) {
	ctx := context.Background()
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, err
	}
	topic := client.Topic(topicName)
	topic.EnableMessageOrdering = true
	return &PubSubPublisher{Client: client, Topic: topic}, nil
}

func (p *PubSubPublisher) PublishJobEvent(ctx context.Context, eventType string, jobID string, traceID string, spanID string, metadata map[string]string) error {
	if !domain.ValidEventTypes[eventType] {
		return fmt.Errorf("unrecognized event type: %s", eventType)
	}

	event := JobEvent{
		EventType: eventType,
		JobID:     jobID,
		TraceID:   traceID,
		SpanID:    spanID,
		Timestamp: time.Now().UTC(),
		Metadata:  metadata,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	publishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := p.Topic.Publish(publishCtx, &pubsub.Message{
		Data:        payload,
		OrderingKey: jobID,
	})

	_, err = result.Get(publishCtx)
	return err
}

func PublishEventStatic(ctx context.Context, eventType, jobID, traceID, spanID string, meta map[string]string) {
	// Adapter to not break existing monolithic calls right away if I do a partial refactor.
	// Left unimplemented intentionally if not needed.
}
