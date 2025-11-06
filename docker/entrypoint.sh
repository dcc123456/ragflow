#!/bin/bash

/usr/sbin/nginx

export LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu/

PY=/root/miniconda3/envs/py11/bin/python

function p_0(){
    while [ 1 -eq 1 ];do
      $PY rag/svr/task_executor.py -p 0 -t $1 -i $2;
    done
}

WS=1

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


while  [ 1 -eq 1 ];do
     $PY api/ragflow_server.py
     #$PY -m gunicorn --workers 1 --worker-class gevent --bind 0.0.0.0:9380 api.wsgi:application --reload
done &

while [ 1 -eq 1 ];do
     $PY admin/server/admin_server.py
done &

wait;

