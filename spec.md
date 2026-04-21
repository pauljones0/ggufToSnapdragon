# HexForge - Architecture Specification

**Project Context**: HexForge is a cloud-native Pipeline-as-a-Service (PaaS) designed to automatically cross-compile Large Language Models (LLMs) from Hugging Face (`.gguf` format) into highly optimized Qualcomm AI Runtime (QAIRT) execution graphs for specific Snapdragon Hexagon NPUs. 

Because compiling large neural network graphs for specialized DSP hardware requires an extreme amount of host machine memory (often >128GB RAM), HexForge orchestrates an asynchronous queue system that dynamically scales a transient worker pool.

---

## 1. System Architecture High-Level Design

The system is decoupled into three primary execution zones:
1. **Frontend Client (The User Interface)**: Static Next.js web app hosted on GitHub Pages
2. **Control Plane (The Orchestrator)**: Terraform-managed Serverless Golang Cloud Functions, Cloud Tasks, and Firestore Database handling traffic, auth, queue state, and Google Cloud Batch API submissions. The Golang backend utilizes a **Domain-Driven/Hexagonal Architecture**, decoupling the core business logic (Use Cases) from Infrastructure adapters (Firestore, Pub/Sub, Cloud Tasks) using clean dependency-injected Domain Interfaces (`JobRepository`, `EventPublisher`). This architecture is fortified with comprehensive unit and integration test mocks to ensure business logic reliability and FinOps resilience.
3. **Data Plane (The Compiler)**: A transient, auto-scaling Google Cloud Batch execution environment utilizing Custom Machine Types, Local SSD swap, and a custom Packer image, running entirely within a Private VPC with Cloud NAT (no public IPs).

---

## 2. The Frontend Client (Next.js)
### 2.1 Aesthetic & UX & Clean Architecture
- **Theme**: Premium Dark Mode, "Sci-Fi/Semiconductor" aesthetic.
- **Components**: Glassmorphic panels with deep `backdrop-blur` layered borders, and emerald glowing neon effects (`box-shadow`), with seamless micro-animations on interactive elements to establish a highly premium SaaS feel.
- **Architecture**: The UI strictly adheres to clean React patterns. Complex integrations like Firebase Auth, Firestore `onSnapshot` real-time listeners, and Hardware dictionaries are abstracted into dedicated hooks (`useAuth`, `useJobTelemetry`, `useHardwareProfiles`) located in `src/hooks/`, keeping component files exclusively focused on rendering logic.
- **Core View**: A dashboard featuring a Phone Model lookup input, a Hugging Face GGUF URL input, and a live streaming "Job Progress" tracking panel incorporating queue psychology principles.

### 2.2 User Authentication
- **Provider**: Firebase Authentication.
- **Methods**: Google OAuth and GitHub OAuth.
- **Purpose**: We enforce mandatory login to prevent anonymous abuse of the expensive backend compute nodes.

### 2.3 Real-Time Streaming & Queue Psychology
- The client does not use HTTP polling. Instead, it natively streams real-time state using Firestore `onSnapshot` listeners.
- As the backend transitions the job (`Queued` → `Provisioning` → `Downloading` → `Compiling` → `Uploading` → `Completed`), the UI reacts in real-time, highlighting a Sci-Fi status progression.
- **Queue Psychology**: To drastically reduce page abandonment during congestion, the UI streams a "Waiting Room" experience while the job is `Queued`. It dynamically displays the exact Queue Position (via another `onSnapshot` listener tracking older queued jobs) alongside a mathematical Estimated Time of Arrival (ETA).

### 2.4 Client-Side Validation
- Before enabling the form submission, the React application instantly parses the provided Hugging Face URL for parameter size using regex.
- If the identified size exceeds the selected device's `Max_NPU_Only_Model_Size_Billions`, the UI disables the "Init Sequence" button.
- An actionable alert is presented, suggesting the user find a smaller parameterized model (e.g., 1.5B or 2B) rather than just stating an error.

---

## 3. The Control Plane (GCP Serverless)
### 3.1 Firestore Database Schema
- **Collection `System`** (Internal Architecture State):
  - `Document: ConcurrencyLock`
    - `active_spend_rate_cents_per_hr` (Number) - Singleton state used to explicitly enforce the global dynamic FinOps budget limit.

- **Collection `HardwareProfiles`** (Dynamic Handset Mapping):
  - `phone_model` (String, PK)
  - `chipset` (String)
  - `dsp_arch` (String)
  - `max_npu_billions` (Number)
  - `recommended_host_ram_gb` (Number)
  - `recommended_host_swap_gb` (Number)

- **Collection `Jobs`**:
  - `job_id` (String, PK)
  - `user_id` (String)
  - `hf_url` (String)
  - `parameter_size_billions` (Number)
  - `phone_model` (String)
  - `chipset` (String)
  - `dsp_arch` (String)
  - `status` (Enum: `Queued`, `Provisioning`, `Downloading`, `Compiling`, `Uploading`, `Completed`, `Failed`)
  - `error_message` (String, optional)
  - `trace_id` (String, optional) - End-to-end UUID for GCP Cloud Logging trace correlation
  - `created_at` (Timestamp)
  - `updated_at` (Timestamp)

### 3.2 Cloud Function 1: SubmitJob (API Gateway)
- **Runtime**: Golang.
- **Trigger**: HTTP POST request from the Next.js frontend.
- **Execution Flow**:
  1. Validates the Firebase Auth JWT token cryptographically to extract the trusted `UID`, actively blocking anonymous sybil-queue-flooding attacks. Reads the `X-Trace-ID` UUID from the HTTP headers for end-to-end telemetry.
  2. Parses the `hf_url`. Extracts the billion-parameter size using regex `(\d+)[bB]`.
  3. Looks up the requested `phone_model` in the internal `HardwareProfiles` dictionary.
  4. **Validation Check**: If the extracted model size exceeds the `Max_NPU_Only_Model_Size_Billions` for that specific DSP architecture, the request is rejected with a 400 response. The error message explicitly states the parameter limit and provides actionable advice (e.g., `"Error: Model size strictly exceeds the maximum NPU capacity of 3 Billion parameters for the requested phone model. Please try a smaller quant or a 1.5B parameter variant of this model."`).
  5. **File Size Check**: To prevent "filename gaming" (e.g., renaming a 70B model's URL to say 1B to bypass the regex check), the backend executes a lightweight `HEAD` request to the Hugging Face URL. It verifies the returned `Content-Length` file size in bytes against a heuristic maximum footprint. This threshold logic adds mathematical "wiggle room" (e.g., expected max size + 50%) to safely allow for higher quants (like Q8 vs Q4) while reliably blocking distinct architectural mismatches (e.g., an 80GB file footprint being passed for a 3B restricted device). This request utilizes a custom HTTP client with a strict 10-second timeout to prevent function starvation and subsequent queue backups if the external API hangs.
  6. **Deduplication / Cache Check**: Construct the intended output Hugging Face repository name (`pauljones0/{Original_Model_Name}-{SoC_Model}-QAIRT`). Query the Hugging Face API to see if this model repo already exists and is populated. If it does, safely bypass compilation and inject a `Completed` job record returning the URL to the frontend instantly.
  7. **Insert & Dispatch**: Creates a new document in the `Jobs` collection with `status: Queued`. Constructs a Google Cloud Task with an **idempotent deterministic name** derived from the `job_id` (format: `hexforge-{job_id}`), preventing duplicate task creation on application-level retries. The task payload holds the `job_id` and `trace_id` and is securely dispatched to the `QueueManager` queue for asynchronous processing via OIDC authentication. A `job.submitted` lifecycle event is published to Pub/Sub for downstream consumers. Backend strictly enforces processing of this job even if the frontend disconnects. Each handler invocation generates a unique **Span ID** for per-invocation log grouping, and every log entry includes a UUID `insertId` for log deduplication on retries.

### 3.3 Cloud Function 2: QueueManager (Batch Submitter)
- **Runtime**: Golang.
- **Trigger**: HTTP POST from Google Cloud Tasks Queue (invoked by `SubmitJob` or by internal retries).
- **Execution Flow**:
  1. Parses the payload for `job_id` and `trace_id`. Fetches the `Queued` job from Firestore.
  2. To prevent race-conditions where multiple parallel HTTP requests might bypass the global billing limit, the manager uses `firestoreClient.RunTransaction()`. Inside the transaction, it atomically queries the `System/ConcurrencyLock` document. It computes the cost of the Batch VM (16 vCPUs + dynamic RAM) against a predefined FinOps budget (e.g. max $1.00/hr). If `active_spend_rate_cents_per_hr + new_job_cost` exceeds the budget, the transaction aborts peacefully. The `QueueManager` immediately returns an **HTTP 429 Status (Too Many Requests)**. Cloud Tasks intercepts this and automatically applies exponential backoff, delaying the retry until budget might be freed. If under budget, it increments the spend lock and transitions the job to `Provisioning`.
  3. Uses the `batch.googleapis.com` API to submit a new Job Definition.
      - **Image**: `hexforge-ubuntu-2404-base`
      - **Instance Size**: Dynamically defined by the Job's `Recommended_Host_RAM_GB` dictionary value. vCPU count is fixed at 16 (sourced from the shared `finops.go` constants).
      - **Environment Variables**: Dynamically injects the `TRACE_ID` and `SPAN_ID` so the worker can format structured logs with trace and span correlation.
      - **Provisioning Model**: `SPOT` - Slashing compute costs by 60-80% compared to standard instances. Cloud Batch `LifecyclePolicies` are configured to automatically retry the job upon infrastructure preemption.
      - **Network**: Deploys into a custom VPC without external public IPs (using Cloud NAT for Hugging Face egress).
      - **Storage**: Attaches a `local-ssd` for high-throughput swap memory.
      - **Command**: Executes `batch_entrypoint.sh` directly.
      - If the batch API submission explicitly fails, the function catches the failure and explicitly decrements the `ConcurrencyLock` back down, returning a 500 so Cloud Tasks can retry.
      - On success, publishes a `job.provisioning` lifecycle event to Pub/Sub.

### 3.4 Cloud Function 3: Stale Job Reaper (Cron)
- **Runtime**: Golang.
- **Trigger**: Google Cloud Scheduler (e.g. Every 30 minutes).
- **Execution Flow**: Automatically queries the `Jobs` collection for documents stuck in any active phase (`Provisioning`, `Downloading`, `Compiling`, `Uploading`) for more than 4 hours. If found, it assumes the Cloud Batch worker VM suffered an unrecoverable crash (e.g., massive OOM locking the OS). It forcibly updates the job's status to `Failed`, explicitly decrements the `System/ConcurrencyLock`'s `active_spend_rate_cents_per_hr` counter by the job's estimated cost (calculated from its `target_ram`), and publishes a `job.reaped` lifecycle event to Pub/Sub, unlocking the queue so subsequent `QueueManager` triggers can process the next user.

  5. Emits a `CRITICAL` severity structured log with full trace correlation for operational alerting.

### 3.6 Cloud Function 6: EventProcessor (Event Consumer)
- **Runtime**: Golang.
- **Trigger**: HTTP POST from a Pub/Sub push subscription on the `hexforge-job-events` topic.
- **Execution Flow**:
  1. Parses the Pub/Sub push envelope and decodes the base64-encoded `JobEvent` payload.
  2. Extracts the `trace_id`, `job_id`, and `event_type`.
  3. **Audit Logging**: Emits an `INFO` level structured log for every processed event with full trace correlation and component tag `"EventProcessor"`.
  4. **Critical Alerting**: If the event type is `job.failed`, `job.dead_lettered`, or `job.reaped`, it emits a second log entry with elevated severity (`WARNING`, `CRITICAL`, or `ALERT`), triggering operational alerts for system failures or retry exhaustion.
  5. **Validation**: Verifies the event type against a centralized catalog (`events.go`) and logs a warning for unrecognized events.
  6. Acknowledges the message with a 200 OK, preventing redelivery.

### 3.7 Pub/Sub Event Bus
- **Topic**: `hexforge-job-events` (7-day message retention, message ordering by `job_id`).
- **Events**: `job.submitted`, `job.provisioning`, `job.downloading`, `job.compiling`, `job.uploading`, `job.completed`, `job.failed`, `job.dead_lettered`, `job.reaped`.
- **Publishers**: `SubmitJob`, `QueueManager`, `UpdateJobStatus`, `DeadLetterHandler`, `StaleJobReaper`, and **Worker Nodes**.
- **Design**: All event types are centralized in `back_end/handler/events.go` for schema enforcement. Publishing is fire-and-forget via a lazy-initialized `pubsub.Client` using `context.Background()` with a 5-second timeout. Failures are logged but never block the main pipeline.

### 3.7 Pub/Sub Subscription & Dead-Letter Topic
- **Primary Subscription**: `hexforge-job-events-sub` — pull-based subscription with message ordering, 30-second ack deadline, and 7-day message retention.
- **Dead-Letter Policy**: Messages that fail delivery after 5 attempts are forwarded to the `hexforge-job-events-dlq` topic.
- **DLQ Topic**: `hexforge-job-events-dlq` — 7-day retention. The Pub/Sub service account is granted `pubsub.publisher` on this topic and `pubsub.subscriber` on the primary subscription to enable dead-letter routing.
- **Retry Policy**: Exponential backoff from 10s to 600s between delivery attempts.

---

## 4. The Data Plane (Cloud Batch Workload)
### 4.1 Base Image Bakery (Packer)
- Instead of downloading the 2GB QAIRT SDK and configuring Python per job, we use HashiCorp Packer (`build_image.pkr.hcl`).
- This pre-bakes an Ubuntu 24.04 LTS image (`hexforge-ubuntu-2404-base`) containing `build-essential`, CMake, Python 3.10 virtual environments, the Hugging Face CLI, and the QAIRT framework pre-linked.

### 4.2 The Batch Entrypoint (`batch_entrypoint.sh`)
When Cloud Batch provisions the node, this script executes:
1. It retrieves its IAM sequence token from the metadata server (using `--fail` and `--retry 3` to counter transient boot networking issues).
2. It identifies the ephemeral NVMe Local SSD, formats it, and enables a dynamic Linux Swap file proportional to the `Recommended_Host_Swap_GB`.
2. It executes the Python compilation logic (`compile.py`).
3. **Resiliency**: The script exits cleanly (`0`) on success or returns a non-zero exit code on failure, allowing Cloud Batch's native retry mechanics or failure logging to govern the lifecycle instead of a custom Bash `trap`.
4. **Structured Logging**: All output uses a `log_json()` function emitting GCP-compliant JSON with `severity`, `message`, `component` (`"BatchEntrypoint"`), `timestamp` (ISO-8601 via `date`), `logging.googleapis.com/trace`, `logging.googleapis.com/spanId` (unique per phase: download, compile, upload), `logging.googleapis.com/insertId` (sequential counter for deduplication), and `logging.googleapis.com/labels` (containing `job_id`) fields. The `update_status()` helper propagates `trace_id` in every curl payload to the `UpdateJobStatus` Cloud Function. Phase-level timing is recorded between Download, Compile, and Upload phases for performance telemetry.

### 4.3 The Compilation Engine (`compile.py` / Sandbox Isolation)
1. **Ingestion**: The host bash wrapper securely executes `huggingface-cli download` to pull the GGUF to the host environment.
2. **QAIRT Targeting**: Inside an airgapped sandbox (`--network none`), Python initializes the Qualcomm `GenAIBuilderFactory` reading strictly from read-only mounts. It leverages a **QNN Compiler Cache** persisted to GCS for resilient retries.
3. **Graph Config & Verification**: Aggressively polls host RAM and swap levels to block linker OOMs. The system aggressively optimizes the QNN IR by injecting the `-O3` flag for L2 cache hit-rate maximization, extracting embedding Look-Up Tables (`--dump_lut`), and applying fallback `BFloat16` mapping to volatile `lm_head` projections. It ensures lossless translation of GGUF K-Quants by invoking the `Qnn_FloatBlockEncoding_t` struct natively within the compiler backend. Additionally, context weight sharing is enabled to support elastic context geometries natively on the Hexagon TCM without thrashing memory.
4. **HMX Alignment**: Forcefully injects zero-padding for attention heads not meeting the 64-element HMX alignment requirement (`--pad_head_dim_to 64`) to prevent **perplexity hemorrhaging** on modern NPU generations (v73+).
5. **GQA/MQA Support**: Automatically detects Grouped-Query Attention topologies from GGUF metadata and injects `architecture.num_kv_heads` into the composer config to ensure correct KV-cache broadcasting on Hexagon backends.
6. **SwiGLU Gating**: Explicitly detects SwiGLU architectures and configures `architecture.gating=gated` with `SiLU` activations to map non-linear gating logic correctly to HTP hardware.
7. **Normalization Parameters**: Supports both RMS-norm and standard layernorm via `operation.normalization` and `operation.normalization_epsilon` injection based on GGUF metadata.
8. **Per-Channel Quantization**: Enables `use_per_channel_quantization 1` to isolate outlier weights and preserve numerical stability in large models.
9. **Legacy INT4 Safety Fallback (W4A16 Execution)**: For architectures lacking native INT4 (v68, v69, 698), the compiler explicitly configures a W4A16 fallback (INT4 Weights + FP16 Activations) invoking on-the-fly register dequantization logic (`use_dequantize_node 1`), preventing destructive Q8_0 fallback bandwidth starvation.
10. **Hybrid Silicon Quantization**: For native INT4 silicon, the compiler prioritizes **IQ4_NL** for core transformer blocks while reserving **Q8_0** for hyper-sensitive layers (embeddings/final projections), maximizing baffle retention.
11. **Generalized Multi-Session Graph Chunking**: The compiler estimates the runtime memory footprint (weights + KV cache + activations) and applies `SplitModelConfig` for **any** architecture where the estimated footprint exceeds `MaxSessionMemoryGB` (3.75GB for all 32-bit cDSP architectures: v68, v69, v73, 698).
12. **Multi-Session Shared Memory**: When splitting models, the compiler enables `in-memory-kv-share` and **Q8_0_32 KV Quantization**, allowing the conversation history to persist across session boundaries in shared LPDDR space.
13. **Speculative Decoding (SSD-Q1, Eaglet, LADE)**: Automatically enables speculative branches to reclaim idle HMX compute rows during decoding phases, transforming GEMV operations into compute-saturated GEMMs.
14. **RoPE AoS Interleaving & LongRoPE**: Explicitly forces `repo_complex_organization=AoS` and `tensor.kq_complex_organization=AoS` to align with Hexagon Vector eXtension (HVX) register efficiency. Automatically injects LongRoPE configurations (`--longrope`, `--long_factor`, `--short_factor`) based on GGUF metadata for extended context windows.
15. **HVX 128-byte Super-Group Alignment & Padding**: Coalesces 8 GGUF quantization groups into 128-byte super-groups to perfectly saturate the HVX registers. Furthermore, the compiler injects a mandatory `--padding.baseline 128 --padding.strategy trailing_dimension` pass to ensure all non-HMX auxiliary tensors are aligned to 128 bytes, structurally preventing hardware memory-alignment exception crashes on the HVX unit.
16. **NHWC Layout Enforcement**: Forces `--force_nhwc 1` to eliminate expensive runtime Transpose ops.
17. **Logits Tensor Offloading**: For memory-constrained devices, the vocabulary projection is offloaded to CPU.
18. **mmap-Budget Enforcement & Shared Context**: Enforces strict `FileReadMemoryBudgetMB` limits mapped to memory chunking. Simultaneously leverages `QNN_HTP_MEM_SHARED_BUFFER` across partitioned models for zero-copy unified inference updates without power-intensive re-duplication.
19. **Standalone Deployment**: Exports a HuggingFace Fast Tokenizer JSON and packages LoRA adapters (if any) with `skip-lora-validation`.
20. **MoE Graph Switching**: Automatically enables `--enable-graph-switching 1` for Mixture of Experts architectures (e.g., Mixtral) on capable hardware (v75+) to optimize VLIW pipeline efficiency.
21. **KV Cache Spill-Fill Buffer**: Dynamic provisioning of `spill-fill-bufsize` (default 512MB) to seamlessly page KV cache chunks between VTCM and LPDDR memory, supporting ultra-long context windows.
22. **Custom Operator Fallback**: Implements an automatic OpPackage generation skeleton to handle mathematical nodes not natively supported by the QNN SDK (e.g. HardSwish, Mish, SwiGLU). The generator enforces **BHWC 4D layout** and `const` parameter constraints (via `setReadOnly(true)`) to satisfy HTP core tensor requirements and enable VLIW depth optimizations.
23. **SpaceToDepth Optimization**: Forcefully injected for reduced channel dimensions (head_dim <= 64) to maximize vector register saturation and prevent ALU idle cycles.
24. **v79/v80 Extended Precision**: Prioritizes INT2 and FP8 precision for Hexagon v79 and v80 architectures to maximize throughput.
25. **Crouton & VTCM Enforcement**: Generates a `tensor_properties.json` to explicitly force intermediate activations into Crouton layout and VTCM residency.
26. **Calibration Data Ingestion**: Supports `--calibration_data` for static quantization refinement via representative context samples.
27. **Macro-Architecture Connectors**: Parses `architecture.connector` from GGUF metadata to orchestrate precise sequential or parallel topology mapping onto the HTP.
28. **Embedding Dimension Derivation**: Implements mathematical derivation ($E_{head} = E_{total} / N_{heads}$) to explicitly calculate and map the attention head dimensionality when omitted from the source format, preventing tensor starvation.
29. **LiteRT Unified API Abstraction**: Intercepts unorthodox graphs and deploys them to the unified LiteRT API (`--execution_target litert_accelerator --enable_jit_fallback 1`), bringing JIT compilation resilience when the rigid QNN composer struggles with non-standard neural network topologies.
30. **Strict Capacity Guard**: Exits with a fatal error if model parameter count exceeds hardware limits.
31. **Worker Event Publishing**: Emits phase-level events and performance timing with full telemetry correlation (Trace/Span/Insert IDs).
32. **Profile Bucketing**: Dynamically generates multiple context binaries for sequence length buckets (128, 512, 1024, 2048, 4096) to enable rapid graph switching at runtime without re-compilation.
33. **Windows-on-ARM Optimization**: Binds execution threads to physical core counts (t 10/12) and enables `Q4_0_4_8` CPU fallbacks for Snapdragon X Elite/Plus platforms.
34. **Advanced Graph Fusion**: Enables monolithic kernel fusion for `Fused_SwiGLU` and `FMHA` to minimize DDR traffic.
35. **FastRPC Fragmentation Mitigation**: Implements `--fastrpc_quarantine 1` and address space fragmentation mitigations for 32-bit architectures (v73, v75) to prevent linker crashes near the 3.75GB wall.
36. **SpinQuant Optimizations**: Applies random orthogonal rotations (`--spin_quant 1`) to weight matrices to eliminate massive numerical outliers prior to block quantization.
37. **Sequential Mean Squared Error (SMSE)**: Automatically performs an intensive layer-by-layer offline search (`--optimize_sqnr 1 --quantization_schema smse`) to minimize the post-quantization delta against golden floating-point outputs.
38. **Mathematical FP32 Firebreaks**: Employs `--volatile_node_precision FP32` on highly volatile summation layers and vocabulary projection heads to act as a precision firebreak and prevent cascading NaN values from propagating through lower-precision integer streams.
39. **L2 Cache Elevation**: Bypasses slower DDR memory by injecting `--enable_l2_vtcm_lock 1` to perfectly lock massive attention activations onto the SoC's ultra-fast L2 Tightly Coupled Memory.
40. **Byte-Granularity Allocation**: Injects `--enable_byte_granularity_allocation 1` to replace legacy megabyte-tier memory paging with precise byte-specific instructions, maximizing NPU memory density.
41. **Global 8-Bit KV Cache Compression**: Injects `--kv_cache_bitwidth 8` to dynamically slash the Key/Value cache footprint by 50% without meaningful precision loss, delaying the 32-bit FastRPC address split.
42. **DeepSeek MLA Unrolling**: Automatically detects Multi-Head Latent Attention (MLA) and unrolls the mechanism into dense Multi-Head Attention (MHA) to maintain HTP acceleration paths.
43. **Mistral SWA Handling**: Detects Sliding Window Attention (SWA) and enforces rigid circular KV buffer sizing to match the sliding window, preventing runaway fragmentation.
44. **Advanced Olive-Style Graph Surgeries**: Injects a `QNNOptimizerEngine` to execute `MatmulAddFusion` (including `MaskedSoftmax` and `MatMul_bias` logic mapping), `SimplifiedLayerNormToL2Norm`, `ReplaceAttentionMask`, `RemoveRopeMultiCache` (saving millions of ALUs), `AttentionMaskToSequenceLengths` (eliminating zero-padded overhead), and `WeightRotation` (outlier suppression ensuring 4-bit fidelity).
45. **Hardware-Aware Tiling & Tensor Legality**: Implements strict `ceil(channels / 4.0)` Packed Channel boundaries and SpaceToDepth low-depth activation thresholds into graph mapping to bypass catastrophic vector limits. Uses `tuner.optimize` SDK API to evaluate optimal VTCM memory tiling against logic specifications.
46. **4D BHWC Layout Enforcement**: Mandates strict `pad_to_4d_bhwc` reshaping prior to AOT compilation for v79/v81 compliance.
47. **Strict Zero-Point Padding Constraints**: Validates asymmetric offset folds to prevent invalid HTP scalar conversions.
48. **Transformer Composer Alignment**: Generates structural JSON enforcing 32-byte physical alignments, RoPE AoS memory organizations, and speculative orchestration roles.
49. **Dynamic Context Window Sizing**: Uses `CalculateSafeContextWindow` heuristics to determine safe KV cache capacities mapped securely against the 3.75GB session limits rather than blind fixed sequences lengths.

### 4.4 Architectural Constraints & Compilation Heuristics

The compilation pipeline is governed by a rigid hierarchy of hardware constraints across Hexagon generations.

#### 4.4.1 Hexagon Hardware Matrix

| Gen | Class | Max Params | Session Mem | Bandwidth | VTCM | Peak INT8 | Native INT4 |
|---|---|---|---|---|---|---|---|
| v85 | Snapdragon 8 Elite | 18B | 8.0 GB | 84 GBps | 8192 KB | 82 TOPS | Yes | Q2_K to FP8, INT2, IQ4_NL, Bfloat16 (Elite Affinity) |
| v81 | Snapdragon 8 Elite | 14B | 8.0 GB | 68 GBps | 8192 KB | 45 TOPS | Yes | Q2_K to FP8, IQ4_NL, Bfloat16 (High Affinity) |
| v80 | Snapdragon X Elite | 14B | 8.0 GB | 68 GBps | 8192 KB | 45 TOPS | Yes | Q2_K to FP8, IQ4_NL (High Affinity) |
| v79 | Snapdragon 8 Elite | 14B | 8.0 GB | 68 GBps | 8192 KB | 45 TOPS | Yes | Q2_K to FP8, INT2, IQ4_NL (High Affinity) |
| v75 | Snapdragon 8 Gen 3 | 10B | 3.75 GB | 45 GBps | 4096 KB | 34 TOPS | Yes | Q2_K to IQ4_NL |
| v73 | Snapdragon 8 Gen 2 | 7B | 3.75 GB | 40 GBps | 2048 KB | 26 TOPS | Yes | Q2_K to IQ4_NL |
| v69 | Snapdragon 8 Gen 1 | 3B | 3.75 GB | 34 GBps | 2048 KB | 13 TOPS | No | Q8_0 |
| v68 | Snapdragon 888 | 3B | 3.75 GB | 25 GBps | 1024 KB | 11 TOPS | No | Q8_0 |
| 698 | Snapdragon 870 5G | 1B | 3.75 GB | 20 GBps | 512 KB | 10 TOPS | No | Q8_0 |

#### 4.4.2 The FastRPC Bottleneck
For Hexagon versions v75 and below, the compute DSP (cDSP) operates on a 32-bit foundation, limiting the virtual address space to exactly **3.75 GB** per session. Models exceeding this threshold must be partitioned into sequential QNN sessions via `SplitModelConfig`.

#### 4.4.3 Predictive Memory Heuristic ($M_{est}$)
The compiler calculates the estimated session memory using a refined heuristic that accounts for weight precision, KV cache geometry at the model's native context length, dynamic cache compression bitwidth, and vocabulary projection spikes:
$$M_{est} = (P_{total} \times 0.5 \text{ bytes}) + (KV_{size}^{FP16 \text{ or } INT8} \times 1.3) + Logits_{spike}$$
where $Logits_{spike} = (vocab\_size \times head\_dim \times 2)$. The 1.3x multiplier is an empirical constant covering activation buffers and FastRPC state tracking overheads. Instructing the compiler to utilize 8-bit cache compression perfectly halves the $KV_{size}$ component of the footprint.

#### 4.4.4 HMX & HVX Alignment
- **HMX**: Mandates `head_dim` multiples of 64. Non-compliant models trigger `--pad_head_dim_to 64` to prevent precision degradation (Perplexity Hemorrhaging).
- **HVX Alignment & Padding**: Coalesces quantization groups into 128-byte super-groups (`REPACK_FOR_HVX=1`) to saturate the vector pipeline. The compiler also executes an explicit `--padding.strategy trailing_dimension` pass at a baseline multiple of `128` to buffer any auxiliary tensors that naturally lack HVX memory alignment, preventing architectural execution crashes.

---

## 5. Security Posture
- **Cloud Function API**: Protected by Firebase JWT validation. The API mitigates DDoS abuse via a strict global queue depth hardcap (50 jobs limit) and enforces payload URL sanitization to prevent shell-escapes. The internal `UpdateJobStatus` webhook is further restricted via IAM (disallowing unauthenticated invocations), ensuring only legitimate internal worker Service Accounts can transition job states. 
- **Worker Node Authority**: Runs under a custom Service Account (`hexforge-worker-sa`). It has zero-trust IAM boundaries:
  - *No* database access. Job status mutations occur purely via the `UpdateJobStatus` webhook API.
  - `roles/secretmanager.secretAccessor` (strictly scoped only to the `HF_TOKEN` secret to publish final artifacts).
  - *No* compute or instance deletion privileges. Cloud Batch manages lifecycle via its own managed identity.
- **Payload Sandboxing**: Due to the hazardous nature of loading generic model weights (e.g. malicious pickle files), the GGUF payload is subjected to a pre-compilation `clamscan`. If safe, the Python compilation executes dynamically encapsulated within a `runsc` (gVisor) Docker container with strictly `--network none` routing to physically isolate the VM kernel and completely eliminate data exfiltration paths.
- The worker machines have no external public IPs. Outbound access to Hugging Face is routed securely through a Cloud NAT attached to a Private VPC.
- The user's HF token is never used; the platform publishes to a central `pauljones0` repository using an admin token.

## 6. Testing Architecture

### 6.1 Philosophy
The HexForge backend follows a strict **domain-driven test pyramid**. All business logic lives in `back_end/internal/usecase/` and `back_end/internal/domain/` and is tested in complete isolation from any GCP SDK. External dependencies are replaced by in-memory mocks that implement the domain port interfaces.

### 6.2 Test Layers

| Layer | Package | File | Coverage |
|---|---|---|---|
| Domain FinOps | `internal/domain` | `finops_test.go` | `EstimateJobCost` unit tests |
| Use Case | `internal/usecase` | `submit_job_uc_test.go` | Happy path, cache hit, enqueue failure, queue full, user quota, device validation, shell injection, blacklist (14 sub-tests) |
| Hardware Models | `models` | `hardware_test.go` | Profile completeness, size monotonicity, device→arch mapping, cross-reference integrity (5 test functions) |

### 6.3 Mock Infrastructure (`internal/infrastructure/`)
- **`MockJobRepository`**: In-memory implementation of `domain.JobRepository`. Supports seeding pre-existing jobs for scenario testing.
- **`MockEventPublisher`**: Captures all `PublishJobEvent` calls in a thread-safe slice; tests assert exact event counts.
- **`MockCloudTasksClient`**: Records enqueued jobs. Supports a `ReturnError` field to simulate Cloud Tasks unavailability, verifying that enqueue failure never surfaces to the caller and never publishes an event.

### 6.4 Known Security Bug Fixed
A critical URL sanitization defect was identified and remediated in `internal/usecase/submit_job_uc.go`. The `strings.ContainsAny` character guard used the Go string literal `"\\n\\r\\t"`, which resolved to the letters `n`, `r`, `t` rather than the intended control characters newline, carriage-return, and tab. This caused every valid URL (including `https://...`) to be incorrectly rejected. The fix uses proper single-backslash escape sequences (`\n`, `\r`, `\t`).

### 6.5 CI/CD Pipeline (`.github/workflows/ci.yml`)
A GitHub Actions workflow runs on every `push` or `pull_request` targeting the `main` branch when `back_end/**` files are modified. Steps:
1. `go vet ./...` — static analysis
2. `go build ./...` — compilation check
3. `go test ./... -count=1 -race -timeout 60s` — full test suite with Go race detector

> EOF
