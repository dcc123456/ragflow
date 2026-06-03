#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  ./k8s_full.sh -i <tag>

Required:
  -i, --image <tag>   Short image tag to build and deploy (example: dev-test-1)

Optional:
  -h, --help          Show this help
USAGE
}

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

warn() {
  echo "WARN: $*" >&2
}

print_summary() {
  echo "RAGFLOW_IMAGE=$RAGFLOW_IMAGE"
  echo "RAGFLOW_NAMESPACE=$NAMESPACE"
  echo "REDIS_DB=$REDIS_DB"
  echo "BYOK_DIR=$BYOK_DIR_REL"
}

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$SCRIPT_DIR"
BYOK_DIR_REL="opentofu_ragflow/byok"
BYOK_DIR="$REPO_ROOT/$BYOK_DIR_REL"
BYOK_HELPER="$BYOK_DIR/deploy_common.sh"
VAR_FILE="terraform.tfvars.dev_smk"
FULL_IMAGE=""
RAGFLOW_IMAGE=""
REDIS_DB=""
NAMESPACE=""
# shellcheck disable=SC2034
USED_DBS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    -i|--image)
      [[ $# -ge 2 ]] || fail "Missing value for $1"
      IMAGE_TAG="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      fail "Unknown argument: $1"
      ;;
  esac
done

[[ -n "${IMAGE_TAG:-}" ]] || {
  usage >&2
  fail "--image is required"
}

[[ "$PWD" == "$REPO_ROOT" ]] || fail "Run this script from the repo root: $REPO_ROOT"
[[ "$IMAGE_TAG" =~ ^[a-z0-9][a-z0-9._-]{0,127}$ ]] || fail "Image tag must match ^[a-z0-9][a-z0-9._-]{0,127}$"
[[ ! "$IMAGE_TAG" =~ [[:space:]/A-Z] ]] || fail "Image tag must not contain whitespace, slash, or uppercase letters"
[[ ! "$IMAGE_TAG" =~ [._-]$ ]] || fail "Image tag must not end with '.', '_', or '-'"

[[ -f "$REPO_ROOT/Dockerfile" ]] || fail "Missing required file: Dockerfile"
[[ -d "$BYOK_DIR" ]] || fail "Missing required directory: $BYOK_DIR_REL"
[[ -f "$BYOK_DIR/$VAR_FILE" ]] || fail "Missing required file: $BYOK_DIR_REL/$VAR_FILE"
[[ -f "$BYOK_DIR/multi_deploy.sh" ]] || fail "Missing required file: $BYOK_DIR_REL/multi_deploy.sh"
[[ -f "$BYOK_DIR/README.md" ]] || fail "Missing required file: $BYOK_DIR_REL/README.md"
[[ -f "$BYOK_HELPER" ]] || fail "Missing required file: $BYOK_DIR_REL/deploy_common.sh"

# shellcheck source=/dev/null
source "$BYOK_HELPER"

missing_tools=()
for tool in docker kubectl jq tofu; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    missing_tools+=("$tool")
  fi
done

if ((${#missing_tools[@]} > 0)); then
  for tool in "${missing_tools[@]}"; do
    warn "Missing required tool: $tool"
  done
  exit 1
fi

byok_resolve_namespace_and_redis_db "$BYOK_DIR" || fail "No free Redis DB/namespace slot available in range 0..15 (excluding 1)."

[[ "$NAMESPACE" == "ragflow-$REDIS_DB" ]] || fail "Namespace/redis mismatch: expected ragflow-$REDIS_DB, got $NAMESPACE"

FULL_IMAGE="192.168.1.51/infiniflow-ai/ragflow:${IMAGE_TAG}"
RAGFLOW_IMAGE="ragflow:${IMAGE_TAG}"

echo "Building image: $FULL_IMAGE"
DOCKER_BUILDKIT=1 docker build --progress=plain --build-arg NEED_MIRROR=1 -f Dockerfile -t "$FULL_IMAGE" .

echo "Pushing image: $FULL_IMAGE"
docker push "$FULL_IMAGE"

echo "Deployment parameters:"
print_summary

cd "$BYOK_DIR"
TF_ARGS=(
    -var-file="$VAR_FILE"
    -var "namespace=$NAMESPACE"
    -var "redis_db=$REDIS_DB"
    -var "ragflow_image=$RAGFLOW_IMAGE"
    -var "ragflow_image_platform=$RAGFLOW_IMAGE"
    -var "ragflow_image_admin=$RAGFLOW_IMAGE"
    -var "load_balancer_provider=metallb"
  )

echo "Running: tofu plan ..."
tofu plan "${TF_ARGS[@]}"

if ! byok_prompt_yes_no_default_no "Apply this plan? [y/N]: "; then
  echo "Skipping tofu apply."
  print_summary
  exit 0
fi

echo "Running: tofu apply -auto-approve ..."
tofu apply -auto-approve "${TF_ARGS[@]}"

echo "Deployment complete."
print_summary
