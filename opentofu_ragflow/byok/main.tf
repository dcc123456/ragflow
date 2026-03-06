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
  required_version = ">= 1.8.0"

  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "2.38.0"
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

# =============================================================================
# Local Values
# =============================================================================

locals {
  # Construct GCS service account from project ID (only when cloud_provider = 'gcp')
  # Format: ragflow-gcs@{gcp_project_id}.iam.gserviceaccount.com
  gcs_service_account = var.cloud_provider == "gcp" && var.gcp_project_id != "" ? "ragflow-gcs@${var.gcp_project_id}.iam.gserviceaccount.com" : ""

  # Cloud provider auto-configuration
  # Detect StorageClass and S3 settings based on cloud_provider
  cloud_config = {
    smk = {
      storage_class = var.storage_class != "" ? var.storage_class : "rook-ceph-block"
      s3_endpoint   = var.s3_endpoint != "" ? var.s3_endpoint : "http://rook-ceph-rgw-my-store.rook-ceph.svc:80"
      s3_region     = var.s3_region != "" ? var.s3_region : "default"
    }
    gcp = {
      # Note: s3_region is not used by GCS client (rag/utils/gcs_conn.py), but kept for consistency
      storage_class = var.storage_class != "" ? var.storage_class : "standard-rwo"
      s3_endpoint   = var.s3_endpoint != "" ? var.s3_endpoint : "https://storage.googleapis.com"
      s3_region     = var.s3_region != "" ? var.s3_region : "us-central1"
    }
    aws = {
      storage_class = var.storage_class != "" ? var.storage_class : "gp3"
      s3_endpoint   = var.s3_endpoint != "" ? var.s3_endpoint : "https://s3.amazonaws.com"
      s3_region     = var.s3_region != "" ? var.s3_region : "us-east-1"
    }
    azure = {
      storage_class = var.storage_class != "" ? var.storage_class : "default"
      s3_endpoint   = var.s3_endpoint != "" ? var.s3_endpoint : "https://${var.storage_account_name}.blob.core.windows.net"
      s3_region     = var.s3_region != "" ? var.s3_region : "eastus"
    }
    alicloud = {
      storage_class = var.storage_class != "" ? var.storage_class : "alicloud-disk-ssd"
      s3_endpoint   = var.s3_endpoint != "" ? var.s3_endpoint : "https://oss-${var.region}.aliyuncs.com"
      s3_region     = var.s3_region != "" ? var.s3_region : "cn-hangzhou"
    }
  }

  # Get configuration for selected cloud provider
  config = local.cloud_config[var.cloud_provider]

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
  mysql_image     = var.public_registry != "" ? "${var.public_registry}/mysql:8.0" : "docker.io/library/mysql:8.0"
  redis_image    = var.public_registry != "" ? "${var.public_registry}/valkey:8" : "valkey/valkey:8"
  tei_image      = var.public_registry != "" ? "${var.public_registry}/text-embeddings-inference:cpu-1.8" : "infiniflow/text-embeddings-inference:cpu-1.8"
  rabbitmq_image = var.public_registry != "" ? "${var.public_registry}/rabbitmq:4-management" : "rabbitmq:4-management"
  curl_image     = var.public_registry != "" ? "${var.public_registry}/curl:latest" : "curlimages/curl:latest"
  minio_mc_image = var.public_registry != "" ? "${var.public_registry}/mc:latest" : "quay.io/minio/mc:latest"

  # Elasticsearch version extracted from es_image
  # Extracts version from format like "elasticsearch:9.3.1" -> "9.3.1"
  # If no tag is provided (image without :tag), defaults to "latest"
  es_version = can(regex(".*:(.+)$", var.es_image)) ? regex(".*:(.+)$", var.es_image)[0] : "latest"

  # Check if using GKE Gateway (vs smk with NGINX Gateway)
  is_gke_gateway = can(regex("^gke-", var.gateway_class_name))

  # Database users (consistent across all components)
  mysql_user     = "ragflow"
  rabbitmq_user = "ragflow"
}

# =============================================================================
# Resources
# =============================================================================


resource "kubernetes_namespace" "ragflow" {
  metadata {
    name = var.namespace
  }

  # Allow namespace to already exist (idempotent)
  lifecycle {
    ignore_changes = [metadata]
  }
}

# =============================================================================
# GCS Service Account (for Workload Identity when cloud_provider = 'gcp')
# =============================================================================

resource "kubernetes_service_account" "ragflow_gcs" {
  count = var.cloud_provider == "gcp" ? 1 : 0

  metadata {
    name      = "ragflow-gcs"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
    annotations = {
      "iam.gke.io/gcp-service-account" = local.gcs_service_account
    }
  }

  lifecycle {
    # Allow service account to already exist (idempotent)
    ignore_changes = [metadata]
  }
}

# =============================================================================
# MySQL Deployment (K8s Mode)
# =============================================================================

resource "random_password" "mysql" {
  length  = 16
  special = false
}

resource "random_password" "redis" {
  length  = 16
  special = false
}

resource "random_password" "rabbitmq" {
  length  = 16
  special = false
}

# Ref: https://github.com/hashicorp/terraform-provider-kubernetes/issues/1986
# Workaround for PVC creation timeout due to provider rate limiting
resource "kubernetes_persistent_volume_claim" "mysql" {
  count = var.mysql_deployment_mode == "k8s" ? 1 : 0

  metadata {
    name      = "mysql-data"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
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

resource "kubernetes_secret" "mysql" {
  count = var.mysql_deployment_mode == "k8s" ? 1 : 0

  metadata {
    name      = "mysql-password"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  data = {
    password = random_password.mysql.result
  }
}

resource "kubernetes_stateful_set" "mysql" {
  count = var.mysql_deployment_mode == "k8s" ? 1 : 0

  metadata {
    name      = "mysql"
    namespace = kubernetes_namespace.ragflow.metadata[0].name

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

          args = ["--max-connections=2000", "--wait-timeout=600", "--interactive-timeout=600"]

          env {
            name = "MYSQL_ROOT_PASSWORD"

            value_from {
              secret_key_ref {
                name = kubernetes_secret.mysql[0].metadata[0].name
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
            value = local.mysql_user
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
            claim_name = kubernetes_persistent_volume_claim.mysql[0].metadata[0].name
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

resource "kubernetes_service" "mysql" {
  count = var.mysql_deployment_mode == "k8s" ? 1 : 0

  metadata {
    name      = "mysql"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
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

# =============================================================================
# Redis Deployment
# =============================================================================

resource "kubernetes_deployment" "redis" {
  metadata {
    name      = "redis"
    namespace = kubernetes_namespace.ragflow.metadata[0].name

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
            random_password.redis.result,
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

resource "kubernetes_service" "redis" {
  metadata {
    name      = "redis"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
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

resource "kubernetes_deployment" "tei" {
  count = var.tei_replicas > 0 ? 1 : 0
  metadata {
    name      = "tei"
    namespace = kubernetes_namespace.ragflow.metadata[0].name

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

resource "kubernetes_service" "tei" {
  count = var.tei_replicas > 0 ? 1 : 0
  metadata {
    name      = "tei"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
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

resource "kubernetes_config_map" "rabbitmq" {
  metadata {
    name      = "rabbitmq-config"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
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
          name     = local.rabbitmq_user
          password = random_password.rabbitmq.result
          tags     = ["administrator"]
        }
      ]
      vhosts = [
        {
          name        = "/"
          description = "Default virtual host"
          metadata = {
            description        = "Default virtual host"
            tags               = []
            default_queue_type = "classic"
          }
          tags = []
        }
      ]
      topic_permissions = []
      permissions = [
        {
          user      = local.rabbitmq_user
          vhost     = "/"
          configure = ".*"
          write     = ".*"
          read      = ".*"
        }
      ]
      parameters        = []
      global_parameters = [
        {
          name  = "cluster_tags"
          value = []
        }
      ]
      policies = []
      queues = [
        {
          name        = "te.0.raptor"
          vhost       = "/"
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.0.common"
          vhost       = "/"
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.error"
          vhost       = "/"
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.0.graphrag"
          vhost       = "/"
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        },
        {
          name        = "te.0.resume"
          vhost       = "/"
          durable     = true
          auto_delete = false
          arguments   = { "x-queue-type" = "classic" }
        }
      ]
      exchanges = [
        {
          name        = "test1"
          vhost       = "/"
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
          vhost            = "/"
          destination      = "te.0.common"
          destination_type = "queue"
          routing_key      = "te.0.common"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = "/"
          destination      = "te.0.graphrag"
          destination_type = "queue"
          routing_key      = "te.0.graphrag"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = "/"
          destination      = "te.0.raptor"
          destination_type = "queue"
          routing_key      = "te.0.raptor"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = "/"
          destination      = "te.error"
          destination_type = "queue"
          routing_key      = "te.error"
          arguments        = {}
        },
        {
          source           = "test1"
          vhost            = "/"
          destination      = "te.0.resume"
          destination_type = "queue"
          routing_key      = "te.0.resume"
          arguments        = {}
        }
      ]
    })
    "10-definitions.conf" = <<-EOT
definitions.import_backend = local_filesystem
definitions.local.path = /etc/rabbitmq/definitions.json

# Enable RabbitMQ management plugin
management.tcp.port = 15672
EOT
  }
}

# Ref: https://github.com/hashicorp/terraform-provider-kubernetes/issues/1986
# Workaround for PVC creation timeout due to provider rate limiting
resource "kubernetes_persistent_volume_claim" "rabbitmq" {
  metadata {
    name      = "rabbitmq-pvc"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
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

resource "kubernetes_deployment" "rabbitmq" {
  metadata {
    name      = "rabbitmq"
    namespace = kubernetes_namespace.ragflow.metadata[0].name

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
            claim_name = kubernetes_persistent_volume_claim.rabbitmq.metadata[0].name
          }
        }

        volume {
          name = "rabbitmq-definitions"

          config_map {
            name = kubernetes_config_map.rabbitmq.metadata[0].name

            items {
              key  = "definitions.json"
              path = "definitions.json"
            }
          }
        }

        volume {
          name = "rabbitmq-definitions-conf"

          config_map {
            name = kubernetes_config_map.rabbitmq.metadata[0].name

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

resource "kubernetes_service" "rabbitmq" {
  metadata {
    name      = "rabbitmq"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
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

# =============================================================================
# Elasticsearch Deployment (K8s Mode)
# =============================================================================

# Deploy ECK (Elastic Cloud on Kubernetes) Operator using Helm
# This installs the CRDs and operator required for Elasticsearch resources
# Ref: https://artifacthub.io/packages/helm/elastic/eck-operator
resource "helm_release" "eck_operator" {
  count = var.es_deployment_mode == "k8s" ? 1 : 0

  name             = "eck-operator"
  repository       = "https://helm.elastic.co"
  chart            = "eck-operator"
  version          = "3.3.1"
  namespace        = "elastic-system"
  create_namespace = true

  # Set timeout to wait for CRDs installation
  timeout = 600

  # Skip CRD validation since CRDs are installed by the chart
  disable_openapi_validation = true

  # Allow helm release to already exist (idempotent)
  lifecycle {
    ignore_changes = [name, namespace, repository, chart, version]
  }
}

# =============================================================================
# Check CRD Availability
# =============================================================================
# Use a local-exec to verify CRD is installed by ECK operator.
# This replaces the fixed time_sleep with a dynamic check.

resource "terraform_data" "wait_for_elasticsearch_crd" {
  count = var.es_deployment_mode == "k8s" ? 1 : 0

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

# CustomComputeClass for GKE to set vm.max_map_count
# Requires GKE >= 1.30.3-gke.1451000
# Reference: https://www.elastic.co/docs/deploy-manage/deploy/cloud-on-k8s/deploy-eck-on-gke-autopilot
resource "kubernetes_manifest" "elasticsearch_compute_class" {
  count = var.es_deployment_mode == "k8s" && var.cloud_provider == "gcp" ? 1 : 0

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
}

# =============================================================================
# Elasticsearch Deployment (K8s Mode) - ConfigMap + Job Pattern
# =============================================================================
# Because the ECK CRDs are installed dynamically by the operator, Terraform's
# kubernetes_manifest would fail validation at plan time.
# To assume the CRDs will exist, we use a ConfigMap + Job to apply the manifest.

# 1. Store the Elasticsearch manifest in a ConfigMap
resource "kubernetes_config_map" "elasticsearch_manifest" {
  count = var.es_deployment_mode == "k8s" ? 1 : 0

  metadata {
    name      = "elasticsearch-manifest"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  data = {
    "elasticsearch.yaml" = <<YAML
apiVersion: elasticsearch.k8s.elastic.co/v1
kind: Elasticsearch
metadata:
  name: elasticsearch
  namespace: ${kubernetes_namespace.ragflow.metadata[0].name}
spec:
  version: ${local.es_version}
  nodeSets:
  - name: masters
    count: ${var.es_k8s_node_count}
    config:
      node.store.allow_mmap: true
      node.roles: ["master", "data", "ingest"]
    podTemplate:
      spec:
%{if local.is_gke_gateway}
        nodeSelector:
          cloud.google.com/compute-class: elasticsearch
%{endif}
        containers:
        - name: elasticsearch
          image: ${var.es_image}
          resources:
            requests:
              cpu: "${var.es_cpu_request}"
              memory: "${var.es_memory_request}"
            limits:
              cpu: "${var.es_cpu_limit}"
              memory: "${var.es_memory_limit}"
          env:
          - name: ES_JAVA_OPTS
            value: "-Xms${var.es_heap_size} -Xmx${var.es_heap_size}"
    volumeClaimTemplates:
    - metadata:
        name: elasticsearch-data
      spec:
        accessModes: ["ReadWriteOnce"]
        storageClassName: ${local.config.storage_class}
        resources:
          requests:
            storage: "${var.es_k8s_storage}Gi"
YAML
  }

  depends_on = [terraform_data.wait_for_elasticsearch_crd]
}

# 2. ServiceAccount for the applier job
resource "kubernetes_service_account" "elasticsearch_applier" {
  count = var.es_deployment_mode == "k8s" ? 1 : 0

  metadata {
    name      = "elasticsearch-applier"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }
}

# 3. Role to allow creating Elasticsearch resources
resource "kubernetes_role" "elasticsearch_applier" {
  count = var.es_deployment_mode == "k8s" ? 1 : 0

  metadata {
    name      = "elasticsearch-applier"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  rule {
    api_groups = ["elasticsearch.k8s.elastic.co"]
    resources  = ["elasticsearches"]
    verbs      = ["get", "create", "update", "patch", "list"]
  }
}

# 4. RoleBinding
resource "kubernetes_role_binding" "elasticsearch_applier" {
  count = var.es_deployment_mode == "k8s" ? 1 : 0

  metadata {
    name      = "elasticsearch-applier"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "Role"
    name      = kubernetes_role.elasticsearch_applier[0].metadata[0].name
  }

  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account.elasticsearch_applier[0].metadata[0].name
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }
}

# 5. Job to apply the manifest
resource "kubernetes_job" "apply_elasticsearch" {
  count = var.es_deployment_mode == "k8s" ? 1 : 0

  metadata {
    name      = "apply-elasticsearch"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  spec {
    template {
      metadata {
        name = "apply-elasticsearch"
      }
      spec {
        service_account_name = kubernetes_service_account.elasticsearch_applier[0].metadata[0].name
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
            name = kubernetes_config_map.elasticsearch_manifest[0].metadata[0].name
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

  depends_on = [
    terraform_data.wait_for_elasticsearch_crd,
    kubernetes_role_binding.elasticsearch_applier,
    kubernetes_config_map.elasticsearch_manifest,
    kubernetes_manifest.elasticsearch_compute_class
  ]
}

# =============================================================================
# Wait for Elasticsearch Secret to be Available
# =============================================================================
resource "terraform_data" "wait_for_elasticsearch_secret" {
  count = var.es_deployment_mode == "k8s" ? 1 : 0

  triggers_replace = [
    kubernetes_job.apply_elasticsearch[0].id
  ]

  provisioner "local-exec" {
    environment = {
      KUBECONFIG = pathexpand(var.kubeconfig_path)
    }
    command = "python3 wait_for_k8s_resource.py ${kubernetes_namespace.ragflow.metadata[0].name} secret elasticsearch-es-elastic-user"
  }
}

# =============================================================================
# Read ECK-managed Elasticsearch Secret (k8s mode)
# =============================================================================
# This secret is managed by ECK operator and contains the auto-generated
# password for the 'elastic' user.
data "kubernetes_secret" "elasticsearch_es_user" {
  count = var.es_deployment_mode == "k8s" ? 1 : 0

  metadata {
    name      = "elasticsearch-es-elastic-user"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  depends_on = [
    kubernetes_job.apply_elasticsearch,
    terraform_data.wait_for_elasticsearch_secret
  ]
}

# =============================================================================
# S3 Storage Secret
# =============================================================================

resource "kubernetes_secret" "storage" {
  metadata {
    name      = "ragflow-storage"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  data = {
    S3_ENDPOINT   = var.s3_endpoint != "" ? var.s3_endpoint : local.config.s3_endpoint
    S3_BUCKET     = var.s3_bucket
    S3_ACCESS_KEY = var.s3_access_key
    S3_SECRET_KEY = var.s3_secret_key
    S3_REGION     = var.s3_region != "" ? var.s3_region : local.config.s3_region
  }

  type = "Opaque"
}

# =============================================================================
# RAGFlow Environment Secret
# =============================================================================

resource "kubernetes_secret" "ragflow_env" {
  depends_on = [
    kubernetes_stateful_set.mysql,
    kubernetes_deployment.redis,
    kubernetes_deployment.rabbitmq,
    terraform_data.wait_for_elasticsearch_secret,
  ]

  metadata {
    name      = "ragflow-env"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  data = {
    # MySQL Configuration
    MYSQL_HOST     = var.mysql_deployment_mode == "k8s" ? "mysql" : ""
    MYSQL_PORT     = "3306"
    MYSQL_USER     = local.mysql_user
    MYSQL_DB_NAME  = var.mysql_db_name
    MYSQL_PASSWORD = var.mysql_deployment_mode == "k8s" ? random_password.mysql.result : ""

    # Elasticsearch Configuration
    # Use ES_PROTOCOL=https to enable HTTPS for ECK-managed Elasticsearch
    ES_PROTOCOL = var.es_deployment_mode == "k8s" ? "https" : "http"
    ES_HOST     = var.es_deployment_mode == "k8s" ? "elasticsearch-es-http" : ""
    ES_PORT     = "9200"
    ES_USER     = "elastic"
    # ELASTIC_PASSWORD: use password from ECK-managed secret (k8s mode)
    # data.kubernetes_secret automatically base64-decodes secret data
    ELASTIC_PASSWORD = var.es_deployment_mode == "k8s" ? data.kubernetes_secret.elasticsearch_es_user[0].data.elastic : ""

    # Redis Configuration
    REDIS_HOST     = "redis"
    REDIS_PASSWORD = random_password.redis.result

    # TEI Configuration
    TEI_HOST  = "tei"
    TEI_MODEL = var.tei_model

    # RabbitMQ Configuration
    RABBITMQ_HOST         = "rabbitmq"
    RABBITMQ_PORT         = "5672"
    RABBITMQ_API_PORT     = "15672"
    RABBITMQ_DEFAULT_USER = local.rabbitmq_user
    RABBITMQ_DEFAULT_PASS = random_password.rabbitmq.result

    # Storage Configuration
    S3_ENDPOINT   = var.s3_endpoint != "" ? var.s3_endpoint : local.config.s3_endpoint
    S3_BUCKET     = var.s3_bucket
    S3_ACCESS_KEY = var.s3_access_key
    S3_SECRET_KEY = var.s3_secret_key
    S3_REGION     = var.s3_region != "" ? var.s3_region : local.config.s3_region

    # Storage Implementation Type (AWS_S3 or OSS)
    # Auto-detect based on cloud_provider or endpoint
    # STORAGE_IMPL: GCS for GCP, OSS for Aliyun, AWS_S3 for others (S3/MinIO)
    STORAGE_IMPL = var.cloud_provider == "gcp" ? "GCS" : (var.cloud_provider == "alicloud" ? "OSS" : "AWS_S3")

    # Application Configuration
    HOST_ADDRESS    = "http://127.0.0.1:9380"
  }

  type = "Opaque"
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
resource "kubernetes_service" "ragflow_frontend" {
  metadata {
    name      = "ragflow-frontend"
    namespace = kubernetes_namespace.ragflow.metadata[0].name

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

# BackendConfig for API service health check (GKE Gateway)
resource "kubernetes_manifest" "ragflow_api_backendconfig" {
  count = local.is_gke_gateway ? 1 : 0

  manifest = {
    apiVersion = "cloud.google.com/v1"
    kind       = "BackendConfig"
    metadata = {
      name      = "ragflow-api-backendconfig"
      namespace = kubernetes_namespace.ragflow.metadata[0].name
    }
    spec = {
      healthCheck = {
        checkIntervalSec = 60
        timeoutSec       = 10
        unhealthyThreshold = 3
        healthyThreshold = 1
        type = "HTTP"
        requestPath = "/"
        port = 9380
      }
    }
  }
}

# BackendConfig for Admin service health check (GKE Gateway)
resource "kubernetes_manifest" "ragflow_admin_backendconfig" {
  count = local.is_gke_gateway ? 1 : 0

  manifest = {
    apiVersion = "cloud.google.com/v1"
    kind       = "BackendConfig"
    metadata = {
      name      = "ragflow-admin-backendconfig"
      namespace = kubernetes_namespace.ragflow.metadata[0].name
    }
    spec = {
      healthCheck = {
        checkIntervalSec = 30
        timeoutSec       = 10
        unhealthyThreshold = 3
        healthyThreshold = 1
        type = "HTTP"
        requestPath = "/api/v1/admin/"
        port = 9381
      }
    }
  }
}

# Service 2: API Server - serves REST API at /v1/*
resource "kubernetes_service" "ragflow_api" {
  metadata {
    name      = "ragflow-api"
    namespace = kubernetes_namespace.ragflow.metadata[0].name

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
resource "kubernetes_service" "ragflow_admin" {
  metadata {
    name      = "ragflow-admin"
    namespace = kubernetes_namespace.ragflow.metadata[0].name

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
      port        = 9381
      target_port = 9381
      name        = "admin"
    }

    type = "ClusterIP"
  }
}

# DEPRECATED: Keeping for backward compatibility reference
# Original multi-port service - no longer used (replaced by above 3 separate services)
# resource "kubernetes_service" "ragflow" { ... }

# =============================================================================
# RAGFlow Deployment
# =============================================================================

resource "kubernetes_deployment" "ragflow" {
  depends_on = [kubernetes_secret.ragflow_env]

  metadata {
    name      = "ragflow"
    namespace = kubernetes_namespace.ragflow.metadata[0].name

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
        annotations = merge(
          var.cloud_provider == "gcp" ? {
            "iam.gke.io/gcp-service-account" = local.gcs_service_account
          } : {},
          {
            # Trigger rollout restart when secret changes
            "checksum/config" = sha256(jsonencode(kubernetes_secret.ragflow_env.data))
          }
        )
      }

      spec {
        # Use Workload Identity for GCP
        service_account_name = var.cloud_provider == "gcp" ? "ragflow-gcs" : "default"

        # Init container to create S3 bucket if needed
        dynamic "init_container" {
          for_each = var.s3_endpoint != "" ? [1] : []
          content {
            name    = "init-s3-bucket"
            image   = local.minio_mc_image
            command = ["sh", "-c", "mc alias set myminio ${var.s3_endpoint} ${var.s3_access_key} ${var.s3_secret_key} && mc mb myminio/${var.s3_bucket} || exit 0"]
          }
        }

        # Init container to wait for Elasticsearch to be ready
        dynamic "init_container" {
          for_each = var.es_deployment_mode == "k8s" ? [1] : []
          content {
            name  = "wait-for-elasticsearch"
            image = local.curl_image

            env {
              name  = "ES_HOST"
              value = var.es_deployment_mode == "k8s" ? "elasticsearch-es-http" : ""
            }

            env {
              name  = "ES_PORT"
              value = "9200"
            }

            env {
              name  = "ES_PROTOCOL"
              value = var.es_deployment_mode == "k8s" ? "https" : "http"
            }

            env {
              name  = "ES_USER"
              value = "elastic"
            }

            env {
              name = "ELASTIC_PASSWORD"

              value_from {
                secret_key_ref {
                  name = "elasticsearch-es-elastic-user"
                  key  = "elastic"
                }
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
          for_each = var.es_deployment_mode == "k8s" ? [1] : []
          content {
            name = "elasticsearch-ca"

            secret {
              secret_name = "elasticsearch-es-http-certs-public"
            }
          }
        }

        container {
          name  = "ragflow"
          image = local.ragflow_image_full

          args = ["--disable-taskexecutor", "--enable-adminserver"]

          # Frontend port (nginx)
          port {
            container_port = 80
            name          = "http"
          }

          # API port
          port {
            container_port = 9380
            name          = "api"
          }

          # Admin port
          port {
            container_port = 9381
            name          = "admin"
          }

          # Readiness probe for GKE Gateway health check
          # IMPORTANT: RAGFlow API server takes significant time to initialize (connecting to MySQL,
          # Redis, Elasticsearch, loading models, etc.). We set a generous timeout to allow
          # up to 600 seconds (60s initial_delay + 54 * 10s period) for startup.
          readiness_probe {
            http_get {
              path = "/"
              port = 80
            }
            initial_delay_seconds = 60
            period_seconds        = 10
            timeout_seconds       = 5
            failure_threshold     = 54
          }

          env_from {
            secret_ref {
              name = kubernetes_secret.ragflow_env.metadata[0].name
            }
          }

          # Inject ES password from ECK-managed secret
          dynamic "env" {
            for_each = var.es_deployment_mode == "k8s" ? [1] : []
            content {
              name = "ELASTIC_PASSWORD"

              value_from {
                secret_key_ref {
                  name = "elasticsearch-es-elastic-user"
                  key  = "elastic"
                }
              }
            }
          }

          # Mount ES CA certificate for HTTPS verification
          dynamic "volume_mount" {
            for_each = var.es_deployment_mode == "k8s" ? [1] : []
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
        }
      }
    }
  }
}

# =============================================================================
# Parser Deployment
# =============================================================================

resource "kubernetes_deployment" "parser" {
  depends_on = [kubernetes_secret.ragflow_env]

  metadata {
    name      = "parser"
    namespace = kubernetes_namespace.ragflow.metadata[0].name

    labels = {
      app     = "parser"
      project = "ragflow"
    }
  }

  spec {
    replicas = var.parser_replicas

    # Limit revision history to reduce orphaned ReplicaSets
    revision_history_limit = 1

    strategy {
      type = "Recreate"
    }

    selector {
      match_labels = {
        app = "parser"
      }
    }

    template {
      metadata {
        labels = {
          app     = "parser"
          project = "ragflow"
        }
        annotations = merge(
          var.cloud_provider == "gcp" ? {
            "iam.gke.io/gcp-service-account" = local.gcs_service_account
          } : {},
          {
            # Trigger rollout restart when secret changes
            "checksum/config" = sha256(jsonencode(kubernetes_secret.ragflow_env.data))
          }
        )
      }

      spec {
        # Use Workload Identity for GCP
        service_account_name = var.cloud_provider == "gcp" ? "ragflow-gcs" : "default"

        # Init container to wait for Elasticsearch to be ready
        dynamic "init_container" {
          for_each = var.es_deployment_mode == "k8s" ? [1] : []
          content {
            name  = "wait-for-elasticsearch"
            image = local.curl_image

            env {
              name  = "ES_HOST"
              value = var.es_deployment_mode == "k8s" ? "elasticsearch-es-http" : ""
            }

            env {
              name  = "ES_PORT"
              value = "9200"
            }

            env {
              name  = "ES_PROTOCOL"
              value = var.es_deployment_mode == "k8s" ? "https" : "http"
            }

            env {
              name  = "ES_USER"
              value = "elastic"
            }

            env {
              name = "ELASTIC_PASSWORD"

              value_from {
                secret_key_ref {
                  name = "elasticsearch-es-elastic-user"
                  key  = "elastic"
                }
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
          for_each = var.es_deployment_mode == "k8s" ? [1] : []
          content {
            name = "elasticsearch-ca"

            secret {
              secret_name = "elasticsearch-es-http-certs-public"
            }
          }
        }

        container {
          name  = "parser"
          image = local.ragflow_image_full

          command = ["/ragflow/entrypoint-parser.sh"]

          port {
            container_port = 9380
            name           = "http"
          }

          env_from {
            secret_ref {
              name = kubernetes_secret.ragflow_env.metadata[0].name
            }
          }

          # Inject ES password from ECK-managed secret
          dynamic "env" {
            for_each = var.es_deployment_mode == "k8s" ? [1] : []
            content {
              name = "ELASTIC_PASSWORD"

              value_from {
                secret_key_ref {
                  name = "elasticsearch-es-elastic-user"
                  key  = "elastic"
                }
              }
            }
          }

          # Mount ES CA certificate for HTTPS verification
          dynamic "volume_mount" {
            for_each = var.es_deployment_mode == "k8s" ? [1] : []
            content {
              name       = "elasticsearch-ca"
              mount_path = "/etc/elasticsearch/certs"
              read_only  = true
            }
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

resource "kubernetes_deployment" "deepdoc" {
  metadata {
    name      = "deepdoc"
    namespace = kubernetes_namespace.ragflow.metadata[0].name

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
        # Container
        container {
          name  = "deepdoc"
          image = local.deepdoc_image_full

          port {
            container_port = 8000
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
        }
      }
    }
  }
}

resource "kubernetes_service" "deepdoc" {
  metadata {
    name      = "deepdoc"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
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
# Gateway API - NGINX Gateway Fabric (only for non-GKE Gateway)
# =============================================================================

resource "kubernetes_manifest" "gateway" {
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "Gateway"
    metadata = {
      name      = "ragflow"
      namespace = kubernetes_namespace.ragflow.metadata[0].name
      labels = {
        app = "ragflow"
      }
    }
    spec = {
      gatewayClassName = var.gateway_class_name
      listeners = [
        {
          name     = "http"
          protocol = "HTTP"
          port     = 80
          allowedRoutes = {
            namespaces = {
              selector = {
                matchLabels = {
                  "kubernetes.io/metadata.name" = kubernetes_namespace.ragflow.metadata[0].name
                }
              }
            }
          }
        }
      ]
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
# HTTPRoute Resources
# =============================================================================
# WORKAROUND for Aliyun ALB bug: Currently, an HTTPRoute that references multiple
# ports of the same service causes server group registration failures. The Aliyun
# team will fix this in a future release. The temporary workaround is to create
# separate HTTPRoutes for different ports of the same service.

# HTTPRoute 1: /v1 and /api -> port 9380 (API service)
resource "kubernetes_manifest" "http_route_api" {
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "HTTPRoute"
    metadata = {
      name      = "ragflow-http-route-api"
      namespace = kubernetes_namespace.ragflow.metadata[0].name
      labels = {
        app = "ragflow"
      }
    }
    spec = {
      parentRefs = [
        {
          name      = "ragflow"
          namespace = kubernetes_namespace.ragflow.metadata[0].name
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
              name = kubernetes_service.ragflow_api.metadata[0].name
              port = 9380
            }
          ]
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
              name = kubernetes_service.ragflow_api.metadata[0].name
              port = 9380
            }
          ]
        }
      ]
    }
  }
}

# HTTPRoute 2: /api/v1/admin -> port 9381 (admin service)
resource "kubernetes_manifest" "http_route_admin" {
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "HTTPRoute"
    metadata = {
      name      = "ragflow-http-route-admin"
      namespace = kubernetes_namespace.ragflow.metadata[0].name
      labels = {
        app = "ragflow"
      }
    }
    spec = {
      parentRefs = [
        {
          name      = "ragflow"
          namespace = kubernetes_namespace.ragflow.metadata[0].name
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
              name = kubernetes_service.ragflow_admin.metadata[0].name
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
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "HTTPRoute"
    metadata = {
      name      = "ragflow-http-route-frontend"
      namespace = kubernetes_namespace.ragflow.metadata[0].name
      labels = {
        app = "ragflow"
      }
    }
    spec = {
      parentRefs = [
        {
          name      = "ragflow"
          namespace = kubernetes_namespace.ragflow.metadata[0].name
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
              name = kubernetes_service.ragflow_frontend.metadata[0].name
              port = 80
            }
          ]
        }
      ]
    }
  }
}

# =============================================================================
# Get Gateway Fabric Service Address
# =============================================================================

# Get NGINX Gateway Fabric service for on-premises deployments
data "kubernetes_service" "gateway_fabric" {
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
data "external" "gateway_ip" {
  count = local.is_gke_gateway ? 1 : 0
  program = ["sh", "-c", "kubectl get gateway ragflow -n ragflow -o jsonpath={.status.addresses[0].value} 2>/dev/null | jq -R -s -c '{address: .}'"]

  depends_on = [kubernetes_manifest.gateway]
}

# Get Gateway IP for on-premises (non-GKE) deployments
# Use Gateway CR status instead of LoadBalancer service IP (they may differ)
data "external" "nginx_gateway_ip" {
  count = !local.is_gke_gateway ? 1 : 0
  program = ["sh", "-c", <<-EOF
    # Get IP from Gateway CR status (more accurate than service IP)
    ip=$(kubectl get gateway ragflow -n ragflow -o jsonpath='{.status.addresses[0].value}' 2>/dev/null)
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
resource "kubernetes_config_map" "gateway_address_nginx" {
  count = local.is_gke_gateway ? 0 : 1
  depends_on = [kubernetes_manifest.gateway, data.external.nginx_gateway_ip]

  metadata {
    name      = "ragflow-gateway-address"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  data = {
    "gateway_address" = data.external.nginx_gateway_ip[0].result.address
  }
}

resource "kubernetes_config_map" "gateway_address_gke" {
  count = local.is_gke_gateway ? 1 : 0
  depends_on = [kubernetes_manifest.gateway, data.external.gateway_ip]

  metadata {
    name      = "ragflow-gateway-address"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  data = {
    "gateway_address" = data.external.gateway_ip[0].result.address
  }
}

# Output uses local variable to support both GKE and smk
output "gateway_address" {
  description = "Gateway IP address or hostname"
  value       = local.is_gke_gateway ? data.external.gateway_ip[0].result.address : data.external.nginx_gateway_ip[0].result.address
}
