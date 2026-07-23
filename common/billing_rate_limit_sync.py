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

Rules:
    1. API Key and BETA key are allowed only for Starter and Pro User
    2. Trial 10 RPM; Starter: 100 RPM; Pro: 1000 RPM

How:
    1. During APP start, Sync all Starter/Pro API Key and BETA key rate limit to Redis, TTL as forever.
    2. During login, Sync Starter/Pro encoded "access_token" to Redis, TTL as 1 day
    3. During billing changed/API token update, update starter/Pro API Key and BETA key, TTL as forever.

NGINX lua script:
    if Authorization token has '.' in it, it is a jwt token
        get rid of possible prefix case-insenstive 'Bearer ' get the jwt token
        then split the rest with '.', get the first part, it is encoded "access_token"
            the encoded access_token has been stored previously with login with TTL as 1 day format as: 'billing:ratelimit:token:<encoded_access_token>'
        go to redis check if in cache:
            if yes, use the rate limit from the key
            if no, fallback default 10 rpm for Free user
    else, it is a API token or BETA token
        get rid of possible prefix case-insenstive 'Bearer ', get the api_or_beta_token
            the 'api_or_beta_token' has been stored previously during APP start as: 'billing:ratelimit:token:<api_or_beta_token>'
        go to redis check if in cache:
            if yes, use the rate limit from the key
            if no, fallback to default 10 rpm for Free user

Token as below:
    (Note: JWT token first part is Base64 encoded "access_token" without trailing padding)
    access_token: "b078caee85a411f1bd01d962482a88eb"
    Authorization: "Bearer ImIwNzhjYWVlODVhNDExZjFiZDAxZDk2MjQ4MmE4OGViIg.amB6xA.JwROU9lUz_rWx5c13sxpN2ejsI4"
        API Token: "ragflow-0vpQVikOOyLSwUbpA_B6NeYN_8tr2_6HWPzx_z44iYk"
    Beta Token: "I4NDRfxVZJMbNBdckeGfeu7qKYWmJahO"
"""

import base64
import json
import logging
import time
from api.db.services.api_service import APITokenService
from rag.utils.redis_conn import REDIS_CONN


# Redis key patterns
_API_AUTH_PLAN_KEY_PREFIX = "billing:api_auth_plan:"
_API_AUTH_ALLOWED_PLANS = {"starter", "pro"}
_API_AUTH_PLAN_KEY_TTL = 86400

_RATE_LIMIT_TOKEN_KEY_PREFIX = "billing:ratelimit:token:"
_RATE_LIMIT_TOKEN_KEY_TTL = 86400

_RATE_LIMIT_API_TOKEN_PLANS = ["starter", "pro"]

_DEFAULT_RPM = 10
_PLANS = ["trial", "starter", "pro"]
_DEFAULT_PLAN = "trial"
_cached_plans_rate_limits = None
_cached_plans_rate_limits_ts = 0
_CACHED_PLANS_RATE_LIMITS_TTL = 3600


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
            host=host,
            port=port,
            db=target_db,
            password=password,
            decode_responses=True,
        )
        _cached_billing_redis.ping()
        logging.info(f"billing_rate_limit_sync: connected to Redis db={target_db}")
        return _cached_billing_redis
    except Exception as e:
        logging.warning(f"billing_rate_limit_sync: failed to connect to Redis db={target_db}: {e}")
        return None


def _get_plan_rate_limit(plan_name: str | None) -> int:
    """
    Get the cached rate limits for "Trial", "Starter", "Pro"
    """
    global _cached_plans_rate_limits
    global _cached_plans_rate_limits_ts
    if _cached_plans_rate_limits is not None:
        if time.time() - _cached_plans_rate_limits_ts < _CACHED_PLANS_RATE_LIMITS_TTL:
            return _cached_plans_rate_limits.get((plan_name or "").lower(), _DEFAULT_RPM)
        _cached_plans_rate_limits = None

    from api.db.services.billing_service import ProductService

    _cached_plans_rate_limits = {}
    for name in _PLANS:
        plan = ProductService.get_by_name(name)
        if not plan:
            plan = ProductService.get_by_name("Trial")

        if plan:
            rpm = plan.get("api_request_limit_per_minute") or _DEFAULT_RPM
        else:
            # MySQL has no data at all — fall back to in-memory config
            from common import settings

            info = settings.BILLING_PLAN_TO_INFO.get(name) or settings.BILLING_PLAN_TO_INFO.get("Trial") or {}
            rpm = info.get("api_request_limit_per_minute", _DEFAULT_RPM)

        try:
            rpm = int(rpm)
        except (TypeError, ValueError):
            rpm = _DEFAULT_RPM

        _cached_plans_rate_limits[name] = rpm

    _cached_plans_rate_limits_ts = time.time()
    return _cached_plans_rate_limits.get((plan_name or "").lower(), _DEFAULT_RPM)


def _get_tenant_plan(tenant_id: str) -> str:
    try:
        from api.db.db_models import DB, Subscription

        with DB.connection_context():
            subscription = (
                Subscription.select(
                    Subscription.plan_name,
                    Subscription.subscription_status,
                )
                .where(Subscription.tenant_id == tenant_id)
                .order_by(Subscription.getter_by("create_time").desc())
                .dicts()
                .first()
            )
        if subscription:
            if (subscription.get("subscription_status") or "").lower() != "active":
                return _DEFAULT_PLAN
            return subscription.get("plan_name") or _DEFAULT_PLAN
    except Exception as e:
        logging.warning(f"_get_tenant_plan SQL lookup failed for {tenant_id}: {e}")
    return _DEFAULT_PLAN


def _encode_access_token(access_token: str) -> str:
    return base64.urlsafe_b64encode(json.dumps(str(access_token)).encode("utf-8")).decode("utf-8").rstrip("=")


def sync_token_rate_limit(tenant_id: str, access_token: str) -> None:
    """
    Writes billing:ratelimit:token:<token> -> rpm where the rpm = requests per minute to Redis, with TTL default 1 day
    """
    from common import settings

    if not settings.BILLING_ENABLED:
        return

    redis = _get_redis()
    if not redis:
        return

    key = f"{_RATE_LIMIT_TOKEN_KEY_PREFIX}{_encode_access_token(access_token)}"
    plan_name = _get_tenant_plan(tenant_id=tenant_id)
    value = _get_plan_rate_limit(plan_name=plan_name)

    try:
        redis.set(key, value, ex=_RATE_LIMIT_TOKEN_KEY_TTL)
        logging.debug(f"sync_token_rate_limit: {key} -> {value}")
    except Exception as e:
        logging.warning(f"sync_token_rate_limit failed for {tenant_id}: {e}")


def sync_api_token_rate_limit(tenant_id: str, api_token: str | None, beta_token: str | None) -> None:
    """
    Writes billing:ratelimit:token:<token> -> rpm where the rpm = requests per minute to Redis, with TTL for ever
    """
    from common import settings

    if not settings.BILLING_ENABLED:
        return

    redis = _get_redis()
    if not redis:
        return

    if not api_token and not beta_token:
        return

    keys = [f"{_RATE_LIMIT_TOKEN_KEY_PREFIX}{token}" for token in (api_token, beta_token) if token]
    plan_name = _get_tenant_plan(tenant_id=tenant_id)
    value = _get_plan_rate_limit(plan_name=plan_name)

    try:
        # Token No TTL for API key
        for key in keys:
            redis.set(key, value)
            logging.info(f"sync_api_token_rate_limit: {tenant_id} -> {key[:16]} -> {value}")
    except Exception as e:
        logging.warning(f"sync_api_token failed for tenant {tenant_id}: {e}")


def delete_api_token_rate_limit(tenant_id: str, api_token: str | None, beta_token: str | None = None) -> None:
    from common import settings

    if not settings.BILLING_ENABLED:
        return

    redis = _get_redis()
    if not redis:
        return

    keys = [f"{_RATE_LIMIT_TOKEN_KEY_PREFIX}{token}" for token in (api_token, beta_token) if token]
    try:
        if keys:
            redis.delete(*keys)
            logging.debug(f"delete_api_token_rate_limit: {tenant_id} -> {len(keys)} token(s)")
    except Exception as e:
        logging.warning(f"delete_api_token_rate_limit failed for {tenant_id}: {e}")


def sync_all_api_token_rate_limit() -> None:
    from common import settings

    if not settings.BILLING_ENABLED:
        return

    try:
        import peewee
        from api.db.db_models import APIToken, DB, Subscription

        with DB.connection_context():
            tokens = (
                APIToken.select(
                    APIToken.tenant_id,
                    APIToken.token,
                    APIToken.beta,
                )
                .join(Subscription, on=(APIToken.tenant_id == Subscription.tenant_id))
                .where((peewee.fn.LOWER(Subscription.plan_name).in_(_RATE_LIMIT_API_TOKEN_PLANS)) & (Subscription.subscription_status == "active"))
                .dicts()
            )

            count = 0
            for token in tokens:
                sync_api_token_rate_limit(
                    tenant_id=token.get("tenant_id"),
                    api_token=token.get("token"),
                    beta_token=token.get("beta"),
                )
                count += 1
        logging.info(f"sync_all_api_token_rate_limit synced {count} API token rows")
    except Exception as e:
        logging.warning(f"sync_all_api_token_rate_limit failed: {e}")


def sync_api_token_rate_limit_for_subscription(tenant_id, plan_name, subscription_status) -> None:
    tokens = APITokenService.query(tenant_id=tenant_id)
    tokens = [t.to_dict() for t in tokens]
    if (plan_name or "").lower() in _RATE_LIMIT_API_TOKEN_PLANS and (subscription_status or "").lower() == "active":
        sync_api_auth_plan(tenant_id, plan_name, subscription_status)
        for token in tokens:
            sync_api_token_rate_limit(
                tenant_id=token.get("tenant_id"),
                api_token=token.get("token"),
                beta_token=token.get("beta"),
            )
    else:
        sync_api_auth_plan(tenant_id, plan_name, subscription_status)
        for token in tokens:
            delete_api_token_rate_limit(
                tenant_id=token.get("tenant_id"),
                api_token=token.get("token"),
                beta_token=token.get("beta"),
            )


def _is_api_auth_plan_allowed(plan_name: str | None, subscription_status: str | None = None) -> bool:
    return (plan_name or "").strip().lower() in _API_AUTH_ALLOWED_PLANS and (subscription_status or "").strip().lower() in {"active"}


def sync_api_auth_plan(tenant_id: str, plan_name: str | None, subscription_status: str | None = None) -> bool:
    """
    Cache whether a tenant is allowed to authenticate with API/Beta keys.

    Only Starter and Pro tenants are stored as positive Redis entries. Missing
    keys intentionally fall back to SQL in the application auth path.
    """
    redis = _get_redis()
    if not redis or not tenant_id:
        return False

    key = f"{_API_AUTH_PLAN_KEY_PREFIX}{tenant_id}"
    try:
        if _is_api_auth_plan_allowed(plan_name, subscription_status):
            redis.set(key, "1", ex=_API_AUTH_PLAN_KEY_TTL)
        else:
            redis.delete(key)
        return True
    except Exception as e:
        logging.warning(f"sync_api_auth_plan failed for {tenant_id}: {e}")
        return False


def is_api_auth_plan_allowed(tenant_id: str) -> bool:
    """
    Return whether API/Beta-key auth is allowed for this tenant.

    Redis is checked first. If the tenant is not cached as Starter/Pro, SQL is
    used as the source of truth and positive results are cached.
    """
    from common import settings

    if not settings.BILLING_ENABLED:
        return True
    if not tenant_id:
        return False

    redis = _get_redis()
    key = f"{_API_AUTH_PLAN_KEY_PREFIX}{tenant_id}"
    if redis:
        try:
            if redis.get(key) == "1":
                return True
        except Exception as e:
            logging.warning(f"is_api_auth_plan_allowed Redis lookup failed for {tenant_id}: {e}")

    try:
        from api.db.db_models import DB, Subscription

        with DB.connection_context():
            subscription = (
                Subscription.select(
                    Subscription.plan_name,
                    Subscription.subscription_status,
                )
                .where(Subscription.tenant_id == tenant_id)
                .order_by(Subscription.getter_by("create_time").desc())
                .dicts()
                .first()
            )
    except Exception as e:
        logging.warning(f"is_api_auth_plan_allowed SQL lookup failed for {tenant_id}: {e}")
        return False

    allowed = bool(subscription) and _is_api_auth_plan_allowed(
        subscription.get("plan_name"),
        subscription.get("subscription_status"),
    )
    if allowed:
        sync_api_auth_plan(
            tenant_id,
            subscription.get("plan_name"),
            subscription.get("subscription_status"),
        )
    elif redis:
        try:
            redis.delete(key)
        except Exception:
            pass
    return allowed
