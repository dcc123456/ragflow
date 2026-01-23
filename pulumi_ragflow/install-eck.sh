#!/bin/bash
set -e

# Install Elastic Cloud on Kubernetes (ECK) Operator
# Latest stable version: 3.2.0 (as of Dec 2025)
ECK_VERSION="3.2.0"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Installing ECK Operator version ${ECK_VERSION}...${NC}"

# Display ECK Operator image information
echo -e "${YELLOW}📦 ECK Operator image: docker.elastic.co/eck/eck-operator:${ECK_VERSION}${NC}"
echo -e "${YELLOW}ℹ️  Ensure your cluster can pull images from docker.elastic.co${NC}"

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}❌ kubectl not found. Please install it first.${NC}"
    exit 1
fi

# Create namespace if it doesn't exist
echo -e "${YELLOW}📦 Creating elastic-system namespace...${NC}"
kubectl create namespace elastic-system --dry-run=client -o yaml | kubectl apply -f -

# Install Custom Resource Definitions (CRDs)
echo -e "${YELLOW}📦 Installing ECK CRDs...${NC}"
CRDS_URL="https://download.elastic.co/downloads/eck/${ECK_VERSION}/crds.yaml"
CRDS_FILE="eck-crds-v${ECK_VERSION}.yaml"
if [ ! -f "$CRDS_FILE" ]; then
    echo "Downloading CRDs from $CRDS_URL"
    curl -o "$CRDS_FILE" "$CRDS_URL"
else
    echo "Using cached CRDs file: $CRDS_FILE"
fi
kubectl apply -f "$CRDS_FILE"

# Install ECK Operator
echo -e "${YELLOW}📦 Installing ECK Operator...${NC}"
OPERATOR_URL="https://download.elastic.co/downloads/eck/${ECK_VERSION}/operator.yaml"
OPERATOR_FILE="eck-operator-v${ECK_VERSION}.yaml"
if [ ! -f "$OPERATOR_FILE" ]; then
    echo "Downloading operator from $OPERATOR_URL"
    curl -o "$OPERATOR_FILE" "$OPERATOR_URL"
else
    echo "Using cached operator file: $OPERATOR_FILE"
fi
kubectl apply -f "$OPERATOR_FILE"

# Wait for operator to be ready
echo -e "${YELLOW}⏳ Waiting for ECK Operator to be ready...${NC}"
kubectl wait --namespace elastic-system --for=condition=ready pod -l control-plane=elastic-operator --timeout=300s

echo -e "${GREEN}✅ ECK Operator installation completed successfully.${NC}"