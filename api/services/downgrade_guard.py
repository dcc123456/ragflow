#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
#
"""Downgrade quota guard — detects and cancels scheduled downgrades that
would exceed the post-downgrade resource limits.

Two layers of defense:
  1. Daily full scan  — warn (>72h) or add to Redis pool (≤72h)
  2. High-freq check  — check pool tenants, cancel if exceeded
"""

import asyncio
import logging
import os
import threading
from datetime import datetime, timezone, timedelta

from api.db.services.billing_service import SubscriptionService, ProductService
from api.utils.billing import cancel_scheduled_subscription_change_async
from rag.utils.redis_conn import REDIS_CONN, RedisDistributedLock

logger = logging.getLogger(__name__)

BEIJING_TZ = timezone(timedelta(hours=8))

REDIS_POOL_KEY = "downgrade:high_freq_pool"
REDIS_LOCK_KEY = "downgrade:daily_scan_lock"
REDIS_DATE_KEY = "downgrade:last_scan_date"
REDIS_WARN_KEY = "downgrade:warn"  # suffix :{tenant_id}, TTL 7 days

CHECK_LOCK_TTL = 600  # 10 min per-tenant lock during high-freq check
WARN_RATE_LIMIT_SEC = 7 * 24 * 3600  # 7 days between warning emails
DOWNGRADE_GUARD_TEST_EMAIL = os.environ.get("DOWNGRADE_GUARD_TEST_EMAIL", "")

# Simple thread-safe metrics (no external dependency required)
_metrics_lock = threading.Lock()
METRICS = {
    "daily_scan_total": 0,
    "cancellations_total": 0,
    "webhook_violations_total": 0,
}


def _inc_metric(name: str, delta: int = 1) -> None:
    with _metrics_lock:
        METRICS[name] = METRICS.get(name, 0) + delta


_last_check_round_at: str = ""


def get_metrics() -> dict:
    """Return a snapshot of current metric values."""
    with _metrics_lock:
        result = dict(METRICS)
    result["last_check_round_at"] = _last_check_round_at
    return result


def _beijing_now() -> datetime:
    return datetime.now(BEIJING_TZ)


def _seconds_until_end_of_day(now: datetime) -> int:
    end_of_day = now.replace(hour=23, minute=59, second=59, microsecond=0)
    return int((end_of_day - now).total_seconds()) + 1


def _to_timestamp(value) -> float | None:
    if value is None:
        return None
    if isinstance(value, datetime):
        return value.timestamp()
    if isinstance(value, (int, float)):
        return float(value)
    return None


def _plan_display_name(name: str) -> str:
    return (name or "").title()


def _format_storage(num_bytes: int) -> str:
    if num_bytes <= 0:
        return "0 GB"
    gb = num_bytes / (1000 ** 3)
    if gb >= 1:
        return f"{gb:.2f} GB"
    mb = num_bytes / (1000 ** 2)
    return f"{mb:.2f} MB"


class DowngradeGuard:
    """Background daemon that scans for scheduled downgrades whose
    post-downgrade quota would be exceeded by current resource usage.

    Three daemon threads:
      - run_daily_scan      — daily full scan (any tick, no time window)
      - run_high_freq_check — periodic check of Redis pool tenants
      - run_cleanup         — remove stale pool entries
    """

    def __init__(self):
        self._stop_event = threading.Event()

    # ------------------------------------------------------------------
    # daily scan
    # ------------------------------------------------------------------

    def run_daily_scan(self) -> None:
        """Daemon thread: tick every 10 min. Execute once per day.
        No time window — if today's scan hasn't run yet, any tick can do it.
        """
        while not self._stop_event.is_set():
            self._stop_event.wait(600)
            now = _beijing_now()
            today = now.strftime("%Y-%m-%d")

            if REDIS_CONN.get(REDIS_DATE_KEY) == today:
                continue  # already done today

            lock = RedisDistributedLock(REDIS_LOCK_KEY, timeout=1800)
            if lock.acquire():
                try:
                    asyncio.run(self._do_daily_scan())
                    _inc_metric("daily_scan_total")
                    ttl = _seconds_until_end_of_day(now)
                    REDIS_CONN.set(REDIS_DATE_KEY, today, exp=ttl)
                except Exception:
                    logger.exception("Daily downgrade scan failed")
                finally:
                    lock.release()

    async def _do_daily_scan(self) -> None:
        """Run a full scan of all tenants with scheduled downgrades."""
        rows = list(_query_scheduled_downgrades())
        if not rows:
            logger.debug("Daily scan: no scheduled downgrades found")
            return

        now_ts = _beijing_now().timestamp()
        logger.info("Daily scan: %d candidate(s)", len(rows))

        for row in rows:
            try:
                await self._process_one(row, now_ts)
            except Exception:
                logger.exception("Daily scan failed for tenant %s", row.get("tenant_id"))

    async def _process_one(self, row: dict, now_ts: float) -> None:
        """Process one tenant: check quota, decide warn vs. add to pool."""
        tenant_id = row["tenant_id"]
        end_ts = _to_timestamp(row.get("end_time"))
        if end_ts is None or end_ts <= 0:
            return

        seconds_until_end = end_ts - now_ts
        if seconds_until_end <= 72 * 3600:
            # ≤ 72h: add to high-freq pool regardless of current usage
            REDIS_CONN.sadd(REDIS_POOL_KEY, tenant_id)
            logger.info(
                "Daily scan: added tenant %s to high-freq pool (%.1f h until end)",
                tenant_id, seconds_until_end / 3600,
            )
            return

        # > 72h: check quota, warn if exceeded
        usage, product = _load_tenant_data(tenant_id, row)
        if usage is not None and product is not None:
            target_addon = _compute_target_addon(row)
            exceed_info = _check_quota_exceeded(usage, product, target_addon)
            if exceed_info is not None:
                await _send_warning_email(tenant_id, row, exceed_info, end_ts)

    # ------------------------------------------------------------------
    # high-freq check
    # ------------------------------------------------------------------

    def run_high_freq_check(self) -> None:
        """Daemon thread: check pool tenants periodically with per-tenant
        distributed lock for multi-worker safety."""
        interval = int(os.environ.get("DOWNGRADE_CHECK_INTERVAL_SEC", 900))
        while not self._stop_event.is_set():
            self._stop_event.wait(interval)
            if self._stop_event.is_set():
                break
            try:
                asyncio.run(self._do_high_freq_check())
            except Exception:
                logger.exception("High-freq downgrade check failed")

    async def _do_high_freq_check(self) -> None:
        """Iterate pool tenants, check quota, cancel if exceeded."""
        members = REDIS_CONN.smembers(REDIS_POOL_KEY)
        if not members:
            return

        global _last_check_round_at
        _last_check_round_at = _beijing_now().isoformat()

        logger.debug("High-freq check: %d tenant(s) in pool", len(members))
        for tid_raw in members:
            tid = tid_raw.decode("utf-8") if isinstance(tid_raw, bytes) else tid_raw
            try:
                lock_key = f"downgrade:check:{tid}"
                lock = RedisDistributedLock(lock_key, timeout=CHECK_LOCK_TTL)
                if not lock.acquire():
                    continue  # another worker is handling this tenant
                try:
                    await _check_and_cancel_if_needed(tid)
                finally:
                    lock.release()
            except Exception:
                logger.exception("High-freq check failed for tenant %s", tid)

    # ------------------------------------------------------------------
    # cleanup
    # ------------------------------------------------------------------

    def run_cleanup(self) -> None:
        """Daemon thread: remove stale pool entries (end_time passed)."""
        while not self._stop_event.is_set():
            self._stop_event.wait(7200)  # every 2 hours
            if self._stop_event.is_set():
                break
            try:
                _cleanup_pool()
            except Exception:
                logger.exception("Cleanup failed")


# ======================================================================
# startup test email
# ======================================================================

async def _send_smtp_test_email() -> None:
    """Send a test email on daemon startup to verify SMTP connectivity."""
    to = DOWNGRADE_GUARD_TEST_EMAIL.strip()
    if not to:
        return
    from api.apps import app
    from api.db.joint_services.mail_service import send_email_html
    try:
        async with app.app_context():
            await send_email_html(
                to_email=to,
                subject="Downgrade Guard Daemon Started",
                template_key="downgrade_startup_test",
            )
        logger.info("Sent startup test email to %s", to)
    except Exception:
        logger.warning("Failed to send startup test email to %s", to)


def send_startup_test_email() -> None:
    """Synchronous entry point for startup code (ragflow_server.py / wsgi.py)."""
    try:
        asyncio.run(_send_smtp_test_email())
    except Exception:
        pass


# ======================================================================
# module-level helpers
# ======================================================================

def _query_scheduled_downgrades():
    """Yield dict rows for every subscription that has a pending downgrade."""
    return SubscriptionService.get_scheduled_downgrades()


def _compute_target_addon(sub: dict) -> int:
    """Pure: return the post-downgrade storage addon in bytes."""
    if (
        sub.get("target_storage_bytes") is not None
        and sub.get("addon_storage_bytes") is not None
        and sub["target_storage_bytes"] < sub["addon_storage_bytes"]
    ):
        return sub["target_storage_bytes"]
    return sub.get("addon_storage_bytes") or 0


def _check_quota_exceeded(usage: dict, product: dict, target_addon: int) -> dict | None:
    """Pure: compare usage dict against product quotas + addon.
    Returns a dict of exceeded dimensions, or None if all within limits.
    """
    storage_used = usage.get("num_storage_bytes", 0) or 0
    members_used = usage.get("num_members", 0) or 0
    apps_used = usage.get("num_apps", 0) or 0

    total_storage = (product.get("quota_storage", 0) or 0) + target_addon
    quota_members = product.get("quota_members", 0) or 0
    quota_apps = product.get("quota_apps", 0) or 0

    exceeded = {}
    if storage_used > total_storage:
        exceeded["storage_used"] = storage_used
        exceeded["storage_limit"] = total_storage
    if members_used > quota_members:
        exceeded["members_used"] = members_used
        exceeded["member_limit"] = quota_members
    if apps_used > quota_apps:
        exceeded["apps_used"] = apps_used
        exceeded["app_limit"] = quota_apps

    return exceeded if exceeded else None


def _load_tenant_data(tenant_id: str, sub: dict) -> tuple[dict | None, dict | None]:
    """IO: fetch current usage and target product for a tenant.
    Returns (usage_dict, product_dict).  Either may be None on failure.
    """
    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id, require_quota_info=True)
    if not tenant_plan:
        return None, None

    name = sub.get("target_plan_name") or sub.get("plan_name") or ""
    product = ProductService.get_by_name(name)
    if not product:
        logger.warning("Product not found for plan %s, tenant %s", name, tenant_id)
        return tenant_plan, None

    return tenant_plan, product


async def _send_guard_email(
    tenant_id: str, sub: dict, exceed_info: dict, *,
    template_key: str, subject: str,
    rate_limit_sec: int = 0,
    **extra_context,
) -> bool:
    """Send a downgrade guard notification email."""
    if rate_limit_sec:
        key = f"{REDIS_WARN_KEY}:{tenant_id}"
        if REDIS_CONN.get(key):
            logger.debug("Guard email rate-limited for tenant %s", tenant_id)
            return False

    from api.apps import app
    from api.db.joint_services.mail_service import send_email_html

    owner = _get_tenant_owner(tenant_id)
    if not owner or not owner.get("email"):
        return False

    logger.debug(
        "Sending guard email: to=%s subject=%s template=%s "
        "plan=%s target_plan=%s storage=%s/%s members=%d/%d apps=%d/%d",
        owner["email"], subject, template_key,
        _plan_display_name(sub.get("plan_name", "")),
        _plan_display_name(sub.get("target_plan_name") or sub.get("plan_name", "")),
        _format_storage(exceed_info.get("storage_used", 0) or 0),
        _format_storage(exceed_info.get("storage_limit", 0) or 0),
        exceed_info.get("members_used", 0) or 0,
        exceed_info.get("member_limit", 0) or 0,
        exceed_info.get("apps_used", 0) or 0,
        exceed_info.get("app_limit", 0) or 0,
    )

    async with app.app_context():
        await send_email_html(
            to_email=owner["email"],
            subject=subject,
            template_key=template_key,
            nickname=owner.get("nickname", ""),
            current_plan=_plan_display_name(sub.get("plan_name", "")),
            target_plan=_plan_display_name(
                sub.get("target_plan_name") or sub.get("plan_name", "")
            ),
            current_storage=_format_storage(exceed_info.get("storage_used", 0) or 0),
            target_storage=_format_storage(exceed_info.get("storage_limit", 0) or 0),
            current_members=exceed_info.get("members_used", 0) or 0,
            target_members=exceed_info.get("member_limit", 0) or 0,
            current_apps=exceed_info.get("apps_used", 0) or 0,
            target_apps=exceed_info.get("app_limit", 0) or 0,
            **extra_context,
        )
    if rate_limit_sec:
        REDIS_CONN.set(key, "1", exp=rate_limit_sec)
    return True


async def _send_warning_email(
    tenant_id: str, sub: dict, exceed_info: dict, end_ts: float
) -> None:
    """Send a daily warning email with 7-day rate limiting."""
    remaining_days = max(1, int((end_ts - _beijing_now().timestamp()) / 86400))
    downgrade_date = datetime.fromtimestamp(end_ts, tz=BEIJING_TZ).strftime("%Y-%m-%d")
    sent = await _send_guard_email(
        tenant_id, sub, exceed_info,
        template_key="downgrade_warning",
        subject="Downgrade Warning — Resource Usage Exceeds Target Quota",
        rate_limit_sec=WARN_RATE_LIMIT_SEC,
        remaining_days=remaining_days,
        downgrade_date=downgrade_date,
    )
    if sent:
        logger.info("Sent daily warning email to tenant %s", tenant_id)


def _get_tenant_owner(tenant_id: str) -> dict | None:
    """Return {email, nickname} for the first tenant owner, or None."""
    from api.db.db_models import User
    from api.db.services.user_service import UserTenantService
    from api.db import UserTenantRole

    try:
        ut_model = UserTenantService.model
        row = (
            ut_model.select(User.email, User.nickname)
            .join(User, on=(ut_model.user_id == User.id))
            .where(
                (ut_model.tenant_id == tenant_id)
                & (ut_model.role == UserTenantRole.OWNER)
            )
            .order_by(ut_model.create_time.asc())
            .limit(1)
            .dicts()
            .first()
        )
        return row
    except Exception:
        logger.exception("Failed to lookup owner for tenant %s", tenant_id)
        return None


async def _check_and_cancel_if_needed(tenant_id: str) -> None:
    """Check one tenant: if exceeded, cancel the downgrade."""
    sub = _get_subscription_downgrade_info(tenant_id)
    if sub is None:
        REDIS_CONN.srem(REDIS_POOL_KEY, tenant_id)
        return

    usage, product = _load_tenant_data(tenant_id, sub)
    if usage is None or product is None:
        REDIS_CONN.srem(REDIS_POOL_KEY, tenant_id)
        logger.warning(
            "Removed tenant %s from pool: unable to load data (usage=%s, product=%s)",
            tenant_id, usage is not None, product is not None,
        )
        return

    target_addon = _compute_target_addon(sub)
    exceed_info = _check_quota_exceeded(usage, product, target_addon)
    if exceed_info is None:
        return  # within limits, keep in pool

    await _cancel_downgrade(tenant_id, sub, exceed_info)


async def _cancel_downgrade(tenant_id: str, sub: dict, exceed_info: dict) -> None:
    """Cancel a scheduled downgrade: release Stripe schedule, clear DB, notify."""
    # Race condition guard: if end_time already passed, downgrade took effect
    end_ts = _to_timestamp(sub.get("end_time"))
    if end_ts is not None and end_ts <= _beijing_now().timestamp():
        logger.info("Skip cancel for tenant %s: end_time already passed", tenant_id)
        REDIS_CONN.srem(REDIS_POOL_KEY, tenant_id)
        return

    # 1. Release Stripe SubscriptionSchedule.  Wrap in try/except so a
    #    Stripe API failure doesn't block the rest of the cancel flow.
    stripe_sub_id = (sub.get("subscription_id") or "").strip()
    if stripe_sub_id:
        try:
            await cancel_scheduled_subscription_change_async(stripe_sub_id)
        except Exception:
            logger.exception(
                "Failed to release Stripe schedule for tenant %s; "
                "continuing with DB cleanup", tenant_id,
            )

    # 2. Clear DB downgrade markers
    _clear_downgrade_markers(tenant_id, sub)

    # 3. ERROR log — downgrade cancellation is an anomaly
    logger.error(
        "Downgrade CANCELLED for tenant %(tid)s: "
        "plan=%(plan)s target_plan=%(tplan)s "
        "storage_used=%(s_used)d storage_limit=%(s_lim)d "
        "members=%(m)d member_limit=%(m_lim)d "
        "apps=%(a)d app_limit=%(a_lim)d "
        "end_time=%(end)s reason=usage_exceeds_target_quota",
        {
            "tid": tenant_id,
            "plan": sub.get("plan_name", ""),
            "tplan": sub.get("target_plan_name") or sub.get("plan_name", ""),
            "s_used": exceed_info.get("storage_used", 0) or 0,
            "s_lim": exceed_info.get("storage_limit", 0) or 0,
            "m": exceed_info.get("members_used", 0) or 0,
            "m_lim": exceed_info.get("member_limit", 0) or 0,
            "a": exceed_info.get("apps_used", 0) or 0,
            "a_lim": exceed_info.get("app_limit", 0) or 0,
            "end": str(sub.get("end_time", "")),
        },
    )

    # 4. Send cancellation email
    await _send_cancelled_email(tenant_id, sub, exceed_info)

    # 5. Remove from Redis pool
    REDIS_CONN.srem(REDIS_POOL_KEY, tenant_id)

    _inc_metric("cancellations_total")


def _clear_downgrade_markers(tenant_id: str, sub: dict) -> None:
    """Clear target_plan_name and reset target_storage_bytes to current value."""
    updates = {}
    if sub.get("target_plan_name") and sub["target_plan_name"] != sub.get("plan_name"):
        updates["target_plan_name"] = None
    if (
        sub.get("target_storage_bytes") is not None
        and sub.get("addon_storage_bytes") is not None
        and sub["target_storage_bytes"] < sub["addon_storage_bytes"]
    ):
        updates["target_storage_bytes"] = sub["addon_storage_bytes"]

    if updates:
        from api.db.db_models import DB
        with DB.atomic():
            SubscriptionService.update_subscription(tenant_id, updates)
        logger.info("Cleared downgrade markers for tenant %s: %s", tenant_id, list(updates.keys()))


def _get_subscription_downgrade_info(tenant_id: str) -> dict | None:
    """Return the subscription dict if a downgrade is still pending, else None."""
    try:
        row = SubscriptionService.get_raw_by_tenant_id(tenant_id)
    except Exception:
        logger.exception("Failed to query subscription for tenant %s", tenant_id)
        return None

    if row is None:
        return None

    has_plan = (
        row.get("target_plan_name")
        and row["target_plan_name"] != row.get("plan_name")
    )
    has_storage = (
        row.get("target_storage_bytes") is not None
        and row.get("addon_storage_bytes") is not None
        and row["target_storage_bytes"] < row["addon_storage_bytes"]
    )
    return row if (has_plan or has_storage) else None


async def _send_cancelled_email(
    tenant_id: str, sub: dict, exceed_info: dict
) -> None:
    """Notify the tenant owner that their downgrade has been cancelled."""
    try:
        await _send_guard_email(
            tenant_id, sub, exceed_info,
            template_key="downgrade_cancelled",
            subject="Your Scheduled Downgrade Has Been Cancelled",
        )
        logger.info("Sent cancellation email to tenant %s", tenant_id)
    except Exception:
        logger.exception(
            "Failed to send cancellation email to tenant %s", tenant_id,
        )


def check_downgrade_effective_exceeded(
    tenant_id: str,
    pre_sync_sub: dict | None,
    plan_changed: bool = False,
) -> dict | None:
    """Called from the webhook handler after a plan/storage sync completes.

    Detects whether a scheduled downgrade just became effective and the
    tenant's current resource usage exceeds the post-downgrade quota.

    Args:
        tenant_id: Target tenant.
        pre_sync_sub: Subscription row before Stripe sync ran.
        plan_changed: True when the Stripe price_id differs from the
            pre-sync ``price_id``. Required for plan-downgrade detection
            but not for pure storage downgrades.

    Returns:
        A dict of exceeded dimensions (same shape as ``_check_quota_exceeded``)
        if a downgrade is effective AND quota is exceeded, or ``None`` otherwise.
    """
    if not pre_sync_sub:
        return None

    old_plan = (pre_sync_sub.get("plan_name") or "").strip()
    old_addon = int(pre_sync_sub.get("addon_storage_bytes") or 0)
    pre_sync_target_plan = pre_sync_sub.get("target_plan_name")
    pre_sync_target_storage = pre_sync_sub.get("target_storage_bytes")

    # Plan downgrade effective: Stripe price actually changed AND the
    # post-sync plan_name matches the scheduled target_plan_name.
    is_plan_downgrade = (
        plan_changed
        and bool(pre_sync_target_plan)
        and old_plan
        and old_plan != pre_sync_target_plan
    )

    # Pure storage downgrade: target additive is lower than current addon.
    # Does NOT require plan_changed because a pure storage downgrade (e.g.
    # 40 GB add-on → 20 GB add-on) keeps the same base plan price_id.
    is_storage_downgrade = (
        pre_sync_target_storage is not None
        and old_addon > 0
        and pre_sync_target_storage < old_addon
    )

    if not is_plan_downgrade and not is_storage_downgrade:
        return None

    usage, product = _load_tenant_data(tenant_id, pre_sync_sub)
    if usage is None or product is None:
        return None

    target_addon = _compute_target_addon(pre_sync_sub)
    return _check_quota_exceeded(usage, product, target_addon)


def _cleanup_pool() -> None:
    """Remove tenants whose end_time has already passed from the Redis pool."""
    members = REDIS_CONN.smembers(REDIS_POOL_KEY)
    if not members:
        return
    now_ts = _beijing_now().timestamp()
    removed = 0
    for tid in members:
        if isinstance(tid, bytes):
            tid = tid.decode("utf-8")
        try:
            row = SubscriptionService.get_raw_by_tenant_id(tid)
            if row is None:
                REDIS_CONN.srem(REDIS_POOL_KEY, tid)
                removed += 1
                continue
            end_ts = _to_timestamp(row.get("end_time"))
            if end_ts is not None and end_ts <= now_ts:
                REDIS_CONN.srem(REDIS_POOL_KEY, tid)
                removed += 1
                logger.info("Cleanup removed tenant %s (period ended)", tid)
        except Exception:
            logger.exception("Cleanup failed for tenant %s", tid)
    if removed:
        logger.info("Cleanup: removed %d tenant(s)", removed)
