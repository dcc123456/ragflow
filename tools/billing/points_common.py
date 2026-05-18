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

import argparse
import hashlib
import hmac
import os
import sys
import time
import uuid
from pathlib import Path
from typing import Any

import requests
import stripe  # type: ignore[reportMissingImports]
import yaml

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from api.utils.crypt import crypt  # noqa: E402
from tools.billing.flow_common import (  # noqa: E402
    FlowError,
    ensure_webhook_delivery_success,
    json_dumps_compact,
    load_persisted_webhook_secret,
)


def env(name: str, fallback: str = "") -> str:
    return (os.getenv(name) or fallback).strip()


def parse_positive_int_env(name: str, fallback: Any) -> int:
    raw_value = env(name, str(fallback))
    try:
        parsed = int(raw_value)
    except (TypeError, ValueError) as exc:
        raise FlowError(f"{name} must be a positive integer, got {raw_value!r}") from exc
    if parsed <= 0:
        raise FlowError(f"{name} must be a positive integer, got {raw_value!r}")
    return parsed


def stripe_dict(obj: Any) -> dict[str, Any]:
    if hasattr(obj, "to_dict_recursive"):
        return obj.to_dict_recursive()
    if isinstance(obj, dict):
        return obj
    return dict(obj)


def load_billing_config() -> dict[str, Any]:
    config_path = Path(env("RAGFLOW_SERVICE_CONF", str(PROJECT_ROOT / "conf" / "service_conf.yaml")))
    if not config_path.exists():
        fallback_path = PROJECT_ROOT / "service_conf.yaml"
        if fallback_path.exists():
            config_path = fallback_path
    if not config_path.exists():
        raise FlowError(f"service config not found: {config_path}")
    with config_path.open("r", encoding="utf-8") as f:
        config = yaml.safe_load(f) or {}
    billing_config = config.get("billing") or {}
    if not isinstance(billing_config, dict):
        raise FlowError(f"billing config is not a map in {config_path}")
    return billing_config


def load_points_runtime_config() -> dict[str, Any]:
    billing_config = load_billing_config()
    stripe_api_key = env("BILLING_STRIPE_API_KEY", env("STRIPE_API_KEY", str(billing_config.get("stripe_api_key") or "")))
    stripe_api_version = str(billing_config.get("stripe_api_version") or "2026-02-25.clover")
    stripe_api_version_override = env("STRIPE_API_VERSION")
    if stripe_api_version_override and stripe_api_version_override != stripe_api_version:
        raise FlowError(
            f"STRIPE_API_VERSION={stripe_api_version_override} does not match service_conf.yaml={stripe_api_version}"
        )
    if not stripe_api_key:
        raise FlowError("BILLING_STRIPE_API_KEY or STRIPE_API_KEY is required")
    if not stripe_api_key.startswith("sk_test_"):
        raise FlowError("Points automation requires a Stripe test-mode secret key")

    recharge_config = billing_config.get("points_recharge") or {}
    if not isinstance(recharge_config, dict):
        raise FlowError("billing.points_recharge must be a map in service_conf.yaml")
    price_id = env("BILLING_POINTS_PRICE_ID", str(recharge_config.get("price_id") or ""))
    if not price_id or price_id == "price_xxx":
        raise FlowError("BILLING_POINTS_PRICE_ID or billing.points_recharge.price_id is not configured")
    points_per_unit = parse_positive_int_env("BILLING_POINTS_PER_UNIT", recharge_config.get("points_per_unit") or 100)

    webhook_secret = env("BILLING_WEBHOOK_SECRET", env("STRIPE_WEBHOOK_SECRET"))
    if not webhook_secret:
        webhook_secret = load_persisted_webhook_secret()

    return {
        "billing_config": billing_config,
        "stripe_api_key": stripe_api_key,
        "stripe_api_version": stripe_api_version,
        "webhook_secret": webhook_secret,
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

class RAGFlowClient:
    """HTTP client for RAGFlow billing APIs used by points flows."""

    def __init__(self, base_url: str, version: str):
        self.base_url = base_url.rstrip("/")
        self.version = version.strip("/")
        self.session = requests.Session()
        self.session.trust_env = False
        self.auth_header = ""

    def url(self, path: str) -> str:
        return f"{self.base_url}/{self.version}/{path.lstrip('/')}"

    def headers(self, *, auth: bool = True) -> dict[str, str]:
        headers: dict[str, str] = {}
        if auth and self.auth_header:
            headers["Authorization"] = self.auth_header
        return headers

    def request_json(self, method: str, path: str, *, auth: bool = True, **kwargs) -> dict[str, Any]:
        response = self.session.request(method, self.url(path), headers=self.headers(auth=auth), timeout=60, **kwargs)
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"{method} {path} returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"{method} {path} failed status={response.status_code}: {payload}")
        return payload

    def wait_until_ready(self, timeout_seconds: int) -> None:
        deadline = time.time() + timeout_seconds
        last_error = ""
        while time.time() < deadline:
            try:
                response = self.session.get(self.url("/billing/status"), headers=self.headers(auth=False), timeout=10)
                if response.status_code == 200 and response.headers.get("content-type", "").startswith("application/json"):
                    response.json()
                    return
                last_error = f"status={response.status_code} body={response.text[:200]}"
            except Exception as exc:
                last_error = str(exc)
            time.sleep(2)
        raise FlowError(f"RAGFlow API did not become ready: {last_error}")

    def register_and_login(self, email: str, password: str) -> tuple[str, str]:
        encrypted_password = crypt(password)
        register_payload = {
            "email": email,
            "nickname": email.split("@", 1)[0],
            "password": encrypted_password,
        }
        register_response = self.session.post(
            self.url("/user/register"),
            headers=self.headers(auth=False),
            json=register_payload,
            timeout=60,
        )
        try:
            register_data = register_response.json()
        except ValueError as exc:
            raise FlowError(
                f"register returned non-JSON status={register_response.status_code}: {register_response.text[:500]}"
            ) from exc
        if register_data.get("code") != 0 and "has already registered" not in (register_data.get("message") or ""):
            raise FlowError(f"register failed: {register_data}")

        login_response = self.session.post(
            self.url("/user/login"),
            headers=self.headers(auth=False),
            json={"email": email, "password": encrypted_password},
            timeout=60,
        )
        try:
            login_data = login_response.json()
        except ValueError as exc:
            raise FlowError(
                f"login returned non-JSON status={login_response.status_code}: {login_response.text[:500]}"
            ) from exc
        if login_data.get("code") != 0:
            raise FlowError(f"login failed: {login_data}")
        self.auth_header = login_response.headers.get("Authorization", "")
        if not self.auth_header:
            raise FlowError("login succeeded without Authorization header")
        data = login_data.get("data") or {}
        user_id = data.get("id") or data.get("user_id") or ""
        tenant_id = data.get("tenant_id") or data.get("tenantId") or user_id
        if not user_id or not tenant_id:
            raise FlowError(f"login response missing ids: {login_data}")
        return user_id, tenant_id

    def plan_overview(self) -> dict[str, Any]:
        return self.request_json("GET", "/billing/plan_overview")["data"]

    def spend_history(self) -> list[dict[str, Any]]:
        return self.request_json("GET", "/billing/spend_overview")["data"].get("items", [])

    def points_balance(self, tenant_id: str) -> dict[str, Any]:
        points = self.request_json("GET", f"/billing/points/balance?tenant_id={tenant_id}")["data"]
        return cal_available_points(points)

    def points_ledger(self, tenant_id: str, *, page: int = 1, page_size: int = 100) -> dict[str, Any]:
        return self.request_json(
            "GET",
            f"/billing/points/ledger?tenant_id={tenant_id}&page={page}&page_size={page_size}",
        )["data"]

    def points_checkout(self, tenant_id: str, points: Any) -> dict[str, Any]:
        return self.request_json("POST", "/billing/points/checkout", json={"tenant_id": tenant_id, "quantity": points})["data"]

    def points_checkout_raw(self, tenant_id: str, points: Any) -> dict[str, Any]:
        response = self.session.post(
            self.url("/billing/points/checkout"),
            headers=self.headers(auth=True),
            json={"tenant_id": tenant_id, "quantity": points},
            timeout=60,
        )
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"POST /billing/points/checkout returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        return {"status_code": response.status_code, "payload": payload}

    def post_signed_webhook(self, event: dict[str, Any], webhook_secret: str) -> None:
        payload = json_dumps_compact(event)
        timestamp = str(int(time.time()))
        signed_payload = f"{timestamp}.{payload}".encode("utf-8")
        signature = hmac.new(webhook_secret.encode("utf-8"), signed_payload, hashlib.sha256).hexdigest()
        headers = {
            "Stripe-Signature": f"t={timestamp},v1={signature}",
            "Content-Type": "application/json",
        }
        response = self.session.post(self.url("/billing/webhook"), data=payload, headers=headers, timeout=60)
        ensure_webhook_delivery_success(response, str(event.get("type") or "unknown"))


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
    *,
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


def create_points_checkout_session(client: RAGFlowClient, tenant_id: str, unit_amount: int, points_per_unit: int) -> tuple[dict[str, Any], dict[str, Any]]:
    started_at = int(time.time()) - 5
    checkout = client.points_checkout(tenant_id, unit_amount)
    if not checkout.get("checkout_url"):
        raise FlowError(f"expected checkout_url for points purchase, got: {checkout}")
    sessions = list_recent_points_checkout_sessions(started_at)
    session = select_points_checkout_session(sessions, tenant_id=tenant_id, unit_amount=unit_amount, points_per_unit=points_per_unit)
    return checkout, session


def complete_points_purchase(
    client: RAGFlowClient,
    tenant_id: str,
    points_to_buy: int,
    webhook_secret: str,
    points_per_unit: int,
    *,
    event_id: str | None = None,
    payment_intent_id: str | None = None,
) -> dict[str, Any]:
    unit_amount = int(points_to_buy / points_per_unit)
    _, session = create_points_checkout_session(client, tenant_id, unit_amount, points_per_unit)
    completed_event = build_points_checkout_completed_event(
        event_id=event_id or f"evt_manual_points_{uuid.uuid4().hex[:20]}",
        session=session,
        points=points_to_buy,
        payment_intent_id=payment_intent_id,
    )
    client.post_signed_webhook(completed_event, webhook_secret)
    return session


def make_default_parser(description: str) -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=description)
    parser.add_argument("--base-url", default=env("RAGFLOW_BASE_URL", "http://127.0.0.1:9380"))
    parser.add_argument("--version", default=env("RAGFLOW_API_VERSION", "v1"))
    parser.add_argument("--email", default=env("RAGFLOW_TEST_EMAIL"))
    parser.add_argument("--password", default=env("RAGFLOW_TEST_PASSWORD", "Test1234!"))
    parser.add_argument("--ready-timeout-seconds", type=int, default=int(env("RAGFLOW_READY_TIMEOUT_SECONDS", "180")))
    return parser
