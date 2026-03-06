#!/bin/bash
# Build script for Pulumi RAGFlow Docker image

set -e

# Configuration - customize these values
IMAGE_NAME="${IMAGE_NAME:-pulumi-ragflow}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
REGISTRY="${REGISTRY:-infiniflow-registry.cn-shanghai.cr.aliyuncs.com}"
NAMESPACE="${NAMESPACE:-infiniflow}"

# Full image name
FULL_IMAGE="${REGISTRY}/${NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}"

echo "=========================================="
echo "Building Pulumi RAGFlow Docker Image"
echo "=========================================="
echo "Image: ${FULL_IMAGE}"
echo ""

# Navigate to the REPO ROOT (build context needs access to docker/ directory)
cd "$(dirname "$0")/.."

# Build the image (context = repo root, Dockerfile in pulumi_ragflow/)
echo "Step 1: Building Docker image..."
docker build --build-arg HTTPS_PROXY=http://192.168.1.29:7890 -f pulumi_ragflow/Dockerfile -t "${FULL_IMAGE}" .

echo ""
echo "Step 2: Image built successfully!"
echo ""

# Optional: Push to registry
read -p "Do you want to push the image to registry? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "Step 3: Pushing image..."
    docker push "${FULL_IMAGE}"

    echo ""
    echo "=========================================="
    echo "Image pushed successfully!"
    echo "=========================================="
    echo ""
    echo "Update your ali_ros_ragflow.yaml with:"
    echo "  image: ${FULL_IMAGE}"
    echo ""
fi
