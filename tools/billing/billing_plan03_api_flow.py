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
Pure API driver for the PLAN-03 case documented in tools/billing/README.md.
Tests: Starter -> Pro upgrade via direct API checkout (not Customer Portal).

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
  BILLING_WEBHOOK_SECRET or STRIPE_WEBHOOK_SECRET (optional if local DB already stores billing_webhook_secret for manual webhook mode)
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import uuid

from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    first_plan_price_id,
    get_starter_quota_apps,
    load_billing_config,
    make_default_parser, find_new_positive_paid_invoice, get_pro_quota_apps,
)
from tools.billing.billing_client import (  # noqa: E402
    BillingClient, create_client,
)

def run_flow(args: argparse.Namespace) -> None:
    """Execute PLAN-03: Starter → Pro upgrade via direct API checkout."""
    # =============================================================================
    # Steps 1-4: Setup Starter environment (validates config, creates test clock,
    #           registers user, upgrades Trial -> Starter)
    # =============================================================================
    print("=" * 80)
    print("Steps 1-4: Setup Starter environment using shared utility")
    print("=" * 80)

    email = args.email or f"billing-plan03-{uuid.uuid4().hex[:12]}@example.test"
    client: BillingClient = create_client(args, email)
    tenant_id: str = client.tenant_id
    customer_id: str = client.customer_id
    starter_subscription_id: str = client.upgrade_trial_to_starter()["subscription_id"]

    print("  Assert: Starter environment ready")
    print(f"  Assert: Tenant ID: {tenant_id}")
    print(f"  Assert: Customer ID: {customer_id}")
    print(f"  Assert: Starter subscription ID: {starter_subscription_id}")

    # Verify Starter quota
    overview_start = client.plan_overview()
    if overview_start.get("plan_name", "") != "Starter":
        raise FlowError(f"expected plan_name 'Starter' after startup flow, got {overview_start.get('plan_name')}")
    starter_quota = get_starter_quota_apps()
    starter_apps_limit = overview_start.get("resources", {}).get("apps", {}).get("limit", 0)
    if starter_apps_limit != starter_quota:
        raise FlowError(f"expected Starter apps quota {starter_quota}, got {starter_apps_limit}")
    print(f"  Assert: Starter apps quota verified: {starter_apps_limit}")

    # Record billing history count before upgrade to validate invoice creation later
    history_before_upgrade = client.spend_history()

    # =============================================================================
    # Step 5: Initiate upgrade from Starter to Pro via API checkout
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 5: Initiate upgrade from Starter to Pro via API checkout")
    print("=" * 80)

    billing_config = load_billing_config()
    pro_price_id = first_plan_price_id(billing_config, "Pro")

    # The /billing/checkout endpoint modifies the subscription directly
    # for active paid subscriptions (no Customer Portal redirect)
    checkout_result = client.schedule_plan_change(pro_price_id)

    # Validate the checkout response contains expected fields
    plan_name = checkout_result.get("plan_name", "")
    subscription_id = checkout_result.get("subscription_id", "")
    if plan_name != "Pro":
        raise FlowError(
            f"Upgrade to Pro failed: expected plan_name='Pro', got plan_name='{plan_name}'. "
            f"Full response: {checkout_result}"
        )
    if not subscription_id:
        raise FlowError(f"Upgrade response missing subscription_id: {checkout_result}")

    print(f"  Assert: Upgrade submitted, plan_name={plan_name}")
    print(f"  Assert: Subscription ID: {subscription_id}")

    # =============================================================================
    # Step 6: Simulate completing the upgrade via Stripe API
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Simulate completing the upgrade via Stripe API")
    print("=" * 80)

    pro_upgrade_started_at = int(time.time()) - 5
    client.schedule_plan_change(pro_price_id)
    print("  Assert: Subscription price replaced with Pro price")

    # =============================================================================
    # Step 7: Sync webhook events to reflect the upgrade
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 7: Sync webhook events to reflect the upgrade")
    print("=" * 80)

    client.sync_webhooks(
        subscription_ids={starter_subscription_id},
        created_gte=pro_upgrade_started_at,
        wait_seconds=args.webhook_wait_seconds,
    )

    # =============================================================================
    # Step 8: Wait for plan to switch to Pro
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 8: Wait for plan to switch to Pro")
    print("=" * 80)

    client.wait_for_plan("Pro", args.webhook_timeout_seconds)
    print("  Assert: Plan switched to Pro")

    # =============================================================================
    # Step 9: Verify billing overview shows Pro quota
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 9: Verify billing overview shows Pro quota")
    print("=" * 80)

    overview = client.plan_overview()
    plan_name = overview.get("plan_name", "")
    if plan_name != "Pro":
        raise FlowError(f"expected plan_name 'Pro' after upgrade, got {plan_name}")

    # Explicitly verify Pro quota from service config.
    apps_limit = overview.get("resources", {}).get("apps", {}).get("limit", 0)
    if apps_limit != get_pro_quota_apps():
        raise FlowError(f"expected Pro quota, got {apps_limit}")
    print(f"  Assert: Pro apps quota verified: {apps_limit}")

    # =============================================================================
    # Step 10: Verify billing history records the upgrade with a paid invoice
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 10: Verify billing history records the upgrade with a paid invoice")
    print("=" * 80)

    # Wait for new history entry to appear
    history_after = client.wait_for_history_count(len(history_before_upgrade) + 1, args.webhook_timeout_seconds, "Pro upgrade invoice")
    if not history_after:
        raise FlowError("billing history empty after upgrade")

    # Find the new invoice that is positive and paid
    previous_invoice_ids = {str(row.get("invoice_id") or "") for row in history_before_upgrade}
    new_invoice = find_new_positive_paid_invoice(history_after, previous_invoice_ids)
    if not new_invoice.get("invoice_id"):
        raise FlowError("Pro upgrade invoice missing invoice_id")
    print("  Assert: Pro upgrade invoice verified in billing history")

    # =============================================================================
    # PLAN-03 Test Summary
    # =============================================================================
    print("\n" + "=" * 80)
    print("PLAN-03 Test Summary")
    print("=" * 80)
    print(
        json.dumps(
            {
                "case": "PLAN-03",
                "description": "Starter → Pro upgrade via direct API checkout",
                "tenant_id": client.tenant_id,
                "email": email,
                "test_clock_id": client.clock_id,
                "customer_id": client.customer_id,
                "starter_subscription_id": starter_subscription_id,
                "final_plan": overview.get("plan_name"),
                "quota_apps": overview.get("resources", {}).get("apps", {}).get("limit"),
                "quota_members": overview.get("resources", {}).get("members", {}).get("limit"),
                "quota_storage_kb": overview.get("resources", {}).get("plan_storage", {}).get("limit"),
                "history_rows": len(history_after),
                "webhook_mode": args.webhook_mode,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing PLAN-03: Starter->Pro upgrade via direct API checkout.")
    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
