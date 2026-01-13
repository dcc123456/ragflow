#!/bin/bash

/usr/sbin/nginx

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
#export DEEPDOC_URL=http://deepdoc:8000

PY=/ragflow/.venv/bin/python

$PY admin/server/admin_server.py &

RAGFLOW_HOST=${RAGFLOW_HOST_IP:-0.0.0.0}
RAGFLOW_PORT=${RAGFLOW_HOST_PORT:-9380}
UVICORN_WORKERS=${UVICORN_WORKERS:-1}
UVICORN_TIMEOUT=${UVICORN_TIMEOUT:-120}

while  [ 1 -eq 1 ];do
      exec /ragflow/.venv/bin/python -m uvicorn \
         api.wsgi:application \
         --host ${RAGFLOW_HOST} \
         --port ${RAGFLOW_PORT} \
         --workers ${UVICORN_WORKERS} \
         --timeout-keep-alive ${UVICORN_TIMEOUT} \
         --log-level info \
         --access-log;

done

wait;

