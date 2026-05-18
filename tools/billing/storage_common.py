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
Common utilities for storage billing API flows.
"""

from __future__ import annotations

import argparse
import hashlib
import hmac
import sys
import time
import uuid
from datetime import datetime, timezone, timedelta
from pathlib import Path
from typing import Any

import requests
import stripe  # type: ignore[reportMissingImports]

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from api.db import SubscriptionStatus  # noqa: E402
from api.db.db_models import DB  # noqa: E402
from api.db.services.billing_service import PaymentOrderService, SubscriptionService  # noqa: E402
from api.utils.crypt import crypt  # noqa: E402
from common.misc_utils import get_uuid  # noqa: E402
from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    ensure_webhook_delivery_success,
    first_plan_price_id,
    get_starter_quota_apps,
    json_dumps_compact,
    load_billing_config,
    load_persisted_webhook_secret, stripe_dict, env,
)


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


def list_recent_checkout_sessions(customer_id: str, created_gte: int) -> list[dict[str, Any]]:
    """List recent checkout sessions for the customer (plan checkout sessions, not storage)."""
    sessions = stripe.checkout.Session.list(limit=20)
    results = []
    for session in sessions.auto_paging_iter():
        session_dict = stripe_dict(session)
        if customer_id and session_dict.get("customer") != customer_id:
            continue
        if int(session_dict.get("created", 0) or 0) < created_gte:
            continue
        results.append(session_dict)
    return results


def sync_webhooks(
        client: "RAGFlowClient",
        *,
        webhook_secret: str,
        customer_id: str,
        subscription_ids: set[str],
        created_gte: int,
        wait_seconds: int,
) -> int:
    """Synchronize webhook events: replay from test clock."""
    replayed = 0
    events = stripe.Event.list(limit=100, created={"gte": created_gte})
    event_dicts = [stripe_dict(event) for event in events.auto_paging_iter()]
    event_dicts.sort(key=lambda event: (event.get("created", 0), event.get("id", "")))
    for event in event_dicts:
        if event.get("type") not in FOCUSED_STRIPE_WEBHOOKS:
            continue
        obj = event.get("data", {}).get("object", {}) or {}
        if obj.get("customer") != customer_id:
            continue
        subscription = obj.get("subscription")
        if isinstance(subscription, str) and subscription not in subscription_ids:
            continue
        if obj.get("id") not in subscription_ids and obj.get("object") != "subscription":
            continue
        client.post_signed_webhook(event, webhook_secret)
        replayed += 1
    time.sleep(wait_seconds)
    return replayed


def replay_stripe_events(
        client: "RAGFlowClient",
        *,
        webhook_secret: str,
        customer_id: str,
        subscription_ids: set[str],
        created_gte: int,
) -> int:
    """Fetch and replay matching Stripe events from test clock (without sleep)."""
    replayed = 0
    events = stripe.Event.list(limit=100, created={"gte": created_gte})
    event_dicts = [stripe_dict(event) for event in events.auto_paging_iter()]
    event_dicts.sort(key=lambda event: (event.get("created", 0), event.get("id", "")))
    for event in event_dicts:
        if event.get("type") not in FOCUSED_STRIPE_WEBHOOKS:
            continue
        obj = event.get("data", {}).get("object", {}) or {}
        if obj.get("customer") != customer_id:
            continue
        subscription = obj.get("subscription")
        if isinstance(subscription, str) and subscription not in subscription_ids:
            continue
        if obj.get("id") not in subscription_ids and obj.get("object") != "subscription":
            continue
        client.post_signed_webhook(event, webhook_secret)
        replayed += 1
    return replayed


def ensure_billing_subscription(tenant_id: str, customer_id: str, plan_name: str = "Trial") -> str:
    """Ensure a billing_subscription record exists with the given customer_id for test.

    Returns:
        The subscription_id from the database record (may be empty string if not set).
    """
    with DB.connection_context():
        existing = SubscriptionService.model.get_or_none(tenant_id=tenant_id)
        if existing:
            SubscriptionService.model.update(
                customer_id=customer_id,
                subscription_id="",
                subscription_status=SubscriptionStatus.ACTIVE,
                plan_name=plan_name,
            ).where(SubscriptionService.model.tenant_id == tenant_id).execute()
            return existing.subscription_id or ""
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
                end_time=now + timedelta(days=30),
            )
            return ""


def require_env(*names: str) -> dict[str, str]:
    values = {name: env(name) for name in names}
    missing = [name for name, value in values.items() if not value]
    if missing:
        raise FlowError(f"missing required environment variables: {', '.join(missing)}")
    return values


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


def wait_for_clock(clock_id: str) -> dict[str, Any]:
    """Wait for Stripe test clock to become ready."""
    deadline = time.time() + 180
    while time.time() < deadline:
        clock = stripe.test_helpers.TestClock.retrieve(clock_id)
        clock_dict = stripe_dict(clock)
        if clock_dict.get("status") == "ready":
            return clock_dict
        time.sleep(1)
    raise FlowError(f"test clock {clock_id} did not become ready")


def advance_clock(clock_id: str, frozen_time: int) -> dict[str, Any]:
    """Advance Stripe test clock to the given frozen time."""
    stripe.test_helpers.TestClock.advance(clock_id, frozen_time=frozen_time)
    return wait_for_clock(clock_id)


def delete_clock(clock_id: str) -> None:
    """Delete Stripe test clock to clean up resources."""
    try:
        stripe.test_helpers.TestClock.delete(clock_id)
        print(f"  Info: Deleted Stripe test clock: {clock_id}")
    except Exception as exc:
        print(f"  Warning: Failed to delete test clock {clock_id}: {exc}")


def get_pro_quota_apps() -> int:
    """Pro plan apps quota from service_conf.yaml."""
    billing_config = load_billing_config()
    for plan in billing_config.get("billing_plans", []):
        if plan.get("name") == "Pro":
            return int(plan.get("quota_apps", 999999999))
    return 999999999


def attach_default_test_card(customer_id: str) -> str:
    """Attach the shared test Visa card (pm_card_visa) to the customer and return its ID."""
    attached = stripe.PaymentMethod.attach("pm_card_visa", customer=customer_id)
    pm_id = getattr(attached, "id", None) or (attached.get("id") if isinstance(attached, dict) else None) or "pm_card_visa"
    stripe.Customer.modify(customer_id, invoice_settings={"default_payment_method": pm_id})
    return pm_id


def create_clock_customer(email: str, tenant_id: str, clock_id: str) -> str:
    """Create a Stripe customer with test clock."""
    customer = stripe.Customer.create(
        email=email,
        name=email.split("@", 1)[0],
        test_clock=clock_id,
        metadata={"tenant_id": tenant_id},
    )
    return stripe_dict(customer)["id"]


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


def load_storage_runtime_config() -> dict[str, Any]:
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
        raise FlowError("Storage automation requires a Stripe test-mode secret key")

    billing_plans_config = billing_config.get("billing_plans") or {}
    storage_config = billing_plans_config[0] if billing_plans_config else {}
    if not isinstance(storage_config, dict):
        raise FlowError("billing.storage_addon must be a map in service_conf.yaml")
    price_id = env("BILLING_STORAGE_PRICE_ID", str(storage_config.get("price_ids") or ""))
    if not price_id or price_id == "price_xxx":
        raise FlowError("BILLING_STORAGE_PRICE_ID or billing.storage_addon.price_id is not configured")

    webhook_secret = env("BILLING_WEBHOOK_SECRET", env("STRIPE_WEBHOOK_SECRET"))
    if not webhook_secret:
        webhook_secret = load_persisted_webhook_secret()

    return {
        "billing_config": billing_config,
        "stripe_api_key": stripe_api_key,
        "stripe_api_version": stripe_api_version,
        "webhook_secret": webhook_secret,
        "storage_price_id": price_id,
    }


BYTES_PER_GB = 1000 * 1000 * 1000


def gb_to_bytes(gb: int) -> int:
    return gb * BYTES_PER_GB


class RAGFlowClient:
    """HTTP client for RAGFlow billing APIs used by storage flows."""

    def __init__(self, base_url: str, version: str, clock_id: str | None = None):
        self.base_url = base_url.rstrip("/")
        self.version = version.strip("/")
        self.clock_id = clock_id
        self.session = requests.Session()
        self.session.trust_env = False
        self.auth_header = ""

    def url(self, path: str) -> str:
        return f"{self.base_url}/{self.version}/{path.lstrip('/')}"

    def headers(self, *, auth: bool = True) -> dict[str, str]:
        headers: dict[str, str] = {}
        if self.clock_id:
            headers[TEST_CLOCK_HEADER] = self.clock_id
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

    def current_plan(self) -> dict[str, Any]:
        return self.request_json("GET", "/billing/current_plan")["data"]

    def plan_overview(self) -> dict[str, Any]:
        return self.request_json("GET", "/billing/plan_overview")["data"]

    def storage_current(self, tenant_id: str) -> dict[str, Any]:
        return self.request_json("GET", f"/billing/storage/current?tenant_id={tenant_id}")["data"]

    def storage_set_target(self, tenant_id: str, target_quantity_bytes: int) -> dict[str, Any]:
        return self.request_json(
            "POST",
            "/billing/storage/set-target",
            json={"tenant_id": tenant_id, "target_quantity_bytes": target_quantity_bytes},
        )["data"]

    def spend_history(self) -> list[dict[str, Any]]:
        return self.request_json("GET", "/billing/spend_overview")["data"].get("items", [])

    def schedule_plan_change(self, tenant_id: str, price_id: str) -> dict[str, Any]:
        """Initiate a subscription change via checkout (upgrade/downgrade)."""
        payload = {
            "tenant_id": tenant_id,
            "payment_type": "subscription",
            "subscription_price_id": price_id,
            "quantity": 1,
        }
        return self.request_json("POST", "/billing/checkout", json=payload)["data"]

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



def list_recent_storage_checkout_sessions(created_gte: int) -> list[dict[str, Any]]:
    sessions = stripe.checkout.Session.list(limit=50)

    filtered: list[dict[str, Any]] = []
    for session in sessions.data:
        session_dict = stripe_dict(session)
        metadata = session_dict.get("metadata") or {}

        if int(session_dict.get("created") or 0) < created_gte:
            continue
        if session_dict.get("mode") != "subscription":
            continue
        if metadata.get("product_name") != "storage":
            continue

        filtered.append(session_dict)
    filtered.sort(key=lambda item: (item.get("created", 0), item.get("id", "")), reverse=True)
    return filtered


def select_storage_checkout_session(
        sessions: list[dict[str, Any]],
        *,
        tenant_id: str,
) -> dict[str, Any]:
    for session in sessions:
        metadata = session.get("metadata") or {}

        if metadata.get("tenant_id") != tenant_id:
            continue
        return session
    raise FlowError(f"expected a matching storage checkout session for tenant {tenant_id}")


def build_storage_checkout_completed_event(
        *,
        event_id: str,
        session: dict[str, Any],
        subscription_id: str,
        original_metadata: dict,
        payment_intent_id: str | None = None,
) -> dict[str, Any]:
    created = int(session.get("created") or time.time())
    expires_at = int(session.get("expires_at") or (created + 86400))
    amount_total = int(session.get("amount_total") or 0)
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
                "subscription": subscription_id,
                "payment_status": "paid",
                "status": "complete",
                "mode": "subscription",
                "currency": str(session.get("currency") or "usd"),
                "amount_subtotal": amount_total,
                "amount_total": amount_total,
                "invoice": session.get("invoice") or f"in_manual_{uuid.uuid4().hex[:24]}",
                "metadata": original_metadata,
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


def make_default_parser(description: str) -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=description)
    parser.add_argument("--base-url", default=env("RAGFLOW_BASE_URL", "http://127.0.0.1:9380"))
    parser.add_argument("--version", default=env("RAGFLOW_API_VERSION", "v1"))
    parser.add_argument("--email", default=env("RAGFLOW_TEST_EMAIL"))
    parser.add_argument("--password", default=env("RAGFLOW_TEST_PASSWORD", "Test1234!"))
    parser.add_argument("--webhook-wait-seconds", type=int, default=int(env("RAGFLOW_WEBHOOK_WAIT_SECONDS", "8")))
    parser.add_argument("--webhook-timeout-seconds", type=int, default=int(env("RAGFLOW_WEBHOOK_TIMEOUT_SECONDS", "60")))
    parser.add_argument("--ready-timeout-seconds", type=int, default=int(env("RAGFLOW_READY_TIMEOUT_SECONDS", "60")))
    return parser

def wait_for_plan(client: RAGFlowClient, expected: str, timeout_seconds: int) -> dict[str, Any]:
    deadline = time.time() + timeout_seconds
    last_plan = {}
    while time.time() < deadline:
        last_plan = client.current_plan()
        if last_plan.get("plan_name") == expected:
            return last_plan
        time.sleep(1)
    raise FlowError(f"timed out waiting for plan {expected}, last plan: {last_plan}")


def create_storage_subscription_via_webhook(
        client: RAGFlowClient,
        tenant_id: str,
        target_quantity_bytes: int,
        webhook_secret: str,
        customer_id: str,
        storage_price_id: str,
) -> dict[str, Any]:
    """
    通过 webhook 创建存储订阅（模仿 PLAN-02 的完整流程）

    流程：
    1. 调用 /billing/storage/set-target 创建 checkout session
    2. 手动创建 Stripe 订阅（模拟用户完成支付）
    3. 发送 checkout.session.completed webhook 触发后端同步
    """
    target_quantity_gb = target_quantity_bytes // BYTES_PER_GB

    # 1. 调用 API 创建 checkout session
    started_at = int(time.time()) - 5
    checkout_result = client.storage_set_target(tenant_id, target_quantity_bytes)

    if not checkout_result.get("redirect_to") and not checkout_result.get("checkout_url"):
        raise FlowError(f"expected redirect_to for storage purchase, got: {checkout_result}")

    # 2. 获取 checkout session 信息
    sessions = list_recent_storage_checkout_sessions(started_at)
    checkout_session = select_storage_checkout_session(sessions, tenant_id=tenant_id)

    # 3. 创建真实的 Stripe 订阅（模拟用户完成支付）
    subscription = stripe.Subscription.create(
        customer=customer_id,
        items=[{"price": storage_price_id, "quantity": target_quantity_gb}],
        metadata=checkout_session.get('metadata'),
        payment_behavior="error_if_incomplete",
        expand=["latest_invoice.payment_intent"],
    )

    subscription_dict = stripe_dict(subscription)
    subscription_id = subscription_dict.get("id", "")

    if not subscription_id:
        raise FlowError(f"Failed to create storage subscription: {subscription_dict}")

    # 4. 获取 invoice 和 payment_intent 信息
    latest_invoice = subscription_dict.get("latest_invoice") or {}
    if isinstance(latest_invoice, dict):
        payment_intent = latest_invoice.get("payment_intent") or {}
        payment_intent_id = payment_intent.get("id", "") if isinstance(payment_intent, dict) else str(payment_intent)
    else:
        payment_intent_id = ""

    # 5. 发送 checkout.session.completed webhook
    completed_event = build_storage_checkout_completed_event(
        event_id=f"evt_manual_storage_{uuid.uuid4().hex[:20]}",
        session=checkout_session,
        subscription_id=subscription_id,
        original_metadata=checkout_session.get('metadata', {}),
        payment_intent_id=payment_intent_id,
    )

    client.post_signed_webhook(completed_event, webhook_secret)

    # 6. 等待 webhook 处理完成
    time.sleep(2)

    return {
        "id": subscription_id,
        "subscription": subscription_id,
        "subscription_id": subscription_id,
        "checkout_session_id": checkout_session.get("id"),
    }


def wait_for_storage_status(
        client: RAGFlowClient,
        tenant_id: str,
        expected_status: str,
        timeout_seconds: int = 30,
) -> dict[str, Any]:
    """等待存储订阅达到指定状态"""
    deadline = time.time() + timeout_seconds
    last_storage = {}
    while time.time() < deadline:
        last_storage = client.storage_current(tenant_id)
        status = last_storage.get("status", "")
        if status == expected_status:
            return last_storage
        print(f"-----sleep 1 seconds, waiting for storage status to be {expected_status}, current: {status}")
        time.sleep(1)
    raise FlowError(f"timed out waiting for storage status {expected_status}, last: {last_storage}")


def replace_storage_subscription_quantity(
        client: "RAGFlowClient",
        tenant_id: str,
        new_quantity_gb: int,
        *,
        webhook_secret: str = "",
        customer_id: str = "",
        subscription_ids: set[str] | None = None,
) -> dict[str, Any]:
    """
    Replace/update storage subscription quantity via the backend API.

    This is used for upgrading or downgrading storage addon quantity.
    Calls the backend /billing/storage/set-target endpoint instead of direct Stripe API.

    Args:
        client: RAGFlowClient instance for API calls
        tenant_id: RAGFlow tenant ID
        new_quantity_gb: New quantity in GB
        webhook_secret: Webhook secret for signing events (optional)
        customer_id: Stripe customer ID for webhook replay filtering (optional)
        subscription_ids: Set of subscription IDs for webhook replay filtering (optional)

    Returns:
        Dictionary with the result including:
        - tenant_id: The tenant ID
        - storage_quantity_gb: The new storage quantity
        - target_quantity_bytes: The target quantity in bytes
        - addon_storage_bytes: The effective addon storage in bytes
    """
    if not tenant_id:
        raise FlowError("tenant_id is required for updating storage")
    if new_quantity_gb < 0:
        raise FlowError("new_quantity_gb must be non-negative")

    target_quantity_bytes = new_quantity_gb * BYTES_PER_GB

    # Step 1: Call backend API to set storage target
    print(f"  Setting storage target: tenant={tenant_id}, quantity={new_quantity_gb}GB ({target_quantity_bytes} bytes)")
    created_gte = int(time.time()) - 5
    try:
        result = client.storage_set_target(tenant_id, target_quantity_bytes)
        print("  ✅ Storage target updated via backend API")
    except FlowError as exc:
        raise FlowError(f"Failed to update storage target via backend API: {exc}") from exc

    addon_storage_bytes = result.get("addon_storage_bytes", 0)
    returned_target_bytes = result.get("target_quantity_bytes", 0)

    # Step 2: Replay webhook events if created_gte provided (for test clock sync)

    print("  Replaying webhook events for synchronization")
    replay_stripe_events(
        client,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids or set(),
        created_gte=created_gte,
    )
    print("  ✅ Webhook events replayed")

    # Step 3: Verify the storage was updated correctly
    print("  Verifying storage update result")
    storage_info = client.storage_current(tenant_id)
    actual_addon_bytes = storage_info.get("addon_storage_bytes", 0)

    expected_bytes = new_quantity_gb * BYTES_PER_GB
    if new_quantity_gb > 0 and actual_addon_bytes < expected_bytes:
        raise FlowError(
            f"Storage verification failed: expected at least {expected_bytes} bytes, got {actual_addon_bytes} bytes"
        )

    print(f"  ✅ Storage update verified: {actual_addon_bytes} bytes ({actual_addon_bytes // BYTES_PER_GB}GB)")

    return {
        "tenant_id": tenant_id,
        "storage_quantity_gb": new_quantity_gb,
        "target_quantity_bytes": returned_target_bytes or target_quantity_bytes,
        "addon_storage_bytes": addon_storage_bytes,
        "redirect_to": result.get("redirect_to", ""),
    }


def schedule_storage_downgrade(
        client: "RAGFlowClient",
        tenant_id: str,
        target_quantity_bytes: int,
) -> dict[str, Any]:
    """Schedule a storage addon downgrade using the billing/storage/set-target endpoint.

    This is similar to schedule_subscription_downgrade for plan downgrades.
    The storage addon quantity will be reduced at the next billing period end.

    Args:
        client: RAGFlowClient instance
        tenant_id: RAGFlow tenant ID
        target_quantity_bytes: Target storage quantity in bytes

    Returns:
        API response data
    """
    response = client.storage_set_target(tenant_id, target_quantity_bytes)
    return response


def downgrade_to_trial(
        client: "RAGFlowClient",
        tenant_id: str,
        subscription_id: str,
        webhook_secret: str,
        *,
        webhook_timeout_seconds: int = 60,
) -> dict[str, Any]:
    """
    Downgrade a user's paid subscription to the Trial plan via server API.

    This method follows the PLAN-01 pattern:
    1. Retrieves the Trial plan price ID from config
    2. Calls client.schedule_plan_change() to send request to server
    3. Server handles Stripe interaction and database updates
    4. Waits for pending downgrade to appear
    5. Optionally syncs webhook events for test clock synchronization
    6. Verifies the downgrade result

    Args:
        client: RAGFlowClient instance for API calls
        tenant_id: RAGFlow tenant ID
        subscription_id: Stripe subscription ID (for tracking purposes)
        webhook_secret: Webhook secret for signing events (optional)
        webhook_timeout_seconds: Timeout for waiting for plan change

    Returns:
        Dictionary with downgrade result including updated subscription info

    Raises:
        FlowError: If any step in the downgrade process fails
    """
    if not subscription_id:
        raise FlowError("subscription_id is required for downgrade")

    # Step 1: Get the Trial plan price ID from config
    print("  Loading Trial plan price ID from config")
    billing_config = load_billing_config()
    trial_price_id = first_plan_price_id(billing_config, "Trial")
    if not trial_price_id:
        raise FlowError("Trial plan price_id not found in service_conf.yaml")
    print(f"  ✅ Trial plan price ID: {trial_price_id}...")

    # Step 2: Call server API to schedule plan change (PLAN-01 pattern)
    # This sends POST /billing/checkout to the server, which handles Stripe interaction
    print("  Scheduling downgrade to Trial via server API")
    checkout_result = client.schedule_plan_change(tenant_id, trial_price_id)
    scheduled_change = extract_scheduled_change(checkout_result)
    if not scheduled_change.get("schedule_id"):
        raise FlowError(f"expected schedule_id for downgrade to Trial, got: {checkout_result}")
    print(f"  ✅ Downgrade scheduled via server, schedule_id: {scheduled_change.get('schedule_id')}")

    # Step 3: Wait for pending downgrade to appear in current_plan
    print("  Waiting for pending downgrade to appear")
    pending_plan = wait_for_pending_downgrade(client, "Trial", webhook_timeout_seconds)
    current_plan_name = pending_plan.get("plan_name", "")
    if current_plan_name == "Trial":
        raise FlowError(f"plan changed prematurely after scheduling downgrade to Trial: expected paid plan, got {current_plan_name}")
    print(f"  ✅ Pending downgrade confirmed: current={current_plan_name}, pending=Trial")

    # Step 5: Verify the downgrade result
    print("  Verifying downgrade result")
    current_plan = client.current_plan()
    plan_name = current_plan.get("plan_name", "")

    # After scheduling, plan should still be the paid plan (downgrade happens at period end)
    if plan_name == "Trial":
        raise FlowError(f"Downgrade verification failed: expected paid plan (pending Trial), got {plan_name}")
    print(f"  ✅ Downgrade to Trial scheduled successfully (will apply at period end)， current:{plan_name}")

    print(" As trial can not have storage addon, set storage addon to 0 as a schedule")
    replace_storage_subscription_quantity(client=client, tenant_id=tenant_id, new_quantity_gb=0, webhook_secret=webhook_secret)

    return {
        "downgraded": False,  # Not yet applied, scheduled for period end
        "scheduled": True,
        "subscription_id": subscription_id,
        "schedule_id": scheduled_change.get("schedule_id"),
        "old_plan_name": current_plan_name,
        "new_plan_name": "Trial",
        "pending": True,
        "current_plan": current_plan,
    }


def extract_scheduled_change(data: dict[str, Any]) -> dict[str, Any]:
    """Extract scheduled_change from response data."""
    scheduled = data.get("scheduled_change")
    return scheduled if isinstance(scheduled, dict) else data


def wait_for_pending_downgrade(client: "RAGFlowClient", expected_target: str, timeout_seconds: int = 60) -> dict[str, Any]:
    """Wait for pending_subscription_change to appear with target plan."""
    deadline = time.time() + timeout_seconds
    while time.time() < deadline:
        plan = client.current_plan()
        pending = plan.get("pending_subscription_change", {})
        if pending:
            pending_plan = pending.get("pending_plan_name", "")
            if pending_plan.lower() == expected_target.lower():
                return plan
        time.sleep(1)
    raise FlowError(f"timed out waiting for pending downgrade to {expected_target}")


def wait_for_no_pending_downgrade(client: "RAGFlowClient", timeout_seconds: int = 60) -> dict[str, Any]:
    """Wait for pending_subscription_change to disappear."""
    deadline = time.time() + timeout_seconds
    while time.time() < deadline:
        plan = client.current_plan()
        pending = plan.get("pending_subscription_change", {})
        if not pending:
            return plan
        time.sleep(1)
    raise FlowError("timed out waiting for pending downgrade to be canceled")


def downgrade_pro_to_starter(
        client: "RAGFlowClient",
        tenant_id: str,
        customer_id: str,
        subscription_id: str,
        *,
        webhook_secret: str = "",
        created_gte: int = 0,
        webhook_wait_seconds: int = 8,
        webhook_timeout_seconds: int = 180,
) -> dict[str, Any]:
    """
    Downgrade a user's Pro subscription to the Starter plan via server API.

    This method follows the PLAN-01 pattern:
    1. Retrieves the Starter plan price ID from config
    2. Calls client.schedule_plan_change() to send request to server
    3. Server handles Stripe interaction and database updates
    4. Waits for pending downgrade to appear
    5. Optionally syncs webhook events for test clock synchronization
    6. Verifies the downgrade result

    Args:
        client: RAGFlowClient instance for API calls
        tenant_id: RAGFlow tenant ID
        customer_id: Stripe customer ID
        subscription_id: Stripe subscription ID (for tracking purposes)
        webhook_secret: Webhook secret for signing events (optional)
        created_gte: Timestamp for webhook replay filtering (optional)
        webhook_wait_seconds: Seconds to wait after webhook replay
        webhook_timeout_seconds: Timeout for waiting for plan change

    Returns:
        Dictionary with downgrade result including updated subscription info

    Raises:
        FlowError: If any step in the downgrade process fails
    """
    if not subscription_id:
        raise FlowError("subscription_id is required for downgrade")

    # Step 1: Get the Starter plan price ID from config
    print("  Loading Starter plan price ID from config")
    billing_config = load_billing_config()
    starter_price_id = first_plan_price_id(billing_config, "Starter")
    if not starter_price_id:
        raise FlowError("Starter plan price_id not found in service_conf.yaml")
    print(f"  ✅ Starter plan price ID: {starter_price_id[:20]}...")

    # Step 2: Call server API to schedule plan change (PLAN-01 pattern)
    # This sends POST /billing/checkout to the server, which handles Stripe interaction
    print("  Scheduling Pro -> Starter downgrade via server API")
    checkout_result = client.schedule_plan_change(tenant_id, starter_price_id)
    scheduled_change = extract_scheduled_change(checkout_result)
    if not scheduled_change.get("schedule_id"):
        raise FlowError(f"expected schedule_id for downgrade to Starter, got: {checkout_result}")
    print(f"  ✅ Downgrade scheduled via server, schedule_id: {scheduled_change.get('schedule_id')}")

    # Step 3: Wait for pending downgrade to appear in current_plan
    print("  Waiting for pending downgrade to appear")
    pending_plan = wait_for_pending_downgrade(client, "Starter", webhook_timeout_seconds)
    if pending_plan.get("plan_name") != "Pro":
        raise FlowError(f"plan changed prematurely after scheduling downgrade to Starter: expected 'Pro', got {pending_plan.get('plan_name')}")
    print("  ✅ Pending downgrade confirmed: current=Pro, pending=Starter")

    # Step 4: Sync webhook events if webhook_secret provided (for test clock sync)
    if webhook_secret and created_gte:
        print("  Replaying webhook events for synchronization")
        subscription_ids = {subscription_id}
        replayed = replay_stripe_events(
            client,
            webhook_secret=webhook_secret,
            customer_id=customer_id,
            subscription_ids=subscription_ids,
            created_gte=created_gte,
        )
        time.sleep(webhook_wait_seconds)
        print(f"  ✅ Webhook events replayed: {replayed} events")

    # Step 5: Verify the downgrade result
    print("  Verifying downgrade result")
    current_plan = client.current_plan()
    plan_name = current_plan.get("plan_name", "")

    # After scheduling, plan should still be Pro (downgrade happens at period end)
    if plan_name != "Pro":
        raise FlowError(f"Downgrade verification failed: expected Pro plan (pending), got {plan_name}")

    print("  ✅ Downgrade from Pro to Starter scheduled successfully (will apply at period end)")

    return {
        "downgraded": False,  # Not yet applied, scheduled for period end
        "scheduled": True,
        "subscription_id": subscription_id,
        "schedule_id": scheduled_change.get("schedule_id"),
        "old_plan_name": "Pro",
        "new_plan_name": "Starter",
        "pending": True,
        "current_plan": current_plan,
    }


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


def add_storage_to_subscription(
        subscription_id: str,
        storage_price_id: str,
        storage_quantity_gb: int,
) -> dict:
    """
    Add storage addon as an item to an existing subscription.

    This is the new model: storage addon is added to the plan subscription
    as a separate line item, not as a separate subscription.

    Args:
        subscription_id: The existing plan subscription ID
        storage_price_id: Stripe price ID for storage
        storage_quantity_gb: Storage quantity in GB

    Returns:
        Updated subscription dict
    """
    # Add storage item to existing subscription
    subscription_item = stripe.SubscriptionItem.create(
        subscription=subscription_id,
        price=storage_price_id,
        quantity=storage_quantity_gb,
    )

    print(f"  Info: Added storage item to subscription: {subscription_item.id}")

    # Retrieve the updated subscription
    updated_subscription = stripe.Subscription.retrieve(
        subscription_id,
        expand=["latest_invoice.payment_intent", "items.data.price"],
    )

    return stripe_dict(updated_subscription)


def add_storage_to_subscription_with_webhook(
        client: "RAGFlowClient",
        tenant_id: str,
        storage_quantity_gb: int,
        *,
        webhook_secret: str = "",
        customer_id: str = "",
        subscription_ids: set[str] | None = None,
        created_gte: int = 0,
) -> dict[str, Any]:
    """
    Add storage addon to an existing subscription via the backend API with webhook synchronization.

    This method:
    1. Calls the backend /billing/storage/set-target API to add storage
    2. Sends webhook events for synchronization (customer.subscription.updated, invoice.paid)
    3. Optionally replays additional webhook events for test clock sync
    4. Verifies the storage addon was added correctly

    Args:
        client: RAGFlowClient instance for API calls and webhook delivery
        tenant_id: The tenant ID to add storage for
        storage_quantity_gb: Storage quantity in GB to add
        webhook_secret: Webhook secret for signing events (optional)
        customer_id: Stripe customer ID for webhook replay filtering (optional)
        subscription_ids: Set of subscription IDs for webhook replay filtering (optional)
        created_gte: Timestamp for webhook replay filtering (optional)

    Returns:
        Dictionary with the result including:
        - tenant_id: The tenant ID
        - storage_quantity_gb: The added storage quantity
        - target_quantity_bytes: The target quantity in bytes
        - addon_storage_bytes: The effective addon storage in bytes

    Raises:
        FlowError: If storage addition or verification fails
    """
    if not tenant_id:
        raise FlowError("tenant_id is required for adding storage")
    if storage_quantity_gb <= 0:
        raise FlowError("storage_quantity_gb must be positive")

    target_quantity_bytes = storage_quantity_gb * BYTES_PER_GB

    # Step 1: Call backend API to set storage target
    print(f"  Setting storage target: tenant={tenant_id}, quantity={storage_quantity_gb}GB ({target_quantity_bytes} bytes)")
    try:
        result = client.storage_set_target(tenant_id, target_quantity_bytes)
        print("  ✅ Storage target set via backend API")
    except FlowError as exc:
        raise FlowError(f"Failed to set storage target via backend API: {exc}") from exc

    addon_storage_bytes = result.get("addon_storage_bytes", 0)
    returned_target_bytes = result.get("target_quantity_bytes", 0)

    # Step 2: Replay webhook events if created_gte provided (for test clock sync)
    if webhook_secret and created_gte and customer_id:
        print("  Replaying webhook events for synchronization")
        replay_stripe_events(
            client,
            webhook_secret=webhook_secret,
            customer_id=customer_id,
            subscription_ids=subscription_ids or set(),
            created_gte=created_gte,
        )
        print("  ✅ Webhook events replayed")

    # Step 3: Verify the storage was added correctly
    print("  Verifying storage addition result")
    storage_info = client.storage_current(tenant_id)
    actual_addon_bytes = storage_info.get("addon_storage_bytes", 0)

    if actual_addon_bytes < target_quantity_bytes:
        raise FlowError(
            f"Storage verification failed: expected at least {target_quantity_bytes} bytes, got {actual_addon_bytes} bytes"
        )

    print(f"  ✅ Storage addon verified: {actual_addon_bytes} bytes ({actual_addon_bytes // BYTES_PER_GB}GB)")

    return {
        "tenant_id": tenant_id,
        "storage_quantity_gb": storage_quantity_gb,
        "target_quantity_bytes": returned_target_bytes or target_quantity_bytes,
        "addon_storage_bytes": addon_storage_bytes,
        "redirect_to": result.get("redirect_to", ""),
    }


def replay_until_payment_order_status(
        client,
        *,
        webhook_secret: str,
        customer_id: str,
        subscription_ids: set[str],
        created_gte: int,
        order_id: str,
        expected_status: str,
        timeout_seconds: int,
        wait_seconds: int,
) -> dict[str, Any]:
    """Wait for payment order to reach expected status by replaying Stripe events."""
    deadline = time.time() + timeout_seconds
    last_payment_order: dict[str, Any] = {}
    while time.time() < deadline:
        sync_webhooks(
            client,
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


def setup_starter(
        base_url: str,
        version: str,
        email: str,
        password: str,
        ready_timeout_seconds: int = 180,
        webhook_wait_seconds: int = 8,
        webhook_timeout_seconds: int = 180,
) -> dict[str, Any]:
    """
    创建一个已升级为 Starter 计划的测试环境。

    该方法封装了 billing_plan01_api_flow.py 中 Steps 1-4 的逻辑，
    提供一个可复用的方法来快速设置测试环境。

    流程：
    1. 验证环境并加载配置
    2. 创建 Stripe test clock 并注册测试用户
    3. 验证初始 Trial 计划状态
    4. 从 Trial 升级到 Starter 计划

    Args:
        base_url: RAGFlow API 基础 URL
        version: API 版本
        email: 测试用户邮箱
        password: 测试用户密码
        ready_timeout_seconds: 等待 API 就绪的超时时间
        webhook_wait_seconds: 等待 webhook 交付的时间
        webhook_timeout_seconds: 等待 webhook 处理的超时时间

    Returns:
        包含所有必要上下文的字典：
        - client: RAGFlowClient 实例
        - tenant_id: 租户 ID
        - user_id: 用户 ID
        - customer_id: Stripe 客户 ID
        - subscription_id: Starter 订阅 ID
        - clock_id: Stripe test clock ID
        - webhook_secret: Webhook 密钥
        - starter_price_id: Starter 计划价格 ID
    """
    # Step 1: 验证环境并加载配置
    print("=" * 80)
    print("Setup: Validate environment and load configuration")
    print("=" * 80)

    stripe_api_key = env("BILLING_STRIPE_API_KEY", env("STRIPE_API_KEY"))
    if not stripe_api_key:
        raise FlowError("BILLING_STRIPE_API_KEY or STRIPE_API_KEY is required")
    print("  Assert: Stripe API key is set")

    billing_config = load_billing_config()

    runtime = load_storage_runtime_config()
    stripe.api_key = runtime["stripe_api_key"]
    stripe.api_version = runtime["stripe_api_version"]
    webhook_secret = runtime["webhook_secret"]
    print("  Assert: Runtime config loaded successfully")

    starter_price_id = first_plan_price_id(billing_config, "Starter")
    if not starter_price_id:
        raise FlowError("Starter plan price_id not found in service_conf.yaml")
    print(f"  Assert: Starter plan price_id found: {starter_price_id[:20]}...")

    # Step 2: 创建 Stripe test clock 并注册测试用户
    print("\n" + "=" * 80)
    print("Setup: Create Stripe test clock and register test user")
    print("=" * 80)

    test_clock = stripe.test_helpers.TestClock.create(
        frozen_time=int(time.time()),
        name=f"setup-starter-{uuid.uuid4().hex[:8]}",
    )
    clock_id = test_clock.id
    wait_for_clock(clock_id)
    print(f"  Assert: Stripe test clock created: {clock_id}")

    client = RAGFlowClient(base_url, version, clock_id=clock_id)
    client.wait_until_ready(ready_timeout_seconds)
    user_id, tenant_id = client.register_and_login(email, password)
    print(f"  Assert: Test user registered: {email}")
    print(f"  Assert: Tenant ID: {tenant_id}")

    customer_id = create_clock_customer(email, tenant_id, clock_id)
    ensure_billing_subscription(tenant_id, customer_id)
    print(f"  Assert: Stripe customer created: {customer_id}")

    # Step 3: 验证初始 Trial 计划状态
    print("\n" + "=" * 80)
    print("Setup: Verify initial Trial plan state")
    print("=" * 80)

    initial_plan = client.current_plan()
    plan_name = initial_plan.get("plan_name", "Trial")
    initial_subscription_id = initial_plan.get("subscription_id", "")
    print(f"  Assert: Trial subscription ID: {initial_subscription_id}")

    if plan_name != "Trial":
        raise FlowError(f"expected Trial plan initially, got {plan_name}")
    print("  Assert: Initial plan is Trial")

    # Step 4: 从 Trial 升级到 Starter 计划
    print("\n" + "=" * 80)
    print("Setup: Upgrade from Trial to Starter plan")
    print("=" * 80)

    pm_id = attach_default_test_card(customer_id)
    print(f"  Assert: Test card attached: {pm_id}")

    starter_checkout_started_at = int(time.time()) - 5
    checkout_result = client.schedule_plan_change(tenant_id, starter_price_id)

    invoice_id = checkout_result.get("invoice_id", "")
    subscription_id_from_result = checkout_result.get("subscription_id", "")

    print("  Assert: Direct upgrade - subscription already paid")
    starter_subscription_id = subscription_id_from_result
    print(f"  Assert: Starter subscription upgraded: {starter_subscription_id}")

    print("  Assert: Subscription upgraded in place")

    since_upgrade = starter_checkout_started_at
    latest_invoice = stripe.Invoice.retrieve(invoice_id, expand=["payment_intent"])
    latest_invoice_id = invoice_id
    print(f"  Assert: Invoice ID: {latest_invoice_id}")

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
    print("  Assert: Invoice.paid webhook posted")


    sync_webhooks(
        client,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
        created_gte=since_upgrade,
        wait_seconds=webhook_wait_seconds,
    )
    print("  Assert: Webhooks synced for plan upgrade")

    wait_for_plan(client, "Starter", webhook_timeout_seconds)
    print("  Assert: Plan upgraded to Starter")

    print("\n" + "=" * 80)
    print("Setup complete: Starter plan ready")
    print("=" * 80)

    return {
        "client": client,
        "tenant_id": tenant_id,
        "user_id": user_id,
        "customer_id": customer_id,
        "subscription_id": starter_subscription_id,
        "clock_id": clock_id,
        "webhook_secret": webhook_secret,
        "starter_price_id": starter_price_id,
    }

def upgrade_starter_to_pro(
        client: "RAGFlowClient",
        tenant_id: str,
        customer_id: str,
        starter_subscription_id: str,
        *,
        webhook_secret: str = "",
        webhook_wait_seconds: int = 8,
        webhook_timeout_seconds: int = 180,
) -> dict[str, Any]:
    """
    Upgrade a user's Starter subscription to the Pro plan via server API,
    with manual webhook injection to ensure immediate state transition.
    """
    if not starter_subscription_id:
        raise FlowError("starter_subscription_id is required for upgrade")

    # 1. Load Pro price ID
    print("  Loading Pro plan price ID from config")
    billing_config = load_billing_config()
    pro_price_id = first_plan_price_id(billing_config, "Pro")
    if not pro_price_id:
        raise FlowError("Pro plan price_id not found in service_conf.yaml")
    print(f"  ✅ Pro plan price ID: {pro_price_id[:20]}...")

    # 2. Ensure payment method (test card)
    pm_id = attach_default_test_card(customer_id)
    print(f"  Assert: Test card attached: {pm_id}")

    # 3. Call server API to perform the upgrade
    print("  Scheduling upgrade to Pro via server API")
    upgrade_started_at = int(time.time()) - 5
    checkout_result = client.schedule_plan_change(tenant_id, pro_price_id)

    invoice_id = checkout_result.get("invoice_id", "")
    subscription_id = checkout_result.get("subscription_id") or starter_subscription_id
    plan_name = checkout_result.get("plan_name", "")
    if plan_name != "Pro":
        raise FlowError(
            f"Upgrade to Pro failed: expected plan_name='Pro', got plan_name='{plan_name}'. "
            f"Full response: {checkout_result}"
        )
    if not subscription_id:
        raise FlowError(f"Upgrade response missing subscription_id: {checkout_result}")
    print(f"  ✅ Upgrade submitted, plan_name={plan_name}, subscription_id={subscription_id}")

    # 4. Manually construct and send invoice.paid webhook（完全仿照 setup_starter）
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
    print("  Assert: Invoice.paid webhook posted")

    # 6. Sync webhooks for test clock consistency
    print("  Replaying webhook events for synchronization")
    subscription_ids = {starter_subscription_id}
    sync_webhooks(
        client,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=upgrade_started_at,
        wait_seconds=webhook_wait_seconds,
    )
    print("  ✅ Webhook events replayed")

    # 7. Wait for plan to actually become Pro
    print("  Waiting for plan to become Pro")
    current_plan = wait_for_plan(client, "Pro", webhook_timeout_seconds)
    final_plan_name = current_plan.get("plan_name", "")
    if final_plan_name != "Pro":
        raise FlowError(f"Plan did not switch to Pro: expected 'Pro', got '{final_plan_name}'")
    print("  ✅ Plan is now Pro")

    # 8. Verify Pro quotas
    print("  Verifying Pro quotas")
    overview_pro = client.plan_overview()
    apps_limit_pro = overview_pro.get("resources", {}).get("apps", {}).get("limit", 0)
    expected_pro_apps = get_pro_quota_apps()
    if apps_limit_pro != expected_pro_apps:
        raise FlowError(
            f"after Pro upgrade, expected Pro apps quota {expected_pro_apps}, got {apps_limit_pro}"
        )
    print(f"  ✅ Pro apps quota verified: {apps_limit_pro}")
    print("  ✅ Upgrade from Starter to Pro completed successfully")

    return {
        "upgraded": True,
        "scheduled": False,
        "pro_subscription_id": subscription_id,
        "subscription_id": subscription_id,
        "old_plan_name": "Starter",
        "new_plan_name": "Pro",
        "current_plan": current_plan,
    }


def upgrade_trial_to_starter(
        client: "RAGFlowClient",
        tenant_id: str,
        customer_id: str,
        trial_subscription_id: str,
        *,
        webhook_secret: str = "",
        webhook_wait_seconds: int = 8,
        webhook_timeout_seconds: int = 180,
) -> dict[str, Any]:
    """
    Upgrade a user's Trial subscription to the Starter plan via server API,
    with manual webhook injection to ensure immediate state transition.
    """
    if not trial_subscription_id:
        raise FlowError("trial_subscription_id is required for upgrade")

    # 1. Load Starter price ID
    print("  Loading Starter plan price ID from config")
    billing_config = load_billing_config()
    starter_price_id = first_plan_price_id(billing_config, "Starter")
    if not starter_price_id:
        raise FlowError("Starter plan price_id not found in service_conf.yaml")
    print(f"  ✅ Starter plan price ID: {starter_price_id[:20]}...")

    # 2. Ensure payment method (test card)
    pm_id = attach_default_test_card(customer_id)
    print(f"  Assert: Test card attached: {pm_id}")

    # 3. Call server API to perform the upgrade
    print("  Scheduling upgrade to Starter via server API")
    created_gte = int(time.time()) - 5
    checkout_result = client.schedule_plan_change(tenant_id, starter_price_id)

    invoice_id = checkout_result.get("invoice_id", "")
    subscription_id = checkout_result.get("subscription_id") or trial_subscription_id
    plan_name = checkout_result.get("plan_name", "")
    if plan_name != "Starter":
        raise FlowError(
            f"Upgrade to Starter failed: expected plan_name='Starter', got plan_name='{plan_name}'. "
            f"Full response: {checkout_result}"
        )
    if not subscription_id:
        raise FlowError(f"Upgrade response missing subscription_id: {checkout_result}")
    print(f"  ✅ Upgrade submitted, plan_name={plan_name}, subscription_id={subscription_id}")

    # 4. Manually send invoice.paid webhook
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
    print("  Assert: Invoice.paid webhook posted")


    # 6. Sync webhooks for test clock consistency
    print("  Replaying webhook events for synchronization")
    subscription_ids = {trial_subscription_id}
    sync_webhooks(
        client,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=created_gte,
        wait_seconds=webhook_wait_seconds,
    )
    print("  ✅ Webhook events replayed")

    # 7. Wait for plan to become Starter
    print("  Waiting for plan to become Starter")
    current_plan = wait_for_plan(client, "Starter", webhook_timeout_seconds)
    final_plan_name = current_plan.get("plan_name", "")
    if final_plan_name != "Starter":
        raise FlowError(f"Plan did not switch to Starter: expected 'Starter', got '{final_plan_name}'")
    print("  ✅ Plan is now Starter")

    # 8. Verify Starter quotas
    print("  Verifying Starter quotas")
    overview_starter = client.plan_overview()
    apps_limit_starter = overview_starter.get("resources", {}).get("apps", {}).get("limit", 0)
    expected_starter_apps = get_starter_quota_apps()
    if apps_limit_starter != expected_starter_apps:
        raise FlowError(
            f"after Starter upgrade, expected Starter apps quota {expected_starter_apps}, got {apps_limit_starter}"
        )
    print(f"  ✅ Starter apps quota verified: {apps_limit_starter}")

    print("  ✅ Upgrade from Trial to Starter completed successfully")

    return {
        "upgraded": True,
        "scheduled": False,
        "starter_subscription_id": subscription_id,
        "subscription_id": subscription_id,
        "old_plan_name": "Trial",
        "new_plan_name": "Starter",
        "current_plan": current_plan,
    }