#!/bin/bash
# HexForge Cloud Batch Entrypoint
# This script executes a single compilation job natively within Google Cloud Batch.
# It provisions ultra-fast Local SSD Swap memory and exits cleanly to let Batch handle retries.

set -eo pipefail

# Generate a unique span ID for phase-level tracing (16-char hex string).
# Each major phase (download, compile, upload) generates its own span ID
# to enable per-phase log grouping in GCP Cloud Trace.
generate_span_id() {
    cat /proc/sys/kernel/random/uuid | tr -d '-' | head -c 16
}

CURRENT_SPAN_ID="${SPAN_ID:-$(generate_span_id)}"
INSERT_COUNTER=0

# Structured logging function emitting GCP Cloud Logging JSON to stdout.
# Includes trace correlation, component tagging, timestamps, labels, span IDs,
# and insertId for log deduplication on retried invocations.
log_json() {
    local severity=$1
    local message=$2
    local component="${3:-BatchEntrypoint}"
    local span_id="${4:-$CURRENT_SPAN_ID}"
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%S.%NZ")
    INSERT_COUNTER=$((INSERT_COUNTER + 1))
    local insert_id="${JOB_ID}-batch-${INSERT_COUNTER}"
    message="${message//\"/\\\"}"
    printf '{"severity":"%s","message":"%s","component":"%s","timestamp":"%s","logging.googleapis.com/trace":"projects/%s/traces/%s","logging.googleapis.com/spanId":"%s","logging.googleapis.com/labels":{"job_id":"%s"},"logging.googleapis.com/insertId":"%s"}\n' "$severity" "$message" "$component" "$timestamp" "$GCP_PROJECT_ID" "$TRACE_ID" "$span_id" "$JOB_ID" "$insert_id"
}

log_json "INFO" "[HEXFORGE BATCH] Initializing Batch Entrypoint for Job: $JOB_ID"

# 1. Gather Context & Credentials
# We rely on Cloud Batch to inject specific environment variables:
# $JOB_ID, $HF_URL, $DSP_ARCH, $CHIPSET, $TARGET_SWAP, $API_BASE_URL, $HF_TOKEN (via Secret env mapping)

# Fetch IAM Token for the worker SA natively via gcloud helper to invoke IAM locked Cloud Functions
export SA_TOKEN=$(curl -sS --fail --retry 3 --retry-delay 2 "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity?audience=$API_BASE_URL" -H "Metadata-Flavor: Google")
if [ -z "$SA_TOKEN" ]; then
    log_json "ERROR" "FATAL: Failed to retrieve IAM SA Token from metadata server after retries."
    exit 1
fi

# Helper function to update job status via the serverless tracking API.
# Propagates trace_id for end-to-end correlation in Cloud Logging.
update_status() {
    local status=$1
    local error_msg=$2
    local payload="{\"job_id\": \"$JOB_ID\", \"status\": \"$status\", \"trace_id\": \"$TRACE_ID\""
    if [ -n "$error_msg" ]; then
        payload="$payload, \"error_message\": \"$error_msg\""
    fi
    payload="$payload}"
    curl -s -X POST \
        -H "Authorization: Bearer $SA_TOKEN" \
        -H "Content-Type: application/json" \
        -d "$payload" \
        "$API_BASE_URL/updateJobStatus" > /dev/null
}

# Helper function to publish lifecycle events directly to Pub/Sub.
# This ensures that phase-level events are available to the event bus
# even if the update_status webhook has latency or polling delays.
publish_event() {
    local event_type=$1
    # Skip if Pub/Sub topic is not configured
    if [ -z "$PUBSUB_TOPIC" ]; then return 0; fi

    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%S.%NZ")
    local payload="{\"event_type\": \"$event_type\", \"job_id\": \"$JOB_ID\", \"trace_id\": \"$TRACE_ID\", \"span_id\": \"$CURRENT_SPAN_ID\", \"timestamp\": \"$timestamp\"}"
    
    # Use gcloud to publish the event. The worker SA has roles/pubsub.publisher.
    gcloud pubsub topics publish "$PUBSUB_TOPIC" --message="$payload" > /dev/null 2>&1 || true
}

# --- Phase: Download ---
DOWNLOAD_SPAN_ID=$(generate_span_id)
update_status "Downloading" ""
publish_event "job.downloading"
PHASE_START=$SECONDS

# 2. Local SSD Swap Provisioning
# Cloud Batch dynamically mounts local-ssds per our job definition.
# We locate the NVMe drive and format it for high IOPS swap space.
SSD_DEVICE=$(nvme list | grep "Google Local System Disk" | awk '{print $1}' | head -n 1)

if [ -n "$SSD_DEVICE" ]; then
    log_json "INFO" "Found NVMe Local SSD at $SSD_DEVICE. Provisioning high-speed swap..." "BatchEntrypoint" "$DOWNLOAD_SPAN_ID"
    # Warning: mkswap destroys data, but local SSDs are ephemeral scratch disks.
    mkswap $SSD_DEVICE
    swapon $SSD_DEVICE
else
    log_json "WARNING" "No Local SSD found. Falling back to persistent disk swapfile." "BatchEntrypoint" "$DOWNLOAD_SPAN_ID"
    SWAP_FILE="/swapfile_${JOB_ID}"
    # Use dd for robust creation to avoid fragmentation that fallocate might cause on certain FS
    # Optimized bs=1M to prevent host memory exhaustion during block zeroing
    dd if=/dev/zero of=$SWAP_FILE bs=1M count=$((TARGET_SWAP * 1024)) status=progress
    chmod 600 $SWAP_FILE
    mkswap $SWAP_FILE
    swapon $SWAP_FILE
fi

# 3. Execution Phase
source /etc/environment
source /opt/hexforge/venv/bin/activate

ORIGINAL_MODEL_NAME=$(basename "$HF_URL" .gguf)
OUTPUT_REPO="pauljones0/${ORIGINAL_MODEL_NAME}-${CHIPSET}-QAIRT"

WORKSPACE="/opt/hexforge/jobs/$JOB_ID"
mkdir -p "$WORKSPACE"
CHECKPOINT_PREFIX="gs://${CHECKPOINT_BUCKET}/${JOB_ID}"

# Pre-flight Check: Verify GCS Bucket connectivity and authorized write access
log_json "INFO" "Executing pre-flight GCS checkpointing check..." "BatchEntrypoint" "$DOWNLOAD_SPAN_ID"
if ! gsutil cp /dev/null "${CHECKPOINT_PREFIX}/.preflight_canary" >/dev/null 2>&1; then
    log_json "ERROR" "FATAL: Pre-flight check failed. GCS bucket $CHECKPOINT_BUCKET is unreachable or write-denied. Aborting to prevent wasted computation." "BatchEntrypoint" "$DOWNLOAD_SPAN_ID"
    update_status "Failed" "GCS Checkpoint Bucket unreachable."
    exit 1
fi
gsutil rm "${CHECKPOINT_PREFIX}/.preflight_canary" >/dev/null 2>&1 || true

log_json "INFO" "[$JOB_ID] Step 1: Downloading GGUF securely..." "BatchEntrypoint" "$DOWNLOAD_SPAN_ID"
DOWNLOAD_START=$SECONDS

# Phase 1: Checkpoint Resume for GGUF Download
if gsutil stat "${CHECKPOINT_PREFIX}/model.gguf" >/dev/null 2>&1; then
    log_json "INFO" "Checkpoint found! Resuming from GCS checkpoint instead of Hugging Face..." "BatchEntrypoint" "$DOWNLOAD_SPAN_ID"
    gsutil cp "${CHECKPOINT_PREFIX}/model.gguf" "$WORKSPACE/model.gguf"
    GGUF_FILE="$WORKSPACE/model.gguf"
else
    # Extract repo name consistently (fields 4 and 5 in the URL)
    REPO=$(echo "$HF_URL" | awk -F/ '{print $4"/"$5}')

    if [[ "$HF_URL" == *".gguf" ]]; then
        # Direct file link
        FILENAME=$(echo "$HF_URL" | awk -F/ '{print $NF}')
        huggingface-cli download "$REPO" "$FILENAME" --local-dir "$WORKSPACE"
        GGUF_FILE="$WORKSPACE/$FILENAME"
    else
        # Repo link
        huggingface-cli download "$REPO" --local-dir "$WORKSPACE"
        # Safely find the first .gguf file avoiding ls parsing issues
        GGUF_FILE=$(find "$WORKSPACE" -maxdepth 1 -name "*.gguf" | head -n 1)
        if [ -z "$GGUF_FILE" ]; then
            log_json "ERROR" "FATAL: No .gguf file found in repository." "BatchEntrypoint" "$DOWNLOAD_SPAN_ID"
            update_status "Failed" "No .gguf file found in HF repository."
            exit 1
        fi
    fi
    
    log_json "INFO" "Saving GGUF download to Checkpoint bucket..." "BatchEntrypoint" "$DOWNLOAD_SPAN_ID"
    gsutil cp "$GGUF_FILE" "${CHECKPOINT_PREFIX}/model.gguf"
fi

DOWNLOAD_ELAPSED=$((SECONDS - DOWNLOAD_START))
log_json "INFO" "[$JOB_ID] Download phase completed in ${DOWNLOAD_ELAPSED}s" "BatchEntrypoint" "$DOWNLOAD_SPAN_ID"

# --- Phase: Compile ---
COMPILE_SPAN_ID=$(generate_span_id)
COMPILE_EXIT_CODE=0

# Phase 2: Compilation (Check for Checkpoint)
if gsutil stat "${CHECKPOINT_PREFIX}/compiled.tar.gz" >/dev/null 2>&1; then
    log_json "INFO" "Checkpoint found! Resuming compiled output from GCS..." "BatchEntrypoint" "$COMPILE_SPAN_ID"
    mkdir -p "$WORKSPACE/compiled"
    mkdir -p "$WORKSPACE/qnn_compiler_cache"
    gsutil cp "${CHECKPOINT_PREFIX}/compiled.tar.gz" "/tmp/compiled.tar.gz"
    tar -xzf "/tmp/compiled.tar.gz" -C "$WORKSPACE"
else
    log_json "INFO" "[$JOB_ID] Step 2.5: Running ClamAV Sandboxing Scan on Host..." "BatchEntrypoint" "$COMPILE_SPAN_ID"
    clamscan --recursive "$WORKSPACE"
    if [ $? -ne 0 ]; then
        log_json "CRITICAL" "SECURITY FATAL: Malware signature detected in GGUF payload. Build halted." "BatchEntrypoint" "$COMPILE_SPAN_ID"
        update_status "Failed" "Malware signature detected in GGUF."
        exit 1
    fi

    update_status "Compiling" ""
    publish_event "job.compiling"
    log_json "INFO" "[$JOB_ID] Starting compilation..." "BatchEntrypoint" "$COMPILE_SPAN_ID"
    COMPILE_START=$SECONDS
    
    # Pre-create cache directory in persistent workspace
    mkdir -p "$WORKSPACE/qnn_compiler_cache"

    # We wrapper the compile command to strictly trap crashes within the gVisor sandbox.
    # Security hardening: Adding --security-opt=no-new-privileges and stricter container isolation.
    docker run --rm --runtime=runsc \
      --network none \
      --security-opt=no-new-privileges \
      --tmpfs /tmp:rw,size=1G,mode=1777 \
      -v /opt/hexforge:/opt/hexforge:ro \
      -v "$WORKSPACE":"$WORKSPACE":rw \
      -v "$WORKSPACE/qnn_compiler_cache":"/opt/hexforge/qnn_compiler_cache":rw \
      -v /dev/urandom:/dev/urandom:ro \
      -e QNN_SDK_ROOT="$QNN_SDK_ROOT" \
      -e LD_LIBRARY_PATH="$LD_LIBRARY_PATH" \
      -e PATH="/opt/hexforge/venv/bin:$PATH" \
      -e ADB_TIMEOUT="1000" \
      -e TRACE_ID="$TRACE_ID" \
      -e SPAN_ID="$COMPILE_SPAN_ID" \
      -e JOB_ID="$JOB_ID" \
      -e GCP_PROJECT_ID="$GCP_PROJECT_ID" \
      hexforge-sandbox /opt/hexforge/venv/bin/python3 /opt/hexforge/compile.py \
      --input_gguf "$GGUF_FILE" \
      --output_dir "$WORKSPACE/compiled" \
      --job_id "$JOB_ID" \
      --dsp_arch "$DSP_ARCH" \
      --chipset "$CHIPSET" \
      --max_session_memory_gb "${MAX_SESSION_MEM_GB:-3.75}" \
      --needs_logits_offload "${NEEDS_LOGITS_OFFLOAD:-0}" \
      --needs_fastrpc_fix "${NEEDS_FASTRPC_FIX:-0}" \
      --native_int4_support "${NATIVE_INT4:-1}" \
      --has_hmx "${HAS_HMX:-1}" \
      --mmap_budget "${MMAP_BUDGET_MB:-25}" \
      --speculative_decoding "${SPECULATIVE_MODE:-SSD-Q1}" \
      --speculative_forecast "${SPEC_FORECAST:-1}" \
      --speculative_expansion "${SPEC_EXPANSION:-top-1}" \
      --export_tokenizer "${EXPORT_TOKENIZER:-1}" \
      --moe_capable "${MOE_CAPABLE:-0}" \
      --kv_offset_mb "${KV_OFFSET_MB:-512}"
    COMPILE_EXIT_CODE=$?
    set -e
    
    if [ "$COMPILE_EXIT_CODE" -eq 0 ]; then
        COMPILE_ELAPSED=$((SECONDS - COMPILE_START))
        log_json "INFO" "[$JOB_ID] Compilation phase completed in ${COMPILE_ELAPSED}s. Saving to Checkpoint bucket..." "BatchEntrypoint" "$COMPILE_SPAN_ID"
        # Tar both compiled output and cache directory for resume
        tar -czf "/tmp/compiled.tar.gz" -C "$WORKSPACE" compiled qnn_compiler_cache
        gsutil cp "/tmp/compiled.tar.gz" "${CHECKPOINT_PREFIX}/compiled.tar.gz"
    fi
fi

# --- Phase: Upload ---
UPLOAD_SPAN_ID=$(generate_span_id)

if [ "$COMPILE_EXIT_CODE" -eq 0 ]; then
    update_status "Uploading" ""
    publish_event "job.uploading"
    log_json "INFO" "[$JOB_ID] Step 4: Pushing Container to Hugging Face from Host..." "BatchEntrypoint" "$UPLOAD_SPAN_ID"
    UPLOAD_START=$SECONDS
    huggingface-cli upload "$OUTPUT_REPO" "$WORKSPACE/compiled" . --repo-type model --token "$HF_TOKEN"
    if [ $? -ne 0 ]; then
        log_json "ERROR" "Upload failed" "BatchEntrypoint" "$UPLOAD_SPAN_ID"
        update_status "Failed" "HF Upload Failed."
        exit 1
    fi
    UPLOAD_ELAPSED=$((SECONDS - UPLOAD_START))
    log_json "INFO" "[$JOB_ID] Upload phase completed in ${UPLOAD_ELAPSED}s" "BatchEntrypoint" "$UPLOAD_SPAN_ID"
fi

# 4. Teardown & Reporting
if [ -n "$SSD_DEVICE" ]; then
    swapoff $SSD_DEVICE || true
else
    swapoff $SWAP_FILE || true
    rm -f $SWAP_FILE
fi

rm -rf "/opt/hexforge/jobs/$JOB_ID"

TOTAL_ELAPSED=$((SECONDS - PHASE_START))
if [ "$COMPILE_EXIT_CODE" -eq 0 ]; then
    log_json "INFO" "Job $JOB_ID completed successfully! Total elapsed: ${TOTAL_ELAPSED}s"
    update_status "Completed" ""
    exit 0
else
    log_json "ERROR" "Job $JOB_ID FAILED with exit code $COMPILE_EXIT_CODE. Total elapsed: ${TOTAL_ELAPSED}s"
    update_status "Failed" "QAIRT Compilation crashed. Pipeline halted."
    exit 1  # Standard non-zero exit lets Cloud Batch know the job failed organically
fi
