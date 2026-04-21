package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

// pubSubPushMessage represents the envelope sent by Pub/Sub push subscriptions.
type pubSubPushMessage struct {
	Message struct {
		Data       string            `json:"data"`
		Attributes map[string]string `json:"attributes"`
		MessageID  string            `json:"messageId"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

// EventProcessor is a Cloud Function triggered by a Pub/Sub push subscription.
// It consumes job lifecycle events, performs audit logging, and routes critical
// events (failures, reaps, dead letters) to elevated alerting severity.
func EventProcessor(w http.ResponseWriter, r *http.Request) {
	spanID := NewSpanID()
	component := "EventProcessor"

	// Parse the Pub/Sub push envelope
	var envelope pubSubPushMessage
	if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
		logJSON("ERROR", fmt.Sprintf("Failed to decode Pub/Sub envelope: %v", err), "", spanID, component)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Decode the base64 job event payload
	decodedData, err := base64.StdEncoding.DecodeString(envelope.Message.Data)
	if err != nil {
		logJSON("ERROR", fmt.Sprintf("Failed to decode base64 event data: %v", err), "", spanID, component)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Unmarshal the JobEvent
	var event JobEvent
	if err := json.Unmarshal(decodedData, &event); err != nil {
		logJSON("ERROR", fmt.Sprintf("Failed to unmarshal JobEvent: %v", err), "", spanID, component)
		// We acknowledge the message anyway to prevent Pub/Sub from retrying malformed JSON indefinitely
		w.WriteHeader(http.StatusOK)
		return
	}

	// Extract lifecycle context for logging
	traceID := event.TraceID
	jobID := event.JobID
	eventType := event.EventType

	// Common labels for correlation
	labels := map[string]string{
		"job_id":     jobID,
		"event_type": eventType,
	}
	if event.Metadata != nil {
		for k, v := range event.Metadata {
			labels[k] = v
		}
	}

	// 1. Audit Logging (Info level for all events)
	logMsg := fmt.Sprintf("Processed lifecycle event: %s", eventType)
	logJSON("INFO", logMsg, traceID, spanID, component, labels)

	// 2. Alert Routing
	// If the event is in the Critical set, emit a higher severity log for operational alerting.
	if CriticalEventTypes[eventType] {
		severity := "CRITICAL"
		// DeadLettered events are critical as they represent total retry exhaustion
		if eventType == EventJobDeadLettered {
			severity = "ALERT"
		}

		alertMsg := fmt.Sprintf("LIFECYCLE ALERT [%s]: Job %s has entered a critical state", eventType, jobID)
		logJSON(severity, alertMsg, traceID, spanID, component, labels)
	}

	// 3. Validation Logging
	// If the event type is unrecognized, log a warning for architectural drift
	if !ValidEventTypes[eventType] {
		warnMsg := fmt.Sprintf("Unrecognized event type: %s", eventType)
		logJSON("WARNING", warnMsg, traceID, spanID, component, labels)
	}

	// Acknowledge receipt
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}
