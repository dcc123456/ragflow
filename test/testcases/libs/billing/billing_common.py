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

This module is the main shared entrypoint for billing test helpers and clients.
"""

from __future__ import annotations

import argparse
import hashlib
import hmac
import json
import logging
import os
import sys
import time
import uuid
import subprocess
from datetime import datetime, timezone

from pathlib import Path
from typing import Any, Type, TypeVar
from urllib.parse import urlparse

import requests
import yaml

logger = logging.getLogger(__name__)

try:
    import stripe  # type: ignore[reportMissingImports]
except ModuleNotFoundError as exc:
    if exc.name == "stripe":
        raise SystemExit(
            "Missing Python dependency 'stripe'. "
            "Create the project env with `uv sync --python 3.12 --all-extras`, "
            "then run billing pytest cases via `pytest test/testcases/test_http_api/test_billing ...` "
            "or `uv run --python 3.12 pytest test/testcases/test_http_api/test_billing ...`."
        ) from exc
    raise

from libs.billing.storage_common import BYTES_PER_GB

TEST_CLOCK_HEADER = "X-Stripe-Test-Clock"
DEFAULT_TEST_PASSWORD = "Test1234!"
DEFAULT_TEST_PASSWORD_ENCRYPTED = (
    "qDF+0dlLDNvrPClNwDmWYD8hKlo45DIDkceUMMdg286iMsGK0nq71Ahvff0alU/pclIVsJAsD8oJuEpj+FAuTdvWkAqJnXpumpLXXfN+vQ5UEvuzyH9/"
    "BjN5M0Qd/udQnfgtwyZMHybu/L6I5IbmkUVqeGvDi1HJH8ivKmbgp3SNZjsvcelxeFdC6/wu/GG8EOIQtNZSFde4z8BDFj+2Zpwn3fb2jTXtn7jJB4r0"
    "7jQJM4vdpNf9q1HtKw5g6jlHKlrzcvSdZn7m0bdoh39RvXA5y7sIiStPNCEzbbjRC8o4/tAP627SW+bQpmS/bnnH7VzqRwvfEBnaLnveh96SWg=="
)
DEFAULT_WEBHOOK_WAIT_SECONDS = 8
DEFAULT_WEBHOOK_TIMEOUT_SECONDS = 180
T = TypeVar("T")

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
            logger.debug("clock ready: %s, time: %s", clock_dict, time.time())
            return clock_dict
        time.sleep(1)
    raise FlowError(f"test clock {clock_id} did not become ready")


class FlowError(RuntimeError):
    """Custom exception for billing flow errors."""
    pass


def env(name: str, default: str = "") -> str:
    """Get environment variable with fallback."""
    return (os.environ.get(name) or default).strip()


def ensure_repo_root() -> None:
    """Compatibility no-op kept for older call sites."""
    return None


def prepare_backend_imports() -> None:
    """Ensure backend package imports resolve to project packages, not test helpers.

    The HTTP API test tree contains a top-level ``common.py`` helper module,
    which can shadow the project ``common`` package and break lazy imports of
    backend modules such as ``api.db``. Before importing backend modules, we
    force ``sys.path`` ordering back to the project root and drop the shadowing
    module if it was loaded as a plain module instead of a package.
    """
    project_root = Path(__file__).resolve().parents[4]
    project_root_str = str(project_root)
    if project_root_str in sys.path:
        sys.path.remove(project_root_str)
    sys.path.insert(0, project_root_str)

    common_mod = sys.modules.get("common")
    if common_mod is not None and not hasattr(common_mod, "__path__"):
        del sys.modules["common"]


def make_test_email(prefix: str) -> str:
    """Generate a unique test email for billing flows."""
    return f"{prefix}-{uuid.uuid4().hex[:12]}@example.test"


def resolve_service_config_path() -> Path:
    """Resolve the service configuration file path on the current filesystem."""
    candidates = [
        Path("/ragflow/conf/service_conf.yaml"),
        Path(env("RAGFLOW_SERVICE_CONF", "")) if env("RAGFLOW_SERVICE_CONF", "") else None,
    ]
    for candidate in candidates:
        if candidate and candidate.exists():
            return candidate
    raise FlowError(
        "service config not found on local filesystem; tried "
        "/ragflow/conf/service_conf.yaml and RAGFLOW_SERVICE_CONF"
    )


def load_service_config() -> dict[str, Any]:
    """Load full service configuration from service_conf.yaml."""
    try:
        config_path = resolve_service_config_path()
        with config_path.open("r", encoding="utf-8") as f:
            config = yaml.safe_load(f) or {}
    except FlowError:
        config = _load_service_config_from_container()
    if not isinstance(config, dict):
        raise FlowError("service config is not a map")
    return config


def _load_service_config_from_container() -> dict[str, Any]:
    """Read service config from a running ragflow container."""
    container_name = env("RAGFLOW_SERVICE_CONTAINER", "docker-ragflow-1")
    try:
        result = subprocess.run(
            [
                "docker",
                "exec",
                container_name,
                "cat",
                "/ragflow/conf/service_conf.yaml",
            ],
            check=True,
            capture_output=True,
            text=True,
        )
    except Exception as exc:
        raise FlowError(
            "service config not found locally and failed to read "
            f"/ragflow/conf/service_conf.yaml from container {container_name}: {exc}"
        ) from exc

    config = yaml.safe_load(result.stdout) or {}
    if not isinstance(config, dict):
        raise FlowError(
            f"service config loaded from container {container_name} is not a map"
        )
    return config


def load_billing_config() -> dict[str, Any]:
    """Load billing configuration from service_conf.yaml."""
    config = load_service_config()
    billing_config = config.get("billing") or {}
    if not isinstance(billing_config, dict):
        raise FlowError("billing config is not a map in service_conf.yaml")
    return billing_config


def default_base_url() -> str:
    """Resolve the default local RAGFlow base URL from service_conf.yaml."""
    service_config = load_service_config()
    ragflow_config = service_config.get("ragflow") or {}
    if not isinstance(ragflow_config, dict):
        return "http://127.0.0.1:9380"
    port = int(ragflow_config.get("http_port") or 9380)
    return f"http://127.0.0.1:{port}"


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


def get_trial_price_id() -> str:
    """Get the Trial plan price ID from billing configuration."""
    return get_plan_price_id("Trial")


def get_starter_price_id() -> str:
    """Get the Starter plan price ID from billing configuration."""
    return get_plan_price_id("Starter")


def get_pro_price_id() -> str:
    """Get the Pro plan price ID from billing configuration."""
    return get_plan_price_id("Pro")


def get_plan_price_id(plan_name: str) -> str:
    """Get the first configured Stripe price ID for a named plan."""
    price_id = first_plan_price_id(load_billing_config(), plan_name)
    if not price_id:
        raise FlowError(f"{plan_name} plan not found in billing configuration")
    return price_id


def get_quota_members_limit(plan_name: str) -> int:
    """Get the member quota limit for a specific plan from billing configuration."""
    config = load_billing_config()
    plans = config.get("billing_plans", [])
    for plan in plans:
        if plan.get("name") == plan_name:
            return int(plan.get("quota_members", 0))
    raise FlowError(f"Plan '{plan_name}' not found in billing configuration")


def stripe_dict(obj: Any) -> dict[str, Any]:
    if hasattr(obj, "to_dict_recursive"):
        return obj.to_dict_recursive()
    if isinstance(obj, dict):
        return obj
    return dict(obj)


def make_default_parser(description: str) -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=description)
    parser.add_argument("--base-url", default=env("RAGFLOW_BASE_URL", default_base_url()))
    parser.add_argument("--version", default=env("RAGFLOW_API_VERSION", "v1"))
    parser.add_argument("--ready-timeout-seconds", type=int, default=int(env("RAGFLOW_READY_TIMEOUT_SECONDS", "60")))
    return parser


def delete_clock(clock_id: str) -> None:
    """Delete Stripe test clock to clean up resources."""
    try:
        stripe.test_helpers.TestClock.delete(clock_id)
        logger.info("Deleted Stripe test clock: %s", clock_id)
    except Exception as exc:
        if "No such billingclock" in str(exc):
            return
        logger.warning("Failed to delete test clock %s: %s", clock_id, exc)


def load_stripe_test_runtime_config(*, require_test_mode_message: str) -> dict[str, Any]:
    """Load shared Stripe runtime config for billing test automation."""
    billing_config = load_billing_config()
    stripe_api_key = env("BILLING_STRIPE_API_KEY", env("STRIPE_API_KEY", str(billing_config.get("stripe_api_key") or "")))
    stripe_api_version = str(billing_config.get("stripe_api_version") or "2026-04-22.dahlia")
    stripe_api_version_override = env("STRIPE_API_VERSION")
    if stripe_api_version_override and stripe_api_version_override != stripe_api_version:
        raise FlowError(
            f"STRIPE_API_VERSION={stripe_api_version_override} does not match service_conf.yaml={stripe_api_version}"
        )
    if not stripe_api_key:
        raise FlowError("billing.stripe_api_key is required in conf/service_conf.yaml")
    if not stripe_api_key.startswith("sk_test_"):
        raise FlowError(require_test_mode_message)

    webhook_secret = env("BILLING_WEBHOOK_SECRET", env("STRIPE_WEBHOOK_SECRET"))
    return {
        "billing_config": billing_config,
        "stripe_api_key": stripe_api_key,
        "stripe_api_version": stripe_api_version,
        "webhook_secret": webhook_secret,
    }


def configure_stripe_runtime(runtime_config: dict[str, Any]) -> None:
    """Apply Stripe API key and version from a loaded runtime config."""
    stripe.api_key = runtime_config["stripe_api_key"]
    stripe.api_version = runtime_config["stripe_api_version"]


def bootstrap_client(client: Any, email: str, password: str = DEFAULT_TEST_PASSWORD) -> None:
    """Register/login the test user and prepare initial Trial billing state."""
    from libs.billing.storage_common import attach_default_test_card

    _user_id, tenant_id = client.register_and_login(email, password)
    logger.info("Assert: Test user registered: %s", email)
    logger.info("Assert: Tenant ID: %s", tenant_id)

    initial_plan = client.current_plan()
    plan_name = initial_plan.get("plan_name", "Trial")
    initial_subscription_id = initial_plan.get("subscription_id", "")
    client.customer_id = str(initial_plan.get("customer_id") or "")
    if not client.customer_id:
        raise FlowError(f"expected customer_id in initial plan response, got {initial_plan}")

    logger.info("Setup: Verify initial Trial plan state")
    logger.info("Assert: Stripe customer created: %s", client.customer_id)
    logger.info("Assert: Trial subscription ID: %s", initial_subscription_id)

    if plan_name != "Trial":
        raise FlowError(f"expected Trial plan initially, got {plan_name}")
    logger.info("Assert: Initial plan is %s", plan_name)

    pm_id = attach_default_test_card(client.customer_id)
    logger.info("Assert: Test card attached: %s", pm_id)


def create_test_clock_client(args: Any, email: str, client_type: type[T]) -> T:
    """Create a billing test client with Stripe runtime configured and a test clock."""
    logger.info("Setup: Validate environment and load configuration")
    runtime = load_stripe_test_runtime_config(
        require_test_mode_message="Billing automation requires a Stripe test-mode secret key"
    )
    logger.info("Assert: Stripe API key is set")
    configure_stripe_runtime(runtime)
    logger.info("Assert: Runtime config loaded successfully")

    logger.info("Setup: Create Stripe test clock and register test user")

    test_clock = stripe.test_helpers.TestClock.create(
        frozen_time=int(time.time()),
        name=f"setup-starter-{uuid.uuid4().hex[:8]}",
    )
    wait_for_clock(test_clock.id)
    logger.info("Assert: Stripe test clock created: %s", test_clock.id)

    client = client_type(args.base_url, args.version, test_clock.id, runtime["webhook_secret"])
    client.wait_until_ready(args.ready_timeout_seconds)
    bootstrap_client(client, email, DEFAULT_TEST_PASSWORD)
    return client


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


def create_client(args: Any, email: str) -> "BillingClient":
    """Create a BillingClient for billing test flows."""
    return create_client_with_type(args, email, client_type=BillingClient)


def create_client_with_type(args: Any, email: str, client_type: Type[T]) -> T:
    """Create an initialized billing test client with a Stripe test clock."""
    return create_test_clock_client(args, email, client_type)


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



class BillingClient:
    """HTTP client for RAGFlow billing APIs used by storage flows."""

    def __init__(self, base_url: str, version: str, clock_id: str, webhook_secret: str):
        self.base_url = base_url.rstrip("/")
        self.version = version.strip("/")
        self.clock_id = clock_id
        self._clock_deleted = False
        self.session = requests.Session()
        self.session.trust_env = False
        self.auth_header = ""
        self.webhook_secret = webhook_secret
        self.user_id = ""
        self.tenant_id = ""
        self.customer_id = ""

    def __del__(self):
        try:
            self.close()
        except Exception:
            pass

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        self.close()
        return False

    def close(self) -> None:
        if self.session:
            self.session.close()
        if self.clock_id and not self._clock_deleted:
            delete_clock(clock_id=self.clock_id)
            self._clock_deleted = True

    def wait_for_plan(self, expected: str) -> dict[str, Any]:
        deadline = time.time() + DEFAULT_WEBHOOK_TIMEOUT_SECONDS
        last_plan = {}
        while time.time() < deadline:
            last_plan = self.current_plan()
            if last_plan.get("plan_name") == expected:
                return last_plan
            time.sleep(1)
        raise FlowError(f"timed out waiting for plan {expected}, last plan: {last_plan}")

    def wait_for_subscription_status(self, expected_status: str) -> dict[str, Any]:
        """Wait until the current plan reflects the expected subscription status."""
        deadline = time.time() + DEFAULT_WEBHOOK_TIMEOUT_SECONDS
        last_plan = {}
        expected = expected_status.lower()
        while time.time() < deadline:
            last_plan = self.current_plan()
            status = str(last_plan.get("subscription_status") or "").lower()
            if status == expected:
                return last_plan
            time.sleep(1)
        raise FlowError(
            f"timed out waiting for subscription status {expected_status}, last plan: {last_plan}"
        )


    def wait_for_storage_status(
            self,
            expected_status: str,
    ) -> dict[str, Any]:
        """Wait for storage subscription to reach the specified status."""
        deadline = time.time() + DEFAULT_WEBHOOK_TIMEOUT_SECONDS
        last_storage = {}
        while time.time() < deadline:
            last_storage = self.storage_current()
            status = last_storage.get("status", "")
            if status == expected_status:
                return last_storage
            logger.debug("waiting for storage status to be %s, current: %s", expected_status, status)
            time.sleep(1)
        raise FlowError(f"timed out waiting for storage status {expected_status}, last: {last_storage}")

    def wait_for_history_count(self, minimum_count: int, label: str) -> list[dict[str, Any]]:
        """Wait until billing history has at least minimum_count rows."""
        deadline = time.time() + DEFAULT_WEBHOOK_TIMEOUT_SECONDS
        last_history: list[dict[str, Any]] = []
        while time.time() < deadline:
            last_history = self.spend_history()
            if len(last_history) >= minimum_count:
                return last_history
            time.sleep(3)
        raise FlowError(f"timed out waiting for {label} billing history row, last count: {len(last_history)}")

    def url(self, path: str, need_api_path:bool=False) -> str:
        if need_api_path:
            return f"{self.base_url}/api/{self.version}/{path.lstrip('/')}"
        else:
            return f"{self.base_url}/{self.version}/{path.lstrip('/')}"

    def billing_url(self, path: str) -> str:
        return f"{self.base_url}/{self.version}/billing/{path.lstrip('/')}"

    def headers(self, *, auth: bool = True) -> dict[str, str]:
        headers: dict[str, str] = {}
        if self.clock_id:
            headers[TEST_CLOCK_HEADER] = self.clock_id
        if auth and self.auth_header:
            headers["Authorization"] = self.auth_header
        return headers

    def request_json(self, method: str, path: str, need_api_path:bool=False, auth: bool = True, **kwargs) -> dict[str, Any]:
        response = self.session.request(method, self.url(path, need_api_path), headers=self.headers(auth=auth), timeout=60, **kwargs)
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
                response = self.session.get(self.billing_url("/status"), headers=self.headers(auth=False), timeout=10)
                if response.status_code == 200 and response.headers.get("content-type", "").startswith("application/json"):
                    response.json()
                    return
                last_error = f"status={response.status_code} body={response.text[:200]}"
            except Exception as exc:
                last_error = str(exc)
            time.sleep(2)
        raise FlowError(f"RAGFlow API did not become ready: {last_error}")

    def register_and_login(self, email: str, password: str) -> tuple[str, str]:
        if password != DEFAULT_TEST_PASSWORD:
            raise FlowError(
                "billing test helper only supports the shared default password; "
                "update the fixed encrypted password mapping before using a new value"
            )
        encrypted_password = DEFAULT_TEST_PASSWORD_ENCRYPTED
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
        self.user_id = user_id
        self.tenant_id = tenant_id
        return user_id, tenant_id

    def current_plan(self) -> dict[str, Any]:
        response = self.session.get(self.billing_url("/subscription"), headers=self.headers(auth=True), timeout=60)
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"GET /billing/subscription returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"GET /billing/subscription failed status={response.status_code}: {payload}")
        return payload["data"]

    def plan_overview(self) -> dict[str, Any]:
        response = self.session.get(self.billing_url("/subscription/overview"), headers=self.headers(auth=True), timeout=60)
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"GET /billing/subscription/overview returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"GET /billing/subscription/overview failed status={response.status_code}: {payload}")
        return payload["data"]

    def storage_current(self) -> dict[str, Any]:
        response = self.session.get(
            self.billing_url(f"/storage?tenant_id={self.tenant_id}"),
            headers=self.headers(auth=True),
            timeout=60,
        )
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"GET /billing/storage returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"GET /billing/storage failed status={response.status_code}: {payload}")
        return payload["data"]

    def upcoming_plan_change(self, new_price_id: str) -> dict[str, Any]:
        response = self.session.post(
            self.billing_url("/subscription/preview"),
            headers=self.headers(auth=True),
            json={"tenant_id": self.tenant_id, "new_price_id": new_price_id},
            timeout=60,
        )
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"POST /billing/subscription/preview returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"POST /billing/subscription/preview failed status={response.status_code}: {payload}")
        return payload["data"]

    def upcoming_storage_change(self, target_storage_bytes: int) -> dict[str, Any]:
        response = self.session.post(
            self.billing_url("/subscription/preview"),
            headers=self.headers(auth=True),
            json={"tenant_id": self.tenant_id, "target_storage_bytes": target_storage_bytes},
            timeout=60,
        )
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"POST /billing/subscription/preview returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"POST /billing/subscription/preview failed status={response.status_code}: {payload}")
        return payload["data"]

    def storage_set_target(self, target_storage_bytes: int, *, setup_intent_id: str = "") -> dict[str, Any]:
        payload: dict[str, Any] = {
            "tenant_id": self.tenant_id,
            "target_storage_bytes": target_storage_bytes,
            "session_success_url": "http://127.0.0.1:9380/billing/checkouts/{session_id}?success=1",
            "session_cancel_url": "http://127.0.0.1:9380/billing/checkouts/?canceled=1",
        }
        if setup_intent_id:
            payload["setup_intent_id"] = setup_intent_id
        response = self.session.patch(
            self.billing_url("/storage"),
            headers=self.headers(auth=True),
            json=payload,
            timeout=60,
        )
        try:
            result = response.json()
        except ValueError as exc:
            raise FlowError(
                f"PATCH /billing/storage returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or result.get("code") not in (0, None):
            raise FlowError(f"PATCH /billing/storage failed status={response.status_code}: {result}")
        return result["data"]

    def create_setup_intent(
            self,
            *,
            setup_type: str,
            price_id: str = "",
            target_storage_bytes: int | None = None,
    ) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "tenant_id": self.tenant_id,
            "setup_type": setup_type,
        }
        if price_id:
            payload["price_id"] = price_id
        if target_storage_bytes is not None:
            payload["target_storage_bytes"] = target_storage_bytes
        response = self.session.post(
            self.billing_url("/setup-intents"),
            headers=self.headers(auth=True),
            json=payload,
            timeout=60,
        )
        try:
            result = response.json()
        except ValueError as exc:
            raise FlowError(
                f"POST /billing/setup-intents returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or result.get("code") not in (0, None):
            raise FlowError(f"POST /billing/setup-intents failed status={response.status_code}: {result}")
        return result["data"]

    def succeed_setup_intent(self, setup_intent_id: str, payment_method_id: str = "pm_card_visa") -> dict[str, Any]:
        if not setup_intent_id:
            raise FlowError("setup_intent_id is required")
        setup_intent = stripe.SetupIntent.confirm(setup_intent_id, payment_method=payment_method_id)
        setup_intent_dict = stripe_dict(setup_intent)
        if setup_intent_dict.get("status") != "succeeded":
            raise FlowError(f"expected SetupIntent to succeed, got {setup_intent_dict}")
        return setup_intent_dict

    def spend_history(self) -> list[dict[str, Any]]:
        response = self.session.get(self.billing_url("/spend_overview"), headers=self.headers(auth=True), timeout=60)
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"GET /billing/spend_overview returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"GET /billing/spend_overview failed status={response.status_code}: {payload}")
        return payload["data"].get("items", [])

    def schedule_plan_change(self, price_id: str, *, setup_intent_id: str = "") -> dict[str, Any]:
        """Initiate a subscription change via checkout (upgrade/downgrade)."""
        payload = {
            "tenant_id": self.tenant_id,
            "payment_type": "subscription",
            "subscription_price_id": price_id,
            "quantity": 1,
            "session_success_url": "http://127.0.0.1:9380/billing/checkouts?success=1",
            "session_cancel_url": "http://127.0.0.1:9380/billing/checkouts/?canceled=1",
        }
        if setup_intent_id:
            payload["setup_intent_id"] = setup_intent_id
        response = self.session.post(
            self.billing_url("/subscription"),
            headers=self.headers(auth=True),
            json=payload,
            timeout=60,
        )
        try:
            result = response.json()
        except ValueError as exc:
            raise FlowError(
                f"POST /billing/subscription returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or result.get("code") not in (0, None):
            raise FlowError(f"POST /billing/subscription failed status={response.status_code}: {result}")
        return result["data"]

    def ensure_setup_intent_for_plan_change(self, target_price_id: str) -> str:
        upcoming = self.upcoming_plan_change(target_price_id)
        if upcoming.get("has_reusable_payment_method", True):
            return ""
        setup_result = self.create_setup_intent(
            setup_type="subscription_upgrade",
            price_id=target_price_id,
        )
        setup_intent_id = str(setup_result.get("setup_intent_id") or "")
        if not setup_intent_id:
            raise FlowError(f"setup-intent response missing setup_intent_id: {setup_result}")
        self.succeed_setup_intent(setup_intent_id)
        return setup_intent_id

    def ensure_setup_intent_for_storage_change(self, target_storage_bytes: int) -> str:
        upcoming = self.upcoming_storage_change(target_storage_bytes)
        if upcoming.get("has_reusable_payment_method", True):
            return ""
        setup_result = self.create_setup_intent(
            setup_type="storage_addon",
            target_storage_bytes=target_storage_bytes,
        )
        setup_intent_id = str(setup_result.get("setup_intent_id") or "")
        if not setup_intent_id:
            raise FlowError(f"setup-intent response missing setup_intent_id: {setup_result}")
        self.succeed_setup_intent(setup_intent_id)
        return setup_intent_id


    def post_signed_webhook(self, event: dict[str, Any]) -> None:
        payload = json_dumps_compact(event)
        timestamp = str(int(time.time()))
        signed_payload = f"{timestamp}.{payload}".encode("utf-8")
        signature = hmac.new(self.webhook_secret.encode("utf-8"), signed_payload, hashlib.sha256).hexdigest()
        headers = {
            "Stripe-Signature": f"t={timestamp},v1={signature}",
            "Content-Type": "application/json",
        }
        response = self.session.post(self.billing_url("/webhooks/stripe"), data=payload, headers=headers, timeout=60)
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
                      subscription_ids: set[str],
                      created_gte: int,
                      wait_seconds: int = DEFAULT_WEBHOOK_WAIT_SECONDS,
                      ) -> int:
        logger.debug("sleeping %d seconds before webhook sync", wait_seconds)
        time.sleep(wait_seconds)
        logger.debug("waiting for forwarded webhooks")
        return 0

    def _replay_stripe_events(self,
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
            if not _event_matches_customer(event, self.customer_id, subscription_ids):
                continue
            self.post_signed_webhook(event)
            obj = event.get("data", {}).get("object", {}) or {}
            subscription = obj.get("subscription")
            logger.debug("event type: %s, customer: %s, subscription: %s", event.get("type"), obj.get("customer"), subscription)
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

        logger.info("Advancing clock by %d seconds to after plan end: %s", offset_seconds, plan_end)
        advance_clock(self.clock_id, plan_end_ts + offset_seconds)
        logger.info("Assert: Clock advanced to after plan end %s, new end: %s", plan_end, plan_end_ts + offset_seconds)

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
                    logger.debug("Invoice draft, advancing clock from %s to %s for auto-finalize...", frozen, finalize_at)
                    advance_clock(self.clock_id, int(finalize_at))
                    continue
                # We're at or past auto-finalize time but still draft; finalize manually
                logger.debug("Invoice still draft after time advance, finalizing manually...")
                try:
                    finalized = stripe.Invoice.finalize_invoice(invoice_dict["id"])
                    return stripe_dict(finalized)
                except Exception as e:
                    logger.debug("Finalize failed: %s", e)
                    return invoice_dict
            return invoice_dict
        return None

    def replace_storage_subscription_quantity(
            self,
            new_quantity_gb: int,
            subscription_ids: set[str] | None = None,
    ) -> dict[str, Any]:
        """
        Replace/update storage subscription quantity via the backend API.

        This is used for upgrading or downgrading storage addon quantity.
        Calls the backend /billing/storage/set-target endpoint instead of direct Stripe API.

        Args:
            new_quantity_gb: New quantity in GB
            subscription_ids: Set of subscription IDs for webhook replay filtering (optional)

        Returns:
            Dictionary with the result including:
            - tenant_id: The tenant ID
            - storage_quantity_gb: The new storage quantity
            - target_storage_bytes: The target quantity in bytes
            - addon_storage_bytes: The effective addon storage in bytes
        """
        if not self.tenant_id:
            raise FlowError("tenant_id is required for updating storage")
        if new_quantity_gb < 0:
            raise FlowError("new_quantity_gb must be non-negative")

        target_storage_bytes = new_quantity_gb * BYTES_PER_GB
        setup_intent_id = self.ensure_setup_intent_for_storage_change(target_storage_bytes)

        # Step 1: Call backend API to set storage target
        logger.info("Setting storage target: tenant=%s, quantity=%dGB (%d bytes)", self.tenant_id, new_quantity_gb, target_storage_bytes)
        created_gte = int(time.time()) - 5
        try:
            result = self.storage_set_target(target_storage_bytes, setup_intent_id=setup_intent_id)
            logger.info("Storage target updated via backend API")
        except FlowError as exc:
            raise FlowError(f"Failed to update storage target via backend API: {exc}") from exc

        addon_storage_bytes = result.get("addon_storage_bytes", 0)
        returned_target_bytes = result.get("target_storage_bytes", 0)

        # Step 2: Wait for Stripe CLI forwarded webhooks to be processed.
        logger.info("Waiting for webhook synchronization")
        self.sync_webhooks(
            subscription_ids=subscription_ids or set(),
            created_gte=created_gte,
        )
        logger.info("Webhook synchronization finished")

        # Step 3: Verify the storage was updated correctly
        logger.info("Verifying storage update result")
        storage_info = self.storage_current()
        actual_addon_bytes = storage_info.get("addon_storage_bytes", 0)

        expected_bytes = new_quantity_gb * BYTES_PER_GB
        if new_quantity_gb > 0 and actual_addon_bytes < expected_bytes:
            raise FlowError(
                f"Storage verification failed: expected at least {expected_bytes} bytes, got {actual_addon_bytes} bytes"
            )

        logger.info("Storage update verified: %d bytes (%dGB)", actual_addon_bytes, actual_addon_bytes // BYTES_PER_GB)

        return {
            "tenant_id": self.tenant_id,
            "storage_quantity_gb": new_quantity_gb,
            "target_storage_bytes": returned_target_bytes or target_storage_bytes,
            "addon_storage_bytes": addon_storage_bytes,
            "redirect_to": result.get("redirect_to", ""),
        }

    def downgrade_to_trial(
            self,
            subscription_id: str,
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
            subscription_id: Stripe subscription ID (for tracking purposes)
        Returns:
            Dictionary with downgrade result including updated subscription info

        Raises:
            FlowError: If any step in the downgrade process fails
        """
        if not subscription_id:
            raise FlowError("subscription_id is required for downgrade")

        # Step 1: Get the Trial plan price ID from config
        logger.info("Loading Trial plan price ID from config")
        billing_config = load_billing_config()
        trial_price_id = first_plan_price_id(billing_config, "Trial")
        if not trial_price_id:
            raise FlowError("Trial plan price_id not found in service_conf.yaml")
        logger.info("Trial plan price ID: %s...", trial_price_id)

        # Step 2: Call server API to schedule plan change (PLAN-01 pattern)
        # This sends POST /billing/checkout to the server, which handles Stripe interaction
        logger.info("Scheduling downgrade to Trial via server API")
        checkout_result = self.schedule_plan_change(trial_price_id)
        scheduled_change = extract_scheduled_change(checkout_result)
        if not scheduled_change.get("schedule_id"):
            raise FlowError(f"expected schedule_id for downgrade to Trial, got: {checkout_result}")
        logger.info("Downgrade scheduled via server, schedule_id: %s", scheduled_change.get("schedule_id"))

        # Step 3: Wait for pending downgrade to appear in current_plan
        logger.info("Waiting for pending downgrade to appear")
        pending_plan = self.wait_for_pending_downgrade("Trial")
        current_plan_name = pending_plan.get("plan_name", "")
        if current_plan_name == "Trial":
            raise FlowError(f"plan changed prematurely after scheduling downgrade to Trial: expected paid plan, got {current_plan_name}")
        logger.info("Pending downgrade confirmed: current=%s, pending=Trial", current_plan_name)

        # Step 5: Verify the downgrade result
        logger.info("Verifying downgrade result")
        current_plan = self.current_plan()
        plan_name = current_plan.get("plan_name", "")

        # After scheduling, plan should still be the paid plan (downgrade happens at period end)
        if plan_name == "Trial":
            raise FlowError(f"Downgrade verification failed: expected paid plan (pending Trial), got {plan_name}")
        logger.info("Downgrade to Trial scheduled successfully (will apply at period end), current: %s", plan_name)

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

    def downgrade_pro_to_starter(
            self,
            subscription_id: str,
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
            subscription_id: Stripe subscription ID (for tracking purposes)
        Returns:
            Dictionary with downgrade result including updated subscription info

        Raises:
            FlowError: If any step in the downgrade process fails
        """
        if not subscription_id:
            raise FlowError("subscription_id is required for downgrade")

        # Get the Starter plan price ID from config
        logger.info("Loading Starter plan price ID from config")
        billing_config = load_billing_config()
        starter_price_id = first_plan_price_id(billing_config, "Starter")
        if not starter_price_id:
            raise FlowError("Starter plan price_id not found in service_conf.yaml")
        logger.info("Starter plan price ID: %s...", starter_price_id)

        # Call server API to schedule plan change (PLAN-01 pattern)
        # This sends POST /billing/checkout to the server, which handles Stripe interaction
        logger.info("Scheduling Pro -> Starter downgrade via server API")
        checkout_result = self.schedule_plan_change(starter_price_id)
        scheduled_change = extract_scheduled_change(checkout_result)
        if not scheduled_change.get("schedule_id"):
            raise FlowError(f"expected schedule_id for downgrade to Starter, got: {checkout_result}")
        logger.info("Downgrade scheduled via server, schedule_id: %s", scheduled_change.get("schedule_id"))

        # Wait for pending downgrade to appear in current_plan
        logger.info("Waiting for pending downgrade to appear")
        pending_plan = self.wait_for_pending_downgrade("Starter")
        if pending_plan.get("plan_name") != "Pro":
            raise FlowError(f"plan changed prematurely after scheduling downgrade to Starter: expected 'Pro', got {pending_plan.get('plan_name')}")
        logger.info("Pending downgrade confirmed: current=Pro, pending=Starter")

        # Verify the downgrade result
        logger.info("Verifying downgrade result")
        current_plan = self.current_plan()
        plan_name = current_plan.get("plan_name", "")

        # After scheduling, plan should still be Pro (downgrade happens at period end)
        if plan_name != "Pro":
            raise FlowError(f"Downgrade verification failed: expected Pro plan (pending), got {plan_name}")

        logger.info("Downgrade from Pro to Starter scheduled successfully (will apply at period end)")

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

    def add_storage_to_subscription_with_webhook(
            self,
            storage_quantity_gb: int,
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
            storage_quantity_gb: Storage quantity in GB to add
            subscription_ids: Set of subscription IDs for webhook replay filtering (optional)
            created_gte: Timestamp for webhook replay filtering (optional)

        Returns:
            Dictionary with the result including:
            - tenant_id: The tenant ID
            - storage_quantity_gb: The added storage quantity
            - target_storage_bytes: The target quantity in bytes
            - addon_storage_bytes: The effective addon storage in bytes

        Raises:
            FlowError: If storage addition or verification fails
        """
        if not self.tenant_id:
            raise FlowError("tenant_id is required for adding storage")
        if storage_quantity_gb <= 0:
            raise FlowError("storage_quantity_gb must be positive")

        target_storage_bytes = storage_quantity_gb * BYTES_PER_GB
        setup_intent_id = self.ensure_setup_intent_for_storage_change(target_storage_bytes)

        # Step 1: Call backend API to set storage target
        logger.info("Setting storage target: tenant=%s, quantity=%dGB (%d bytes)", self.tenant_id, storage_quantity_gb, target_storage_bytes)
        try:
            result = self.storage_set_target(target_storage_bytes, setup_intent_id=setup_intent_id)
            logger.info("Storage target set via backend API")
        except FlowError as exc:
            raise FlowError(f"Failed to set storage target via backend API: {exc}") from exc

        addon_storage_bytes = result.get("addon_storage_bytes", 0)
        returned_target_bytes = result.get("target_storage_bytes", 0)

        # Step 2: Wait for Stripe CLI forwarded webhooks to be processed.
        if created_gte and self.customer_id:
            logger.info("Waiting for webhook synchronization")
            self.sync_webhooks(
                subscription_ids=subscription_ids or set(),
                created_gte=created_gte,
            )
            logger.info("Webhook synchronization finished")

        # Step 3: Verify the storage was added correctly
        logger.info("Verifying storage addition result")
        storage_info = self.storage_current()
        actual_addon_bytes = storage_info.get("addon_storage_bytes", 0)

        if actual_addon_bytes < target_storage_bytes:
            raise FlowError(
                f"Storage verification failed: expected at least {target_storage_bytes} bytes, got {actual_addon_bytes} bytes"
            )

        logger.info("Storage addon verified: %d bytes (%dGB)", actual_addon_bytes, actual_addon_bytes // BYTES_PER_GB)

        return {
            "tenant_id": self.tenant_id,
            "storage_quantity_gb": storage_quantity_gb,
            "target_storage_bytes": returned_target_bytes or target_storage_bytes,
            "addon_storage_bytes": addon_storage_bytes,
            "redirect_to": result.get("redirect_to", ""),
        }


    def upgrade_trial_to_starter(self) -> dict[str, Any]:
        # Upgrade from Trial to Starter plan
        logger.info("Setup: Upgrade from Trial to Starter plan")
        billing_config = load_billing_config()
        starter_price_id = first_plan_price_id(billing_config, "Starter")
        starter_checkout_started_at = int(time.time()) - 10
        setup_intent_id = self.ensure_setup_intent_for_plan_change(starter_price_id)

        # Record invoice count before upgrade
        history_before_upgrade = self.spend_history()
        invoice_count_before_upgrade = len(history_before_upgrade)

        checkout_result = self.schedule_plan_change(starter_price_id, setup_intent_id=setup_intent_id)
        subscription_id_from_result = checkout_result.get("subscription_id", "")

        starter_subscription_id = subscription_id_from_result
        logger.info("Assert: Starter subscription: %s", starter_subscription_id)

        sent = self.sync_webhooks(
            subscription_ids={starter_subscription_id},
            created_gte=starter_checkout_started_at,
            wait_seconds=DEFAULT_WEBHOOK_WAIT_SECONDS,
        )
        logger.info("Assert: Webhooks synced for plan upgrade, sent: %s", sent)

        # Verify exactly one new invoice was created after upgrade
        self.wait_for_plan("Starter")
        self.wait_for_history_count(
            len(history_before_upgrade) + 1,
            "Trial→Starter upgrade payment",
            )
        history_after_upgrade = self.spend_history()
        invoice_count_after_upgrade = len(history_after_upgrade)
        new_invoice_count = invoice_count_after_upgrade - invoice_count_before_upgrade
        if new_invoice_count == 0:
            raise FlowError(f"expected new invoice after upgrade, got {new_invoice_count}")

        # Verify there should be an invoice has amount $59 with "paid" status
        new_invoice = [row for row in history_after_upgrade if row.get("status", "") == "paid" and int(row.get("amount", 0)) == 59]
        if len(new_invoice) != 1:
            raise FlowError("expected new invoice amount with $59.00 and status with paid")
        logger.info("Assert: New invoice verified: $%s", new_invoice[0])

        logger.info("Setup complete: Starter plan ready")

        return {"subscription_id": starter_subscription_id}


    def upgrade_starter_to_pro(
            self,
            starter_subscription_id: str,
    ) -> dict[str, Any]:
        """
        Upgrade a user's Starter subscription to the Pro plan via server API,
        with manual webhook injection to ensure immediate state transition.
        """
        if not starter_subscription_id:
            raise FlowError("starter_subscription_id is required for upgrade")

        # Load Pro price ID
        logger.info("Loading Pro plan price ID from config")
        billing_config = load_billing_config()
        pro_price_id = first_plan_price_id(billing_config, "Pro")
        if not pro_price_id:
            raise FlowError("Pro plan price_id not found in service_conf.yaml")
        logger.info("Pro plan price ID: %s...", pro_price_id)

        # Call server API to perform the upgrade
        logger.info("Scheduling upgrade to Pro via server API")
        upgrade_started_at = int(time.time()) - 5
        setup_intent_id = self.ensure_setup_intent_for_plan_change(pro_price_id)
        checkout_result = self.schedule_plan_change(pro_price_id, setup_intent_id=setup_intent_id)

        subscription_id = checkout_result.get("subscription_id") or starter_subscription_id
        plan_name = checkout_result.get("plan_name", "")
        if plan_name != "Pro":
            raise FlowError(
                f"Upgrade to Pro failed: expected plan_name='Pro', got plan_name='{plan_name}'. "
                f"Full response: {checkout_result}"
            )
        if not subscription_id:
            raise FlowError(f"Upgrade response missing subscription_id: {checkout_result}")
        logger.info("Upgrade submitted, plan_name=%s, subscription_id=%s", plan_name, subscription_id)

        # Sync webhooks for test clock consistency
        logger.info("Waiting for webhook synchronization")
        # self.ensure_invoice_finalized(starter_subscription_id)
        subscription_ids = {starter_subscription_id}
        self.sync_webhooks(
            subscription_ids=subscription_ids,
            created_gte=upgrade_started_at,
            wait_seconds=DEFAULT_WEBHOOK_WAIT_SECONDS,
        )
        logger.info("Webhook synchronization finished")

        # 7. Wait for plan to actually become Pro
        logger.info("Waiting for plan to become Pro")
        current_plan = self.wait_for_plan("Pro")
        final_plan_name = current_plan.get("plan_name", "")
        if final_plan_name != "Pro":
            raise FlowError(f"Plan did not switch to Pro: expected 'Pro', got '{final_plan_name}'")
        logger.info("Plan is now Pro")

        # 8. Verify Pro quotas
        logger.info("Verifying Pro quotas")
        overview_pro = self.plan_overview()
        apps_limit_pro = overview_pro.get("resources", {}).get("apps", {}).get("limit", 0)
        expected_pro_apps = get_pro_quota_apps()
        if apps_limit_pro != expected_pro_apps:
            raise FlowError(
                f"after Pro upgrade, expected Pro apps quota {expected_pro_apps}, got {apps_limit_pro}"
            )
        logger.info("Pro apps quota verified: %d", apps_limit_pro)
        logger.info("Upgrade from Starter to Pro completed successfully")

        return {
            "upgraded": True,
            "scheduled": False,
            "pro_subscription_id": subscription_id,
            "subscription_id": subscription_id,
            "old_plan_name": "Starter",
            "new_plan_name": "Pro",
            "current_plan": current_plan,
        }




    def _cancel_scheduled_change_api(self, tenant_id: str) -> dict[str, Any]:
        """Internal method to call the cancel scheduled change API."""
        _ = tenant_id
        raise FlowError("cancel scheduled subscription change callback was removed; use portal or subscription APIs instead")



    def wait_for_pending_downgrade(self, expected_target: str) -> dict[str, Any]:
        """Wait for pending_subscription_change to appear with target plan."""
        deadline = time.time() + DEFAULT_WEBHOOK_TIMEOUT_SECONDS
        while time.time() < deadline:
            plan = self.current_plan()
            pending = plan.get("pending_subscription_change", {})
            if pending:
                pending_plan = pending.get("pending_plan_name", "")
                if pending_plan.lower() == expected_target.lower():
                    return plan
            time.sleep(1)
        raise FlowError(f"timed out waiting for pending downgrade to {expected_target}")

    def wait_for_no_pending_downgrade(self) -> dict[str, Any]:
        """Wait for pending_subscription_change to disappear."""
        last_plan: dict[str, Any] = {}
        deadline = time.time() + DEFAULT_WEBHOOK_TIMEOUT_SECONDS
        while time.time() < deadline:
            plan = self.current_plan()
            last_plan = plan
            pending = plan.get("pending_subscription_change", {})
            if not pending:
                return plan
            time.sleep(3)
        raise FlowError(f"timed out waiting for pending downgrade to be canceled, last plan: {last_plan}")

    def wait_for_payment_order_status(
            self,
            order_id: str,
            expected_status: str,
    ) -> dict[str, Any]:
        """Wait for a billing payment order to reach the expected status via billing API polling."""
        status_map = {
            "paid": "success",
            "unpaid": "failed",
            "pending": "pending",
        }

        deadline = time.time() + DEFAULT_WEBHOOK_TIMEOUT_SECONDS
        last_payment_order: dict[str, Any] = {}
        while time.time() < deadline:
            history = self.spend_history()
            for row in history:
                if str(row.get("invoice_id") or "") != order_id:
                    continue
                last_payment_order = {
                    **row,
                    "order_id": order_id,
                    "payment_status": status_map.get(str(row.get("status") or "").lower(), "pending"),
                }
                if last_payment_order.get("payment_status") == expected_status:
                    return last_payment_order
                break
            time.sleep(2)
        raise FlowError(
            f"timed out waiting for billing payment order {order_id} to reach {expected_status}, "
            f"last={last_payment_order}"
        )


__all__ = [
    "BillingClient",
    "FOCUSED_STRIPE_WEBHOOKS",
    "DEFAULT_TEST_PASSWORD",
    "DEFAULT_WEBHOOK_TIMEOUT_SECONDS",
    "DEFAULT_WEBHOOK_WAIT_SECONDS",
    "FlowError",
    "TEST_CLOCK_HEADER",
    "advance_clock",
    "assert_portal_subscription_update_url",
    "create_client",
    "create_client_with_type",
    "create_clock_customer",
    "create_test_clock_client",
    "default_base_url",
    "delete_clock",
    "ensure_repo_root",
    "ensure_webhook_delivery_success",
    "env",
    "extract_scheduled_change",
    "find_new_positive_paid_invoice",
    "first_plan_price_id",
    "get_plan_price_id",
    "get_pro_price_id",
    "get_pro_quota_apps",
    "get_quota_members_limit",
    "get_starter_price_id",
    "get_starter_quota_apps",
    "get_trial_price_id",
    "get_trial_quota_apps",
    "bootstrap_client",
    "configure_stripe_runtime",
    "json_dumps_compact",
    "load_billing_config",
    "load_persisted_webhook_secret",
    "load_service_config",
    "load_stripe_test_runtime_config",
    "make_default_parser",
    "make_test_email",
    "parse_plan_end",
    "remove_customer_payment_method",
    "replace_subscription_price",
    "resolve_service_config_path",
    "stripe_dict",
    "wait_for_clock",
]
