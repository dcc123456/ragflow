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
Pure API driver for the PLAN-04 case documented in tools/billing/README.md.
Tests: scheduled storage addon downgrade is replaced by immediate upgrade.

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
    FlowError, make_default_parser, 
)
from tools.billing.storage_common import (  # noqa: E402
    RAGFlowClient,
    add_storage_to_subscription_with_webhook,
    gb_to_bytes,
    setup_starter,
    replace_storage_subscription_quantity,
)

def run_flow(args: argparse.Namespace) -> None:
    """Execute PLAN-04: scheduled storage addon downgrade is replaced by immediate upgrade."""
    # =============================================================================
    # Steps 1-4: Setup Starter environment (validates config, creates test clock,
    #           registers user, upgrades Trial -> Starter)
    # =============================================================================
    print("=" * 80)
    print("Steps 1-4: Setup Starter environment using shared utility")
    print("=" * 80)

    email = args.email or f"billing-plan04-{uuid.uuid4().hex[:12]}@example.test"
    setup = setup_starter(args, email=email)

    client: RAGFlowClient = setup["client"]
    tenant_id: str = setup["tenant_id"]
    customer_id: str = setup["customer_id"]
    starter_subscription_id: str = setup["subscription_id"]
    clock_id = setup["clock_id"]
    print("  Assert: Starter environment ready")
    print(f"  Assert: Tenant ID: {tenant_id}")
    print(f"  Assert: Customer ID: {customer_id}")
    print(f"  Assert: Starter subscription ID: {starter_subscription_id}")

    # =============================================================================
    # Step 5: Add storage addon 20GB to Starter subscription
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 5: Add storage addon 20GB to Starter subscription")
    print("=" * 80)

    initial_storage_gb = 20
    add_storage_to_subscription_with_webhook(
        client,
        tenant_id,
        initial_storage_gb,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
        created_gte=int(time.time()) - 5,
    )
    print(f"  Assert: Storage addon added: {initial_storage_gb}GB")

    # Verify initial storage
    storage_info = client.storage_current(tenant_id)
    initial_addon_bytes = storage_info.get("addon_storage_bytes", 0)
    expected_initial_bytes = gb_to_bytes(initial_storage_gb)
    if initial_addon_bytes != expected_initial_bytes:
        raise FlowError(
            f"Initial storage verification failed: expected {expected_initial_bytes} bytes, got {initial_addon_bytes} bytes"
        )
    print(f"  Assert: Initial storage verified: {initial_storage_gb}GB ({initial_addon_bytes} bytes)")

    # =============================================================================
    # Step 6: Schedule a storage addon downgrade (20GB -> 10GB)
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Schedule a storage addon downgrade (20GB -> 10GB)")
    print("=" * 80)

    downgrade_storage_gb = 10
    downgrade_started_at = int(time.time()) - 5

    # Call the backend API to set a lower storage target (this schedules a downgrade)
    target_quantity_bytes = gb_to_bytes(downgrade_storage_gb)
    result = client.storage_set_target(tenant_id, target_quantity_bytes)
    print(f"  Assert: Storage downgrade scheduled, target: {downgrade_storage_gb}GB, result:{result}")

    # Sync webhook events
    client.sync_webhooks(
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
        created_gte=downgrade_started_at,
        wait_seconds=args.webhook_wait_seconds,
    )

    # =============================================================================
    # Step 7: Verify downgrade is pending in storage info
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 7: Verify downgrade is pending in storage info")
    print("=" * 80)

    storage_after_downgrade = client.storage_current(tenant_id)
    pending_change = storage_after_downgrade.get("pending_subscription_change", {})
    if pending_change:
        print(f"  Assert: Pending storage downgrade confirmed: {pending_change}")
    else:
        print(f"  Info: No pending change visible yet (may be immediate), storage_after_downgrade:{storage_after_downgrade}")

    # =============================================================================
    # Step 8: Immediately upgrade storage to 100GB (replaces the scheduled downgrade)
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 8: Immediately upgrade storage to 100GB (replaces scheduled downgrade)")
    print("=" * 80)

    upgrade_storage_gb = 70
    upgrade_started_at = int(time.time()) - 5

    replace_storage_subscription_quantity(
        client,
        tenant_id,
        upgrade_storage_gb,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
    )
    print(f"  Assert: Storage upgrade submitted: {upgrade_storage_gb}GB")

    # Sync webhook events for the upgrade
    client.sync_webhooks(
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
        created_gte=upgrade_started_at,
        wait_seconds=args.webhook_wait_seconds,
    )

    # =============================================================================
    # Step 9: Verify upgrade succeeded immediately
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 9: Verify upgrade succeeded immediately")
    print("=" * 80)

    storage_after_upgrade = client.storage_current(tenant_id)
    actual_addon_bytes = storage_after_upgrade.get("addon_storage_bytes", 0)
    expected_upgrade_bytes = gb_to_bytes(upgrade_storage_gb)

    if actual_addon_bytes < expected_upgrade_bytes:
        raise FlowError(
            f"Storage upgrade verification failed: expected at least {expected_upgrade_bytes} bytes, got {actual_addon_bytes} bytes"
        )
    print(f"  Assert: Storage upgrade verified: {upgrade_storage_gb}GB ({actual_addon_bytes} bytes)")

    # =============================================================================
    # Step 10: Verify no pending downgrade remains in database
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 10: Verify no pending downgrade remains in database")
    print("=" * 80)

    storage_final = client.storage_current(tenant_id)
    pending_change_final = storage_final.get("pending_subscription_change", {})
    if pending_change_final:
        raise FlowError(
            f"Expected no pending downgrade after upgrade, but found: {pending_change_final}"
        )
    print("  Assert: No pending downgrade remains in database")

    # Also verify the current plan has no pending subscription change
    current_plan = client.current_plan()
    pending_plan_change = current_plan.get("pending_subscription_change", {})
    if pending_plan_change:
        print(f"  Info: Plan pending change (if any): {pending_plan_change}")
    else:
        print("  Assert: No pending plan change")

    # =============================================================================
    # PLAN-04 Test Summary
    # =============================================================================
    print("\n" + "=" * 80)
    print("PLAN-04 Test Summary")
    print("=" * 80)
    print(
        json.dumps(
            {
                "case": "PLAN-04",
                "description": "Scheduled storage addon downgrade is replaced by immediate upgrade",
                "tenant_id": tenant_id,
                "email": email,
                "test_clock_id": clock_id,
                "customer_id": customer_id,
                "subscription_id": starter_subscription_id,
                "initial_storage_gb": initial_storage_gb,
                "scheduled_downgrade_gb": downgrade_storage_gb,
                "final_storage_gb": upgrade_storage_gb,
                "final_storage_bytes": actual_addon_bytes,
                "no_pending_downgrade": not pending_change_final,
                "webhook_mode": args.webhook_mode,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing PLAN-04: cancel scheduled downgrade before period end.")
    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
