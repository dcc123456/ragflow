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
    create_points_checkout_session,
    load_points_runtime_config,
    make_default_parser,
)
from tools.billing.points_case_common import get_points_case_metadata


def run_flow(args) -> None:
    case_metadata = get_points_case_metadata("POINT-04")
    runtime = load_points_runtime_config()
    stripe.api_key = runtime["stripe_api_key"]
    stripe.api_version = runtime["stripe_api_version"]
    points_per_unit = int(runtime["points_per_unit"])

    client = RAGFlowClient(args.base_url, args.version)
    client.wait_until_ready(args.ready_timeout_seconds)
    email = args.email or f"billing-point04-{uuid.uuid4().hex[:12]}@example.test"
    _, tenant_id = client.register_and_login(email, args.password)

    before_balance = client.points_balance(tenant_id)
    before_ledger = client.points_ledger(tenant_id)
    before_history = client.spend_history()
    before_overview = client.plan_overview()

    _, session = create_points_checkout_session(client, tenant_id, points_per_unit)
    expired = stripe.checkout.Session.expire(session["id"])
    expired_dict = expired.to_dict_recursive() if hasattr(expired, "to_dict_recursive") else dict(expired)
    expired_status = str(expired_dict.get("status") or "")
    if expired_status != "expired":
        raise FlowError(f"expected Stripe checkout session to expire, got {expired_dict}")

    after_balance = client.points_balance(tenant_id)
    after_ledger = client.points_ledger(tenant_id)
    after_history = client.spend_history()
    after_overview = client.plan_overview()

    if after_balance != before_balance:
        raise FlowError(f"canceled checkout should not change balance: before={before_balance}, after={after_balance}")
    if after_ledger.get("total") != before_ledger.get("total"):
        raise FlowError(f"canceled checkout should not change ledger count: before={before_ledger}, after={after_ledger}")
    if len(after_history) != len(before_history):
        raise FlowError(
            f"canceled checkout should not change billing history count: before={len(before_history)}, after={len(after_history)}"
        )
    if any(row.get("invoice_id") == session["id"] for row in after_history):
        raise FlowError(f"canceled checkout should not create a spend history row for session {session['id']}")
    if before_overview.get("payment_required", False):
        raise FlowError(f"fresh tenant should not start with payment_required=true: {before_overview}")
    if after_overview.get("payment_required", False):
        raise FlowError(f"canceled checkout should not trigger payment recovery banner state: {after_overview}")

    print(
        json.dumps(
            {
                **case_metadata,
                "tenant_id": tenant_id,
                "email": email,
                "checkout_session_id": session["id"],
                "stripe_session_status": expired_status,
                "balance_after": after_balance,
                "ledger_total_after": after_ledger.get("total"),
                "history_total_after": len(after_history),
                "payment_required_after": after_overview.get("payment_required"),
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing POINT-04: canceled points checkout should not mutate state.")
    try:
        run_flow(parser.parse_args())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
