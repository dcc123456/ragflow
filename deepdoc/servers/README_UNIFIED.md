# Unified DeepDoc Server - Deployment Guide

## Overview

The Unified DeepDoc Server consolidates three model services into a single Docker image and process:

- **DLA** (Document Layout Analysis) - Document structure analysis using YOLOv10 TensorRT
- **OCR** (Optical Character Recognition) - Text detection and recognition using PaddleOCR
- **TSR** (Table Structure Recognition) - Table structure analysis using YOLOv8 TensorRT

**Available Variants:**
- **GPU Version**: Full functionality with CUDA 12.8, TensorRT, and GPU-optimized models
- **CPU Version**: OCR-only functionality for CPU-only deployments

**Key Benefits:**
- **Operational Simplicity**: One Docker image, one container to deploy and monitor
- **Resource Efficiency**: Shared GPU memory footprint through single process (GPU version)
- **Simplified Scaling**: Single port (8000) with path-based routing to all models
- **Worker Auto-Restart**: Built-in resilience with LitServe 0.2.17+
- **Flexible Deployment**: Choose GPU or CPU variant based on your infrastructure

## Architecture

### Multi-Endpoint Design

The server uses LitServe's multi-endpoint capability to serve all models on a single port:

```
http://deepdoc-service:8000/predict/dla  - Document Layout Analysis (GPU only)
http://deepdoc-service:8000/predict/ocr  - OCR (detection & recognition)
http://deepdoc-service:8000/predict/tsr  - Table Structure Recognition (GPU only)
```

**Note**: DLA and TSR endpoints are only available in the GPU version. The CPU version only supports OCR.

### Directory Structure

```
deepdoc/servers/
├── deepdoc_svr.py            # Main unified server entry point
├── Dockerfile_deepdoc_cpu    # CPU-only Docker image
├── Dockerfile_deepdoc_gpu    # GPU-enabled Docker image
├── pyproject.toml            # Python dependencies with optional GPU extras
├── uv.lock                   # Locked dependency versions
├── dla/
│   ├── dla_svr.py            # DLA endpoint (refactored to LitAPI)
│   ├── yolov10_to_tensor/    # YOLOv10 TensorRT conversion utilities
│   └── dla.trt               # DLA TensorRT engine file (GPU only)
├── ocr/
│   ├── paddleocr_server.py   # OCR endpoint (refactored to LitAPI)
│   └── .paddleocr/           # PaddleOCR cache and models
└── tsr/
    ├── tsr_svr.py            # TSR endpoint (refactored to LitAPI)
    ├── yolov8_to_tensorrt/   # YOLOv8 TensorRT utilities
    └── tsr.trt            # TSR TensorRT engine file (GPU only)
```

## Building the Docker Images

### GPU Version

#### Prerequisites

You need the following files mounted or available during build:

```bash
# uv package manager
uv-x86_64-unknown-linux-gnu.tar.gz

# TensorRT 10.8 for CUDA 12.8
nv-tensorrt-local-repo-ubuntu2404-10.8.0-cuda-12.8_1.0-1_amd64.deb

# YOLOv10 dependencies
yolov10.zip

# Model engine files (must be built first - see instructions below)
dla/dla.trt
tsr/tsr.trt
ocr/.paddleocr/
```

#### Building TensorRT Engines (GPU Only)

The DLA and TSR TensorRT engine files must be generated on a compatible GPU system before building the Docker image.

##### **IMPORTANT: Build Environment Compatibility**

**Known Working Configuration:**
- **NVIDIA Driver**: 570.195.03 (or 565/575 series)
- **CUDA Runtime**: 12.8
- **PyTorch**: 2.9.1 with CUDA 12.8 support
- **TensorRT**: 10.8.0.43
- **Python**: 3.11 or 3.12

**⚠️ Critical Warnings:**
- **NVIDIA Driver 580 is NOT compatible** with current PyTorch and TensorRT versions
  - Driver 580 causes CUDA initialization failures (error code 100)
  - Driver 580 causes PyTorch `torch.cuda.is_available()` to return False
  - **Solution**: Use Driver 570 or downgrade from Driver 580 to 570

##### **Step 1: Prepare Build Environment**

```bash
# On a GPU machine with compatible driver (e.g., inf24 node)
ssh <gpu-node>

# Check driver version (must be 570.x or 565.x)
nvidia-smi
# Expected output: Driver Version: 570.195.03 (or similar)

# Navigate to the servers directory
cd ~/github.com/infiniflow-ai/ragflow_enterprise/deepdoc/servers
```

##### **Step 2: Install Build Dependencies**

```bash
# Install uv package manager
curl -LsSf https://astral.sh/uv/install.sh | sh

# Create Python virtual environment
uv venv .venv_engine_builder
source .venv_engine_builder/bin/activate

# Install PyTorch with CUDA 12.8 support
uv pip install torch torchvision --index-url https://download.pytorch.org/whl/cu128

# Install TensorRT 10.8.0.43
uv pip install 'tensorrt==10.8.0.43' --index-url https://pypi.org/simple
uv pip install 'tensorrt-cu12-bindings==10.8.0.43' --index-url https://pypi.org/simple
uv pip install 'tensorrt-cu12-libs==10.8.0.43' --index-url https://pypi.org/simple

# Install additional dependencies
uv pip install ultralytics onnx onnxscript cuda-python==11.8.7

# Verify installation
python -c "import torch; print(f'PyTorch: {torch.__version__}, CUDA: {torch.cuda.is_available()}')"
python -c "import tensorrt as trt; print(f'TensorRT: {trt.__version__}')"
```

##### **Step 3: Download Model Files**

```bash
# DLA model (doclayout-yolo)
# Source: https://github.com/org/doclayout-yolo or your internal model repository
# Place in: deepdoc/servers/doclayout_yolo_docstructbench_imgsz1024.pt

# TSR model (YOLOv8x) - will be downloaded automatically
cd tsr
python -c "from ultralytics import YOLO; YOLO('yolov8x.pt')"
```

##### **Step 4: Build DLA Engine**

```bash
cd ~/github.com/infiniflow-ai/ragflow_enterprise/deepdoc/servers/dla/yolov10_to_tensor

# Build TensorRT engine
python export.py \
    -o ../../layout.onnx \
    -e ../dla.trt \
    --end2end \
    -p fp16 \
    --v10

# Expected output:
# - Build time: ~3-5 minutes
# - File size: ~40 MB
# - Output: ../dla/dla.trt

# Verify
ls -lh ../dla/dla.trt
# Expected: -rw-r--r-- 1 user user 40M ... dla.trt
```

##### **Step 5: Build TSR Engine**

```bash
cd ~/github.com/infiniflow-ai/ragflow_enterprise/deepdoc/servers/tsr

# Export ONNX with full model weights
python -c 'from ultralytics import YOLO; model = YOLO("yolov8x.pt"); model.export(format="onnx", opset=11, simplify=True, imgsz=640)'

# Verify ONNX file size (should be 200-300 MB)
ls -lh yolov8x.onnx

# Build TensorRT engine
python << 'EOF'
import os
import tensorrt as trt

onnx_path = "yolov8x.onnx"
engine_path = "tsr.trt"

logger = trt.Logger(trt.Logger.ERROR)
builder = trt.Builder(logger)
network = builder.create_network(1 << int(trt.NetworkDefinitionCreationFlag.EXPLICIT_BATCH))
parser = trt.OnnxParser(network, logger)

print(f"Parsing ONNX: {onnx_path}")
with open(onnx_path, 'rb') as model:
    parser.parse(model.read())

print("Building TensorRT engine...")
config = builder.create_builder_config()
config.set_flag(trt.BuilderFlag.FP16)
config.set_memory_pool_limit(trt.MemoryPoolType.WORKSPACE, 4 << 30)

serialized_engine = builder.build_serialized_network(network, config)

with open(engine_path, 'wb') as f:
    f.write(serialized_engine)

size_mb = os.path.getsize(engine_path) / (1024 * 1024)
print(f"✓ Engine saved: {engine_path}")
print(f"  Size: {size_mb:.2f} MB")
EOF

# Expected output:
# - Build time: ~5-10 minutes
# - File size: ~130-140 MB

# Verify
ls -lh tsr.trt
# Expected: -rw-r--r-- 1 user user 134M ... tsr.trt
```

#### Build Command (GPU)

```bash
# Navigate to the correct directory FIRST
cd deepdoc/servers

# Verify you're in the right place
ls Dockerfile_deepdoc_gpu

# Build the GPU image
export DOCKER_BUILDKIT=1
docker build --platform linux/amd64 -f Dockerfile_deepdoc_gpu -t infiniflow-ai/deepdoc_gpu:latest .
```

**Expected build time:** 10-15 minutes
**Image size:** ~8-10 GB

### CPU Version

#### Prerequisites

You need the following files for the CPU version:

```bash
# uv package manager
uv-x86_64-unknown-linux-gnu.tar.gz

# OCR models (no TensorRT engines needed)
ocr/.paddleocr/
```

#### Build Command (CPU)

```bash
# Navigate to the correct directory FIRST
cd deepdoc/servers

# Verify you're in the right place
ls Dockerfile_deepdoc_cpu

# Build the CPU image
export DOCKER_BUILDKIT=1
docker build --platform linux/amd64 -f Dockerfile_deepdoc_cpu -t infiniflow-ai/deepdoc_cpu:latest .
```

**Expected build time:** 5-8 minutes
**Image size:** ~4-6 GB (smaller than GPU version)

### Image Tagging Convention

Images follow a consistent naming pattern:

```
infiniflow-ai/deepdoc_<hardware>:<tag>

Examples:
infiniflow-ai/deepdoc_cpu:latest     # CPU version, latest tag
infiniflow-ai/deepdoc_cpu:v1.0.0      # CPU version, specific tag
infiniflow-ai/deepdoc_gpu:latest     # GPU version, latest tag
infiniflow-ai/deepdoc_gpu:v1.0.0      # GPU version, specific tag
```

## Kubernetes Deployment with Pulumi

### Configuration

The Pulumi configuration automatically selects the appropriate image based on the hardware type:

```bash
cd pulumi_ragflow

# DeepDoc is always enabled (replaces TSR/DLA/OCR services)

# Configure hardware type (default: cpu)
pulumi config set deepdoc_hardware cpu    # Use infiniflow-ai/deepdoc_cpu:latest
# OR
pulumi config set deepdoc_hardware gpu    # Use infiniflow-ai/deepdoc_gpu:latest

# Configure image tag (default: latest)
pulumi config set deepdoc_image_tag latest
# pulumi config set deepdoc_image_tag v1.0.0

# Configure replicas
pulumi config set deepdoc_replicas 1

# GPU Configuration (only for GPU version)
pulumi config set deepdoc_vram_mb 10240   # Total VRAM for all three models
pulumi config set deepdoc_vcore 100       # Compute percentage (0-100)
```

### Deployment Matrix

| Configuration | Hardware Type | Image Used | Features Available |
|--------------|---------------|------------|-------------------|
| `deepdoc_hardware=cpu` | CPU | `infiniflow-ai/deepdoc_cpu:<tag>` | OCR only |
| `deepdoc_hardware=gpu` | GPU | `infiniflow-ai/deepdoc_gpu:<tag>` | DLA + OCR + TSR |

**Note:** When using the unified DeepDoc service, the legacy TSR/DLA/OCR services are automatically disabled.

### Deploy

```bash
# Preview deployment
pulumi preview

# Deploy to Kubernetes
pulumi up
```

### Verification

```bash
# Check deployment status
kubectl get pods -n ragflow -l app=infiniflow-ragflow-deepdoc

# Check service
kubectl get svc -n ragflow infiniflow-ragflow-deepdoc

# View logs
kubectl logs -n ragflow -l app=infiniflow-ragflow-deepdoc --tail=50

# Test endpoints (port-forward to local machine)
kubectl port-forward -n ragflow svc/infiniflow-ragflow-deepdoc 8000:8000

# Test OCR endpoint (works on both CPU and GPU versions)
curl -X POST http://localhost:8000/predict/ocr \
  -F "request=@test_image.jpg" \
  -F "operator=det"

# Test DLA endpoint (GPU version only)
curl -X POST http://localhost:8000/predict/dla \
  -F "request=@test_image.jpg"

# Test TSR endpoint (GPU version only)
curl -X POST http://localhost:8000/predict/tsr \
  -F "request=@test_image.jpg"
```

## Environment Variables

The unified server respects the following environment variables (typically set in RAGFlow):

```bash
# When set, RAGFlow clients will use the unified server endpoints
TENSORRT_DLA_SVR=http://infiniflow-ragflow-deepdoc:8000
TENSORRT_TSR_SVR=http://infiniflow-ragflow-deepdoc:8000
```

**Note:** The OCR client is typically auto-configured by RAGFlow's service discovery.

## Client Integration

### Python Client Examples

The unified server maintains backward-compatible client interfaces:

```python
from deepdoc.vision.dla_cli import DLAClient
from deepdoc.vision.ocr_cli import OCRClient
from deepdoc.vision.tsr_cli import TSRClient

# All clients point to the same unified service
DEEPDOC_URL = "http://infiniflow-ragflow-deepdoc:8000"

# DLA Client (GPU version only)
dla_client = DLAClient(DEEPDOC_URL)
layouts = dla_client.predict(images)

# OCR Client (CPU and GPU versions)
ocr_client = OCRClient(DEEPDOC_URL)
text_boxes = ocr_client.detect(image_array)
texts = ocr_client.recognize_batch(image_arrays)

# TSR Client (GPU version only)
tsr_client = TSRClient(DEEPDOC_URL)
tables = tsr_client.predict(images)
```

### Endpoint Routing

The unified server automatically routes requests based on the path:

- `DLAClient` appends `/predict/dla`
- `OCRClient` appends `/predict/ocr`
- `TSRClient` appends `/predict/tsr`

**Note:** DLA and TSR endpoints will return errors on the CPU version.

## Performance Tuning

### GPU Memory Allocation

The GPU version loads all three models at startup. Recommended VRAM allocations:

| Configuration | VRAM | Use Case |
|--------------|------|----------|
| Minimum | 10GB | Development/testing |
| Recommended | 16GB | Production |
| High Throughput | 24GB+ | High-volume processing |

### CPU Resource Allocation

The CPU version only requires OCR models. Recommended resources:

| Configuration | CPU | RAM | Use Case |
|--------------|-----|-----|----------|
| Minimum | 2 cores | 4GB | Development/testing |
| Recommended | 4 cores | 8GB | Production |

### Worker Configuration

Adjust workers in `deepdoc_svr.py` or via command-line args:

```python
# Default: 2 workers per device (GPU version)
server = ls.LitServer(
    apis=[dla_api, ocr_api, tsr_api],
    accelerator="gpu",  # or "cpu" for CPU version
    workers_per_device=2,
    max_batch_size=8,
)
```

## Troubleshooting

### Common Issues

1. **Out of Memory (OOM)**
   - GPU: Reduce `workers_per_device` or `max_batch_size`, increase `deepdoc_vram_mb`
   - CPU: Increase pod memory limits

2. **Slow First Request**
   - Normal behavior - models are loading
   - Subsequent requests will be faster

3. **DLA/TSR Endpoint Not Found (CPU Version)**
   - Expected behavior - DLA and TSR are GPU-only
   - Use `deepdoc_hardware=gpu` for full functionality

4. **GPU Not Detected (GPU Version)**
   - Ensure GPU nodes are available: `kubectl get nodes -L gpu`
   - Check Volcano scheduler: `kubectl get pods -n volcano-system`

### Debug Mode

Enable verbose logging:

```bash
# Update entrypoint in deployment
ENTRYPOINT ["/app/.venv/bin/python3", "/app/deepdoc_svr.py", "--log-level=debug"]
```

### Health Checks

The unified server exposes LitServe's default health endpoint:

```bash
curl http://deepdoc-service:8000/health
```

## Migration from Separate Services

### Step 1: Deploy Unified Service

```bash
cd pulumi_ragflow

# Configure unified DeepDoc (always enabled)
pulumi config set deepdoc_hardware gpu    # or cpu
pulumi config set deepdoc_image_tag latest

# Deploy
pulumi up
```

### Step 2: Verify Migration

```bash
# Verify unified service is running
kubectl get pods -n ragflow -l app=infiniflow-ragflow-deepdoc

# Test all three endpoints (GPU version)
kubectl exec -it -n ragflow <ragflow-pod> -- curl -X POST \
  http://infiniflow-ragflow-deepdoc:8000/predict/ocr \
  -F "request=@/tmp/test.jpg" -F "operator=det"
```

## References

- [LitServe Multi-Endpoint Documentation](https://lightning.ai/docs/litserve/features/multiple-apis-single-port)
- [LitServe Worker Restart PR](https://github.com/Lightning-AI/LitServe/pull/624)
- [Pulumi Kubernetes Provider](https://www.pulumi.com/registry/packages/kubernetes/)
- [RAGFlow Documentation](../../README.md)
