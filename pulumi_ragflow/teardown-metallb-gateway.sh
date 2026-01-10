#!/bin/bash

# MetalLB Gateway API Teardown Script
# This script helps tear down resources created by setup-metallb-gateway.sh
# and restore configuration changes.
#
# Prerequisites:
# - kubectl: Kubernetes command-line tool
# - helm: Helm CLI tool (if Nginx Gateway Fabric was installed via Helm)

set -e

echo "🗑️ Tearing down MetalLB Gateway API..."

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

# Helper: wait up to $WAIT_TIMEOUT seconds for a namespace to be removed
# If still present, clear finalizers and force delete as last resort
WAIT_TIMEOUT=30
function wait_and_force_delete_namespace() {
    ns="$1"
    echo -e "${YELLOW}Attempting to delete namespace '$ns' gracefully...${NC}"
    kubectl delete namespace "$ns" --ignore-not-found=true || true

    elapsed=0
    while kubectl get namespace "$ns" >/dev/null 2>&1 && [ $elapsed -lt $WAIT_TIMEOUT ]; do
        sleep 2
        elapsed=$((elapsed + 2))
    done

    if kubectl get namespace "$ns" >/dev/null 2>&1; then
        echo -e "${YELLOW}Namespace '$ns' still exists after timeout; forcing removal by clearing finalizers...${NC}"
        kubectl patch namespace "$ns" -p '{"metadata":{"finalizers":null}}' --type=merge || true
        kubectl delete namespace "$ns" --force --grace-period=0 --ignore-not-found=true || true
        # one more wait
        elapsed=0
        while kubectl get namespace "$ns" >/dev/null 2>&1 && [ $elapsed -lt $WAIT_TIMEOUT ]; do
            sleep 2
            elapsed=$((elapsed + 2))
        done
        if kubectl get namespace "$ns" >/dev/null 2>&1; then
            echo -e "${RED}Failed to remove namespace '$ns' after forced delete. Manual cleanup may be required.${NC}"
        else
            echo -e "${GREEN}Namespace '$ns' removed successfully (forced).${NC}"
        fi
    else
        echo -e "${GREEN}Namespace '$ns' removed successfully.${NC}"
    fi
}

# Helper: wait and force delete CRDs in a group (e.g., gateway.networking.k8s.io)
function wait_and_force_delete_crd_group() {
    group="$1"
    echo -e "${YELLOW}Deleting CRDs from group '$group'...${NC}"
    kubectl delete -f gateway-api-crds-v1.4.1.yaml --ignore-not-found=true || true

    elapsed=0
    while kubectl get crd -o name | grep -q "$group" >/dev/null 2>&1 && [ $elapsed -lt $WAIT_TIMEOUT ]; do
        sleep 2
        elapsed=$((elapsed + 2))
    done

    remaining=$(kubectl get crd -o name | grep "$group" || true)
    if [ -n "$remaining" ]; then
        echo -e "${YELLOW}CRDs in group '$group' still exist after timeout; forcing removal...${NC}"
        for crd in $remaining; do
            echo -e "${YELLOW}Clearing finalizers for $crd...${NC}"
            kubectl patch "$crd" -p '{"metadata":{"finalizers":null}}' --type=merge || true
            echo -e "${YELLOW}Deleting $crd...${NC}"
            kubectl delete "$crd" --force --grace-period=0 --ignore-not-found=true || true
        done
        # verify
        remaining=$(kubectl get crd -o name | grep "$group" || true)
        if [ -n "$remaining" ]; then
            echo -e "${RED}Failed to remove some CRDs in group '$group'. Manual cleanup may be required:${NC}"
            echo "$remaining"
        else
            echo -e "${GREEN}All CRDs in group '$group' removed successfully.${NC}"
        fi
    else
        echo -e "${GREEN}All CRDs in group '$group' removed successfully.${NC}"
    fi
}

# Teardown order is the reverse of setup for safe dependency removal:
# 1) Remove Nginx Gateway Fabric (it depends on MetalLB for LoadBalancer)
# 2) Remove MetalLB IP pool
# 3) Remove MetalLB itself
# 4) Remove Gateway API CRDs last

echo -e "${YELLOW}🧹 Removing Nginx Gateway Fabric...${NC}"
if command -v helm &> /dev/null; then
    helm uninstall nginx-gateway --namespace nginx-gateway --ignore-not-found
    # Wait and force-delete namespace if needed
    wait_and_force_delete_namespace nginx-gateway
else
    echo -e "${YELLOW}Helm not found; skipping Helm uninstall. Please remove Nginx Gateway Fabric manually.${NC}"
fi

# Remove MetalLB IP pool and manifests in one operation
echo -e "${YELLOW}🧹 Removing MetalLB manifests and IP pool...${NC}"
# Delete both the IP pool and the MetalLB manifests in one kubectl command
kubectl delete -f ip-pool.yaml -f metallb-v0.14.8.yaml --ignore-not-found=true

# Wait and force-delete metallb-system namespace if needed
wait_and_force_delete_namespace metallb-system

echo -e "${YELLOW}🧹 Removing Gateway API CRDs...${NC}"
# Try to delete CRDs and force-remove any remaining gateway.networking.k8s.io CRDs if necessary
wait_and_force_delete_crd_group gateway.networking.k8s.io

echo -e "${YELLOW}🔧 Restoring Cilium Gateway API configuration...${NC}"
if command -v cilium &> /dev/null; then
    cilium config set enable-gateway-api true
    echo -e "${YELLOW}⏳ Waiting for Cilium to restart...${NC}"
    kubectl rollout restart deployment/cilium-operator -n kube-system
    kubectl rollout status deployment/cilium-operator -n kube-system
else
    echo -e "${YELLOW}cilium CLI not found; skipping Cilium config restoration.${NC}"
fi

echo -e "${YELLOW}🔍 Checking GatewayClass...${NC}"
# `kubectl get gatewayclass` will fail if the Gateway CRDs were removed; handle that gracefully
if ! kubectl get gatewayclass >/dev/null 2>&1; then
    echo -e "${YELLOW}No GatewayClass found (CRDs likely removed).${NC}"
else
    kubectl get gatewayclass
fi

echo -e "${GREEN}✅ MetalLB Gateway API teardown complete!${NC}"
echo ""
echo -e "${YELLOW}📋 Note:${NC}"
echo "If any resources were not removed, check for dependencies or manual cleanup."
echo ""
echo -e "${YELLOW}🔗 Useful commands:${NC}"
echo "kubectl get gatewayclass"
echo "kubectl get svc -A"
echo "kubectl get pods -A | grep metallb"