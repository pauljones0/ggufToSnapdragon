package domain

const (
	VCpuSpotCentsPerHour  = 0.8
	RamGbSpotCentsPerHour = 0.1
	MaxBudgetCentsPerHour = 100.0
	StandardVCpuCount     = 16
)

func EstimateJobCost(vcpuCount int, ramGB int) float64 {
	return (float64(vcpuCount) * VCpuSpotCentsPerHour) + (float64(ramGB) * RamGbSpotCentsPerHour)
}
