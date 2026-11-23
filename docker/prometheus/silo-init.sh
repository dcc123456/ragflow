#!/bin/sh
set -eu
umask 077

if [ "$MONITORING_MINIO_ENABLED" != true ] || [ "$MINIO_METRICS_ADDRESS" != minio:9000 ]; then
    exit 0
fi

die() { printf 'Silo monitoring: %s\n' "$*" >&2; exit 1; }

MC_CONFIG_DIR="$(mktemp -d)"
export MC_CONFIG_DIR
staged=''

cleanup() {
    rm -rf "$MC_CONFIG_DIR"
    if [ -n "$staged" ]; then
        rm -f "$staged"
    fi
}
trap cleanup EXIT

# With an explicit signature version, mc can sign the token offline.
mc --no-color alias set --api S3v4 monitoring http://minio:9000 "$MINIO_USER" "$MINIO_PASSWORD" >/dev/null 2>&1 ||
    die 'Cannot configure mc. Check MINIO_USER and MINIO_PASSWORD.'
mc --no-color admin prometheus generate monitoring --api-version v3 >"$MC_CONFIG_DIR/scrape.yml" 2>/dev/null ||
    die 'Cannot generate the v3 token.'

token=''
while read -r key value; do
    [ "$key" = bearer_token: ] || continue
    [ -z "$token" ] || die 'mc returned more than one bearer token.'
    token="$value"
done < "$MC_CONFIG_DIR/scrape.yml"
case "$token" in
    .*|*..*|*.|*.*.*.*|*[!A-Za-z0-9_.-]*) die 'mc returned an invalid bearer token.' ;;
    *.*.*) ;;
    *) die 'mc did not return a bearer token.' ;;
esac

credentials=/monitoring/credentials
staged="$(mktemp "$credentials/minio.token.XXXXXX")"
printf '%s\n' "$token" > "$staged"
chown 65534:65534 "$staged"
mv -f "$staged" "$credentials/minio.token"
staged=''
echo 'Silo monitoring token ready.'
