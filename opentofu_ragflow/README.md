# Terraform Multicloud RAGFlow Deployment

Simplified Terraform configurations for deploying RAGFlow on different cloud providers and on-premises Kubernetes clusters.

## Overview

This project provides **single-file Terraform configurations** for deploying RAGFlow, with **one directory per deployment target**. Each cloud provider has its own isolated configuration to avoid API conflicts and simplify maintenance.

**Key Features:**
- 📁 **Isolated Directories**: One directory per cloud/provider
- 📄 **Single File**: Each deployment uses a single `.tf` file
- 🌐 **Multi-Cloud Support**: Aliyun, Google Cloud, On-Premises
- 🏠 **On-Premises**: Deploy on existing K8s clusters (kubeadm, RKE, K3s, etc.)
- 🔧 **Simple Deployment**: Just `cd` into the directory and run `terraform apply`

## Architecture

```
terraform_ragflow/
├── onpremises/                # On-premises K8s deployment
│   ├── main.tf               # Single file with all resources
│   └── README.md             # Usage guide
├── aliyun/                    # Aliyun Cloud deployment
│   ├── main.tf               # Single file with all resources
│   ├── example.tfvars        # Configuration template
│   └── README.md             # Usage guide
├── google/                    # Google Cloud deployment
│   ├── main.tf               # Single file with all resources
│   ├── example.tfvars        # Configuration template
│   └── README.md             # Usage guide
├── old_architecture/          # Deprecated shared files
│   ├── aliyun.tf
│   ├── google.tf
│   ├── k8s.tf
│   └── ...                   # Old multi-file architecture
└── README.md                  # This file
```

**每个云平台独立目录，单个 tf 文件实现，互不共享代码。**

## Quick Start

### On-Premises Deployment

```bash
cd onpremises
terraform init
terraform apply
```

### Aliyun Cloud Deployment

```bash
cd aliyun
terraform init
terraform plan -var-file="example.tfvars"
terraform apply -var-file="example.tfvars"
```

### Google Cloud Deployment

```bash
cd google
terraform init
terraform plan -var-file="example.tfvars"
terraform apply -var-file="example.tfvars"
```

### Access RAGFlow

After deployment, access RAGFlow at:

**On-Premises**: `http://ragflow.inf51`
**Aliyun**: `http://ragflow.aliyun.com`
**Google Cloud**: `http://ragflow.gcp.cloud`

## Deployment Options

### On-Premises (Existing K8s Cluster)

**Directory**: `onpremises/`

**Prerequisites**:
- Existing Kubernetes cluster (v1.24+)
- `kubectl` configured
- StorageClass available (rook-ceph-block, standard, etc.)
- S3-compatible storage (MinIO, Rook-Ceph RGW, etc.)
- Ingress controller (nginx-ingress recommended)

**Deployment Modes**:
- MySQL: K8s internal (StatefulSet)
- Elasticsearch: K8s internal (ECK operator)
- Storage: S3-compatible (MinIO, RGW, etc.)

**Configuration Example**:
```hcl
namespace     = "ragflow"
storage_class = "rook-ceph-block"

mysql_deployment_mode = "k8s"
es_deployment_mode    = "k8s"

s3_endpoint  = "http://rook-ceph-rgw-my-store.rook-ceph.svc:80"
s3_bucket    = "ragflow"
s3_access_key = "your-access-key"
s3_secret_key = "your-secret-key"

gateway_host = "ragflow.your-domain.com"
```

### Aliyun Cloud

**Directory**: `aliyun/`

**Prerequisites**:
- Aliyun account with access key configured
- ACK or ASK cluster quota

**Deployment Modes**:
- Kubernetes: ACK (AckBasic/AckPro) or ASK (AskBasic/AskPro)
- MySQL: RDS MySQL (cloud) or K8s internal
- Elasticsearch: Alibaba Elasticsearch (cloud) or K8s internal
- Storage: OSS (Object Storage Service)

**Quick Start**:
```bash
cd aliyun
terraform init
terraform plan -var-file="example.tfvars"
terraform apply -var-file="example.tfvars"
```

### Google Cloud

**Directory**: `google/`

**Prerequisites**:
- Google Cloud project with billing enabled
- gcloud CLI configured with appropriate credentials
- GKE quota available

**Deployment Modes**:
- Kubernetes: GKE (Google Kubernetes Engine)
- MySQL: Cloud SQL (cloud) or K8s internal
- Elasticsearch: K8s internal (no managed ES on GCP)
- Storage: GCS (Google Cloud Storage)

**Quick Start**:
```bash
cd google
terraform init
terraform plan -var-file="example.tfvars"
terraform apply -var-file="example.tfvars"
```

## Components Deployed

Each deployment creates:

1. **Namespace**: `ragflow`
2. **MySQL**: StatefulSet with PVC (K8s mode)
3. **Elasticsearch**: ECK operator with ES cluster (K8s mode)
4. **RAGFlow**: Deployment with init container for S3 bucket
5. **Secrets**: MySQL password, ES password, S3 credentials
6. **Ingress**: NGINX Ingress routing to RAGFlow service
7. **Service**: ClusterIP service for RAGFlow

## Troubleshooting

### Check Pod Status

```bash
kubectl get pods -n ragflow
```

### Check RAGFlow Logs

```bash
kubectl logs -n ragflow -l app=ragflow
```

### Check MySQL Logs

```bash
kubectl logs -n ragflow -l app=mysql
```

### Check Elasticsearch

```bash
kubectl get elasticsearch -n ragflow
kubectl describe elasticsearch elasticsearch -n ragflow
```

### Port Forward to RAGFlow

```bash
kubectl port-forward -n ragflow svc/ragflow 9380:9380
# Access at http://localhost:9380
```

## Cleanup

Remove RAGFlow deployment:

```bash
terraform destroy -var-file="inf51.tfvars"
```

## Design Philosophy

### Why Single File Per Deployment?

1. **Simplicity**: All code in one place, easy to review
2. **No Shared Code**: Avoids cloud provider API conflicts
3. **Independent Updates**: Each cloud can evolve independently
4. **Easy Debugging**: No complex conditional logic
5. **Clear Separation**: Each deployment is self-contained

### Why Separate Directories?

1. **Isolation**: Each cloud has its own terraform state
2. **Independent Workflows**: Deploy to multiple clouds simultaneously
3. **Provider Versions**: Different clouds can use different provider versions
4. **Maintainability**: Fixing one cloud doesn't break others

## Migration from Old Architecture

The old architecture (with `main.tf`, `aliyun.tf`, `k8s.tf`, etc.) is deprecated. To migrate:

1. Copy relevant configuration from old `.tf` files
2. Use new directory structure
3. Update variable names as needed
4. Test in non-production environment first

## Contributing

When adding a new cloud provider:

1. Create a new directory (e.g., `aws/`)
2. Create `main.tf` with all resources in a single file
3. Create `example.tfvars` with configuration template
4. Create `README.md` with usage instructions
5. Update this root `README.md` with the new cloud's information

## License

See project root LICENSE file.
