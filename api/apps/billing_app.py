#
#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
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
import asyncio
import json
import logging
import uuid
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit
from datetime import datetime, timezone
from decimal import Decimal

import stripe
from peewee import IntegrityError
from pydantic import ValidationError
from quart import g, jsonify, request

from api.apps import current_user, login_required
from api.db import PaymentChannel, PaymentMethod, PaymentStatus, PriceType, ProductType, SubscriptionStatus
from api.db.db_models import DB, PaymentOrder, PointHold, Subscription
from api.db.services.billing_service import (
    BillingWebhookEventService,
    PaymentOrderService,
    PointAccountService,
    PricePointService,
    ProductService,
    PurchasedProductOverviewService,
    SubscriptionService,
)
from api.db.services.file_service import FileService
from api.utils.api_utils import get_data_error_result, get_json_result, get_request_json
from api.utils.billing import (
    BYTES_PER_GB,
    BILLING_PLAN_TRIAL_NAME,
    extract_invoice_failure_context,
    extract_latest_invoice_obj,
    extract_plan_item_and_price,
    extract_previous_plan_price,
    extract_plan_subscription_item,
    extract_storage_subscription_item,
    extract_subscription_period,
    get_attr_or_item,
    get_product_id_by_name,
    get_storage_price_id_from_config,
    is_storage_plan_name,
    parse_storage_size,
    is_storage_price_id,
    is_trial_plan_name,
    reset_stripe_test_clock_id_for_current_context,
    safe_float,
    safe_int,
    set_stripe_test_clock_id_for_current_context,
    billing_set_customer_id_async,
    create_or_get_portal_configuration,
    get_plans_equal_or_higher,
    get_plan_priority_by_price_id,
    get_pending_subscription_change_async,
    get_product_ids_for_prices,
    get_receipt_url_from_intent_latest_charge,
    is_subscription_latest_invoice_paid_async,
    is_subscription_latest_invoice_paid_sync,
    get_trial_price_id,
    has_reusable_payment_method_async,
    STRIPE_TEST_CLOCK_HEADER,
    is_downgrade_by_price_id,
    modify_subscription_plan_async,
    schedule_subscription_items_change_at_period_end_async,
    schedule_subscription_price_change_at_period_end_async,
    storage_bytes_to_quantity,
    storage_quantity_to_bytes,
)
from api.utils.billing_schema import CheckoutSessionCompleted, IntentSucceed, InvoicePaid, SubscriptionUpdated
from common import settings
from common.billing_utils import (
    amount_to_float,
    billing_enabled_guard,
    build_date_keys,
    decimal_amount,
    normalize_stripe_invoice_status,
    normalize_stripe_payment_intent_status,
    parse_datetime_arg,
    to_utc_date_str,
    to_utc_datetime,
    to_utc_isoformat,
)
from common.constants import RetCode
from common.misc_utils import get_uuid
from common.time_utils import current_timestamp

UNLIMITED_API_REQUESTS = 2_147_483_647
LIMITED_API_REQUESTS = 5000

# subscription
INVOICE_PAID = "invoice.paid"  # store 'subscription.id' and 'customer.id'verification.
INVOICE_FAILED = "invoice.payment_failed"  #  notify customers and send them to the customer portal to update their payment method.
INVOICE_PAYMENT_ACTION_REQUIRED = "invoice.payment_action_required"
CHECKOUT_SESSION_COMPLETED = "checkout.session.completed"
# Stripe fires this on ANY subscription state change: creation, renewal, upgrade/downgrade,
# cancellation, trial-end, pending_update resolution, etc. Guard with _period_changed()
# to isolate only cycle-start events (creation / renewal).
SUBSCRIPTION_UPDATED = "customer.subscription.updated"
SUBSCRIPTION_DELETED = "customer.subscription.deleted"
# one-off
PAYMENT_INTENT_SUCCEEDED = "payment_intent.succeeded"

FOCUSED_STRIPE_WEBHOOK = [
    INVOICE_PAID,
    INVOICE_FAILED,
    INVOICE_PAYMENT_ACTION_REQUIRED,
    SUBSCRIPTION_UPDATED,
    SUBSCRIPTION_DELETED,
    CHECKOUT_SESSION_COMPLETED,
    PAYMENT_INTENT_SUCCEEDED,
]
MAIN_SUBSCRIPTION_ENTITLED_STATUSES = {"active", "trialing"}
MAIN_SUBSCRIPTION_DELINQUENT_STATUSES = {
    "incomplete",
    "incomplete_expired",
    "past_due",
    "unpaid",
    "canceled",
    "paused",
}
MAIN_SUBSCRIPTION_RECOVERABLE_STATUSES = {"incomplete", "past_due", "unpaid"}
# Cached Stripe webhook endpoint secret (loaded from DB once, cached forever)
_stripe_webhook_secret: str | None = None
STRIPE_CHECKOUT_SESSION_ID_PLACEHOLDER = "{CHECKOUT_SESSION_ID}"
def _get_default_currency() -> str:
    """Return the default currency from BILLING_PRICE_POINT config, falling back to 'usd'."""
    price_points = getattr(settings, "BILLING_PRICE_POINT", []) or []
    for pp in price_points:
        currency = (pp.get("price_currency") or "").strip().lower()
        if currency:
            return currency
    return "usd"


def _check_downgrade_resource_compatibility(tenant_id: str, target_plan_name: str) -> list[dict]:
    """
    Check if current resource usage exceeds target plan quotas (including addons).
    Returns a list of resource conflicts if any quota would be exceeded.
    """
    conflicts = []

    # Resolve target plan quotas from config with a Trial fallback so missing
    # in-memory cache fields do not silently turn every limit into zero.
    # Don't check consumed_plan_points since it's resetted at every cycle's end.
    target_plan_info = _resolve_billing_plan_info(target_plan_name)
    target_quota_storage = parse_storage_size(str(target_plan_info.get("quota_storage", 0)))
    target_quota_members = safe_int(target_plan_info.get("quota_members", 0), 0)
    target_quota_apps = safe_int(target_plan_info.get("quota_apps", 0), 0)

    # Get current addon quotas from existing subscription
    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id, require_quota_info=True)
    addon_storage_bytes = _storage_effective_bytes(tenant_id)
    # Trial cannot retain storage addons after downgrade, so only the Trial
    # plan quota counts toward the compatibility check in that case.
    total_storage_limit_bytes = target_quota_storage if is_trial_plan_name(target_plan_name) else target_quota_storage + addon_storage_bytes
    total_members_limit = target_quota_members  # Members don't have addons
    total_apps_limit = target_quota_apps  # Apps don't have addons

    # Get current usage
    storage_used_bytes = FileService.get_total_size_by_tenant_id(tenant_id) or 0
    members_used = safe_int(tenant_plan.get("num_members", 0), 0)
    apps_used = safe_int(tenant_plan.get("num_apps", 0), 0)

    # Check each resource
    if storage_used_bytes > total_storage_limit_bytes:
        overage_bytes = storage_used_bytes - target_quota_storage
        overage_gb = overage_bytes / BYTES_PER_GB
        conflicts.append({
            "resource": "storage",
            "used": storage_used_bytes,
            "limit": total_storage_limit_bytes,
            "unit": "bytes",
            "message": f"Storage usage ({overage_gb:.2f} GB over limit) exceeds target plan quota including addon storage. Please delete data before downgrading.",
            "action_required": "delete_data",
            "overage": overage_bytes,
        })

    if members_used > total_members_limit:
        overage_members = members_used - total_members_limit
        conflicts.append({
            "resource": "members",
            "used": members_used,
            "limit": total_members_limit,
            "unit": "users",
            "message": f"Member count ({overage_members} users over limit) exceeds target plan quota. Please remove members before downgrading.",
            "action_required": "remove_members",
            "overage_members": overage_members,
        })

    if apps_used > total_apps_limit:
        overage_apps = apps_used - total_apps_limit
        conflicts.append({
            "resource": "apps",
            "used": apps_used,
            "limit": total_apps_limit,
            "unit": "applications",
            "message": f"App count ({overage_apps} apps over limit) exceeds target plan quota. Please remove apps before downgrading.",
            "action_required": "remove_apps",
            "overage_apps": overage_apps,
        })

    return conflicts


def _resolve_billing_plan_info(plan_name: str) -> dict:
    key = (plan_name or "").strip()
    if not key:
        key = BILLING_PLAN_TRIAL_NAME

    info = settings.BILLING_PLAN_TO_INFO.get(key) or settings.BILLING_PLAN_TO_INFO.get(key.title()) or {}
    if info.get("quota_storage") is not None and info.get("quota_members") is not None and info.get("quota_apps") is not None:
        return info

    for plan in settings.BILLING.get("billing_plans", []):
        candidate_name = (plan.get("name") or "").strip()
        if candidate_name.lower() == key.lower():
            merged = dict(info)
            merged.update({
                "quota_storage": plan.get("quota_storage", merged.get("quota_storage", 0)),
                "quota_points": plan.get("quota_points", merged.get("quota_points", 0)),
                "quota_members": plan.get("quota_members", merged.get("quota_members", 0)),
                "quota_apps": plan.get("quota_apps", merged.get("quota_apps", 0)),
                "product_type": plan.get("product_type", merged.get("product_type")),
            })
            return merged

    if key != BILLING_PLAN_TRIAL_NAME:
        return _resolve_billing_plan_info(BILLING_PLAN_TRIAL_NAME)
    return info


@manager.before_request  # noqa: F821
async def _inject_request_stripe_test_clock_id():
    if not settings.BILLING_ENABLED:
        return
    test_clock_id = (request.headers.get(STRIPE_TEST_CLOCK_HEADER) or "").strip()
    g.stripe_test_clock_token = set_stripe_test_clock_id_for_current_context(test_clock_id)


@manager.after_request  # noqa: F821
async def _reset_request_stripe_test_clock_id(response):
    token = getattr(g, "stripe_test_clock_token", None)
    if token:
        reset_stripe_test_clock_id_for_current_context(token)
    return response


def _billing_disabled_response():
    return get_data_error_result(message="Billing is disabled.")


def _billing_disabled_webhook_response():
    logging.info("Billing disabled; ignoring Stripe webhook.")
    return jsonify(success=True)


def _build_checkout_success_url(url: str) -> str:
    if not url:
        return url

    if STRIPE_CHECKOUT_SESSION_ID_PLACEHOLDER in url:
        return url

    parsed = urlsplit(url)
    query_pairs = [(key, value) for key, value in parse_qsl(parsed.query, keep_blank_values=True) if key != "session_id"]
    query_pairs.append(("session_id", STRIPE_CHECKOUT_SESSION_ID_PLACEHOLDER))
    query = urlencode(query_pairs, doseq=True, safe='{}')
    return urlunsplit(parsed._replace(query=query))


async def _create_billing_setup_checkout_session(
    *,
    tenant_id: str,
    customer_id: str,
    session_success_url: str,
    session_cancel_url: str,
    metadata: dict | None = None,
):
    setup_metadata = {
        "tenant_id": tenant_id,
        "setup_for_billing_change": "1",
        **(metadata or {}),
    }
    return await stripe.checkout.Session.create_async(
        customer=customer_id,
        client_reference_id=f"setup_{uuid.uuid4()}",
        mode="setup",
        currency=_get_default_currency(),
        success_url=_build_checkout_success_url(session_success_url),
        cancel_url=session_cancel_url,
        metadata=setup_metadata,
        setup_intent_data={"metadata": setup_metadata},
    )


def _normalize_subscription_status(status: str | None) -> str:
    return (status or "").strip().lower()


def _is_main_subscription_entitled(status: str | None) -> bool:
    return _normalize_subscription_status(status) in MAIN_SUBSCRIPTION_ENTITLED_STATUSES


def _is_main_subscription_delinquent(status: str | None) -> bool:
    return _normalize_subscription_status(status) in MAIN_SUBSCRIPTION_DELINQUENT_STATUSES


def _is_main_subscription_recoverable(status: str | None) -> bool:
    return _normalize_subscription_status(status) in MAIN_SUBSCRIPTION_RECOVERABLE_STATUSES


def _should_preview_as_new_subscription(current_plan_name: str, target_plan_name: str) -> bool:
    """Return True when preview should start a fresh billing cycle from now."""
    return is_trial_plan_name(current_plan_name) and bool(target_plan_name) and not is_trial_plan_name(target_plan_name)


def _main_subscription_payment_state(subscription: dict) -> dict:
    status = _normalize_subscription_status(subscription.get("subscription_status"))
    invoice_url = subscription.get("invoice_url") or ""
    return {
        "payment_required": _is_main_subscription_delinquent(status),
        "payment_recoverable": _is_main_subscription_recoverable(status),
        "payment_recovery_url": invoice_url,
    }


def _safe_payment_order_created_at(value, order_id: str = ""):
    try:
        return to_utc_datetime(value)
    except (TypeError, ValueError, OSError, OverflowError) as e:
        logging.warning(f"Ignore invalid payment order created_at for {order_id or 'unknown order'}: {value!r}, {e}")
        return None


def _sync_main_subscription_from_stripe(
    *,
    tenant_id: str,
    stripe_subscription,
    subscription_status: str = "",
    invoice_id: str = "",
    invoice_url: str = "",
    invoice_pdf_url: str = "",
) -> None:
    if not tenant_id:
        logging.warning("Main subscription sync skipped without tenant_id.")
        return

    existing = SubscriptionService.get_by_tenant_id(tenant_id)
    if not existing:
        logging.warning(f"Main subscription sync skipped; tenant subscription not found: {tenant_id}")
        return

    _item_id, price_id, _quantity = extract_plan_subscription_item(stripe_subscription)
    plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(price_id, "") or existing.get("plan_name", "")
    product_id = get_product_id_by_name(plan_name)

    subscription_id = (get_attr_or_item(stripe_subscription, "id", "") or existing.get("subscription_id", "") or "").strip()
    customer_id = (get_attr_or_item(stripe_subscription, "customer", "") or existing.get("customer_id", "") or "").strip()
    stripe_status = _normalize_subscription_status(get_attr_or_item(stripe_subscription, "status", ""))
    period_start, period_end = extract_subscription_period(stripe_subscription)
    final_status = _normalize_subscription_status(subscription_status or stripe_status or existing.get("subscription_status"))

    subscription_dict = {
        "tenant_id": tenant_id,
        "product_id": product_id or existing.get("product_id", ""),
        "plan_name": plan_name,
        "order_id": existing.get("order_id", ""),
        "status": final_status,
        "customer_id": customer_id,
        "price_id": price_id or existing.get("price_id", ""),
        "subscription_id": subscription_id,
        "subscription_status": final_status,
        "invoice_id": invoice_id or existing.get("invoice_id", ""),
        "invoice_url": invoice_url or existing.get("invoice_url", ""),
        "invoice_pdf_url": invoice_pdf_url or existing.get("invoice_pdf_url", ""),
        "start_time": period_start or existing.get("start_time"),
        "end_time": period_end or existing.get("end_time"),
        "renew_time": None,
        "original_subscription_id": existing.get("original_subscription_id") or subscription_id,
    }

    with DB.atomic():
        SubscriptionService.upsert_subscription(tenant_id, subscription_dict)


@manager.route("/status", methods=["GET"])  # noqa: F821
def billing_status():
    """Return current billing enabled status - no auth required."""
    return jsonify({"billing_enabled": settings.BILLING_ENABLED == 1})


def _storage_effective_kb(tenant_id: str) -> int:
    addon_bytes, _ = SubscriptionService.get_storage_bytes_for_tenant(tenant_id)
    return addon_bytes // 1024


def _storage_effective_bytes(tenant_id: str) -> int:
    addon_bytes, _ = SubscriptionService.get_storage_bytes_for_tenant(tenant_id)
    return addon_bytes


async def _get_storage_unit_price_async(price_id: str = "") -> float:
    target_price_id = (price_id or "").strip() or get_storage_price_id_from_config()
    if not target_price_id:
        return 0.0
    stripe_price = await stripe.Price.retrieve_async(target_price_id)
    if isinstance(stripe_price, dict):
        unit_amount = stripe_price.get("unit_amount")
    else:
        unit_amount = getattr(stripe_price, "unit_amount", None)
    if unit_amount is None:
        return 0.0
    return safe_float(unit_amount, 0.0) / 100.0


def _sync_storage_subscription_record(
    tenant_id: str,
    subscription_obj,
    customer_id: str = "",
    *,
    target_storage_bytes: int | None = None,
) -> bool:
    """Sync storage fields in billing_subscription from a Stripe subscription object.

    Design note — ``target_storage_bytes`` vs. Stripe SubscriptionSchedule
    -----------------------------------------------------------------------
    ``addon_storage_bytes``   — the storage quota *currently active* on the live
                                Stripe subscription item.  Updated by webhooks
                                (subscription.updated / subscription.deleted).

    ``target_storage_bytes`` — a write-through cache of *where storage is headed*.
                                It is the fallback used by
                                ``_get_storage_target_storage_bytes_async`` when the
                                Stripe SubscriptionSchedule is not available (no
                                schedule, or schedule already released/canceled).

    The authoritative source for a pending downgrade quantity is the Stripe
    SubscriptionSchedule ``phases[1].items[…].quantity``.  The DB field is kept in
    sync so that:
      1. Read endpoints (e.g. ``/billing/storage/current``) can serve the correct
         pending-target without an extra Stripe API round-trip when no schedule
         exists yet or after one has been released.
      2. Race-free reads: if a schedule is released between the subscription
         retrieve call and the schedule retrieve call, the released fallback
         returns the DB value, which must already reflect the post-release state.

    Invariants that callers MUST maintain:
            - After scheduling a storage change via Stripe, write ``target_storage_bytes``
        to the new pending quantity.
      - After releasing/cancelling a schedule (reverting the pending change), reset
                ``target_storage_bytes`` back to ``addon_storage_bytes``.
      - After a storage change becomes effective (webhook), set both
                ``addon_storage_bytes`` and ``target_storage_bytes`` to the new live value.
    """
    if not tenant_id:
        return False

    item_id, price_id, quantity = extract_storage_subscription_item(subscription_obj)
    quantity_bytes = storage_quantity_to_bytes(quantity)
    if isinstance(subscription_obj, dict):
        subscription_id = (subscription_obj.get("id") or "").strip()
        status = (subscription_obj.get("status") or "").strip()
        period_start, period_end = extract_subscription_period(subscription_obj)
        cancel_at_period_end = bool(subscription_obj.get("cancel_at_period_end", False))
        customer = customer_id or (subscription_obj.get("customer") or "")
    else:
        subscription_id = (getattr(subscription_obj, "id", "") or "").strip()
        status = (getattr(subscription_obj, "status", "") or "").strip()
        period_start, period_end = extract_subscription_period(subscription_obj)
        cancel_at_period_end = bool(getattr(subscription_obj, "cancel_at_period_end", False))
        customer = customer_id or (getattr(subscription_obj, "customer", "") or "")

    update_dict = {
        "customer_id": customer,
        "subscription_id": subscription_id,
        "subscription_item_id": item_id,
        "price_id": price_id,
        "current_period_start": period_start,
        "current_period_end": period_end,
        "cancel_at_period_end": cancel_at_period_end,
        "status": status,
        "addon_storage_bytes": quantity_bytes,
        "target_storage_bytes": quantity_bytes,
    }

    if target_storage_bytes is not None:
        update_dict["target_storage_bytes"] = max(safe_int(target_storage_bytes, 0), 0)

    # Storage subscription data is written directly to billing_subscription.
    # New columns (addon_subscription_item_id, addon_storage_bytes, target_storage_bytes)
    # are authoritative for storage information.
    from api.db.db_models import Subscription
    with DB.atomic():
        Subscription.update(
            addon_subscription_item_id=item_id or None,
            addon_storage_bytes=quantity_bytes,
            target_storage_bytes=update_dict.get("target_storage_bytes", quantity_bytes),
        ).where(Subscription.tenant_id == tenant_id).execute()

    logging.info(
        "Synced storage subscription record: tenant_id=%s subscription_id=%s customer_id=%s item_id=%s "
        "price_id=%s quantity=%s addon_storage_bytes=%s target_storage_bytes=%s cancel_at_period_end=%s status=%s",
        tenant_id,
        subscription_id,
        customer,
        item_id,
        price_id,
        quantity,
        quantity_bytes,
        update_dict.get("target_storage_bytes", quantity_bytes),
        cancel_at_period_end,
        status,
    )

    return True


async def _schedule_storage_target_storage_bytes_at_period_end_async(tenant_id: str, target_storage_bytes: int) -> tuple[bool, dict]:
    """Schedule storage quantity change on the unified plan subscription at period end.

    Storage is represented by a dedicated subscription item on the main plan
    subscription, so this helper owns all storage cancel/downgrade scheduling.
    """
    storage = SubscriptionService.get_by_tenant_id(tenant_id) or {}
    main_subscription_id = (storage.get("subscription_id") or "").strip()
    if not main_subscription_id:
        return True, {}

    stripe_sub = await stripe.Subscription.retrieve_async(main_subscription_id)
    storage_item_id, storage_price_id, current_quantity_gb = extract_storage_subscription_item(stripe_sub)
    if not storage_item_id or not storage_price_id:
        return True, {}

    target_quantity = storage_bytes_to_quantity(target_storage_bytes)

    _plan_item_id, plan_price_id, plan_quantity = extract_plan_subscription_item(stripe_sub)
    if not plan_price_id:
        return False, {"error": "Plan subscription item not found."}

    current_phase_items = [{"price": plan_price_id, "quantity": max(plan_quantity, 1)}]
    if current_quantity_gb > 0:
        current_phase_items.append({"price": storage_price_id, "quantity": current_quantity_gb})

    next_phase_items = [{"price": plan_price_id, "quantity": max(plan_quantity, 1)}]
    if target_quantity > 0:
        next_phase_items.append({"price": storage_price_id, "quantity": target_quantity})

    scheduled = await schedule_subscription_items_change_at_period_end_async(
        main_subscription_id,
        current_phase_items=current_phase_items,
        next_phase_items=next_phase_items,
    )
    if not scheduled:
        return False, {"error": "Failed to determine storage schedule boundaries."}

    from api.db.db_models import Subscription

    with DB.atomic():
        Subscription.update(
            target_storage_bytes=max(target_storage_bytes, 0),
            addon_subscription_item_id=storage_item_id or None,
        ).where(Subscription.tenant_id == tenant_id).execute()

    return True, {
        "scheduled_change": True,
        "cancel_at_period_end": target_quantity == 0,
        "effective_at": scheduled["effective_at"],
        "schedule_id": scheduled["schedule_id"],
        "target_storage_bytes": max(target_storage_bytes, 0),
    }


def _current_storage_effective_bytes(storage_row: dict, stripe_quantity: int = 0) -> int:
    local_effective = safe_int(storage_row.get("addon_storage_bytes"), 0) if storage_row else 0
    if local_effective > 0:
        return local_effective
    return storage_quantity_to_bytes(stripe_quantity)


async def _get_storage_target_storage_bytes_async(
    storage_row: dict,
    stripe_subscription=None,
) -> int:
    """Return the pending storage target in bytes, preferring Stripe schedule data."""
    fallback = safe_int((storage_row or {}).get("target_storage_bytes"), safe_int((storage_row or {}).get("addon_storage_bytes"), 0))
    if not storage_row:
        return max(fallback, 0)
    if bool(storage_row.get("cancel_at_period_end", False)):
        return 0

    subscription = stripe_subscription
    if subscription is None:
        subscription_id = ((storage_row or {}).get("subscription_id") or "").strip()
        if not subscription_id:
            return max(fallback, 0)
        subscription = await stripe.Subscription.retrieve_async(subscription_id)

    schedule_id = (get_attr_or_item(subscription, "schedule", "") or "").strip()
    if not schedule_id:
        return max(fallback, 0)

    schedule = await stripe.SubscriptionSchedule.retrieve_async(schedule_id)

    schedule_status = (get_attr_or_item(schedule, "status", "") or "").strip().lower()
    if schedule_status in {"released", "canceled", "completed"}:
        return max(fallback, 0)

    phases = get_attr_or_item(schedule, "phases", []) or []
    if len(phases) < 2:
        return max(fallback, 0)

    pending_phase = phases[1] if isinstance(phases, list) else None
    pending_items = get_attr_or_item(pending_phase, "items", []) or []
    if not pending_items:
        return max(fallback, 0)

    pending_item = pending_items[0]
    pending_quantity = get_attr_or_item(pending_item, "quantity", None)
    if pending_quantity is None:
        return max(fallback, 0)

    return max(storage_quantity_to_bytes(safe_int(pending_quantity, 0)), 0)


async def _set_storage_target_storage_bytes_async(
    tenant_id: str,
    target_storage_bytes: int,
    *,
    session_success_url: str = "",
    session_cancel_url: str = "",
) -> tuple[bool, dict]:
    if target_storage_bytes < 0:
        return False, {"error": "Quantity must be a non-negative integer."}
    if target_storage_bytes % BYTES_PER_GB != 0:
        return False, {"error": "Storage quantity must be a multiple of 1GB."}
    target_quantity = storage_bytes_to_quantity(target_storage_bytes)

    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id)
    is_trial = is_trial_plan_name(tenant_plan.get("plan_name", ""))
    main_subscription_id = (tenant_plan.get("subscription_id") or "").strip()

    storage = SubscriptionService.get_by_tenant_id(tenant_id) or {}

    # Trial tenants are allowed to keep/cancel existing storage, but cannot add or resume positive quantity.
    if is_trial and target_storage_bytes > 0:
        return False, {"error": "Trial plan does not support storage add-on."}

    if not main_subscription_id:
        if target_storage_bytes == 0:
            return True, {
                "addon_storage_bytes": 0,
                "target_storage_bytes": 0,
                "message": "No active plan subscription.",
            }
        return False, {"error": "No active plan subscription found."}

    stripe_sub = await stripe.Subscription.retrieve_async(main_subscription_id)
    stripe_customer_id = (get_attr_or_item(stripe_sub, "customer", "") or "").strip()
    customer_id = (tenant_plan.get("customer_id") or "").strip() or stripe_customer_id

    item_id, _price_id, stripe_quantity_gb = extract_storage_subscription_item(stripe_sub)
    invoice_url = ""

    addon_storage_bytes = _current_storage_effective_bytes(storage, stripe_quantity_gb)
    current_target_storage_bytes = await _get_storage_target_storage_bytes_async(storage, stripe_sub)
    cancel_at_period_end = bool(storage.get("cancel_at_period_end", False))

    if target_storage_bytes == current_target_storage_bytes and not cancel_at_period_end:
        return True, {
            "addon_storage_bytes": addon_storage_bytes,
            "target_storage_bytes": current_target_storage_bytes,
            "message": "Storage target is unchanged.",
        }

    if target_storage_bytes < addon_storage_bytes:
        ok, data = await _schedule_storage_target_storage_bytes_at_period_end_async(tenant_id, target_storage_bytes)
        if not ok:
            return False, {"error": data.get("error") or "Failed to schedule storage change."}
        return True, {
            "scheduled_cancel": target_storage_bytes == 0,
            "scheduled_change": True,
            "addon_storage_bytes": addon_storage_bytes,
            "target_storage_bytes": target_storage_bytes,
            **data,
        }

    storage_price_id = (get_storage_price_id_from_config() or "").strip()
    if not storage_price_id:
        return False, {"error": "Storage price is not configured."}

    has_payment_method = await has_reusable_payment_method_async(
        customer_id=customer_id,
        subscription=stripe_sub,
    )
    if not has_payment_method:
        logging.info(
            "Storage change has no reusable payment method; starting setup Checkout: tenant_id=%s, subscription_id=%s, target_storage_bytes=%s",
            tenant_id,
            main_subscription_id,
            target_storage_bytes,
        )
        session = await _create_billing_setup_checkout_session(
            tenant_id=tenant_id,
            customer_id=customer_id,
            session_success_url=session_success_url,
            session_cancel_url=session_cancel_url,
            metadata={
                "price_type": PriceType.SUBSCRIPTION,
                "price_id": storage_price_id,
                "product_name": "storage",
                "setup_for_storage_change": "1",
                "subscription_id": main_subscription_id,
                "target_storage_bytes": str(target_storage_bytes),
            },
        )
        return True, {
            "addon_storage_bytes": addon_storage_bytes,
            "target_storage_bytes": target_storage_bytes,
            "redirect_to": session.url,
            "requires_payment_method_setup": True,
        }

    try:
        if item_id:
            updated = await stripe.Subscription.modify_async(
                main_subscription_id,
                items=[{"id": item_id, "quantity": target_quantity}],
                proration_behavior="always_invoice",
                payment_behavior="pending_if_incomplete",
                billing_cycle_anchor="unchanged",
                expand=["latest_invoice"],
                idempotency_key=f"{tenant_id}:{main_subscription_id}:storage-modify:{uuid.uuid4()}",
            )
        else:
            updated = await stripe.Subscription.modify_async(
                main_subscription_id,
                items=[{"price": storage_price_id, "quantity": target_quantity}],
                proration_behavior="always_invoice",
                payment_behavior="pending_if_incomplete",
                billing_cycle_anchor="unchanged",
                expand=["latest_invoice"],
                idempotency_key=f"{tenant_id}:{main_subscription_id}:storage-add:{uuid.uuid4()}",
            )
    except stripe.InvalidRequestError as e:
        if "Item already exists" in str(e):
            updated = await stripe.Subscription.retrieve_async(main_subscription_id, expand=["latest_invoice"])
        else:
            return False, {"error": f"Failed to modify subscription: {e}"}

    latest_invoice = extract_latest_invoice_obj(updated)
    if isinstance(latest_invoice, dict):
        invoice_url = latest_invoice.get("hosted_invoice_url", "") or ""
    else:
        invoice_url = getattr(latest_invoice, "hosted_invoice_url", "") or ""

    _sync_storage_subscription_record(
        tenant_id,
        updated,
        customer_id=(tenant_plan.get("customer_id") or "").strip(),
        target_storage_bytes=target_storage_bytes,
    )
    return True, {
        "addon_storage_bytes": addon_storage_bytes,
        "target_storage_bytes": target_storage_bytes,
        "redirect_to": invoice_url,
    }


async def _get_tenant_plan_with_customer_id(tenant_id: str, *, require_quota_info: bool = False) -> dict:
    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id, require_quota_info=require_quota_info)
    customer_id = (tenant_plan.get("customer_id") or "").strip()
    if not customer_id:
        logging.warning(
            "No customer_id found while loading billing plan, expected one after registration; "
            "trying to create a Stripe customer for tenant %s",
            tenant_id,
        )
        customer_id = await billing_set_customer_id_async(tenant_id)
        tenant_plan["customer_id"] = customer_id
    return tenant_plan


def _build_main_subscription_overview_base(tenant_plan: dict) -> dict:
    return {
        "customer_id": (tenant_plan.get("customer_id") or "").strip(),
        "subscription_id": (tenant_plan.get("subscription_id") or "").strip(),
        "price_id": (tenant_plan.get("price_id") or "").strip(),
        "plan_name": tenant_plan.get("plan_name", "unknown"),
        "subscription_status": tenant_plan.get("subscription_status", ""),
        "billing_cycle": {
            "start": to_utc_isoformat(tenant_plan.get("start_time")),
            "end": to_utc_isoformat(tenant_plan.get("end_time")),
        },
        **_main_subscription_payment_state(tenant_plan),
    }


def _serialize_pending_subscription_change(change: dict) -> dict:
    serialized = dict(change or {})
    effective_at = to_utc_isoformat(serialized.get("effective_at"))
    if effective_at:
        serialized["effective_at"] = effective_at
    return serialized


def _serialize_current_plan_payload(tenant_plan: dict) -> dict:
    payload = dict(tenant_plan or {})
    for field in ("start_time", "end_time"):
        iso_value = to_utc_isoformat(payload.get(field))
        if iso_value:
            payload[field] = iso_value
    pending_change = payload.get("pending_subscription_change")
    if isinstance(pending_change, dict):
        payload["pending_subscription_change"] = _serialize_pending_subscription_change(pending_change)
    return payload


@manager.route("/current_plan", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_current_plan():
    tenant_plan = await _get_tenant_plan_with_customer_id(current_user.id)

    subscription_id = (tenant_plan.get("subscription_id") or "").strip()
    if subscription_id:
        tenant_plan["pending_subscription_change"] = await get_pending_subscription_change_async(subscription_id)
    tenant_plan.update(_main_subscription_payment_state(tenant_plan))
    return get_json_result(data=_serialize_current_plan_payload(tenant_plan))


@manager.route("/storage/current", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_storage_current():
    tenant_id = request.args.get("tenant_id", current_user.id)
    if current_user.id != tenant_id:
        return get_json_result(
            data=False,
            message="No authorization.",
            code=RetCode.AUTHENTICATION_ERROR,
        )

    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id)
    storage = SubscriptionService.get_by_tenant_id(tenant_id) or {}
    storage_current_period_start = to_utc_datetime(tenant_plan.get("start_time"))
    storage_current_period_end = to_utc_datetime(tenant_plan.get("end_time"))
    unit_price = await _get_storage_unit_price_async()

    stripe_sub = None
    stripe_quantity_gb = 0
    sub_id = (tenant_plan.get("subscription_id") or "").strip()
    if sub_id:
        stripe_sub = await stripe.Subscription.retrieve_async(sub_id, expand=["latest_invoice"])
        _item_id, stripe_price_id, stripe_quantity_gb = extract_storage_subscription_item(stripe_sub)
        stripe_period_start, stripe_period_end = extract_subscription_period(stripe_sub)
        storage_current_period_start = stripe_period_start or storage_current_period_start
        storage_current_period_end = stripe_period_end or storage_current_period_end
        storage_status = (get_attr_or_item(stripe_sub, "status", "") or "").strip().lower()
        if storage_status:
            storage["status"] = storage_status
        if stripe_price_id:
            storage["price_id"] = stripe_price_id
            unit_price = await _get_storage_unit_price_async(stripe_price_id)

    # When storage subscription renewal fails, expose the invoice URL so the
    # frontend can offer a "Pay Invoice" link — mirroring the main plan flow.
    storage_status = (storage.get("status") or "").strip().lower()
    storage_payment_required = storage_status in {"past_due", "unpaid", "incomplete"}
    storage_invoice_url = ""
    if storage_payment_required:
        if stripe_sub is not None:
            _, _, _, storage_invoice_url = await is_subscription_latest_invoice_paid_async(stripe_sub)

    addon_storage_bytes = _current_storage_effective_bytes(storage, stripe_quantity_gb)
    target_storage_bytes = await _get_storage_target_storage_bytes_async(
        storage,
        stripe_sub,
    )

    data = {
        "tenant_id": tenant_id,
        "plan_name": tenant_plan.get("plan_name", ""),
        "trial_forbidden": is_trial_plan_name(tenant_plan.get("plan_name", "")),
        "unit_price": unit_price,
        "addon_storage_bytes": addon_storage_bytes,
        "target_storage_bytes": target_storage_bytes,
        "subscription_id": storage.get("subscription_id", ""),
        "price_id": storage.get("price_id", ""),
        "status": storage_status,
        "cancel_at_period_end": bool(storage.get("cancel_at_period_end", False)),
        "current_period_start": storage_current_period_start,
        "current_period_end": storage_current_period_end,
        "payment_required": storage_payment_required,
        "payment_recovery_url": storage_invoice_url,
    }
    return get_json_result(data=data)


@manager.route("/storage/set-target", methods=["POST"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_storage_set_target():
    req = await get_request_json()
    tenant_id = req.get("tenant_id", current_user.id)
    if current_user.id != tenant_id:
        return get_json_result(
            data=False,
            message="No authorization.",
            code=RetCode.AUTHENTICATION_ERROR,
        )

    target_storage = req.get("target_storage_bytes")
    try:
        target_storage_bytes = int(target_storage)
    except (TypeError, ValueError):
        return get_json_result(data=False, message="target_storage_bytes must be an integer.", code=RetCode.BAD_REQUEST)
    if target_storage_bytes < 0:
        return get_json_result(data=False, message="target_storage_bytes must be >= 0.", code=RetCode.BAD_REQUEST)

    ok, data = await _set_storage_target_storage_bytes_async(
        tenant_id,
        target_storage_bytes,
        session_success_url=req.get("session_success_url", settings.BILLING["session_success_url"]),
        session_cancel_url=req.get("session_cancel_url", settings.BILLING["session_cancel_url"]),
    )
    if not ok:
        return get_data_error_result(message=data.get("error", "Failed to set storage target."))
    return get_json_result(data=data)


@manager.route("/plan_overview", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_plan_overview():
    """
    Get a comprehensive overview of the current plan including:
    - Storage usage and limits
    - Member count and limits
    - App count and limits
    - API request limits (if applicable)
    """
    tenant_id = request.args.get("tenant_id", current_user.id)

    tenant_plan = await _get_tenant_plan_with_customer_id(tenant_id, require_quota_info=True)
    plan_name = tenant_plan.get("plan_name", BILLING_PLAN_TRIAL_NAME)
    plan_info = settings.BILLING_PLAN_TO_INFO.get(plan_name) or settings.BILLING_PLAN_TO_INFO.get(plan_name.title()) or {}
    plan_quota_points = safe_int(plan_info.get("quota_points", 0), 0)

    points_balance = PointAccountService.get_balance(tenant_id) or {}
    plan_points_used = safe_int(points_balance.get("consumed_plan_points", 0), 0)
    addon_points_total = safe_int(points_balance.get("addon_purchased_points", 0), 0)
    addon_points_used = safe_int(points_balance.get("consumed_addon_points", 0), 0)

    storage_used_bytes = FileService.get_total_size_by_tenant_id(tenant_id) or 0
    storage_limit_bytes = tenant_plan.get("quota_storage", 0) or 0

    # Extract the relevant information for the overview
    plan_overview = {
        **_build_main_subscription_overview_base(tenant_plan),
        "resources": {
            "plan_storage": {
                "used": storage_used_bytes,
                "limit": storage_limit_bytes,
                "unit": "bytes",
            },
            "addon_storage": {
                "used": 0,
                "limit": 0,
                "unit": "bytes",
            },
            "plan_points": {
                "used": plan_points_used,
                "limit": plan_quota_points,
                "unit": "points",
            },
            "addon_points": {
                "used": addon_points_used,
                "limit": addon_points_total,
                "unit": "points",
            },
            "members": {
                "used": tenant_plan.get("num_members", 0),
                "limit": tenant_plan.get("quota_members", 0),
                "unit": "users",
            },
            "apps": {
                "used": tenant_plan.get("num_apps", 0),
                "limit": tenant_plan.get("quota_apps", 0),
                "unit": "applications",
            },
        },
        "api_request_limits": {
            "requests_per_minute": _get_api_request_limit_by_plan(tenant_plan.get("plan_name", BILLING_PLAN_TRIAL_NAME)),
        },
    }

    addon_storage_bytes = _storage_effective_bytes(tenant_id)
    if addon_storage_bytes > 0:
        plan_overview["resources"]["addon_storage"]["limit"] = addon_storage_bytes

    total_storage_limit_bytes = storage_limit_bytes + addon_storage_bytes
    if storage_used_bytes > storage_limit_bytes:
        plan_overview["resources"]["plan_storage"]["used"] = storage_limit_bytes
        plan_overview["resources"]["addon_storage"]["used"] = min(storage_used_bytes - storage_limit_bytes, max(addon_storage_bytes, 0))
    elif total_storage_limit_bytes <= 0:
        plan_overview["resources"]["addon_storage"]["used"] = 0

    return get_json_result(data=plan_overview)


def _get_api_request_limit_by_plan(plan_name: str) -> int:
    """
    Get per-minute API request limit based on plan type.

    Args:
        plan_name: Name of the plan (e.g., "Trial", "Starter", "Pro", "Enterprise")

    Returns:
        Request limit as integer
    """
    key = (plan_name or "").strip()
    if not key:
        return LIMITED_API_REQUESTS

    info = settings.BILLING_PLAN_TO_INFO.get(key) or settings.BILLING_PLAN_TO_INFO.get(key.title()) or {}
    if not info:
        info = settings.BILLING_PLAN_TO_INFO.get(BILLING_PLAN_TRIAL_NAME) or {}
    value = info.get("api_request_limit_per_minute")

    if not value:
        return LIMITED_API_REQUESTS
    try:
        return int(value)
    except (TypeError, ValueError):
        return LIMITED_API_REQUESTS


@manager.route("/addon_overview", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_addon_overview():
    """
    Get a comprehensive overview of usage-based products including:
    - DeepDoc page usage and limits
    - Storage usage (if implemented as usage-based)
    - Token usage and limits
    """
    tenant_id = request.args.get("tenant_id", current_user.id)

    purchased_products = PurchasedProductOverviewService.query(tenant_id=tenant_id)
    now_utc = datetime.now(timezone.utc)

    usage_overview = {
        "tenant_id": tenant_id,
        "deepdoc_pages": {"purchased": 0, "remaining": 0, "used": 0, "unit": "pages"},
        "storage": {"purchased": 0, "remaining": 0, "used": 0, "unit": "KB"},
        "tokens": {"purchased": 0, "remaining": 0, "used": 0, "unit": "tokens"},
    }

    for product in purchased_products:
        expiry_time = to_utc_datetime(getattr(product, "expiry_time", None))
        if expiry_time and expiry_time < now_utc:
            continue
        product_name = product.product_name.lower() if product.product_name else ""
        quantity = product.quantity if product.quantity else 0

        if "deepdoc" in product_name:
            usage_overview["deepdoc_pages"]["purchased"] += quantity
            usage_overview["deepdoc_pages"]["remaining"] += quantity

        elif "token" in product_name:
            usage_overview["tokens"]["purchased"] += quantity
            usage_overview["tokens"]["remaining"] += quantity

    storage_quota_kb = _storage_effective_kb(tenant_id)
    usage_overview["storage"]["purchased"] = storage_quota_kb
    usage_overview["storage"]["remaining"] = storage_quota_kb

    num_storage_in_kb = FileService.get_total_size_by_tenant_id(tenant_id) // 1024
    usage_overview["storage"]["remaining"] -= num_storage_in_kb

    return get_json_result(data=usage_overview)


@manager.route("/points/checkout", methods=["POST"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_points_checkout():
    """Create a Stripe Checkout session for purchasing points."""
    req = await get_request_json()
    tenant_id = req.get("tenant_id", current_user.id)
    session_success_url = req.get("session_success_url", settings.BILLING["session_success_url"])
    session_cancel_url = req.get("session_cancel_url", settings.BILLING["session_cancel_url"])
    if current_user.id != tenant_id:
        return get_json_result(data=False, message="No authorization.", code=RetCode.AUTHENTICATION_ERROR)

    quantity = req.get("quantity")
    try:
        quantity = int(quantity)
    except (TypeError, ValueError):
        return get_json_result(data=False, message="quantity must be an integer.", code=RetCode.BAD_REQUEST)
    if quantity <= 0:
        return get_json_result(data=False, message="quantity must be positive.", code=RetCode.BAD_REQUEST)

    recharge_config = settings.BILLING.get("points_recharge") or {}
    price_id = (recharge_config.get("price_id") or "").strip()
    points_per_unit = int(recharge_config.get("points_per_unit") or 100)
    if not price_id or price_id == "price_xxx":
        return get_json_result(data=False, message="Points recharge is not configured.", code=RetCode.SERVER_ERROR)

    points = quantity * points_per_unit
    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id)
    customer_id = (tenant_plan.get("customer_id") or "").strip()
    if not customer_id:
        customer_id = await billing_set_customer_id_async(tenant_id)
    if not customer_id:
        return get_json_result(data=False, message="Customer not found.", code=RetCode.SERVER_ERROR)

    session = await stripe.checkout.Session.create_async(
        customer=customer_id,
        mode="payment",
        line_items=[{"price": price_id, "quantity": quantity}],
        metadata={
            "payment_type": "points_recharge",
            "tenant_id": tenant_id,
            "points_amount": str(points),
        },
        success_url=_build_checkout_success_url(session_success_url),
        cancel_url=session_cancel_url,
    )
    return get_json_result(data={"checkout_url": session.url})


@manager.route("/points/price", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_points_price():
    """Return the points recharge price info from Stripe."""
    recharge_config = settings.BILLING.get("points_recharge") or {}
    price_id = (recharge_config.get("price_id") or "").strip()
    points_per_unit = int(recharge_config.get("points_per_unit") or 100)

    if not price_id or price_id == "price_xxx":
        return get_json_result(data=False, message="Points recharge is not configured.", code=RetCode.SERVER_ERROR)

    price_obj = await stripe.Price.retrieve_async(price_id)
    unit_amount = getattr(price_obj, "unit_amount", None)
    price_usd = unit_amount / 100 if unit_amount is not None else None

    return get_json_result(data={
        "price_id": price_id,
        "price_usd": price_usd,
        "points_per_unit": points_per_unit,
    })


@manager.route("/deepdoc/usage", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_deepdoc_usage():
    """Return DeepDoc usage summary derived from the point ledger."""
    tenant_id = request.args.get("tenant_id", current_user.id)
    if current_user.id != tenant_id:
        return get_json_result(data=False, message="No authorization.", code=RetCode.AUTHENTICATION_ERROR)

        price_point = PricePointService.get_by_name("deepdoc") or {}
        consuming_point_amount = price_point.get("consuming_point_amount") or 1

        # Committed holds = successfully parsed pages in the current billing cycle
        subscription = SubscriptionService.get_by_tenant_id(tenant_id)
        cycle_start = subscription.get("start_time") if subscription else None
        cycle_end = subscription.get("end_time") if subscription else None
        start_ms = int(to_utc_datetime(cycle_start).timestamp() * 1000) if cycle_start else None
        end_ms = int(to_utc_datetime(cycle_end).timestamp() * 1000) if cycle_end else None

        committed_points = 0
        held_points = 0

        with DB.connection_context():
            query = PointHold.select(PointHold.points, PointHold.status, PointHold.create_time).where(
                PointHold.tenant_id == tenant_id,
            )
            for hold in query:
                if hold.status == "committed":
                    if start_ms is None or (hold.create_time >= start_ms and hold.create_time <= end_ms):
                        committed_points += hold.points
                elif hold.status == "held":
                    held_points += hold.points

        pages_paid = committed_points // consuming_point_amount
        pages_unpaid = held_points // consuming_point_amount
        # 1 point = 1 cent = 0.01 USD
        amount_paid = round(committed_points / 100, 2)
        amount_unpaid = round(held_points / 100, 2)

    return get_json_result(data={
        "current_period_start": to_utc_date_str(cycle_start),
        "current_period_end": to_utc_date_str(cycle_end),
        "deepdoc": {
            "pages_paid": pages_paid,
            "pages_unpaid": pages_unpaid,
            "amount_paid": amount_paid,
            "amount_unpaid": amount_unpaid,
            "currency": "USD",
        },
    })


@manager.route("/points/balance", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_points_balance():
    """Return normalized point usage for the authenticated tenant."""
    tenant_id = request.args.get("tenant_id", current_user.id)
    if current_user.id != tenant_id:
        return get_json_result(data=False, message="No authorization.", code=RetCode.AUTHENTICATION_ERROR)

    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id)
    plan_name = tenant_plan.get("plan_name", BILLING_PLAN_TRIAL_NAME) if tenant_plan else BILLING_PLAN_TRIAL_NAME
    plan_info = settings.BILLING_PLAN_TO_INFO.get(plan_name, {})
    plan_quota = safe_int(plan_info.get("quota_points", 0), 0)

    raw_balance = PointAccountService.get_balance(tenant_id) or {}
    addon_total = safe_int(raw_balance.get("addon_purchased_points", 0), 0)
    plan_used = safe_int(raw_balance.get("consumed_plan_points", 0), 0)
    addon_used = safe_int(raw_balance.get("consumed_addon_points", 0), 0)

    normalized_balance = {
        "plan_points": {
            "used": max(0, plan_used),
            "limit": max(0, plan_quota),
            "unit": "points",
        },
        "addon_points": {
            "used": max(0, addon_used),
            "limit": max(0, addon_total),
            "unit": "points",
        },
    }

    return get_json_result(data=normalized_balance)


@manager.route("/points/ledger", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_points_ledger():
    """Return paginated point ledger entries for the authenticated tenant."""
    from api.db.db_models import PointLedger as PointLedgerModel
    tenant_id = request.args.get("tenant_id", current_user.id)
    if current_user.id != tenant_id:
        return get_json_result(data=False, message="No authorization.", code=RetCode.AUTHENTICATION_ERROR)
    page = max(1, int(request.args.get("page", 1)))
    page_size = max(1, min(100, int(request.args.get("page_size", 20))))
    event_type = request.args.get("event_type", "")
    start_date = request.args.get("start_date", "")
    end_date = request.args.get("end_date", "")

    with DB.connection_context():
        query = PointLedgerModel.select().where(PointLedgerModel.tenant_id == tenant_id)
        if event_type:
            query = query.where(PointLedgerModel.event_type == event_type)
        if start_date:
            start_dt = parse_datetime_arg(start_date)
            if start_dt:
                query = query.where(PointLedgerModel.create_time >= int(start_dt.timestamp() * 1000))
        if end_date:
            end_dt = parse_datetime_arg(end_date)
            if end_dt:
                query = query.where(PointLedgerModel.create_time <= int(end_dt.timestamp() * 1000))
        total = query.count()
        items = list(
            query.order_by(PointLedgerModel.create_time.desc())
            .paginate(page, page_size)
            .dicts()
        )
    return get_json_result(data={"total": total, "items": items})


@manager.route("/points/holds", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_points_holds():
    """Return paginated point hold records for the authenticated tenant."""
    from api.db.db_models import PointHold as PointHoldModel
    tenant_id = request.args.get("tenant_id", current_user.id)
    if current_user.id != tenant_id:
        return get_json_result(data=False, message="No authorization.", code=RetCode.AUTHENTICATION_ERROR)
    page = max(1, int(request.args.get("page", 1)))
    page_size = max(1, min(100, int(request.args.get("page_size", 20))))
    status_filter = request.args.get("status", "")
    start_date = request.args.get("start_date", "")
    end_date = request.args.get("end_date", "")

    with DB.connection_context():
        query = PointHoldModel.select().where(PointHoldModel.tenant_id == tenant_id)
        if status_filter:
            query = query.where(PointHoldModel.status == status_filter)
        if start_date:
            start_dt = parse_datetime_arg(start_date)
            if start_dt:
                query = query.where(PointHoldModel.create_time >= int(start_dt.timestamp() * 1000))
        if end_date:
            end_dt = parse_datetime_arg(end_date)
            if end_dt:
                query = query.where(PointHoldModel.create_time <= int(end_dt.timestamp() * 1000))
        total = query.count()
        items = list(
            query.order_by(PointHoldModel.create_time.desc())
            .paginate(page, page_size)
            .dicts()
        )
    return get_json_result(data={"total": total, "items": items})


@manager.route("/spend_overview", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_spend_overview():
    tenant_id = request.args.get("tenant_id", current_user.id)

    # Legacy implementation (Stripe invoices only).
    # This only reflects subscription invoices and will NOT include one-off
    # payments like storage add-ons (mode=payment -> PaymentIntent/Charge, no Invoice).
    #
    # tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id, require_quota_info=True)
    # customer_id = tenant_plan["customer_id"]
    # if not customer_id:
    #     return get_data_error_result(message="Internal error, cannot determine customer id")
    #
    # start_dt = parse_datetime_arg(request.args.get("start"))
    # end_dt = parse_datetime_arg(request.args.get("end"))
    # start_ts = int(start_dt.timestamp()) if start_dt else None
    # end_ts = int(end_dt.timestamp()) if end_dt else None
    #
    # query_filter = {"customer": customer_id}
    # if start_ts or end_ts:
    #     query_filter["created"] = {}
    #     if start_ts:
    #         query_filter["created"]["gte"] = start_ts
    #     if end_ts:
    #         query_filter["created"]["lte"] = end_ts
    #
    # invoices = await stripe.Invoice.list_async(limit=50, **query_filter)
    #
    # spend_overview = []
    # for inv in invoices:
    #     spend_overview.append(
    #         {
    #             "invoice_id": inv.id,
    #             "amount": inv.amount_paid / 100,
    #             "currency": inv.currency.upper() if inv.currency else None,
    #             "status": inv.status,
    #             "created_at": inv.created,
    #             "hosted_invoice_url": inv.hosted_invoice_url,
    #             "invoice_pdf_url": inv.invoice_pdf,
    #         }
    #     )
    # return get_json_result(data=spend_overview)

    start_dt = parse_datetime_arg(request.args.get("start"))
    end_dt = parse_datetime_arg(request.args.get("end"))
    page = max(1, int(request.args.get("page", 1)))
    page_size = max(1, min(100, int(request.args.get("page_size", 20))))
    status_map = {
        PaymentStatus.SUCCESS.value: "paid",
        PaymentStatus.FAILED.value: "unpaid",
        PaymentStatus.PENDING.value: "pending",
    }

    with DB.connection_context():
        # One row per invoice — no Python aggregation needed
        query = (
            PaymentOrder.select(
                PaymentOrder.order_id,
                PaymentOrder.amount_cents,
                PaymentOrder.currency,
                PaymentOrder.payment_status,
                PaymentOrder.order_created_at,
                PaymentOrder.receipt_url,
                PaymentOrder.receipt_pdf_url,
                PaymentOrder.product_ids,
                PaymentOrder.product_names,
                PaymentOrder.product_quantities,
                PaymentOrder.product_amount_cents,
            )
            .where(PaymentOrder.tenant_id == tenant_id)
            .order_by(PaymentOrder.order_created_at.desc())
        )

        if start_dt:
            query = query.where(PaymentOrder.order_created_at >= start_dt)
        if end_dt:
            query = query.where(PaymentOrder.order_created_at <= end_dt)

        total = query.count()
        orders = query.paginate(page, page_size)

        spend_overview = []
        for order in orders:
            created_at = _safe_payment_order_created_at(order.order_created_at, order.order_id)
            product_names = order.product_names or []
            product_quantities = order.product_quantities or []
            product_amount_cents = getattr(order, 'product_amount_cents', None) or []
            spend_overview.append({
                "invoice_id": order.order_id,
                "amount": float((order.amount_cents or 0) / 100),
                "currency": (order.currency or "").upper() if order.currency else None,
                "status": status_map.get(order.payment_status, "pending"),
                "created_at": int(created_at.timestamp() * 1000) if created_at else None,
                "hosted_invoice_url": order.receipt_url,
                "invoice_pdf_url": order.receipt_pdf_url or order.receipt_url,
                "product_ids": order.product_ids or [],
                "product": ", ".join(product_names) if product_names else "UNKNOWN",
                "product_quantities": product_quantities,
                "product_amount_cents": product_amount_cents,
            })

    return get_json_result(data={"total": total, "items": spend_overview})


@manager.route("/spend_metrics", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_spend_metrics():
    tenant_id = request.args.get("tenant_id", current_user.id)
    if not tenant_id:
        return get_data_error_result(message="Missing tenant_id.")

    start_dt = parse_datetime_arg(request.args.get("start"))
    end_dt = parse_datetime_arg(request.args.get("end"))
    if start_dt and end_dt and end_dt < start_dt:
        return get_data_error_result(message="Invalid time range.")

    series_map: dict[str, Decimal] = {}
    total_spend = Decimal("0")
    currency = None
    category_map: dict[str, dict] = {}
    product_pricing_cache: dict[str, dict] = {}

    def _get_pricing(product_name: str) -> dict:
        cached = product_pricing_cache.get(product_name)
        if cached is not None:
            return cached
        price_point = PricePointService.get_by_name(product_name) or {}
        unit = price_point.get("unit") or ""
        unit_quantity = price_point.get("unit_quantity") or 0
        price_amount_cents = price_point.get("price_amount")
        price_currency = price_point.get("price_currency")
        pricing = {
            "unit": unit,
            "unit_quantity": unit_quantity,
            "price_amount": Decimal(str(price_amount_cents)) / 100 if price_amount_cents is not None else None,
            "price_currency": price_currency,
        }
        product_pricing_cache[product_name] = pricing
        return pricing

    def _detail_quantity(detail) -> Decimal | None:
        if not detail:
            return None
        if isinstance(detail, dict):
            payload = detail
        else:
            try:
                payload = json.loads(detail)
            except (TypeError, ValueError):
                return None
        quantity = payload.get("quantity")
        if quantity is None:
            return None
        return decimal_amount(quantity)

    def _estimate_quantity(amount: Decimal, product_name: str, order_currency: str) -> Decimal:
        pricing = _get_pricing(product_name)
        price_amount = pricing.get("price_amount")
        unit_quantity = pricing.get("unit_quantity") or 0
        price_currency = pricing.get("price_currency")
        if not price_amount or price_amount <= 0 or unit_quantity <= 0:
            return Decimal("0")
        if price_currency and order_currency and price_currency != order_currency:
            return Decimal("0")
        return (amount / price_amount) * Decimal(str(unit_quantity))

    with DB.connection_context():
        query = (
            PaymentOrder.select(
                PaymentOrder.order_id,
                PaymentOrder.product_names,
                PaymentOrder.amount_cents,
                PaymentOrder.currency,
                PaymentOrder.order_created_at,
                PaymentOrder.payment_detail,
            )
            .where(
                PaymentOrder.tenant_id == tenant_id,
                PaymentOrder.payment_type == PriceType.ADDON,
                PaymentOrder.paid,
            )
            .order_by(PaymentOrder.order_created_at.asc())
        )
        if start_dt:
            query = query.where(PaymentOrder.order_created_at >= start_dt)
        if end_dt:
            query = query.where(PaymentOrder.order_created_at <= end_dt)

        for order in query:
            created_at = _safe_payment_order_created_at(order.order_created_at, order.order_id)
            if not created_at:
                continue
            order_amount = Decimal(str(order.amount_cents or 0)) / 100
            order_currency = order.currency
            if currency is None:
                currency = order_currency
            total_spend += order_amount
            date_key = created_at.date().isoformat()
            series_map[date_key] = series_map.get(date_key, Decimal("0")) + order_amount

            product_names = order.product_names or []
            # Distribute amount across products for multi-product orders
            product_count = len(product_names) or 1
            per_product_amount = order_amount / product_count
            for product_name in product_names:
                category = category_map.setdefault(
                    product_name,
                    {
                        "product_name": product_name,
                        "unit": _get_pricing(product_name).get("unit") or "",
                        "total_spend": Decimal("0"),
                        "total_quantity": Decimal("0"),
                        "series_map": {},
                        "quantity_series_map": {},
                    },
                )
                category["total_spend"] += per_product_amount
                category["series_map"][date_key] = category["series_map"].get(date_key, Decimal("0")) + per_product_amount
                quantity = _detail_quantity(order.payment_detail)
                if quantity is None:
                    quantity = _estimate_quantity(per_product_amount, product_name, order_currency)
                if quantity:
                    per_quantity = quantity / product_count
                    category["total_quantity"] += per_quantity
                    category["quantity_series_map"][date_key] = category["quantity_series_map"].get(date_key, Decimal("0")) + per_quantity

    date_keys = []
    if start_dt and end_dt:
        date_keys = build_date_keys(start_dt, end_dt)

    if date_keys:
        series = [{"date": d, "spend": amount_to_float(series_map.get(d, Decimal("0")))} for d in date_keys]
    else:
        series = [{"date": d, "spend": amount_to_float(v)} for d, v in sorted(series_map.items())]

    categories = []
    for category in category_map.values():
        category_series_map = category.pop("series_map")
        quantity_series_map = category.pop("quantity_series_map")
        if date_keys:
            category_series = [{"date": d, "spend": amount_to_float(category_series_map.get(d, Decimal("0")))} for d in date_keys]
            quantity_series = [{"date": d, "quantity": amount_to_float(quantity_series_map.get(d, Decimal("0")))} for d in date_keys]
        else:
            category_series = [{"date": d, "spend": amount_to_float(v)} for d, v in sorted(category_series_map.items())]
            quantity_series = [{"date": d, "quantity": amount_to_float(v)} for d, v in sorted(quantity_series_map.items())]
        category["total_spend"] = amount_to_float(category["total_spend"])
        category["total_quantity"] = amount_to_float(category["total_quantity"])
        category["series"] = category_series
        category["quantity_series"] = quantity_series
        categories.append(category)

    return get_json_result(
        data={
            "tenant_id": tenant_id,
            "currency": currency.upper() if currency else None,
            "total_spend": amount_to_float(total_spend),
            "series": series,
            "categories": categories,
        }
    )


@manager.route("/plans", methods=["GET"])  # noqa: F821
@billing_enabled_guard(_billing_disabled_response)
async def billing_all_plans():
    price_ids = []
    for plan_name, info in settings.BILLING_PLAN_TO_INFO.items():
        plan_price_ids = info.get("price_ids", [])
        for pid in plan_price_ids:
            if pid and pid != "price_xxx":
                price_ids.append((plan_name, pid))

    price_dict = {}
    if price_ids and settings.BILLING_ENABLED:
        for plan_name, price_id in price_ids:
            price_obj = await stripe.Price.retrieve_async(price_id)
            logging.info(f"billing_all_plans Stripe price: plan={plan_name}, price_id={price_id}, price_obj={price_obj}")
            unit_amount = getattr(price_obj, "unit_amount", None)
            price_dict[plan_name] = unit_amount / 100 if unit_amount else -1
    if BILLING_PLAN_TRIAL_NAME not in price_dict:
        price_dict[BILLING_PLAN_TRIAL_NAME] = 0
    logging.info(f"billing_all_plans price_dict={price_dict}")

    latest_plans = ProductService.get_latest_by_type(ProductType.SUBSCRIPTION)

    plans = []
    latest_plans = list(latest_plans)
    latest_plans.sort(
        key=lambda plan: (
            settings.BILLING_PLAN_TO_INFO.get(plan.name, {}).get("priority", 9999),
            plan.name,
        )
    )
    for plan in latest_plans:
        plan_info = settings.BILLING_PLAN_TO_INFO.get(plan.name, {})
        quota_storage = getattr(plan, "quota_storage", 0) or plan_info.get("quota_storage", 0) or 0
        quota_points = plan_info.get("quota_points", 0)
        p = {
            "id": plan.id,
            "name": plan.name,
            "price": price_dict.get(plan.name, -1),
            "description": plan.description,
            "price_ids": plan.price_ids,
            "feature": {
                "quota_apps": plan.quota_apps,
                "quota_members": plan.quota_members,
                "quota_storage": quota_storage,
                "quota_points": quota_points,
                "quota_api_limits": _get_api_request_limit_by_plan(plan.name),
                "price_per_gb": _calc_storage_price_per_gb(
                    price_dict.get(plan.name, -1),
                    quota_storage,
                ),
            },
        }
        plans.append(p)

    return get_json_result(data=plans)


def _calc_storage_price_per_gb(price_usd: float, quota_storage: int) -> float:
    """Calculate storage price per GB from plan price and quota. Returns 0 if unavailable."""
    if price_usd <= 0 or quota_storage <= 0:
        return 0.0
    quota_gb = quota_storage / (1000 * 1000 * 1000)
    return price_usd / quota_gb if quota_gb > 0 else 0.0


@manager.route("/addon_plans", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_all_addon_plans():
    latest_products = ProductService.get_latest_by_type(ProductType.ADDON)
    latest_products = list(latest_products)

    price_dict = {}
    if latest_products and settings.BILLING_ENABLED:
        for product in latest_products:
            plan_info = settings.BILLING_PLAN_TO_INFO.get(product.name, {})
            for price_id in plan_info.get("price_ids", []):
                if price_id and price_id != "price_xxx":
                    price_obj = await stripe.Price.retrieve_async(price_id)
                    unit_amount = getattr(price_obj, "unit_amount", None)
                    price_dict[product.name] = unit_amount / 100 if unit_amount else -1
                    break
    addon_plans = []
    latest_products.sort(key=lambda product: product.name)
    for product in latest_products:
        plan_info = settings.BILLING_PLAN_TO_INFO.get(product.name, {})
        product_quota_apps = getattr(product, "quota_apps", 0) or 0
        product_quota_members = getattr(product, "quota_members", 0) or 0
        product_quota_storage = getattr(product, "quota_storage", 0) or plan_info.get("quota_storage", 0) or 0
        product_quota_points = plan_info.get("quota_points", 0) or 0
        product_quota_api_limits = plan_info.get("api_request_limit_per_minute", 0) or 0
        addon_plans.append(
            {
                "id": product.id,
                "name": product.name,
                "price": price_dict.get(product.name, -1),
                "description": product.description,
                "price_ids": product.price_ids,
                "feature": {
                    "quota_apps": product_quota_apps,
                    "quota_members": product_quota_members,
                    "quota_storage": product_quota_storage,
                    "quota_points": product_quota_points,
                    "quota_api_limits": product_quota_api_limits,
                    "price_per_gb": _calc_storage_price_per_gb(
                        price_dict.get(product.name, -1),
                        product_quota_storage,
                    ),
                },
            }
        )

    return get_json_result(data=addon_plans)


@manager.route("/upcoming", methods=["POST"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_upcoming():
    """
    Preview the invoice amount due today for a plan upgrade.

    This endpoint is called by the frontend *before* the user confirms an upgrade,
    so the confirmation dialog can display the prorated charge ("You will be billed
    $X today").  It does NOT initiate any payment.

    Request body (JSON):
        new_price_id (str): Stripe price ID of the target plan.
        tenant_id (str, optional): Defaults to the authenticated user.
        customer_id (str, optional): Stripe customer ID override.

    Behavior:
        - If the tenant has an active or past_due subscription, calls
          stripe.Invoice.create_preview with the existing subscription and the new
          price to calculate the exact proration amount.
        - If the tenant has no subscription or the existing subscription is in a
          non-modifiable state (e.g. canceled, trialing), calls
          stripe.Invoice.create_preview without a subscription to simulate the
          first charge for a brand-new subscription.

    Response data:
        amount_due_today (float): Amount in the subscription currency, e.g. 12.50.
        currency (str): ISO currency code, e.g. "usd".
        invoice_preview (dict): Full Stripe invoice preview object.
    """
    req = await get_request_json()

    new_price_id = req.get("new_price_id")
    tenant_id = req.get("tenant_id") or current_user.id
    target_storage_bytes_raw = req.get("target_storage_bytes")

    current_plan = SubscriptionService.get_by_tenant_id(tenant_id=tenant_id)
    customer_id = req.get("customer_id") or current_plan.get("customer_id")
    subscription_id = current_plan.get("subscription_id")

    logging.debug(
        "billing_upcoming request: tenant_id=%s, customer_id=%s, subscription_id=%s, new_price_id=%s",
        tenant_id,
        customer_id,
        subscription_id,
        new_price_id,
    )
    if not new_price_id and target_storage_bytes_raw is None:
        return get_data_error_result(message="Missing required parameters")
    if not customer_id:
        customer_id = await billing_set_customer_id_async(tenant_id)
    if not customer_id:
        return get_data_error_result(message="Missing required parameters")

    if target_storage_bytes_raw is not None:
        if not subscription_id:
            return get_data_error_result(message="No active plan subscription found")

        try:
            target_storage_bytes = int(target_storage_bytes_raw)
        except (TypeError, ValueError):
            return get_data_error_result(message="target_storage_bytes must be an integer")

        if target_storage_bytes < 0:
            return get_data_error_result(message="target_storage_bytes must be non-negative")
        if target_storage_bytes % BYTES_PER_GB != 0:
            return get_data_error_result(message="Storage quantity must be a multiple of 1GB")

        subscription = stripe.Subscription.retrieve(subscription_id)
        if not subscription or not subscription.get("items") or not subscription["items"].get("data"):
            return get_data_error_result(message="Subscription items not found")

        plan_item_id, plan_price_id, plan_quantity = extract_plan_subscription_item(subscription)
        if not plan_item_id or not plan_price_id:
            return get_data_error_result(message="Plan subscription item not found")

        storage_item_id, storage_price_id, _current_storage_quantity = extract_storage_subscription_item(subscription)
        target_storage_quantity = storage_bytes_to_quantity(target_storage_bytes)
        preview_items = [
            {
                "id": plan_item_id,
                "price": plan_price_id,
                "quantity": max(plan_quantity, 1),
            }
        ]

        resolved_storage_price_id = storage_price_id or get_storage_price_id_from_config()
        if storage_item_id:
            if target_storage_quantity > 0:
                preview_items.append(
                    {
                        "id": storage_item_id,
                        "price": resolved_storage_price_id,
                        "quantity": target_storage_quantity,
                    }
                )
            else:
                preview_items.append({"id": storage_item_id, "deleted": True})
        elif target_storage_quantity > 0 and resolved_storage_price_id:
            preview_items.append(
                {
                    "price": resolved_storage_price_id,
                    "quantity": target_storage_quantity,
                }
            )

        upcoming_invoice = stripe.Invoice.create_preview(
            customer=customer_id,
            subscription=subscription_id,
            subscription_details={
                "proration_behavior": "always_invoice",
                "items": preview_items,
            },
        )

        amount_due_today = upcoming_invoice.total / 100.0
        return get_json_result(
            data={
                "amount_due_today": amount_due_today,
                "currency": upcoming_invoice.currency,
                "invoice_preview": upcoming_invoice,
            }
        )

    # Check if subscription exists and is in a valid state for modification
    # Only "active" or "past_due" subscriptions can be modified.
    # If subscription was canceled, treat as new subscription request.
    is_valid_subscription = False
    if subscription_id:
        sub = stripe.Subscription.retrieve(subscription_id)
        if not sub or not sub["items"] or not sub["items"]["data"]:
            return get_data_error_result(message="Subscription items not found")

        # Only allow modification of active or past_due subscriptions
        # For canceled/trialing/other states, create new subscription instead
        sub_status = sub.get("status")
        logging.info(f"billing_upcoming: subscription {subscription_id} has status={sub_status}")
        if sub_status in ("active", "past_due"):
            is_valid_subscription = True
        else:
            logging.info(f"Subscription {subscription_id} is {sub_status}; treating as new subscription request")

    force_new_subscription_preview = False
    old_item_id = ""
    current_price_id = ""
    current_quantity = 1
    storage_item_id = ""
    storage_price_id = ""
    storage_quantity = 0
    if is_valid_subscription:
        old_item_id, current_price_id, current_quantity = extract_plan_subscription_item(sub)
        storage_item_id, storage_price_id, storage_quantity = extract_storage_subscription_item(sub)
        if not old_item_id or not current_price_id:
            return get_data_error_result(message="Plan subscription item not found")
        current_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(current_price_id, "") or current_plan.get("plan_name", "")
        target_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(new_price_id, "")
        force_new_subscription_preview = _should_preview_as_new_subscription(current_plan_name, target_plan_name)
        if force_new_subscription_preview:
            logging.info(
                f"billing_upcoming: preview Trial->paid as new subscription, "
                f"current_plan={current_plan_name}, target_plan={target_plan_name}, customer={customer_id}"
            )

    if is_valid_subscription and not force_new_subscription_preview:
        logging.info(f"Previewing price change for subscription {subscription_id}: new_price_id={new_price_id}")
        preview_items = [
            {
                "id": old_item_id,
                "price": new_price_id,
                "quantity": max(current_quantity, 1),
            }
        ]
        if storage_item_id and storage_price_id:
            preview_items.append(
                {
                    "id": storage_item_id,
                    "price": storage_price_id,
                    "quantity": max(storage_quantity, 0),
                }
            )
        upcoming_invoice = stripe.Invoice.create_preview(
            customer=customer_id,
            subscription=subscription_id,
            subscription_details={
                "proration_behavior": "always_invoice",
                "items": preview_items,
            },
        )
    else:
        # For new subscriptions or when existing subscription is not active
        logging.info(f"Creating new subscription preview for customer {customer_id}: price_id={new_price_id}")
        upcoming_invoice = stripe.Invoice.create_preview(
            customer=customer_id,
            subscription_details={
                "proration_behavior": "always_invoice",
                "items": [
                    {
                        "price": new_price_id,
                        "quantity": 1,
                    }
                ],
            },
        )

    amount_due_today = upcoming_invoice.total / 100.0

    return get_json_result(
        data={
            "amount_due_today": amount_due_today,
            "currency": upcoming_invoice.currency,
            "invoice_preview": upcoming_invoice,
        }
    )


def _validate_billing_checkout_request(req: dict):
    """Normalize and validate checkout input so route logic stays branch-focused."""
    tenant_id = req.get("tenant_id") or current_user.id
    addon_price_id = req.get("addon_price_id")
    subscription_price_id = req.get("subscription_price_id")
    quantity = req.get("quantity", 1)
    payment_type = req.get("payment_type")
    expiry_time = req.get("expiry_time")
    session_success_url = req.get("session_success_url", settings.BILLING["session_success_url"])
    session_cancel_url = req.get("session_cancel_url", settings.BILLING["session_cancel_url"])

    if not tenant_id:
        return None, get_json_result(
            data=False,
            message="Missing required parameter tenant_id.",
            code=RetCode.BAD_REQUEST,
        )

    if not payment_type:
        return None, get_json_result(
            data=False,
            message="Missing required parameter payment_type.",
            code=RetCode.BAD_REQUEST,
        )

    try:
        quantity = int(quantity)
    except (TypeError, ValueError):
        return None, get_json_result(
            data=False,
            message="Invalid quantity.",
            code=RetCode.BAD_REQUEST,
        )

    if quantity < 0:
        return None, get_json_result(
            data=False,
            message="Quantity must be a non-negative integer.",
            code=RetCode.BAD_REQUEST,
        )

    if current_user.id != tenant_id:
        return None, get_json_result(
            data=False,
            message="No authorization.",
            code=RetCode.AUTHENTICATION_ERROR,
        )

    logging.info(f"{payment_type=}")
    logging.info(f"{subscription_price_id=}")

    if payment_type not in (PriceType.SUBSCRIPTION, PriceType.ADDON):
        return None, get_data_error_result(message="Unsupported payment type.")

    if payment_type == PriceType.SUBSCRIPTION and not subscription_price_id:
        return None, get_json_result(
            data=False,
            message="Missing required parameters subscription_price_id.",
            code=RetCode.BAD_REQUEST,
        )

    if payment_type == PriceType.ADDON and not addon_price_id:
        return None, get_json_result(
            data=False,
            message="Missing required parameters addon_price_id.",
            code=RetCode.BAD_REQUEST,
        )

    if payment_type == PriceType.SUBSCRIPTION and quantity <= 0:
        return None, get_json_result(
            data=False,
            message="Quantity must be a positive integer.",
            code=RetCode.BAD_REQUEST,
        )

    if payment_type == PriceType.SUBSCRIPTION and not float(quantity).is_integer():
        return None, get_json_result(
            data=False,
            message="Quantity must be an integer.",
            code=RetCode.BAD_REQUEST,
        )

    return {
        "tenant_id": tenant_id,
        "addon_price_id": addon_price_id,
        "subscription_price_id": subscription_price_id,
        "quantity": quantity,
        "payment_type": payment_type,
        "expiry_time": expiry_time,
        "session_success_url": session_success_url,
        "session_cancel_url": session_cancel_url,
    }, None


async def _handle_active_subscription_checkout(
    *,
    tenant_id: str,
    tenant_plan: dict,
    subscription_id: str,
    subscription_status: str,
    subscription_price_id: str,
    session_success_url: str,
    session_cancel_url: str,
):
    """Handle checkout for active/trialing subscriptions.

    Checkout only submits Stripe changes and returns accepted/pending-payment hints.
    Webhook handlers own all DB updates and side effects.
    """
    subscription = await stripe.Subscription.retrieve_async(subscription_id)
    if isinstance(subscription, dict):
        subscription_items = (subscription.get("items") or {}).get("data", []) or []
    else:
        subscription_items = getattr(getattr(subscription, "items", None), "data", []) or []

    _current_item_id, current_price_id, _current_quantity = extract_plan_subscription_item(subscription)
    current_item_price_ids = []
    for item in subscription_items:
        if isinstance(item, dict):
            price_obj = item.get("price", {})
            price_id = price_obj.get("id", "") if isinstance(price_obj, dict) else ""
        else:
            price_obj = getattr(item, "price", None)
            price_id = (getattr(price_obj, "id", "") or "") if price_obj else ""
        if price_id:
            current_item_price_ids.append(price_id)

    trial_price_id = get_trial_price_id(settings.BILLING.get("billing_plans", []))
    current_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(current_price_id, "") or tenant_plan.get("plan_name", "")
    requested_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, "")

    if subscription_price_id in current_item_price_ids:
        logging.info(
            f"Tenant {tenant_id} already has subscription {subscription_id} on price {subscription_price_id}, "
            f"current items: {current_item_price_ids}"
        )
        msg = (
            f"Tenant {tenant_id} already has an active subscription {subscription_id} on price {subscription_price_id}. "
            f"Requested price_id matches the current plan '{current_plan_name}'."
        )
        if trial_price_id and subscription_price_id != trial_price_id:
            msg += f" To downgrade, pass the Trial price_id {trial_price_id}."
        return get_json_result(
            data={
                "current_price_id": current_price_id,
                "current_plan_name": current_plan_name,
                "requested_price_id": subscription_price_id,
                "requested_plan_name": requested_plan_name,
                "trial_price_id": trial_price_id,
            },
            message=msg,
            code=RetCode.SUCCESS,
        )

    if current_price_id and is_downgrade_by_price_id(current_price_id, subscription_price_id):
        target_plan_name_for_downgrade = settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, "")
        is_trial_target = is_trial_plan_name(target_plan_name_for_downgrade)

        usage_exceeded = _check_downgrade_resource_compatibility(tenant_id, target_plan_name_for_downgrade)
        if usage_exceeded:
            conflict_resources = [c["resource"] for c in usage_exceeded]
            msg = f"Resource usage exceeds {target_plan_name_for_downgrade} quota: {', '.join(conflict_resources)}. "
            msg += " ".join(c["message"] for c in usage_exceeded)
            return get_json_result(
                code=RetCode.BILLING_RESOURCE_INSUFFICIENT,
                data={"resource_conflicts": usage_exceeded},
                message=msg,
            )

        # When downgrading to Trial, cancel storage atomically in the same schedule
        # call so the two phases cannot diverge (a separate prior call would be
        # overwritten by the plan-change call which re-reads the live quantity).
        target_storage_quantity = 0 if is_trial_target else None
        scheduled = await schedule_subscription_price_change_at_period_end_async(
            subscription_id, subscription_price_id, target_storage_quantity=target_storage_quantity
        )
        if not scheduled:
            return get_data_error_result(message="Failed to schedule plan downgrade.")

        # When storage is being cancelled as part of a Trial downgrade, update the
        # DB target so storage quota reads reflect the pending cancellation immediately.
        if is_trial_target:
            from api.db.db_models import Subscription as _Sub
            _storage_item_id = (SubscriptionService.get_by_tenant_id(tenant_id) or {}).get("addon_subscription_item_id") or None
            with DB.atomic():
                _Sub.update(
                    target_storage_bytes=0,
                    addon_subscription_item_id=_storage_item_id,
                ).where(_Sub.tenant_id == tenant_id).execute()

        msg = f"Tenant {tenant_id} scheduled a plan downgrade at period end."
        return get_json_result(data={"scheduled_change": scheduled}, message=msg, code=RetCode.SUCCESS)

    target_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, "")
    is_trial_like_current = is_trial_plan_name(current_plan_name) or (subscription_status == "trialing")

    if isinstance(subscription, dict):
        subscription_customer_id = (subscription.get("customer") or "").strip()
    else:
        subscription_customer_id = (getattr(subscription, "customer", "") or "").strip()
    customer_id = (tenant_plan.get("customer_id") or "").strip() or subscription_customer_id

    if subscription_price_id != current_price_id:
        has_payment_method = await has_reusable_payment_method_async(
            customer_id=customer_id,
            subscription=subscription,
        )
        if not has_payment_method:
            logging.info(
                "Subscription upgrade has no reusable payment method; starting setup Checkout: tenant_id=%s, subscription_id=%s, current_price_id=%s, target_price_id=%s",
                tenant_id,
                subscription_id,
                current_price_id,
                subscription_price_id,
            )
            session = await _create_billing_setup_checkout_session(
                tenant_id=tenant_id,
                customer_id=customer_id,
                session_success_url=session_success_url,
                session_cancel_url=session_cancel_url,
                metadata={
                    "price_type": PriceType.SUBSCRIPTION,
                    "price_id": subscription_price_id,
                    "product_name": target_plan_name,
                    "setup_for_trial_upgrade": "1" if is_trial_like_current else "0",
                    "previous_subscription_id": subscription_id,
                },
            )
            logging.info(
                "Created setup Checkout session for subscription upgrade payment method collection: tenant_id=%s, session_id=%s",
                tenant_id,
                session.id,
            )
            return get_json_result(
                data={
                    "customer_id": customer_id,
                    "redirect_to": session.url,
                    "requires_payment_method_setup": True,
                },
                message="Please add a payment method to continue the upgrade.",
                code=RetCode.SUCCESS,
            )

    modified_result = await modify_subscription_plan_async(
        tenant_id=tenant_id,
        subscription_id=subscription_id,
        target_price_id=subscription_price_id,
        reset_billing_cycle=is_trial_like_current,
        end_trial_now=is_trial_like_current,
    )
    updated_subscription = modified_result.get("subscription")
    if not updated_subscription:
        return get_json_result(
            code=RetCode.BILLING_UPGRADE_FAILED,
            message=modified_result.get("error_message") or "Failed to modify subscription.",
            data=None,
        )

    invoice_id = modified_result.get("invoice_id", "")
    invoice_url = modified_result.get("invoice_url", "")
    invoice_status = modified_result.get("invoice_status", "")
    payment_intent_status = modified_result.get("payment_intent_status", "")
    plan_name_after_upgrade = target_plan_name or settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, "")

    payment_action_statuses = {
        "requires_action",
        "requires_payment_method",
        "requires_confirmation",
        "requires_source_action",
        "requires_source",
        "processing",
    }
    accepted_data = {
        "customer_id": (tenant_plan.get("customer_id") or "").strip()
        or (
            (getattr(updated_subscription, "customer", "") or "").strip()
            if not isinstance(updated_subscription, dict)
            else (updated_subscription.get("customer") or "").strip()
        ),
        "subscription_id": subscription_id,
        "plan_name": plan_name_after_upgrade,
        "price_id": subscription_price_id,
        "invoice_id": invoice_id,
        "invoice_url": invoice_url,
        "invoice_status": invoice_status,
        "amount_cents": modified_result.get("amount_cents", 0),
        "currency": modified_result.get("currency", ""),
        "payment_intent_status": payment_intent_status,
    }
    if payment_intent_status in payment_action_statuses and invoice_url:
        accepted_data["redirect_to"] = invoice_url

    return get_json_result(
        code=RetCode.SUCCESS,
        message="Subscription change request submitted. Final state will be updated by webhook.",
        data=accepted_data,
    )


async def _handle_recoverable_subscription_checkout(
    *,
    tenant_id: str,
    tenant_plan: dict,
    subscription_id: str,
    subscription_status: str,
    subscription_price_id: str,
):
    """Handle delinquent but recoverable subscriptions: downgrade-to-trial cancel path or invoice-recovery path."""
    target_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, "")
    if is_trial_plan_name(target_plan_name):
        logging.info(
            f"Tenant {tenant_id} has delinquent subscription {subscription_id} "
            f"({subscription_status}) and is downgrading to free/trial; cancelling immediately."
        )
        await _schedule_storage_target_storage_bytes_at_period_end_async(tenant_id, 0)
        await stripe.Subscription.cancel_async(subscription_id)
        SubscriptionService.update_subscription(
            tenant_id,
            {
                "subscription_id": "",
                "subscription_status": SubscriptionStatus.INACTIVE,
                "plan_name": target_plan_name,
                "price_id": subscription_price_id,
            },
        )
        return get_json_result(
            data={"cancelled": True, "plan_name": target_plan_name},
            message="Your subscription has been cancelled and your plan downgraded.",
            code=RetCode.SUCCESS,
        )

    invoice_url = (tenant_plan.get("invoice_url") or "").strip()
    logging.info(
        f"Tenant {tenant_id} has delinquent subscription {subscription_id} "
        f"({subscription_status}); redirecting to invoice {invoice_url} for payment."
    )
    return get_json_result(
        data={"payment_required": True, "invoice_url": invoice_url},
        message="Your subscription has an outstanding invoice. Please pay it before changing your plan.",
        code=RetCode.SUCCESS,
    )


async def _create_subscription_checkout_session(
    *,
    tenant_id: str,
    customer_id: str,
    subscription_status: str,
    subscription_price_id: str,
    quantity: int,
    session_success_url: str,
    session_cancel_url: str,
    extra_metadata: dict | None = None,
):
    """Create a fresh subscription checkout session for tenants without a modifiable subscription."""
    is_inactive = subscription_status == SubscriptionStatus.INACTIVE
    trail_price_id = get_trial_price_id(settings.BILLING.get("billing_plans", []))
    is_trail_plan = subscription_price_id == trail_price_id
    logging.info(
        "Create subscription checkout session: tenant_id=%s, price_id=%s, plan=%s",
        tenant_id,
        subscription_price_id,
        settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, ""),
    )

    session_params = {
        "customer": customer_id,
        "client_reference_id": f"order_{uuid.uuid4()}",
        "line_items": [{"price": subscription_price_id, "quantity": quantity}],
        "mode": PriceType.SUBSCRIPTION,
        "success_url": _build_checkout_success_url(session_success_url),
        "cancel_url": session_cancel_url,
        "metadata": {
            "price_type": PriceType.SUBSCRIPTION,
            "tenant_id": tenant_id,
            "price_id": subscription_price_id,
            "product_name": settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, ""),
            **(extra_metadata or {}),
        },
        "subscription_data": {
            "metadata": {
                "price_type": PriceType.SUBSCRIPTION,
                "tenant_id": tenant_id,
                "price_id": subscription_price_id,
                "product_name": settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, ""),
                **(extra_metadata or {}),
            },
        },
    }

    if is_inactive and is_trail_plan:
        session_params.update(
            {
                "payment_method_collection": "if_required",
                "subscription_data": session_params.get("subscription_data", {})
                | {
                    "trial_period_days": 365,
                    "trial_settings": {"end_behavior": {"missing_payment_method": "pause"}},
                },
            }
        )

    logging.debug("subscription checkout session params prepared for tenant_id=%s", tenant_id)
    session = await stripe.checkout.Session.create_async(**session_params)
    logging.info(f"created stripe session id {session.id}, url: {session.url}")
    return get_json_result(data={"redirect_to": session.url})


async def _handle_subscription_checkout(
    *,
    tenant_id: str,
    tenant_plan: dict,
    customer_id: str,
    subscription_price_id: str,
    quantity: int,
    session_success_url: str,
    session_cancel_url: str,
):
    """Dispatch subscription checkout to active/recoverable/new-subscription flows."""
    subscription_id = tenant_plan.get("subscription_id")
    subscription_status = tenant_plan.get("subscription_status")

    if subscription_status in {SubscriptionStatus.ACTIVE, "trialing"} and subscription_id:
        return await _handle_active_subscription_checkout(
            tenant_id=tenant_id,
            tenant_plan=tenant_plan,
            subscription_id=subscription_id,
            subscription_status=subscription_status,
            subscription_price_id=subscription_price_id,
            session_success_url=session_success_url,
            session_cancel_url=session_cancel_url,
        )

    if subscription_id and subscription_status in MAIN_SUBSCRIPTION_RECOVERABLE_STATUSES:
        return await _handle_recoverable_subscription_checkout(
            tenant_id=tenant_id,
            tenant_plan=tenant_plan,
            subscription_id=subscription_id,
            subscription_status=subscription_status,
            subscription_price_id=subscription_price_id,
        )

    logging.info(f"found customer {customer_id} for tenant {tenant_id}")
    return await _create_subscription_checkout_session(
        tenant_id=tenant_id,
        customer_id=customer_id,
        subscription_status=subscription_status,
        subscription_price_id=subscription_price_id,
        quantity=quantity,
        session_success_url=session_success_url,
        session_cancel_url=session_cancel_url,
    )


async def _handle_addon_checkout(
    *,
    tenant_id: str,
    customer_id: str,
    addon_price_id: str,
    quantity: int,
    expiry_time,
    session_success_url: str,
    session_cancel_url: str,
):
    """Process one-off addon checkout and attach metadata for webhook-side quota accounting."""
    logging.info("ENTERING PAYMENT SECTION")
    usage_product_name = settings.BILLING_PRICEID_TO_PRODUCT.get(addon_price_id, "")

    if is_storage_price_id(addon_price_id):
        return get_data_error_result(message="Storage add-on checkout moved to /billing/storage/set-target.")

    if quantity <= 0:
        return get_json_result(
            data=False,
            message="Quantity must be a positive integer.",
            code=RetCode.BAD_REQUEST,
        )

    usage_metadata = {
        "price_type": PriceType.ADDON,
        "tenant_id": tenant_id,
        "price_id": addon_price_id,
        "product_name": usage_product_name,
        "quantity": quantity,
    }
    if "storage" in (usage_metadata["product_name"] or "").lower():
        usage_metadata["quantity_unit"] = "GB"
    if expiry_time:
        usage_metadata["expiry_time"] = expiry_time

    session = await stripe.checkout.Session.create_async(
        customer=customer_id,
        client_reference_id=f"order_{uuid.uuid4()}",
        line_items=[{"price": addon_price_id, "quantity": quantity}],
        mode="payment",
        success_url=_build_checkout_success_url(session_success_url),
        cancel_url=session_cancel_url,
        payment_intent_data={"metadata": usage_metadata},
    )
    logging.info(f"created stripe session id {session.id}, url: {session.url}")
    return get_json_result(data={"redirect_to": session.url})


@manager.route("/checkout", methods=["POST"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_checkout():
    """
    Handles subscription purchase, upgrade, and downgrade via Stripe Checkout.
    """
    req = await get_request_json()
    params, error_response = _validate_billing_checkout_request(req)
    if error_response:
        return error_response

    tenant_id = params["tenant_id"]
    payment_type = params["payment_type"]
    subscription_price_id = params["subscription_price_id"]
    addon_price_id = params["addon_price_id"]
    quantity = params["quantity"]
    expiry_time = params["expiry_time"]
    session_success_url = params["session_success_url"]
    session_cancel_url = params["session_cancel_url"]

    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id)
    customer_id = tenant_plan.get("customer_id")
    if not customer_id:
        logging.warning("No customer_id found while checkout, it was expected create when user registion, try to create a stripe accout to proceed...")
        customer_id = await billing_set_customer_id_async(tenant_id)

    if payment_type == PriceType.SUBSCRIPTION:
        return await _handle_subscription_checkout(
            tenant_id=tenant_id,
            tenant_plan=tenant_plan,
            customer_id=customer_id,
            subscription_price_id=subscription_price_id,
            quantity=quantity,
            session_success_url=session_success_url,
            session_cancel_url=session_cancel_url,
        )

    return await _handle_addon_checkout(
        tenant_id=tenant_id,
        customer_id=customer_id,
        addon_price_id=addon_price_id,
        quantity=quantity,
        expiry_time=expiry_time,
        session_success_url=session_success_url,
        session_cancel_url=session_cancel_url,
    )


@manager.route("/create-portal-session", methods=["POST"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def customer_portal():
    req = await get_request_json()
    tenant_id = req.get("tenant_id") or current_user.id
    return_url = req.get("return_url", settings.BILLING["customer_portal_return_url"])

    if current_user.id != tenant_id:
        return get_json_result(
            data=False,
            message="No authorization.",
            code=RetCode.AUTHENTICATION_ERROR,
        )

    subscription = SubscriptionService.get_by_tenant_id(tenant_id)
    if not subscription:
        return get_data_error_result("Subscription not found.")
    current_plan_name = subscription.get("plan_name", "")
    if not current_plan_name:
        return get_data_error_result("Current plan not found.")

    customer_id = subscription.get("customer_id", "").strip()
    subscription_id = subscription.get("subscription_id", "").strip()
    # current_price_id = subscription.get("price_id", "").strip()
    if not customer_id or not subscription_id:
        return get_json_result(data={"redirect_to": return_url})

    try:
        advancer_plans = get_plans_equal_or_higher(current_plan_name)
        advancer_price_ids = list({price_id for _, price_ids in advancer_plans for price_id in price_ids})

        price_to_product = get_product_ids_for_prices(advancer_price_ids)
        product_id_to_prices: dict[str, list[str]] = {}
        for price_id, product_id in price_to_product.items():
            product_id_to_prices.setdefault(product_id, []).append(price_id)

        configuration = create_or_get_portal_configuration(product_id_to_prices)
        logging.debug("Resolved customer portal configuration id=%s", getattr(configuration, "id", ""))

        portal_session = stripe.billing_portal.Session.create(
            customer=customer_id,
            return_url=return_url,
            configuration=configuration.id,
        )
        return get_json_result(data={"redirect_to": portal_session.url})

    except stripe.StripeError as e:
        logging.error(f"Stripe API error: {e}")
        return get_data_error_result("Failed to create billing portal session.")


def _get_stripe_webhook_secret(force_refresh: bool = False) -> str | None:
    """
    Retrieve the signing secret for our webhook endpoint.
    Reads from database on first call, then caches in memory forever.
    The secret is saved at webhook creation time and retrieved from persistent storage.

    Note: Stripe's list API does NOT return the secret - it only returns
    the secret once when the webhook endpoint is created.
    """
    global _stripe_webhook_secret

    # Return cached secret (never expires - it's persistent in DB)
    if _stripe_webhook_secret and not force_refresh:
        return _stripe_webhook_secret

    # Load from persistent storage
    from api.db.services.system_settings_service import SystemSettingsService
    setting = SystemSettingsService.get_by_name("billing_webhook_secret")
    setting_list = list(setting) if setting else []
    if setting_list and hasattr(setting_list[0], 'value') and setting_list[0].value:
        _stripe_webhook_secret = setting_list[0].value
        return _stripe_webhook_secret

    logging.error("Could not retrieve webhook secret from database. Webhook verification will fail.")
    return None
    return None


@manager.route("/success", methods=["GET"])  # noqa: F821
async def billing_success():
    """
    Handle successful Stripe checkout redirect.
    Stripe redirects here with ?session_id=xxx query parameter.
    We extract it and redirect to the frontend price page with success status.
    """
    from quart import redirect

    session_id = request.args.get("session_id", "")
    if not session_id:
        logging.warning("Stripe success redirect missing session_id.")
        return redirect(f"{settings.BILLING.get('customer_portal_return_url', '')}/price?price-pay-status=error")

    # session_id present = Stripe confirmed success. Trust the redirect.
    # Actual payment state is verified asynchronously by the checkout.session.completed
    # webhook (with idempotency via payment_intent_id), so we don't query Stripe here.
    return redirect(f"{settings.BILLING.get('customer_portal_return_url', '')}/price?price-pay-status=success")


@manager.route("/cancel", methods=["GET"])  # noqa: F821
async def billing_cancel():
    """
    Handle cancelled Stripe checkout redirect.
    Redirects back to the price page with cancel status.
    """
    from quart import redirect

    return redirect(f"{settings.BILLING.get('customer_portal_return_url', '')}/price?price-pay-status=cancel")


@manager.route("/session/<session_id>", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_session_status(session_id: str):
    """
    Return the payment status of a Stripe Checkout session.
    Frontend polls this after redirect from Stripe to determine outcome.
    """
    checkout_session = await stripe.checkout.Session.retrieve_async(session_id)
    return get_json_result(data={
        "payment_status": checkout_session.payment_status,
        "mode": getattr(checkout_session, "mode", None),
        "amount_cents": checkout_session.amount_total,
        "currency": checkout_session.currency,
        "created": checkout_session.created,
        "metadata": dict(checkout_session.metadata or {}),
    })


@manager.route("/webhook", methods=["POST"])  # noqa: F821
@billing_enabled_guard(_billing_disabled_webhook_response)
async def billing_webhook():
    """
    https://docs.stripe.com/webhooks/quickstart
    """
    event = None
    payload = await request.data  # do not refactor this line

    try:
        event = json.loads(payload)
    except json.decoder.JSONDecodeError:
        logging.exception("billing_webhook error while parsing basic request.")
        # Return 400 for malformed payloads so Stripe marks the delivery as failed
        # instead of silently accepting an event we could not even parse.
        return jsonify(success=False), RetCode.BAD_REQUEST

    # Dynamically fetch the webhook secret from Stripe API to avoid config drift
    webhook_secret = _get_stripe_webhook_secret()
    if not webhook_secret:
        logging.error("Could not retrieve webhook secret from Stripe. Cannot verify webhook signature. Rejecting webhook.")
        # Signature verification cannot proceed without the endpoint secret.
        # Return non-2xx so Stripe retries after transient config/database issues.
        return jsonify(success=False), RetCode.BAD_REQUEST

    sig_header = request.headers.get("stripe-signature")
    try:
        event = stripe.Webhook.construct_event(payload, sig_header, webhook_secret)
    except stripe.SignatureVerificationError:
        # Secret may have been rotated; force refresh and retry once
        logging.warning("Signature verification failed, refreshing secret and retrying...")
        webhook_secret = _get_stripe_webhook_secret(force_refresh=True)
        if not webhook_secret:
            logging.error("Could not retrieve webhook secret after refresh. Rejecting webhook.")
            return jsonify(success=False), 400
        try:
            event = stripe.Webhook.construct_event(payload, sig_header, webhook_secret)
        except stripe.SignatureVerificationError:
            logging.exception("Signature verification failed after refresh. Rejecting webhook.")
            # Invalid signatures must never be acknowledged with 2xx, otherwise
            # we would accept an event we cannot trust or replay safely.
            return jsonify(success=False), 400

    # Handle the event
    event_type = event["type"]
    if event_type in FOCUSED_STRIPE_WEBHOOK:
        logging.info("Processing focused Stripe webhook: type=%s, id=%s", event_type, event.get("id", ""))
        try:
            _handle_event(event)
        except Exception:
            logging.exception(
                "Stripe webhook handler failed: type=%s, id=%s",
                event_type,
                event.get("id", ""),
            )
            # Business-logic failures should surface as 5xx so Stripe retries.
            # Returning 2xx here would permanently lose the event after a partial failure.
            return jsonify(success=False), 500
        # Only acknowledge with 2xx after the event has been processed or
        # deliberately skipped through idempotent duplicate handling.
        return jsonify(success=True), 200
    return jsonify(success=True), 200


@billing_enabled_guard(None)
def _handle_event(event):
    event_handlers = {
        PAYMENT_INTENT_SUCCEEDED: _handle_payment_intent_succeeded,  # one-off
        INVOICE_FAILED: _handle_invoice_payment_failed,  # subscription failed
        INVOICE_PAYMENT_ACTION_REQUIRED: _handle_invoice_payment_action_required,
        CHECKOUT_SESSION_COMPLETED: _handle_checkout_session_completed,  # subscription part
        INVOICE_PAID: _handle_invoice_paid,  # subscription succeeded
        SUBSCRIPTION_UPDATED: _handle_customer_subscription_updated,
        SUBSCRIPTION_DELETED: _handle_customer_subscription_deleted,
    }

    event_type = event["type"]
    event_data = event["data"]
    event_data_object = event_data["object"]
    event_payment_type = event_data_object["object"]
    event_id = event.get("id", "")

    if event_id:
        payload_created = event.get("created")
        payload_created_at = to_utc_datetime(payload_created) if payload_created else None
        object_id = event_data_object.get("id", "")
        payload = event
        if not isinstance(event, dict):
            if hasattr(event, "to_dict"):
                payload = event.to_dict()
            else:
                payload = json.loads(json.dumps(event))
        try:
            with DB.atomic():
                BillingWebhookEventService.save(
                    event_id=event_id,
                    event_type=event_type,
                    object_id=object_id,
                    payload=payload,
                    created_at=payload_created_at,
                    received_at=to_utc_datetime(datetime.now(timezone.utc)),
                )
        except IntegrityError:
            logging.info(f"Skip duplicated webhook event: {event_id} ({event_type})")
            return

    handler = event_handlers.get(event_type)
    if handler:
        handler(event)
    else:
        logging.info("Unhandled Stripe event: type=%s, object=%s", event_type, event_payment_type)


def _handle_payment_intent_succeeded(event: dict):
    event_data = event["data"]["object"]

    try:
        intent = IntentSucceed(**event_data)
    except ValidationError as e:
        logging.warning("IntentSucceed data validation failed: %s", e)
        return

    tenant_id = ""
    product_name = ""
    price_type = "NOT DETERMINED"
    price_id = ""
    quantity = 0
    expiry_time = None
    intent_metadata = intent.metadata or {}

    if intent_metadata:
        tenant_id = intent_metadata.get("tenant_id", "")
        product_name = intent_metadata.get("product_name", "")
        price_id = intent_metadata.get("price_id", "")

        price_type = intent_metadata.get("price_type", "")
        quantity = int(intent_metadata.get("quantity", "0"))
        quantity_unit = (intent_metadata.get("quantity_unit") or "").strip().upper()
        expiry_time = intent_metadata.get("expiry_time")
    else:
        logging.warning("Expected metadata in _handle_payment_intent_succeeded, but get empty.")

    if not intent_metadata or price_type != PriceType.ADDON:
        logging.info(f"{tenant_id} triggered {price_type} product {product_name} in intent succeeded, skipped. May handle in subscription.paid.")
        return

    valid_price_ids = []
    from api.db.services.billing_service import ProductService
    from api.db.db_models import ProductType
    latest_addon_products = ProductService.get_latest_by_type(ProductType.ADDON)
    for product in latest_addon_products:
        if product.price_ids:
            valid_price_ids.extend(product.price_ids.split())

    if price_id not in valid_price_ids:
        logging.info(f"{tenant_id} triggered price_type {price_type} product {product_name} with unhandled price_id {price_id}, skipped.")
        return

    amount_cents = intent.amount
    amount_received = intent.amount_received
    currency = intent.currency
    payment_method = PaymentMethod.CARD
    order_id = intent.id
    payment_intent_id = intent.id
    stripe_status = intent.status
    payment_status = normalize_stripe_payment_intent_status(stripe_status)
    paid = bool(amount_received)
    captured = bool(amount_received)
    order_created_at = intent.created

    latest_charge_id = intent.latest_charge_id or ""
    receipt_url = get_receipt_url_from_intent_latest_charge(latest_charge_id) if latest_charge_id else ""

    customer_id = intent.customer_id or ""
    product_id = get_product_id_by_name(product_name)

    quota_quantity = quantity
    quota_unit = ""

    payment_order = {
        "id": get_uuid(),
        "tenant_id": tenant_id,
        "customer_id": customer_id,
        "payment_type": PriceType.ADDON,
        "product_ids": [product_id] if product_id else [],
        "product_names": [product_name] if product_name else [],
        "product_quantities": [quota_quantity],
        "product_amount_cents": [amount_cents],
        "price_ids": [price_id] if price_id else [],
        "is_prorated": False,
        "amount_cents": amount_cents,
        "currency": currency,
        "payment_method": payment_method,
        "order_id": order_id,
        "payment_intent_id": payment_intent_id,
        "receipt_url": receipt_url,
        "payment_channel": PaymentChannel.STRIPE,
        "payment_status": payment_status,
        "stripe_status": stripe_status,
        "paid": paid,
        "captured": captured,
        "description": "",
        "order_created_at": order_created_at,
        "payment_detail": {"quantity": quantity, "quantity_unit": quantity_unit, "quota_quantity": quota_quantity, "quota_unit": quota_unit},
    }
    # NOTE: We intentionally do NOT persist to the legacy `billing_addon` table.
    # The current system uses:
    # - `billing_payment_order` as the per-purchase ledger/history (needed for spend analytics), and
    # - `billing_purchased_product_overview` as the current remaining quota snapshot.

    purchased_overview = PurchasedProductOverviewService.get_by_product_name_and_tenant_id(product_name, tenant_id)
    if PaymentOrderService.get_by_payment_intent_id(payment_intent_id):
        logging.info(f"Skip duplicated payment_intent for tenant {tenant_id}: {payment_intent_id}")
        return

    expiry_dt = to_utc_datetime(expiry_time) if expiry_time else None
    with DB.atomic():
        PaymentOrderService.save(**payment_order)
        if not purchased_overview:
            purchased_overview_dict = {
                "id": get_uuid(),
                "tenant_id": tenant_id,
                "product_id": product_id,
                "product_name": product_name,
                "quantity": quota_quantity,
                "effective_time": to_utc_datetime(datetime.now(timezone.utc)),
                "expiry_time": expiry_dt,
            }
            PurchasedProductOverviewService.save(**purchased_overview_dict)
        else:
            ok = PurchasedProductOverviewService.update_quantity(product_name, tenant_id, quota_quantity)
            if not ok:
                logging.warning(f"Customer {customer_id} with tenant_id {tenant_id}, purchased {quantity} {product_name}, but update to purchase overview failed.")
            if expiry_dt:
                prev_expiry = to_utc_datetime(purchased_overview.get("expiry_time"))
                if not prev_expiry or expiry_dt > prev_expiry:
                    PurchasedProductOverviewService.model.update(expiry_time=expiry_dt).where(
                        (PurchasedProductOverviewService.model.product_name == product_name) & (PurchasedProductOverviewService.model.tenant_id == tenant_id)
                    ).execute()


def _upsert_main_subscription_payment_order(
    *,
    tenant_id: str,
    customer_id: str,
    subscription_id: str,
    invoice_id: str,
    price_id: str,
    product_id: str,
    product_name: str,
    amount_cents: int,
    currency: str,
    invoice_url: str,
    invoice_pdf_url: str,
    payment_status: str,
    stripe_status: str,
    paid: bool,
    payment_intent_id: str = "",
    description: str = "",
    order_created_at=None,
    payment_detail: dict | None = None,
) -> None:
    if not invoice_id:
        return

    created_at = _safe_payment_order_created_at(order_created_at, invoice_id) or to_utc_datetime(datetime.now(timezone.utc))
    payload = {
        "tenant_id": tenant_id,
        "customer_id": customer_id,
        "payment_type": PriceType.SUBSCRIPTION,
        "product_ids": [product_id] if product_id else [],
        "product_names": [product_name] if product_name else [],
        "product_quantities": [1],
        "product_amount_cents": [amount_cents or 0],
        "price_ids": [price_id] if price_id else [],
        "is_prorated": True,
        "amount_cents": amount_cents or 0,
        "currency": currency or "usd",
        "payment_method": PaymentMethod.CARD,
        "order_id": invoice_id,
        "payment_intent_id": payment_intent_id or "",
        "payment_subscription_id": subscription_id,
        "receipt_url": invoice_url or "",
        "receipt_pdf_url": invoice_pdf_url or "",
        "payment_channel": PaymentChannel.STRIPE,
        "payment_status": payment_status,
        "stripe_status": stripe_status or "",
        "paid": paid,
        "captured": paid,
        "description": description,
        "order_created_at": created_at,
        "payment_detail": payment_detail or {},
    }

    existing = PaymentOrderService.get_by_order_id(invoice_id)
    if existing:
        PaymentOrderService.update_by_order_id(invoice_id, payload)
        return

    PaymentOrderService.save(id=get_uuid(), **payload)


def _handle_main_subscription_invoice_not_paid(event: dict, description: str) -> None:
    event_data = event["data"]["object"]
    if not isinstance(event_data, dict):
        logging.warning("Main subscription invoice failure skipped because event data object is not a dict.")
        return

    context = extract_invoice_failure_context(event_data)
    subscription_id = context["subscription_id"]
    customer_id = context["customer_id"]
    tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id) if customer_id else ""

    if not tenant_id:
        logging.warning(f"Main subscription invoice failure missing tenant context: {subscription_id=}, {customer_id=}")
        return

    stripe_subscription = stripe.Subscription.retrieve(subscription_id)
    stripe_status = _normalize_subscription_status(get_attr_or_item(stripe_subscription, "status", ""))
    local_status = stripe_status
    if local_status in {"", "active", "trialing"}:
        local_status = "past_due"

    _sync_main_subscription_from_stripe(
        tenant_id=tenant_id,
        stripe_subscription=stripe_subscription,
        subscription_status=local_status,
        invoice_id=context["invoice_id"],
        invoice_url=context["invoice_url"],
        invoice_pdf_url=context["invoice_pdf_url"],
    )

    existing = SubscriptionService.get_by_tenant_id(tenant_id) or {}
    _upsert_main_subscription_payment_order(
        tenant_id=tenant_id,
        customer_id=customer_id or existing.get("customer_id", ""),
        subscription_id=subscription_id or existing.get("subscription_id", ""),
        invoice_id=context["invoice_id"],
        price_id=existing.get("price_id", ""),
        product_id=existing.get("product_id", ""),
        product_name=existing.get("plan_name", ""),
        amount_cents=context["amount_cents"],
        currency=context["currency"],
        invoice_url=context["invoice_url"],
        invoice_pdf_url=context["invoice_pdf_url"],
        payment_status=PaymentStatus.FAILED.value,
        stripe_status=context["invoice_status"],
        paid=False,
        payment_intent_id=context["payment_intent_id"],
        description=description,
        order_created_at=context["created"] or event.get("created"),
        payment_detail={
            "attempt_count": context["attempt_count"],
            "next_payment_attempt": context["next_payment_attempt"],
            "billing_reason": context["billing_reason"],
        },
    )


def _handle_invoice_payment_failed(event: dict):
    # The payment failed or the customer does not have a valid payment method.
    # The subscription becomes past_due. Notify your customer and send them to the
    # customer portal to update their payment information.
    _handle_main_subscription_invoice_not_paid(event, "Main subscription invoice payment failed")


def _handle_invoice_payment_action_required(event: dict):
    _handle_main_subscription_invoice_not_paid(event, "Main subscription invoice payment action required")


def _handle_checkout_session_completed(event: dict):
    # NOTE: save customer_id for portal session (front end)
    # Payment is successful and the subscription is created.
    # You should provision the subscription and save the customer ID to your database.

    event_data = event["data"]["object"]

    try:
        checkout_session_completed = CheckoutSessionCompleted(**event_data)
    except ValidationError as e:
        logging.warning("CheckoutSessionCompleted data validation failed: %s", e)
        return

    if checkout_session_completed.mode == "payment":
        metadata = checkout_session_completed.metadata or {}
        if metadata.get("payment_type") == "points_recharge":
            tenant_id = (metadata.get("tenant_id") or "").strip()
            if not tenant_id:
                logging.warning("checkout.session.completed(points_recharge) missing tenant_id.")
                return
            points = int(metadata.get("points_amount") or 0)
            if points <= 0:
                logging.warning(f"checkout.session.completed(points_recharge) invalid points_amount for tenant {tenant_id}.")
                return
            idempotency_key = f"checkout:{checkout_session_completed.id}"
            PointAccountService.recharge(
                tenant_id=tenant_id,
                points=points,
                idempotency_key=idempotency_key,
                description="Stripe checkout recharge",
                metadata={"session_id": checkout_session_completed.id},
            )
            # Record PaymentOrder for spend history
            customer_id = (checkout_session_completed.customer_id or "").strip()
            amount_cents = checkout_session_completed.amount_total or 0
            currency = checkout_session_completed.currency or "usd"
            receipt_url = ""
            payment_intent_id = checkout_session_completed.payment_intent_id or ""
            if payment_intent_id:
                payment_intent = stripe.PaymentIntent.retrieve(payment_intent_id)
                charges = getattr(payment_intent, "charges", None)
                if charges and hasattr(charges, "data") and charges.data:
                    receipt_url = (getattr(charges.data[0], "receipt_url", None) or "").strip()
            if not PaymentOrderService.get_by_order_id(checkout_session_completed.id):
                PaymentOrderService.save(
                    id=get_uuid(),
                    tenant_id=tenant_id,
                    customer_id=customer_id,
                    payment_type=PriceType.ADDON,
                    product_ids=[],
                    product_names=["points_recharge"],
                    product_quantities=[points],
                    product_amount_cents=[amount_cents],
                    price_ids=[],
                    is_prorated=False,
                    amount_cents=amount_cents,
                    currency=currency,
                    payment_method=PaymentMethod.CARD,
                    order_id=checkout_session_completed.id,
                    payment_intent_id=payment_intent_id,
                    receipt_url=receipt_url,
                    payment_channel=PaymentChannel.STRIPE,
                    payment_status=PaymentStatus.SUCCESS.value,
                    stripe_status=checkout_session_completed.payment_status or "",
                    paid=True,
                    captured=True,
                    description=f"Points recharge: {points} points",
                    order_created_at=checkout_session_completed.created,
                    payment_detail={"points_amount": points},
                )
            return
    elif checkout_session_completed.mode == "subscription":
        metadata = checkout_session_completed.metadata or {}
        tenant_id = metadata.get("tenant_id")
        price_id = metadata.get("price_id", "")
        if not price_id or not tenant_id:
            logging.warning("checkout.session.completed missing required metadata.")
            return
        # For subscription mode, subscription state is handled by:
        # - customer.subscription.updated
        # invoice.paid handles PaymentOrder creation.
        logging.info(f"checkout.session.completed subscription mode for tenant {tenant_id}: handled by customer.subscription.updated")

    elif checkout_session_completed.mode == "setup":
        metadata = checkout_session_completed.metadata or {}
        if (metadata.get("setup_for_billing_change") or "").strip() != "1":
            logging.info("checkout.session.completed(setup) ignored without billing-change marker")
            return
        customer_id = (checkout_session_completed.customer_id or "").strip()
        setup_intent_id = (checkout_session_completed.setup_intent_id or "").strip()
        if not customer_id or not setup_intent_id:
            logging.warning("checkout.session.completed(setup) missing customer_id/setup_intent_id.")
            return

        setup_intent = stripe.SetupIntent.retrieve(setup_intent_id)
        payment_method_id = (getattr(setup_intent, "payment_method", None) or "").strip()

        if not payment_method_id:
            logging.warning("checkout.session.completed(setup) missing payment_method for setup_intent_id=%s", setup_intent_id)
            return

        stripe.Customer.modify(
            customer_id,
            invoice_settings={"default_payment_method": payment_method_id},
        )
        logging.info(
            "checkout.session.completed(setup) saved default payment method for customer %s (tenant_id=%s)",
            customer_id,
            (metadata.get("tenant_id") or "").strip(),
        )


def _resolve_tenant_context_for_invoice_line(
    *,
    invoice_paid: InvoicePaid,
    item,
    tenant_id: str,
    customer_id: str,
    subscription_id: str,
) -> tuple[str, str]:
    resolved_tenant_id = (tenant_id or "").strip()
    resolved_customer_id = (customer_id or "").strip()
    resolved_subscription_id = (subscription_id or "").strip()

    if not resolved_tenant_id and resolved_customer_id:
        resolved_tenant_id = SubscriptionService.get_tenant_id_by_customer_id(resolved_customer_id) or ""

    stripe_subscription = None
    if (not resolved_tenant_id or not resolved_customer_id) and resolved_subscription_id:
        plan_sub = SubscriptionService.get_by_subscription_id(resolved_subscription_id) or {}
        resolved_tenant_id = resolved_tenant_id or (plan_sub.get("tenant_id", "") or "").strip()
        resolved_customer_id = resolved_customer_id or (plan_sub.get("customer_id", "") or "").strip()

        if not resolved_tenant_id or not resolved_customer_id:
            stripe_subscription = stripe.Subscription.retrieve(resolved_subscription_id)

    if stripe_subscription:
        resolved_customer_id = resolved_customer_id or (get_attr_or_item(stripe_subscription, "customer", "") or "").strip()
        subscription_metadata = get_attr_or_item(stripe_subscription, "metadata", {}) or {}
        if not resolved_tenant_id:
            resolved_tenant_id = (subscription_metadata.get("tenant_id", "") or "").strip()

    if not resolved_tenant_id:
        line_metadata = getattr(item, "metadata", None) or {}
        resolved_tenant_id = (line_metadata.get("tenant_id", "") or "").strip()

    if not resolved_tenant_id:
        invoice_metadata = invoice_paid.metadata or {}
        resolved_tenant_id = (invoice_metadata.get("tenant_id", "") or "").strip()

    if not resolved_tenant_id and resolved_customer_id:
        resolved_tenant_id = SubscriptionService.get_tenant_id_by_customer_id(resolved_customer_id) or ""

    return resolved_tenant_id, resolved_customer_id


def _handle_invoice_paid(event: dict):
    # Continue to provision the subscription as payments continue to be made.
    # Store the status in your database and check when a user accesses your service.
    # This approach helps you avoid hitting rate limits.

    event_data = event["data"]["object"]

    try:
        invoice_paid = InvoicePaid(**event_data)
    except ValidationError as e:
        logging.warning("InvoicePaid data validation failed: %s", e)
        return

    line_items = invoice_paid.lines.data
    customer_id = invoice_paid.customer_id
    tenant_id = ""
    metadata = invoice_paid.metadata or {}
    if metadata:
        tenant_id = metadata.get("tenant_id", "")
    if not tenant_id:
        tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id)

    order_id = invoice_paid.id
    stripe_status = invoice_paid.status or ""
    status = normalize_stripe_invoice_status(stripe_status)
    order_created_at = invoice_paid.created
    invoice_url = invoice_paid.hosted_invoice_url or ""
    invoice_pdf_url = invoice_paid.invoice_pdf or ""

    # Collect all line items into a single aggregated PaymentOrder record.
    # One row per invoice, not one per line item.
    aggregated_product_ids: list[str] = []
    aggregated_product_names: list[str] = []
    aggregated_product_quantities: list[int] = []
    aggregated_product_amount_cents: list[int] = []
    aggregated_price_ids: list[str] = []
    aggregated_descriptions: list[str] = []
    aggregated_payment_details: list[dict] = []
    storage_subscription_id = ""
    resolved_tenant_id = tenant_id
    resolved_customer_id = customer_id

    for idx, item in enumerate(line_items):
        item_description = item.description or ""
        item_subscription_detail = item.parent.subscription_item_details
        item_subscription_id = item_subscription_detail.subscription if item_subscription_detail else ""
        item_price_id = (
            getattr(getattr(getattr(item, "pricing", None), "price_details", None), "price", "")
            or (metadata.get("price_id", "") if metadata else "")
        )

        item_tenant_id, item_customer_id = _resolve_tenant_context_for_invoice_line(
            invoice_paid=invoice_paid,
            item=item,
            tenant_id=tenant_id,
            customer_id=customer_id,
            subscription_id=item_subscription_id,
        )
        resolved_tenant_id = item_tenant_id or resolved_tenant_id
        resolved_customer_id = item_customer_id or resolved_customer_id

        item_quantity = getattr(item, "quantity", 0) or 0
        item_amount_cents = getattr(item, "amount", 0) or 0

        item_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(item_price_id, "")
        item_is_storage = is_storage_price_id(item_price_id) or is_storage_plan_name(item_plan_name)

        item_product_id = get_product_id_by_name(item_plan_name) if item_plan_name else ""
        aggregated_product_ids.append(item_product_id)
        aggregated_product_names.append(item_plan_name or "UNKNOWN")
        aggregated_product_quantities.append(item_quantity)
        aggregated_product_amount_cents.append(item_amount_cents)
        aggregated_price_ids.append(item_price_id)
        desc = (invoice_paid.description or invoice_paid.billing_reason or "") + f" {item_description}".strip()
        aggregated_descriptions.append(desc)
        if item_is_storage:
            aggregated_payment_details.append({
                "type": "storage",
                "quantity": item_quantity,
            })
            if item_subscription_id:
                storage_subscription_id = item_subscription_id
        else:
            aggregated_payment_details.append({
                "type": "plan",
                "quantity": item_quantity,
            })

    # Sync storage subscription if any storage item was present
    if storage_subscription_id:
        stripe_subscription = stripe.Subscription.retrieve(storage_subscription_id)
        _sync_storage_subscription_record(
            resolved_tenant_id,
            stripe_subscription,
            customer_id=resolved_customer_id,
        )

    # Build the aggregated PaymentOrder — one row per invoice
    existing_order = PaymentOrderService.get_by_order_id(order_id)
    payment_order = {
        "id": get_uuid() if not existing_order else existing_order.get("id", get_uuid()),
        "tenant_id": resolved_tenant_id,
        "customer_id": resolved_customer_id,
        "payment_type": PriceType.SUBSCRIPTION,
        "product_ids": aggregated_product_ids,
        "product_names": aggregated_product_names,
        "product_quantities": aggregated_product_quantities,
        "product_amount_cents": aggregated_product_amount_cents,
        "price_ids": aggregated_price_ids,
        "is_prorated": True,
        "amount_cents": invoice_paid.amount_paid,
        "currency": invoice_paid.currency,
        "payment_method": PaymentMethod.CARD,
        "order_id": order_id,
        "payment_intent_id": "",
        "payment_subscription_id": storage_subscription_id,
        "receipt_url": invoice_url,
        "receipt_pdf_url": invoice_pdf_url,
        "payment_channel": PaymentChannel.STRIPE,
        "payment_status": status,
        "stripe_status": stripe_status,
        "paid": status == PaymentStatus.SUCCESS.value,
        "captured": status == PaymentStatus.SUCCESS.value,
        "description": "; ".join(aggregated_descriptions),
        "order_created_at": order_created_at,
        "payment_detail": {"line_items": aggregated_payment_details},
    }

    if existing_order and existing_order.get("id"):
        if existing_order.get("payment_status") != PaymentStatus.SUCCESS.value:
            payment_order.pop("id", None)
            PaymentOrderService.update_by_order_id(order_id, payment_order)
        else:
            logging.info(f"invoice.paid payment_order already successful for tenant {resolved_tenant_id}: {order_id}")
    else:
        try:
            PaymentOrderService.save(**payment_order)
        except IntegrityError:
            logging.info(f"Skip duplicated invoice.paid payment_order for tenant {resolved_tenant_id}: {order_id}")

    # Note: Subscription state is handled by customer.subscription.updated (sole handler).
    # invoice.paid only handles PaymentOrder creation/update.
    # hold/commit/recharge ops. Both are reset at billing cycle start via
    # reset_plan_consumed_points_at_cycle_start (called from subscription.updated),
    # not here — invoice.paid also fires for mid-cycle upgrade proration.


def _period_changed(previous_start, previous_end, current_start, current_end) -> bool:
    if not previous_start or not previous_end or not current_start or not current_end:
        return False
    return int(previous_start.timestamp()) != int(current_start.timestamp()) or int(previous_end.timestamp()) != int(current_end.timestamp())


def _handle_storage_subscription_updated(subscription_updated: SubscriptionUpdated):
    subscription = subscription_updated.data.object
    subscription_id = subscription.id
    customer_id = subscription.customer_id
    tenant_id = subscription.metadata.get("tenant_id", "")
    if not tenant_id:
        # Storage is now on the plan subscription; look up by subscription_id.
        plan_sub = SubscriptionService.get_by_subscription_id(subscription_id) or {}
        tenant_id = plan_sub.get("tenant_id", "")
    if not tenant_id:
        tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id)
    if not tenant_id:
        logging.warning(f"Skip storage subscription.updated without tenant context: {subscription_id}")
        return

    item_id, price_id, quantity_gb = extract_storage_subscription_item(subscription)
    logging.info(
        "Handling storage subscription.updated: tenant_id=%s subscription_id=%s customer_id=%s item_id=%s price_id=%s quantity_gb=%s",
        tenant_id,
        subscription_id,
        customer_id,
        item_id,
        price_id,
        quantity_gb,
    )

    _sync_storage_subscription_record(
        tenant_id,
        subscription,
        customer_id=customer_id,
    )


async def _release_schedule_async(schedule_id: str, delay: int = 0) -> None:
    """Release a SubscriptionSchedule after an optional delay. Fire-and-forget; never raises."""
    try:
        if delay:
            await asyncio.sleep(delay)
        await stripe.SubscriptionSchedule.release_async(schedule_id)
        logging.info("Released subscription schedule %s (delay=%ds)", schedule_id, delay)
    except Exception:
        logging.exception("Failed to release subscription schedule %s", schedule_id)


def _handle_customer_subscription_updated(event: dict):
    logging.info("Handling customer.subscription.updated")

    try:
        subscription_updated = SubscriptionUpdated(**event)
    except ValidationError as e:
        logging.warning("Subscription Updated data validation failed: %s", e)
        return

    subscription = subscription_updated.data.object
    previous = subscription_updated.data.previous_attributes
    subscription_id = subscription.id
    customer_id = subscription.customer_id
    tenant_id = subscription.metadata.get("tenant_id", "")
    if not tenant_id:
        tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id)
    if not tenant_id:
        logging.warning(f"Skip subscription.updated without tenant context: {subscription_id}")
        return

    existing_main_subscription = SubscriptionService.get_by_tenant_id(tenant_id) if tenant_id else {}
    previous_main_start = to_utc_datetime(existing_main_subscription.get("start_time")) if existing_main_subscription else None
    previous_main_end = to_utc_datetime(existing_main_subscription.get("end_time")) if existing_main_subscription else None

    # Phase 1a: detect whether this event carries a storage line item (storage is a second
    # line item on the plan subscription after unification). Must be assigned before the
    # items-check below.
    has_storage_item_in_event = False
    if existing_main_subscription and subscription_id == existing_main_subscription.get("subscription_id"):
        _si_item_id, _si_price_id, _si_qty = extract_storage_subscription_item(subscription)
        has_storage_item_in_event = bool(_si_item_id)

    logging.info("Handling update for subscription: %s (tenant_id=%s)", subscription_id, tenant_id)

    if not subscription.items or not subscription.items.data:
        logging.warning("subscription.updated missing subscription items; running fallback sync.")
        _sync_main_subscription_from_stripe(
            tenant_id=tenant_id,
            stripe_subscription=subscription,
            subscription_status=_normalize_subscription_status(subscription.status),
            invoice_id=subscription.latest_invoice_id or "",
        )
        return

    plan_item, plan_price, plan_price_id = extract_plan_item_and_price(subscription)
    first_price_id = plan_price_id

    if has_storage_item_in_event:
        _handle_storage_subscription_updated(subscription_updated)
    elif (
        existing_main_subscription
        and subscription_id == existing_main_subscription.get("subscription_id")
        and (
            existing_main_subscription.get("addon_subscription_item_id")
            or safe_int(existing_main_subscription.get("addon_storage_bytes", 0), 0) > 0
        )
    ):
        # Storage item is absent from this event but DB still records one.
        # This happens when storage is downgraded to 0: Stripe removes the item
        # entirely from the subscription rather than setting quantity to zero.
        # Clear all storage fields so the user loses the addon storage quota.
        logging.info(
            "Storage item absent from subscription.updated for tenant %s; clearing storage fields in DB.",
            tenant_id,
        )
        from api.db.db_models import Subscription as _Subscription
        with DB.atomic():
            _Subscription.update(
                addon_subscription_item_id=None,
                addon_storage_bytes=0,
                target_storage_bytes=0,
            ).where(_Subscription.tenant_id == tenant_id).execute()

    if previous and (previous.plan or previous.items):
        old_price = None
        new_price = plan_price
        new_price_id = plan_price_id
        if not new_price or not new_price_id:
            logging.warning(f"subscription.updated missing non-storage plan item; running fallback sync: {subscription_id=}")
            _sync_main_subscription_from_stripe(
                tenant_id=tenant_id,
                stripe_subscription=subscription,
                subscription_status=_normalize_subscription_status(subscription.status),
                invoice_id=subscription.latest_invoice_id or "",
            )
            return
        product_name = settings.BILLING_PRICEID_TO_PRODUCT.get(new_price_id, "")
        product_id = get_product_id_by_name(product_name)

        old_price = extract_previous_plan_price(previous)
        old_price_id = getattr(old_price, "id", "") if old_price else ""

        pending_update = getattr(subscription, "pending_update", None)
        if pending_update:
            with DB.atomic():
                Subscription.update(
                    subscription_status="past_due",
                    status="past_due",
                    invoice_id=subscription.latest_invoice_id or "",
                    update_time=current_timestamp(),
                ).where(Subscription.tenant_id == tenant_id).execute()
            logging.info(f"Skip main subscription entitlement update because pending_update exists: {subscription_id=}")
            return

        old_priority = get_plan_priority_by_price_id(old_price_id)
        new_priority = get_plan_priority_by_price_id(new_price_id)
        is_upgrade = old_priority is not None and new_priority is not None and new_priority > old_priority

        if is_upgrade and not is_subscription_latest_invoice_paid_sync(subscription):
            with DB.atomic():
                Subscription.update(
                    subscription_status="past_due",
                    status="past_due",
                    invoice_id=subscription.latest_invoice_id or "",
                    update_time=current_timestamp(),
                ).where(Subscription.tenant_id == tenant_id).execute()
            logging.info(f"Skip main subscription upgrade entitlement until latest invoice is paid: {subscription_id=}")
            return

        def _plan_label(price_id: str, nickname: str | None) -> str:
            return settings.BILLING_PRICEID_TO_PRODUCT.get(price_id, "") or (nickname or "") or "unknown"

        latest_invoice_id = subscription.latest_invoice_id or ""
        invoice_url = ""
        invoice_pdf_url = ""
        invoice_created = subscription_updated.created
        if latest_invoice_id:
            existing_payment_order = PaymentOrderService.get_by_order_id(latest_invoice_id)
            if existing_payment_order:
                invoice_url = existing_payment_order.get("receipt_url", "") or ""
                invoice_pdf_url = existing_payment_order.get("receipt_pdf_url", "") or ""
                invoice_created = existing_payment_order.get("order_created_at") or invoice_created

        period_start_value = subscription.current_period_start or (getattr(plan_item, "current_period_start", None) if plan_item else None)
        period_end_value = subscription.current_period_end or (getattr(plan_item, "current_period_end", None) if plan_item else None)
        current_period_start = to_utc_datetime(period_start_value)
        current_period_end = to_utc_datetime(period_end_value)
        if not current_period_start or not current_period_end:
            logging.warning(f"subscription.updated missing current period boundaries: {subscription_id=}, {period_start_value=}, {period_end_value=}")
            return

        if old_price:
            existing_payment_order_id = existing_payment_order.get("id") if latest_invoice_id and existing_payment_order else ""
            subscription_order_id = existing_payment_order_id or existing_main_subscription.get("order_id", "")

            previous_subscription_items = previous.items
            previous_subscription_id = ""
            if previous_subscription_items and previous_subscription_items.data:
                previous_subscription_id = previous_subscription_items.data[0].subscription_id or ""
            if not previous_subscription_id:
                previous_subscription_id = SubscriptionService.get_by_tenant_id(tenant_id).get("original_subscription_id") or SubscriptionService.get_by_tenant_id(tenant_id).get("subscription_id", "")

            subscription_dict = {
                "tenant_id": tenant_id,
                "product_id": product_id,
                "plan_name": product_name,
                "order_id": subscription_order_id,
                "status": _normalize_subscription_status(subscription.status) or SubscriptionStatus.ACTIVE,
                "customer_id": customer_id,
                "price_id": new_price_id,
                "subscription_id": subscription_id,
                "subscription_status": _normalize_subscription_status(subscription.status) or SubscriptionStatus.ACTIVE,
                "invoice_id": latest_invoice_id,
                "invoice_url": invoice_url,
                "invoice_pdf_url": invoice_pdf_url,
                "start_time": current_period_start,
                "end_time": current_period_end,
                "renew_time": None,
                "original_subscription_id": previous_subscription_id if previous_subscription_id else subscription_id,
            }

            old_amount = None
            if old_price:
                old_amount = getattr(old_price, "unit_amount", None)
                if old_amount is None:
                    old_amount = getattr(old_price, "amount", None)
            if new_price.unit_amount > (old_amount or 0):
                old_label = _plan_label(old_price_id, getattr(old_price, "nickname", None) if old_price else None)
                new_label = _plan_label(new_price_id, new_price.nickname)
                logging.info(f"UPGRADE from {old_label} to {new_label}")
                # consumed_plan_points carries forward across upgrades — it's the
                # authoritative record of points used this cycle. available is derived
                # as new_plan_quota - consumed, so no sync or compensation needed.
            else:
                old_label = _plan_label(old_price_id, getattr(old_price, "nickname", None) if old_price else None)
                new_label = _plan_label(new_price_id, new_price.nickname)
                logging.info(f"DOWNGRADE from {old_label} to {new_label}")
                # Additional downgrade-specific logic if needed

            SubscriptionService.upsert_subscription(tenant_id, subscription_dict)

            if tenant_id and _period_changed(previous_main_start, previous_main_end, current_period_start, current_period_end):
                PointAccountService.reset_plan_consumed_points_at_cycle_start(tenant_id)

            schedule_id = (event.get("data", {}).get("object", {}) or {}).get("schedule") if isinstance(event, dict) else ""
            if schedule_id:
                test_clock_id = (event.get("data", {}).get("object", {}) or {}).get("test_clock") if isinstance(event, dict) else ""
                delay = 30 if test_clock_id else 0
                asyncio.ensure_future(_release_schedule_async(schedule_id, delay))

    elif previous and previous.status:
        new_status = _normalize_subscription_status(subscription.status)
        db_price_id = (existing_main_subscription.get("price_id") or "").strip() if existing_main_subscription else ""
        latest_invoice_id = subscription.latest_invoice_id or ""

        # Stripe can omit previous.items on some updates (for example trial-end/status-only updates).
        # Fallback to comparing current event price_id with DB state and sync if drift is detected.
        if first_price_id and first_price_id != db_price_id:
            _sync_main_subscription_from_stripe(
                tenant_id=tenant_id,
                stripe_subscription=subscription,
                subscription_status=new_status or _normalize_subscription_status(subscription.status),
                invoice_id=latest_invoice_id,
            )
            logging.info(
                "subscription.updated fallback sync by item diff: "
                f"tenant_id={tenant_id}, subscription_id={subscription_id}, db_price_id={db_price_id}, event_price_id={first_price_id}"
            )

        if new_status:
            _sync_main_subscription_from_stripe(
                tenant_id=tenant_id,
                stripe_subscription=subscription,
                subscription_status=new_status,
                invoice_id=latest_invoice_id,
            )
        logging.info("Subscription status changed: %s -> %s", previous.status, subscription.status)
        # TODO: handle cancellation, reactivation, etc.

    elif previous and previous.trial_end:
        logging.info("Subscription trial_end changed: %s -> %s", previous.trial_end, subscription.trial_end)

    else:
        current_plan_name = (existing_main_subscription.get("plan_name") or "") if existing_main_subscription else ""
        event_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(first_price_id, "") if first_price_id else ""

        if existing_main_subscription and _should_preview_as_new_subscription(current_plan_name, event_plan_name):
            _sync_main_subscription_from_stripe(
                tenant_id=tenant_id,
                stripe_subscription=subscription,
                subscription_status=_normalize_subscription_status(subscription.status),
                invoice_id=subscription.latest_invoice_id or "",
            )
            logging.info(
                "subscription.updated fallback sync from trial placeholder: "
                "tenant_id=%s, subscription_id=%s, db_plan=%s, event_plan=%s",
                tenant_id,
                subscription_id,
                current_plan_name,
                event_plan_name,
            )
        else:
            logging.debug("Subscription updated with no actionable fields.")


def _handle_customer_subscription_deleted(event: dict):
    event_data = event["data"]["object"]
    subscription_id = (event_data.get("id") or "").strip() if isinstance(event_data, dict) else ""
    customer_id = (event_data.get("customer") or "").strip() if isinstance(event_data, dict) else ""
    if subscription_id:
        # Check if this subscription is the plan subscription carrying a storage item.
        # If the plan subscription had a storage item (extract returns non-empty), clear it.
        _candidate_tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id) if customer_id else ""
        if _candidate_tenant_id:
            plan_sub = SubscriptionService.get_by_tenant_id(_candidate_tenant_id)
            if plan_sub and subscription_id == plan_sub.get("subscription_id"):
                _si_item_id, _si_price_id, _si_qty = extract_storage_subscription_item(event_data)
                if _si_item_id:
                    # Plan subscription had a storage item.
                    # Do NOT call _sync_storage_subscription_record here: the deleted event
                    # still carries non-zero quantities, so syncing would overwrite storage
                    # with stale non-zero bytes instead of clearing them.  Fall through to
                    # the main cancellation handler below which zeroes all storage fields.
                    logging.info(
                        "Plan subscription with storage item deleted for tenant %s; falling through to main cancel handler.",
                        _candidate_tenant_id,
                    )
    tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id) if customer_id else ""
    if tenant_id:
        existing = SubscriptionService.get_by_tenant_id(tenant_id)
        if existing:
            current_subscription_id = (existing.get("subscription_id") or "").strip()
            if subscription_id and current_subscription_id and subscription_id != current_subscription_id:
                logging.info(
                    "Skip stale customer.subscription.deleted for tenant %s: event subscription %s does not match current main subscription %s.",
                    tenant_id,
                    subscription_id,
                    current_subscription_id,
                )
                return
            subscription_dict = {
                "status": "canceled",
                "subscription_status": "canceled",
                # Clear subscription_id so that subsequent /upcoming or /checkout calls
                # treat the tenant as having no active subscription.  Preserving the
                # canceled id here causes /upcoming to query a canceled Stripe subscription
                # which returns invoice_upcoming_none (HTTP 400).
                "subscription_id": "",
                "customer_id": customer_id or existing.get("customer_id", ""),
                # Clear all storage quota fields so stale add-on bytes and the
                # storage subscription item id do not persist after cancellation
                # and inflate the tenant's effective quota on next subscription.
                "addon_subscription_item_id": None,
                "addon_storage_bytes": None,
                "target_storage_bytes": None,
            }
            SubscriptionService.upsert_subscription(tenant_id, subscription_dict)
        logging.info("Handled main customer.subscription.deleted for tenant %s", tenant_id)
        return
    logging.info("customer.subscription.deleted without tenant context: subscription_id=%s", subscription_id)
