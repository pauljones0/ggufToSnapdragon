# =============================================================
# Module: networking
# Purpose: Private VPC, subnets, Cloud Router, and Cloud NAT
# for batch worker nodes with no external public IPs.
# =============================================================

variable "project_id" {
  type = string
}
variable "region" {
  type = string
}

# Private VPC for Cloud Batch workers
resource "google_compute_network" "hexforge_vpc" {
  name                    = "hexforge-vpc"
  project                 = var.project_id
  auto_create_subnetworks = false
}

# Private subnet with Google API access enabled
resource "google_compute_subnetwork" "hexforge_subnet" {
  name                     = "hexforge-subnet"
  ip_cidr_range            = "10.0.0.0/24"
  region                   = var.region
  project                  = var.project_id
  network                  = google_compute_network.hexforge_vpc.id
  private_ip_google_access = true

  log_config {
    aggregation_interval = "INTERVAL_10_MIN"
    flow_sampling        = 0.5
    metadata             = "INCLUDE_ALL_METADATA"
  }
}

# Cloud Router enables Cloud NAT for secure outbound egress
resource "google_compute_router" "hexforge_router" {
  name    = "hexforge-router"
  region  = var.region
  project = var.project_id
  network = google_compute_network.hexforge_vpc.id
}

# Cloud NAT: allows worker nodes (no public IPs) to reach Hugging Face
resource "google_compute_router_nat" "hexforge_nat" {
  name                               = "hexforge-nat"
  router                             = google_compute_router.hexforge_router.name
  region                             = var.region
  project                            = var.project_id
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"
}

# --- Outputs ---
output "vpc_id" {
  description = "The ID of the HexForge VPC network"
  value       = google_compute_network.hexforge_vpc.id
}

output "subnet_id" {
  description = "The ID of the HexForge subnet"
  value       = google_compute_subnetwork.hexforge_subnet.id
}
