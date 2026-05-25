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
from __future__ import annotations

import sys
import time
import uuid
from pathlib import Path
from typing import Any

import stripe

from libs.billing.billing_common import BillingClient, FlowError, env, load_stripe_test_runtime_config, stripe_dict

PROJECT_ROOT = Path(__file__).resolve().parents[4]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

POINTS_SUCCESS_URL = "http://127.0.0.1:9380/billing/points?price-pay-status=success"
POINTS_CANCEL_URL = "http://127.0.0.1:9380/billing/points?price-pay-status=cancel"


def parse_positive_int_env(name: str, fallback: Any) -> int:
    raw_value = env(name, str(fallback))
    try:
        parsed = int(raw_value)
    except (TypeError, ValueError) as exc:
        raise FlowError(f"{name} must be a positive integer, got {raw_value!r}") from exc
    if parsed <= 0:
        raise FlowError(f"{name} must be a positive integer, got {raw_value!r}")
    return parsed


def load_points_runtime_config() -> dict[str, Any]:
    runtime = load_stripe_test_runtime_config(require_test_mode_message="Points automation requires a Stripe test-mode secret key")
    billing_config = runtime["billing_config"]
    recharge_config = billing_config.get("points_recharge") or {}
    if not isinstance(recharge_config, dict):
        raise FlowError("billing.points_recharge must be a map in service_conf.yaml")
    price_id = env("BILLING_POINTS_PRICE_ID", str(recharge_config.get("price_id") or ""))
    if not price_id or price_id == "price_xxx":
        raise FlowError("billing.points_recharge.price_id is not configured in conf/service_conf.yaml")
    points_per_unit = parse_positive_int_env("BILLING_POINTS_PER_UNIT", recharge_config.get("points_per_unit") or 100)

    return {
        **runtime,
        "points_price_id": price_id,
        "points_per_unit": points_per_unit,
    }

def cal_available_points(points_balance: dict[str, Any]) -> dict[str, Any]:
    """
    Add available_points field to points_balance dict.

    Input format (from billing_points_balance API):
        {
            "plan_points": {
                "used": <int>,
                "limit": <int>,
                "unit": "points",
            },
            "addon_points": {
                "used": <int>,
                "limit": <int>,
                "unit": "points",
            },
        }

    Output: Same dict with added "available_points" field:
        {
            "plan_points": {...},
            "addon_points": {...},
            "available_points": <int>,  # plan_remaining + addon_remaining
        }

    Calculation (matching frontend logic):
        plan_remaining = max(0, plan_points.limit - plan_points.used)
        addon_remaining = max(0, addon_points.limit - addon_points.used)
        available_points = plan_remaining + addon_remaining
    """
    plan_points = points_balance.get("plan_points", {})
    addon_points = points_balance.get("addon_points", {})

    plan_limit = plan_points.get("limit", 0)
    plan_used = plan_points.get("used", 0)
    addon_limit = addon_points.get("limit", 0)
    addon_used = addon_points.get("used", 0)

    plan_remaining = max(0, plan_limit - plan_used)
    addon_remaining = max(0, addon_limit - addon_used)
    available_points = plan_remaining + addon_remaining

    points_balance["available_points"] = available_points
    return points_balance

class PointsClient(BillingClient):
    """HTTP client for RAGFlow billing APIs used by points flows."""

    def points_balance(self) -> dict[str, Any]:
        response = self.session.get(
            self.billing_url(f"/points/balance?tenant_id={self.tenant_id}"),
            headers=self.headers(auth=True),
            timeout=60,
        )
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"GET /billing/points/balance returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"GET /billing/points/balance failed status={response.status_code}: {payload}")
        points = payload["data"]
        return cal_available_points(points)

    def points_ledger(self, page: int = 1, page_size: int = 100) -> dict[str, Any]:
        response = self.session.get(
            self.billing_url(f"/points/ledger?tenant_id={self.tenant_id}&page={page}&page_size={page_size}"),
            headers=self.headers(auth=True),
            timeout=60,
        )
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"GET /billing/points/ledger returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"GET /billing/points/ledger failed status={response.status_code}: {payload}")
        return payload["data"]

    def points_checkout(self, points: Any) -> dict[str, Any]:
        response = self.session.post(
            self.billing_url("/points/checkout"),
            headers=self.headers(auth=True),
            json={
                "tenant_id": self.tenant_id,
                "quantity": points,
                "session_success_url": POINTS_SUCCESS_URL,
                "session_cancel_url": POINTS_CANCEL_URL,
            },
            timeout=60,
        )
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"POST /billing/points/checkout returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"POST /billing/points/checkout failed status={response.status_code}: {payload}")
        return payload["data"]

    def points_checkout_raw(self, points: Any) -> dict[str, Any]:
        response = self.session.post(
            self.billing_url("/points/checkout"),
            headers=self.headers(auth=True),
            json={
                "tenant_id": self.tenant_id,
                "quantity": points,
                "session_success_url": POINTS_SUCCESS_URL,
                "session_cancel_url": POINTS_CANCEL_URL,
            },
            timeout=60,
        )
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"POST /billing/points/checkout returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        return {"status_code": response.status_code, "payload": payload}

    def create_points_checkout_session(self, unit_amount: int, points_per_unit: int) -> tuple[dict[str, Any], dict[str, Any]]:
        started_at = int(time.time()) - 5
        checkout = self.points_checkout(unit_amount)
        if not checkout.get("checkout_url"):
            raise FlowError(f"expected checkout_url for points purchase, got: {checkout}")
        sessions = list_recent_points_checkout_sessions(started_at)
        session = select_points_checkout_session(sessions, tenant_id=self.tenant_id, unit_amount=unit_amount, points_per_unit=points_per_unit)
        return checkout, session


    def complete_points_purchase_via_synthetic_webhook(
            self,
            points_to_buy: int,
            points_per_unit: int,
            event_id: str | None = None,
            payment_intent_id: str | None = None,
    ) -> dict[str, Any]:
        unit_amount = int(points_to_buy / points_per_unit)
        _, session = self.create_points_checkout_session(unit_amount, points_per_unit)

        completed_event = build_points_checkout_completed_event(
            event_id=event_id or f"evt_manual_points_{uuid.uuid4().hex[:20]}",
            session=session,
            points=points_to_buy,
            payment_intent_id=payment_intent_id,
        )
        self.post_signed_webhook(completed_event)
        return session

    def complete_points_purchase(
            self,
            points_to_buy: int,
            points_per_unit: int,
            event_id: str | None = None,
            payment_intent_id: str | None = None,
    ) -> dict[str, Any]:
        """
        Legacy compatibility wrapper.

        Points automation still uses a synthetic checkout.session.completed event
        because these scripts do not drive the hosted Stripe Checkout UI.
        """
        return self.complete_points_purchase_via_synthetic_webhook(
            points_to_buy,
            points_per_unit,
            event_id=event_id,
            payment_intent_id=payment_intent_id,
        )


def build_points_checkout_completed_event(
    *,
    event_id: str,
    session: dict[str, Any],
    points: int,
    payment_intent_id: str | None = None,
) -> dict[str, Any]:
    created = int(session.get("created") or time.time())
    expires_at = int(session.get("expires_at") or (created + 86400))
    amount_total = int(session.get("amount_total") or 0)
    if amount_total <= 0:
        raise FlowError(f"points checkout session missing amount_total: {session}")
    return {
        "id": event_id,
        "object": "event",
        "type": "checkout.session.completed",
        "created": created,
        "data": {
            "object": {
                "id": str(session.get("id") or ""),
                "object": "checkout.session",
                "customer": session.get("customer"),
                "payment_intent": payment_intent_id or session.get("payment_intent") or f"pi_manual_{uuid.uuid4().hex[:24]}",
                "subscription": None,
                "payment_status": "paid",
                "status": "complete",
                "mode": "payment",
                "currency": str(session.get("currency") or "usd"),
                "amount_subtotal": amount_total,
                "amount_total": amount_total,
                "invoice": "",
                "metadata": {
                    "payment_type": "points_recharge",
                    "tenant_id": str((session.get("metadata") or {}).get("tenant_id") or ""),
                    "points_amount": str(points),
                },
                "created": created,
                "expires_at": expires_at,
                "line_items": {
                    "object": "list",
                    "has_more": False,
                    "data": [],
                },
            }
        },
    }


def list_recent_points_checkout_sessions(created_gte: int) -> list[dict[str, Any]]:
    sessions = stripe.checkout.Session.list(limit=50)
    filtered: list[dict[str, Any]] = []
    for session in sessions.data:
        session_dict = stripe_dict(session)
        metadata = session_dict.get("metadata") or {}
        if int(session_dict.get("created") or 0) < created_gte:
            continue
        if session_dict.get("mode") != "payment":
            continue
        if metadata.get("payment_type") != "points_recharge":
            continue
        filtered.append(session_dict)
    filtered.sort(key=lambda item: (item.get("created", 0), item.get("id", "")), reverse=True)
    return filtered


def select_points_checkout_session(
    sessions: list[dict[str, Any]],
    tenant_id: str,
    unit_amount: int,
    points_per_unit: int,
) -> dict[str, Any]:
    points_expected = unit_amount * points_per_unit
    for session in sessions:
        metadata = session.get("metadata") or {}
        if metadata.get("tenant_id") != tenant_id:
            continue

        session_points = int(metadata.get("points_amount") or 0)
        if session_points == points_expected:
            return session
    raise FlowError(f"expected a matching points checkout session for tenant {tenant_id}, points {points_expected}")


_POINTS_CASE_METADATA = {
    "POINT-01": {
        "case_adjusted": True,
        "case_adjustment_notes": [
            "Uses a synthetic signed checkout.session.completed webhook after creating the Checkout Session instead of driving the hosted Stripe Checkout UI.",
        ],
    },
    "POINT-02": {
        "case_adjusted": True,
        "case_adjustment_notes": [
            "Uses synthetic signed checkout.session.completed webhooks for both purchases instead of completing two hosted Stripe Checkout UI sessions.",
        ],
    },
    "POINT-03": {
        "case_adjusted": True,
        "case_adjustment_notes": [
            "Covers API-side rejection and no-mutation guarantees only; frontend validation still needs separate manual verification.",
        ],
    },
    "POINT-04": {
        "case_adjusted": True,
        "case_adjustment_notes": [
            "Uses Stripe Checkout Session expire as the automation proxy for a user-cancelled or abandoned Checkout.",
        ],
    },
    "POINT-05": {
        "case_adjusted": True,
        "case_adjustment_notes": [
            "Replays the same synthetic signed checkout.session.completed payload twice instead of using Stripe dashboard or CLI replay tooling.",
        ],
    },
}


def get_points_case_metadata(case_id: str) -> dict[str, Any]:
    metadata = _POINTS_CASE_METADATA.get(case_id)
    if metadata is None:
        raise ValueError(f"Unknown points case id: {case_id}")
    return {
        "case_id": case_id,
        "case_adjusted": metadata["case_adjusted"],
        "case_adjustment_notes": list(metadata["case_adjustment_notes"]),
    }


def get_checkout_session_amount(session: dict[str, Any]) -> float:
    raw_amount = session.get("amount_total")
    if isinstance(raw_amount, bool):
        raise FlowError(f"checkout session amount_total is invalid: {raw_amount!r}")
    try:
        amount_cents = int(raw_amount)
    except (TypeError, ValueError) as exc:
        raise FlowError(f"checkout session amount_total is invalid: {raw_amount!r}") from exc
    if amount_cents < 0:
        raise FlowError(f"checkout session amount_total must be non-negative, got {amount_cents}")
    return amount_cents / 100
