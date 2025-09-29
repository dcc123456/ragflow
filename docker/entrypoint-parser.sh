#!/bin/bash

/usr/sbin/nginx

export LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu/

PY=/root/miniconda3/envs/py11/bin/python

function p_0(){
    while [ 1 -eq 1 ];do
      $PY rag/svr/task_executor.py -p 0 -t $1;
    done
}

WS=46

for ((i=0;i<WS;i++))
do
  p_0 common &
done

RAPTOR=3
for ((i=0;i<RAPTOR;i++))
do
  p_0 raptor  &
done


GRAPHRAG=3
for ((i=0;i<GRAPHRAG;i++))
do
  p_0 graphrag  &
done


RESUME=1
for ((i=0;i<RESUME;i++))
do
  p_0 resume  &
done



wait;
