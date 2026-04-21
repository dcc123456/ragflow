import logging
import os
import time

import requests

from common.observer import set_queue_metrics


def fetch_rabbitmq_queues(url, user, pwd):
    """
    it used below to query queue status from RabbitMQ
    curl -s -u 'ragflow:PASS' http://rabbitmq:15672/api/queues | jq '.[] | {name, messages_ready, messages_unacknowledged, ack: (.message_stats.ack // 0)}'
    """
    resp = requests.get(url, auth=(user, pwd), timeout=15)
    resp.raise_for_status()
    data = resp.json()
    return [
        {
            "name": q.get("name"),
            "ready": q.get("messages_ready", 0),
            "inflight": q.get("messages_unacknowledged", 0),
            "ack": q.get("message_stats", {}).get("ack", 0),
        }
        for q in data
    ]


def rabbitmq_metrics_worker(interval=60):
    host = os.environ.get("RABBITMQ_HOST", "rabbitmq")
    port = os.environ.get("RABBITMQ_API_PORT", "15672")
    user = os.environ.get("RABBITMQ_DEFAULT_USER", "ragflow")
    pwd = os.environ.get("RABBITMQ_DEFAULT_PASS", "")
    url = f"http://{host}:{port}/api/queues"
    while True:
        try:
            queues = fetch_rabbitmq_queues(url, user, pwd)

            for q in queues:
                set_queue_metrics(q)

        except Exception as e:
            logging.warning("unable to set rabbitmq queues metrics: %s", str(e))

        time.sleep(interval)
