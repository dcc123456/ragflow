#!/usr/bin/env bash
set -euo pipefail

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

is_db_used() {
  local needle="$1"
  for db in "${USED_DBS[@]}"; do
    [[ "$db" == "$needle" ]] && return 0
  done
  return 1
}

state_file_path() {
  local ws="default"
  if [[ -f ".terraform/environment" ]]; then
    ws="$(tr -d '[:space:]' < .terraform/environment)"
    [[ -n "$ws" ]] || ws="default"
  fi

  if [[ "$ws" == "default" ]]; then
    echo "terraform.tfstate"
  else
    echo "terraform.tfstate.d/${ws}/terraform.tfstate"
  fi
}

state_namespace_from_local_state() {
  local state_fp="$1"
  [[ -f "$state_fp" ]] || return 1

  jq -r '
    .resources[]?
    | select(.type == "kubernetes_namespace_v1" and .name == "ragflow")
    | .instances[]?
    | (.attributes.metadata[0].name // empty)
  ' "$state_fp" | head -n 1
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

STATE_FILE="$(state_file_path)"
STATE_NAMESPACE="$(state_namespace_from_local_state "$STATE_FILE" || true)"

REDIS_DB=""
NAMESPACE=""
REUSE_STATE_NAMESPACE=false

if [[ -n "$STATE_NAMESPACE" ]]; then
  NAMESPACE="$STATE_NAMESPACE"

  if namespace_has_ragflow_env "$NAMESPACE"; then
    REDIS_DB="$(secret_redis_db "$NAMESPACE")"
  fi

  if ! [[ "$REDIS_DB" =~ ^[0-9]+$ ]]; then
    if [[ "$NAMESPACE" =~ ^ragflow-([0-9]+)$ ]]; then
      REDIS_DB="${BASH_REMATCH[1]}"
    fi
  fi

  if [[ "$REDIS_DB" =~ ^[0-9]+$ ]] && [[ "$NAMESPACE" =~ ^ragflow-[0-9]+$ ]]; then
    REUSE_STATE_NAMESPACE=true
    echo "Detected existing managed namespace in state: $NAMESPACE (redis_db=$REDIS_DB)"
  else
    # Ignore non-app state namespaces (e.g. ragflow-infra) and do fresh-slot allocation.
    REDIS_DB=""
    NAMESPACE=""
  fi
fi

if [[ "$REUSE_STATE_NAMESPACE" == "false" ]]; then
  mapfile -t USED_DBS < <(
    kubectl get secrets -A -o json \
      | jq -r '
        .items[]
        | select(.metadata.name == "ragflow-env")
        | ((.data.REDIS_DB // "") | @base64d | gsub("^\\s+|\\s+$"; ""))
        | select(test("^[0-9]+$"))
      ' \
      | sort -nu
  )

  for db in $(seq 0 15); do
    [[ "$db" == "1" ]] && continue
    candidate_ns="ragflow-${db}"

    if is_db_used "$db"; then
      continue
    fi

    if namespace_has_ragflow_env "$candidate_ns"; then
      continue
    fi

    REDIS_DB="$db"
    NAMESPACE="$candidate_ns"
    break
  done

  [[ -n "$REDIS_DB" ]] || fail "No free Redis DB/namespace slot available in range 0..15 (excluding 1)."
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
