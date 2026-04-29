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
"""

from __future__ import annotations

import json
import sys
import time
import uuid
from pathlib import Path

import stripe  # type: ignore[reportMissingImports]

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from tools.billing.flow_common import FlowError
from tools.billing.points_common import (
    RAGFlowClient,
    list_recent_points_checkout_sessions,
    load_points_runtime_config,
    make_default_parser,
)
from tools.billing.points_case_common import get_points_case_metadata


def run_flow(args) -> None:
    case_metadata = get_points_case_metadata("POINT-03")
    runtime = load_points_runtime_config()
    stripe.api_key = runtime["stripe_api_key"]
    stripe.api_version = runtime["stripe_api_version"]

    client = RAGFlowClient(args.base_url, args.version)
    client.wait_until_ready(args.ready_timeout_seconds)
    email = args.email or f"billing-point03-{uuid.uuid4().hex[:12]}@example.test"
    _, tenant_id = client.register_and_login(email, args.password)

    before_balance = client.points_balance(tenant_id)
    before_ledger = client.points_ledger(tenant_id)
    before_history = client.spend_history()

    invalid_cases = [
        {"points": 0, "expected_message_part": "positive"},
        {"points": -100, "expected_message_part": "positive"},
        {"points": 50, "expected_message_part": "multiple of 100"},
        {"points": 100.5, "expected_message_part": "integer"},
        {"points": "abc", "expected_message_part": "integer"},
    ]

    started_at = int(time.time()) - 5
    results: list[dict[str, object]] = []
    for case in invalid_cases:
        result = client.points_checkout_raw(tenant_id, case["points"])
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

    after_balance = client.points_balance(tenant_id)
    after_ledger = client.points_ledger(tenant_id)
    after_history = client.spend_history()
    after_sessions = [
        session
        for session in list_recent_points_checkout_sessions(started_at)
        if (session.get("metadata") or {}).get("tenant_id") == tenant_id
    ]

    if after_balance != before_balance:
        raise FlowError(f"invalid points requests should not change balance: before={before_balance}, after={after_balance}")
    if after_ledger.get("total") != before_ledger.get("total"):
        raise FlowError(f"invalid points requests should not change ledger count: before={before_ledger}, after={after_ledger}")
    if len(after_history) != len(before_history):
        raise FlowError(
            f"invalid points requests should not change billing history count: before={len(before_history)}, after={len(after_history)}"
        )
    if after_sessions:
        raise FlowError(f"invalid points requests should not create Stripe checkout sessions, got {after_sessions}")

    print(
        json.dumps(
            {
                **case_metadata,
                "tenant_id": tenant_id,
                "email": email,
                "invalid_case_results": results,
                "balance_after": after_balance,
                "ledger_total_after": after_ledger.get("total"),
                "history_total_after": len(after_history),
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing POINT-03: invalid inputs for points checkout.")
    try:
        run_flow(parser.parse_args())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
