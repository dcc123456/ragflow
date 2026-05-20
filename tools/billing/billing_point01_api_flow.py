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
API-adjusted driver for POINT-01.
Tests: successful purchase of the minimum valid 100 points recharge.

This is an adjusted automation case:
- it creates the Checkout Session through the billing API,
- but completes the purchase via a synthetic signed `checkout.session.completed`
  webhook instead of driving hosted Stripe Checkout in a browser.

Test flow:
- Step 1: Setup - Register user and initialize environment
- Step 2: Record baseline - Capture points balance, ledger, and spend history before purchase
- Step 3: Purchase points - Complete a 100 points checkout session via synthetic webhook
- Step 4: Verify results - Validate balance increase, ledger entry, and paid history record
"""

from __future__ import annotations

import argparse
import json
import sys
import uuid
from pathlib import Path

import stripe

from tools.billing.billing_common import make_default_parser, FlowError
from tools.billing.billing_client import create_client_with_type

from tools.billing.points_common import (
    PointsClient, load_points_runtime_config
)
from tools.billing.points_case_common import get_checkout_session_amount, get_points_case_metadata

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

def run_flow(args: argparse.Namespace) -> None:
    """Execute POINT-01: successful purchase of 100 points recharge test."""
    case_metadata = get_points_case_metadata("POINT-01")
    runtime = load_points_runtime_config()
    points_per_unit = int(runtime["points_per_unit"])

    email = args.email or f"billing-point01-{uuid.uuid4().hex[:12]}@example.test"
    client:PointsClient = create_client_with_type(args, email, PointsClient)

    # =============================================================================
    # Step 1: Setup - Register user and initialize environment
    # =============================================================================
    print("=" * 80)
    print("Step 1: Setup - Register user and initialize environment")
    print("=" * 80)
    print(f"  Assert: User registered with email: {email}")
    print(f"  Assert: Tenant ID: {client.tenant_id}")

    # =============================================================================
    # Step 2: Record baseline - Capture pre-purchase state
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 2: Record baseline - Capture pre-purchase state")
    print("=" * 80)

    before_balance = client.points_balance()
    before_ledger = client.points_ledger()
    before_history = client.spend_history()

    available_before = int(before_balance.get("available_points") or 0)
    held_before = int(before_balance.get("held_points") or 0)
    ledger_before_count = int(before_ledger.get("total") or 0)
    history_before_count = len(before_history)

    print(f"  Assert: Available points before: {available_before}")
    print(f"  Assert: Held points before: {held_before}")
    print(f"  Assert: Ledger total before: {ledger_before_count}")
    print(f"  Assert: History rows before: {history_before_count}")

    # =============================================================================
    # Step 3: Purchase points - Complete a 100 points checkout session
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 3: Purchase points - Complete a 100 points checkout session via synthetic webhook")
    print("=" * 80)

    points_to_buy = 100
    pi = stripe.PaymentIntent.create(
        amount=points_to_buy,
        currency="USD",
        customer=client.customer_id,
        description="Manual points recharge (test)",
        metadata={"source": "points_common_test"},
    )

    session = client.complete_points_purchase_via_synthetic_webhook(
        points_to_buy,
        points_per_unit,
        payment_intent_id=pi.id,
    )
    print(f"  Assert: Checkout session created: {session['id']}")
    print(f"  Assert: Points to purchase: {points_to_buy}")

    # =============================================================================
    # Step 4: Verify results - Validate post-purchase state
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 4: Verify results - Validate post-purchase state")
    print("=" * 80)

    after_balance = client.points_balance()
    after_ledger = client.points_ledger()
    after_history = client.spend_history()

    available_after = int(after_balance.get("available_points") or 0)
    held_after = int(after_balance.get("held_points") or 0)
    if available_after != available_before + points_per_unit:
        raise FlowError(
            f"available_points should increase by {points_per_unit}, before={available_before}, after={available_after}"
        )
    print(f"  Assert: Available points after: {available_after} (increased by {points_per_unit})")

    if held_after != held_before:
        raise FlowError(f"held_points should not change on recharge, before={held_before}, after={held_after}")
    print(f"  Assert: Held points unchanged: {held_after}")

    ledger_after_count = int(after_ledger.get("total") or 0)
    if ledger_after_count != ledger_before_count + 1:
        raise FlowError(
            f"expected exactly one new ledger row, before={ledger_before_count}, after={ledger_after_count}"
        )
    recharge_rows = [
        row
        for row in (after_ledger.get("items") or [])
        if row.get("event_type") == "recharge"
        and int(row.get("points") or 0) == points_per_unit
        and ((row.get("metadata") or {}).get("session_id") == session["id"])
    ]
    if len(recharge_rows) != 1:
        raise FlowError(f"expected exactly one recharge ledger row for session {session['id']}, got {recharge_rows}")
    print(f"  Assert: New recharge ledger row verified for session {session['id']}")

    history_after_count = len(after_history)
    if history_after_count != history_before_count + 1:
        raise FlowError(
            f"expected exactly one new billing history row, before={history_before_count}, after={history_after_count}"
        )
    history_rows = [row for row in after_history if row.get("invoice_id") == session["id"]]
    if len(history_rows) != 1:
        raise FlowError(f"expected exactly one history row for session {session['id']}, got {history_rows}")
    history_row = history_rows[0]
    if history_row.get("status") != "paid":
        raise FlowError(f"points recharge history should be paid, got {history_row}")
    expected_amount = get_checkout_session_amount(session)
    if abs(float(history_row.get("amount", 0) or 0) - expected_amount) > 1e-9:
        raise FlowError(f"points recharge history amount should be {expected_amount}, got {history_row}")
    print(f"  Assert: Billing history row verified with status 'paid' and amount ${expected_amount}")

    # =============================================================================
    # POINT-01 Test Summary
    # =============================================================================
    print("\n" + "=" * 80)
    print("POINT-01 Test Summary")
    print("=" * 80)
    print(
        json.dumps(
            {
                **case_metadata,
                "tenant_id": client.tenant_id,
                "email": email,
                "checkout_session_id": session["id"],
                "points_purchased": points_per_unit,
                "available_points_before": available_before,
                "available_points_after": available_after,
                "ledger_total_before": ledger_before_count,
                "ledger_total_after": ledger_after_count,
                "history_total_before": history_before_count,
                "history_total_after": history_after_count,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing POINT-01: successful purchase of 100 points.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
