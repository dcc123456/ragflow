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

import sys
import time
import uuid
from datetime import datetime, timezone, timedelta
from pathlib import Path
from typing import Any

import stripe  # type: ignore[reportMissingImports]

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from api.db import SubscriptionStatus  # noqa: E402
from api.db.db_models import DB  # noqa: E402
from api.db.services.billing_service import SubscriptionService  # noqa: E402
from common.misc_utils import get_uuid  # noqa: E402
from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    first_plan_price_id,
    load_billing_config,
    load_persisted_webhook_secret, stripe_dict, env, RAGFlowClient,
    wait_for_clock, wait_for_pending_downgrade
)


def ensure_billing_subscription(tenant_id: str, customer_id: str, plan_name: str = "Trial") -> str:
    """Ensure a billing_subscription record exists with the given customer_id for test.

    Returns:
        The subscription_id from the database record (maybe empty string if not set).
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


def replace_storage_subscription_quantity(
        client: RAGFlowClient,
        tenant_id: str,
        new_quantity_gb: int,
        *,
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
        customer_id: Stripe customer ID for webhook replay filtering (optional)
        subscription_ids: Set of subscription IDs for webhook replay filtering (optional)

    Returns:
        Dictionary with the result including:
        - tenant_id: The tenant ID
        - storage_quantity_gb: The new storage quantity
        - target_storage_bytes: The target quantity in bytes
        - addon_storage_bytes: The effective addon storage in bytes
    """
    if not tenant_id:
        raise FlowError("tenant_id is required for updating storage")
    if new_quantity_gb < 0:
        raise FlowError("new_quantity_gb must be non-negative")

    target_storage_bytes = new_quantity_gb * BYTES_PER_GB

    # Step 1: Call backend API to set storage target
    print(f"  Setting storage target: tenant={tenant_id}, quantity={new_quantity_gb}GB ({target_storage_bytes} bytes)")
    created_gte = int(time.time()) - 5
    try:
        result = client.storage_set_target(tenant_id, target_storage_bytes)
        print("  ✅ Storage target updated via backend API")
    except FlowError as exc:
        raise FlowError(f"Failed to update storage target via backend API: {exc}") from exc

    addon_storage_bytes = result.get("addon_storage_bytes", 0)
    returned_target_bytes = result.get("target_storage_bytes", 0)

    # Step 2: Replay webhook events if created_gte provided (for test clock sync)

    print("  Replaying webhook events for synchronization")
    client.sync_webhooks(
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
        "target_storage_bytes": returned_target_bytes or target_storage_bytes,
        "addon_storage_bytes": addon_storage_bytes,
        "redirect_to": result.get("redirect_to", ""),
    }


def downgrade_to_trial(
        client: "RAGFlowClient",
        tenant_id: str,
        subscription_id: str,
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


def extract_scheduled_change(data: dict[str, Any]) -> dict[str, Any]:
    """Extract scheduled_change from response data."""
    scheduled = data.get("scheduled_change")
    return scheduled if isinstance(scheduled, dict) else data


def cancel_scheduled_change(client: "RAGFlowClient", tenant_id: str) -> dict[str, Any]:
    """
    Cancel a pending scheduled subscription change.

    This method calls the backend API to cancel any pending subscription change
    (upgrade or downgrade) that has been scheduled but not yet applied.

    Args:
        client: RAGFlowClient instance for API calls
        tenant_id: RAGFlow tenant ID

    Returns:
        Dictionary with the cancel result including:
        - canceled: Boolean indicating if a scheduled change was canceled

    Raises:
        FlowError: If the cancel request fails
    """
    if not tenant_id:
        raise FlowError("tenant_id is required for canceling scheduled change")

    print(f"  Canceling scheduled change for tenant: {tenant_id}")
    result = client.cancel_scheduled_change(tenant_id)
    canceled = result.get("canceled", False)
    if canceled:
        print("  ✅ Scheduled change canceled successfully")
    else:
        print("  ℹ️  No scheduled change found to cancel")
    return result


def downgrade_pro_to_starter(
        client: "RAGFlowClient",
        tenant_id: str,
        subscription_id: str,
        *,
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
        client: RAGFlowClient instance for API calls
        tenant_id: RAGFlow tenant ID
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
    print(f"  ✅ Starter plan price ID: {starter_price_id[:20]}...")

    # Call server API to schedule plan change (PLAN-01 pattern)
    # This sends POST /billing/checkout to the server, which handles Stripe interaction
    print("  Scheduling Pro -> Starter downgrade via server API")
    checkout_result = client.schedule_plan_change(tenant_id, starter_price_id)
    scheduled_change = extract_scheduled_change(checkout_result)
    if not scheduled_change.get("schedule_id"):
        raise FlowError(f"expected schedule_id for downgrade to Starter, got: {checkout_result}")
    print(f"  ✅ Downgrade scheduled via server, schedule_id: {scheduled_change.get('schedule_id')}")

    # Wait for pending downgrade to appear in current_plan
    print("  Waiting for pending downgrade to appear")
    pending_plan = wait_for_pending_downgrade(client, "Starter", webhook_timeout_seconds)
    if pending_plan.get("plan_name") != "Pro":
        raise FlowError(f"plan changed prematurely after scheduling downgrade to Starter: expected 'Pro', got {pending_plan.get('plan_name')}")
    print("  ✅ Pending downgrade confirmed: current=Pro, pending=Starter")

    # Verify the downgrade result
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


def add_storage_to_subscription_with_webhook(
        client: RAGFlowClient,
        tenant_id: str,
        storage_quantity_gb: int,
        *,
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
        customer_id: Stripe customer ID for webhook replay filtering (optional)
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
    if not tenant_id:
        raise FlowError("tenant_id is required for adding storage")
    if storage_quantity_gb <= 0:
        raise FlowError("storage_quantity_gb must be positive")

    target_storage_bytes = storage_quantity_gb * BYTES_PER_GB

    # Step 1: Call backend API to set storage target
    print(f"  Setting storage target: tenant={tenant_id}, quantity={storage_quantity_gb}GB ({target_storage_bytes} bytes)")
    try:
        result = client.storage_set_target(tenant_id, target_storage_bytes)
        print("  ✅ Storage target set via backend API")
    except FlowError as exc:
        raise FlowError(f"Failed to set storage target via backend API: {exc}") from exc

    addon_storage_bytes = result.get("addon_storage_bytes", 0)
    returned_target_bytes = result.get("target_storage_bytes", 0)

    # Step 2: Replay webhook events if created_gte provided (for test clock sync)
    if created_gte and customer_id:
        print("  Replaying webhook events for synchronization")
        client.sync_webhooks(
            customer_id=customer_id,
            subscription_ids=subscription_ids or set(),
            created_gte=created_gte,
        )
        print("  ✅ Webhook events replayed")

    # Step 3: Verify the storage was added correctly
    print("  Verifying storage addition result")
    storage_info = client.storage_current(tenant_id)
    actual_addon_bytes = storage_info.get("addon_storage_bytes", 0)

    if actual_addon_bytes < target_storage_bytes:
        raise FlowError(
            f"Storage verification failed: expected at least {target_storage_bytes} bytes, got {actual_addon_bytes} bytes"
        )

    print(f"  ✅ Storage addon verified: {actual_addon_bytes} bytes ({actual_addon_bytes // BYTES_PER_GB}GB)")

    return {
        "tenant_id": tenant_id,
        "storage_quantity_gb": storage_quantity_gb,
        "target_storage_bytes": returned_target_bytes or target_storage_bytes,
        "addon_storage_bytes": addon_storage_bytes,
        "redirect_to": result.get("redirect_to", ""),
    }


def setup_starter(args, email:str) -> dict[str, Any]:
    """
    Create a test environment with an upgraded Starter plan.

    This method encapsulates the logic of Steps 1-4 from billing_plan01_api_flow.py,
    providing a reusable method to quickly set up the test environment.

    Process:
    1. Validate environment and load configuration
    2. Create Stripe test clock and register test user
    3. Verify initial Trial plan state
    4. Upgrade from Trial to Starter plan


    Returns:
        Dictionary containing all necessary context:
        - client: RAGFlowClient instance
        - tenant_id: Tenant ID
        - user_id: User ID
        - customer_id: Stripe customer ID
        - subscription_id: Starter subscription ID
        - clock_id: Stripe test clock ID
        - webhook_secret: Webhook secret
        - starter_price_id: Starter plan price ID
        :param args:
        :param email:
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

    runtime = load_storage_runtime_config()
    stripe.api_key = runtime["stripe_api_key"]
    stripe.api_version = runtime["stripe_api_version"]
    webhook_secret = runtime["webhook_secret"]
    print("  Assert: Runtime config loaded successfully")

    starter_price_id = first_plan_price_id(billing_config, "Starter")
    if not starter_price_id:
        raise FlowError("Starter plan price_id not found in service_conf.yaml")
    print(f"  Assert: Starter plan price_id found: {starter_price_id[:20]}...")

    # Create Stripe test clock and register test user
    print("\n" + "=" * 80)
    print("Setup: Create Stripe test clock and register test user")
    print("=" * 80)

    test_clock = stripe.test_helpers.TestClock.create(
        frozen_time=int(time.time()),
        name=f"setup-starter-{uuid.uuid4().hex[:8]}",
    )
    clock_id = test_clock.id
    wait_for_clock(clock_id)
    base_url=args.base_url
    version=args.version
    password=args.password
    ready_timeout_seconds=args.ready_timeout_seconds
    webhook_wait_seconds=args.webhook_wait_seconds
    webhook_timeout_seconds=args.webhook_timeout_seconds
    client = RAGFlowClient(base_url, version, clock_id=clock_id, webhook_secret=webhook_secret, mode=args.webhook_mode)
    print(f"  Assert: Stripe test clock created: {clock_id}")


    client.wait_until_ready(ready_timeout_seconds)
    user_id, tenant_id = client.register_and_login(email, password)
    print(f"  Assert: Test user registered: {email}")
    print(f"  Assert: Tenant ID: {tenant_id}")

    customer_id = create_clock_customer(email, tenant_id, clock_id)
    ensure_billing_subscription(tenant_id, customer_id)
    print(f"  Assert: Stripe customer created: {customer_id}")

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

    # Upgrade from Trial to Starter plan
    print("\n" + "=" * 80)
    print("Setup: Upgrade from Trial to Starter plan")
    print("=" * 80)

    pm_id = attach_default_test_card(customer_id)
    print(f"  Assert: Test card attached: {pm_id}")

    starter_checkout_started_at = int(time.time()) - 10

    # Record invoice count before upgrade
    history_before_upgrade = client.spend_history()
    invoice_count_before_upgrade = len(history_before_upgrade)

    checkout_result = client.schedule_plan_change(tenant_id, starter_price_id)
    subscription_id_from_result = checkout_result.get("subscription_id", "")

    starter_subscription_id = subscription_id_from_result
    print(f"  Assert: Starter subscription: {starter_subscription_id}")

    # subscription = stripe.Subscription.retrieve(subscription_id_from_result, expand=["latest_invoice"])
    # client.ensure_invoice_finalized(subscription_id_from_result)

    # latest_invoice = subscription.get("latest_invoice") or {}
    # invoice_id = latest_invoice if isinstance(latest_invoice, str) else latest_invoice.get("id", "")
    # client.post_invoice_paid_event(invoice_id)
    # input("before sync_webhooks")
    sent = client.sync_webhooks(
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
        created_gte=starter_checkout_started_at,
        wait_seconds=webhook_wait_seconds,
    )
    # input("after sync_webhooks")
    print(f"  Assert: Webhooks synced for plan upgrade, sent:{sent}")

    # Verify exactly one new invoice was created after upgrade
    client.wait_for_plan("Starter", webhook_timeout_seconds)
    client.wait_for_history_count(
        len(history_before_upgrade) + 1,
        webhook_timeout_seconds,"Trial→Starter upgrade payment",
        )
    history_after_upgrade = client.spend_history()
    invoice_count_after_upgrade = len(history_after_upgrade)
    new_invoice_count = invoice_count_after_upgrade - invoice_count_before_upgrade
    if new_invoice_count != 2:
        raise FlowError(f"expected exactly 2 new invoice (trial & starter) after upgrade, got {new_invoice_count}")
    print("  Assert: Exactly 2 new invoice created after upgrade")

    # Verify there should be an invoice has amount $59 with "paid" status
    new_invoice = [row for row in history_after_upgrade if row.get("status", "") == "paid" and int(row.get("amount", 0)) == 59]
    if len(new_invoice) != 1:
        raise FlowError("expected new invoice amount with $59.00 and status with paid")
    print(f"  Assert: New invoice verified: ${new_invoice[0]}")

    client.wait_for_plan("Starter", webhook_timeout_seconds)
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
        client: RAGFlowClient,
        tenant_id: str,
        customer_id: str,
        starter_subscription_id: str,
        *,
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
    checkout_result = client.schedule_plan_change(tenant_id, pro_price_id)

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
    print("  Replaying webhook events for synchronization")
    # client.ensure_invoice_finalized(starter_subscription_id)
    subscription_ids = {starter_subscription_id}
    client.sync_webhooks(
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=upgrade_started_at,
        wait_seconds=webhook_wait_seconds,
    )
    print("  ✅ Webhook events replayed")

    # 7. Wait for plan to actually become Pro
    print("  Waiting for plan to become Pro")
    current_plan = client.wait_for_plan("Pro", webhook_timeout_seconds)
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

