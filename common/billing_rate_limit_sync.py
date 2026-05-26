#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#

"""
Billing Rate Limit Sync — keeps Redis cache up-to-date for Nginx Lua rate limiter.

This module:
  1. Syncs billing rate limits per tenant to Redis (billing:ratelimit:<tenant_id>)
  2. Syncs API token -> tenant_id mappings to Redis (billing:token:<token>)
  3. Provides periodic full-sync via start_periodic_sync()

The Nginx Lua rate limiter reads these Redis keys to enforce limits at the gateway layer,
before requests reach the Python/Go application.

Usage:
  from common.billing_rate_limit_sync import sync_all_rate_limits, start_periodic_sync
  sync_all_rate_limits()           # Call on startup
  start_periodic_sync(interval=3600)  # Start hourly background refresh
"""

import json
import logging
import threading
from datetime import datetime, timezone
from typing import Optional

from rag.utils.redis_conn import REDIS_CONN


# Redis key patterns
_TOKEN_KEY_PREFIX = "billing:token:"
_RATELIMIT_KEY_PREFIX = "billing:ratelimit:"
# Set a TTL on rate limit keys so stale entries auto-expire
_RATELIMIT_KEY_TTL = 86400 * 31  # 31 days

# Periodic sync state
_sync_thread: Optional[threading.Thread] = None
_sync_stop_event = threading.Event()


_cached_billing_redis = None


def _get_redis():
    """Get a Redis client connected to the correct billing database.

    REDIS_CONN (used by the app) may point to a different Redis DB than what
    the Nginx Lua rate limiter uses.  The Lua side reads RATELIMIT_REDIS_DB /
    REDIS_DB from the environment, so we must use the same DB here.

    Returns a Redis client connected to the billing-rate-limit DB, or None.
    """
    global _cached_billing_redis
    if _cached_billing_redis is not None:
        return _cached_billing_redis

    import os
    import redis as _redis

    target_db = int(os.environ.get("RATELIMIT_REDIS_DB") or os.environ.get("REDIS_DB") or "1")

    # If REDIS_CONN happens to use the same DB, reuse it
    if REDIS_CONN and REDIS_CONN.REDIS:
        pool_kwargs = REDIS_CONN.REDIS.connection_pool.connection_kwargs
        conn_db = pool_kwargs.get("db", 0)
        if conn_db == target_db:
            _cached_billing_redis = REDIS_CONN.REDIS
            return _cached_billing_redis

    # Otherwise, create a dedicated connection to the target DB
    try:
        host = os.environ.get("REDIS_HOST", "redis")
        port = int(os.environ.get("REDIS_PORT", "6379"))
        password = os.environ.get("REDIS_PASSWORD") or None
        _cached_billing_redis = _redis.StrictRedis(
            host=host, port=port, db=target_db,
            password=password, decode_responses=True,
        )
        _cached_billing_redis.ping()
        logging.info(f"billing_rate_limit_sync: connected to Redis db={target_db}")
        return _cached_billing_redis
    except Exception as e:
        logging.warning(f"billing_rate_limit_sync: failed to connect to Redis db={target_db}: {e}")
        return None


def _to_unix_ts(dt_val) -> float | None:
    """Convert a datetime (naive or aware) to a Unix timestamp (float)."""
    if dt_val is None:
        return None
    if isinstance(dt_val, (int, float)):
        return float(dt_val)
    if isinstance(dt_val, datetime):
        if dt_val.tzinfo is None:
            # Assume UTC for naive datetimes
            dt_val = dt_val.replace(tzinfo=timezone.utc)
        return dt_val.timestamp()
    return None


def _resolve_rate_limits_from_db(plan_name: str | None) -> int:
    """
    Look up rate limits from MySQL billing_product table (durable source of truth).
    Falls back to Trial plan if plan_name is None or not found.
    Returns rpm (requests per minute).
    """
    from api.db.services.billing_service import ProductService

    name = (plan_name or "Trial").strip()
    plan = ProductService.get_by_name(name)
    if not plan:
        plan = ProductService.get_by_name("Trial")

    if plan:
        rpm = plan.get("api_request_limit_per_minute") or 500
    else:
        # MySQL has no data at all — fall back to in-memory config
        from common import settings
        info = settings.BILLING_PLAN_TO_INFO.get(name) or settings.BILLING_PLAN_TO_INFO.get("Trial") or {}
        rpm = info.get("api_request_limit_per_minute", 500)

    return rpm


def sync_tenant_rate_limit(tenant_id: str, plan_name: str | None, period_end: float | None = None) -> bool:
    """
    Sync a single tenant's rate limit configuration to Redis.

    Writes billing:ratelimit:<tenant_id> -> {"rpm": <int>}
    where rpm = requests per minute.

    Rate limit values are read from MySQL billing_product (durable). If MySQL
    has no data, falls back to in-memory YAML config.

    Args:
        tenant_id: The tenant identifier
        plan_name: Billing plan name (e.g., "Trial", "Starter", "Pro")

    Returns True on success.
    """
    redis = _get_redis()
    if not redis:
        return False

    rpm = _resolve_rate_limits_from_db(plan_name)

    # Cap at reasonable values (2147483647 means "unlimited" in config)
    UNLIMITED = 2147483647
    if rpm and int(rpm) >= UNLIMITED:
        rpm = 100000  # Effective unlimited for the Lua layer

    try:
        rpm = int(rpm)
    except (TypeError, ValueError):
        rpm = 500

    key = f"{_RATELIMIT_KEY_PREFIX}{tenant_id}"
    value = json.dumps({"rpm": rpm})

    try:
        redis.set(key, value, ex=_RATELIMIT_KEY_TTL)
        logging.debug(f"sync_tenant_rate_limit: {key} -> {value}")
        return True
    except Exception as e:
        logging.warning(f"sync_tenant_rate_limit failed for {tenant_id}: {e}")
        return False


def sync_api_token(token: str, tenant_id: str) -> bool:
    """
    Sync an API token -> tenant_id mapping to Redis.

    Writes billing:token:<token> -> <tenant_id>
    The Lua rate limiter uses this to identify the tenant from the Authorization header.
    """
    redis = _get_redis()
    if not redis:
        return False

    key = f"{_TOKEN_KEY_PREFIX}{token}"
    try:
        # Token mapping TTL = 31 days (refreshed on sync)
        redis.set(key, tenant_id, ex=_RATELIMIT_KEY_TTL)
        logging.debug(f"sync_api_token: {key[:30]}... -> {tenant_id}")
        return True
    except Exception as e:
        logging.warning(f"sync_api_token failed: {e}")
        return False


def delete_api_token(token: str) -> bool:
    """
    Remove an API token -> tenant_id mapping from Redis.
    """
    redis = _get_redis()
    if not redis:
        return False

    key = f"{_TOKEN_KEY_PREFIX}{token}"
    try:
        redis.delete(key)
        logging.debug(f"delete_api_token: {key[:30]}...")
        return True
    except Exception as e:
        logging.warning(f"delete_api_token failed: {e}")
        return False


def sync_session_token(access_token: str, tenant_id: str) -> bool:
    """
    Sync a session (User) token -> tenant_id mapping to Redis.

    Stores both the raw access_token and its JWT-encoded form so that
    the Lua rate limiter can resolve the tenant from the Authorization
    header regardless of token encoding.

    Args:
        access_token: The raw access_token UUID from the User table.
        tenant_id: The tenant_id to associate with this token.
    """
    redis = _get_redis()
    if not redis:
        return False

    try:
        # Store raw access_token -> tenant_id
        redis.set(f"{_TOKEN_KEY_PREFIX}{access_token}", tenant_id, ex=_RATELIMIT_KEY_TTL)

        # Store JWT-encoded access_token -> tenant_id
        # NOTE: Must use URLSafeTimedSerializer to match User.get_id()
        # which imports `from itsdangerous.url_safe import URLSafeTimedSerializer as Serializer`
        from itsdangerous.url_safe import URLSafeTimedSerializer
        from common import settings as _settings
        jwt_serializer = URLSafeTimedSerializer(secret_key=_settings.get_secret_key())
        jwt_token = jwt_serializer.dumps(str(access_token))
        if isinstance(jwt_token, bytes):
            jwt_token = jwt_token.decode("utf-8")
        redis.set(f"{_TOKEN_KEY_PREFIX}{jwt_token}", tenant_id, ex=_RATELIMIT_KEY_TTL)

        return True
    except Exception as e:
        logging.warning(f"sync_session_token failed: {e}")
        return False


def sync_jwt_to_redis(jwt_token: str, tenant_id: str) -> bool:
    """
    Sync the exact JWT token (as returned by User.get_id()) to Redis.

    This must be called AFTER get_id() because URLSafeTimedSerializer.dumps()
    includes a timestamp and produces a unique token each call.  We store the
    exact JWT string that will be sent to the frontend so the Lua rate limiter
    can look it up directly.

    Args:
        jwt_token: The JWT string from User.get_id() (sent in Authorization header).
        tenant_id: The tenant_id to associate with this token.
    """
    redis = _get_redis()
    if not redis:
        return False

    try:
        redis.set(f"{_TOKEN_KEY_PREFIX}{jwt_token}", tenant_id, ex=_RATELIMIT_KEY_TTL)
        logging.debug(f"sync_jwt_to_redis: stored JWT -> {tenant_id}")
        return True
    except Exception as e:
        logging.warning(f"sync_jwt_to_redis failed: {e}")
        return False


def remove_api_token(token: str) -> bool:
    """Remove an API token mapping from Redis."""
    redis = _get_redis()
    if not redis:
        return False

    key = f"{_TOKEN_KEY_PREFIX}{token}"
    try:
        redis.delete(key)
        return True
    except Exception as e:
        logging.warning(f"remove_api_token failed: {e}")
        return False


def remove_tenant_rate_limit(tenant_id: str) -> bool:
    """Remove a tenant's rate limit config and all bucket state from Redis."""
    redis = _get_redis()
    if not redis:
        return False

    key = f"{_RATELIMIT_KEY_PREFIX}{tenant_id}"
    try:
        redis.delete(key)
        # Clean up per-minute bucket
        redis.delete(f"billing:tb:min:{tenant_id}")
        return True
    except Exception as e:
        logging.warning(f"remove_tenant_rate_limit failed: {e}")
        return False


def sync_all_rate_limits() -> int:
    """
    Full sync: load all tenants and their API tokens into Redis.

    Call this on application startup and periodically (e.g., every hour).

    Returns the number of tenants synced.
    """
    from api.db.db_models import APIToken, Tenant

    redis = _get_redis()
    if not redis:
        logging.warning("sync_all_rate_limits: Redis not available, skipping")
        return 0

    count = 0

    try:
        # Sync all API tokens -> tenant_id mappings
        tokens = APIToken.select(APIToken.token, APIToken.tenant_id).dicts()
        pipe = redis.pipeline()
        token_count = 0
        for row in tokens:
            key = f"{_TOKEN_KEY_PREFIX}{row['token']}"
            pipe.set(key, row["tenant_id"], ex=_RATELIMIT_KEY_TTL)
            token_count += 1
            if token_count % 500 == 0:
                pipe.execute()
                pipe = redis.pipeline()

        if token_count % 500 != 0:
            pipe.execute()
        logging.info(f"sync_all_rate_limits: synced {token_count} API token mappings")

        # Sync all User session tokens (access_token) -> tenant_id mappings.
        # The frontend sends JWT-encoded access_token in the Authorization header.
        # Lua receives the JWT string, so we store both raw UUID and JWT-encoded form.
        from api.db.db_models import User, UserTenant
        # NOTE: Must use URLSafeTimedSerializer to match User.get_id()
        from itsdangerous.url_safe import URLSafeTimedSerializer
        from common import settings as _settings

        jwt_serializer = URLSafeTimedSerializer(secret_key=_settings.get_secret_key())
        session_token_count = 0
        session_skip_no_token = 0
        session_skip_no_tenant = 0
        users = User.select(User.id, User.access_token).where(User.status == "1").dicts()
        pipe = redis.pipeline()
        for u in users:
            if not u["access_token"] or not u["access_token"].strip():
                session_skip_no_token += 1
                continue
            # Look up the user's default tenant_id via UserTenant
            ut = UserTenant.select(UserTenant.tenant_id).where(
                UserTenant.user_id == u["id"],
                UserTenant.status == "1"
            ).dicts().first()
            if not ut:
                session_skip_no_tenant += 1
                continue
            # Store raw access_token -> tenant_id (for API-token style matching)
            key_raw = f"{_TOKEN_KEY_PREFIX}{u['access_token']}"
            pipe.set(key_raw, ut["tenant_id"], ex=_RATELIMIT_KEY_TTL)
            # Store JWT-encoded access_token -> tenant_id (for session token matching)
            jwt_token = jwt_serializer.dumps(str(u["access_token"]))
            if isinstance(jwt_token, bytes):
                jwt_token = jwt_token.decode("utf-8")
            key_jwt = f"{_TOKEN_KEY_PREFIX}{jwt_token}"
            pipe.set(key_jwt, ut["tenant_id"], ex=_RATELIMIT_KEY_TTL)
            session_token_count += 1
            if session_token_count % 500 == 0:
                pipe.execute()
                pipe = redis.pipeline()

        if session_token_count % 500 != 0:
            pipe.execute()
        logging.info(
            f"sync_all_rate_limits: synced {session_token_count} session token mappings "
            f"(skipped: {session_skip_no_token} no_token, {session_skip_no_tenant} no_tenant)"
        )

        # Sync rate limits for ALL tenants with active/trialing subscriptions
        from api.db.db_models import Subscription
        subscriptions = (
            Subscription
            .select(Subscription.tenant_id, Subscription.plan_name, Subscription.end_time)
            .where(Subscription.subscription_status.in_(["active", "trialing"]))
            .dicts()
        )

        tenant_ids_seen = set()
        for sub in subscriptions:
            tid = sub["tenant_id"]
            if tid in tenant_ids_seen:
                continue
            tenant_ids_seen.add(tid)
            period_end = _to_unix_ts(sub.get("end_time"))
            sync_tenant_rate_limit(tid, sub["plan_name"], period_end=period_end)
            count += 1

        # Also sync tenants that have no subscription — apply default (Trial) limits
        # so that Lua-layer rate limiting covers all API users, not just subscribers.
        all_tenants = Tenant.select(Tenant.id).dicts()
        for t in all_tenants:
            tid = t["id"]
            if tid not in tenant_ids_seen:
                tenant_ids_seen.add(tid)
                sync_tenant_rate_limit(tid, None)
                count += 1

        logging.info(
            f"sync_all_rate_limits: synced {count} tenant rate limit configs"
        )

    except Exception as e:
        logging.exception(f"sync_all_rate_limits failed: {e}")

    return count


def start_periodic_sync(interval: int = 3600) -> None:
    """
    Start a background daemon thread that periodically calls sync_all_rate_limits().

    This ensures rate-limit configs stay fresh even if plan configs change
    or new tenants are added outside of webhook events.

    Args:
        interval: seconds between full syncs (default 3600 = 1 hour)
    """
    global _sync_thread
    if _sync_thread is not None and _sync_thread.is_alive():
        logging.info("billing_rate_limit_sync: periodic sync already running")
        return

    def _loop():
        while not _sync_stop_event.is_set():
            _sync_stop_event.wait(timeout=interval)
            if _sync_stop_event.is_set():
                break
            try:
                logging.info("billing_rate_limit_sync: running periodic full sync")
                sync_all_rate_limits()
            except Exception as e:
                logging.warning(f"billing_rate_limit_sync: periodic sync failed: {e}")

    _sync_thread = threading.Thread(target=_loop, daemon=True, name="billing-rl-sync")
    _sync_thread.start()
    logging.info(f"billing_rate_limit_sync: periodic sync started (interval={interval}s)")


def stop_periodic_sync() -> None:
    """Stop the periodic sync thread."""
    _sync_stop_event.set()
