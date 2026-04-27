# =============================================================================
# RAGFlow On-Premises Deployment - Variables
# =============================================================================
# Variables for deploying RAGFlow on existing Kubernetes clusters
#
# Usage:
#   1. Copy terraform.tfvars.example to terraform.tfvars
#   2. Edit terraform.tfvars with your custom values
#   3. Run: tofu init && tofu apply
# =============================================================================

# =============================================================================
# General Configuration
# =============================================================================

variable "kubeconfig_path" {
  type        = string
  default     = "~/.kube/config"
  description = "Path to kubeconfig file"
}

variable "namespace" {
  description = "Kubernetes namespace for RAGFlow deployment"
  type        = string
  default     = "ragflow"
}

# =============================================================================
# Kubernetes Configuration
# =============================================================================

variable "cloud_provider" {
  description = "Cloud provider for auto-config detection: smk, gcp (GKE), aws (EKS), azure (AKS), alicloud (ACK)"
  type        = string
  default     = "smk"

  validation {
    condition     = contains(["smk", "gcp", "aws", "azure", "alicloud"], var.cloud_provider)
    error_message = "cloud_provider must be one of: 'smk', 'gcp', 'aws', 'azure', or 'alicloud'."
  }
}

variable "cluster_scoped_resource_mode" {
  description = "Cluster-scoped ownership mode. 'auto' resolves ownership from BYOK state + cluster detection. 'manual' uses manage_cluster_scoped_resources."
  type        = string
  default     = "auto"

  validation {
    condition     = contains(["auto", "manual"], var.cluster_scoped_resource_mode)
    error_message = "cluster_scoped_resource_mode must be 'auto' or 'manual'."
  }
}

variable "manage_cluster_scoped_resources" {
  description = "Manual ownership flag for cluster-scoped shared resources (ECK operator and GKE ComputeClass). Used when cluster_scoped_resource_mode = 'manual'."
  type        = bool
  default     = true
}

variable "gcp_project_id" {
  description = "GCP project ID. Required when cloud_provider = 'gcp'. Used to construct GCS service account (ragflow-gcs@{gcp_project_id}.iam.gserviceaccount.com)"
  type        = string
  default     = ""
}

# =============================================================================
# Private Registry Configuration (Optional)
# =============================================================================

variable "private_registry" {
  description = "Private container registry URL for RAGFlow and DeepDoc images (e.g., 'gcr.io/ragflow-462809' or 'infiniflow-registry.cn-shanghai.cr.aliyuncs.com')"
  type        = string
  default     = ""
}

variable "public_registry" {
  description = "Public container registry URL for third-party images (MySQL, Redis, TEI, RabbitMQ, etc.). If empty, uses default registries (docker.io, quay.io, etc.)"
  type        = string
  default     = ""
}

# =============================================================================
# S3-Compatible Storage Configuration
# =============================================================================
# Supports S3-compatible storage across different cloud providers:
# - SMK: MinIO, Rook-Ceph RGW, or any S3-compatible storage
# - GCP: Cloud Storage (via S3-compatible API or MinIO gateway)
# - AWS: S3
# - Azure: Blob Storage (via S3-compatible gateway or MinIO)
# - AliCloud: OSS (S3-compatible API)

variable "s3_endpoint" {
  description = "S3-compatible endpoint URL. Leave empty to use cloud provider defaults (GCP/AWS/Azure)"
  type        = string
  default     = "" # Empty for cloud provider auto-detection
}

variable "s3_bucket" {
  description = "S3 bucket name. When empty, defaults to the RagFlow instance namespace."
  type        = string
  default     = ""
}

variable "s3_access_key" {
  description = "S3 access key. Required for smk, optional for cloud providers with workload identity"
  type        = string
  sensitive   = true
  default     = ""
}

variable "s3_secret_key" {
  description = "S3 secret key. Required for smk, optional for cloud providers with workload identity"
  type        = string
  sensitive   = true
  default     = ""
}

variable "s3_region" {
  description = "S3 region (e.g., us-central1 for GCP, us-east-1 for AWS)"
  type        = string
  default     = ""
}

variable "storage_account_name" {
  description = "Azure storage account name (required only for Azure cloud provider)"
  type        = string
  default     = ""
}

variable "region" {
  description = "Cloud region for Aliyun (e.g., cn-hangzhou, cn-shanghai)"
  type        = string
  default     = "cn-shanghai"
}


# =============================================================================
# MySQL Configuration
# =============================================================================

variable "mysql_db_name" {
  description = "MySQL database name for RAGFlow application"
  type        = string
  default     = "rag_flow"
}

variable "mysql_k8s_storage" {
  description = "MySQL storage size in GB"
  type        = number
  default     = 500
}

variable "mysql_cpu_request" {
  description = "MySQL CPU request"
  type        = string
  default     = "4"
}

variable "mysql_cpu_limit" {
  description = "MySQL CPU limit"
  type        = string
  default     = "8"
}

variable "mysql_memory_request" {
  description = "MySQL memory request"
  type        = string
  default     = "16Gi"
}

variable "mysql_memory_limit" {
  description = "MySQL memory limit"
  type        = string
  default     = "20Gi"
}

variable "mysql_max_connections" {
  description = "MySQL maximum number of connections"
  type        = number
  default     = 2000
}

variable "mysql_node_selector" {
  description = "MySQL node selector for scheduling pods on specific nodes (e.g., for GKE Autopilot)"
  type        = map(string)
  default     = {}
}

# =============================================================================
# Elasticsearch Configuration
# =============================================================================

variable "es_image" {
  description = "Elasticsearch container image (including tag)"
  type        = string
  default     = "elasticsearch:9.3.2"
}

# Master node configuration (cluster management only)
variable "es_master_node_count" {
  description = "Number of Elasticsearch master nodes"
  type        = number
  default     = 3
}

variable "es_master_cpu_request" {
  description = "Elasticsearch master node CPU request"
  type        = string
  default     = "2"
}

variable "es_master_cpu_limit" {
  description = "Elasticsearch master node CPU limit"
  type        = string
  default     = "4"
}

variable "es_master_memory_request" {
  description = "Elasticsearch master node memory request"
  type        = string
  default     = "8Gi"
}

variable "es_master_memory_limit" {
  description = "Elasticsearch master node memory limit"
  type        = string
  default     = "8Gi"
}

variable "es_master_heap_size" {
  description = "Elasticsearch master node JVM heap size (should be ~50% of memory limit)"
  type        = string
  default     = "4g"
}

# Data/Ingest node configuration (data storage and ingest pipelines)
variable "es_data_node_count" {
  description = "Number of Elasticsearch data/ingest nodes"
  type        = number
  default     = 4
}

variable "es_data_cpu_request" {
  description = "Elasticsearch data/ingest node CPU request"
  type        = string
  default     = "4"
}

variable "es_data_cpu_limit" {
  description = "Elasticsearch data/ingest node CPU limit"
  type        = string
  default     = "8"
}

variable "es_data_memory_request" {
  description = "Elasticsearch data/ingest node memory request"
  type        = string
  default     = "32Gi"
}

variable "es_data_memory_limit" {
  description = "Elasticsearch data/ingest node memory limit"
  type        = string
  default     = "32Gi"
}

variable "es_data_heap_size" {
  description = "Elasticsearch data/ingest node JVM heap size (should be ~50% of memory limit)"
  type        = string
  default     = "16g"
}

variable "es_data_storage" {
  description = "Elasticsearch data/ingest node storage size per node in GB"
  type        = number
  default     = 500
}

# =============================================================================
# TEI (Text Embeddings) Configuration
# =============================================================================

variable "tei_model" {
  description = "TEI model to use"
  type        = string
  default     = "BAAI/bge-small-en-v1.5"
}

variable "tei_replicas" {
  description = "Number of TEI replicas"
  type        = number
  default     = 0
}

variable "tei_cpu_request" {
  description = "TEI CPU request"
  type        = string
  default     = "4"
}

variable "tei_cpu_limit" {
  description = "TEI CPU limit"
  type        = string
  default     = "8"
}

variable "tei_memory_request" {
  description = "TEI memory request"
  type        = string
  default     = "8Gi"
}

variable "tei_memory_limit" {
  description = "TEI memory limit"
  type        = string
  default     = "16Gi"
}

# =============================================================================
# Redis Configuration
# =============================================================================


variable "redis_cpu_request" {
  description = "Redis CPU request"
  type        = string
  default     = "2"
}

variable "redis_cpu_limit" {
  description = "Redis CPU limit"
  type        = string
  default     = "4"
}

variable "redis_memory_request" {
  description = "Redis memory request"
  type        = string
  default     = "4Gi"
}

variable "redis_memory_limit" {
  description = "Redis memory limit"
  type        = string
  default     = "8Gi"
}

# =============================================================================
# RabbitMQ Configuration
# =============================================================================

variable "rabbitmq_storage" {
  description = "RabbitMQ storage size in GB"
  type        = number
  default     = 20
}

variable "rabbitmq_cpu_request" {
  description = "RabbitMQ CPU request"
  type        = string
  default     = "1"
}

variable "rabbitmq_cpu_limit" {
  description = "RabbitMQ CPU limit"
  type        = string
  default     = "2"
}

variable "rabbitmq_memory_request" {
  description = "RabbitMQ memory request"
  type        = string
  default     = "2Gi"
}

variable "rabbitmq_memory_limit" {
  description = "RabbitMQ memory limit"
  type        = string
  default     = "4Gi"
}


# =============================================================================
# RAGFlow Application Configuration
# =============================================================================

variable "ragflow_image" {
  description = "RAGFlow container image (including tag, will be prefixed with private_registry)"
  type        = string
  default     = "ragflow:v0.24.0-5-mt"
}

variable "ragflow_replicas" {
  description = "Number of RAGFlow replicas"
  type        = number
  default     = 3
}

variable "ragflow_cpu_request" {
  description = "RAGFlow CPU request (cores)"
  type        = string
  default     = "2"
}

variable "ragflow_cpu_limit" {
  description = "RAGFlow CPU limit (cores)"
  type        = string
  default     = "4"
}

variable "ragflow_memory_request" {
  description = "RAGFlow memory request"
  type        = string
  default     = "4Gi"
}

variable "ragflow_memory_limit" {
  description = "RAGFlow memory limit"
  type        = string
  default     = "8Gi"
}

# =============================================================================
# Parser Configuration
# =============================================================================

variable "parser_replicas" {
  description = "Number of Parser replicas"
  type        = number
  default     = 3
}

variable "parser_cpu_request" {
  description = "Parser CPU request"
  type        = string
  default     = "2"
}

variable "parser_cpu_limit" {
  description = "Parser CPU limit"
  type        = string
  default     = "4"
}

variable "parser_memory_request" {
  description = "Parser memory request"
  type        = string
  default     = "16Gi"
}

variable "parser_memory_limit" {
  description = "Parser memory limit"
  type        = string
  default     = "20Gi"
}

variable "parser_ws_workers" {
  description = "WS_WORKERS value for parser task executors"
  type        = string
  default     = "1"
}

# =============================================================================
# DeepDoc Configuration
# =============================================================================

variable "deepdoc_image" {
  description = "DeepDoc container image (including tag, will be prefixed with private_registry)"
  type        = string
  default     = "deepdoc_cpu:v0.24.0-5-mt"
}

variable "deepdoc_replicas" {
  description = "Number of DeepDoc replicas"
  type        = number
  default     = 2
}

variable "deepdoc_cpu_request" {
  description = "DeepDoc CPU request"
  type        = string
  default     = "4"
}

variable "deepdoc_cpu_limit" {
  description = "DeepDoc CPU limit"
  type        = string
  default     = "8"
}

variable "deepdoc_memory_request" {
  description = "DeepDoc memory request"
  type        = string
  default     = "16Gi"
}

variable "deepdoc_memory_limit" {
  description = "DeepDoc memory limit"
  type        = string
  default     = "32Gi"
}

variable "deepdoc_use_gpu" {
  description = "Enable GPU for DeepDoc (requires GPU nodes and NVIDIA runtime)"
  type        = bool
  default     = false
}

# =============================================================================
# ohttps Configuration (for SSL certificate sync)
# =============================================================================

variable "ohttps_enabled" {
  description = "Enable ohttps certificate sync. When enabled, HTTPS listener and CronJob will be created."
  type        = bool
  default     = false
}

variable "ohttps_api_id" {
  description = "ohttps API ID for certificate sync"
  type        = string
  default     = ""
}

variable "ohttps_api_key" {
  description = "ohttps API Key for certificate sync"
  type        = string
  default     = ""
  sensitive   = true
}

variable "ohttps_cert_id" {
  description = "ohttps Certificate ID to sync"
  type        = string
  default     = ""
}

variable "ohttps_sync_image" {
  description = "Docker image for ohttps certificate sync CronJob"
  type        = string
  default     = "infiniflow/sync_ohttps_cert:latest"
}

# =============================================================================
# Infinity Configuration (Shadow Database / Alternative Doc Engine)
# =============================================================================

variable "infinity_enabled" {
  description = "Enable Infinity as a shadow database alongside Elasticsearch. When enabled, DOC_ENGINE will be set to 'elasticsearch,infinity' for shadow write proxy comparison."
  type        = bool
  default     = false
}

variable "infinity_image" {
  description = "Infinity container image (including tag)"
  type        = string
  default     = "infiniflow/infinity:v0.7.0-dev2"
}

variable "infinity_replicas" {
  description = "Number of Infinity replicas"
  type        = number
  default     = 1
}

variable "infinity_cpu_request" {
  description = "Infinity CPU request"
  type        = string
  default     = "2"
}

variable "infinity_cpu_limit" {
  description = "Infinity CPU limit"
  type        = string
  default     = "8"
}

variable "infinity_memory_request" {
  description = "Infinity memory request"
  type        = string
  default     = "4Gi"
}

variable "infinity_memory_limit" {
  description = "Infinity memory limit"
  type        = string
  default     = "16Gi"
}

variable "infinity_storage" {
  description = "Infinity storage size in GB"
  type        = number
  default     = 500
}

# =============================================================================
# Billing Configuration
# =============================================================================

variable "billing_enabled" {
  description = "Enable billing feature"
  type        = bool
  default     = false
}

variable "billing_stripe_api_key" {
  description = "Stripe API key. Set via environment variable: export TF_VAR_billing_stripe_api_key='sk_live_xxx'"
  type        = string
  sensitive   = true
  default     = ""
  validation {
    condition     = var.billing_enabled == false || substr(var.billing_stripe_api_key, 0, 3) == "sk_"
    error_message = "billing_stripe_api_key must start with 'sk_' (Stripe secret key prefix)."
  }
}


variable "billing_stripe_api_version" {
  description = "Stripe API version"
  type        = string
  default     = "2026-02-25.clover"
}

variable "billing_service_url" {
  description = "Billing service base URL for Stripe callbacks and redirects. Must be a publicly accessible URL (e.g., https://billing.example.com). Cannot be localhost or 127.0.0.1."
  type        = string
  default     = "http://127.0.0.1:9380"
  validation {
    condition     = var.billing_enabled == false || (var.billing_service_url != "" && !can(regex("localhost|127\\.0\\.0\\.1", var.billing_service_url)))
    error_message = "billing_service_url must be a valid publicly accessible URL and cannot be localhost or 127.0.0.1."
  }
}

variable "billing_price_id_points" {
  description = "Stripe price ID for points recharge"
  type        = string
  default     = ""
  validation {
    condition     = var.billing_enabled == false || var.billing_price_id_points != ""
    error_message = "billing_price_id_points must not be empty when billing is enabled."
  }
}

variable "billing_price_id_storage" {
  description = "Stripe price ID for storage subscription"
  type        = string
  default     = ""
  validation {
    condition     = var.billing_enabled == false || var.billing_price_id_storage != ""
    error_message = "billing_price_id_storage must not be empty when billing is enabled."
  }
}

variable "billing_price_id_trial" {
  description = "Stripe price ID for Trial subscription plan"
  type        = string
  default     = ""
  validation {
    condition     = var.billing_enabled == false || var.billing_price_id_trial != ""
    error_message = "billing_price_id_trial must not be empty when billing is enabled."
  }
}

variable "billing_price_id_starter" {
  description = "Stripe price ID for Starter subscription plan"
  type        = string
  default     = ""
  validation {
    condition     = var.billing_enabled == false || var.billing_price_id_starter != ""
    error_message = "billing_price_id_starter must not be empty when billing is enabled."
  }
}

variable "billing_price_id_pro" {
  description = "Stripe price ID for Pro subscription plan"
  type        = string
  default     = ""
  validation {
    condition     = var.billing_enabled == false || var.billing_price_id_pro != ""
    error_message = "billing_price_id_pro must not be empty when billing is enabled."
  }
}

variable "stripe_test_clock_id" {
  description = "Stripe test clock ID"
  type        = string
  default     = ""
}

