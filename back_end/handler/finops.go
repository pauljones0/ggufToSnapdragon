package handler

// Shared FinOps cost estimation constants for Google Cloud Spot VM pricing.
// These constants are used by QueueManager, DeadLetterHandler, StaleJobReaper,
// and UpdateJobStatus to calculate and manage the dynamic budget lock.
//
// IMPORTANT: If GCP Spot pricing changes, update ONLY this file.
const (
	// VCpuSpotCentsPerHour is the estimated cost per vCPU-hour for Spot instances.
	VCpuSpotCentsPerHour = 0.8

	// RamGbSpotCentsPerHour is the estimated cost per GB-hour of RAM for Spot instances.
	RamGbSpotCentsPerHour = 0.1

	// MaxBudgetCentsPerHour is the FinOps ceiling: maximum allowed active spend rate
	// across all concurrently running Spot VMs. Exceeding this returns HTTP 429 to
	// Cloud Tasks, triggering exponential backoff.
	MaxBudgetCentsPerHour = 100.0

	// StandardVCpuCount is the fixed number of vCPUs allocated per compilation VM.
	StandardVCpuCount = 16
)

// EstimateJobCost calculates the estimated per-hour Spot VM cost for a job
// based on dynamic vCPU and RAM allocation from the hardware profile.
func EstimateJobCost(vcpuCount int, ramGB int) float64 {
	return (float64(vcpuCount) * VCpuSpotCentsPerHour) + (float64(ramGB) * RamGbSpotCentsPerHour)
}
