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

variable "gcp_project_id" {
  description = "GCP project ID. Required when cloud_provider = 'gcp'. Used to construct GCS service account (ragflow-gcs@{gcp_project_id}.iam.gserviceaccount.com)"
  type        = string
  default     = ""
}

variable "storage_class" {
  description = "Kubernetes StorageClass for PVCs. Overrides cloud_provider auto-detection if specified"
  type        = string
  default     = ""  # Empty means use cloud_provider defaults
}

variable "gateway_class_name" {
  description = "GatewayClass name for routing (use 'gke-l7-regional-external-managed' for GKE)"
  type        = string
  default     = "nginx"
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
  default     = ""  # Empty for cloud provider auto-detection
}

variable "s3_bucket" {
  description = "S3 bucket name"
  type        = string
  default     = "ragflow"
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
# Gateway Configuration
# =============================================================================

variable "enable_tls" {
  description = "Enable TLS for gateway"
  type        = bool
  default     = false
}

# =============================================================================
# MySQL Configuration
# =============================================================================

variable "mysql_deployment_mode" {
  description = "MySQL deployment mode: k8s (internal) or cloud (external)"
  type        = string
  default     = "k8s"

  validation {
    condition     = contains(["k8s", "cloud"], var.mysql_deployment_mode)
    error_message = "mysql_deployment_mode must be 'k8s' or 'cloud'."
  }
}

variable "mysql_db_name" {
  description = "MySQL database name for RAGFlow application"
  type        = string
  default     = "rag_flow"
}

variable "mysql_k8s_storage" {
  description = "MySQL storage size in GB"
  type        = number
  default     = 200
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
  default     = "8Gi"
}

variable "mysql_memory_limit" {
  description = "MySQL memory limit"
  type        = string
  default     = "16Gi"
}

variable "mysql_max_connections" {
  description = "MySQL maximum number of connections"
  type        = number
  default     = 2000
}

# =============================================================================
# Elasticsearch Configuration
# =============================================================================

variable "es_deployment_mode" {
  description = "Elasticsearch deployment mode: k8s (internal) or cloud (external)"
  type        = string
  default     = "k8s"

  validation {
    condition     = contains(["k8s", "cloud"], var.es_deployment_mode)
    error_message = "es_deployment_mode must be 'k8s' or 'cloud'."
  }
}

variable "es_image" {
  description = "Elasticsearch container image (including tag)"
  type        = string
  default     = "elasticsearch:9.3.1"
}

variable "es_k8s_node_count" {
  description = "Number of Elasticsearch nodes"
  type        = number
  default     = 3
}

variable "es_k8s_storage" {
  description = "Elasticsearch storage size per node in GB"
  type        = number
  default     = 500
}

variable "es_cpu_request" {
  description = "Elasticsearch CPU request"
  type        = string
  default     = "4"
}

variable "es_cpu_limit" {
  description = "Elasticsearch CPU limit"
  type        = string
  default     = "8"
}

variable "es_memory_request" {
  description = "Elasticsearch memory request"
  type        = string
  default     = "32Gi"
}

variable "es_memory_limit" {
  description = "Elasticsearch memory limit"
  type        = string
  default     = "32Gi"
}

variable "es_heap_size" {
  description = "Elasticsearch JVM heap size (should be ~50% of memory limit)"
  type        = string
  default     = "16g"
}

# =============================================================================
# TEI (Text Embeddings) Configuration
# =============================================================================

variable "tei_image" {
  description = "TEI container image"
  type        = string
  default     = "infiniflow/text-embeddings-inference:cpu-1.8"
}

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

variable "redis_image" {
  description = "Redis container image"
  type        = string
  default     = "valkey/valkey:8"
}


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

variable "curl_image" {
  description = "Curl image for init containers"
  type        = string
  default     = "curlimages/curl:latest"
}

# =============================================================================
# RabbitMQ Configuration
# =============================================================================

variable "rabbitmq_image" {
  description = "RabbitMQ container image"
  type        = string
  default     = "rabbitmq:4-management"
}

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
  default     = "ragflow:latest"
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
  default     = "8Gi"
}

variable "ragflow_memory_limit" {
  description = "RAGFlow memory limit"
  type        = string
  default     = "16Gi"
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
  default     = "8Gi"
}

variable "parser_memory_limit" {
  description = "Parser memory limit"
  type        = string
  default     = "16Gi"
}

# =============================================================================
# DeepDoc Configuration
# =============================================================================

variable "deepdoc_image" {
  description = "DeepDoc container image (including tag, will be prefixed with private_registry)"
  type        = string
  default     = "deepdoc_cpu:latest"
}

variable "deepdoc_replicas" {
  description = "Number of DeepDoc replicas"
  type        = number
  default     = 1
}

variable "deepdoc_cpu_request" {
  description = "DeepDoc CPU request"
  type        = string
  default     = "8"
}

variable "deepdoc_cpu_limit" {
  description = "DeepDoc CPU limit"
  type        = string
  default     = "16"
}

variable "deepdoc_memory_request" {
  description = "DeepDoc memory request"
  type        = string
  default     = "32Gi"
}

variable "deepdoc_memory_limit" {
  description = "DeepDoc memory limit"
  type        = string
  default     = "64Gi"
}

variable "deepdoc_use_gpu" {
  description = "Enable GPU for DeepDoc (requires GPU nodes and NVIDIA runtime)"
  type        = bool
  default     = false
}
