# RAGFlow Aliyun Cloud Deployment

Complete Terraform configuration for deploying RAGFlow on Aliyun Cloud with two-stage deployment approach.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Deployment Steps](#deployment-steps)
- [Cluster Types](#cluster-types)
- [Deployment Modes](#deployment-modes)
- [Common Issues & Solutions](#common-issues--solutions)
- [Troubleshooting](#troubleshooting)
- [Cleanup](#cleanup)
- [Components Deployed](#components-deployed)
- [References](#references)

## Overview

This Terraform configuration deploys RAGFlow on Aliyun Cloud using a two-stage approach:

1. **Stage 1 (infrastructure)**: Creates cloud resources (VPC, ACK, OSS, RDS, ES) and outputs kubeconfig
2. **Stage 2 (kubernetes)**: Deploys RAGFlow components to the Kubernetes cluster

## Architecture

### File Structure

```
terraform_ragflow/aliyun/
├── README.md                       # This file
├── variables.tf                    # Shared variables for both stages
├── terraform.tfvars               # Configuration values
├── main_ros_template.json         # ROS template for reference
├── ali_ros_ragflow.yaml           # ROS YAML configuration
├── tf_to_ros.py                   # Terraform to ROS converter
├── Dockerfile                     # Build configuration
├── remove_alicloud_provider.patch # Provider removal patch
│
├── stage1-infrastructure/         # Stage 1: Cloud infrastructure
│   └── infrastructure.tf          # Cloud resources (VPC, ACK, OSS, RDS, ES)
│
└── stage2-kubernetes/             # Stage 2: Kubernetes resources
    └── kubernetes.tf              # K8s deployments, services, ingress
```

> **Note**: Both stages use symlinks to the shared `variables.tf` file, ensuring variable definitions stay in sync.

## Prerequisites

### Required Tools

- **Aliyun CLI** - For resource management and verification
- **Terraform >= 1.5.7** (or **OpenTofu >= 1.8**) - For infrastructure deployment
- **kubectl** - For cluster verification and troubleshooting
- **Aliyun Account** - With appropriate permissions and quota

### Authentication Setup

Both Aliyun CLI and the Terraform/OpenTofu `alicloud` provider require authentication credentials to manage Aliyun resources.

#### Why Authentication is Required

- **Aliyun CLI**: Needs credentials to query and manage Aliyun resources (VPC, ECS, ACK, OSS, etc.)
- **Terraform/OpenTofu alicloud provider**: Needs credentials to create, update, and delete cloud resources during deployment
- **kubectl**: Uses kubeconfig (exported after cluster creation) to communicate with the Kubernetes cluster

#### Creating Access Keys

1. Log in to [Aliyun Console](https://ram.console.aliyun.com/manage/ak)
2. Go to **RAM** > **Users** > Create User (or use your existing account)
3. Create an **AccessKey** for programmatic access:
   - Click **Create AccessKey**
   - Save the **AccessKey ID** and **AccessKey Secret** (only shown once!)
4. Grant required permissions to the user:
   - Add user to `AdministratorAccess` policy (for full access), OR
   - Create custom policy with specific permissions

#### Setting Environment Variables

Export the following environment variables to configure authentication:

```bash
# For Aliyun CLI and alicloud provider
export ALIYUN_ACCESS_KEY_ID="YOUR_ACCESS_KEY_ID"
export ALIYUN_ACCESS_KEY_SECRET="YOUR_ACCESS_KEY_SECRET"
export ALIYUN_REGION="cn-shanghai"  # Optional, defaults to cn-hangzhou
```

**Important Security Notes**:
- Never commit access keys to version control
- Use different keys for different environments (dev/staging/prod)
- Rotate keys regularly
- Consider using RAM roles with temporary credentials for production

#### Aliyun CLI Configuration

You have two options to configure Aliyun CLI credentials:

**Option 1: Interactive Configuration (Beginner-Friendly)**

```bash
# Verify Aliyun CLI installation
aliyun version

# Configure credentials interactively
aliyun configure

# Follow prompts:
# - Access Key Id [None]: YOUR_ACCESS_KEY_ID
# - Access Key Secret [None]: YOUR_ACCESS_KEY_SECRET
# - Default Region Id [None]: cn-shanghai
# - Default Output Format [None]: json

# Verify configuration
aliyun configure list
```

**Option 2: Environment Variables (Recommended for Automation)**

```bash
# Set environment variables
export ALIYUN_ACCESS_KEY_ID="YOUR_ACCESS_KEY_ID"
export ALIYUN_ACCESS_KEY_SECRET="YOUR_ACCESS_KEY_SECRET"
export ALIYUN_REGION="cn-shanghai"

# Verify credentials work
aliyun sts GetCallerIdentity
```

Expected output:
```json
{
  "AccountId": "1694882927301628",
  "PrincipalId": "1234567890123456",
  "Arn": "acs:ram::1694882927301628:root",
  "Region": "cn-shanghai"
}
```

#### Terraform/OpenTofu Provider Configuration

The `alicloud` provider automatically reads credentials from environment variables:

```hcl
# provider "alicloud" block (usually in provider.tf)
provider "alicloud" {
  region = var.region
  # Credentials are automatically read from:
  # - ALIYUN_ACCESS_KEY_ID
  # - ALIYUN_ACCESS_KEY_SECRET
  # - ALIYUN_REGION (optional)
}
```

**Alternative: Using variables file** (`terraform.tfvars`):
```hcl
# NOT RECOMMENDED for production (security risk)
access_key = "YOUR_ACCESS_KEY_ID"
secret_key = "YOUR_ACCESS_KEY_SECRET"
region     = "cn-shanghai"
```

**Alternative: Using CLI args** (for testing only):
```bash
terraform apply \
  -var="access_key=YOUR_ACCESS_KEY_ID" \
  -var="secret_key=YOUR_ACCESS_KEY_SECRET"
```

#### Authentication Best Practices

| Method | Security | Recommended For |
|--------|----------|------------------|
| **Environment Variables** | Medium | Local development, CI/CD |
| **RAM Roles with STS** | High | Production deployments |
| **Variables File** | Low | ❌ Never commit to git |
| **CLI Arguments** | Low | ❌ Appears in shell history |

#### For Production Deployments

Use RAM Roles with temporary credentials (STS):

```bash
# Assume RAM role for elevated permissions
aliyun sts AssumeRole \
  --RoleArn "acs:ram::<account-id>:role/<role-name>" \
  --RoleSessionName "terraform-deployment" \
  --DurationSeconds 3600
```

#### Verification

Test your authentication setup before deployment:

```bash
# 1. Test Aliyun CLI
aliyun vpc DescribeVpcs --RegionId cn-shanghai

# 2. Test Terraform/OpenTofu
cd stage1-infrastructure
tofu init
tofu plan

# 3. Test kubectl (after cluster creation)
export KUBECONFIG=../kubeconfig.tf
kubectl get nodes
```

### RAM Roles and Service Linked Roles

Before deploying RAGFlow on Aliyun, you need to create specific RAM roles and enable service-linked roles for ACK/ASK clusters.

#### Why Are RAM Roles Required?

ACK (Alibaba Cloud Container Service for Kubernetes) and ASK (Serverless Kubernetes) require RAM roles for:

1. **Worker Node Management** - Creating/managing ECS instances, attaching disks, network configuration
2. **Storage Provisioning** - Dynamic provisioning of cloud disks (ESSD) for persistent volumes
3. **Load Balancing** - Creating and managing SLB/ALB for services
4. **Log Collection** - Sending container logs to SLS (Simple Log Service)
5. **Monitoring** - Integrating with CloudMonitor (CMS) and ARMS/Prometheus
6. **Network Management** - Managing VPC routes, security groups, and ENIs

#### Creating RAM Roles Using Aliyun CLI

A helper script is provided to create all required roles automatically:

```bash
# Navigate to the deployment directory
cd opentofu_ragflow/aliyun

# Run the role creation script
bash setup_ram_roles.sh
```

This script creates the following **16 RAM roles**:

| Role Name | Purpose |
|-----------|---------|
| `AliyunCSManagedVKRole` | ACK virtual node (ECI) management |
| `AliyunCSManagedNlcRole` | Network load controller |
| `AliyunCSManagedAutoScalerRole` | Cluster autoscaler |
| `AliyunOOSLifecycleHook4CSRole` | OOS lifecycle hooks |
| `AliyunCCCSIPluginRole` | CSI plugin operations |
| `AliyunCSDefaultRole` | Worker node default role |
| `AliyunCSManagedKubernetesRole` | Managed cluster control plane |
| `AliyunCSManagedLogRole` | SLS log collection |
| `AliyunCSManagedCmsRole` | CloudMonitor integration |
| `AliyunCSManagedCsiRole` | CSI storage operations |
| `AliyunCSKubernetesAuditRole` | Kubernetes audit logging |
| `AliyunCSManagedNetworkRole` | Network management |
| `AliyunCSManagedArmsRole` | ARMS/Prometheus integration |
| `AliyunCSServerlessKubernetesRole` | ASK (Serverless) cluster |
| `AliyunCSManagedCsiPluginRole` | CSI plugin operations |
| `AliyunCSManagedCsiProvisionerRole` | CSI provisioner operations |

#### Manual Role Creation (Optional)

If you prefer to create roles manually via the RAM Console:

1. Visit [RAM Console > Roles](https://ram.console.aliyun.com/roles)
2. Click **Create Role**
3. Select **Alibaba Cloud Service**
4. Choose the service (e.g., Container Service)
5. Select the role template (e.g., `AliyunCSDefaultRole`)
6. Click **Create**

#### Verifying RAM Roles

After creation, verify all roles exist:

```bash
# List all CS-related roles
aliyun ram ListRoles --MaxItems 50 | grep -i "CS"

# Check specific role details
aliyun ram GetRole --RoleName AliyunCSDefaultRole
```

#### Service Linked Roles

Two service-linked roles must be created for cluster operation:

| Service | Role Name | Purpose |
|---------|-----------|---------|
| **CS** (Container Service) | Auto-created during cluster creation | Manages cluster resources |
| **OSS** (Object Storage) | AliyunOSSServiceRole | Enables CSI driver to access OSS buckets |

**Automatic Creation**: These roles are typically created automatically when you first create an ACK/ASK cluster.

**Manual Creation** (if needed):

```bash
# Create service-linked role for CS
aliyun ram CreateServiceLinkedRole \
    --ServiceName "CS" \
    --TemplateId "ServiceLinkedRoleForCS"

# Create service-linked role for OSS
aliyun ram CreateServiceLinkedRole \
    --ServiceName "OSS" \
    --TemplateId "ServiceLinkedRoleForOSS"
```

### ACR Credential Helper (Private Image Pull)

The aliyun-acr-credential-helper component is automatically installed during infrastructure deployment to enable passwordless private image pulls from Aliyun Container Registry (ACR).

#### Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         ACK Cluster                                          │
│  ┌───────────────┐         Worker RAM Role assumes                         │
│  │   Pod Workload│ ───────────────────────────────┐                      │
│  └───────────────┘                                 │                      │
│           │                                         ▼                      │
│           │                              ┌──────────────────┐             │
│           │                              │   ACRPullRole    │             │
│           └─────────────────────────────>│   (Custom Role)  │             │
│            Injects temporary credentials  └──────────────────┘             │
│                                                  │                         │
│                                                  ▼                         │
│                                         ┌─────────────────┐               │
│                                         │  Aliyun ACR      │               │
│                                         │  Enterprise      │               │
│                                         └─────────────────┘               │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### Automatic Installation

The acr-credential-helper addon is automatically configured in:
- **Terraform/OpenTofu**: [infrastructure.tf](stage1-infrastructure/infrastructure.tf) - `alicloud_cs_serverless_kubernetes` resource
- **ROS Template**: [ali_ros_ragflow.yaml](ali_ros_ragflow.yaml) - `AskCluster` resource

#### Verification

```bash
# 1. Check if acr-credential-helper Pod is running
kubectl get pods -n kube-system -l app=acr-credential-helper

# 2. Check ConfigMap is created
kubectl get configmap acr-configuration -n kube-system -o yaml

# 3. Test image pull (no imagePullSecrets needed)
kubectl run test-acr --image=infiniflow-registry.cn-shanghai.cr.aliyuncs.com/infiniflow/ragflow:latest --rm -it --restart=Never --command -- /bin/sh
```

#### Usage

After configuration, Pods can use private images directly without `imagePullSecrets`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ragflow
spec:
  template:
    spec:
      containers:
        - name: ragflow
          # Private image works directly, no imagePullSecrets needed
          image: infiniflow-registry.cn-shanghai.cr.aliyuncs.com/infiniflow/ragflow:latest
```

#### Troubleshooting

```bash
# View acr-credential-helper logs
kubectl logs -n kube-system -l app=acr-credential-helper --tail=100

# Verify ConfigMap JSON format
kubectl get configmap acr-configuration -n kube-system -o jsonpath='{.data.ACR_CONFIGURATION}' | jq .
```

### Enabling Required Services

Before deploying, ensure these Aliyun services are enabled in your account:

| Service | Required For | How to Enable |
|---------|--------------|---------------|
| **ACK (Container Service)** | Kubernetes cluster | Auto-enabled on first cluster creation |
| **OSS (Object Storage)** | RAGFlow data storage | Auto-enabled on bucket creation |
| **RDS (ApsaraDB)** | MySQL database (optional) | Purchase RDS instance via console |
| **Elasticsearch** | Vector search (optional) | Purchase ES instance via console |

### Checking Service Quota

Verify you have sufficient quota for deployment:

```bash
# Check ACK cluster quota
aliyun cs DescribeClusterUserQuota

# Check ECS quota (for worker nodes)
aliyun ecs DescribeInstanceAttribute --InstanceId <test-instance>

# Check ESSD disk quota
aliyun ecs DescribeDiskMonitorData --RegionId cn-shanghai
```

### Minimum Quota Requirements

| Resource | Minimum Quantity | Recommended |
|----------|------------------|-------------|
| ACK Clusters | 1 | 2-3 (for dev/staging/prod) |
| ECS Instances | 2 | 4-6 |
| ESSD Storage | 200 GB | 500 GB - 2 TB |
| SLB Instances | 1 | 2-3 |
| OSS Buckets | 1 | 3-5 |

### Configuration File Setup

Before deployment, copy and configure the variables file:

```bash
# Copy example configuration
cp terraform.tfvars.example terraform.tfvars

# Edit with your values
vim terraform.tfvars
```

Required minimum configuration:
```hcl
# Environment
environment  = "dev"
project_name = "ragflow"
region       = "cn-shanghai"

# Network (leave empty to create new)
vpc_id      = ""
vswitch_ids = []
```

## Quick Start

### Using Helper Script

```bash
cd terraform_ragflow/aliyun

# Stage 1: Deploy infrastructure
cd stage1-infrastructure
terraform init
terraform apply -out=tfplan
terraform apply tfplan

# Export kubeconfig for Stage 2
terraform output -raw kubeconfig > ../kubeconfig.tf

# Stage 2: Deploy Kubernetes resources
cd ../stage2-kubernetes
export KUBECONFIG=../kubeconfig.tf
terraform init
terraform apply -out=tfplan
terraform apply tfplan
```

## Configuration

All configuration is done through variables defined in `variables.tf`. You have several options to set variable values:

### Option 1: Using `.tfvars` file

```bash
# Copy example and edit
cp terraform.tfvars.example terraform.tfvars
vim terraform.tfvars

# Terraform will auto-load terraform.tfvars
terraform apply
```

### Option 2: Using environment variables (CI/CD friendly)

```bash
export TF_VAR_region="cn-shanghai"
export TF_VAR_mysql_password="secret"
terraform apply
```

### Option 3: Using CLI arguments

```bash
terraform apply -var="region=cn-hangzhou" -var="ragflow_replicas=3"
```

### Example Configuration

```hcl
# Environment
environment  = "dev"
project_name = "ragflow"
region       = "cn-hangzhou"

# Network
vpc_id        = ""  # Leave empty to create new VPC
vpc_cidr      = "10.0.0.0/16"
vswitch_ids   = []  # Leave empty to create new VSwitches
vswitch_cidrs = ["10.0.1.0/24", "10.0.2.0/24"]

# Kubernetes Cluster
cluster_type       = "AckPro"  # AckBasic, AckPro, AskBasic, AskPro
kubernetes_version = "1.30.0-aliyun.1"

# Storage
existing_bucket_name = ""  # Leave empty to create new OSS bucket

# MySQL (RDS) - High Availability Edition
mysql_deployment_mode = "cloud"  # Use Aliyun RDS
mysql_instance_class  = "rds.mysql.s2.large"  # High Availability edition
mysql_storage         = 100
zone_id               = "cn-hangzhou-i"  # Primary zone
zone_id_2             = "cn-hangzhou-h"  # Secondary zone for HA (optional but recommended)

# Elasticsearch (Aliyun ES) - Vector Enhanced Edition
es_deployment_mode         = "cloud"  # Use Aliyun Elasticsearch
es_version                = "8.17_with_X-Pack"  # Vector Enhanced (RAGFlow requires ES 8.x)
elasticsearch_node_count   = 3
elasticsearch_node_spec    = "elasticsearch.sn2ne.2xlarge"  # 32GB memory - Recommended
elasticsearch_disk_size    = 500  # GB
elasticsearch_disk_type    = "cloud_essd"  # ESSD recommended
es_disk_performance_level = "PL2"  # Performance level for ESSD

# Gateway
gateway_host = "ragflow.aliyun.com"
```

### Key Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `region` | `cn-shanghai` | Aliyun region |
| `zone_id` | `cn-shanghai-b` | Primary availability zone |
| `vpc_option` | `new` | Create new VPC or use existing |
| `mysql_deployment_mode` | `k8s` | MySQL: `k8s` or `cloud` |
| `es_deployment_mode` | `k8s` | Elasticsearch: `k8s` or `cloud` |
| `es_version` | `8.17_with_X-Pack` | ES version for vector search (RAGFlow requires 8.x) |
| `es_node_spec` | `elasticsearch.sn2ne.2xlarge` | Node spec (memory-optimized for vector) |
| `es_disk_size` | `500` | Disk size per node in GB (ESSD supports up to 12TB) |
| `es_disk_type` | `cloud_essd` | Disk type: cloud_ssd or cloud_essd |
| `es_disk_performance_level` | `PL2` | ESSD performance level: PL1/PL2/PL3/PL4 |
| `ragflow_replicas` | `1` | Number of RAGFlow replicas |

## Deployment Steps

### Stage 1: Cloud Infrastructure

```bash
cd stage1-infrastructure

# Initialize Terraform
terraform init

# Review the plan
terraform plan -out=tfplan

# Apply changes
terraform apply tfplan

# Export kubeconfig for Stage 2
terraform output -raw kubeconfig > ../kubeconfig.tf
```

**Stage 1 Outputs:**
- `kubeconfig`: Kubernetes cluster configuration (export to file)
- `cluster_id`: Kubernetes cluster ID

### Stage 2: Kubernetes Resources

```bash
cd ../stage2-kubernetes

# Verify kubeconfig exists
ls ../kubeconfig.tf

# Test cluster connection
export KUBECONFIG=../kubeconfig.tf
kubectl get nodes

# Initialize Terraform
terraform init

# Review the plan
terraform plan -out=tfplan

# Apply changes
terraform apply tfplan
```

**Stage 2 Outputs:**
- `gateway_address`: ALB Ingress address (hostname or IP)

### Access RAGFlow

After Stage 2 completes, access RAGFlow using the gateway address:

```bash
# Get the gateway address
terraform output gateway_address

# Or get the address from kubectl
kubectl get ingress -n ragflow
```

## Cluster Types

| Type | Description |
|------|-------------|
| **AckBasic** | Basic edition of ACK (Alibaba Cloud Container Service for Kubernetes) |
| **AckPro** | Pro edition of ACK with enhanced features |
| **AskBasic** | Basic edition of ASK (Serverless Kubernetes) |
| **AskPro** | Pro edition of ASK |

## Deployment Modes

### MySQL Deployment Modes
- **cloud**: Use Aliyun RDS (recommended for production)
- **k8s**: Deploy MySQL in Kubernetes (StatefulSet)

### Elasticsearch Deployment Modes
- **cloud**: Use Aliyun Elasticsearch (recommended for production)
- **k8s**: Deploy Elasticsearch in Kubernetes using ECK operator

## Elasticsearch Vector Enhanced Edition

### Overview

RAGFlow requires Elasticsearch 8.x for optimal vector search functionality. Aliyun offers **Vector Enhanced Edition** (向量增强版) with support for:
- Dense vector fields
- K-Nearest Neighbor (kNN) search
- Vector similarity scoring
- High-performance vector indexing

### Supported Versions

| Version | Description | Release Date |
|----------|-------------|--------------|
| **8.17_with_X-Pack** | Latest Vector Enhanced (Recommended) | 2026 |
| **8.15_with_X-Pack** | Vector Enhanced | 2025 |
| 7.16_with_X-Pack | Kernel Enhanced (no vector support) | 2024 |
| 7.10_with_X-Pack | Kernel Enhanced (no vector support) | 2023 |

**Important**: Only 8.17 and 8.15 support vector search features. Use 8.x versions for RAGFlow.

### Instance Category

Vector Enhanced Edition requires `instance_category = "x-pack"`:
- **x-pack**: Full X-Pack commercial features with vector search support
- **advanced**: Log-optimized version (no vector support)
- **IS**: Integration Service edition (limited features)

### Recommended Specifications

#### Memory-Optimized Series (sn2ne) - **Recommended for Vector Search**

| Spec | Memory | Use Case |
|-------|---------|------------|
| `elasticsearch.sn2ne.large` | 8 GB | Small-scale testing |
| `elasticsearch.sn2ne.xlarge` | 16 GB | Small-scale production |
| `elasticsearch.sn2ne.2xlarge` | 32 GB | **Recommended** |
| `elasticsearch.sn2ne.4xlarge` | 64 GB | Large-scale |
| `elasticsearch.sn2ne.8xlarge` | 128 GB | Extra large-scale |

#### Ultra High Memory Series (r7a)

| Spec | Memory | Use Case |
|-------|---------|------------|
| `elasticsearch.r7a.8xlarge` | 256 GB | Ultra high memory requirements |

### Storage Configuration

#### Disk Types

| Type | Max Size | Recommendation |
|-------|-----------|---------------|
| `cloud_ssd` | 2,048 GB | Standard |
| `cloud_essd` | **12,288 GB** | **Recommended for vector search** |

#### ESSD Performance Levels

| Level | Min Size | Max Size | Description |
|-------|-----------|-----------|-------------|
| **PL1** (Standard) | 40 GB | 12,288 GB | Basic performance |
| **PL2** (Recommended) | 20 GB | 12,288 GB | **Recommended for vector search** |
| **PL3** (High Performance) | 461 GB | 12,288 GB | High IOPS requirements |
| **PL4** (Ultra High) | 1,261 GB | 12,288 GB | Maximum performance |

### High Availability Configuration

The Terraform configuration automatically includes:

#### Dedicated Master Nodes
```hcl
master_node_configuration {
  spec      = "elasticsearch.sn1ne.large"  # 3 nodes, 4GB each
  amount    = 3
  disk      = 20
  disk_type = "cloud_ssd"
}
```

**Benefits**:
- Isolates master node responsibilities from data nodes
- Prevents cluster instability during heavy vector indexing
- Improves query performance for vector search

#### Data Nodes (Memory-Optimized)
```hcl
data_node_amount    = 3
data_node_spec     = "elasticsearch.sn2ne.2xlarge"  # 32GB each
data_node_disk_size = 500  # GB
data_node_disk_type = "cloud_essd"
```

### Configuration Examples

#### Development/Testing Environment
```hcl
es_version                = "8.17_with_X-Pack"
es_node_count            = 2
elasticsearch_node_spec     = "elasticsearch.sn2ne.large"  # 8GB
elasticsearch_disk_size    = 200
es_disk_performance_level = "PL1"
```

#### Small-Scale Production
```hcl
es_version                = "8.17_with_X-Pack"
es_node_count            = 3
elasticsearch_node_spec     = "elasticsearch.sn2ne.2xlarge"  # 32GB
elasticsearch_disk_size    = 500
es_disk_performance_level = "PL2"
```

#### Large-Scale Production
```hcl
es_version                = "8.17_with_X-Pack"
es_node_count            = 6
elasticsearch_node_spec     = "elasticsearch.sn2ne.4xlarge"  # 64GB
elasticsearch_disk_size    = 1000
es_disk_performance_level = "PL2"
```

#### Ultra High Memory Configuration
```hcl
es_version                = "8.17_with_X-Pack"
es_node_count            = 6
elasticsearch_node_spec     = "elasticsearch.r7a.8xlarge"  # 256GB
elasticsearch_disk_size    = 2048
es_disk_performance_level = "PL3"
```

### Verification

After deployment, verify vector search capabilities:

```bash
# Check ES version
curl -u elastic:<password> https://<es-endpoint>:9200
{
  "name" : "ragflow-es",
  "version" : {
    "number" : "8.17.0",
    "build_flavor" : "default"
  }
}

# Verify X-Pack features
curl -u elastic:<password> https://<es-endpoint>:9200/_license
```

### References

- [Aliyun ES Vector Enhanced Documentation](https://help.aliyun.com/zh/es/product-overview/overview-6)
- [Elasticsearch 8.x Vector Search](https://www.elastic.co/guide/en/elasticsearch/reference/current/vector-search.html)
- [RAGFlow ES Requirements](https://github.com/infiniflow/ragflow)

## Common Issues & Solutions

### ECK CRD Registration Issue

#### Problem Description

`terraform plan` validates `kubernetes_manifest` resource CRDs exist, even with `depends_on` added. This causes:

```
Error: API did not recognize GroupVersionKind from manifest (CRD may not be installed)
no matches for kind "Elasticsearch" in group "elasticsearch.k8s.elastic.co"
```

#### Solution: Phased Deployment

Use phased deployment to install ECK Operator (with CRDs) first, then deploy other resources.

**One-Click Deployment (Recommended):**

```bash
cd terraform_ragflow/aliyun
./apply_eck_phase1.sh
```

The script automatically:
1. **Phase 1**: Install ECK Operator Helm chart
2. **Phase 2**: Wait for CRDs to register with Kubernetes API
3. **Phase 3**: Deploy all remaining resources

**Manual Phased Deployment:**

```bash
cd terraform_ragflow/aliyun

# Step 1: Only install ECK Operator
terraform apply -target=helm_release.eck_operator -target=time_sleep.wait_for_eck_crds

# Step 2: Wait for CRDs to register
kubectl get crd elasticsearches.elasticsearch.k8s.elastic.co

# Step 3: Deploy all resources
terraform apply
```

#### Troubleshooting ECK Issues

**If Phase 1 Fails:**

```bash
# Cleanup and retry
./cleanup_eck_complete.sh
terraform apply -target=helm_release.eck_operator
```

**If CRD Registration Times Out:**

```bash
# Manually check CRDs
kubectl get crd | grep elastic

# If CRDs don't exist, check Helm release
helm list -n elastic-system

# If Helm release is abnormal, uninstall and retry
helm uninstall eck-operator -n elastic-system
./cleanup_eck_complete.sh
```

**Verify Deployment Success:**

```bash
# Check ECK Operator
kubectl get pods -n elastic-system

# Check Elasticsearch CR
kubectl get elasticsearch -n ragflow

# Check Elasticsearch Pods
kubectl get pods -n ragflow -l elasticsearch.k8s.elastic.co/cluster-name=elasticsearch
```

### Kubernetes Provider Limitations

#### Problem: Cross-Stage Resource Dependencies

When using Kubernetes Provider in Terraform/OpenTofu to create a cluster and deploy resources in the same `apply`, you may encounter:

```
Error: Failed to construct REST client
  with kubernetes_manifest.alb_config:
cannot create REST client: no client config
```

#### Root Cause

1. Terraform/OpenTofu execution flow requires all Providers to be initialized during Plan phase
2. Provider configuration depends on cluster resources that don't exist yet
3. `depends_on` only affects Apply phase, not Plan phase

#### Solutions

**Solution 1: Two-Stage Deployment (Recommended for This Project)**

This configuration already uses the recommended two-stage approach:
- Stage 1: Create cloud infrastructure and export kubeconfig
- Stage 2: Use exported kubeconfig to deploy Kubernetes resources

**Solution 2: Using `-target` for Single Command**

```bash
tofu apply -auto-approve \
  -target=alicloud_cs_serverless_kubernetes.main \
  -target=data.alicloud_cs_cluster_credential.main
```

**Solution 3: Manual Step-by-Step**

```bash
# Step 1: Create cluster
tofu apply -target=alicloud_cs_serverless_kubernetes.main -auto-approve

# Step 2: Deploy all resources
tofu apply -auto-approve
```

### Terraform kubernetes_manifest CRD Issues

#### Problem: CRD Forced Replacement

When managing Aliyun AlbConfig custom resources, `terraform plan` shows resources need **destroy and then create replacement**:

```
# kubernetes_manifest.alb_config must be replaced
~ object.metadata = {
    - finalizers = ["ingress.k8s.alibaba/resources"]
    - labels     = {"alb.ingress.kubernetes.io/hash" = "..."}
  }
```

#### Root Cause

1. ALB Ingress Controller dynamically adds `finalizers` and `labels` to AlbConfig resources
2. Terraform compares cluster actual state (`object` field) with configuration file (`manifest` field)
3. **No OpenAPI schema** - Warning: "This custom resource does not have an associated OpenAPI schema"
4. HashiCorp `kubernetes_manifest` Provider architecture limitation requires CRD OpenAPI Schema for patch operations

#### Recommended Solutions

**Solution 1: Use `null_resource` + `kubectl apply` (Most Recommended)**

Bypass Terraform's comparison mechanism using `kubectl apply` native Client-Side Apply capability:

```hcl
variable "alb_config_yaml" {
  description = "AlbConfig YAML content"
  type        = string
  default     = <<YAML
apiVersion: alibabacloud.com/v1
kind: AlbConfig
metadata:
  name: ragflow-alb
spec:
  config:
    name: ragflow-alb
    addressType: Internet
YAML
}

# Write YAML to disk
resource "local_file" "alb_config_manifest" {
  content  = var.alb_config_yaml
  filename = "${path.module}/alb-config.yaml"
}

# Apply using kubectl
resource "null_resource" "apply_alb_config" {
  triggers = {
    manifest_sha = sha256(var.alb_config_yaml)
  }

  provisioner "local-exec" {
    command = "kubectl apply -f ${local_file.alb_config_manifest.filename}"
  }

  depends_on = [kubernetes_namespace.ragflow]
}
```

**Solution 2: Use `helm_release` Wrapper**

Helm handles differences through 3-way merge patch:

```hcl
resource "helm_release" "alb_config" {
  name       = "alb-config-manager"
  namespace  = "ragflow"
  chart      = "${path.module}/charts/alb-config"

  force_update  = false
  recreate_pods = false
}
```

## Troubleshooting

### Stage 1 Issues

- **VPC/VSwitch errors**: Ensure you have quota in the selected region/zone
- **RDS creation fails**: Check if the instance class is available in your region
- **OSS bucket exists**: Set `existing_bucket_name` variable

### Stage 2 Issues

- **kubeconfig not found**: Run Stage 1 first and export kubeconfig
- **Cluster connection fails**: Verify kubeconfig is valid and cluster is ready
- **Image pull errors**: Ensure you have access to the container registry

### General Troubleshooting

Check pod status:
```bash
kubectl get pods -n ragflow
```

Check RAGFlow logs:
```bash
kubectl logs -n ragflow -l app=ragflow
```

Check ingress status:
```bash
kubectl get ingress -n ragflow
```

### ALB 503 Service Unavailable

If you encounter 503 errors when accessing the ALB:

**Diagnosis Steps:**

1. **Check ALB Status:**
   ```bash
   # Get ALB hostname
   kubectl get ingress ragflow -n ragflow -o jsonpath='{.status.loadBalancer[0].ingress[0].hostname}'
   ```

2. **Verify Pod Health:**
   ```bash
   # Ensure pods respond to HTTP GET / on port 80
   kubectl exec -n ragflow <ragflow-pod-name> -- curl -s http://localhost:80/
   ```

3. **Check Service Endpoints:**
   ```bash
   kubectl get endpoints -n ragflow ragflow
   ```

4. **Check Security Group:**
   - Ensure port 80 is allowed in the security group associated with ALB
   - Check both inbound and outbound rules

**Common Causes:**
- Health Checks failing (Pods not returning 200 OK on path /)
- Security Group rules blocking ALB -> Pod communication
- Service type mismatch

### PVC Storage Size Requirements

Aliyun ACS/ACK requires PVC sizes to be at least **20Gi**. The configuration already handles this:

```hcl
# In variables.tf - Already configured with 20Gi minimum
variable "mysql_k8s_storage" {
  default     = 20
  description = "Aliyun PVC shall be no less than 20Gi"
}

variable "es_k8s_storage" {
  default     = 20
  description = "Aliyun PVC shall be no less than 20Gi"
}

variable "rabbitmq_storage" {
  default     = 20
  description = "Aliyun PVC shall be no less than 20Gi"
}
```

### OSS Bucket Access

The deployment includes an init container to verify S3/OSS bucket access. If bucket access fails:

1. **Verify Bucket Exists:**
   ```bash
   aws s3 ls s3://<bucket-name> \
     --region cn-shanghai \
     --endpoint-url http://oss-cn-shanghai-internal.aliyuncs.com
   ```

2. **Check Credentials:**
   - Ensure RAM user has `oss:*` permissions
   - Verify S3_ACCESS_KEY and S3_SECRET_KEY are correct

3. **Bucket Creation:**
   If bucket doesn't exist, it will be created automatically during Stage 1 deployment.
   Bucket names must be globally unique across Aliyun.

## Cleanup

To destroy all resources:

```bash
# Destroy Stage 2 first
cd stage2-kubernetes
terraform destroy

# Destroy Stage 1
cd ../stage1-infrastructure
terraform destroy
```

Or use a single command:
```bash
terraform destroy -var-file="example.tfvars"
```

## GPU Support (Future Reference)

Currently, GPU is not purchased for Aliyun deployment. The following notes are for future reference if GPU support is needed:

### GPU Options on Aliyun

1. **ACK GPU Nodes**: Use ACK with GPU-enabled ECS instances
2. **ACS with vGPUs**: Use Aliyun's vGPU solution (HAMi)

### GPU Configuration Variables

If GPU is needed in the future, the following variables can be configured:

| Variable | Description |
|----------|-------------|
| `enable_gpu` | Enable GPU support |
| `gpu_type` | GPU type (e.g., NVIDIA_T4, NVIDIA_A10) |
| `gpu_count` | Number of GPUs per node |

### Related Documentation

- See `KNATIVE_GPU_*.md` files in `pulumi_ragflow/` directory for Knative GPU implementation details
- See `ACS_GPU_CONFIGURATION_SUMMARY.md` for ACS GPU configuration
- See `ALIYUN_GPU_SUPPORT_TICKET.md` for Aliyun GPU support ticket process

## Components Deployed

1. **VPC and VSwitches**: Network infrastructure
2. **OSS Bucket**: Object storage for RAGFlow data
3. **RAM User**: Service account for K8s pods to access OSS
4. **MySQL RDS**: Cloud database (if `mysql_deployment_mode = "cloud"`)
5. **Elasticsearch**: Cloud search engine (if `es_deployment_mode = "cloud"`)
6. **ACK/ASK Cluster**: Managed Kubernetes
7. **RAGFlow**: Application deployment
8. **Ingress**: Gateway routing

## Notes

- OSS bucket access keys are saved to `~/.ragflow-ram-access-key-secret-<environment>`
- For production use, consider:
  - Enable TLS with proper certificates
  - Use PrePaid payment type for cost savings
  - Increase resource limits based on workload
  - Configure proper backups for RDS and Elasticsearch

## References

- Aliyun ROS Terraform support: https://help.aliyun.com/zh/ros/user-guide/ros-features-and-resources-supported-by-terraform
- RAGFlow Documentation: https://github.com/infiniflow/ragflow
- HashiCorp Kubernetes Provider Issues: https://github.com/hashicorp/terraform-provider-kubernetes/issues
- Kubernetes Server-Side Apply: https://kubernetes.io/docs/reference/using-api/server-side-apply/

---

*Document Version: 2.0*
*Last Updated: 2026-02-11*
