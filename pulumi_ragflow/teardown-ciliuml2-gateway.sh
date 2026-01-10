#!/bin/bash

# Cilium Gateway API Teardown Script
# This script helps tear down resources created by setup-ciliuml2-gateway.sh
# and restore configuration changes.
#
# Prerequisites:
# - kubectl: Kubernetes command-line tool
# - cilium: Cilium CLI tool

set -e

echo "🗑️ Tearing down Cilium Gateway API..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if cilium CLI is available
if ! command -v cilium &> /dev/null; then
    echo -e "${RED}❌ cilium CLI not found. Please install it first:${NC}"
    echo "   curl -L --remote-name-all https://github.com/cilium/cilium-cli/releases/latest/download/cilium-linux-amd64.tar.gz"
    echo "   tar xzvf cilium-linux-amd64.tar.gz"
    echo "   sudo cp cilium /usr/local/bin/"
    exit 1
fi

echo -e "${YELLOW}🧹 Removing nginx demo...${NC}"
kubectl delete -f nginx-demo.yaml --ignore-not-found=true

echo -e "${YELLOW}🧹 Removing L2 Announcement Policy...${NC}"
kubectl delete -f l2-policy.yaml --ignore-not-found=true

echo -e "${YELLOW}🔧 Restoring Cilium Gateway API configuration...${NC}"
cilium config set enable-gateway-api false
cilium config set gateway-api-secrets-namespace ""

echo -e "${YELLOW}⏳ Waiting for Cilium to restart...${NC}"
kubectl rollout restart deployment/cilium-operator -n kube-system
kubectl rollout status deployment/cilium-operator -n kube-system

echo -e "${YELLOW}🔍 Checking GatewayClass...${NC}"
kubectl get gatewayclass

echo -e "${GREEN}✅ Cilium Gateway API teardown complete!${NC}"
echo ""
echo -e "${YELLOW}📋 Note:${NC}"
echo "Gateway API CRDs are not removed to avoid affecting other setups."
echo "If you want to remove them manually:"
echo "  kubectl delete -f standard-install-v1.4.1.yaml"
echo ""
echo -e "${YELLOW}🔗 Useful commands:${NC}"
echo "kubectl get gatewayclass"
echo "cilium status"