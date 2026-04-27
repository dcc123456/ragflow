import logging
import os
import time

import requests

from common.active_users import get_active_users_count
from common.observer import set_queue_metrics, set_active_users_count


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


def admin_metrics_worker(interval=60):
    host = os.environ.get("RABBITMQ_HOST", "rabbitmq")
    port = os.environ.get("RABBITMQ_API_PORT")
    if not port:
        port = os.environ.get("RABBITMQ_MANAGEMENT_PORT", "15672")
    user = os.environ.get("RABBITMQ_DEFAULT_USER", "ragflow")
    pwd = os.environ.get("RABBITMQ_DEFAULT_PASS", "")
    url = f"http://{host}:{port}/api/queues"
    while True:
        active_users, q_messages = 0, []
        try:
            active_users = get_active_users_count()
            set_active_users_count(active_users)
        except Exception as e:
            logging.warning("[metrics] unable to update active users: %s", str(e))

        try:
            queues = fetch_rabbitmq_queues(url, user, pwd)

            for q in queues:
                name, ready, inflight, ack = q["name"], q["ready"], q["inflight"], q["ack"]
                set_queue_metrics(name, ready, inflight, ack)
                q_messages.append(f"{name}: pending={ready}, in_progress={inflight}, completed={ack}")

        except Exception as e:
            logging.warning("[metrics] unable to get rabbitmq queues metrics: %s", str(e))

        logging.info("[metrics] active users: %s. tasks: %s", active_users, "; ".join(q_messages))
        time.sleep(interval)
