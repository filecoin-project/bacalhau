provider "google" {
  project = var.gcp_project
  region  = var.region
  zone    = var.zone
}

terraform {
  backend "gcs" {
    # this bucket lives in the bacalhau-cicd google project
    # https://console.cloud.google.com/storage/browser/bacalhau-global-storage;tab=objects?project=bacalhau-cicd
    bucket = "bacalhau-global-storage"
    prefix = "terraform/state"
  }
}

// A single Google Cloud Engine instance
resource "google_compute_instance" "bacalhau_vm" {
  name         = "bacalhau-vm-${terraform.workspace}-${count.index}"
  count        = var.instance_count
  machine_type = var.machine_type
  zone         = var.zone

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2204-lts"
      size  = var.boot_disk_size_gb
    }
  }

  metadata_startup_script = <<-EOF
#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

sudo mkdir -p /terraform_node

##############################
# export the terraform variables ready for scripts to use
# we write these to a file so the bacalhau startup script
# called by systemd can also source them
##############################

sudo tee /terraform_node/variables > /dev/null <<'EOI'
export TERRAFORM_NODE_INDEX="${count.index}"
export TERRAFORM_NODE_IP="${var.protect_resources ? google_compute_address.ipv4_address[count.index].address : google_compute_address.ipv4_address_unprotected[count.index].address}"
export TERRAFORM_NODE0_IP="${var.protect_resources ? google_compute_address.ipv4_address[0].address : google_compute_address.ipv4_address_unprotected[0].address}"
export IPFS_VERSION="${var.ipfs_version}"
export BACALHAU_ENVIRONMENT="${terraform.workspace}"
export BACALHAU_VERSION="${var.bacalhau_version}"
export BACALHAU_PORT="${var.bacalhau_port}"
export BACALHAU_UNSAFE_CLUSTER="${var.bacalhau_unsafe_cluster ? "yes" : ""}"
export BACALHAU_CONNECT_NODE0="${var.bacalhau_connect_node0}"
export BACALHAU_NODE0_UNSAFE_ID="QmUqesBmpC7pSzqH86ZmZghtWkLwL6RRop3M1SrNbQN5QD"
export SECRETS_HONEYCOMB_KEY="${var.honeycomb_api_key}"
EOI

##############################
# write the local files to the node filesystem
##############################

#########
# node scripts
#########

sudo mkdir -p /terraform_node

sudo tee /terraform_node/bacalhau-unsafe-private-key > /dev/null <<'EOI'
${var.bacalhau_unsafe_cluster ? file("${path.module}/remote_files/configs/unsafe-private-key") : ""}
EOI

sudo tee /terraform_node/install-node.sh > /dev/null <<'EOI'
${file("${path.module}/remote_files/scripts/install-node.sh")}
EOI

sudo tee /terraform_node/start-bacalhau.sh > /dev/null <<'EOI'
${file("${path.module}/remote_files/scripts/start-bacalhau.sh")}
EOI

#########
# health checker
#########

sudo mkdir -p /var/www/health_checker

# this will be copied to the correct location once openresty has installed to avoid
# an interactive prompt warning about the file existing blocking the headless install
sudo tee /terraform_node/nginx.conf > /dev/null <<'EOI'
${file("${path.module}/remote_files/health_checker/nginx.conf")}
EOI

sudo tee /var/www/health_checker/livez.sh > /dev/null <<'EOI'
${file("${path.module}/remote_files/health_checker/livez.sh")}
EOI

sudo tee /var/www/health_checker/healthz.sh > /dev/null <<'EOI'
${file("${path.module}/remote_files/health_checker/healthz.sh")}
EOI

sudo tee /var/www/health_checker/network_name.txt > /dev/null <<EOI
${var.auto_subnets ? google_compute_network.bacalhau_network[0].name : google_compute_network.bacalhau_network_manual[0].name}
EOI

sudo tee /var/www/health_checker/address.txt > /dev/null <<EOI
${var.protect_resources ? google_compute_address.ipv4_address[count.index].address : google_compute_address.ipv4_address_unprotected[count.index].address}
EOI

sudo chmod u+x /var/www/health_checker/*.sh

#########
# systemd units
#########

sudo tee /etc/systemd/system/ipfs-daemon.service > /dev/null <<'EOI'
${file("${path.module}/remote_files/configs/ipfs-daemon.service")}
EOI

sudo tee /etc/systemd/system/bacalhau-daemon.service > /dev/null <<'EOI'
${file("${path.module}/remote_files/configs/bacalhau-daemon.service")}
EOI

##############################
# run the install script
##############################

sudo bash /terraform_node/install-node.sh 2>&1 | tee -a /tmp/bacalhau.log
EOF
  network_interface {
    network    = var.auto_subnets ? google_compute_network.bacalhau_network[0].name : google_compute_network.bacalhau_network_manual[0].name
    subnetwork = var.auto_subnets ? "" : google_compute_subnetwork.bacalhau_subnetwork_manual[0].name
    access_config {
      nat_ip = var.protect_resources ? google_compute_address.ipv4_address[count.index].address : google_compute_address.ipv4_address_unprotected[count.index].address
    }
  }

  lifecycle {
    ignore_changes = [attached_disk]
  }
  #   service_account {
  #     scopes = ["cloud-platform"]
  #   }
  allow_stopping_for_update = true

}

resource "google_compute_address" "ipv4_address" {
  # keep the same ip addresses if we are production (because they are in DNS and the auto connect serve codebase)
  name  = terraform.workspace == "production" ? "bacalhau-ipv4-address-${count.index}" : "bacalhau-ipv4-address-${terraform.workspace}-${count.index}"
  count = var.protect_resources ? var.instance_count : 0
  lifecycle {
    prevent_destroy = true
  }
}

resource "google_compute_address" "ipv4_address_unprotected" {
  # keep the same ip addresses if we are production (because they are in DNS and the auto connect serve codebase)
  name  = terraform.workspace == "production" ? "bacalhau-ipv4-address-${count.index}" : "bacalhau-ipv4-address-${terraform.workspace}-${count.index}"
  count = var.protect_resources ? 0 : var.instance_count
}

output "public_ip_address" {
  value = google_compute_instance.bacalhau_vm.*.network_interface.0.access_config.0.nat_ip
}

resource "google_compute_disk" "bacalhau_disk" {
  # keep the same disk names if we are production because the libp2p ids are in the auto connect serve codebase
  name     = terraform.workspace == "production" ? "bacalhau-disk-${count.index}" : "bacalhau-disk-${terraform.workspace}-${count.index}"
  count    = var.protect_resources ? var.instance_count : 0
  type     = "pd-ssd"
  zone     = var.zone
  size     = var.volume_size_gb
  snapshot = var.restore_from_backup
  lifecycle {
    prevent_destroy = true
  }
}

resource "google_compute_disk" "bacalhau_disk_unprotected" {
  # keep the same disk names if we are production because the libp2p ids are in the auto connect serve codebase
  name     = terraform.workspace == "production" ? "bacalhau-disk-${count.index}" : "bacalhau-disk-${terraform.workspace}-${count.index}"
  count    = var.protect_resources ? 0 : var.instance_count
  type     = "pd-ssd"
  zone     = var.zone
  size     = var.volume_size_gb
  snapshot = var.restore_from_backup
}

resource "google_compute_disk_resource_policy_attachment" "attachment" {
  name  = google_compute_resource_policy.bacalhau_disk_backups.name
  disk  = var.protect_resources ? google_compute_disk.bacalhau_disk[count.index].name : google_compute_disk.bacalhau_disk_unprotected[count.index].name
  zone  = var.zone
  count = var.instance_count
}

resource "google_compute_resource_policy" "bacalhau_disk_backups" {
  name   = "bacalhau-disk-backups-${terraform.workspace}"
  region = var.region
  snapshot_schedule_policy {
    schedule {
      daily_schedule {
        days_in_cycle = 1
        start_time    = "23:00"
      }
    }
    retention_policy {
      max_retention_days    = 30
      on_source_disk_delete = "KEEP_AUTO_SNAPSHOTS"
    }
    snapshot_properties {
      labels = {
        bacalhau_backup = "true"
      }
      guest_flush = true
    }
  }
}

resource "google_compute_attached_disk" "default" {
  disk     = var.protect_resources ? google_compute_disk.bacalhau_disk[count.index].self_link : google_compute_disk.bacalhau_disk_unprotected[count.index].self_link
  instance = google_compute_instance.bacalhau_vm[count.index].self_link
  count    = var.instance_count
}

resource "google_compute_firewall" "bacalhau_firewall" {
  name    = "bacalhau-ingress-firewall-${terraform.workspace}"
  network = var.auto_subnets ? google_compute_network.bacalhau_network[0].name : google_compute_network.bacalhau_network_manual[0].name

  allow {
    protocol = "icmp"
  }

  allow {
    protocol = "tcp"
    ports = [
      "4001",  // ipfs swarm
      "1234",  // bacalhau API
      "1235",  // bacalhau swarm
      "2112",  // bacalhau metrics
      "44443", // nginx is healthy - for running health check scripts
      "44444", // nginx node health check scripts
    ]
  }

  source_ranges = var.ingress_cidrs
}

resource "google_compute_firewall" "bacalhau_ssh_firewall" {
  name    = "bacalhau-ssh-firewall-${terraform.workspace}"
  network = var.auto_subnets ? google_compute_network.bacalhau_network[0].name : google_compute_network.bacalhau_network_manual[0].name

  allow {
    protocol = "icmp"
  }

  allow {
    protocol = "tcp"
    // Port 22   - Provides ssh access to the bacalhau server, for debugging 
    ports = ["22"]
  }

  source_ranges = var.ssh_access_cidrs
}

resource "google_compute_network" "bacalhau_network" {
  name                    = "bacalhau-network-${terraform.workspace}"
  auto_create_subnetworks = true
  count         = var.auto_subnets ? 1 : 0
}

// these are used for short lived clusters where we only make
// 1 subnet otherwise we use up our quota for subnetworks
resource "google_compute_network" "bacalhau_network_manual" {
  name                    = "bacalhau-network-manual-${terraform.workspace}"
  auto_create_subnetworks = false
  count         = var.auto_subnets ? 0 : 1
}

resource "google_compute_subnetwork" "bacalhau_subnetwork_manual" {
  name          = "bacalhau-subnetwork-manual-${terraform.workspace}"
  ip_cidr_range = "192.168.0.0/16"
  region        = var.region
  network       = google_compute_network.bacalhau_network_manual[0].id
  count         = var.auto_subnets ? 0 : 1
}
