package domain

import (
	"math"
	"testing"
)

func TestEstimateJobCost(t *testing.T) {
	tests := []struct {
		name      string
		vcpuCount int
		ramGB     int
		expected  float64
	}{
		{
			name:      "Standard v79 Profile",
			vcpuCount: 32,
			ramGB:     768,
			expected:  (32 * VCpuSpotCentsPerHour) + (768 * RamGbSpotCentsPerHour),
		},
		{
			name:      "Standard v68 Profile",
			vcpuCount: 8,
			ramGB:     128,
			expected:  (8 * VCpuSpotCentsPerHour) + (128 * RamGbSpotCentsPerHour),
		},
		{
			name:      "Zero Resources",
			vcpuCount: 0,
			ramGB:     0,
			expected:  0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateJobCost(tt.vcpuCount, tt.ramGB)
			if math.Abs(got-tt.expected) > 1e-9 {
				t.Errorf("EstimateJobCost() = %v, want %v", got, tt.expected)
			}
		})
	}
}
