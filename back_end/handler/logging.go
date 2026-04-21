package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// gcpLogEntry represents a GCP Cloud Logging compliant JSON log entry.
// When printed to stdout in a Cloud Function environment, the Cloud Logging agent
// automatically ingests and indexes these fields, enabling trace correlation
// across the entire HexForge pipeline (Frontend → SubmitJob → QueueManager → Cloud Batch Worker).
//
// Fields follow the GCP structured logging special JSON fields specification:
// https://cloud.google.com/logging/docs/structured-logging#special-payload-fields
type gcpLogEntry struct {
	Severity  string            `json:"severity"`
	Message   string            `json:"message"`
	Component string            `json:"component,omitempty"`
	Trace     string            `json:"logging.googleapis.com/trace,omitempty"`
	SpanID    string            `json:"logging.googleapis.com/spanId,omitempty"`
	Timestamp string            `json:"timestamp,omitempty"`
	Labels    map[string]string `json:"logging.googleapis.com/labels,omitempty"`
	InsertID  string            `json:"logging.googleapis.com/insertId,omitempty"`
}

// NewSpanID generates a random 16-character hex string suitable for use as a
// GCP Cloud Logging span ID. Each handler invocation should call this once at
// entry to create a unique span, enabling per-invocation log grouping in
// Cloud Trace alongside the end-to-end Trace ID.
func NewSpanID() string {
	b := make([]byte, 8) // 8 bytes = 16 hex chars
	if _, err := rand.Read(b); err != nil {
		// Fallback: use a truncated UUID if crypto/rand fails
		return uuid.New().String()[:16]
	}
	return hex.EncodeToString(b)
}

// logJSON emits a single structured log line to stdout in GCP Cloud Logging format.
// It replaces all log.Printf calls across the backend to enable end-to-end trace
// correlation with the worker plane's log_json() output.
//
// Parameters:
//   - severity: GCP severity level (DEBUG, INFO, WARNING, ERROR, CRITICAL)
//   - message: The human-readable log message
//   - traceID: The end-to-end UUID trace ID (empty string is safe for non-traced contexts like StaleJobReaper)
//   - spanID: A 16-char hex span ID scoped to this handler invocation (use NewSpanID() at handler entry)
//   - component: The originating Cloud Function name (e.g. "SubmitJob", "QueueManager")
//   - labels: Optional key-value pairs for structured filtering (e.g. job_id, user_id).
//     Pass nil or omit for log calls that don't need labels.
func logJSON(severity, message, traceID, spanID, component string, labels ...map[string]string) {
	entry := gcpLogEntry{
		Severity:  severity,
		Message:   message,
		Component: component,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		InsertID:  uuid.New().String(),
	}

	if spanID != "" {
		entry.SpanID = spanID
	}

	if traceID != "" {
		projectID := os.Getenv("GCP_PROJECT_ID")
		if projectID != "" {
			entry.Trace = fmt.Sprintf("projects/%s/traces/%s", projectID, traceID)
		}
	}

	// Merge all label maps into a single map
	if len(labels) > 0 {
		merged := make(map[string]string)
		for _, lm := range labels {
			for k, v := range lm {
				merged[k] = v
			}
		}
		if len(merged) > 0 {
			entry.Labels = merged
		}
	}

	line, err := json.Marshal(entry)
	if err != nil {
		// Absolute fallback: if JSON marshaling itself fails, write raw text
		fmt.Fprintf(os.Stderr, `{"severity":"ERROR","message":"logJSON marshal failure: %v"}`, err)
		return
	}

	// Write directly to stdout. We avoid the `log` package because its prefix
	// (timestamp + file info) breaks GCP's JSON log parser.
	fmt.Fprintln(os.Stdout, string(line))
}

// ParseCloudTraceContext extracts the trace ID from a GCP X-Cloud-Trace-Context header.
// Format: TRACE_ID/SPAN_ID;o=TRACE_TRUE
// Returns (traceID, spanID) or empty strings if the header is not present/parseable.
func ParseCloudTraceContext(header string) (string, string) {
	if header == "" {
		return "", ""
	}
	// Split on "/" to get traceID and the rest
	parts := strings.SplitN(header, "/", 2)
	traceID := parts[0]
	spanID := ""
	if len(parts) > 1 {
		// spanID is before the ";o=" options
		spanParts := strings.SplitN(parts[1], ";", 2)
		spanID = spanParts[0]
	}
	return traceID, spanID
}
