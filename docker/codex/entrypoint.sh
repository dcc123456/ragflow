#!/usr/bin/env sh
#
# Entry point for the Codex app-server container.
#
# The app-server reads its WS capability token ONLY from a file — it does not honor
# environment variables for --ws-auth/--ws-token-file (verified on 0.162.0). So we
# materialize the token from an env var into a private file and hand the app-server that
# path. The operator-facing interface stays env-driven, and the secret never appears on
# the command line (only the token *file* path does).
set -eu

: "${CODEX_WS_TOKEN:?CODEX_WS_TOKEN is required: set it to the same value as RAGFLOW_CODEX_BEARER_TOKEN on the API}"

# Codex's memory workspace expects $CODEX_HOME/memories to exist. A fresh volume is seeded
# from the image, which ships only .codex/ itself, so create it here; without it every turn
# logs "failed preparing memory workspace".
mkdir -p "${CODEX_HOME:-/home/codex/.codex}/memories"

TOKEN_FILE="${CODEX_WS_TOKEN_FILE:-/tmp/codex-ws-token}"
umask 077
printf '%s' "$CODEX_WS_TOKEN" > "$TOKEN_FILE"

# Bind 0.0.0.0 inside the container (the API reaches it cross-container over the compose
# network); host exposure is restricted to 127.0.0.1 by the compose port mapping. A
# non-loopback listener refuses to start without auth, hence --ws-auth is always set.
exec /usr/local/bin/codex-app-server \
  --listen "${CODEX_WS_LISTEN:-ws://0.0.0.0:8790}" \
  --ws-auth capability-token \
  --ws-token-file "$TOKEN_FILE" \
  "$@"
