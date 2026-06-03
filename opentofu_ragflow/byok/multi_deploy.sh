#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/deploy_common.sh"

usage() {
  cat <<'USAGE'
Usage:
  ./multi_deploy.sh -i <image> [--plan-only]

Required:
  -i, --image <image>   Ragflow image tag (example: ragflow:latest)

Optional:
      --plan-only        Run only 'tofu plan' (skip apply)
  -h, --help             Show this help
USAGE
}

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "Missing required tool: $1"
}

namespace_has_ragflow_env() {
  local ns="$1"
  local secret_json
  secret_json="$(kubectl get secret ragflow-env -n "$ns" -o json --ignore-not-found)"
  [[ -n "$secret_json" ]]
}

secret_redis_db() {
  local ns="$1"
  kubectl get secret ragflow-env -n "$ns" -o json --ignore-not-found \
    | jq -r '((.data.REDIS_DB // "") | @base64d | gsub("^\\s+|\\s+$"; ""))'
}

RAGFLOW_IMAGE=""
PLAN_ONLY=false
VAR_FILE="terraform.tfvars.dev_smk"
# shellcheck disable=SC2034
USED_DBS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    -i|--image)
      [[ $# -ge 2 ]] || fail "Missing value for $1"
      RAGFLOW_IMAGE="$2"
      shift 2
      ;;
    --plan-only)
      PLAN_ONLY=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "Unknown argument: $1 (use --help)"
      ;;
  esac
done

[[ -n "$RAGFLOW_IMAGE" ]] || fail "--image is required"
[[ -f "$VAR_FILE" ]] || fail "Var file not found: $VAR_FILE"

require_cmd kubectl
require_cmd jq
require_cmd tofu

REDIS_DB=""
NAMESPACE=""
byok_resolve_namespace_and_redis_db "$SCRIPT_DIR" || fail "No free Redis DB/namespace slot available in range 0..15 (excluding 1)."

if [[ "$NAMESPACE" =~ ^ragflow-([0-9]+)$ ]]; then
  ns_suffix_db="${BASH_REMATCH[1]}"
  if [[ "$REDIS_DB" != "$ns_suffix_db" ]]; then
    fail "Namespace/redis mismatch: namespace '$NAMESPACE' implies redis_db=$ns_suffix_db but resolved redis_db=$REDIS_DB."
  fi
fi

echo "Deployment parameters:"
echo "  namespace : $NAMESPACE"
echo "  image     : $RAGFLOW_IMAGE"
echo "  var-file  : $VAR_FILE"
echo "  redis_db  : $REDIS_DB"
echo "  plan_only : $PLAN_ONLY"

TF_ARGS=(
  -var-file="$VAR_FILE"
  -var "namespace=$NAMESPACE"
  -var "redis_db=$REDIS_DB"
  -var "ragflow_image=$RAGFLOW_IMAGE"
  -var "ragflow_image_platform=$RAGFLOW_IMAGE"
  -var "ragflow_image_admin=$RAGFLOW_IMAGE"
)

echo "Running: tofu plan ..."
tofu plan "${TF_ARGS[@]}"

if [[ "$PLAN_ONLY" == "true" ]]; then
  echo "Plan-only mode enabled; skipping apply."
  echo "RAGFLOW_NAMESPACE=$NAMESPACE"
  exit 0
fi

if ! byok_prompt_yes_no_default_no "Apply this plan? [y/N]: "; then
  echo "Skipping tofu apply."
  echo "RAGFLOW_NAMESPACE=$NAMESPACE"
  exit 0
fi

echo "Running: tofu apply -auto-approve ..."
tofu apply -auto-approve "${TF_ARGS[@]}"

echo "Deployment complete."
echo "RAGFLOW_NAMESPACE=$NAMESPACE"
