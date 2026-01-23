
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
- **RAGFLOW_SECRET_KEY**: Secret key for session signing (default: "infini_rag_flow_secret_key_1234567890"). **Important**: All replicas must use the same key. Use `pulumi config set --secret ragflow_secret_key <your-key>` for production.

### Configuration Examples

#### Using Elasticsearch (Default)

All configurations are hardcoded in `config.go`. The deployment uses Elasticsearch by default.

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
| `mysql.storage.capacity` | `CONFIG["mysql"]["storage"]["capacity"]` |

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

## Conclusion

This Pulumi deployment provides a modern, programmatic approach to deploying RAGFlow on Kubernetes. It offers:

- **Type Safety**: Python's type system helps catch errors early
- **Reusability**: Modular code structure for easy customization
- **Testing**: Built-in validation and testing capabilities
- **Flexibility**: Full power of Python for complex deployments
- **Equivalence**: Same functionality as the Helm chart deployment

The deployment is production-ready and can be easily integrated into CI/CD pipelines for automated deployments across multiple environments.
