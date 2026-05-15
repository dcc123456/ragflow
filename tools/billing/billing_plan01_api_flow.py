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
API-adjusted driver for PLAN-01.
Tests: full subscription lifecycle: Trial→Pro→Starter→Trial→Starter with renewals.

New subscription model:
- One user can only have ONE subscription
- Plan changes happen within the same subscription
- Renewal happens at period end
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import uuid

from tools.billing.billing_common import (  # noqa: E402
    first_plan_price_id,
    get_starter_quota_apps,
    get_trial_quota_apps,
    load_billing_config, FlowError,
    make_default_parser,
    find_new_positive_paid_invoice, parse_plan_end, get_pro_quota_apps,
)
from tools.billing.billing_client import (  # noqa: E402
    BillingClient, create_client,
)

def run_flow(args: argparse.Namespace) -> None:
    """Execute PLAN-01: full subscription lifecycle test."""
    # =============================================================================
    # Steps 1-4: Setup Starter environment (validates config, creates test clock,
    #           registers user, upgrades Trial -> Starter)
    # =============================================================================
    print("=" * 80)
    print("Steps 1-4: Setup Starter environment using shared utility")
    print("=" * 80)

    email = args.email or f"billing-plan01-{uuid.uuid4().hex[:12]}@example.test"
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
    history_before_upgrade = client.spend_history()
    upgrade_result = client.upgrade_starter_to_pro(
        starter_subscription_id=starter_subscription_id,
        webhook_wait_seconds=args.webhook_wait_seconds,
        webhook_timeout_seconds=args.webhook_timeout_seconds,
    )
    pro_subscription_id = upgrade_result["pro_subscription_id"]
    pro_plan = upgrade_result["current_plan"]
    print(f"  Assert: Pro subscription ID: {pro_subscription_id}")

    # Verify: After Starter->Pro upgrade, there should be a new invoice with amount $200 (259-59=200, i.e., 20000 cents)
    client.wait_for_history_count(len(history_before_upgrade)+1, args.webhook_timeout_seconds, "Wait for Pro")

    history_after_upgrade = client.spend_history()
    new_invoice = [row for row in history_after_upgrade if row.get("status", "") == "paid" and int(row.get("amount", 0)) == 200]
    if len(new_invoice) != 1:
        raise FlowError(f"expected 1 invoice paid with 200 USD, got {len(new_invoice)}")

    # Verify Pro quota
    pro_quota_apps = get_pro_quota_apps()
    overview_pro = client.plan_overview()
    apps_limit_pro = overview_pro.get("resources", {}).get("apps", {}).get("limit", 0)
    if apps_limit_pro != pro_quota_apps:
        raise RuntimeError(f"after Pro upgrade, expected Pro apps quota {pro_quota_apps}, got {apps_limit_pro}")
    print(f"  Assert: Pro apps quota verified: {apps_limit_pro}")

    # =============================================================================
    # Step 6: Pro renewal at period end
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Pro renewal at period end")
    print("=" * 80)

    pro_period_end_before_renewal = parse_plan_end(pro_plan)
    history_before_pro_renewal = client.spend_history()

    created_gte = int(time.time()) - 5
    client.advance_clock_to_plan_end()

    # Get the latest invoice ID from the subscription and post invoice.paid event
    # client.ensure_invoice_finalized(pro_subscription_id)


    # client.post_invoice_paid_event(invoice_id)
    # print("  Assert: Invoice.paid event posted after Pro renewal clock advance")

    client.sync_webhooks(
        subscription_ids={pro_subscription_id},
        created_gte=created_gte,
        wait_seconds=args.webhook_wait_seconds,
    )
    pro_plan_after = client.wait_for_plan("Pro", args.webhook_timeout_seconds)
    pro_period_end_after = parse_plan_end(pro_plan_after)
    if pro_period_end_after <= pro_period_end_before_renewal:
        raise RuntimeError(f"Pro billing cycle did not advance after renewal: before={pro_period_end_before_renewal}, after={pro_period_end_after}")
    client.wait_for_history_count(
        len(history_before_pro_renewal) + 1,
        args.webhook_timeout_seconds,
        "Pro renewal",
    )
    history_after_pro_renewal = client.spend_history()
    new_invoice = [row for row in history_after_pro_renewal if row.get("status", "") == "paid" and int(row.get("amount", 0)) == 259]
    if len(new_invoice) != 1:
        raise FlowError(f"expected 1 invoice paid with 259 USD, got {len(new_invoice)}, history_after_pro_renewal:{history_after_pro_renewal}")
    print(f"  Assert: Pro renewal completed with paid invoice, {new_invoice[0]}")

    # =============================================================================
    # Step 7: Pro -> Starter downgrade at period end
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 7: Pro -> Starter downgrade at period end")
    print("=" * 80)
    history_before_starter = client.spend_history()
    created_gte = int(time.time()) - 10
    client.downgrade_pro_to_starter(
        subscription_id=pro_subscription_id,
        webhook_timeout_seconds=args.webhook_timeout_seconds,
    )

    client.advance_clock_to_plan_end()
    sync_count = client.sync_webhooks(
        subscription_ids={pro_subscription_id},
        created_gte=created_gte,
        wait_seconds=args.webhook_wait_seconds,
    )
    print(f"after sync_webhooks, sync_count:{sync_count}")

    client.wait_for_plan("Starter", args.webhook_timeout_seconds)
    starter_quota = get_starter_quota_apps()
    overview_starter = client.plan_overview()
    if overview_starter.get("resources", {}).get("apps", {}).get("limit", 0) != starter_quota:
        raise RuntimeError(f"after downgrade to Starter, expected Starter apps quota {starter_quota}, got {overview_starter}")
    history_after_starter = client.wait_for_history_count(
        len(history_before_starter) + 1,
        args.webhook_timeout_seconds,
        "Starter renewal after downgrade",
    )
    find_new_positive_paid_invoice(
        history_after_starter,
        {str(row.get("invoice_id") or "") for row in history_before_starter},
    )
    print("  Assert: Pro -> Starter downgrade completed")

    # =============================================================================
    # Step 8: Starter -> Trial downgrade at period end
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 8: Starter -> Trial downgrade at period end")
    print("=" * 80)

    client.downgrade_to_trial(
        subscription_id=pro_subscription_id,
        webhook_timeout_seconds=args.webhook_timeout_seconds,
    )
    history_before_trial = client.spend_history()
    client.advance_clock_to_plan_end()

    # client.ensure_invoice_finalized(pro_subscription_id)
    client.sync_webhooks(
        subscription_ids={pro_subscription_id},
        created_gte=int(time.time()) - 60,
        wait_seconds=args.webhook_wait_seconds,
    )
    trial_plan = client.wait_for_plan("Trial", args.webhook_timeout_seconds)
    if client.plan_overview().get("resources", {}).get("apps", {}).get("limit") != get_trial_quota_apps():
        raise RuntimeError(f"after downgrade to Trial, expected Trial apps quota {get_trial_quota_apps()}, got {client.plan_overview()}")
    history_after_trial = client.spend_history()
    new_trial_rows = history_after_trial[: max(0, len(history_after_trial) - len(history_before_trial))]
    paid_rows = [row for row in new_trial_rows if float(row.get("amount", 0) or 0) > 0]
    if paid_rows:
        raise RuntimeError(f"Trial period should not create paid renewal rows, got {paid_rows}")
    client.wait_for_no_pending_downgrade(args.webhook_timeout_seconds)
    print("  Assert: Starter -> Trial downgrade completed")

    # =============================================================================
    # Step 9: Final Trial -> Starter immediate upgrade
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 9: Final Trial -> Starter immediate upgrade")
    print("=" * 80)

    final_trial_subscription_id = str(trial_plan.get("subscription_id") or "")
    final_subscription_id, _, _ = client.complete_trial_checkout_upgrade(
        previous_subscription_id=final_trial_subscription_id,
        target_price_id=first_plan_price_id(load_billing_config(), "Starter"),
        target_plan_name="Starter",
        subscription_ids={pro_subscription_id},
        webhook_wait_seconds=args.webhook_wait_seconds,
        webhook_timeout_seconds=args.webhook_timeout_seconds,
    )
    overview_now = client.plan_overview()
    if overview_now.get("resources", {}).get("apps", {}).get("limit", 0) != get_starter_quota_apps():
        raise RuntimeError(f"after final upgrade to Starter, expected Starter apps quota {get_starter_quota_apps()}, got {overview_now}")
    print("  Assert: Final Trial -> Starter upgrade completed")

    # =============================================================================
    # PLAN-01 Test Summary
    # =============================================================================
    print("\n" + "=" * 80)
    print("PLAN-01 Test Summary")
    print("=" * 80)
    overview = client.plan_overview()
    history_final = client.spend_history()
    print(json.dumps({
        "case": "PLAN-01",
        "description": "Full subscription lifecycle: Trial→Pro→Starter→Trial→Starter",
        "tenant_id": client.tenant_id,
        "email": email,
        "test_clock_id": client.clock_id,
        "customer_id": client.customer_id,
        "final_plan": overview.get("plan_name"),
        "history_rows": len(history_final),
        "webhook_mode": args.webhook_mode,
    }, indent=2, sort_keys=True))


def main() -> int:
    parser = make_default_parser("Run billing PLAN-01: full subscription lifecycle test.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
