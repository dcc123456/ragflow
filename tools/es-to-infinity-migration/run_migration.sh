#!/bin/bash
# =============================================================================
# ES to Infinity Migration Script Runner
# =============================================================================
# This script simplifies running the migration by:
# 1. Auto-discovering K8s cluster configuration OR
# 2. Loading configuration from environment variables
# 3. Setting up port-forwarding for external cluster access
# 4. Running the migration script with appropriate parameters
#
# Usage:
#   # K8s auto-discovery mode (recommended)
#   ./run_migration.sh --auto-discover
#   ./run_migration.sh --auto-discover --namespace ragflow
#   ./run_migration.sh --auto-discover --dry-run
#
#   # Manual configuration mode
#   ./run_migration.sh [--dry-run]
#
# Environment Variables:
#   RAGFLOW_NAMESPACE - Kubernetes namespace (default: ragflow)
#   ES_HOST           - Elasticsearch host (default: es01)
#   ES_PORT           - Elasticsearch port (default: 9200)
#   ELASTIC_PASSWORD  - Elasticsearch password (required for manual mode)
#   INFINITY_URI      - Infinity URI (default: infinity:23817)
# =============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIGRATION_SCRIPT="$SCRIPT_DIR/migrate_es_to_infinity.py"

# PIDs for port-forward processes (for cleanup)
ES_PF_PID=""
INFINITY_PF_PID=""

# Cleanup function
cleanup_port_forward() {
    if [ -n "$ES_PF_PID" ] && kill -0 "$ES_PF_PID" 2>/dev/null; then
        echo -e "${YELLOW}Stopping ES port-forward (PID: $ES_PF_PID)...${NC}"
        kill "$ES_PF_PID" 2>/dev/null || true
        wait "$ES_PF_PID" 2>/dev/null || true
    fi
    if [ -n "$INFINITY_PF_PID" ] && kill -0 "$INFINITY_PF_PID" 2>/dev/null; then
        echo -e "${YELLOW}Stopping Infinity port-forward (PID: $INFINITY_PF_PID)...${NC}"
        kill "$INFINITY_PF_PID" 2>/dev/null || true
        wait "$INFINITY_PF_PID" 2>/dev/null || true
    fi
}

# Register cleanup on exit
trap cleanup_port_forward EXIT

# Check if migration script exists
if [ ! -f "$MIGRATION_SCRIPT" ]; then
    echo -e "${RED}ERROR: Migration script not found: $MIGRATION_SCRIPT${NC}"
    exit 1
fi

# Parse arguments
AUTO_DISCOVER=false
DRY_RUN=false
VERBOSE=false
NAMESPACE=""
NO_PORT_FORWARD=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --auto-discover)
            AUTO_DISCOVER=true
            shift
            ;;
        --namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --verbose)
            VERBOSE=true
            shift
            ;;
        --no-port-forward)
            NO_PORT_FORWARD=true
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Usage: $0 [--auto-discover] [--namespace NAMESPACE] [--dry-run] [--verbose] [--no-port-forward]"
            exit 1
            ;;
    esac
done

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}ES to Infinity Migration${NC}"
echo -e "${GREEN}Shadow Proxy Mode${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# =============================================================================
# Auto-Discovery Mode
# =============================================================================

if [ "$AUTO_DISCOVER" = true ]; then
    echo -e "${BLUE}Using K8s Auto-Discovery Mode${NC}"
    echo ""
    
    # Set namespace
    : ${NAMESPACE:="${RAGFLOW_NAMESPACE:-ragflow}"}
    echo "Namespace: $NAMESPACE"
    
    # Check if running inside cluster
    if [ -f "/var/run/secrets/kubernetes.io/serviceaccount/token" ]; then
        echo -e "${GREEN}Running inside Kubernetes cluster${NC}"
        INSIDE_CLUSTER="true"
    else
        echo -e "${YELLOW}Running outside Kubernetes cluster${NC}"
        INSIDE_CLUSTER="false"
    fi
    
    echo "INSIDE_CLUSTER=$INSIDE_CLUSTER"
    echo "NO_PORT_FORWARD=$NO_PORT_FORWARD"
    
    # Debug: print condition check
    if [ "$INSIDE_CLUSTER" = "false" ]; then
        echo "DEBUG: INSIDE_CLUSTER equals 'false'"
    else
        echo "DEBUG: INSIDE_CLUSTER does NOT equal 'false' (value: '$INSIDE_CLUSTER')"
    fi
    
    if [ "$NO_PORT_FORWARD" != "true" ]; then
        echo "DEBUG: NO_PORT_FORWARD is not 'true' (value: '$NO_PORT_FORWARD')"
    else
        echo "DEBUG: NO_PORT_FORWARD equals 'true'"
    fi
    
    # =============================================================================
    # Setup Port-Forward (if outside cluster and not disabled)
    # =============================================================================
    
    if [ "$INSIDE_CLUSTER" = "false" ] && [ "$NO_PORT_FORWARD" != "true" ]; then
        echo "DEBUG: Entering port-forward setup branch"
        echo ""
        echo -e "${YELLOW}Setting up port-forwarding...${NC}"
        
        # Discover ES service name
        ES_SERVICE=""
        for svc in "elasticsearch-es-http" "elasticsearch" "es01" "elasticsearch-master" "ragflow-elasticsearch"; do
            if kubectl get svc "$svc" -n "$NAMESPACE" &>/dev/null; then
                ES_SERVICE="$svc"
                echo -e "  ${GREEN}✓${NC} Found ES service: $svc"
                break
            fi
        done
        
        if [ -z "$ES_SERVICE" ]; then
            echo -e "${RED}ERROR: Could not find Elasticsearch service in namespace $NAMESPACE${NC}"
            exit 1
        fi
        
        # Discover Infinity service name
        INFINITY_SERVICE=""
        for svc in "infinity" "ragflow-infinity" "infinity-server"; do
            if kubectl get svc "$svc" -n "$NAMESPACE" &>/dev/null; then
                INFINITY_SERVICE="$svc"
                echo -e "  ${GREEN}✓${NC} Found Infinity service: $svc"
                break
            fi
        done
        
        if [ -z "$INFINITY_SERVICE" ]; then
            echo -e "${RED}ERROR: Could not find Infinity service in namespace $NAMESPACE${NC}"
            exit 1
        fi
        
        # Get ES password from secret
        ES_PASSWORD=""
        for secret in "ragflow-env" "elastic-credentials" "elasticsearch-credentials"; do
            for key in "ELASTIC_PASSWORD" "ES_PASSWORD" "password"; do
                ES_PASSWORD=$(kubectl get secret "$secret" -n "$NAMESPACE" -o jsonpath="{.data.$key}" 2>/dev/null | base64 -d 2>/dev/null || true)
                if [ -n "$ES_PASSWORD" ]; then
                    echo -e "  ${GREEN}✓${NC} Found ES password in secret/$secret key=$key"
                    break 2
                fi
            done
        done
        
        if [ -z "$ES_PASSWORD" ]; then
            echo -e "${RED}ERROR: Could not find ES password in secrets${NC}"
            exit 1
        fi
        
        # Local ports for port-forward
        ES_LOCAL_PORT=19200
        INFINITY_LOCAL_PORT=23817
        
        echo ""
        echo -e "${YELLOW}Starting port-forward processes...${NC}"
        
        # Start ES port-forward with retry
        echo "  Starting ES port-forward: 127.0.0.1:$ES_LOCAL_PORT -> $ES_SERVICE:9200"
        nohup bash -c "
            while true; do
                kubectl port-forward svc/$ES_SERVICE -n $NAMESPACE ${ES_LOCAL_PORT}:9200 --address=127.0.0.1 2>/dev/null
                sleep 2
            done
        " > /tmp/es_portforward.log 2>&1 &
        ES_PF_PID=$!
        echo "  ES port-forward PID: $ES_PF_PID"
        
        # Start Infinity port-forward with retry
        echo "  Starting Infinity port-forward: 127.0.0.1:$INFINITY_LOCAL_PORT -> $INFINITY_SERVICE:23817"
        nohup bash -c "
            while true; do
                kubectl port-forward svc/$INFINITY_SERVICE -n $NAMESPACE ${INFINITY_LOCAL_PORT}:23817 --address=127.0.0.1 2>/dev/null
                sleep 2
            done
        " > /tmp/infinity_portforward.log 2>&1 &
        INFINITY_PF_PID=$!
        echo "  Infinity port-forward PID: $INFINITY_PF_PID"
        
        # Wait for port-forward to be ready
        echo ""
        echo -e "${YELLOW}Waiting for port-forward to be ready...${NC}"
        sleep 3
        
        # Test ES connection - determine protocol
        # ECK-managed ES services (elasticsearch-es-*) use HTTPS by default
        ES_CONNECTED=false
        ES_PROTOCOL=""
        
        if [[ "$ES_SERVICE" == *"elasticsearch-es"* ]]; then
            ES_PROTOCOL="https"
        fi
        
        # Retry loop for protocol detection
        for attempt in 1 2 3 4 5; do
            if [ -n "$ES_PROTOCOL" ]; then
                # Protocol determined from service name, just verify connectivity
                if curl -s -k -u "elastic:${ES_PASSWORD}" "${ES_PROTOCOL}://127.0.0.1:${ES_LOCAL_PORT}" &>/dev/null; then
                    ES_CONNECTED=true
                    echo -e "  ${GREEN}✓${NC} ES port-forward ready (${ES_PROTOCOL})"
                    break
                fi
            else
                # Auto-detect protocol by trying both
                for protocol in "https" "http"; do
                    if curl -s -k -u "elastic:${ES_PASSWORD}" "${protocol}://127.0.0.1:${ES_LOCAL_PORT}" &>/dev/null; then
                        ES_PROTOCOL="$protocol"
                        ES_CONNECTED=true
                        echo -e "  ${GREEN}✓${NC} ES port-forward ready (${protocol})"
                        break 2
                    fi
                done
            fi
            sleep 2
        done
        
        if [ "$ES_CONNECTED" = false ]; then
            echo -e "${RED}ERROR: ES port-forward not ready after waiting${NC}"
            echo "  Check logs: /tmp/es_portforward.log"
            exit 1
        fi
        
        # Test Infinity connection
        if python3 -c "
import infinity
from infinity.common import NetworkAddress
conn = infinity.connect(NetworkAddress('127.0.0.1', $INFINITY_LOCAL_PORT))
status = conn.show_current_node()
conn.disconnect()
exit(0 if status.error_code == 0 else 1)
" 2>/dev/null; then
            echo -e "  ${GREEN}✓${NC} Infinity port-forward ready"
        else
            echo -e "${RED}ERROR: Infinity port-forward not ready after waiting${NC}"
            echo "  Check logs: /tmp/infinity_portforward.log"
            exit 1
        fi
        
        echo ""
        echo -e "${GREEN}Port-forwarding established:${NC}"
        echo "  ES:      ${ES_PROTOCOL}://127.0.0.1:${ES_LOCAL_PORT}"
        echo "  Infinity: 127.0.0.1:${INFINITY_LOCAL_PORT}"
        echo ""
        
        # Build command for port-forward mode
        CMD="python3 $MIGRATION_SCRIPT"
        CMD="${CMD} --es-host ${ES_PROTOCOL}://127.0.0.1:${ES_LOCAL_PORT}"
        CMD="${CMD} --es-user elastic"
        CMD="${CMD} --es-password ${ES_PASSWORD}"
        CMD="${CMD} --infinity-uri 127.0.0.1:${INFINITY_LOCAL_PORT}"
        CMD="${CMD} --index-patterns ragflow_*"
        
    else
        # Inside cluster or no port-forward - use auto-discovery directly
        CMD="python3 $MIGRATION_SCRIPT --auto-discover --namespace $NAMESPACE"
    fi
    
    # Add common options
    if [ "$DRY_RUN" = true ]; then
        CMD="${CMD} --dry-run"
        echo -e "${YELLOW}Running in DRY-RUN mode (no data will be inserted)${NC}"
    fi
    
    if [ "$VERBOSE" = true ]; then
        CMD="${CMD} --verbose"
    fi
    
    echo ""
    echo -e "${YELLOW}Starting migration...${NC}"
    echo "Command: ${CMD}"
    echo ""
    
    # Run migration
    exec $CMD
fi

# =============================================================================
# Manual Configuration Mode
# =============================================================================

echo -e "${BLUE}Using Manual Configuration Mode${NC}"
echo ""

# Function to load variable with fallback
load_var() {
    local var_name=$1
    local default=$2
    local value
    
    # Try environment variable
    value=$(printenv "$var_name" 2>/dev/null || true)
    
    if [ -n "$value" ]; then
        echo "$value"
    elif [ -n "$default" ]; then
        echo "$default"
    else
        return 1
    fi
}

# Load configuration
echo -e "${YELLOW}Loading configuration...${NC}"

# ES Configuration
ES_HOST=$(load_var ES_HOST "es01")
ES_PORT=$(load_var ES_PORT "9200")
ELASTIC_PASSWORD=$(load_var ELASTIC_PASSWORD "infini_rag_flow")

# Infinity Configuration
INFINITY_URI=$(load_var INFINITY_URI "infinity:23817")

# Batch size
BATCH_SIZE=$(load_var BATCH_SIZE "2000")

echo "  ES Host: ${ES_HOST}:${ES_PORT}"
echo "  ES User: elastic"
echo "  Infinity URI: ${INFINITY_URI}"
echo "  Batch Size: ${BATCH_SIZE}"
echo ""

# Verify services are accessible
echo -e "${YELLOW}Verifying services...${NC}"

# Test ES connection
echo "Testing Elasticsearch connection..."
if curl -s -u "elastic:${ELASTIC_PASSWORD}" "http://${ES_HOST}:${ES_PORT}" > /dev/null 2>&1; then
    echo -e "  ${GREEN}✓${NC} Elasticsearch is accessible"
else
    echo -e "  ${RED}✗${NC} Cannot connect to Elasticsearch at http://${ES_HOST}:${ES_PORT}"
    echo "  Please check ES_HOST, ES_PORT, and ELASTIC_PASSWORD"
    exit 1
fi

# Test Infinity connection
echo "Testing Infinity connection..."
if python3 -c "
import infinity
from infinity.common import NetworkAddress
conn = infinity.connect(NetworkAddress('${INFINITY_URI%%:*}', ${INFINITY_URI##*:}))
status = conn.show_current_node()
conn.disconnect()
exit(0 if status.error_code == 0 else 1)
" 2>/dev/null; then
    echo -e "  ${GREEN}✓${NC} Infinity is accessible"
else
    echo -e "  ${RED}✗${NC} Cannot connect to Infinity at ${INFINITY_URI}"
    echo "  Please check INFINITY_URI"
    exit 1
fi

echo ""

# Build command
CMD="python3 $MIGRATION_SCRIPT"
CMD="${CMD} --es-host http://${ES_HOST}:${ES_PORT}"
CMD="${CMD} --es-user elastic"
CMD="${CMD} --es-password ${ELASTIC_PASSWORD}"
CMD="${CMD} --infinity-uri ${INFINITY_URI}"
CMD="${CMD} --batch-size ${BATCH_SIZE}"
CMD="${CMD} --index-patterns ragflow_*"

if [ "$DRY_RUN" = true ]; then
    CMD="${CMD} --dry-run"
    echo -e "${YELLOW}Running in DRY-RUN mode (no data will be inserted)${NC}"
fi

if [ "$VERBOSE" = true ]; then
    CMD="${CMD} --verbose"
fi

# Run migration
echo -e "${YELLOW}Starting migration...${NC}"
echo "Command: ${CMD}"
echo ""

if [ "$DRY_RUN" = true ]; then
    # Run in foreground for dry-run
    exec $CMD
else
    # Run in background
    LOG_FILE="migration_$(date +%Y%m%d_%H%M%S).log"
    nohup $CMD > "$LOG_FILE" 2>&1 &
    PID=$!
    
    echo $PID > migration.pid
    
    echo -e "${GREEN}Migration started in background${NC}"
    echo "  PID: $PID"
    echo "  Log: $LOG_FILE"
    echo ""
    echo "To monitor progress:"
    echo "  tail -f $LOG_FILE"
    echo ""
    echo "To check status:"
    echo "  ps aux | grep $PID"
    echo ""
    echo "To stop migration:"
    echo "  kill $PID"
    echo ""
    
    # Show initial log
    sleep 2
    echo -e "${YELLOW}Initial output:${NC}"
    tail -n 20 "$LOG_FILE"
fi
