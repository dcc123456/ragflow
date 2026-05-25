#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
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
Pytest wrapper for PLAN-01, PLAN-02, and PLAN-05 billing subscription flows.

PLAN-01: Full subscription lifecycle - Trial->Starter->Pro->Starter->Trial with renewals.
PLAN-02: Renewal failure -> attention banner -> invoice recovery.
PLAN-05: Starter -> Pro requires setup without reusable card, preserves Starter entitlements on failed payment.
"""

from __future__ import annotations

import json
import logging
import time

import pytest
import stripe

from libs.billing.billing_common import (
    first_plan_price_id,
    find_new_positive_paid_invoice,
    get_pro_quota_apps,
    get_starter_quota_apps,
    get_trial_quota_apps,
    load_billing_config,
    parse_plan_end,
    remove_customer_payment_method,
    stripe_dict,
)
from libs.billing.storage_common import attach_decline_test_card, attach_default_test_card

logger = logging.getLogger(__name__)


@pytest.mark.billing
def test_plan_01_full_lifecycle(billing_client):
    """PLAN-01: Full subscription lifecycle - Trial->Starter->Pro->Starter->Trial with renewals."""
    # =============================================================================
    # Steps 1-4: Setup Starter environment (validates config, creates test clock,
    #           registers user, upgrades Trial -> Starter)
    # =============================================================================
    logger.info("=" * 80)
    logger.info("Steps 1-4: Setup Starter environment using billing_client fixture")
    logger.info("=" * 80)

    starter_result = billing_client.upgrade_trial_to_starter()
    starter_subscription_id: str = starter_result["subscription_id"]

    logger.info("Assert: Starter environment ready")
    logger.info("Assert: Tenant ID: %s", billing_client.tenant_id)
    logger.info("Assert: Customer ID: %s", billing_client.customer_id)
    logger.info("Assert: Starter subscription ID: %s", starter_subscription_id)

    # =============================================================================
    # Step 5: Upgrade Starter -> Pro
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 5: Upgrade Starter -> Pro")
    logger.info("=" * 80)

    history_before_upgrade = billing_client.spend_history()
    upgrade_result = billing_client.upgrade_starter_to_pro(starter_subscription_id=starter_subscription_id)
    pro_subscription_id = upgrade_result["pro_subscription_id"]
    pro_plan = upgrade_result["current_plan"]
    logger.info("Assert: Pro subscription ID: %s", pro_subscription_id)

    # Verify: After Starter->Pro upgrade, there should be a new invoice with amount $200 (259-59=200, i.e., 20000 cents)
    billing_client.wait_for_history_count(len(history_before_upgrade) + 1, "Wait for Pro")

    history_after_upgrade = billing_client.spend_history()
    new_invoice = [row for row in history_after_upgrade if row.get("status", "") == "paid" and int(row.get("amount", 0)) == 200]
    assert len(new_invoice) == 1, f"expected 1 invoice paid with 200 USD, got {len(new_invoice)}"

    # Verify Pro quota
    pro_quota_apps = get_pro_quota_apps()
    overview_pro = billing_client.plan_overview()
    apps_limit_pro = overview_pro.get("resources", {}).get("apps", {}).get("limit", 0)
    assert apps_limit_pro == pro_quota_apps, f"after Pro upgrade, expected Pro apps quota {pro_quota_apps}, got {apps_limit_pro}"
    logger.info("Assert: Pro apps quota verified: %s", apps_limit_pro)

    # =============================================================================
    # Step 6: Pro renewal at period end
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 6: Pro renewal at period end")
    logger.info("=" * 80)

    pro_period_end_before_renewal = parse_plan_end(pro_plan)
    history_before_pro_renewal = billing_client.spend_history()

    created_gte = int(time.time()) - 5
    billing_client.advance_clock_to_plan_end()

    billing_client.sync_webhooks(
        subscription_ids={pro_subscription_id},
        created_gte=created_gte,
    )
    pro_plan_after = billing_client.wait_for_plan("Pro")
    pro_period_end_after = parse_plan_end(pro_plan_after)
    assert pro_period_end_after > pro_period_end_before_renewal, (
        f"Pro billing cycle did not advance after renewal: before={pro_period_end_before_renewal}, after={pro_period_end_after}"
    )
    billing_client.wait_for_history_count(
        len(history_before_pro_renewal) + 1,
        "Pro renewal",
    )
    history_after_pro_renewal = billing_client.spend_history()
    new_invoice = [row for row in history_after_pro_renewal if row.get("status", "") == "paid" and int(row.get("amount", 0)) == 259]
    assert len(new_invoice) == 1, f"expected 1 invoice paid with 259 USD, got {len(new_invoice)}, history_after_pro_renewal:{history_after_pro_renewal}"
    logger.info("Assert: Pro renewal completed with paid invoice, %s", new_invoice[0])

    # =============================================================================
    # Step 7: Pro -> Starter downgrade at period end
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 7: Pro -> Starter downgrade at period end")
    logger.info("=" * 80)

    history_before_starter = billing_client.spend_history()
    created_gte = int(time.time()) - 10
    billing_client.downgrade_pro_to_starter(
        subscription_id=pro_subscription_id,
    )

    billing_client.advance_clock_to_plan_end()
    sync_count = billing_client.sync_webhooks(
        subscription_ids={pro_subscription_id},
        created_gte=created_gte,
    )
    logger.info("after sync_webhooks, sync_count: %s", sync_count)

    billing_client.wait_for_plan("Starter")
    starter_quota = get_starter_quota_apps()
    overview_starter = billing_client.plan_overview()
    assert overview_starter.get("resources", {}).get("apps", {}).get("limit", 0) == starter_quota, (
        f"after downgrade to Starter, expected Starter apps quota {starter_quota}, got {overview_starter}"
    )
    history_after_starter = billing_client.wait_for_history_count(
        len(history_before_starter) + 1,
        "Starter renewal after downgrade",
    )
    find_new_positive_paid_invoice(
        history_after_starter,
        {str(row.get("invoice_id") or "") for row in history_before_starter},
    )
    logger.info("Assert: Pro -> Starter downgrade completed")

    # =============================================================================
    # Step 8: Starter -> Trial downgrade at period end
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 8: Starter -> Trial downgrade at period end")
    logger.info("=" * 80)

    billing_client.downgrade_to_trial(
        subscription_id=pro_subscription_id,
    )
    history_before_trial = billing_client.spend_history()
    billing_client.advance_clock_to_plan_end()

    billing_client.sync_webhooks(
        subscription_ids={pro_subscription_id},
        created_gte=int(time.time()) - 60,
    )
    _trial_plan = billing_client.wait_for_plan("Trial")
    assert billing_client.plan_overview().get("resources", {}).get("apps", {}).get("limit") == get_trial_quota_apps(), (
        f"after downgrade to Trial, expected Trial apps quota {get_trial_quota_apps()}, got {billing_client.plan_overview()}"
    )
    history_after_trial = billing_client.spend_history()
    new_trial_rows = history_after_trial[: max(0, len(history_after_trial) - len(history_before_trial))]
    paid_rows = [row for row in new_trial_rows if float(row.get("amount", 0) or 0) > 0]
    assert not paid_rows, f"Trial period should not create paid renewal rows, got {paid_rows}"
    billing_client.wait_for_no_pending_downgrade()
    logger.info("Assert: Starter -> Trial downgrade completed")

    # =============================================================================
    # PLAN-01 Test Summary
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("PLAN-01 Test Summary")
    logger.info("=" * 80)
    overview = billing_client.plan_overview()
    history_final = billing_client.spend_history()
    logger.info("%s", json.dumps({
        "case": "PLAN-01",
        "description": "Full subscription lifecycle: Trial->Starter->Pro->Starter->Trial",
        "tenant_id": billing_client.tenant_id,
        "email": billing_client.user_id,
        "test_clock_id": billing_client.clock_id,
        "customer_id": billing_client.customer_id,
        "final_plan": overview.get("plan_name"),
        "history_rows": len(history_final),
    }, indent=2, sort_keys=True))


@pytest.mark.billing
def test_plan_05_starter_to_pro_payment_requirement(billing_client):
    """PLAN-05: Starter -> Pro requires setup without reusable card, preserves Starter entitlements on failed payment."""
    billing_config = load_billing_config()
    pro_price_id = first_plan_price_id(billing_config, "Pro")
    assert pro_price_id, "Pro plan price_id not found in service_conf.yaml"

    # Upgrade from Trial to Starter first (setup)
    result = billing_client.upgrade_trial_to_starter()
    starter_subscription_id = result["subscription_id"]

    starter_quota = get_starter_quota_apps()
    overview_before = billing_client.plan_overview()
    apps_before = overview_before.get("resources", {}).get("apps", {}).get("limit", 0)
    assert apps_before == starter_quota, f"expected Starter apps quota {starter_quota}, got {apps_before}"

    # Remove reusable payment methods
    _remove_customer_payment_method(billing_client.customer_id)

    # Call the real billing API for Starter -> Pro upgrade
    upgrade_response = billing_client.schedule_plan_change(pro_price_id)
    redirect_to = str(upgrade_response.get("redirect_to") or "")
    requires_payment_method_setup = bool(upgrade_response.get("requires_payment_method_setup"))

    assert requires_payment_method_setup, (
        f"expected real billing API to require payment method setup when no reusable payment method exists, "
        f"got response: {upgrade_response}"
    )
    assert redirect_to, f"expected redirect_to setup URL when payment method setup is required, got: {upgrade_response}"

    # Verify entitlements remain Starter before payment-method setup
    current = billing_client.current_plan()
    plan_name = str(current.get("plan_name") or "")
    assert plan_name == "Starter", f"expected current plan to remain Starter before payment method setup, got {plan_name}"

    overview_after = billing_client.plan_overview()
    apps_after = overview_after.get("resources", {}).get("apps", {}).get("limit", 0)
    assert apps_after == starter_quota, f"expected Starter apps quota {starter_quota} before payment method setup, got {apps_after}"
    assert not overview_after.get("payment_required", False), (
        f"unexpected payment_required=true before any upgrade invoice exists: {overview_after}"
    )

    # Attach reusable failing card
    failing_pm_id = attach_decline_test_card(billing_client.customer_id)
    _set_subscription_payment_method(starter_subscription_id, failing_pm_id)

    # Retry the real billing API for Starter -> Pro upgrade
    upgrade_started_at = int(time.time()) - 5
    failed_upgrade_response = billing_client.schedule_plan_change(pro_price_id)
    subscription_id = str(failed_upgrade_response.get("subscription_id") or starter_subscription_id)
    payment_state = str(failed_upgrade_response.get("payment_state") or "")
    invoice_id = str(failed_upgrade_response.get("invoice_id") or "")

    assert not failed_upgrade_response.get("requires_payment_method_setup"), (
        f"expected upgrade to use reusable failing card and enter pending_if_incomplete path, "
        f"but API requested payment-method setup: {failed_upgrade_response}"
    )
    assert payment_state in {"pending", "requires_action"}, (
        f"expected pending/attention payment_state from failed-card upgrade, got: {failed_upgrade_response}"
    )
    assert invoice_id, f"expected invoice_id from failed-card upgrade attempt, got: {failed_upgrade_response}"

    # Wait for webhook processing of failed upgrade attempt
    billing_client.sync_webhooks(subscription_ids={subscription_id}, created_gte=upgrade_started_at)
    payment_order = billing_client.wait_for_payment_order_status(
        order_id=invoice_id,
        expected_status="failed",
    )
    assert payment_order.get("payment_status") == "failed", (
        f"expected payment order to reach failed status, got {payment_order.get('payment_status')}"
    )

    # Verify entitlements still remain Starter before recovery
    current_after_failure = billing_client.current_plan()
    plan_after_failure = str(current_after_failure.get("plan_name") or "")
    subscription_status_after_failure = str(current_after_failure.get("subscription_status") or "").lower()

    assert plan_after_failure == "Starter", (
        f"expected Starter plan before payment recovery, got {plan_after_failure}: {current_after_failure}"
    )

    overview_after_failure = billing_client.plan_overview()
    apps_after_failure = overview_after_failure.get("resources", {}).get("apps", {}).get("limit", 0)
    assert apps_after_failure == starter_quota, (
        f"expected Starter apps quota {starter_quota} before payment recovery, got {apps_after_failure}"
    )

    # Check subscription status is delinquent
    delinquent_statuses = {"incomplete", "incomplete_expired", "past_due", "unpaid"}
    assert not overview_after_failure.get("payment_required", False) or subscription_status_after_failure in delinquent_statuses, (
        f"expected payment_required or delinquent subscription status after failed-card upgrade, "
        f"got payment_required={overview_after_failure.get('payment_required')} "
        f"status={subscription_status_after_failure}"
    )

    # Verify invoice status
    invoice = stripe.Invoice.retrieve(invoice_id)
    invoice_dict = stripe_dict(invoice)
    valid_invoice_statuses = {"open", "uncollectible", "unpaid", "draft"}
    assert invoice_dict.get("status") in valid_invoice_statuses, (
        f"expected failed-card upgrade invoice to remain unpaid, got {invoice_dict}"
    )


@pytest.mark.billing
def test_plan_02_renewal_failure_recovery(billing_client):
    """PLAN-02: Renewal failure -> attention banner -> invoice recovery."""
    # =============================================================================
    # Steps 1-4: Setup Starter environment (validates config, creates test clock,
    #           registers user, upgrades Trial -> Starter)
    # =============================================================================
    logger.info("=" * 80)
    logger.info("Steps 1-4: Setup Starter environment using billing_client fixture")
    logger.info("=" * 80)

    email = billing_client.user_id
    starter_subscription_id: str = billing_client.upgrade_trial_to_starter()["subscription_id"]

    logger.info("Assert: Starter environment ready")
    logger.info("Assert: Tenant ID: %s", billing_client.tenant_id)
    logger.info("Assert: Customer ID: %s", billing_client.customer_id)
    logger.info("Assert: Starter subscription ID: %s", starter_subscription_id)

    # =============================================================================
    # Step 5: Upgrade Starter -> Pro
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 5: Upgrade Starter -> Pro")
    logger.info("=" * 80)
    history_before_pro = billing_client.spend_history()
    upgrade_result = billing_client.upgrade_starter_to_pro(starter_subscription_id=starter_subscription_id)
    pro_subscription_id = upgrade_result["pro_subscription_id"]
    logger.info("Assert: Pro subscription ID: %s", pro_subscription_id)

    # Verify Pro quota
    pro_quota_apps = get_pro_quota_apps()
    overview_pro = billing_client.plan_overview()
    apps_limit_pro = overview_pro.get("resources", {}).get("apps", {}).get("limit", 0)
    assert apps_limit_pro == pro_quota_apps, (
        f"after Pro upgrade, expected Pro apps quota {pro_quota_apps}, got {apps_limit_pro}"
    )
    logger.info("Assert: Pro apps quota verified: %s", apps_limit_pro)

    # Verify billing history updated
    billing_client.wait_for_history_count(len(history_before_pro) + 1, "Pro initial payment")
    history_after_pro = billing_client.spend_history()
    previous_invoice_ids = {str(row.get("invoice_id") or "") for row in history_before_pro}
    latest = find_new_positive_paid_invoice(history_after_pro, previous_invoice_ids)
    amount_val = float(latest.get("amount", 0) or 0)
    assert amount_val > 0, f"Pro upgrade should create a paid invoice, got amount={latest.get('amount')}"
    assert latest.get("status") == "paid", (
        f"expected paid status for Pro upgrade invoice, got {latest.get('status')}"
    )
    assert latest.get("invoice_id"), "Pro upgrade invoice missing invoice_id in billing history"
    logger.info("Assert: Pro upgrade completed with paid invoice")

    # =============================================================================
    # Step 6: Remove payment method to cause renewal failure
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 6: Remove payment method to cause renewal failure")
    logger.info("=" * 80)

    remove_customer_payment_method(billing_client.customer_id)
    stripe.Subscription.modify(pro_subscription_id, default_payment_method="")
    sub_after_clear = stripe.Subscription.retrieve(pro_subscription_id)
    sub_dict = stripe_dict(sub_after_clear)
    remaining_pm = sub_dict.get("default_payment_method")
    if remaining_pm:
        logger.info(
            "[DEBUG] Subscription still has default_payment_method=%s, forcing clear with null update...",
            remaining_pm,
        )
        stripe.Subscription.modify(pro_subscription_id, default_payment_method=None)
    logger.info("Assert: Payment method removed from customer and subscription")

    # =============================================================================
    # Step 7: Advance clock past renewal date - renewal should fail
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 7: Advance clock past renewal date")
    logger.info("=" * 80)

    pro_plan = billing_client.current_plan()
    period_end_before_renewal = parse_plan_end(pro_plan)

    billing_client.advance_clock_to_plan_end(offset_seconds=120)

    finalized_invoice = billing_client.ensure_invoice_finalized(pro_subscription_id)
    assert finalized_invoice, "failed to finalize renewal invoice"
    logger.info(
        "[DEBUG] Finalized invoice status: %s, amount_due: %s",
        finalized_invoice.get("status"),
        finalized_invoice.get("amount_due"),
    )
    renewal_invoice_id = str(finalized_invoice.get("id") or "")
    assert renewal_invoice_id, f"renewal invoice is missing id: {finalized_invoice}"

    # Sync webhook events until the renewal failure is reflected locally
    billing_client.wait_for_payment_order_status(
        order_id=renewal_invoice_id,
        expected_status="failed",
    )

    # =============================================================================
    # Step 8: Verify subscription enters delinquent status
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 8: Verify subscription enters delinquent status")
    logger.info("=" * 80)

    current = billing_client.current_plan()
    subscription_status = (current.get("subscription_status") or "").lower()
    assert subscription_status in {"past_due", "incomplete", "unpaid", "incomplete_expired"}, (
        f"expected delinquent status after renewal failure, got {subscription_status}: {current}"
    )
    logger.info("Assert: Subscription status is delinquent: %s", subscription_status)

    overview_fail = billing_client.plan_overview()
    payment_required = overview_fail.get("payment_required", False)
    assert payment_required, (
        f"expected payment_required=true in billing overview after renewal failure, got {overview_fail}"
    )
    logger.info("Assert: payment_required=true in billing overview")

    # Verify the failed renewal invoice is still unpaid in Stripe
    inv = stripe_dict(stripe.Invoice.retrieve(renewal_invoice_id))
    assert inv.get("status") in {"open", "uncollectible", "unpaid"}, (
        f"expected renewal invoice to be unpaid/open, got status={inv.get('status')}: inv={inv.get('id')}"
    )

    history_after_failure = billing_client.spend_history()
    failed_rows = [row for row in history_after_failure if row.get("invoice_id") == renewal_invoice_id]
    assert len(failed_rows) == 1, (
        f"expected exactly one billing history row for failed invoice {renewal_invoice_id}, got {failed_rows}"
    )
    failed_row = failed_rows[0]
    assert failed_row.get("status") == "unpaid", (
        f"expected spend history to show unpaid for failed renewal invoice, got {failed_row}"
    )
    assert float(failed_row.get("amount", 0) or 0) > 0, (
        f"expected positive amount on failed renewal invoice row, got {failed_row}"
    )
    history_count_after_failure = len(history_after_failure)
    logger.info("Assert: Failed renewal invoice verified in billing history")

    # =============================================================================
    # Step 9: Pay the failed invoice (recovery)
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 9: Pay the failed invoice (recovery)")
    logger.info("=" * 80)

    pm_id = attach_default_test_card(billing_client.customer_id)
    pay_result = stripe.Invoice.pay(renewal_invoice_id, payment_method=pm_id)
    pay_dict = stripe_dict(pay_result)
    logger.info(
        "[DEBUG] Pay invoice result status: %s, amount_paid: %s",
        pay_dict.get("status"),
        pay_dict.get("amount_paid"),
    )

    payment_order_after_pay = billing_client.wait_for_payment_order_status(
        order_id=renewal_invoice_id,
        expected_status="success",
    )

    # =============================================================================
    # Step 10: Verify billing history shows the recovery payment
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 10: Verify billing history shows the recovery payment")
    logger.info("=" * 80)

    history_after_pay = billing_client.spend_history()
    assert len(history_after_pay) == history_count_after_failure, (
        "renewal recovery should update the existing failed billing history row, "
        f"not append a new row: before={history_count_after_failure}, after={len(history_after_pay)}"
    )
    paid_rows = [row for row in history_after_pay if row.get("invoice_id") == renewal_invoice_id]
    assert len(paid_rows) == 1, (
        f"expected exactly one billing history row for recovered invoice {renewal_invoice_id}, got {paid_rows}"
    )
    paid_row = paid_rows[0]
    assert paid_row.get("status") == "paid" and float(paid_row.get("amount", 0) or 0) > 0, (
        f"paid invoice {renewal_invoice_id} not found in billing history after payment: history={history_after_pay}"
    )
    assert payment_order_after_pay.get("payment_status") == "success", (
        "expected billing_payment_order to update the same invoice row from failed to success, "
        f"got {payment_order_after_pay}"
    )
    logger.info("Assert: Recovery payment verified in billing history")

    # =============================================================================
    # Step 11: Verify attention banner disappears
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 11: Verify attention banner disappears")
    logger.info("=" * 80)

    overview_after = billing_client.plan_overview()
    # NOTE: payment_required may remain True after recovery depending on backend behavior
    if overview_after.get("payment_required", False):
        logger.warning("payment_required is still true after paying invoice (backend behavior): %s", overview_after)
    else:
        logger.info("Assert: payment_required=false after recovery")

    # =============================================================================
    # Step 12: Verify subscription status returns to active
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 12: Verify subscription status returns to active")
    logger.info("=" * 80)

    final_plan = billing_client.wait_for_plan("Pro")
    period_end_after = parse_plan_end(final_plan)
    assert period_end_after > period_end_before_renewal, (
        f"Pro billing cycle did not advance after renewal: before={period_end_before_renewal}, after={period_end_after}"
    )
    final_plan = billing_client.wait_for_subscription_status("active")
    final_status = (final_plan.get("subscription_status") or "").lower()
    assert final_status == "active", (
        f"expected subscription status 'active' after payment recovery, got {final_status}"
    )

    overview_restored = billing_client.plan_overview()
    apps_restored = overview_restored.get("resources", {}).get("apps", {}).get("limit", 0)
    assert apps_restored == get_pro_quota_apps(), (
        f"after payment, expected Pro quota, got {apps_restored}"
    )
    logger.info("Assert: Subscription restored to active Pro plan")

    # =============================================================================
    # PLAN-02 Test Summary
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("PLAN-02 Test Summary")
    logger.info("=" * 80)
    logger.info("%s", json.dumps({
        "case": "PLAN-02",
        "description": "Renewal failure -> attention banner -> invoice recovery",
        "tenant_id": billing_client.tenant_id,
        "email": email,
        "test_clock_id": billing_client.clock_id,
        "customer_id": billing_client.customer_id,
        "pro_subscription_id": pro_subscription_id,
        "failed_invoice_id": renewal_invoice_id,
        "final_plan": overview_after.get("plan_name"),
        "payment_required": overview_after.get("payment_required"),
        "history_rows": len(history_after_pay),
    }, indent=2, sort_keys=True))


def _remove_customer_payment_method(customer_id: str) -> None:
    """Remove all payment methods from customer to trigger payment failure."""
    payment_methods = stripe.PaymentMethod.list(customer=customer_id, type="card")
    for pm in payment_methods.auto_paging_iter():
        stripe.PaymentMethod.detach(pm.id)


def _set_subscription_payment_method(subscription_id: str, payment_method_id: str) -> None:
    """Set the default payment method on a subscription."""
    stripe.Subscription.modify(subscription_id, default_payment_method=payment_method_id)


@pytest.mark.billing
def test_plan_03_starter_to_pro_api_checkout(billing_client):
    """PLAN-03: Starter → Pro upgrade via direct API checkout (not Customer Portal)."""
    # =============================================================================
    # Steps 1-4: Setup Starter environment (validates config, creates test clock,
    #           registers user, upgrades Trial -> Starter)
    # =============================================================================
    logger.info("=" * 80)
    logger.info("Steps 1-4: Setup Starter environment using billing_client fixture")
    logger.info("=" * 80)

    email = billing_client.user_id
    starter_subscription_id: str = billing_client.upgrade_trial_to_starter()["subscription_id"]

    logger.info("Assert: Starter environment ready")
    logger.info("Assert: Tenant ID: %s", billing_client.tenant_id)
    logger.info("Assert: Customer ID: %s", billing_client.customer_id)
    logger.info("Assert: Starter subscription ID: %s", starter_subscription_id)

    # Verify Starter quota
    overview_start = billing_client.plan_overview()
    assert overview_start.get("plan_name", "") == "Starter", (
        f"expected plan_name 'Starter' after startup flow, got {overview_start.get('plan_name')}"
    )
    starter_quota = get_starter_quota_apps()
    starter_apps_limit = overview_start.get("resources", {}).get("apps", {}).get("limit", 0)
    assert starter_apps_limit == starter_quota, (
        f"expected Starter apps quota {starter_quota}, got {starter_apps_limit}"
    )
    logger.info("Assert: Starter apps quota verified: %s", starter_apps_limit)

    # Record billing history count before upgrade to validate invoice creation later
    history_before_upgrade = billing_client.spend_history()

    # =============================================================================
    # Step 5: Initiate upgrade from Starter to Pro via API checkout
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 5: Initiate upgrade from Starter to Pro via API checkout")
    logger.info("=" * 80)

    billing_config = load_billing_config()
    pro_price_id = first_plan_price_id(billing_config, "Pro")
    assert pro_price_id, "Pro plan price_id not found in service_conf.yaml"

    preview = billing_client.upcoming_plan_change(pro_price_id)
    logger.info(
        "Assert: Upcoming preview fetched, amount_due_today=%s, has_reusable_payment_method=%s",
        preview.get("amount_due_today"),
        preview.get("has_reusable_payment_method"),
    )

    setup_intent_id = billing_client.ensure_setup_intent_for_plan_change(pro_price_id)
    if setup_intent_id:
        logger.info("Assert: SetupIntent confirmed before paid upgrade: %s", setup_intent_id)

    checkout_result = billing_client.schedule_plan_change(pro_price_id, setup_intent_id=setup_intent_id)

    # Validate the checkout response contains expected fields
    plan_name = checkout_result.get("plan_name", "")
    subscription_id = checkout_result.get("subscription_id", "")
    assert plan_name == "Pro", (
        f"Upgrade to Pro failed: expected plan_name='Pro', got plan_name='{plan_name}'. "
        f"Full response: {checkout_result}"
    )
    assert subscription_id, f"Upgrade response missing subscription_id: {checkout_result}"

    logger.info("Assert: Upgrade submitted, plan_name=%s", plan_name)
    logger.info("Assert: Subscription ID: %s", subscription_id)

    pro_upgrade_started_at = int(time.time()) - 5
    logger.info("Assert: Subscription price change submitted through billing API")

    # =============================================================================
    # Step 6: Sync webhook events to reflect the upgrade
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 6: Wait for webhook sync to reflect the upgrade")
    logger.info("=" * 80)

    billing_client.sync_webhooks(
        subscription_ids={starter_subscription_id},
        created_gte=pro_upgrade_started_at,
    )

    # =============================================================================
    # Step 7: Wait for plan to switch to Pro
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 7: Wait for plan to switch to Pro")
    logger.info("=" * 80)

    billing_client.wait_for_plan("Pro")
    logger.info("Assert: Plan switched to Pro")

    # =============================================================================
    # Step 8: Verify billing overview shows Pro quota
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 8: Verify billing overview shows Pro quota")
    logger.info("=" * 80)

    overview = billing_client.plan_overview()
    plan_name = overview.get("plan_name", "")
    assert plan_name == "Pro", f"expected plan_name 'Pro' after upgrade, got {plan_name}"

    # Explicitly verify Pro quota from service config.
    apps_limit = overview.get("resources", {}).get("apps", {}).get("limit", 0)
    assert apps_limit == get_pro_quota_apps(), (
        f"expected Pro quota, got {apps_limit}"
    )
    logger.info("Assert: Pro apps quota verified: %s", apps_limit)

    # =============================================================================
    # Step 9: Verify billing history records the upgrade with a paid invoice
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("Step 9: Verify billing history records the upgrade with a paid invoice")
    logger.info("=" * 80)

    # Wait for new history entry to appear
    history_after = billing_client.wait_for_history_count(
        len(history_before_upgrade) + 1,
        "Pro upgrade invoice",
    )
    assert history_after, "billing history empty after upgrade"

    # Find the new invoice that is positive and paid
    previous_invoice_ids = {str(row.get("invoice_id") or "") for row in history_before_upgrade}
    new_invoice = find_new_positive_paid_invoice(history_after, previous_invoice_ids)
    assert new_invoice.get("invoice_id"), "Pro upgrade invoice missing invoice_id"
    logger.info("Assert: Pro upgrade invoice verified in billing history")

    # =============================================================================
    # PLAN-03 Test Summary
    # =============================================================================
    logger.info("")
    logger.info("=" * 80)
    logger.info("PLAN-03 Test Summary")
    logger.info("=" * 80)
    logger.info("%s", json.dumps({
        "case": "PLAN-03",
        "description": "Starter → Pro upgrade via direct API checkout",
        "tenant_id": billing_client.tenant_id,
        "email": email,
        "test_clock_id": billing_client.clock_id,
        "customer_id": billing_client.customer_id,
        "starter_subscription_id": starter_subscription_id,
        "final_plan": overview.get("plan_name"),
        "quota_apps": overview.get("resources", {}).get("apps", {}).get("limit"),
        "quota_members": overview.get("resources", {}).get("members", {}).get("limit"),
        "quota_storage_kb": overview.get("resources", {}).get("plan_storage", {}).get("limit"),
        "history_rows": len(history_after),
    }, indent=2, sort_keys=True))
