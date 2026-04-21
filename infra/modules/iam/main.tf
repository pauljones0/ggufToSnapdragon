# =============================================================
# Module: iam
# Purpose: Service Accounts and IAM role bindings following
# zero-trust least-privilege for worker, functions, and
# Cloud Batch service agents.
# =============================================================

variable "project_id" {
  type = string
}
variable "project_number" {
  type        = string
  description = "Numeric GCP project number (for service agent emails)"
}
variable "qairt_bucket_name" {
  type        = string
  description = "GCS bucket holding the QAIRT SDK zip"
}
variable "checkpoint_bucket_name" {
  type        = string
  description = "GCS bucket name for Spot VM checkpoints"
}
variable "hf_token_secret_id" {
  type        = string
  description = "Secret Manager secret resource ID for the HF token"
}

# --- Worker Service Account (runs on Batch VMs) ---
resource "google_service_account" "hexforge_worker_sa" {
  account_id   = "hexforge-worker-sa"
  project      = var.project_id
  display_name = "HexForge Worker Node Service Account"
}

# STRICT: Read the QAIRT SDK from GCS
resource "google_storage_bucket_iam_member" "worker_gcs_read" {
  bucket = var.qairt_bucket_name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.hexforge_worker_sa.email}"
}

# Read/write checkpoints (NOT bucket admin)
resource "google_storage_bucket_iam_member" "worker_checkpoint_admin" {
  bucket = var.checkpoint_bucket_name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${google_service_account.hexforge_worker_sa.email}"
}

# STRICT: Only the worker SA can read the HF token secret at runtime
resource "google_secret_manager_secret_iam_member" "worker_secret_accessor" {
  secret_id = var.hf_token_secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.hexforge_worker_sa.email}"
}

# Allow the worker to publish Pub/Sub lifecycle events
resource "google_project_iam_member" "worker_pubsub_publisher" {
  project = var.project_id
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:${google_service_account.hexforge_worker_sa.email}"
}

# --- Cloud Functions Service Account ---
resource "google_service_account" "hexforge_functions_sa" {
  account_id   = "hexforge-functions-sa"
  project      = var.project_id
  display_name = "HexForge Cloud Functions Service Account"
}

resource "google_project_iam_member" "functions_batch_editor" {
  project = var.project_id
  role    = "roles/batch.jobsEditor"
  member  = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

resource "google_project_iam_member" "functions_tasks_enqueuer" {
  project = var.project_id
  role    = "roles/cloudtasks.enqueuer"
  member  = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

resource "google_project_iam_member" "functions_pubsub_publisher" {
  project = var.project_id
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

resource "google_project_iam_member" "functions_pubsub_subscriber" {
  project = var.project_id
  role    = "roles/pubsub.subscriber"
  member  = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

# Allow Cloud Functions to act as the worker SA when submitting Batch Jobs
resource "google_service_account_iam_member" "functions_sa_user" {
  service_account_id = google_service_account.hexforge_worker_sa.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

# --- Service Agents ---
# Allow Cloud Tasks SA to create OIDC tokens for secure DLQ routing
resource "google_project_iam_member" "cloudtasks_sa_token_creator" {
  project = var.project_id
  role    = "roles/iam.serviceAccountTokenCreator"
  member  = "serviceAccount:service-${var.project_number}@gcp-sa-cloudtasks.iam.gserviceaccount.com"
}

# Allow Cloud Batch service agent to inject the HF Token secret into worker VMs
resource "google_secret_manager_secret_iam_member" "batch_agent_secret_accessor" {
  secret_id = var.hf_token_secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:service-${var.project_number}@gcp-sa-batch.iam.gserviceaccount.com"
}

# --- Outputs ---
output "worker_sa_email" {
  description = "Email of the HexForge Worker Service Account"
  value       = google_service_account.hexforge_worker_sa.email
}

output "functions_sa_email" {
  description = "Email of the HexForge Cloud Functions Service Account"
  value       = google_service_account.hexforge_functions_sa.email
}
