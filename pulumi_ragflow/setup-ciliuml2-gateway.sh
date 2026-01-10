#!/bin/bash

# Cilium Gateway API Setup Script
# This script helps set up Cilium as a Gateway API controller
#
# Prerequisites:
# - kubectl: Kubernetes command-line tool
# - cilium: Cilium CLI tool
#
# Note: The Gateway API CRDs installation step downloads files from GitHub.
# If you are behind a firewall or have network restrictions, you may need to
# set up an HTTP proxy before running this script:
#   export HTTP_PROXY=http://your-proxy:port
#   export HTTPS_PROXY=http://your-proxy:port

# Creates:
# - Gateway API CRDs (gateway.networking.k8s.io)
# - Enables the Cilium Gateway API feature and configures Cilium (sets enable-gateway-api)
# - GatewayClass `cilium` (controller: io.cilium/gateway-controller)
# - Applies L2 Announcement policy from `l2-policy.yaml`
# - Deploys demo `nginx-demo` Deployment and LoadBalancer Service `nginx-lb` for connectivity testing
# - Pods: cilium-operator, nginx-demo, and related Cilium components

set -e

echo "🚀 Setting up Cilium Gateway API..."

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

echo -e "${YELLOW}📦 Installing Gateway API CRDs...${NC}"
if [ ! -f standard-install-v1.4.1.yaml ]; then
    echo -e "${YELLOW}⬇️ Downloading Gateway API CRDs...${NC}"
    curl -L -o standard-install-v1.4.1.yaml https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.1/standard-install.yaml
fi
kubectl apply -f standard-install-v1.4.1.yaml

echo -e "${YELLOW}🔧 Enabling Cilium Gateway API...${NC}"
cilium config set enable-gateway-api true
cilium config set gateway-api-secrets-namespace default

echo -e "${YELLOW}⏳ Waiting for Cilium to restart...${NC}"
kubectl rollout restart deployment/cilium-operator -n kube-system
kubectl rollout status deployment/cilium-operator -n kube-system

echo -e "${YELLOW}🔍 Checking GatewayClass...${NC}"
kubectl get gatewayclass

echo -e "${GREEN}✅ Cilium Gateway API setup complete!${NC}"
echo ""
echo -e "${YELLOW}� Applying L2 Announcement Policy...${NC}"
kubectl apply -f l2-policy.yaml

echo -e "${YELLOW}🚀 Deploying nginx demo...${NC}"
kubectl apply -f nginx-demo.yaml

echo -e "${YELLOW}⏳ Waiting for nginx deployment...${NC}"
kubectl wait --for=condition=available --timeout=300s deployment/nginx-demo

echo -e "${YELLOW}🔍 Checking LoadBalancer service...${NC}"
kubectl get svc nginx-lb

echo -e "${YELLOW}🧪 Testing connectivity to nginx demo...${NC}"
EXTERNAL_IP=$(kubectl get svc nginx-lb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
if [ -n "$EXTERNAL_IP" ]; then
  if curl -s --max-time 20 http://$EXTERNAL_IP | grep -q "Welcome to nginx"; then
    echo -e "${GREEN}✅ Connectivity test passed! Nginx is accessible at http://$EXTERNAL_IP${NC}"
  else
    echo -e "${RED}❌ Connectivity test failed! Nginx is inaccessible at http://$EXTERNAL_IP${NC}"
    exit 1
  fi
else
  echo -e "${RED}❌ No external IP found for nginx-lb service${NC}"
  exit 1
fi

echo ""
echo -e "${YELLOW}📋 Next steps:${NC}"
echo "1. Deploy your application (Gateway API is now available)"
echo "2. The deployment will show Gateway setup instructions"
echo "3. Run the kubectl commands shown in the deployment output"
echo ""
echo -e "${YELLOW}🔗 Useful commands:${NC}"
echo "kubectl get gateway -A"
echo "kubectl get httproute -A"
echo "cilium status"
echo "kubectl get svc -o wide"
echo -e "${YELLOW}🧪 Testing connectivity to nginx demo...${NC}"
EXTERNAL_IP=$(kubectl get svc nginx-lb -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null)
# fallback to hostname (some LB implementations use hostname)
[ -z "$EXTERNAL_IP" ] && EXTERNAL_IP=$(kubectl get svc nginx-lb -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null)
if [ -n "$EXTERNAL_IP" ]; then
  if curl -s --max-time 20 "http://$EXTERNAL_IP" | grep -q "Welcome to nginx"; then
    echo -e "${GREEN}✅ Connectivity test passed! Nginx is accessible at http://$EXTERNAL_IP${NC}"
  else
    echo -e "${RED}❌ Connectivity test failed! Nginx is inaccessible at http://$EXTERNAL_IP${NC}"
    exit 1
  fi
else
  echo -e "${RED}❌ No external IP found for nginx-lb service${NC}"
  exit 1
fi

echo ""
echo -e "${YELLOW}📋 Next steps:${NC}"
echo "1. Deploy your application (Gateway API is now available)"
echo "2. The deployment will show Gateway setup instructions"
echo "3. Run the kubectl commands shown in the deployment output"
echo ""
echo -e "${YELLOW}🔗 Useful commands:${NC}"
echo "kubectl get gateway -A"
echo "kubectl get httproute -A"
echo "cilium status"
echo "kubectl get svc -o wide"