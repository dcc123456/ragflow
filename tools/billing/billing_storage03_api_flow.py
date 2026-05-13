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
API-adjusted driver for STORAGE-03.
Tests: upgrading storage addon (increase quantity) takes effect immediately.

Required environment:
  BILLING_STRIPE_API_KEY or STRIPE_API_KEY
  BILLING_PRICE_ID_STARTER (for plan subscription)
  BILLING_STORAGE_PRICE_ID (for storage addon)

Optional environment:
  RAGFLOW_BASE_URL=http://127.0.0.1:9380
  RAGFLOW_API_VERSION=v1
  RAGFLOW_TEST_EMAIL=<fresh email>
  RAGFLOW_TEST_PASSWORD=Test1234!
  BILLING_WEBHOOK_SECRET or STRIPE_WEBHOOK_SECRET (optional if local DB already stores billing_webhook_secret)
"""

from __future__ import annotations

import json
import sys
import uuid

import stripe  # type: ignore[reportMissingImports]

from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    make_default_parser,
)
from tools.billing.storage_common import (  # noqa: E402
    RAGFlowClient,
    gb_to_bytes,
    replace_storage_subscription_quantity,
    setup_starter,
)


def run_flow(args) -> None:
    # =============================================================================
    # Steps 1-5: Setup Starter environment using shared utility
    # =============================================================================
    print("=" * 80)
    print("Steps 1-5: Setup Starter environment using shared utility")
    print("=" * 80)

    email = args.email or f"billing-storage03-{uuid.uuid4().hex[:12]}@example.test"
    setup = setup_starter(args, email=email)

    client: RAGFlowClient = setup["client"]
    tenant_id: str = setup["tenant_id"]
    customer_id: str = setup["customer_id"]
    starter_subscription_id: str = setup["subscription_id"]
    print("  Assert: Starter environment ready")
    print(f"  Assert: Tenant ID: {tenant_id}")
    print(f"  Assert: Customer ID: {customer_id}")
    print(f"  Assert: Starter subscription ID: {starter_subscription_id}")

    # =============================================================================
    # Step 6: Purchase initial storage addon (10GB) via direct subscription modification
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Purchase initial storage addon (10GB)")
    print("=" * 80)

    initial_storage_gb = 10
    initial_target_bytes = gb_to_bytes(initial_storage_gb)
    print(f"  Info: Adding {initial_storage_gb}GB storage addon to subscription {starter_subscription_id}")

    replace_storage_subscription_quantity(
        client=client,
        tenant_id=tenant_id,
        new_quantity_gb=initial_storage_gb,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage addon modification sent")

    client.wait_for_storage_status(tenant_id, "active", timeout_seconds=30)
    print("  Assert: Storage addon is active")

    # Verify storage addon quantity
    storage = client.storage_current(tenant_id)
    addon_bytes_initial = int(storage.get("addon_storage_bytes") or 0)
    if addon_bytes_initial != initial_target_bytes:
        raise FlowError(f"expected addon_storage_bytes={initial_target_bytes}, got {addon_bytes_initial}")
    print(f"  Assert: Addon storage is {addon_bytes_initial} bytes ({initial_storage_gb}GB)")

    # Verify subscription has two items (plan + storage)
    sub = stripe.Subscription.retrieve(starter_subscription_id)
    items = sub.get("items", {}).get("data", [])
    if len(items) != 2:
        raise FlowError(f"expected 2 subscription items, got {len(items)}")
    print(f"  Assert: Subscription has {len(items)} items (plan + storage)")

    # =============================================================================
    # Step 7: Upgrade storage addon to higher quantity (10GB -> 20GB) immediately
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 7: Upgrade storage addon from 10GB to 20GB (immediate effect)")
    print("=" * 80)

    upgraded_storage_gb = 20
    upgraded_target_bytes = gb_to_bytes(upgraded_storage_gb)

    print(f"  Info: Upgrading storage quantity to {upgraded_storage_gb}GB")
    replace_storage_subscription_quantity(
        client=client,
        tenant_id=tenant_id,
        new_quantity_gb=upgraded_storage_gb,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage upgrade modification sent")

    client.wait_for_storage_status(tenant_id, "active", timeout_seconds=30)

    storage_after_upgrade = client.storage_current(tenant_id)
    addon_bytes_upgraded = int(storage_after_upgrade.get("addon_storage_bytes") or 0)

    if addon_bytes_upgraded != upgraded_target_bytes:
        raise FlowError(f"expected addon_storage_bytes={upgraded_target_bytes} after upgrade, got {addon_bytes_upgraded}")
    print(f"  Assert: Addon storage is {addon_bytes_upgraded} bytes ({upgraded_storage_gb}GB)")

    if addon_bytes_upgraded <= addon_bytes_initial:
        raise FlowError(
            f"addon_storage_bytes should increase after upgrade, "
            f"before={addon_bytes_initial}, after={addon_bytes_upgraded}"
        )
    print("  Assert: Storage quota increased immediately (not at period end)")

    # Optional: verify billing history contains both purchase and upgrade invoices
    history = client.spend_history()
    print(f"  Info: Billing history contains {len(history)} records")

    print(
        json.dumps(
            {
                "case": "STORAGE-03",
                "description": "Storage addon upgrade (immediate effect)",
                "tenant_id": tenant_id,
                "email": email,
                "initial_storage_gb": initial_storage_gb,
                "upgraded_storage_gb": upgraded_storage_gb,
                "addon_bytes_after_initial": addon_bytes_initial,
                "addon_bytes_after_upgrade": addon_bytes_upgraded,
                "history_count": len(history),
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing STORAGE-03: storage addon upgrade (immediate effect).")
    try:
        run_flow(parser.parse_args())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
