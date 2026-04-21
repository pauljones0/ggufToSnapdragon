# =============================================================
# Root Terraform Orchestration
# Purpose: Wires together all child modules and exposes
# top-level variables and outputs.
#
# Modules:
#   - apis        : Enables required GCP APIs
#   - networking  : VPC, subnet, Cloud Router, Cloud NAT
#   - storage     : GCS buckets, Secret Manager, Firestore
#   - iam         : Service accounts, role bindings, service agents
#   - queues      : Cloud Tasks queues, Pub/Sub topics & subscriptions
#   - serverless  : All Cloud Functions + IAM + Scheduler + SRE alerts
# =============================================================

terraform {
  backend "gcs" {
    bucket = "hexforge-terraform-state-bucket"
    prefix = "terraform/state"
  }
}

# --- Variables ---
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
  description = "The GCP Zone for Batch worker nodes"
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

data "google_project" "project" {}

# --- Module: APIs ---
module "apis" {
  source     = "./modules/apis"
  project_id = var.project_id
}

# --- Module: Networking ---
module "networking" {
  source     = "./modules/networking"
  project_id = var.project_id
  region     = var.region
  depends_on = [module.apis]
}

# --- Module: Storage ---
module "storage" {
  source     = "./modules/storage"
  project_id = var.project_id
  region     = var.region
  depends_on = [module.apis]
}

# --- Cloud Functions Source Archive ---
# This data archive packs the backend code into a GCS object for deployment
data "archive_file" "backend_source" {
  type        = "zip"
  source_dir  = "${path.module}/../back_end"
  output_path = "${path.module}/backend_source.zip"
}

resource "google_storage_bucket_object" "functions_zip" {
  name   = "source-${data.archive_file.backend_source.output_md5}.zip"
  bucket = module.storage.functions_bucket_name
  source = data.archive_file.backend_source.output_path
}

# --- Module: IAM ---
module "iam" {
  source                 = "./modules/iam"
  project_id             = var.project_id
  project_number         = data.google_project.project.number
  qairt_bucket_name      = var.qairt_bucket_name
  checkpoint_bucket_name = module.storage.checkpoint_bucket_name
  hf_token_secret_id     = module.storage.hf_token_secret_id
  depends_on             = [module.storage]
}

# --- Module: Serverless (Functions must exist before Queues can reference event_processor_url) ---
module "serverless" {
  source                 = "./modules/serverless"
  project_id             = var.project_id
  region                 = var.region
  zone                   = var.zone
  project_number         = data.google_project.project.number
  functions_sa_email     = module.iam.functions_sa_email
  worker_sa_email        = module.iam.worker_sa_email
  functions_bucket_name  = module.storage.functions_bucket_name
  functions_zip_name     = google_storage_bucket_object.functions_zip.name
  checkpoint_bucket_name = module.storage.checkpoint_bucket_name
  pubsub_topic_name      = module.queues.job_events_topic_name
  depends_on             = [module.iam, module.storage]
}

# --- Module: Queues ---
module "queues" {
  source              = "./modules/queues"
  project_id          = var.project_id
  region              = var.region
  project_number      = data.google_project.project.number
  event_processor_url = module.serverless.event_processor_url
  functions_sa_email  = module.iam.functions_sa_email
  depends_on          = [module.serverless]
}

# --- Root Outputs ---
output "submit_job_url" {
  description = "The public POST endpoint to submit a new compilation job"
  value       = module.serverless.submit_job_url
}
