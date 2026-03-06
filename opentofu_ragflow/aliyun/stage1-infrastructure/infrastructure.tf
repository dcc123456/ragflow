# =============================================================================
# RAGFlow Aliyun Cloud Deployment - Phase 1: Cloud Infrastructure
# =============================================================================
# This file creates cloud resources on Aliyun and outputs kubeconfig
#
# Prerequisites:
# - Aliyun account with access key configured
#
# Usage:
#   terraform init
#   terraform plan
#   terraform apply
#   terraform output -raw kubeconfig > kubeconfig
# =============================================================================
terraform {
  required_version = ">= 1.8.0"

  required_providers {
    alicloud = {
      source  = "aliyun/alicloud"
      version = "1.254.0"
    }

    random = {
      source  = "hashicorp/random"
      version = "3.6.3"
    }
  }
}

# =============================================================================
# Provider Configuration
# =============================================================================

provider "alicloud" {
  region = var.region
}

provider "random" {}

# =============================================================================
# Random Deployment ID
# =============================================================================
# Generates a stable random suffix for unique resource naming
# This value is stored in state and persists across runs
resource "random_string" "deployment_id" {
  length  = 6
  special = false
  upper   = false
  numeric = true

  lifecycle {
    # Prevent accidental recreation of this resource
    ignore_changes = all
  }
}

# =============================================================================
# Variables are defined in variables.tf (shared between stages)
# =============================================================================
locals {
  name_prefix = "ragflow-${random_string.deployment_id.result}"

  # Determine if we need to create VPC/VSwitch based on vpc_option
  aliyun_create_vpc     = var.vpc_option == "new"
  aliyun_create_vswitch = var.vpc_option == "new"

  # Payment type mapping
  mysql_payment_type = var.payment_type == "PostPaid" ? "Postpaid" : "Prepaid"
  es_payment_type    = var.payment_type # ES uses PrePaid/PostPaid directly

  # Network (computed after VPC/VSwitch resources)
  vpc_id      = local.aliyun_create_vpc ? alicloud_vpc.main[0].id : var.vpc_id
  vswitch_ids = length(var.vswitch_ids) > 0 ? var.vswitch_ids : alicloud_vswitch.main[*].id

  # Storage (computed after OSS bucket resource)
  bucket_name = var.existing_bucket_name != "" ? var.existing_bucket_name : alicloud_oss_bucket.ragflow[0].bucket

  # MySQL (computed after RDS resource)
  mysql_host           = var.mysql_deployment_mode == "cloud" ? alicloud_db_instance.mysql[0].connection_string : ""
  mysql_password_value = var.mysql_deployment_mode == "cloud" ? (var.mysql_password != "" ? var.mysql_password : random_password.mysql[0].result) : ""

  # Elasticsearch (computed after ES resource)
  es_endpoint       = var.es_deployment_mode == "cloud" ? alicloud_elasticsearch_instance.elasticsearch[0].domain : ""
  es_password_value = var.es_deployment_mode == "cloud" ? (var.es_password != "" ? var.es_password : random_password.elasticsearch[0].result) : ""
}

resource "alicloud_vpc" "main" {
  count = local.aliyun_create_vpc ? 1 : 0

  vpc_name   = "${local.name_prefix}-vpc"
  cidr_block = var.vpc_cidr

  tags = merge(
    var.tags,
    {
      Name = "${local.name_prefix}-vpc"
    }
  )
}

# See Aliyun ROS documentation: https://help.aliyun.com/zh/ros/developer-reference/aliyun-ecs-vswitch
resource "alicloud_vswitch" "main" {
  count = local.aliyun_create_vswitch ? length(var.vswitch_cidrs) : 0

  vpc_id     = local.aliyun_create_vpc ? alicloud_vpc.main[0].id : var.vpc_id
  cidr_block = var.vswitch_cidrs[count.index]
  zone_id    = count.index == 0 ? var.zone_id : (var.zone_id_2 != "" ? var.zone_id_2 : var.zone_id)

  tags = merge(
    var.tags,
    {
      Name = "${local.name_prefix}-vswitch-${count.index + 1}"
    }
  )
}

# =============================================================================
# OSS Bucket for Storage
# =============================================================================

resource "random_integer" "bucket_suffix" {
  count = var.existing_bucket_name == "" ? 1 : 0

  min = 10000
  max = 99999
}

# See Aliyun ROS documentation: https://help.aliyun.com/zh/ros/developer-reference/aliyun-oss-bucket
resource "alicloud_oss_bucket" "ragflow" {
  count = var.existing_bucket_name == "" ? 1 : 0

  bucket = "${local.name_prefix}-storage-${random_integer.bucket_suffix[0].result}"

  versioning {
    status = "Enabled"
  }

  lifecycle_rule {
    id      = "delete-old-versions"
    enabled = true

    noncurrent_version_expiration {
      days = 30
    }
  }

  tags = merge(
    var.tags,
    {
      Name = "${local.name_prefix}-storage"
    }
  )
}

# =============================================================================
# RAM User for K8s Access to OSS
# =============================================================================

resource "alicloud_ram_user" "ragflow" {
  name         = "${local.name_prefix}-ram-user"
  display_name = "RAGFlow Service Account"

  force = true
}

resource "alicloud_ram_access_key" "ragflow" {
  user_name = alicloud_ram_user.ragflow.name
}

resource "alicloud_ram_user_policy_attachment" "ragflow_oss_full" {
  policy_name = "AliyunOSSFullAccess"
  policy_type = "System"
  user_name   = alicloud_ram_user.ragflow.name
}

# =============================================================================
# MySQL RDS (Cloud Mode)
# =============================================================================

# Aliyun MySQL password requirements: 8-32 chars, must contain 3 of: uppercase, lowercase, numbers, special chars
# Allowed special chars: !@#$%&*()_+-=
resource "random_password" "mysql" {
  count            = var.mysql_deployment_mode == "cloud" && var.mysql_password == "" ? 1 : 0
  length           = 20
  min_upper        = 2
  min_lower        = 2
  min_numeric      = 2
  min_special      = 2
  override_special = "!@#$%&*()_+-="
}

# See Aliyun ROS documentation: https://help.aliyun.com/zh/ros/developer-reference/aliyun-rds-dbinstance
# High Availability RDS MySQL instance with multi-zone deployment
resource "alicloud_db_instance" "mysql" {
  count = var.mysql_deployment_mode == "cloud" ? 1 : 0

  engine                   = "MySQL"
  engine_version           = "8.0"
  category                 = "HighAvailability"
  instance_type            = var.mysql_instance_class
  instance_storage         = var.mysql_storage
  instance_charge_type     = local.mysql_payment_type
  db_instance_storage_type = "cloud_essd"

  # Multi-zone HA deployment: use comma-separated vswitch IDs for primary and slave zones
  # If zone_id_2 is specified, deploy across zones for high availability
  vswitch_id      = length(local.vswitch_ids) >= 2 && var.zone_id_2 != "" ? join(",", slice(local.vswitch_ids, 0, 2)) : local.vswitch_ids[0]
  zone_id         = var.zone_id
  zone_id_slave_a = var.zone_id_2 != "" ? var.zone_id_2 : var.zone_id

  # Security group whitelist for RDS access
  # Include VPC CIDR, Pod CIDR, and Service CIDR to allow K8s cluster access
  security_ips = [
    var.vpc_cidr,        # VPC CIDR (10.0.0.0/16) - allows communication within VPC
    var.pod_cidr,        # K8s Pod CIDR (10.16.0.0/16) - allows pod-to-RDS connections
    var.service_cidr,      # K8s Service CIDR (10.17.0.0/16) - allows service-to-RDS connections
  ]

  instance_name = "${local.name_prefix}-mysql"

  tags = merge(
    var.tags,
    {
      Name = "${local.name_prefix}-mysql"
    }
  )

  period = var.payment_type == "PrePaid" ? var.payment_period : null

  lifecycle {
    ignore_changes = [security_ips]
  }
}

resource "alicloud_db_database" "ragflow" {
  count = var.mysql_deployment_mode == "cloud" ? 1 : 0

  instance_id   = alicloud_db_instance.mysql[0].id
  name          = "rag_flow"
  character_set = "utf8mb4"
  description   = "RAGFlow database"
}

resource "alicloud_db_account" "ragflow" {
  count = var.mysql_deployment_mode == "cloud" ? 1 : 0

  db_instance_id      = alicloud_db_instance.mysql[0].id
  account_name        = "ragflow"
  account_password    = var.mysql_password != "" ? var.mysql_password : random_password.mysql[0].result
  account_description = "RAGFlow database account"
}

# =============================================================================
# Elasticsearch (Cloud Mode)
# =============================================================================

# Aliyun Elasticsearch password requirements: 8-32 chars, must contain 3 of: uppercase, lowercase, numbers, special chars
# Allowed special chars: !@#$%&*()_+-=
resource "random_password" "elasticsearch" {
  count            = var.es_deployment_mode == "cloud" && var.es_password == "" ? 1 : 0
  length           = 20
  min_upper        = 2
  min_lower        = 2
  min_numeric      = 2
  min_special      = 2
  override_special = "!@#$%&*()_+-="
}

# See Aliyun ROS documentation: https://help.aliyun.com/zh/ros/developer-reference/aliyun-elasticsearch-instance
resource "alicloud_elasticsearch_instance" "elasticsearch" {
  count = var.es_deployment_mode == "cloud" ? 1 : 0

  description          = "${local.name_prefix}-elasticsearch-vector-enhanced"
  vswitch_id           = local.vswitch_ids[0]
  version              = var.es_version
  instance_charge_type = local.es_payment_type

  # Single-zone deployment (avoid multi-zone master node requirement issues)
  # For multi-zone HA: use zone_count=2/3 and enable master_node_spec
  # zone_count = 1

  # Data Node Configuration (Memory-optimized for vector search)
  data_node_amount         = var.es_node_count
  data_node_spec           = var.es_node_spec
  data_node_disk_size      = var.es_disk_size
  data_node_disk_type      = var.es_disk_type
  data_node_disk_encrypted = false  # Disable encryption to avoid compatibility issues

  # Set ESSD performance level if using ESSD
  data_node_disk_performance_level = var.es_disk_type == "cloud_essd" ? var.es_disk_performance_level : null

  # Kibana Node (required for ES 7.x/8.x)
  # Note: Using sn1ne.large for better compatibility with 8.17_with_X-Pack vector enhanced
  kibana_node_spec = "elasticsearch.sn1ne.large"

  # Security whitelist for ES access
  # Include VPC CIDR, Pod CIDR, and Service CIDR to allow K8s cluster access
  private_whitelist = [
    var.vpc_cidr,      # VPC CIDR (10.0.0.0/16)
    var.pod_cidr,      # K8s Pod CIDR (10.16.0.0/16)
    var.service_cidr,   # K8s Service CIDR (10.17.0.0/16)
  ]

  password = var.es_password != "" ? var.es_password : random_password.elasticsearch[0].result

  tags = merge(
    var.tags,
    {
      Name    = "${local.name_prefix}-elasticsearch"
      Purpose = "vector-search"
    }
  )

  period = var.payment_type == "PrePaid" ? var.payment_period : null

  # Ignore auto-generated attributes that cause drift
  lifecycle {
    ignore_changes = [
      private_whitelist, # Temporarily removed to update whitelist
    ]
  }
}

# =============================================================================
# Kubernetes Cluster (ASK)
# =============================================================================
# See Aliyun ROS documentation: https://help.aliyun.com/zh/ros/developer-reference/aliyun-cs-serverlesskubernetescluster

resource "alicloud_cs_serverless_kubernetes" "main" {
  name        = "${local.name_prefix}-ask"
  vpc_id      = local.vpc_id
  vswitch_ids = local.vswitch_ids

  service_cidr = var.service_cidr

  # Enable public API endpoint access
  # This is required for Terraform to manage the cluster from outside the VPC
  endpoint_public_access_enabled = true

  tags = merge(
    var.tags,
    {
      Name = "${local.name_prefix}-ask"
    }
  )

  # Managed addons for ASK cluster
  # Reference: https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/user-guide/component-overview
  addons {
    name = "coredns"
  }

  addons {
    name = "alb-ingress-controller"
  }

  addons {
    name = "gateway-api"
  }

  addons {
    name = "csi-provisioner"
  }

  addons {
    name = "knative"
  }

  # ACR Credential Helper for private image pull without imagePullSecrets
  # See: https://help.aliyun.com/zh/ack/product-overview/aliyun-acr-credential-helper
  addons {
    name = "aliyun-acr-credential-helper"
  }
}

# =============================================================================
# Cluster Authentication and Connection
# =============================================================================

data "alicloud_cs_cluster_credential" "main" {
  cluster_id = alicloud_cs_serverless_kubernetes.main.id
  # Note: When endpoint_public_access_enabled is true on the cluster,
  # the kube_config will contain the public endpoint automatically
}

locals {
  kube_config_decoded = yamldecode(data.alicloud_cs_cluster_credential.main.kube_config)
  # Get the cluster endpoint from kube_config (public endpoint when endpoint_public_access_enabled=true)
  cluster_host = local.kube_config_decoded.clusters[0].cluster.server
  # ASK uses client certificate authentication (not token)
  # The kubeconfig contains base64-encoded client certificate and key
  client_certificate_data = local.kube_config_decoded.users[0].user["client-certificate-data"]
  client_key_data         = local.kube_config_decoded.users[0].user["client-key-data"]
}

output "kubeconfig" {
  description = "Kubernetes kubeconfig file. Export it: `terraform output -raw kubeconfig > ../kubeconfig`"
  value       = data.alicloud_cs_cluster_credential.main.kube_config
  sensitive   = true
}

output "cluster_id" {
  description = "Kubernetes cluster ID"
  value       = alicloud_cs_serverless_kubernetes.main.id
}

# =============================================================================
# S3 (OSS) Storage Access Information
# =============================================================================

output "s3_config" {
  description = "S3 (OSS) configuration for RAGFlow storage"
  value = {
    endpoint   = "https://oss-${var.region}.aliyuncs.com"
    bucket     = local.bucket_name
    access_key = alicloud_ram_access_key.ragflow.id
    secret_key = alicloud_ram_access_key.ragflow.secret
    region     = var.region
  }
  sensitive = true
}

# =============================================================================
# MySQL Access Information (Cloud Mode)
# =============================================================================

output "mysql_config" {
  description = "MySQL configuration (cloud mode only, empty for k8s mode)"
  value = var.mysql_deployment_mode == "cloud" ? {
    host     = local.mysql_host
    port     = 3306
    user     = "ragflow"
    database = "rag_flow"
    password = local.mysql_password_value
  } : null
  sensitive = true
}

# =============================================================================
# Elasticsearch Access Information (Cloud Mode)
# =============================================================================

output "es_config" {
  description = "Elasticsearch configuration (cloud mode only, empty for k8s mode)"
  value = var.es_deployment_mode == "cloud" ? {
    endpoint = local.es_endpoint
    port     = 9200
    user     = "elastic"
    password = local.es_password_value
    protocol = "http"
  } : null
  sensitive = true
}

# =============================================================================
# VPC Information
# =============================================================================

output "vpc_id" {
  description = "VPC ID"
  value       = local.vpc_id
}

output "vswitch_ids" {
  description = "VSwitch IDs for ALB zone mappings"
  value       = local.vswitch_ids
}