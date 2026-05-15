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
API-adjusted driver for POINT-04.
Tests: a canceled or abandoned points checkout does not create credits or recovery state.

Test flow:
- Step 1: Setup - Register user and initialize environment
- Step 2: Record baseline - Capture points balance, ledger, spend history, and plan overview before testing
- Step 3: Create and expire checkout - Create a points checkout session and expire it
- Step 4: Verify results - Validate no state mutation or payment recovery occurred
"""

from __future__ import annotations

import argparse
import json
import sys
import uuid
from pathlib import Path

import stripe  # type: ignore[reportMissingImports]

from tools.billing.billing_common import make_default_parser
from tools.billing.billing_client import create_client_with_type

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from tools.billing.billing_common import FlowError
from tools.billing.points_common import (
    load_points_runtime_config, PointsClient,
)
from tools.billing.points_case_common import get_points_case_metadata


def run_flow(args: argparse.Namespace) -> None:
    """Execute POINT-04: a canceled or abandoned points checkout does not create credits or recovery state."""
    case_metadata = get_points_case_metadata("POINT-04")
    runtime = load_points_runtime_config()
    points_per_unit = int(runtime["points_per_unit"])

    email = args.email or f"billing-point04-{uuid.uuid4().hex[:12]}@example.test"
    client: PointsClient = create_client_with_type(args, email, PointsClient)

    # =============================================================================
    # Step 1: Setup - Register user and initialize environment
    # =============================================================================
    print("=" * 80)
    print("Step 1: Setup - Register user and initialize environment")
    print("=" * 80)
    print(f"  Assert: User registered with email: {email}")
    print(f"  Assert: Tenant ID: {client.tenant_id}")

    # =============================================================================
    # Step 2: Record baseline - Capture pre-test state
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 2: Record baseline - Capture pre-test state")
    print("=" * 80)

    before_balance = client.points_balance()
    before_ledger = client.points_ledger()
    before_history = client.spend_history()
    before_overview = client.plan_overview()

    available_before = int(before_balance.get("available_points") or 0)
    held_before = int(before_balance.get("held_points") or 0)
    ledger_before_count = int(before_ledger.get("total") or 0)
    history_before_count = len(before_history)
    payment_required_before = before_overview.get("payment_required", False)

    print(f"  Assert: Available points before: {available_before}")
    print(f"  Assert: Held points before: {held_before}")
    print(f"  Assert: Ledger total before: {ledger_before_count}")
    print(f"  Assert: History rows before: {history_before_count}")
    print(f"  Assert: Payment required before: {payment_required_before}")

    # =============================================================================
    # Step 3: Create and expire checkout - Create a points checkout session and expire it
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 3: Create and expire checkout - Create a points checkout session and expire it")
    print("=" * 80)

    points_to_buy = 100
    _, session = client.create_points_checkout_session(points_to_buy, points_per_unit)
    expired = stripe.checkout.Session.expire(session["id"])
    expired_dict = expired.to_dict_recursive() if hasattr(expired, "to_dict_recursive") else dict(expired)
    expired_status = str(expired_dict.get("status") or "")
    if expired_status != "expired":
        raise FlowError(f"expected Stripe checkout session to expire, got {expired_dict}")
    print(f"  Assert: Checkout session created: {session['id']}")
    print(f"  Assert: Session expired with status: {expired_status}")

    # =============================================================================
    # Step 4: Verify results - Validate no state mutation or payment recovery occurred
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 4: Verify results - Validate no state mutation or payment recovery occurred")
    print("=" * 80)

    after_balance = client.points_balance()
    after_ledger = client.points_ledger()
    after_history = client.spend_history()
    after_overview = client.plan_overview()

    available_after = int(after_balance.get("available_points") or 0)
    held_after = int(after_balance.get("held_points") or 0)
    ledger_after_count = int(after_ledger.get("total") or 0)
    history_after_count = len(after_history)
    payment_required_after = after_overview.get("payment_required", False)

    if available_after != available_before:
        raise FlowError(f"canceled checkout should not change balance: before={available_before}, after={available_after}")
    print(f"  Assert: Available points unchanged: {available_after}")

    if held_after != held_before:
        raise FlowError(f"canceled checkout should not change held points: before={held_before}, after={held_after}")
    print(f"  Assert: Held points unchanged: {held_after}")

    if ledger_after_count != ledger_before_count:
        raise FlowError(f"canceled checkout should not change ledger count: before={ledger_before_count}, after={ledger_after_count}")
    print(f"  Assert: Ledger total unchanged: {ledger_after_count}")

    if history_after_count != history_before_count:
        raise FlowError(
            f"canceled checkout should not change billing history count: before={history_before_count}, after={history_after_count}"
        )
    print(f"  Assert: History total unchanged: {history_after_count}")

    if any(row.get("invoice_id") == session["id"] for row in after_history):
        raise FlowError(f"canceled checkout should not create a spend history row for session {session['id']}")
    print("  Assert: No spend history row created for expired session")

    if payment_required_before:
        raise FlowError(f"fresh tenant should not start with payment_required=true: {before_overview}")
    print("  Assert: Fresh tenant did not start with payment_required=true")

    if payment_required_after:
        raise FlowError(f"canceled checkout should not trigger payment recovery banner state: {after_overview}")
    print(f"  Assert: Payment required unchanged: {payment_required_after}")

    # =============================================================================
    # POINT-04 Test Summary
    # =============================================================================
    print("\n" + "=" * 80)
    print("POINT-04 Test Summary")
    print("=" * 80)
    print(
        json.dumps(
            {
                **case_metadata,
                "tenant_id": client.tenant_id,
                "email": email,
                "checkout_session_id": session["id"],
                "stripe_session_status": expired_status,
                "balance_after": after_balance,
                "ledger_total_after": ledger_after_count,
                "history_total_after": history_after_count,
                "payment_required_before": payment_required_before,
                "payment_required_after": payment_required_after,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing POINT-04: canceled points checkout should not mutate state.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
