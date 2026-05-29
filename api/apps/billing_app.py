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
import hashlib
import json
import logging
import time
import uuid
from urllib.parse import parse_qsl, urlencode, urlparse, urlsplit, urlunsplit
from datetime import datetime, timezone
from decimal import Decimal

import stripe
from quart import g, jsonify, request

from api.apps import current_user, login_required
from api.db import PaymentStatus, PriceType, ProductType
from api.db.db_models import DB, PaymentOrder, PointHold, Subscription
from api.db.services.billing_service import (  # noqa: F401
    PaymentOrderService,
    PointAccountService,
    PricePointService,
    ProductService,
    PurchasedProductOverviewService,
    SubscriptionService,
)
from api.db.services.file_service import FileService
from api.utils.api_utils import get_data_error_result, get_json_result, get_request_json
from pydantic import ValidationError
from api.utils.validation_utils import format_validation_error_message

from api.apps.schemas.billing_schemas import (  # noqa: F401
    CheckoutRequest,
    PointsCheckoutRequest,
    PortalSessionRequest,
    SetupIntentRequest,
    StorageSetTargetRequest,
    SubscriptionPreviewRequest,
)
from api.utils.billing import (
    BYTES_PER_GB,
    BILLING_PLAN_TRIAL_NAME,
    extract_latest_invoice_obj,
    extract_list_data,
    extract_plan_subscription_item,
    extract_storage_subscription_item,
    extract_subscription_period,
    get_attr_or_item,
    get_receipt_url_from_intent_latest_charge_async,
    get_storage_price_id_from_config,
    parse_storage_size,
    is_storage_price_id,
    is_trial_plan_name,
    reset_stripe_test_clock_id_for_current_context,
    safe_float,
    safe_int,
    set_stripe_test_clock_id_for_current_context,
    billing_set_customer_id_async,
    create_or_get_portal_configuration,
    get_pending_subscription_change_async,
    is_subscription_latest_invoice_paid_async,
    get_trial_price_id,
    has_reusable_payment_method_async,
    STRIPE_TEST_CLOCK_HEADER,
    is_downgrade_by_price_id,
    modify_subscription_plan_async,
    schedule_subscription_items_change_at_period_end_async,
    schedule_subscription_price_change_at_period_end_async,
    storage_bytes_to_quantity,
    storage_quantity_to_bytes,
    extract_subscription_items_data,
)
from api.services.billing_webhook_service import (
    FOCUSED_STRIPE_WEBHOOK,
    handle_billing_webhook_event,
)
from common import settings
from common.billing_utils import (
    amount_to_float,
    billing_enabled_guard,
    build_date_keys,
    decimal_amount,
    parse_datetime_arg,
    to_utc_date_str,
    to_utc_datetime,
    to_utc_isoformat,
)
from common.constants import RetCode


# Global cached values for billing webhook
_stripe_webhook_secret: str | None = None
_is_local_webhook_url: str | None = None
_is_local_webhook: bool | None = None


def _format_request_validation_error(e: ValidationError) -> str:
    raw = format_validation_error_message(e).strip()
    if not raw:
        return "Invalid request."

    first_line = raw.splitlines()[0]
    prefix = "Field: <"
    middle = "> - Message: <"
    suffix = ">"
    if first_line.startswith(prefix) and middle in first_line:
        field, remainder = first_line[len(prefix):].split(middle, 1)
        message = remainder.rsplit(suffix, 1)[0]
        return f"{field}: {message}"

    return first_line


# subscription
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


def _get_stored_stripe_webhook_id() -> str:
    from api.db.services.system_settings_service import SystemSettingsService

    setting = SystemSettingsService.get_first_by_name("billing_webhook_id")
    return getattr(setting, "value", "") or ""


def _summarize_stripe_signature_header(sig_header: str | None) -> dict:
    if not sig_header:
        return {
            "present": False,
            "length": 0,
            "parts": [],
            "has_timestamp": False,
            "has_v1": False,
        }

    keys = []
    for part in sig_header.split(","):
        key, _, _value = part.partition("=")
        key = key.strip()
        if key:
            keys.append(key)

    return {
        "present": True,
        "length": len(sig_header),
        "parts": keys,
        "has_timestamp": "t" in keys,
        "has_v1": "v1" in keys,
    }


def _summarize_webhook_payload(payload: bytes) -> dict:
    return {
        "length": len(payload),
        "sha256": hashlib.sha256(payload).hexdigest(),
    }


def _diagnose_unverified_stripe_event(raw_event_id: str) -> dict:
    """Best-effort diagnosis for signature failures without weakening verification."""
    if not raw_event_id:
        return {
            "checked": False,
            "reason": "missing_event_id",
        }

    try:
        event = stripe.Event.retrieve(raw_event_id)
    except stripe.InvalidRequestError as exc:
        return {
            "checked": True,
            "exists_in_configured_account": False,
            "reason": "event_not_found",
            "error_code": getattr(exc, "code", "") or "",
        }
    except Exception as exc:
        return {
            "checked": True,
            "exists_in_configured_account": None,
            "reason": "stripe_lookup_failed",
            "error_type": type(exc).__name__,
            "error": str(exc),
        }

    return {
        "checked": True,
        "exists_in_configured_account": True,
        "event_id": getattr(event, "id", "") or raw_event_id,
        "event_type": getattr(event, "type", "") or "",
        "livemode": getattr(event, "livemode", None),
    }


def _get_safe_stripe_publishable_key() -> str | None:
    publishable_key = settings.BILLING.get("stripe_publishable_key", "")
    if not publishable_key:
        return None

    if not publishable_key.startswith("pk_"):
        logging.warning("stripe_publishable_key in config does not start with 'pk_', refusing to return")
        return None

    return publishable_key


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
        conflicts.append(
            {
                "resource": "storage",
                "used": storage_used_bytes,
                "limit": total_storage_limit_bytes,
                "unit": "bytes",
                "message": f"Storage usage ({overage_gb:.2f} GB over limit) exceeds target plan quota including addon storage. Please delete data before downgrading.",
                "action_required": "delete_data",
                "overage": overage_bytes,
            }
        )

    if members_used > total_members_limit:
        overage_members = members_used - total_members_limit
        conflicts.append(
            {
                "resource": "members",
                "used": members_used,
                "limit": total_members_limit,
                "unit": "users",
                "message": f"Member count ({overage_members} users over limit) exceeds target plan quota. Please remove members before downgrading.",
                "action_required": "remove_members",
                "overage_members": overage_members,
            }
        )

    if apps_used > total_apps_limit:
        overage_apps = apps_used - total_apps_limit
        conflicts.append(
            {
                "resource": "apps",
                "used": apps_used,
                "limit": total_apps_limit,
                "unit": "applications",
                "message": f"App count ({overage_apps} apps over limit) exceeds target plan quota. Please remove apps before downgrading.",
                "action_required": "remove_apps",
                "overage_apps": overage_apps,
            }
        )

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
            merged.update(
                {
                    "quota_storage": plan.get("quota_storage", merged.get("quota_storage", 0)),
                    "quota_points": plan.get("quota_points", merged.get("quota_points", 0)),
                    "quota_members": plan.get("quota_members", merged.get("quota_members", 0)),
                    "quota_apps": plan.get("quota_apps", merged.get("quota_apps", 0)),
                    "product_type": plan.get("product_type", merged.get("product_type")),
                }
            )
            return merged

    if key != BILLING_PLAN_TRIAL_NAME:
        return _resolve_billing_plan_info(BILLING_PLAN_TRIAL_NAME)
    return info


# manager is set by the page loader in api/apps/__init__.py before exec_module runs.
# Guard so plain imports (e.g., from init_data.py for _handle_event) don't crash.
try:
    manager
except NameError:
    manager = type(
        "ManagerPlaceholder",
        (),
        {
            "before_request": lambda self, f: f,
            "after_request": lambda self, f: f,
            "route": lambda self, *_args, **_kwargs: lambda f: f,
        },
    )()


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


def handle_undelivered_events():
    from api.services.billing_webhook_service import (
        handle_undelivered_events as _handle_undelivered_events,
    )

    return _handle_undelivered_events()


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
    query = urlencode(query_pairs, doseq=True, safe="{}")
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
    setup_intent_id: str = "",
    session_success_url: str,
    session_cancel_url: str,
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
    plan_item_id, _plan_price_id, _plan_quantity = extract_plan_subscription_item(stripe_sub)
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
    if not has_payment_method and setup_intent_id:
        try:
            await _apply_setup_intent_payment_method_if_present(
                customer_id=customer_id,
                setup_intent_id=setup_intent_id,
            )
            stripe_sub = await stripe.Subscription.retrieve_async(main_subscription_id)
            has_payment_method = await has_reusable_payment_method_async(
                customer_id=customer_id,
                subscription=stripe_sub,
            )
        except ValueError as e:
            return False, {"error": str(e)}
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
        # Build full items list: preserve plan item + update/add storage item.
        # Stripe replaces all items, so omitting plan would drop it.
        if item_id:
            modify_items = [
                {"id": plan_item_id, "price": _plan_price_id, "quantity": _plan_quantity} if plan_item_id else None,
                {"id": item_id, "quantity": target_quantity},
            ]
            modify_items = [x for x in modify_items if x]
            updated = await stripe.Subscription.modify_async(
                main_subscription_id,
                items=modify_items,
                proration_behavior="always_invoice",
                payment_behavior="pending_if_incomplete",
                billing_cycle_anchor="unchanged",
                expand=["latest_invoice"],
                idempotency_key=f"billing:{tenant_id}:storage_change:{main_subscription_id}:{target_storage_bytes}",
            )
        else:
            modify_items = [
                {"id": plan_item_id, "price": _plan_price_id, "quantity": _plan_quantity} if plan_item_id else None,
                {"price": storage_price_id, "quantity": target_quantity},
            ]
            modify_items = [x for x in modify_items if x]
            updated = await stripe.Subscription.modify_async(
                main_subscription_id,
                items=modify_items,
                proration_behavior="always_invoice",
                payment_behavior="pending_if_incomplete",
                billing_cycle_anchor="unchanged",
                expand=["latest_invoice"],
                idempotency_key=f"billing:{tenant_id}:storage_change:{main_subscription_id}:{target_storage_bytes}",
            )
    except stripe.InvalidRequestError as e:
        if "Item already exists" in str(e):
            updated = await stripe.Subscription.retrieve_async(main_subscription_id, expand=["latest_invoice"])
        else:
            return False, {"error": f"Failed to modify subscription: {e}"}

    latest_invoice = extract_latest_invoice_obj(updated)
    invoice_url = (get_attr_or_item(latest_invoice, "hosted_invoice_url", "") or "").strip()
    invoice_status = (get_attr_or_item(latest_invoice, "status", "") or "").strip().lower()
    invoice_id = (get_attr_or_item(latest_invoice, "id", "") or "").strip()
    invoice_pdf_url = (get_attr_or_item(latest_invoice, "invoice_pdf", "") or "").strip()
    amount_cents = get_attr_or_item(latest_invoice, "amount_due", None)
    if amount_cents is None:
        amount_cents = get_attr_or_item(latest_invoice, "amount_paid", 0) or 0
    currency = (get_attr_or_item(latest_invoice, "currency", "") or "").upper()

    # Determine payment_state based on invoice status and amount_due
    # If amount_due is 0 or invoice is paid, treat as paid
    amount_due = get_attr_or_item(latest_invoice, "amount_due", None) or 0
    if invoice_status == "paid" or amount_due == 0:
        payment_state = "paid"
    elif invoice_status in ("open", "draft") and amount_due > 0:
        payment_state = "requires_action"
    else:
        payment_state = "pending"

    _sync_storage_subscription_record(
        tenant_id,
        updated,
        customer_id=(tenant_plan.get("customer_id") or "").strip(),
        target_storage_bytes=target_storage_bytes,
    )
    return True, {
        "addon_storage_bytes": addon_storage_bytes,
        "target_storage_bytes": target_storage_bytes,
        "payment_state": payment_state,
        "redirect_to": invoice_url if payment_state == "requires_action" else None,
        "invoice_url": invoice_url,
        "invoice_pdf_url": invoice_pdf_url,
        "invoice_id": invoice_id,
        "amount_cents": amount_cents,
        "currency": currency,
        "product_type": "storage",
    }


async def _get_tenant_plan_with_customer_id(tenant_id: str, *, require_quota_info: bool = False) -> dict:
    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id, require_quota_info=require_quota_info)
    customer_id = (tenant_plan.get("customer_id") or "").strip()
    if not customer_id:
        logging.warning(
            "No customer_id found while loading billing plan, expected one after registration; trying to create a Stripe customer for tenant %s",
            tenant_id,
        )
        customer_id = await billing_set_customer_id_async(tenant_id)
        tenant_plan["customer_id"] = customer_id
    else:
        try:
            await stripe.Customer.retrieve_async(customer_id)
        except stripe.InvalidRequestError:
            logging.warning(
                "Stripe customer %s not found for tenant %s (likely deleted); provisioning a new one",
                customer_id,
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


@manager.route("/subscription", methods=["GET"])
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_current_plan():
    tenant_plan = await _get_tenant_plan_with_customer_id(current_user.id)

    subscription_id = (tenant_plan.get("subscription_id") or "").strip()
    if subscription_id:
        tenant_plan["pending_subscription_change"] = await get_pending_subscription_change_async(subscription_id)
    tenant_plan.update(_main_subscription_payment_state(tenant_plan))
    return get_json_result(data=_serialize_current_plan_payload(tenant_plan))


@manager.route("/storage", methods=["GET"])
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
        storage_item_id, stripe_price_id, stripe_quantity_gb = extract_storage_subscription_item(stripe_sub)
        stripe_period_start, stripe_period_end = extract_subscription_period(stripe_sub)
        storage_current_period_start = stripe_period_start or storage_current_period_start
        storage_current_period_end = stripe_period_end or storage_current_period_end
        storage_status = (get_attr_or_item(stripe_sub, "status", "") or "").strip().lower()
        if storage_status:
            storage["status"] = storage_status
        if stripe_price_id:
            storage["price_id"] = stripe_price_id
            unit_price = await _get_storage_unit_price_async(stripe_price_id)
        elif safe_int(storage.get("addon_storage_bytes"), 0) > 0 or safe_int(storage.get("target_storage_bytes"), 0) > 0:
            # Stripe is authoritative: if the live subscription no longer has a
            # storage item, clear stale local storage quota cached in DB.
            with DB.atomic():
                Subscription.update(
                    addon_subscription_item_id=None,
                    addon_storage_bytes=0,
                    target_storage_bytes=0,
                ).where(Subscription.tenant_id == tenant_id).execute()
            storage["addon_subscription_item_id"] = None
            storage["addon_storage_bytes"] = 0
            storage["target_storage_bytes"] = 0
            logging.info(
                "Cleared stale local storage quota from Stripe state: tenant_id=%s subscription_id=%s status=%s",
                tenant_id,
                sub_id,
                storage_status,
            )

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


@manager.route("/storage", methods=["PATCH"])
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_storage_set_target():
    req = await get_request_json()
    try:
        validated: StorageSetTargetRequest = StorageSetTargetRequest.model_validate(req)
    except ValidationError as e:
        return get_json_result(data=False, message=_format_request_validation_error(e), code=RetCode.BAD_REQUEST)

    tenant_id = validated.tenant_id or current_user.id
    if current_user.id != tenant_id:
        return get_json_result(
            data=False,
            message="No authorization.",
            code=RetCode.AUTHENTICATION_ERROR,
        )

    ok, data = await _set_storage_target_storage_bytes_async(
        tenant_id,
        validated.target_storage_bytes,
        setup_intent_id=(validated.setup_intent_id or "").strip(),
        session_success_url=validated.session_success_url,
        session_cancel_url=validated.session_cancel_url,
    )
    if not ok:
        return get_data_error_result(message=data.get("error", "Failed to set storage target."))
    return get_json_result(data=data)


@manager.route("/subscription/overview", methods=["GET"])  # noqa: F821
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


# Short-lived in-memory cache for _get_api_request_limit_by_plan().
# Avoids a MySQL query on every API request; 60 s TTL keeps it fresh
# while still providing significant relief under high QPS.
_api_limit_cache: dict[str, tuple[float, int]] = {}  # key -> (expiry_ts, value)
_API_LIMIT_CACHE_TTL = 60  # seconds


def _invalidate_api_limit_cache(plan_name: str | None = None) -> None:
    """Evict cached API limit entries.  If *plan_name* is given only that plan's
    entries are removed; otherwise the entire cache is cleared."""
    if not plan_name:
        _api_limit_cache.clear()
        return
    key = plan_name.strip()
    _api_limit_cache.pop(f"{key}:minute", None)


def _get_api_request_limit_by_plan(plan_name: str) -> int:
    """
    Get per-minute API request limit based on plan type.

    Reads from MySQL billing_product table (durable). Falls back to
    in-memory YAML config if MySQL has no data.  Results are cached
    in-process for up to 60 seconds to avoid hammering MySQL on every
    API request.

    Args:
        plan_name: Name of the plan (e.g., "Trial", "Starter", "Pro", "Enterprise")

    Returns:
        Request limit as integer
    """
    key = (plan_name or "").strip()
    if not key:
        return 500

    # Check in-memory cache first
    cache_key = f"{key}:minute"
    cached = _api_limit_cache.get(cache_key)
    if cached:
        expiry_ts, cached_val = cached
        if time.time() < expiry_ts:
            return cached_val
        # Expired — evict
        del _api_limit_cache[cache_key]

    # Try MySQL first (durable source of truth)
    from api.db.services.billing_service import ProductService

    plan = ProductService.get_by_name(key)
    if not plan:
        plan = ProductService.get_by_name(BILLING_PLAN_TRIAL_NAME)

    result = None
    if plan:
        value = plan.get("api_request_limit_per_minute")
        if value:
            try:
                result = int(value)
            except (TypeError, ValueError):
                pass

    if result is None:
        # Fallback to in-memory YAML config
        info = settings.BILLING_PLAN_TO_INFO.get(key) or settings.BILLING_PLAN_TO_INFO.get(key.title()) or {}
        if not info:
            info = settings.BILLING_PLAN_TO_INFO.get(BILLING_PLAN_TRIAL_NAME) or {}
        value = info.get("api_request_limit_per_minute")

        if not value:
            result = 500
        else:
            try:
                result = int(value)
            except (TypeError, ValueError):
                result = 500

    # Populate cache
    _api_limit_cache[cache_key] = (time.time() + _API_LIMIT_CACHE_TTL, result)
    return result


@manager.route("/addons/overview", methods=["GET"])  # noqa: F821
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
    try:
        validated: PointsCheckoutRequest = PointsCheckoutRequest.model_validate(req)
    except ValidationError as e:
        return get_json_result(data=False, message=_format_request_validation_error(e), code=RetCode.BAD_REQUEST)

    tenant_id = validated.tenant_id or current_user.id
    if current_user.id != tenant_id:
        return get_json_result(data=False, message="No authorization.", code=RetCode.AUTHENTICATION_ERROR)

    quantity = validated.quantity
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
            "product_type": "points",
        },
        success_url=_build_checkout_success_url(validated.session_success_url),
        cancel_url=validated.session_cancel_url,
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

    return get_json_result(
        data={
            "price_id": price_id,
            "price_usd": price_usd,
            "points_per_unit": points_per_unit,
        }
    )


@manager.route("/usages/deepdoc", methods=["GET"])  # noqa: F821
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
                committed_points += hold.points
            elif hold.status == "held":
                held_points += hold.points

    pages_paid = committed_points // consuming_point_amount
    pages_unpaid = held_points // consuming_point_amount
    # 1 point = 1 cent = 0.01 USD
    amount_paid = round(committed_points / 100, 2)
    amount_unpaid = round(held_points / 100, 2)

    return get_json_result(
        data={
            "current_period_start": to_utc_date_str(cycle_start),
            "current_period_end": to_utc_date_str(cycle_end),
            "deepdoc": {
                "pages_paid": pages_paid,
                "pages_unpaid": pages_unpaid,
                "amount_paid": amount_paid,
                "amount_unpaid": amount_unpaid,
                "currency": "USD",
            },
        }
    )


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


@manager.route("/points/overview", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_points_overview():
    """Alias for /points/balance — returns normalized point usage overview."""
    return await billing_points_balance()


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
        items = list(query.order_by(PointLedgerModel.create_time.desc()).paginate(page, page_size).dicts())
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
        items = list(query.order_by(PointHoldModel.create_time.desc()).paginate(page, page_size).dicts())
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
            product_amount_cents = getattr(order, "product_amount_cents", None) or []
            spend_overview.append(
                {
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
                }
            )

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


@manager.route("/addons", methods=["GET"])
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
        product_quota_api_limits = getattr(product, "api_request_limit_per_minute", 0) or 0
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


@manager.route("/subscription/preview", methods=["POST"])
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
    try:
        validated: SubscriptionPreviewRequest = SubscriptionPreviewRequest.model_validate(req)
    except ValidationError as e:
        return get_json_result(data=False, message=_format_request_validation_error(e), code=RetCode.BAD_REQUEST)

    new_price_id = validated.new_price_id
    tenant_id = validated.tenant_id or current_user.id
    target_storage_bytes_raw = validated.target_storage_bytes

    current_plan = SubscriptionService.get_by_tenant_id(tenant_id=tenant_id)
    customer_id = validated.customer_id or current_plan.get("customer_id")
    subscription_id = current_plan.get("subscription_id")

    logging.debug(
        "billing_upcoming request: tenant_id=%s, customer_id=%s, subscription_id=%s, new_price_id=%s",
        tenant_id,
        customer_id,
        subscription_id,
        new_price_id,
    )
    if not customer_id:
        customer_id = await billing_set_customer_id_async(tenant_id)
    if not customer_id:
        return get_data_error_result(message="Missing required parameters")

    if target_storage_bytes_raw is not None:
        if not subscription_id:
            return get_data_error_result(message="No active plan subscription found")

        subscription = stripe.Subscription.retrieve(subscription_id)
        if not subscription or not subscription.get("items") or not subscription["items"].get("data"):
            return get_data_error_result(message="Subscription items not found")

        plan_item_id, plan_price_id, plan_quantity = extract_plan_subscription_item(subscription)
        if not plan_item_id or not plan_price_id:
            return get_data_error_result(message="Plan subscription item not found")

        storage_item_id, storage_price_id, _current_storage_quantity = extract_storage_subscription_item(subscription)
        target_storage_quantity = storage_bytes_to_quantity(target_storage_bytes_raw)
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
        has_payment_method = await has_reusable_payment_method_async(
            customer_id=customer_id,
            subscription=subscription,
        )
        return get_json_result(
            data={
                "amount_due_today": amount_due_today,
                "currency": upcoming_invoice.currency,
                "invoice_preview": upcoming_invoice,
                "has_reusable_payment_method": has_payment_method,
                "stripe_publishable_key": _get_safe_stripe_publishable_key(),
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
            logging.info(f"billing_upcoming: preview Trial->paid as new subscription, current_plan={current_plan_name}, target_plan={target_plan_name}, customer={customer_id}")

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

    has_payment_method = await has_reusable_payment_method_async(
        customer_id=customer_id,
        subscription=sub if is_valid_subscription and not force_new_subscription_preview else None,
    )

    return get_json_result(
        data={
            "amount_due_today": amount_due_today,
            "currency": upcoming_invoice.currency,
            "invoice_preview": upcoming_invoice,
            "has_reusable_payment_method": has_payment_method,
            "stripe_publishable_key": _get_safe_stripe_publishable_key(),
        }
    )


async def _apply_setup_intent_payment_method_if_present(*, customer_id: str, setup_intent_id: str) -> str:
    setup_intent_id = (setup_intent_id or "").strip()
    if not setup_intent_id:
        return ""

    setup_intent = await stripe.SetupIntent.retrieve_async(setup_intent_id)
    resolved_customer_id = (getattr(setup_intent, "customer", None) or "").strip()
    if resolved_customer_id and resolved_customer_id != customer_id:
        raise ValueError("Setup intent does not belong to current customer")

    status = (getattr(setup_intent, "status", None) or "").strip().lower()
    if status != "succeeded":
        raise ValueError(f"Setup intent is {status or 'not completed'}")

    payment_method_id = (getattr(setup_intent, "payment_method", None) or "").strip()
    if not payment_method_id:
        raise ValueError("Setup intent has no payment method")

    await stripe.Customer.modify_async(
        customer_id,
        invoice_settings={"default_payment_method": payment_method_id},
    )
    logging.info(
        "Saved default payment method from SetupIntent for customer_id=%s setup_intent_id=%s",
        customer_id,
        setup_intent_id,
    )
    return payment_method_id


async def _handle_active_subscription_checkout(
    *,
    tenant_id: str,
    tenant_plan: dict,
    subscription_id: str,
    subscription_status: str,
    subscription_price_id: str,
    setup_intent_id: str = "",
    session_success_url: str,
    session_cancel_url: str,
):
    """Handle checkout for active/trialing subscriptions.

    Checkout only submits Stripe changes and returns accepted/pending-payment hints.
    Webhook handlers own all DB updates and side effects.
    """
    subscription = await stripe.Subscription.retrieve_async(subscription_id)
    subscription_items = extract_subscription_items_data(subscription)

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
        logging.info(f"Tenant {tenant_id} already has subscription {subscription_id} on price {subscription_price_id}, current items: {current_item_price_ids}")
        msg = f"Tenant {tenant_id} already has an active subscription {subscription_id} on price {subscription_price_id}. Requested price_id matches the current plan '{current_plan_name}'."
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
        scheduled = await schedule_subscription_price_change_at_period_end_async(subscription_id, subscription_price_id, target_storage_quantity=target_storage_quantity)
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
        if setup_intent_id:
            try:
                await _apply_setup_intent_payment_method_if_present(
                    customer_id=customer_id,
                    setup_intent_id=setup_intent_id,
                )
                subscription = await stripe.Subscription.retrieve_async(subscription_id)
            except ValueError as e:
                return get_data_error_result(message=str(e))
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
    error_message = modified_result.get("error_message")
    if error_message:
        logging.warning(
            "Subscription modification failed for tenant %s: %s",
            tenant_id,
            error_message,
        )
        return get_json_result(
            code=RetCode.BILLING_UPGRADE_FAILED,
            message=error_message,
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

    # Determine payment_state based on invoice/payment status
    if invoice_status == "paid" or payment_intent_status == "succeeded":
        payment_state = "paid"
    elif payment_intent_status in payment_action_statuses or invoice_status in payment_action_statuses:
        payment_state = "requires_action"
    else:
        payment_state = "pending"

    accepted_data = {
        "customer_id": (tenant_plan.get("customer_id") or "").strip()
        or ((getattr(updated_subscription, "customer", "") or "").strip() if not isinstance(updated_subscription, dict) else (updated_subscription.get("customer") or "").strip()),
        "subscription_id": subscription_id,
        "plan_name": plan_name_after_upgrade,
        "price_id": subscription_price_id,
        "invoice_id": invoice_id,
        "invoice_url": invoice_url,
        "invoice_status": invoice_status,
        "amount_cents": modified_result.get("amount_cents", 0),
        "currency": modified_result.get("currency", ""),
        "payment_intent_status": payment_intent_status,
        "payment_state": payment_state,
        "product_type": "subscription",
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
    """Handle delinquent but recoverable subscriptions by requiring payment recovery first."""
    invoice_url = (tenant_plan.get("invoice_url") or "").strip()
    logging.info(f"Tenant {tenant_id} has delinquent subscription {subscription_id} ({subscription_status}); redirecting to invoice {invoice_url} for payment.")
    return get_json_result(
        data={"payment_required": True, "invoice_url": invoice_url},
        message="Your subscription has an outstanding invoice. Please pay it before changing your plan.",
        code=RetCode.SUCCESS,
    )


async def _create_subscription_checkout(
    *,
    tenant_id: str,
    customer_id: str,
    subscription_price_id: str,
    quantity: int,
    setup_intent_id: str = "",
):
    """
    Create a subscription directly via Stripe Subscription API (no Checkout Session).

    Args:
        setup_intent_id: If provided, apply the SetupIntent's payment method as the
            customer's default before creating the subscription.

    Raises:
        ValueError: if subscription_price_id belongs to Trial plan (Trial must not
            go through Stripe subscription lifecycle per Plan B).
    """
    from api.utils.billing import get_trial_price_id

    # Trial plan must NOT be created via Stripe subscription (Plan B)
    trail_price_id = get_trial_price_id(settings.BILLING.get("billing_plans", []))
    if subscription_price_id == trail_price_id:
        raise ValueError(
            f"Trial plan price_id {subscription_price_id!r} must not be used with "
            "_create_subscription_checkout. Trial tenants do not create Stripe subscriptions."
        )

    logging.info(
        "Create subscription (direct API): tenant_id=%s, price_id=%s, plan=%s",
        tenant_id,
        subscription_price_id,
        settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, ""),
    )

    # Apply SetupIntent payment method as customer's default before creating subscription
    if setup_intent_id:
        await _apply_setup_intent_payment_method_if_present(
            customer_id=customer_id,
            setup_intent_id=setup_intent_id,
        )

    subscription_params = {
        "customer": customer_id,
        "items": [{"price": subscription_price_id, "quantity": quantity}],
        "payment_behavior": "error_if_incomplete",
    }

    subscription = await stripe.Subscription.create_async(**subscription_params)
    logging.info(f"created stripe subscription id {subscription.id}")
    return get_json_result(data={"subscription_id": subscription.id})


async def _handle_subscription_checkout(
    *,
    tenant_id: str,
    tenant_plan: dict,
    customer_id: str,
    subscription_price_id: str,
    quantity: int,
    setup_intent_id: str = "",
    session_success_url: str,
    session_cancel_url: str,
):
    """Dispatch subscription checkout to active/recoverable/new-subscription flows."""
    subscription_id = tenant_plan.get("subscription_id")
    subscription_status = tenant_plan.get("subscription_status")

    if not subscription_id:
        logging.info(
            "No existing subscription for tenant %s; creating subscription for customer %s",
            tenant_id,
            customer_id,
        )
        return await _create_subscription_checkout(
            tenant_id=tenant_id,
            customer_id=customer_id,
            subscription_price_id=subscription_price_id,
            quantity=quantity,
            setup_intent_id=setup_intent_id,
        )
    elif subscription_status in MAIN_SUBSCRIPTION_RECOVERABLE_STATUSES:
        return await _handle_recoverable_subscription_checkout(
            tenant_id=tenant_id,
            tenant_plan=tenant_plan,
            subscription_id=subscription_id,
            subscription_status=subscription_status,
            subscription_price_id=subscription_price_id,
        )
    else:
        return await _handle_active_subscription_checkout(
            tenant_id=tenant_id,
            tenant_plan=tenant_plan,
            subscription_id=subscription_id,
            subscription_status=subscription_status,
            subscription_price_id=subscription_price_id,
            setup_intent_id=setup_intent_id,
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


@manager.route("/subscription", methods=["PATCH", "POST"])
@manager.route("/addon-purchases", methods=["POST"])
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_checkout():
    """
    Handles subscription purchase, upgrade, and downgrade via Stripe Checkout.
    """
    req = await get_request_json()
    try:
        validated: CheckoutRequest = CheckoutRequest.model_validate(req)
    except ValidationError as e:
        return get_json_result(data=False, message=_format_request_validation_error(e), code=RetCode.BAD_REQUEST)

    # Authorization check (kept separate from Pydantic validation)
    tenant_id = validated.tenant_id or current_user.id
    if current_user.id != tenant_id:
        return get_json_result(data=False, message="No authorization.", code=RetCode.AUTHENTICATION_ERROR)

    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id)
    customer_id = tenant_plan.get("customer_id")
    if not customer_id:
        logging.warning("No customer_id found while checkout, it was expected create when user registion, try to create a stripe accout to proceed...")
        customer_id = await billing_set_customer_id_async(tenant_id)

    if validated.payment_type == PriceType.SUBSCRIPTION:
        return await _handle_subscription_checkout(
            tenant_id=tenant_id,
            tenant_plan=tenant_plan,
            customer_id=customer_id,
            subscription_price_id=validated.subscription_price_id,
            quantity=validated.quantity,
            setup_intent_id=validated.setup_intent_id,
            session_success_url=validated.session_success_url,
            session_cancel_url=validated.session_cancel_url,
        )

    return await _handle_addon_checkout(
        tenant_id=tenant_id,
        customer_id=customer_id,
        addon_price_id=validated.addon_price_id,
        quantity=validated.quantity,
        expiry_time=validated.expiry_time,
        session_success_url=validated.session_success_url,
        session_cancel_url=validated.session_cancel_url,
    )


@manager.route("/setup-intents", methods=["POST"])
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_create_setup_intent():
    """
    Create a Stripe SetupIntent for collecting payment method details.

    Request body (JSON):
        tenant_id (str, optional): Defaults to authenticated user.
        setup_type (str): Either "subscription_upgrade" or "storage_addon".
        price_id (str, optional): Target price ID for subscription upgrade.
        target_storage_bytes (int, optional): Target storage for addon increase.

    Response data:
        client_secret (str): Stripe SetupIntent client secret for Elements.
        setup_intent_id (str): Stripe SetupIntent ID for confirmation.
    """
    req = await get_request_json()
    try:
        validated: SetupIntentRequest = SetupIntentRequest.model_validate(req)
    except ValidationError as e:
        return get_json_result(data=False, message=_format_request_validation_error(e), code=RetCode.BAD_REQUEST)

    tenant_id = current_user.id

    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id)
    customer_id = tenant_plan.get("customer_id")
    if not customer_id:
        customer_id = await billing_set_customer_id_async(tenant_id)
    if not customer_id:
        return get_data_error_result(message="No customer_id found for tenant")

    # Create Stripe SetupIntent
    setup_intent = stripe.SetupIntent.create(
        customer=customer_id,
        usage="off_session",
        metadata={
            "tenant_id": tenant_id,
            "setup_type": validated.setup_type,
            "price_id": validated.price_id or "",
            "target_storage_bytes": str(validated.target_storage_bytes or ""),
        },
    )

    logging.info(f"Created SetupIntent {setup_intent.id} for tenant {tenant_id}, type={validated.setup_type}")

    return get_json_result(
        data={
            "client_secret": setup_intent.client_secret,
            "setup_intent_id": setup_intent.id,
        }
    )


@manager.route("/portal-sessions", methods=["POST"])
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def customer_portal():
    req = await get_request_json()
    try:
        validated: PortalSessionRequest = PortalSessionRequest.model_validate(req)
    except ValidationError as e:
        return get_json_result(data=False, message=_format_request_validation_error(e), code=RetCode.BAD_REQUEST)

    tenant_id = validated.tenant_id or current_user.id
    return_url = validated.return_url

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
        configuration = create_or_get_portal_configuration()
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
    stored_webhook_id = _get_stored_stripe_webhook_id()

    # Return cached secret (never expires - it's persistent in DB)
    if _stripe_webhook_secret and not force_refresh:
        logging.info("Using cached Stripe webhook secret for stored webhook id=%s", stored_webhook_id)
        return _stripe_webhook_secret

    # Load from persistent storage
    from api.db.services.system_settings_service import SystemSettingsService

    if force_refresh:
        logging.info("Refreshing Stripe webhook secret from database for stored webhook id=%s", stored_webhook_id)
    setting = SystemSettingsService.get_first_by_name("billing_webhook_secret")
    if setting and getattr(setting, "value", ""):
        _stripe_webhook_secret = setting.value
        logging.info("Loaded Stripe webhook secret from database for stored webhook id=%s", stored_webhook_id)
        return _stripe_webhook_secret

    logging.error("Could not retrieve webhook secret from database for stored webhook id=%s. Webhook verification will fail.", stored_webhook_id)
    return None


def _get_is_local_webhook() -> bool:
    """
    Check if the configured webhook URL is a local address.
    Cache the result only for the currently configured webhook URL.

    The billing config may be initialized lazily or refreshed after process
    startup. A plain boolean cache can therefore become stale and incorrectly
    force local Stripe CLI webhooks down the signature-verification path.
    """
    global _is_local_webhook, _is_local_webhook_url

    webhook_url = settings.BILLING.get("webhook_url", "")
    if _is_local_webhook is not None and _is_local_webhook_url == webhook_url:
        return _is_local_webhook

    hostname = urlparse(webhook_url).hostname
    _is_local_webhook_url = webhook_url
    _is_local_webhook = hostname in ["localhost", "127.0.0.1"]
    logging.info("Detected local webhook: %s (is_local=%s)", webhook_url, _is_local_webhook)
    return _is_local_webhook


@manager.route("/checkouts/<session_id>", methods=["GET"])
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_session_status(session_id: str):
    """
    Return the payment status of a Stripe Checkout session.
    Frontend polls this after redirect from Stripe to determine outcome.
    """
    checkout_session = await stripe.checkout.Session.retrieve_async(session_id)

    payment_intent_id = get_attr_or_item(checkout_session, "payment_intent", None)
    if payment_intent_id and not isinstance(payment_intent_id, str):
        payment_intent_id = get_attr_or_item(payment_intent_id, "id", None)
    receipt_url = None
    invoice_id = None
    invoice_url = None
    invoice_pdf_url = None

    if isinstance(payment_intent_id, str):
        try:
            payment_intent = await stripe.PaymentIntent.retrieve_async(payment_intent_id)
            latest_charge = get_attr_or_item(payment_intent, "latest_charge", None)
            latest_charge_id = latest_charge if isinstance(latest_charge, str) else get_attr_or_item(latest_charge, "id", "")
            if latest_charge_id:
                receipt_url = await get_receipt_url_from_intent_latest_charge_async(latest_charge_id)
            if not receipt_url:
                charges = extract_list_data(get_attr_or_item(payment_intent, "charges", None))
                if charges:
                    receipt_url = get_attr_or_item(charges[0], "receipt_url", None)
        except stripe.StripeError as e:
            logging.warning("Failed to retrieve Stripe payment intent details for checkout session %s: %s", session_id, e)

    raw_invoice = get_attr_or_item(checkout_session, "invoice", None)
    if raw_invoice:
        invoice_id = raw_invoice if isinstance(raw_invoice, str) else get_attr_or_item(raw_invoice, "id", None)
        invoice_obj = raw_invoice if not isinstance(raw_invoice, str) else None
        if isinstance(invoice_id, str) and not invoice_obj:
            try:
                invoice_obj = await stripe.Invoice.retrieve_async(invoice_id)
            except stripe.StripeError as e:
                logging.warning("Failed to retrieve Stripe invoice details for checkout session %s: %s", session_id, e)
        if invoice_obj:
            invoice_url = get_attr_or_item(invoice_obj, "hosted_invoice_url", None)
            invoice_pdf_url = get_attr_or_item(invoice_obj, "invoice_pdf", None)

    return get_json_result(
        data={
            "payment_status": get_attr_or_item(checkout_session, "payment_status", "unknown"),
            "mode": get_attr_or_item(checkout_session, "mode", None),
            "amount_cents": get_attr_or_item(checkout_session, "amount_total", None),
            "currency": get_attr_or_item(checkout_session, "currency", None),
            "created": get_attr_or_item(checkout_session, "created", None),
            "metadata": dict(get_attr_or_item(checkout_session, "metadata", None) or {}),
            "payment_intent_id": payment_intent_id,
            "receipt_url": receipt_url,
            "invoice_id": invoice_id,
            "invoice_url": invoice_url,
            "invoice_pdf_url": invoice_pdf_url,
        }
    )


@manager.route("/webhooks/stripe", methods=["POST"])
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
    is_local_webhook = _get_is_local_webhook()
    webhook_url = settings.BILLING.get("webhook_url", "")

    # For local webhooks (stripe CLI forwarding), skip signature verification
    # regardless of whether we have a webhook secret in the database.
    if not is_local_webhook and not webhook_secret:
        logging.error("Could not retrieve webhook secret from Stripe. Cannot verify webhook signature. Rejecting webhook.")
        # Signature verification cannot proceed without the endpoint secret.
        # Return non-2xx so Stripe retries after transient config/database issues.
        return jsonify(success=False), RetCode.BAD_REQUEST

    sig_header = request.headers.get("stripe-signature")
    raw_event_id = event.get("id", "") if isinstance(event, dict) else ""
    raw_event_type = event.get("type", "") if isinstance(event, dict) else ""
    stored_webhook_id = _get_stored_stripe_webhook_id()
    sig_header_summary = _summarize_stripe_signature_header(sig_header)
    payload_summary = _summarize_webhook_payload(payload)

    # Skip signature verification for local webhook URLs (e.g., when using stripe CLI forwarding)
    if is_local_webhook:
        logging.warning(
            "Skipping webhook signature verification for local webhook_url=%s. raw_event_id=%s raw_event_type=%s",
            webhook_url,
            raw_event_id,
            raw_event_type,
        )
    else:
        try:
            event = stripe.Webhook.construct_event(payload, sig_header, webhook_secret)
        except stripe.SignatureVerificationError:
            # Secret may have been rotated; force refresh and retry once
            logging.warning(
                "Signature verification failed, refreshing secret and retrying: stored_webhook_id=%s webhook_url=%s raw_event_id=%s raw_event_type=%s sig_header=%s payload=%s",
                stored_webhook_id,
                webhook_url,
                raw_event_id,
                raw_event_type,
                sig_header_summary,
                payload_summary,
            )
            webhook_secret = _get_stripe_webhook_secret(force_refresh=True)
            if not webhook_secret:
                logging.error("Could not retrieve webhook secret after refresh. Rejecting webhook.")
                return jsonify(success=False), 400
            try:
                event = stripe.Webhook.construct_event(payload, sig_header, webhook_secret)
            except stripe.SignatureVerificationError:
                event_lookup = _diagnose_unverified_stripe_event(raw_event_id)
                logging.exception(
                    "Signature verification failed after refresh. Rejecting webhook: stored_webhook_id=%s webhook_url=%s raw_event_id=%s raw_event_type=%s sig_header=%s payload=%s event_lookup=%s",
                    stored_webhook_id,
                    webhook_url,
                    raw_event_id,
                    raw_event_type,
                    sig_header_summary,
                    payload_summary,
                    event_lookup,
                )
                # Invalid signatures must never be acknowledged with 2xx, otherwise
                # we would accept an event we cannot trust or replay safely.
                return jsonify(success=False), 400

    # Handle the event
    event_type = event["type"]
    if event_type in FOCUSED_STRIPE_WEBHOOK:
        logging.info("Processing focused Stripe webhook: type=%s, id=%s", event_type, event.get("id", ""))
        try:
            await handle_billing_webhook_event(event)
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
