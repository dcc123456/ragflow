#!/usr/bin/env bash
# One-time credential setup for the running MySQL and MinIO Compose services.
set -euo pipefail
umask 077

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
die() { printf 'Error: %s\n' "$*" >&2; exit 1; }
if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
    echo 'Usage: bash docker/prometheus/setup.sh [Compose options, e.g. -p PROJECT]'
    exit 0
fi
compose=(docker compose --env-file "$script_dir/../.env" -f "$script_dir/../docker-compose-base.yml" "$@")
work="$(mktemp -d)"
staged=''
as_root() { if (( EUID == 0 )); then "$@"; else sudo -- "$@"; fi; }
trap 'rm -rf -- "$work"; [[ -z "$staged" ]] || as_root rm -f -- "$staged"' EXIT
trap 'exit 1' HUP INT TERM

# Let Compose parse .env; do not execute it as a shell script.
environment="$("${compose[@]}" config --environment 2> "$work/error")" || die 'Cannot read Compose configuration; check your .env and Compose options.'
while IFS= read -r line; do
    key="${line%%=*}"
    case "$key" in
        MYSQL_PASSWORD|MYSQL_EXPORTER_USER|MYSQL_EXPORTER_PASSWORD|MINIO_USER|MINIO_PASSWORD)
            printf -v "$key" '%s' "${line#*=}" ;;
    esac
done <<< "$environment"
unset environment line
for key in MYSQL_PASSWORD MYSQL_EXPORTER_PASSWORD MINIO_USER MINIO_PASSWORD; do
    [[ -n "${!key:-}" ]] || die "Set $key in the deployment .env file."
    [[ "${!key}" != *$'\n'* && "${!key}" != *$'\r'* ]] || die "$key must be a single-line value."
done
user="${MYSQL_EXPORTER_USER:-exporter}"
[[ "$user" =~ ^[a-zA-Z0-9_-]{1,32}$ && "$user" != root ]] || die 'Use a dedicated MYSQL_EXPORTER_USER (1..32 letters, digits, _ or -).'

mysql_query() {
    { printf '%s\n' "$2"; printf '%s\n' "$3"; } |
        "${compose[@]}" exec -T mysql sh -c '
            IFS= read -r MYSQL_PWD
            export MYSQL_PWD
            exec mysql --protocol=TCP --host=127.0.0.1 --user="$1" --batch --skip-column-names --raw
        ' sh "$1" 2> "$work/error"
}
# Existing passwords stay unchanged; quote doubling handles special characters.
password="${MYSQL_EXPORTER_PASSWORD//\'/\'\'}"
mysql_query root "$MYSQL_PASSWORD" "SET SESSION sql_mode='NO_BACKSLASH_ESCAPES'; CREATE USER IF NOT EXISTS '$user'@'%' IDENTIFIED BY '$password' WITH MAX_USER_CONNECTIONS 3;" > /dev/null || die 'Cannot prepare MySQL; check the Compose project (-p), MYSQL_PASSWORD and root permissions.'
unset password
identity="$(mysql_query "$user" "$MYSQL_EXPORTER_PASSWORD" 'SELECT CURRENT_USER();')" || die 'MySQL monitoring credentials do not match; existing passwords were not changed.'
[[ "$identity" == "$user@%" ]] || die 'A more specific MySQL account matched; check account host restrictions.'
mysql_query root "$MYSQL_PASSWORD" "GRANT PROCESS, REPLICATION CLIENT ON *.* TO '$user'@'%'; GRANT SELECT ON performance_schema.* TO '$user'@'%';" > /dev/null || die 'Cannot grant MySQL monitoring permissions.'
echo 'MySQL: monitoring account ready.'

generated="$(printf '%s\n' "$MINIO_USER" "$MINIO_PASSWORD" |
    "${compose[@]}" exec -T minio sh -c '
        set -eu
        IFS= read -r access_key
        IFS= read -r secret_key
        MC_CONFIG_DIR=$(mktemp -d /tmp/ragflow-monitoring.XXXXXX)
        export MC_CONFIG_DIR MC_NO_COLOR=1 MC_QUIET=1
        trap '\''rm -rf "$MC_CONFIG_DIR"'\'' EXIT
        mc alias set setup-monitor http://localhost:9000 "$access_key" "$secret_key" >/dev/null
        mc admin prometheus generate setup-monitor --api-version v3
    ' 2> "$work/error")" || die 'Cannot generate the MinIO token; check the Compose project (-p), MINIO_USER/MINIO_PASSWORD and mc availability.'
token="$(printf '%s\n' "$generated" | awk '$1 == "bearer_token:" {print $2; n++} END {if (n != 1) exit 1}')" || die 'mc did not return exactly one monitoring token.'
[[ "$token" =~ ^[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+$ ]] || die 'mc returned an invalid token format.'
unset generated
printf '%s\n' "$token" > "$work/minio.token"
secrets="$script_dir/secrets"
[[ ! -L "$secrets" && ! -L "$secrets/minio.token" ]] || die 'The token directory and file must not be symbolic links.'
as_root install -d -m 755 -- "$secrets"
staged="$(as_root mktemp "$secrets/.minio.token.XXXXXX")"
as_root install -m 600 -o 65534 -g 65534 -- "$work/minio.token" "$staged"
as_root mv -f -- "$staged" "$secrets/minio.token"
staged=''
echo 'MinIO/Silo: monitoring token generated and saved (owner 65534, mode 600).'
echo 'Setup complete. Start monitoring, or restart Prometheus if it is already running.'
