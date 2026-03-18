# RAGFlow BYOK Deployment

Single-file Terraform configuration for deploying RAGFlow on existing Kubernetes clusters.

## Acronyms

| Acronym | Full Name | Description |
|---------|-----------|-------------|
| **BYOK** | Bring Your Own Kubernetes | Deploy RAGFlow on an existing Kubernetes cluster you manage |
| **SMK** | Self-Managed Kubernetes | A Kubernetes cluster that you provision and manage yourself (on-premises, VM-based, etc.) |
| **GKE** | Google Kubernetes Engine | Google's managed Kubernetes service (includes GKE Autopilot and GKE Standard) |
| **GKE Autopilot** | GKE Autopilot | Fully managed Kubernetes service where Google handles node management |
| **GKE Standard** | GKE Standard | Managed Kubernetes service where you manage node pools yourself |

## Cluster Types Supported

| Cluster Type | Description |
|--------------|-------------|
| **GKE (Autopilot only)** | Google Cloud GKE with Autopilot mode |
| **SMK** | Self-Managed Kubernetes (on-premises, VM-based clusters using kubeadm, kubespray, etc.) |

## Prerequisites

### Common Requirements

- Existing Kubernetes cluster (v1.24+)
- `kubectl` configured to access the cluster
- Available StorageClass (e.g., `rook-ceph-block`, `standard`)
- S3-compatible storage (MinIO, Rook-Ceph RGW, etc.)
- Ingress controller (nginx-ingress recommended)
- **OpenTofu** (or Terraform 1.8.0+)
- **Python 3** with `wait_for_k8s_resource.py` script in the same directory

### Cloud-Specific Prerequisites

#### GKE (Google Cloud Platform - Autopilot Only)

> **Important:** This deployment supports **GKE Autopilot only**. Standard mode is not supported because:
> - The two modes implement Elasticsearch mmap differently
> - Autopilot mode can use computeclass for ES deployment
> - Standard mode may require DaemonSet configuration (not yet implemented in main.tf)

**1. Create a VM with Full Access**

When creating a VM in GCP, ensure it has the following:
- **Compute Engine**: Full access to all Cloud APIs
- **Storage**: Sufficient disk space (50GB+ recommended)
- **Service Account**: Default compute service account with appropriate roles

Required roles for the service account:
- **Kubernetes Engine Admin** (`roles/container.admin`)
- **Service Usage Consumer** (`roles/servicemanagement.usageServiceConsumer`)

**2. Configure GKE Cluster Access**

Use the setup script to configure kubectl access:

```bash
# Run the GKE configuration helper script
python3 gke_setup.py
```

The script will:
1. Check and install kubectl (if needed)
2. Check and install gcloud CLI (if needed)
3. Check and install gke-gcloud-auth-plugin (if needed)
4. List available GKE clusters
5. Enable Gateway API on the selected cluster
6. Create GCS bucket for RAGFlow storage (if not exists)
7. Create GCS service account with Workload Identity
8. Generate kubeconfig with automatic token refresh
9. Create a timestamped token-based kubeconfig for other environments

For details, see `python3 gke_setup.py -h`.

**3. Configure terraform.tfvars for GCP**

The default resource sizing in `variables.tf` follows production requirements. You can customize resources in `terraform.tfvars` as needed. See [Resource Sizing](#resource-sizing) for available configuration options.

#### Self-Managed Kubernetes (SMK) Cluster Setup

For Self-Managed Kubernetes (SMK) clusters, you need to set up a Gateway API controller and load balancer before deploying RAGFlow. This section covers the recommended setup using NGINX Gateway Fabric with MetalLB.

##### Gateway API Controllers

RAGFlow uses the Kubernetes Gateway API for ingress. You have two options:

##### Option 1: NGINX Gateway Fabric + MetalLB (Recommended)

This is the recommended setup for Self-Managed Kubernetes (SMK) clusters, providing:
- NGINX Gateway Fabric as the Gateway API controller
- MetalLB for LoadBalancer IP address allocation

**Quick Setup:**

```bash
# 1. Install Gateway API CRDs
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.1/standard-install.yaml

# 2. Install NGINX Gateway Fabric
helm install nginx-gateway oci://ghcr.io/nginx/charts/nginx-gateway-fabric \
  --version 2.2.2 \
  --namespace nginx-gateway \
  --create-namespace \
  --set service.ports[0].port=80 \
  --set service.ports[0].targetPort=80 \
  --set service.ports[1].port=443 \
  --set service.ports[1].targetPort=443 \
  --set service.ports[2].port=9380 \
  --set service.ports[2].targetPort=9380 \
  --set service.ports[3].port=9381 \
  --set service.ports[3].targetPort=9381 \
  --set service.ports[4].port=9382 \
  --set service.ports[4].targetPort=9382

# 3. Install MetalLB
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.15.3/config/manifests/metallb-native.yaml

# 4. Wait for MetalLB to be ready
kubectl wait --for=condition=ready pod -l app=metallb,component=controller -n metallb-system --timeout=60s

# 5. Apply IP pool configuration
kubectl apply -f smk-ip-pool.yaml

# 6. Verify GatewayClass
kubectl get gatewayclass
```

**Required Manifest Files:**

| File | Description |
|------|-------------|
| `smk-ip-pool.yaml` | MetalLB IP address pool configuration |
| `smk-allow-metallb-webhook.yaml` | Cilium network policy for MetalLB webhook (if using Cilium CNI) |

**IP Pool Configuration (smk-ip-pool.yaml):**

```yaml
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: my-lan-pool
  namespace: metallb-system
spec:
  addresses:
  - 192.168.1.200-192.168.1.254  # Adjust to your network range
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: my-lan-advertisement
  namespace: metallb-system
spec:
  ipAddressPools:
  - my-lan-pool
```

**Cilium Integration (if using Cilium CNI):**

If your Self-Managed Kubernetes (SMK) cluster uses Cilium CNI, apply the MetalLB webhook policy:

```bash
kubectl apply -f smk-allow-metallb-webhook.yaml
```

##### Option 2: Cilium Gateway (if using Cilium CNI)

If your cluster already has Cilium installed with L2 announcement support, you can use Cilium Gateway instead:

```bash
# Apply Cilium L2 announcement policy
kubectl apply -f smk-cilium-l2-policy.yaml
```

**Note:** Update the interface name in `smk-cilium-l2-policy.yaml` to match your physical network interface (e.g., `^eth0`, `^ens.*`).

##### Verifying Gateway Setup

After setup, verify the Gateway is ready:

```bash
# Check GatewayClass
kubectl get gatewayclass

# Check Gateway status
kubectl get gateway -A

# Check MetalLB IP pool
kubectl get ipaddresspool -n metallb-system
```

##### Troubleshooting

##### MetalLB Not Assigning External IP

```bash
# Check MetalLB controller logs
kubectl logs -n metallb-system -l component=controller

# Verify IP pool exists
kubectl get ipaddresspool -n metallb-system

# Check L2Advertisement
kubectl get l2advertisement -n metallb-system
```

##### NGINX Gateway Not Ready

```bash
# Check NGINX Gateway Fabric pods
kubectl get pods -n nginx-gateway

# Check Gateway status
kubectl describe gateway -n nginx-gateway
```

---

## Quick Start

### 1. Create Configuration File

Copy the appropriate environment configuration file to `terraform.tfvars`:

```bash
# For Self-Managed Kubernetes (SMK) / development environment
cp terraform.tfvars.dev_smk terraform.tfvars
```

Then edit `terraform.tfvars` with your cloud-specific settings as needed.

### 2. Initialize Terraform

```bash
tofu init -upgrade
```

### 3. Review Configuration

```bash
tofu plan
```

### 4. Deploy RAGFlow

```bash
tofu apply -auto-approve
```

## Cloud Provider Configuration

### Image Registry Configuration

RAGFlow supports flexible image registry configuration through two variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `private_registry` | Private container registry for RAGFlow and DeepDoc images | `gcr.io/ragflow-462809` or `gcr.io/ragflow-462809`, `192.168.1.51/infiniflow-ai` |
| `public_registry` | Public registry for third-party images (MySQL, Redis, TEI, RabbitMQ, etc.). If empty, uses default registries | leavy empty or `192.168.1.51/infiniflow` |

**Image Path Resolution:**

| Image Type | Source |
|------------|--------|
| **RAGFlow Application Images** | |
| ragflow | `private_registry/ragflow:<tag>` |
| deepdoc | `private_registry/deepdoc_cpu:<tag>` |
| **Third-Party Infrastructure Images** | |
| mysql | `public_registry/mysql:8.0` (or default docker.io) |
| redis | `public_registry/valkey:8` (or default docker.io) |
| tei | `public_registry/text-embeddings-inference:cpu-1.8` |
| rabbitmq | `public_registry/rabbitmq:4-management` (or default docker.io) |
| curl | `public_registry/curl:latest` (or default docker.io) |
| minio_mc | `public_registry/mc:latest` (or default docker.io) |

**Examples:**

For GKE with Google Container Registry:
```
private_registry = "gcr.io/ragflow-462809"
public_registry  = ""
```

For air-gapped environments (all images from private registry):
```
private_registry = "192.168.1.51/infiniflow-ai"
public_registry  = "192.168.1.51/infiniflow"
```

### Resource Sizing

The default resource sizing in `variables.tf` follows production requirements. You can customize resources in `terraform.tfvars` as needed. See [Resource Sizing](#resource-sizing) for available configuration options.

## Access RAGFlow

The gateway address will be shown at the end of `tofu apply` output. You can also retrieve it using either of the following methods:

**From Terraform output:**
```bash
tofu output gateway_address
```

**From ConfigMap:**
```bash
kubectl get configmap ragflow-gateway-address -n ragflow -o jsonpath='{.data.gateway_address}'
```

For cloud deployments with load balancers, the gateway may be accessible via an external IP or hostname.

## Testing

After deployment, you can run the HTTP API tests to verify RAGFlow is working correctly.

**1. Install test dependencies:**
```bash
uv sync --python 3.12 --only-group test --no-default-groups --frozen && uv pip install sdk/python --group test
```

**2. Activate virtual environment:**
```bash
source .venv/bin/activate
```

**3. Set required environment variables:**
```bash
export ZHIPU_API_KEY=your_api_key_here
export HOST_ADDRESS=http://<gateway-ip>
```

You can get the gateway IP from the ConfigMap:
```bash
export HOST_ADDRESS=http://$(kubectl get configmap ragflow-gateway-address -n ragflow -o jsonpath='{.data.gateway_address}')
```

**4. Run tests:**
```bash
# Run all P1 tests
pytest -s --tb=short --level=p1 test/testcases/test_http_api

# Run specific test file
pytest -s --tb=short test/testcases/test_http_api/test_chat_assistant_management/test_create_chat_assistant.py::TestChatAssistantCreate::test_name[payload0-0-]


# Run with verbose output
pytest -v test/testcases/test_http_api
```

**Test Levels:**
- `p1` - Critical path tests (recommended for basic validation)
- `p2` - Extended functionality tests
- `p3` - Edge case tests

## Cleanup

**Warning:** This will delete all Kubernetes resources including PVCs (MySQL, Elasticsearch, RabbitMQ data), leading to permanent data loss. Make sure to back up your data before proceeding.

Remove RAGFlow deployment:

```bash
tofu destroy -auto-approve
```

For GKE, remember to remove authorized networks if no longer needed:

```bash
gcloud container clusters update CLUSTER_NAME \
  --region REGION \
  --no-enable-master-authorized-networks
```

## Production Considerations

### Security
- **Enable TLS**: Set `enable_tls = true` for production
- **Network policies**: Consider adding network policies to restrict traffic
- **Workload Identity**: Use cloud provider's Workload Identity instead of access keys

### Database Credentials Management

RAGFlow uses a consistent approach for managing database credentials across all deployments:

**Principles:**
1. **No hardcoded credentials**: All passwords are randomly generated using Terraform's `random_password` resource
2. **Consistent usernames**: Usernames are defined as local variables (not variables) to ensure consistency across all components
3. **No credential variables**: Variables for usernames and passwords are intentionally NOT defined to prevent misconfiguration

**Implementation:**

| Component | Username | Password |
|-----------|----------|----------|
| MySQL | `ragflow` (local variable) | `random_password.mysql.result` |
| Redis | N/A (no auth user) | `random_password.redis.result` |
| RabbitMQ | `ragflow` (local variable) | `random_password.rabbitmq.result` |

**Local Variables (defined in `locals` block):**
```hcl
locals {
  # Database users (consistent across all components)
  mysql_user     = "ragflow"
  rabbitmq_user = "ragflow"
}
```

**Random Password Generation:**
```hcl
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
```

**Benefits:**
- Eliminates credential mismatch between components
- Passwords are automatically generated and stored in Kubernetes secrets
- No manual password configuration needed
- Consistent deployment experience across all environments

**Note:** For existing deployments that need to retrieve generated passwords, use:
```bash
# Get MySQL password
kubectl get secret mysql-password -n ragflow -o jsonpath={.data.password} | base64 -d

# Get Redis password
kubectl get secret ragflow-env -n ragflow -o jsonpath={.data.REDIS_PASSWORD} | base64 -d

# Get RabbitMQ password
kubectl get secret ragflow-env -n ragflow -o jsonpath={.data.RABBITMQ_DEFAULT_PASS} | base64 -d
```
### Resource Sizing


The following table shows the default resource values for each environment. Copy the appropriate environment file to `terraform.tfvars` before deployment:

| Resource | Variable | Production | Development (dev_gke / dev_smk) |
|----------|----------|------------|---------------------------------------|
| **MySQL** |
| Storage | mysql_k8s_storage | 200 Gi | 20 Gi |
| CPU Request | mysql_cpu_request | 4 | 4 |
| CPU Limit | mysql_cpu_limit | 8 | 8 |
| Memory Request | mysql_memory_request | 8Gi | 8Gi |
| Memory Limit | mysql_memory_limit | 16Gi | 16Gi |
| **Elasticsearch - Master Nodes** |
| Node Count | es_master_node_count | 3 | 1 |
| CPU Request | es_master_cpu_request | 2 | 2 |
| CPU Limit | es_master_cpu_limit | 4 | 4 |
| Memory Request | es_master_memory_request | 8Gi | 4Gi |
| Memory Limit | es_master_memory_limit | 8Gi | 4Gi |
| Heap Size | es_master_heap_size | 4g | 2g |
| **Elasticsearch - Data/Ingest Nodes** |
| Node Count | es_data_node_count | 4 | 1 |
| Storage per Node | es_data_storage | 500 Gi | 20 Gi |
| CPU Request | es_data_cpu_request | 4 | 4 |
| CPU Limit | es_data_cpu_limit | 8 | 8 |
| Memory Request | es_data_memory_request | 32Gi | 16Gi |
| Memory Limit | es_data_memory_limit | 32Gi | 16Gi |
| Heap Size | es_data_heap_size | 16g | 8g |
| **RabbitMQ** |
| Storage | rabbitmq_storage | 20 Gi | 20 Gi |
| CPU Request | rabbitmq_cpu_request | 1 | 1 |
| CPU Limit | rabbitmq_cpu_limit | 2 | 2 |
| Memory Request | rabbitmq_memory_request | 2Gi | 2Gi |
| Memory Limit | rabbitmq_memory_limit | 4Gi | 4Gi |
| **Redis** |
| CPU Request | redis_cpu_request | 2 | 2 |
| CPU Limit | redis_cpu_limit | 4 | 4 |
| Memory Request | redis_memory_request | 4Gi | 4Gi |
| Memory Limit | redis_memory_limit | 8Gi | 8Gi |
| **TEI** |
| Replicas | tei_replicas | 0 | 1 |
| CPU Request | tei_cpu_request | 4 | 4 |
| CPU Limit | tei_cpu_limit | 8 | 8 |
| Memory Request | tei_memory_request | 8Gi | 8Gi |
| Memory Limit | tei_memory_limit | 16Gi | 16Gi |
| **RAGFlow** |
| Replicas | ragflow_replicas | 3 | 1 |
| CPU Request | ragflow_cpu_request | 2 | 2 |
| CPU Limit | ragflow_cpu_limit | 4 | 4 |
| Memory Request | ragflow_memory_request | 8Gi | 8Gi |
| Memory Limit | ragflow_memory_limit | 16Gi | 16Gi |
| **Parser** |
| Replicas | parser_replicas | 3 | 1 |
| CPU Request | parser_cpu_request | 2 | 2 |
| CPU Limit | parser_cpu_limit | 4 | 4 |
| Memory Request | parser_memory_request | 8Gi | 8Gi |
| Memory Limit | parser_memory_limit | 16Gi | 16Gi |
| **DeepDoc** |
| Replicas | deepdoc_replicas | 3 | 1 |
| CPU Request | deepdoc_cpu_request | 8 | 8 |
| CPU Limit | deepdoc_cpu_limit | 16 | 16 |
| Memory Request | deepdoc_memory_request | 32Gi | 32Gi |
| Memory Limit | deepdoc_memory_limit | 64Gi | 64Gi |

### Scaling and Capacity Management

RAGFlow deployment supports safe, zero-downtime scaling operations through OpenTofu/Terraform:

#### Storage Expansion (PVC Scaling)
You can safely increase the storage capacity of stateful components (MySQL, Elasticsearch, RabbitMQ) without data loss:
1. Update the corresponding storage variable in your `terraform.tfvars` (e.g., increase `mysql_k8s_storage` from `20` to `50`).
2. Run `tofu apply -auto-approve`.

**How it ensures no data loss:**
- OpenTofu triggers an **in-place update** of the Kubernetes PersistentVolumeClaim (PVC) resources.
- Expected default StorageClasses (like GKE's `premium-rwo`, `standard-rwo`, or SMK's `rook-ceph-block`) already have `allowVolumeExpansion: true` configured by default. (See [GKE Volume Expansion Documentation](https://cloud.google.com/kubernetes-engine/docs/how-to/persistent-volumes/volume-expansion) and [Rook/Ceph Volume Expansion Documentation](https://rook.io/docs/rook/latest/Storage-Configuration/Block-Storage-RBD/block-storage/#volume-expansion)). You can verify this in your cluster by running `kubectl get storageclass <your-storage-class> -o yaml | grep allowVolumeExpansion`.
- The underlying cloud provider or CSI driver (e.g., Rook/Ceph CSI) dynamically resizes the disk and expands the file system seamlessly without recreating the PVC.
- **⚠️ IMPORTANT:** Kubernetes *only* supports increasing volume size. Attempting to decrease storage size may force Terraform to drop and recreate the PVC, which **will result in permanent data loss**.

#### Stateless Replica Scaling
Stateless components (`ragflow`, `parser`, `deepdoc`, `tei`) can be scaled out natively:
1. Increase the replica count variables in `terraform.tfvars` (e.g., change `ragflow_replicas` from `1` to `3`).
2. Run `tofu apply -auto-approve`.

**How it ensures zero downtime:**
- Adding replicas simply updates the `replicas` parameter in the Kubernetes Deployment spec.
- The Kubernetes controller spins up new pods without terminating the existing running pods.

#### Elasticsearch Node Scaling (via ECK Operator)
You can safely scale out the Elasticsearch cluster by adding more master or data/ingest nodes:
1. Increase the node count variable in `terraform.tfvars`:
   - For master nodes: change `es_master_node_count` from `1` to `3`
   - For data/ingest nodes: change `es_data_node_count` from `1` to `4`
2. Run `tofu apply -auto-approve`.

**How it ensures no data loss:**
- OpenTofu updates the `Elasticsearch` Custom Resource (CR) manifest and applies it via a Kubernetes Job.
- The **Elastic Cloud on Kubernetes (ECK) Operator** detects the change and orchestrates the scaling process.
- ECK safely provisions new Pods and their associated PersistentVolumeClaims (PVCs).
- Once the new nodes join the cluster, Elasticsearch automatically migrates and rebalances data shards across the expanded nodes in the background, ensuring high availability and zero data loss.

#### Compute Resource Resizing (CPU and Memory)
You can adjust the compute resources (CPU request/limit and Memory request/limit) for any component:
1. Update the corresponding compute variables in `terraform.tfvars` (e.g., change `ragflow_cpu_limit` or `es_data_memory_request`).
2. Run `tofu apply -auto-approve`.

**How it ensures stability and data integrity:**
- For stateless components, the Kubernetes Deployment Controller performs a **rolling update**—starting new Pods with the updated specs before gracefully terminating the old ones, meaning zero downtime.
- For StatefulSets (like MySQL), the controller rolls out the Pods sequentially (one by one) to ensure data stability and cluster health.
- For Elasticsearch, the ECK Operator manages the rollout smoothly, ensuring the cluster remains green and data is not lost while it restarts pods with the new resource specifications.

### Backups and Data Migration

#### Backups
- **MySQL**: Regular backups of MySQL data using `mysqldump` or Percona XtraBackup.
- **Elasticsearch**: Snapshot and restore policies using Elasticsearch Snapshot Lifecycle Management (SLM).
- **GCS/S3 Bucket**: Enable versioning on your object storage bucket.

#### Data Migration (Cross-Cluster / External to K8s)

**Migrating MySQL Data:**
The standard and most reliable way to migrate MySQL data between different Kubernetes clusters (or from external to K8s) is via `mysqldump`:
1. **Export** data from the source database:
   ```bash
   mysqldump -h <source-host> -u root -p<source-password> ragflow > ragflow_backup.sql
   ```
2. **Import** data into the destination Kubernetes deployment:
   ```bash
   # Connect to the target MySQL pod
   kubectl port-forward svc/mysql 3306:3306 -n ragflow
   # Import the data
   mysql -h 127.0.0.1 -u root -p<target-password> ragflow < ragflow_backup.sql
   ```

**Migrating Elasticsearch Data:**
Do not copy PVC underlying files directly. The robust method for Elasticsearch data migration is using **Snapshot and Restore** with a shared S3/GCS repository:
1. **Register a Snapshot Repository** on the source cluster pointing to an S3/GCS bucket:
   ```json
   PUT /_snapshot/migration_repo
   {
     "type": "s3",
     "settings": { "bucket": "my-migration-bucket" }
   }
   ```
2. **Take a Snapshot** of your data on the source cluster:
   ```json
   PUT /_snapshot/migration_repo/snapshot_1?wait_for_completion=true
   ```
3. **Register the same Repository** in the destination Elasticsearch cluster.
4. **Restore the Snapshot** on the destination cluster:
   ```json
   POST /_snapshot/migration_repo/snapshot_1/_restore
   ```
*For cloud operator deployments like ECK, refer to the [Elasticsearch Snapshot Documentation](https://www.elastic.co/guide/en/elasticsearch/reference/current/snapshot-restore.html) for detailed configurations on securing repository access.*

### Monitoring and Alerting

- **Prometheus/ELK**: In general environments, it is recommended to install a metrics server (Prometheus) and a logging stack (ELK/Loki) to oversee cluster health.
- **GKE Pod Status Alerting**: For GKE deployments, you can automatically configure an alert policy that sends email notifications whenever a Pod's status changes. To do this, simply set the `GCP_ALERT_EMAIL` environment variable with a comma-separated list of email addresses before running the GCP setup script:
  ```bash
  export GCP_ALERT_EMAIL="admin1@example.com,admin2@example.com"
  python3 gke_setup.py
  ```
  *This leverages Google Cloud Monitoring to create a policy tracking the `logging.googleapis.com/user/pod_status_change_events` metric.*

## Additional Resources

- [GKE Documentation](https://cloud.google.com/kubernetes-engine/docs)
- [EKS Documentation](https://docs.aws.amazon.com/eks/)
- [AKS Documentation](https://docs.microsoft.com/en-us/azure/aks/)
- [Terraform Kubernetes Provider](https://registry.terraform.io/providers/hashicorp/kubernetes/latest/docs)
- [RAGFlow Documentation](https://github.com/infiniflow/ragflow)
