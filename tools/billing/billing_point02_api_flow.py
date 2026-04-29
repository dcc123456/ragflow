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
"""

from __future__ import annotations

import json
import sys
import uuid
from pathlib import Path

import stripe  # type: ignore[reportMissingImports]

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from tools.billing.flow_common import FlowError
from tools.billing.points_common import (
    RAGFlowClient,
    complete_points_purchase,
    load_points_runtime_config,
    make_default_parser,
)
from tools.billing.points_case_common import get_checkout_session_amount, get_points_case_metadata


def run_flow(args) -> None:
    case_metadata = get_points_case_metadata("POINT-02")
    runtime = load_points_runtime_config()
    stripe.api_key = runtime["stripe_api_key"]
    stripe.api_version = runtime["stripe_api_version"]
    webhook_secret = runtime["webhook_secret"]
    points_per_unit = int(runtime["points_per_unit"])

    client = RAGFlowClient(args.base_url, args.version)
    client.wait_until_ready(args.ready_timeout_seconds)
    email = args.email or f"billing-point02-{uuid.uuid4().hex[:12]}@example.test"
    _, tenant_id = client.register_and_login(email, args.password)

    points_first = points_per_unit * 5
    points_second = points_per_unit * 10
    total_points = points_first + points_second

    before_balance = client.points_balance(tenant_id)
    before_ledger = client.points_ledger(tenant_id)
    before_history = client.spend_history()

    session_first = complete_points_purchase(client, tenant_id, points_first, webhook_secret)
    session_second = complete_points_purchase(client, tenant_id, points_second, webhook_secret)

    after_balance = client.points_balance(tenant_id)
    after_ledger = client.points_ledger(tenant_id)
    after_history = client.spend_history()

    available_before = int(before_balance.get("available_points") or 0)
    available_after = int(after_balance.get("available_points") or 0)
    if available_after != available_before + total_points:
        raise FlowError(
            f"available_points should increase by {total_points}, before={available_before}, after={available_after}"
        )

    ledger_before_count = int(before_ledger.get("total") or 0)
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

    history_before_count = len(before_history)
    history_after_count = len(after_history)
    if history_after_count != history_before_count + 2:
        raise FlowError(
            f"expected exactly two new billing history rows, before={history_before_count}, after={history_after_count}"
        )
    after_history_rows = [row for row in after_history if row.get("invoice_id") in {session_first["id"], session_second["id"]}]
    if len(after_history_rows) != 2:
        raise FlowError(f"expected exactly two history rows for the new sessions, got {after_history_rows}")
    amount_by_session = {row["invoice_id"]: float(row.get("amount", 0) or 0) for row in after_history_rows}
    if abs(amount_by_session.get(session_first["id"], -1) - get_checkout_session_amount(session_first)) > 1e-9:
        raise FlowError(f"unexpected amount for first purchase row: {after_history_rows}")
    if abs(amount_by_session.get(session_second["id"], -1) - get_checkout_session_amount(session_second)) > 1e-9:
        raise FlowError(f"unexpected amount for second purchase row: {after_history_rows}")
    if any(row.get("status") != "paid" for row in after_history_rows):
        raise FlowError(f"all points history rows should be paid, got {after_history_rows}")

    print(
        json.dumps(
            {
                **case_metadata,
                "tenant_id": tenant_id,
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
    try:
        run_flow(parser.parse_args())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
