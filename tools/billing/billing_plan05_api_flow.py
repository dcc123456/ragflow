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
Pure API driver for the PLAN-05 case documented in tools/billing/README.md.
Tests: Upgrade with unpaid invoice must NOT grant higher entitlements early.

Required environment:
  BILLING_STRIPE_API_KEY or STRIPE_API_KEY
  BILLING_PRICE_ID_TRIAL
  BILLING_PRICE_ID_STARTER
  BILLING_PRICE_ID_PRO

Optional environment:
  RAGFLOW_BASE_URL=http://127.0.0.1:9380
  RAGFLOW_API_VERSION=v1
  RAGFLOW_TEST_EMAIL=<fresh email>
  RAGFLOW_TEST_PASSWORD=Test1234!
  BILLING_WEBHOOK_SECRET or STRIPE_WEBHOOK_SECRET (optional only for legacy manual webhook mode)
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import uuid

import stripe  # type: ignore[reportMissingImports]

from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    first_plan_price_id,
    get_starter_quota_apps,
    load_billing_config,
    stripe_dict,
    make_default_parser, replace_subscription_price, remove_customer_payment_method, get_pro_quota_apps,
)
from tools.billing.billing_client import (  # noqa: E402
    BillingClient, create_client,
)
from tools.billing.storage_common import (  # noqa: E402
    attach_default_test_card,
)

def run_flow(args: argparse.Namespace) -> None:
    """Execute PLAN-05: upgrade with unpaid invoice must NOT grant higher entitlements early."""
    billing_config = load_billing_config()
    pro_price_id = first_plan_price_id(billing_config, "Pro")

    # =============================================================================
    # Steps 1-4: Setup Starter environment (validates config, creates test clock,
    #           registers user, upgrades Trial -> Starter)
    # =============================================================================
    print("=" * 80)
    print("Steps 1-4: Setup Starter environment using shared utility")
    print("=" * 80)

    email = args.email or f"billing-plan05-{uuid.uuid4().hex[:12]}@example.test"
    client: BillingClient = create_client(args, email)
    starter_subscription_id: str = client.upgrade_trial_to_starter()["subscription_id"]

    print("  Assert: Starter environment ready")
    print(f"  Assert: Tenant ID: {client.tenant_id}")
    print(f"  Assert: Customer ID: {client.customer_id}")
    print(f"  Assert: Starter subscription ID: {starter_subscription_id}")

    # Verify Starter quota before upgrade
    starter_quota = get_starter_quota_apps()
    overview_before = client.plan_overview()
    apps_before = overview_before.get("resources", {}).get("apps", {}).get("limit", 0)
    if apps_before != starter_quota:
        raise FlowError(f"expected Starter apps quota {starter_quota}, got {apps_before}")
    print(f"  Assert: Starter apps quota verified: {apps_before}")

    # =============================================================================
    # Step 5: Upgrade to Pro through the paid-plan upgrade path (with no payment method)
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 5: Attempt Pro upgrade with no payment method (creates unpaid invoice)")
    print("=" * 80)

    # Remove payment method to ensure the upgrade invoice remains unpaid
    remove_customer_payment_method(client.customer_id)
    stripe.Customer.modify(client.customer_id, invoice_settings={"default_payment_method": None}, default_payment_method=None)
    stripe.Subscription.modify(starter_subscription_id, default_payment_method=None)
    print("  Assert: Payment method removed from customer")

    # Simulate finishing the portal upgrade and leaving the proration invoice pending payment.
    upgrade_attempt_started_at = int(time.time()) - 5
    try:
        sub_result = replace_subscription_price(
            starter_subscription_id,
            pro_price_id,
            proration_behavior="always_invoice",
            default_payment_method=None,
            payment_behavior="default_incomplete",
            expand=["latest_invoice.payment_intent"],
        )
    except Exception as exc:
        raise FlowError(f"failed to create pending Pro upgrade invoice: {exc}") from exc

    latest_invoice = sub_result.get("latest_invoice")
    if not latest_invoice:
        raise FlowError("No invoice created during Pro upgrade")
    latest_invoice_id = str(latest_invoice.get("id") if isinstance(latest_invoice, dict) else latest_invoice)
    if not latest_invoice_id:
        raise FlowError("pending upgrade invoice is missing invoice_id")
    print(f"  Assert: Pending Pro upgrade invoice created: {latest_invoice_id}")

    # =============================================================================
    # Step 6: Sync webhook events until the unpaid upgrade invoice is reflected locally.
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Sync webhooks for unpaid upgrade invoice")
    print("=" * 80)

    history_count_before_failed_upgrade = len(client.spend_history())
    client.sync_webhooks(
        subscription_ids={starter_subscription_id},
        created_gte=upgrade_attempt_started_at,
        wait_seconds=args.webhook_wait_seconds,
    )

    # =============================================================================
    # Step 7: Verify entitlements are NOT upgraded yet (should remain at Starter level)
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 7: Verify entitlements NOT upgraded (still at Starter level)")
    print("=" * 80)

    current = client.current_plan()
    subscription_status = (current.get("subscription_status") or "").lower()

    overview = client.plan_overview()
    apps_limit = overview.get("resources", {}).get("apps", {}).get("limit", 0)

    # Apps quota must still be at Starter level
    if apps_limit != starter_quota:
        raise FlowError(f"before payment, expected Starter apps quota {starter_quota}, got {apps_limit}")

    # Plan should still show Starter (upgrade not yet effective), but payment required
    plan_before = current.get("plan_name", "").lower()
    if plan_before != "starter":
        raise FlowError(f"before payment, expected plan_name='starter' (upgrade not paid yet), got '{plan_before}'")

    latest_invoice_dict = stripe_dict(stripe.Invoice.retrieve(latest_invoice_id))
    if latest_invoice_dict.get("status") not in {"open", "uncollectible", "unpaid", "draft"}:
        raise FlowError(f"expected pending Pro upgrade invoice to remain unpaid before recovery, got {latest_invoice_dict}")

    # Check billing history for unpaid row
    history_before_payment = client.spend_history()
    failed_rows = [row for row in history_before_payment if row.get("invoice_id") == latest_invoice_id]
    if failed_rows:
        if len(failed_rows) != 1:
            raise FlowError(
                f"expected at most one billing history row for unpaid Pro upgrade invoice {latest_invoice_id}, got {failed_rows}"
            )
        failed_row = failed_rows[0]
        if failed_row.get("status") != "unpaid":
            raise FlowError(f"expected spend history to show unpaid for pending Pro upgrade invoice, got {failed_row}")
        if len(history_before_payment) != history_count_before_failed_upgrade + 1:
            raise FlowError(
                "unpaid Pro upgrade should add exactly one billing history row before recovery payment, "
                f"expected {history_count_before_failed_upgrade + 1}, got {len(history_before_payment)}"
            )

    # Should indicate payment is required (either payment_required flag or recoverable delinquency status)
    if not overview.get("payment_required", False) and subscription_status not in {"incomplete", "incomplete_expired", "past_due", "unpaid"}:
        raise FlowError(f"expected payment_required or incomplete status before payment, got status={subscription_status}, payment_required={overview.get('payment_required')}")
    print("  Assert: Entitlements still at Starter level, payment required flag set")

    # =============================================================================
    # Step 8: Pay the invoice
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 8: Pay the pending Pro upgrade invoice")
    print("=" * 80)

    pm_id = attach_default_test_card(client.customer_id)
    pay_started_at = int(time.time()) - 5
    try:
        pay_result = stripe.Invoice.pay(latest_invoice_id, payment_method=pm_id)
    except Exception as exc:
        raise FlowError(f"failed to recover pending Pro upgrade invoice {latest_invoice_id}: {exc}") from exc
    pay_dict = stripe_dict(pay_result)
    if pay_dict.get("status") != "paid":
        raise FlowError(f"expected Stripe to pay pending Pro upgrade invoice, got {pay_dict}")
    print("  Assert: Invoice paid successfully")

    # =============================================================================
    # Step 9: Sync webhook until the same invoice row becomes paid.
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 9: Sync webhooks for paid invoice")
    print("=" * 80)

    client.replay_until_payment_order_status(
        subscription_ids={starter_subscription_id},
        created_gte=pay_started_at,
        order_id=latest_invoice_id,
        expected_status="success",
        timeout_seconds=args.webhook_timeout_seconds,
        wait_seconds=args.webhook_wait_seconds,
    )

    # =============================================================================
    # Step 10: Verify upgrade to Pro completed
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 10: Verify upgrade to Pro completed")
    print("=" * 80)

    final_plan = client.wait_for_plan("Pro", args.webhook_timeout_seconds)
    final_overview = client.plan_overview()
    final_apps_limit = final_overview.get("resources", {}).get("apps", {}).get("limit", 0)
    pro_quota = get_pro_quota_apps()

    if final_apps_limit != pro_quota:
        raise FlowError(f"after payment, expected Pro apps quota {pro_quota}, got {final_apps_limit}")

    # Payment required flag must be cleared after invoice paid
    if final_overview.get("payment_required", False):
        raise FlowError(f"after payment, payment_required should be false, got: {final_overview.get('payment_required')}")
    print(f"  Assert: Pro apps quota verified: {final_apps_limit}")

    # =============================================================================
    # Step 11: Billing history should show the paid Pro invoice after recovery.
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 11: Verify billing history shows paid Pro invoice")
    print("=" * 80)

    history = client.spend_history()
    paid_rows = [row for row in history if row.get("invoice_id") == latest_invoice_id]
    if len(paid_rows) != 1:
        raise FlowError(f"expected exactly one billing history row for recovered Pro upgrade invoice {latest_invoice_id}, got {paid_rows}")
    latest_paid = paid_rows[0]
    if latest_paid.get("status", "").lower() != "paid" or float(latest_paid.get("amount", 0) or 0) <= 0:
        raise FlowError(f"paid Pro invoice not found in billing history after payment: {history}")
    print("  Assert: Paid Pro invoice found in billing history")

    # =============================================================================
    # PLAN-05 Test Summary
    # =============================================================================
    print("\n" + "=" * 80)
    print("PLAN-05 Test Summary")
    print("=" * 80)
    print(
        json.dumps(
            {
                "case": "PLAN-05",
                "description": "Upgrade with unpaid invoice must NOT grant higher entitlements early",
                "tenant_id": client.tenant_id,
                "email": email,
                "test_clock_id": client.clock_id,
                "customer_id": client.customer_id,
                "starter_subscription_id": starter_subscription_id,
                "pro_subscription_id": starter_subscription_id,
                "unpaid_invoice_id": latest_invoice_id,
                "final_plan": final_plan.get("plan_name"),
                "quota_apps_final": final_apps_limit,
                "payment_required_final": final_overview.get("payment_required", False),
                "history_rows": len(history),
                "webhook_mode": args.webhook_mode,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing PLAN-05: unpaid upgrade invoice must not grant entitlements early.")
    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
