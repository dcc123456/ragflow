#!/bin/bash

# Increase file descriptor limit to prevent "too many open files" error
# The parser runs multiple worker processes that use fsnotify for file watching
ulimit -n 65536

#/usr/sbin/nginx

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

export LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu/

# Extract TE_IDX from hostname
# Hostname format: parser-common-794bc8cf88-d49nt, parser-graphrag-0 etc.
HOSTNAME=$(hostname)
if [[ -n "$HOSTNAME" ]]; then
    # Extract the ordinal number from hostname (e.g., parser-common-0 -> 0)
    TE_IDX=$(echo "$HOSTNAME" | sed 's/.*-//')
    export TE_IDX
    echo "Detected hostname=$HOSTNAME, setting TE_IDX=$TE_IDX"
else
    export TE_IDX=0
    echo "hostname not set, defaulting TE_IDX=0"
fi

PY=/ragflow/.venv/bin/python
LD_PRELOAD="$(pkg-config --variable=libdir jemalloc)/libjemalloc.so"

# PARSER_TYPE determines which task type this pod handles.
# Valid values: common, graphrag, raptor, resume
# If not set or "all", fall back to legacy behavior (start all types).
PARSER_TYPE="${PARSER_TYPE:-all}"
echo "PARSER_TYPE=$PARSER_TYPE"

function run_task_executor(){
    local task_type=$1
    while [ 1 -eq 1 ];do
      $PY rag/svr/task_executor.py -t $task_type -i $TE_IDX;
    done
}

if [[ "$PARSER_TYPE" == "all" ]]; then
    # Legacy mode: start all task types in one pod
    WS=${WS_WORKERS:-3}
    for ((i=0;i<WS;i++))
    do
      run_task_executor common &
    done

    RAPTOR=${RAPTOR_WORKERS:-1}
    for ((i=0;i<RAPTOR;i++))
    do
      run_task_executor raptor &
    done

    GRAPHRAG=${GRAPHRAG_WORKERS:-1}
    for ((i=0;i<GRAPHRAG;i++))
    do
      run_task_executor graphrag &
    done

    RESUME=${RESUME_WORKERS:-1}
    for ((i=0;i<RESUME;i++))
    do
      run_task_executor resume &
    done
else
    # Single-type mode: only start the specified task type
    run_task_executor "$PARSER_TYPE" &
fi

wait;
