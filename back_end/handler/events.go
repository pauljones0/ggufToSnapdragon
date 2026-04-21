package handler

// Centralized event type constants for the HexForge Pub/Sub lifecycle event bus.
// All handlers MUST use these constants instead of raw strings to ensure
// compile-time safety and a single source of truth for the event catalog.
//
// Event naming convention: "job.<lowercase_state>"
// These map 1:1 to the Firestore job status transitions.
const (
	// EventJobSubmitted is published by SubmitJob when a new job enters the queue.
	EventJobSubmitted = "job.submitted"

	// EventJobProvisioning is published by QueueManager when a Cloud Batch VM is being created.
	EventJobProvisioning = "job.provisioning"

	// EventJobDownloading is published by the worker (via UpdateJobStatus or directly)
	// when the GGUF model download begins.
	EventJobDownloading = "job.downloading"

	// EventJobCompiling is published by the worker when QAIRT compilation begins.
	EventJobCompiling = "job.compiling"

	// EventJobUploading is published by the worker when the compiled artifact upload begins.
	EventJobUploading = "job.uploading"

	// EventJobCompleted is published by UpdateJobStatus when a job finishes successfully.
	EventJobCompleted = "job.completed"

	// EventJobFailed is published by UpdateJobStatus when a job fails.
	EventJobFailed = "job.failed"

	// EventJobDeadLettered is published by DeadLetterHandler when Cloud Tasks
	// exhausts all retries for a task.
	EventJobDeadLettered = "job.dead_lettered"

	// EventJobReaped is published by StaleJobReaper when a ghost job is forcibly terminated.
	EventJobReaped = "job.reaped"
)

// ValidEventTypes is the canonical set of all recognized job lifecycle event types.
// Used by EventProcessor to validate inbound Pub/Sub messages.
var ValidEventTypes = map[string]bool{
	EventJobSubmitted:    true,
	EventJobProvisioning: true,
	EventJobDownloading:  true,
	EventJobCompiling:    true,
	EventJobUploading:    true,
	EventJobCompleted:    true,
	EventJobFailed:       true,
	EventJobDeadLettered: true,
	EventJobReaped:       true,
}

// CriticalEventTypes are events that warrant elevated alerting severity.
// The EventProcessor emits CRITICAL/WARNING logs for these event types.
var CriticalEventTypes = map[string]bool{
	EventJobFailed:       true,
	EventJobDeadLettered: true,
	EventJobReaped:       true,
}
