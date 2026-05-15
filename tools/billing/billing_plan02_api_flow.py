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
API-adjusted driver for PLAN-02.
Tests: renewal failure → attention banner → invoice recovery.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import uuid

import stripe

from tools.billing.billing_common import (  # noqa: E402
    FlowError, stripe_dict,
    make_default_parser, find_new_positive_paid_invoice, parse_plan_end, get_pro_quota_apps,
    remove_customer_payment_method,
)
from tools.billing.billing_client import (  # noqa: E402
    BillingClient, create_client,
)
from tools.billing.storage_common import (  # noqa: E402
    attach_default_test_card,
)

def run_flow(args: argparse.Namespace) -> None:
    """Execute PLAN-02: renewal failure → attention banner → invoice recovery flow."""
    # =============================================================================
    # Steps 1-4: Setup Starter environment (validates config, creates test clock,
    #           registers user, upgrades Trial -> Starter)
    # =============================================================================
    print("=" * 80)
    print("Steps 1-4: Setup Starter environment using shared utility")
    print("=" * 80)

    email = args.email or f"billing-plan02-{uuid.uuid4().hex[:12]}@example.test"
    client: BillingClient = create_client(args, email)
    starter_subscription_id: str = client.upgrade_trial_to_starter()["subscription_id"]

    print("  Assert: Starter environment ready")
    print(f"  Assert: Tenant ID: {client.tenant_id}")
    print(f"  Assert: Customer ID: {client.customer_id}")
    print(f"  Assert: Starter subscription ID: {starter_subscription_id}")

    # =============================================================================
    # Step 5: Upgrade Starter -> Pro
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 5: Upgrade Starter -> Pro")
    print("=" * 80)
    history_before_pro = client.spend_history()
    upgrade_result = client.upgrade_starter_to_pro(
        starter_subscription_id=starter_subscription_id,
        webhook_wait_seconds=args.webhook_wait_seconds,
        webhook_timeout_seconds=args.webhook_timeout_seconds,
    )
    pro_subscription_id = upgrade_result["pro_subscription_id"]
    print(f"  Assert: Pro subscription ID: {pro_subscription_id}")

    # Verify Pro quota
    pro_quota_apps = get_pro_quota_apps()
    overview_pro = client.plan_overview()
    apps_limit_pro = overview_pro.get("resources", {}).get("apps", {}).get("limit", 0)
    if apps_limit_pro != pro_quota_apps:
        raise RuntimeError(f"after Pro upgrade, expected Pro apps quota {pro_quota_apps}, got {apps_limit_pro}")
    print(f"  Assert: Pro apps quota verified: {apps_limit_pro}")

    # Verify billing history updated
    client.wait_for_history_count(len(history_before_pro) + 1, args.webhook_timeout_seconds, "Pro initial payment")
    history_after_pro = client.spend_history()
    previous_invoice_ids = {str(row.get("invoice_id") or "") for row in history_before_pro}
    latest = find_new_positive_paid_invoice(history_after_pro, previous_invoice_ids)
    amount_val = float(latest.get("amount", 0) or 0)
    if amount_val <= 0:
        raise FlowError(f"Pro upgrade should create a paid invoice, got amount={latest.get('amount')}")
    if latest.get("status") != "paid":
        raise FlowError(f"expected paid status for Pro upgrade invoice, got {latest.get('status')}")
    if not latest.get("invoice_id"):
        raise FlowError("Pro upgrade invoice missing invoice_id in billing history")
    print("  Assert: Pro upgrade completed with paid invoice")

    # =============================================================================
    # Step 6: Remove payment method to cause renewal failure
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Remove payment method to cause renewal failure")
    print("=" * 80)

    remove_customer_payment_method(client.customer_id)
    stripe.Subscription.modify(pro_subscription_id, default_payment_method="")
    sub_after_clear = stripe.Subscription.retrieve(pro_subscription_id)
    sub_dict = stripe_dict(sub_after_clear)
    remaining_pm = sub_dict.get("default_payment_method")
    if remaining_pm:
        print(f"[DEBUG] Subscription still has default_payment_method={remaining_pm}, forcing clear with null update...")
        stripe.Subscription.modify(pro_subscription_id, default_payment_method=None)
    print("  Assert: Payment method removed from customer and subscription")

    # =============================================================================
    # Step 7: Advance clock past renewal date - renewal should fail
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 7: Advance clock past renewal date")
    print("=" * 80)

    pro_plan = client.current_plan()
    period_end_before_renewal = parse_plan_end(pro_plan)

    client.advance_clock_to_plan_end(offset_seconds=120)

    finalized_invoice = client.ensure_invoice_finalized(pro_subscription_id)
    if not finalized_invoice:
        raise FlowError("failed to finalize renewal invoice")
    print(f"[DEBUG] Finalized invoice status: {finalized_invoice.get('status')}, amount_due: {finalized_invoice.get('amount_due')}")
    renewal_invoice_id = str(finalized_invoice.get("id") or "")
    if not renewal_invoice_id:
        raise FlowError(f"renewal invoice is missing id: {finalized_invoice}")

    # Sync webhook events until the renewal failure is reflected locally
    client.replay_until_payment_order_status(
        subscription_ids={pro_subscription_id},
        created_gte=int(time.time()) - 60,
        order_id=renewal_invoice_id,
        expected_status="failed",
        timeout_seconds=args.webhook_timeout_seconds,
        wait_seconds=args.webhook_wait_seconds,
    )

    # =============================================================================
    # Step 8: Verify subscription enters delinquent status
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 8: Verify subscription enters delinquent status")
    print("=" * 80)

    current = client.current_plan()
    subscription_status = (current.get("subscription_status") or "").lower()
    if subscription_status not in {"past_due", "incomplete", "unpaid", "incomplete_expired"}:
        raise FlowError(f"expected delinquent status after renewal failure, got {subscription_status}: {current}")
    print(f"  Assert: Subscription status is delinquent: {subscription_status}")

    overview_fail = client.plan_overview()
    payment_required = overview_fail.get("payment_required", False)
    if not payment_required:
        raise FlowError(f"expected payment_required=true in billing overview after renewal failure, got {overview_fail}")
    print("  Assert: payment_required=true in billing overview")

    # Verify the failed renewal invoice is still unpaid in Stripe
    inv = stripe_dict(stripe.Invoice.retrieve(renewal_invoice_id))
    if inv.get("status") != "open" and inv.get("status") != "uncollectible" and inv.get("status") != "unpaid":
        raise FlowError(f"expected renewal invoice to be unpaid/open, got status={inv.get('status')}: inv={inv.get('id')}")

    history_after_failure = client.spend_history()
    failed_rows = [row for row in history_after_failure if row.get("invoice_id") == renewal_invoice_id]
    if len(failed_rows) != 1:
        raise FlowError(f"expected exactly one billing history row for failed invoice {renewal_invoice_id}, got {failed_rows}")
    failed_row = failed_rows[0]
    if failed_row.get("status") != "unpaid":
        raise FlowError(f"expected spend history to show unpaid for failed renewal invoice, got {failed_row}")
    if float(failed_row.get("amount", 0) or 0) <= 0:
        raise FlowError(f"expected positive amount on failed renewal invoice row, got {failed_row}")
    history_count_after_failure = len(history_after_failure)
    print("  Assert: Failed renewal invoice verified in billing history")

    # =============================================================================
    # Step 9: Pay the failed invoice (recovery)
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 9: Pay the failed invoice (recovery)")
    print("=" * 80)

    pm_id = attach_default_test_card(client.customer_id)
    pay_result = stripe.Invoice.pay(renewal_invoice_id, payment_method=pm_id)
    pay_dict = stripe_dict(pay_result)
    print(f"[DEBUG] Pay invoice result status: {pay_dict.get('status')}, amount_paid: {pay_dict.get('amount_paid')}")

    pay_started_at = int(time.time()) - 5
    payment_order_after_pay = client.replay_until_payment_order_status(
        subscription_ids={pro_subscription_id},
        created_gte=pay_started_at,
        order_id=renewal_invoice_id,
        expected_status="success",
        timeout_seconds=args.webhook_timeout_seconds,
        wait_seconds=args.webhook_wait_seconds,
    )

    # =============================================================================
    # Step 10: Verify billing history shows the recovery payment
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 10: Verify billing history shows the recovery payment")
    print("=" * 80)

    history_after_pay = client.spend_history()
    if len(history_after_pay) != history_count_after_failure:
        raise FlowError(
            "renewal recovery should update the existing failed billing history row, "
            f"not append a new row: before={history_count_after_failure}, after={len(history_after_pay)}"
        )
    paid_rows = [row for row in history_after_pay if row.get("invoice_id") == renewal_invoice_id]
    if len(paid_rows) != 1:
        raise FlowError(
            f"expected exactly one billing history row for recovered invoice {renewal_invoice_id}, got {paid_rows}"
        )
    paid_row = paid_rows[0]
    if paid_row.get("status") != "paid" or float(paid_row.get("amount", 0) or 0) <= 0:
        raise FlowError(f"paid invoice {renewal_invoice_id} not found in billing history after payment: history={history_after_pay}")
    if payment_order_after_pay.get("payment_status") != "success":
        raise FlowError(
            "expected billing_payment_order to update the same invoice row from failed to success, "
            f"got {payment_order_after_pay}"
        )
    print("  Assert: Recovery payment verified in billing history")

    # =============================================================================
    # Step 11: Verify attention banner disappears
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 11: Verify attention banner disappears")
    print("=" * 80)

    overview_after = client.plan_overview()
    if overview_after.get("payment_required", False):
        raise FlowError(f"payment_required should be false after paying invoice: {overview_after}")
    print("  Assert: payment_required=false after recovery")

    # =============================================================================
    # Step 12: Verify subscription status returns to active
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 12: Verify subscription status returns to active")
    print("=" * 80)

    final_plan = client.wait_for_plan("Pro", args.webhook_timeout_seconds)
    period_end_after = parse_plan_end(final_plan)
    if period_end_after <= period_end_before_renewal:
        raise FlowError(f"Pro billing cycle did not advance after renewal: before={period_end_before_renewal}, after={period_end_after}")
    final_status = (final_plan.get("subscription_status") or "").lower()
    if final_status != "active":
        raise FlowError(f"expected subscription status 'active' after payment recovery, got {final_status}")

    overview_restored = client.plan_overview()
    apps_restored = overview_restored.get("resources", {}).get("apps", {}).get("limit", 0)
    if apps_restored != get_pro_quota_apps():
        raise FlowError(f"after payment, expected Pro quota, got {apps_restored}")
    print("  Assert: Subscription restored to active Pro plan")

    # =============================================================================
    # PLAN-02 Test Summary
    # =============================================================================
    print("\n" + "=" * 80)
    print("PLAN-02 Test Summary")
    print("=" * 80)
    print(
        json.dumps(
            {
                "case": "PLAN-02",
                "description": "Renewal failure → attention banner → invoice recovery",
                "tenant_id": client.tenant_id,
                "email": email,
                "test_clock_id": client.clock_id,
                "customer_id": client.customer_id,
                "pro_subscription_id": pro_subscription_id,
                "failed_invoice_id": renewal_invoice_id,
                "final_plan": overview_after.get("plan_name"),
                "payment_required": overview_after.get("payment_required"),
                "history_rows": len(history_after_pay),
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing PLAN-02: renewal failure → attention → recovery.")
    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
