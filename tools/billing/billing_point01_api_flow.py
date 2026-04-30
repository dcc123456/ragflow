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
    case_metadata = get_points_case_metadata("POINT-01")
    runtime = load_points_runtime_config()
    stripe.api_key = runtime["stripe_api_key"]
    stripe.api_version = runtime["stripe_api_version"]
    webhook_secret = runtime["webhook_secret"]
    points_per_unit = int(runtime["points_per_unit"])

    client = RAGFlowClient(args.base_url, args.version)
    client.wait_until_ready(args.ready_timeout_seconds)
    email = args.email or f"billing-point01-{uuid.uuid4().hex[:12]}@example.test"
    _, tenant_id = client.register_and_login(email, args.password)

    before_balance = client.points_balance(tenant_id)
    before_ledger = client.points_ledger(tenant_id)
    before_history = client.spend_history()

    session = complete_points_purchase(client, tenant_id, points_per_unit, webhook_secret)

    after_balance = client.points_balance(tenant_id)
    after_ledger = client.points_ledger(tenant_id)
    after_history = client.spend_history()

    available_before = int(before_balance.get("available_points") or 0)
    available_after = int(after_balance.get("available_points") or 0)
    if available_after != available_before + points_per_unit:
        raise FlowError(
            f"available_points should increase by {points_per_unit}, before={available_before}, after={available_after}"
        )

    held_before = int(before_balance.get("held_points") or 0)
    held_after = int(after_balance.get("held_points") or 0)
    if held_after != held_before:
        raise FlowError(f"held_points should not change on recharge, before={held_before}, after={held_after}")

    ledger_before_count = int(before_ledger.get("total") or 0)
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

    history_before_count = len(before_history)
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

    print(
        json.dumps(
            {
                **case_metadata,
                "tenant_id": tenant_id,
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
    try:
        run_flow(parser.parse_args())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
