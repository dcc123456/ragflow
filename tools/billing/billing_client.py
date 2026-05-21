"""
Flow-specific common utilities for billing test flows.

Re-exports shared utilities from billing_common.py for backward compatibility.
"""

from __future__ import annotations

import hashlib
import hmac
import time
import uuid
import requests
import stripe

from api.utils.crypt import crypt
from datetime import datetime, timezone
from typing import Any, Type, TypeVar

from tools.billing.billing_common import FlowError, delete_clock, json_dumps_compact, ensure_webhook_delivery_success, \
    stripe_dict, advance_clock, load_billing_config, list_recent_checkout_sessions, \
    select_subscription_checkout_session, create_paid_subscription, build_checkout_session_completed_event, \
    first_plan_price_id, find_new_positive_paid_invoice, TEST_CLOCK_HEADER, FOCUSED_STRIPE_WEBHOOKS, \
    extract_scheduled_change, get_pro_quota_apps, env, wait_for_clock, create_clock_customer
from tools.billing.storage_common import BYTES_PER_GB, ensure_billing_subscription, attach_default_test_card

# Type variable for client types (forward reference to BillingClient)
T = TypeVar("T", bound="BillingClient")


def init_client(client, email:str, password:str) -> None:
    # Validate environment and load configuration
    user_id, tenant_id = client.register_and_login(email, password)
    print(f"  Assert: Test user registered: {email}")
    print(f"  Assert: Tenant ID: {tenant_id}")

    client.create_clock_customer(email)
    pm_id = attach_default_test_card(client.customer_id)
    print(f"  Assert: Test card attached: {pm_id}")
    ensure_billing_subscription(tenant_id, client.customer_id)
    print(f"  Assert: Stripe customer created: {client.customer_id}")

    # Verify initial Trial plan state
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
    return client

def create_client(args, email: str) -> BillingClient:
    """Create a BillingClient for billing test flows."""
    return create_client_with_type(args, email, client_type=BillingClient)


def create_client_with_type(args, email: str, client_type: Type[T]) -> T:
    """Create a client of the specified type (RAGFlowClient or PointsClient) for billing test flows.
    
    Args:
        args: Command line arguments containing base_url, version, webhook_mode, password, ready_timeout_seconds
        email: Test user email
        client_type: The client class to instantiate (default: BillingClient, can be PointsClient)
    
    Returns:
        An initialized client instance of the specified type
    """
    # Validate environment and load configuration
    print("=" * 80)
    print("Setup: Validate environment and load configuration")
    print("=" * 80)

    stripe_api_key = env("BILLING_STRIPE_API_KEY", env("STRIPE_API_KEY"))
    if not stripe_api_key:
        raise FlowError("BILLING_STRIPE_API_KEY or STRIPE_API_KEY is required")
    print("  Assert: Stripe API key is set")

    billing_config = load_billing_config()
    webhook_secret = env("BILLING_WEBHOOK_SECRET", env("STRIPE_WEBHOOK_SECRET"))
    stripe.api_key = stripe_api_key
    stripe.api_version = str(billing_config.get("stripe_api_version") or env("STRIPE_API_VERSION") or "2026-02-25.clover")
    print("  Assert: Runtime config loaded successfully")

    # Create Stripe test clock and register test user
    print("\n" + "=" * 80)
    print("Setup: Create Stripe test clock and register test user")
    print("=" * 80)

    test_clock = stripe.test_helpers.TestClock.create(
        frozen_time=int(time.time()),
        name=f"setup-starter-{uuid.uuid4().hex[:8]}",
    )
    wait_for_clock(test_clock.id)
    print(f"  Assert: Stripe test clock created: {test_clock.id}")

    client = client_type(args.base_url, args.version, test_clock.id, webhook_secret, args.webhook_mode)
    client.wait_until_ready(args.ready_timeout_seconds)

    init_client(client, email, args.password)
    return client


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

    def __init__(self, base_url: str, version: str, clock_id: str, webhook_secret: str, mode:str):
        self.base_url = base_url.rstrip("/")
        self.version = version.strip("/")
        self.clock_id = clock_id
        self.session = requests.Session()
        self.session.trust_env = False
        self.auth_header = ""
        self.webhook_secret = webhook_secret
        self.mode = mode
        self.user_id = ""
        self.tenant_id = ""
        self.customer_id = ""

    def __del__(self):
        if self.clock_id:
            print(f" delete clock:{self.clock_id}")
            delete_clock(clock_id=self.clock_id)

    def create_clock_customer(self, email: str) -> None:
        """Create a Stripe customer with test clock."""
        customer_id = create_clock_customer(email, self.tenant_id, self.clock_id)
        self.customer_id = customer_id

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
            expected_status: str,
            timeout_seconds: int = 30,
    ) -> dict[str, Any]:
        """Wait for storage subscription to reach the specified status."""
        deadline = time.time() + timeout_seconds
        last_storage = {}
        while time.time() < deadline:
            last_storage = self.storage_current()
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

    def url(self, path: str, need_api_path:bool=False) -> str:
        if need_api_path:
            return f"{self.base_url}/api/{self.version}/{path.lstrip('/')}"
        else:
            return f"{self.base_url}/{self.version}/{path.lstrip('/')}"

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
        self.user_id = user_id
        self.tenant_id = tenant_id
        return user_id, tenant_id

    def current_plan(self) -> dict[str, Any]:
        return self.request_json("GET", "/billing/current_plan")["data"]

    def plan_overview(self) -> dict[str, Any]:
        return self.request_json("GET", "/billing/plan_overview")["data"]

    def storage_current(self) -> dict[str, Any]:
        return self.request_json("GET", f"/billing/storage/current?tenant_id={self.tenant_id}")["data"]

    def upcoming_plan_change(self, new_price_id: str) -> dict[str, Any]:
        return self.request_json(
            "POST",
            "/billing/upcoming",
            json={"tenant_id": self.tenant_id, "new_price_id": new_price_id},
        )["data"]

    def upcoming_storage_change(self, target_storage_bytes: int) -> dict[str, Any]:
        return self.request_json(
            "POST",
            "/billing/upcoming",
            json={"tenant_id": self.tenant_id, "target_storage_bytes": target_storage_bytes},
        )["data"]

    def storage_set_target(self, target_storage_bytes: int, *, setup_intent_id: str = "") -> dict[str, Any]:
        payload: dict[str, Any] = {
            "tenant_id": self.tenant_id,
            "target_storage_bytes": target_storage_bytes,
        }
        if setup_intent_id:
            payload["setup_intent_id"] = setup_intent_id
        return self.request_json(
            "POST",
            "/billing/storage/set-target",
            json=payload,
        )["data"]

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
        return self.request_json("POST", "/billing/setup-intent", json=payload)["data"]

    def succeed_setup_intent(self, setup_intent_id: str, payment_method_id: str = "pm_card_visa") -> dict[str, Any]:
        if not setup_intent_id:
            raise FlowError("setup_intent_id is required")
        setup_intent = stripe.SetupIntent.confirm(setup_intent_id, payment_method=payment_method_id)
        setup_intent_dict = stripe_dict(setup_intent)
        if setup_intent_dict.get("status") != "succeeded":
            raise FlowError(f"expected SetupIntent to succeed, got {setup_intent_dict}")
        return setup_intent_dict

    def spend_history(self) -> list[dict[str, Any]]:
        return self.request_json("GET", "/billing/spend_overview")["data"].get("items", [])

    def schedule_plan_change(self, price_id: str, *, setup_intent_id: str = "") -> dict[str, Any]:
        """Initiate a subscription change via checkout (upgrade/downgrade)."""
        payload = {
            "tenant_id": self.tenant_id,
            "payment_type": "subscription",
            "subscription_price_id": price_id,
            "quantity": 1,
        }
        if setup_intent_id:
            payload["setup_intent_id"] = setup_intent_id
        return self.request_json("POST", "/billing/checkout", json=payload)["data"]

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
                      subscription_ids: set[str],
                      created_gte: int,
                      wait_seconds = 10,
                      ) -> int:
        print(f"-------sleep {wait_seconds} seconds")
        time.sleep(wait_seconds)
        """Synchronize webhook events: manual mode replays from test clock; auto mode just waits."""
        if self.mode != "manual":
            print(f"-------self.mode is {self.mode}, waiting for forwarded webhooks")
            return 0
        return self._replay_stripe_events(
            subscription_ids=subscription_ids,
            created_gte=created_gte,
        )

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
        print(f"  Setting storage target: tenant={self.tenant_id}, quantity={new_quantity_gb}GB ({target_storage_bytes} bytes)")
        created_gte = int(time.time()) - 5
        try:
            result = self.storage_set_target(target_storage_bytes, setup_intent_id=setup_intent_id)
            print("  ✅ Storage target updated via backend API")
        except FlowError as exc:
            raise FlowError(f"Failed to update storage target via backend API: {exc}") from exc

        addon_storage_bytes = result.get("addon_storage_bytes", 0)
        returned_target_bytes = result.get("target_storage_bytes", 0)

        # Step 2: Wait for webhook sync; manual mode will replay selected events.
        print("  Waiting for webhook synchronization")
        self.sync_webhooks(
            subscription_ids=subscription_ids or set(),
            created_gte=created_gte,
        )
        print("  ✅ Webhook synchronization finished")

        # Step 3: Verify the storage was updated correctly
        print("  Verifying storage update result")
        storage_info = self.storage_current()
        actual_addon_bytes = storage_info.get("addon_storage_bytes", 0)

        expected_bytes = new_quantity_gb * BYTES_PER_GB
        if new_quantity_gb > 0 and actual_addon_bytes < expected_bytes:
            raise FlowError(
                f"Storage verification failed: expected at least {expected_bytes} bytes, got {actual_addon_bytes} bytes"
            )

        print(f"  ✅ Storage update verified: {actual_addon_bytes} bytes ({actual_addon_bytes // BYTES_PER_GB}GB)")

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
            subscription_id: Stripe subscription ID (for tracking purposes)
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
        checkout_result = self.schedule_plan_change(trial_price_id)
        scheduled_change = extract_scheduled_change(checkout_result)
        if not scheduled_change.get("schedule_id"):
            raise FlowError(f"expected schedule_id for downgrade to Trial, got: {checkout_result}")
        print(f"  ✅ Downgrade scheduled via server, schedule_id: {scheduled_change.get('schedule_id')}")

        # Step 3: Wait for pending downgrade to appear in current_plan
        print("  Waiting for pending downgrade to appear")
        pending_plan = self.wait_for_pending_downgrade("Trial", webhook_timeout_seconds)
        current_plan_name = pending_plan.get("plan_name", "")
        if current_plan_name == "Trial":
            raise FlowError(f"plan changed prematurely after scheduling downgrade to Trial: expected paid plan, got {current_plan_name}")
        print(f"  ✅ Pending downgrade confirmed: current={current_plan_name}, pending=Trial")

        # Step 5: Verify the downgrade result
        print("  Verifying downgrade result")
        current_plan = self.current_plan()
        plan_name = current_plan.get("plan_name", "")

        # After scheduling, plan should still be the paid plan (downgrade happens at period end)
        if plan_name == "Trial":
            raise FlowError(f"Downgrade verification failed: expected paid plan (pending Trial), got {plan_name}")
        print(f"  ✅ Downgrade to Trial scheduled successfully (will apply at period end), current:{plan_name}")

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
            webhook_timeout_seconds: int = 10,
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
            webhook_timeout_seconds: Timeout for waiting for plan change

        Returns:
            Dictionary with downgrade result including updated subscription info

        Raises:
            FlowError: If any step in the downgrade process fails
        """
        if not subscription_id:
            raise FlowError("subscription_id is required for downgrade")

        # Get the Starter plan price ID from config
        print("  Loading Starter plan price ID from config")
        billing_config = load_billing_config()
        starter_price_id = first_plan_price_id(billing_config, "Starter")
        if not starter_price_id:
            raise FlowError("Starter plan price_id not found in service_conf.yaml")
        print(f"  ✅ Starter plan price ID: {starter_price_id}...")

        # Call server API to schedule plan change (PLAN-01 pattern)
        # This sends POST /billing/checkout to the server, which handles Stripe interaction
        print("  Scheduling Pro -> Starter downgrade via server API")
        checkout_result = self.schedule_plan_change(starter_price_id)
        scheduled_change = extract_scheduled_change(checkout_result)
        if not scheduled_change.get("schedule_id"):
            raise FlowError(f"expected schedule_id for downgrade to Starter, got: {checkout_result}")
        print(f"  ✅ Downgrade scheduled via server, schedule_id: {scheduled_change.get('schedule_id')}")

        # Wait for pending downgrade to appear in current_plan
        print("  Waiting for pending downgrade to appear")
        pending_plan = self.wait_for_pending_downgrade("Starter", webhook_timeout_seconds)
        if pending_plan.get("plan_name") != "Pro":
            raise FlowError(f"plan changed prematurely after scheduling downgrade to Starter: expected 'Pro', got {pending_plan.get('plan_name')}")
        print("  ✅ Pending downgrade confirmed: current=Pro, pending=Starter")

        # Verify the downgrade result
        print("  Verifying downgrade result")
        current_plan = self.current_plan()
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
        print(f"  Setting storage target: tenant={self.tenant_id}, quantity={storage_quantity_gb}GB ({target_storage_bytes} bytes)")
        try:
            result = self.storage_set_target(target_storage_bytes, setup_intent_id=setup_intent_id)
            print("  ✅ Storage target set via backend API")
        except FlowError as exc:
            raise FlowError(f"Failed to set storage target via backend API: {exc}") from exc

        addon_storage_bytes = result.get("addon_storage_bytes", 0)
        returned_target_bytes = result.get("target_storage_bytes", 0)

        # Step 2: Wait for webhook sync; manual mode will replay selected events.
        if created_gte and self.customer_id:
            print("  Waiting for webhook synchronization")
            self.sync_webhooks(
                subscription_ids=subscription_ids or set(),
                created_gte=created_gte,
            )
            print("  ✅ Webhook synchronization finished")

        # Step 3: Verify the storage was added correctly
        print("  Verifying storage addition result")
        storage_info = self.storage_current()
        actual_addon_bytes = storage_info.get("addon_storage_bytes", 0)

        if actual_addon_bytes < target_storage_bytes:
            raise FlowError(
                f"Storage verification failed: expected at least {target_storage_bytes} bytes, got {actual_addon_bytes} bytes"
            )

        print(f"  ✅ Storage addon verified: {actual_addon_bytes} bytes ({actual_addon_bytes // BYTES_PER_GB}GB)")

        return {
            "tenant_id": self.tenant_id,
            "storage_quantity_gb": storage_quantity_gb,
            "target_storage_bytes": returned_target_bytes or target_storage_bytes,
            "addon_storage_bytes": addon_storage_bytes,
            "redirect_to": result.get("redirect_to", ""),
        }


    def upgrade_trial_to_starter(self, webhook_wait_seconds: int = 8,
                                 webhook_timeout_seconds: int = 30,) -> dict[str, Any]:
        # Upgrade from Trial to Starter plan
        print("\n" + "=" * 80)
        print("Setup: Upgrade from Trial to Starter plan")
        print("=" * 80)
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
        print(f"  Assert: Starter subscription: {starter_subscription_id}")

        sent = self.sync_webhooks(
            subscription_ids={starter_subscription_id},
            created_gte=starter_checkout_started_at,
            wait_seconds = webhook_wait_seconds
        )
        # input("after sync_webhooks")
        print(f"  Assert: Webhooks synced for plan upgrade, sent:{sent}")

        # Verify exactly one new invoice was created after upgrade
        self.wait_for_plan("Starter", webhook_timeout_seconds)
        self.wait_for_history_count(
            len(history_before_upgrade) + 1,
            webhook_timeout_seconds, "Trial→Starter upgrade payment",
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
        print(f"  Assert: New invoice verified: ${new_invoice[0]}")

        print("\n" + "=" * 80)
        print("Setup complete: Starter plan ready")
        print("=" * 80)

        return {"subscription_id": starter_subscription_id}


    def upgrade_starter_to_pro(
            self,
            starter_subscription_id: str,
            webhook_wait_seconds: int = 8,
            webhook_timeout_seconds: int = 180,
    ) -> dict[str, Any]:
        """
        Upgrade a user's Starter subscription to the Pro plan via server API,
        with manual webhook injection to ensure immediate state transition.
        """
        if not starter_subscription_id:
            raise FlowError("starter_subscription_id is required for upgrade")

        # Load Pro price ID
        print("  Loading Pro plan price ID from config")
        billing_config = load_billing_config()
        pro_price_id = first_plan_price_id(billing_config, "Pro")
        if not pro_price_id:
            raise FlowError("Pro plan price_id not found in service_conf.yaml")
        print(f"  ✅ Pro plan price ID: {pro_price_id}...")

        # Call server API to perform the upgrade
        print("  Scheduling upgrade to Pro via server API")
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
        print(f"  ✅ Upgrade submitted, plan_name={plan_name}, subscription_id={subscription_id}")

        # Sync webhooks for test clock consistency
        print("  Waiting for webhook synchronization")
        # self.ensure_invoice_finalized(starter_subscription_id)
        subscription_ids = {starter_subscription_id}
        self.sync_webhooks(
            subscription_ids=subscription_ids,
            created_gte=upgrade_started_at,
            wait_seconds=webhook_wait_seconds,
        )
        print("  ✅ Webhook synchronization finished")

        # 7. Wait for plan to actually become Pro
        print("  Waiting for plan to become Pro")
        current_plan = self.wait_for_plan("Pro", webhook_timeout_seconds)
        final_plan_name = current_plan.get("plan_name", "")
        if final_plan_name != "Pro":
            raise FlowError(f"Plan did not switch to Pro: expected 'Pro', got '{final_plan_name}'")
        print("  ✅ Plan is now Pro")

        # 8. Verify Pro quotas
        print("  Verifying Pro quotas")
        overview_pro = self.plan_overview()
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




    def _cancel_scheduled_change_api(self, tenant_id: str) -> dict[str, Any]:
        """Internal method to call the cancel scheduled change API."""
        payload = {"tenant_id": tenant_id}
        return self.request_json("POST", "/billing/callbacks/cancel-scheduled-subscription-change", json=payload)["data"]



    def wait_for_pending_downgrade(self, expected_target: str, timeout_seconds: int = 60) -> dict[str, Any]:
        """Wait for pending_subscription_change to appear with target plan."""
        deadline = time.time() + timeout_seconds
        while time.time() < deadline:
            plan = self.current_plan()
            pending = plan.get("pending_subscription_change", {})
            if pending:
                pending_plan = pending.get("pending_plan_name", "")
                if pending_plan.lower() == expected_target.lower():
                    return plan
            time.sleep(1)
        raise FlowError(f"timed out waiting for pending downgrade to {expected_target}")

    def wait_for_no_pending_downgrade(self, timeout_seconds: int = 180) -> dict[str, Any]:
        """Wait for pending_subscription_change to disappear."""
        deadline = time.time() + timeout_seconds
        while time.time() < deadline:
            plan = self.current_plan()
            pending = plan.get("pending_subscription_change", {})
            if not pending:
                return plan
            time.sleep(3)
        raise FlowError("timed out waiting for pending downgrade to be canceled")

    def complete_trial_checkout_upgrade(
            self,
            previous_subscription_id: str,
            target_price_id: str,
            target_plan_name: str,
            subscription_ids: set[str],
            webhook_wait_seconds: int,
            webhook_timeout_seconds: int,
    ) -> tuple[str, dict[str, Any], list[dict[str, Any]]]:
        """Complete a Trial→Target plan upgrade with webhook synchronization."""
        upgrade_started_at = int(time.time()) - 5
        setup_intent_id = self.ensure_setup_intent_for_plan_change(target_price_id)
        checkout_result = self.schedule_plan_change(target_price_id, setup_intent_id=setup_intent_id)
        if not checkout_result.get("redirect_to"):
            # Direct upgrade path
            checkout_result.get("invoice_id", "")
            subscription_id = checkout_result.get("subscription_id", "")
            if not subscription_id:
                raise FlowError("Direct upgrade missing subscription_id")
            if checkout_result.get("plan_name") != target_plan_name:
                raise FlowError(f"Unexpected plan after direct upgrade: {checkout_result.get('plan_name')}")
            subscription_ids.add(subscription_id)

            # Manually send invoice.paid webhook
            self.sync_webhooks(
                subscription_ids=subscription_ids,
                created_gte=upgrade_started_at,
                wait_seconds=webhook_wait_seconds,
            )

            upgraded_plan = self.wait_for_plan(target_plan_name, webhook_timeout_seconds)
            if upgraded_plan.get("plan_name") != target_plan_name:
                raise FlowError(f"Plan did not switch to {target_plan_name}: {upgraded_plan.get('plan_name')}")

            return subscription_id, upgraded_plan, []

        # Checkout flow path
        history_before_upgrade = self.spend_history()
        customer_id = self.customer_id
        tenant_id = self.tenant_id
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
        self.post_signed_webhook(checkout_completed_event)
        self.sync_webhooks(
            subscription_ids=subscription_ids,
            created_gte=since_upgrade,
            wait_seconds=webhook_wait_seconds,
        )

        upgraded_plan = self.wait_for_plan(target_plan_name, webhook_timeout_seconds)
        self.wait_for_history_count(
            len(history_before_upgrade) + 1,
            webhook_timeout_seconds,
            f"Trial→{target_plan_name} upgrade payment",
            )
        history_after_upgrade = self.spend_history()
        latest = find_new_positive_paid_invoice(
            history_after_upgrade,
            {str(row.get("invoice_id") or "") for row in history_before_upgrade},
        )
        amount_val = float(latest.get("amount", 0) or 0)
        if amount_val <= 0 or latest.get("status") != "paid" or not latest.get("invoice_id"):
            raise FlowError(f"Trial→{target_plan_name} upgrade should create a paid invoice, got {latest}")
        return subscription_id, upgraded_plan, history_after_upgrade

    def replay_until_payment_order_status(
            self,
            subscription_ids: set[str],
            created_gte: int,
            order_id: str,
            expected_status: str,
            timeout_seconds: int,
            wait_seconds: int,
    ) -> dict[str, Any]:
        """Wait for payment order to reach expected status by replaying Stripe events."""
        from api.db.db_models import DB
        from api.db.services.billing_service import PaymentOrderService

        deadline = time.time() + timeout_seconds
        last_payment_order: dict[str, Any] = {}
        while time.time() < deadline:
            self.sync_webhooks(
                subscription_ids=subscription_ids,
                created_gte=created_gte,
                wait_seconds=wait_seconds,
            )
            with DB.connection_context():
                last_payment_order = PaymentOrderService.get_by_order_id(order_id) or {}
            if last_payment_order.get("payment_status") == expected_status:
                return last_payment_order
            time.sleep(2)
        raise FlowError(
            f"timed out waiting for billing_payment_order {order_id} to reach {expected_status}, "
            f"last={last_payment_order}"
        )
