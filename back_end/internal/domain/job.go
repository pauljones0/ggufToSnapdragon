package domain

import "time"

// JobRequest represents the incoming JSON payload from the Next.js frontend.
type JobRequest struct {
	UserID     string `json:"user_id"`
	PhoneModel string `json:"phone_model"`
	HFUrl      string `json:"hf_url"`
}

// JobRecord represents the document schema stored in Firestore.
type JobRecord struct {
	JobID                 string    `firestore:"job_id"`
	UserID                string    `firestore:"user_id"`
	HFUrl                 string    `firestore:"hf_url"`
	ParameterSizeBillions int       `firestore:"parameter_size_billions"`
	PhoneModel            string    `firestore:"phone_model"`
	Chipset               string    `firestore:"chipset"`
	DSPArch               string    `firestore:"dsp_arch"`
	TargetRAM             int       `firestore:"target_ram"`
	TargetSwap            int       `firestore:"target_swap"`
	TargetVCpu            int       `firestore:"target_vcpu"`
	NativeINT4Support     bool      `firestore:"native_int4_support"`
	HasHMX                bool      `firestore:"has_hmx"`
	NeedsLogitsOffload    bool      `firestore:"needs_logits_offload"`
	MaxSessionMemoryGB    float64   `firestore:"max_session_memory_gb"`
	VTCMSizeKB            int       `firestore:"vtcm_size_kb"`
	HTPGeneration         int       `firestore:"htp_generation"`
	MmapBudgetMB          int       `firestore:"mmap_budget_mb"`
	MoECapable            bool      `firestore:"moe_capable"`
	KVOffsetBufferMB      int       `firestore:"kv_offset_buffer_mb"`
	EstimatedINT8TOPS     int       `firestore:"estimated_int8_tops"`
	MemBandwidthGBps      int       `firestore:"mem_bandwidth_gbps"`
	Status                string    `firestore:"status"`
	ErrorMessage          string    `firestore:"error_message,omitempty"`
	TraceID               string    `firestore:"trace_id,omitempty"`
	CreatedAt             time.Time `firestore:"created_at"`
	UpdatedAt             time.Time `firestore:"updated_at"`
}
