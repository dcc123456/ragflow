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
Pure API driver for docs/develop/billing-manual-test-flows.md PLAN-01.

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
  BILLING_WEBHOOK_SECRET or STRIPE_WEBHOOK_SECRET (manual webhook mode only)

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
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import requests
import stripe
import yaml

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from api.db.db_models import DB  # noqa: E402
from api.db.services.billing_service import SubscriptionService  # noqa: E402
from api.utils.crypt import crypt  # noqa: E402

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


class FlowError(RuntimeError):
    pass


class RAGFlowClient:
    def __init__(self, base_url: str, version: str, clock_id: str):
        self.base_url = base_url.rstrip("/")
        self.version = version.strip("/")
        self.clock_id = clock_id
        self.session = requests.Session()
        self.auth_header = ""

    def url(self, path: str) -> str:
        return f"{self.base_url}/{self.version}/{path.lstrip('/')}"

    def headers(self, *, auth: bool = True) -> dict[str, str]:
        headers = {TEST_CLOCK_HEADER: self.clock_id}
        if auth and self.auth_header:
            headers["Authorization"] = self.auth_header
        return headers

    def request_json(self, method: str, path: str, *, auth: bool = True, **kwargs) -> dict[str, Any]:
        response = self.session.request(method, self.url(path), headers=self.headers(auth=auth), timeout=60, **kwargs)
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(f"{method} {path} returned non-JSON status={response.status_code}: {response.text[:500]}") from exc
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

    def register_and_login(self, email: str, password: str) -> str:
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
        return login_data["data"]["id"]

    def current_plan(self) -> dict[str, Any]:
        return self.request_json("GET", "/billing/current_plan")["data"]

    def plan_overview(self) -> dict[str, Any]:
        return self.request_json("GET", "/billing/plan_overview")["data"]

    def spend_history(self) -> list[dict[str, Any]]:
        return self.request_json("GET", "/billing/spend_overview")["data"]

    def schedule_plan_change(self, tenant_id: str, price_id: str) -> dict[str, Any]:
        payload = {
            "tenant_id": tenant_id,
            "payment_type": "subscription",
            "subscription_price_id": price_id,
            "quantity": 1,
        }
        return self.request_json("POST", "/billing/checkout", json=payload)["data"]

    def post_signed_webhook(self, event: dict[str, Any], webhook_secret: str) -> None:
        payload = json.dumps(event, separators=(",", ":"), sort_keys=False)
        timestamp = str(int(time.time()))
        signed_payload = f"{timestamp}.{payload}".encode("utf-8")
        signature = hmac.new(webhook_secret.encode("utf-8"), signed_payload, hashlib.sha256).hexdigest()
        headers = {
            "Stripe-Signature": f"t={timestamp},v1={signature}",
            "Content-Type": "application/json",
        }
        response = self.session.post(self.url("/billing/webhook"), data=payload, headers=headers, timeout=60)
        if response.status_code >= 400:
            raise FlowError(f"webhook {event.get('type')} failed: status={response.status_code} body={response.text[:500]}")


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


def attach_default_test_card(customer_id: str) -> None:
    payment_method = stripe.PaymentMethod.create(type="card", card={"token": "tok_visa"})
    stripe.PaymentMethod.attach(payment_method.id, customer=customer_id)
    stripe.Customer.modify(customer_id, invoice_settings={"default_payment_method": payment_method.id})


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


def create_paid_subscription(customer_id: str, tenant_id: str, price_id: str, product_name: str) -> str:
    before = int(time.time()) - 5
    subscription = stripe.Subscription.create(
        customer=customer_id,
        items=[{"price": price_id, "quantity": 1}],
        metadata={
            "price_type": "subscription",
            "tenant_id": tenant_id,
            "price_id": price_id,
            "product_name": product_name,
        },
        payment_behavior="error_if_incomplete",
        expand=["latest_invoice"],
    )
    return stripe_dict(subscription)["id"], before


def schedule_stripe_price_change_at_period_end(subscription_id: str, target_price_id: str) -> dict[str, Any]:
    subscription = stripe.Subscription.retrieve(subscription_id)
    subscription_dict = stripe_dict(subscription)
    items = subscription_dict.get("items", {}).get("data", [])
    if not items:
        raise FlowError(f"subscription {subscription_id} has no items")

    current_item = items[0]
    current_price_id = current_item["price"]["id"]
    current_quantity = current_item.get("quantity", 1)

    schedule_id = (subscription_dict.get("schedule") or "").strip()
    if schedule_id:
        schedule = stripe.SubscriptionSchedule.retrieve(schedule_id)
        schedule_dict = stripe_dict(schedule)
    else:
        schedule = stripe.SubscriptionSchedule.create(from_subscription=subscription_id)
        schedule_dict = stripe_dict(schedule)

    phases = schedule_dict.get("phases", []) or []
    current_phase = schedule_dict.get("current_phase") or {}
    phase_start = current_phase.get("start_date") or (phases[0].get("start_date") if phases else None) or schedule_dict.get("start_date") or subscription_dict.get("current_period_start")
    phase_end = current_phase.get("end_date") or (phases[0].get("end_date") if phases else None) or subscription_dict.get("current_period_end")
    if not phase_start or not phase_end:
        raise FlowError(f"subscription {subscription_id} is missing schedule period boundaries")

    updated = stripe.SubscriptionSchedule.modify(
        schedule_dict["id"],
        end_behavior="release",
        phases=[
            {
                "start_date": phase_start,
                "end_date": phase_end,
                "items": [{"price": current_price_id, "quantity": current_quantity}],
            },
            {
                "start_date": phase_end,
                "items": [{"price": target_price_id, "quantity": current_quantity}],
            },
        ],
    )
    return {
        "schedule_id": stripe_dict(updated)["id"],
        "current_price_id": current_price_id,
        "target_price_id": target_price_id,
        "effective_at": phase_end,
    }


def run_flow(args: argparse.Namespace) -> None:
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
    tenant_id = client.register_and_login(email, args.password)

    subscription_ids: set[str] = set()
    initial_plan = assert_plan(client, "Trial")
    customer_id = create_clock_customer(email, tenant_id, clock_id)
    bind_local_subscription_customer(tenant_id, customer_id)
    if initial_plan.get("subscription_id"):
        subscription_ids.add(initial_plan["subscription_id"])
    attach_default_test_card(customer_id)

    history_before_pro_create = client.spend_history()
    pro_subscription_id, since = create_paid_subscription(customer_id, tenant_id, required["BILLING_PRICE_ID_PRO"], "Pro")
    subscription_ids.add(pro_subscription_id)
    sync_webhooks(
        client,
        mode=args.webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=since,
        wait_seconds=args.webhook_wait_seconds,
    )
    pro_plan = wait_for_plan(client, "Pro", args.webhook_timeout_seconds)
    wait_for_history_count(client, len(history_before_pro_create) + 1, args.webhook_timeout_seconds, "Pro initial payment")

    history_before_pro_renewal = client.spend_history()
    advance_clock(clock_id, parse_plan_end(pro_plan) + 120)
    settle_latest_subscription_invoice(clock_id, pro_subscription_id)
    sync_webhooks(
        client,
        mode=args.webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=since,
        wait_seconds=args.webhook_wait_seconds,
    )
    pro_plan = wait_for_plan(client, "Pro", args.webhook_timeout_seconds)
    wait_for_history_count(client, len(history_before_pro_renewal) + 1, args.webhook_timeout_seconds, "Pro renewal")

    scheduled = schedule_stripe_price_change_at_period_end(pro_subscription_id, required["BILLING_PRICE_ID_STARTER"])
    if not scheduled.get("schedule_id"):
        raise FlowError(f"expected scheduled Pro -> Starter downgrade, got {scheduled}")
    history_before_starter_renewal = client.spend_history()
    advance_clock(clock_id, parse_plan_end(pro_plan) + 120)
    settle_latest_subscription_invoice(clock_id, pro_subscription_id)
    sync_webhooks(
        client,
        mode=args.webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=since,
        wait_seconds=args.webhook_wait_seconds,
    )
    starter_plan = wait_for_plan(client, "Starter", args.webhook_timeout_seconds)
    wait_for_history_count(client, len(history_before_starter_renewal) + 1, args.webhook_timeout_seconds, "Starter renewal")

    scheduled = schedule_stripe_price_change_at_period_end(pro_subscription_id, required["BILLING_PRICE_ID_TRIAL"])
    if not scheduled.get("schedule_id"):
        raise FlowError(f"expected scheduled Starter -> Trial downgrade, got {scheduled}")
    history_before_trial = client.spend_history()
    advance_clock(clock_id, parse_plan_end(starter_plan) + 120)
    settle_latest_subscription_invoice(clock_id, pro_subscription_id)
    sync_webhooks(
        client,
        mode=args.webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=since,
        wait_seconds=args.webhook_wait_seconds,
    )
    wait_for_plan(client, "Trial", args.webhook_timeout_seconds)
    history_after_trial = client.spend_history()
    if len(history_after_trial) > len(history_before_trial):
        new_rows = history_after_trial[: len(history_after_trial) - len(history_before_trial)]
        paid_rows = [row for row in new_rows if float(row.get("amount", 0) or 0) > 0]
        if paid_rows:
            raise FlowError(f"Trial renewal created paid rows: {paid_rows}")

    starter_subscription_id, since_starter = create_paid_subscription(customer_id, tenant_id, required["BILLING_PRICE_ID_STARTER"], "Starter")
    subscription_ids.add(starter_subscription_id)
    sync_webhooks(
        client,
        mode=args.webhook_mode,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=since_starter,
        wait_seconds=args.webhook_wait_seconds,
    )
    wait_for_plan(client, "Starter", args.webhook_timeout_seconds)

    overview = client.plan_overview()
    history = client.spend_history()
    print(
        json.dumps(
            {
                "tenant_id": tenant_id,
                "email": email,
                "test_clock_id": clock_id,
                "customer_id": customer_id,
                "final_plan": overview.get("plan_name"),
                "history_rows": len(history),
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
