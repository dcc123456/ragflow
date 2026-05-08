#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  ./deploy.sh -i <image> [--plan-only]

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

is_db_used() {
  local needle="$1"
  for db in "${USED_DBS[@]}"; do
    [[ "$db" == "$needle" ]] && return 0
  done
  return 1
}

RAGFLOW_IMAGE=""
PLAN_ONLY=false
VAR_FILE="terraform.tfvars.dev_smk"

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

mapfile -t USED_DBS < <(
  kubectl get secrets -A -o json \
    | jq -r '
      .items[]
      | select(.metadata.name == "ragflow-env")
      | ((.data.REDIS_DB // "") | @base64d)
      | select(test("^[0-9]+$"))
    ' \
    | sort -nu
)

REDIS_DB=""
for db in $(seq 0 15); do
  [[ "$db" == "1" ]] && continue
  if ! is_db_used "$db"; then
    REDIS_DB="$db"
    break
  fi
done

[[ -n "$REDIS_DB" ]] || fail "No free Redis DB available in range 0..15 (excluding 1)."

NAMESPACE="ragflow-${REDIS_DB}"

ns_secret_json="$(kubectl get secret ragflow-env -n "$NAMESPACE" -o json --ignore-not-found)"
if [[ -n "$ns_secret_json" ]]; then
  ns_redis_db="$(jq -r '(.data.REDIS_DB // "") | @base64d' <<<"$ns_secret_json")"
  if [[ "$ns_redis_db" =~ ^[0-9]+$ ]]; then
    fail "Namespace '$NAMESPACE' already has ragflow-env with REDIS_DB=$ns_redis_db. Refusing deployment conflict."
  fi
  fail "Namespace '$NAMESPACE' already has ragflow-env secret. Refusing deployment conflict."
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
)

echo "Running: tofu plan ..."
tofu plan "${TF_ARGS[@]}"

if [[ "$PLAN_ONLY" == "true" ]]; then
  echo "Plan-only mode enabled; skipping apply."
  echo "RAGFLOW_NAMESPACE=$NAMESPACE"
  exit 0
fi

echo "Running: tofu apply -auto-approve ..."
tofu apply -auto-approve "${TF_ARGS[@]}"

echo "Deployment complete."
echo "RAGFLOW_NAMESPACE=$NAMESPACE"
