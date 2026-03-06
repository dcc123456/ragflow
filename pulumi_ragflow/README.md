
# Pulumi RAGFlow Deployment

This guide provides step-by-step instructions for deploying RAGFlow using Pulumi with Go, equivalent to the Helm chart deployment.

## Overview

This Pulumi project deploys the complete RAGFlow stack on Kubernetes, including:

- **RAGFlow Application**: Web interface and API
- **Document Engine**: Choose from Elasticsearch, OpenSearch, or Infinity
- **Database**: MySQL for metadata
- **Cache**: Redis for session storage
- **Object Storage**: Object storage is provided by Kubernetes cluster object storage (such as Rook Ceph), no need to deploy MinIO separately.
- **Networking**: Gateway for external access

## Prerequisites

### Software Requirements

1. **Pulumi CLI**: Install from [https://www.pulumi.com/docs/install/](https://www.pulumi.com/docs/install/)
2. **Go 1.24+**: Install from [https://golang.org/dl/](https://golang.org/dl/)
3. **Kubernetes CLI**: `kubectl` configured to access your cluster
4. **helm**: Install from [https://github.com/helm/helm](hhttps://github.com/helm/helm)

### Kubernetes Cluster

- A running Kubernetes cluster (v1.24+ recommended)
- `kubectl` configured to access the cluster
- Sufficient resources for the deployment (see resource requirements below)

### Resource Requirements

| Component | CPU | Memory | Storage |
|-----------|-----|--------|---------|
| RAGFlow | 1-2 cores | 2-4GB | - |
| MySQL | 1 core | 1-2GB | 5GB |
| Redis | 1 core | 512MB | 5GB |
| MinIO | 1 core | 1GB | 5GB |
| Elasticsearch | 4 cores | 16GB | 20GB |
| OpenSearch | 4 cores | 16GB | 20GB |
| Infinity | 2 cores | 4GB | 5GB |

## Installation

### 1. Clone the RAGFlow Repository

```bash
git clone https://github.com/infiniflow/ragflow.git
cd ragflow/pulumi_ragflow
```

### 2. Install Go Dependencies

```bash
go mod download
```

This will download all required Go modules:
- `github.com/pulumi/pulumi/sdk/v3`
- `github.com/pulumi/pulumi-kubernetes/sdk/v4`

### 3. Install Pulumi Kubernetes Provider

```bash
pulumi plugin install resource kubernetes v4.24.1
```

## Configuration

### Environment Variables

The deployment is configured via environment variables. Key configuration options:

- **PULUMI_NAME**: Name of the Pulumi stack (default: "ragflow")
- **PULUMI_NAMESPACE**: Kubernetes namespace for deployment (default: "ragflow")
- **RAGFLOW_GATEWAY**: The gateway hostname for external access (default: "ragflow.ai"). If this hostname is unmanaged by a DNS, then add it to `/etc/hosts` to the client(browser, API client, pytest etc.).
- **RAGFLOW_SECRET_KEY**: Secret key for session signing (default: "DOnghtfiCeriTENdywhERlEtivOLicuL"). **Important**: All replicas must use the same key. Use `pulumi config set --secret ragflow_secret_key <your-key>` for production.

### Configuration Examples

#### Using Elasticsearch (Default)

All configurations are defined in `main.go`. The deployment uses Elasticsearch by default.

#### Using Custom Namespace

```bash
export PULUMI_NAMESPACE="ragflow-prod"
```

#### Gateway API Setup

**Important**: Gateway API is always enabled. You must set up either Cilium or NGINX Gateway before deployment:

**For Cilium Gateway:**
```bash
./setup-cilium-gateway.sh
```

**For NGINX Gateway:**
```bash
./setup-nginx-gateway.sh
```

## Gateway API

**Important**: Gateway API is always enabled and required for RAGFlow deployment. You must install either Cilium or NGINX Gateway before deploying.

Gateway API is Kubernetes' next-generation networking API that replaces traditional Ingress resources. It provides advanced routing, load balancing, and traffic management capabilities.

### Gateway API Overview

Gateway API enables **external cluster access** to internal services with:

- **Advanced Routing**: Hostname, path, and header-based traffic routing
- **TLS Termination**: HTTPS certificate handling and encryption
- **Load Balancing**: Traffic distribution across multiple backend instances
- **Traffic Control**: Rate limiting, circuit breaking, and retry mechanisms

### Supported Gateway Implementations

#### Cilium Gateway (Recommended)

**Use Case**: Clusters with Cilium CNI installed

**Advantages**:
- Deep integration with Cilium network policies
- Enhanced performance and security
- eBPF-accelerated network processing

**Setup**:
```bash
./setup-cilium-gateway.sh
```

#### NGINX Gateway Fabric

**Use Case**: Need for full-featured Gateway implementation

**Advantages**:
- Mature and stable implementation
- Rich feature set
- Broad community support

**Setup**:
```bash
./setup-nginx-gateway.sh
```

### Gateway HTTPS/TLS Configuration

The Gateway supports HTTPS with TLS termination. By default, only HTTP is enabled.

#### Default Configuration (HTTP Only)

By default, the Gateway only listens on port 80 (HTTP). This is suitable for internal deployments or when TLS is handled by an external load balancer.

```bash
# Default: HTTPS disabled
pulumi config set gateway_enable_https false
```

#### Enable HTTPS with User-Provided Certificate

To enable HTTPS, you must provide your own TLS certificate signed by a trusted Certificate Authority (CA). **Self-signed certificates are not supported**.

**Configuration**:
```bash
# Enable HTTPS
pulumi config set gateway_enable_https true

# Set your TLS certificate (PEM format)
pulumi config set gateway_tls_cert "$(cat /path/to/tls.crt)"

# Set your TLS private key (PEM format)
pulumi config set --secret gateway_tls_key "$(cat /path/to/tls.key)"
```

**Example with Let's Encrypt Certificate**:
```bash
# Enable HTTPS
pulumi config set gateway_enable_https true

# Assuming you have certbot certificates
pulumi config set gateway_tls_cert "$(cat /etc/letsencrypt/live/ragflow.example.com/fullchain.pem)"
pulumi config set --secret gateway_tls_key "$(cat /etc/letsencrypt/live/ragflow.example.com/privkey.pem)"
```

**Important**: When `gateway_enable_https` is `true`, both `gateway_tls_cert` and `gateway_tls_key` **must** be provided. The deployment will fail if either is missing.

#### Certificate Format

Both the certificate and private key must be in PEM format:

**Certificate (tls.crt)**:
```
-----BEGIN CERTIFICATE-----
MIIC9jCCAd4CCQD2rKXxBHxTzDANBgkqhkiG9w0BAQsFADA9MQswCQYDVQQGEwJV
...
-----END CERTIFICATE-----
```

**Private Key (tls.key)**:
```
-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA2Z3q3X2X8X9X0X0X0X0X0X0X0X0X0X0X0X0X0X0X0X0X0X0X
...
-----END RSA PRIVATE KEY-----
```

#### Configuration Reference

| Configuration | Type | Default | Description |
|---------------|------|---------|-------------|
| `gateway_enable_https` | boolean | `false` | Enable HTTPS listener on port 443 |
| `gateway_tls_cert` | string | `""` | TLS certificate in PEM format (required when HTTPS enabled) |
| `gateway_tls_key` | string | `""` | TLS private key in PEM format (required when HTTPS enabled, secret) |

**Behavior**:
- If `gateway_enable_https` is `false` (default): Only HTTP listener (port 80) is created
- If `gateway_enable_https` is `true`: Both HTTP (port 80) and HTTPS (port 443) listeners are created, **requires** `gateway_tls_cert` and `gateway_tls_key`

#### Security Considerations

1. **Certificate Source**: Always use certificates signed by a trusted CA (e.g., Let's Encrypt, DigiCert, etc.)
2. **Secret Management**: Always use `pulumi config set --secret` for the private key to keep it encrypted
3. **Certificate Rotation**: Periodically rotate your TLS certificates before they expire
4. **Certificate Expiry**: Monitor certificate expiration and renew before expiry
5. **No Self-Signed Certs**: Self-signed certificates are not supported; use proper CA-signed certificates for production

### Responsibility Separation

| Layer | Provider Responsibility (Cilium/Nginx) | RAGFlow Responsibility |
|-------|----------------------------------------|----------------------|
| **Network** | Packet routing, load balancing algorithms | Route rule definitions |
| **Security** | TLS certificates, encryption/decryption | Access policy configuration |
| **Performance** | Connection pooling, caching | Health check strategies |
| **Observability** | Traffic metrics, logging | Application-specific routing |

### Automatic Detection

RAGFlow automatically detects available Gateway classes with Cilium having priority:

1. **Priority Order**: Cilium → NGINX
2. **Required**: Deployment will fail if no supported Gateway class is found

### Troubleshooting

**Check Gateway Status**:
```bash
kubectl get gateway -A
kubectl get httproute -A
kubectl get gatewayclass
```

**Common Issues**:
- **Missing CRDs**: Run setup script to install Gateway API CRDs
- **No Gateway Class**: Ensure Cilium or NGINX Gateway is properly installed
- **Permission Issues**: Verify ServiceAccount has Gateway API resource access

## Deployment

### 1. Initialize Pulumi Stack

```bash
pulumi stack init dev
```

### 2. Configure Environment (Optional)

Set optional environment variables:

```bash
export PULUMI_NAME="ragflow"  # If you prefer NGINX over Cilium
export PULUMI_NAMESPACE="ragflow-prod"
```

### 3. Preview Deployment

```bash
pulumi preview
```

This will show you all the resources that will be created without actually deploying them.

### 4. Deploy

```bash
pulumi up
```

Confirm the deployment when prompted. This will create:
- Namespace: `ragflow`
- Deployments for all components
- Services for internal communication
- PersistentVolumeClaims for storage
- Ingress (if enabled)

### 5. Verify Deployment

Check that all pods are running:

```bash
kubectl get pods -n ragflow
```

Check services:

```bash
kubectl get services -n ragflow
```

### 6. Access RAGFlow

**NOTE**
- Replace `ragflow.local` with the correct `RAGFLOW_GATEWAY` value.
- Ensure `Access to RAGFlow API service` works before run pytest.

Access to RAGFlow API service(nginx port 80 -> ragflow port 9380):
```
curl -X POST -H "Content-Type: application/json" -d '{"email": "qa@infiniflow.org"}' http://ragflow.local/v1/user/register
{"code":101,"data":null,"message":"required argument are missing: nickname,password; "}

curl http://ragflow.local/api/v1/file/list
{"code":0,"data":false,"message":"`Authorization` can't be empty"}
```

Access to RAGFlow admin service(nginx port 80 -> ragflow port 9381):
```
curl http://ragflow.local/api/v1/admin/users
<!doctype html>
<html lang=en>
<title>401 Unauthorized</title>
<h1>Unauthorized</h1>
<p>The server could not verify that you are authorized to access the URL requested. You either supplied the wrong credentials (e.g. a bad password), or your browser doesn&#39;t understand how to supply the credentials required.</p>
```

#### Check Gateway Status

```bash
# Check Gateway resources
kubectl get gateway -A
kubectl get httproute -A

# Get Gateway external IP/hostname
kubectl get gateway ragflow-gateway -o yaml
```

#### Access RAGFlow

```bash
curl http://ragflow.local:9380/v1/
```

If using a custom hostname, ensure your DNS is configured or add it to `/etc/hosts`.

## Troubleshooting

### Common Issues

#### PVC Pending
If PersistentVolumeClaims remain in "Pending" status:

```bash
kubectl get pvc -n ragflow
kubectl describe pvc <pvc-name> -n ragflow
```

This usually indicates insufficient storage capacity or missing StorageClass.

#### Pod Failures
Check pod status and logs:

```bash
kubectl get pods -n ragflow
kubectl logs -n ragflow <pod-name>
```

#### Network Issues
Verify service endpoints:

```bash
kubectl get endpoints -n ragflow
```

### Resource Cleanup

To destroy all deployed resources:

```bash
pulumi destroy
```

To remove the stack completely:

```bash
pulumi stack rm dev
```

## Advanced Configuration

### Custom Resource Limits

Edit the resource specifications in `main.go` for each deployment.

### Custom Storage Classes

Modify the `storageClassName` in PVC specifications if you have custom StorageClasses.

### TLS/SSL for Ingress

For production deployments, configure TLS termination at the Ingress level by adding TLS secrets and annotations.

## Migration from Python Version

If you were using the previous Python implementation:

1. Export your current stack outputs: `pulumi stack output > outputs.json`
2. Destroy the Python stack: `pulumi destroy`
3. Remove the Python stack: `pulumi stack rm <stack-name>`
4. Follow the Go deployment steps above
5. Restore any custom configurations using environment variables

## Upgrading

### Updating RAGFlow Version

Edit the `tag` in your configuration:

```python
"ragflow": {
    "image": {
        "repository": "infiniflow/ragflow",
        "tag": "v0.23.0",  # Update to new version
        "pullPolicy": "IfNotPresent",
    }
}
```

Then deploy the changes:

```bash
pulumi up
```

### Updating Other Components

Similarly, update the `tag` for any component:

```python
"elasticsearch": {
    "image": {
        "repository": "elasticsearch",
        "tag": "8.12.0",  # Updated version
    }
}
```

## Monitoring and Maintenance

### Viewing Logs

```bash
# View RAGFlow logs
pulumi logs --resource-name ragflow

# View specific component logs
kubectl logs -l app.kubernetes.io/component=ragflow
```

### Scaling

To scale the RAGFlow deployment:

```python
# In your configuration, modify the deployment spec
"ragflow": {
    "deployment": {
        "replicas": 3  # Default is 1
    }
}
```

### Backups

For database backups:

```bash
# Create MySQL backup
kubectl exec -it <mysql-pod> -- mysqldump -u root -p rag_flow > backup.sql

# Create MinIO backup
# Use MinIO client or kubectl cp to copy data
```

## Migration from Helm

If you're migrating from the Helm chart deployment:

### Key Differences

1. **Configuration**: Golang type-safe structs vs Helm values.yaml
2. **Deployment**: `pulumi up` vs `helm install`
3. **Updates**: `pulumi up` vs `helm upgrade`
4. **Rollback**: `pulumi cancel` vs `helm rollback`

### Configuration Mapping

| Helm values.yaml | Pulumi config.py |
|------------------|------------------|
| `env.DOC_ENGINE` | `CONFIG["env"]["DOC_ENGINE"]` |
| `ragflow.image.tag` | `CONFIG["ragflow"]["image"]["tag"]` |
| `mysql.disk_size.capacity` | `CONFIG["mysql"]["storage"]["capacity"]` |

### Migration Steps

1. Extract your current Helm values:
   ```bash
   helm get values <release-name> > helm-values.yaml
   ```

2. Convert to Pulumi configuration format

3. Create `config.py` with the converted values

4. Deploy with Pulumi:
   ```bash
   pulumi stack init <stack-name>
   pulumi up
   ```

## Advanced Topics

### Multiple Environments

Use different Pulumi stacks for different environments:

```bash
# Development
pulumi stack init dev
pulumi config set --stack dev env:DOC_ENGINE infinity

# Production
pulumi stack init prod
pulumi config set --stack prod env:DOC_ENGINE elasticsearch
pulumi config set --stack prod elasticsearch:storage:capacity 50Gi
```

### Secret Management

For production deployments, use Pulumi's secret management:

```bash
# MySQL password
pulumi config set --secret mysql:env:MYSQL_PASSWORD super-secret-password

# RAGFlow secret key for session signing
# IMPORTANT: All RAGFlow replicas must use the same secret key
# Generate a secure random key for production:
pulumi config set --secret ragflow_secret_key $(openssl rand -base64 32)

# Or set a specific key (useful for migration from existing deployments):
pulumi config set --secret ragflow_secret_key your-secret-key-here
```

**Important Notes**:
- The `ragflow_secret_key` is used for session signing and must be consistent across all RAGFlow replicas
- If not specified, a default key is used (not recommended for production)
- Changing the secret key will invalidate all existing user sessions
- For production, always use `--secret` flag to encrypt the value in Pulumi state

## Support

### Getting Help

- **Pulumi Documentation**: [https://www.pulumi.com/docs/](https://www.pulumi.com/docs/)
- **RAGFlow Documentation**: [https://ragflow.io/docs/](https://ragflow.io/docs/)
- **Kubernetes Documentation**: [https://kubernetes.io/docs/home/](https://kubernetes.io/docs/home/)

### Debugging Tips

```bash
# Enable verbose logging
pulumi up --debug --logtostderr -v=9

# View stack history
pulumi stack history

# Export stack for backup
pulumi stack export > backup.json
```

## Alibaba Cloud (Aliyun) Deployment

This Pulumi project supports deployment to Alibaba Cloud (Aliyun) for infrastructure components (VPC, RDS MySQL, Elasticsearch) while Kubernetes resources are deployed separately.

### Prerequisites for Aliyun Deployment

1. **Pulumi CLI**: v3.0 or later
2. **Go 1.24+**: For the Pulumi Go program
3. **Aliyun Account**: With active subscription
4. **Access Keys**: Configure in `Pulumi.ali.yaml` or environment variables:
   - `ALIBABA_CLOUD_ACCESS_KEY_ID`
   - `ALIBABA_CLOUD_ACCESS_KEY_SECRET`

### Creating Kubernetes Clusters (ACK/ASK)

This Pulumi project supports creating Alibaba Cloud Kubernetes clusters:
- **ACK (Alibaba Cloud Container Service for Kubernetes)**: Managed Kubernetes cluster with worker nodes
- **ASK (ACK Serverless)**: Serverless Kubernetes without worker nodes

#### Quick Start: Create a Kubernetes Cluster

```bash
# Enable cluster creation
pulumi config set pulumi_ragflow:create_cluster true

# Set cluster type (optional: "AckPro" for ACK or "Ask" for ASK)
pulumi config set pulumi_ragflow:cluster.type Managed

# Set Kubernetes version (optional)
pulumi config set pulumi_ragflow:kubernetes.version 1.30.15-aliyun.1

# Deploy infrastructure including the cluster
pulumi up

# After cluster creation, get kubeconfig
alicloud cs get-kubeconfig --region cn-shanghai --cluster-id <cluster-id> > ~/.kube/config
```

#### Kubernetes Configuration Options

| Configuration | Default | Description |
|---------------|---------|-------------|
| `pulumi_ragflow:create_cluster` | `false` | Enable/disable cluster creation |
| `pulumi_ragflow:cluster.type` | `AckPro` | Cluster type: "AckPro" (ACK) or "Ask" (ASK) |
| `pulumi_ragflow:kubernetes.version` | `1.30.15-aliyun.1` | Kubernetes version |
| `pulumi_ragflow:kubernetes.service_cidr` | `172.21.0.0/20` | Service network CIDR |
| `pulumi_ragflow:kubernetes.pod_cidr` | `10.1.0.0/16` | Pod network CIDR (ACK only) |
| `pulumi_ragflow:kubernetes.master_instance_types` | `ecs.c6.xlarge` | Master node instance type (ACK only) |
| `pulumi_ragflow:kubernetes.master_disk_category` | `cloud_essd` | Master disk type |
| `pulumi_ragflow:kubernetes.master_disk_size` | `120` | Master disk size (GB) |

#### Cluster Type Comparison

| Feature | ACK (Managed) | ASK (Serverless) |
|---------|---------------|------------------|
| Worker Nodes | Required | Not required |
| Pod CIDR | Required | Not required |
| Control Plane | Managed by Aliyun | Managed by Aliyun |
| Use Case | Production workloads | Development/testing, variable workloads |
| Cost | Higher (pay for nodes) | Lower (pay per pod execution time) |

#### Cluster Output

After successful cluster creation, Pulumi will output:
- **Cluster Name**: The name of the created cluster
- **Cluster ID**: Aliyun cluster identifier

> **Note**: The cluster endpoint and kubeconfig are **NOT** included in Pulumi outputs to avoid state sync issues. Use the `GetClusterEndpoint()` and `GetClusterKubeconfig()` helper functions to query them dynamically from Aliyun API when needed (see below).

##### Retrieving Cluster Endpoint and Kubeconfig Dynamically

The cluster endpoint and kubeconfig are **not stored in Pulumi state** to avoid sync issues when the cluster is recreated. Instead, query them dynamically using helper functions:

**Method 1: Using Go Helper Functions**

```go
package main

import (
    "fmt"
    "os"
)

func GetClusterInfoFromAPI() {
    clusterID := "cf4f8dc14070441eeba597fac25f9fcdd" // From Pulumi output
    region := "cn-shanghai"                             // From Pulumi config
    accessKey := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
    secretKey := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

    // Get public endpoint (usePrivateIP=false)
    endpoint, err := GetClusterEndpoint(clusterID, region, accessKey, secretKey, false)
    if err != nil {
        fmt.Printf("Failed to get cluster endpoint: %v\n", err)
        return
    }

    fmt.Printf("Cluster endpoint: %s\n", endpoint)

    // Set HOST_ADDRESS for API testing
    os.Setenv("HOST_ADDRESS", endpoint)

    // Get kubeconfig with public endpoint
    kubeconfig, err := GetClusterKubeconfig(clusterID, region, accessKey, secretKey, false)
    if err != nil {
        fmt.Printf("Failed to get cluster kubeconfig: %v\n", err)
        return
    }

    // Write kubeconfig to file for kubectl use
    err = os.WriteFile("/tmp/ack-kubeconfig", []byte(kubeconfig), 0600)
    if err != nil {
        fmt.Printf("Failed to write kubeconfig: %v\n", err)
        return
    }

    fmt.Printf("Kubeconfig written to /tmp/ack-kubeconfig\n")
}
```

**Method 2: Using Aliyun CLI**

```bash
# List clusters to find your cluster ID
alicloud cs GET /clusters --region cn-shanghai

# Get kubeconfig for your cluster
alicloud cs get-kubeconfig \
  --region cn-shanghai \
  --cluster-id <cluster-id> \
  > ~/.kube/config

# Verify cluster access
kubectl cluster-info
kubectl get nodes
```
```

**Why this pattern?**
- **Avoids state sync issues**: When cluster is recreated, endpoint and kubeconfig change but Pulumi state becomes stale
- **Always returns current value**: Queries Aliyun API for actual endpoint and kubeconfig
- **Follows ES secret pattern**: Similar to how Elasticsearch credentials are handled
- **Resilient to external changes**: Works even if cluster is modified outside Pulumi

**Helper Functions:**
- `GetClusterEndpoint(clusterID, region, accessKey, secretKey, usePrivateIP)`: Returns cluster API server endpoint
- `GetClusterKubeconfig(clusterID, region, accessKey, secretKey, usePrivateIP)`: Returns full kubeconfig YAML

**Parameters:**
- `clusterID`: Aliyun ACK cluster ID (from Pulumi output `clusterId`)
- `region`: Aliyun region (from Pulumi config `region`)
- `accessKey`, `secretKey`: Aliyun credentials
- `usePrivateIP`: `false` for public endpoint, `true` for private endpoint

#### Important Notes

1. **Private Cluster**: Clusters are created with private API endpoints by default (`SlbInternetEnabled: false`)
2. **VPC Integration**: Clusters are created in the same VPC as other infrastructure resources
3. **Terway CNI**: Uses Terway-eniip for pod networking with VPC integration
4. **Addons**: Automatically installs essential addons:
   - `terway-eniip`: CNI plugin for pod networking
   - `csi-plugin`: Container Storage Interface plugin
   - `csi-provisioner`: Dynamic volume provisioning
   - `logtail-ds`: Log collection

### Quick Start

```bash
# Initialize Aliyun stack
pulumi stack init ali

# Configure region (required)
pulumi config set pulumi_ragflow:region cn-shanghai

# Configure Aliyun credentials
pulumi config set alicloud.accessKey YOUR_ACCESS_KEY
pulumi config set --secret alicloud.secretKey YOUR_SECRET_KEY

# Set availability zone (required)
pulumi config set pulumi_ragflow:vswitch.zone cn-shanghai-b

# Enable Elasticsearch creation
pulumi config set pulumi_ragflow:external_es true

# Preview and deploy
pulumi up
```

### Configuration Options

#### Required Configuration

| Configuration | Example | Description |
|---------------|---------|-------------|
| `pulumi_ragflow:region` | `cn-shanghai` | Aliyun region |
| `pulumi_ragflow:vswitch.zone` | `cn-shanghai-b` | Availability zone for VSwitch |
| `alicloud.accessKey` | `LTAI5t***` | Aliyun access key ID |
| `alicloud.secretKey` | `***` | Aliyun secret access key |

#### Elasticsearch Configuration

| Configuration | Default | Description |
|---------------|---------|-------------|
| `pulumi_ragflow:elasticsearch.version` | `8.13_with_X-Pack` | ES version |
| `pulumi_ragflow:elasticsearch.node_spec` | `elasticsearch.sn1ne.large.new` | Data node instance type |
| `pulumi_ragflow:elasticsearch.node_amount` | `3` | Number of data nodes |
| `pulumi_ragflow:elasticsearch.disk_size` | `20` | Disk size per node (GB) |
| `pulumi_ragflow:elasticsearch.disk_type` | `cloud_essd` | Disk type |
| `pulumi_ragflow:elasticsearch.disk_size_performance_level` | `PL1` | ESSD performance level |
| `pulumi_ragflow:elasticsearch.kibana_spec` | `elasticsearch.sn1ne.large` | Kibana node instance type |
| `pulumi_ragflow:payment_type` | `PostPaid` | Payment type (PostPaid/PrePaid) |

#### Payment Configuration

```yaml
# Pay-as-you-go (PostPaid) - Recommended for dev/test
pulumi_ragflow:payment_type: PostPaid

# Subscription (PrePaid) - Cost-effective for production
pulumi_ragflow:payment_type: PrePaid
pulumi_ragflow:payment_period: "1"  # Duration in months (1-9, 12, 24, 36)
```

### Important: Elasticsearch Instance Type Selection

**⚠️ Critical**: Always use instance types with the `.new` suffix for better availability and PostPaid support.

#### The `.new` Suffix Matters

Aliyun Elasticsearch has two generations of instance types:

| Instance Type | Generation | Availability | PostPaid Support |
|---------------|------------|--------------|------------------|
| `elasticsearch.sn1ne.large` | Legacy | Limited | Zone-restricted |
| `elasticsearch.sn1ne.large.new` | **Current** | **Wide** | **Full support** ✅ |

**Why `.new` suffix is critical**:

1. **Better Availability**: Available in more availability zones
2. **PostPaid Support**: Consistently supports pay-as-you-go billing
3. **Newer Hardware**: Uses latest generation infrastructure
4. **Pricing**: Often more cost-effective than legacy types

#### Common Errors and Solutions

**Error**: `PRICE.PRICING_PLAN_RESULT_NOT_FOUND`

This error indicates that the selected instance type/payment combination is not available in the chosen zone.

**Solution 1: Use `.new` suffix instance types**
```yaml
# ❌ Wrong - Legacy type without .new suffix
pulumi_ragflow:elasticsearch.node_spec: elasticsearch.sn2ne.large

# ✅ Correct - Current generation with .new suffix
pulumi_ragflow:elasticsearch.node_spec: elasticsearch.sn1ne.large.new
```

**Solution 2: Match version and instance type**
```yaml
# Version 8.x works best with .new specs
pulumi_ragflow:elasticsearch.version: 8.13_with_X-Pack
pulumi_ragflow:elasticsearch.node_spec: elasticsearch.sn1ne.large.new
```

**Solution 3: Try different zone**
```yaml
# If cn-shanghai-b has issues, try another zone
pulumi_ragflow:vswitch.zone: cn-shanghai-e
```

#### Instance Type Reference

**Recommended configurations by workload**:

| Workload | Version | Data Node Spec | Nodes | Disk |
|----------|---------|----------------|-------|------|
| Development/Testing | `8.13_with_X-Pack` | `elasticsearch.sn1ne.large.new` | 3 | 20GB |
| Small Production | `8.13_with_X-Pack` | `elasticsearch.sn2ne.large.new` | 3-6 | 50GB |
| Medium Production | `8.13_with_X-Pack` | `elasticsearch.sn2ne.2xlarge.new` | 6-12 | 100GB |
| Large Production | `8.13_with_X-Pack` | `elasticsearch.sn2ne.4xlarge.new` | 12+ | 200GB+ |

**Available `.new` instance types** (ordered by size):
- `elasticsearch.sn1ne.large.new` (1 core, 4GB RAM)
- `elasticsearch.sn2ne.large.new` (2 cores, 8GB RAM)
- `elasticsearch.sn2ne.xlarge.new` (2 cores, 8GB RAM)
- `elasticsearch.sn2ne.2xlarge.new` (2 cores, 16GB RAM)
- `elasticsearch.sn2ne.4xlarge.new` (4 cores, 16GB RAM)

### Troubleshooting Aliyun Deployment

#### Query Available Instance Types and Versions

Before deployment, it's **strongly recommended** to check what instance types and versions are available in your region and zone.

**Method 1: Check existing instances in your zone** (Recommended)
```bash
# List all instances in your region
aliyun elasticsearch GET /openapi/instances --region cn-shanghai

# Check what versions and specs are actually running in your target zone
aliyun elasticsearch GET /openapi/instances --region cn-shanghai | \
  jq '.Result[] | select(.networkConfig.vsArea == "cn-shanghai-b") | \
  {instanceId, esVersion, nodeSpec: .nodeSpec.spec, paymentType, status}'

# Find all .new suffix instances (these are the current generation)
aliyun elasticsearch GET /openapi/instances --region cn-shanghai | \
  jq '.Result[] | select(.nodeSpec.spec | endswith(".new")) | \
  {instanceId, esVersion, nodeSpec: .nodeSpec.spec, amount: .nodeAmount}'
```

**Method 2: Create a test instance manually**
```bash
# Create a small test instance via Aliyun Console to verify:
# - The spec is available in your chosen zone
# - The payment type works
# - The version is supported
#
# Once verified, use the same configuration in Pulumi
```

**Method 3: Check Aliyun Console**
1. Go to Elasticsearch product page in Aliyun Console
2. Select your region (e.g., China East 2 - Shanghai)
3. Click "Create Instance"
4. The dropdown menus will show available:
   - Versions (in "Version" dropdown)
   - Instance types (in "Node Specification" dropdown)
   - Zones (in "Available Zone" dropdown)

**Known Valid Values** (tested and confirmed):
```bash
# Versions (as of 2026):
8.13_with_X-Pack    # Latest stable - BEST for .new specs ✅
8.9_with_X-Pack     # Previous stable
7.16_with_X-Pack    # LTS version
7.10_with_X-Pack    # Older version (limited .new spec support)

# Instance Types (sorted by size):
elasticsearch.sn1ne.large.new    # 1C 4GB - Best compatibility ✅
elasticsearch.sn2ne.large.new    # 2C 8GB
elasticsearch.sn2ne.xlarge.new   # 2C 8GB
elasticsearch.sn2ne.2xlarge.new  # 2C 16GB
elasticsearch.sn2ne.4xlarge.new  # 4C 16GB - Production
elasticsearch.sn2ne.8xlarge.new  # 4C 32GB

# Payment Types:
PostPaid    # Pay-as-you-go (recommended for dev/test)
PrePaid     # Subscription (30-60% cost savings for production)

# Disk Types:
cloud_essd  # Enhanced SSD (REQUIRED, cloud_ssd is NOT supported)

# Performance Levels (for cloud_essd):
PL0   # Baseline (10K IOPS, cheapest)
PL1   # Recommended (50K IOPS, cost-effective) ✅
PL2   # High performance (100K IOPS)
PL3   # Ultra-high (1M IOPS, most expensive)

# Zones (example for cn-shanghai region):
cn-shanghai-b    # Most commonly available
cn-shanghai-e    # Alternative
cn-shanghai-f    # Alternative
```

#### Verify Instance Creation

```bash
# List Elasticsearch instances
aliyun elasticsearch GET /openapi/instances --region cn-shanghai

# Get instance details
aliyun elasticsearch GET /openapi/instances/<instance-id> --region cn-shanghai
```

#### Check Instance Configuration

```bash
# View instance specs and pricing info
aliyun elasticsearch GET /openapi/instances/<instance-id> --region cn-shanghai | jq '.Result | {instanceId, esVersion, paymentType, nodeSpec, nodeAmount}'
```

#### Common Issues

**Issue**: `PRICE.PRICING_PLAN_RESULT_NOT_FOUND`

**Root Cause**: Instance type or payment type not available in selected zone

**Solutions**:
1. Use `.new` suffix instance types
2. Change to version 8.13_with_X-Pack
3. Try different availability zone
4. For PostPaid, try larger instance types (not smaller)

**Issue**: `TheSpecNotEnoughInDetail`

**Root Cause**: Insufficient resources in selected zone for the instance spec

**Solutions**:
1. Use a larger instance type (e.g., `.2xlarge.new` instead of `.large.new`)
2. Try a different availability zone
3. Reduce node count to fit available capacity

**Issue**: `InvalidComponent`

**Root Cause**: Incompatible disk type or configuration

**Solutions**:
1. Use `cloud_essd` (not `cloud_ssd`) for Elasticsearch
2. Ensure `performanceLevel` is set when using `cloud_essd`
3. Kibana disk must be 0 (no dedicated disk)

### Verification Checklist

After successful deployment, verify:

- [ ] Instance status is `active`
- [ ] ES version matches configuration
- [ ] Node count matches configuration
- [ ] Payment type is correct (PostPaid/PrePaid)
- [ ] Node spec has `.new` suffix
- [ ] Disk type is `cloud_essd`
- [ ] Kibana is accessible
- [ ] VPC connectivity works

```bash
# Quick verification script
INSTANCE_ID=$(pulumi stack output es_endpoint | cut -d'.' -f1)
aliyun elasticsearch GET /openapi/instances/$INSTANCE_ID --region cn-shanghai | jq '{
  status: .Result.status,
  version: .Result.esVersion,
  payment: .Result.paymentType,
  nodes: .Result.nodeAmount,
  spec: .Result.nodeSpec.spec,
  disk: .Result.nodeSpec.disk
}'
```

#### Troubleshooting Node Pool Creation

Node pool creation may fail with specific errors. Below are common issues and their solutions.

**Issue 1: `MissingAuth.AliyunOOSLifecycleHook4CSRole`**

**Error Message**:
```
please complete the AliyunOOSLifecycleHook4CSRole ramrole authorization
https://ram.console.aliyun.com/role/authorize?request=...
```

**Root Cause**: ACK requires OOS (Operation Orchestration Service) to manage node lifecycle through lifecycle hooks. This requires a service role authorization.

**Solutions**:

1. **Authorize Service Role** (Recommended):
   ```bash
   # Visit the authorization URL from the error message
   # Or navigate manually:
   # RAM Console → Roles → Find "AliyunOOSLifecycleHook4CSRole"
   # Click "Authorize" and complete the authorization
   ```

2. **Required RAM Permissions** (for node pool creation):
   - `AliyunCSFullAccess`: Container Service full access
   - `AliyunESSFullAccess`: Elastic Scaling Service full access
   - `AliyunECSFullAccess`: ECS full access
   - `AliyunOOSLifecycleHook4CSRole`: OOS lifecycle hook service role

3. **Workaround - Skip Node Pool**:
   - Cluster will be created successfully without worker nodes
   - Manually add nodes later through Aliyun console/CLI
   - Or re-run `pulumi up` after authorizing the role

**Issue 2: `ClusterNameAlreadyExist`**

**Error Message**:
```
cluster name {name} already exist in your clusters
```

**Root Cause**: Cluster with the same name already exists from a previous deployment.

**Solution**: Current implementation automatically detects existing clusters:
```go
// Code automatically checks if cluster exists before creating
existingClusterID, err := r.findClusterByName(ctx, csClient, name)
if err == nil && existingClusterID != "" {
    // Reuse existing cluster, skip creation
    return existingClusterID, "", nil
}
```

To start fresh:
```bash
# Delete existing cluster
alicloud cs DELETE /clusters/<cluster-id> --region cn-shanghai

# Re-run Pulumi deployment
pulumi up --yes
```

**Issue 3: API Parameter Format Errors**

**Error Messages**:
```
InvalidVSwitch.Count: The count of vswitch_ids must be between 1 to 8
MissingParameter.InstanceTypes: The input parameter 'instance_types' is mandatory
```

**Root Cause**: Aliyun CreateClusterNodePool API requires specific parameter format.

**Solution**: Code has been fixed with correct format according to [official API documentation](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-createclusternodepool):

✅ **Correct format** (current implementation):
```json
{
  "name": "default-pool",
  "scaling_group": {
    "vswitch_ids": ["vsw-xxx", "vsw-yyy"],           // ✅ Array, required
    "instance_types": ["ecs.c6.xlarge"],          // ✅ Array, required
    "instance_charge_type": "PostPaid",            // ✅ String, required
    "desired_size": 3,                            // ✅ Long, expected nodes
    "system_disk_category": "cloud_essd",
    "system_disk_size": 120,
    "image_type": "AliyunLinux3",
    "internet_charge_type": "PayByTraffic",
    "login_password": "your-password"
  }
}
```

❌ **Incorrect format** (old implementation):
```json
{
  "vswitch_ids": "vsw-xxx,vsw-yyy",              // ❌ String (wrong)
  "instance_types": "ecs.c6.xlarge",          // ❌ String (wrong)
  "count": 3,                                    // ❌ Wrong parameter name
  "system_disk": {...}                           // ❌ Wrong structure
}
```

**Verification**:
```bash
# Check node pool creation request
pulumi up --yes 2>&1 | grep "Node pool creation request body"

# Verify structure in logs
```

**Issue 4: Nodes Not Ready**

**Symptoms**: Node pool created successfully but nodes show as `NotReady`.

**Solutions**:
1. Wait 2-3 minutes for nodes to initialize and join cluster
2. Check node status:
   ```bash
   # Get cluster ID from Pulumi
   clusterID=$(pulumi stack output ragflow-k8s-k8s:clusterId | head -1)

   # Query kubeconfig dynamically
   region=$(pulumi config get pulumi_ragflow:region)
   kubeconfig=$(GetClusterKubeconfig $clusterID $region ... false)

   # Write to temp file
   echo "$kubeconfig" > /tmp/ack-kubeconfig
   export KUBECONFIG=/tmp/ack-kubeconfig

   # Check nodes
   kubectl get nodes -o wide
   kubectl describe nodes
   ```

3. Common reasons for nodes not ready:
   - **Insufficient quota**: No ECS instances available in the zone
   - **VSwitch exhausted**: No available IP addresses in VSwitch
   - **Security group**: Rules blocking node-to-control-plane communication
   - **Instance startup**: OS or initialization issues (check instance console)

4. Debug steps:
   ```bash
   # Check node pool details
   alicloud cs DescribeClusterNodePools \
     --cluster-id $clusterID \
     --nodepool-id np495def7832f74748a56fc2b6d18d4fd2 \
     --region cn-shanghai

   # Check instance status
   alicloud ecs DescribeInstances \
     --instance-ids i-xxx,i-yyy,i-zzz \
     --region cn-shanghai
   ```

**Key Lessons Learned**:

1. **API Documentation is Critical**: Always refer to official Aliyun API docs for parameter structure
2. **Service Roles ≠ User Permissions**: Some operations require service-linked roles, not just RAM user permissions
3. **Parameter Format Matters**: Arrays vs comma-separated strings, nested structures
4. **State Sync Challenges**: Dynamic resource queries (endpoint, kubeconfig) avoid state synchronization issues
5. **Graceful Degradation**: Node pool creation failure should not break cluster creation
6. **⚠️ Scaling Configuration Issue**: ACK CreateClusterNodePool API may not create ESS scaling configuration automatically (see Issue 5 below)

**Issue 5: Node Pool Shows "failed" Status - Scaling Configuration Missing**

**Symptoms**:
- Pulumi reports: "✓ Worker nodes created successfully!"
- Aliyun console shows: Node pool state = "失败" (failed)
- Total nodes = 0, even with `desired_size: 3`

**Root Cause Analysis**:

After extensive investigation, the issue was identified:

1. **ACK creates ESS scaling group** ✓
   - Scaling group ID: `asg-uf6bonv9l74f1s6nqjlu`
   - MinSize/MaxSize/DesiredCapacity are set correctly

2. **ACK does NOT create ESS scaling configuration** ✗
   - `aliyun ess DescribeScalingConfigurations` returns: `TotalCount: 0`
   - Without scaling configuration, ESS cannot create ECS instances

3. **Scaling group remains inactive**
   - `LifecycleState: "Inactive"`
   - `ActiveCapacity: 0`
   - No scaling activities are triggered

**API Response Analysis**:
```bash
# Node pool status
aliyun cs DescribeClusterNodePools --ClusterId $clusterID --region cn-shanghai
# Returns: state="failed", total_nodes=0

# Scaling group status
aliyun ess DescribeScalingGroups --region cn-shanghai
# Returns: DesiredCapacity=null, LifecycleState="Inactive"

# Scaling configuration
aliyun ess DescribeScalingConfigurations --ScalingGroupId $sgID --region cn-shanghai
# Returns: TotalCount=0 (empty)
```

**Current Status**: ⚠️ **UNRESOLVED**

This appears to be a limitation or bug in the Aliyun ACK CreateClusterNodePool API. The API creates the scaling group but fails to create the scaling configuration, which is required for ECS instance creation.

**Workarounds**:

**Option 1**: Create node pool manually through Aliyun Console
1. Visit ACK console: https://cs.console.aliyun.com/
2. Navigate to your cluster → Node Pools
3. Create node pool with same configuration
4. Console flow properly creates scaling configuration

**Option 2**: Create ECS instances directly and add to cluster
```bash
# Manually create ECS instances
aliyun ecs CreateInstance \
  --region cn-shanghai \
  --ImageId aliyun_3_x64_20G_alibase_20251215.vhd \
  --InstanceType ecs.c6.xlarge \
  --VSwitchId vsw-uf61ig5srtcnrc32k61oy \
  --SecurityGroupId sg-uf68ijrniupv5d33iqw2 \
  --Password Ragflow@123456 \
  --SystemDiskCategory cloud_essd \
  --SystemDiskSize 120

# Then attach to cluster using AttachClusterNodes API
# or kubectl certificate approval process
```

**Option 3**: Use Terraform instead of Go SDK
```hcl
# Terraform alicloud_cs_kubernetes_node_pool may handle
# scaling configuration creation differently
resource "alicloud_cs_kubernetes_node_pool" "default" {
  name                 = "default-pool"
  cluster_id           = alicloud_cs_managed_kubernetes.main.id
  vswitch_ids          = [vswitch_id]
  instance_types       = ["ecs.c6.xlarge"]
  desired_size         = 3
  # ... other parameters
}
```

**Investigation Commands**:

```bash
# Check node pool status
aliyun cs DescribeClusterNodePools \
  --ClusterId $clusterID \
  --region cn-shanghai

# Check scaling group
aliyun ess DescribeScalingGroups \
  --region cn-shanghai | jq '.ScalingGroups.ScalingGroup[] | select(.ScalingGroupId | contains("uf6"))'

# Check scaling configurations
aliyun ess DescribeScalingConfigurations \
  --ScalingGroupId $sgID \
  --region cn-shanghai

# Check scaling activities
aliyun ess DescribeScalingActivities \
  --ScalingGroupId $sgID \
  --region cn-shanghai
```

**References**:
- [ACK CreateClusterNodePool API](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-createclusternodepool)
- [ESS CreateScalingConfiguration API](https://help.aliyun.com/zh/ess/developer-reference/api-ess-2014-08-28-createscalingconfiguration)

**Successful Deployment Output**:
```
✓ Cluster already exists: ali-ragflow-k8s (ID: cf4f8dc14070441eeba597fac25f9fcdd)
Node pool created successfully! NodePool ID: np495def7832f74748a56fc2b6d18d4fd2
✓ Worker nodes created successfully!
```

### Architecture Decisions

#### Why Separate Kubernetes and Infrastructure Deployment?

1. **Flexibility**: Kubernetes clusters can be managed separately (ACK, ASK, self-hosted)
2. **Cost**: Infrastructure-only mode allows reusing existing clusters
3. **Control**: Fine-grained control over cluster configuration
4. **Compliance**: Meet organizational requirements for cluster management

#### Why Not Deploy Kubernetes with Pulumi?

Current Pulumi Aliyun provider has limited Kubernetes support:
- ACK (Aliyun Kubernetes Service) support is incomplete
- No dedicated node pool management
- Limited add-on support
- Better alternatives: Aliyun Console/CLI, Terraform

#### Exported Infrastructure Resources

Pulumi outputs infrastructure resource IDs for use in cluster deployment:

```bash
# Get VPC and VSwitch IDs
pulumi stack output vpc_id
pulumi stack output vswitch_ids

# Get Elasticsearch endpoint
pulumi stack output es_endpoint
```

These can be used when creating Kubernetes clusters via console or CLI.

### Cost Optimization

#### Payment Type Selection

| Payment Type | Best For | Cost | Flexibility |
|--------------|----------|------|-------------|
| **PostPaid** | Dev/test, variable workloads | Higher hourly | None - pay as you go |
| **PrePaid** | Production, stable workloads | Lower (30-60% savings) | Commitment required |

**Recommendation**: Use PrePaid for production with known capacity needs

#### Instance Type Selection

- Start with smaller `.new` types (`.large.new`)
- Scale up based on actual usage
- Monitor resource utilization
- Consider multi-zone deployment for high availability

### Migration from Manual to Pulumi

If you have manually created Aliyun resources:

1. **Export existing resource IDs**:
   ```bash
   # Get VPC ID
   aliyun vpc DescribeVpcs --region cn-shanghai

   # Get ES instance ID
   aliyun elasticsearch GET /openapi/instances --region cn-shanghai
   ```

2. **Import into Pulumi** (optional, for resource management):
   ```bash
   pulumi import alicloud:vpc/network:Network ragflow-vpc vpc-xxx
   pulumi import alicloud:elasticsearch/instance:Instance ragflow-es es-cn-xxx
   ```

3. **Or reference existing resources** in Pulumi configuration:
   ```yaml
   pulumi_ragflow:vpc_id: vpc-existing-id
   pulumi_ragflow:es_host: es-cn-existing.elasticsearch.aliyuncs.com
   ```

## Conclusion

This Pulumi deployment provides a modern, programmatic approach to deploying RAGFlow on Kubernetes. It offers:

- **Type Safety**: Python's type system helps catch errors early
- **Reusability**: Modular code structure for easy customization
- **Testing**: Built-in validation and testing capabilities
- **Flexibility**: Full power of Python for complex deployments
- **Equivalence**: Same functionality as the Helm chart deployment
- **Multi-Cloud**: Support for both Kubernetes and Aliyun infrastructure

The deployment is production-ready and can be easily integrated into CI/CD pipelines for automated deployments across multiple environments.
