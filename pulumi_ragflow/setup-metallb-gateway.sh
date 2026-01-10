#!/bin/bash

# Nginx Gateway API Setup Script
# This script helps set up Nginx Gateway Fabric as a Gateway API controller
# and installs MetalLB for LoadBalancer support
#
# Prerequisites:
# - kubectl: Kubernetes command-line tool
#
# Note: The Gateway API CRDs and Nginx Gateway Fabric installation steps download files from GitHub.
# If you are behind a firewall or have network restrictions, you may need to
# set up an HTTP proxy before running this script:
#   export HTTP_PROXY=http://your-proxy:port
#   export HTTPS_PROXY=http://your-proxy:port

# Creates:
# - Gateway API CRDs (gateway.networking.k8s.io)
# - Namespace `nginx-gateway` with Nginx Gateway Fabric deployments (pods labeled app.kubernetes.io/name=nginx-gateway-fabric)
# - GatewayClass `nginx` (installed by Nginx Gateway Fabric Helm chart)
# - MetalLB components in namespace `metallb-system` (controller, speaker, webhook)
# - IPAddressPool / L2Advertisement from `ip-pool.yaml` used to assign external IPs
# - Deploys demo `nginx-demo` Deployment and LoadBalancer Service `nginx-lb` for connectivity testing

set -e

echo "🚀 Setting up Nginx Gateway API..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}❌ kubectl not found. Please install it first.${NC}"
    exit 1
fi

echo -e "${YELLOW}📦 Installing Gateway API CRDs...${NC}"
GATEWAY_CRDS_URL="https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.1/standard-install.yaml"
GATEWAY_CRDS_FILE="gateway-api-crds-v1.4.1.yaml"
if [ ! -f "$GATEWAY_CRDS_FILE" ]; then
  curl -o "$GATEWAY_CRDS_FILE" "$GATEWAY_CRDS_URL"
fi
kubectl apply -f "$GATEWAY_CRDS_FILE"

echo -e "${YELLOW}📦 Installing Nginx Gateway Fabric...${NC}"
# The Nginx Gateway Fabric Helm chart installs Gateway API components and
# creates a GatewayClass named "nginx".
if command -v helm &> /dev/null; then
  # https://github.com/nginx/nginx-gateway-fabric
  # https://docs.nginx.com/nginx-gateway-fabric/install/helm/
  # Need to add ports to the LoadBalancer service per application need.
  helm upgrade --install nginx-gateway \
    oci://ghcr.io/nginx/charts/nginx-gateway-fabric \
    --version 2.2.2 \
    --namespace nginx-gateway \
    --create-namespace \
    --set service.ports[0].port=80 \
    --set service.ports[0].targetPort=80 \
    --set service.ports[1].port=443 \
    --set service.ports[1].targetPort=443 \
    --set service.ports[2].port=9380 \
    --set service.ports[2].targetPort=9380 \
    --set service.ports[3].port=9381 \
    --set service.ports[3].targetPort=9381 \
    --set service.ports[4].port=9382 \
    --set service.ports[4].targetPort=9382
else
  echo -e "${RED}❌ Helm not found. Please install Helm or Nginx Gateway Fabric manually.${NC}"
  exit 1
fi

echo -e "${YELLOW}📦 Installing MetalLB for LoadBalancer support...${NC}"
METALLB_URL="https://raw.githubusercontent.com/metallb/metallb/v0.15.3/config/manifests/metallb-native.yaml"
METALLB_FILE="metallb-v0.15.3.yaml"
if [ ! -f "$METALLB_FILE" ]; then
  curl -o "$METALLB_FILE" "$METALLB_URL"
fi
kubectl apply -f "$METALLB_FILE"

echo -e "${YELLOW}⏳ Waiting for MetalLB to be ready...${NC}"
kubectl wait --for=condition=ready pod -l app=metallb,component=controller -n metallb-system --timeout=60s

# Wait for MetalLB webhook service to have endpoints (webhook must be ready before applying IP pools)
WEBHOOK_SVC="metallb-webhook-service"
WEBHOOK_NS="metallb-system"
WAIT_WEBHOOK_TIMEOUT=60
echo -e "${YELLOW}⏳ Waiting up to ${WAIT_WEBHOOK_TIMEOUT}s for $WEBHOOK_SVC endpoints to be ready...${NC}"
elapsed=0
while [ $elapsed -lt $WAIT_WEBHOOK_TIMEOUT ]; do
  webhook_ip=$(kubectl get endpoints "$WEBHOOK_SVC" -n "$WEBHOOK_NS" -o jsonpath='{.subsets[0].addresses[0].ip}' 2>/dev/null || true)
  if [ -n "$webhook_ip" ]; then
    echo -e "${GREEN}✅ MetalLB webhook endpoints ready: $webhook_ip${NC}"
    break
  fi
  sleep 3
  elapsed=$((elapsed + 3))
done

if [ -z "$webhook_ip" ]; then
  echo -e "${RED}❌ MetalLB webhook endpoints not ready after ${WAIT_WEBHOOK_TIMEOUT}s. Aborting—please check MetalLB webhook and try again.${NC}"
  exit 1
fi

# Ensure MetalLB webhook's Cilium policy is in place before CRDs are installed.
# This creates the metallb-system namespace (if missing), applies the
# allow-metallb-webhook policy (if present) and annotates it for easy
# identification/cleanup.
WEBHOOK_POLICY_FILE="allow-metallb-webhook.yaml"
echo -e "${YELLOW}🔐 Ensuring Cilium policy for MetalLB webhook is applied...${NC}"
kubectl apply -f "$WEBHOOK_POLICY_FILE"

echo -e "${YELLOW}⏳ Waiting for MetallLB controller pod to be ready...${NC}"
kubectl wait --for=condition=ready pod -l component=controller -n metallb-system --timeout=60s

# Apply IP pool once; fail on error and let caller handle retries/inspection
echo -e "${YELLOW}🔧 Applying MetalLB IP pool configuration...${NC}"
if ! kubectl apply -f ip-pool.yaml; then
  echo -e "${RED}❌ Failed to apply ip-pool.yaml. Please check MetalLB webhook readiness and logs.${NC}"
  exit 1
fi

echo -e "${GREEN}✅ IP pool applied successfully${NC}"

echo -e "${YELLOW}⏳ Waiting for nginx-gateway-fabric to be ready...${NC}"
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=nginx-gateway-fabric -n nginx-gateway --timeout=30s

echo -e "${YELLOW}🔍 Checking GatewayClass...${NC}"
kubectl get gatewayclass

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
    echo -e "${GREEN}✅ Connectivity test passed! nginx-demo is accessible at http://$EXTERNAL_IP${NC}"
  else
    echo -e "${RED}❌ Connectivity test failed! nginx-demo is inaccessible at http://$EXTERNAL_IP${NC}"
    exit 1
  fi
else
  echo -e "${RED}❌ No external IP found for nginx-lb service${NC}"
  exit 1
fi

echo -e "${GREEN}✅ Nginx Gateway API setup complete!${NC}"
echo ""
echo -e "${YELLOW}📋 Next steps:${NC}"
echo "1. Nginx Gateway Fabric is running with a LoadBalancer service at http://$EXTERNAL_IP"
echo "2. You can now deploy your own applications using Gateway API"
echo ""
echo -e "${YELLOW}🔗 Useful commands:${NC}"
echo "kubectl get gateway -A"
echo "kubectl get httproute -A"
echo "kubectl get svc -A"
echo "kubectl get ipaddresspool -n metallb-system"