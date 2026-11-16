#!/bin/sh
set -eu
umask 077

script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
. "$script_dir/../exporter/monitoring-config.sh"
monitoring_init
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

# Direct endpoints accept host:port or [IPv6]:port, with no YAML/JSON metacharacters.
native_target() {
    job="$1" service="$2" selection="$3" address="$4" credentials_ready="$5"
    selected=1 valid=0
    case "$selection" in
        false) selected=0 ;;
        true)
            if printf '%s' "$address" | LC_ALL=C grep -Eq '^(\[[0-9a-fA-F:]+\]|[a-zA-Z0-9_.-]+):[0-9]+$' && monitoring_valid_port "${address##*:}" 1; then
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

# An empty private credentials file keeps missing tokens local to the MinIO job.
minio_credentials_ready=0
if [ -s "$script_dir/secrets/minio.token" ] && cp "$script_dir/secrets/minio.token" "$runtime/minio.token"; then
    minio_credentials_ready=1
else
    : > "$runtime/minio.token"
fi
native_target minio-job minio "${MONITORING_MINIO_ENABLED:-true}" "${MINIO_METRICS_ADDRESS:-minio:9000}" "$minio_credentials_ready"
native_target clickhouse clickhouse "${MONITORING_CLICKHOUSE_ENABLED:-true}" "${CLICKHOUSE_METRICS_ADDRESS:-clickhouse:9363}" 1

configured=0; if [ "$MYSQL_EXPORTER_ENABLED" = true ]; then configured=1; fi
inventory mysql mysql "$MYSQL_EXPORTER_SELECTED" "$configured" "components-exporter:$MYSQL_EXPORTER_PORT" "$configured"
configured=0; if [ "$KVROCKS_EXPORTER_ENABLED" = true ]; then configured=1; fi
inventory kvrocks kvrocks 1 "$configured" "components-exporter:$KVROCKS_EXPORTER_PORT" "$configured"
configured=0; if [ "$NATS_EXPORTER_ENABLED" = true ]; then configured=1; fi
inventory nats nats 1 "$configured" "components-exporter:$NATS_EXPORTER_PORT" "$configured"
configured=0; if [ "$ES_EXPORTER_ENABLED" = true ]; then configured=1; fi
inventory elasticsearch elasticsearch "$ES_EXPORTER_SELECTED" "$configured" "components-exporter:$ES_EXPORTER_PORT" "$configured"

case "${1:-}" in
    --generate-config)
        trap - EXIT HUP INT TERM
        printf '%s\n' "$runtime"
        ;;
    --check-config) promtool check config "$runtime/prometheus.yml" ;;
    *) exec prometheus --config.file="$runtime/prometheus.yml" "$@" ;;
esac
