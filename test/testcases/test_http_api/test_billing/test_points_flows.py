#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
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
Pytest wrapper for POINT-03: invalid points purchase inputs are rejected by API and do not mutate state.

This test validates that the billing API correctly rejects invalid points values
and that no state mutation occurs on the tenant's points account.

Case: POINT-03 (adjusted for API-only testing without hosted Stripe Checkout)
"""

from __future__ import annotations

import time
import uuid

import pytest

from libs.billing.points_common import (
    PointsClient,
    list_recent_points_checkout_sessions,
    stripe_dict,
)


@pytest.mark.billing
def test_point_03_invalid_inputs_rejected(points_client: PointsClient):
    """POINT-03: invalid points purchase inputs are rejected by API and do not mutate state."""

    # -------------------------------------------------------------------------
    # Step 2: Record baseline - Capture pre-test state
    # -------------------------------------------------------------------------
    before_balance = points_client.points_balance()
    before_ledger = points_client.points_ledger()
    before_history = points_client.spend_history()

    available_before = int(before_balance.get("available_points") or 0)
    held_before = int(before_balance.get("held_points") or 0)
    ledger_before_count = int(before_ledger.get("total") or 0)
    history_before_count = len(before_history)

    # -------------------------------------------------------------------------
    # Step 3: Test invalid inputs - Submit invalid points values and verify rejection
    # -------------------------------------------------------------------------
    invalid_cases = [
        {"points": 0, "expected_message_parts": ("greater than 0", "positive")},
        {"points": -1, "expected_message_parts": ("greater than 0", "positive")},
        {"points": 0.1, "expected_message_parts": ("integer",)},
        {"points": 0.9, "expected_message_parts": ("integer",)},
        {"points": "abc", "expected_message_parts": ("integer",)},
    ]

    started_at = int(time.time()) - 5
    results: list[dict[str, object]] = []
    for case in invalid_cases:
        result = points_client.points_checkout_raw(case["points"])
        payload = result["payload"]
        if result["status_code"] != 200:
            pytest.fail(f"invalid points request should return JSON body with 200 status, got {result}")
        if payload.get("code") == 0:
            pytest.fail(f"invalid points value {case['points']} unexpectedly succeeded: {payload}")
        if payload.get("data") not in (False, None):
            pytest.fail(f"invalid points value {case['points']} should not create checkout data: {payload}")
        message = str(payload.get("message") or "")
        expected_message_parts = case["expected_message_parts"]
        if not any(part in message for part in expected_message_parts):
            pytest.fail(f"invalid points value {case['points']} should mention one of {expected_message_parts!r}, got {payload}")
        results.append({"points": case["points"], "message": message, "code": payload.get("code")})

    # -------------------------------------------------------------------------
    # Step 4: Verify results - Validate no state mutation occurred
    # -------------------------------------------------------------------------
    after_balance = points_client.points_balance()
    after_ledger = points_client.points_ledger()
    after_history = points_client.spend_history()
    after_sessions = [session for session in list_recent_points_checkout_sessions(started_at) if (session.get("metadata") or {}).get("tenant_id") == points_client.tenant_id]

    available_after = int(after_balance.get("available_points") or 0)
    held_after = int(after_balance.get("held_points") or 0)
    ledger_after_count = int(after_ledger.get("total") or 0)
    history_after_count = len(after_history)

    assert available_after == available_before, f"invalid points requests should not change balance: before={available_before}, after={available_after}"
    assert held_after == held_before, f"invalid points requests should not change held points: before={held_before}, after={held_after}"
    assert ledger_after_count == ledger_before_count, f"invalid points requests should not change ledger count: before={ledger_before_count}, after={ledger_after_count}"
    assert history_after_count == history_before_count, f"invalid points requests should not change billing history count: before={history_before_count}, after={history_after_count}"
    assert not after_sessions, f"invalid points requests should not create Stripe checkout sessions, got {after_sessions}"


# -----------------------------------------------------------------------------
# POINT-04: canceled checkout does not create credits or recovery state
# -----------------------------------------------------------------------------


@pytest.mark.billing
def test_point_04_canceled_checkout_no_state_mutation(points_client: PointsClient):
    """POINT-04: a canceled or abandoned points checkout does not create credits or recovery state."""
    # load_points_runtime_config requires Stripe API key to be set
    import stripe
    from libs.billing.points_common import load_points_runtime_config

    runtime = load_points_runtime_config()
    points_per_unit = int(runtime["points_per_unit"])

    # Record baseline
    before_balance = points_client.points_balance()
    before_ledger = points_client.points_ledger()
    before_history = points_client.spend_history()
    before_overview = points_client.plan_overview()

    available_before = int(before_balance.get("available_points") or 0)
    held_before = int(before_balance.get("held_points") or 0)
    ledger_before_count = int(before_ledger.get("total") or 0)
    history_before_count = len(before_history)
    payment_required_before = before_overview.get("payment_required", False)

    # Create and expire checkout session as automation proxy for cancellation
    points_to_buy = 100
    _, session = points_client.create_points_checkout_session(points_to_buy, points_per_unit)

    expired = stripe.checkout.Session.expire(session["id"])
    expired_dict = stripe_dict(expired)
    expired_status = str(expired_dict.get("status") or "")
    assert expired_status == "expired", f"expected Stripe checkout session to expire, got {expired_dict}"

    # Verify no state mutation
    after_balance = points_client.points_balance()
    after_ledger = points_client.points_ledger()
    after_history = points_client.spend_history()
    after_overview = points_client.plan_overview()

    available_after = int(after_balance.get("available_points") or 0)
    held_after = int(after_balance.get("held_points") or 0)
    ledger_after_count = int(after_ledger.get("total") or 0)
    history_after_count = len(after_history)
    payment_required_after = after_overview.get("payment_required", False)

    assert available_after == available_before, f"canceled checkout should not change balance: before={available_before}, after={available_after}"
    assert held_after == held_before, f"canceled checkout should not change held points: before={held_before}, after={held_after}"
    assert ledger_after_count == ledger_before_count, f"canceled checkout should not change ledger count: before={ledger_before_count}, after={ledger_after_count}"
    assert history_after_count == history_before_count, f"canceled checkout should not change billing history count: before={history_before_count}, after={history_after_count}"
    assert not any(row.get("invoice_id") == session["id"] for row in after_history), f"canceled checkout should not create a spend history row for session {session['id']}"
    assert not payment_required_before, f"fresh tenant should not start with payment_required=true: {before_overview}"
    assert not payment_required_after, f"canceled checkout should not trigger payment recovery banner state: {after_overview}"


# -----------------------------------------------------------------------------
# POINT-05: webhook replay is idempotent
# -----------------------------------------------------------------------------


@pytest.mark.billing
def test_point_05_webhook_replay_is_idempotent(points_client: PointsClient):
    """POINT-05: replaying the same successful points webhook remains idempotent."""

    import stripe

    from libs.billing.points_common import build_points_checkout_completed_event, load_points_runtime_config

    runtime = load_points_runtime_config()
    points_per_unit = int(runtime["points_per_unit"])

    # Record baseline
    before_balance = points_client.points_balance()
    before_ledger = points_client.points_ledger()
    before_history = points_client.spend_history()

    available_before = int(before_balance.get("available_points") or 0)
    ledger_before_count = int(before_ledger.get("total") or 0)
    history_before_count = len(before_history)

    # Create checkout session and post signed webhook
    pi = stripe.PaymentIntent.create(
        amount=points_per_unit,
        currency="USD",
        customer=points_client.customer_id,
        description="Points recharge for idempotency test",
        metadata={"source": "point05_test"},
    )
    payment_intent_id = pi.id

    _, session = points_client.create_points_checkout_session(1, points_per_unit)
    event = build_points_checkout_completed_event(
        event_id=f"evt_manual_points_replay_{uuid.uuid4().hex[:20]}",
        session=session,
        points=points_per_unit,
        payment_intent_id=payment_intent_id,
    )
    points_client.post_signed_webhook(event)

    # Verify first webhook credited points
    after_first_balance = points_client.points_balance()
    after_first_ledger = points_client.points_ledger()
    after_first_history = points_client.spend_history()

    available_after_first = int(after_first_balance.get("available_points") or 0)
    ledger_after_first_count = int(after_first_ledger.get("total") or 0)
    history_after_first_count = len(after_first_history)

    assert available_after_first == available_before + points_per_unit, f"first webhook should credit {points_per_unit} points, before={available_before}, after_first={available_after_first}"
    assert ledger_after_first_count == ledger_before_count + 1, f"first webhook should add one ledger row, before={ledger_before_count}, after_first={ledger_after_first_count}"
    assert history_after_first_count == history_before_count + 1, f"first webhook should add one history row, before={history_before_count}, after_first={history_after_first_count}"

    # Replay the exact same webhook event
    points_client.post_signed_webhook(event)

    # Verify no duplicate state mutation
    after_replay_balance = points_client.points_balance()
    after_replay_ledger = points_client.points_ledger()
    after_replay_history = points_client.spend_history()

    available_after_replay = int(after_replay_balance.get("available_points") or 0)
    ledger_after_replay_count = int(after_replay_ledger.get("total") or 0)
    history_after_replay_count = len(after_replay_history)

    assert available_after_replay == available_after_first, f"replayed webhook should not credit again, after_first={available_after_first}, after_replay={available_after_replay}"
    assert ledger_after_replay_count == ledger_after_first_count, (
        f"replayed webhook should not add another ledger row, after_first={ledger_after_first_count}, after_replay={ledger_after_replay_count}"
    )
    assert history_after_replay_count == history_after_first_count, (
        f"replayed webhook should not add another history row, after_first={history_after_first_count}, after_replay={history_after_replay_count}"
    )

    # Verify only one ledger/history row for this session
    replay_ledger_rows = [row for row in (after_replay_ledger.get("items") or []) if (row.get("metadata") or {}).get("session_id") == session["id"]]
    assert len(replay_ledger_rows) == 1, f"expected exactly one ledger row for replayed session {session['id']}, got {replay_ledger_rows}"
    replay_history_rows = [row for row in after_replay_history if row.get("invoice_id") == session["id"]]
    assert len(replay_history_rows) == 1, f"expected exactly one history row for replayed session {session['id']}, got {replay_history_rows}"
