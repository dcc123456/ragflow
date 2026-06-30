#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
#
"""Unit tests for downgrade_guard — daily scan and quota checking."""

import asyncio
import sys
import types
import warnings
from contextlib import nullcontext
from datetime import datetime, timezone, timedelta
from unittest.mock import MagicMock


warnings.filterwarnings("ignore", category=DeprecationWarning)
warnings.filterwarnings("ignore", category=ResourceWarning)

from api.services.downgrade_guard import (
    DowngradeGuard,
    _check_quota_exceeded,
    _beijing_now,
    check_downgrade_effective_exceeded,
)

BEIJING_TZ = timezone(timedelta(hours=8))


# ------------------------------------------------------------------
# helpers
# ------------------------------------------------------------------

def _sub(**overrides):
    d = {
        "tenant_id": "tenant_1",
        "plan_name": "Pro",
        "target_plan_name": "Trial",
        "addon_storage_bytes": 40 * 1000**3,
        "target_storage_bytes": None,
        "end_time": datetime(2026, 6, 30, tzinfo=BEIJING_TZ),
        "subscription_id": "sub_123",
    }
    d.update(overrides)
    return d


def _product(**kw):
    d = {"name": "Trial", "quota_storage": 10 * 1000**3,
         "quota_members": 5, "quota_apps": 10, "priority": 1, "task_priority": "low"}
    d.update(kw)
    return d


def _fake_async(fn):
    async def wrapper(*a, **kw):
        return fn(*a, **kw)
    return wrapper


def _fake_redis(get_return=None, *, sadd=None, srem=None):
    """Return a mock Redis connection with controllable .get() return value."""
    _s = sadd
    _r = srem
    class FR:
        sadd = _s if _s is not None else MagicMock()
        srem = _r if _r is not None else MagicMock()
        smembers = MagicMock(return_value=[])
        set = MagicMock()
        @staticmethod
        def get(key): return get_return
    return FR


def _patch_app_context():
    mod = sys.modules.get("api.apps")
    if mod is None:
        mod = types.ModuleType("api.apps")
        sys.modules["api.apps"] = mod
    if not hasattr(mod, "app"):
        mod.app = MagicMock()
    mod.app.app_context = lambda: nullcontext()


# ------------------------------------------------------------------
# _check_quota_exceeded
# ------------------------------------------------------------------

class TestCheckQuotaExceeded:
    """Pure function — no mocks needed."""
    USAGE = {"num_storage_bytes": 5*1000**3, "num_members": 3, "num_apps": 5}
    PROD = _product()

    def test_within_limits(self):
        assert _check_quota_exceeded(self.USAGE, self.PROD, target_addon=40*1000**3) is None

    def test_storage_exceeded(self):
        usage = {**self.USAGE, "num_storage_bytes": 51*1000**3}
        prod = _product(quota_storage=10*1000**3)
        r = _check_quota_exceeded(usage, prod, target_addon=40*1000**3)
        assert r is not None
        assert r["storage_used"] == 51*1000**3
        assert r["storage_limit"] == 50*1000**3

    def test_members_exceeded(self):
        usage = {**self.USAGE, "num_members": 20}
        prod = _product(quota_members=5)
        r = _check_quota_exceeded(usage, prod, target_addon=0)
        assert r is not None
        assert r["members_used"] == 20
        assert r["member_limit"] == 5

    def test_apps_exceeded(self):
        usage = {**self.USAGE, "num_apps": 50}
        prod = _product(quota_apps=10)
        r = _check_quota_exceeded(usage, prod, target_addon=0)
        assert r is not None
        assert r["apps_used"] == 50
        assert r["app_limit"] == 10

    def test_multiple_dimensions(self):
        usage = {"num_storage_bytes": 51*1000**3, "num_members": 20, "num_apps": 50}
        r = _check_quota_exceeded(usage, self.PROD, target_addon=40*1000**3)
        assert len(r) == 6

    def test_addon_also_downgrading(self):
        usage = {"num_storage_bytes": 35*1000**3, "num_members": 3, "num_apps": 5}
        prod = _product(quota_storage=10*1000**3)
        r = _check_quota_exceeded(usage, prod, target_addon=20*1000**3)
        assert r is not None
        assert r["storage_limit"] == 30*1000**3

    def test_addon_target_zero(self):
        usage = {"num_storage_bytes": 5*1000**3, "num_members": 3, "num_apps": 5}
        prod = _product(quota_storage=10*1000**3)
        r = _check_quota_exceeded(usage, prod, target_addon=0)
        assert r is None  # 5GB < 10GB + 0

    def test_plan_downgrade_only_no_addon_change(self):
        usage = {"num_storage_bytes": 55*1000**3, "num_members": 3, "num_apps": 5}
        prod = _product(quota_storage=10*1000**3)
        r = _check_quota_exceeded(usage, prod, target_addon=40*1000**3)
        assert r is not None
        assert r["storage_limit"] == 50*1000**3


# ------------------------------------------------------------------
# daily scan: _do_daily_scan
# ------------------------------------------------------------------

class TestDailyScan:
    def setup_method(self):
        _patch_app_context()

    def test_warn_only_gt_72h(self, monkeypatch):
        """Verify send_email_html is called for >72h + exceeded."""
        far = _beijing_now() + timedelta(days=10)
        sub = _sub(end_time=far)

        monkeypatch.setattr(
            "api.services.downgrade_guard._query_scheduled_downgrades", lambda: [sub],
        )
        monkeypatch.setattr(
            "api.services.downgrade_guard._load_tenant_data",
            lambda _tid, _s: ({"num_storage_bytes": 51*1000**3, "num_members": 3, "num_apps": 5}, _product()),
        )
        monkeypatch.setattr(
            "api.services.downgrade_guard._get_tenant_owner",
            lambda _tid: {"email": "test@example.com", "nickname": "test"},
        )
        send_calls = []
        monkeypatch.setattr(
            "api.db.joint_services.mail_service.send_email_html",
            _fake_async(lambda **kw: send_calls.append(kw)),
        )
        monkeypatch.setattr("api.services.downgrade_guard.REDIS_CONN", _fake_redis())

        asyncio.run(DowngradeGuard()._do_daily_scan())
        assert len(send_calls) == 1
        assert send_calls[0]["template_key"] == "downgrade_warning"

    def test_add_to_pool_le_72h(self, monkeypatch):
        soon = _beijing_now() + timedelta(hours=48)
        sub = _sub(end_time=soon)
        monkeypatch.setattr(
            "api.services.downgrade_guard._query_scheduled_downgrades", lambda: [sub],
        )
        _sadd = MagicMock()
        monkeypatch.setattr("api.services.downgrade_guard.REDIS_CONN", _fake_redis(sadd=_sadd))

        asyncio.run(DowngradeGuard()._do_daily_scan())
        _sadd.assert_called()

    def test_no_op_when_exceed_info_none_gt_72h(self, monkeypatch):
        email_calls = []
        far = _beijing_now() + timedelta(days=10)
        sub = _sub(end_time=far)
        monkeypatch.setattr(
            "api.services.downgrade_guard._query_scheduled_downgrades", lambda: [sub],
        )
        monkeypatch.setattr(
            "api.services.downgrade_guard._load_tenant_data",
            lambda _tid, _s: ({"num_storage_bytes": 5*1000**3, "num_members": 3, "num_apps": 5}, _product()),
        )
        monkeypatch.setattr(
            "api.services.downgrade_guard._send_warning_email",
            _fake_async(lambda *a, **kw: email_calls.append(a)),
        )
        _sadd = MagicMock()
        monkeypatch.setattr("api.services.downgrade_guard.REDIS_CONN", _fake_redis(sadd=_sadd))

        asyncio.run(DowngradeGuard()._do_daily_scan())
        assert not email_calls
        assert not _sadd.called


# ------------------------------------------------------------------
# email rate limiting
# ------------------------------------------------------------------

def test_warning_email_rate_limited(monkeypatch):
    """Rate limit: _send_warning_email skips when Redis rate-limit key is set."""
    _patch_app_context()
    far = _beijing_now() + timedelta(days=10)
    sub = _sub(end_time=far)

    monkeypatch.setattr(
        "api.services.downgrade_guard._query_scheduled_downgrades", lambda: [sub],
    )
    monkeypatch.setattr(
        "api.services.downgrade_guard._load_tenant_data",
        lambda _tid, _s: ({"num_storage_bytes": 51*1000**3, "num_members": 3, "num_apps": 5}, _product()),
    )
    monkeypatch.setattr(
        "api.services.downgrade_guard._get_tenant_owner",
        lambda _tid: {"email": "test@example.com", "nickname": "test"},
    )
    send_calls = []
    monkeypatch.setattr(
        "api.db.joint_services.mail_service.send_email_html",
        _fake_async(lambda **kw: send_calls.append(kw)),
    )

    # Round 1: rate limit active
    monkeypatch.setattr("api.services.downgrade_guard.REDIS_CONN", _fake_redis("1"))
    asyncio.run(DowngradeGuard()._do_daily_scan())
    assert not send_calls

    # Round 2: rate limit expired
    monkeypatch.setattr("api.services.downgrade_guard.REDIS_CONN", _fake_redis(None))
    asyncio.run(DowngradeGuard()._do_daily_scan())
    assert len(send_calls) == 1


# ------------------------------------------------------------------
# _get_subscription_downgrade_info
# ------------------------------------------------------------------

class TestGetSubscriptionDowngradeInfo:
    def test_returns_row_when_plan_downgrade(self, monkeypatch):
        monkeypatch.setattr(
            "api.services.downgrade_guard.SubscriptionService.get_raw_by_tenant_id",
            lambda _tid: _sub(),
        )
        from api.services.downgrade_guard import _get_subscription_downgrade_info
        r = _get_subscription_downgrade_info("t1")
        assert r is not None
        assert r["target_plan_name"] == "Trial"

    def test_returns_none_when_no_downgrade(self, monkeypatch):
        monkeypatch.setattr(
            "api.services.downgrade_guard.SubscriptionService.get_raw_by_tenant_id",
            lambda _tid: _sub(target_plan_name="Pro", target_storage_bytes=None, addon_storage_bytes=None),
        )
        from api.services.downgrade_guard import _get_subscription_downgrade_info
        assert _get_subscription_downgrade_info("t1") is None

    def test_returns_row_when_storage_downgrade_only(self, monkeypatch):
        monkeypatch.setattr(
            "api.services.downgrade_guard.SubscriptionService.get_raw_by_tenant_id",
            lambda _tid: _sub(target_plan_name=None, target_storage_bytes=20*1000**3, addon_storage_bytes=40*1000**3),
        )
        from api.services.downgrade_guard import _get_subscription_downgrade_info
        assert _get_subscription_downgrade_info("t1") is not None


# ------------------------------------------------------------------
# _clear_downgrade_markers
# ------------------------------------------------------------------

class TestClearDowngradeMarkers:
    def test_clears_both(self, monkeypatch):
        caps = []
        def _mock_update(tenant_id, updates):
            caps.append(updates)

        from contextlib import nullcontext
        monkeypatch.setattr("api.db.db_models.DB.atomic", lambda: nullcontext())
        monkeypatch.setattr("api.services.downgrade_guard.SubscriptionService.update_subscription", _mock_update)
        from api.services.downgrade_guard import _clear_downgrade_markers
        sub = _sub(target_storage_bytes=20*1000**3, addon_storage_bytes=40*1000**3)
        _clear_downgrade_markers("t1", sub)
        assert caps[0]["target_plan_name"] is None
        assert caps[0]["target_storage_bytes"] == 40 * 1000**3

    def test_plan_only(self, monkeypatch):
        caps = []
        def _mock_update(tenant_id, updates):
            caps.append(updates)

        from contextlib import nullcontext
        monkeypatch.setattr("api.db.db_models.DB.atomic", lambda: nullcontext())
        monkeypatch.setattr("api.services.downgrade_guard.SubscriptionService.update_subscription", _mock_update)
        from api.services.downgrade_guard import _clear_downgrade_markers
        _clear_downgrade_markers("t1", _sub(target_storage_bytes=None, addon_storage_bytes=None))
        assert caps[0]["target_plan_name"] is None
        assert "target_storage_bytes" not in caps[0]


# ------------------------------------------------------------------
# high-freq check
# ------------------------------------------------------------------

class TestHighFreqCheck:
    def test_cancels_when_exceeded(self, monkeypatch):
        cancel_calls = []
        email_calls = []
        sub = _sub(end_time=_beijing_now() + timedelta(days=1))

        monkeypatch.setattr(
            "api.services.downgrade_guard._get_subscription_downgrade_info",
            lambda _tid: sub,
        )
        # Mock only the IO layer; _check_quota_exceeded runs as pure function
        monkeypatch.setattr(
            "api.services.downgrade_guard._load_tenant_data",
            lambda _tid, _s: ({"num_storage_bytes": 51*1000**3, "num_members": 3, "num_apps": 5}, _product()),
        )
        monkeypatch.setattr(
            "api.services.downgrade_guard._clear_downgrade_markers",
            lambda _tid, _s: None,
        )
        monkeypatch.setattr(
            "api.services.downgrade_guard.cancel_scheduled_subscription_change_async",
            _fake_async(lambda _sid: cancel_calls.append(_sid)),
        )
        monkeypatch.setattr(
            "api.services.downgrade_guard._send_cancelled_email",
            _fake_async(lambda *a, **kw: email_calls.append(a)),
        )
        _srem = MagicMock()
        monkeypatch.setattr("api.services.downgrade_guard.REDIS_CONN", _fake_redis(srem=_srem))

        from api.services.downgrade_guard import _check_and_cancel_if_needed
        asyncio.run(_check_and_cancel_if_needed("t1"))
        assert len(cancel_calls) == 1
        assert len(email_calls) == 1
        _srem.assert_called()

    def test_keeps_when_within_limits(self, monkeypatch):
        cancel_calls = []
        monkeypatch.setattr(
            "api.services.downgrade_guard._get_subscription_downgrade_info",
            lambda _tid: _sub(),
        )
        monkeypatch.setattr(
            "api.services.downgrade_guard._load_tenant_data",
            lambda _tid, _s: ({"num_storage_bytes": 5*1000**3, "num_members": 3, "num_apps": 5}, _product()),
        )
        monkeypatch.setattr(
            "api.services.downgrade_guard.cancel_scheduled_subscription_change_async",
            _fake_async(lambda _sid: cancel_calls.append(_sid)),
        )
        monkeypatch.setattr("api.services.downgrade_guard.REDIS_CONN", _fake_redis())

        from api.services.downgrade_guard import _check_and_cancel_if_needed
        asyncio.run(_check_and_cancel_if_needed("t1"))
        assert not cancel_calls

    def test_removes_when_no_downgrade_left(self, monkeypatch):
        monkeypatch.setattr(
            "api.services.downgrade_guard._get_subscription_downgrade_info",
            lambda _tid: None,
        )
        _srem = MagicMock()
        monkeypatch.setattr("api.services.downgrade_guard.REDIS_CONN", _fake_redis(srem=_srem))

        from api.services.downgrade_guard import _check_and_cancel_if_needed
        asyncio.run(_check_and_cancel_if_needed("t1"))
        _srem.assert_called()

    def test_skips_cancel_when_end_time_passed(self, monkeypatch):
        cancel_calls = []
        past = _beijing_now() - timedelta(hours=1)
        sub = _sub(end_time=past)

        monkeypatch.setattr(
            "api.services.downgrade_guard._get_subscription_downgrade_info",
            lambda _tid: sub,
        )
        # exceeded, but end_time is past
        monkeypatch.setattr(
            "api.services.downgrade_guard._load_tenant_data",
            lambda _tid, _s: ({"num_storage_bytes": 51*1000**3, "num_members": 3, "num_apps": 5}, _product()),
        )
        monkeypatch.setattr(
            "api.services.downgrade_guard.cancel_scheduled_subscription_change_async",
            _fake_async(lambda _sid: cancel_calls.append(_sid)),
        )
        _srem = MagicMock()
        monkeypatch.setattr("api.services.downgrade_guard.REDIS_CONN", _fake_redis(srem=_srem))

        from api.services.downgrade_guard import _check_and_cancel_if_needed
        asyncio.run(_check_and_cancel_if_needed("t1"))
        assert not cancel_calls
        _srem.assert_called()


# ------------------------------------------------------------------
# check_downgrade_effective_exceeded (webhook final defense)
# ------------------------------------------------------------------

class TestCheckDowngradeEffectiveExceeded:
    """Integration of detection + quota check for the webhook final-defense path."""

    def _pre_sync_sub(self, **kw):
        base = {
            "tenant_id": "t1",
            "plan_name": "Pro",
            "price_id": "price_pro",
            "target_plan_name": "Trial",
            "addon_storage_bytes": 40 * 1000**3,
            "target_storage_bytes": None,
        }
        base.update(kw)
        return base

    def test_none_when_no_pre_sync_sub(self, monkeypatch):
        assert check_downgrade_effective_exceeded("t1", None) is None

    def test_plan_downgrade_effective_and_exceeded(self, monkeypatch):
        pre = self._pre_sync_sub()
        # Simulate: Plan changed (Pro→Trial) + storage exceeds Trial quota
        monkeypatch.setattr(
            "api.services.downgrade_guard._load_tenant_data",
            lambda _tid, _s: (
                {"num_storage_bytes": 51 * 1000**3, "num_members": 3, "num_apps": 5},
                {"quota_storage": 10 * 1000**3, "quota_members": 5, "quota_apps": 10},
            ),
        )
        r = check_downgrade_effective_exceeded("t1", pre, plan_changed=True)
        assert r is not None
        assert r["storage_used"] == 51 * 1000**3

    def test_plan_downgrade_plan_changed_false_no_trigger(self, monkeypatch):
        """plan_changed=False means Stripe price didn't change → no plan downgrade effective."""
        pre = self._pre_sync_sub()
        r = check_downgrade_effective_exceeded("t1", pre, plan_changed=False)
        assert r is None

    def test_storage_downgrade_no_plan_change_triggers(self, monkeypatch):
        """Pure storage downgrade (40GB→20GB) should fire even when plan_changed=False."""
        pre = self._pre_sync_sub(
            target_plan_name=None,           # no plan downgrade
            target_storage_bytes=20 * 1000**3,
            addon_storage_bytes=40 * 1000**3,
        )
        # target_addon = 20GB, quota_storage = 10GB, total = 30GB
        # So 31GB usage should exceed
        monkeypatch.setattr(
            "api.services.downgrade_guard._load_tenant_data",
            lambda _tid, _s: (
                {"num_storage_bytes": 31 * 1000**3, "num_members": 3, "num_apps": 5},
                {"quota_storage": 10 * 1000**3, "quota_members": 5, "quota_apps": 10},
            ),
        )
        r = check_downgrade_effective_exceeded("t1", pre, plan_changed=False)
        assert r is not None
        assert r["storage_used"] == 31 * 1000**3
        assert r["storage_limit"] == 30 * 1000**3

    def test_storage_downgrade_within_limits(self, monkeypatch):
        pre = self._pre_sync_sub(
            target_plan_name=None,
            target_storage_bytes=20 * 1000**3,
            addon_storage_bytes=40 * 1000**3,
        )
        monkeypatch.setattr(
            "api.services.downgrade_guard._load_tenant_data",
            lambda _tid, _s: (
                {"num_storage_bytes": 5 * 1000**3, "num_members": 3, "num_apps": 5},
                {"quota_storage": 10 * 1000**3, "quota_members": 5, "quota_apps": 10},
            ),
        )
        r = check_downgrade_effective_exceeded("t1", pre, plan_changed=False)
        assert r is None

    def test_no_downgrade_scheduled_returns_none(self, monkeypatch):
        pre = self._pre_sync_sub(
            target_plan_name="Pro",   # same as plan_name → no downgrade
            target_storage_bytes=40 * 1000**3,
            addon_storage_bytes=40 * 1000**3,
        )
        r = check_downgrade_effective_exceeded("t1", pre, plan_changed=False)
        assert r is None

    def test_load_tenant_data_fails_returns_none(self, monkeypatch):
        pre = self._pre_sync_sub()
        monkeypatch.setattr(
            "api.services.downgrade_guard._load_tenant_data",
            lambda _tid, _s: (None, None),
        )
        r = check_downgrade_effective_exceeded("t1", pre, plan_changed=True)
        assert r is None
