terraform {
  backend "gcs" {
    bucket  = "hexforge-terraform-state-bucket"
    prefix  = "terraform/state"
  }
}

variable "project_id" {
  description = "The GCP Project ID"
  type        = string
}

variable "region" {
  description = "The GCP Region"
  type        = string
  default     = "us-central1"
}

variable "zone" {
  description = "The GCP Zone for the worker VM"
  type        = string
  default     = "us-central1-a"
}

variable "qairt_bucket_name" {
  description = "Name of the GCS bucket holding the QAIRT SDK zip"
  type        = string
}

provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}

# --- 0. Data Sources ---
data "google_project" "project" {}

# --- 1. Enable Required APIs ---
resource "google_project_service" "compute" {
  service            = "compute.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "firestore" {
  service            = "firestore.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "cloudfunctions" {
  service            = "cloudfunctions.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "secretmanager" {
  service            = "secretmanager.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "cloudrun" {
  service            = "run.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "cloudbuild" {
  service            = "cloudbuild.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "batch" {
  service            = "batch.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "cloudtasks" {
  service            = "cloudtasks.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "cloudscheduler" {
  service            = "cloudscheduler.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "pubsub" {
  service            = "pubsub.googleapis.com"
  disable_on_destroy = false
}

# --- 2. IAM & Service Accounts ---
# The Service Account the worker VM will run as
resource "google_service_account" "hexforge_worker_sa" {
  account_id   = "hexforge-worker-sa"
  display_name = "HexForge Worker Node Service Account"
}

# Allow the worker SA to read the QAIRT SDK from GCS
resource "google_storage_bucket_iam_member" "worker_gcs_read" {
  bucket = var.qairt_bucket_name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.hexforge_worker_sa.email}"
}

# Allow the worker SA to publish Pub/Sub lifecycle events
resource "google_project_iam_member" "worker_pubsub_publisher" {
  project = var.project_id
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:${google_service_account.hexforge_worker_sa.email}"
}

# --- 2.5 Checkpoint Bucket (For Spot VM Resumption) ---
resource "google_storage_bucket" "hexforge_checkpoints" {
  name          = "${var.project_id}-hexforge-checkpoints"
  location      = var.region
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

# Allow the worker SA to read/write checkpoints but NOT modify bucket ACLs/metadata
resource "google_storage_bucket_iam_member" "worker_checkpoint_admin" {
  bucket = google_storage_bucket.hexforge_checkpoints.name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${google_service_account.hexforge_worker_sa.email}"
}

# The Service Account the Cloud Functions will run as
resource "google_service_account" "hexforge_functions_sa" {
  account_id   = "hexforge-functions-sa"
  display_name = "HexForge Cloud Functions Service Account"
}

# Allow Cloud Functions to submit Cloud Batch Jobs
resource "google_project_iam_member" "functions_batch_editor" {
  project = var.project_id
  role    = "roles/batch.jobsEditor"
  member  = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

# Allow Cloud Functions to enqueue Cloud Tasks
resource "google_project_iam_member" "functions_tasks_enqueuer" {
  project = var.project_id
  role    = "roles/cloudtasks.enqueuer"
  member  = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

# Allow Cloud Functions to publish Pub/Sub lifecycle events
resource "google_project_iam_member" "functions_pubsub_publisher" {
  project = var.project_id
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

# Allow Cloud Functions to consume Pub/Sub messages (for future event-driven handlers)
resource "google_project_iam_member" "functions_pubsub_subscriber" {
  project = var.project_id
  role    = "roles/pubsub.subscriber"
  member  = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

# Allow Cloud Tasks SA to create OIDC tokens for DLQ routing
resource "google_project_iam_member" "cloudtasks_sa_token_creator" {
  project = var.project_id
  role    = "roles/iam.serviceAccountTokenCreator"
  member  = "serviceAccount:service-${data.google_project.project.number}@gcp-sa-cloudtasks.iam.gserviceaccount.com"
}

# Allow Cloud Functions to ACT AS the worker SA when submitting Batch Jobs
resource "google_service_account_iam_member" "functions_sa_user" {
  service_account_id = google_service_account.hexforge_worker_sa.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

# --- 3. Secret Manager (For HF Token) ---
resource "google_secret_manager_secret" "hf_token" {
  secret_id = "hf-token"
  replication {
    auto {}
  }
}

# STRICT IAM: Only the worker SA can ACCESS the secret value at runtime
resource "google_secret_manager_secret_iam_member" "worker_secret_accessor" {
  secret_id = google_secret_manager_secret.hf_token.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.hexforge_worker_sa.email}"
}

# Add Secret Accessor for the Cloud Batch Service Agent so it can inject the secret into the VM env
resource "google_secret_manager_secret_iam_member" "batch_agent_secret_accessor" {
  secret_id = google_secret_manager_secret.hf_token.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:service-${data.google_project.project.number}@gcp-sa-batch.iam.gserviceaccount.com"
}

# NOTE: We bind directly to HF Token secret
# --- 4. Firestore Database ---
resource "google_firestore_database" "hexforge_db" {
  name        = "(default)"
  location_id = var.region
  type        = "FIRESTORE_NATIVE"
  
  depends_on = [google_project_service.firestore]
}

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

# --- 5. VPC & Private Networking for Cloud Batch Workers ---
resource "google_compute_network" "hexforge_vpc" {
  name                    = "hexforge-vpc"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "hexforge_subnet" {
  name                     = "hexforge-subnet"
  ip_cidr_range            = "10.0.0.0/24"
  region                   = var.region
  network                  = google_compute_network.hexforge_vpc.id
  private_ip_google_access = true

  log_config {
    aggregation_interval = "INTERVAL_10_MIN"
    flow_sampling        = 0.5
    metadata             = "INCLUDE_ALL_METADATA"
  }
}

resource "google_compute_router" "hexforge_router" {
  name    = "hexforge-router"
  region  = var.region
  network = google_compute_network.hexforge_vpc.id
}

# Cloud NAT enables the worker Nodes (which have no public IPs) to securely egress
# to HuggingFace to download the large model files.
resource "google_compute_router_nat" "hexforge_nat" {
  name                               = "hexforge-nat"
  router                             = google_compute_router.hexforge_router.name
  region                             = var.region
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"
}

# --- 6. Cloud Functions Code Packaging & Deployment ---
# Ensure Cloud Functions are deployed as part of the architecture definition instead of manual scripts.
data "archive_file" "backend_source" {
  type        = "zip"
  source_dir  = "${path.module}/../back_end"
  output_path = "${path.module}/backend_source.zip"
}

resource "google_storage_bucket" "functions_bucket" {
  name     = "${var.project_id}-hexforge-functions"
  location = var.region
}

resource "google_storage_bucket_object" "functions_zip" {
  name   = "source-${data.archive_file.backend_source.output_md5}.zip"
  bucket = google_storage_bucket.functions_bucket.name
  source = data.archive_file.backend_source.output_path
}

# --- 7. Cloud Tasks Queues ---
# Primary job processing queue
resource "google_cloud_tasks_queue" "job_queue" {
  name     = "hexforge-job-queue"
  location = var.region
  
  rate_limits {
    max_concurrent_dispatches = 10
    max_dispatches_per_second = 2
  }

  retry_config {
    max_attempts       = 100
    max_retry_duration = "3600s" # 1 hour of backoff retries if budget capped
    min_backoff        = "5s"
    max_backoff        = "600s"
    max_doublings      = 4
  }

  stackdriver_logging_config {
    sampling_ratio = 1.0 # Log 100% of task dispatches for full observability
  }

  depends_on = [google_cloud_tasks_queue.dlq_queue]
}

# Dead-Letter Queue: receives tasks that exhaust all retries on the primary queue.
# The DLQ queue itself has lenient settings since the DeadLetterHandler should always succeed.
resource "google_cloud_tasks_queue" "dlq_queue" {
  name     = "hexforge-dlq"
  location = var.region

  rate_limits {
    max_concurrent_dispatches = 5
    max_dispatches_per_second = 1
  }

  retry_config {
    max_attempts  = 5
    min_backoff   = "10s"
    max_backoff   = "300s"
    max_doublings = 3
  }

  stackdriver_logging_config {
    sampling_ratio = 1.0
  }
}

# --- 7.5 Pub/Sub Event Bus ---
# Job lifecycle events are published here for downstream consumers
# (e.g., Slack alerts, analytics dashboards, mobile push notifications)
resource "google_pubsub_topic" "job_events" {
  name = "hexforge-job-events"

  message_retention_duration = "604800s" # 7 days

  depends_on = [google_project_service.pubsub]
}

# Dead-Letter Topic: receives Pub/Sub messages that could not be delivered
# after max_delivery_attempts on the primary subscription.
resource "google_pubsub_topic" "job_events_dlq" {
  name = "hexforge-job-events-dlq"

  message_retention_duration = "604800s" # 7 days

  depends_on = [google_project_service.pubsub]
}

# Primary pull subscription for downstream consumers (analytics, alerting, etc.)
resource "google_pubsub_subscription" "job_events_sub" {
  name  = "hexforge-job-events-sub"
  topic = google_pubsub_topic.job_events.id

  ack_deadline_seconds       = 30
  message_retention_duration = "604800s" # 7 days
  retain_acked_messages      = false
  enable_message_ordering    = true

  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.job_events_dlq.id
    max_delivery_attempts = 5
  }

  retry_policy {
    minimum_backoff = "10s"
    maximum_backoff = "600s"
  }
}

# Allow the Pub/Sub service account to publish to the DLQ topic
# (required for dead-letter routing to function)
resource "google_pubsub_topic_iam_member" "pubsub_dlq_publisher" {
  topic  = google_pubsub_topic.job_events_dlq.id
  role   = "roles/pubsub.publisher"
  member = "serviceAccount:service-${data.google_project.project.number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}

# Allow the Pub/Sub service account to acknowledge messages on the primary subscription
# (required for dead-letter routing to function)
resource "google_pubsub_subscription_iam_member" "pubsub_dlq_subscriber" {
  subscription = google_pubsub_subscription.job_events_sub.id
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:service-${data.google_project.project.number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}

# Function 1: SubmitJob (The Public API Gateway)
resource "google_cloudfunctions_function" "submit_job" {
  name        = "SubmitJob"
  description = "Validates constraints and pushes jobs into Firestore Queue"
  runtime     = "go121"
  region      = var.region

  available_memory_mb   = 256
  source_archive_bucket = google_storage_bucket.functions_bucket.name
  source_archive_object = google_storage_bucket_object.functions_zip.name
  entry_point           = "SubmitJob"
  trigger_http          = true
  max_instances         = 10

  service_account_email = google_service_account.hexforge_functions_sa.email

  environment_variables = {
    GCP_PROJECT_ID     = var.project_id
    GCP_REGION         = var.region
    FUNCTIONS_SA_EMAIL = google_service_account.hexforge_functions_sa.email
    PUBSUB_TOPIC       = google_pubsub_topic.job_events.name
  }
}

# Allow public invocations of SubmitJob (Auth is handled internally in Go via Firebase JWT)
resource "google_cloudfunctions_function_iam_member" "invoker_submit_job" {
  project        = google_cloudfunctions_function.submit_job.project
  region         = google_cloudfunctions_function.submit_job.region
  cloud_function = google_cloudfunctions_function.submit_job.name
  role           = "roles/cloudfunctions.invoker"
  member         = "allUsers"
}

# Function 2: QueueManager (The Cloud Tasks Consumer)
resource "google_cloudfunctions_function" "queue_manager" {
  name        = "QueueManager"
  description = "Invoked by Cloud Tasks. Provisions Google Cloud Batch Jobs"
  runtime     = "go121"
  region      = var.region

  available_memory_mb   = 256
  source_archive_bucket = google_storage_bucket.functions_bucket.name
  source_archive_object = google_storage_bucket_object.functions_zip.name
  entry_point           = "QueueManager"
  trigger_http          = true
  max_instances         = 10

  service_account_email = google_service_account.hexforge_functions_sa.email

  environment_variables = {
    GCP_PROJECT_ID    = var.project_id
    GCP_REGION        = var.region
    GCP_ZONE          = var.zone
    CHECKPOINT_BUCKET = google_storage_bucket.hexforge_checkpoints.name
    PUBSUB_TOPIC      = google_pubsub_topic.job_events.name
  }
}

# Allow SubmitJob (via Cloud Tasks) to invoke QueueManager securely
resource "google_cloudfunctions_function_iam_member" "tasks_invoke_queue_manager" {
  project        = google_cloudfunctions_function.queue_manager.project
  region         = google_cloudfunctions_function.queue_manager.region
  cloud_function = google_cloudfunctions_function.queue_manager.name
  role           = "roles/cloudfunctions.invoker"
  member         = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

# Function 3: UpdateJobStatus (The Internal Webhook)
resource "google_cloudfunctions_function" "update_job_status" {
  name        = "UpdateJobStatus"
  description = "Allows the worker VM to report status back to Terraform securely via IAM Auth"
  runtime     = "go121"
  region      = var.region

  available_memory_mb   = 128
  source_archive_bucket = google_storage_bucket.functions_bucket.name
  source_archive_object = google_storage_bucket_object.functions_zip.name
  entry_point           = "UpdateJobStatus"
  trigger_http          = true
  
  service_account_email = google_service_account.hexforge_functions_sa.email

  environment_variables = {
    GCP_PROJECT_ID = var.project_id
    PUBSUB_TOPIC   = google_pubsub_topic.job_events.name
  }
}

# STRICT SECURITY: Only the Worker Service Account can hit the UpdateJobStatus hook. 
# Anonymous requests will be blocked by API Gateway inherently.
resource "google_cloudfunctions_function_iam_member" "worker_invoke_update_status" {
  project        = google_cloudfunctions_function.update_job_status.project
  region         = google_cloudfunctions_function.update_job_status.region
  cloud_function = google_cloudfunctions_function.update_job_status.name
  role           = "roles/cloudfunctions.invoker"
  member         = "serviceAccount:${google_service_account.hexforge_worker_sa.email}"
}

# Function 4: StaleJobReaper (The Cron Monitor)
resource "google_cloudfunctions_function" "stale_job_reaper" {
  name        = "StaleJobReaper"
  description = "Cron job that cleans up stuck HexForge worker jobs"
  runtime     = "go121"
  region      = var.region

  available_memory_mb   = 128
  source_archive_bucket = google_storage_bucket.functions_bucket.name
  source_archive_object = google_storage_bucket_object.functions_zip.name
  entry_point           = "StaleJobReaper"
  trigger_http          = true
  
  service_account_email = google_service_account.hexforge_functions_sa.email

  environment_variables = {
    GCP_PROJECT_ID = var.project_id
    PUBSUB_TOPIC   = google_pubsub_topic.job_events.name
  }
}

# Allow Cloud Scheduler to invoke the Reaper securely
resource "google_cloudfunctions_function_iam_member" "scheduler_invoke_reaper" {
  project        = google_cloudfunctions_function.stale_job_reaper.project
  region         = google_cloudfunctions_function.stale_job_reaper.region
  cloud_function = google_cloudfunctions_function.stale_job_reaper.name
  role           = "roles/cloudfunctions.invoker"
  member         = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}

# The Cron Trigger
resource "google_cloud_scheduler_job" "reaper_cron" {
  name             = "hexforge-reaper-cron"
  description      = "Triggers the StaleJobReaper every 30 minutes"
  schedule         = "*/30 * * * *"
  time_zone        = "Etc/UTC"
  
  http_target {
    http_method = "GET"
    uri         = google_cloudfunctions_function.stale_job_reaper.https_trigger_url
    
    oidc_token {
      service_account_email = google_service_account.hexforge_functions_sa.email
    }
  }
}

# --- 8. Dead-Letter Queue Handler ---
# When Cloud Tasks exhausts all 100 retries (e.g. budget cap never freed),
# the task is forwarded here to gracefully mark the job Failed and release the FinOps lock.
resource "google_cloudfunctions_function" "dead_letter_handler" {
  name        = "DeadLetterHandler"
  description = "Handles permanently failed Cloud Tasks that exhausted all retries"
  runtime     = "go121"
  region      = var.region

  available_memory_mb   = 128
  source_archive_bucket = google_storage_bucket.functions_bucket.name
  source_archive_object = google_storage_bucket_object.functions_zip.name
  entry_point           = "DeadLetterHandler"
  trigger_http          = true

  service_account_email = google_service_account.hexforge_functions_sa.email

  environment_variables = {
    GCP_PROJECT_ID = var.project_id
    GCP_REGION     = var.region
    PUBSUB_TOPIC   = google_pubsub_topic.job_events.name
  }
}

# Allow Cloud Tasks to invoke the DLQ handler when retries are exhausted
resource "google_cloudfunctions_function_iam_member" "tasks_invoke_dead_letter" {
  project        = google_cloudfunctions_function.dead_letter_handler.project
  region         = google_cloudfunctions_function.dead_letter_handler.region
  cloud_function = google_cloudfunctions_function.dead_letter_handler.name
  role           = "roles/cloudfunctions.invoker"
  member         = "serviceAccount:${google_service_account.hexforge_functions_sa.email}"
}
# Function 6: EventProcessor (The Pub/Sub Event Consumer)
resource "google_cloudfunctions_function" "event_processor" {
  name        = "EventProcessor"
  description = "Consumes Pub/Sub lifecycle events for auditing and alerting"
  runtime     = "go121"
  region      = var.region

  available_memory_mb   = 128
  source_archive_bucket = google_storage_bucket.functions_bucket.name
  source_archive_object = google_storage_bucket_object.functions_zip.name
  entry_point           = "EventProcessor"
  trigger_http          = true

  service_account_email = google_service_account.hexforge_functions_sa.email

  environment_variables = {
    GCP_PROJECT_ID = var.project_id
  }
}

# Allow Pub/Sub to invoke the EventProcessor securely
resource "google_cloudfunctions_function_iam_member" "pubsub_invoke_event_processor" {
  project        = google_cloudfunctions_function.event_processor.project
  region         = google_cloudfunctions_function.event_processor.region
  cloud_function = google_cloudfunctions_function.event_processor.name
  role           = "roles/cloudfunctions.invoker"
  member         = "serviceAccount:service-${data.google_project.project.number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}

# Add a push subscription for the EventProcessor
resource "google_pubsub_subscription" "job_events_push" {
  name  = "hexforge-job-events-push"
  topic = google_pubsub_topic.job_events.id

  ack_deadline_seconds = 60

  push_config {
    push_endpoint = google_cloudfunctions_function.event_processor.https_trigger_url
    
    oidc_token {
      service_account_email = google_service_account.hexforge_functions_sa.email
    }
  }

  # Use the same DLQ policy as the pull subscription
  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.job_events_dlq.id
    max_delivery_attempts = 5
  }
}

# --- 9. SRE Observability & Alerting ---

# Create a Log-Based Metric to count 'job.reaped' events
resource "google_logging_metric" "reaped_jobs_metric" {
  name        = "hexforge/reaped_jobs_count"
  description = "Counts the number of Ghost Jobs reaped by the StaleJobReaper"
  filter      = "jsonPayload.component=\"EventProcessor\" AND jsonPayload.message=\"Processing event: job.reaped\""
  
  metric_descriptor {
    metric_kind = "DELTA"
    value_type  = "INT64"
  }
}

# Create an Alert Policy that triggers if any job is reaped
resource "google_monitoring_alert_policy" "reaped_jobs_alert" {
  display_name = "HexForge - Ghost Job Reaped Alert"
  combiner     = "OR"
  
  conditions {
    display_name = "Ghost Job Reaped > 0"
    condition_threshold {
      filter     = "metric.type=\"logging.googleapis.com/user/${google_logging_metric.reaped_jobs_metric.name}\""
      duration   = "0s" # Immediate
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
    content   = "A worker node failed to update its status or crashed without emitting an error, triggering the StaleJobReaper. Check the Cloud Batch logs for OOM or preemption events."
    mime_type = "text/markdown"
  }
}
