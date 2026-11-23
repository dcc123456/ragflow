#!/bin/sh
set -eu
umask 077

script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
runtime="$(mktemp -d "${TMPDIR:-/tmp}/ragflow-prometheus.XXXXXX")"
cleanup() { rm -rf "$runtime"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

# Preserve relative paths without rewriting YAML; keep generated files separate.
for entry in "$script_dir"/*; do
    [ -e "$entry" ] || continue
    case "${entry##*/}" in prometheus.yml|targets|expected_inventory.yml|minio.token) continue ;; esac
    ln -s "$entry" "$runtime/${entry##*/}"
done
cp "$script_dir/prometheus.yml" "$runtime/prometheus.yml"
mkdir "$runtime/targets"
printf 'groups:\n- name: monitoring_expected_inventory\n  rules:\n' > "$runtime/expected_inventory.yml"

inventory() {
    job="$1" service="$2" selected="$3" configured="$4" address="$5" target_valid="$6"
    if [ "$selected" = 1 ] && [ "$target_valid" = 1 ]; then
        printf '[{"targets":["%s"],"labels":{"service":"%s"}}]\n' "$address" "$service" > "$runtime/targets/$job.json"
    else
        printf '[]\n' > "$runtime/targets/$job.json"
    fi
    cat >> "$runtime/expected_inventory.yml" <<EOF
  - record: ragflow_monitoring_target_enabled
    expr: vector($selected)
    labels:
      job: $job
      service: $service
  - record: ragflow_monitoring_target_configured
    expr: vector($configured)
    labels:
      job: $job
      service: $service
EOF
}

valid_port() {
    case "$1" in ''|*[!0-9]*|0*) return 1 ;; esac
    [ "${#1}" -le 5 ] && [ "$1" -ge "$2" ] && [ "$1" -le 65535 ]
}

# Direct endpoints accept host:port or [IPv6]:port, with no YAML/JSON metacharacters.
native_target() {
    job="$1" service="$2" selection="$3" address="$4" credentials_ready="$5"
    selected=1 valid=0
    case "$selection" in
        false) selected=0 ;;
        true)
            if printf '%s' "$address" | LC_ALL=C grep -Eq '^(\[[0-9a-fA-F:]+\]|[a-zA-Z0-9_.-]+):[0-9]+$' && valid_port "${address##*:}" 1; then
                valid=1
            else
                echo "$service monitoring configuration invalid: expected host:port" >&2
            fi
            ;;
        *) echo "$service monitoring configuration invalid: enabled must be true or false" >&2 ;;
    esac
    configured=0
    if [ "$selected" = 1 ] && [ "$valid" = 1 ] && [ "$credentials_ready" = 1 ]; then configured=1; fi
    inventory "$job" "$service" "$selected" "$configured" "$address" "$valid"
}

exporter_target() {
    if [ -z "$3" ]; then
        inventory "$1" "$2" 0 0 '' 0
    elif valid_port "$4" 1024; then
        inventory "$1" "$2" 1 1 "$3:$4" 1
    else
        echo "$2 exporter configuration invalid: listener port must be 1024..65535" >&2
        inventory "$1" "$2" 1 0 '' 0
    fi
}

# An empty private credentials file keeps missing tokens local to the MinIO job.
minio_enabled="${MONITORING_MINIO_ENABLED:-true}"
minio_address="${MINIO_METRICS_ADDRESS:-minio:9000}"
minio_token="$script_dir/secrets/minio.token"
if [ "$minio_address" = minio:9000 ]; then
    minio_token=/monitoring/credentials/minio.token
fi
minio_credentials_ready=0
if [ "$minio_enabled" = true ] && [ -s "$minio_token" ] && cp "$minio_token" "$runtime/minio.token"; then
    minio_credentials_ready=1
else
    : > "$runtime/minio.token"
fi
native_target minio-job minio "$minio_enabled" "$minio_address" "$minio_credentials_ready"
native_target clickhouse clickhouse "${MONITORING_CLICKHOUSE_ENABLED:-true}" "${CLICKHOUSE_METRICS_ADDRESS:-clickhouse:9363}" 1

mysql_exporter=''
if [ "${DB_TYPE:-mysql}" = mysql ]; then mysql_exporter=mysql-exporter; fi
doc_engine="${DOC_ENGINE:-elasticsearch}"
search_exporter=''
case "$doc_engine" in
    elasticsearch|opensearch) search_exporter="$doc_engine-exporter" ;;
esac
exporter_target mysql mysql "$mysql_exporter" "${MYSQL_EXPORTER_PORT:-9104}"
exporter_target kvrocks kvrocks kvrocks-exporter "${KVROCKS_EXPORTER_PORT:-9121}"
exporter_target nats nats nats-exporter "${NATS_EXPORTER_PORT:-7777}"
exporter_target elasticsearch elasticsearch "$search_exporter" "${ES_EXPORTER_PORT:-9114}"

case "${1:-}" in
    --generate-config)
        trap - EXIT HUP INT TERM
        printf '%s\n' "$runtime"
        ;;
    --check-config) promtool check config "$runtime/prometheus.yml" ;;
    *) exec prometheus --config.file="$runtime/prometheus.yml" "$@" ;;
esac
