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
export TIKA_SERVER_JAR="file:///ragflow/tika-server-standard-3.0.0.jar"
#export TENSORRT_TSR_SVR=http://localhost:11234

PY=/root/miniconda3/envs/py11/bin/python

function p_0(){
    while [ 1 -eq 1 ];do
      $PY rag/svr/task_executor.py -p 0 -t $1 -i $2;
    done
}

WS=12

for ((i=0;i<WS;i++))
do
  p_0 common $i &
done

RAPTOR=1
for ((i=0;i<RAPTOR;i++))
do
  p_0 raptor $i &
done


GRAPHRAG=1
for ((i=0;i<GRAPHRAG;i++))
do
  p_0 graphrag $i &
done

$PY admin/server/admin_server.py &

RAGFLOW_HOST=${RAGFLOW_HOST_IP:-0.0.0.0}
RAGFLOW_PORT=${RAGFLOW_HOST_PORT:-9380}
GUNICORN_WORKERS=${GUNICORN_WORKERS:-10}
GUNICORN_TIMEOUT=${GUNICORN_TIMEOUT:-120}
GUNICORN_MODE=${GUNICORN_MODE:-uvicorn.workers.UvicornWorker}

while  [ 1 -eq 1 ];do
      exec /root/miniconda3/envs/py11/bin/gunicorn \
         --workers ${GUNICORN_WORKERS} \
         --worker-class ${GUNICORN_MODE} \
         --worker-connections 1000 \
         --max-requests 1000 \
         --max-requests-jitter 100 \
         --timeout ${GUNICORN_TIMEOUT} \
         --keep-alive 2 \
         --preload \
         --bind ${RAGFLOW_HOST}:${RAGFLOW_PORT} \
         --access-logfile - \
         --error-logfile - \
         --log-level info \
         'api.wsgi:application';

done

wait;

