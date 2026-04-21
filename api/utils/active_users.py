import logging
import time

from common.observer import set_active_users_count
from rag.utils.redis_conn import REDIS_CONN

ACTIVE_USERS_KEY = "active_users"


def mark_user_active(user_id: str):
    now = int(time.time())
    REDIS_CONN.zadd(ACTIVE_USERS_KEY, user_id, now)


def get_active_users(window=300):
    now = int(time.time())
    cutoff = now - window

    REDIS_CONN.zremrangebyscore(ACTIVE_USERS_KEY, 0, cutoff)
    return REDIS_CONN.zcard(ACTIVE_USERS_KEY)


def set_active_users_worker(interval=120):
    while True:
        try:
            set_active_users_count(get_active_users(300))
        except Exception as e:
            logging.warning("unable to set active users metrics: %s", str(e))

        time.sleep(interval)
