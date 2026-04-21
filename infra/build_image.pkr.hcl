packer {
  required_plugins {
    googlecompute = {
      version = ">= 1.1.4"
      source  = "github.com/hashicorp/googlecompute"
    }
  }
}

variable "project_id" {
  type        = string
  description = "The GCP Project ID where the image will be built."
}

variable "zone" {
  type    = string
  default = "us-central1-a"
}

variable "network" {
  type    = string
  default = "default"
}

variable "qairt_bucket_uri" {
  type        = string
  description = "The full GS URI to the qairt SDK zip file (e.g. gs://my-bucket/qairt_sdk.zip)"
}

source "googlecompute" "hexforge_base" {
  project_id          = var.project_id
  source_image_family = "ubuntu-2404-lts"
  zone                = var.zone
  image_name          = "hexforge-ubuntu-2404-base-{{timestamp}}"
  image_family        = "hexforge-base"
  disk_size           = 100
  network             = var.network

  # A temporary service account with Read permissions on GCS must be attached
  # to download the QAIRT SDK during build time.
  scopes = [
    "https://www.googleapis.com/auth/cloud-platform",
    "https://www.googleapis.com/auth/devstorage.read_only",
  ]
}

build {
  name    = "hexforge-image-builder"
  sources = ["source.googlecompute.hexforge_base"]

  # 1. System Setup and Dependencies
  provisioner "shell" {
    inline = [
      "echo 'Updating System and installing essential tooling...'",
      "sudo apt-get update",
      "sudo apt-get install -y build-essential cmake unzip curl python3.10 python3.10-venv python3-pip htop jq tmux nvme-cli clamav clamav-daemon docker.io",
      
      "echo 'Installing runsc (gVisor) for secure Docker runtime...'",
      "ARCH=$(uname -m)",
      "URL=https://storage.googleapis.com/gvisor/releases/release/latest/$${ARCH}",
      "curl -sSL -O $${URL}/runsc",
      "chmod a+rx runsc",
      "sudo mv runsc /usr/local/bin",
      "sudo /usr/local/bin/runsc install",
      "sudo systemctl restart docker",
      
      "echo 'Building local hexforge-sandbox docker image...'",
      "sudo tee Dockerfile.sandbox <<EOF",
      "FROM ubuntu:24.04",
      "RUN apt-get update && apt-get install -y python3.10 python3-pip clamav && rm -rf /var/lib/apt/lists/*",
      "EOF",
      "sudo docker build -t hexforge-sandbox -f Dockerfile.sandbox .",
      
      "echo 'Updating ClamAV definitions...'",
      "sudo freshclam || true",

      "echo 'Creating global hexforge working directory...'",
      "sudo mkdir -p /opt/hexforge",
      
      "echo 'Downloading QAIRT SDK from GCS...'",
      "sudo gsutil cp ${var.qairt_bucket_uri} /opt/hexforge/qairt_sdk.zip",
      
      "echo 'Extracting QAIRT SDK...'",
      "cd /opt/hexforge",
      "sudo unzip -q qairt_sdk.zip -d qairt",
      "sudo rm qairt_sdk.zip",
      
      # We assume the unzipped folder structure has a 'lib' and 'python' wheel inside.
      "QNN_DIR=$(find /opt/hexforge/qairt -maxdepth 1 -type d -name 'qairt-*' | head -n 1)",
      "echo \"SDK Path: $QNN_DIR\"",
      
      "echo 'Setting up persistent Python Virtual Environment...'",
      "sudo python3.10 -m venv /opt/hexforge/venv",
      "sudo /opt/hexforge/venv/bin/pip install --upgrade pip",
      "sudo /opt/hexforge/venv/bin/pip install huggingface_hub requests gguf",
      
      "echo 'Installing QAIRT Python API...'",
      "sudo /opt/hexforge/venv/bin/pip install $QNN_DIR/lib/python/*.whl",

      "echo 'Injecting global QNN environment variables into /etc/environment...'",
      "echo \"QNN_SDK_ROOT=$QNN_DIR\" | sudo tee -a /etc/environment",
      "echo \"LD_LIBRARY_PATH=$QNN_DIR/lib/x86_64-linux-clang\" | sudo tee -a /etc/environment",
      "echo \"PATH=$QNN_DIR/bin/x86_64-linux-clang:$PATH\" | sudo tee -a /etc/environment",
      
      "echo 'Changing ownership to root for security...'",
      "sudo chown -R root:root /opt/hexforge",
      "sudo chmod -R 755 /opt/hexforge",
      
      "echo '--- [CI/CD Validation] Running Smoke Tests ---'",
      "echo 'Validating runsc installation...'",
      "sudo /usr/local/bin/runsc --version",
      "echo 'Validating python environment...'",
      "sudo /opt/hexforge/venv/bin/python3.10 -c 'import huggingface_hub; print(\"HF Hub installed successfully.\")'",
      
      "echo 'Base Image Bakery Complete.'"
    ]
  }
}
