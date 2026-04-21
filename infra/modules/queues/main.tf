# =============================================================
# Module: queues
# Purpose: Cloud Tasks queues (primary + DLQ) and Pub/Sub
# event bus (topics, subscriptions, DLQ topic, IAM.
# =============================================================

variable "project_id" {
  type = string
}
variable "region" {
  type = string
}
variable "project_number" {
  type        = string
  description = "Numeric GCP project number for service agent emails"
}
variable "event_processor_url" {
  type        = string
  description = "HTTPS trigger URL of the EventProcessor Cloud Function"
}
variable "functions_sa_email" {
  type        = string
  description = "Email of the HexForge Cloud Functions Service Account"
}

# --- Cloud Tasks: Dead-Letter Queue (must be created before primary) ---
resource "google_cloud_tasks_queue" "dlq_queue" {
  name     = "hexforge-dlq"
  location = var.region
  project  = var.project_id

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

# --- Cloud Tasks: Primary Job Queue ---
resource "google_cloud_tasks_queue" "job_queue" {
  name     = "hexforge-job-queue"
  location = var.region
  project  = var.project_id

  rate_limits {
    max_concurrent_dispatches = 10
    max_dispatches_per_second = 2
  }

  retry_config {
    max_attempts       = 100
    max_retry_duration = "3600s" # 1 hour of exponential backoff when budget-capped
    min_backoff        = "5s"
    max_backoff        = "600s"
    max_doublings      = 4
  }

  stackdriver_logging_config {
    sampling_ratio = 1.0 # 100% log sampling for full observability
  }

  depends_on = [google_cloud_tasks_queue.dlq_queue]
}

# --- Pub/Sub: Primary Event Bus Topic ---
resource "google_pubsub_topic" "job_events" {
  name    = "hexforge-job-events"
  project = var.project_id

  message_retention_duration = "604800s" # 7 days
}

# --- Pub/Sub: Dead-Letter Topic ---
resource "google_pubsub_topic" "job_events_dlq" {
  name    = "hexforge-job-events-dlq"
  project = var.project_id

  message_retention_duration = "604800s" # 7 days
}

# --- Pub/Sub: Pull Subscription (for analytics / offline consumers) ---
resource "google_pubsub_subscription" "job_events_sub" {
  name    = "hexforge-job-events-sub"
  topic   = google_pubsub_topic.job_events.id
  project = var.project_id

  ack_deadline_seconds       = 30
  message_retention_duration = "604800s"
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

# --- Pub/Sub: Push Subscription (drives the EventProcessor function) ---
resource "google_pubsub_subscription" "job_events_push" {
  name    = "hexforge-job-events-push"
  topic   = google_pubsub_topic.job_events.id
  project = var.project_id

  ack_deadline_seconds = 60

  push_config {
    push_endpoint = var.event_processor_url

    oidc_token {
      service_account_email = var.functions_sa_email
    }
  }

  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.job_events_dlq.id
    max_delivery_attempts = 5
  }
}

# --- Pub/Sub IAM: Allow Pub/Sub service agent to route dead-letters ---
resource "google_pubsub_topic_iam_member" "pubsub_dlq_publisher" {
  topic   = google_pubsub_topic.job_events_dlq.id
  project = var.project_id
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:service-${var.project_number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}

resource "google_pubsub_subscription_iam_member" "pubsub_dlq_subscriber" {
  subscription = google_pubsub_subscription.job_events_sub.id
  project      = var.project_id
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:service-${var.project_number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}

# --- Outputs ---
output "job_events_topic_name" {
  description = "The name of the primary Pub/Sub topic"
  value       = google_pubsub_topic.job_events.name
}
