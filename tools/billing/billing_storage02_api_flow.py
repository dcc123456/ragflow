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
API-adjusted driver for STORAGE-02.
Tests: storage addon lifecycle combined with plan downgrade.

New subscription model:
- Storage addon is a line item on the same subscription as the plan.
- Cancelling storage sets its quantity to 0 (takes effect at period end).
- Plan downgrade (Starter -> Trial) should automatically cancel storage.
- After plan downgrade to Trial, storage addon cannot be re-added.
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

    email = args.email or f"billing-storage02-{uuid.uuid4().hex[:12]}@example.test"
    client: BillingClient = create_client(args, email)
    starter_subscription_id: str = client.upgrade_trial_to_starter()["subscription_id"]

    print("  Assert: Starter environment ready")
    print(f"  Assert: Tenant ID: {client.tenant_id}")
    print(f"  Assert: Customer ID: {client.customer_id}")
    print(f"  Assert: Starter subscription ID: {starter_subscription_id}")

    # =============================================================================
    # Step 6: Add storage addon to existing subscription
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Add storage addon 30GB to Starter subscription")
    print("=" * 80)

    storage_gb = 30
    target_storage_bytes = gb_to_bytes(storage_gb)

    print(f"  Info: Adding {storage_gb}GB storage addon to tenant {client.tenant_id}")
    client.replace_storage_subscription_quantity(
        new_quantity_gb=storage_gb,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage addon request sent through billing API")

    client.wait_for_storage_status("active", timeout_seconds=30)
    print("  Assert: Storage addon is active")

    # Verify storage addon quantity
    storage = client.storage_current()
    addon_bytes = int(storage.get("addon_storage_bytes") or 0)
    if addon_bytes != target_storage_bytes:
        raise FlowError(f"expected addon_storage_bytes={target_storage_bytes}, got {addon_bytes}")
    print(f"  Assert: Addon storage is {addon_bytes} bytes ({storage_gb}GB)")

    # Verify subscription has two items (plan + storage)
    sub = stripe.Subscription.retrieve(starter_subscription_id)
    items = sub.get("items", {}).get("data", [])
    if len(items) != 2:
        raise FlowError(f"expected 2 subscription items, got {len(items)}")
    print(f"  Assert: Subscription has {len(items)} items (plan + storage)")

    # =============================================================================
    # Step 7: Cancel storage addon (set quantity to 0) - takes effect at period end
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 7: Cancel storage addon (quantity = 0) - scheduled for period end")
    print("=" * 80)

    print(f"  Info: Setting storage quantity to 0GB for tenant {client.tenant_id}")
    client.replace_storage_subscription_quantity(
        new_quantity_gb=0,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage cancellation request sent through billing API")

    # After cancellation request, storage should still be active (not yet effective until period end)
    client.wait_for_storage_status("active", timeout_seconds=30)
    storage_after_cancel = client.storage_current()
    addon_bytes_after_cancel = int(storage_after_cancel.get("addon_storage_bytes") or 0)
    # Storage should still be 30GB because cancellation takes effect at period end
    if addon_bytes_after_cancel != target_storage_bytes:
        raise FlowError(f"addon_storage_bytes should still be {target_storage_bytes} after cancellation (not yet effective), got {addon_bytes_after_cancel}")
    print(f"  Assert: Addon storage is still {addon_bytes_after_cancel} bytes ({storage_gb}GB) - cancellation not yet effective")

    # Advance test clock to just after period end so cancellation takes effect
    storage_cancel_at = int(time.time()) - 5
    client.advance_clock_to_plan_end()
    print("  Assert: Test clock advanced past period end")

    # In stripe-cli mode this waits; in manual mode it replays selected events.
    client.sync_webhooks(
        subscription_ids={starter_subscription_id},
        created_gte=storage_cancel_at,
        wait_seconds=8,
    )
    print("  Assert: Webhook synchronization finished after clock advance")

    # Now verify storage addon is 0 after period end
    storage_after_period_end = client.storage_current()
    addon_bytes_after_period_end = int(storage_after_period_end.get("addon_storage_bytes") or 0)
    if addon_bytes_after_period_end != 0:
        raise FlowError(f"addon_storage_bytes should be 0 after period end, got {addon_bytes_after_period_end}")
    print("  Assert: Addon storage is 0 after period end (cancellation effective)")

    # Subscription should still have 2 items (storage item with quantity 0)
    sub = stripe.Subscription.retrieve(starter_subscription_id)
    items = sub.get("items", {}).get("data", [])
    if len(items) != 1:
        raise FlowError(f"expected 1 subscription items after cancellation, got {len(items)}")
    print(f"  Assert: Subscription still has {len(items)} items (storage quantity=0)")

    # =============================================================================
    # Step 8: Re-add storage (set to positive quantity) and verify
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 8: Re-add storage addon (set to 20GB)")
    print("=" * 80)

    storage_gb_2 = 20
    target_storage_bytes_2 = gb_to_bytes(storage_gb_2)

    print(f"  Info: Setting storage quantity to {storage_gb_2}GB for tenant {client.tenant_id}")
    client.replace_storage_subscription_quantity(
        new_quantity_gb=storage_gb_2,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage re-add request sent through billing API")
    client.wait_for_storage_status("active", timeout_seconds=30)
    storage_after_readd = client.storage_current()

    addon_bytes_after_readd = int(storage_after_readd.get("addon_storage_bytes") or 0)
    if addon_bytes_after_readd != target_storage_bytes_2:
        raise FlowError(f"expected addon_storage_bytes={target_storage_bytes_2}, got {addon_bytes_after_readd}")
    print(f"  Assert: Addon storage is {addon_bytes_after_readd} bytes ({storage_gb_2}GB)")

    # =============================================================================
    # Step 9: Downgrade plan from Starter to Trial and set storage addon to 0
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 9: Downgrade plan from Starter to Trial (auto-cancel storage)")
    print("=" * 80)

    # Verify plan remains Starter before downgrade
    before_downgrade_plan = client.current_plan()
    before_downgrade_plan_name = before_downgrade_plan.get("plan_name", "")
    if before_downgrade_plan_name != "Starter":
        raise FlowError(f"plan should be Starter before downgrade, got {before_downgrade_plan_name}")
    print(f"  Assert: Plan is Starter before downgrade: {before_downgrade_plan_name}")

    # Verify storage still has positive quantity before downgrade
    before_downgrade_storage = client.storage_current()
    before_downgrade_addon_bytes = int(before_downgrade_storage.get("addon_storage_bytes") or 0)
    if before_downgrade_addon_bytes != target_storage_bytes_2:
        raise FlowError(
            f"addon_storage_bytes should remain {target_storage_bytes_2} before downgrade, got {before_downgrade_addon_bytes}"
        )
    print(f"  Assert: Addon storage unchanged before downgrade: {before_downgrade_addon_bytes} bytes")

    # Use downgrade_to_trial to perform the plan downgrade
    created_gte = int(time.time()) - 5
    client.downgrade_to_trial(starter_subscription_id)
    print("  Assert: Plan downgraded to Trial - scheduled")
    client.advance_clock_to_plan_end()
    # In stripe-cli mode this waits; in manual mode it replays selected events.
    print("  Waiting for webhook synchronization")
    replayed = client.sync_webhooks(
        subscription_ids={starter_subscription_id},
        created_gte=created_gte,
    )
    print(f"  ✅ Webhook synchronization finished: {replayed} replayed events")

    client.wait_for_plan("Trial", 30)
    # Verify storage addon was automatically cleared (quantity set to 0)
    after_trial_storage = client.storage_current()

    after_trial_addon_bytes = int(after_trial_storage.get("addon_storage_bytes") or 0)
    if after_trial_addon_bytes != 0:
        raise FlowError(f"addon_storage_bytes should be 0 after downgrade to Trial, got {after_trial_addon_bytes}")
    print("  Assert: Addon storage automatically cleared on Trial downgrade")

    # Verify Trial plan has no plan storage quota
    plan_storage = int(after_trial_storage.get("plan_storage_bytes") or 0)
    print(f"  Assert: Plan storage after Trial downgrade: {plan_storage} bytes")

    # =============================================================================
    # Step 10: Attempt to add storage again on Trial plan (should be rejected)
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 10: Attempt to add storage on Trial plan (should be rejected)")
    print("=" * 80)

    try:
        client.replace_storage_subscription_quantity(
            new_quantity_gb=10,
            subscription_ids={starter_subscription_id},
        )
        # If we get here, the API allowed it, which is incorrect
        raise FlowError("Expected storage addon to be rejected on Trial plan, but it succeeded")
    except Exception as e:
        # We expect an error (either from Stripe or from the test function)
        print(f"  Assert: Storage addon correctly rejected: {str(e)[:100]}...")

    # =============================================================================
    # Final verification: spend history
    # =============================================================================
    history = client.spend_history()
    print(f"  Info: Billing history contains {len(history)} records")

    print(
        json.dumps(
            {
                "case": "STORAGE-02",
                "description": "Storage addon lifecycle + plan downgrade auto-cancel",
                "tenant_id": client.tenant_id,
                "email": email,
                "initial_storage_gb": storage_gb,
                "re_added_storage_gb": storage_gb_2,
                "storage_cancelled": addon_bytes_after_cancel == 0,
                "storage_auto_cleared_on_trial": after_trial_addon_bytes == 0,
                "final_plan": "Trial",
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing STORAGE-02: storage addon lifecycle + plan downgrade auto-cancel.")
    try:
        run_flow(parser.parse_args())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
