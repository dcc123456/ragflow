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

This module breaks the circular dependency between storage_common.py and billing_client.py
by providing shared utilities that both can import from.
"""

from __future__ import annotations

import argparse
import json
import os
import time
from datetime import datetime, timezone

from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import requests
import stripe  # type: ignore[reportMissingImports]
import yaml

TEST_CLOCK_HEADER = "X-Stripe-Test-Clock"

# Focused Stripe webhook events for billing flows
FOCUSED_STRIPE_WEBHOOKS = {
    "invoice.paid",
    "invoice.payment_failed",
    "customer.subscription.updated",
    "customer.subscription.deleted",
    "checkout.session.completed",
    "payment_intent.succeeded",
}


def advance_clock(clock_id: str, frozen_time: int) -> dict[str, Any]:
    """Advance Stripe test clock to the given frozen time."""
    stripe.test_helpers.TestClock.advance(clock_id, frozen_time=frozen_time)
    return wait_for_clock(clock_id)


def wait_for_clock(clock_id: str) -> dict[str, Any]:
    """Wait for Stripe test clock to become ready."""
    deadline = time.time() + 180
    while time.time() < deadline:
        clock = stripe.test_helpers.TestClock.retrieve(clock_id)
        clock_dict = stripe_dict(clock)
        if clock_dict.get("status") == "ready":
            print(f"--------clock is ready:{clock_dict}, time:{time.time()}")
            return clock_dict
        time.sleep(1)
    raise FlowError(f"test clock {clock_id} did not become ready")


class FlowError(RuntimeError):
    """Custom exception for billing flow errors."""
    pass


def env(name: str, default: str = "") -> str:
    """Get environment variable with fallback."""
    return (os.environ.get(name) or default).strip()


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


def create_clock_customer(email: str, tenant_id: str, clock_id: str) -> str:
    """Create a Stripe customer with test clock."""
    customer = stripe.Customer.create(
        email=email,
        name=email.split("@", 1)[0],
        test_clock=clock_id,
        metadata={"tenant_id": tenant_id},
    )
    return stripe_dict(customer)["id"]


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


def make_default_parser(description: str) -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=description)
    parser.add_argument("--base-url", default=env("RAGFLOW_BASE_URL", "http://127.0.0.1:9380"))
    parser.add_argument("--version", default=env("RAGFLOW_API_VERSION", "v1"))
    parser.add_argument("--email", default=env("RAGFLOW_TEST_EMAIL"))
    parser.add_argument("--password", default=env("RAGFLOW_TEST_PASSWORD", "Test1234!"))
    parser.add_argument("--webhook-wait-seconds", type=int, default=int(env("RAGFLOW_WEBHOOK_WAIT_SECONDS", "8")))
    parser.add_argument("--webhook-timeout-seconds", type=int, default=int(env("RAGFLOW_WEBHOOK_TIMEOUT_SECONDS", "60")))
    parser.add_argument("--ready-timeout-seconds", type=int, default=int(env("RAGFLOW_READY_TIMEOUT_SECONDS", "60")))
    parser.add_argument("--webhook-mode", choices=("manual", "stripe-cli"), default=env("RAGFLOW_BILLING_WEBHOOK_MODE", "stripe-cli"),
                        help="stripe-cli is the preferred mode for plan/storage flows; manual remains for legacy synthetic-webhook cases.",
                        )
    return parser


def delete_clock(clock_id: str) -> None:
    """Delete Stripe test clock to clean up resources."""
    try:
        stripe.test_helpers.TestClock.delete(clock_id)
        print(f"  Info: Deleted Stripe test clock: {clock_id}")
    except Exception as exc:
        print(f"  Warning: Failed to delete test clock {clock_id}: {exc}")


def create_paid_subscription(
    customer_id: str,
    tenant_id: str,
    price_id: str,
    product_name: str,
    *,
    extra_metadata: dict[str, str] | None = None,
) -> tuple[dict[str, Any], int]:
    """Create a paid Stripe subscription with immediate payment; returns subscription payload."""
    before = int(time.time()) - 5
    metadata = {
        "price_type": "subscription",
        "tenant_id": tenant_id,
        "price_id": price_id,
        "product_name": product_name,
    }
    if extra_metadata:
        metadata.update({key: value for key, value in extra_metadata.items() if value is not None})
    subscription = stripe.Subscription.create(
        customer=customer_id,
        items=[{"price": price_id, "quantity": 1}],
        metadata=metadata,
        payment_behavior="error_if_incomplete",
        expand=["latest_invoice.payment_intent"],
    )
    return stripe_dict(subscription), before


def list_recent_checkout_sessions(customer_id: str, created_gte: int) -> list[dict[str, Any]]:
    """List recent checkout sessions for a customer after a given timestamp."""
    sessions = stripe.checkout.Session.list(limit=20)
    results: list[dict[str, Any]] = []
    for session in sessions.auto_paging_iter():
        session_dict = stripe_dict(session)
        if customer_id and session_dict.get("customer") != customer_id:
            continue
        if int(session_dict.get("created", 0) or 0) < created_gte:
            continue
        results.append(session_dict)
    return results


def replace_subscription_price(subscription_id: str, price_id: str, **kwargs):
    """Replace the primary subscription item's price (avoids adding duplicate items).

    This is the PLAN-05 mode: directly modify the subscription via Stripe API
    without relying on Checkout Session.

    Args:
        subscription_id: Stripe subscription ID
        price_id: New Stripe price ID to apply
        **kwargs: Additional arguments for stripe.Subscription.modify

    Returns:
        Updated Stripe subscription object
    """
    subscription = stripe_dict(stripe.Subscription.retrieve(subscription_id))
    items = ((subscription.get("items") or {}).get("data") or [])
    if not items:
        raise FlowError(f"subscription {subscription_id} has no items")
    item_id = items[0].get("id")
    if not item_id:
        raise FlowError(f"subscription {subscription_id} primary item id missing")
    kwargs.setdefault("proration_behavior", "always_invoice")
    return stripe.Subscription.modify(
        subscription_id,
        items=[{"id": item_id, "price": price_id, "quantity": 1}],
        **kwargs,
    )


def parse_plan_end(plan: dict[str, Any]) -> int:
    """Extract period end timestamp from plan response, handling multiple formats."""
    value = plan.get("end_time") or plan.get("billing_cycle", {}).get("end")
    if not value:
        raise FlowError(f"plan response is missing end_time: {plan}")
    if isinstance(value, (int, float)):
        return int(value)
    text = str(value).replace("Z", "+00:00")
    if len(text) == 10:
        dt = datetime.fromisoformat(text).replace(tzinfo=timezone.utc)
    else:
        dt = datetime.fromisoformat(text)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
    return int(dt.timestamp())


def find_new_positive_paid_invoice(history: list[dict[str, Any]], previous_invoice_ids: set[str]) -> dict[str, Any]:
    """Find a new paid invoice with positive amount not in previous_invoice_ids."""
    for row in history:
        invoice_id = str(row.get("invoice_id") or "")
        if not invoice_id or invoice_id in previous_invoice_ids:
            continue
        amount_val = float(row.get("amount", 0) or 0)
        if amount_val > 0 and row.get("status") == "paid":
            return row
    raise FlowError(f"no new paid invoice with positive amount found; history={history}")


def remove_customer_payment_method(customer_id: str) -> None:
    """Remove all payment methods from customer to trigger payment failure."""
    payment_methods = stripe.PaymentMethod.list(customer=customer_id, type="card")
    for pm in payment_methods.auto_paging_iter():
        stripe.PaymentMethod.detach(pm.id)


def extract_scheduled_change(data: dict[str, Any]) -> dict[str, Any]:
    """Extract scheduled_change from response data."""
    scheduled = data.get("scheduled_change")
    return scheduled if isinstance(scheduled, dict) else data


def get_pro_quota_apps() -> int:
    """Pro plan apps quota from service_conf.yaml."""
    billing_config = load_billing_config()
    for plan in billing_config.get("billing_plans", []):
        if plan.get("name") == "Pro":
            return int(plan.get("quota_apps", 999999999))
    return 999999999
