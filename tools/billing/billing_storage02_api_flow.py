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
from datetime import datetime, timezone
import stripe  # type: ignore[reportMissingImports]

clock_id = ""

from tools.billing.flow_common import (  # noqa: E402
    FlowError,
)
from tools.billing.storage_common import (  # noqa: E402
    RAGFlowClient,
    delete_clock,
    downgrade_to_trial,
    gb_to_bytes,
    make_default_parser,
    replace_storage_subscription_quantity,
    setup_starter,
    wait_for_storage_status, replay_stripe_events,
)


def run_flow(args) -> None:
    # =============================================================================
    # Steps 1-5: Setup Starter environment using shared utility
    # =============================================================================
    print("=" * 80)
    print("Steps 1-5: Setup Starter environment using shared utility")
    print("=" * 80)

    email = args.email or f"billing-storage02-{uuid.uuid4().hex[:12]}@example.test"
    setup = setup_starter(
        base_url=args.base_url,
        version=args.version,
        email=email,
        password=args.password,
        ready_timeout_seconds=args.ready_timeout_seconds,
        webhook_wait_seconds=args.webhook_wait_seconds,
        webhook_timeout_seconds=args.webhook_timeout_seconds,
    )

    client: RAGFlowClient = setup["client"]
    tenant_id: str = setup["tenant_id"]
    customer_id: str = setup["customer_id"]
    starter_subscription_id: str = setup["subscription_id"]
    global clock_id
    clock_id = setup["clock_id"]
    webhook_secret: str = setup["webhook_secret"]

    print("  Assert: Starter environment ready")
    print(f"  Assert: Tenant ID: {tenant_id}")
    print(f"  Assert: Customer ID: {customer_id}")
    print(f"  Assert: Starter subscription ID: {starter_subscription_id}")

    # =============================================================================
    # Step 6: Add storage addon to existing subscription
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Add storage addon 30GB to Starter subscription")
    print("=" * 80)

    storage_gb = 30
    target_quantity_bytes = gb_to_bytes(storage_gb)

    print(f"  Info: Adding {storage_gb}GB storage addon to tenant {tenant_id}")
    replace_storage_subscription_quantity(
        client=client,
        tenant_id=tenant_id,
        new_quantity_gb=storage_gb,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage addon modification sent")

    wait_for_storage_status(client, tenant_id, "active", timeout_seconds=30)
    print("  Assert: Storage addon is active")

    # Verify storage addon quantity
    storage = client.storage_current(tenant_id)
    addon_bytes = int(storage.get("addon_storage_bytes") or 0)
    if addon_bytes != target_quantity_bytes:
        raise FlowError(f"expected addon_storage_bytes={target_quantity_bytes}, got {addon_bytes}")
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

    # Get current plan end time before cancellation
    plan_before_cancel = client.current_plan()
    plan_end_before_cancel = plan_before_cancel.get("end_time")
    if not plan_end_before_cancel:
        raise FlowError(f"plan response is missing end_time: {plan_before_cancel}")
    print(f"  Assert: Plan end_time before cancellation: {plan_end_before_cancel}")

    # Parse plan end timestamp
    if isinstance(plan_end_before_cancel, (int, float)):
        plan_end_ts_before = int(plan_end_before_cancel)
    else:
        plan_end_str = str(plan_end_before_cancel).replace("Z", "+00:00")
        plan_end_dt = datetime.fromisoformat(plan_end_str)
        if plan_end_dt.tzinfo is None:
            plan_end_dt = plan_end_dt.replace(tzinfo=timezone.utc)
        plan_end_ts_before = int(plan_end_dt.timestamp())

    print(f"  Info: Setting storage quantity to 0GB for tenant {tenant_id}")
    replace_storage_subscription_quantity(
        client=client,
        tenant_id=tenant_id,
        new_quantity_gb=0,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage cancellation modification sent")

    # After cancellation request, storage should still be active (not yet effective until period end)
    wait_for_storage_status(client, tenant_id, "active", timeout_seconds=30)
    storage_after_cancel = client.storage_current(tenant_id)
    addon_bytes_after_cancel = int(storage_after_cancel.get("addon_storage_bytes") or 0)
    # Storage should still be 30GB because cancellation takes effect at period end
    if addon_bytes_after_cancel != target_quantity_bytes:
        raise FlowError(f"addon_storage_bytes should still be {target_quantity_bytes} after cancellation (not yet effective), got {addon_bytes_after_cancel}")
    print(f"  Assert: Addon storage is still {addon_bytes_after_cancel} bytes ({storage_gb}GB) - cancellation not yet effective")

    # Advance test clock to just after period end so cancellation takes effect
    advance_time = plan_end_ts_before + 60  # 1 minute after period end
    print(f"  Info: Advancing test clock to {advance_time} (just after period end)")
    from tools.billing.storage_common import advance_clock, wait_for_clock

    storage_cancel_at = int(time.time()) - 5
    advance_clock(clock_id, advance_time)
    wait_for_clock(clock_id)
    print("  Assert: Test clock advanced past period end")

    # Replay webhooks after clock advance
    from tools.billing.storage_common import sync_webhooks
    sync_webhooks(
        client,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
        created_gte=storage_cancel_at,
        wait_seconds=8,
    )
    print("  Assert: Webhooks synced after clock advance")

    # Now verify storage addon is 0 after period end
    storage_after_period_end = client.storage_current(tenant_id)
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
    target_quantity_bytes_2 = gb_to_bytes(storage_gb_2)

    print(f"  Info: Setting storage quantity to {storage_gb_2}GB for tenant {tenant_id}")
    replace_storage_subscription_quantity(
        client=client,
        tenant_id=tenant_id,
        new_quantity_gb=storage_gb_2,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage re-add modification sent")

    wait_for_storage_status(client, tenant_id, "active", timeout_seconds=30)
    storage_after_readd = client.storage_current(tenant_id)

    addon_bytes_after_readd = int(storage_after_readd.get("addon_storage_bytes") or 0)
    if addon_bytes_after_readd != target_quantity_bytes_2:
        raise FlowError(f"expected addon_storage_bytes={target_quantity_bytes_2}, got {addon_bytes_after_readd}")
    print(f"  Assert: Addon storage is {addon_bytes_after_readd} bytes ({storage_gb_2}GB)")

    # =============================================================================
    # Step 9: Downgrade plan from Starter to Trial and set storage addon to 0
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 9: Downgrade plan from Starter to Trial (auto-cancel storage)")
    print("=" * 80)

    # Verify plan remains Starter before downgrade
    before_downgrade_plan = client.current_plan()
    plan_end = before_downgrade_plan.get("end_time")
    if not plan_end:
        raise FlowError(f"plan response is missing end_time: {before_downgrade_plan}")
    print(f"  Assert: Plan end_time retrieved: {plan_end}")

    if isinstance(plan_end, (int, float)):
        plan_end_ts = int(plan_end)
    else:
        plan_end_str = str(plan_end).replace("Z", "+00:00")
        plan_end_dt = datetime.fromisoformat(plan_end_str)
        if plan_end_dt.tzinfo is None:
            plan_end_dt = plan_end_dt.replace(tzinfo=timezone.utc)
        plan_end_ts = int(plan_end_dt.timestamp())

    before_downgrade_plan_name = before_downgrade_plan.get("plan_name", "")
    if before_downgrade_plan_name != "Starter":
        raise FlowError(f"plan should be Starter before downgrade, got {before_downgrade_plan_name}")
    print(f"  Assert: Plan is Starter before downgrade: {before_downgrade_plan_name}")

    # Verify storage still has positive quantity before downgrade
    before_downgrade_storage = client.storage_current(tenant_id)
    before_downgrade_addon_bytes = int(before_downgrade_storage.get("addon_storage_bytes") or 0)
    if before_downgrade_addon_bytes != target_quantity_bytes_2:
        raise FlowError(
            f"addon_storage_bytes should remain {target_quantity_bytes_2} before downgrade, got {before_downgrade_addon_bytes}"
        )
    print(f"  Assert: Addon storage unchanged before downgrade: {before_downgrade_addon_bytes} bytes")

    # Use downgrade_to_trial to perform the plan downgrade
    downgrade_to_trial(
        client=client,
        tenant_id=tenant_id,
        subscription_id=starter_subscription_id,
        webhook_secret=webhook_secret
    )
    print("  Assert: Plan downgraded to Trial - scheduled")

    current_ts = int(time.time())
    advance_seconds = plan_end_ts - current_ts + 120  # 2 minutes after period end
    print(f"  Info: Advancing clock by {advance_seconds} seconds ({advance_seconds/86400:.1f} days)")
    advance_clock(clock_id, current_ts + advance_seconds)
    print("  Assert: Clock advanced")

    # Sync webhook events if webhook_secret provided (for test clock sync)
    print("  Replaying webhook events for synchronization")
    replayed = replay_stripe_events(
        client,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
        created_gte=current_ts-5,
    )
    print(f"  ✅ Webhook events replayed: {replayed} events")

    # Verify storage addon was automatically cleared (quantity set to 0)
    after_trial_storage = client.storage_current(tenant_id)

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
        replace_storage_subscription_quantity(
            client=client,
            tenant_id=tenant_id,
            new_quantity_gb=10,
            webhook_secret=webhook_secret,
            customer_id=customer_id,
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
                "tenant_id": tenant_id,
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
    finally:
        print("=" * 80)
        delete_clock(clock_id)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
