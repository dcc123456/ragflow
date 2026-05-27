#!/usr/bin/env bash

set -e

echo "Start RAGFlow cluster, version: "
cat /ragflow/VERSION

# -----------------------------------------------------------------------------
# Usage and command-line argument parsing
# -----------------------------------------------------------------------------
function usage() {
    echo "Usage: $0 [--disable-webserver] [--disable-taskexecutor] [--disable-datasync] [--consumer-no-beg=<num>] [--consumer-no-end=<num>] [--workers=<num>] [--host-id=<string>]"
    echo
    echo "  --disable-webserver             Disables the web server (nginx + ragflow_server)."
    echo "  --disable-taskexecutor          Disables task executor workers."
    echo "  --disable-datasync              Disables synchronization of datasource workers."
    echo "  --enable-mcpserver              Enables the MCP server."
    echo "  --enable-adminserver            Enables the Admin server."
    echo "  --init-superuser                Initializes the superuser."
    echo "  --consumer-no-beg=<num>         Start range for consumers (if using range-based)."
    echo "  --consumer-no-end=<num>         End range for consumers (if using range-based)."
    echo "  --workers=<num>                 Number of task executors to run (if range is not used)."
    echo "  --host-id=<string>              Unique ID for the host (defaults to \`hostname\`)."
    echo
    echo "Examples:"
    echo "  $0 --disable-taskexecutor"
    echo "  $0 --disable-webserver --consumer-no-beg=0 --consumer-no-end=5"
    echo "  $0 --disable-webserver --workers=2 --host-id=myhost123"
    echo "  $0 --enable-mcpserver"
    echo "  $0 --enable-adminserver"
    echo "  $0 --init-superuser"
    exit 1
}

ENABLE_WEBSERVER=1 # Default to enable web server
ENABLE_TASKEXECUTOR=1  # Default to enable task executor
ENABLE_DATASYNC=1
ENABLE_MCP_SERVER=0
ENABLE_ADMIN_SERVER=0 # Default close admin server
INIT_SUPERUSER_ARGS="" # Default to not initialize superuser
CONSUMER_NO_BEG=0
CONSUMER_NO_END=0
WORKERS=1

MCP_HOST="127.0.0.1"
MCP_PORT=9382
MCP_BASE_URL="http://127.0.0.1:9380"
MCP_SCRIPT_PATH="/ragflow/mcp/server/server.py"
MCP_MODE="self-host"
MCP_HOST_API_KEY=""
MCP_TRANSPORT_SSE_FLAG="--transport-sse-enabled"
MCP_TRANSPORT_STREAMABLE_HTTP_FLAG="--transport-streamable-http-enabled"
MCP_JSON_RESPONSE_FLAG="--json-response"

# -----------------------------------------------------------------------------
# Host ID logic:
#   1. By default, use the system hostname if length <= 32
#   2. Otherwise, use the full MD5 hash of the hostname (32 hex chars)
# -----------------------------------------------------------------------------
CURRENT_HOSTNAME="$(hostname)"
if [ ${#CURRENT_HOSTNAME} -le 32 ]; then
  DEFAULT_HOST_ID="$CURRENT_HOSTNAME"
else
  DEFAULT_HOST_ID="$(echo -n "$CURRENT_HOSTNAME" | md5sum | cut -d ' ' -f 1)"
fi

HOST_ID="$DEFAULT_HOST_ID"

# Parse arguments
for arg in "$@"; do
  case $arg in
    --disable-webserver)
      ENABLE_WEBSERVER=0
      shift
      ;;
    --disable-taskexecutor)
      ENABLE_TASKEXECUTOR=0
      shift
      ;;
    --disable-datasync)
      ENABLE_DATASYNC=0
      shift
      ;;
    --enable-mcpserver)
      ENABLE_MCP_SERVER=1
      shift
      ;;
    --enable-adminserver)
      ENABLE_ADMIN_SERVER=1
      shift
      ;;
    --init-superuser)
      INIT_SUPERUSER_ARGS="--init-superuser"
      shift
      ;;
    --mcp-host=*)
      MCP_HOST="${arg#*=}"
      shift
      ;;
    --mcp-port=*)
      MCP_PORT="${arg#*=}"
      shift
      ;;
    --mcp-base-url=*)
      MCP_BASE_URL="${arg#*=}"
      shift
      ;;
    --mcp-mode=*)
      MCP_MODE="${arg#*=}"
      shift
      ;;
    --mcp-host-api-key=*)
      MCP_HOST_API_KEY="${arg#*=}"
      shift
      ;;
    --mcp-script-path=*)
      MCP_SCRIPT_PATH="${arg#*=}"
      shift
      ;;
    --no-transport-sse-enabled)
      MCP_TRANSPORT_SSE_FLAG="--no-transport-sse-enabled"
      shift
      ;;
    --no-transport-streamable-http-enabled)
      MCP_TRANSPORT_STREAMABLE_HTTP_FLAG="--no-transport-streamable-http-enabled"
      shift
      ;;
    --no-json-response)
      MCP_JSON_RESPONSE_FLAG="--no-json-response"
      shift
      ;;
    --consumer-no-beg=*)
      CONSUMER_NO_BEG="${arg#*=}"
      shift
      ;;
    --consumer-no-end=*)
      CONSUMER_NO_END="${arg#*=}"
      shift
      ;;
    --workers=*)
      WORKERS="${arg#*=}"
      shift
      ;;
    --host-id=*)
      HOST_ID="${arg#*=}"
      shift
      ;;
    *)
      usage
      ;;
  esac
done

# -----------------------------------------------------------------------------
# Replace env variables in the service_conf.yaml file
# -----------------------------------------------------------------------------
CONF_DIR="/ragflow/conf"
TEMPLATE_FILE="${CONF_DIR}/service_conf.yaml.template"
CONF_FILE="${CONF_DIR}/service_conf.yaml"

rm -f "${CONF_FILE}"
while IFS= read -r line || [[ -n "$line" ]]; do
    eval "echo \"$line\"" >> "${CONF_FILE}"
done < "${TEMPLATE_FILE}"

# -----------------------------------------------------------------------------
# Export Redis connection info for Nginx Lua rate limiter
# -----------------------------------------------------------------------------
PY=python3
# Parse redis config using a Python script file (avoids quoting issues with inline -c)
"$PY" <<'PYEOF' > /tmp/ratelimit_env.sh
import yaml, os
conf = os.environ.get("CONF_FILE", "/ragflow/conf/service_conf.yaml")
try:
    with open(conf) as f:
        cfg = yaml.safe_load(f) or {}
except Exception:
    cfg = {}
redis_cfg = cfg.get("redis", {})
host_port = str(redis_cfg.get("host", "redis:6379"))
parts = host_port.split(":")
host = parts[0] if parts[0] else "redis"
port = parts[1] if len(parts) > 1 else "6379"
import shlex, os
password = str(redis_cfg.get("password", ""))
db = str(redis_cfg.get("db", "1"))
# Prefer existing env vars (from K8s Secret) over yaml defaults
if not os.environ.get("RATELIMIT_REDIS_HOST"):
    print(f"export RATELIMIT_REDIS_HOST={shlex.quote(host)}")
if not os.environ.get("RATELIMIT_REDIS_PORT"):
    print(f"export RATELIMIT_REDIS_PORT={shlex.quote(port)}")
if not os.environ.get("RATELIMIT_REDIS_PASSWORD"):
    print(f"export RATELIMIT_REDIS_PASSWORD={shlex.quote(password)}")
if not os.environ.get("RATELIMIT_REDIS_DB"):
    print(f"export RATELIMIT_REDIS_DB={shlex.quote(db)}")
PYEOF
source /tmp/ratelimit_env.sh
echo "Rate limiter Redis: ${RATELIMIT_REDIS_HOST}:${RATELIMIT_REDIS_PORT} db=${RATELIMIT_REDIS_DB}"

# Ensure RATELIMIT_DISABLED is exported so nginx workers can read it via os.getenv
if [ -n "${RATELIMIT_DISABLED}" ]; then
    export RATELIMIT_DISABLED
    echo "Rate limiter disabled: ${RATELIMIT_DISABLED}"
fi

# Inject DNS resolver into nginx.conf (required for Lua cosocket DNS resolution in K8s)
DNS_RESOLVER=$(grep '^nameserver' /etc/resolv.conf | head -1 | awk '{print $2}')
if [ -n "$DNS_RESOLVER" ]; then
    sed -i "s/^http {/http {\n    resolver ${DNS_RESOLVER} valid=30s ipv6=off;/" /etc/nginx/nginx.conf
    echo "Nginx DNS resolver: ${DNS_RESOLVER}"
fi

# -----------------------------------------------------------------------------
# Select Nginx Configuration based on API_PROXY_SCHEME
# -----------------------------------------------------------------------------
NGINX_CONF_DIR="/etc/nginx/conf.d"
if [ -n "$API_PROXY_SCHEME" ]; then
    if [[ "${API_PROXY_SCHEME}" == "hybrid" ]]; then
        cp -f "$NGINX_CONF_DIR/ragflow.conf.hybrid" "$NGINX_CONF_DIR/ragflow.conf"
        echo "Applied nginx config: ragflow.conf.hybrid"
    elif [[ "${API_PROXY_SCHEME}" == "go" ]]; then
        cp -f "$NGINX_CONF_DIR/ragflow.conf.golang" "$NGINX_CONF_DIR/ragflow.conf"
        echo "Applied nginx config: ragflow.conf.golang (default)"
    else
        cp -f "$NGINX_CONF_DIR/ragflow.conf.python" "$NGINX_CONF_DIR/ragflow.conf"
        echo "Applied nginx config: ragflow.conf.python"
    fi
else
    # Default to python backend
    cp -f "$NGINX_CONF_DIR/ragflow.conf.python" "$NGINX_CONF_DIR/ragflow.conf"
    echo "Default: applied nginx config: ragflow.conf.python"
fi

export LD_LIBRARY_PATH="/usr/local/openresty/openssl3/lib:/usr/local/openresty/openssl/lib:/usr/lib/x86_64-linux-gnu/"
PY=python3
export LD_PRELOAD="$(pkg-config --variable=libdir jemalloc)/libjemalloc.so"
export MALLOC_CONF="dirty_decay_ms:2000,muzzy_decay_ms:2000"
export PYTHONMALLOC=malloc

function start_mcp_server() {
    echo "Starting MCP Server on ${MCP_HOST}:${MCP_PORT} with base URL ${MCP_BASE_URL}..."
    "$PY" "${MCP_SCRIPT_PATH}" \
        --host="${MCP_HOST}" \
        --port="${MCP_PORT}" \
        --base-url="${MCP_BASE_URL}" \
        --mode="${MCP_MODE}" \
        --api-key="${MCP_HOST_API_KEY}" \
        "${MCP_TRANSPORT_SSE_FLAG}" \
        "${MCP_TRANSPORT_STREAMABLE_HTTP_FLAG}" \
        "${MCP_JSON_RESPONSE_FLAG}" &
}

function ensure_docling() {
    [[ "${USE_DOCLING}" == "true" ]] || { echo "[docling] disabled by USE_DOCLING"; return 0; }
    DOCLING_PIN="${DOCLING_VERSION:-==2.71.0}"
    "$PY" -c "import importlib.util,sys; sys.exit(0 if importlib.util.find_spec('docling') else 1)" \
      || uv pip install -i https://pypi.tuna.tsinghua.edu.cn/simple --extra-index-url https://pypi.org/simple --no-cache-dir "docling${DOCLING_PIN}"
}

function ensure_db_init() {
    echo "Initializing database tables..."
    "$PY" -c "from api.db.db_models import init_database_tables as init_web_db; init_web_db()"
    echo "Database tables initialized."
}

function wait_for_server() {
    local url="$1"
    local server_name="$2"
    local timeout=90
    local interval=2
    local start_time=$(date +%s)

    echo "Waiting for $server_name to be ready at $url..."
    while ! curl -f -s -o /dev/null "$url"; do
        if [ $(($(date +%s) - start_time)) -gt $timeout ]; then
            echo "Timeout waiting for $server_name after $timeout seconds"
            return 1
        fi
        sleep $interval
    done
    echo "$server_name is ready."
}

# -----------------------------------------------------------------------------
# Start components based on flags
# -----------------------------------------------------------------------------
ensure_docling
ensure_db_init

run_with_restart() {
  local process_name="$1"
  shift

  while true; do
    echo "Attempt to start ${process_name}..."
    set +e
    "$@"
    local exit_code=$?
    set -e
    echo "${process_name} exited with code ${exit_code}. Restarting in 1 second..."
    sleep 1
  done
}

if [[ "${ENABLE_WEBSERVER}" -eq 1 ]]; then
    echo "Starting nginx..."
    /usr/sbin/nginx -c /etc/nginx/nginx.conf

    run_with_restart "RAGFlow server" "$PY" api/ragflow_server.py ${INIT_SUPERUSER_ARGS} &

    if [[ "${API_PROXY_SCHEME}" == "hybrid" ]]; then
        while true; do
            echo "Attempt to start RAGFlow go server..."
            wait_for_server "http://127.0.0.1:9380/healthz" "ragflow_server"
            echo "Starting RAGFlow go server..."
      set +e
      bin/server_main
      exit_code=$?
      set -e
      echo "RAGFlow go server exited with code ${exit_code}. Restarting in 1 second..."
            sleep 1;
        done &
    fi
fi


if [[ "${ENABLE_ADMIN_SERVER}" -eq 1 ]]; then
    run_with_restart "Admin python server" "$PY" admin/server/admin_server.py &

    if [[ "${API_PROXY_SCHEME}" == "hybrid" ]]; then
        while true; do
            echo "Attempt to starting Admin go server..."
            wait_for_server "http://127.0.0.1:9381/api/v1/admin/ping" "admin_server"
            echo "Starting Admin go server..."
      set +e
      bin/admin_server
      exit_code=$?
      set -e
      echo "Admin go server exited with code ${exit_code}. Restarting in 1 second..."
            sleep 1;
        done &
    fi
fi

if [[ "${ENABLE_DATASYNC}" -eq 1 ]]; then
    echo "Starting data sync..."
    run_with_restart "data sync" "$PY" rag/svr/sync_data_source.py &
fi

if [[ "${ENABLE_MCP_SERVER}" -eq 1 ]]; then
    start_mcp_server
fi


#if [[ "${ENABLE_TASKEXECUTOR}" -eq 1 ]]; then
#    if [[ "${CONSUMER_NO_END}" -gt "${CONSUMER_NO_BEG}" ]]; then
#        echo "Starting task executors on host '${HOST_ID}' for IDs in [${CONSUMER_NO_BEG}, ${CONSUMER_NO_END})..."
#        for (( i=CONSUMER_NO_BEG; i<CONSUMER_NO_END; i++ ))
#        do
#          task_exe "${i}" "${HOST_ID}" &
#        done
#    else
#        # Otherwise, start a fixed number of workers
#        echo "Starting ${WORKERS} task executor(s) on host '${HOST_ID}'..."
#        for (( i=0; i<WORKERS; i++ ))
#        do
#          task_exe "${i}" "${HOST_ID}" &
#        done
#    fi
#fi

wait
