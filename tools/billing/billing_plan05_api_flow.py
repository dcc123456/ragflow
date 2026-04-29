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
Pure API driver for the PLAN-05 case documented in tools/billing/README.md.
Tests: Upgrade with unpaid invoice must NOT grant higher entitlements early.

Required environment:
  BILLING_STRIPE_API_KEY or STRIPE_API_KEY
  BILLING_PRICE_ID_TRIAL
  BILLING_PRICE_ID_STARTER
  BILLING_PRICE_ID_PRO

Optional environment:
  RAGFLOW_BASE_URL=http://127.0.0.1:9380
  RAGFLOW_API_VERSION=v1
  RAGFLOW_TEST_EMAIL=<fresh email>
  RAGFLOW_TEST_PASSWORD=Test1234!
  BILLING_WEBHOOK_SECRET or STRIPE_WEBHOOK_SECRET (optional if local DB already stores billing_webhook_secret for manual webhook mode)
"""

from __future__ import annotations

import argparse
import hashlib
import hmac
import json
import os
import sys
import time
import uuid
from datetime import datetime, timezone, timedelta
from pathlib import Path
from typing import Any

import requests
import stripe  # type: ignore[reportMissingImports]
import yaml

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from api.db import SubscriptionStatus  # noqa: E402
from api.db.db_models import DB  # noqa: E402
from api.db.services.billing_service import PaymentOrderService, SubscriptionService  # noqa: E402
from api.utils.crypt import crypt  # noqa: E402
from common.misc_utils import get_uuid  # noqa: E402
from tools.billing.flow_common import (  # noqa: E402
    FlowError,
    assert_portal_subscription_update_url,
    build_checkout_session_completed_event,
    ensure_webhook_delivery_success,
    json_dumps_compact,
    load_persisted_webhook_secret,
    select_subscription_checkout_session,
)

FOCUSED_STRIPE_WEBHOOKS = {
    "invoice.paid",
    "invoice.payment_failed",
    "invoice.payment_action_required",
    "customer.subscription.updated",
    "customer.subscription.deleted",
    "checkout.session.completed",
    "payment_intent.succeeded",
}
TEST_CLOCK_HEADER = "X-Stripe-Test-Clock"


def ensure_billing_subscription(tenant_id: str, customer_id: str, plan_name: str = "Trial") -> None:
    """Ensure a billing_subscription record exists with the given customer_id for test."""
    with DB.connection_context():
        existing = SubscriptionService.model.get_or_none(tenant_id=tenant_id)
        if existing:
            SubscriptionService.model.update(
                customer_id=customer_id,
                subscription_id="",
                subscription_status=SubscriptionStatus.ACTIVE,
                plan_name=plan_name,
            ).where(SubscriptionService.model.tenant_id == tenant_id).execute()
        else:
            now = datetime.now(timezone.utc)
            SubscriptionService.model.create(
                id=get_uuid(),
                tenant_id=tenant_id,
                customer_id=customer_id,
                plan_name=plan_name,
                status="active",
                subscription_status=SubscriptionStatus.ACTIVE,
                start_time=now,
                end_time=now + timedelta(days=365),
            )


class RAGFlowClient:
    """HTTP client for RAGFlow billing API with Stripe test clock integration."""

    def __init__(self, base_url: str, version: str, clock_id: str):
        self.base_url = base_url.rstrip("/")
        self.version = version.strip("/")
        self.clock_id = clock_id
        self.session = requests.Session()
        self.session.trust_env = False
        self.auth_header = ""

    def url(self, path: str) -> str:
        return f"{self.base_url}/{self.version}/{path.lstrip('/')}"

    def headers(self, *, auth: bool = True) -> dict[str, str]:
        headers = {TEST_CLOCK_HEADER: self.clock_id}
        if auth and self.auth_header:
            headers["Authorization"] = self.auth_header
        return headers

    def request_json(self, method: str, path: str, *, auth: bool = True, **kwargs) -> dict[str, Any]:
        """Send authenticated HTTP request and return parsed JSON response, raising FlowError on failure."""
        response = self.session.request(method, self.url(path), headers=self.headers(auth=auth), timeout=60, **kwargs)
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(f"{method} {path} returned non-JSON status={response.status_code}: {response.text[:500]}") from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"{method} {path} failed status={response.status_code}: {payload}")
        return payload

    def wait_until_ready(self, timeout_seconds: int) -> None:
        """Poll /billing/status until server returns 200 or timeout expires."""
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
        """Register a new user and log in, returning (user_id, tenant_id)."""
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
            raise FlowError(f"register returned non-JSON status={register_response.status_code}: {register_response.text[:500]}") from exc
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
            raise FlowError(f"login returned non-JSON status={login_response.status_code}: {login_response.text[:500]}") from exc
        if login_data.get("code") != 0:
            raise FlowError(f"login failed: {login_data}")
        self.auth_header = login_response.headers.get("Authorization", "")
        if not self.auth_header:
            raise FlowError("login succeeded without Authorization header")
        data = login_data.get("data") or {}
        user_id = data.get("id") or data.get("user_id")
        tenant_id = data.get("tenant_id") or data.get("tenantId") or user_id
        if not user_id or not tenant_id:
            raise FlowError(f"login response missing ids: {login_data}")
        return user_id, tenant_id

    def current_plan(self) -> dict[str, Any]:
        return self.request_json("GET", "/billing/current_plan")["data"]

    def plan_overview(self) -> dict[str, Any]:
        return self.request_json("GET", "/billing/plan_overview")["data"]

    def spend_history(self) -> list[dict[str, Any]]:
        return self.request_json("GET", "/billing/spend_overview")["data"]

    def post_signed_webhook(self, event: dict[str, Any], webhook_secret: str) -> None:
        """Post a Stripe event to the local webhook endpoint with proper signature."""
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

    def schedule_plan_change(self, tenant_id: str, price_id: str) -> dict[str, Any]:
        """Initiate a subscription change via checkout (upgrade/downgrade)."""
        payload = {
            "tenant_id": tenant_id,
            "payment_type": "subscription",
            "subscription_price_id": price_id,
            "quantity": 1,
        }
        return self.request_json("POST", "/billing/checkout", json=payload)["data"]

    def cancel_scheduled_subscription_change(self) -> dict[str, Any]:
        """Cancel any scheduled subscription change (downgrade/upgrade at period end)."""
        return self.request_json("POST", "/billing/cancel-scheduled-subscription-change", json={})["data"]


def env(name: str, fallback: str = "") -> str:
    return (os.getenv(name) or fallback).strip()


def require_env(*names: str) -> dict[str, str]:
    values = {name: env(name) for name in names}
    missing = [name for name, value in values.items() if not value]
    if missing:
        raise FlowError(f"missing required environment variables: {', '.join(missing)}")
    return values


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


def first_plan_price_id(billing_config: dict[str, Any], plan_name: str) -> str:
    for plan in billing_config.get("billing_plans", []) or []:
        if plan.get("name") != plan_name:
            continue
        price_ids = str(plan.get("price_ids") or "").split()
        return price_ids[0] if price_ids else ""
    return ""


def assert_plan_price_ids_match_config(required: dict[str, str], billing_config: dict[str, Any]) -> None:
    plan_env_names = {
        "Trial": "BILLING_PRICE_ID_TRIAL",
        "Starter": "BILLING_PRICE_ID_STARTER",
        "Pro": "BILLING_PRICE_ID_PRO",
    }
    mismatches = []
    for plan_name, env_name in plan_env_names.items():
        configured_price_id = first_plan_price_id(billing_config, plan_name)
        if configured_price_id != required[env_name]:
            mismatches.append(f"{plan_name}: env {env_name}={required[env_name]} config={configured_price_id or '<missing>'}")
    if mismatches:
        raise FlowError("billing price_id env does not match service_conf.yaml: " + "; ".join(mismatches))


def stripe_dict(obj: Any) -> dict[str, Any]:
    if hasattr(obj, "to_dict_recursive"):
        return obj.to_dict_recursive()
    if isinstance(obj, dict):
        return obj
    return dict(obj)


def wait_for_clock(clock_id: str) -> dict[str, Any]:
    deadline = time.time() + 180
    while time.time() < deadline:
        clock = stripe.test_helpers.TestClock.retrieve(clock_id)
        clock_dict = stripe_dict(clock)
        if clock_dict.get("status") == "ready":
            return clock_dict
        time.sleep(2)
    raise FlowError(f"test clock {clock_id} did not become ready")


def advance_clock(clock_id: str, frozen_time: int) -> dict[str, Any]:
    stripe.test_helpers.TestClock.advance(clock_id, frozen_time=frozen_time)
    return wait_for_clock(clock_id)


def replace_subscription_price(subscription_id: str, price_id: str, **kwargs):
    """Replace the primary subscription item's price (avoids adding duplicate items)."""
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


def ensure_invoice_finalized(clock_id: str, subscription_id: str) -> dict[str, Any] | None:
    """Ensure the latest subscription invoice is finalized (not draft). Returns invoice dict or None."""
    for attempt in range(3):
        subscription = stripe.Subscription.retrieve(subscription_id, expand=["latest_invoice"])
        subscription_dict = stripe_dict(subscription)
        latest_invoice = subscription_dict.get("latest_invoice")
        if not latest_invoice:
            return None
        invoice = latest_invoice if isinstance(latest_invoice, dict) else stripe.Invoice.retrieve(str(latest_invoice))
        invoice_dict = stripe_dict(invoice)
        status = invoice_dict.get("status")
        if status in {"paid", "void", "uncollectible"}:
            return invoice_dict
        if status == "draft":
            finalize_at = invoice_dict.get("automatically_finalizes_at") or int(invoice_dict.get("created", 0)) + 3660
            clock = stripe_dict(stripe.test_helpers.TestClock.retrieve(clock_id))
            frozen = int(clock.get("frozen_time", 0))
            if finalize_at and int(finalize_at) > frozen:
                advance_clock(clock_id, int(finalize_at))
                continue
            try:
                stripe.Invoice.finalize_invoice(invoice_dict["id"])
            except Exception:
                return invoice_dict
        return invoice_dict
    return None


def attach_default_test_card(customer_id: str) -> str:
    """Attach the shared test Visa card (pm_card_visa) to the customer and return its ID."""
    attached = stripe.PaymentMethod.attach("pm_card_visa", customer=customer_id)
    pm_id = getattr(attached, "id", None) or (attached.get("id") if isinstance(attached, dict) else None) or "pm_card_visa"
    stripe.Customer.modify(customer_id, invoice_settings={"default_payment_method": pm_id})
    return pm_id


def attach_named_payment_method(customer_id: str, payment_method_id: str, *, set_as_default: bool = True) -> str:
    if payment_method_id.startswith("tok_"):
        created = stripe.PaymentMethod.create(type="card", card={"token": payment_method_id})
        source_payment_method_id = getattr(created, "id", None) or (created.get("id") if isinstance(created, dict) else None) or payment_method_id
    else:
        source_payment_method_id = payment_method_id
    attached = stripe.PaymentMethod.attach(source_payment_method_id, customer=customer_id)
    pm_id = getattr(attached, "id", None) or (attached.get("id") if isinstance(attached, dict) else None) or source_payment_method_id
    if set_as_default:
        stripe.Customer.modify(customer_id, invoice_settings={"default_payment_method": pm_id})
    return pm_id


def remove_customer_payment_method(customer_id: str) -> None:
    """Remove all payment methods from customer to trigger payment failure."""
    payment_methods = stripe.PaymentMethod.list(customer=customer_id, type="card")
    for pm in payment_methods.auto_paging_iter():
        stripe.PaymentMethod.detach(pm.id)


def create_clock_customer(email: str, tenant_id: str, clock_id: str) -> str:
    customer = stripe.Customer.create(
        email=email,
        name=email.split("@", 1)[0],
        test_clock=clock_id,
        metadata={"tenant_id": tenant_id},
    )
    return stripe_dict(customer)["id"]


def bind_local_subscription_customer(tenant_id: str, customer_id: str) -> None:
    with DB.connection_context():
        updated = SubscriptionService.model.update(customer_id=customer_id).where(SubscriptionService.model.tenant_id == tenant_id).execute()
    if not updated:
        raise FlowError(f"failed to bind local subscription customer for tenant {tenant_id}")


def create_paid_subscription(
    customer_id: str,
    tenant_id: str,
    price_id: str,
    product_name: str,
    *,
    extra_metadata: dict[str, str] | None = None,
) -> tuple[dict[str, Any], int]:
    """Create a paid subscription and return (subscription_payload, created_timestamp)."""
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


def parse_plan_end(plan: dict[str, Any]) -> int:
    """Extract period end timestamp from plan response."""
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


def assert_plan(client: RAGFlowClient, expected: str) -> dict[str, Any]:
    plan = client.current_plan()
    actual = plan.get("plan_name")
    if actual != expected:
        raise FlowError(f"expected plan {expected}, got {actual}: {plan}")
    return plan


def wait_for_plan(client: RAGFlowClient, expected: str, timeout_seconds: int) -> dict[str, Any]:
    deadline = time.time() + timeout_seconds
    last_plan = {}
    while time.time() < deadline:
        last_plan = client.current_plan()
        if last_plan.get("plan_name") == expected:
            return last_plan
        time.sleep(3)
    raise FlowError(f"timed out waiting for plan {expected}, last plan: {last_plan}")


def wait_for_plan_quota(client: RAGFlowClient, expected_quota_apps: int, timeout_seconds: int) -> dict[str, Any]:
    """Wait until plan_overview shows expected app quota (proof of entitlement)."""
    deadline = time.time() + timeout_seconds
    while time.time() < deadline:
        overview = client.plan_overview()
        apps_limit = overview.get("resources", {}).get("apps", {}).get("limit", 0)
        if apps_limit == expected_quota_apps:
            return overview
        time.sleep(3)
    raise FlowError(f"timed out waiting for apps quota {expected_quota_apps}")


def wait_for_history_count(client: RAGFlowClient, minimum_count: int, timeout_seconds: int, label: str) -> list[dict[str, Any]]:
    deadline = time.time() + timeout_seconds
    last_history: list[dict[str, Any]] = []
    while time.time() < deadline:
        last_history = client.spend_history()
        if len(last_history) >= minimum_count:
            return last_history
        time.sleep(3)
    raise FlowError(f"timed out waiting for {label} billing history row, last count: {len(last_history)}")


def sync_webhooks(
    client: RAGFlowClient,
    *,
    mode: str,
    webhook_secret: str,
    customer_id: str,
    subscription_ids: set[str],
    created_gte: int,
    wait_seconds: int,
) -> int:
    """Synchronize webhook events: manual mode replays from test clock; auto mode just waits."""
    if mode == "manual":
        return replay_stripe_events(
            client,
            webhook_secret=webhook_secret,
            customer_id=customer_id,
            subscription_ids=subscription_ids,
            created_gte=created_gte,
        )
    time.sleep(wait_seconds)
    return 0


def replay_stripe_events(
    client: RAGFlowClient,
    *,
    webhook_secret: str,
    customer_id: str,
    subscription_ids: set[str],
    created_gte: int,
) -> int:
    """Fetch and replay matching Stripe events from test clock; return count of events replayed."""
    replayed = 0
    events = stripe.Event.list(limit=100, created={"gte": created_gte})
    event_dicts = [stripe_dict(event) for event in events.auto_paging_iter()]
    event_dicts.sort(key=lambda event: (event.get("created", 0), event.get("id", "")))
    for event in event_dicts:
        if event.get("type") not in FOCUSED_STRIPE_WEBHOOKS:
            continue
        if not event_matches_customer(event, customer_id, subscription_ids):
            continue
        client.post_signed_webhook(event, webhook_secret)
        replayed += 1
    return replayed


def replay_until_payment_order_status(
    client: RAGFlowClient,
    *,
    mode: str,
    webhook_secret: str,
    customer_id: str,
    subscription_ids: set[str],
    created_gte: int,
    order_id: str,
    expected_status: str,
    timeout_seconds: int,
    wait_seconds: int,
) -> dict[str, Any]:
    deadline = time.time() + timeout_seconds
    last_payment_order: dict[str, Any] = {}
    while time.time() < deadline:
        sync_webhooks(
            client,
            mode=mode,
            webhook_secret=webhook_secret,
            customer_id=customer_id,
            subscription_ids=subscription_ids,
            created_gte=created_gte,
            wait_seconds=wait_seconds,
        )
        last_payment_order = PaymentOrderService.get_by_order_id(order_id) or {}
        if last_payment_order.get("payment_status") == expected_status:
            return last_payment_order
        time.sleep(2)
    raise FlowError(
        f"timed out waiting for billing_payment_order {order_id} to reach {expected_status}, "
        f"last={last_payment_order}"
    )


def event_matches_customer(event: dict[str, Any], customer_id: str, subscription_ids: set[str]) -> bool:
    obj = event.get("data", {}).get("object", {}) or {}
    if obj.get("customer") == customer_id:
        return True
    subscription = obj.get("subscription")
    if isinstance(subscription, str) and subscription in subscription_ids:
        return True
    if obj.get("id") in subscription_ids and obj.get("object") == "subscription":
        return True
    return False


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


def get_pro_quota_apps() -> int:
    """Pro plan apps quota from service_conf.yaml."""
    billing_config = load_billing_config()
    for plan in billing_config.get("billing_plans", []):
        if plan.get("name") == "Pro":
            return int(plan.get("quota_apps", 999999999))
    return 999999999  # fallback


def run_flow(args: argparse.Namespace) -> None:
    """Execute PLAN-05: upgrade with unpaid invoice must NOT grant higher entitlements early."""
    required = require_env(
        "BILLING_PRICE_ID_TRIAL",
        "BILLING_PRICE_ID_STARTER",
        "BILLING_PRICE_ID_PRO",
    )
    billing_config = load_billing_config()
    assert_plan_price_ids_match_config(required, billing_config)
    stripe_api_key = env("BILLING_STRIPE_API_KEY", env("STRIPE_API_KEY"))
    stripe_api_version = str(billing_config.get("stripe_api_version") or "2026-02-25.clover")
    stripe_api_version_override = env("STRIPE_API_VERSION")
    if stripe_api_version_override and stripe_api_version_override != stripe_api_version:
        raise FlowError(f"STRIPE_API_VERSION={stripe_api_version_override} does not match service_conf.yaml={stripe_api_version}")
    webhook_secret = env("BILLING_WEBHOOK_SECRET", env("STRIPE_WEBHOOK_SECRET"))
    if args.webhook_mode == "manual" and not webhook_secret:
        webhook_secret = load_persisted_webhook_secret()
    if not stripe_api_key:
        raise FlowError("BILLING_STRIPE_API_KEY or STRIPE_API_KEY is required")
    if args.webhook_mode == "manual" and not webhook_secret:
        raise FlowError("BILLING_WEBHOOK_SECRET or STRIPE_WEBHOOK_SECRET is required in manual webhook mode")
    if not stripe_api_key.startswith("sk_test_"):
        raise FlowError("PLAN-05 automation requires a Stripe test-mode secret key")

    stripe.api_key = stripe_api_key
    stripe.api_version = stripe_api_version
    clock = stripe.test_helpers.TestClock.create(frozen_time=int(time.time()), name=f"ragflow-plan05-{uuid.uuid4().hex[:8]}")
    clock_id = stripe_dict(clock)["id"]
    wait_for_clock(clock_id)

    client = RAGFlowClient(args.base_url, args.version, clock_id)
    client.wait_until_ready(args.ready_timeout_seconds)
    email = args.email or f"billing-plan05-{uuid.uuid4().hex[:12]}@example.test"
    user_id, tenant_id = client.register_and_login(email, args.password)

    # Create test clock customer and ensure subscription record uses it
    customer_id = create_clock_customer(email, tenant_id, clock_id)
    ensure_billing_subscription(tenant_id, customer_id)

    subscription_ids: set[str] = set()
    initial_plan = assert_plan(client, "Trial")
    # Validate Trial quota initially
    overview0 = client.plan_overview()
    trial_apps = overview0.get("resources", {}).get("apps", {}).get("limit")
    if trial_apps != get_trial_quota_apps():
        raise FlowError(f"expected Trial apps quota {get_trial_quota_apps()}, got {trial_apps}")

    initial_subscription_id = str(initial_plan["subscription_id"])
    subscription_ids.add(initial_subscription_id)
    # Attach test card and set as default
    pm_id = attach_default_test_card(customer_id)
    stripe.Subscription.modify(initial_subscription_id, default_payment_method=pm_id)

    # Step 1: Prepare a Starter tenant (upgrade from Trial -> Starter via app checkout)
    starter_checkout_started_at = int(time.time()) - 5
    schedule = client.schedule_plan_change(tenant_id, required["BILLING_PRICE_ID_STARTER"])
    if not schedule.get("redirect_to"):
        raise FlowError(f"expected portal redirect for upgrade to Starter, got: {schedule}")
    history_before_starter = client.spend_history()

    checkout_sessions = list_recent_checkout_sessions(customer_id, starter_checkout_started_at)
    checkout_session = select_subscription_checkout_session(
        checkout_sessions,
        tenant_id=tenant_id,
        price_id=required["BILLING_PRICE_ID_STARTER"],
        previous_subscription_id=initial_subscription_id,
    )

    checkout_metadata = dict(checkout_session.get("metadata") or {})
    starter_subscription, since_starter = create_paid_subscription(
        customer_id,
        tenant_id,
        required["BILLING_PRICE_ID_STARTER"],
        "Starter",
        extra_metadata=checkout_metadata,
    )
    starter_subscription_id = str(starter_subscription.get("id") or "")
    if not starter_subscription_id:
        raise FlowError("failed to create Starter subscription for checkout completion")
    subscription_ids.add(starter_subscription_id)

    latest_invoice = starter_subscription.get("latest_invoice") or {}
    if not isinstance(latest_invoice, dict):
        latest_invoice = stripe_dict(stripe.Invoice.retrieve(str(latest_invoice), expand=["payment_intent"]))
    starter_invoice_id = str(latest_invoice.get("id") or "")
    if not starter_invoice_id:
        raise FlowError("Starter checkout completion is missing invoice_id")
    payment_intent = latest_invoice.get("payment_intent") or {}
    if isinstance(payment_intent, dict):
        starter_payment_intent_id = str(payment_intent.get("id") or "")
    else:
        starter_payment_intent_id = str(payment_intent or "")
    checkout_completed_event = build_checkout_session_completed_event(
        event_id=f"evt_manual_checkout_{uuid.uuid4().hex[:20]}",
        session_id=str(checkout_session.get("id") or ""),
        customer_id=customer_id,
        subscription_id=starter_subscription_id,
        tenant_id=tenant_id,
        price_id=required["BILLING_PRICE_ID_STARTER"],
        product_name="Starter",
        previous_subscription_id=initial_subscription_id,
        invoice_id=starter_invoice_id,
        payment_intent_id=starter_payment_intent_id,
        amount_total=int(latest_invoice.get("amount_paid") or latest_invoice.get("amount_due") or checkout_session.get("amount_total") or 0),
        currency=str(latest_invoice.get("currency") or checkout_session.get("currency") or "usd"),
        created=int(checkout_session.get("created") or since_starter),
        expires_at=int(checkout_session.get("expires_at") or (int(checkout_session.get("created") or since_starter) + 86400)),
    )
    client.post_signed_webhook(checkout_completed_event, webhook_secret)

    sync_webhooks(
        client,
        mode=args.webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=since_starter,
        wait_seconds=args.webhook_wait_seconds,
    )
    # Ensure starting plan is Starter before proceeding
    _ = wait_for_plan(client, "Starter", args.webhook_timeout_seconds)

    # Verify Starter quota before upgrade
    starter_quota = get_starter_quota_apps()
    overview_before = client.plan_overview()
    apps_before = overview_before.get("resources", {}).get("apps", {}).get("limit", 0)
    if apps_before != starter_quota:
        raise FlowError(f"expected Starter apps quota {starter_quota}, got {apps_before}")

    # Validate the initial Starter upgrade invoice (must be paid)
    wait_for_history_count(client, len(history_before_starter) + 1, args.webhook_timeout_seconds, "Starter initial payment")
    history_after = client.spend_history()
    if not history_after:
        raise FlowError("billing history empty after Starter upgrade")
    # Identify the new invoice entry by comparing invoice IDs
    previous_invoice_ids = {str(row.get("invoice_id") or "") for row in history_before_starter}
    new_invoice = None
    for row in history_after:
        inv_id = str(row.get("invoice_id") or "")
        if inv_id and inv_id not in previous_invoice_ids:
            amount_val = float(row.get("amount", 0) or 0)
            if amount_val > 0 and row.get("status") == "paid":
                new_invoice = row
                break
    if not new_invoice:
        raise FlowError("no new positive paid invoice found after Starter upgrade")
    if not new_invoice.get("invoice_id"):
        raise FlowError("Starter upgrade invoice missing invoice_id in billing history")
    # Note: billing_history rows may not include plan_name; plan already verified via current_plan above

    # Step 2: Upgrade to Pro through the paid-plan upgrade path.
    checkout_result = client.schedule_plan_change(tenant_id, required["BILLING_PRICE_ID_PRO"])
    portal_url = checkout_result.get("redirect_to", "")
    assert_portal_subscription_update_url(portal_url, starter_subscription_id)

    remove_customer_payment_method(customer_id)
    stripe.Customer.modify(customer_id, invoice_settings={"default_payment_method": None}, default_payment_method=None)
    stripe.Subscription.modify(starter_subscription_id, default_payment_method=None)

    # Simulate finishing the portal upgrade and leaving the proration invoice pending payment.
    upgrade_attempt_started_at = int(time.time()) - 5
    try:
        sub_result = replace_subscription_price(
            starter_subscription_id,
            required["BILLING_PRICE_ID_PRO"],
            proration_behavior="always_invoice",
            default_payment_method=None,
            payment_behavior="default_incomplete",
            expand=["latest_invoice.payment_intent"],
        )
    except Exception as exc:
        raise FlowError(f"failed to create pending Pro upgrade invoice: {exc}") from exc
    latest_invoice = sub_result.get("latest_invoice")
    if not latest_invoice:
        raise FlowError("No invoice created during Pro upgrade")
    latest_invoice_id = str(latest_invoice.get("id") if isinstance(latest_invoice, dict) else latest_invoice)
    if not latest_invoice_id:
        raise FlowError("pending upgrade invoice is missing invoice_id")
    pro_subscription_id = starter_subscription_id

    # Step 3: Sync webhook events until the unpaid upgrade invoice is reflected locally.
    history_count_before_failed_upgrade = len(history_after)
    sync_webhooks(
        client,
        mode=args.webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=upgrade_attempt_started_at,
        wait_seconds=args.webhook_wait_seconds,
    )

    # Step 4: Verify entitlements are NOT upgraded yet (should remain at Starter level)
    current = client.current_plan()
    subscription_status = (current.get("subscription_status") or "").lower()

    overview = client.plan_overview()
    apps_limit = overview.get("resources", {}).get("apps", {}).get("limit", 0)
    starter_quota = get_starter_quota_apps()

    # Apps quota must still be at Starter level
    if apps_limit != starter_quota:
        raise FlowError(f"before payment, expected Starter apps quota {starter_quota}, got {apps_limit}")

    # Plan should still show Starter (upgrade not yet effective), but payment required
    plan_before = current.get("plan_name", "").lower()
    if plan_before != "starter":
        raise FlowError(f"before payment, expected plan_name='starter' (upgrade not paid yet), got '{plan_before}'")

    latest_invoice = stripe_dict(stripe.Invoice.retrieve(latest_invoice_id))
    if latest_invoice.get("status") not in {"open", "uncollectible", "unpaid", "draft"}:
        raise FlowError(f"expected pending Pro upgrade invoice to remain unpaid before recovery, got {latest_invoice}")

    history_before_payment = client.spend_history()
    failed_rows = [row for row in history_before_payment if row.get("invoice_id") == latest_invoice_id]
    if failed_rows:
        if len(failed_rows) != 1:
            raise FlowError(
                f"expected at most one billing history row for unpaid Pro upgrade invoice {latest_invoice_id}, got {failed_rows}"
            )
        failed_row = failed_rows[0]
        if failed_row.get("status") != "unpaid":
            raise FlowError(f"expected spend history to show unpaid for pending Pro upgrade invoice, got {failed_row}")
        if len(history_before_payment) != history_count_before_failed_upgrade + 1:
            raise FlowError(
                "unpaid Pro upgrade should add exactly one billing history row before recovery payment, "
                f"expected {history_count_before_failed_upgrade + 1}, got {len(history_before_payment)}"
            )

    # Should indicate payment is required (either payment_required flag or recoverable delinquency status)
    if not overview.get("payment_required", False) and subscription_status not in {"incomplete", "incomplete_expired", "past_due", "unpaid"}:
        raise FlowError(f"expected payment_required or incomplete status before payment, got status={subscription_status}, payment_required={overview.get('payment_required')}")

    # Step 6: Pay the invoice
    pm_id = attach_default_test_card(customer_id)
    pay_started_at = int(time.time()) - 5
    try:
        pay_result = stripe.Invoice.pay(latest_invoice_id, payment_method=pm_id)
    except Exception as exc:
        raise FlowError(f"failed to recover pending Pro upgrade invoice {latest_invoice_id}: {exc}") from exc
    pay_dict = stripe_dict(pay_result)
    if pay_dict.get("status") != "paid":
        raise FlowError(f"expected Stripe to pay pending Pro upgrade invoice, got {pay_dict}")

    # Step 7: Sync webhook until the same invoice row becomes paid.
    replay_until_payment_order_status(
        client,
        mode=args.webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=pay_started_at,
        order_id=latest_invoice_id,
        expected_status="success",
        timeout_seconds=args.webhook_timeout_seconds,
        wait_seconds=args.webhook_wait_seconds,
    )

    # Step 8: Now verify upgrade to Pro completed
    final_plan = wait_for_plan(client, "Pro", args.webhook_timeout_seconds)
    final_overview = client.plan_overview()
    final_apps_limit = final_overview.get("resources", {}).get("apps", {}).get("limit", 0)
    pro_quota = get_pro_quota_apps()

    if final_apps_limit != pro_quota:
        raise FlowError(f"after payment, expected Pro apps quota {pro_quota}, got {final_apps_limit}")

    # Payment required flag must be cleared after invoice paid
    if final_overview.get("payment_required", False):
        raise FlowError(f"after payment, payment_required should be false, got: {final_overview.get('payment_required')}")

    # Step 9: Billing history should show the paid Pro invoice after recovery.
    history = client.spend_history()
    paid_rows = [row for row in history if row.get("invoice_id") == latest_invoice_id]
    if len(paid_rows) != 1:
        raise FlowError(f"expected exactly one billing history row for recovered Pro upgrade invoice {latest_invoice_id}, got {paid_rows}")
    latest_paid = paid_rows[0]
    if latest_paid.get("status", "").lower() != "paid" or float(latest_paid.get("amount", 0) or 0) <= 0:
        raise FlowError(f"paid Pro invoice not found in billing history after payment: {history}")

    print(
        json.dumps(
            {
                "tenant_id": tenant_id,
                "email": email,
                "test_clock_id": clock_id,
                "customer_id": customer_id,
                "starter_subscription_id": starter_subscription_id,
                "pro_subscription_id": pro_subscription_id,
                "unpaid_invoice_id": latest_invoice_id,
                "final_plan": final_plan.get("plan_name"),
                "quota_apps_final": final_apps_limit,
                "payment_required_final": final_overview.get("payment_required", False),
                "history_rows": len(history),
                "webhook_mode": args.webhook_mode,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = argparse.ArgumentParser(description="Run billing PLAN-05: unpaid upgrade invoice must not grant entitlements early.")
    parser.add_argument("--base-url", default=env("RAGFLOW_BASE_URL", "http://127.0.0.1:9380"))
    parser.add_argument("--version", default=env("RAGFLOW_API_VERSION", "v1"))
    parser.add_argument("--email", default=env("RAGFLOW_TEST_EMAIL"))
    parser.add_argument("--password", default=env("RAGFLOW_TEST_PASSWORD", "Test1234!"))
    parser.add_argument(
        "--webhook-mode",
        choices=("manual", "stripe-cli"),
        default=env("RAGFLOW_BILLING_WEBHOOK_MODE", "manual"),
        help="manual signs/replays Stripe events itself; stripe-cli waits for `stripe listen --forward-to ...`.",
    )
    parser.add_argument("--webhook-wait-seconds", type=int, default=int(env("RAGFLOW_WEBHOOK_WAIT_SECONDS", "8")))
    parser.add_argument("--webhook-timeout-seconds", type=int, default=int(env("RAGFLOW_WEBHOOK_TIMEOUT_SECONDS", "180")))
    parser.add_argument("--ready-timeout-seconds", type=int, default=int(env("RAGFLOW_READY_TIMEOUT_SECONDS", "180")))
    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
