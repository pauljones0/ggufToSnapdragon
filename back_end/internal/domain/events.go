package domain

// Event types constants
const (
	EventJobSubmitted    = "job.submitted"
	EventJobProvisioning = "job.provisioning"
	EventJobDownloading  = "job.downloading"
	EventJobCompiling    = "job.compiling"
	EventJobUploading    = "job.uploading"
	EventJobCompleted    = "job.completed"
	EventJobFailed       = "job.failed"
	EventJobDeadLettered = "job.dead_lettered"
	EventJobReaped       = "job.reaped"
)

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

var CriticalEventTypes = map[string]bool{
	EventJobFailed:       true,
	EventJobDeadLettered: true,
	EventJobReaped:       true,
}
