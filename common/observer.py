# To avoid metrics explosion, explicitly set the metric name

import functools
import os
import time

from prometheus_client import Counter, Histogram, Gauge

HOSTNAME = os.environ.get("HOSTNAME", "ragflow")

RETRIEVAL_REQUESTS = Counter("retrieval_requests_total", "Total retrieval requests", ["hostname"])
RETRIEVAL_FAILURES = Counter("retrieval_failures_total", "Total retrieval failures", ["hostname"])
RETRIEVAL_LATENCY = Histogram("retrieval_latency_seconds", "Retrieval latency in seconds", ["hostname"])

ACTIVE_USERS_GAUGE = Gauge("active_users_total", "Number of active users in last 5 minutes")

TASKS_PENDING = Gauge("queue_tasks_pending", "Tasks pending", ["queue"])
TASKS_IN_PROGRESS = Gauge("queue_tasks_in_progress", "Tasks in progress", ["queue"])
TASKS_COMPLETED_TOTAL = Gauge("queue_tasks_completed_total", "Total completed tasks", ["queue"])


def retrieval_metrics():
    def decorator(func):
        @functools.wraps(func)
        async def wrapper(*args, **kwargs):
            RETRIEVAL_REQUESTS.labels(hostname=HOSTNAME).inc()
            start = time.perf_counter()
            try:
                return await func(*args, **kwargs)
            except Exception:
                RETRIEVAL_FAILURES.labels(hostname=HOSTNAME).inc()
                raise
            finally:
                RETRIEVAL_LATENCY.labels(hostname=HOSTNAME).observe(time.perf_counter() - start)

        return wrapper

    return decorator


def set_active_users_count(count):
    ACTIVE_USERS_GAUGE.set(count)


def set_queue_metrics(name, ready, inflight, ack):
    TASKS_PENDING.labels(queue=name).set(ready)
    TASKS_IN_PROGRESS.labels(queue=name).set(inflight)
    TASKS_COMPLETED_TOTAL.labels(queue=name).set(ack)
