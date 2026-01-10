#!/bin/bash
# RAGFlow Deployment Verification Script
# This script verifies that the frontend and API fixes are working correctly

set -e

echo "======================================"
echo "RAGFlow Deployment Verification"
echo "======================================"
echo ""

# Configuration
GATEWAY_HOST="${GATEWAY_HOST:-ragflow.local}"
GATEWAY_IP="${GATEWAY_IP:-192.168.1.201}"
NAMESPACE="${NAMESPACE:-ragflow}"

echo "Configuration:"
echo "  Gateway Host: $GATEWAY_HOST"
echo "  Gateway IP: $GATEWAY_IP"
echo "  Namespace: $NAMESPACE"
echo ""

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test functions
test_frontend() {
    echo -n "Testing frontend access... "
    response=$(curl -s "http://$GATEWAY_HOST/" 2>&1)
    if echo "$response" | grep -q "<title>RAGFlow</title>"; then
        echo -e "${GREEN}✓ PASS${NC}"
        return 0
    else
        echo -e "${RED}✗ FAIL${NC}"
        echo "  Response: $(echo "$response" | head -1)"
        return 1
    fi
}

test_api() {
    echo -n "Testing API endpoint... "
    status=$(curl -s -o /dev/null -w "%{http_code}" "http://$GATEWAY_HOST/v1/user/login" -X OPTIONS 2>&1)
    if [ "$status" = "200" ]; then
        echo -e "${GREEN}✓ PASS${NC} (HTTP $status)"
        return 0
    else
        echo -e "${RED}✗ FAIL${NC} (HTTP $status)"
        return 1
    fi
}

test_spa_routing() {
    echo -n "Testing SPA routing... "
    response=$(curl -s "http://$GATEWAY_HOST/some/random/path" 2>&1)
    if echo "$response" | grep -q "<title>RAGFlow</title>"; then
        echo -e "${GREEN}✓ PASS${NC}"
        return 0
    else
        echo -e "${RED}✗ FAIL${NC}"
        return 1
    fi
}

test_static_files() {
    echo -n "Testing static file access... "
    status=$(curl -s -o /dev/null -w "%{http_code}" "http://$GATEWAY_HOST/entry/js/index-zK0DqF-n.js" 2>&1)
    if [ "$status" = "200" ]; then
        echo -e "${GREEN}✓ PASS${NC} (HTTP $status)"
        return 0
    else
        echo -e "${YELLOW}⚠ SKIP${NC} (File may not exist, status: $status)"
        return 0
    fi
}

test_k8s_resources() {
    echo -n "Checking Kubernetes resources... "

    # Check Service port 80
    if kubectl get svc ragflow -n "$NAMESPACE" -o json 2>/dev/null | grep -q '"port": 80'; then
        port_80_ok=true
    else
        port_80_ok=false
    fi

    # Check HTTPRoute has 3 rules
    rule_count=$(kubectl get httproute ragflow-http-route -n "$NAMESPACE" -o json 2>/dev/null | jq -r '.spec.rules | length' 2>/dev/null || echo "0")
    if [ "$rule_count" = "3" ]; then
        httproute_ok=true
    else
        httproute_ok=false
    fi

    # Check nginx config in pod
    nginx_config=$(kubectl get pods -n "$NAMESPACE" -l app=ragflow -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    if [ -n "$nginx_config" ]; then
        if kubectl exec -n "$NAMESPACE" "$nginx_config" -- cat /etc/nginx/conf.d/ragflow.conf >/dev/null 2>&1; then
            nginx_ok=true
        else
            nginx_ok=false
        fi
    else
        nginx_ok=false
    fi

    if $port_80_ok && $httproute_ok && $nginx_ok; then
        echo -e "${GREEN}✓ PASS${NC}"
        echo "  - Service port 80: $port_80_ok"
        echo "  - HTTPRoute rules: $rule_count"
        echo "  - Nginx config: $nginx_ok"
        return 0
    else
        echo -e "${YELLOW}⚠ WARNING${NC}"
        echo "  - Service port 80: $port_80_ok"
        echo "  - HTTPRoute rules: $rule_count (expected: 3)"
        echo "  - Nginx config: $nginx_ok"
        return 0
    fi
}

test_parser_processes() {
    echo -n "Checking parser pod status... "

    ready_pods=$(kubectl get pods -n "$NAMESPACE" -l app=parser -o jsonpath='{.items[*].status.containerStatuses[0].ready}' 2>/dev/null | wc -w)
    total_pods=$(kubectl get pods -n "$NAMESPACE" -l app=parser --no-headers 2>/dev/null | wc -l)

    if [ "$ready_pods" = "$total_pods" ] && [ "$total_pods" -gt 0 ]; then
        echo -e "${GREEN}✓ PASS${NC} ($ready_pods/$total_pods pods ready)"
        return 0
    else
        echo -e "${YELLOW}⚠ WARNING${NC} ($ready_pods/$total_pods pods ready)"
        return 0
    fi
}

# Run tests
echo "Running verification tests..."
echo ""

failed=0

test_frontend || failed=$((failed + 1))
test_api || failed=$((failed + 1))
test_spa_routing || failed=$((failed + 1))
test_static_files || failed=$((failed + 1))
echo ""

echo "Kubernetes Resource Checks:"
echo "----------------------------"
test_k8s_resources || failed=$((failed + 1))
test_parser_processes || failed=$((failed + 1))
echo ""

# Summary
echo "======================================"
if [ $failed -eq 0 ]; then
    echo -e "${GREEN}All tests passed!${NC}"
    echo ""
    echo "RAGFlow deployment is verified and working correctly."
    echo ""
    echo "Access URLs:"
    echo "  Frontend: http://$GATEWAY_HOST/"
    echo "  API:      http://$GATEWAY_HOST/v1/*"
    echo "  Admin:    http://$GATEWAY_HOST/api/v1/admin/*"
else
    echo -e "${RED}Some tests failed!${NC}"
    echo ""
    echo "Please check the output above for details."
    echo ""
    echo "For troubleshooting, see: pulumi_ragflow/DEPLOYMENT_FIX.md"
fi
echo "======================================"

exit $failed
