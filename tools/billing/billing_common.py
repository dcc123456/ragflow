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
Common utilities shared across all billing test flows.

This module breaks the circular dependency between storage_common.py and flow_common.py
by providing shared utilities that both can import from.
"""

from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import requests
import yaml


class FlowError(RuntimeError):
    """Custom exception for billing flow errors."""
    pass


def env(name: str, default: str = "") -> str:
    """Get environment variable with default."""
    return os.environ.get(name, default)


def load_billing_config() -> dict[str, Any]:
    """Load billing configuration from service_conf.yaml."""
    project_root = Path(__file__).resolve().parents[2]
    config_path = Path(env("RAGFLOW_SERVICE_CONF", str(project_root / "conf" / "service_conf.yaml")))
    if not config_path.exists():
        fallback_path = project_root / "service_conf.yaml"
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


def json_dumps_compact(payload: dict) -> str:
    """Compact JSON serialization without spaces."""
    return json.dumps(payload, separators=(",", ":"), sort_keys=False)


def ensure_webhook_delivery_success(response: requests.Response, event_type: str) -> None:
    """Ensure webhook response indicates successful delivery."""
    if response.status_code >= 400:
        raise FlowError(f"webhook {event_type} failed: status={response.status_code} body={response.text[:500]}")
    try:
        payload = response.json()
    except ValueError as exc:
        raise FlowError(f"webhook {event_type} returned non-JSON status={response.status_code}: {response.text[:500]}") from exc
    if payload.get("success") is False:
        raise FlowError(f"webhook {event_type} was rejected: {payload}")


def load_persisted_webhook_secret() -> str:
    """Load the locally persisted Stripe webhook signing secret from RAGFlow DB."""
    try:
        from api.db.db_models import DB  # noqa: E402
        from api.db.services.system_settings_service import SystemSettingsService  # noqa: E402
    except Exception as exc:  # pragma: no cover - import failures depend on local env setup
        raise FlowError(f"failed to import DB services for billing_webhook_secret lookup: {exc}") from exc

    with DB.connection_context():
        setting = SystemSettingsService.get_by_name("billing_webhook_secret")
        rows = list(setting) if setting else []

    if not rows or not getattr(rows[0], "value", ""):
        raise FlowError("billing_webhook_secret is not persisted in local DB")
    return str(rows[0].value)


def build_checkout_session_completed_event(
    *,
    event_id: str,
    session_id: str,
    customer_id: str,
    subscription_id: str,
    tenant_id: str,
    price_id: str,
    product_name: str,
    previous_subscription_id: str,
    invoice_id: str,
    payment_intent_id: str,
    amount_total: int,
    currency: str,
    created: int,
    expires_at: int,
) -> dict:
    """Build a checkout.session.completed Stripe event for webhook replay."""
    return {
        "id": event_id,
        "object": "event",
        "type": "checkout.session.completed",
        "created": created,
        "data": {
            "object": {
                "id": session_id,
                "object": "checkout.session",
                "mode": "subscription",
                "payment_status": "paid",
                "customer": customer_id,
                "subscription": subscription_id,
                "invoice": invoice_id,
                "payment_intent": payment_intent_id,
                "amount_total": amount_total,
                "currency": currency,
                "created": created,
                "expires_at": expires_at,
                "metadata": {
                    "price_type": "subscription",
                    "tenant_id": tenant_id,
                    "price_id": price_id,
                    "product_name": product_name,
                    "previous_subscription_id": previous_subscription_id,
                },
            }
        },
    }


def select_subscription_checkout_session(
    sessions: list[dict],
    *,
    tenant_id: str,
    price_id: str,
    previous_subscription_id: str,
) -> dict:
    """Find the matching checkout session for the given parameters."""
    matching_sessions = []
    for session in sessions:
        metadata = session.get("metadata") or {}

        if session.get("mode") != "subscription":
            continue
        if metadata.get("tenant_id") != tenant_id:
            continue
        if metadata.get("price_id") != price_id:
            continue
        if metadata.get("previous_subscription_id") != previous_subscription_id:
            continue
        matching_sessions.append(session)
    if not matching_sessions:
        raise FlowError(
            "expected a matching Stripe Checkout Session for tenant "
            f"{tenant_id}, price {price_id}, previous subscription {previous_subscription_id}"
        )
    matching_sessions.sort(key=lambda item: (item.get("created", 0), item.get("id", "")), reverse=True)
    return matching_sessions[0]


def assert_portal_subscription_update_url(url: str, subscription_id: str) -> None:
    """Assert that the URL is a valid Stripe Customer Portal subscription update URL."""
    parsed = urlparse(url)
    if not parsed.scheme or not parsed.netloc:
        raise FlowError(f"expected Stripe Customer Portal URL, got malformed URL: {url}")
    if "stripe.com" not in parsed.netloc:
        raise FlowError(f"expected Stripe Customer Portal URL, got non-Stripe URL: {url}")
    expected_path = f"/subscriptions/{subscription_id}/update"
    if expected_path not in parsed.path:
        raise FlowError(f"expected Stripe Customer Portal subscription update URL containing {expected_path}, got: {url}")


def get_trial_quota_apps() -> int:
    """Trial plan apps quota from service_conf.yaml."""
    billing_config = load_billing_config()
    for plan in billing_config.get("billing_plans", []):
        if plan.get("name") == "Trial":
            return int(plan.get("quota_apps", 0))
    return 0  # fallback


def get_starter_quota_apps() -> int:
    """Starter plan apps quota from service_conf.yaml."""
    billing_config = load_billing_config()
    for plan in billing_config.get("billing_plans", []):
        if plan.get("name") == "Starter":
            return int(plan.get("quota_apps", 100))
    return 100  # fallback


def first_plan_price_id(billing_config: dict[str, Any], plan_name: str) -> str:
    """Get the first price ID for a given plan name from billing config."""
    for plan in billing_config.get("billing_plans", []) or []:
        if plan.get("name") != plan_name:
            continue
        price_ids = str(plan.get("price_ids") or "").split()
        return price_ids[0] if price_ids else ""
    return ""


def stripe_dict(obj: Any) -> dict[str, Any]:
    if hasattr(obj, "to_dict_recursive"):
        return obj.to_dict_recursive()
    if isinstance(obj, dict):
        return obj
    return dict(obj)