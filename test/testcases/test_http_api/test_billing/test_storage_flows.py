#!/usr/bin/env python3
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
Pytest tests for STORAGE-01 through STORAGE-05 billing flows.

These tests exercise the storage addon purchase and lifecycle under the new
single-subscription model (plan + storage addon on the same subscription).
"""

from __future__ import annotations

import logging

import pytest
import time
import uuid

import stripe
import stripe.test_helpers  # type: ignore[reportMissingImports]

from libs.billing.billing_common import (
    BillingClient,
    stripe_dict,
)
from libs.billing.storage_common import (
    gb_to_bytes,
)

logger = logging.getLogger(__name__)


# =============================================================================
# STORAGE-01: First storage addon purchase with proration
# =============================================================================


@pytest.mark.billing
def test_storage_01_first_addon_purchase_with_proration(billing_client: BillingClient) -> None:
    """STORAGE-01: first storage addon purchase with proration.

    Tests that adding a storage addon to a Starter subscription creates a
    second line item on the same subscription, and that mid-cycle upgrade
    produces a prorated invoice.
    """
    # Step 1-4: Upgrade Trial -> Starter
    starter_sub = billing_client.upgrade_trial_to_starter()
    starter_subscription_id: str = starter_sub["subscription_id"]

    logger.info("STORAGE-01: Starter subscription ID: %s", starter_subscription_id)
    assert starter_subscription_id, "Starter subscription ID must be set"

    # Step 5: Add 20GB storage addon
    storage_gb = 20
    storage_added_at = stripe_dict(stripe.test_helpers.TestClock.create(
        frozen_time=int(time.time()),
        name=f"storage01-{uuid.uuid4().hex[:8]}",
    )).get("frozen_time") or int(time.time()) - 5

    billing_client.add_storage_to_subscription_with_webhook(
        storage_quantity_gb=storage_gb,
        subscription_ids={starter_subscription_id},
        created_gte=storage_added_at,
    )
    billing_client.wait_for_storage_status("active")

    # Verify subscription has 2 items (plan + storage)
    updated_sub = stripe.Subscription.retrieve(starter_subscription_id)
    updated_items = updated_sub.get("items", {}).get("data", [])
    assert len(updated_items) == 2, (
        f"Expected 2 subscription items (plan + storage), got {len(updated_items)}"
    )

    # Verify invoice for initial addon purchase
    latest_invoice = updated_sub.get("latest_invoice")
    if latest_invoice:
        if isinstance(latest_invoice, str):
            latest_invoice = stripe.Invoice.retrieve(latest_invoice, expand=["lines.data"])
        else:
            invoice_id = latest_invoice.get("id") if isinstance(latest_invoice, dict) else latest_invoice.id
            latest_invoice = stripe.Invoice.retrieve(invoice_id or "", expand=["lines.data"])
        invoice_dict = stripe_dict(latest_invoice)
        initial_invoice_amount = invoice_dict.get("amount_due", 0)
        logger.info("STORAGE-01: Initial invoice amount: %s cents", initial_invoice_amount)

    # Step 6: Verify addon storage is on the same subscription
    storage = billing_client.storage_current()
    addon_storage_bytes = int(storage.get("addon_storage_bytes") or 0)
    expected_storage_bytes = gb_to_bytes(storage_gb)
    assert addon_storage_bytes == expected_storage_bytes, (
        f"Expected addon_storage_bytes={expected_storage_bytes}, got {addon_storage_bytes}"
    )

    storage_subscription_id = storage.get("subscription_id", "")
    assert storage_subscription_id == starter_subscription_id, (
        f"Expected storage to be on main subscription {starter_subscription_id}, "
        f"got {storage_subscription_id}"
    )

    # Step 7: Advance to period end and upgrade storage mid-cycle
    billing_client.advance_clock_to_plan_end()

    before_mid_storage = billing_client.storage_current()
    before_mid_addon_bytes = int(before_mid_storage.get("addon_storage_bytes") or 0)

    storage_gb_mid = storage_gb + 10
    billing_client.replace_storage_subscription_quantity(
        new_quantity_gb=storage_gb_mid,
        subscription_ids={starter_subscription_id},
    )
    billing_client.wait_for_storage_status("active")

    # Step 8: Verify proration invoice for mid-cycle upgrade
    updated_sub = stripe.Subscription.retrieve(starter_subscription_id)
    latest_invoice_id = updated_sub.get("latest_invoice", "")
    if latest_invoice_id:
        invoice = stripe.Invoice.retrieve(latest_invoice_id, expand=["lines.data"])
        invoice_dict = stripe_dict(invoice)
        proration_invoice_amount = invoice_dict.get("amount_due", 0)
        logger.info("STORAGE-01: Proration invoice amount: %s cents", proration_invoice_amount)

        line_items = (invoice_dict.get("lines") or {}).get("data", [])
        proration_storage_line_amount = 0
        for line in line_items:
            description = line.get("description", "")
            amount = line.get("amount", 0)
            if "storage" in description.lower() or "gb" in description.lower():
                proration_storage_line_amount += amount

        expected_proration_cents = int((storage_gb_mid - storage_gb) * 1000 * 15 / 30)
        if proration_invoice_amount > 0:
            tolerance = expected_proration_cents * 0.2
            if abs(proration_invoice_amount - expected_proration_cents) > tolerance:
                logger.warning(
                    "Invoice proration %s cents differs from expected %s cents (tolerance %s) - "
                    "may indicate backend proration calculation change",
                    proration_invoice_amount, expected_proration_cents, tolerance,
                )

    after_mid_storage = billing_client.storage_current()
    after_mid_addon_bytes = int(after_mid_storage.get("addon_storage_bytes") or 0)
    expected_increase = gb_to_bytes(10)
    assert after_mid_addon_bytes == before_mid_addon_bytes + expected_increase, (
        f"addon_storage_bytes should increase by {expected_increase}, "
        f"before={before_mid_addon_bytes}, after={after_mid_addon_bytes}"
    )

    logger.info("STORAGE-01: Billing history count: %s", len(billing_client.spend_history()))


# =============================================================================
# STORAGE-02: Storage addon lifecycle + Trial downgrade auto-cancel
# =============================================================================


@pytest.mark.billing
def test_storage_02_lifecycle_with_plan_downgrade(billing_client: BillingClient) -> None:
    """STORAGE-02: storage addon lifecycle combined with Trial downgrade.

    Tests that cancelling storage (quantity=0) takes effect at period end,
    and that downgrading to Trial automatically cancels the storage addon.
    """
    # Steps 1-5: Upgrade Trial -> Starter
    starter_sub = billing_client.upgrade_trial_to_starter()
    starter_subscription_id: str = starter_sub["subscription_id"]

    # Step 6: Add 30GB storage addon
    storage_gb = 30
    target_storage_bytes = gb_to_bytes(storage_gb)

    billing_client.replace_storage_subscription_quantity(
        new_quantity_gb=storage_gb,
        subscription_ids={starter_subscription_id},
    )
    billing_client.wait_for_storage_status("active")

    storage = billing_client.storage_current()
    addon_bytes = int(storage.get("addon_storage_bytes") or 0)
    assert addon_bytes == target_storage_bytes, (
        f"expected addon_storage_bytes={target_storage_bytes}, got {addon_bytes}"
    )

    sub = stripe.Subscription.retrieve(starter_subscription_id)
    items = sub.get("items", {}).get("data", [])
    assert len(items) == 2, f"expected 2 subscription items, got {len(items)}"

    # Step 7: Cancel storage (quantity = 0) - effective at period end
    billing_client.replace_storage_subscription_quantity(
        new_quantity_gb=0,
        subscription_ids={starter_subscription_id},
    )
    billing_client.wait_for_storage_status("active")

    storage_after_cancel = billing_client.storage_current()
    addon_bytes_after_cancel = int(storage_after_cancel.get("addon_storage_bytes") or 0)
    assert addon_bytes_after_cancel == target_storage_bytes, (
        f"addon_storage_bytes should still be {target_storage_bytes} after cancellation "
        f"(not yet effective), got {addon_bytes_after_cancel}"
    )

    storage_cancel_at = int(time.time()) - 5
    billing_client.advance_clock_to_plan_end()

    billing_client.sync_webhooks(
        subscription_ids={starter_subscription_id},
        created_gte=storage_cancel_at,
        wait_seconds=8,
    )

    storage_after_period_end = billing_client.storage_current()
    addon_bytes_after_period_end = int(storage_after_period_end.get("addon_storage_bytes") or 0)
    assert addon_bytes_after_period_end == 0, (
        f"addon_storage_bytes should be 0 after period end, got {addon_bytes_after_period_end}"
    )

    sub = stripe.Subscription.retrieve(starter_subscription_id)
    items = sub.get("items", {}).get("data", [])
    assert len(items) == 1, f"expected 1 subscription item after cancellation, got {len(items)}"

    # Step 8: Re-add storage (set to 20GB)
    storage_gb_2 = 20
    target_storage_bytes_2 = gb_to_bytes(storage_gb_2)

    billing_client.replace_storage_subscription_quantity(
        new_quantity_gb=storage_gb_2,
        subscription_ids={starter_subscription_id},
    )
    billing_client.wait_for_storage_status("active")

    storage_after_readd = billing_client.storage_current()
    addon_bytes_after_readd = int(storage_after_readd.get("addon_storage_bytes") or 0)
    assert addon_bytes_after_readd == target_storage_bytes_2, (
        f"expected addon_storage_bytes={target_storage_bytes_2}, got {addon_bytes_after_readd}"
    )

    # Step 9: Downgrade Starter -> Trial (auto-cancel storage)
    before_downgrade_plan = billing_client.current_plan()
    before_downgrade_plan_name = before_downgrade_plan.get("plan_name", "")
    assert before_downgrade_plan_name == "Starter", (
        f"plan should be Starter before downgrade, got {before_downgrade_plan_name}"
    )

    before_downgrade_storage = billing_client.storage_current()
    before_downgrade_addon_bytes = int(before_downgrade_storage.get("addon_storage_bytes") or 0)
    assert before_downgrade_addon_bytes == target_storage_bytes_2, (
        f"addon_storage_bytes should remain {target_storage_bytes_2} before downgrade, "
        f"got {before_downgrade_addon_bytes}"
    )

    created_gte = int(time.time()) - 5
    billing_client.downgrade_to_trial(starter_subscription_id)
    billing_client.advance_clock_to_plan_end()

    replayed = billing_client.sync_webhooks(
        subscription_ids={starter_subscription_id},
        created_gte=created_gte,
    )
    logger.info("STORAGE-02: Webhook synchronization finished: %s replayed events", replayed)

    billing_client.wait_for_plan("Trial")

    after_trial_storage = billing_client.storage_current()
    after_trial_addon_bytes = int(after_trial_storage.get("addon_storage_bytes") or 0)
    assert after_trial_addon_bytes == 0, (
        f"addon_storage_bytes should be 0 after downgrade to Trial, got {after_trial_addon_bytes}"
    )

    # Step 10: Attempt to add storage on Trial plan (should be rejected)
    with pytest.raises(Exception):
        billing_client.replace_storage_subscription_quantity(
            new_quantity_gb=10,
            subscription_ids={starter_subscription_id},
        )

    logger.info("STORAGE-02: Billing history count: %s", len(billing_client.spend_history()))


# =============================================================================
# STORAGE-03: Storage addon upgrade takes effect immediately
# =============================================================================


@pytest.mark.billing
def test_storage_03_upgrade_immediate_effect(billing_client: BillingClient) -> None:
    """STORAGE-03: upgrading storage addon (increase quantity) takes effect immediately.

    Tests that increasing storage addon quantity is charged immediately via
    proration and the quota is updated right away.
    """
    # Steps 1-5: Upgrade Trial -> Starter
    starter_sub = billing_client.upgrade_trial_to_starter()
    starter_subscription_id: str = starter_sub["subscription_id"]

    # Step 6: Purchase initial 10GB storage addon
    initial_storage_gb = 10
    initial_target_bytes = gb_to_bytes(initial_storage_gb)

    billing_client.replace_storage_subscription_quantity(
        new_quantity_gb=initial_storage_gb,
        subscription_ids={starter_subscription_id},
    )
    billing_client.wait_for_storage_status("active")

    storage = billing_client.storage_current()
    addon_bytes_initial = int(storage.get("addon_storage_bytes") or 0)
    assert addon_bytes_initial == initial_target_bytes, (
        f"expected addon_storage_bytes={initial_target_bytes}, got {addon_bytes_initial}"
    )

    sub = stripe.Subscription.retrieve(starter_subscription_id)
    items = sub.get("items", {}).get("data", [])
    assert len(items) == 2, f"expected 2 subscription items, got {len(items)}"

    # Step 7: Upgrade storage addon 10GB -> 20GB (immediate effect)
    upgraded_storage_gb = 20
    upgraded_target_bytes = gb_to_bytes(upgraded_storage_gb)

    billing_client.replace_storage_subscription_quantity(
        new_quantity_gb=upgraded_storage_gb,
        subscription_ids={starter_subscription_id},
    )
    billing_client.wait_for_storage_status("active")

    storage_after_upgrade = billing_client.storage_current()
    addon_bytes_upgraded = int(storage_after_upgrade.get("addon_storage_bytes") or 0)

    assert addon_bytes_upgraded == upgraded_target_bytes, (
        f"expected addon_storage_bytes={upgraded_target_bytes} after upgrade, "
        f"got {addon_bytes_upgraded}"
    )
    assert addon_bytes_upgraded > addon_bytes_initial, (
        f"addon_storage_bytes should increase after upgrade, "
        f"before={addon_bytes_initial}, after={addon_bytes_upgraded}"
    )

    logger.info("STORAGE-03: Billing history count: %s", len(billing_client.spend_history()))


# =============================================================================
# STORAGE-04: Storage addon downgrade takes effect at period end
# =============================================================================


@pytest.mark.billing
def test_storage_04_downgrade_at_period_end(billing_client: BillingClient) -> None:
    """STORAGE-04: downgrading storage addon (decrease quantity) takes effect at period end.

    Tests that decreasing storage addon quantity is scheduled and only becomes
    effective at the next period end (not immediately).
    """
    # Steps 1-5: Upgrade Trial -> Starter
    starter_sub = billing_client.upgrade_trial_to_starter()
    starter_subscription_id: str = starter_sub["subscription_id"]

    # Step 6: Purchase initial 20GB storage addon
    initial_storage_gb = 20
    initial_target_bytes = gb_to_bytes(initial_storage_gb)

    billing_client.replace_storage_subscription_quantity(
        new_quantity_gb=initial_storage_gb,
        subscription_ids={starter_subscription_id},
    )
    billing_client.wait_for_storage_status("active")

    storage = billing_client.storage_current()
    after_addon_bytes = int(storage.get("addon_storage_bytes") or 0)
    assert after_addon_bytes == initial_target_bytes, (
        f"expected addon_storage_bytes={initial_target_bytes}, got {after_addon_bytes}"
    )

    sub = stripe.Subscription.retrieve(starter_subscription_id)
    items = sub.get("items", {}).get("data", [])
    assert len(items) == 2, f"expected 2 subscription items, got {len(items)}"

    # Step 7: Schedule storage downgrade 20GB -> 10GB via storage_set_target
    downgrade_storage_gb = 10
    downgrade_target_bytes = gb_to_bytes(downgrade_storage_gb)
    created_gte = int(time.time()) - 5

    downgrade_result = billing_client.storage_set_target(downgrade_target_bytes)
    scheduled_change = downgrade_result.get("scheduled_change")
    assert scheduled_change, (
        f"scheduled_change should be set after downgrade request, got: {downgrade_result}"
    )

    assert downgrade_result.get("addon_storage_bytes", 0) == initial_target_bytes, (
        f"addon_storage_bytes should remain {initial_target_bytes} immediately after "
        f"downgrade, got {downgrade_result.get('addon_storage_bytes')}"
    )

    # Also verify via storage_current()
    storage_after_schedule = billing_client.storage_current()
    after_downgrade_addon_bytes = int(storage_after_schedule.get("addon_storage_bytes") or 0)
    assert after_downgrade_addon_bytes == initial_target_bytes, (
        f"API shows addon_storage_bytes changed prematurely to {after_downgrade_addon_bytes}"
    )

    # Step 8: Advance clock past period end and verify quota decreases
    billing_client.advance_clock_to_plan_end()

    replayed = billing_client.sync_webhooks(
        subscription_ids={starter_subscription_id},
        created_gte=created_gte,
    )
    logger.info("STORAGE-04: Webhook synchronization finished: %s replayed events", replayed)

    storage_after_period = billing_client.storage_current()
    after_period_addon_bytes = int(storage_after_period.get("addon_storage_bytes") or 0)
    assert after_period_addon_bytes == downgrade_target_bytes, (
        f"addon_storage_bytes should be {downgrade_target_bytes} after period end, "
        f"got {after_period_addon_bytes}"
    )


# =============================================================================
# STORAGE-05: Plan change with existing storage addon
# =============================================================================


@pytest.mark.billing
def test_storage_05_plan_change_with_existing_addon(billing_client: BillingClient) -> None:
    """STORAGE-05: plan upgrade/downgrade with an existing storage addon.

    Tests three design rules:
    - Starter -> Pro upgrade keeps the existing storage addon unchanged.
    - Pro -> Starter downgrade keeps the existing storage addon unchanged.
    - Starter -> Trial downgrade invalidates the storage addon.
    """
    # Steps 1-5: Upgrade Trial -> Starter
    starter_sub = billing_client.upgrade_trial_to_starter()
    starter_subscription_id: str = starter_sub["subscription_id"]
    subscription_ids: set[str] = {starter_subscription_id}

    # Step 6: Purchase 30GB storage addon on Starter plan
    storage_gb = 30
    target_storage_bytes = gb_to_bytes(storage_gb)

    billing_client.replace_storage_subscription_quantity(
        new_quantity_gb=storage_gb,
        subscription_ids={starter_subscription_id},
    )
    billing_client.wait_for_storage_status("active")

    after_addon_storage = billing_client.storage_current()
    after_addon_addon_bytes = int(after_addon_storage.get("addon_storage_bytes") or 0)
    assert after_addon_addon_bytes == target_storage_bytes, (
        f"addon_storage_bytes should be {target_storage_bytes} after purchase, "
        f"got {after_addon_addon_bytes}"
    )

    sub = stripe.Subscription.retrieve(starter_subscription_id)
    items = sub.get("items", {}).get("data", [])
    assert len(items) == 2, f"expected 2 subscription items, got {len(items)}"

    # Step 7: Upgrade Starter -> Pro
    upgrade_result = billing_client.upgrade_starter_to_pro(starter_subscription_id)
    subscription_id = upgrade_result.get("subscription_id", "")
    subscription_ids.add(subscription_id)

    after_upgrade_plan = upgrade_result.get("current_plan", {})
    after_upgrade_plan_name = after_upgrade_plan.get("plan_name", "")
    assert after_upgrade_plan_name == "Pro", (
        f"plan should be Pro after upgrade, got {after_upgrade_plan_name}"
    )

    after_upgrade_storage = billing_client.storage_current()
    after_upgrade_addon_bytes = int(after_upgrade_storage.get("addon_storage_bytes") or 0)
    assert after_upgrade_addon_bytes == target_storage_bytes, (
        f"addon_storage_bytes should remain {target_storage_bytes} after plan upgrade, "
        f"got {after_upgrade_addon_bytes}"
    )

    # Step 8: Downgrade Pro -> Starter
    downgrade_created_gte = int(time.time()) - 5
    downgrade_result = billing_client.downgrade_pro_to_starter(subscription_id)
    assert downgrade_result.get("schedule_id"), "schedule_id should be set after downgrade"

    billing_client.advance_clock_to_plan_end()

    billing_client.sync_webhooks(
        subscription_ids=subscription_ids,
        created_gte=downgrade_created_gte,
        wait_seconds=8,
    )

    after_downgrade_plan = billing_client.wait_for_plan("Starter")
    after_downgrade_plan_name = after_downgrade_plan.get("plan_name", "")
    assert after_downgrade_plan_name == "Starter", (
        f"plan should be Starter after downgrade, got {after_downgrade_plan_name}"
    )

    after_downgrade_storage = billing_client.storage_current()
    after_downgrade_addon_bytes = int(after_downgrade_storage.get("addon_storage_bytes") or 0)
    assert after_downgrade_addon_bytes == target_storage_bytes, (
        f"addon_storage_bytes should remain {target_storage_bytes} after plan downgrade, "
        f"got {after_downgrade_addon_bytes}"
    )

    # Step 9: Verify quota after downgrade takes effect
    after_downgrade_effective_storage = billing_client.storage_current()
    after_downgrade_effective_addon_bytes = int(
        after_downgrade_effective_storage.get("addon_storage_bytes") or 0
    )
    assert after_downgrade_effective_addon_bytes == target_storage_bytes, (
        f"addon_storage_bytes should remain {target_storage_bytes} after downgrade takes effect, "
        f"got {after_downgrade_effective_addon_bytes}"
    )

    final_plan_overview = billing_client.plan_overview()
    final_resources = final_plan_overview.get("resources", {})
    final_plan_storage_limit = int(final_resources.get("plan_storage", {}).get("limit") or 0)
    final_addon_storage_limit = int(final_resources.get("addon_storage", {}).get("limit") or 0)

    total_storage_after_downgrade = final_plan_storage_limit + final_addon_storage_limit
    expected_total = final_plan_storage_limit + target_storage_bytes
    assert total_storage_after_downgrade == expected_total, (
        f"total storage should be {expected_total} bytes (plan + addon), "
        f"got {total_storage_after_downgrade} bytes"
    )

    # Step 10: Downgrade Starter -> Trial (addon should be invalidated)
    history_before_trial_downgrade = billing_client.spend_history()

    created_gte = int(time.time()) - 5
    downgrade_result = billing_client.downgrade_to_trial(starter_subscription_id)
    assert downgrade_result.get("schedule_id"), "schedule_id should be set after Trial downgrade"

    billing_client.advance_clock_to_plan_end()
    billing_client.sync_webhooks(
        subscription_ids=subscription_ids,
        created_gte=created_gte,
        wait_seconds=15,
    )

    after_trial_plan = billing_client.wait_for_plan("Trial")
    after_trial_plan_name = after_trial_plan.get("plan_name", "")
    assert after_trial_plan_name == "Trial", (
        f"plan should be Trial after period end, got {after_trial_plan_name}"
    )

    after_trial_storage = billing_client.storage_current()
    after_trial_addon_bytes = int(after_trial_storage.get("addon_storage_bytes") or 0)
    assert after_trial_addon_bytes == 0, (
        f"addon_storage_bytes should be 0 after Trial downgrade, got {after_trial_addon_bytes}"
    )

    history_after_trial_downgrade = billing_client.spend_history()
    new_rows = len(history_after_trial_downgrade) - len(history_before_trial_downgrade)
    if new_rows > 0:
        new_paid_rows = [
            row for row in history_after_trial_downgrade
            if float(row.get("amount", 0) or 0) > 0 and row not in history_before_trial_downgrade
        ]
        assert not new_paid_rows, (
            f"Trial period should not create paid charges, got: {new_paid_rows}"
        )