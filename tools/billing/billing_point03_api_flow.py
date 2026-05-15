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
API-adjusted driver for POINT-03.
Tests: invalid points purchase inputs are rejected by API and do not mutate state.

Test flow:
- Step 1: Setup - Register user and initialize environment
- Step 2: Record baseline - Capture points balance, ledger, and spend history before testing
- Step 3: Test invalid inputs - Submit invalid points values and verify rejection
- Step 4: Verify results - Validate no state mutation occurred
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import uuid
from pathlib import Path

from tools.billing.billing_common import make_default_parser
from tools.billing.billing_client import create_client_with_type

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from tools.billing.billing_common import FlowError
from tools.billing.points_common import (
    PointsClient,
    list_recent_points_checkout_sessions,
)
from tools.billing.points_case_common import get_points_case_metadata


def run_flow(args: argparse.Namespace) -> None:
    """Execute POINT-03: invalid points purchase inputs are rejected by API and do not mutate state."""
    case_metadata = get_points_case_metadata("POINT-03")

    email = args.email or f"billing-point03-{uuid.uuid4().hex[:12]}@example.test"
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
    # Step 3: Test invalid inputs - Submit invalid points values and verify rejection
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 3: Test invalid inputs - Submit invalid points values and verify rejection")
    print("=" * 80)

    invalid_cases = [
        {"points": 0, "expected_message_part": "positive"},
        {"points": -1, "expected_message_part": "positive"},
        {"points": 0.1, "expected_message_part": "positive"},
        {"points": 0.9, "expected_message_part": "positive"},
        {"points": "abc", "expected_message_part": "integer"},
    ]

    started_at = int(time.time()) - 5
    results: list[dict[str, object]] = []
    for case in invalid_cases:
        result = client.points_checkout_raw(case["points"])
        payload = result["payload"]
        if result["status_code"] != 200:
            raise FlowError(f"invalid points request should return JSON body with 200 status, got {result}")
        if payload.get("code") == 0:
            raise FlowError(f"invalid points value {case['points']} unexpectedly succeeded: {payload}")
        if payload.get("data") not in (False, None):
            raise FlowError(f"invalid points value {case['points']} should not create checkout data: {payload}")
        message = str(payload.get("message") or "")
        if case["expected_message_part"] not in message:
            raise FlowError(
                f"invalid points value {case['points']} should mention {case['expected_message_part']!r}, got {payload}"
            )
        results.append({"points": case["points"], "message": message, "code": payload.get("code")})
        print(f"  Assert: Invalid input {case['points']} rejected with message: {message}")

    # =============================================================================
    # Step 4: Verify results - Validate no state mutation occurred
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 4: Verify results - Validate no state mutation occurred")
    print("=" * 80)

    after_balance = client.points_balance()
    after_ledger = client.points_ledger()
    after_history = client.spend_history()
    after_sessions = [
        session
        for session in list_recent_points_checkout_sessions(started_at)
        if (session.get("metadata") or {}).get("tenant_id") == client.tenant_id
    ]

    available_after = int(after_balance.get("available_points") or 0)
    held_after = int(after_balance.get("held_points") or 0)
    ledger_after_count = int(after_ledger.get("total") or 0)
    history_after_count = len(after_history)

    if available_after != available_before:
        raise FlowError(f"invalid points requests should not change balance: before={available_before}, after={available_after}")
    print(f"  Assert: Available points unchanged: {available_after}")

    if held_after != held_before:
        raise FlowError(f"invalid points requests should not change held points: before={held_before}, after={held_after}")
    print(f"  Assert: Held points unchanged: {held_after}")

    if ledger_after_count != ledger_before_count:
        raise FlowError(f"invalid points requests should not change ledger count: before={ledger_before_count}, after={ledger_after_count}")
    print(f"  Assert: Ledger total unchanged: {ledger_after_count}")

    if history_after_count != history_before_count:
        raise FlowError(
            f"invalid points requests should not change billing history count: before={history_before_count}, after={history_after_count}"
        )
    print(f"  Assert: History total unchanged: {history_after_count}")

    if after_sessions:
        raise FlowError(f"invalid points requests should not create Stripe checkout sessions, got {after_sessions}")
    print("  Assert: No Stripe checkout sessions created")

    # =============================================================================
    # POINT-03 Test Summary
    # =============================================================================
    print("\n" + "=" * 80)
    print("POINT-03 Test Summary")
    print("=" * 80)
    print(
        json.dumps(
            {
                **case_metadata,
                "tenant_id": client.tenant_id,
                "email": email,
                "invalid_case_results": results,
                "balance_after": after_balance,
                "ledger_total_after": ledger_after_count,
                "history_total_after": history_after_count,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing POINT-03: invalid inputs for points checkout.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
