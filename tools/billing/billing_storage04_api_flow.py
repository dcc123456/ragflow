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
API-adjusted driver for STORAGE-04.
Tests: downgrading storage addon (decrease quantity) takes effect at period end.

New subscription model:
- Storage addon shares the same subscription with the plan (cannot be independent).
- During downgrade, the quantity decrease is scheduled and takes effect at the next period end.
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
    advance_clock,
    delete_clock,
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

    email = args.email or f"billing-storage04-{uuid.uuid4().hex[:12]}@example.test"
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

    # Get plan end time for period-end testing
    plan = client.current_plan()
    plan_end = plan.get("end_time")
    if not plan_end:
        raise FlowError(f"plan response is missing end_time: {plan}")
    print(f"  Assert: Plan end_time retrieved: {plan_end}")

    if isinstance(plan_end, (int, float)):
        plan_end_ts = int(plan_end)
    else:
        plan_end_str = str(plan_end).replace("Z", "+00:00")
        plan_end_dt = datetime.fromisoformat(plan_end_str)
        if plan_end_dt.tzinfo is None:
            plan_end_dt = plan_end_dt.replace(tzinfo=timezone.utc)
        plan_end_ts = int(plan_end_dt.timestamp())

    # =============================================================================
    # Step 6: Purchase initial storage addon (20GB) via direct subscription modification
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Purchase initial storage addon (20GB) on Starter plan")
    print("=" * 80)

    initial_storage_gb = 20
    initial_target_bytes = gb_to_bytes(initial_storage_gb)

    print(f"  Info: Adding {initial_storage_gb}GB storage addon to subscription {starter_subscription_id}")
    replace_storage_subscription_quantity(
        client=client,
        tenant_id=tenant_id,
        new_quantity_gb=initial_storage_gb,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage addon modification sent")

    wait_for_storage_status(client, tenant_id, "active", timeout_seconds=30)
    print("  Assert: Storage addon is active")

    storage = client.storage_current(tenant_id)
    after_addon_bytes = int(storage.get("addon_storage_bytes") or 0)
    if after_addon_bytes != initial_target_bytes:
        raise FlowError(f"expected addon_storage_bytes={initial_target_bytes}, got {after_addon_bytes}")
    print(f"  Assert: Addon storage is {after_addon_bytes} bytes ({initial_storage_gb}GB)")

    # Verify subscription has two items (plan + storage)
    sub = stripe.Subscription.retrieve(starter_subscription_id)
    items = sub.get("items", {}).get("data", [])
    if len(items) != 2:
        raise FlowError(f"expected 2 subscription items, got {len(items)}")
    print(f"  Assert: Subscription has {len(items)} items (plan + storage)")

    # =============================================================================
    # Step 7: Schedule storage addon downgrade (20GB -> 10GB)
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 7: Schedule storage addon downgrade (20GB -> 10GB)")
    print("=" * 80)

    downgrade_storage_gb = 10
    downgrade_target_bytes = gb_to_bytes(downgrade_storage_gb)
    print(f"  Info: Scheduling downgrade to {downgrade_storage_gb}GB")

    # Schedule the downgrade via the storage set-target endpoint
    downgrade_result = client.storage_set_target(tenant_id, downgrade_target_bytes)
    scheduled_change = downgrade_result.get("scheduled_change")
    if not scheduled_change:
        raise FlowError(f"scheduled_change should be set after downgrade request, got: {downgrade_result}")
    print("  Assert: Scheduled change is set")

    # Verify current quota remains 20GB immediately after scheduling
    if downgrade_result.get("addon_storage_bytes", 0) != initial_target_bytes:
        raise FlowError(
            f"addon_storage_bytes should remain {initial_target_bytes} immediately after downgrade, got {downgrade_result.get('addon_storage_bytes')}"
        )
    print("  Assert: Addon quota unchanged immediately after downgrade request")

    # Also re-fetch from API to ensure backend reflects the same
    storage_after_schedule = client.storage_current(tenant_id)
    after_downgrade_addon_bytes = int(storage_after_schedule.get("addon_storage_bytes") or 0)
    if after_downgrade_addon_bytes != initial_target_bytes:
        raise FlowError(f"API shows addon_storage_bytes changed prematurely to {after_downgrade_addon_bytes}")
    print("  Assert: Addon quota unchanged in API response")

    # =============================================================================
    # Step 8: Advance clock past period end and verify storage quota decreases
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 8: Advance clock past period end and verify storage quota decreases")
    print("=" * 80)

    current_ts = int(time.time())
    advance_seconds = plan_end_ts - current_ts + 120  # 2 minutes after period end
    print(f"  Info: Advancing clock by {advance_seconds} seconds to after period end")
    advance_clock(clock_id, current_ts + advance_seconds)
    print("  Assert: Clock advanced past period end")


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

    storage_after_period = client.storage_current(tenant_id)
    after_period_addon_bytes = int(storage_after_period.get("addon_storage_bytes") or 0)
    if after_period_addon_bytes != downgrade_target_bytes:
        raise FlowError(
            f"addon_storage_bytes should be {downgrade_target_bytes} after period end, got {after_period_addon_bytes}"
        )
    print(f"  Assert: Addon quota decreased to {after_period_addon_bytes} bytes ({downgrade_storage_gb}GB)")

    print(
        json.dumps(
            {
                "case": "STORAGE-04",
                "description": "Storage addon downgrade (at period end, single subscription model)",
                "tenant_id": tenant_id,
                "email": email,
                "plan_end": plan_end,
                "subscription_id": starter_subscription_id,
                "initial_storage_gb": initial_storage_gb,
                "downgrade_storage_gb": downgrade_storage_gb,
                "addon_bytes_after_initial": after_addon_bytes,
                "addon_bytes_immediately_after_downgrade": after_downgrade_addon_bytes,
                "addon_bytes_after_period_end": after_period_addon_bytes,
                "scheduled_change": scheduled_change,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing STORAGE-04: storage addon downgrade (at period end).")
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
