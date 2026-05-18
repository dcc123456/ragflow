# RAGFlow Aliyun Cloud Deployment - Phase 2: Kubernetes Resources
# =============================================================================
# This file deploys RAGFlow components to an existing Kubernetes cluster
#
# Prerequisites:
# - Run infrastructure.tf first to create the cluster
# - Export kubeconfig: terraform output -raw kubeconfig > kubeconfig
# - Ensure kubectl can connect: kubectl get nodes
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
      version = "2.37.1"
    }

    helm = {
      source  = "hashicorp/helm"
      version = "2.16.1"
    }

    random = {
      source  = "hashicorp/random"
      version = "3.6.3"
    }
  }
}

# =============================================================================
# Remote State Data Source (Stage 1 Infrastructure)
# =============================================================================
# Reads outputs from the infrastructure stage to get cloud resource configs
data "terraform_remote_state" "infrastructure" {
  backend = "local"

  config = {
    path = "${path.module}/../stage1-infrastructure/terraform.tfstate"
  }
}

# =============================================================================
# Kubeconfig Configuration
# =============================================================================

variable "kubeconfig_path" {
  type        = string
  default     = "../kubeconfig"
  description = "Path to kubeconfig file from infrastructure stage"
}

# =============================================================================
# Local Values
# =============================================================================

# Load and parse kubeconfig file
locals {
  kube_config_raw = file(var.kubeconfig_path)
  kube_config     = yamldecode(local.kube_config_raw)
  cluster_host    = local.kube_config.clusters[0].cluster.server

  # Support both token and client certificate authentication
  # Priority: client certificate > token (for external kubeconfig compatibility)
  token_data              = try(local.kube_config.users[0].user.token, "")
  client_certificate_data = try(local.kube_config.users[0].user["client-certificate-data"], "")
  client_key_data         = try(local.kube_config.users[0].user["client-key-data"], "")
  cluster_ca_data         = try(local.kube_config.clusters[0].cluster["certificate-authority-data"], "")

  # Network configuration from infrastructure stage
  vswitch_ids = data.terraform_remote_state.infrastructure.outputs.vswitch_ids

  # Image configuration
  # private_registry: for RAGFlow and DeepDoc images (e.g., gcr.io/ragflow-462809, infiniflow-registry.cn-shanghai.cr.aliyuncs.com)
  # public_registry: for third-party images (MySQL, Redis, TEI, RabbitMQ, etc.). If empty, uses default registries

  # RAGFlow and DeepDoc images use private_registry
  ragflow_image_full = "${var.private_registry}/${var.ragflow_image}"
  deepdoc_image_full = "${var.private_registry}/${var.deepdoc_image}"

  # Third-party images use public_registry or default registries
  mysql_image     = var.public_registry != "" ? "${var.public_registry}/mysql:8.0.39" : "docker.io/library/mysql:8.0.39"
  redis_image     = var.public_registry != "" ? "${var.public_registry}/valkey:8" : "valkey/valkey:8"
  tei_image       = var.public_registry != "" ? "${var.public_registry}/text-embeddings-inference:cpu-1.8" : "infiniflow/text-embeddings-inference:cpu-1.8"
  rabbitmq_image  = var.public_registry != "" ? "${var.public_registry}/rabbitmq:4-management" : "rabbitmq:4-management"
  curl_image      = var.public_registry != "" ? "${var.public_registry}/curl:latest" : "curlimages/curl:latest"
  aws_cli_image   = var.public_registry != "" ? "${var.public_registry}/awscli:latest" : "quay.io/minio/mc:latest"

  # Database users (consistent across all components)
  mysql_user     = "ragflow"
  rabbitmq_user = "ragflow"
}

# =============================================================================
# Provider Configuration
# =============================================================================

# Configure Kubernetes provider using kubeconfig
# Supports both token (for in-cluster) and client certificate (for external kubeconfig)
provider "kubernetes" {
  host                   = local.cluster_host
  cluster_ca_certificate = local.cluster_ca_data != "" ? base64decode(local.cluster_ca_data) : null

  # Use token auth if no client certificate (in-cluster service account)
  # Use client certificate auth if available (external kubeconfig)
  client_certificate = local.client_certificate_data != "" ? base64decode(local.client_certificate_data) : null
  client_key         = local.client_key_data != "" ? base64decode(local.client_key_data) : null
  token              = local.client_certificate_data == "" ? local.token_data : null
}

provider "helm" {
  kubernetes {
    host                   = local.cluster_host
    cluster_ca_certificate = local.cluster_ca_data != "" ? base64decode(local.cluster_ca_data) : null
    client_certificate     = local.client_certificate_data != "" ? base64decode(local.client_certificate_data) : null
    client_key             = local.client_key_data != "" ? base64decode(local.client_key_data) : null
    token                  = local.client_certificate_data == "" ? local.token_data : null
  }
}

provider "terraform" {}

provider "random" {}

# =============================================================================
# Kubernetes Resources
# =============================================================================

resource "kubernetes_namespace" "ragflow" {
  metadata {
    name = var.namespace
  }
}

# =============================================================================
# ACR Credential Helper Configuration
# =============================================================================
# Enables ACK nodes to pull images from private ACR registry without imagePullSecrets
# Uses Worker RAM Role (AliyunCSDefaultRole) to assume ACRPullRole for authentication
# See: https://help.aliyun.com/zh/ack/ack-managed-and-ack-serverless_clusters/user-guide/use-rrsa-to-cross-account-acr
resource "kubernetes_config_map" "acr_configuration" {
  metadata {
    name      = "acr-configuration"
    namespace = "kube-system"
  }

  data = {
    "ACR_CONFIGURATION" = jsonencode({
      instances = [
        {
          instanceId = "cri-cy7xfknr3ysv2x64"
          regionId   = "cn-shanghai"
          domain     = "infiniflow-registry.cn-shanghai.cr.aliyuncs.com"
        }
      ]
      assumeRoleARN = "acs:ram::1363212506972526:role/acrpullrole"
    })
  }
}

# =============================================================================
# MySQL Deployment (K8s Mode)
# =============================================================================

resource "random_password" "mysql_k8s" {
  count   = var.mysql_deployment_mode == "k8s" ? 1 : 0
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

    storage_class_name = var.storage_class
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

          env {
            name  = "MYSQL_DATABASE"
            value = "rag_flow"
          }

          env {
            name  = "MYSQL_ROOT_PASSWORD"
            # Use random password in k8s mode, otherwise use provided password
            value = var.mysql_deployment_mode == "k8s" ? random_password.mysql_k8s[0].result : var.mysql_password
          }

          env {
            name  = "MYSQL_USER"
            value = local.mysql_user
          }

          env {
            name  = "MYSQL_PASSWORD"
            # Use same password source as ragflow_env secret to ensure consistency
            # K8s mode: use random_password.mysql_k8s[0].result
            # Cloud mode: use var.mysql_password from infrastructure outputs
            value = var.mysql_deployment_mode == "k8s" ? random_password.mysql_k8s[0].result : var.mysql_password
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

    storage_class_name = var.storage_class
  }
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
            name  = "RABBITMQ_DEFAULT_USER"
            value = local.rabbitmq_user
          }

          env {
            name  = "RABBITMQ_DEFAULT_PASS"
            value = random_password.rabbitmq.result
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
  version          = "3.2.0"
  namespace        = "elastic-system"
  create_namespace = true

  # Set timeout to wait for CRDs installation
  timeout = 600

  # Ensure CRDs are installed
  set_list {
    name  = "installCRDs"
    value = ["true"]
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
# Elasticsearch Deployment (K8s Mode) - Manifest Application
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
  version: 8.11.3
  nodeSets:
  - name: masters
    count: ${var.es_k8s_node_count}
    config:
      node.store.allow_mmap: true
      node.roles: ["master", "data", "ingest"]
    podTemplate:
      spec:
        containers:
        - name: elasticsearch
          image: var.es_image
          resources:
            requests:
              cpu: ${var.es_cpu_request}
              memory: ${var.es_memory_request}
            limits:
              cpu: ${var.es_cpu_limit}
              memory: ${var.es_memory_limit}
          env:
          - name: "ES_JAVA_OPTS"
            value: "-Xms${var.es_heap_size} -Xmx${var.es_heap_size}"
    volumeClaimTemplates:
    - metadata:
        name: elasticsearch-data
      spec:
        accessModes:
        - ReadWriteOnce
        storageClassName: ${var.storage_class}
        resources:
          requests:
            storage: ${var.es_k8s_storage}Gi
YAML
  }
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
    verbs      = ["get", "create", "update", "patch"]
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

          volume_mount {
            name       = "manifest"
            mount_path = "/data"
          }
        }
        volume {
          name = "manifest"
          config_map {
            name = kubernetes_config_map.elasticsearch_manifest[0].metadata[0].name
          }
        }
      }
    }
    backoff_limit = 4
  }

  # Wait for the job to complete implies kubectl apply returned success
  wait_for_completion = true

  depends_on = [
    terraform_data.wait_for_elasticsearch_crd,
    kubernetes_role_binding.elasticsearch_applier
  ]
}

# =============================================================================
# Verify Elasticsearch Secret is Available
# =============================================================================
# =============================================================================
# Wait for Elasticsearch Secret to be Available
# =============================================================================
# The secret is created by ECK operator after the Elasticsearch cluster is ready.
# We use terraform_data with local-exec to poll until the secret exists.
# This replaces the ineffective check block which only validates, not waits.
resource "terraform_data" "wait_for_elasticsearch_secret" {
  count = var.es_deployment_mode == "k8s" ? 1 : 0

  # Trigger recreation if job is rerun
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
# NOTE: This resource uses kubectl wait instead of complex shell loops.
# See SHELL_SCRIPT_LESSON.md for detailed explanation of best practices.

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
    S3_ENDPOINT   = data.terraform_remote_state.infrastructure.outputs.s3_config.endpoint
    S3_BUCKET     = data.terraform_remote_state.infrastructure.outputs.s3_config.bucket
    S3_ACCESS_KEY = data.terraform_remote_state.infrastructure.outputs.s3_config.access_key
    S3_SECRET_KEY = data.terraform_remote_state.infrastructure.outputs.s3_config.secret_key
    S3_REGION     = data.terraform_remote_state.infrastructure.outputs.s3_config.region
  }

  type = "Opaque"
}

# =============================================================================
# RAGFlow Environment Secret
# =============================================================================

resource "kubernetes_secret" "ragflow_env" {
  metadata {
    name      = "ragflow-env"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  data = {
    # MySQL Configuration
    MYSQL_HOST     = var.mysql_deployment_mode == "k8s" ? "mysql" : data.terraform_remote_state.infrastructure.outputs.mysql_config.host
    MYSQL_PORT     = "3306"
    MYSQL_USER     = local.mysql_user
    MYSQL_DB_NAME  = "rag_flow"
    MYSQL_PASSWORD = var.mysql_deployment_mode == "k8s" ? random_password.mysql_k8s[0].result : data.terraform_remote_state.infrastructure.outputs.mysql_config.password

    # Elasticsearch Configuration
    # Use ES_PROTOCOL=https for cloud ES, http for k8s ES
    ES_PROTOCOL = var.es_deployment_mode == "k8s" ? "https" : data.terraform_remote_state.infrastructure.outputs.es_config.protocol
    ES_HOST     = var.es_deployment_mode == "k8s" ? "elasticsearch-es-http" : data.terraform_remote_state.infrastructure.outputs.es_config.endpoint
    ES_PORT     = "9200"
    ES_USER     = "elastic"
    # ELASTIC_PASSWORD:
    # - Cloud mode: use password from remote state infrastructure outputs
    # - K8s mode: use password from ECK-managed secret
    #   The data source has depends_on to wait for the ECK secret to be created
    #   NOTE: data.kubernetes_secret automatically base64-decodes secret data,
    #   so we use the value directly without base64decode()
    #   NOTE: No try() needed - dependency chain guarantees secret is ready:
    #     kubernetes_secret.ragflow_env → elasticsearch_es_user data → wait_for_elasticsearch_secret
    ELASTIC_PASSWORD = var.es_deployment_mode == "cloud" ? data.terraform_remote_state.infrastructure.outputs.es_config.password : data.kubernetes_secret.elasticsearch_es_user[0].data.elastic

    # Redis Configuration
    REDIS_HOST     = "redis"
    REDIS_PASSWORD = random_password.redis.result

    # TEI Configuration
    TEI_ENABLED = var.tei_replicas > 0 ? "1" : "0"
    TEI_HOST    = "tei"
    TEI_MODEL   = var.tei_replicas > 0 ? var.tei_model : ""

    # RabbitMQ Configuration
    RABBITMQ_HOST         = "rabbitmq"
    RABBITMQ_PORT         = "5672"
    RABBITMQ_API_PORT     = "15672"
    RABBITMQ_DEFAULT_USER = local.rabbitmq_user
    RABBITMQ_DEFAULT_PASS = random_password.rabbitmq.result

    # Storage Configuration
    S3_ENDPOINT   = data.terraform_remote_state.infrastructure.outputs.s3_config.endpoint
    S3_BUCKET     = data.terraform_remote_state.infrastructure.outputs.s3_config.bucket
    S3_ACCESS_KEY = data.terraform_remote_state.infrastructure.outputs.s3_config.access_key
    S3_SECRET_KEY = data.terraform_remote_state.infrastructure.outputs.s3_config.secret_key
    S3_REGION     = data.terraform_remote_state.infrastructure.outputs.s3_config.region

    # Storage Implementation Type (AWS_S3 or OSS)
    # Always use OSS for Aliyun
    STORAGE_IMPL = "OSS"

    # Application Configuration
    HOST_ADDRESS    = var.enable_tls ? "https://${var.gateway_host}" : "http://${var.gateway_host}"
  }

  # Ensure Elasticsearch secret is created before ragflow_env
  # This ensures ELASTIC_PASSWORD is populated from the ECK-managed secret
  depends_on = [
    kubernetes_job.apply_elasticsearch
  ]

  type = "Opaque"
}

# Data resource to compute hash of secret content
# When kubernetes_secret.ragflow_env.data changes, SHA256 hash changes,
# triggering deployments that reference this data resource to restart
# This is a pure computation node (Terraform 1.4+)
resource "terraform_data" "ragflow_env_hash" {
  input = sha256(jsonencode(kubernetes_secret.ragflow_env.data))
}

# =============================================================================
# RAGFlow Service
# =============================================================================

resource "kubernetes_service" "ragflow" {
  metadata {
    name      = "ragflow"
    namespace = kubernetes_namespace.ragflow.metadata[0].name

    labels = {
      app = "ragflow"
    }
  }

  spec {
    selector = {
      app = "ragflow"
    }

    # Frontend port (nginx)
    port {
      port        = 80
      target_port = 80
      name        = "http"
    }

    # API port
    port {
      port        = 9380
      target_port = 9380
      name        = "api"
    }

    # Admin port
    port {
      port        = 9381
      target_port = 9381
      name        = "admin"
    }

    type = "ClusterIP"
  }
}

# =============================================================================
# RAGFlow Deployment
# =============================================================================

resource "kubernetes_deployment" "ragflow" {
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
      }

      spec {
        # Init container to verify S3/OSS bucket access
        # Always enable as Aliyun OSS is required for storage
        dynamic "init_container" {
          for_each = length(keys(data.terraform_remote_state.infrastructure.outputs.s3_config)) > 0 ? [1] : []
          content {
            name  = "init-s3-bucket"
            image = local.aws_cli_image

            # Load all environment variables from ragflow_env secret
            # This provides S3_* variables (S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, etc.)
            env_from {
              secret_ref {
                name = kubernetes_secret.ragflow_env.metadata[0].name
              }
            }

            # AWS CLI requires specific environment variable names for authentication
            # The AWS CLI tool (infiniflow/registry.cn-shanghai.cr.aliyuncs.com/infiniflow/awscli:latest)
            # expects AWS_* prefixed variables for credentials, but our ragflow_env secret uses S3_* prefix.
            #
            # Mapping required:
            #   - AWS_ACCESS_KEY_ID     <- S3_ACCESS_KEY (Aliyun OSS AccessKey ID)
            #   - AWS_SECRET_ACCESS_KEY <- S3_SECRET_KEY (Aliyun OSS AccessKey Secret)
            #   - AWS_DEFAULT_REGION    <- S3_REGION (Aliyun OSS region)
            #
            # Without these mappings, AWS CLI will fail with "Unable to locate credentials" error
            # because it doesn't recognize S3_ACCESS_KEY/S3_SECRET_KEY environment variables.
            #
            # Note: We keep both S3_* (for RAGFlow application) and AWS_* (for AWS CLI init container)
            env {
              name  = "AWS_ACCESS_KEY_ID"
              value_from {
                secret_key_ref {
                  name = kubernetes_secret.ragflow_env.metadata[0].name
                  key  = "S3_ACCESS_KEY"
                }
              }
            }
            env {
              name  = "AWS_SECRET_ACCESS_KEY"
              value_from {
                secret_key_ref {
                  name = kubernetes_secret.ragflow_env.metadata[0].name
                  key  = "S3_SECRET_KEY"
                }
              }
            }
            env {
              name  = "AWS_DEFAULT_REGION"
              value_from {
                secret_key_ref {
                  name = kubernetes_secret.ragflow_env.metadata[0].name
                  key  = "S3_REGION"
                }
              }
            }

            command = ["sh", "-c", <<-EOT
              set +e
              log() { echo "S3 Init: $*"; }

              # Configure AWS CLI for Aliyun OSS compatibility
              if echo "$${S3_ENDPOINT}" | grep -q "aliyuncs.com"; then
                aws configure set default.s3.addressing_style virtual
              fi

              # Verify bucket exists and is accessible
              if aws s3 ls "s3://$${S3_BUCKET}" --endpoint-url "$${S3_ENDPOINT}" >/dev/null 2>&1; then
                log "Bucket verified"
                exit 0
              fi

              log "Bucket not found, attempting creation..."
              # For Aliyun OSS, must specify region when creating bucket
              # Use --region parameter to set location constraint
              if aws s3 mb "s3://$${S3_BUCKET}" --endpoint-url "$${S3_ENDPOINT}" --region "$${S3_REGION}"; then
                log "Bucket created successfully"
                exit 0
              fi

              # Bucket not accessible - provide guidance
              if echo "$${S3_ENDPOINT}" | grep -q "aliyuncs.com"; then
                log "Error: Aliyun OSS bucket not accessible"
                log "Create bucket: https://oss.console.aliyun.com/"
                log "Name: $${S3_BUCKET}, Region: $${S3_REGION}"
              else
                log "Error: Bucket not accessible"
              fi
              exit 1
              EOT
            ]
          }
        }

        # Init container to wait for Elasticsearch to be ready (k8s mode)
        dynamic "init_container" {
          for_each = var.es_deployment_mode == "k8s" ? [1] : []
          content {
            name  = "wait-for-elasticsearch"
            image = local.curl_image
            env_from {
              secret_ref {
                name = kubernetes_secret.ragflow_env.metadata[0].name
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

        # ES CA certificate volume (k8s mode)
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

          port {
            container_port = 9380
            name           = "http"
          }

          env_from {
            secret_ref {
              name = kubernetes_secret.ragflow_env.metadata[0].name
            }
          }

          # Mount ES CA certificate for HTTPS verification (k8s mode)
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

  # Trigger deployment restart when ragflow_env secret content changes
  # Use terraform_data with SHA256 hash instead of secret.id
  # The hash output changes whenever any value in input changes
  lifecycle {
    replace_triggered_by = [
      terraform_data.ragflow_env_hash.output
    ]
  }
}

# =============================================================================
# Parser Deployment
# =============================================================================

resource "kubernetes_deployment" "parser" {
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
      }

      spec {
        # Init container to wait for Elasticsearch to be ready (k8s mode)
        dynamic "init_container" {
          for_each = var.es_deployment_mode == "k8s" ? [1] : []
          content {
            name  = "wait-for-elasticsearch"
            image = local.curl_image
            env_from {
              secret_ref {
                name = kubernetes_secret.ragflow_env.metadata[0].name
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

        # ES CA certificate volume (k8s mode)
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

          # Mount ES CA certificate for HTTPS verification (k8s mode)
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

  # Trigger deployment restart when ragflow_env secret content changes
  # Use terraform_data with SHA256 hash instead of secret.id
  # The hash output changes whenever any value in input changes
  lifecycle {
    replace_triggered_by = [
      terraform_data.ragflow_env_hash.output
    ]
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
# ALB Configuration (AlibabaCloud AlbConfig)
# =============================================================================

# AlbConfig custom resource for ALB Ingress Controller
# This defines the ALB instance configuration (IP type, availability zones, listeners)
# See: https://help.aliyun.com/zh/ack/product-overview/alb-ingress-controller
resource "kubernetes_manifest" "alb_config" {
  manifest = {
    apiVersion = "alibabacloud.com/v1"
    kind       = "AlbConfig"
    metadata = {
      name = "ragflow-alb"
      # Note: AlbConfig is a cluster-level resource, no namespace
    }
    spec = {
      config = {
        name        = "ragflow-alb"
        addressType = "Internet" # Internet (public) or Intranet (private)
        # Zone mappings require at least 2 vSwitches in different zones
        zoneMappings = [
          for vsw_id in local.vswitch_ids : {
            vSwitchId = vsw_id
          }
        ]
      }
      listeners = [
        {
          port     = 80
          protocol = "HTTP"
        }
      ]
    }
  }

  # Ignore fields that are dynamically added/managed by ALB Ingress Controller
  # This prevents Terraform drift from controller-managed fields
  computed_fields = [
    "metadata.annotations",
    "metadata.finalizers",
    "metadata.labels",
    "metadata.resourceVersion",
    "status"
  ]

  # Force conflicts with ALB Ingress Controller's field management
  # The controller manages some metadata fields that Terraform also tracks
  field_manager {
    force_conflicts = true
  }

  depends_on = [kubernetes_namespace.ragflow]
}

# IngressClass that references the AlbConfig
# This allows Ingress resources to use the ALB configuration
resource "kubernetes_manifest" "alb_ingress_class" {
  manifest = {
    apiVersion = "networking.k8s.io/v1"
    kind       = "IngressClass"
    metadata = {
      name = "alb"
    }
    spec = {
      controller = "ingress.k8s.alibabacloud/alb"
      parameters = {
        apiGroup = "alibabacloud.com"
        kind     = "AlbConfig"
        name     = kubernetes_manifest.alb_config.manifest.metadata.name
        scope    = "Cluster"
      }
    }
  }

  # Ignore fields that are dynamically added/managed by Kubernetes controllers
  computed_fields = [
    "metadata.annotations",
    "metadata.finalizers",
    "metadata.labels",
    "metadata.resourceVersion"
  ]

  depends_on = [kubernetes_manifest.alb_config]
}

# =============================================================================
# Ingress Resource (Aliyun ALB Ingress Controller)
# =============================================================================

resource "kubernetes_ingress_v1" "ragflow" {
  metadata {
    name      = "ragflow"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
    labels = {
      app = "ragflow"
    }
    annotations = {
      "alb.ingress.kubernetes.io/scheme"      = "internet-facing"
      "alb.ingress.kubernetes.io/target-type" = "ip"
      # Reference the AlbConfig directly
      "alb.ingress.kubernetes.io/alb-config" = kubernetes_manifest.alb_config.manifest.metadata.name
    }
  }

  # Enable dynamic waiting for ALB address assignment.
  # Terraform will poll the Ingress resource status until the Load Balancer IP/Hostname is populated.
  # This avoids fixed `time_sleep` buffers and proceeds immediately when the ALB is ready.
  wait_for_load_balancer = true

  spec {
    ingress_class_name = kubernetes_manifest.alb_ingress_class.manifest.metadata.name
    # Rule 1: /v1 and /api -> port 9380 (API service)
    rule {
      http {
        # /v1 path -> API port 9380
        path {
          path      = "/v1"
          path_type = "Prefix"

          backend {
            service {
              name = kubernetes_service.ragflow.metadata[0].name
              port {
                number = 9380
              }
            }
          }
        }

        # /api path -> API port 9380
        path {
          path      = "/api"
          path_type = "Prefix"

          backend {
            service {
              name = kubernetes_service.ragflow.metadata[0].name
              port {
                number = 9380
              }
            }
          }
        }
      }
    }

    # Rule 2: /api/v1/admin -> port 9381 (admin service)
    rule {
      http {
        path {
          path      = "/api/v1/admin"
          path_type = "Prefix"

          backend {
            service {
              name = kubernetes_service.ragflow.metadata[0].name
              port {
                number = 9381
              }
            }
          }
        }
      }
    }

    # Rule 3: / (root path) -> port 80 (frontend nginx)
    rule {
      http {
        path {
          path      = "/"
          path_type = "Prefix"

          backend {
            service {
              name = kubernetes_service.ragflow.metadata[0].name
              port {
                number = 80
              }
            }
          }
        }
      }
    }
  }

  # Ignore fields that are dynamically added/managed by ALB Ingress Controller
  # This prevents Terraform drift from controller-managed fields
  lifecycle {
    ignore_changes = [
      metadata,
    ]
    # Critical: Force Ingress recreation when ALB Config changes.
    # Why: If AlbConfig is replaced (e.g. due to config change or forced replacement),
    # the underlying ALB instance is rebuilt. However, the Ingress resource itself (YAML)
    # might not verifyable change.
    # Without this trigger, Terraform would assume the Ingress is "no-op" and return the
    # STALE gateway address from state, skipping the `wait_for_load_balancer` logic.
    # This trigger ensures the Ingress is re-applied, forcing Terraform to wait for the
    # NEW ALB address to be assigned.
    replace_triggered_by = [
      kubernetes_manifest.alb_config
    ]
  }

  depends_on = [kubernetes_manifest.alb_ingress_class]
}

resource "kubernetes_config_map" "gateway_address" {
  depends_on = [kubernetes_ingress_v1.ragflow]

  metadata {
    name      = "ragflow-gateway-address"
    namespace = kubernetes_namespace.ragflow.metadata[0].name
  }

  data = {
    "gateway_address" = flatten([
      for s in tolist(kubernetes_ingress_v1.ragflow.status) : [
        for lb in s.load_balancer : lb.ingress[*].hostname
      ]
    ])[0]
  }
}

# =============================================================================
# Outputs
# =============================================================================

output "gateway_address" {
  description = "ALB Ingress address (hostname or IP)"
  value = flatten([
    for s in tolist(kubernetes_ingress_v1.ragflow.status) : [
      for lb in s.load_balancer :
      lb.ingress[*].hostname
    ]
  ])[0]
}
