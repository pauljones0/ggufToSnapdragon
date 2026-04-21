# HexForge Matrix

HexForge is an enterprise-grade MLOps automation pipeline. It allows users to cross-compile large Hugging Face `GGUF` model weights into serialized Qualcomm Hexagon execution graphs for specific Snapdragon mobile architectures.

Due to the extreme memory demands of Neural Network graph compilation (often requiring over 128GB of host RAM), HexForge utilizes a dynamic, transient worker pool on Google Cloud Platform to maintain cost efficiency.

## 🧱 Architecture Overview

HexForge consists of three distinct execution planes:

1. **Frontend (Next.js)**: A premium Sci-Fi SaaS dark-mode Dashboard featuring glassmorphism aesthetics, allowing authenticated users to select their target Snapdragon architecture and supply a Hugging Face URL with real-time streaming queue telemetry.
2. **Orchestrator (Golang, Cloud Tasks, & Firestore)**: Serverless functions (managed natively by Terraform) that validate the incoming model size against specific hardware constraints. It provisions execution jobs via the **Google Cloud Batch** API using atomic Firestore transactions mapping a FinOps budget cap. Job orchestration is managed by **Google Cloud Tasks** with **idempotent task names** (preventing duplicate dispatch on retry) and OIDC-authenticated dispatch for guaranteed exactly-once execution and exponential backoff. A dedicated **Dead-Letter Queue** (`hexforge-dlq`) receives tasks that exhaust all retries, routing them to a `DeadLetterHandler` Cloud Function that gracefully marks jobs as Failed and releases the FinOps spend lock. A matured **Pub/Sub Event Bus** (`hexforge-job-events`) publishes lifecycle events (`job.submitted`, `job.provisioning`, `job.downloading`, `job.compiling`, `job.uploading`, `job.completed`, `job.failed`, `job.dead_lettered`, `job.reaped`) with message ordering. Events are published by Go Cloud Functions and directly from **Worker nodes**. An **EventProcessor** Cloud Function consumes these events via a push subscription for unified audit logging and elevated alerting on critical failures. **End-to-End UUID Trace IDs** with per-handler **Span IDs** and component-tagged structured JSON logs provide unified correlation across GCP Cloud Logging.
3. **Compute Plane (Ubuntu & Python)**: A transient Cloud Batch workload utilizing **Spot VMs** to drastically slash costs. It runs securely within a Private VPC with Cloud NAT (no public IPs), uses a **GCS Checkpoint Bucket** to gracefully resume interrupted work from preemptions (including **GenAIBuilder Compilation Caching**), runs extremely fast NVMe Local SSDs for swap partitions, executes the compilation with **advanced silicon-level optimizations**:
    - **Advanced Graph Slicing**: Automated multi-session chunking for 32-bit cDSP architectures.
    - **Hybrid Silicon Quantization**: Prioritizes **IQ4_NL** for transformer blocks and **Q8_0** for sensitive projections.
    - **Speculative Decoding (SSD-Q1)**: Reclaims HMX hardware rows by guessing future tokens in parallel.
    - **MoE Graph Switching**: Optimizes Mixture of Experts architectures on v75+ NPUs.
    - **KV Cache Spill-Fill**: Seamless memory paging between VTCM and LPDDR for ultra-long context windows.
    - **RoPE AoS Interleaving**: Peak vector MAC efficiency via HVX-aligned complex organization.
    - **HVX 128-byte alignment** & **NHWC Layout Enforcement**.
    - **Crouton & VTCM Enforcement**: Explicitly forces blocked Z-order spatial tiles and SRAM residency for intermediate activations.
    - **L2 Cache Elevation**: Dynamically coercing Level 2 Cache for VTCM locking of attention activations.
    - **Byte-Granularity Allocation**: Precise memory mapping to eliminate NPU fragmentation.
    - **8-Bit KV Cache Compression**: Slices generation memory footprint by 50% to prevent rigid FastRPC splits.
    - **Static Quantization Calibration**: Supports representative dataset ingestion for optimized integer scaling.
    - **HMX Matrix Transposition**: Aligns weight matrices for peak HMX silicon ingestion.
    - **Standalone Deployment**: Exports Fast Tokenizer JSON and integrated LoRA adapters.
    - **Profile Bucketing**: Generates multi-binary context deployments for dynamic sequence lengths (128-4096).
    - **Windows-on-ARM Optimizations**: Thread-to-core binding and Q4_0_4_8 CPU fallback kernels.
    - **FastRPC Fragmentation Mitigation**: Safety pass for 32-bit address space stability on v73/v75.
    - **DeepSeek MLA Unrolling**: Automatic detection and unrolling of Multi-Head Latent Attention to maintain HTP acceleration.
    - **Olive-Style Graph Surgeries**: Injects `QNNOptimizerEngine` for `MatmulAddFusion` (now explicitly mapping `Add -> Softmax` to `MaskedSoftmax` and `MatMul -> Add` to `MatMul_bias`), `SimplifiedLayerNormToL2Norm`, `ReplaceAttentionMask`, `RemoveRopeMultiCache`, `AttentionMaskToSequenceLengths`, and `WeightRotation`.
    - **v85 Architecture Optimization**: Full support for Snapdragon 8 Elite (v85) with GenAI encryption, 18MB HPM cache, and INT2 support.
    - **Targeted SoC Tiling & Native Precision**: The graph orchestrator now explicitly passes the specific SoC architecture dynamically (e.g. `QNN_SOC_MODEL_SM8750`) to enable accurate L2 cache layouts and forces Float16 K-Quant execution on HMX units.
    - **SIMD Vector Pad Alignments**: Mathematical `MemoryAlignment` pass dynamically inflates trailing tensor dimensions to perfect integer multiples (often 128) intrinsically required by the HTP SIMD registers to block OS OOM crashes.
    - **W4A16 Hardware-Aware Execution**: Bypasses destructive Q8_0 fallback on legacy hardware by relying on on-the-fly FP16 dequantization kernels.
    - **Tightly Coupled Memory (TCM)**: Enables zero-copy inference via `QNN_HTP_MEM_SHARED_BUFFER` and enforced context config read memory budgets.
    - **Hardware-Aware Tiling**: Integrates `tuner.optimize` API and enforces `ceil(channels / 4.0)` spatial packed channel limits and LowDepth buffers.
    - **4D BHWC Layout Enforcement**: Mandates strict `pad_to_4d_bhwc` reshaping prior to AOT compilation for v79/v81 mathematical compliance.
    - **Strict Zero-Point Padding Constraints**: Checks for asymmetric offset folds to prevent invalid HTP scalar conversions.
    - **Transformer Composer Alignment**: Auto-generates structural JSON enforcing 32-byte physical alignments, RoPE AoS memory organizations, and speculative orchestration roles.
    - **Dynamic Context Window Sizing**: Leverages `CalculateSafeContextWindow` heuristics to determine safe KV cache capacities mapped securely against the 3.75GB session limits.

---

## 🚀 Setup & Deployment Guide

### 1. Prerequisites
- A Google Cloud Platform (GCP) Project with billing enabled.
- A Hugging Face account with a designated write token.
- Terraform installed locally.
- Packer installed locally.

### 2. The Hugging Face Secret
The worker node needs secure access to your Hugging Face account to upload the final artifacts (resulting in a repository formatted as `pauljones0/{Original_Model_Name}-{SoC_Model}-QAIRT`).

1. Navigate to **Google Cloud Console > Security > Secret Manager**.
2. Create a new secret named exactly `hf-token`.
3. Paste your Hugging Face Write Token as the value.
4. *The Terraform scripts will automatically grant the worker VM read-access to this specific secret.*

### 3. Baking the Custom Image (Packer)
To achieve sub-60-second boot times, the worker nodes use a pre-compiled environment instead of downloading gigabytes of dependencies every run.

1. Ensure the QAIRT SDK `.zip` is hosted in a private Google Cloud Storage bucket you own.
2. Navigate to the `infra/` directory.
3. Run the Packer build:
   ```bash
   packer build -var 'project_id=YOUR_PROJECT' -var 'qairt_bucket_uri=gs://YOUR_BUCKET/qairt_sdk.zip' build_image.pkr.hcl
   ```
4. This will create a machine image named `hexforge-ubuntu-2404-base` in your GCP project.

### 4. Deploying Infrastructure (Terraform)
HexForge uses a Google Cloud Storage (GCS) backend for Terraform remote state.
1. Create a GCS bucket to hold your terraform state (e.g., `gs://my-hexforge-tf-state`).
2. Update the `backend "gcs"` block in `infra/main.tf` with your bucket name.
3. In the `infra/` directory, initialize Terraform:
   ```bash
   terraform init
   ```
4.  Apply the configuration (this provisions the VPC Network, Firestore Database, Cloud Functions, Service Accounts, and IAM roles):
    ```bash
    terraform apply
    ```
5.  **Configure Dead-Letter Queue Routing** (one-time manual step — the Terraform Cloud Tasks provider does not yet support `dead_letter_config` inline):
    ```bash
    gcloud tasks queues update hexforge-job-queue \
      --dead-letter-queue=projects/YOUR_PROJECT_ID/locations/us-central1/queues/hexforge-dlq \
      --max-task-count=1 \
      --location=us-central1
    ```
    This routes tasks that exhaust all 100 retries on the primary queue to the `hexforge-dlq` queue, which dispatches them to the `DeadLetterHandler` Cloud Function.
6.  **Verify Pub/Sub Subscription**: After `terraform apply`, verify the pull subscription is active:
    ```bash
    gcloud pubsub subscriptions describe hexforge-job-events-sub
    ```
7.  **SRE Alerting**: Check the Cloud Monitoring dashboard to ensure the `hexforge/reaped_jobs_count` metric and its associated `HexForge - Ghost Job Reaped Alert` logic are enabled.

### 5. Deploying the Backend
HexForge uses Golang Cloud Functions natively. The Cloud Functions (`SubmitJob`, `QueueManager`, `UpdateJobStatus`, `StaleJobReaper`, and `DeadLetterHandler`) and the Cloud Scheduler triggers are now fully managed and deployed automatically by the Terraform configuration mentioned in Step 4. You no longer need to run manual `gcloud functions deploy` scripts.

### 6. Frontend Deployment
The `front_end/` directory contains standard Next.js 14 source code. It utilizes `framer-motion` for complex Sci-Fi state transitions, so ensure legacy peer dependencies are allowed if needed.
1. Install dependencies: `npm install --legacy-peer-deps`
2. Run development server: `npm run dev`
3. Deploy to your static host (e.g. Vercel, GitHub Pages) via `npm run build`.

---

## 🔒 Security Architecture

HexForge enforces a strict defense-in-depth model to protect cloud resources from malicious Hugging Face payloads and abuse:
- **API Hardening**: The Go backend validates Firebase JWT tokens directly to prevent queue-flooding/sybil attacks, sanitizes Hugging Face URLs to prevent shell-injection, and enforces a **malicious repository blacklist** to block known abusive sources. A global max queue depth (50) is also enforced.
- **Least Privilege IAM**: The worker service account operates with absolutely minimal permissions (Pub/Sub publishing and secret access), explicitly audited to ensure no horizontal movement.
- **Pre-Compilation Scanning**: Before any compilation begins, the worker performs a recursive `clamscan` on the downloaded GGUF payload natively on the host.
- **Airgapped gVisor Sandboxing**: The Qualcomm Python compilation executes inside a Docker container using the `runsc` (gVisor) runtime with all network access disabled (`--network none`). The sandbox is further hardened with `--security-opt=no-new-privileges` and isolated `tmpfs` mounts to prevent persistence. Hugging Face downloads and uploads occur securely on the host VM, preventing any malicious payload from exfiltrating secrets.

---

## ✋ Current Limitations
- **Snapdragon X Elite Laptops**: Version 1 of HexForge strictly supports mobile Snapdragon SoCs (Gen 1 through Elite). It drops support for laptop SKUs because the X Elite NPU driver stack and RAM allocation differ drastically from the mobile Hexagon DSP memory blocks. Laptop compilation is slated for v2.
- **FinOps Active Budget Cap**: The system enforces a dynamic expenditure limit (e.g. $1.00/hr active spend) to safely govern the elastic pool of Spot VMs, ensuring runaway queue requests don't cause sudden billing shock.
- **Hardware Profile Quantization Limits**: Architecture older than v73 generally requires `Q8_0` fallbacks natively, while elite tier v79 and v80 platforms exclusively introduce dynamic `"INT2"` and `"FP8"` quantizations. v73+ architectures additionally support `IQ4_NL` (Importance-Matrix Non-Linear 4-bit) for optimal hybrid quantization (IQ4_NL weights + Q8_0 activations).
- **Strict Capacity Validation**: The system now strictly enforces billion-parameter limits at the compiler level (e.g., 14B for v79/v80, 10B for v75, 7B for v73) to prevent linker and address space exhaustion.
- **32-bit cDSP Address Space**: All architectures prior to v79 operate on a 32-bit cDSP, limiting each QNN session to ~3.75GB. The compiler automatically chunks models via `SplitModelConfig` when the estimated memory footprint exceeds this limit. The `mmap-budget` is enforced at 25MB for incremental paging.
- **Crouton Memory Layout**: The HTP's proprietary `1×8×8×32` chunked memory layout (with pad-value 31) is enforced at the SDK level. Custom operators must use `get_raw()` for chunk-aware memory access.
