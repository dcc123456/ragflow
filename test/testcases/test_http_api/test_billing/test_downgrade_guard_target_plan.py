#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
#
"""End-to-end tests for target_plan_name field lifecycle.

Requires: API server running, Stripe test mode, Redis available.
"""

import logging
import time

import pytest
import stripe

from libs.billing.billing_common import (
    parse_plan_end,
    stripe_dict,
)

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------


def _advance_to_hours_before_period_end(client, hours_before: int):
    """Advance test clock to `hours_before` hours before current period end."""
    plan = client.current_plan()
    end_ts = parse_plan_end(plan)
    target_ts = end_ts - (hours_before * 3600)
    stripe.test_helpers.TestClock.advance(
        client.clock_id,
        frozen_time=target_ts,
    )
    deadline = time.time() + 60
    while time.time() < deadline:
        c = stripe_dict(stripe.test_helpers.TestClock.retrieve(client.clock_id))
        if c.get("status") == "ready":
            return
        time.sleep(1)


def _advance_past_period_end(client, offset_seconds: int = 3600):
    """Advance test clock past current period end."""
    plan = client.current_plan()
    end_ts = parse_plan_end(plan)
    stripe.test_helpers.TestClock.advance(
        client.clock_id,
        frozen_time=end_ts + offset_seconds,
    )
    deadline = time.time() + 60
    while time.time() < deadline:
        c = stripe_dict(stripe.test_helpers.TestClock.retrieve(client.clock_id))
        if c.get("status") == "ready":
            return
        time.sleep(1)


# ---------------------------------------------------------------------------
# tests
# ---------------------------------------------------------------------------


@pytest.mark.billing
class TestTargetPlanNamePlanDowngrade:
    """Plan downgrade (Starter → Trial): target_plan_name written and cleared."""

    def test_written_on_starter_to_trial_downgrade(self, billing_client):
        client = billing_client

        # ── Setup: go to Starter ──
        client.upgrade_trial_to_starter()

        # ── Schedule downgrade Starter → Trial ──
        client.downgrade_to_trial(
            subscription_id=client.current_plan().get("subscription_id", ""),
        )

        # Verify: target_plan_name written, plan still Starter
        plan = client.current_plan()
        assert plan.get("target_plan_name") == "Trial", f"target_plan_name should be Trial, got {plan.get('target_plan_name')}"
        assert plan.get("plan_name") == "Starter", f"plan_name should still be Starter, got {plan.get('plan_name')}"
        logger.info("PASS: target_plan_name=Trial, plan_name=Starter")

    def test_cleared_after_downgrade_effective(self, billing_client):
        client = billing_client

        # ── Setup: Starter → Trial downgrade ──
        client.upgrade_trial_to_starter()
        client.downgrade_to_trial(
            subscription_id=client.current_plan().get("subscription_id", ""),
        )

        assert client.current_plan().get("target_plan_name") == "Trial"

        # ── Advance clock past period end ──
        _advance_past_period_end(client, offset_seconds=3600)
        sid = client.current_plan().get("subscription_id", "")
        client.sync_webhooks(
            subscription_ids={sid},
            created_gte=int(time.time()) - 120,
            wait_seconds=12,
        )

        # ── Wait for plan to become Trial ──
        client.wait_for_plan("Trial")

        # Verify: cleared
        plan = client.current_plan()
        assert plan.get("plan_name") == "Trial", f"plan should be Trial, got {plan.get('plan_name')}"
        assert plan.get("target_plan_name") is None, f"target_plan_name should be None, got {plan.get('target_plan_name')}"
        logger.info("PASS: target_plan_name cleared after downgrade effective")


@pytest.mark.billing
class TestTargetPlanNameStorageDowngrade:
    """Storage addon downgrade: target_storage_bytes behavior."""

    def test_storage_downgrade_40gb_to_20gb(self, billing_client):
        client = billing_client

        # ── Setup: Starter + 40GB addon ──
        client.upgrade_trial_to_starter()
        client.add_storage_to_subscription_with_webhook(
            storage_quantity_gb=40,
            subscription_ids={client.current_plan().get("subscription_id", "")},
        )
        storage = client.storage_current()
        addon_bytes = storage.get("addon_storage_bytes", 0)
        assert addon_bytes >= 40 * 1000**3, f"addon should be 40GB, got {addon_bytes}"

        # ── Schedule storage downgrade 40GB → 20GB ──
        resp = client.storage_set_target(20 * 1000**3)
        assert resp.get("scheduled_change"), f"Expected scheduled_change in response, got {resp}"

        # Verify via API: downgrade target is set (less than current addon)
        storage = client.storage_current()
        target = storage.get("target_storage_bytes") or 0
        assert target < addon_bytes, f"target_storage_bytes should be < 40GB, got {target}"
        assert storage.get("addon_storage_bytes") == addon_bytes, "addon_storage_bytes should still be 40GB"
        plan = client.current_plan()
        assert plan.get("target_plan_name") is None, "target_plan_name should be None (plan not downgraded)"
        logger.info("PASS: target_storage_bytes=%d < addon=%d, target_plan_name=None", target, addon_bytes)

    def test_storage_downgrade_20gb_to_0gb(self, billing_client):
        client = billing_client

        # ── Setup: Starter + 20GB addon ──
        client.upgrade_trial_to_starter()
        client.add_storage_to_subscription_with_webhook(
            storage_quantity_gb=20,
            subscription_ids={client.current_plan().get("subscription_id", "")},
        )

        # ── Schedule storage downgrade 20GB → 0GB ──
        resp = client.storage_set_target(0)
        assert resp.get("scheduled_change"), f"Expected scheduled_change, got {resp}"

        storage = client.storage_current()
        target = storage.get("target_storage_bytes") or 0
        assert target < storage.get("addon_storage_bytes", 0), f"target_storage_bytes should be < addon, got target={target} addon={storage.get('addon_storage_bytes')}"
        logger.info("PASS: target_storage_bytes < addon (downgrade scheduled)")

    def test_storage_target_cleared_after_effective(self, billing_client):
        client = billing_client

        # ── Setup: Starter + 40GB addon, downgrade to 20GB ──
        client.upgrade_trial_to_starter()
        client.add_storage_to_subscription_with_webhook(
            storage_quantity_gb=40,
            subscription_ids={client.current_plan().get("subscription_id", "")},
        )
        addon_before = client.storage_current().get("addon_storage_bytes") or 0
        client.storage_set_target(20 * 1000**3)

        # ── Advance past period end ──
        _advance_past_period_end(client, offset_seconds=3600)
        sid = client.current_plan().get("subscription_id", "")
        client.sync_webhooks(
            subscription_ids={sid},
            created_gte=int(time.time()) - 120,
            wait_seconds=12,
        )

        # Verify: addon reduced
        import time as _t

        deadline = _t.time() + 180
        storage = {}
        while _t.time() < deadline:
            storage = client.storage_current()
            if (storage.get("addon_storage_bytes") or 0) < addon_before:
                break
            _t.sleep(3)
        assert (storage.get("addon_storage_bytes") or 0) < addon_before, f"addon should have decreased from {addon_before}, got {storage.get('addon_storage_bytes')}"
        logger.info("PASS: storage downgrade effective, target cleared")


@pytest.mark.billing
class TestTargetPlanNameCombined:
    """Plan + Storage combined downgrade."""

    def test_starter_to_trial_with_storage_cancel(self, billing_client):
        client = billing_client

        # ── Setup: Starter + 40GB addon ──
        client.upgrade_trial_to_starter()
        client.add_storage_to_subscription_with_webhook(
            storage_quantity_gb=40,
            subscription_ids={client.current_plan().get("subscription_id", "")},
        )

        # ── Schedule Starter → Trial downgrade (cancels storage) ──
        client.downgrade_to_trial(
            subscription_id=client.current_plan().get("subscription_id", ""),
        )

        # Verify both fields via API
        plan = client.current_plan()
        storage = client.storage_current()
        assert plan.get("target_plan_name") == "Trial", f"target_plan_name should be Trial, got {plan.get('target_plan_name')}"
        addon = storage.get("addon_storage_bytes") or 0
        target = storage.get("target_storage_bytes") or 0
        assert target < addon, f"target_storage_bytes should be < addon, got target={target} addon={addon}"
        logger.info("PASS: combined downgrade: target_plan_name=Trial, target_storage_bytes < addon")


@pytest.mark.billing
class TestWebhookDefenseStorageExceeded:
    """Webhook final defense for pure storage downgrade with exceeded usage."""

    def test_storage_downgrade_exceeded_triggers_defense(self, billing_client):
        """Storage 40GB→20GB + injected file data exceeds total limit.

        Starter base 5GB + addon 20GB = 25GB total.  Inject 30GB of files
        so the webhook defense detects the exceeded quota and increments
        webhook_violations_total.
        """
        import uuid as _uuid
        from libs.billing.billing_common import prepare_backend_imports

        prepare_backend_imports()
        from api.db.db_models import DB, File as FileModel
        from api.db.services.file_service import FileService

        client = billing_client
        tid = client.tenant_id

        # ── Setup: Starter + 40GB addon, schedule 40→20 GB ──
        client.upgrade_trial_to_starter()
        client.add_storage_to_subscription_with_webhook(
            storage_quantity_gb=40,
            subscription_ids={client.current_plan().get("subscription_id", "")},
        )
        client.storage_set_target(20 * 1000**3)

        # ── Inject a fake file (30 GB) to exceed 5GB base + 20GB addon ──
        fid = _uuid.uuid4().hex
        FileService.insert(
            {
                "id": fid,
                "tenant_id": tid,
                "parent_id": tid,
                "created_by": _uuid.uuid4().hex,
                "name": f"dg-test-{_uuid.uuid4().hex[:8]}.bin",
                "location": "",
                "size": 30 * 1000**3,
                "type": "bin",
            }
        )

        # ── Read metric before ──
        status = client.request_json("GET", "/billing/downgrade-guard/health")
        violations_before = status.get("metrics", {}).get("webhook_violations_total", 0)

        # ── Advance past period end ──
        _advance_past_period_end(client, offset_seconds=3600)
        sid = client.current_plan().get("subscription_id", "")
        client.sync_webhooks(
            subscription_ids={sid},
            created_gte=int(time.time()) - 120,
            wait_seconds=12,
        )

        # ── Verify webhook defense fired ──
        status_after = client.request_json("GET", "/billing/downgrade-guard/health")
        violations_after = status_after.get("metrics", {}).get("webhook_violations_total", 0)
        assert violations_after > violations_before, f"webhook_violations_total must increment: {violations_before} → {violations_after}"
        logger.info("PASS: defense detected exceeded: %d→%d", violations_before, violations_after)

        # ── Cleanup: remove injected file ──
        try:
            with DB.connection_context():
                FileModel.delete().where(FileModel.id == fid).execute()
        except Exception:
            logger.warning("Failed to cleanup fake file %s", fid)
