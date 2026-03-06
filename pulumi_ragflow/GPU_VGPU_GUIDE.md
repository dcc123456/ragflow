# GPU and vGPU Configuration Guide

> **Status**: Future Reference - GPU is not currently deployed on Aliyun

This guide documents GPU configuration options for RAGFlow on Kubernetes clusters, including Aliyun ACK/ACS with HAMi vGPU sharing.

---

## 1. GPU Deployment Options

### Option 1: Aliyun ACK with GPU Nodes

Managed Kubernetes with dedicated GPU ECS instances.

**Characteristics**:
- Full GPU allocation per node
- Direct access to GPU hardware
- Managed node lifecycle

### Option 2: Aliyun ACS with HAMi vGPU

Serverless Kubernetes with vGPU sharing via HAMi (Heterogeneous AI Management Infrastructure).

**Characteristics**:
- Fractional GPU allocation (e.g., 1/2, 1/4 GPU)
- Better resource utilization
- Cost-effective for variable workloads

### Option 3: Standard Kubernetes with GPU Nodes

On-premises or other cloud providers with NVIDIA GPU nodes.

---

## 2. HAMi vGPU Configuration

HAMi provides vGPU sharing capabilities for Kubernetes clusters.

### Architecture

```
┌─────────────────────────────────┐
│  HAMi Scheduler                 │
│  (hami-scheduler)               │
└─────────────────────────────────┘
              ↓
┌─────────────────────────────────┐
│  HAMi Device Plugin             │
│  (hami-device-plugin)           │
│  - Exposes GPU resources        │
│  - Manages GPU allocation       │
└─────────────────────────────────┘
              ↓
┌─────────────────────────────────┐
│  GPU Pods                       │
│  - hamitech.com/gpu             │
│  - hamitech.com/gpumem          │
│  - hamitech.com/gpucores        │
└─────────────────────────────────┘
```

### Key Commands

```bash
# Verify HAMi Installation
kubectl get pods -n kube-system | grep hami

Expected output:
hami-device-plugin-xxxx   1/1     Running   0          Xm
hami-scheduler-xxxx       1/1     Running   0          Xm

# Remove Volcano (if previously installed)
kubectl delete namespace volcano-system

# Check GPU resources
kubectl describe node <gpu-node> | grep -A 10 "hamitech.com"
```

### HAMi vs Volcano (Legacy)

| Feature | Volcano (Old) | HAMi (New) |
|---------|---------------|-------------|
| **GPU Count** | `volcano.sh/gpu-num` | `hamitech.com/gpu` |
| **Memory** | `volcano.sh/gpu-memory` (MB) | `hamitech.com/gpumem` (GB) |
| **Compute** | N/A | `hamitech.com/gpucores` (%) |
| **Scheduler** | `volcano` | `hami-scheduler` |
| **Fractional GPU** | No | Yes |

### GPU Resource Configurations

```yaml
# Full GPU (24GB)
hamitech.com/gpu: "1"
hamitech.com/gpumem: "24Gi"
hamitech.com/gpucores: "100"

# Half GPU (12GB)
hamitech.com/gpu: "1"
hamitech.com/gpumem: "12Gi"
hamitech.com/gpucores: "50"

# Quarter GPU (6GB)
hamitech.com/gpu: "1"
hamitech.com/gpumem: "6Gi"
hamitech.com/gpucores: "25"
```

### Pulumi Configuration

```bash
# Enable GPU hardware
pulumi config set deepdoc_hardware gpu

# Configure VRAM in MB
pulumi config set deepdoc_vram_mb 10240    # 10GB

# Configure vCPU percentage (0-100)
pulumi config set deepdoc_vcore 100        # 100% compute

# Deploy
pulumi up -y
```

---

## 3. Aliyun GPU Instance Types

Aliyun ACK supports various GPU ECS instance types:

| Series | Model | VRAM | Use Case |
|--------|-------|------|----------|
| GU8TF | NVIDIA T4 | 16GB | General ML inference |
| GU6RF | NVIDIA A10 | 24GB | High performance |
| GU30 | NVIDIA A10 | 16GB | Cost-effective |
| P100 | NVIDIA V100 | 16GB | Training & inference |
| P4 | NVIDIA P4 | 8GB | Entry-level inference |

### GPU Model Labels

Required labels for Aliyun GPU pods:

```yaml
labels:
  alibabacloud.com/compute-class: gpu         # CRITICAL
  alibabacloud.com/gpu-model-series: GU8TF   # CRITICAL
```

---

## 4. Knative GPU Autoscaling

Knative Serving can be used for GPU workloads with automatic scaling.

### When Knative Activates

Knative Service is automatically used when ALL conditions are met:

1. ✅ Cluster is on Aliyun (S3 endpoint contains `aliyuncs.com`)
2. ✅ `deepdoc_hardware: gpu` is configured
3. ✅ Knative Serving is installed in cluster

### Autoscaling Configuration

```yaml
autoscaling.knative.dev/minScale: "0"           # Scale to zero when idle
autoscaling.knative.dev/maxScale: "10"          # Maximum 10 pods
autoscaling.knative.dev/target: "1"             # 1 concurrent request per pod
autoscaling.knative.dev/scaleToZeroGracePeriodSeconds: "30"
```

### Cost Savings

| Deployment Type | Daily Cost (1 GPU) |
|-----------------|-------------------|
| Standard (24/7) | $36/day |
| Knative (4h active) | $6/day |
| **Savings** | **83%** |

---

## 5. CUDA and TensorRT Compatibility

### Driver Version Matrix

| Driver Version | CUDA Runtime | TensorRT Version |
|----------------|--------------|-------------------|
| 535.x | 12.1, 12.2 | 8.6.x |
| 550.x | 12.4, 12.6 | 10.x |

### CUDA Initialization Issues

**Symptom**: `CUDA initialization failure with error: 35`

**Cause**: TensorRT/CUDA version incompatible with GPU driver.

**Solution**:
- Driver 535: Use CUDA 12.1 + TensorRT 8.6.3
- Driver 550+: Use CUDA 12.6+ + TensorRT 10.x

---

## 6. Troubleshooting

### Pod Not Scheduling

```bash
kubectl describe pod -n ragflow -l app=deepdoc | tail -20
```

### Insufficient GPU Resources

```bash
kubectl describe node <gpu-node> | grep -A 10 "Allocated resources"
```

### HAMi Plugin Issues

```bash
kubectl logs -n kube-system -l app=hami-device-plugin --tail=50
```

### GPU Not Available

```bash
# Verify GPU nodes
kubectl get nodes -l gpu=true

# Check NVIDIA runtime
kubectl get runtimeclass nvidia
```
