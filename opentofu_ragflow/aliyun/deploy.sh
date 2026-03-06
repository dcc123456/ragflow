#!/bin/bash
# =============================================================================
# RAGFlow Aliyun Two-Stage Deployment Script
# =============================================================================
# This script orchestrates the complete deployment of RAGFlow on Aliyun:
#   Stage 1: Cloud infrastructure (VPC, OSS, MySQL/ES, K8s cluster)
#   Stage 2: Kubernetes resources (Deployments, Services, Ingress)
#
# Prerequisites:
#   - OpenTofu/Terraform >= 1.5.7 installed
#   - Aliyun credentials configured (ALICLOUD_ACCESS_KEY, ALICLOUD_SECRET_KEY)
#   - kubectl installed
#
# Usage:
#   ./deploy.sh [options]
#
# Options:
#   -s, --stage STAGE    Deploy specific stage only (1 or 2)
#   -d, --destroy        Destroy all resources instead of deploying
#   -p, --plan           Run terraform plan without applying
#   -n, --no-color       Disable colored output
#   -h, --help           Show this help message
# =============================================================================

set -e  # Exit on error

# =============================================================================
# Script Configuration
# =============================================================================
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STAGE1_DIR="${SCRIPT_DIR}/stage1-infrastructure"
STAGE2_DIR="${SCRIPT_DIR}/stage2-kubernetes"
KUBECONFIG_SRC="${STAGE1_DIR}/kubeconfig"
KUBECONFIG_DST="${SCRIPT_DIR}/kubeconfig"

# Color codes (can be disabled with -n)
if [[ -t 1 ]] && [[ "${NO_COLOR:-}" != "true" ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

# =============================================================================
# Helper Functions
# =============================================================================

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_separator() {
    echo ""
    echo "============================================================================================================"
    echo "$1"
    echo "============================================================================================================"
    echo ""
}

show_help() {
    sed -n '/^# Usage:/,/^#=============/p' "$0" | sed 's/^# //g' | sed 's/^#//g' | head -n -1
    exit 0
}

check_prerequisites() {
    log_info "Checking prerequisites..."

    # Check for OpenTofu or Terraform
    if command -v tofu &> /dev/null; then
        TF_CMD="tofu"
        log_info "Using OpenTofu: $(tofu version -json | jq -r '.terraform_version' 2>/dev/null || tofu version | head -n1)"
    elif command -v terraform &> /dev/null; then
        TF_CMD="terraform"
        log_info "Using Terraform: $(terraform version -json | jq -r '.terraform_version' 2>/dev/null || terraform version | head -n1)"
    else
        log_error "Neither OpenTofu nor Terraform found. Please install OpenTofu (recommended) or Terraform >= 1.5.7"
        exit 1
    fi

    # Check for kubectl
    if ! command -v kubectl &> /dev/null; then
        log_warning "kubectl not found. Required for Stage 2 deployment."
        if [[ "${DEPLOY_STAGE:-all}" == "2" ]] || [[ "${DEPLOY_STAGE:-all}" == "all" ]]; then
            log_error "Please install kubectl to proceed with Stage 2 deployment"
            exit 1
        fi
    fi

    # Check for Aliyun credentials
    if [[ -z "${ALICLOUD_ACCESS_KEY:-}" ]] || [[ -z "${ALICLOUD_SECRET_KEY:-}" ]]; then
        log_warning "ALICLOUD_ACCESS_KEY or ALICLOUD_SECRET_KEY not set in environment"
        log_info "Make sure credentials are configured in ~/.aliyun/config or via environment variables"
    fi

    log_success "Prerequisites check passed"
}

# =============================================================================
# Stage 1: Cloud Infrastructure
# =============================================================================

deploy_stage1() {
    print_separator "Stage 1: Deploying Cloud Infrastructure"

    cd "${STAGE1_DIR}"

    # Initialize Terraform only if not already initialized
    if [[ ! -d ".terraform" ]] || [[ ! -f ".terraform.lock.hcl" ]]; then
        log_info "Initializing Terraform (Stage 1)..."
        "$TF_CMD" init -upgrade=true -input=false -backend=false
    fi

    # Run plan if requested
    if [[ "${PLAN_ONLY:-false}" == "true" ]]; then
        log_info "Running Terraform plan (Stage 1)..."
        "$TF_CMD" plan -out=tfplan
        log_info "Plan saved to tfplan. Run '$TF_CMD apply tfplan' to apply"
        exit 0
    fi

    # Apply Terraform configuration
    log_info "Applying Terraform configuration (Stage 1)..."
    "$TF_CMD" apply -auto-approve

    # Export kubeconfig for Stage 2
    log_info "Exporting kubeconfig for Stage 2..."
    "$TF_CMD" output -raw kubeconfig > "${KUBECONFIG_SRC}"
    cp "${KUBECONFIG_SRC}" "${KUBECONFIG_DST}"

    # Export outputs for Stage 2 (via terraform_remote_state)
    log_info "Exporting state for Stage 2..."
    "$TF_CMD" output -json > "${SCRIPT_DIR}/stage1-outputs.json"

    cd "${SCRIPT_DIR}"
    log_success "Stage 1 deployment completed successfully!"
}

destroy_stage1() {
    print_separator "Stage 1: Destroying Cloud Infrastructure"

    cd "${STAGE1_DIR}"

    log_warning "This will destroy all cloud resources including VPC, OSS, MySQL, ES, and Kubernetes cluster"
    read -p "Are you sure? (yes/no): " confirm
    if [[ "${confirm}" != "yes" ]]; then
        log_info "Destruction cancelled"
        exit 0
    fi

    log_info "Destroying Terraform resources (Stage 1)..."
    "$TF_CMD" destroy -auto-approve

    # Clean up generated files
    rm -f "${KUBECONFIG_SRC}" "${KUBECONFIG_DST}" "${SCRIPT_DIR}/stage1-outputs.json"

    cd "${SCRIPT_DIR}"
    log_success "Stage 1 destruction completed!"
}

# =============================================================================
# Stage 2: Kubernetes Resources
# =============================================================================

deploy_stage2() {
    print_separator "Stage 2: Deploying Kubernetes Resources"

    # Check if kubeconfig exists
    if [[ ! -f "${KUBECONFIG_DST}" ]]; then
        log_error "Kubeconfig not found at ${KUBECONFIG_DST}"
        log_error "Please run Stage 1 deployment first or export kubeconfig manually"
        exit 1
    fi

    cd "${STAGE2_DIR}"

    # Initialize Terraform only if not already initialized
    if [[ ! -d ".terraform" ]] || [[ ! -f ".terraform.lock.hcl" ]]; then
        log_info "Initializing Terraform (Stage 2)..."
        "$TF_CMD" init -upgrade=true -input=false -backend=false
    fi

    # Verify cluster connectivity
    log_info "Verifying Kubernetes cluster connectivity..."
    export KUBECONFIG="${KUBECONFIG_DST}"
    if ! kubectl get nodes &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        log_error "Please check kubeconfig at ${KUBECONFIG_DST}"
        exit 1
    fi
    log_success "Kubernetes cluster is accessible"

    # Run plan if requested
    if [[ "${PLAN_ONLY:-false}" == "true" ]]; then
        log_info "Running Terraform plan (Stage 2)..."
        "$TF_CMD" plan -out=tfplan
        log_info "Plan saved to tfplan. Run '$TF_CMD apply tfplan' to apply"
        exit 0
    fi

    # Apply Terraform configuration
    log_info "Applying Terraform configuration (Stage 2)..."
    "$TF_CMD" apply -auto-approve

    # Export outputs
    log_info "Exporting Stage 2 outputs..."
    "$TF_CMD" output -json > "${SCRIPT_DIR}/stage2-outputs.json"

    # Display gateway address
    GATEWAY_ADDRESS="$("$TF_CMD" output -raw gateway_address)"
    log_success "Gateway Address: ${GATEWAY_ADDRESS}"

    cd "${SCRIPT_DIR}"
    log_success "Stage 2 deployment completed successfully!"
}

destroy_stage2() {
    print_separator "Stage 2: Destroying Kubernetes Resources"

    if [[ ! -f "${KUBECONFIG_DST}" ]]; then
        log_error "Kubeconfig not found at ${KUBECONFIG_DST}"
        exit 1
    fi

    cd "${STAGE2_DIR}"

    export KUBECONFIG="${KUBECONFIG_DST}"

    log_warning "This will destroy all Kubernetes resources"
    read -p "Are you sure? (yes/no): " confirm
    if [[ "${confirm}" != "yes" ]]; then
        log_info "Destruction cancelled"
        exit 0
    fi

    log_info "Destroying Terraform resources (Stage 2)..."
    "$TF_CMD" destroy -auto-approve

    # Clean up outputs file
    rm -f "${SCRIPT_DIR}/stage2-outputs.json"

    cd "${SCRIPT_DIR}"
    log_success "Stage 2 destruction completed!"
}

# =============================================================================
# Final Summary
# =============================================================================

show_final_summary() {
    print_separator "Deployment Summary"

    # Only show gateway address from Stage 2
    if [[ -f "${SCRIPT_DIR}/stage2-outputs.json" ]]; then
        if command -v jq &> /dev/null; then
            GATEWAY_ADDRESS=$(jq -r '.gateway_address.value // "N/A"' "${SCRIPT_DIR}/stage2-outputs.json" 2>/dev/null)
            echo "Gateway Address: ${GATEWAY_ADDRESS}"
        else
            echo "Gateway Address: $(cat "${SCRIPT_DIR}/stage2-outputs.json" 2>/dev/null | grep -o '"gateway_address"[^}]*' | head -1 || echo "N/A")"
        fi
    else
        echo "Gateway Address: N/A (Stage 2 not deployed yet)"
    fi
    echo ""

    echo "============================================================================================================"
    echo "Next Steps:"
    echo "============================================================================================================"
    echo "1. Configure DNS to point your gateway hostname to the gateway address"
    echo "2. Access RAGFlow at: http://<your-gateway-hostname>"
    echo "3. For kubectl access: export KUBECONFIG=${KUBECONFIG_DST}"
    echo "4. View pods: kubectl get pods -n ragflow"
    echo "5. View logs: kubectl logs -f deployment/ragflow -n ragflow"
    echo ""
}

# =============================================================================
# Main Script
# =============================================================================

main() {
    print_separator "RAGFlow Aliyun Deployment Script"

    # Parse command line arguments
    DEPLOY_STAGE="all"
    DESTROY_MODE=false
    PLAN_ONLY=false

    while [[ $# -gt 0 ]]; do
        case $1 in
            -s|--stage)
                DEPLOY_STAGE="$2"
                shift 2
                ;;
            -d|--destroy)
                DESTROY_MODE=true
                shift
                ;;
            -p|--plan)
                PLAN_ONLY=true
                shift
                ;;
            -n|--no-color)
                NO_COLOR=true
                shift
                ;;
            -h|--help)
                show_help
                ;;
            *)
                log_error "Unknown option: $1"
                show_help
                ;;
        esac
    done

    # Run prerequisites check
    check_prerequisites

    # Execute deployment or destruction
    if [[ "${DESTROY_MODE}" == "true" ]]; then
        if [[ "${DEPLOY_STAGE}" == "2" ]] || [[ "${DEPLOY_STAGE}" == "all" ]]; then
            destroy_stage2
        fi
        if [[ "${DEPLOY_STAGE}" == "1" ]] || [[ "${DEPLOY_STAGE}" == "all" ]]; then
            destroy_stage1
        fi
    else
        if [[ "${DEPLOY_STAGE}" == "1" ]] || [[ "${DEPLOY_STAGE}" == "all" ]]; then
            deploy_stage1
        fi

        if [[ "${DEPLOY_STAGE}" == "2" ]] || [[ "${DEPLOY_STAGE}" == "all" ]]; then
            deploy_stage2
        fi

        show_final_summary
    fi

    log_success "Script completed successfully!"
}

# Run main function
main "$@"
