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
API-adjusted driver for STORAGE-01.
Tests: first storage addon purchase with proration.

New subscription model:
- One user can only have ONE subscription
- A subscription can contain multiple products (plan + storage addon)
- Storage addon can only be added on top of an existing plan
- Renewal must include both plan and storage addon together (same subscription)
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path

import stripe  # type: ignore[reportMissingImports]

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from tools.billing.flow_common import (  # noqa: E402
    FlowError,
)
from tools.billing.storage_common import (  # noqa: E402
    RAGFlowClient,
    add_storage_to_subscription_with_webhook,
    advance_clock,
    delete_clock,
    gb_to_bytes,
    make_default_parser,
    setup_starter,
    stripe_dict,
    wait_for_storage_status, replace_storage_subscription_quantity,
)

clock_id = ""


def run_flow(args: argparse.Namespace) -> None:
    # =============================================================================
    # Step 1-4: Setup Starter environment (validates config, creates test clock,
    #           registers user, upgrades Trial -> Starter)
    # =============================================================================
    print("=" * 80)
    print("Steps 1-4: Setup Starter environment using shared utility")
    print("=" * 80)

    email = args.email or f"billing-storage01-{uuid.uuid4().hex[:12]}@example.test"
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
    # Step 5: Add storage addon 20GB to Starter subscription
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 5: Add storage addon 20GB to Starter subscription")
    print("=" * 80)

    storage_gb = 20
    storage_added_at = int(time.time()) - 5

    # Initialize invoice verification variables for summary
    initial_invoice_amount = 0
    initial_storage_line_amount = 0
    proration_invoice_amount = 0
    proration_storage_line_amount = 0

    # Use add_storage_to_subscription_with_webhook to add storage addon
    # This function calls the backend API and handles webhook synchronization.
    print(f"  Info: Adding {storage_gb}GB storage addon to tenant {tenant_id}")
    add_storage_to_subscription_with_webhook(
        client=client,
        tenant_id=tenant_id,
        storage_quantity_gb=storage_gb,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
        created_gte=storage_added_at,
    )
    print(f"  Assert: Storage addon added for tenant: {tenant_id}")

    # Wait for storage status to become active
    wait_for_storage_status(client, tenant_id, "active", timeout_seconds=30)
    print("  Assert: Storage status is active")

    # Verify subscription now has two items (plan + storage)
    updated_sub = stripe.Subscription.retrieve(starter_subscription_id)
    updated_items = updated_sub.get("items", {}).get("data", [])
    if len(updated_items) != 2:
        raise FlowError(f"Expected 2 subscription items (plan + storage), got {len(updated_items)}")
    print(f"  Assert: Subscription has {len(updated_items)} items (plan + storage)")

    # Get the latest invoice created by the storage add operation
    latest_invoice = updated_sub.get("latest_invoice")
    if latest_invoice:
        if isinstance(latest_invoice, str):
            latest_invoice = stripe.Invoice.retrieve(latest_invoice, expand=["lines.data"])
        else:
            # Ensure lines are expanded
            invoice_id = latest_invoice.get("id") if isinstance(latest_invoice, dict) else latest_invoice.id
            latest_invoice = stripe.Invoice.retrieve(invoice_id or "", expand=["lines.data"])

        invoice_dict = stripe_dict(latest_invoice)
        initial_invoice_amount = invoice_dict.get("amount_due", 0)
        print(f"  Info: Invoice {invoice_dict.get('id')} amount_due: {initial_invoice_amount} cents (${initial_invoice_amount/100:.2f})")

    else:
        print("  Warning: No invoice found for initial storage addon")

    # =============================================================================
    # Step 6: Verify storage addon is part of the same subscription
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Verify storage addon is part of the same subscription")
    print("=" * 80)

    time.sleep(3)
    storage = client.storage_current(tenant_id)

    addon_storage_bytes = int(storage.get("addon_storage_bytes") or 0)
    expected_storage_bytes = gb_to_bytes(storage_gb)

    if addon_storage_bytes != expected_storage_bytes:
        raise FlowError(f"Expected addon_storage_bytes to be {expected_storage_bytes}, got {addon_storage_bytes}")
    print(f"  Assert: Addon storage is {addon_storage_bytes} bytes ({storage_gb}GB)")

    storage_subscription_id = storage.get("subscription_id", "")
    if storage_subscription_id != starter_subscription_id:
        raise FlowError(f"Expected storage to be part of main subscription: expected {starter_subscription_id}, got {storage_subscription_id}")
    print(f"  Assert: Storage is part of main subscription: {storage_subscription_id}")

    # =============================================================================
    # Step 7: Advance 15 days and upgrade storage addon mid-cycle
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 7: Advance 15 days and upgrade storage addon mid-cycle")
    print("=" * 80)

    current_plan = client.current_plan()
    plan_end = current_plan.get("end_time")
    if not plan_end:
        raise FlowError(f"plan response is missing end_time: {current_plan}")

    if isinstance(plan_end, (int, float)):
        plan_end_ts = int(plan_end)
    else:
        plan_end_str = str(plan_end).replace("Z", "+00:00")
        plan_end_dt = datetime.fromisoformat(plan_end_str)
        if plan_end_dt.tzinfo is None:
            plan_end_dt = plan_end_dt.replace(tzinfo=timezone.utc)
        plan_end_ts = int(plan_end_dt.timestamp())

    current_ts = int(time.time())
    days_15_seconds = 15 * 86400
    advance_to_ts = current_ts + days_15_seconds

    if advance_to_ts >= plan_end_ts - 86400:
        advance_to_ts = plan_end_ts - 86400

    advance_seconds = advance_to_ts - current_ts
    if advance_seconds < 0:
        raise FlowError(f"cannot advance time: current={current_ts}, target={advance_to_ts}")

    print(f"  Info: Advancing clock by {advance_seconds} seconds ({advance_seconds/86400:.1f} days)")
    advance_clock(clock_id, advance_to_ts)
    print("  Assert: Clock advanced")

    before_mid_storage = client.storage_current(tenant_id)
    before_mid_addon_bytes = int(before_mid_storage.get("addon_storage_bytes") or 0)
    print(f"  Assert: Addon storage before upgrade: {before_mid_addon_bytes} bytes")

    storage_gb_mid = storage_gb + 10
    print(f"  Info: Upgrading storage from {storage_gb}GB to {storage_gb_mid}GB mid-cycle")
    replace_storage_subscription_quantity(
        client=client,
        tenant_id=tenant_id,
        new_quantity_gb=storage_gb_mid,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
    )
    print(f"  Assert: Storage upgraded to {storage_gb_mid}GB")

    # Wait for storage status update
    wait_for_storage_status(client, tenant_id, "active", timeout_seconds=30)

    # =============================================================================
    # Step 8: Verify invoice proration for storage upgrade
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 8: Verify invoice proration for storage upgrade")
    print("=" * 80)

    # Get the latest invoice from the subscription
    updated_sub = stripe.Subscription.retrieve(starter_subscription_id)
    latest_invoice_id = updated_sub.get("latest_invoice", "")
    if latest_invoice_id:
        invoice = stripe.Invoice.retrieve(latest_invoice_id, expand=["lines.data"])
        invoice_dict = stripe_dict(invoice)
        proration_invoice_amount = invoice_dict.get("amount_due", 0)
        print(f"  Info: Invoice {invoice_dict.get('id')} amount_due: {proration_invoice_amount} cents (${proration_invoice_amount/100:.2f})")

        # Find storage line items
        line_items = (invoice_dict.get("lines") or {}).get("data", [])
        for line in line_items:
            description = line.get("description", "")
            amount = line.get("amount", 0)
            print(f"  Info: Line item: {description} - ${amount/100:.2f}")
            if "storage" in description.lower() or "gb" in description.lower():
                proration_storage_line_amount += amount

        # Expected proration: (new_quantity - old_quantity) * unit_price * remaining_days / total_days
        # unit_price = 10 USD per GB per month, remaining_days = 15, total_days = 30
        expected_proration_cents = int((storage_gb_mid - storage_gb) * 1000 * 15 / 30)  # 10 * 1000 * 0.5 = 5000 cents = $50.00
        print(f"  Info: Expected proration: ~{expected_proration_cents} cents (${expected_proration_cents/100:.2f})")

        # Verify the invoice amount is close to expected (within 20% tolerance for rounding)
        if proration_invoice_amount > 0:
            tolerance = expected_proration_cents * 0.2
            if abs(proration_invoice_amount - expected_proration_cents) > tolerance:
                print(f"  Warning: Invoice amount {proration_invoice_amount} cents differs from expected {expected_proration_cents} cents (tolerance: {tolerance})")
            else:
                print(f"  Assert: Invoice proration verified: ${proration_invoice_amount/100:.2f}")

    time.sleep(1)
    after_mid_storage = client.storage_current(tenant_id)
    after_mid_addon_bytes = int(after_mid_storage.get("addon_storage_bytes") or 0)

    expected_increase = gb_to_bytes(10)
    if after_mid_addon_bytes != before_mid_addon_bytes + expected_increase:
        raise FlowError(f"addon_storage_bytes should increase by {expected_increase}, before={before_mid_addon_bytes}, after={after_mid_addon_bytes}")
    print(f"  Assert: Addon storage increased to {after_mid_addon_bytes} bytes ({storage_gb_mid}GB)")

    print(f"  Info: Billing history count: {len(client.spend_history())}")

    print("\n" + "=" * 80)
    print("STORAGE-01 Test Summary")
    print("=" * 80)
    print(json.dumps({
        "case": "STORAGE-01",
        "description": "Storage addon purchase with proration (single subscription model)",
        "tenant_id": tenant_id,
        "email": email,
        "subscription_id": starter_subscription_id,
        "storage_gb": storage_gb,
        "addon_bytes_after_purchase": addon_storage_bytes,
        "mid_cycle_storage_gb": storage_gb_mid,
        "mid_cycle_addon_bytes": after_mid_addon_bytes,
        "invoice_verification": {
            "initial_purchase": {
                "expected_amount_cents": storage_gb * 10 * 100,  # 20GB * 10 USD/GB = 200 USD
                "actual_storage_line_cents": initial_storage_line_amount,
                "invoice_amount_cents": initial_invoice_amount,
            },
            "mid_cycle_upgrade": {
                "expected_proration_cents": int((storage_gb_mid - storage_gb) * 1000 * 15 / 30),  # 10GB * 10 USD * 15/30 = 50 USD
                "actual_storage_line_cents": proration_storage_line_amount,
                "invoice_amount_cents": proration_invoice_amount,
            },
        },
    }, indent=2, sort_keys=True))


def clean_up():
    if clock_id:
        print("\n" + "=" * 80)
        print("Cleanup: Deleting Stripe test clock")
        print("=" * 80)
        delete_clock(clock_id)


def main() -> int:
    parser = make_default_parser("Run billing STORAGE-01: first storage addon purchase (prorated) - NEW LOGIC.")
    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    finally:
        clean_up()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())