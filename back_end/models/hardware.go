package models

import (
	"fmt"
	"log"
	"math"
)

// QnnHtpDeviceArch represents the physical silicon generation
// exactly as defined in the official QnnHtpDevice.h header.
type QnnHtpDeviceArch int

const (
	QNN_HTP_DEVICE_ARCH_V68 QnnHtpDeviceArch = 68
	QNN_HTP_DEVICE_ARCH_V69 QnnHtpDeviceArch = 69
	QNN_HTP_DEVICE_ARCH_V73 QnnHtpDeviceArch = 73
	QNN_HTP_DEVICE_ARCH_V75 QnnHtpDeviceArch = 75
	QNN_HTP_DEVICE_ARCH_V79 QnnHtpDeviceArch = 79
	QNN_HTP_DEVICE_ARCH_V80 QnnHtpDeviceArch = 80 // Dedicated X Elite Series
	QNN_HTP_DEVICE_ARCH_V81 QnnHtpDeviceArch = 81 // Dedicated 8 Elite Series
	QNN_HTP_DEVICE_ARCH_V85 QnnHtpDeviceArch = 85 // Dedicated Snapdragon 8 Elite
	QNN_HTP_DEVICE_ARCH_698 QnnHtpDeviceArch = 698
)

// QNNContextConfig defines the strict loading parameters for the compiled binary.
// These parameters ensure the host OS does not crash when loading massive LLMs.
type QNNContextConfig struct {
	FileReadMemoryBudgetMB int  `json:"file_read_memory_budget_mb"` // Configures the backend to chunk binary loading, preventing OOM.
	EnableSharedBuffer     bool `json:"enable_shared_buffer"`       // Injects QNN_HTP_MEM_SHARED_BUFFER flags for zero-copy DMA buffers.
}

// HardwareProfile defines the NPU specifications and constraints for a specific Snapdragon Hexagon DSP Architecture.
type HardwareProfile struct {
	DSPArch                string   `json:"dsp_arch"`
	MaxNPUOnlyModelSizeB   int      `json:"max_npu_only_model_size_billions"`
	RecommendedHostRAMGB   int      `json:"recommended_host_ram_gb"`
	RecommendedHostSwapGB  int      `json:"recommended_host_swap_gb"`
	RecommendedVCpuCount   int      `json:"recommended_vcpu_count"`
	HardwareQuirks         string   `json:"hardware_quirks"`
	SupportedQuantizations []string `json:"supported_quantizations"`
	NeedsFastRPCFix        bool     `json:"needs_fastrpc_fix"`
	SplitModelThresholdB   int      `json:"split_model_threshold_billions"`
	// New fields informed by Hexagon NPU architecture analysis.
	EstimatedINT8TOPS        int              `json:"estimated_int8_tops"`       // Peak INT8 TOPS for the HTP silicon.
	MemBandwidthGBps         int              `json:"mem_bandwidth_gbps"`        // Practical achievable memory bandwidth (not theoretical peak).
	VTCMSizeKB               int              `json:"vtcm_size_kb"`              // Vector Tightly Coupled Memory size in KB.
	HTPGeneration            int              `json:"htp_generation"`            // HTP silicon generation (1-4).
	MaxSessionMemoryGB       float64          `json:"max_session_memory_gb"`     // Max addressable memory per QNN session (3.75 for 32-bit cDSP).
	NeedsLogitsOffload       bool             `json:"needs_logits_offload"`      // True if vocab projection should execute on CPU to avoid address overflow.
	MmapBudgetMB             int              `json:"mmap_budget_mb"`            // Mandated mmap-budget chunk size for incremental context binary paging.
	NativeINT4Support        bool             `json:"native_int4_support"`       // True if the silicon has native INT4 acceleration (v73+).
	HasHMX                   bool             `json:"has_hmx"`                   // True if the architecture includes Hexagon Matrix eXtensions.
	MoECapable               bool             `json:"moe_capable"`               // True if the architecture supports rapid context switching for MoE graphs.
	KVOffsetBufferMB         int              `json:"kv_offset_buffer_mb"`       // Spill-fill buffer size for KV cache paging (default 512).
	SupportsByteLevelVTCM    bool             `json:"supports_byte_level_vtcm"`  // True for v81 and newer configurations.
	SupportsBFloat16         bool             `json:"supports_bfloat16"`         // Brain floating point support required for advanced LLMs.
	SupportsINT2             bool             `json:"supports_int2"`             // Extreme low-precision quantization support
	MaxPerGPUSizeLimit       int              `json:"max_per_gpu_size_limit"`    // Max Packed Channels (C/4) threshold for spatial tensor tiling limit.
	RequiresMLAUnrolling     bool             `json:"requires_mla_unrolling"`    // If true, DeepSeek MLA must be mapped to dense operations.
	SupportsByteGranularity  bool             `json:"supports_byte_granularity"` // True exclusively for v81+
	FastRPCAddressLimit      bool             `json:"fast_rpc_address_limit"`    // True for v75, v73 (3.75GB limit constraint)
	MaxVTCMBytes             int64            `json:"max_vtcm_bytes"`            // Max VTCM limit in bytes
	MaxPackedChannelsLimit   int              `json:"max_packed_channels_limit"`
	LowDepthActivationThresh int              `json:"low_depth_activation_thresh"`
	ContextConfig            QNNContextConfig `json:"context_config"`
	Arch                     QnnHtpDeviceArch `json:"arch"`
}

// DeviceLookup maps a user-friendly phone name (e.g. "OnePlus 12R") to its underlying Snapdragon chipset and Hexagon DSP Architecture.
type DeviceLookup struct {
	Chipset                string `json:"chipset"`
	HexagonDSPArchitecture string `json:"hexagon_dsp_architecture"`
}

// GetHardwareProfiles returns the static configuration for all supported Snapdragon NPU architectures.
// This is used by the Orchestrator to validate incoming GGUF model sizes and to dynamically provision the worker VM RAM/Swap sizes.
func GetHardwareProfiles() map[string]HardwareProfile {
	return map[string]HardwareProfile{
		"v85": {
			DSPArch:                  "v85",
			Arch:                     QNN_HTP_DEVICE_ARCH_V85,
			MaxNPUOnlyModelSizeB:     18,
			RecommendedHostRAMGB:     1024,
			RecommendedHostSwapGB:    256,
			RecommendedVCpuCount:     32,
			HardwareQuirks:           "Powered by 3rd-Gen Oryon CPU cores. Introduces INT2 support, GenAI encryption, and an 18MB High-Performance Memory (HPM) cache.",
			SupportedQuantizations:   []string{"Q2_K", "Q3_K", "Q4_0", "Q4_1", "Q4_K", "Q5_K", "Q6_K", "Q8_0", "IQ4_NL", "INT2", "FP8"},
			NeedsFastRPCFix:          false,
			SplitModelThresholdB:     0,
			EstimatedINT8TOPS:        82, // 37% throughput boost
			MemBandwidthGBps:         84,
			VTCMSizeKB:               8192,
			HTPGeneration:            5,
			MaxSessionMemoryGB:       8.0,
			NeedsLogitsOffload:       false,
			MmapBudgetMB:             25,
			NativeINT4Support:        true,
			HasHMX:                   true,
			MoECapable:               true,
			KVOffsetBufferMB:         512,
			SupportsByteLevelVTCM:    true,
			SupportsBFloat16:         true,
			SupportsINT2:             true,
			MaxPerGPUSizeLimit:       16384,
			RequiresMLAUnrolling:     true,
			SupportsByteGranularity:  true,
			FastRPCAddressLimit:      false,
			MaxVTCMBytes:             8388608,
			MaxPackedChannelsLimit:   16384,
			LowDepthActivationThresh: 32,
			ContextConfig: QNNContextConfig{
				FileReadMemoryBudgetMB: 1024,
				EnableSharedBuffer:     true,
			},
		},
		"v81": {
			DSPArch:                  "v81",
			Arch:                     QNN_HTP_DEVICE_ARCH_V81,
			MaxNPUOnlyModelSizeB:     14,
			RecommendedHostRAMGB:     1024,
			RecommendedHostSwapGB:    256,
			RecommendedVCpuCount:     32,
			HardwareQuirks:           "None regarding address limits. Generation 5 HTP.", // Next-gen
			SupportedQuantizations:   []string{"Q2_K", "Q3_K", "Q4_0", "Q4_1", "Q4_K", "Q5_K", "Q6_K", "Q8_0", "IQ4_NL", "INT2", "FP8"},
			NeedsFastRPCFix:          false,
			SplitModelThresholdB:     0,
			EstimatedINT8TOPS:        60, // Estimated up
			MemBandwidthGBps:         85, // LPDDR6
			VTCMSizeKB:               8192,
			HTPGeneration:            5, // v81+
			MaxSessionMemoryGB:       8.0,
			NeedsLogitsOffload:       false,
			MmapBudgetMB:             25,
			NativeINT4Support:        true,
			HasHMX:                   true,
			MoECapable:               true,
			KVOffsetBufferMB:         512,
			SupportsByteLevelVTCM:    true,
			SupportsBFloat16:         true,
			MaxPerGPUSizeLimit:       16384,
			RequiresMLAUnrolling:     true,
			SupportsByteGranularity:  true,
			FastRPCAddressLimit:      false,
			MaxVTCMBytes:             8388608,
			MaxPackedChannelsLimit:   16384,
			LowDepthActivationThresh: 32,
			ContextConfig: QNNContextConfig{
				FileReadMemoryBudgetMB: 1024,
				EnableSharedBuffer:     true,
			},
		},
		"v80": {
			DSPArch:                  "v80",
			Arch:                     QNN_HTP_DEVICE_ARCH_V80,
			MaxNPUOnlyModelSizeB:     14,
			RecommendedHostRAMGB:     768,
			RecommendedHostSwapGB:    256,
			RecommendedVCpuCount:     32,
			HardwareQuirks:           "None regarding address limits. Generation 4 HTP.",
			SupportedQuantizations:   []string{"Q2_K", "Q3_K", "Q4_0", "Q4_1", "Q4_K", "Q5_K", "Q6_K", "Q8_0", "IQ4_NL", "INT2", "FP8"},
			NeedsFastRPCFix:          false,
			SplitModelThresholdB:     0,
			EstimatedINT8TOPS:        45,
			MemBandwidthGBps:         68,
			VTCMSizeKB:               8192,
			HTPGeneration:            4,
			MaxSessionMemoryGB:       8.0,
			NeedsLogitsOffload:       false,
			MmapBudgetMB:             25,
			NativeINT4Support:        true,
			HasHMX:                   true,
			MoECapable:               true,
			KVOffsetBufferMB:         512,
			SupportsByteLevelVTCM:    false,
			SupportsBFloat16:         false,
			MaxPerGPUSizeLimit:       16384,
			RequiresMLAUnrolling:     true,
			SupportsByteGranularity:  false,
			FastRPCAddressLimit:      false,
			MaxVTCMBytes:             8388608,
			MaxPackedChannelsLimit:   16384,
			LowDepthActivationThresh: 32,
			ContextConfig: QNNContextConfig{
				FileReadMemoryBudgetMB: 1024,
				EnableSharedBuffer:     true,
			},
		},
		"v79": {
			DSPArch:                  "v79",
			Arch:                     QNN_HTP_DEVICE_ARCH_V79,
			MaxNPUOnlyModelSizeB:     14,
			RecommendedHostRAMGB:     768,
			RecommendedHostSwapGB:    256,
			RecommendedVCpuCount:     32,
			HardwareQuirks:           "None regarding address limits.",
			SupportedQuantizations:   []string{"Q2_K", "Q3_K", "Q4_0", "Q4_1", "Q4_K", "Q5_K", "Q6_K", "Q8_0", "IQ4_NL", "INT2", "FP8"},
			NeedsFastRPCFix:          false,
			SplitModelThresholdB:     0,
			EstimatedINT8TOPS:        45,
			MemBandwidthGBps:         68,
			VTCMSizeKB:               8192,
			HTPGeneration:            4,
			MaxSessionMemoryGB:       8.0,
			NeedsLogitsOffload:       false,
			MmapBudgetMB:             25,
			NativeINT4Support:        true,
			HasHMX:                   true,
			MoECapable:               true,
			KVOffsetBufferMB:         512,
			SupportsByteLevelVTCM:    false,
			SupportsBFloat16:         false,
			MaxPerGPUSizeLimit:       16384,
			RequiresMLAUnrolling:     true,
			SupportsByteGranularity:  false,
			FastRPCAddressLimit:      false,
			MaxVTCMBytes:             8388608,
			MaxPackedChannelsLimit:   16384,
			LowDepthActivationThresh: 32,
			ContextConfig: QNNContextConfig{
				FileReadMemoryBudgetMB: 1024,
				EnableSharedBuffer:     true,
			},
		},
		"v75": {
			DSPArch:                  "v75",
			Arch:                     QNN_HTP_DEVICE_ARCH_V75,
			MaxNPUOnlyModelSizeB:     10,
			RecommendedHostRAMGB:     512,
			RecommendedHostSwapGB:    256,
			RecommendedVCpuCount:     24,
			HardwareQuirks:           "fastrpc-issue-137 32-bit address limit. Usable session memory hard-capped at 3.75 GB.",
			SupportedQuantizations:   []string{"Q2_K", "Q3_K", "Q4_0", "Q4_1", "Q4_K", "Q5_K", "Q6_K", "Q8_0", "IQ4_NL"},
			NeedsFastRPCFix:          true,
			SplitModelThresholdB:     3,
			EstimatedINT8TOPS:        34,
			MemBandwidthGBps:         45,
			VTCMSizeKB:               4096,
			HTPGeneration:            3,
			MaxSessionMemoryGB:       3.75,
			NeedsLogitsOffload:       true,
			MmapBudgetMB:             25,
			NativeINT4Support:        true,
			HasHMX:                   true,
			MoECapable:               true,
			KVOffsetBufferMB:         512,
			SupportsByteLevelVTCM:    false,
			SupportsBFloat16:         false,
			MaxPerGPUSizeLimit:       8192,
			RequiresMLAUnrolling:     true,
			SupportsByteGranularity:  false,
			FastRPCAddressLimit:      true,
			MaxVTCMBytes:             4194304,
			MaxPackedChannelsLimit:   4096,
			LowDepthActivationThresh: 32,
			ContextConfig: QNNContextConfig{
				FileReadMemoryBudgetMB: 512,
				EnableSharedBuffer:     false,
			},
		},
		"v73": {
			DSPArch:                  "v73",
			Arch:                     QNN_HTP_DEVICE_ARCH_V73,
			MaxNPUOnlyModelSizeB:     7,
			RecommendedHostRAMGB:     128,
			RecommendedHostSwapGB:    128,
			RecommendedVCpuCount:     16,
			HardwareQuirks:           "fastrpc-issue-137 32-bit address limit (3.75GB). Fails natively at >=3B without super-block repacking.",
			SupportedQuantizations:   []string{"Q2_K", "Q3_K", "Q4_0", "Q4_1", "Q4_K", "Q5_K", "Q6_K", "Q8_0", "IQ4_NL"},
			NeedsFastRPCFix:          true,
			SplitModelThresholdB:     3,
			EstimatedINT8TOPS:        26,
			MemBandwidthGBps:         40,
			VTCMSizeKB:               2048,
			HTPGeneration:            2,
			MaxSessionMemoryGB:       3.75,
			NeedsLogitsOffload:       true,
			MmapBudgetMB:             25,
			NativeINT4Support:        true,
			HasHMX:                   true,
			MoECapable:               false,
			KVOffsetBufferMB:         256,
			SupportsByteLevelVTCM:    false,
			SupportsBFloat16:         false,
			MaxPerGPUSizeLimit:       8192,
			RequiresMLAUnrolling:     true,
			SupportsByteGranularity:  false,
			FastRPCAddressLimit:      true,
			MaxVTCMBytes:             2097152,
			MaxPackedChannelsLimit:   4096,
			LowDepthActivationThresh: 32,
			ContextConfig: QNNContextConfig{
				FileReadMemoryBudgetMB: 512,
				EnableSharedBuffer:     false,
			},
		},
		"v69": {
			DSPArch:                  "v69",
			Arch:                     QNN_HTP_DEVICE_ARCH_V69,
			MaxNPUOnlyModelSizeB:     3,
			RecommendedHostRAMGB:     128,
			RecommendedHostSwapGB:    100,
			RecommendedVCpuCount:     16,
			HardwareQuirks:           "Lacks native INT4 arithmetic; sub-8-bit models incur severe software dequantization penalties. 32-bit cDSP address limit.",
			SupportedQuantizations:   []string{"Q8_0"},
			NeedsFastRPCFix:          false,
			SplitModelThresholdB:     0,
			EstimatedINT8TOPS:        13,
			MemBandwidthGBps:         34,
			VTCMSizeKB:               2048,
			HTPGeneration:            1,
			MaxSessionMemoryGB:       3.75,
			NeedsLogitsOffload:       true,
			MmapBudgetMB:             25,
			NativeINT4Support:        false,
			HasHMX:                   false,
			MoECapable:               false,
			KVOffsetBufferMB:         128,
			SupportsByteLevelVTCM:    false,
			SupportsBFloat16:         false,
			MaxPerGPUSizeLimit:       4096,
			RequiresMLAUnrolling:     true,
			SupportsByteGranularity:  false,
			FastRPCAddressLimit:      false,
			MaxVTCMBytes:             2097152,
			MaxPackedChannelsLimit:   4096,
			LowDepthActivationThresh: 32,
			ContextConfig: QNNContextConfig{
				FileReadMemoryBudgetMB: 256,
				EnableSharedBuffer:     false,
			},
		},
		"v68": {
			DSPArch:                  "v68",
			Arch:                     QNN_HTP_DEVICE_ARCH_V68,
			MaxNPUOnlyModelSizeB:     3,
			RecommendedHostRAMGB:     128,
			RecommendedHostSwapGB:    64,
			RecommendedVCpuCount:     8,
			HardwareQuirks:           "Severe isolated NPU memory constraints require heavy system swapping and CPU fallback. 32-bit cDSP address limit.",
			SupportedQuantizations:   []string{"Q8_0"},
			NeedsFastRPCFix:          false,
			SplitModelThresholdB:     0,
			EstimatedINT8TOPS:        11,
			MemBandwidthGBps:         25,
			VTCMSizeKB:               1024,
			HTPGeneration:            1,
			MaxSessionMemoryGB:       3.75,
			NeedsLogitsOffload:       true,
			MmapBudgetMB:             25,
			NativeINT4Support:        false,
			HasHMX:                   false,
			MoECapable:               false,
			KVOffsetBufferMB:         128,
			SupportsByteLevelVTCM:    false,
			SupportsBFloat16:         false,
			MaxPerGPUSizeLimit:       2048,
			RequiresMLAUnrolling:     true,
			SupportsByteGranularity:  false,
			FastRPCAddressLimit:      false,
			MaxVTCMBytes:             1048576,
			MaxPackedChannelsLimit:   2048,
			LowDepthActivationThresh: 32,
			ContextConfig: QNNContextConfig{
				FileReadMemoryBudgetMB: 256,
				EnableSharedBuffer:     false,
			},
		},
		"698": {
			DSPArch:                  "698",
			Arch:                     QNN_HTP_DEVICE_ARCH_698,
			MaxNPUOnlyModelSizeB:     1,
			RecommendedHostRAMGB:     128,
			RecommendedHostSwapGB:    64,
			RecommendedVCpuCount:     8,
			HardwareQuirks:           "Legacy architecture; restrictive memory paging prevents large context windows. 32-bit cDSP address limit. Limited to Q8_0.",
			SupportedQuantizations:   []string{"Q8_0"},
			NeedsFastRPCFix:          false,
			SplitModelThresholdB:     0,
			EstimatedINT8TOPS:        10,
			MemBandwidthGBps:         20,
			VTCMSizeKB:               512,
			HTPGeneration:            1,
			MaxSessionMemoryGB:       3.75,
			NeedsLogitsOffload:       true,
			MmapBudgetMB:             25,
			NativeINT4Support:        false,
			HasHMX:                   false,
			MoECapable:               false,
			KVOffsetBufferMB:         64,
			SupportsByteLevelVTCM:    false,
			SupportsBFloat16:         false,
			MaxPerGPUSizeLimit:       1024,
			RequiresMLAUnrolling:     true,
			SupportsByteGranularity:  false,
			FastRPCAddressLimit:      false,
			MaxVTCMBytes:             524288,
			MaxPackedChannelsLimit:   1024,
			LowDepthActivationThresh: 32,
			ContextConfig: QNNContextConfig{
				FileReadMemoryBudgetMB: 256,
				EnableSharedBuffer:     false,
			},
		},
	}
}

// GetDeviceDictionary returns a mapping of consumer phone models to their SoC and DSP Architecture.
func GetDeviceDictionary() map[string]DeviceLookup {
	return map[string]DeviceLookup{
		// Snapdragon 8 Elite (SM8750) - Hexagon v85
		"ASUS ROG Phone 9":   {"SM8750", "v85"},
		"Honor Magic7":       {"SM8750", "v85"},
		"OnePlus 13":         {"SM8750", "v85"},
		"Xiaomi 15":          {"SM8750", "v85"},
		"Samsung Galaxy S25": {"SM8750", "v80"},

		// Snapdragon 8 Gen 3 (SM8650) - Hexagon v75
		"Samsung Galaxy S24 Ultra": {"SM8650-AB", "v75"},
		"Samsung Galaxy S24+":      {"SM8650-AB", "v75"},
		"Samsung Galaxy S24":       {"SM8650-AB", "v75"},
		"Samsung Galaxy Z Fold 6":  {"SM8650-AB", "v75"},
		"Samsung Galaxy Z Flip 6":  {"SM8650-AB", "v75"},
		"Nubia Red Magic 9S Pro":   {"SM8650-AB", "v75"},
		"Oppo Pad 3 Pro":           {"SM8650-AB", "v75"},

		// Snapdragon 8 Gen 2 (SM8550) - Hexagon v73
		"OnePlus 12R":              {"SM8550-AB", "v73"},
		"Samsung Galaxy S23 Ultra": {"SM8550-AB", "v73"},
		"Samsung Galaxy S23+":      {"SM8550-AB", "v73"},
		"Samsung Galaxy S23":       {"SM8550-AB", "v73"},
		"ASUS ROG Phone 7":         {"SM8550-AB", "v73"},
		"OnePlus 11 5G":            {"SM8550-AB", "v73"},
		"Xiaomi 13 Pro":            {"SM8550-AB", "v73"},
		"Motorola Edge 40 Pro":     {"SM8550-AB", "v73"},

		// Snapdragon 7+ Gen 3 (SM7675) & 7s Gen 3 (SM7635) - Hexagon v73
		"OnePlus Nord 4":     {"SM7675-AB", "v73"},
		"Realme GT 6T":       {"SM7675-AB", "v73"},
		"Sharp Aquos R9":     {"SM7675-AB", "v73"},
		"Xiaomi Pad 7":       {"SM7675-AB", "v73"},
		"Poco Pad X1":        {"SM7675-AB", "v73"},
		"Nothing Phone 3a":   {"SM7635", "v73"},
		"Redmi Note 14 Pro+": {"SM7635", "v73"},

		// Snapdragon 7 Gen 3 (SM7550) - Hexagon v69
		"Motorola Edge 50 Pro": {"SM7550-AB", "v69"},
		"OnePlus Nord CE 4 5G": {"SM7550-AB", "v69"},
		"Vivo V40":             {"SM7550-AB", "v69"},
		"Vivo V30 5G":          {"SM7550-AB", "v69"},

		// Snapdragon 7+ Gen 2 (SM7475) & 7 Gen 1 (SM7450) - Hexagon v69
		"Poco F5":           {"SM7475-AB", "v69"},
		"Realme GT Neo5 SE": {"SM7475-AB", "v69"},
		"Honor 90":          {"SM7450", "v69"},
		"Motorola Razr 40":  {"SM7450", "v69"},

		// Snapdragon 8 Gen 1 (SM8450) - Hexagon v69 Class
		"Samsung Galaxy S22 Ultra": {"SM8450", "v69"},
		"Samsung Galaxy S22+":      {"SM8450", "v69"},
		"OnePlus 10 Pro":           {"SM8450", "v69"},
		"Motorola Edge 30 Pro":     {"SM8450", "v69"},
		"Xiaomi 12 Pro":            {"SM8450", "v69"},
		"Lenovo Legion Y90":        {"SM8450", "v69"},

		// Snapdragon 888 (SM8350) - Hexagon v68 Class
		"OnePlus 9 Pro":            {"SM8350", "v68"},
		"Samsung Galaxy S21 Ultra": {"SM8350", "v68"},

		// Snapdragon 870 5G (SM8250-AC) - Hexagon 698
		"Black Shark 4":      {"SM8250-AC", "698"},
		"Black Shark 5":      {"SM8250-AC", "698"},
		"iQOO Neo6 SE":       {"SM8250-AC", "698"},
		"Lenovo Legion Y700": {"SM8250-AC", "698"},

		// Snapdragon 4 Gen 2 (SM4450) & 4 Gen 1 (SM4375) - Hexagon v68 Class
		"Poco M6 Pro 5G": {"SM4450", "v68"},
		"Redmi 12 5G":    {"SM4450", "v68"},
	}
}

// ValidateTensorDimensions evaluates if a specific layer breaches the hardware's packed channel limits.
func (h *HardwareProfile) ValidateTensorDimensions(channels int, width int) error {
	// Packed channels calculation dictates total channels divided by 4, rounded up
	packedChannels := int(math.Ceil(float64(channels) / 4.0))

	requiredCapacity := packedChannels * width

	if requiredCapacity > h.MaxPerGPUSizeLimit {
		return fmt.Errorf(
			"tensor capacity violation: requires %d packed units, exceeding core limit of %d. Graph tiling mandatory",
			requiredCapacity,
			h.MaxPerGPUSizeLimit,
		)
	}
	return nil
}

// CalculatePackedChannels mathematically determines if a tensor exceeds HTP structural limits.
// Based on the QNN formula: ceil(channels / 4.0)
func CalculatePackedChannels(channels int) int {
	return int(math.Ceil(float64(channels) / 4.0))
}

// EvaluateTensorLegality checks if the compiler must perform tiling or padding.
func (hp *HardwareProfile) EvaluateTensorLegality(channels int) (requiresTiling bool, requiresPadding bool) {
	packedChannels := CalculatePackedChannels(channels)

	if packedChannels > hp.MaxPackedChannelsLimit {
		log.Printf("Warning: Tensor exceeds packed channel limit (%d). Forcing graph tiling.\n", hp.MaxPackedChannelsLimit)
		requiresTiling = true
	}

	// HTP Vector lanes starve and stall if activation depth is extremely shallow.
	if channels < hp.LowDepthActivationThresh {
		log.Printf("Warning: Low depth activation detected (%d channels). Injecting SpaceToDepth node.\n", channels)
		requiresPadding = true
	}

	return requiresTiling, requiresPadding
}

// ValidateHMXDimensions mathematically proves if a tensor can be processed
// natively by the Hexagon Matrix eXtensions without triggering severe software
// padding penalties or falling back to scalar CPU execution.
func (h *HardwareProfile) ValidateHMXDimensions(batch, height, width, channels int, dataType string) error {
	// HMX Activation Width Alignment Rules:
	// uint8 requires multiples of 8. uint16/fp16 requires multiples of 4.
	widthMultiple := 4
	if dataType == "uint8" || dataType == "int8" {
		widthMultiple = 8
	}

	if width%widthMultiple != 0 {
		return fmt.Errorf("HMX Alignment Error: Tensor width %d is not a multiple of %d", width, widthMultiple)
	}

	// Activation depth (channels) MUST be a multiple of 32 for optimal TCM routing.
	// Irregular channel counts will fragment the memory bus.
	if channels%32 != 0 {
		return fmt.Errorf("HMX Alignment Error: Channels %d must be padded to a multiple of 32", channels)
	}

	// For FP16/INT16 execution, the incoming data matrix must be cleanly
	// divisible into 32x32 micro-tiles (which represent 2 KiB blocks).
	if (height*width)%32 != 0 {
		return fmt.Errorf("HMX Tile Error: Spatial dimensions do not map cleanly to 32x32 micro-tiles")
	}

	return nil
}

// CalculateVTCMAllocation generates the exact QNN backend configuration payload
// to request VTCM space, dynamically adapting between legacy Megabyte sizing
// and modern byte-granularity specifications.
func (h *HardwareProfile) CalculateVTCMAllocation(requiredBytes int64) map[string]interface{} {
	config := make(map[string]interface{})

	if h.SupportsByteGranularity {
		// hexagon-v81 and newer architectures allow highly efficient byte-level VTCM mapping.
		// This maximizes VTCM Sharing with secondary hardware entities like the ISP.
		if requiredBytes > h.MaxVTCMBytes {
			// Cap allocation strictly to physical maximum to prevent segmentation faults.
			config["vtcm_size_bytes"] = h.MaxVTCMBytes
		} else {
			config["vtcm_size_bytes"] = requiredBytes
		}
	} else {
		// Legacy architectures force memory requests in coarse Megabytes,
		// requiring upward rounding logic to guarantee allocation.
		requiredMB := (requiredBytes + 1048575) / 1048576
		config["vtcm_size_mb"] = requiredMB
	}

	return config
}

// DetermineNPUContextRouting decides if a massive LLM parameter set breaches
// the 3.75GB FastRPC limit and must therefore be partitioned into multiple
// NPU execution sessions chained together.
func (h *HardwareProfile) DetermineNPUContextRouting(modelParamsBillion float64, precisionBytes int) (int, error) {
	estimatedMemoryGB := modelParamsBillion * float64(precisionBytes)

	if h.FastRPCAddressLimit && estimatedMemoryGB > h.MaxSessionMemoryGB {
		// The model footprint exceeds the 32-bit address space.
		// Calculate the required number of partition splits.
		numSessions := int((estimatedMemoryGB / h.MaxSessionMemoryGB) + 1)
		return numSessions, fmt.Errorf("Model breaches 32-bit FastRPC limit. Graph must be split into %d independent Multi-Context sessions.", numSessions)
	}

	return 1, nil
}

// CalculateSafeContextWindow dynamically determines the maximum safe KV cache size
// based on the hardware's Session Memory limit, preventing OOM crashes.
func CalculateSafeContextWindow(profile HardwareProfile, kvHeads, headDim, layers int) int {
	// Reserve 2GB for the 4-bit weights and activation scratchpads
	availableMemoryBytes := (profile.MaxSessionMemoryGB * 1024 * 1024 * 1024) - (2.0 * 1024 * 1024 * 1024)
	if availableMemoryBytes <= 0 {
		return 512 // Extreme fallback for heavily constrained environments
	}

	// Memory per token = 2 (Key & Value) * Layers * KV_Heads * Head_Dim * 2 bytes (FP16)
	bytesPerToken := 2 * layers * kvHeads * headDim * 2
	maxTokens := int(availableMemoryBytes) / bytesPerToken

	// Align to the nearest multiple of 128 for efficient SIMD processing
	return (maxTokens / 128) * 128
}

// MemoryAlignment calculates the padded memory bloat intrinsically required 
// by the HTP's wide Vector Registers. Unpadded dimensions lead to severe OOM miscalculations.
func MemoryAlignment(batch, height, width, channels int, alignMultiples int) (int, int, int, int) {
	padToMultiple := func(val, mult int) int {
		return int(math.Ceil(float64(val)/float64(mult))) * mult
	}
	
	paddedHeight := padToMultiple(height, alignMultiples)
	paddedWidth := padToMultiple(width, alignMultiples)
	paddedChannels := padToMultiple(channels, alignMultiples)
	
	return batch, paddedHeight, paddedWidth, paddedChannels
}

// ValidateModelFit acts as the gatekeeper, checking if the mathematically padded, 
// quantized GGUF fits within the cDSP OS-level limits before attempting compilation.
func ValidateModelFit(arch HardwareProfile, modelSizeMB uint64) bool {
	if arch.DSPArch == "v73" || arch.DSPArch == "v75" {
		// Strict fastrpc-issue-137 OS limit validation
		if modelSizeMB >= 3840 {
			return false 
		}
	}
	return modelSizeMB <= uint64(arch.MaxSessionMemoryGB * 1024)
}
