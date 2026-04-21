# To avoid metrics explosion, explicitly set the metric name

import time
import functools

from prometheus_client import Counter, Histogram, Gauge

RETRIEVAL_REQUESTS = Counter("retrieval_requests_total", "Total retrieval requests")
RETRIEVAL_FAILURES = Counter("retrieval_failures_total", "Total retrieval failures")
RETRIEVAL_LATENCY = Histogram("retrieval_latency_seconds", "Retrieval latency in seconds")

ACTIVE_USERS_GAUGE = Gauge("active_users_total", "Number of active users in last 5 minutes")

def retrieval_metrics():
    def decorator(func):
        @functools.wraps(func)
        async def wrapper(*args, **kwargs):
            RETRIEVAL_REQUESTS.inc()
            start = time.perf_counter()
            try:
                return await func(*args, **kwargs)
            except Exception:
                RETRIEVAL_FAILURES.inc()
                raise
            finally:
                RETRIEVAL_LATENCY.observe(time.perf_counter() - start)
        return wrapper
    return decorator


def set_active_users_count(count):
    ACTIVE_USERS_GAUGE.set(count)
