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
API-adjusted driver for STORAGE-05.
Tests: plan upgrade/downgrade with existing storage addon.
"""

from __future__ import annotations

import json
import sys
import time
import uuid

import stripe  # type: ignore[reportMissingImports]

from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    make_default_parser,
)
from tools.billing.billing_client import BillingClient, create_client
from tools.billing.storage_common import (  # noqa: E402
    gb_to_bytes,
)


def run_flow(args) -> None:
    # =============================================================================
    # Steps 1-5: Setup Starter environment using shared utility
    # =============================================================================
    print("=" * 80)
    print("Steps 1-5: Setup Starter environment using shared utility")
    print("=" * 80)

    email = args.email or f"billing-storage05-{uuid.uuid4().hex[:12]}@example.test"
    client: BillingClient = create_client(args, email)
    starter_subscription_id: str = client.upgrade_trial_to_starter()["subscription_id"]

    print("  Assert: Starter environment ready")
    print(f"  Assert: Tenant ID: {client.tenant_id}")
    print(f"  Assert: Customer ID: {client.customer_id}")
    print(f"  Assert: Starter subscription ID: {starter_subscription_id}")

    subscription_ids: set[str] = {starter_subscription_id}

    # =============================================================================
    # Step 6: Purchase storage addon (30GB) on Starter plan via direct modification
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Purchase storage addon (30GB) on Starter plan")
    print("=" * 80)

    initial_storage = client.storage_current()
    initial_addon_bytes = int(initial_storage.get("addon_storage_bytes") or 0)
    print(f"  Assert: Initial addon storage: {initial_addon_bytes} bytes")

    storage_gb = 30
    target_storage_bytes = gb_to_bytes(storage_gb)
    print(f"  Info: Adding {storage_gb}GB storage addon to subscription {starter_subscription_id}")

    client.replace_storage_subscription_quantity(
        new_quantity_gb=storage_gb,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage addon modification sent")

    client.wait_for_storage_status("active", timeout_seconds=30)
    print("  Assert: Storage subscription is active")

    after_addon_storage = client.storage_current()
    after_addon_addon_bytes = int(after_addon_storage.get("addon_storage_bytes") or 0)
    if after_addon_addon_bytes != target_storage_bytes:
        raise FlowError(f"addon_storage_bytes should be {target_storage_bytes} after purchase, got {after_addon_addon_bytes}")
    print(f"  Assert: Addon storage equals target: {after_addon_addon_bytes} bytes")

    # Verify subscription has two items (plan + storage)
    sub = stripe.Subscription.retrieve(starter_subscription_id)
    items = sub.get("items", {}).get("data", [])
    if len(items) != 2:
        raise FlowError(f"expected 2 subscription items, got {len(items)}")
    print(f"  Assert: Subscription has {len(items)} items (plan + storage)")

    # =============================================================================
    # Step 7: Upgrade plan from Starter to Pro (using shared utility)
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 7: Upgrade plan from Starter to Pro (using upgrade_starter_to_pro)")
    print("=" * 80)

    upgrade_result = client.upgrade_starter_to_pro(
        starter_subscription_id,
        webhook_wait_seconds=args.webhook_wait_seconds,
        webhook_timeout_seconds=args.webhook_timeout_seconds,
    )
    subscription_id = upgrade_result.get("subscription_id", "")
    subscription_ids.add(subscription_id)
    print(f"  Assert: Upgraded subscription ID: {subscription_id}")

    after_upgrade_plan = upgrade_result.get("current_plan", {})
    after_upgrade_plan_name = after_upgrade_plan.get("plan_name", "")
    after_upgrade_plan_overview = client.plan_overview()
    after_upgrade_storage = client.storage_current()
    after_upgrade_addon_bytes = int(after_upgrade_storage.get("addon_storage_bytes") or 0)

    if after_upgrade_plan_name != "Pro":
        raise FlowError(f"plan should be Pro after upgrade, got {after_upgrade_plan_name}")
    print(f"  Assert: Plan upgraded to Pro: {after_upgrade_plan_name}")

    plan_storage_after_upgrade = after_upgrade_plan_overview.get("resources", {}).get("plan_storage", {})
    plan_storage_limit_after_upgrade = int(plan_storage_after_upgrade.get("limit") or 0)
    print(f"  Assert: Plan storage limit after upgrade: {plan_storage_limit_after_upgrade} bytes")

    if after_upgrade_addon_bytes != target_storage_bytes:
        raise FlowError(f"addon_storage_bytes should remain {target_storage_bytes} after plan upgrade, got {after_upgrade_addon_bytes}")
    print(f"  Assert: Addon storage unchanged after upgrade: {after_upgrade_addon_bytes} bytes")

    # Step 8: Downgrade plan Pro -> Starter (using shared utility)
    print("\n" + "=" * 80)
    print("Step 8: Downgrade plan from Pro to Starter (using downgrade_pro_to_starter)")
    print("=" * 80)

    if not subscription_id:
        raise FlowError("no active subscription found for plan downgrade")
    print(f"  Assert: Current subscription ID: {subscription_id}")

    # Use shared downgrade_pro_to_starter() method (schedules downgrade, waits for pending)
    downgrade_created_gte = int(time.time()) - 5
    downgrade_result = client.downgrade_pro_to_starter(subscription_id)
    print(f"  Assert: Pro -> Starter downgrade scheduled, schedule_id: {downgrade_result.get('schedule_id')}")

    # Advance clock to plan end using shared utility to apply the downgrade
    client.advance_clock_to_plan_end()
    print("  Assert: Clock advanced to plan end for downgrade to take effect")

    # Sync webhooks after clock advance
    client.sync_webhooks(
        subscription_ids=subscription_ids,
        created_gte=downgrade_created_gte,
        wait_seconds=8,
    )
    print("  Assert: Webhooks synced after clock advance")

    # Wait for plan to become Starter
    after_downgrade_plan = client.wait_for_plan("Starter", args.webhook_timeout_seconds)
    after_downgrade_plan_name = after_downgrade_plan.get("plan_name", "")
    after_downgrade_plan_overview = client.plan_overview()
    after_downgrade_storage = client.storage_current()
    after_downgrade_addon_bytes = int(after_downgrade_storage.get("addon_storage_bytes") or 0)

    if after_downgrade_plan_name != "Starter":
        raise FlowError(f"plan should be Starter after downgrade, got {after_downgrade_plan_name}")
    print(f"  Assert: Plan downgraded to Starter: {after_downgrade_plan_name}")

    plan_storage_after_downgrade = after_downgrade_plan_overview.get("resources", {}).get("plan_storage", {})
    plan_storage_limit_after_downgrade = int(plan_storage_after_downgrade.get("limit") or 0)
    print(f"  Assert: Plan storage limit after downgrade: {plan_storage_limit_after_downgrade} bytes")

    if after_downgrade_addon_bytes != target_storage_bytes:
        raise FlowError(f"addon_storage_bytes should remain {target_storage_bytes} after plan downgrade, got {after_downgrade_addon_bytes}")
    print(f"  Assert: Addon storage unchanged after downgrade: {after_downgrade_addon_bytes} bytes")

    # =============================================================================
    # Step 9: Verify quota after downgrade takes effect
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 9: Verify quota after downgrade takes effect")
    print("=" * 80)
    after_downgrade_effective_storage = client.storage_current()
    after_downgrade_effective_addon_bytes = int(after_downgrade_effective_storage.get("addon_storage_bytes") or 0)

    if after_downgrade_effective_addon_bytes != target_storage_bytes:
        raise FlowError(f"addon_storage_bytes should remain {target_storage_bytes} after downgrade takes effect, got {after_downgrade_effective_addon_bytes}")
    print(f"  Assert: Addon storage unchanged after downgrade takes effect: {after_downgrade_effective_addon_bytes} bytes")


    final_plan_overview = client.plan_overview()
    final_resources = final_plan_overview.get("resources", {})
    final_plan_storage = final_resources.get("plan_storage", {})
    final_plan_storage_limit = int(final_plan_storage.get("limit") or 0)
    final_addon_storage = final_resources.get("addon_storage", {})
    final_addon_storage_limit = int(final_addon_storage.get("limit") or 0)

    print(f"  Assert: Final plan storage limit: {final_plan_storage_limit} bytes")
    print(f"  Assert: Final addon storage limit: {final_addon_storage_limit} bytes")

    total_storage_after_downgrade = final_plan_storage_limit + final_addon_storage_limit
    expected_total = plan_storage_limit_after_downgrade + target_storage_bytes
    if total_storage_after_downgrade != expected_total:
        raise FlowError(f"total storage should be {expected_total} bytes (5GB plan + 30GB addon), got {total_storage_after_downgrade} bytes")
    print(f"  Assert: Total storage quota: {total_storage_after_downgrade} bytes (5GB plan + 30GB addon)")

    # =============================================================================
    # Step 10: Downgrade plan from Starter to Trial (addon should be invalidated)
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 10: Downgrade plan from Starter to Trial (addon invalidation)")
    print("=" * 80)

    history_before_trial_downgrade = client.spend_history()
    print(f"  Assert: Billing history rows before Trial downgrade: {len(history_before_trial_downgrade)}")

    # Use shared downgrade_to_trial() method (handles scheduling + verification)
    created_gte = int(time.time()) - 5
    downgrade_result = client.downgrade_to_trial(
        starter_subscription_id,
        webhook_timeout_seconds=args.webhook_timeout_seconds,
    )
    print(f"  Assert: Downgrade to Trial scheduled successfully, schedule_id: {downgrade_result.get('schedule_id')}")

    # Verify addon storage unchanged after scheduling downgrade
    before_period_end_storage = client.storage_current()
    before_period_end_addon_bytes = int(before_period_end_storage.get("addon_storage_bytes") or 0)
    if before_period_end_addon_bytes != target_storage_bytes:
        raise FlowError(f"addon_storage_bytes should remain {target_storage_bytes} before period end, got {before_period_end_addon_bytes}")
    print(f"  Assert: Addon storage unchanged before period end: {before_period_end_addon_bytes} bytes")

    # Advance clock to after plan end using shared utility
    client.advance_clock_to_plan_end()
    client.sync_webhooks(
        subscription_ids=subscription_ids,
        created_gte=created_gte,
        wait_seconds=15,
    )
    print("  Assert: Webhooks synced after period end")

    after_trial_plan = client.wait_for_plan("Trial", args.webhook_timeout_seconds)
    after_trial_plan_name = after_trial_plan.get("plan_name", "")
    if after_trial_plan_name != "Trial":
        raise FlowError(f"plan should be Trial after period end, got {after_trial_plan_name}")
    print(f"  Assert: Plan changed to Trial after period end: {after_trial_plan_name}")

    after_trial_storage = client.storage_current()
    after_trial_addon_bytes = int(after_trial_storage.get("addon_storage_bytes") or 0)
    after_trial_plan_storage = int(after_trial_storage.get("plan_storage_bytes") or 0)

    trial_storage_gb = 0
    expected_trial_plan_storage =  gb_to_bytes(trial_storage_gb)
    print(f"  Assert: Plan storage after Trial downgrade: {after_trial_plan_storage} bytes (expected: {expected_trial_plan_storage})")

    if after_trial_addon_bytes != 0:
        raise FlowError(f"addon_storage_bytes should be 0 after Trial downgrade (no base plan), got {after_trial_addon_bytes}")
    print(f"  Assert: Addon storage invalidated after Trial downgrade: {after_trial_addon_bytes} bytes")

    history_after_trial_downgrade = client.spend_history()
    new_rows = len(history_after_trial_downgrade) - len(history_before_trial_downgrade)
    if new_rows > 0:
        new_paid_rows = [
            row for row in history_after_trial_downgrade
            if float(row.get("amount", 0) or 0) > 0 and row not in history_before_trial_downgrade
        ]
        if new_paid_rows:
            raise FlowError(f"Trial period should not create paid charges, got: {new_paid_rows}")
    print("  Assert: No charges made during Trial period")

    final_overview = client.plan_overview()
    final_resources = final_overview.get("resources", {})
    final_plan_storage_limit = int(final_resources.get("plan_storage", {}).get("limit") or 0)
    final_addon_storage_limit = int(final_resources.get("addon_storage", {}).get("limit") or 0)

    print(f"  Assert: Final plan storage limit: {final_plan_storage_limit} bytes")
    print(f"  Assert: Final addon storage limit: {final_addon_storage_limit} bytes")

    total_storage_trial = final_plan_storage_limit + final_addon_storage_limit
    print(f"  Assert: Total storage quota after Trial downgrade: {total_storage_trial} bytes")

    print(
        json.dumps(
            {
                "case": "STORAGE-05",
                "description": "Plan change with existing addon (PLAN-05 mode)",
                "tenant_id": client.tenant_id,
                "email": email,
                "initial_plan": "Starter",
                "storage_gb": storage_gb,
                "addon_bytes_after_purchase": after_addon_addon_bytes,
                "plan_after_upgrade": after_upgrade_plan_name,
                "plan_subscription_id": subscription_id,
                "addon_bytes_after_upgrade": after_upgrade_addon_bytes,
                "plan_after_downgrade": after_downgrade_plan_name,
                "addon_bytes_after_downgrade": after_downgrade_addon_bytes,
                "addon_bytes_after_downgrade_effective": after_downgrade_effective_addon_bytes,
                "plan_storage_after_downgrade_effective": final_plan_storage_limit,
                "total_storage_quota": total_storage_after_downgrade,
                "plan_after_trial_downgrade": after_trial_plan_name,
                "addon_bytes_after_trial_downgrade": after_trial_addon_bytes,
                "plan_storage_after_trial_downgrade": after_trial_plan_storage,
                "total_storage_after_trial_downgrade": total_storage_trial,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing STORAGE-05: plan change with existing addon.")
    try:
        run_flow(parser.parse_args())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
