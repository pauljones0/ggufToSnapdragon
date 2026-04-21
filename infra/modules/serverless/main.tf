# =============================================================
# Module: serverless
# Purpose: All Cloud Functions deployments, their IAM invoker
# bindings, Cloud Scheduler cron, and SRE alerting.
# =============================================================

variable "project_id" {
  type = string
}
variable "region" {
  type = string
}
variable "zone" {
  type = string
}
variable "project_number" {
  type        = string
  description = "Numeric GCP project number for service agent emails"
}
variable "functions_sa_email" {
  type        = string
  description = "Email of the Cloud Functions Service Account"
}
variable "worker_sa_email" {
  type        = string
  description = "Email of the Worker Node Service Account"
}
variable "functions_bucket_name" {
  type        = string
  description = "GCS bucket containing the source archive"
}
variable "functions_zip_name" {
  type        = string
  description = "GCS object name of the source archive"
}
variable "checkpoint_bucket_name" {
  type        = string
  description = "GCS bucket for Spot VM checkpoints"
}
variable "pubsub_topic_name" {
  type        = string
  description = "Name of the Pub/Sub topic for job lifecycle events"
}

# =============================================================
# Function 1: SubmitJob (Public API Gateway)
# =============================================================
resource "google_cloudfunctions_function" "submit_job" {
  name        = "SubmitJob"
  description = "Validates constraints and pushes jobs into the Firestore queue"
  runtime     = "go121"
  region      = var.region
  project     = var.project_id

  available_memory_mb   = 256
  source_archive_bucket = var.functions_bucket_name
  source_archive_object = var.functions_zip_name
  entry_point           = "SubmitJob"
  trigger_http          = true
  max_instances         = 10

  service_account_email = var.functions_sa_email

  environment_variables = {
    GCP_PROJECT_ID     = var.project_id
    GCP_REGION         = var.region
    FUNCTIONS_SA_EMAIL = var.functions_sa_email
    PUBSUB_TOPIC       = var.pubsub_topic_name
  }
}

# Auth handled inside Go via Firebase JWT; public invocations allowed
resource "google_cloudfunctions_function_iam_member" "invoker_submit_job" {
  project        = google_cloudfunctions_function.submit_job.project
  region         = google_cloudfunctions_function.submit_job.region
  cloud_function = google_cloudfunctions_function.submit_job.name
  role           = "roles/cloudfunctions.invoker"
  member         = "allUsers"
}

# =============================================================
# Function 2: QueueManager (Cloud Tasks consumer)
# =============================================================
resource "google_cloudfunctions_function" "queue_manager" {
  name        = "QueueManager"
  description = "Invoked by Cloud Tasks. Provisions Google Cloud Batch Jobs"
  runtime     = "go121"
  region      = var.region
  project     = var.project_id

  available_memory_mb   = 256
  source_archive_bucket = var.functions_bucket_name
  source_archive_object = var.functions_zip_name
  entry_point           = "QueueManager"
  trigger_http          = true
  max_instances         = 10

  service_account_email = var.functions_sa_email

  environment_variables = {
    GCP_PROJECT_ID    = var.project_id
    GCP_REGION        = var.region
    GCP_ZONE          = var.zone
    CHECKPOINT_BUCKET = var.checkpoint_bucket_name
    PUBSUB_TOPIC      = var.pubsub_topic_name
  }
}

# Only the Functions SA (via Cloud Tasks) can invoke QueueManager
resource "google_cloudfunctions_function_iam_member" "tasks_invoke_queue_manager" {
  project        = google_cloudfunctions_function.queue_manager.project
  region         = google_cloudfunctions_function.queue_manager.region
  cloud_function = google_cloudfunctions_function.queue_manager.name
  role           = "roles/cloudfunctions.invoker"
  member         = "serviceAccount:${var.functions_sa_email}"
}

# =============================================================
# Function 3: UpdateJobStatus (Internal Webhook)
# =============================================================
resource "google_cloudfunctions_function" "update_job_status" {
  name        = "UpdateJobStatus"
  description = "Allows the worker VM to report status back securely via IAM auth"
  runtime     = "go121"
  region      = var.region
  project     = var.project_id

  available_memory_mb   = 128
  source_archive_bucket = var.functions_bucket_name
  source_archive_object = var.functions_zip_name
  entry_point           = "UpdateJobStatus"
  trigger_http          = true

  service_account_email = var.functions_sa_email

  environment_variables = {
    GCP_PROJECT_ID = var.project_id
    PUBSUB_TOPIC   = var.pubsub_topic_name
  }
}

# STRICT: Only the Worker SA can invoke this internal webhook
resource "google_cloudfunctions_function_iam_member" "worker_invoke_update_status" {
  project        = google_cloudfunctions_function.update_job_status.project
  region         = google_cloudfunctions_function.update_job_status.region
  cloud_function = google_cloudfunctions_function.update_job_status.name
  role           = "roles/cloudfunctions.invoker"
  member         = "serviceAccount:${var.worker_sa_email}"
}

# =============================================================
# Function 4: StaleJobReaper (Cron Monitor)
# =============================================================
resource "google_cloudfunctions_function" "stale_job_reaper" {
  name        = "StaleJobReaper"
  description = "Cron job that cleans up ghost HexForge worker jobs"
  runtime     = "go121"
  region      = var.region
  project     = var.project_id

  available_memory_mb   = 128
  source_archive_bucket = var.functions_bucket_name
  source_archive_object = var.functions_zip_name
  entry_point           = "StaleJobReaper"
  trigger_http          = true

  service_account_email = var.functions_sa_email

  environment_variables = {
    GCP_PROJECT_ID = var.project_id
    PUBSUB_TOPIC   = var.pubsub_topic_name
  }
}

resource "google_cloudfunctions_function_iam_member" "scheduler_invoke_reaper" {
  project        = google_cloudfunctions_function.stale_job_reaper.project
  region         = google_cloudfunctions_function.stale_job_reaper.region
  cloud_function = google_cloudfunctions_function.stale_job_reaper.name
  role           = "roles/cloudfunctions.invoker"
  member         = "serviceAccount:${var.functions_sa_email}"
}

# Cloud Scheduler: triggers the Reaper every 30 minutes
resource "google_cloud_scheduler_job" "reaper_cron" {
  name        = "hexforge-reaper-cron"
  description = "Triggers the StaleJobReaper every 30 minutes"
  schedule    = "*/30 * * * *"
  time_zone   = "Etc/UTC"
  project     = var.project_id
  region      = var.region

  http_target {
    http_method = "GET"
    uri         = google_cloudfunctions_function.stale_job_reaper.https_trigger_url

    oidc_token {
      service_account_email = var.functions_sa_email
    }
  }
}

# =============================================================
# Function 5: DeadLetterHandler (DLQ Consumer)
# =============================================================
resource "google_cloudfunctions_function" "dead_letter_handler" {
  name        = "DeadLetterHandler"
  description = "Handles permanently failed Cloud Tasks, releases FinOps lock"
  runtime     = "go121"
  region      = var.region
  project     = var.project_id

  available_memory_mb   = 128
  source_archive_bucket = var.functions_bucket_name
  source_archive_object = var.functions_zip_name
  entry_point           = "DeadLetterHandler"
  trigger_http          = true

  service_account_email = var.functions_sa_email

  environment_variables = {
    GCP_PROJECT_ID = var.project_id
    GCP_REGION     = var.region
    PUBSUB_TOPIC   = var.pubsub_topic_name
  }
}

resource "google_cloudfunctions_function_iam_member" "tasks_invoke_dead_letter" {
  project        = google_cloudfunctions_function.dead_letter_handler.project
  region         = google_cloudfunctions_function.dead_letter_handler.region
  cloud_function = google_cloudfunctions_function.dead_letter_handler.name
  role           = "roles/cloudfunctions.invoker"
  member         = "serviceAccount:${var.functions_sa_email}"
}

# =============================================================
# Function 6: EventProcessor (Pub/Sub consumer)
# =============================================================
resource "google_cloudfunctions_function" "event_processor" {
  name        = "EventProcessor"
  description = "Consumes Pub/Sub lifecycle events for auditing and alerting"
  runtime     = "go121"
  region      = var.region
  project     = var.project_id

  available_memory_mb   = 128
  source_archive_bucket = var.functions_bucket_name
  source_archive_object = var.functions_zip_name
  entry_point           = "EventProcessor"
  trigger_http          = true

  service_account_email = var.functions_sa_email

  environment_variables = {
    GCP_PROJECT_ID = var.project_id
  }
}

# Allow Pub/Sub service agent to invoke EventProcessor
resource "google_cloudfunctions_function_iam_member" "pubsub_invoke_event_processor" {
  project        = google_cloudfunctions_function.event_processor.project
  region         = google_cloudfunctions_function.event_processor.region
  cloud_function = google_cloudfunctions_function.event_processor.name
  role           = "roles/cloudfunctions.invoker"
  member         = "serviceAccount:service-${var.project_number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}

# =============================================================
# SRE Observability: Log-Based Metric + Alert Policy
# =============================================================
resource "google_logging_metric" "reaped_jobs_metric" {
  name        = "hexforge/reaped_jobs_count"
  description = "Counts ghost jobs reaped by the StaleJobReaper"
  project     = var.project_id
  filter      = "jsonPayload.component=\"EventProcessor\" AND jsonPayload.message=\"Processing event: job.reaped\""

  metric_descriptor {
    metric_kind = "DELTA"
    value_type  = "INT64"
  }
}

resource "google_monitoring_alert_policy" "reaped_jobs_alert" {
  display_name = "HexForge - Ghost Job Reaped Alert"
  project      = var.project_id
  combiner     = "OR"

  conditions {
    display_name = "Ghost Job Reaped > 0"
    condition_threshold {
      filter     = "metric.type=\"logging.googleapis.com/user/${google_logging_metric.reaped_jobs_metric.name}\""
      duration   = "0s"
      comparison = "COMPARISON_GT"

      aggregations {
        alignment_period     = "60s"
        per_series_aligner   = "ALIGN_DELTA"
        cross_series_reducer = "REDUCE_SUM"
      }

      threshold_value = 0
    }
  }

  documentation {
    content   = "A worker node crashed without emitting an error, triggering the StaleJobReaper. Check Cloud Batch logs for OOM or preemption events."
    mime_type = "text/markdown"
  }
}

# --- Outputs ---
output "event_processor_url" {
  description = "HTTPS trigger URL of the EventProcessor Cloud Function"
  value       = google_cloudfunctions_function.event_processor.https_trigger_url
}

output "submit_job_url" {
  description = "HTTPS trigger URL of the SubmitJob Cloud Function"
  value       = google_cloudfunctions_function.submit_job.https_trigger_url
}
