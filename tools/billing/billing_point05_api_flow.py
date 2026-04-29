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
    build_points_checkout_completed_event,
    create_points_checkout_session,
    load_points_runtime_config,
    make_default_parser,
)
from tools.billing.points_case_common import get_points_case_metadata


def run_flow(args) -> None:
    case_metadata = get_points_case_metadata("POINT-05")
    runtime = load_points_runtime_config()
    stripe.api_key = runtime["stripe_api_key"]
    stripe.api_version = runtime["stripe_api_version"]
    webhook_secret = runtime["webhook_secret"]
    points_per_unit = int(runtime["points_per_unit"])

    client = RAGFlowClient(args.base_url, args.version)
    client.wait_until_ready(args.ready_timeout_seconds)
    email = args.email or f"billing-point05-{uuid.uuid4().hex[:12]}@example.test"
    _, tenant_id = client.register_and_login(email, args.password)

    before_balance = client.points_balance(tenant_id)
    before_ledger = client.points_ledger(tenant_id)
    before_history = client.spend_history()

    _, session = create_points_checkout_session(client, tenant_id, points_per_unit)
    event = build_points_checkout_completed_event(
        event_id=f"evt_manual_points_replay_{uuid.uuid4().hex[:20]}",
        session=session,
        points=points_per_unit,
    )

    client.post_signed_webhook(event, webhook_secret)
    after_first_balance = client.points_balance(tenant_id)
    after_first_ledger = client.points_ledger(tenant_id)
    after_first_history = client.spend_history()

    client.post_signed_webhook(event, webhook_secret)
    after_replay_balance = client.points_balance(tenant_id)
    after_replay_ledger = client.points_ledger(tenant_id)
    after_replay_history = client.spend_history()

    available_before = int(before_balance.get("available_points") or 0)
    available_after_first = int(after_first_balance.get("available_points") or 0)
    available_after_replay = int(after_replay_balance.get("available_points") or 0)
    if available_after_first != available_before + points_per_unit:
        raise FlowError(
            f"first webhook should credit {points_per_unit} points, before={available_before}, after_first={available_after_first}"
        )
    if available_after_replay != available_after_first:
        raise FlowError(
            f"replayed webhook should not credit again, after_first={available_after_first}, after_replay={available_after_replay}"
        )

    ledger_before_count = int(before_ledger.get("total") or 0)
    ledger_after_first_count = int(after_first_ledger.get("total") or 0)
    ledger_after_replay_count = int(after_replay_ledger.get("total") or 0)
    if ledger_after_first_count != ledger_before_count + 1:
        raise FlowError(
            f"first webhook should add one ledger row, before={ledger_before_count}, after_first={ledger_after_first_count}"
        )
    if ledger_after_replay_count != ledger_after_first_count:
        raise FlowError(
            f"replayed webhook should not add another ledger row, after_first={ledger_after_first_count}, after_replay={ledger_after_replay_count}"
        )
    replay_ledger_rows = [
        row
        for row in (after_replay_ledger.get("items") or [])
        if (row.get("metadata") or {}).get("session_id") == session["id"]
    ]
    if len(replay_ledger_rows) != 1:
        raise FlowError(f"expected exactly one ledger row for replayed session {session['id']}, got {replay_ledger_rows}")

    history_before_count = len(before_history)
    history_after_first_count = len(after_first_history)
    history_after_replay_count = len(after_replay_history)
    if history_after_first_count != history_before_count + 1:
        raise FlowError(
            f"first webhook should add one history row, before={history_before_count}, after_first={history_after_first_count}"
        )
    if history_after_replay_count != history_after_first_count:
        raise FlowError(
            f"replayed webhook should not add another history row, after_first={history_after_first_count}, after_replay={history_after_replay_count}"
        )
    replay_history_rows = [row for row in after_replay_history if row.get("invoice_id") == session["id"]]
    if len(replay_history_rows) != 1:
        raise FlowError(f"expected exactly one history row for replayed session {session['id']}, got {replay_history_rows}")

    print(
        json.dumps(
            {
                **case_metadata,
                "tenant_id": tenant_id,
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
    try:
        run_flow(parser.parse_args())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
