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
# Hostname format: parser-794bc8cf88-d49nt, parser-0 etc.
HOSTNAME=$(hostname)
if [[ -n "$HOSTNAME" ]]; then
    # Extract the ordinal number from hostname (e.g., parser-0 -> 0)
    TE_IDX=$(echo "$HOSTNAME" | sed 's/.*-//')
    export TE_IDX
    echo "Detected hostname=$HOSTNAME, setting TE_IDX=$TE_IDX"
else
    export TE_IDX=0
    echo "hostname not set, defaulting TE_IDX=0"
fi

PY=/ragflow/.venv/bin/python
LD_PRELOAD="$(pkg-config --variable=libdir jemalloc)/libjemalloc.so"

function p_0(){
    while [ 1 -eq 1 ];do
      $PY rag/svr/task_executor.py -p 0 -i $TE_IDX -t $1;
    done
}

# Allow worker counts to be configured via environment variables
# This helps reduce fsnotify usage to avoid "too many open files" error
# Default: WS=3, RAPTOR=3, GRAPHRAG=3, RESUME=1 (total 10 workers)
# Recommended for limited fs.inotify: WS=1, RAPTOR=1, GRAPHRAG=1, RESUME=0 (total 3 workers)
WS=${WS_WORKERS:-3}
for ((i=0;i<WS;i++))
do
  p_0 common &
done

RAPTOR=${RAPTOR_WORKERS:-1}
for ((i=0;i<RAPTOR;i++))
do
  p_0 raptor  &
done

GRAPHRAG=${GRAPHRAG_WORKERS:-1}
for ((i=0;i<GRAPHRAG;i++))
do
  p_0 graphrag  &
done

RESUME=${RESUME_WORKERS:-1}
for ((i=0;i<RESUME;i++))
do
  p_0 resume  &
done

wait;
