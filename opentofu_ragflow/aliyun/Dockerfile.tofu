# Dockerfile for OpenTofu RAGFlow Deployment Image
# =============================================================================
# Optimized version: Single layer install + tar archive for efficiency
# =============================================================================

# Usage:
#   docker build -f Dockerfile.tofu -t infiniflow-registry.cn-shanghai.cr.aliyuncs.com/infiniflow/tofu-ragflow:latest .
#   docker push infiniflow-registry.cn-shanghai.cr.aliyuncs.com/infiniflow/tofu-ragflow:latest
# =============================================================================

FROM ubuntu:24.04

# Copy OpenTofu binary from first stage
COPY --from=ghcr.io/opentofu/opentofu:minimal /usr/local/bin/tofu /usr/local/bin/tofu

# Install all dependencies in single layer
# Use system packages instead of pip where possible
RUN apt-get update && apt-get install -y \
    git \
    curl \
    ca-certificates \
    python3 \
    python3-yaml \
    && rm -rf /var/lib/apt/lists/*

# Install kubectl for cluster operations
ARG KUBECTL_VERSION=v1.31.0
RUN curl -LO https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl && \
    chmod +x kubectl && \
    mv kubectl /usr/local/bin/

# Set working directory
WORKDIR /workspace

# Copy Terraform files maintaining directory structure
# Stage 1: Infrastructure resources
COPY stage1-infrastructure/ /workspace/stage1-infrastructure/

# Stage 2: Kubernetes resources
COPY stage2-kubernetes/ /workspace/stage2-kubernetes/

# Common files
COPY variables.tf terraform.tfvars /workspace/

# Create symlinks
RUN cd /workspace/stage1-infrastructure/ && \
    ln -sf ../variables.tf variables.tf && \
    ln -sf ../terraform.tfvars terraform.tfvars && \
    cd /workspace/stage2-kubernetes/ && \
    ln -sf ../variables.tf variables.tf && \
    ln -sf ../terraform.tfvars terraform.tfvars

ENV TF_PLUGIN_CACHE_DIR="/workspace/.terraform.d/plugin-cache"

# Pre-initialize Terraform providers during image build
# Download and verify providers are present
# NOTE: Cache mount at /tmp/plugin-cache speeds up rebuilds by caching downloads
RUN --mount=type=cache,target=/tmp/plugin-cache,sharing=locked \
    set -e \
    && export TF_PLUGIN_CACHE_DIR="/tmp/plugin-cache" \
    && echo "=== Cleaning up existing .terraform directories and lock files ===" \
    && rm -rf /workspace/stage1-infrastructure/.terraform \
    && rm -rf /workspace/stage1-infrastructure/.terraform.lock.hcl \
    && rm -rf /workspace/stage2-kubernetes/.terraform \
    && rm -rf /workspace/stage2-kubernetes/.terraform.lock.hcl \
    && echo "=== Downloading Terraform providers ===" \
    && cd /workspace/stage1-infrastructure \
    && tofu init -upgrade=true -input=false -backend=false \
    && echo "Stage 1: tofu init completed" \
    && cd /workspace/stage2-kubernetes \
    && tofu init -upgrade=true -input=false -backend=false \
    && echo "Stage 2: tofu init completed" \
    && mkdir -p /workspace/.terraform.d/plugin-cache \
    && cp -r /tmp/plugin-cache/registry.opentofu.org /workspace/.terraform.d/plugin-cache/ \
    && echo "=== Verifying provider cache ===" \
    && find /workspace/.terraform.d/plugin-cache -name "terraform-provider-*" -type f | wc -l \
    && echo "=== Cleaning up .terraform directories (keeping only providers) ===" \
    && rm -rf /workspace/stage1-infrastructure/.terraform \
    && rm -rf /workspace/stage2-kubernetes/.terraform

# Common files
COPY ros_tofu_deploy.py /workspace/

# Set environment variables
ENV PATH="/usr/local/bin:${PATH}"
ENV PYTHONUNBUFFERED=1
ENV TF_IN_AUTOMATION=1

# Set entrypoint to Python deployment script
ENTRYPOINT ["python3", "/workspace/ros_tofu_deploy.py"]
