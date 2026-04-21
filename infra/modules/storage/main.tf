# =============================================================
# Module: storage
# Purpose: GCS Buckets (functions code, checkpoints),
# Secret Manager for HF token, and Firestore database.
# =============================================================

variable "project_id" {
  type = string
}
variable "region" {
  type = string
}

# --- Secret Manager: Hugging Face Token ---
resource "google_secret_manager_secret" "hf_token" {
  secret_id = "hf-token"
  project   = var.project_id

  replication {
    auto {}
  }
}

# --- Firestore Database ---
resource "google_firestore_database" "hexforge_db" {
  name        = "(default)"
  project     = var.project_id
  location_id = var.region
  type        = "FIRESTORE_NATIVE"
}

# Composite index for efficient stale job reaper queries
resource "google_firestore_index" "jobs_status_updated_at" {
  project    = var.project_id
  database   = google_firestore_database.hexforge_db.name
  collection = "Jobs"

  fields {
    field_path = "status"
    order      = "ASCENDING"
  }

  fields {
    field_path = "updated_at"
    order      = "ASCENDING"
  }
}

# --- GCS: Functions Source Code ---
resource "google_storage_bucket" "functions_bucket" {
  name     = "${var.project_id}-hexforge-functions"
  location = var.region
  project  = var.project_id
}

# --- GCS: Spot VM Checkpoints ---
# Spot preemption checkpoints enable job resumption without restarting
resource "google_storage_bucket" "hexforge_checkpoints" {
  name          = "${var.project_id}-hexforge-checkpoints"
  location      = var.region
  project       = var.project_id
  force_destroy = true

  lifecycle_rule {
    condition {
      age = 3 # Clean up stale checkpoints after 3 days
    }
    action {
      type = "Delete"
    }
  }
}

# --- Outputs ---
output "hf_token_secret_id" {
  description = "The resource ID of the HF Token secret"
  value       = google_secret_manager_secret.hf_token.id
}

output "functions_bucket_name" {
  description = "GCS bucket name for Cloud Functions source"
  value       = google_storage_bucket.functions_bucket.name
}

output "checkpoint_bucket_name" {
  description = "GCS bucket name for Spot VM checkpoints"
  value       = google_storage_bucket.hexforge_checkpoints.name
}
