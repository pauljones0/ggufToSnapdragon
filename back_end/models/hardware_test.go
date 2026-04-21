package models

import "testing"

func TestGetHardwareProfiles_AllArchsPresent(t *testing.T) {
	expected := []string{"v85", "v81", "v80", "v79", "v75", "v73", "v69", "v68", "698"}
	profiles := GetHardwareProfiles()

	for _, arch := range expected {
		p, ok := profiles[arch]
		if !ok {
			t.Errorf("expected DSP arch %q to be present in hardware profiles", arch)
			continue
		}
		if p.MaxNPUOnlyModelSizeB <= 0 {
			t.Errorf("arch %q: MaxNPUOnlyModelSizeB must be > 0, got %d", arch, p.MaxNPUOnlyModelSizeB)
		}
		if p.RecommendedHostRAMGB <= 0 {
			t.Errorf("arch %q: RecommendedHostRAMGB must be > 0, got %d", arch, p.RecommendedHostRAMGB)
		}
		if p.RecommendedVCpuCount <= 0 {
			t.Errorf("arch %q: RecommendedVCpuCount must be > 0, got %d", arch, p.RecommendedVCpuCount)
		}
		if len(p.SupportedQuantizations) == 0 {
			t.Errorf("arch %q: SupportedQuantizations must not be empty", arch)
		}
	}
}

func TestGetHardwareProfiles_SizeConstraintsAreMonotonic(t *testing.T) {
	// Higher-generation architectures should support equal-or-larger models.
	profiles := GetHardwareProfiles()
	if profiles["v75"].MaxNPUOnlyModelSizeB < profiles["v68"].MaxNPUOnlyModelSizeB {
		t.Errorf("expected v75 max model size >= v68, got v75=%d v68=%d",
			profiles["v75"].MaxNPUOnlyModelSizeB, profiles["v68"].MaxNPUOnlyModelSizeB)
	}
	if profiles["v79"].MaxNPUOnlyModelSizeB < profiles["v75"].MaxNPUOnlyModelSizeB {
		t.Errorf("expected v79 max model size >= v75, got v79=%d v75=%d",
			profiles["v79"].MaxNPUOnlyModelSizeB, profiles["v75"].MaxNPUOnlyModelSizeB)
	}
	if profiles["v81"].MaxNPUOnlyModelSizeB < profiles["v80"].MaxNPUOnlyModelSizeB {
		t.Errorf("expected v81 max model size >= v80, got v81=%d v80=%d",
			profiles["v81"].MaxNPUOnlyModelSizeB, profiles["v80"].MaxNPUOnlyModelSizeB)
	}
	if profiles["v85"].MaxNPUOnlyModelSizeB < profiles["v81"].MaxNPUOnlyModelSizeB {
		t.Errorf("expected v85 max model size >= v81, got v85=%d v81=%d",
			profiles["v85"].MaxNPUOnlyModelSizeB, profiles["v81"].MaxNPUOnlyModelSizeB)
	}
}

func TestGetDeviceDictionary_KnownDevices(t *testing.T) {
	cases := []struct {
		device      string
		wantDSPArch string
		wantChipset string
	}{
		{"Xiaomi 15", "v85", "SM8750"},
		{"Samsung Galaxy S25", "v80", "SM8750"},
		{"Samsung Galaxy S24 Ultra", "v75", "SM8650-AB"},
		{"OnePlus 12R", "v73", "SM8550-AB"},
		{"OnePlus 9 Pro", "v68", "SM8350"},
		{"Black Shark 4", "698", "SM8250-AC"},
	}

	dict := GetDeviceDictionary()
	for _, tc := range cases {
		t.Run(tc.device, func(t *testing.T) {
			entry, ok := dict[tc.device]
			if !ok {
				t.Fatalf("device %q not found in dictionary", tc.device)
			}
			if entry.HexagonDSPArchitecture != tc.wantDSPArch {
				t.Errorf("device %q: expected DSP arch %q, got %q", tc.device, tc.wantDSPArch, entry.HexagonDSPArchitecture)
			}
			if entry.Chipset != tc.wantChipset {
				t.Errorf("device %q: expected chipset %q, got %q", tc.device, tc.wantChipset, entry.Chipset)
			}
		})
	}
}

func TestGetDeviceDictionary_UnknownDevice(t *testing.T) {
	dict := GetDeviceDictionary()
	_, ok := dict["Nokia 3310"]
	if ok {
		t.Error("Nokia 3310 should not be in the device dictionary")
	}
}

func TestGetHardwareProfiles_NewFieldsPopulated(t *testing.T) {
	profiles := GetHardwareProfiles()
	for arch, p := range profiles {
		if p.EstimatedINT8TOPS <= 0 {
			t.Errorf("arch %q: EstimatedINT8TOPS must be > 0, got %d", arch, p.EstimatedINT8TOPS)
		}
		if p.MemBandwidthGBps <= 0 {
			t.Errorf("arch %q: MemBandwidthGBps must be > 0, got %d", arch, p.MemBandwidthGBps)
		}
		if p.VTCMSizeKB <= 0 {
			t.Errorf("arch %q: VTCMSizeKB must be > 0, got %d", arch, p.VTCMSizeKB)
		}
		if p.HTPGeneration <= 0 {
			t.Errorf("arch %q: HTPGeneration must be > 0, got %d", arch, p.HTPGeneration)
		}
		if p.MaxSessionMemoryGB <= 0 {
			t.Errorf("arch %q: MaxSessionMemoryGB must be > 0, got %.2f", arch, p.MaxSessionMemoryGB)
		}
		if p.MmapBudgetMB <= 0 {
			t.Errorf("arch %q: MmapBudgetMB must be > 0, got %d", arch, p.MmapBudgetMB)
		}
		if p.KVOffsetBufferMB < 0 {
			t.Errorf("arch %q: KVOffsetBufferMB must be >= 0, got %d", arch, p.KVOffsetBufferMB)
		}
		// MoECapable is a bool, so no easy > 0 check, but we can check consistency in another test.
	}
}

func TestGetHardwareProfiles_TOPSMonotonic(t *testing.T) {
	profiles := GetHardwareProfiles()
	// Higher-gen architectures should have equal or greater TOPS.
	if profiles["v75"].EstimatedINT8TOPS < profiles["v69"].EstimatedINT8TOPS {
		t.Errorf("expected v75 TOPS >= v69, got v75=%d v69=%d",
			profiles["v75"].EstimatedINT8TOPS, profiles["v69"].EstimatedINT8TOPS)
	}
	if profiles["v79"].EstimatedINT8TOPS < profiles["v75"].EstimatedINT8TOPS {
		t.Errorf("expected v79 TOPS >= v75, got v79=%d v75=%d",
			profiles["v79"].EstimatedINT8TOPS, profiles["v75"].EstimatedINT8TOPS)
	}
	if profiles["v80"].EstimatedINT8TOPS < profiles["v79"].EstimatedINT8TOPS {
		t.Errorf("expected v80 TOPS >= v79, got v80=%d v79=%d",
			profiles["v80"].EstimatedINT8TOPS, profiles["v79"].EstimatedINT8TOPS)
	}
	if profiles["v81"].EstimatedINT8TOPS < profiles["v80"].EstimatedINT8TOPS {
		t.Errorf("expected v81 TOPS >= v80, got v81=%d v80=%d",
			profiles["v81"].EstimatedINT8TOPS, profiles["v80"].EstimatedINT8TOPS)
	}
	if profiles["v85"].EstimatedINT8TOPS < profiles["v81"].EstimatedINT8TOPS {
		t.Errorf("expected v85 TOPS >= v81, got v85=%d v81=%d",
			profiles["v85"].EstimatedINT8TOPS, profiles["v81"].EstimatedINT8TOPS)
	}
}

func TestGetHardwareProfiles_32BitSessionMemory(t *testing.T) {
	// All 32-bit cDSP architectures must have MaxSessionMemoryGB <= 4.0.
	profiles := GetHardwareProfiles()
	thirtyTwoBitArchs := []string{"v68", "v69", "v73", "698"}
	for _, arch := range thirtyTwoBitArchs {
		p, ok := profiles[arch]
		if !ok {
			continue
		}
		if p.MaxSessionMemoryGB > 4.0 {
			t.Errorf("arch %q: 32-bit cDSP must have MaxSessionMemoryGB <= 4.0, got %.2f", arch, p.MaxSessionMemoryGB)
		}
	}
}

func TestGetHardwareProfiles_IQ4NL_SupportedOnV73Plus(t *testing.T) {
	profiles := GetHardwareProfiles()
	archsThatShouldSupport := []string{"v73", "v75", "v79"}
	for _, arch := range archsThatShouldSupport {
		p := profiles[arch]
		found := false
		for _, q := range p.SupportedQuantizations {
			if q == "IQ4_NL" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("arch %q: must include IQ4_NL in SupportedQuantizations", arch)
		}
	}
}

func TestGetHardwareProfiles_LogitsOffloadConsistency(t *testing.T) {
	profiles := GetHardwareProfiles()
	for arch, p := range profiles {
		// If session memory is very constrained (<= 3.75GB), logits offload should be true.
		if p.MaxSessionMemoryGB <= 3.75 && !p.NeedsLogitsOffload {
			t.Errorf("arch %q: MaxSessionMemoryGB=%.2f but NeedsLogitsOffload is false", arch, p.MaxSessionMemoryGB)
		}
	}
}

func TestGetDeviceDictionary_AllDevicesHaveValidDSPArch(t *testing.T) {
	dict := GetDeviceDictionary()
	profiles := GetHardwareProfiles()

	for device, entry := range dict {
		if _, ok := profiles[entry.HexagonDSPArchitecture]; !ok {
			t.Errorf("device %q maps to DSP arch %q which has no hardware profile", device, entry.HexagonDSPArchitecture)
		}
		if entry.Chipset == "" {
			t.Errorf("device %q has an empty chipset", device)
		}
	}
}

func TestGetHardwareProfiles_NativeINT4Consistency(t *testing.T) {
	profiles := GetHardwareProfiles()
	// v73 and above support native INT4.
	supportExpected := map[string]bool{
		"v79": true,
		"v75": true,
		"v73": true,
		"v69": false,
		"v68": false,
		"698": false,
	}

	for arch, expected := range supportExpected {
		p, ok := profiles[arch]
		if !ok {
			continue
		}
		if p.NativeINT4Support != expected {
			t.Errorf("arch %q: expected NativeINT4Support=%v, got %v", arch, expected, p.NativeINT4Support)
		}
	}
}

func TestGetHardwareProfiles_SupportsINT2Consistency(t *testing.T) {
	profiles := GetHardwareProfiles()
	supportExpected := map[string]bool{
		"v85": true,
		"v81": false,
		"v80": false,
		"v79": false,
		"v75": false,
		"v73": false,
		"v69": false,
		"v68": false,
		"698": false,
	}

	for arch, expected := range supportExpected {
		p, ok := profiles[arch]
		if !ok {
			continue
		}
		if p.SupportsINT2 != expected {
			t.Errorf("arch %q: expected SupportsINT2=%v, got %v", arch, expected, p.SupportsINT2)
		}
	}
}

func TestGetHardwareProfiles_HasHMXConsistency(t *testing.T) {
	profiles := GetHardwareProfiles()
	// v73 and above have HMX.
	hmxExpected := map[string]bool{
		"v79": true,
		"v75": true,
		"v73": true,
		"v69": false,
		"v68": false,
		"698": false,
	}

	for arch, expected := range hmxExpected {
		p, ok := profiles[arch]
		if !ok {
			continue
		}
		if p.HasHMX != expected {
			t.Errorf("arch %q: expected HasHMX=%v, got %v", arch, expected, p.HasHMX)
		}
	}
}

func TestGetHardwareProfiles_SplitThresholdConsistency(t *testing.T) {
	profiles := GetHardwareProfiles()
	// v73 and v75 are 32-bit cDSP and can handle models larger than 3.75GB, so they need a threshold.
	// v79 is 64-bit/extended (8GB) and doesn't need it for current model limits.
	// Legacy (v69, v68, 698) are 32-bit but their max model size is <= 3B, so they don't strictly need a threshold in this static config.

	if profiles["v75"].SplitModelThresholdB <= 0 {
		t.Error("v75 should have a positive SplitModelThresholdB due to 3.75GB session limit")
	}
	if profiles["v73"].SplitModelThresholdB <= 0 {
		t.Error("v73 should have a positive SplitModelThresholdB due to 3.75GB session limit")
	}
	if profiles["v79"].SplitModelThresholdB != 0 {
		t.Errorf("v79 should have 0 SplitModelThresholdB (monolithic support up to 14B), got %d", profiles["v79"].SplitModelThresholdB)
	}
}

func TestValidateTensorDimensions(t *testing.T) {
	profiles := GetHardwareProfiles()
	v81Profile := profiles["v81"]
	v68Profile := profiles["v68"]

	// Valid size on v81 (Limit 16384)
	err := v81Profile.ValidateTensorDimensions(4096, 12) // packedChannels = 1024. 1024 * 12 = 12288 < 16384
	if err != nil {
		t.Errorf("v81 should support tensor size 4096x12, got error: %v", err)
	}

	// Invalid size on v68 (Limit 2048)
	err = v68Profile.ValidateTensorDimensions(4096, 8) // packedChannels = 1024. 1024 * 8 = 8192 > 2048
	if err == nil {
		t.Errorf("v68 should not support tensor size 4096x8, expected error due to capacity limits")
	}
}

func TestSupportsBFloat16_MonotonicProgression(t *testing.T) {
	profiles := GetHardwareProfiles()
	if !profiles["v85"].SupportsBFloat16 {
		t.Errorf("expected v85 to support BFloat16")
	}
	if !profiles["v81"].SupportsBFloat16 {
		t.Errorf("expected v81 to support BFloat16")
	}
	if profiles["v80"].SupportsBFloat16 {
		t.Errorf("expected v80 not to support BFloat16 natively")
	}
}

func TestSupportsByteLevelVTCM_MonotonicProgression(t *testing.T) {
	profiles := GetHardwareProfiles()
	if !profiles["v81"].SupportsByteLevelVTCM {
		t.Errorf("expected v81 to support Byte Level VTCM")
	}
	if profiles["v79"].SupportsByteLevelVTCM {
		t.Errorf("expected v79 not to support Byte Level VTCM")
	}
}
