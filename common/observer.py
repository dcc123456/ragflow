# To avoid metrics explosion, explicitly set the metric name

import functools
import os
import time

from prometheus_client import Counter, Histogram, Gauge

RETRIEVAL_REQUESTS = Counter("retrieval_requests_total", "Total retrieval requests", ["pod"])
RETRIEVAL_FAILURES = Counter("retrieval_failures_total", "Total retrieval failures", ["pod"])
RETRIEVAL_LATENCY = Histogram("retrieval_latency_seconds", "Retrieval latency in seconds", ["pod"])

ACTIVE_USERS_GAUGE = Gauge("active_users_total", "Number of active users in last 5 minutes", ["pod"])

QUEUE_READY = Gauge("queue_messages_ready", "Messages waiting in queue", ["queue"])
QUEUE_INFLIGHT = Gauge("queue_messages_inflight", "Messages being processed", ["queue"])
QUEUE_ACK_TOTAL = Counter("queue_messages_ack_total", "Total acknowledged messages", ["queue"])

HOSTNAME = os.environ.get("HOSTNAME", "ragflow")


def retrieval_metrics():
    def decorator(func):
        @functools.wraps(func)
        async def wrapper(*args, **kwargs):
            RETRIEVAL_REQUESTS.labels(pod=HOSTNAME).inc()
            start = time.perf_counter()
            try:
                return await func(*args, **kwargs)
            except Exception:
                RETRIEVAL_FAILURES.labels(pod=HOSTNAME).inc()
                raise
            finally:
                RETRIEVAL_LATENCY.labels(pod=HOSTNAME).observe(time.perf_counter() - start)

        return wrapper

    return decorator


def set_active_users_count(count):
    ACTIVE_USERS_GAUGE.labels(pod=HOSTNAME).set(count)


def set_queue_metrics(queue):
    name = queue["name"]
    QUEUE_READY.labels(queue=name).set(queue["ready"])
    QUEUE_INFLIGHT.labels(queue=name).set(queue["inflight"])
    QUEUE_ACK_TOTAL.labels(queue=name).set(queue["ack"])
