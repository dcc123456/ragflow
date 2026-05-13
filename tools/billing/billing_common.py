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

import argparse
import json
import hashlib
import os
import time
import uuid
from datetime import datetime, timezone

from api.utils.crypt import crypt
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import requests
import stripe  # type: ignore[reportMissingImports]
import yaml
import hmac

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

def _event_matches_customer(event: dict[str, Any], customer_id: str, subscription_ids: set[str]) -> bool:
    obj = event.get("data", {}).get("object", {}) or {}
    if obj.get("customer") == customer_id:
        return True
    subscription = obj.get("subscription")
    if isinstance(subscription, str) and subscription in subscription_ids:
        return True
    if obj.get("id") in subscription_ids and obj.get("object") == "subscription":
        return True
    return False

class RAGFlowClient:
    """HTTP client for RAGFlow billing APIs used by storage flows."""

    def __init__(self, base_url: str, version: str, clock_id: str, webhook_secret: str, mode:str):
        self.base_url = base_url.rstrip("/")
        self.version = version.strip("/")
        self.clock_id = clock_id
        self.session = requests.Session()
        self.session.trust_env = False
        self.auth_header = ""
        self.webhook_secret = webhook_secret
        self.mode = mode
        print(f"------------RAGFlowClient mode:{mode}")


    def __del__(self):
        if self.clock_id:
            print(f" delete clock:{self.clock_id}")
            delete_clock(clock_id=self.clock_id)

    def wait_for_plan(self, expected: str, timeout_seconds: int) -> dict[str, Any]:
        deadline = time.time() + timeout_seconds
        last_plan = {}
        while time.time() < deadline:
            last_plan = self.current_plan()
            if last_plan.get("plan_name") == expected:
                return last_plan
            time.sleep(1)
        raise FlowError(f"timed out waiting for plan {expected}, last plan: {last_plan}")


    def wait_for_storage_status(
            self,
            tenant_id: str,
            expected_status: str,
            timeout_seconds: int = 30,
    ) -> dict[str, Any]:
        """Wait for storage subscription to reach the specified status."""
        deadline = time.time() + timeout_seconds
        last_storage = {}
        while time.time() < deadline:
            last_storage = self.storage_current(tenant_id)
            status = last_storage.get("status", "")
            if status == expected_status:
                return last_storage
            print(f"-----sleep 1 seconds, waiting for storage status to be {expected_status}, current: {status}")
            time.sleep(1)
        raise FlowError(f"timed out waiting for storage status {expected_status}, last: {last_storage}")

    def wait_for_history_count(self, minimum_count: int, timeout_seconds: int, label: str) -> list[dict[str, Any]]:
        """Wait until billing history has at least minimum_count rows."""
        deadline = time.time() + timeout_seconds
        last_history: list[dict[str, Any]] = []
        while time.time() < deadline:
            last_history = self.spend_history()
            if len(last_history) >= minimum_count:
                return last_history
            time.sleep(3)
        raise FlowError(f"timed out waiting for {label} billing history row, last count: {len(last_history)}")

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

    def storage_set_target(self, tenant_id: str, target_storage_bytes: int) -> dict[str, Any]:
        return self.request_json(
            "POST",
            "/billing/storage/set-target",
            json={"tenant_id": tenant_id, "target_storage_bytes": target_storage_bytes},
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

    def cancel_scheduled_change(self, tenant_id: str) -> dict[str, Any]:
        """Cancel a pending scheduled subscription change."""
        payload = {"tenant_id": tenant_id}
        return self.request_json("POST", "/billing/cancel-scheduled-subscription-change", json=payload)["data"]

    def post_signed_webhook(self, event: dict[str, Any]) -> None:
        payload = json_dumps_compact(event)
        timestamp = str(int(time.time()))
        signed_payload = f"{timestamp}.{payload}".encode("utf-8")
        signature = hmac.new(self.webhook_secret.encode("utf-8"), signed_payload, hashlib.sha256).hexdigest()
        headers = {
            "Stripe-Signature": f"t={timestamp},v1={signature}",
            "Content-Type": "application/json",
        }
        response = self.session.post(self.url("/billing/webhook"), data=payload, headers=headers, timeout=60)
        ensure_webhook_delivery_success(response, str(event.get("type") or "unknown"))

    def post_invoice_paid_event(self, invoice_id:str):
        latest_invoice = stripe.Invoice.retrieve(invoice_id, expand=["payment_intent"])
        invoice_dict = stripe_dict(latest_invoice)
        invoice_paid_event = {
            "id": f"evt_manual_invoice_{uuid.uuid4().hex[:20]}",
            "object": "event",
            "type": "invoice.paid",
            "api_version": stripe.api_version,
            "created": int(time.time()),
            "data": {"object": invoice_dict},
            "livemode": False,
            "pending_webhooks": 0,
        }

        self.post_signed_webhook(invoice_paid_event)

    def sync_webhooks(self,
            customer_id: str,
            subscription_ids: set[str],
            created_gte: int,
            wait_seconds = 10,
    ) -> int:
        print(f"-------sleep {wait_seconds} seconds")
        time.sleep(wait_seconds)
        """Synchronize webhook events: manual mode replays from test clock; auto mode just waits."""
        if self.mode != "manual":
            print(f"-------self.mode is {self.mode}, not manual, ignore")
            return 0
        return self._replay_stripe_events(
            customer_id=customer_id,
            subscription_ids=subscription_ids,
            created_gte=created_gte,
        )

    def _replay_stripe_events(self,
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
            if not _event_matches_customer(event, customer_id, subscription_ids):
                continue
            self.post_signed_webhook(event)
            obj = event.get("data", {}).get("object", {}) or {}
            subscription = obj.get("subscription")
            print(f"-------event type:{event.get('type')}, customer:{obj.get("customer")}, subscription:{subscription}")
            # if event.get("type") == "invoice.paid":
                # print(f"--------invoice paid event:{event}")
            replayed += 1
        return replayed


    def advance_clock_to_plan_end(
            self,
            offset_seconds: int = 86400,
    ) -> int:
        """Advance Stripe test clock to after the current plan's period end.

        This method retrieves the current plan's end_time, calculates the target
        timestamp by adding the offset, and advances the test clock to that time.

        Args:
            offset_seconds: Seconds to add after plan end (default: 120)

        Returns:
            The target timestamp the clock was advanced to

        Raises:
            FlowError: If plan end_time is missing or clock advance fails
        """
        current_plan = self.current_plan()
        plan_end = current_plan.get("end_time")
        if not plan_end:
            raise FlowError(f"plan response is missing end_time: {current_plan}")

        if isinstance(plan_end, (int, float)):
            plan_end_ts = int(plan_end)
        else:
            plan_end_str = str(plan_end).replace("Z", "+00:00")
            plan_end_dt = datetime.fromisoformat(plan_end_str)
            if plan_end_dt.tzinfo is None:
                plan_end_dt = plan_end_dt.replace(tzinfo=timezone.utc)
            plan_end_ts = int(plan_end_dt.timestamp())

        print(f"  Info: Advancing clock by {offset_seconds} seconds to after plan end:{plan_end}")
        advance_clock(self.clock_id, plan_end_ts + offset_seconds)
        print(f"  Assert: Clock advanced to after plan end {plan_end}, new end:{plan_end} + {offset_seconds} seconds")

        return plan_end_ts + offset_seconds


    def ensure_invoice_finalized(self, subscription_id: str) -> dict[str, Any] | None:
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
                clock = stripe_dict(stripe.test_helpers.TestClock.retrieve(self.clock_id))
                frozen = int(clock.get("frozen_time", 0))
                if finalize_at and int(finalize_at) > frozen:
                    print(f"[DEBUG] Invoice draft, advancing clock from {frozen} to {finalize_at} for auto-finalize...")
                    advance_clock(self.clock_id, int(finalize_at))
                    continue
                # We're at or past auto-finalize time but still draft; finalize manually
                print("[DEBUG] Invoice still draft after time advance, finalizing manually...")
                try:
                    finalized = stripe.Invoice.finalize_invoice(invoice_dict["id"])
                    return stripe_dict(finalized)
                except Exception as e:
                    print(f"[DEBUG] Finalize failed: {e}")
                    return invoice_dict
            return invoice_dict
        return None


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
    parser.add_argument("--webhook-mode", choices=("manual", "stripe-cli"), default=env("RAGFLOW_BILLING_WEBHOOK_MODE", "manual"),
                        help="manual signs/replays Stripe events itself; stripe-cli waits for `stripe listen --forward-to ...`.",
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


def wait_for_no_pending_downgrade(client: "RAGFlowClient", timeout_seconds: int = 180) -> dict[str, Any]:
    """Wait for pending_subscription_change to disappear."""
    deadline = time.time() + timeout_seconds
    while time.time() < deadline:
        plan = client.current_plan()
        pending = plan.get("pending_subscription_change", {})
        if not pending:
            return plan
        time.sleep(3)
    raise FlowError("timed out waiting for pending downgrade to be canceled")