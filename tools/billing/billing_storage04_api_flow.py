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
- Unlike immediate paid changes, this case does not require `/billing/setup-intent`.
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

    email = args.email or f"billing-storage04-{uuid.uuid4().hex[:12]}@example.test"
    client: BillingClient = create_client(args, email)
    starter_subscription_id: str = client.upgrade_trial_to_starter()["subscription_id"]

    print("  Assert: Starter environment ready")
    print(f"  Assert: Tenant ID: {client.tenant_id}")
    print(f"  Assert: Customer ID: {client.customer_id}")
    print(f"  Assert: Starter subscription ID: {starter_subscription_id}")

    # =============================================================================
    # Step 6: Purchase initial storage addon (20GB) via billing storage target flow
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Purchase initial storage addon (20GB) on Starter plan")
    print("=" * 80)

    initial_storage_gb = 20
    initial_target_bytes = gb_to_bytes(initial_storage_gb)

    print(f"  Info: Adding {initial_storage_gb}GB storage addon to subscription {starter_subscription_id}")
    client.replace_storage_subscription_quantity(
        new_quantity_gb=initial_storage_gb,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage addon request sent through billing API")

    client.wait_for_storage_status("active", timeout_seconds=30)
    print("  Assert: Storage addon is active")

    storage = client.storage_current()
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
    downgrade_result = client.storage_set_target(downgrade_target_bytes)
    scheduled_change = downgrade_result.get("scheduled_change")
    if not scheduled_change:
        raise FlowError(f"scheduled_change should be set after downgrade request, got: {downgrade_result}")
    print("  Assert: Scheduled change is set")

    # Verify current quota remains 20GB immediately after scheduling
    created_gte = int(time.time()) - 5
    if downgrade_result.get("addon_storage_bytes", 0) != initial_target_bytes:
        raise FlowError(
            f"addon_storage_bytes should remain {initial_target_bytes} immediately after downgrade, got {downgrade_result.get('addon_storage_bytes')}"
        )
    print("  Assert: Addon quota unchanged immediately after downgrade request")

    # Also re-fetch from API to ensure backend reflects the same
    storage_after_schedule = client.storage_current()
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

    client.advance_clock_to_plan_end()

    # In stripe-cli mode this only waits; in manual mode it replays selected events.
    print("  Waiting for webhook synchronization after period end")
    replayed = client.sync_webhooks(
        subscription_ids={starter_subscription_id},
        created_gte=created_gte,
    )
    print(f"  ✅ Webhook synchronization finished: {replayed} replayed events")

    storage_after_period = client.storage_current()
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
                "tenant_id": client.tenant_id,
                "email": email,
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
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
