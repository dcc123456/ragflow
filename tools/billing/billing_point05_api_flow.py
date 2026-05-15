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
API-adjusted driver for POINT-05.
Tests: replaying the same successful points webhook remains idempotent.

Test flow:
- Step 1: Setup - Register user and initialize environment
- Step 2: Record baseline - Capture points balance, ledger, and spend history before testing
- Step 3: Process webhook - Create checkout session and post signed webhook event
- Step 4: Verify idempotency - Replay webhook and validate no duplicate state mutation
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
    build_points_checkout_completed_event,
    load_points_runtime_config, PointsClient,
)
from tools.billing.points_case_common import get_points_case_metadata


def run_flow(args: argparse.Namespace) -> None:
    """Execute POINT-05: replaying the same successful points webhook remains idempotent."""
    case_metadata = get_points_case_metadata("POINT-05")
    runtime = load_points_runtime_config()
    points_per_unit = int(runtime["points_per_unit"])

    email = args.email or f"billing-point05-{uuid.uuid4().hex[:12]}@example.test"
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

    available_before = int(before_balance.get("available_points") or 0)
    held_before = int(before_balance.get("held_points") or 0)
    ledger_before_count = int(before_ledger.get("total") or 0)
    history_before_count = len(before_history)

    print(f"  Assert: Available points before: {available_before}")
    print(f"  Assert: Held points before: {held_before}")
    print(f"  Assert: Ledger total before: {ledger_before_count}")
    print(f"  Assert: History rows before: {history_before_count}")

    # =============================================================================
    # Step 3: Process webhook - Create checkout session and post signed webhook event
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 3: Process webhook - Create checkout session and post signed webhook event")
    print("=" * 80)

    points_to_buy = points_per_unit
    pi = stripe.PaymentIntent.create(
        amount=points_to_buy,
        currency="USD",
        customer=client.customer_id,
        description="Points recharge for idempotency test",
        metadata={"source": "point05_test"},
    )
    payment_intent_id = pi.id

    _, session = client.create_points_checkout_session(int(points_to_buy/points_per_unit), points_per_unit)
    event = build_points_checkout_completed_event(
        event_id=f"evt_manual_points_replay_{uuid.uuid4().hex[:20]}",
        session=session,
        points=points_per_unit,
        payment_intent_id=payment_intent_id,
    )

    client.post_signed_webhook(event)
    print(f"  Assert: Checkout session created: {session['id']}")
    print("  Assert: First webhook posted successfully")

    after_first_balance = client.points_balance()
    after_first_ledger = client.points_ledger()
    after_first_history = client.spend_history()

    available_after_first = int(after_first_balance.get("available_points") or 0)
    ledger_after_first_count = int(after_first_ledger.get("total") or 0)
    history_after_first_count = len(after_first_history)

    if available_after_first != available_before + points_per_unit:
        raise FlowError(
            f"first webhook should credit {points_per_unit} points, before={available_before}, after_first={available_after_first}"
        )
    print(f"  Assert: Available points after first webhook: {available_after_first} (increased by {points_per_unit})")

    if ledger_after_first_count != ledger_before_count + 1:
        raise FlowError(
            f"first webhook should add one ledger row, before={ledger_before_count}, after_first={ledger_after_first_count}"
        )
    print(f"  Assert: Ledger total after first webhook: {ledger_after_first_count} (increased by 1)")

    if history_after_first_count != history_before_count + 1:
        raise FlowError(
            f"first webhook should add one history row, before={history_before_count}, after_first={history_after_first_count}"
        )
    print(f"  Assert: History total after first webhook: {history_after_first_count} (increased by 1)")

    # =============================================================================
    # Step 4: Verify idempotency - Replay webhook and validate no duplicate state mutation
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 4: Verify idempotency - Replay webhook and validate no duplicate state mutation")
    print("=" * 80)

    client.post_signed_webhook(event)
    print("  Assert: Second webhook (replay) posted successfully")

    after_replay_balance = client.points_balance()
    after_replay_ledger = client.points_ledger()
    after_replay_history = client.spend_history()

    available_after_replay = int(after_replay_balance.get("available_points") or 0)
    ledger_after_replay_count = int(after_replay_ledger.get("total") or 0)
    history_after_replay_count = len(after_replay_history)

    if available_after_replay != available_after_first:
        raise FlowError(
            f"replayed webhook should not credit again, after_first={available_after_first}, after_replay={available_after_replay}"
        )
    print(f"  Assert: Available points unchanged after replay: {available_after_replay}")

    if ledger_after_replay_count != ledger_after_first_count:
        raise FlowError(
            f"replayed webhook should not add another ledger row, after_first={ledger_after_first_count}, after_replay={ledger_after_replay_count}"
        )
    print(f"  Assert: Ledger total unchanged after replay: {ledger_after_replay_count}")

    replay_ledger_rows = [
        row
        for row in (after_replay_ledger.get("items") or [])
        if (row.get("metadata") or {}).get("session_id") == session["id"]
    ]
    if len(replay_ledger_rows) != 1:
        raise FlowError(f"expected exactly one ledger row for replayed session {session['id']}, got {replay_ledger_rows}")
    print(f"  Assert: Only one ledger row exists for session {session['id']}")

    if history_after_replay_count != history_after_first_count:
        raise FlowError(
            f"replayed webhook should not add another history row, after_first={history_after_first_count}, after_replay={history_after_replay_count}"
        )
    print(f"  Assert: History total unchanged after replay: {history_after_replay_count}")

    replay_history_rows = [row for row in after_replay_history if row.get("invoice_id") == session["id"]]
    if len(replay_history_rows) != 1:
        raise FlowError(f"expected exactly one history row for replayed session {session['id']}, got {replay_history_rows}")
    print(f"  Assert: Only one history row exists for session {session['id']}")

    # =============================================================================
    # POINT-05 Test Summary
    # =============================================================================
    print("\n" + "=" * 80)
    print("POINT-05 Test Summary")
    print("=" * 80)
    print(
        json.dumps(
            {
                **case_metadata,
                "tenant_id": client.tenant_id,
                "email": email,
                "checkout_session_id": session["id"],
                "available_points_before": available_before,
                "available_points_after_first": available_after_first,
                "available_points_after_replay": available_after_replay,
                "ledger_total_before": ledger_before_count,
                "ledger_total_after_first": ledger_after_first_count,
                "ledger_total_after_replay": ledger_after_replay_count,
                "history_total_before": history_before_count,
                "history_total_after_first": history_after_first_count,
                "history_total_after_replay": history_after_replay_count,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing POINT-05: replaying a successful points webhook stays idempotent.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
