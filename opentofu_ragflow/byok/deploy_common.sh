#!/usr/bin/env bash

byok_state_file_path() {
  local byok_dir="$1"
  local workspace="default"

  if [[ -f "$byok_dir/.terraform/environment" ]]; then
    workspace="$(tr -d '[:space:]' < "$byok_dir/.terraform/environment")"
    [[ -n "$workspace" ]] || workspace="default"
  fi

  if [[ "$workspace" == "default" ]]; then
    printf '%s\n' "$byok_dir/terraform.tfstate"
  else
    printf '%s\n' "$byok_dir/terraform.tfstate.d/${workspace}/terraform.tfstate"
  fi
}

byok_state_namespace_from_local_state() {
  local state_file="$1"
  [[ -f "$state_file" ]] || return 1

  jq -r '
    .resources[]?
    | select(.type == "kubernetes_namespace_v1" and .name == "ragflow")
    | .instances[]?
    | (.attributes.metadata[0].name // empty)
  ' "$state_file" | head -n 1
}

byok_redis_db_from_namespace() {
  local namespace="$1"

  if [[ "$namespace" =~ ^ragflow-([0-9]+)$ ]]; then
    printf '%s\n' "${BASH_REMATCH[1]}"
    return 0
  fi

  return 1
}

byok_is_valid_redis_db() {
  local db="$1"
  [[ "$db" =~ ^[0-9]+$ ]] || return 1
  (( db >= 0 && db <= 15 )) || return 1
  [[ "$db" != "1" ]]
}

byok_has_value_in_list() {
  local needle="$1"
  shift || true

  local value
  for value in "$@"; do
    [[ "$value" == "$needle" ]] && return 0
  done
  return 1
}

byok_secret_namespace_exists_in_json() {
  local secrets_json_file="$1"
  local namespace="$2"

  jq -e --arg namespace "$namespace" '
    .items[]?
    | select(.metadata.namespace == $namespace and .metadata.name == "ragflow-env")
  ' "$secrets_json_file" >/dev/null
}

byok_secret_redis_db_from_json() {
  local secrets_json_file="$1"
  local namespace="$2"

  jq -r --arg namespace "$namespace" '
    .items[]?
    | select(.metadata.namespace == $namespace and .metadata.name == "ragflow-env")
    | ((.data.REDIS_DB // "") | @base64d | gsub("^\\s+|\\s+$"; ""))
  ' "$secrets_json_file" | head -n 1
}

byok_used_redis_dbs_from_json() {
  local secrets_json_file="$1"

  jq -r '
    .items[]?
    | select(.metadata.name == "ragflow-env")
    | ((.data.REDIS_DB // "") | @base64d | gsub("^\\s+|\\s+$"; ""))
    | select(test("^[0-9]+$"))
  ' "$secrets_json_file" | sort -nu
}

byok_prompt_yes_no_default_yes() {
  local prompt="$1"
  local reply=""

  while true; do
    read -r -p "$prompt" reply
    case "${reply,,}" in
      ""|y|yes)
        return 0
        ;;
      n|no)
        return 1
        ;;
      *)
        echo "Please answer y, yes, n, or no."
        ;;
    esac
  done
}

byok_prompt_yes_no_default_no() {
  local prompt="$1"
  local reply=""

  while true; do
    read -r -p "$prompt" reply
    case "${reply,,}" in
      y|yes)
        return 0
        ;;
      ""|n|no)
        return 1
        ;;
      *)
        echo "Please answer y, yes, n, or no."
        ;;
    esac
  done
}

byok_select_redis_db() {
  local secrets_json_file="$1"
  local default_db="$2"
  local candidate_ns=""
  local choice=""
  local status=""
  local db=""

  echo "Redis DB status:"
  for db in $(seq 0 15); do
    if [[ "$db" == "1" ]]; then
      echo "  $db: reserved"
      continue
    fi

    candidate_ns="ragflow-${db}"
    status="free"

    if byok_has_value_in_list "$db" "${USED_DBS[@]}" || byok_secret_namespace_exists_in_json "$secrets_json_file" "$candidate_ns"; then
      status="used"
    fi

    if [[ "$db" == "$default_db" && "$status" == "free" ]]; then
      status="free (default)"
    fi

    echo "  $db: $status"
  done

  while true; do
    read -r -p "Select Redis DB [$default_db]: " choice
    choice="${choice:-$default_db}"

    if ! [[ "$choice" =~ ^[0-9]+$ ]]; then
      echo "Please enter a number between 0 and 15."
      continue
    fi

    if [[ "$choice" == "1" ]]; then
      echo "Redis DB 1 is reserved."
      continue
    fi

    if (( choice < 0 || choice > 15 )); then
      echo "Redis DB must be in the range 0..15."
      continue
    fi

    candidate_ns="ragflow-${choice}"
    if byok_has_value_in_list "$choice" "${USED_DBS[@]}" || byok_secret_namespace_exists_in_json "$secrets_json_file" "$candidate_ns"; then
      echo "Redis DB $choice is already in use."
      continue
    fi

    REDIS_DB="$choice"
    NAMESPACE="$candidate_ns"
    return 0
  done
}

byok_resolve_namespace_and_redis_db() {
  local byok_dir="$1"
  local state_namespace=""
  local state_db=""
  local default_db=""
  local candidate_ns=""
  local db=""
  local secrets_json_file=""

  REDIS_DB=""
  NAMESPACE=""

  state_namespace="$(byok_state_namespace_from_local_state "$(byok_state_file_path "$byok_dir")" || true)"
  if [[ -n "$state_namespace" ]]; then
    state_db="$(byok_redis_db_from_namespace "$state_namespace" || true)"
    if byok_is_valid_redis_db "${state_db:-}" && [[ "$state_namespace" == "ragflow-$state_db" ]]; then
      echo "Detected reusable state slot: namespace=$state_namespace redis_db=$state_db"
      if byok_prompt_yes_no_default_yes "Reuse this existing state slot? [Y/n]: "; then
        # shellcheck disable=SC2034
        REDIS_DB="$state_db"
        # shellcheck disable=SC2034
        NAMESPACE="$state_namespace"
        return 0
      fi
    fi
  fi

  secrets_json_file="$(mktemp)"
  kubectl get secrets -A -o json > "$secrets_json_file"

  mapfile -t USED_DBS < <(byok_used_redis_dbs_from_json "$secrets_json_file")

  default_db=""
  for db in $(seq 0 15); do
    [[ "$db" == "1" ]] && continue
    candidate_ns="ragflow-${db}"
    if byok_has_value_in_list "$db" "${USED_DBS[@]}" || byok_secret_namespace_exists_in_json "$secrets_json_file" "$candidate_ns"; then
      continue
    fi
    default_db="$db"
    break
  done

  if [[ -z "$default_db" ]]; then
    rm -f "$secrets_json_file"
    return 1
  fi

  byok_select_redis_db "$secrets_json_file" "$default_db"
  rm -f "$secrets_json_file"
}
