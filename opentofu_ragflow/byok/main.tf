# =============================================================================
# RAGFlow On-Premises Deployment on Kubernetes
# =============================================================================
# Single-file Terraform configuration for deploying RAGFlow on existing K8s clusters
#
# Prerequisites:
# - Existing Kubernetes cluster (v1.24+)
# - kubeconfig file configured to access the cluster
# - StorageClass available (e.g., rook-ceph-block, standard)
# - S3-compatible storage (MinIO, Rook-Ceph RGW, etc.)
# - Ingress controller (nginx-ingress, Gateway API, etc.)
#
# Usage:
#   terraform init
#   terraform plan
#   terraform apply
# =============================================================================

terraform {
  required_version = ">= 1.11.0"

  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "3.0.1"
    }

    helm = {
      source  = "hashicorp/helm"
      version = "3.1.1"
    }

    random = {
      source  = "hashicorp/random"
      version = "3.8.1"
    }

    external = {
      source  = "hashicorp/external"
      version = "2.3.5"
    }

    google = {
      source  = "hashicorp/google"
      version = "7.24"
    }
  }
}

# =============================================================================
# Provider Configuration
# =============================================================================

provider "kubernetes" {
  config_path = var.kubeconfig_path
}

provider "helm" {
  kubernetes = {
    config_path = var.kubeconfig_path
  }
}

provider "random" {}

provider "external" {}

provider "google" {
  project = var.gcp_project_id
  region  = var.region
}

# =============================================================================
# Local Values
# =============================================================================

locals {
  # Cloud provider auto-configuration
  # Detect StorageClass and S3 settings based on cloud_provider
  cloud_config = {
    smk = {
      storage_class = "rook-ceph-block"
      s3_endpoint   = var.s3_endpoint != "" ? var.s3_endpoint : "http://rook-ceph-rgw-my-store.rook-ceph.svc:80"
      s3_region     = var.s3_region != "" ? var.s3_region : "default"
    }
    gcp = {
      # GKE Autopilot requires premium-rwo for Elasticsearch mmap; standard-rwo is not supported
      storage_class = "premium-rwo"
      s3_endpoint   = var.s3_endpoint != "" ? var.s3_endpoint : "https://storage.googleapis.com"
      # Note: s3_region is not used by GCS client (rag/utils/gcs_conn.py), but kept for consistency
      s3_region = var.s3_region != "" ? var.s3_region : "us-central1"
    }
    aws = {
      storage_class = "gp3"
      s3_endpoint   = var.s3_endpoint != "" ? var.s3_endpoint : "https://s3.amazonaws.com"
      s3_region     = var.s3_region != "" ? var.s3_region : "us-east-1"
    }
    azure = {
      storage_class = "default"
      s3_endpoint   = var.s3_endpoint != "" ? var.s3_endpoint : "https://${var.storage_account_name}.blob.core.windows.net"
      s3_region     = var.s3_region != "" ? var.s3_region : "eastus"
    }
    alicloud = {
      storage_class = "alicloud-disk-ssd"
      s3_endpoint   = var.s3_endpoint != "" ? var.s3_endpoint : "https://oss-${var.region}.aliyuncs.com"
      s3_region     = var.s3_region != "" ? var.s3_region : "cn-hangzhou"
    }
  }

  # Get configuration for selected cloud provider
  config = local.cloud_config[var.cloud_provider]

  ragflow_namespace   = var.namespace
  s3_bucket_name      = var.s3_bucket != "" ? var.s3_bucket : local.ragflow_namespace
  namespace_sanitized = replace(lower(local.ragflow_namespace), "-", "_")

  # Image transformation logic
  # RAGFlow image (including tag, will be prefixed with private_registry)
  # Format: image:tag (e.g., ragflow:latest)
  ragflow_image_full = "${var.private_registry}/${var.ragflow_image}"

  # Deepdoc image selection based on deepdoc_use_gpu
  # When deepdoc_use_gpu=true: use deepdoc_gpu
  # When deepdoc_use_gpu=false: use deepdoc_cpu
  # Override with deepdoc_image variable (including tag)
  deepdoc_image_full = "${var.private_registry}/${var.deepdoc_image}"

  # Infrastructure images
  # When public_registry is empty: use original public registry (docker.io, quay.io, etc.)
  # When public_registry is set: use public_registry/image:tag
  mysql_image    = var.public_registry != "" ? "${var.public_registry}/mysql:8.0" : "docker.io/library/mysql:8.0"
  redis_image    = var.public_registry != "" ? "${var.public_registry}/valkey:8" : "valkey/valkey:8"
  tei_image      = var.public_registry != "" ? "${var.public_registry}/text-embeddings-inference:cpu-1.8" : "infiniflow/text-embeddings-inference:cpu-1.8"
  rabbitmq_image = var.public_registry != "" ? "${var.public_registry}/rabbitmq:4-management" : "rabbitmq:4-management"
  curl_image     = var.public_registry != "" ? "${var.public_registry}/curl:latest" : "curlimages/curl:latest"
  minio_mc_image = var.public_registry != "" ? "${var.public_registry}/mc:latest" : "quay.io/minio/mc:latest"
  infinity_image = var.public_registry != "" ? "${var.public_registry}/infinity:v0.7.0-dev5" : "infiniflow/infinity:v0.7.0-dev5"

  # Elasticsearch version extracted from es_image
  # Extracts version from format like "elasticsearch:9.3.1" -> "9.3.1"
  # If no tag is provided (image without :tag), defaults to "latest"
  es_version = can(regex(".*:(.+)$", var.es_image)) ? regex(".*:(.+)$", var.es_image)[0] : "latest"

  # GatewayClass name based on cloud provider
  # GCP uses GKE Gateway, other providers use NGINX Gateway
  gateway_class_name = var.cloud_provider == "gcp" ? "gke-l7-regional-external-managed" : "nginx"

  # Check if using GKE Gateway (vs smk with NGINX Gateway)
  is_gke_gateway = can(regex("^gke-", local.gateway_class_name))

  # Service credentials/hosts (allow in-namespace deploy or external/shared services)
  shared_infra_app_mode = var.deploy_app_stack && !var.deploy_infra

  mysql_user_default       = substr("rf_${local.namespace_sanitized}", 0, 32)
  mysql_user_effective     = !var.deploy_infra ? (var.mysql_user != "" ? var.mysql_user : local.mysql_user_default) : (var.mysql_user != "" ? var.mysql_user : "ragflow")
  mysql_password_effective = local.use_shared_mysql_autoprovision ? random_password.shared_mysql[0].result : (var.mysql_password != "" ? var.mysql_password : random_password.mysql.result)
  mysql_host_effective     = var.mysql_host != "" ? var.mysql_host : "mysql"
  mysql_db_name_default    = substr("rag_flow_${local.namespace_sanitized}", 0, 64)
  mysql_db_name_effective  = !var.deploy_infra ? ((var.mysql_db_name != "" && var.mysql_db_name != "rag_flow") ? var.mysql_db_name : local.mysql_db_name_default) : var.mysql_db_name

  use_shared_redis_secret  = var.deploy_app_stack && !var.deploy_infra && var.redis_password == ""
  redis_host_effective     = var.redis_host != "" ? var.redis_host : "redis"
  redis_password_effective = var.redis_password != "" ? var.redis_password : (var.deploy_infra ? random_password.redis[0].result : try(data.kubernetes_secret_v1.shared_redis_password[0].data.password, ""))
  redis_username_effective = var.redis_username

  rabbitmq_user_effective     = var.rabbitmq_user != "" ? var.rabbitmq_user : "ragflow"
  rabbitmq_password_effective = var.rabbitmq_password != "" ? var.rabbitmq_password : (var.deploy_app_stack ? random_password.rabbitmq[0].result : "")
  rabbitmq_host_effective     = var.rabbitmq_host != "" ? var.rabbitmq_host : "rabbitmq"
  rabbitmq_vhost_effective    = var.rabbitmq_vhost != "" ? var.rabbitmq_vhost : "/"

  es_protocol_effective       = var.es_protocol != "" ? var.es_protocol : "https"
  es_host_effective           = var.es_host != "" ? var.es_host : "elasticsearch-es-http"
  use_shared_es_autoprovision = var.deploy_app_stack && !var.deploy_infra && var.es_password == ""
  es_user_default             = substr("rf_${local.namespace_sanitized}_user", 0, 64)
  es_user_effective           = local.use_shared_es_autoprovision ? local.es_user_default : var.es_user
  es_password_effective       = var.es_password != "" ? var.es_password : (local.use_shared_es_autoprovision ? random_password.shared_elasticsearch[0].result : try(data.kubernetes_secret_v1.elasticsearch_es_user[0].data.elastic, ""))
  es_index_prefix             = local.namespace_sanitized
  es_index_prefix_effective   = local.shared_infra_app_mode && var.shared_es_index_prefix_enabled ? local.es_index_prefix : ""

  deepdoc_url_effective = var.deepdoc_url != "" ? var.deepdoc_url : "http://deepdoc:8000"
  tei_host_effective    = var.tei_host != "" ? var.tei_host : "tei"

  use_shared_mysql_autoprovision = var.deploy_app_stack && var.auto_provision_shared_service_credentials && !var.deploy_infra && var.mysql_password == ""
  s3_prefix_path_effective       = trimspace(var.s3_prefix_path) != "" ? var.s3_prefix_path : (local.shared_infra_app_mode ? "${local.ragflow_namespace}/" : "")
  shared_mysql_root_password     = local.use_shared_mysql_autoprovision ? try(data.kubernetes_secret_v1.shared_mysql_password[0].data.password, "") : ""

  # Infinity memory allocation derived from infinity_memory_request (e.g. "4Gi" -> 4)
  # Extract numeric GB value from K8s resource string (supports Gi, G suffixes)
  infinity_memory_gb = tonumber(regex("^(\\d+)(Gi|G)$", var.infinity_memory_request)[0])

  # buffer_manager_size: ~50% of total memory, formatted as "XGB" for infinity_conf.toml
  infinity_buffer_manager_size = "${local.infinity_memory_gb / 2}GB"

  # memindex_memory_quota: ~25% of total memory, formatted as "XGB" for infinity_conf.toml
  infinity_memindex_memory_quota = "${max(1, local.infinity_memory_gb / 4)}GB"

  # Cluster-scoped ownership resolution:
  # - auto: resolve from local state ownership + shared ECK detection
  # - manual: use explicit manage_cluster_scoped_resources variable
  manage_cluster_scoped_resources_resolved = var.cluster_scoped_resource_mode == "auto" ? (
    data.external.cluster_scoped_resource_ownership.result.manage_cluster_scoped_resources == "true"
  ) : var.manage_cluster_scoped_resources

  # Parse upload_size_limit string to bytes for MAX_CONTENT_LENGTH env var
  # Uses decimal units: 1m = 1000*1000, 1g = 1000*1000*1000
  max_content_length_bytes = floor(
    tonumber(regex("^(\\d+)", var.upload_size_limit)[0]) *
    (can(regex("g$", var.upload_size_limit)) ? 1000 * 1000 * 1000 :
    can(regex("m$", var.upload_size_limit)) ? 1000 * 1000 : 1)
  )
}

# =============================================================================
# Resources
# =============================================================================


resource "kubernetes_namespace_v1" "ragflow" {
  metadata {
    name = local.ragflow_namespace
  }
}

resource "terraform_data" "validate_shared_mode_inputs" {
  count = var.deploy_app_stack ? 1 : 0

  lifecycle {
    precondition {
      condition     = var.deploy_infra || var.redis_db != 1
      error_message = "When deploy_infra=false (shared Redis), redis_db must be explicitly set to a unique non-default value (0..15, not 1)."
    }
    precondition {
      condition     = var.deploy_infra || trimspace(local.redis_password_effective) != ""
      error_message = "When deploy_infra=false (shared Redis), redis_password must be provided or resolvable from shared_infra_namespace redis-password secret."
    }
    precondition {
      condition = (
        var.cloud_provider == "gcp" ||
        !var.deploy_app_stack ||
        trimspace(var.s3_access_key) != "" && trimspace(var.s3_secret_key) != ""
      )
      error_message = "S3 credentials are required for this profile. Set s3_access_key and s3_secret_key (GCP workload identity is the only credentialless exception)."
    }
  }
}

# Shared service secrets (used only in app namespaces when deploy_infra=false and auto-provision is enabled)
data "kubernetes_secret_v1" "shared_mysql_password" {
  count = local.use_shared_mysql_autoprovision ? 1 : 0
  metadata {
    name      = "mysql-password"
    namespace = var.shared_infra_namespace
  }
}

data "kubernetes_secret_v1" "shared_redis_password" {
  count = local.use_shared_redis_secret ? 1 : 0
  metadata {
    name      = "redis-password"
    namespace = var.shared_infra_namespace
  }
}

# =============================================================================
# GCS Service Account (for Workload Identity when cloud_provider = 'gcp')
# =============================================================================

# =============================================================================
# MySQL Deployment (K8s Mode)
# =============================================================================

resource "random_password" "mysql" {
  length  = 16
  special = false
}

resource "random_password" "redis" {
  count   = var.deploy_infra ? 1 : 0
  length  = 16
  special = false
}

resource "random_password" "rabbitmq" {
  count   = var.deploy_app_stack ? 1 : 0
  length  = 16
  special = false
}

resource "random_password" "shared_mysql" {
  count   = local.use_shared_mysql_autoprovision ? 1 : 0
  length  = 24
  special = false
  keepers = {
    namespace = local.ragflow_namespace
  }
}

resource "random_password" "shared_elasticsearch" {
  count   = local.use_shared_es_autoprovision ? 1 : 0
  length  = 24
  special = false
  keepers = {
    namespace = local.ragflow_namespace
  }
}

# Ref: https://github.com/hashicorp/terraform-provider-kubernetes/issues/1986
# Workaround for PVC creation timeout due to provider rate limiting
resource "kubernetes_persistent_volume_claim_v1" "mysql" {
  count = var.deploy_infra ? 1 : 0

  metadata {
    name      = "mysql-data"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    access_modes = ["ReadWriteOnce"]

    resources {
      requests = {
        storage = "${var.mysql_k8s_storage}Gi"
      }
    }

    storage_class_name = local.config.storage_class
  }

  # Avoid provider rate limit timeout when PVC is pending binding
  wait_until_bound = false
}

resource "kubernetes_secret_v1" "mysql" {
  count = var.deploy_infra ? 1 : 0

  metadata {
    name      = "mysql-password"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  data = {
    password = random_password.mysql.result
  }
}

resource "kubernetes_stateful_set_v1" "mysql" {
  count = var.deploy_infra ? 1 : 0

  metadata {
    name      = "mysql"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app = "mysql"
    }
  }

  spec {
    service_name = "mysql"

    replicas = 1

    selector {
      match_labels = {
        app = "mysql"
      }
    }

    template {
      metadata {
        labels = {
          app = "mysql"
        }
      }

      spec {
        container {
          name  = "mysql"
          image = local.mysql_image

          port {
            container_port = 3306
            name           = "mysql"
          }

          # MySQL binlog retention: binlog-expire-logs-seconds=172800 (2 days = 172800 seconds)
          # Note: binlog_expire_logs_seconds takes precedence over expire_logs_days in MySQL 8.0
          # max-binlog-size=1G (default, single file max 1GB)
          # With 2-day retention and 1GB max per file, worst-case binlog storage is ~2-3GB
          args = ["--max-connections=2000", "--wait-timeout=600", "--interactive-timeout=600", "--binlog-expire-logs-seconds=172800"]

          env {
            name = "MYSQL_ROOT_PASSWORD"

            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.mysql[0].metadata[0].name
                key  = "password"
              }
            }
          }

          env {
            name  = "MYSQL_DATABASE"
            value = var.mysql_db_name
          }

          env {
            name  = "MYSQL_USER"
            value = local.mysql_user_effective
          }

          env {
            name  = "MYSQL_PASSWORD"
            value = random_password.mysql.result
          }

          env {
            name  = "MYSQL_MAX_CONNECTIONS"
            value = var.mysql_max_connections
          }

          volume_mount {
            name       = "data"
            mount_path = "/var/lib/mysql"
          }

          resources {
            requests = {
              cpu    = var.mysql_cpu_request
              memory = var.mysql_memory_request
            }
            limits = {
              cpu    = var.mysql_cpu_limit
              memory = var.mysql_memory_limit
            }
          }
        }

        volume {
          name = "data"

          persistent_volume_claim {
            claim_name = kubernetes_persistent_volume_claim_v1.mysql[0].metadata[0].name
          }
        }
      }
    }
  }

  timeouts {
    create = "30m"
    update = "30m"
    delete = "30m"
  }
}

resource "kubernetes_service_v1" "mysql" {
  count = var.deploy_infra ? 1 : 0

  metadata {
    name      = "mysql"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    selector = {
      app = "mysql"
    }

    port {
      port        = 3306
      target_port = 3306
    }
  }
}

resource "kubernetes_config_map_v1" "shared_mysql_bootstrap_sql" {
  count = local.use_shared_mysql_autoprovision ? 1 : 0
  metadata {
    name      = "shared-mysql-bootstrap-sql"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }
  data = {
    "bootstrap.sql" = <<-SQL
      CREATE DATABASE IF NOT EXISTS `${local.mysql_db_name_effective}`;
      CREATE USER IF NOT EXISTS '${local.mysql_user_effective}'@'%' IDENTIFIED BY '${local.mysql_password_effective}';
      ALTER USER '${local.mysql_user_effective}'@'%' IDENTIFIED BY '${local.mysql_password_effective}';
      GRANT ALL PRIVILEGES ON `${local.mysql_db_name_effective}`.* TO '${local.mysql_user_effective}'@'%';
      FLUSH PRIVILEGES;
    SQL
  }
}

resource "kubernetes_job_v1" "shared_mysql_user_bootstrap" {
  count = local.use_shared_mysql_autoprovision ? 1 : 0
  metadata {
    name      = "shared-mysql-user-bootstrap"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    ttl_seconds_after_finished = var.shared_service_job_ttl_seconds
    template {
      metadata {
        name = "shared-mysql-user-bootstrap"
      }
      spec {
        restart_policy = "OnFailure"
        container {
          name  = "mysql-bootstrap"
          image = local.mysql_image
          command = [
            "sh",
            "-c",
            <<-EOT
              mysql -h "${local.mysql_host_effective}" -P "${var.mysql_port}" -u root -p"${local.shared_mysql_root_password}" < /work/bootstrap.sql
            EOT
          ]
          volume_mount {
            name       = "bootstrap-sql"
            mount_path = "/work/bootstrap.sql"
            sub_path   = "bootstrap.sql"
          }
        }
        volume {
          name = "bootstrap-sql"
          config_map {
            name = kubernetes_config_map_v1.shared_mysql_bootstrap_sql[0].metadata[0].name
            items {
              key  = "bootstrap.sql"
              path = "bootstrap.sql"
            }
          }
        }
      }
    }
    backoff_limit = 5
  }
  wait_for_completion = true
  timeouts {
    create = "5m"
    update = "5m"
  }
}

resource "kubernetes_job_v1" "shared_mysql_verify" {
  count = var.enable_shared_service_verify_jobs && var.deploy_app_stack && !var.deploy_infra ? 1 : 0
  metadata {
    name      = "shared-mysql-verify"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    ttl_seconds_after_finished = var.shared_service_job_ttl_seconds
    template {
      metadata {
        name = "shared-mysql-verify"
      }
      spec {
        restart_policy = "OnFailure"
        container {
          name  = "mysql-verify"
          image = local.mysql_image
          command = [
            "sh",
            "-c",
            <<-EOT
              mysql -h "${local.mysql_host_effective}" -P "${var.mysql_port}" -u "${local.mysql_user_effective}" -p"${local.mysql_password_effective}" -D "${local.mysql_db_name_effective}" -e "SELECT 1;"
            EOT
          ]
        }
      }
    }
    backoff_limit = 5
  }
  wait_for_completion = true
  timeouts {
    create = "5m"
    update = "5m"
  }
  depends_on = [kubernetes_job_v1.shared_mysql_user_bootstrap]
}

# =============================================================================
# Redis Deployment
# =============================================================================

resource "kubernetes_secret_v1" "redis" {
  count = var.deploy_infra ? 1 : 0

  metadata {
    name      = "redis-password"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  data = {
    password = random_password.redis[0].result
  }
}

resource "kubernetes_deployment_v1" "redis" {
  count = var.deploy_infra ? 1 : 0
  metadata {
    name      = "redis"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app = "redis"
    }
  }

  spec {
    replicas = 1

    # Limit revision history to reduce orphaned ReplicaSets
    revision_history_limit = 1

    selector {
      match_labels = {
        app = "redis"
      }
    }

    strategy {
      type = "Recreate"
    }

    template {
      metadata {
        labels = {
          app = "redis"
        }
      }

      spec {
        container {
          name  = "redis"
          image = local.redis_image

          port {
            container_port = 6379
          }

          # Use valkey-server for valkey image (default)
          command = [
            "valkey-server",
            "--requirepass",
            local.redis_password_effective,
            "--maxmemory",
            # Convert Kubernetes memory format (Gi, Mi) to Redis/Valkey format (gb, mb)
            lower(replace(replace(var.redis_memory_limit, "Gi", "gb"), "Mi", "mb")),
            "--maxmemory-policy",
            "allkeys-lru",
          ]

          resources {
            requests = {
              cpu    = var.redis_cpu_request
              memory = var.redis_memory_request
            }
            limits = {
              cpu    = var.redis_cpu_limit
              memory = var.redis_memory_limit
            }
          }
        }
      }
    }
  }
}

resource "kubernetes_service_v1" "redis" {
  count = var.deploy_infra ? 1 : 0
  metadata {
    name      = "redis"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    selector = {
      app = "redis"
    }

    port {
      port        = 6379
      target_port = 6379
    }
  }
}

# =============================================================================
# TEI (Text Embeddings) Deployment
# =============================================================================

resource "kubernetes_deployment_v1" "tei" {
  count = var.tei_replicas > 0 ? 1 : 0
  metadata {
    name      = "tei"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app = "tei"
    }
  }

  spec {
    replicas = var.tei_replicas

    # Limit revision history to reduce orphaned ReplicaSets
    revision_history_limit = 1

    selector {
      match_labels = {
        app = "tei"
      }
    }

    strategy {
      type = "Recreate"
    }

    template {
      metadata {
        labels = {
          app = "tei"
        }
      }

      spec {
        container {
          name  = "tei"
          image = local.tei_image

          port {
            container_port = 80
          }

          args = [
            "--model-id",
            "/data/${var.tei_model}",
            "--auto-truncate",
          ]

          resources {
            requests = {
              cpu    = var.tei_cpu_request
              memory = var.tei_memory_request
            }
            limits = {
              cpu    = var.tei_cpu_limit
              memory = var.tei_memory_limit
            }
          }
        }
      }
    }
  }
}

resource "kubernetes_service_v1" "tei" {
  count = var.tei_replicas > 0 ? 1 : 0
  metadata {
    name      = "tei"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    selector = {
      app = "tei"
    }

    port {
      port        = 80
      target_port = 80
    }
  }
}

# =============================================================================
# RabbitMQ Deployment
# =============================================================================

resource "kubernetes_config_map_v1" "rabbitmq" {
  count = var.deploy_app_stack ? 1 : 0
  metadata {
    name      = "rabbitmq-config"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  data = {
    "definitions.json" = jsonencode({
      rabbit_version             = "4.1.3"
      rabbitmq_version           = "4.1.3"
      product_name               = "RabbitMQ"
      product_version            = "4.1.3"
      rabbitmq_definition_format = "cluster"
      # User definition required for RabbitMQ 4.x (no longer auto-created from env vars)
      # Password hash is dynamically computed from random_password.rabbitmq using bcrypt
      users = [
        {
          name     = local.rabbitmq_user_effective
          password = local.rabbitmq_password_effective
          tags     = ["administrator"]
        }
      ]
      vhosts = [
        {
          name        = local.rabbitmq_vhost_effective
          description = "RAGFlow virtual host"
          metadata = {
            description        = "RAGFlow virtual host"
            tags               = []
            default_queue_type = "classic"
          }
          tags = []
        }
      ]
      topic_permissions = []
      permissions = [
        {
          user      = local.rabbitmq_user_effective
          vhost     = local.rabbitmq_vhost_effective
          configure = ".*"
          write     = ".*"
          read      = ".*"
        }
      ]
      parameters = []
      global_parameters = [
        {
          name  = "cluster_tags"
          value = []
        }
      ]
      policies = []
      queues = [
        {
          name        = "te.1.common"
          vhost       = local.rabbitmq_vhost_effective
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.1.graphrag"
          vhost       = local.rabbitmq_vhost_effective
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.1.raptor"
          vhost       = local.rabbitmq_vhost_effective
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.1.resume"
          vhost       = local.rabbitmq_vhost_effective
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.0.common"
          vhost       = local.rabbitmq_vhost_effective
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.0.graphrag"
          vhost       = local.rabbitmq_vhost_effective
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.0.raptor"
          vhost       = local.rabbitmq_vhost_effective
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.0.resume"
          vhost       = local.rabbitmq_vhost_effective
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.error"
          vhost       = local.rabbitmq_vhost_effective
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        }
      ]
      exchanges = [
        {
          name        = "test1"
          vhost       = local.rabbitmq_vhost_effective
          type        = "direct"
          durable     = true
          auto_delete = false
          internal    = false
          arguments   = {}
        }
      ]
      bindings = [
        {
          source           = "test1"
          vhost            = local.rabbitmq_vhost_effective
          destination      = "te.1.common"
          destination_type = "queue"
          routing_key      = "te.1.common"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = local.rabbitmq_vhost_effective
          destination      = "te.1.graphrag"
          destination_type = "queue"
          routing_key      = "te.1.graphrag"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = local.rabbitmq_vhost_effective
          destination      = "te.1.raptor"
          destination_type = "queue"
          routing_key      = "te.1.raptor"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = local.rabbitmq_vhost_effective
          destination      = "te.1.resume"
          destination_type = "queue"
          routing_key      = "te.1.resume"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = local.rabbitmq_vhost_effective
          destination      = "te.0.common"
          destination_type = "queue"
          routing_key      = "te.0.common"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = local.rabbitmq_vhost_effective
          destination      = "te.0.graphrag"
          destination_type = "queue"
          routing_key      = "te.0.graphrag"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = local.rabbitmq_vhost_effective
          destination      = "te.0.raptor"
          destination_type = "queue"
          routing_key      = "te.0.raptor"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = local.rabbitmq_vhost_effective
          destination      = "te.0.resume"
          destination_type = "queue"
          routing_key      = "te.0.resume"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = local.rabbitmq_vhost_effective
          destination      = "te.error"
          destination_type = "queue"
          routing_key      = "te.error"
          arguments        = {}
        }
      ]
    })
    "10-definitions.conf" = <<-EOT
definitions.import_backend = local_filesystem
definitions.local.path = /etc/rabbitmq/definitions.json

# Enable RabbitMQ management plugin
management.tcp.port = 15672
# Allow longer parser tasks before broker force-closes the channel.
consumer_timeout = 7200000
EOT
  }
}

# Ref: https://github.com/hashicorp/terraform-provider-kubernetes/issues/1986
# Workaround for PVC creation timeout due to provider rate limiting
resource "kubernetes_persistent_volume_claim_v1" "rabbitmq" {
  count = var.deploy_app_stack ? 1 : 0
  metadata {
    name      = "rabbitmq-pvc"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    access_modes = ["ReadWriteOnce"]

    resources {
      requests = {
        storage = "${var.rabbitmq_storage}Gi"
      }
    }

    storage_class_name = local.config.storage_class
  }

  # Avoid provider PVC is pending binding rate limit timeout when
  wait_until_bound = false
}

resource "kubernetes_deployment_v1" "rabbitmq" {
  count = var.deploy_app_stack ? 1 : 0
  metadata {
    name      = "rabbitmq"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app = "rabbitmq"
    }
  }

  spec {
    replicas = 1

    # Limit revision history to reduce orphaned ReplicaSets
    revision_history_limit = 1

    selector {
      match_labels = {
        app = "rabbitmq"
      }
    }

    strategy {
      type = "Recreate"
    }

    template {
      metadata {
        labels = {
          app = "rabbitmq"
        }
      }

      spec {
        container {
          name  = "rabbitmq"
          image = local.rabbitmq_image

          port {
            container_port = 5672
            name           = "amqp"
          }

          port {
            container_port = 15672
            name           = "management"
          }

          port {
            container_port = 15692
            name           = "prometheus"
          }

          env {
            name  = "RABBITMQ_DEFINITIONS_FILE"
            value = "/etc/rabbitmq/definitions.json"
          }

          volume_mount {
            name       = "rabbitmq-storage"
            mount_path = "/var/lib/rabbitmq"
          }

          volume_mount {
            name       = "rabbitmq-definitions"
            mount_path = "/etc/rabbitmq/definitions.json"
            sub_path   = "definitions.json"
          }

          volume_mount {
            name       = "rabbitmq-definitions-conf"
            mount_path = "/etc/rabbitmq/conf.d/10-definitions.conf"
            sub_path   = "10-definitions.conf"
          }

          resources {
            requests = {
              cpu    = var.rabbitmq_cpu_request
              memory = var.rabbitmq_memory_request
            }
            limits = {
              cpu    = var.rabbitmq_cpu_limit
              memory = var.rabbitmq_memory_limit
            }
          }
        }

        volume {
          name = "rabbitmq-storage"

          persistent_volume_claim {
            claim_name = kubernetes_persistent_volume_claim_v1.rabbitmq[0].metadata[0].name
          }
        }

        volume {
          name = "rabbitmq-definitions"

          config_map {
            name = kubernetes_config_map_v1.rabbitmq[0].metadata[0].name

            items {
              key  = "definitions.json"
              path = "definitions.json"
            }
          }
        }

        volume {
          name = "rabbitmq-definitions-conf"

          config_map {
            name = kubernetes_config_map_v1.rabbitmq[0].metadata[0].name

            items {
              key  = "10-definitions.conf"
              path = "10-definitions.conf"
            }
          }
        }
      }
    }
  }

  timeouts {
    create = "30m"
    update = "30m"
    delete = "30m"
  }
}

resource "kubernetes_service_v1" "rabbitmq" {
  count = var.deploy_app_stack ? 1 : 0
  metadata {
    name      = "rabbitmq"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    selector = {
      app = "rabbitmq"
    }

    port {
      name        = "amqp"
      port        = 5672
      target_port = 5672
    }

    port {
      name        = "management"
      port        = 15672
      target_port = 15672
    }

    port {
      name        = "prometheus"
      port        = 15692
      target_port = 15692
    }
  }
}

resource "kubernetes_secret_v1" "rabbitmq_password" {
  count = var.deploy_app_stack ? 1 : 0

  metadata {
    name      = "rabbitmq-password"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  data = {
    password = local.rabbitmq_password_effective
  }
}

# =============================================================================
# Elasticsearch Deployment (K8s Mode)
# =============================================================================

# Resolve ownership of cluster-scoped resources so BYOK behavior is reusable
# across all callers (CI, local runs, and scripts).
data "external" "cluster_scoped_resource_ownership" {
  program = ["python3", "${path.module}/resolve_cluster_scoped_ownership.py"]

  query = {
    kubeconfig_path = pathexpand(var.kubeconfig_path)
    cloud_provider  = var.cloud_provider
    state_path      = terraform.workspace == "default" ? "${path.module}/terraform.tfstate" : "${path.module}/terraform.tfstate.d/${terraform.workspace}/terraform.tfstate"
  }
}

# Deploy ECK (Elastic Cloud on Kubernetes) Operator using Helm
# This installs the CRDs and operator required for Elasticsearch resources
# Ref: https://artifacthub.io/packages/helm/elastic/eck-operator
resource "helm_release" "eck_operator" {
  count            = var.deploy_infra && local.manage_cluster_scoped_resources_resolved ? 1 : 0
  name             = "eck-operator"
  repository       = "https://helm.elastic.co"
  chart            = "eck-operator"
  version          = "3.3.1"
  namespace        = "elastic-system"
  create_namespace = true
  upgrade_install  = true

  # Set timeout to wait for CRDs installation
  timeout = 600

  # Skip CRD validation since CRDs are installed by the chart
  disable_openapi_validation = true

  # Allow helm release to already exist (idempotent)
  lifecycle {
    ignore_changes  = [name, namespace, repository, chart, version]
    prevent_destroy = true
  }
}

# =============================================================================
# Check CRD Availability
# =============================================================================
# Use a local-exec to verify CRD is installed by ECK operator.
# This replaces the fixed time_sleep with a dynamic check.

resource "terraform_data" "wait_for_elasticsearch_crd" {
  count      = var.deploy_infra ? 1 : 0
  depends_on = [helm_release.eck_operator]

  # Idempotent check for CRD - if already established, succeed immediately
  # Otherwise wait for CRD registration and establishment
  # Set KUBECONFIG to match the provider's configuration
  provisioner "local-exec" {
    environment = {
      KUBECONFIG = pathexpand(var.kubeconfig_path)
    }
    command = "python3 wait_for_k8s_resource.py default crd elasticsearches.elasticsearch.k8s.elastic.co"
  }
}

# =============================================================================
# Elasticsearch Deployment (K8s Mode) - Using kubernetes_manifest
# =============================================================================
# Uses kubernetes_manifest to deploy Elasticsearch CR directly.
# This avoids kubectl dependency and uses Terraform's native Kubernetes provider.
# The CRD is installed by ECK operator helm chart.

resource "kubernetes_manifest" "elasticsearch_compute_class" {
  # CustomComputeClass for GKE to set vm.max_map_count
  # Requires GKE >= 1.30.3-gke.1451000
  # Reference: https://www.elastic.co/docs/deploy-manage/deploy/cloud-on-k8s/deploy-eck-on-gke-autopilot
  count = var.deploy_infra && var.cloud_provider == "gcp" && local.manage_cluster_scoped_resources_resolved ? 1 : 0

  manifest = {
    apiVersion = "cloud.google.com/v1"
    kind       = "ComputeClass"
    metadata = {
      name = "elasticsearch"
    }
    spec = {
      whenUnsatisfiable = "DoNotScaleUp"
      nodePoolAutoCreation = {
        enabled = true
      }
      priorityDefaults = {
        nodeSystemConfig = {
          linuxNodeConfig = {
            sysctls = {
              "vm.max_map_count" = 262144
            }
          }
        }
      }
      priorities = [
        {
          machineFamily = "n2"
        }
      ]
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

# =============================================================================
# Elasticsearch Deployment (K8s Mode) - ConfigMap + Job Pattern
# =============================================================================
# Because the ECK CRDs are installed dynamically by the operator, Terraform's
# kubernetes_manifest would fail validation at plan time.
# To assume the CRDs will exist, we use a ConfigMap + Job to apply the manifest.

# 1. Store the Elasticsearch manifest in a ConfigMap
resource "kubernetes_config_map_v1" "elasticsearch_manifest" {
  count = var.deploy_infra ? 1 : 0
  metadata {
    name      = "elasticsearch-manifest"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  data = {
    "elasticsearch.yaml" = <<YAML
apiVersion: elasticsearch.k8s.elastic.co/v1
kind: Elasticsearch
metadata:
  name: elasticsearch
  namespace: ${kubernetes_namespace_v1.ragflow.metadata[0].name}
spec:
  version: ${local.es_version}
  nodeSets:
  # Master nodes - cluster management only, no data storage
  - name: masters
    count: ${var.es_master_node_count}
    config:
      node.store.allow_mmap: true
      node.roles: ["master"]
      # Some settings apply only to master, some only to data, some to both. Keeping Master and Data consistent is safest.
      # Set max shards per node (default is typically 1000)
      cluster.max_shards_per_node: 40000
      # --- Recovery tuning parameters ---
      # Limit recovery bandwidth (default is 40mb, recommended to increase for high-throughput networks)
      indices.recovery.max_bytes_per_sec: 150mb
      # Limit concurrent snapshot file downloads per node during recovery (default is 25, recommended <= 3)
      indices.recovery.max_concurrent_snapshot_file_downloads_per_node: 5
      # --- Other recommended stability parameters ---
      # Note: index.unassigned.node_left.delayed_timeout is an index-level setting,
      # not a node-level setting. In ES 9.x it must be set via index templates, not here.
    podTemplate:
      spec:
%{if local.is_gke_gateway}
        # ServiceAccount: use default (node identity) with node SA having GCS permissions
        serviceAccountName: "default"
        nodeSelector:
          cloud.google.com/compute-class: elasticsearch
%{endif}
        containers:
        - name: elasticsearch
          image: ${var.es_image}
          imagePullPolicy: Always
          resources:
            requests:
              cpu: "${var.es_master_cpu_request}"
              memory: "${var.es_master_memory_request}"
            limits:
              cpu: "${var.es_master_cpu_limit}"
              memory: "${var.es_master_memory_limit}"
          env:
          - name: ES_JAVA_OPTS
            value: "-Xms${var.es_master_heap_size} -Xmx${var.es_master_heap_size}"
%{if !local.is_gke_gateway}
    volumeClaimTemplates:
    - metadata:
        name: elasticsearch-data
      spec:
        accessModes: ["ReadWriteOnce"]
        storageClassName: ${local.config.storage_class}
        resources:
          requests:
            storage: "1Gi"
%{endif}
  # Data/Ingest nodes - data storage and ingest pipelines
  - name: data-ingest
    count: ${var.es_data_node_count}
    config:
      node.store.allow_mmap: true
      node.roles: ["data", "ingest"]
      # Some settings apply only to master, some only to data, some to both. Keeping Master and Data consistent is safest.
      # Set max shards per node (default is typically 1000)
      cluster.max_shards_per_node: 40000
      # --- Recovery tuning parameters ---
      # Limit recovery bandwidth (default is 40mb, recommended to increase for high-throughput networks)
      indices.recovery.max_bytes_per_sec: 150mb
      # Limit concurrent snapshot file downloads per node during recovery (default is 25, recommended <= 3)
      indices.recovery.max_concurrent_snapshot_file_downloads_per_node: 5
      # --- Other recommended stability parameters ---
      # Note: index.unassigned.node_left.delayed_timeout is an index-level setting,
      # not a node-level setting. In ES 9.x it must be set via index templates, not here.
    podTemplate:
      spec:
        # ServiceAccount: use default (node identity) with node SA having GCS permissions
        serviceAccountName: "default"
%{if local.is_gke_gateway}
        nodeSelector:
          cloud.google.com/compute-class: elasticsearch
%{endif}
        containers:
        - name: elasticsearch
          image: ${var.es_image}
          imagePullPolicy: Always
          resources:
            requests:
              cpu: "${var.es_data_cpu_request}"
              memory: "${var.es_data_memory_request}"
            limits:
              cpu: "${var.es_data_cpu_limit}"
              memory: "${var.es_data_memory_limit}"
          env:
          - name: ES_JAVA_OPTS
            value: "-Xms${var.es_data_heap_size} -Xmx${var.es_data_heap_size}"
    volumeClaimTemplates:
    - metadata:
        name: elasticsearch-data
      spec:
        accessModes: ["ReadWriteOnce"]
        storageClassName: ${local.config.storage_class}
        resources:
          requests:
            storage: "${var.es_data_storage}Gi"
YAML
  }

  depends_on = [terraform_data.wait_for_elasticsearch_crd]
}

# 2. ServiceAccount for the applier job
resource "kubernetes_service_account_v1" "elasticsearch_applier" {
  count = var.deploy_infra ? 1 : 0
  metadata {
    name      = "elasticsearch-applier"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }
}

# 3. Role to allow creating Elasticsearch resources
resource "kubernetes_role_v1" "elasticsearch_applier" {
  count = var.deploy_infra ? 1 : 0
  metadata {
    name      = "elasticsearch-applier"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  rule {
    api_groups = ["elasticsearch.k8s.elastic.co"]
    resources  = ["elasticsearches"]
    verbs      = ["get", "create", "update", "patch", "list"]
  }
}

# 4. RoleBinding
resource "kubernetes_role_binding_v1" "elasticsearch_applier" {
  count = var.deploy_infra ? 1 : 0
  metadata {
    name      = "elasticsearch-applier"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "Role"
    name      = kubernetes_role_v1.elasticsearch_applier[0].metadata[0].name
  }

  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account_v1.elasticsearch_applier[0].metadata[0].name
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }
}

# 5. Job to apply the manifest
resource "kubernetes_job_v1" "apply_elasticsearch" {
  count = var.deploy_infra ? 1 : 0
  metadata {
    name      = "apply-elasticsearch"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    template {
      metadata {
        name = "apply-elasticsearch"
      }
      spec {
        service_account_name = kubernetes_service_account_v1.elasticsearch_applier[0].metadata[0].name
        restart_policy       = "OnFailure"
        container {
          name    = "kubectl"
          image   = "bitnami/kubectl:latest"
          command = ["kubectl", "apply", "-f", "/data/elasticsearch.yaml"]

          env {
            name  = "HOME"
            value = "/tmp"
          }

          volume_mount {
            name       = "manifest"
            mount_path = "/data"
          }

          volume_mount {
            name       = "tmp"
            mount_path = "/tmp"
          }
        }
        volume {
          name = "manifest"
          config_map {
            name = kubernetes_config_map_v1.elasticsearch_manifest[0].metadata[0].name
          }
        }
        volume {
          name = "tmp"
          empty_dir {}
        }
      }
    }
    backoff_limit = 6
  }

  wait_for_completion = true

  timeouts {
    create = "5m"
    update = "5m"
  }

  depends_on = [
    terraform_data.wait_for_elasticsearch_crd,
    kubernetes_role_binding_v1.elasticsearch_applier,
    kubernetes_config_map_v1.elasticsearch_manifest,
  ]
}

# =============================================================================
# Wait for Elasticsearch Secret to be Available
# =============================================================================
resource "terraform_data" "wait_for_elasticsearch_secret" {
  count = var.deploy_infra ? 1 : 0
  triggers_replace = [
    kubernetes_job_v1.apply_elasticsearch[0].id
  ]

  provisioner "local-exec" {
    environment = {
      KUBECONFIG = pathexpand(var.kubeconfig_path)
    }
    command = "python3 wait_for_k8s_resource.py ${kubernetes_namespace_v1.ragflow.metadata[0].name} secret elasticsearch-es-elastic-user"
  }
}

# =============================================================================
# Read ECK-managed Elasticsearch Secret (k8s mode)
# =============================================================================
# This secret is managed by ECK operator and contains the auto-generated
# password for the 'elastic' user.
data "kubernetes_secret_v1" "elasticsearch_es_user" {
  count = var.deploy_infra ? 1 : 0
  metadata {
    name      = "elasticsearch-es-elastic-user"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  depends_on = [
    kubernetes_job_v1.apply_elasticsearch,
    terraform_data.wait_for_elasticsearch_secret
  ]
}

data "kubernetes_secret_v1" "shared_elasticsearch_es_user" {
  count = local.use_shared_es_autoprovision ? 1 : 0
  metadata {
    name      = "elasticsearch-es-elastic-user"
    namespace = var.shared_infra_namespace
  }
}

resource "kubernetes_config_map_v1" "shared_elasticsearch_role_payload" {
  count = local.use_shared_es_autoprovision ? 1 : 0
  metadata {
    name      = "shared-es-role-payload"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }
  data = {
    "role.json" = jsonencode({
      cluster = ["monitor"]
      indices = [
        for pattern in(
          local.es_index_prefix_effective != ""
          ? ["ragflow_${local.es_index_prefix_effective}_*", "ragflow_doc_meta_${local.es_index_prefix_effective}_*"]
          : ["ragflow_*", "ragflow_doc_meta_*"]
          ) : {
          names      = [pattern]
          privileges = ["read", "write", "create_index", "view_index_metadata", "manage"]
        }
      ]
    })
  }
}

resource "kubernetes_config_map_v1" "shared_elasticsearch_user_payload" {
  count = local.use_shared_es_autoprovision ? 1 : 0
  metadata {
    name      = "shared-es-user-payload"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }
  data = {
    "user.json" = jsonencode({
      password = local.es_password_effective
      roles    = [local.es_user_effective]
    })
  }
}

resource "kubernetes_job_v1" "shared_elasticsearch_user_bootstrap" {
  count = local.use_shared_es_autoprovision ? 1 : 0
  metadata {
    name      = "shared-es-user-bootstrap"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    ttl_seconds_after_finished = var.shared_service_job_ttl_seconds
    template {
      metadata {
        name = "shared-es-user-bootstrap"
      }
      spec {
        restart_policy = "OnFailure"
        container {
          name  = "es-bootstrap"
          image = local.curl_image
          command = [
            "sh",
            "-c",
            <<-EOT
              set -e
              curl -sS -k -u "elastic:${data.kubernetes_secret_v1.shared_elasticsearch_es_user[0].data.elastic}" -H "Content-Type: application/json" -X PUT "${local.es_protocol_effective}://${local.es_host_effective}:${var.es_port}/_security/role/${local.es_user_effective}" --data-binary @/payload/role.json
              curl -sS -k -u "elastic:${data.kubernetes_secret_v1.shared_elasticsearch_es_user[0].data.elastic}" -H "Content-Type: application/json" -X PUT "${local.es_protocol_effective}://${local.es_host_effective}:${var.es_port}/_security/user/${local.es_user_effective}" --data-binary @/payload/user.json
            EOT
          ]
          volume_mount {
            name       = "role-payload"
            mount_path = "/payload/role.json"
            sub_path   = "role.json"
          }
          volume_mount {
            name       = "user-payload"
            mount_path = "/payload/user.json"
            sub_path   = "user.json"
          }
        }
        volume {
          name = "role-payload"
          config_map {
            name = kubernetes_config_map_v1.shared_elasticsearch_role_payload[0].metadata[0].name
            items {
              key  = "role.json"
              path = "role.json"
            }
          }
        }
        volume {
          name = "user-payload"
          config_map {
            name = kubernetes_config_map_v1.shared_elasticsearch_user_payload[0].metadata[0].name
            items {
              key  = "user.json"
              path = "user.json"
            }
          }
        }
      }
    }
    backoff_limit = 5
  }
  wait_for_completion = true
  timeouts {
    create = "5m"
    update = "5m"
  }
}

# =============================================================================
# Infinity Deployment (Shadow Database / Alternative Doc Engine)
# =============================================================================
# Infinity is deployed as an optional shadow database alongside Elasticsearch.
# When enabled, RAGFlow uses ShadowWriteProxy to write to both ES and Infinity,
# allowing comparison and validation between the two doc engines.
# Ref: docker-compose-base.yml (infinity service)
# Ref: common/settings.py (DOC_ENGINE=elasticsearch,infinity for shadow mode)

resource "kubernetes_persistent_volume_claim_v1" "infinity" {
  count = var.infinity_enabled ? 1 : 0

  metadata {
    name      = "infinity-data"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    access_modes = ["ReadWriteOnce"]

    resources {
      requests = {
        storage = "${var.infinity_storage}Gi"
      }
    }

    storage_class_name = local.config.storage_class
  }

  wait_until_bound = false
}

# Infinity configuration file (mirrors docker/infinity_conf.toml)
resource "kubernetes_config_map_v1" "infinity_conf" {
  count = var.infinity_enabled ? 1 : 0

  metadata {
    name      = "infinity-conf"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  data = {
    "infinity_conf.toml" = <<-EOT
[general]
version                  = "0.7.0"
time_zone                = "utc-8"

[network]
server_address           = "0.0.0.0"
postgres_port            = 5432
http_port                = 23820
client_port              = 23817
connection_pool_size     = 128

[log]
log_filename             = "infinity.log"
log_dir                  = "/var/infinity/log"
log_to_stdout            = false
log_file_max_size        = "10GB"
log_file_rotate_count    = 10

# trace/debug/info/warning/error/critical 6 log levels, default: info
log_level               = "info"

[storage]
persistence_dir         = "/var/infinity/persistence"
data_dir                = "/var/infinity/data"
catalog_dir             = "/var/infinity/catalog"
# periodically activates garbage collection:
# 0 means real-time,
# s means seconds, for example "60s", 60 seconds
# m means minutes, for example "60m", 60 minutes
# h means hours, for example "1h", 1 hour
optimize_interval        = "10s"
cleanup_interval         = "60s"
compact_interval         = "120s"
storage_type             = "local"

# dump memory index entry when it reachs the capacity
mem_index_capacity       = 65536

# S3 storage config example:
# [storage.object_storage]
# url                      = "127.0.0.1:9005"
# bucket_name              = "infinity"
# access_key               = "minioadmin"
# secret_key               = "minioadmin"
# enable_https             = false

snapshot_dir            = "/var/infinity/snapshots"

[buffer]
buffer_manager_size      = "${local.infinity_buffer_manager_size}"
lru_num                  = 7
temp_dir                 = "/var/infinity/tmp"
result_cache             = "off"
memindex_memory_quota    = "${local.infinity_memindex_memory_quota}"

[wal]
wal_dir                       = "/var/infinity/wal"
checkpoint_interval      = "86400s"
wal_compact_threshold         = "1GB"

# flush_at_once: write and flush log each commit
# only_write: write log, OS control when to flush the log, default
# flush_per_second: logs are written after each commit and flushed to disk per second.
wal_flush                     = "only_write"

[resource]
resource_dir                  = "/usr/share/infinity/resource"
EOT
  }
}

resource "kubernetes_stateful_set_v1" "infinity" {
  count = var.infinity_enabled ? 1 : 0

  metadata {
    name      = "infinity"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app = "infinity"
    }
  }

  spec {
    service_name = "infinity"

    replicas = var.infinity_replicas

    selector {
      match_labels = {
        app = "infinity"
      }
    }

    template {
      metadata {
        labels = {
          app = "infinity"
        }
      }

      spec {
        container {
          name  = "infinity"
          image = local.infinity_image

          # Thrift client port (primary port for RAGFlow)
          port {
            container_port = 23817
            name           = "thrift"
          }

          # HTTP API port (health check, admin)
          port {
            container_port = 23820
            name           = "http"
          }

          # PostgreSQL-compatible port
          port {
            container_port = 5432
            name           = "postgresql"
          }

          args = ["-f", "/etc/infinity/infinity_conf.toml"]

          startup_probe {
            http_get {
              path = "/admin/node/current"
              port = 23820
            }
            initial_delay_seconds = 10
            period_seconds        = 10
            failure_threshold     = 30 # ~5 min max startup
          }

          readiness_probe {
            http_get {
              path = "/admin/node/current"
              port = 23820
            }
            initial_delay_seconds = 0
            period_seconds        = 10
            timeout_seconds       = 5
            failure_threshold     = 3
          }

          liveness_probe {
            http_get {
              path = "/admin/node/current"
              port = 23820
            }
            initial_delay_seconds = 30
            period_seconds        = 20
            timeout_seconds       = 5
            failure_threshold     = 3
          }

          resources {
            requests = {
              cpu    = var.infinity_cpu_request
              memory = var.infinity_memory_request
            }
            limits = {
              cpu    = var.infinity_cpu_limit
              memory = var.infinity_memory_limit
            }
          }

          volume_mount {
            name       = "data"
            mount_path = "/var/infinity"
          }

          volume_mount {
            name       = "config"
            mount_path = "/etc/infinity"
            read_only  = true
          }
        }

        volume {
          name = "data"

          persistent_volume_claim {
            claim_name = kubernetes_persistent_volume_claim_v1.infinity[0].metadata[0].name
          }
        }

        volume {
          name = "config"

          config_map {
            name = kubernetes_config_map_v1.infinity_conf[0].metadata[0].name

            items {
              key  = "infinity_conf.toml"
              path = "infinity_conf.toml"
            }
          }
        }
      }
    }
  }

  timeouts {
    create = "15m"
    update = "15m"
    delete = "10m"
  }
}

resource "kubernetes_service_v1" "infinity" {
  count = var.infinity_enabled ? 1 : 0

  metadata {
    name      = "infinity"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    selector = {
      app = "infinity"
    }

    port {
      name        = "thrift"
      port        = 23817
      target_port = 23817
    }

    port {
      name        = "http"
      port        = 23820
      target_port = 23820
    }

    port {
      name        = "postgresql"
      port        = 5432
      target_port = 5432
    }
  }
}

# =============================================================================
# S3 Storage Secret
# =============================================================================

resource "kubernetes_secret_v1" "storage" {
  count = var.deploy_app_stack ? 1 : 0

  metadata {
    name      = "ragflow-storage"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  data = {
    S3_ENDPOINT   = var.s3_endpoint != "" ? var.s3_endpoint : local.config.s3_endpoint
    S3_BUCKET     = local.s3_bucket_name
    S3_ACCESS_KEY = var.s3_access_key
    S3_SECRET_KEY = var.s3_secret_key
    S3_REGION     = var.s3_region != "" ? var.s3_region : local.config.s3_region
  }

  type = "Opaque"
}

resource "kubernetes_job_v1" "shared_s3_verify" {
  count = var.enable_shared_service_verify_jobs && var.deploy_app_stack ? 1 : 0
  metadata {
    name      = "shared-s3-verify"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    ttl_seconds_after_finished = var.shared_service_job_ttl_seconds
    template {
      metadata {
        name = "shared-s3-verify"
      }
      spec {
        restart_policy = "OnFailure"
        container {
          name  = "s3-verify"
          image = local.minio_mc_image
          command = [
            "sh",
            "-c",
            <<-EOT
              set -e
              if [ "${var.cloud_provider}" = "gcp" ] && ( [ -z "${var.s3_access_key}" ] || [ -z "${var.s3_secret_key}" ] ); then
                echo "Skipping shared-s3-verify: GCP credentialless storage mode."
                exit 0
              fi
              mc alias set verify "${var.s3_endpoint != "" ? var.s3_endpoint : local.config.s3_endpoint}" "${var.s3_access_key}" "${var.s3_secret_key}"
              PREFIX="${local.s3_prefix_path_effective}"
              TMP_OBJ="_verify_${local.namespace_sanitized}_$$(date +%s)_$${RANDOM:-0}.tmp"
              if [ -n "$${PREFIX}" ]; then
                case "$${PREFIX}" in
                  */) OBJ_PATH="$${PREFIX}$${TMP_OBJ}" ;;
                  *) OBJ_PATH="$${PREFIX}/$${TMP_OBJ}" ;;
                esac
              else
                OBJ_PATH="$${TMP_OBJ}"
              fi
              printf 'verify\n' | mc pipe "verify/${local.s3_bucket_name}/$${OBJ_PATH}" >/dev/null
              mc stat "verify/${local.s3_bucket_name}/$${OBJ_PATH}" >/dev/null
              mc rm "verify/${local.s3_bucket_name}/$${OBJ_PATH}" >/dev/null
            EOT
          ]
        }
      }
    }
    backoff_limit = 5
  }
  wait_for_completion = true
  timeouts {
    create = "5m"
    update = "5m"
  }
}

resource "kubernetes_job_v1" "shared_deepdoc_verify" {
  count = var.enable_shared_service_verify_jobs && var.deploy_app_stack && !var.deploy_infra ? 1 : 0
  metadata {
    name      = "shared-deepdoc-verify"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    ttl_seconds_after_finished = var.shared_service_job_ttl_seconds
    template {
      metadata {
        name = "shared-deepdoc-verify"
      }
      spec {
        restart_policy = "OnFailure"
        container {
          name  = "deepdoc-verify"
          image = local.curl_image
          command = [
            "sh",
            "-c",
            <<-EOT
              set -e
              curl -fsS "${local.deepdoc_url_effective}/health" >/dev/null
            EOT
          ]
        }
      }
    }
    backoff_limit = 5
  }
  wait_for_completion = true
  timeouts {
    create = "5m"
    update = "5m"
  }
}

# =============================================================================
# RAGFlow Environment Secret
# =============================================================================

resource "kubernetes_secret_v1" "ragflow_env" {
  count = var.deploy_app_stack ? 1 : 0

  metadata {
    name      = "ragflow-env"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  data = {
    # MySQL Configuration
    MYSQL_HOST     = local.mysql_host_effective
    MYSQL_PORT     = var.mysql_port
    MYSQL_USER     = local.mysql_user_effective
    MYSQL_DB_NAME  = local.mysql_db_name_effective
    MYSQL_DBNAME   = local.mysql_db_name_effective
    MYSQL_PASSWORD = local.mysql_password_effective

    # Elasticsearch Configuration
    ES_PROTOCOL      = local.es_protocol_effective
    ES_HOST          = local.es_host_effective
    ES_PORT          = var.es_port
    ES_USER          = local.es_user_effective
    ELASTIC_PASSWORD = local.es_password_effective
    ES_INDEX_PREFIX  = local.es_index_prefix_effective

    # DOC_ENGINE configuration
    # When infinity_enabled=true, set DOC_ENGINE="elasticsearch,infinity" for shadow write proxy
    # This makes ES primary and Infinity a shadow database for comparison
    # Ref: common/settings.py - parses comma-separated engines, first is primary, rest are shadows
    DOC_ENGINE = var.infinity_enabled ? "elasticsearch,infinity" : "elasticsearch"

    # Infinity Shadow Database Configuration
    INFINITY_HOST      = var.infinity_enabled ? "infinity" : ""
    INFINITY_PORT      = var.infinity_enabled ? "23817" : ""
    INFINITY_URI       = var.infinity_enabled ? "infinity:23817" : ""
    INFINITY_HTTP_PORT = var.infinity_enabled ? "23820" : ""
    INFINITY_PSQL_PORT = var.infinity_enabled ? "5432" : ""
    INFINITY_DB_NAME   = var.infinity_enabled ? "default_db" : ""

    # Redis Configuration
    REDIS_HOST     = local.redis_host_effective
    REDIS_PORT     = var.redis_port
    REDIS_USERNAME = local.redis_username_effective
    REDIS_PASSWORD = local.redis_password_effective
    REDIS_DB       = tostring(var.redis_db)

    # TEI Configuration
    TEI_HOST  = local.tei_host_effective
    TEI_MODEL = var.tei_model

    # RabbitMQ Configuration
    RABBITMQ_HOST         = local.rabbitmq_host_effective
    RABBITMQ_PORT         = var.rabbitmq_port
    RABBITMQ_API_PORT     = var.rabbitmq_api_port
    RABBITMQ_DEFAULT_USER = local.rabbitmq_user_effective
    RABBITMQ_DEFAULT_PASS = local.rabbitmq_password_effective

    # Storage Configuration
    S3_ENDPOINT       = var.s3_endpoint != "" ? var.s3_endpoint : local.config.s3_endpoint
    S3_BUCKET         = local.s3_bucket_name
    S3_ACCESS_KEY     = var.s3_access_key
    S3_SECRET_KEY     = var.s3_secret_key
    S3_REGION         = var.s3_region != "" ? var.s3_region : local.config.s3_region
    S3_PREFIX_PATH    = local.s3_prefix_path_effective
    MINIO_PREFIX_PATH = local.s3_prefix_path_effective

    # Storage Implementation Type (AWS_S3 or OSS)
    # Auto-detect based on cloud_provider or endpoint
    # STORAGE_IMPL: GCS for GCP, OSS for Aliyun, AWS_S3 for others (S3/MinIO)
    STORAGE_IMPL = var.cloud_provider == "gcp" ? "GCS" : (var.cloud_provider == "alicloud" ? "OSS" : "AWS_S3")

    # DeepDoc Service Configuration
    # Point to the deepdoc service for OCR, DLA, and TSR tasks
    DEEPDOC_URL = local.deepdoc_url_effective

    # Secret key for JWT tokens - must be fixed to maintain session across pod restarts
    # Generate with: openssl rand -hex 32
    RAGFLOW_SECRET_KEY = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"

    # Billing Configuration
    BILLING_ENABLED            = var.billing_enabled ? "1" : "0"
    BILLING_STRIPE_API_VERSION = var.billing_stripe_api_version
    BILLING_SERVICE_URL        = var.billing_service_url
    BILLING_STRIPE_API_KEY     = var.billing_stripe_api_key
    BILLING_PRICE_ID_POINTS    = var.billing_price_id_points
    BILLING_PRICE_ID_STORAGE   = var.billing_price_id_storage
    BILLING_PRICE_ID_TRIAL     = var.billing_price_id_trial
    BILLING_PRICE_ID_STARTER   = var.billing_price_id_starter
    BILLING_PRICE_ID_PRO       = var.billing_price_id_pro
    STRIPE_TEST_CLOCK_ID       = var.stripe_test_clock_id

    # Upload size limit for RAGFlow API server (affects file uploads via web UI/API)
    # This is read by common/settings.py to set MAX_CONTENT_LENGTH in Quart/Flask
    # and also by rag/svr/task_executor.py to reject oversized documents before processing
    MAX_CONTENT_LENGTH = local.max_content_length_bytes
  }

  type = "Opaque"

  depends_on = [
    kubernetes_job_v1.shared_mysql_user_bootstrap,
    kubernetes_job_v1.shared_mysql_verify,
    kubernetes_job_v1.shared_elasticsearch_user_bootstrap,
    kubernetes_job_v1.shared_s3_verify,
    kubernetes_job_v1.shared_deepdoc_verify,
  ]
}

# =============================================================================
# RAGFlow Service
# =============================================================================

# =============================================================================
# RAGFlow Services - Split into Separate Services for Independent Health Checks
# =============================================================================
# IMPORTANT: Why split into 3 separate Services instead of one multi-port Service?
#
# Gateway API controllers (GKE, EKS, AKS, NGINX Gateway Fabric, etc.) typically
# generate health checks based on each Service's single port. When a single Service
# exposes multiple ports with different health check URLs, the controller cannot
# differentiate which health check applies to which port.
#
# By splitting into separate Services:
# - Each Service has exactly one port -> controller generates correct health check
# - Each HTTPRoute can independently reference its dedicated Service
# - Health checks work correctly: /v1/healthz for API, / for frontend, etc.
# - This follows the Gateway API best practice: one Service per port/endpoint
#
# References:
# - GKE Gateway: Health checks are generated per Service, not per port
# - NGINX Gateway Fabric: Similar behavior, best with single-port Services
# - Envoy Gateway: Supports per-port health checks but simpler with separate Services
# =============================================================================

# Service 1: Frontend (nginx) - serves React web UI
resource "kubernetes_service_v1" "ragflow_frontend" {
  count = var.deploy_app_stack ? 1 : 0
  metadata {
    name      = "ragflow-frontend"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app = "ragflow"
    }
  }

  spec {
    selector = {
      app = "ragflow"
    }

    port {
      port        = 80
      target_port = 80
      name        = "http"
    }

    type = "ClusterIP"
  }
}

# Service 2: API Server - serves REST API at /v1/*
resource "kubernetes_service_v1" "ragflow_api" {
  count = var.deploy_app_stack ? 1 : 0
  metadata {
    name      = "ragflow-api"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app = "ragflow"
    }

    annotations = {
      # NEG health check interval (requires GKE 1.29+)
      "cloud.google.com/neg-health-check-interval-seconds" = "60"
    }
  }

  spec {
    selector = {
      app = "ragflow"
    }

    port {
      port        = 9380
      target_port = 9380
      name        = "api"
    }

    type = "ClusterIP"
  }
}

# Service 3: Admin Server - serves admin API at /api/v1/admin
resource "kubernetes_service_v1" "admin" {
  count = var.deploy_app_stack ? 1 : 0
  metadata {
    name      = "admin"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app = "ragflow"
    }

    annotations = {
      # NEG health check interval (requires GKE 1.29+)
      "cloud.google.com/neg-health-check-interval-seconds" = "60"
    }
  }

  spec {
    selector = {
      app = "admin"
    }

    port {
      port        = 9381
      target_port = 9381
      name        = "admin"
    }

    type = "ClusterIP"
  }
}

# DEPRECATED: Keeping for backward compatibility reference
# Original multi-port service - no longer used (replaced by above 3 separate services)
# resource "kubernetes_service_v1" "ragflow" { ... }

# =============================================================================
# GKE Managed Prometheus - PodMonitor Resources
# =============================================================================
# PodMonitor tells Google Managed Prometheus (GMP) how to scrape metrics from
# RAGFlow pods. Both ragflow_server (port 9380) and admin_server (port 9381)
# expose Prometheus /metrics endpoints via prometheus_client.
#
# Prerequisites:
#   - GKE cluster must have Managed Prometheus enabled:
#     gcloud container clusters update CLUSTER --enable-managed-prometheus
#   - PodMonitor CRD is auto-installed by GMP
#
# Scrape interval is set to 60s as requested.
# =============================================================================

resource "kubernetes_manifest" "podmonitor_ragflow" {
  count = var.cloud_provider == "gcp" && var.deploy_app_stack ? 1 : 0

  field_manager {
    force_conflicts = true
  }

  manifest = {
    apiVersion = "monitoring.googleapis.com/v1"
    kind       = "PodMonitoring"
    metadata = {
      name      = "ragflow-metrics"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
      labels = {
        app     = "ragflow"
        project = "ragflow"
      }
    }
    spec = {
      selector = {
        matchLabels = {
          app = "ragflow"
        }
      }
      endpoints = [
        {
          port     = "api"
          path     = "/metrics"
          interval = "60s"
        }
      ]
    }
  }
}

resource "kubernetes_manifest" "podmonitor_admin" {
  count = var.cloud_provider == "gcp" && var.deploy_app_stack ? 1 : 0

  field_manager {
    force_conflicts = true
  }

  manifest = {
    apiVersion = "monitoring.googleapis.com/v1"
    kind       = "PodMonitoring"
    metadata = {
      name      = "admin-metrics"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
      labels = {
        app     = "admin"
        project = "ragflow"
      }
    }
    spec = {
      selector = {
        matchLabels = {
          app = "admin"
        }
      }
      endpoints = [
        {
          port     = "admin"
          path     = "/metrics"
          interval = "60s"
        }
      ]
    }
  }
}

# =============================================================================
# RAGFlow Deployment
# =============================================================================

resource "kubernetes_deployment_v1" "ragflow" {
  count = var.deploy_app_stack ? 1 : 0
  depends_on = [
    kubernetes_secret_v1.ragflow_env[0],
    kubernetes_stateful_set_v1.mysql,
    kubernetes_deployment_v1.redis,
    kubernetes_deployment_v1.rabbitmq,
    kubernetes_service_v1.deepdoc,
    terraform_data.wait_for_elasticsearch_secret,
  ]

  metadata {
    name      = "ragflow"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app     = "ragflow"
      project = "ragflow"
    }
  }

  spec {
    replicas = var.ragflow_replicas

    # Limit revision history to reduce orphaned ReplicaSets
    revision_history_limit = 1

    strategy {
      type = "Recreate"
    }

    selector {
      match_labels = {
        app = "ragflow"
      }
    }

    template {
      metadata {
        labels = {
          app     = "ragflow"
          project = "ragflow"
        }
        annotations = {
          # Trigger rollout restart when secret changes
          "checksum/config" = sha256(jsonencode(kubernetes_secret_v1.ragflow_env[0].data))
          # Prometheus scrape annotations (for self-managed Prometheus or GMP annotation-based discovery)
          "prometheus.io/scrape" = "true"
          "prometheus.io/port"   = "9380"
          "prometheus.io/path"   = "/metrics"
        }
      }

      spec {
        # Use default SA (node identity) - node SA has storage.objectCreator for GCS access
        service_account_name = "default"

        # Use imagePullSecrets for GCR authentication (GCP only)
        dynamic "image_pull_secrets" {
          for_each = var.cloud_provider == "gcp" ? [1] : []
          content {
            name = "gcr-image-pull"
          }
        }

        # Init container to create S3 bucket if needed
        dynamic "init_container" {
          for_each = var.s3_endpoint != "" ? [1] : []
          content {
            name    = "init-s3-bucket"
            image   = local.minio_mc_image
            command = ["sh", "-c", "mc alias set myminio ${var.s3_endpoint} ${var.s3_access_key} ${var.s3_secret_key} && mc mb myminio/${local.s3_bucket_name} || exit 0"]
          }
        }

        # Init container to wait for Elasticsearch to be ready
        dynamic "init_container" {
          for_each = local.es_host_effective != "" ? [1] : []
          content {
            name  = "wait-for-elasticsearch"
            image = local.curl_image

            # Inherit environment from ragflow_env secret
            env_from {
              secret_ref {
                name = kubernetes_secret_v1.ragflow_env[0].metadata[0].name
              }
            }

            command = ["sh", "-c", <<-EOT
              until curl -s -k -u "$${ES_USER}:$${ELASTIC_PASSWORD}" "$${ES_PROTOCOL}://$${ES_HOST}:$${ES_PORT}/_cluster/health" | grep -q '"status":"green"\|"status":"yellow"'; do
                echo "Waiting for Elasticsearch at $${ES_HOST}..."
                sleep 5
              done
              echo "Elasticsearch is ready."
              EOT
            ]
          }
        }

        # ES CA certificate volume
        dynamic "volume" {
          for_each = var.mount_elasticsearch_ca_secret ? [1] : []
          content {
            name = "elasticsearch-ca"

            secret {
              secret_name = var.elasticsearch_ca_secret_name
            }
          }
        }

        container {
          name              = "ragflow"
          image             = local.ragflow_image_full
          image_pull_policy = "Always"

          args = ["--disable-taskexecutor"]

          # Frontend port (nginx)
          port {
            container_port = 80
            name           = "http"
          }

          # API port
          port {
            container_port = 9380
            name           = "api"
          }

          # Startup probe: allows slow-starting containers to initialize before liveness/readiness kicks in
          # RAGFlow API server takes significant time to initialize (connecting to MySQL, Redis,
          # Elasticsearch, loading models, etc.). This probe allows up to 900 seconds for startup.
          startup_probe {
            http_get {
              path = "/healthz"
              port = 9380
            }
            initial_delay_seconds = 0
            period_seconds        = 10
            failure_threshold     = 90 # 90 * 10s = 900s (15 min) max startup time for slow environments
          }

          # Liveness probe: lightweight check - just returns 200 OK without checking dependencies
          # Note: startupProbe gates liveness/readiness, so they only start after startup succeeds
          liveness_probe {
            http_get {
              path = "/live"
              port = 9380
            }
            initial_delay_seconds = 0
            period_seconds        = 30
            timeout_seconds       = 5
            failure_threshold     = 2
          }

          # =============================================================================
          # IMPORTANT: GKE NEG Readiness Gate Behavior
          # =============================================================================
          #
          # When using container-native load balancing (NEG) with GKE Gateway/Ingress,
          # GKE automatically injects a readiness gate into Pod spec:
          #   readinessGates:
          #     - conditionType: cloud.google.com/load-balancer-neg-ready
          #
          # Pod Ready State = Container Readiness Probe Success AND All Readiness Gates True
          #
          # How it works:
          #   1. NEG controller creates a NetworkEndpointGroup for the Service
          #   2. Pod's IP gets registered to NEG when Pod is scheduled
          #   3. NEG must attach to BackendService with health check configured
          #   4. Health check uses Readiness Probe path (/healthz on port 9380)
          #   5. NEG controller sets cloud.google.com/load-balancer-neg-ready = True
          #   6. Only then Pod.status.conditions Ready = True
          #
          # Impact:
          #   - Pod may show "Running" but "Ready 0/1" for 1-5 minutes after container starts
          #   - This delay is due to NEG sync + LB health check convergence
          #   - Common in GKE Autopilot, large clusters, or first-time deployment
          #   - This does NOT affect actual container functionality, only traffic routing
          #
          # Timeline example:
          #   T+0s:    Pod created
          #   T+10s:   Container Running ( Readiness Probe passes)
          #   T+30s:   NEG endpoint registered
          #   T+60s:   NEG attached to BackendService + LB health check passes
          #   T+60s:   NEG readiness gate = True -> Pod Ready = True
          #
          # Reference:
          #   - https://cloud.google.com/kubernetes-engine/docs/concepts/ingress-xlb#neg
          #   - https://cloud.google.com/kubernetes-engine/docs/how-to/container-native-load-balancing
          #   - https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#pod-readiness-gate
          # =============================================================================

          # Readiness probe for GKE Gateway health check
          # Note: startupProbe gates this, so it only starts after startup succeeds
          readiness_probe {
            http_get {
              path = "/healthz"
              port = 9380
            }
            initial_delay_seconds = 0
            period_seconds        = 30
            timeout_seconds       = 5
            failure_threshold     = 2
          }

          env_from {
            secret_ref {
              name = kubernetes_secret_v1.ragflow_env[0].metadata[0].name
            }
          }

          # Mount ES CA certificate for HTTPS verification
          dynamic "volume_mount" {
            for_each = var.mount_elasticsearch_ca_secret ? [1] : []
            content {
              name       = "elasticsearch-ca"
              mount_path = "/etc/elasticsearch/certs"
              read_only  = true
            }
          }

          resources {
            requests = {
              cpu    = var.ragflow_cpu_request
              memory = var.ragflow_memory_request
            }
            limits = {
              cpu    = var.ragflow_cpu_limit
              memory = var.ragflow_memory_limit
            }
          }

          # Add SYS_PTRACE capability for austin/py-spy profiler
          security_context {
            capabilities {
              add = ["SYS_PTRACE"]
            }
          }
        }
      }
    }
  }

  # Increase timeout to accommodate slow startup (startup_probe allows up to 15 min)
  # Default kubernetes provider timeout is ~10 min, which causes timeout when startup is slow
  timeouts {
    create = "20m"
    update = "20m"
    delete = "10m"
  }
}

# =============================================================================
# Admin Deployment
# =============================================================================

resource "kubernetes_deployment_v1" "admin" {
  count = var.deploy_app_stack ? 1 : 0
  depends_on = [
    kubernetes_secret_v1.ragflow_env[0],
    kubernetes_stateful_set_v1.mysql,
    kubernetes_deployment_v1.redis,
    kubernetes_deployment_v1.rabbitmq,
    kubernetes_service_v1.deepdoc,
    terraform_data.wait_for_elasticsearch_secret,
  ]

  metadata {
    name      = "admin"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app     = "admin"
      project = "ragflow"
    }
  }

  spec {
    # Always 1 replica as requested
    replicas = 1

    # Limit revision history to reduce orphaned ReplicaSets
    revision_history_limit = 1

    strategy {
      type = "Recreate"
    }

    selector {
      match_labels = {
        app = "admin"
      }
    }

    template {
      metadata {
        labels = {
          app     = "admin"
          project = "ragflow"
        }
        annotations = {
          # Trigger rollout restart when secret changes
          "checksum/config" = sha256(jsonencode(kubernetes_secret_v1.ragflow_env[0].data))
          # Prometheus scrape annotations (for self-managed Prometheus or GMP annotation-based discovery)
          "prometheus.io/scrape" = "true"
          "prometheus.io/port"   = "9381"
          "prometheus.io/path"   = "/metrics"
        }
      }

      spec {
        # Use default SA (node identity) - node SA has storage.objectCreator for GCS access
        service_account_name = "default"

        # Use imagePullSecrets for GCR authentication (GCP only)
        dynamic "image_pull_secrets" {
          for_each = var.cloud_provider == "gcp" ? [1] : []
          content {
            name = "gcr-image-pull"
          }
        }

        # ES CA certificate volume
        dynamic "volume" {
          for_each = var.mount_elasticsearch_ca_secret ? [1] : []
          content {
            name = "elasticsearch-ca"

            secret {
              secret_name = var.elasticsearch_ca_secret_name
            }
          }
        }

        container {
          name              = "admin"
          image             = local.ragflow_image_full
          image_pull_policy = "Always"

          args = ["--disable-webserver", "--disable-taskexecutor", "--disable-datasync", "--enable-adminserver"]

          # Admin port
          port {
            container_port = 9381
            name           = "admin"
          }

          # Startup probe: allows slow-starting containers to initialize before liveness/readiness kicks in
          # RAGFlow admin takes ~2-3 minutes to start (loading configs, connecting to DB/Redis/ES)
          startup_probe {
            http_get {
              path = "/live"
              port = 9381
            }
            initial_delay_seconds = 0
            period_seconds        = 10
            failure_threshold     = 30 # 30 * 10s = 300s max startup time
          }

          # Liveness probe: lightweight check - just returns 200 OK without checking dependencies
          # Note: startupProbe gates liveness/readiness, so they only start after startup succeeds
          liveness_probe {
            http_get {
              path = "/live"
              port = 9381
            }
            initial_delay_seconds = 0
            period_seconds        = 30
            timeout_seconds       = 5
            failure_threshold     = 2
          }

          # Standard ragflow environment variables
          env_from {
            secret_ref {
              name = kubernetes_secret_v1.ragflow_env[0].metadata[0].name
            }
          }

          # Mount ES CA certificate for HTTPS verification
          dynamic "volume_mount" {
            for_each = var.mount_elasticsearch_ca_secret ? [1] : []
            content {
              name       = "elasticsearch-ca"
              mount_path = "/etc/elasticsearch/certs"
              read_only  = true
            }
          }

          resources {
            requests = {
              cpu    = var.admin_cpu_request
              memory = var.admin_memory_request
            }
            limits = {
              cpu    = var.admin_cpu_limit
              memory = var.admin_memory_limit
            }
          }

          # Add SYS_PTRACE capability for austin/py-spy profiler
          security_context {
            capabilities {
              add = ["SYS_PTRACE"]
            }
          }
        }
      }
    }
  }
}

# =============================================================================
# Parser Deployments — one per task type (common, graphrag, raptor, resume)
# Each pod runs a single task_executor consumer that monitors both priority
# queues (te.1.<type> and te.0.<type>) via priority_queue_consumer.
# =============================================================================

locals {
  parser_types = ["common", "graphrag", "raptor", "resume"]
}

resource "kubernetes_deployment_v1" "parser" {
  depends_on = [
    kubernetes_secret_v1.ragflow_env[0],
    kubernetes_stateful_set_v1.mysql,
    kubernetes_deployment_v1.redis,
    kubernetes_deployment_v1.rabbitmq,
    kubernetes_service_v1.deepdoc,
    terraform_data.wait_for_elasticsearch_secret,
  ]

  for_each = var.deploy_app_stack ? toset(local.parser_types) : toset([])

  metadata {
    name      = "parser-${each.key}"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app       = "parser-${each.key}"
      component = "parser"
      task-type = each.key
      project   = "ragflow"
    }
  }

  spec {
    replicas = var.parser_replicas[each.key]

    # Limit revision history to reduce orphaned ReplicaSets
    revision_history_limit = 1

    strategy {
      type = "Recreate"
    }

    selector {
      match_labels = {
        app       = "parser-${each.key}"
        component = "parser"
        task-type = each.key
      }
    }

    template {
      metadata {
        labels = {
          app       = "parser-${each.key}"
          component = "parser"
          task-type = each.key
          project   = "ragflow"
        }
        annotations = {
          # Trigger rollout restart when secret changes
          "checksum/config" = sha256(jsonencode(kubernetes_secret_v1.ragflow_env[0].data))
        }
      }

      spec {
        # Use default SA (node identity) - node SA has storage.objectCreator for GCS access
        service_account_name = "default"

        # Use imagePullSecrets for GCR authentication (GCP only)
        dynamic "image_pull_secrets" {
          for_each = var.cloud_provider == "gcp" ? [1] : []
          content {
            name = "gcr-image-pull"
          }
        }

        # Init container to wait for Elasticsearch to be ready
        dynamic "init_container" {
          for_each = local.es_host_effective != "" ? [1] : []
          content {
            name  = "wait-for-elasticsearch"
            image = local.curl_image

            # Inherit environment from ragflow_env secret
            env_from {
              secret_ref {
                name = kubernetes_secret_v1.ragflow_env[0].metadata[0].name
              }
            }

            command = ["sh", "-c", <<-EOT
              until curl -s -k -u "$${ES_USER}:$${ELASTIC_PASSWORD}" "$${ES_PROTOCOL}://$${ES_HOST}:$${ES_PORT}/_cluster/health" | grep -q '"status":"green"\|"status":"yellow"'; do
                echo "Waiting for Elasticsearch at $${ES_HOST}..."
                sleep 5
              done
              echo "Elasticsearch is ready."
              EOT
            ]
          }
        }

        # ES CA certificate volume
        dynamic "volume" {
          for_each = var.mount_elasticsearch_ca_secret ? [1] : []
          content {
            name = "elasticsearch-ca"

            secret {
              secret_name = var.elasticsearch_ca_secret_name
            }
          }
        }

        container {
          name              = "parser-${each.key}"
          image             = local.ragflow_image_full
          image_pull_policy = "Always"

          command = ["/ragflow/entrypoint-parser.sh"]

          port {
            container_port = 9380
            name           = "http"
          }

          env_from {
            secret_ref {
              name = kubernetes_secret_v1.ragflow_env[0].metadata[0].name
            }
          }

          env {
            name  = "PARSER_TYPE"
            value = each.key
          }

          # Mount ES CA certificate for HTTPS verification
          dynamic "volume_mount" {
            for_each = var.mount_elasticsearch_ca_secret ? [1] : []
            content {
              name       = "elasticsearch-ca"
              mount_path = "/etc/elasticsearch/certs"
              read_only  = true
            }
          }

          readiness_probe {
            exec {
              command = ["/bin/bash", "-c", "ps aux | grep '[r]ag/svr/task_executor.py.*-t ${each.key}'"]
            }
            initial_delay_seconds = 20
            period_seconds        = 10
          }

          liveness_probe {
            exec {
              command = ["/bin/bash", "-c", "ps aux | grep '[r]ag/svr/task_executor.py.*-t ${each.key}'"]
            }
            initial_delay_seconds = 60
            period_seconds        = 20
          }

          # Add SYS_PTRACE capability for austin profiler
          security_context {
            capabilities {
              add = ["SYS_PTRACE"]
            }
          }

          resources {
            requests = {
              cpu    = var.parser_cpu_request
              memory = var.parser_memory_request
            }
            limits = {
              cpu    = var.parser_cpu_limit
              memory = var.parser_memory_limit
            }
          }
        }
      }
    }
  }
}

# =============================================================================
# DeepDoc Deployment
# =============================================================================

resource "kubernetes_deployment_v1" "deepdoc" {
  count = var.deploy_infra ? 1 : 0
  metadata {
    name      = "deepdoc"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name

    labels = {
      app = "deepdoc"
    }

    annotations = {
      "ragflow/deepdoc-hardware" = var.deepdoc_use_gpu ? "gpu" : "cpu"
    }
  }

  spec {
    replicas = var.deepdoc_replicas

    # Limit revision history to reduce orphaned ReplicaSets
    revision_history_limit = 1

    selector {
      match_labels = {
        app = "deepdoc"
      }
    }

    strategy {
      type = "Recreate"
    }

    template {
      metadata {
        labels = {
          app = "deepdoc"
        }

        annotations = {
          "ragflow/deepdoc-hardware" = var.deepdoc_use_gpu ? "gpu" : "cpu"
        }
      }

      spec {
        service_account_name = "default"

        # Use imagePullSecrets for GCR authentication (GCP only)
        dynamic "image_pull_secrets" {
          for_each = var.cloud_provider == "gcp" ? [1] : []
          content {
            name = "gcr-image-pull"
          }
        }

        # Container
        container {
          name              = "deepdoc"
          image             = local.deepdoc_image_full
          image_pull_policy = "Always"

          port {
            container_port = 8000
            name           = "http"
          }

          # Startup probe: allows slow-starting container to initialize before liveness/readiness kicks in
          # DeepDoc loads OCR, DLA, and TSR models which may take time, especially on GPU
          startup_probe {
            http_get {
              path = "/health"
              port = 8000
            }
            initial_delay_seconds = 0
            period_seconds        = 10
            failure_threshold     = 30 # 30 * 10s = 300s max startup time
          }

          # Readiness probe: check if server is ready to accept requests
          readiness_probe {
            http_get {
              path = "/health"
              port = 8000
            }
            initial_delay_seconds = 0
            period_seconds        = 10
            timeout_seconds       = 5
            failure_threshold     = 3
          }

          # Liveness probe: check if server is still running
          liveness_probe {
            http_get {
              path = "/health"
              port = 8000
            }
            initial_delay_seconds = 0
            period_seconds        = 20
            timeout_seconds       = 5
            failure_threshold     = 5
          }

          # GPU-specific environment variables
          dynamic "env" {
            for_each = var.deepdoc_use_gpu ? [1] : []
            content {
              name  = "NVIDIA_VISIBLE_DEVICES"
              value = "all"
            }
          }

          dynamic "env" {
            for_each = var.deepdoc_use_gpu ? [1] : []
            content {
              name  = "NVIDIA_DRIVER_CAPABILITIES"
              value = "compute,utility"
            }
          }

          resources {
            requests = {
              cpu    = var.deepdoc_cpu_request
              memory = var.deepdoc_memory_request
            }
            limits = {
              cpu    = var.deepdoc_cpu_limit
              memory = var.deepdoc_memory_limit
            }
          }

          # Add SYS_PTRACE capability for austin/py-spy profiler
          security_context {
            capabilities {
              add = ["SYS_PTRACE"]
            }
          }
        }
      }
    }
  }
}

resource "kubernetes_service_v1" "deepdoc" {
  count = var.deploy_infra ? 1 : 0
  metadata {
    name      = "deepdoc"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  spec {
    selector = {
      app = "deepdoc"
    }

    port {
      name        = "http"
      port        = 8000
      target_port = 8000
    }
  }
}

# =============================================================================
# Gateway API
# =============================================================================

resource "kubernetes_manifest" "gateway" {
  count = var.deploy_app_stack ? 1 : 0
  depends_on = [
    kubernetes_secret_v1.tls_secret,
  ]

  field_manager {
    force_conflicts = true
  }

  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "Gateway"
    metadata = {
      name      = "ragflow"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
      labels = {
        app = "ragflow"
      }
      annotations = {
        # For ohttps pull mode: TLS is managed via kubernetes.io/tls Secret
        # The TLS secret will be synced by sync_ohttps_cert.py script
      }
    }
    spec = {
      gatewayClassName = local.gateway_class_name
      listeners = concat(
        [
          {
            name     = "http"
            protocol = "HTTP"
            port     = 80
            # Note: GKE Gateway does not support httpRedirect
            # When ohttps is enabled, HTTP (80) and HTTPS (443) both work
            # Users can access via HTTPS directly, or use the HTTPS URL
            allowedRoutes = {
              namespaces = {
                selector = {
                  matchLabels = {
                    "kubernetes.io/metadata.name" = kubernetes_namespace_v1.ragflow.metadata[0].name
                  }
                }
              }
            }
          }
        ],
        var.ohttps_enabled ? [
          {
            name     = "https"
            protocol = "HTTPS"
            port     = 443
            tls = {
              mode = "Terminate"
              certificateRefs = [
                {
                  name      = "ragflow-tls"
                  namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
                  kind      = "Secret"
                }
              ]
            }
            allowedRoutes = {
              namespaces = {
                selector = {
                  matchLabels = {
                    "kubernetes.io/metadata.name" = kubernetes_namespace_v1.ragflow.metadata[0].name
                  }
                }
              }
            }
          }
        ] : []
      )
    }
  }

  # Wait for Gateway to be programmed (status.addresses available)
  # Works with GKE Gateway, NGINX Gateway Fabric, and other Gateway API implementations
  # Note: This ensures the Gateway is ready but does not store status in terraform state
  wait {
    condition {
      type   = "Programmed"
      status = "True"
    }
  }
}

# =============================================================================
# TLS Secret (for ohttps pull mode)
# Use local certificate files in the current byok directory when ohttps is enabled.

resource "kubernetes_secret_v1" "tls_secret" {
  count = var.deploy_app_stack && var.ohttps_enabled ? 1 : 0

  metadata {
    name      = "ragflow-tls"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
    labels = {
      app          = "ragflow"
      "managed-by" = "terraform"
    }
  }

  type = "kubernetes.io/tls"

  data = var.ohttps_enabled ? {
    "tls.crt" = file("${path.module}/ragflow-tls.crt")
    "tls.key" = file("${path.module}/ragflow-tls.key")
  } : {}
}

# =============================================================================
# ohttps Sync ServiceAccount (for CronJob)
# =============================================================================

resource "kubernetes_manifest" "ohttps_sync_sa" {
  count = 0

  manifest = {
    apiVersion = "v1"
    kind       = "ServiceAccount"
    metadata = {
      name      = "ohttps-sync-sa"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
      labels = {
        app = "ragflow"
      }
    }
  }
}

resource "kubernetes_manifest" "ohttps_sync_role" {
  count = 0

  manifest = {
    apiVersion = "rbac.authorization.k8s.io/v1"
    kind       = "Role"
    metadata = {
      name      = "ohttps-sync-role"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
    }
    rules = [
      {
        apiGroups = [""]
        resources = ["secrets"]
        verbs     = ["get", "list", "create", "update", "patch"]
      }
    ]
  }
}

resource "kubernetes_manifest" "ohttps_sync_rolebinding" {
  count = 0

  manifest = {
    apiVersion = "rbac.authorization.k8s.io/v1"
    kind       = "RoleBinding"
    metadata = {
      name      = "ohttps-sync-rolebinding"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
    }
    subjects = [
      {
        kind      = "ServiceAccount"
        name      = "ohttps-sync-sa"
        namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
      }
    ]
    roleRef = {
      kind     = "Role"
      name     = "ohttps-sync-role"
      apiGroup = "rbac.authorization.k8s.io"
    }
  }
}

# =============================================================================
# ohttps Sync CronJob
# =============================================================================

resource "kubernetes_manifest" "ohttps_sync_cronjob" {
  count = 0

  manifest = {
    apiVersion = "batch/v1"
    kind       = "CronJob"
    metadata = {
      name      = "ohttps-cert-sync"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
      labels = {
        app          = "ragflow"
        "managed-by" = "terraform"
      }
    }
    spec = {
      schedule                   = "0 3 * * 1"
      successfulJobsHistoryLimit = 3
      failedJobsHistoryLimit     = 3
      jobTemplate = {
        spec = {
          template = {
            metadata = {
              labels = {
                app = "ohttps-cert-sync"
              }
            }
            spec = {
              restartPolicy      = "OnFailure"
              serviceAccountName = "ohttps-sync-sa"
              containers = [
                {
                  name            = "sync"
                  image           = var.ohttps_sync_image
                  imagePullPolicy = "Always"
                  env = [
                    {
                      name  = "OHTTPS_API_ID"
                      value = var.ohttps_api_id
                    },
                    {
                      name  = "OHTTPS_API_KEY"
                      value = var.ohttps_api_key
                    },
                    {
                      name  = "OHTTPS_CERT_ID"
                      value = var.ohttps_cert_id
                    },
                    {
                      name  = "SYNC_K8S_SECRET"
                      value = "1"
                    },
                    {
                      name  = "SECRET_NAMESPACE"
                      value = local.ragflow_namespace
                    },
                    {
                      name  = "SECRET_NAME"
                      value = "ragflow-tls"
                    }
                  ]
                }
              ]
            }
          }
        }
      }
    }
  }
}

# =============================================================================
# HTTPRoute Resources
# =============================================================================
# WORKAROUND for Aliyun ALB bug: Currently, an HTTPRoute that references multiple
# ports of the same service causes server group registration failures. The Aliyun
# team will fix this in a future release. The temporary workaround is to create
# separate HTTPRoutes for different ports of the same service.

# HTTPRoute 1: /v1 and /api -> port 9380 (API service)
# Note: /api/v1/admin is excluded and handled by http_route_admin
resource "kubernetes_manifest" "http_route_api" {
  count = var.deploy_app_stack ? 1 : 0
  field_manager {
    force_conflicts = true
  }
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "HTTPRoute"
    metadata = {
      name      = "ragflow-http-route-api"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
      labels = {
        app = "ragflow"
      }
    }
    spec = {
      parentRefs = [
        {
          name        = "ragflow"
          namespace   = kubernetes_namespace_v1.ragflow.metadata[0].name
          kind        = "Gateway"
          sectionName = var.ohttps_enabled ? "https" : "http"
        }
      ]
      rules = [
        {
          matches = [
            {
              path = {
                type  = "PathPrefix"
                value = "/v1"
              }
            }
          ]
          backendRefs = [
            {
              name = kubernetes_service_v1.ragflow_api[0].metadata[0].name
              port = 9380
            }
          ]
          # NOTE: Do NOT use HTTPRoute timeouts {} here.
          # gke-l7-regional-external-managed does NOT support the
          # Gateway API timeouts field (GWCER104: "timeouts are not
          # supported").  Including it causes the entire HTTPRoute to be
          # rejected (Accepted=false), which also prevents the
          # GCPBackendPolicy from taking effect.
          # Use GCPBackendPolicy.spec.default.timeoutSec instead (below).
        },
        {
          matches = [
            {
              path = {
                type  = "PathPrefix"
                value = "/api"
              }
            }
          ]
          backendRefs = [
            {
              name = kubernetes_service_v1.ragflow_api[0].metadata[0].name
              port = 9380
            }
          ]
        }
      ]
    }
  }

  lifecycle {
    replace_triggered_by = [kubernetes_manifest.http_route_admin[0]]
  }
}

# NGINX Gateway Fabric policy: allow larger request bodies for API uploads
resource "kubernetes_manifest" "upload_size_policy" {
  count = var.deploy_app_stack && !local.is_gke_gateway ? 1 : 0

  field_manager {
    force_conflicts = true
  }

  manifest = {
    apiVersion = "gateway.nginx.org/v1alpha1"
    kind       = "ClientSettingsPolicy"
    metadata = {
      name      = "ragflow-upload-size"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
    }
    spec = {
      targetRef = {
        group = "gateway.networking.k8s.io"
        kind  = "Gateway"
        name  = "ragflow"
      }
      body = {
        maxSize = "1000m"
      }
    }
  }

  depends_on = [kubernetes_manifest.gateway]
}

# GCPBackendPolicy: Configure backend service timeout for GKE regional external ALB.
# Note: GKE Gateway does not support body size limits via any native API.
# Upload size is enforced at the Python application layer via MAX_CONTENT_LENGTH env var
# (set in ragflow_env Secret -> common/settings.py -> Quart/Flask).
# GKE default Cloud LB request limit (~32MB) applies when Python limit is not reached.
resource "kubernetes_manifest" "gcp_backend_policy_api" {
  count = var.deploy_app_stack && local.is_gke_gateway ? 1 : 0

  field_manager {
    force_conflicts = true
  }

  manifest = {
    apiVersion = "networking.gke.io/v1"
    kind       = "GCPBackendPolicy"
    metadata = {
      name      = "ragflow-api-backend-policy"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
    }
    spec = {
      default = {
        # 24 hours – effectively unlimited for long-running SSE workflows.
        # GCP does not support a true "unlimited" value; 0 means "use default (30s)".
        timeoutSec = 86400
      }
      targetRef = {
        group = ""
        kind  = "Service"
        name  = kubernetes_service_v1.ragflow_api[0].metadata[0].name
      }
    }
  }
}

# HTTPRoute 2: /api/v1/admin -> port 9381 (admin service)
resource "kubernetes_manifest" "http_route_admin" {
  count = var.deploy_app_stack ? 1 : 0
  field_manager {
    force_conflicts = true
  }
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "HTTPRoute"
    metadata = {
      name      = "ragflow-http-route-admin"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
      labels = {
        app = "ragflow"
      }
    }
    spec = {
      parentRefs = [
        {
          name        = "ragflow"
          namespace   = kubernetes_namespace_v1.ragflow.metadata[0].name
          kind        = "Gateway"
          sectionName = var.ohttps_enabled ? "https" : "http"
        }
      ]
      rules = [
        {
          matches = [
            {
              path = {
                type  = "PathPrefix"
                value = "/api/v1/admin"
              }
            }
          ]
          backendRefs = [
            {
              name = kubernetes_service_v1.admin[0].metadata[0].name
              port = 9381
            }
          ]
        }
      ]
    }
  }
}

# HTTPRoute 3: / (root path) -> port 80 (frontend nginx)
resource "kubernetes_manifest" "http_route_frontend" {
  count = var.deploy_app_stack ? 1 : 0
  field_manager {
    force_conflicts = true
  }
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "HTTPRoute"
    metadata = {
      name      = "ragflow-http-route-frontend"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
      labels = {
        app = "ragflow"
      }
    }
    spec = {
      parentRefs = [
        {
          name        = "ragflow"
          namespace   = kubernetes_namespace_v1.ragflow.metadata[0].name
          kind        = "Gateway"
          sectionName = var.ohttps_enabled ? "https" : "http"
        }
      ]
      rules = [
        {
          matches = [
            {
              path = {
                type  = "PathPrefix"
                value = "/"
              }
            }
          ]
          backendRefs = [
            {
              name = kubernetes_service_v1.ragflow_frontend[0].metadata[0].name
              port = 80
            }
          ]
        }
      ]
    }
  }
}

# =============================================================================
# HTTPRoute for HTTP to HTTPS Redirect (only when ohttps is enabled)
# =============================================================================
# When ohttps is enabled, redirect HTTP requests to HTTPS
# Uses RequestRedirect filter to return 308 permanent redirect
# =============================================================================
resource "kubernetes_manifest" "http_redirect" {
  count = var.deploy_app_stack && var.ohttps_enabled ? 1 : 0

  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "HTTPRoute"
    metadata = {
      name      = "ragflow-http-redirect"
      namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
      labels = {
        app = "ragflow"
      }
    }
    spec = {
      parentRefs = [
        {
          name        = "ragflow"
          namespace   = kubernetes_namespace_v1.ragflow.metadata[0].name
          kind        = "Gateway"
          sectionName = "http"
        }
      ]
      rules = [
        {
          filters = [
            {
              type = "RequestRedirect"
              requestRedirect = {
                scheme     = "https"
                statusCode = 301
              }
            }
          ]
        }
      ]
    }
  }
}

# =============================================================================

# Get NGINX Gateway Fabric service for on-premises deployments
data "kubernetes_service_v1" "gateway_fabric" {
  count = var.deploy_app_stack && !local.is_gke_gateway ? 1 : 0

  metadata {
    name      = "nginx-gateway-nginx-gateway-fabric"
    namespace = "nginx-gateway"
  }

  # This will fail if the service doesn't exist, which is OK
  # We'll handle the error in the output
}

# =============================================================================
# Outputs
# =============================================================================

# Get Gateway IP using external data source (calls kubectl)
# Get Gateway IP using external data source (only for GCP/GKE environment)
# =============================================================================
# Get Gateway IP - Workaround for kubernetes_manifest not syncing status to state
# =============================================================================
# The kubernetes_manifest resource does NOT sync the 'status' field to terraform state,
# even when using the 'wait' block. This is a known limitation:
# - GitHub Issue: https://github.com/hashicorp/terraform-provider-kubernetes/issues/1886
# - GitHub Issue: https://github.com/hashicorp/terraform-provider-kubernetes/issues/2336
#
# The 'wait' block ensures the Gateway is programmed (IP assigned), but it does NOT
# update the terraform state with status.addresses. Therefore, we use the external
# data source to dynamically fetch the IP via kubectl after the Gateway is ready.
#
# This workaround is only needed for cloud providers (GKE) where the Gateway IP is
# allocated dynamically. For on-premises with NGINX Gateway Fabric, the IP is
# obtained from the LoadBalancer service (see fallback in output).
# =============================================================================

# Only create this when using GKE Gateway (gke-*)
# GKE Gateway Controller stores the assigned IP in metadata.annotations.networking.gke.io/addresses,
# NOT in status.addresses (a known GKE behavior). Extract region and address name from the annotation
# path to query the actual IP from GCP.
data "external" "gateway_ip" {
  count = var.deploy_app_stack && local.is_gke_gateway ? 1 : 0
  program = ["sh", "-c", <<-EOF
    export KUBECONFIG="${pathexpand(var.kubeconfig_path)}"
    kubectl get gateway ragflow -n ${local.ragflow_namespace} -o jsonpath={.status.addresses[0].value} 2>/dev/null | jq -R -s -c '{address: .}'
  EOF
  ]

  depends_on = [kubernetes_manifest.gateway]
}

# Get Gateway IP for on-premises (non-GKE) deployments
# Use Gateway CR status instead of LoadBalancer service IP (they may differ)
data "external" "nginx_gateway_ip" {
  count = var.deploy_app_stack && !local.is_gke_gateway ? 1 : 0
  program = ["sh", "-c", <<-EOF
    export KUBECONFIG="${pathexpand(var.kubeconfig_path)}"
    # Get IP from Gateway CR status (more accurate than service IP)
    ip=$(kubectl get gateway ragflow -n ${local.ragflow_namespace} -o jsonpath='{.status.addresses[0].value}' 2>/dev/null)
    if [ -n "$ip" ]; then
      echo "{\"address\": \"$ip\"}"
    else
      # Fallback to service IP
      ip=$(kubectl get svc nginx-gateway-nginx-gateway-fabric -n nginx-gateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null)
      if [ -n "$ip" ]; then
        echo "{\"address\": \"$ip\"}"
      else
        hostname=$(kubectl get svc nginx-gateway-nginx-gateway-fabric -n nginx-gateway -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null)
        if [ -n "$hostname" ]; then
          echo "{\"address\": \"$hostname\"}"
        else
          echo '{"address": "pending"}'
        fi
      fi
    fi
  EOF
  ]

  depends_on = [kubernetes_manifest.gateway]
}

# ConfigMap to store Gateway address
# Uses count-based resources to support both GKE and smk (nginx gateway)
# This avoids ternary expression in map value which is not supported by Terraform
resource "kubernetes_config_map_v1" "gateway_address_nginx" {
  count      = var.deploy_app_stack && !local.is_gke_gateway ? 1 : 0
  depends_on = [kubernetes_manifest.gateway, data.external.nginx_gateway_ip]

  metadata {
    name      = "ragflow-gateway-address"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  data = {
    "gateway_address" = data.external.nginx_gateway_ip[0].result.address
  }
}

resource "kubernetes_config_map_v1" "gateway_address_gke" {
  count      = var.deploy_app_stack && local.is_gke_gateway ? 1 : 0
  depends_on = [kubernetes_manifest.gateway, data.external.gateway_ip]

  metadata {
    name      = "ragflow-gateway-address"
    namespace = kubernetes_namespace_v1.ragflow.metadata[0].name
  }

  data = {
    "gateway_address" = data.external.gateway_ip[0].result.address
  }
}

# Output uses local variable to support both GKE and smk
output "gateway_address" {
  description = "Gateway IP address or hostname"
  value       = var.deploy_app_stack ? (local.is_gke_gateway ? data.external.gateway_ip[0].result.address : data.external.nginx_gateway_ip[0].result.address) : ""
}
