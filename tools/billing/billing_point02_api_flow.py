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
API-adjusted driver for POINT-02.
Tests: sequential purchase of 500 points and 1000 points with cumulative accounting.

Test flow:
- Step 1: Setup - Register user and initialize environment
- Step 2: Record baseline - Capture points balance, ledger, and spend history before purchase
- Step 3: Purchase points - Complete two sequential checkout sessions (500 and 1000 points)
- Step 4: Verify results - Validate cumulative balance increase, ledger entries, and paid history records
"""

from __future__ import annotations

import argparse
import json
import sys
import uuid
from pathlib import Path

import stripe

from tools.billing.billing_common import make_default_parser
from tools.billing.billing_client import create_client_with_type

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from tools.billing.billing_common import FlowError
from tools.billing.points_common import (
    PointsClient,
    load_points_runtime_config,
)
from tools.billing.points_case_common import get_checkout_session_amount, get_points_case_metadata


def run_flow(args: argparse.Namespace) -> None:
    """Execute POINT-02: sequential purchase of 500 and 1000 points with cumulative accounting."""
    case_metadata = get_points_case_metadata("POINT-02")
    runtime = load_points_runtime_config()
    points_per_unit = int(runtime["points_per_unit"])


    email = args.email or f"billing-point02-{uuid.uuid4().hex[:12]}@example.test"
    client:PointsClient = create_client_with_type(args, email, PointsClient)

    points_first = points_per_unit * 5
    points_second = points_per_unit * 10
    total_points = points_first + points_second

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
    # Step 3: Purchase points - Complete two sequential checkout sessions
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 3: Purchase points - Complete two sequential checkout sessions")
    print("=" * 80)


    pi_first = stripe.PaymentIntent.create(
        amount=points_first,
        currency="USD",
        customer=client.customer_id,
        description="First points recharge (test)",
        metadata={"source": "points_common_test", "sequence": "first"},
    )

    pi_second = stripe.PaymentIntent.create(
        amount=points_second,
        currency="USD",
        customer=client.customer_id,
        description="Second points recharge (test)",
        metadata={"source": "points_common_test", "sequence": "second"},
    )

    session_first = client.complete_points_purchase(
        points_first, points_per_unit, payment_intent_id=pi_first.id
    )
    print(f"  Assert: First checkout session created: {session_first['id']}")
    print(f"  Assert: Points to purchase (first): {points_first}")

    session_second = client.complete_points_purchase(
        points_second, points_per_unit, payment_intent_id=pi_second.id
    )
    print(f"  Assert: Second checkout session created: {session_second['id']}")
    print(f"  Assert: Points to purchase (second): {points_second}")

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
    if available_after != available_before + total_points:
        raise FlowError(
            f"available_points should increase by {total_points}, before={available_before}, after={available_after}"
        )
    print(f"  Assert: Available points after: {available_after} (increased by {total_points})")

    if held_after != held_before:
        raise FlowError(f"held_points should not change on recharge, before={held_before}, after={held_after}")
    print(f"  Assert: Held points unchanged: {held_after}")

    ledger_after_count = int(after_ledger.get("total") or 0)
    if ledger_after_count != ledger_before_count + 2:
        raise FlowError(
            f"expected exactly two new ledger rows, before={ledger_before_count}, after={ledger_after_count}"
        )
    after_ledger_items = after_ledger.get("items") or []
    recharge_rows = [
        row
        for row in after_ledger_items
        if row.get("event_type") == "recharge"
        and (row.get("metadata") or {}).get("session_id") in {session_first["id"], session_second["id"]}
    ]
    if len(recharge_rows) != 2:
        raise FlowError(
            f"expected exactly two recharge ledger rows for sessions {session_first['id']} and {session_second['id']}, got {recharge_rows}"
        )
    points_by_session = {row["metadata"]["session_id"]: int(row.get("points") or 0) for row in recharge_rows}
    if (
        points_by_session.get(session_first["id"]) != points_first
        or points_by_session.get(session_second["id"]) != points_second
    ):
        raise FlowError(f"unexpected ledger point amounts by session: {points_by_session}")
    print("  Assert: Two recharge ledger rows verified with correct point amounts")

    history_after_count = len(after_history)
    if history_after_count != history_before_count + 2:
        raise FlowError(
            f"expected exactly two new billing history rows, before={history_before_count}, after={history_after_count}"
        )
    after_history_rows = [row for row in after_history if row.get("invoice_id") in {session_first["id"], session_second["id"]}]
    if len(after_history_rows) != 2:
        raise FlowError(f"expected exactly two history rows for the new sessions, got {after_history_rows}")
    if any(row.get("status") != "paid" for row in after_history_rows):
        raise FlowError(f"all points history rows should be paid, got {after_history_rows}")
    amount_by_session = {row["invoice_id"]: float(row.get("amount", 0) or 0) for row in after_history_rows}
    if abs(amount_by_session.get(session_first["id"], -1) - get_checkout_session_amount(session_first)) > 1e-9:
        raise FlowError(f"unexpected amount for first purchase row: {after_history_rows}")
    if abs(amount_by_session.get(session_second["id"], -1) - get_checkout_session_amount(session_second)) > 1e-9:
        raise FlowError(f"unexpected amount for second purchase row: {after_history_rows}")
    print("  Assert: Two billing history rows verified with status 'paid' and correct amounts")

    # =============================================================================
    # POINT-02 Test Summary
    # =============================================================================
    print("\n" + "=" * 80)
    print("POINT-02 Test Summary")
    print("=" * 80)
    print(
        json.dumps(
            {
                **case_metadata,
                "tenant_id": client.tenant_id,
                "email": email,
                "first_checkout_session_id": session_first["id"],
                "second_checkout_session_id": session_second["id"],
                "points_first": points_first,
                "points_second": points_second,
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
    parser = make_default_parser("Run billing POINT-02: sequential purchase of 500 and 1000 points.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
