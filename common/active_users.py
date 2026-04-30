import time

from rag.utils.redis_conn import REDIS_CONN

ACTIVE_USERS_KEY = "active_users"


def mark_user_active(user_id: str):
    now = int(time.time())
    REDIS_CONN.zadd(ACTIVE_USERS_KEY, user_id, now)


def get_active_users_count(window=300):
    now = int(time.time())
    cutoff = now - window

    REDIS_CONN.zremrangebyscore(ACTIVE_USERS_KEY, 0, cutoff)
    return REDIS_CONN.zcard(ACTIVE_USERS_KEY)
