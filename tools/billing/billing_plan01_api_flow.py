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
Pure API driver for the PLAN-01 case documented in tools/billing/README.md.

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

!!!!!!!!!!!!!!!!!!!!!!!!!!!!!
export STRIPE_API_KEY=sk_test_51RLgXMPtsKvwxxxxxxxxxxxxxxxxxx
export STRIPE_WEBHOOK_SECRET=whsec_10c6xxxxxxxxxxxxxxxxxxxxx
export BILLING_PRICE_ID_TRIAL=price_1RWUhlP
export BILLING_PRICE_ID_STARTER=price_1Si7
export BILLING_PRICE_ID_PRO=price_1Si7Sy
!!!!!!!!!!!!!!!!!!!!!!!!!!!!!
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
from api.db.services.billing_service import SubscriptionService  # noqa: E402
from api.utils.crypt import crypt  # noqa: E402
from common.misc_utils import get_uuid  # noqa: E402
from tools.billing.flow_common import (  # noqa: E402
    FlowError,
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

    def schedule_plan_change(self, tenant_id: str, price_id: str) -> dict[str, Any]:
        """Initiate a subscription change via checkout (upgrade/downgrade)."""
        payload = {
            "tenant_id": tenant_id,
            "payment_type": "subscription",
            "subscription_price_id": price_id,
            "quantity": 1,
        }
        return self.request_json("POST", "/billing/checkout", json=payload)["data"]

    def cancel_scheduled_subscription_change(self, tenant_id: str) -> dict[str, Any]:
        """Cancel any scheduled subscription change (downgrade/upgrade at period end)."""
        return self.request_json("POST", "/billing/cancel-scheduled-subscription-change", json={"tenant_id": tenant_id})["data"]

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


def env(name: str, fallback: str = "") -> str:
    return (os.getenv(name) or fallback).strip()


def require_env(*names: str) -> dict[str, str]:
    values = {name: env(name) for name in names}
    missing = [name for name, value in values.items() if not value]
    if missing:
        raise FlowError(f"missing required environment variables: {', '.join(missing)}")
    return values


def load_billing_config() -> dict[str, Any]:
    """Load billing configuration from service_conf.yaml with fallback handling."""
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


def replay_stripe_events(
    client: RAGFlowClient,
    *,
    webhook_secret: str,
    customer_id: str,
    subscription_ids: set[str],
    created_gte: int,
) -> int:
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


def settle_latest_subscription_invoice(clock_id: str, subscription_id: str) -> dict[str, Any] | None:
    """Advance test clock and/or pay invoice to settle the latest subscription invoice; returns settled invoice dict or None."""
    for _ in range(3):
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
            if finalize_at and int(finalize_at) > int(clock.get("frozen_time", 0)):
                advance_clock(clock_id, int(finalize_at))
                continue
        if status == "open" and int(invoice_dict.get("amount_remaining") or invoice_dict.get("amount_due") or 0) > 0:
            paid_invoice = stripe.Invoice.pay(invoice_dict["id"])
            return stripe_dict(paid_invoice)
        return invoice_dict
    return None


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


def assert_plan(client: RAGFlowClient, expected: str) -> dict[str, Any]:
    plan = client.current_plan()
    actual = plan.get("plan_name")
    if actual != expected:
        raise FlowError(f"expected plan {expected}, got {actual}: {plan}")
    return plan


def wait_for_plan(client: RAGFlowClient, expected: str, timeout_seconds: int) -> dict[str, Any]:
    """Poll current_plan until plan_name matches expected or timeout expires."""
    deadline = time.time() + timeout_seconds
    last_plan = {}
    while time.time() < deadline:
        last_plan = client.current_plan()
        if last_plan.get("plan_name") == expected:
            return last_plan
        time.sleep(3)
    raise FlowError(f"timed out waiting for plan {expected}, last plan: {last_plan}")


def wait_for_history_count(client: RAGFlowClient, minimum_count: int, timeout_seconds: int, label: str) -> list[dict[str, Any]]:
    """Poll spend_history until at least minimum_count entries appear or timeout expires."""
    deadline = time.time() + timeout_seconds
    last_history: list[dict[str, Any]] = []
    while time.time() < deadline:
        last_history = client.spend_history()
        if len(last_history) >= minimum_count:
            return last_history
        time.sleep(3)
    raise FlowError(f"timed out waiting for {label} billing history row, last count: {len(last_history)}")


def find_new_positive_paid_invoice(history: list[dict[str, Any]], previous_invoice_ids: set[str]) -> dict[str, Any]:
    for row in history:
        invoice_id = str(row.get("invoice_id") or "")
        if not invoice_id or invoice_id in previous_invoice_ids:
            continue
        amount_val = float(row.get("amount", 0) or 0)
        if amount_val > 0 and row.get("status") == "paid":
            return row
    raise FlowError(f"no new paid invoice with positive amount found; history={history}")


def extract_scheduled_change(data: dict[str, Any]) -> dict[str, Any]:
    scheduled = data.get("scheduled_change")
    return scheduled if isinstance(scheduled, dict) else data


def wait_for_pending_downgrade(client: RAGFlowClient, expected_target: str, timeout_seconds: int) -> dict[str, Any]:
    """Wait for pending_subscription_change to appear with target plan."""
    deadline = time.time() + timeout_seconds
    while time.time() < deadline:
        plan = client.current_plan()
        pending = plan.get("pending_subscription_change", {})
        if pending:
            pending_plan = pending.get("pending_plan_name", "")
            if pending_plan.lower() == expected_target.lower():
                return plan
        time.sleep(3)
    raise FlowError(f"timed out waiting for pending downgrade to {expected_target}")


def wait_for_no_pending_downgrade(client: RAGFlowClient, timeout_seconds: int) -> dict[str, Any]:
    """Wait for pending_subscription_change to disappear."""
    deadline = time.time() + timeout_seconds
    while time.time() < deadline:
        plan = client.current_plan()
        pending = plan.get("pending_subscription_change", {})
        if not pending:
            return plan
        time.sleep(3)
    raise FlowError("timed out waiting for pending downgrade to be canceled")


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


def attach_default_test_card(customer_id: str) -> str:
    """Attach the shared test Visa card (pm_card_visa) to the customer and return its ID."""
    # Attach the preset test card using keyword-arg style (Stripe SDK standard)
    attached = stripe.PaymentMethod.attach("pm_card_visa", customer=customer_id)
    pm_id = getattr(attached, "id", None) or (attached.get("id") if isinstance(attached, dict) else None) or "pm_card_visa"
    stripe.Customer.modify(customer_id, invoice_settings={"default_payment_method": pm_id})
    return pm_id


def replace_subscription_price(subscription_id: str, price_id: str, **kwargs):
    subscription = stripe_dict(stripe.Subscription.retrieve(subscription_id))
    items = ((subscription.get("items") or {}).get("data") or [])
    if not items:
        raise FlowError(f"subscription {subscription_id} has no items")
    item_id = items[0].get("id")
    if not item_id:
        raise FlowError(f"subscription {subscription_id} primary item id missing")
    return stripe.Subscription.modify(
        subscription_id,
        items=[{"id": item_id, "price": price_id, "quantity": 1}],
        **kwargs,
    )


def create_clock_customer(email: str, tenant_id: str, clock_id: str) -> str:
    """Create a Stripe test customer scoped to the test clock, returning customer_id."""
    customer = stripe.Customer.create(
        email=email,
        name=email.split("@", 1)[0],
        test_clock=clock_id,
        metadata={"tenant_id": tenant_id},
    )
    return stripe_dict(customer)["id"]


def bind_local_subscription_customer(tenant_id: str, customer_id: str) -> None:
    """Update local billing_subscription table to link tenant's subscription to Stripe customer_id."""
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


def complete_trial_checkout_upgrade(
    client: RAGFlowClient,
    *,
    webhook_secret: str,
    tenant_id: str,
    customer_id: str,
    previous_subscription_id: str,
    target_price_id: str,
    target_plan_name: str,
    subscription_ids: set[str],
    webhook_mode: str,
    webhook_wait_seconds: int,
    webhook_timeout_seconds: int,
) -> tuple[str, dict[str, Any], list[dict[str, Any]]]:
    upgrade_started_at = int(time.time()) - 5
    checkout_result = client.schedule_plan_change(tenant_id, target_price_id)
    if not checkout_result.get("redirect_to"):

        invoice_id = checkout_result.get("invoice_id", "")
        subscription_id = checkout_result.get("subscription_id", "")
        if not subscription_id:
            raise FlowError("Direct upgrade missing subscription_id")
        if checkout_result.get("plan_name") != target_plan_name:
            raise FlowError(f"Unexpected plan after direct upgrade: {checkout_result.get('plan_name')}")
        subscription_ids.add(subscription_id)

        # 手动发送 invoice.paid webhook
        latest_invoice = stripe.Invoice.retrieve(invoice_id, expand=["payment_intent"])
        invoice_paid_event = {
            "id": f"evt_manual_invoice_{uuid.uuid4().hex[:20]}",
            "object": "event",
            "type": "invoice.paid",
            "api_version": stripe.api_version,
            "created": int(time.time()),
            "data": {"object": stripe_dict(latest_invoice)},
            "livemode": False,
            "pending_webhooks": 0,
        }
        client.post_signed_webhook(invoice_paid_event, webhook_secret)
        print("  Assert: Invoice.paid webhook posted (direct upgrade)")

        # 手动发送 customer.subscription.updated webhook
        updated_sub = stripe.Subscription.retrieve(subscription_id)
        billing_config = load_billing_config()
        trial_price_id = first_plan_price_id(billing_config, "Trial")
        trial_price_obj = stripe.Price.retrieve(trial_price_id)
        trial_price_dict = stripe_dict(trial_price_obj)
        previous_attributes = {
            "plan": {
                "id": trial_price_id,
                "object": "plan",
                "product": trial_price_dict.get("product", ""),
                "amount": trial_price_dict.get("unit_amount"),
                "interval": trial_price_dict.get("recurring", {}).get("interval", ""),
                "nickname": trial_price_dict.get("nickname", ""),           }
        }
        subscription_updated_event = {
            "id": f"evt_manual_sub_updated_{uuid.uuid4().hex[:20]}",
            "object": "event",
            "type": "customer.subscription.updated",
            "api_version": stripe.api_version,
            "created": int(time.time()),
            "data": {
                "object": stripe_dict(updated_sub),
                "previous_attributes": previous_attributes,
            },
            "livemode": False,
            "pending_webhooks": 0,
        }
        client.post_signed_webhook(subscription_updated_event, webhook_secret)
        print("  Assert: Customer.subscription.updated webhook posted (direct upgrade)")

        sync_webhooks(
            client,
            mode=webhook_mode,
            webhook_secret=webhook_secret,
            customer_id=customer_id,
            subscription_ids=subscription_ids,
            created_gte=upgrade_started_at,
            wait_seconds=webhook_wait_seconds,
        )

        upgraded_plan = wait_for_plan(client, target_plan_name, webhook_timeout_seconds)
        if upgraded_plan.get("plan_name") != target_plan_name:
            raise FlowError(f"Plan did not switch to {target_plan_name}: {upgraded_plan.get('plan_name')}")

        return subscription_id, upgraded_plan, []

    history_before_upgrade = client.spend_history()
    checkout_sessions = list_recent_checkout_sessions(customer_id, upgrade_started_at)
    checkout_session = select_subscription_checkout_session(
        checkout_sessions,
        tenant_id=tenant_id,
        price_id=target_price_id,
        previous_subscription_id=previous_subscription_id,
    )
    checkout_metadata = dict(checkout_session.get("metadata") or {})
    paid_subscription, since_upgrade = create_paid_subscription(
        customer_id,
        tenant_id,
        target_price_id,
        target_plan_name,
        extra_metadata=checkout_metadata,
    )
    subscription_id = str(paid_subscription.get("id") or "")
    subscription_ids.add(subscription_id)

    latest_invoice = paid_subscription.get("latest_invoice") or {}
    if not isinstance(latest_invoice, dict):
        latest_invoice = stripe_dict(stripe.Invoice.retrieve(str(latest_invoice), expand=["payment_intent"]))
    latest_invoice_id = str(latest_invoice.get("id") or "")
    if not latest_invoice_id:
        raise FlowError(f"{target_plan_name} checkout completion is missing invoice_id")
    payment_intent = latest_invoice.get("payment_intent") or {}
    if isinstance(payment_intent, dict):
        payment_intent_id = str(payment_intent.get("id") or "")
    else:
        payment_intent_id = str(payment_intent or "")

    checkout_completed_event = build_checkout_session_completed_event(
        event_id=f"evt_manual_checkout_{uuid.uuid4().hex[:20]}",
        session_id=str(checkout_session.get("id") or ""),
        customer_id=customer_id,
        subscription_id=subscription_id,
        tenant_id=tenant_id,
        price_id=target_price_id,
        product_name=target_plan_name,
        previous_subscription_id=previous_subscription_id,
        invoice_id=latest_invoice_id,
        payment_intent_id=payment_intent_id,
        amount_total=int(latest_invoice.get("amount_paid") or latest_invoice.get("amount_due") or checkout_session.get("amount_total") or 0),
        currency=str(latest_invoice.get("currency") or checkout_session.get("currency") or "usd"),
        created=int(checkout_session.get("created") or since_upgrade),
        expires_at=int(checkout_session.get("expires_at") or (int(checkout_session.get("created") or since_upgrade) + 86400)),
    )
    client.post_signed_webhook(checkout_completed_event, webhook_secret)
    sync_webhooks(
        client,
        mode=webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=since_upgrade,
        wait_seconds=webhook_wait_seconds,
    )

    upgraded_plan = wait_for_plan(client, target_plan_name, webhook_timeout_seconds)
    wait_for_history_count(
        client,
        len(history_before_upgrade) + 1,
        webhook_timeout_seconds,
        f"Trial→{target_plan_name} upgrade payment",
    )
    history_after_upgrade = client.spend_history()
    latest = find_new_positive_paid_invoice(
        history_after_upgrade,
        {str(row.get("invoice_id") or "") for row in history_before_upgrade},
    )
    amount_val = float(latest.get("amount", 0) or 0)
    if amount_val <= 0 or latest.get("status") != "paid" or not latest.get("invoice_id"):
        raise FlowError(f"Trial→{target_plan_name} upgrade should create a paid invoice, got {latest}")
    return subscription_id, upgraded_plan, history_after_upgrade


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
    """Starter plan apps quota from service_conf.yaml."""
    billing_config = load_billing_config()
    for plan in billing_config.get("billing_plans", []):
        if plan.get("name") == "Starter":
            return int(plan.get("quota_apps", 3))
    return 3  # fallback


def get_pro_quota_apps() -> int:
    """Pro plan apps quota from service_conf.yaml."""
    billing_config = load_billing_config()
    for plan in billing_config.get("billing_plans", []):
        if plan.get("name") == "Pro":
            return int(plan.get("quota_apps", 10))
    return 10  # fallback


def run_flow(args: argparse.Namespace) -> None:
    """Execute PLAN-01 full subscription lifecycle: Trial→Pro→Starter→Trial→Starter with renewals."""
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
        raise FlowError("PLAN-01 automation requires a Stripe test-mode secret key")

    stripe.api_key = stripe_api_key
    stripe.api_version = stripe_api_version
    clock = stripe.test_helpers.TestClock.create(frozen_time=int(time.time()), name=f"ragflow-plan01-{uuid.uuid4().hex[:8]}")
    clock_id = stripe_dict(clock)["id"]
    wait_for_clock(clock_id)

    client = RAGFlowClient(args.base_url, args.version, clock_id)
    client.wait_until_ready(args.ready_timeout_seconds)
    email = args.email or f"billing-plan01-{uuid.uuid4().hex[:12]}@example.test"
    user_id, tenant_id = client.register_and_login(email, args.password)

    # Create test clock customer and ensure subscription record uses it
    customer_id = create_clock_customer(email, tenant_id, clock_id)
    # Ensure local billing_subscription record uses this customer_id so Stripe subscription is created under test clock
    ensure_billing_subscription(tenant_id, customer_id)

    subscription_ids: set[str] = set()
    initial_plan = assert_plan(client, "Trial")
    # Validate Trial quota
    overview0 = client.plan_overview()
    trial_apps = overview0.get("resources", {}).get("apps", {}).get("limit")
    if trial_apps != get_trial_quota_apps():
        raise FlowError(f"expected Trial apps quota {get_trial_quota_apps()}, got {trial_apps}")

    # Attach test card and set as default for immediate charges
    pm_id = attach_default_test_card(customer_id)
    stripe.Subscription.modify(initial_plan["subscription_id"], default_payment_method=pm_id)

    initial_subscription_id = str(initial_plan["subscription_id"])
    subscription_ids.add(initial_subscription_id)
    pro_subscription_id, pro_plan, history_after_pro = complete_trial_checkout_upgrade(
        client,
        webhook_secret=webhook_secret,
        tenant_id=tenant_id,
        customer_id=customer_id,
        previous_subscription_id=initial_subscription_id,
        target_price_id=required["BILLING_PRICE_ID_PRO"],
        target_plan_name="Pro",
        subscription_ids=subscription_ids,
        webhook_mode=args.webhook_mode,
        webhook_wait_seconds=args.webhook_wait_seconds,
        webhook_timeout_seconds=args.webhook_timeout_seconds,
    )
    pro_quota = get_pro_quota_apps()
    overview_pro = client.plan_overview()
    apps_limit_pro = overview_pro.get("resources", {}).get("apps", {}).get("limit", 0)
    if apps_limit_pro != pro_quota:
        raise FlowError(f"after Pro upgrade, expected Pro apps quota {pro_quota}, got {apps_limit_pro}")

    # Pro renewal
    pro_period_end_before_renewal = parse_plan_end(pro_plan)
    history_before_pro_renewal = client.spend_history()
    advance_clock(clock_id, pro_period_end_before_renewal + 120)
    settle_latest_subscription_invoice(clock_id, pro_subscription_id)
    sync_webhooks(
        client,
        mode=args.webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=int(time.time()) - 60,
        wait_seconds=args.webhook_wait_seconds,
    )
    pro_plan_after = wait_for_plan(client, "Pro", args.webhook_timeout_seconds)
    pro_period_end_after = parse_plan_end(pro_plan_after)
    if pro_period_end_after <= pro_period_end_before_renewal:
        raise FlowError(f"Pro billing cycle did not advance after renewal: before={pro_period_end_before_renewal}, after={pro_period_end_after}")
    history_after_pro_renewal = wait_for_history_count(
        client,
        len(history_before_pro_renewal) + 1,
        args.webhook_timeout_seconds,
        "Pro renewal",
    )
    _ = find_new_positive_paid_invoice(
        history_after_pro_renewal,
        {str(row.get("invoice_id") or "") for row in history_before_pro_renewal},
    )

    # Pro -> Starter at period end
    schedule = client.schedule_plan_change(tenant_id, required["BILLING_PRICE_ID_STARTER"])
    scheduled_change = extract_scheduled_change(schedule)
    if not scheduled_change.get("schedule_id"):
        raise FlowError(f"expected schedule_id for downgrade to Starter, got: {schedule}")
    pending_plan = wait_for_pending_downgrade(client, "Starter", args.webhook_timeout_seconds)
    if pending_plan.get("plan_name") != "Pro":
        raise FlowError(f"plan changed prematurely after scheduling downgrade to Starter: expected 'Pro', got {pending_plan.get('plan_name')}")
    pro_period_end_before_starter = parse_plan_end(pro_plan_after)
    history_before_starter = client.spend_history()
    advance_clock(clock_id, pro_period_end_before_starter + 120)
    created_gte=int(time.time()) - 60

    input("before settle_latest_subscription_invoice, press enter to continue")

    settle_latest_subscription_invoice(clock_id, pro_subscription_id)

    input("before sync_webhooks, press enter to continue")

    sync_webhooks(
        client,
        mode=args.webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=created_gte,
        wait_seconds=args.webhook_wait_seconds,
    )
    starter_plan = wait_for_plan(client, "Starter", args.webhook_timeout_seconds)
    starter_quota = get_starter_quota_apps()
    overview_starter = client.plan_overview()
    if overview_starter.get("resources", {}).get("apps", {}).get("limit", 0) != starter_quota:
        raise FlowError(f"after downgrade to Starter, expected Starter apps quota {starter_quota}, got {overview_starter}")
    history_after_starter = wait_for_history_count(
        client,
        len(history_before_starter) + 1,
        args.webhook_timeout_seconds,
        "Starter renewal after downgrade",
    )
    _ = find_new_positive_paid_invoice(
        history_after_starter,
        {str(row.get("invoice_id") or "") for row in history_before_starter},
    )

    # Starter -> Trial at period end
    schedule = client.schedule_plan_change(tenant_id, required["BILLING_PRICE_ID_TRIAL"])
    scheduled_change = extract_scheduled_change(schedule)
    if not scheduled_change.get("schedule_id"):
        raise FlowError(f"expected schedule_id for downgrade to Trial, got: {schedule}")
    pending_plan = wait_for_pending_downgrade(client, "Trial", args.webhook_timeout_seconds)
    if pending_plan.get("plan_name") != "Starter":
        raise FlowError(f"plan changed prematurely after scheduling downgrade to Trial: expected 'Starter', got {pending_plan.get('plan_name')}")
    starter_period_end_before_trial = parse_plan_end(starter_plan)
    history_before_trial = client.spend_history()
    advance_clock(clock_id, starter_period_end_before_trial + 120)
    settle_latest_subscription_invoice(clock_id, pro_subscription_id)
    sync_webhooks(
        client,
        mode=args.webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=int(time.time()) - 60,
        wait_seconds=args.webhook_wait_seconds,
    )
    trial_plan = wait_for_plan(client, "Trial", args.webhook_timeout_seconds)
    if client.plan_overview().get("resources", {}).get("apps", {}).get("limit") != get_trial_quota_apps():
        raise FlowError(f"after downgrade to Trial, expected Trial apps quota {get_trial_quota_apps()}, got {client.plan_overview()}")
    history_after_trial = client.spend_history()
    new_trial_rows = history_after_trial[: max(0, len(history_after_trial) - len(history_before_trial))]
    paid_rows = [row for row in new_trial_rows if float(row.get("amount", 0) or 0) > 0]
    if paid_rows:
        raise FlowError(f"Trial period should not create paid renewal rows, got {paid_rows}")
    wait_for_no_pending_downgrade(client, args.webhook_timeout_seconds)

    # Final Trial -> Starter immediate upgrade
    final_trial_subscription_id = str(trial_plan.get("subscription_id") or "")
    final_subscription_id, _, _ = complete_trial_checkout_upgrade(
        client,
        webhook_secret=webhook_secret,
        tenant_id=tenant_id,
        customer_id=customer_id,
        previous_subscription_id=final_trial_subscription_id,
        target_price_id=required["BILLING_PRICE_ID_STARTER"],
        target_plan_name="Starter",
        subscription_ids=subscription_ids,
        webhook_mode=args.webhook_mode,
        webhook_wait_seconds=args.webhook_wait_seconds,
        webhook_timeout_seconds=args.webhook_timeout_seconds,
    )
    subscription_ids.add(final_subscription_id)
    overview_now = client.plan_overview()
    if overview_now.get("resources", {}).get("apps", {}).get("limit", 0) != get_starter_quota_apps():
        raise FlowError(f"after final upgrade to Starter, expected Starter apps quota {get_starter_quota_apps()}, got {overview_now}")

    overview = client.plan_overview()
    history_final = client.spend_history()
    print(
        json.dumps(
            {
                "tenant_id": tenant_id,
                "email": email,
                "test_clock_id": clock_id,
                "customer_id": customer_id,
                "final_plan": overview.get("plan_name"),
                "history_rows": len(history_final),
                "webhook_mode": args.webhook_mode,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = argparse.ArgumentParser(description="Run billing PLAN-01 with Stripe Test Clock and signed webhook replay.")
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
