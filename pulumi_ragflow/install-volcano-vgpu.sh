#!/bin/bash
#
# Volcano vGPU Installation Script
# This script installs Volcano scheduler for GPU sharing
#
# Reference: https://volcano.sh/en/docs/
#

set -e

echo "========================================="
echo "Installing Volcano on Kubernetes"
echo "========================================="

# Check if cluster is accessible
if ! kubectl cluster-info &> /dev/null; then
    echo "Error: Cannot access Kubernetes cluster"
    exit 1
fi

echo "✓ Kubernetes cluster is accessible"

# Check GPU node
GPU_NODE=$(kubectl get nodes -l gpu=true -o name | head -1)
if [ -z "$GPU_NODE" ]; then
    echo "Error: No GPU nodes found (expected label: gpu=true)"
    exit 1
fi

echo "✓ Found GPU node: $GPU_NODE"

# Check current GPU resources
echo ""
echo "Current GPU resources:"
kubectl describe node "$GPU_NODE" | grep -A 5 "nvidia.com/gpu" || echo "No GPU resources found yet"

# Install Volcano scheduler if not present
echo ""
echo "Checking Volcano scheduler..."
if ! kubectl get namespace volcano-system &> /dev/null; then
    echo "Installing Volcano scheduler..."

    # Install Volcano scheduler from official YAML
    kubectl apply -f https://raw.githubusercontent.com/volcano-sh/volcano/master/installer/volcano-development.yaml

    # Wait for Volcano to be ready
    echo "Waiting for Volcano scheduler to be ready..."
    kubectl wait --for=condition=available -n volcano-system deployment/volcano-scheduler --timeout=120s
    kubectl wait --for=condition=available -n volcano-system deployment/volcano-controllers --timeout=120s
    kubectl wait --for=condition=available -n volcano-system deployment/volcano-admission --timeout=120s

    echo "✓ Volcano scheduler installed"
else
    echo "✓ Volcano scheduler already installed"
fi

echo ""
echo "========================================="
echo "Verifying Volcano Installation"
echo "========================================="

echo ""
echo "Volcano scheduler pods:"
kubectl get pods -n volcano-system

echo ""
echo "========================================="
echo "Installation Complete!"
echo "========================================="
echo ""
echo "Volcano scheduler is now running."
echo ""
echo "Important Notes:"
echo "1. Volcano vGPU device plugin is NOT installed (deprecated)"
echo "   Use Volcano's native GPU sharing instead"
echo "2. For GPU sharing, use:"
echo "   - volcano.sh/gpu-num: Number of GPUs (integer, typically 1)"
echo "   - volcano.sh/gpu-memory: GPU memory in MB (allows sharing)"
echo "3. Make sure your pods use 'schedulerName: volcano'"
echo ""
echo "To verify GPU resources: kubectl describe node $GPU_NODE | grep volcano"
echo ""
echo "Example Pod spec with GPU sharing:"
cat <<'EXAMPLE'

apiVersion: v1
kind: Pod
metadata:
  name: gpu-shared-test
spec:
  schedulerName: volcano
  containers:
  - name: test
    image: nvidia/cuda:11.0.3-base-ubuntu20.04
    command: ["nvidia-smi"]
    resources:
      limits:
        volcano.sh/gpu-num: 1      # Use 1 GPU
        volcano.sh/gpu-memory: 4096 # Allocate 4GB VRAM
    env:
    - name: NVIDIA_VISIBLE_DEVICES
      value: "all"
  nodeSelector:
    gpu: "true"
  tolerations:
  - key: nvidia.com/gpu
    operator: Equal
    value: "true"
    effect: NoSchedule

EXAMPLE

echo ""
echo "For more info: https://volcano.sh/en/docs/"
