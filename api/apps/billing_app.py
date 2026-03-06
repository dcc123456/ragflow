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
import json
import logging
import time
import uuid
from datetime import datetime, timezone
from decimal import Decimal

import stripe
from peewee import IntegrityError
from pydantic import ValidationError
from quart import jsonify, redirect, request

from api.apps import current_user, login_required
from api.db import PaymentChannel, PaymentMethod, PaymentStatus, PriceType, ProductType, SubscriptionStatus
from api.db.db_models import DB, PaymentOrder, PointHold
from api.db.services.billing_service import (
    BillingWebhookEventService,
    LocalPriceService,
    PaymentOrderService,
    PointAccountService,
    PricePointService,
    ProductService,
    PurchasedProductOverviewService,
    StorageSubscriptionService,
    SubscriptionService,
)
from api.db.services.file_service import FileService
from api.utils.api_utils import get_data_error_result, get_json_result, get_request_json, server_error_response
from api.utils.billing import (
    BILLING_PLAN_TRIAL_NAME,
    STORAGE_PRODUCT_NAME,
    extract_invoice_id_and_status,
    extract_latest_invoice_obj,
    extract_subscription_item,
    extract_subscription_period,
    get_product_id_by_name,
    get_storage_price_id_from_config,
    is_storage_plan_name,
    is_storage_price_id,
    is_trial_plan_name,
    safe_float,
    safe_int,
    billing_set_customer_id_async,
    cancel_scheduled_subscription_change_async,
    create_or_get_portal_configuration,
    get_plans_equal_or_higher,
    get_pending_subscription_change_async,
    get_product_ids_for_prices,
    get_receipt_url_from_intent_latest_charge,
    is_subscription_latest_invoice_paid_async,
    is_subscription_latest_invoice_paid_sync,
    get_trial_price_id,
    is_downgrade_by_price_id,
    schedule_subscription_quantity_change_at_period_end_async,
    schedule_subscription_price_change_at_period_end_async,
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
)
from common.constants import RetCode
from common.misc_utils import get_uuid
from rag.utils.redis_conn import REDIS_CONN

UNLIMITED_API_REQUESTS = 2_147_483_647
LIMITED_API_REQUESTS = 5000

# subscription
INVOICE_PAID = "invoice.paid"  # store 'subscription.id' and 'customer.id'verification.
INVOICE_FAILED = "invoice.payment_failed"  #  notify customers and send them to the customer portal to update their payment method.
CHECKOUT_SESSION_COMPLETED = "checkout.session.completed"
# SUBSCRIPTION_CREATED = "customer.subscription.created"
SUBSCRIPTION_UPDATED = "customer.subscription.updated"
SUBSCRIPTION_DELETED = "customer.subscription.deleted"
# one-off
PAYMENT_INTENT_SUCCEEDED = "payment_intent.succeeded"

FOCUSED_STRIPE_WEBHOOK = [INVOICE_PAID, INVOICE_FAILED, SUBSCRIPTION_UPDATED, SUBSCRIPTION_DELETED, CHECKOUT_SESSION_COMPLETED, PAYMENT_INTENT_SUCCEEDED]
PLANS_CACHE_KEY = settings.BILLING.get("plans_cache_key", "saas:billing:plans:latest")
PLANS_CACHE_TTL_SECONDS = settings.BILLING.get("plans_cache_ttl_seconds", 60 * 60 * 24)
USAGE_BASED_PLANS_CACHE_KEY = settings.BILLING.get("usage_based_plans_cache_key", "saas:billing:usage_based:latest")


def _billing_disabled_response():
    return get_data_error_result(message="Billing is disabled.")


def _billing_disabled_webhook_response():
    logging.info("Billing disabled; ignoring Stripe webhook.")
    return jsonify(success=True)


def _storage_effective_kb(tenant_id: str) -> int:
    return StorageSubscriptionService.effective_storage_kb(tenant_id)


async def _get_storage_unit_price_async(price_id: str = "") -> float:
    target_price_id = (price_id or "").strip() or get_storage_price_id_from_config()
    if not target_price_id:
        return 0.0
    try:
        stripe_price = await stripe.Price.retrieve_async(target_price_id)
        if isinstance(stripe_price, dict):
            unit_amount = stripe_price.get("unit_amount")
        else:
            unit_amount = getattr(stripe_price, "unit_amount", None)
        if unit_amount is None:
            return 0.0
        return safe_float(unit_amount, 0.0) / 100.0
    except Exception as e:
        logging.warning(f"Failed to retrieve storage unit price from Stripe for {target_price_id}: {e}")
        return 0.0


async def _hydrate_storage_period_fields_if_missing(tenant_id: str, storage_row: dict) -> dict:
    """
    Self-heal legacy/incomplete rows: if storage subscription exists but period
    fields are empty, refresh from Stripe and persist once.
    """
    if not storage_row:
        return {}
    subscription_id = (storage_row.get("subscription_id") or "").strip()
    if not subscription_id:
        return storage_row

    has_period = bool(storage_row.get("current_period_start") and storage_row.get("current_period_end"))
    if has_period:
        return storage_row

    try:
        stripe_subscription = await stripe.Subscription.retrieve_async(subscription_id)
        _sync_storage_subscription_record(
            tenant_id,
            stripe_subscription,
            customer_id=storage_row.get("customer_id", ""),
            clear_pending=False,
            pending_quantity_gb=storage_row.get("pending_quantity_gb"),
            pending_action=storage_row.get("pending_action", ""),
            pending_effective_at=storage_row.get("pending_effective_at"),
            schedule_id=storage_row.get("schedule_id", ""),
            target_quantity_gb=storage_row.get("target_quantity_gb"),
        )
        return StorageSubscriptionService.get_by_tenant_id(tenant_id) or storage_row
    except Exception as e:
        logging.warning(f"Failed to hydrate storage period fields for tenant {tenant_id}: {e}")
        return storage_row


def _sync_storage_subscription_record(
    tenant_id: str,
    subscription_obj,
    customer_id: str = "",
    *,
    target_quantity_gb: int | None = None,
    clear_pending: bool = False,
    pending_quantity_gb: int | None = None,
    pending_action: str = "",
    pending_effective_at=None,
    schedule_id: str | None = None,
) -> bool:
    if not tenant_id:
        return False

    item_id, price_id, quantity = extract_subscription_item(subscription_obj)
    if isinstance(subscription_obj, dict):
        subscription_id = (subscription_obj.get("id") or "").strip()
        status = (subscription_obj.get("status") or "").strip()
        period_start, period_end = extract_subscription_period(subscription_obj)
        cancel_at_period_end = bool(subscription_obj.get("cancel_at_period_end", False))
        schedule = (subscription_obj.get("schedule") or "").strip()
        customer = customer_id or (subscription_obj.get("customer") or "")
    else:
        subscription_id = (getattr(subscription_obj, "id", "") or "").strip()
        status = (getattr(subscription_obj, "status", "") or "").strip()
        period_start, period_end = extract_subscription_period(subscription_obj)
        cancel_at_period_end = bool(getattr(subscription_obj, "cancel_at_period_end", False))
        schedule = (getattr(subscription_obj, "schedule", "") or "").strip()
        customer = customer_id or (getattr(subscription_obj, "customer", "") or "")

    update_dict = {
        "customer_id": customer,
        "subscription_id": subscription_id,
        "subscription_item_id": item_id,
        "price_id": price_id,
        "schedule_id": schedule_id if schedule_id is not None else schedule,
        "current_period_start": period_start,
        "current_period_end": period_end,
        "cancel_at_period_end": cancel_at_period_end,
        "status": status,
    }

    if target_quantity_gb is not None:
        update_dict["target_quantity_gb"] = max(safe_int(target_quantity_gb, 0), 0)

    if clear_pending:
        update_dict["pending_quantity_gb"] = None
        update_dict["pending_action"] = ""
        update_dict["pending_effective_at"] = None
        update_dict["effective_quantity_gb"] = max(quantity, 0)
        update_dict["target_quantity_gb"] = max(quantity, 0)
    else:
        if pending_quantity_gb is not None:
            update_dict["pending_quantity_gb"] = max(safe_int(pending_quantity_gb, 0), 0)
        if pending_action:
            update_dict["pending_action"] = pending_action
        if pending_effective_at is not None:
            update_dict["pending_effective_at"] = to_utc_datetime(pending_effective_at)
        if "effective_quantity_gb" not in update_dict:
            existed = StorageSubscriptionService.get_by_tenant_id(tenant_id)
            if existed:
                update_dict["effective_quantity_gb"] = max(safe_int(existed.get("effective_quantity_gb"), 0), 0)
            else:
                update_dict["effective_quantity_gb"] = 0
            if "target_quantity_gb" not in update_dict:
                update_dict["target_quantity_gb"] = update_dict["effective_quantity_gb"]

    return StorageSubscriptionService.upsert_by_tenant_id(tenant_id, **update_dict)


def _has_storage_blocking_pending(tenant_id: str) -> bool:
    return StorageSubscriptionService.has_blocking_pending_by_tenant_id(tenant_id)


async def _abandon_storage_pending_increase_async(tenant_id: str) -> tuple[bool, dict]:
    """
    Void the unpaid proration invoice for a pending storage increase and roll back
    the Stripe pending_update so the user can set a new target.

    Only applicable when pending_action == "increase".  Stripe automatically clears
    the pending_update and reverts the subscription quantity once the invoice is voided.
    """
    storage = StorageSubscriptionService.get_by_tenant_id(tenant_id)
    if not storage:
        return False, {"error": "No storage subscription found."}

    pending_action = (storage.get("pending_action") or "").strip().lower()
    if pending_action != "increase":
        return False, {"error": "No pending increase to abandon."}

    subscription_id = (storage.get("subscription_id") or "").strip()
    if not subscription_id:
        return False, {"error": "No storage subscription found."}

    try:
        stripe_sub = await stripe.Subscription.retrieve_async(subscription_id, expand=["latest_invoice"])
    except Exception as e:
        return False, {"error": f"Failed to retrieve subscription: {e}"}

    latest_invoice = extract_latest_invoice_obj(stripe_sub)
    invoice_id, invoice_status = extract_invoice_id_and_status(latest_invoice)
    effective_quantity_gb = safe_int(storage.get("effective_quantity_gb"), 0)
    pending_quantity_gb = safe_int(storage.get("pending_quantity_gb"), 0)

    if not invoice_id:
        # No invoice attached — clear pending state optimistically.
        _sync_storage_subscription_record(tenant_id, stripe_sub, clear_pending=True)
        return True, {"abandoned": True, "effective_quantity_gb": effective_quantity_gb}

    if invoice_status == "paid":
        return False, {"error": "Invoice already paid. Cannot abandon."}

    if invoice_status == "void":
        # Already voided (race with another request or webhook) — just clean up DB.
        _sync_storage_subscription_record(tenant_id, stripe_sub, clear_pending=True)
        return True, {
            "abandoned": True,
            "effective_quantity_gb": effective_quantity_gb,
            "voided_invoice_id": invoice_id,
        }

    if invoice_status not in {"open", "draft"}:
        return False, {"error": f"Invoice in unexpected state '{invoice_status}'. Cannot abandon."}

    try:
        await stripe.Invoice.void_invoice_async(invoice_id)
    except Exception as e:
        return False, {"error": f"Failed to void invoice: {e}"}

    # With payment_behavior="pending_if_incomplete", the subscription quantity is
    # NEVER applied until the invoice is paid.  Voiding the invoice discards all
    # pending items and leaves the subscription at its original quantity — no manual
    # revert or proration cleanup is needed.
    try:
        fresh_sub = await stripe.Subscription.retrieve_async(subscription_id)
        _sync_storage_subscription_record(tenant_id, fresh_sub, clear_pending=True)
    except Exception as e:
        logging.warning(f"Failed to sync storage record after void for tenant {tenant_id}: {e}")
        StorageSubscriptionService.upsert_by_tenant_id(
            tenant_id,
            pending_quantity_gb=None,
            pending_action="",
            pending_effective_at=None,
            effective_quantity_gb=effective_quantity_gb,
            target_quantity_gb=effective_quantity_gb,
        )

    return True, {
        "abandoned": True,
        "effective_quantity_gb": effective_quantity_gb,
        "abandoned_quantity_gb": pending_quantity_gb,
        "voided_invoice_id": invoice_id,
    }


async def _create_storage_checkout_session_async(
    tenant_id: str,
    customer_id: str,
    storage_price_id: str,
    target_quantity_gb: int,
    session_success_url: str,
    session_cancel_url: str,
    main_period_end=None,
):
    metadata = {
        "price_type": PriceType.SUBSCRIPTION,
        "tenant_id": tenant_id,
        "price_id": storage_price_id,
        "product_name": STORAGE_PRODUCT_NAME,
        "target_quantity_gb": str(target_quantity_gb),
    }
    subscription_data = {
        "metadata": metadata.copy(),
        "proration_behavior": "create_prorations",
    }
    if main_period_end:
        subscription_data["billing_cycle_anchor"] = int(to_utc_datetime(main_period_end).timestamp())

    session = await stripe.checkout.Session.create_async(
        customer=customer_id,
        client_reference_id=f"storage_order_{uuid.uuid4()}",
        line_items=[{"price": storage_price_id, "quantity": target_quantity_gb}],
        mode=PriceType.SUBSCRIPTION,
        success_url=session_success_url,
        cancel_url=session_cancel_url,
        metadata=metadata,
        subscription_data=subscription_data,
    )
    return session


async def _set_storage_cancel_at_period_end_async(tenant_id: str, value: bool = True) -> tuple[bool, dict]:
    storage = StorageSubscriptionService.get_by_tenant_id(tenant_id)
    if not storage or not storage.get("subscription_id"):
        return True, {}

    subscription_id = storage.get("subscription_id")
    updated = await stripe.Subscription.modify_async(subscription_id, cancel_at_period_end=value)
    _, updated_period_end = extract_subscription_period(updated)
    if value:
        _sync_storage_subscription_record(
            tenant_id,
            updated,
            target_quantity_gb=0,
            clear_pending=False,
            pending_quantity_gb=0,
            pending_action="cancel",
            pending_effective_at=updated_period_end,
        )
    else:
        _sync_storage_subscription_record(
            tenant_id,
            updated,
            clear_pending=True,
        )
    return True, {
        "cancel_at_period_end": value,
        "effective_at": updated_period_end,
    }


def _current_storage_effective_gb(storage_row: dict, stripe_quantity: int = 0) -> int:
    local_effective = safe_int(storage_row.get("effective_quantity_gb"), 0) if storage_row else 0
    if local_effective > 0:
        return local_effective
    return max(safe_int(stripe_quantity, 0), 0)


async def _set_storage_target_quantity_async(
    tenant_id: str,
    target_quantity_gb: int,
    *,
    session_success_url: str = "",
    session_cancel_url: str = "",
) -> tuple[bool, dict]:
    if target_quantity_gb < 0:
        return False, {"error": "Quantity must be a non-negative integer."}

    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id)
    is_trial = is_trial_plan_name(tenant_plan.get("plan_name", ""))

    if _has_storage_blocking_pending(tenant_id):
        storage_row = StorageSubscriptionService.get_by_tenant_id(tenant_id) or {}
        pending_action = (storage_row.get("pending_action") or "").strip().lower()
        pending_quantity_gb = storage_row.get("pending_quantity_gb")
        invoice_url = ""
        if pending_action == "increase":
            try:
                sub = await stripe.Subscription.retrieve_async(
                    storage_row.get("subscription_id", ""), expand=["latest_invoice"]
                )
                _, _, _, invoice_url = await is_subscription_latest_invoice_paid_async(sub)
            except Exception:
                pass
        return False, {
            "error": "Storage has a pending payment. Please finish current payment first.",
            "pending_action": pending_action,
            "pending_quantity_gb": pending_quantity_gb,
            "invoice_url": invoice_url,
            "can_abandon": pending_action == "increase",
        }

    storage = StorageSubscriptionService.get_by_tenant_id(tenant_id)
    main_period_end = to_utc_datetime(tenant_plan.get("end_time"))
    has_storage_subscription = bool((storage or {}).get("subscription_id", "").strip())

    # Trial tenants are allowed to keep/cancel existing storage, but cannot add or resume positive quantity.
    if is_trial and target_quantity_gb > 0:
        return False, {"error": "Trial plan does not support storage add-on."}

    # First purchase path: create a dedicated storage subscription via Checkout.
    if not has_storage_subscription:
        if target_quantity_gb == 0:
            return True, {
                "effective_quantity_gb": 0,
                "target_quantity_gb": 0,
                "pending": False,
                "message": "No active storage subscription.",
            }

        customer_id = (tenant_plan.get("customer_id") or "").strip()
        if not customer_id:
            customer_id = await billing_set_customer_id_async(tenant_id)
        if not customer_id:
            return False, {"error": "Customer not found."}

        storage_price_id = get_storage_price_id_from_config()
        if not storage_price_id:
            return False, {"error": "Storage price is not configured."}

        session = await _create_storage_checkout_session_async(
            tenant_id=tenant_id,
            customer_id=customer_id,
            storage_price_id=storage_price_id,
            target_quantity_gb=target_quantity_gb,
            session_success_url=session_success_url or settings.BILLING["session_success_url"],
            session_cancel_url=session_cancel_url or settings.BILLING["session_cancel_url"],
            main_period_end=main_period_end,
        )
        return True, {"redirect_to": session.url, "pending": True, "target_quantity_gb": target_quantity_gb}

    storage_subscription_id = (storage.get("subscription_id") or "").strip()
    stripe_storage_subscription = await stripe.Subscription.retrieve_async(storage_subscription_id)
    storage_subscription_status = (
        (stripe_storage_subscription.get("status") or "").strip().lower()
        if isinstance(stripe_storage_subscription, dict)
        else (getattr(stripe_storage_subscription, "status", "") or "").strip().lower()
    )
    # A canceled/expired subscription cannot be modified; create a new one via Checkout.
    if storage_subscription_status in {"canceled", "incomplete_expired"}:
        if target_quantity_gb == 0:
            return True, {
                "effective_quantity_gb": 0,
                "target_quantity_gb": 0,
                "pending": False,
                "message": "No active storage subscription.",
            }

        customer_id = (tenant_plan.get("customer_id") or "").strip()
        if not customer_id:
            customer_id = await billing_set_customer_id_async(tenant_id)
        if not customer_id:
            return False, {"error": "Customer not found."}

        storage_price_id = get_storage_price_id_from_config()
        if not storage_price_id:
            return False, {"error": "Storage price is not configured."}

        session = await _create_storage_checkout_session_async(
            tenant_id=tenant_id,
            customer_id=customer_id,
            storage_price_id=storage_price_id,
            target_quantity_gb=target_quantity_gb,
            session_success_url=session_success_url or settings.BILLING["session_success_url"],
            session_cancel_url=session_cancel_url or settings.BILLING["session_cancel_url"],
            main_period_end=main_period_end,
        )
        return True, {"redirect_to": session.url, "pending": True, "target_quantity_gb": target_quantity_gb}

    item_id, _price_id, stripe_quantity = extract_subscription_item(stripe_storage_subscription)
    if not item_id:
        return False, {"error": "Storage subscription item not found."}

    effective_quantity_gb = _current_storage_effective_gb(storage, stripe_quantity)
    cancel_at_period_end = bool(storage.get("cancel_at_period_end", False))
    _, storage_current_period_end = extract_subscription_period(stripe_storage_subscription)

    # If user sets a positive target while cancellation is pending, treat it as resume.
    if target_quantity_gb > 0 and cancel_at_period_end:
        try:
            await _set_storage_cancel_at_period_end_async(tenant_id, value=False)
            storage = StorageSubscriptionService.get_by_tenant_id(tenant_id) or storage
        except Exception as e:
            logging.warning(f"Failed to resume storage subscription for tenant {tenant_id}: {e}")

    if target_quantity_gb == effective_quantity_gb and not cancel_at_period_end:
        return True, {
            "effective_quantity_gb": effective_quantity_gb,
            "target_quantity_gb": effective_quantity_gb,
            "pending": False,
            "message": "Storage target is unchanged.",
        }

    if target_quantity_gb == 0:
        ok, data = await _set_storage_cancel_at_period_end_async(tenant_id, value=True)
        if not ok:
            return False, {"error": "Failed to schedule storage cancellation."}
        return True, {
            "scheduled_cancel": True,
            "effective_quantity_gb": effective_quantity_gb,
            "target_quantity_gb": 0,
            "pending": True,
            **data,
        }

    if target_quantity_gb > effective_quantity_gb:
        # Clear any schedule first; increasing should take effect immediately after successful payment.
        try:
            await cancel_scheduled_subscription_change_async(storage_subscription_id)
        except Exception:
            pass
        updated = await stripe.Subscription.modify_async(
            storage_subscription_id,
            items=[{"id": item_id, "quantity": target_quantity_gb}],
            proration_behavior="always_invoice",
            payment_behavior="pending_if_incomplete",
            billing_cycle_anchor="unchanged",
            expand=["latest_invoice"],
        )
        pending_update = updated.get("pending_update") if isinstance(updated, dict) else getattr(updated, "pending_update", None)
        invoice_paid, invoice_id, invoice_status, invoice_url = await is_subscription_latest_invoice_paid_async(updated)
        payment_pending = bool(pending_update) or not invoice_paid
        _sync_storage_subscription_record(
            tenant_id,
            updated,
            target_quantity_gb=target_quantity_gb,
            clear_pending=not payment_pending,
            pending_quantity_gb=target_quantity_gb if payment_pending else None,
            pending_action="increase" if payment_pending else "",
            pending_effective_at=storage_current_period_end if payment_pending else None,
        )
        return True, {
            "pending_payment": payment_pending,
            "effective_quantity_gb": effective_quantity_gb if payment_pending else target_quantity_gb,
            "target_quantity_gb": target_quantity_gb,
            "pending": payment_pending,
            "invoice_id": invoice_id,
            "invoice_status": invoice_status,
            "redirect_to": invoice_url if payment_pending and invoice_url else "",
        }

    # Decrease path: schedule at period end.
    scheduled = await schedule_subscription_quantity_change_at_period_end_async(storage_subscription_id, target_quantity_gb)
    if not scheduled:
        return False, {"error": "Failed to schedule storage quantity decrease."}
    latest_storage_sub = await stripe.Subscription.retrieve_async(storage_subscription_id)
    _sync_storage_subscription_record(
        tenant_id,
        latest_storage_sub,
        target_quantity_gb=target_quantity_gb,
        clear_pending=False,
        pending_quantity_gb=target_quantity_gb,
        pending_action="decrease",
        pending_effective_at=scheduled.get("effective_at"),
        schedule_id=scheduled.get("schedule_id", ""),
    )
    return True, {
        "scheduled_change": scheduled,
        "effective_quantity_gb": effective_quantity_gb,
        "target_quantity_gb": target_quantity_gb,
        "pending": True,
    }


@manager.route("/current_plan", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_current_plan():
    tenant_plan = SubscriptionService.get_by_tenant_id(current_user.id)
    customer_id = tenant_plan.get("customer_id")
    if not customer_id:
        logging.warning("No customer_id found while checkout, it was expected create when user registion, try to create a stripe accout to proceed...")
        customer_id = await billing_set_customer_id_async(current_user.id)
        tenant_plan["customer_id"] = customer_id

    subscription_id = (tenant_plan.get("subscription_id") or "").strip()
    if subscription_id:
        try:
            tenant_plan["pending_subscription_change"] = await get_pending_subscription_change_async(subscription_id)
        except Exception as e:
            logging.warning(f"Failed to fetch pending subscription change for tenant {current_user.id}: {e}")
    return get_json_result(data=tenant_plan)


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
    storage = StorageSubscriptionService.get_by_tenant_id(tenant_id) or {}
    storage = await _hydrate_storage_period_fields_if_missing(tenant_id, storage)
    storage_current_period_start = to_utc_datetime(storage.get("current_period_start")) or to_utc_datetime(tenant_plan.get("start_time"))
    storage_current_period_end = to_utc_datetime(storage.get("current_period_end")) or to_utc_datetime(tenant_plan.get("end_time"))
    decrease_effective_at = to_utc_datetime(storage.get("pending_effective_at")) or storage_current_period_end
    unit_price = await _get_storage_unit_price_async(storage.get("price_id", ""))
    data = {
        "tenant_id": tenant_id,
        "plan_name": tenant_plan.get("plan_name", ""),
        "trial_forbidden": is_trial_plan_name(tenant_plan.get("plan_name", "")),
        "unit_price": unit_price,
        "effective_quantity_gb": safe_int(storage.get("effective_quantity_gb"), 0),
        "target_quantity_gb": safe_int(storage.get("target_quantity_gb"), 0),
        "pending_quantity_gb": storage.get("pending_quantity_gb"),
        "pending_action": storage.get("pending_action", ""),
        "pending_effective_at": to_utc_datetime(storage.get("pending_effective_at")),
        "decrease_effective_at": decrease_effective_at,
        "subscription_id": storage.get("subscription_id", ""),
        "price_id": storage.get("price_id", ""),
        "schedule_id": storage.get("schedule_id", ""),
        "status": storage.get("status", ""),
        "cancel_at_period_end": bool(storage.get("cancel_at_period_end", False)),
        "current_period_start": storage_current_period_start,
        "current_period_end": storage_current_period_end,
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

    target_quantity = req.get("target_quantity_gb")
    try:
        target_quantity_gb = int(target_quantity)
    except (TypeError, ValueError):
        return get_json_result(data=False, message="target_quantity_gb must be an integer.", code=RetCode.BAD_REQUEST)
    if target_quantity_gb < 0:
        return get_json_result(data=False, message="target_quantity_gb must be >= 0.", code=RetCode.BAD_REQUEST)

    ok, data = await _set_storage_target_quantity_async(
        tenant_id,
        target_quantity_gb,
        session_success_url=req.get("session_success_url", settings.BILLING["session_success_url"]),
        session_cancel_url=req.get("session_cancel_url", settings.BILLING["session_cancel_url"]),
    )
    if not ok:
        # When there is a pending increase that the user can abandon, include
        # structured context so the frontend can offer "Pay Now" / "Abandon" actions.
        if data.get("can_abandon"):
            return get_json_result(
                code=RetCode.DATA_ERROR,
                message=data.get("error", "Failed to set storage target."),
                data=data,
            )
        return get_data_error_result(message=data.get("error", "Failed to set storage target."))
    return get_json_result(data=data)


@manager.route("/storage/abandon-pending", methods=["POST"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_storage_abandon_pending():
    req = await get_request_json()
    tenant_id = req.get("tenant_id", current_user.id)
    if current_user.id != tenant_id:
        return get_json_result(
            data=False,
            message="No authorization.",
            code=RetCode.AUTHENTICATION_ERROR,
        )

    ok, data = await _abandon_storage_pending_increase_async(tenant_id)
    if not ok:
        return get_data_error_result(message=data.get("error", "Failed to abandon pending increase."))
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
    try:
        tenant_id = request.args.get("tenant_id", current_user.id)

        tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id, require_quota_info=True)
        storage_used_kb = tenant_plan.get("num_kb_storage", 0) or 0
        storage_limit_kb = tenant_plan.get("quota_kb_storage", 0) or 0

        # Extract the relevant information for the overview
        plan_overview = {
            "plan_name": tenant_plan.get("plan_name", "unknown"),
            "subscription_status": tenant_plan.get("subscription_status", ""),
            "billing_cycle": {
                "start": to_utc_date_str(tenant_plan.get("start_time")),
                "end": to_utc_date_str(tenant_plan.get("end_time")),
            },
            "resources": {
                "plan_storage": {
                    "used": storage_used_kb,
                    "limit": storage_limit_kb,
                    "unit": "KB",
                },
                "add_on_storage": {
                    "used": 0,
                    "limit": 0,
                    "unit": "KB",
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
                "requests_per_minute": _get_api_request_limit_by_plan(tenant_plan.get("plan_name", BILLING_PLAN_TRIAL_NAME), limit_type="minute"),
                "requests_per_month": _get_api_request_limit_by_plan(tenant_plan.get("plan_name", BILLING_PLAN_TRIAL_NAME), limit_type="month"),
            },
        }

        add_on_storage_kb = _storage_effective_kb(tenant_id)
        if add_on_storage_kb > 0:
            plan_overview["resources"]["add_on_storage"]["limit"] = add_on_storage_kb

        total_storage_limit_kb = storage_limit_kb + add_on_storage_kb
        if storage_used_kb > storage_limit_kb:
            plan_overview["resources"]["plan_storage"]["used"] = storage_limit_kb
            plan_overview["resources"]["add_on_storage"]["used"] = min(storage_used_kb - storage_limit_kb, max(add_on_storage_kb, 0))
        elif total_storage_limit_kb <= 0:
            plan_overview["resources"]["add_on_storage"]["used"] = 0

        return get_json_result(data=plan_overview)
    except Exception as e:
        return server_error_response(e)


def _get_api_request_limit_by_plan(plan_name: str, limit_type: str = "month") -> int:
    """
    Get API request limits based on plan type.

    Args:
        plan_name: Name of the plan (e.g., "Trial", "Starter", "Pro", "Enterprise")
        limit_type: Type of limit ("minute" or "month")

    Returns:
        Request limit as integer
    """
    key = (plan_name or "").strip()
    if not key:
        return LIMITED_API_REQUESTS

    info = settings.BILLING_PLAN_TO_INFO.get(key) or settings.BILLING_PLAN_TO_INFO.get(key.title()) or {}
    if not info:
        info = settings.BILLING_PLAN_TO_INFO.get(BILLING_PLAN_TRIAL_NAME) or {}
    if limit_type == "minute":
        value = info.get("api_request_limit_per_minute")
    else:  # "month"
        value = info.get("api_request_limit_per_month")

    if not value:
        return LIMITED_API_REQUESTS
    try:
        return int(value)
    except (TypeError, ValueError):
        return LIMITED_API_REQUESTS


@manager.route("/usage_based_overview", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_usage_based_overview():
    """
    Get a comprehensive overview of usage-based products including:
    - DeepDoc page usage and limits
    - Storage usage (if implemented as usage-based)
    - Token usage and limits
    """
    try:
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
    except Exception as e:
        return server_error_response(e)


@manager.route("/points/checkout", methods=["POST"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_points_checkout():
    """Create a Stripe Checkout session for purchasing points."""
    try:
        req = await get_request_json()
        tenant_id = req.get("tenant_id", current_user.id)
        if current_user.id != tenant_id:
            return get_json_result(data=False, message="No authorization.", code=RetCode.AUTHENTICATION_ERROR)

        points = req.get("points")
        try:
            points = int(points)
        except (TypeError, ValueError):
            return get_json_result(data=False, message="points must be an integer.", code=RetCode.BAD_REQUEST)
        if points <= 0:
            return get_json_result(data=False, message="points must be positive.", code=RetCode.BAD_REQUEST)

        recharge_config = settings.BILLING.get("points_recharge") or {}
        price_id = (recharge_config.get("price_id") or "").strip()
        points_per_unit = int(recharge_config.get("points_per_unit") or 100)
        if not price_id or price_id == "price_xxx":
            return get_json_result(data=False, message="Points recharge is not configured.", code=RetCode.SERVER_ERROR)
        if points % points_per_unit != 0:
            return get_json_result(
                data=False,
                message=f"points must be a multiple of {points_per_unit}.",
                code=RetCode.BAD_REQUEST,
            )

        quantity = points // points_per_unit
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
            success_url=settings.BILLING.get("session_success_url", ""),
            cancel_url=settings.BILLING.get("session_cancel_url", ""),
        )
        return get_json_result(data={"checkout_url": session.url})
    except Exception as e:
        return server_error_response(e)


@manager.route("/deepdoc/usage", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_deepdoc_usage():
    """Return DeepDoc usage summary derived from the point ledger."""
    try:
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
    except Exception as e:
        return server_error_response(e)


@manager.route("/points/balance", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_points_balance():
    """Return the current point balance for the authenticated tenant."""
    try:
        tenant_id = request.args.get("tenant_id", current_user.id)
        if current_user.id != tenant_id:
            return get_json_result(data=False, message="No authorization.", code=RetCode.AUTHENTICATION_ERROR)
        balance = PointAccountService.get_balance(tenant_id)
        return get_json_result(data=balance)
    except Exception as e:
        return server_error_response(e)


@manager.route("/points/ledger", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_points_ledger():
    """Return paginated point ledger entries for the authenticated tenant."""
    try:
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
    except Exception as e:
        return server_error_response(e)


@manager.route("/points/holds", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_points_holds():
    """Return paginated point hold records for the authenticated tenant."""
    try:
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
    except Exception as e:
        return server_error_response(e)


@manager.route("/spend_overview", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_spend_overview():
    try:
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
        status_map = {
            PaymentStatus.SUCCESS.value: "paid",
            PaymentStatus.FAILED.value: "unpaid",
            PaymentStatus.PENDING.value: "pending",
        }

        spend_overview: list[dict] = []
        with DB.connection_context():
            query = PaymentOrder.select(
                PaymentOrder.order_id,
                PaymentOrder.amount_cents,
                PaymentOrder.currency,
                PaymentOrder.payment_status,
                PaymentOrder.order_created_at,
                PaymentOrder.receipt_url,
                PaymentOrder.receipt_pdf_url,
            ).where(PaymentOrder.tenant_id == tenant_id)

            if start_dt:
                query = query.where(PaymentOrder.order_created_at >= start_dt)
            if end_dt:
                query = query.where(PaymentOrder.order_created_at <= end_dt)

            query = query.order_by(PaymentOrder.order_created_at.desc())

            for order in query:
                created_at = to_utc_datetime(order.order_created_at)
                spend_overview.append(
                    {
                        "invoice_id": order.order_id,
                        "amount": float((order.amount_cents or 0) / 100),
                        "currency": (order.currency or "").upper() if order.currency else None,
                        "status": status_map.get(order.payment_status, "pending"),
                        "created_at": int(created_at.timestamp()) if created_at else None,
                        "hosted_invoice_url": order.receipt_url,
                        "invoice_pdf_url": order.receipt_pdf_url or order.receipt_url,
                    }
                )

        return get_json_result(data=spend_overview)

    except Exception as e:
        return server_error_response(e)


@manager.route("/spend_metrics", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_spend_metrics():
    try:
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
            local_price = LocalPriceService.get_by_name(product_name) or {}
            unit = price_point.get("unit") or ""
            unit_quantity = price_point.get("unit_quantity") or 0
            amount_cents_val = local_price.get("amount_cents")
            price_currency = local_price.get("currency")
            pricing = {
                "unit": unit,
                "unit_quantity": unit_quantity,
                "price_amount": Decimal(str(amount_cents_val)) / 100 if amount_cents_val is not None else None,
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
                    PaymentOrder.product_name,
                    PaymentOrder.amount_cents,
                    PaymentOrder.currency,
                    PaymentOrder.order_created_at,
                    PaymentOrder.payment_detail,
                )
                .where(
                    PaymentOrder.tenant_id == tenant_id,
                    PaymentOrder.payment_type == PriceType.USAGE_BASED,
                    PaymentOrder.paid,
                )
                .order_by(PaymentOrder.order_created_at.asc())
            )
            if start_dt:
                query = query.where(PaymentOrder.order_created_at >= start_dt)
            if end_dt:
                query = query.where(PaymentOrder.order_created_at <= end_dt)

            for order in query:
                order_amount = Decimal(str(order.amount_cents or 0)) / 100
                order_currency = order.currency
                if currency is None:
                    currency = order_currency
                total_spend += order_amount
                date_key = to_utc_date_str(order.order_created_at)
                series_map[date_key] = series_map.get(date_key, Decimal("0")) + order_amount

                product_name = order.product_name or "unknown"
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
                category["total_spend"] += order_amount
                category["series_map"][date_key] = category["series_map"].get(date_key, Decimal("0")) + order_amount
                quantity = _detail_quantity(order.payment_detail)
                if quantity is None:
                    quantity = _estimate_quantity(order_amount, product_name, order_currency)
                if quantity:
                    category["total_quantity"] += quantity
                    category["quantity_series_map"][date_key] = category["quantity_series_map"].get(date_key, Decimal("0")) + quantity

        date_keys = []
        if start_dt and end_dt:
            date_keys = build_date_keys(start_dt, end_dt)

        if date_keys:
            series = [{"date": d, "spend": amount_to_float(series_map.get(d, Decimal("0")))} for d in date_keys]
        else:
            series = [{"date": d, "spend": amount_to_float(v)} for d, v in sorted(series_map.items())]

        categories = []
        for category in category_map.values():
            series_map = category.pop("series_map")
            quantity_series_map = category.pop("quantity_series_map")
            if date_keys:
                category_series = [{"date": d, "spend": amount_to_float(series_map.get(d, Decimal("0")))} for d in date_keys]
                quantity_series = [{"date": d, "quantity": amount_to_float(quantity_series_map.get(d, Decimal("0")))} for d in date_keys]
            else:
                category_series = [{"date": d, "spend": amount_to_float(v)} for d, v in sorted(series_map.items())]
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
    except Exception as e:
        return server_error_response(e)


@manager.route("/plans", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_all_plans():
    cached_plans = REDIS_CONN.get(PLANS_CACHE_KEY)
    if cached_plans:
        try:
            return get_json_result(data=json.loads(cached_plans))
        except json.JSONDecodeError:
            logging.warning("Failed to decode cached billing plans, rebuilding cache.")

    price_lookup_keys = {}
    for plan_name, info in settings.BILLING_PLAN_TO_INFO.items():
        lookup_key = info.get("price_lookup_key", "")
        if lookup_key:
            price_lookup_keys[plan_name] = lookup_key

    price_dict = {}
    if price_lookup_keys and settings.BILLING_ENABLED:
        try:
            prices = await stripe.Price.list_async(lookup_keys=list(price_lookup_keys.values()), limit=len(price_lookup_keys))
            lookup_price_map = {}
            for price in prices.data:
                lookup_key = getattr(price, "lookup_key", None)
                unit_amount = getattr(price, "unit_amount", None)
                if lookup_key:
                    lookup_price_map[lookup_key] = unit_amount
            for plan_name, lookup_key in price_lookup_keys.items():
                unit_amount = lookup_price_map.get(lookup_key)
                price_dict[plan_name] = unit_amount / 100 if unit_amount else -1
        except Exception as e:
            logging.warning(f"Failed to fetch Stripe prices by lookup_key: {e}")
    if BILLING_PLAN_TRIAL_NAME not in price_dict:
        price_dict[BILLING_PLAN_TRIAL_NAME] = 0

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
        p = {
            "id": plan.id,
            "name": plan.name,
            "price": price_dict.get(plan.name, -1),
            "description": plan.description,
            "price_ids": plan.price_ids,
            "feature": {
                "quota_apps": plan.quota_apps,
                "quota_members": plan.quota_members,
                "quota_kb_storage": plan.quota_kb_storage,
                "quota_api_limits": _get_api_request_limit_by_plan(plan.name, limit_type="month"),
            },
        }
        plans.append(p)

    REDIS_CONN.set_obj(PLANS_CACHE_KEY, plans, PLANS_CACHE_TTL_SECONDS)
    return get_json_result(data=plans)


@manager.route("/usage_based_plans", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_all_usage_based_plans():
    cached_plans = REDIS_CONN.get(USAGE_BASED_PLANS_CACHE_KEY)
    if cached_plans:
        try:
            return get_json_result(data=json.loads(cached_plans))
        except json.JSONDecodeError:
            logging.warning("Failed to decode cached usage-based plans, rebuilding cache.")

    latest_products = ProductService.get_latest_by_type(ProductType.USAGE_BASED)
    latest_products = list(latest_products)

    price_lookup_keys = {}
    for product in latest_products:
        lookup_key = settings.BILLING_PLAN_TO_INFO.get(product.name, {}).get("price_lookup_key", "")
        if lookup_key:
            price_lookup_keys[product.name] = lookup_key

    price_dict = {}
    if price_lookup_keys and settings.BILLING_ENABLED:
        try:
            prices = await stripe.Price.list_async(lookup_keys=list(price_lookup_keys.values()), limit=len(price_lookup_keys))
            lookup_price_map = {}
            for price in prices.data:
                lookup_key = getattr(price, "lookup_key", None)
                unit_amount = getattr(price, "unit_amount", None)
                if lookup_key:
                    lookup_price_map[lookup_key] = unit_amount
            for plan_name, lookup_key in price_lookup_keys.items():
                unit_amount = lookup_price_map.get(lookup_key)
                price_dict[plan_name] = unit_amount / 100 if unit_amount else -1
        except Exception as e:
            logging.warning(f"Failed to fetch Stripe prices by lookup_key: {e}")
    usage_based_plans = []
    latest_products.sort(key=lambda product: product.name)
    for product in latest_products:
        usage_based_plans.append(
            {
                "id": product.id,
                "name": product.name,
                "price": price_dict.get(product.name, -1),
                "description": product.description,
                "price_ids": product.price_ids,
                "usage_stat_type": product.usage_stat_type,
            }
        )

    REDIS_CONN.set_obj(USAGE_BASED_PLANS_CACHE_KEY, usage_based_plans, PLANS_CACHE_TTL_SECONDS)
    return get_json_result(data=usage_based_plans)


@manager.route("/upcoming", methods=["POST"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_upcoming():
    req = await get_request_json()

    new_price_id = req.get("new_price_id")
    tenant_id = req.get("tenant_id") or current_user.id

    current_plan = SubscriptionService.get_by_tenant_id(tenant_id=tenant_id)
    customer_id = req.get("customer_id") or current_plan.get("customer_id")
    subscription_id = current_plan.get("subscription_id")

    print("@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@", flush=True)
    print(f"{customer_id=}, {subscription_id=}, {new_price_id=}", flush=True)
    print("@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@", flush=True)
    if not new_price_id:
        return get_data_error_result(message="Missing required parameters")
    if not customer_id:
        customer_id = await billing_set_customer_id_async(tenant_id)
    if not customer_id:
        return get_data_error_result(message="Missing required parameters")

    try:
        if subscription_id:
            sub = stripe.Subscription.retrieve(subscription_id)
            if not sub or not sub["items"] or not sub["items"]["data"]:
                return get_data_error_result(message="Subscription items not found")

            old_item_id = sub["items"]["data"][0]["id"]  # assumming there is only one item
            upcoming_invoice = stripe.Invoice.create_preview(
                customer=customer_id,
                subscription=subscription_id,
                subscription_details={
                    "proration_behavior": "always_invoice",
                    "items": [
                        {
                            "id": old_item_id,
                            "price": new_price_id,
                            "quantity": 1,
                        }
                    ],
                },
            )
        else:
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

    except Exception as e:
        return server_error_response(e)


@manager.route("/checkout", methods=["POST"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_checkout():
    """
    https://docs.stripe.com/payments/accept-a-payment

    Arguments:
        tenant_id:
        price_id:
        payment_type: subscription, usage_based
    """
    req = await get_request_json()
    tenant_id = req.get("tenant_id")
    # price_id = req.get("price_id")
    usage_based_price_id = req.get("usage_based_price_id")
    subscription_price_id = req.get("subscription_price_id")
    quantity = req.get("quantity", 1)
    payment_type = req.get("payment_type")
    expiry_time = req.get("expiry_time")
    session_success_url = req.get("session_success_url", settings.BILLING["session_success_url"])
    session_cancel_url = req.get("session_cancel_url", settings.BILLING["session_cancel_url"])
    if not tenant_id or not payment_type:
        return get_json_result(
            data=False,
            message="Missing required parameters tenant_id and payment_type.",
            code=RetCode.BAD_REQUEST,
        )
    try:
        quantity = int(quantity)
    except (TypeError, ValueError):
        return get_json_result(
            data=False,
            message="Invalid quantity.",
            code=RetCode.BAD_REQUEST,
        )
    if quantity < 0:
        return get_json_result(
            data=False,
            message="Quantity must be a non-negative integer.",
            code=RetCode.BAD_REQUEST,
        )
    if current_user.id != tenant_id:
        return get_json_result(
            data=False,
            message="No authorization.",
            code=RetCode.AUTHENTICATION_ERROR,
        )

    logging.info(f"{payment_type=}")
    if payment_type not in (PriceType.SUBSCRIPTION, PriceType.USAGE_BASED):
        return get_data_error_result(message="Unsupported payment type.")
    if payment_type == PriceType.SUBSCRIPTION and not subscription_price_id:
        return get_json_result(
            data=False,
            message="Missing required parameters subscription_price_id.",
            code=RetCode.BAD_REQUEST,
        )
    if payment_type == PriceType.USAGE_BASED and not usage_based_price_id:
        return get_json_result(
            data=False,
            message="Missing required parameters usage_based_price_id.",
            code=RetCode.BAD_REQUEST,
        )

    if payment_type == PriceType.SUBSCRIPTION and quantity <= 0:
        return get_json_result(
            data=False,
            message="Quantity must be a positive integer.",
            code=RetCode.BAD_REQUEST,
        )

    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id)
    customer_id = tenant_plan.get("customer_id")
    if not customer_id:
        logging.warning("No customer_id found while checkout, it was expected create when user registion, try to create a stripe accout to proceed...")
        customer_id = await billing_set_customer_id_async(tenant_id)

    try:
        if payment_type == PriceType.SUBSCRIPTION:
            subscription_id = tenant_plan.get("subscription_id")
            subscription_status = tenant_plan.get("subscription_status")
            # Stripe has built-in retry logic and will automatically retry deductions after a deduction failure. During the retry period, the subscription status may still be active and will only change to past_due after all retry attempts fail.
            if subscription_status == SubscriptionStatus.ACTIVE:
                if subscription_id:
                    # https://docs.stripe.com/api/subscriptions/update
                    subscription = await stripe.Subscription.retrieve_async(subscription_id)
                    subscription_items = subscription["items"]["data"]
                    current_price_id = subscription_items[0]["price"]["id"] if subscription_items else ""

                    if any(item["price"]["id"] == subscription_price_id for item in subscription_items):
                        msg = f"Tenant {tenant_id} already has an active subscription {subscription_id} on price {subscription_price_id}"
                        return get_json_result(data=False, message=msg, code=RetCode.SUCCESS)

                    if current_price_id and is_downgrade_by_price_id(current_price_id, subscription_price_id):
                        target_plan_name_for_downgrade = settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, "")
                        if is_trial_plan_name(target_plan_name_for_downgrade):
                            ok, _ = await _set_storage_cancel_at_period_end_async(tenant_id, value=True)
                            if not ok:
                                return get_data_error_result(message="Failed to auto-cancel storage for Trial downgrade.")
                        scheduled = await schedule_subscription_price_change_at_period_end_async(subscription_id, subscription_price_id)
                        if not scheduled:
                            return get_data_error_result(message="Failed to schedule plan downgrade.")
                        msg = f"Tenant {tenant_id} scheduled a plan downgrade at period end."
                        return get_json_result(data={"scheduled_change": scheduled}, message=msg, code=RetCode.SUCCESS)

                    trial_price_id = get_trial_price_id(settings.BILLING.get("billing_plans", []))
                    trial_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(trial_price_id, "") or BILLING_PLAN_TRIAL_NAME
                    current_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(current_price_id, "") or tenant_plan.get("plan_name", "")
                    target_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, "")
                    if current_plan_name == trial_plan_name and target_plan_name and target_plan_name != trial_plan_name:
                        try:
                            await cancel_scheduled_subscription_change_async(subscription_id)
                        except Exception as e:
                            logging.info(f"Skip cancelling scheduled subscription change for {subscription_id}: {e}")

                        session = await stripe.checkout.Session.create_async(
                            customer=customer_id,
                            client_reference_id=f"order_{uuid.uuid4()}",
                            line_items=[{"price": subscription_price_id, "quantity": quantity}],
                            mode=PriceType.SUBSCRIPTION,
                            success_url=session_success_url,
                            cancel_url=session_cancel_url,
                            metadata={
                                "price_type": PriceType.SUBSCRIPTION,
                                "tenant_id": tenant_id,
                                "price_id": subscription_price_id,
                                "product_name": target_plan_name,
                                "previous_subscription_id": subscription_id,
                            },
                            subscription_data={
                                "metadata": {
                                    "price_type": PriceType.SUBSCRIPTION,
                                    "tenant_id": tenant_id,
                                    "price_id": subscription_price_id,
                                    "product_name": target_plan_name,
                                    "previous_subscription_id": subscription_id,
                                },
                            },
                        )
                        logging.info(f"created stripe session id {session.id}, url: {session.url}")
                        return get_json_result(data={"redirect_to": session.url})

                    try:
                        await cancel_scheduled_subscription_change_async(subscription_id)
                    except Exception as e:
                        logging.info(f"Skip cancelling scheduled subscription change for {subscription_id}: {e}")

                    current_plan_name = tenant_plan.get("plan_name", "")
                    customer_portal_url = _create_customer_portal(tenant_id, current_plan_name, return_url=session_cancel_url)
                    msg = f"Tenant {tenant_id} already has an active subscription {subscription_id}, change plan on customer portal {customer_portal_url}."
                    return get_json_result(
                        data={"redirect_to": f"{customer_portal_url}/subscriptions/{subscription_id}/update"},
                        message=msg,
                        code=RetCode.SUCCESS,
                    )
            else:
                # NO subscription yet
                logging.info(f"found customer {customer_id} for tenant {tenant_id}")

            is_inactive = subscription_status == SubscriptionStatus.INACTIVE
            trail_price_id = get_trial_price_id(settings.BILLING.get("billing_plans", []))
            is_trail_plan = subscription_price_id == trail_price_id
            print(f"{trail_price_id=}")

            print(f"\n create subscription {subscription_price_id}: {settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, '')}")

            session_params = {
                "customer": customer_id,
                "client_reference_id": f"order_{uuid.uuid4()}",
                "line_items": [
                    {
                        "price": subscription_price_id,
                        "quantity": quantity,
                    }
                ],
                # automatic_tax={"enabled": True},  # need valid address
                # phone_number_collection={"enabled": True},
                "mode": PriceType.SUBSCRIPTION,
                "success_url": session_success_url,
                "cancel_url": session_cancel_url,
                "metadata": {
                    "price_type": PriceType.SUBSCRIPTION,
                    "tenant_id": tenant_id,
                    "price_id": subscription_price_id,
                    "product_name": settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, ""),
                },
                "subscription_data": {
                    "metadata": {
                        "price_type": PriceType.SUBSCRIPTION,
                        "tenant_id": tenant_id,
                        "price_id": subscription_price_id,
                        "product_name": settings.BILLING_PRICEID_TO_PRODUCT.get(subscription_price_id, ""),
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

            print(f"{session_params}")
            session = await stripe.checkout.Session.create_async(**session_params)
            logging.info(f"created stripe session id {session.id}, url: {session.url}")
            return get_json_result(data={"redirect_to": session.url})

        elif payment_type == PriceType.USAGE_BASED:
            logging.info("ENTERING PAYMENT SECTION")
            usage_product_name = settings.BILLING_PRICEID_TO_PRODUCT.get(usage_based_price_id, "")

            if is_storage_price_id(usage_based_price_id):
                return get_data_error_result(message="Storage add-on checkout moved to /billing/storage/set-target.")

            if quantity <= 0:
                return get_json_result(
                    data=False,
                    message="Quantity must be a positive integer.",
                    code=RetCode.BAD_REQUEST,
                )

            usage_metadata = {
                "price_type": PriceType.USAGE_BASED,
                "tenant_id": tenant_id,
                "price_id": usage_based_price_id,
                "product_name": usage_product_name,
                "quantity": quantity,
            }
            # Storage add-on is sold in GB (Stripe quantity), but our internal quota unit is KB.
            # Keep the codebase/storage accounting consistent in KB by converting at the webhook ingestion layer.
            if "storage" in (usage_metadata["product_name"] or "").lower():
                usage_metadata["quantity_unit"] = "GB"
            if expiry_time:
                usage_metadata["expiry_time"] = expiry_time

            session = await stripe.checkout.Session.create_async(
                customer=customer_id,
                client_reference_id=f"order_{uuid.uuid4()}",
                line_items=[
                    {
                        # TODO: just for testing
                        "price": usage_based_price_id,
                        "quantity": quantity,
                    }
                ],
                # automatic_tax={"enabled": True},
                # phone_number_collection={"enabled": True},
                mode="payment",
                success_url=session_success_url,
                cancel_url=session_cancel_url,
                payment_intent_data={"metadata": usage_metadata},
            )
            logging.info(f"created stripe session id {session.id}, url: {session.url}")
            return get_json_result(data={"redirect_to": session.url})

    except Exception as e:
        return server_error_response(e)


@manager.route("/cancel-scheduled-subscription-change", methods=["POST"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_cancel_scheduled_subscription_change():
    req = await get_request_json()
    tenant_id = req.get("tenant_id")
    if not tenant_id:
        return get_json_result(
            data=False,
            message="Missing required parameters tenant_id.",
            code=RetCode.BAD_REQUEST,
        )
    if current_user.id != tenant_id:
        return get_json_result(
            data=False,
            message="No authorization.",
            code=RetCode.AUTHENTICATION_ERROR,
        )

    tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id)
    subscription_id = (tenant_plan.get("subscription_id") or "").strip()
    if not subscription_id:
        return get_json_result(
            data=False,
            message="Subscription not found.",
            code=RetCode.BAD_REQUEST,
        )

    try:
        canceled = await cancel_scheduled_subscription_change_async(subscription_id)
        return get_json_result(
            data={"canceled": bool(canceled)},
            message="Scheduled subscription change canceled." if canceled else "No scheduled subscription change found.",
            code=RetCode.SUCCESS,
        )
    except Exception as e:
        return server_error_response(e)


@manager.route("/unsubscribe", methods=["POST"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_unsubscribe():
    req = await request.json
    tenant_id = req.get("tenant_id")
    # https://docs.stripe.com/api/subscriptions/cancel
    # Possible enum values of feedback: customer_service, low_quality, missing_features, other, switched_service, too_complex, too_expensive, unused
    feedback = req.get("feedback")
    comment = req.get("comment")
    cancel_at_period_end = req.get("cancel_at_period_end")
    if not tenant_id:
        return get_json_result(
            data=False,
            message="Missing required parameters tenant_id and price_id.",
            code=RetCode.BAD_REQUEST,
        )
    if current_user.id != tenant_id:
        return get_json_result(
            data=False,
            message="No authorization.",
            code=RetCode.AUTHENTICATION_ERROR,
        )
    try:
        tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id)
        subscription_id = tenant_plan.get("subscription_id")
        if not subscription_id:
            msg = f"Tenant {tenant_id} has no subscription."
            logging.info(msg)
            return get_json_result(data=False, message=msg, code=RetCode.SUCCESS)
        if cancel_at_period_end == "yes":
            _ = stripe.Subscription.modify(subscription_id, cancel_at_period_end=True)
            msg = f"Tenant {tenant_id} subscription {subscription_id} will be cancelled at the end of the current period."
        else:
            _ = stripe.Subscription.delete(subscription_id, cancellation_details={"comment": comment, "feedback": feedback}, prorate=True)
            msg = f"Tenant {tenant_id} subscription {subscription_id} has been cancelled."
        return get_json_result(data=False, message=msg, code=RetCode.SUCCESS)
    except Exception as e:
        return server_error_response(e)


def _create_customer_portal(tenant_id: str, current_plan_name: str, return_url: str) -> str:
    subscription = SubscriptionService.get_by_tenant_id(tenant_id)
    if not subscription:
        return return_url

    customer_id = subscription.get("customer_id", "").strip()
    subscription_id = subscription.get("subscription_id", "").strip()
    # current_price_id = subscription.get("price_id", "").strip()

    if not customer_id or not subscription_id:
        return return_url

    try:
        advancer_plans = get_plans_equal_or_higher(current_plan_name)
        advancer_price_ids = list({price_id for _, price_ids in advancer_plans for price_id in price_ids})

        price_to_product = get_product_ids_for_prices(advancer_price_ids)
        product_id_to_prices: dict[str, list[str]] = {}
        for price_id, product_id in price_to_product.items():
            product_id_to_prices.setdefault(product_id, []).append(price_id)

        configuration = create_or_get_portal_configuration(product_id_to_prices)
        print(f"{configuration=}")

        portal_session = stripe.billing_portal.Session.create(
            customer=customer_id,
            return_url=return_url,
            configuration=configuration.id,
        )
        return portal_session.url

    except stripe.StripeError as e:
        logging.error(f"Stripe API error: {e}")
        return return_url
    except Exception as e:
        logging.error(f"Unexpected error: {e}")
        return return_url


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
    current_plan_name = subscription.get("plan_name", "")
    if not subscription:
        return get_data_error_result("Subscription not found.")
    if not current_plan_name:
        return get_data_error_result("Current plan not found.")

    customer_id = subscription.get("customer_id", "").strip()
    subscription_id = subscription.get("subscription_id", "").strip()
    # current_price_id = subscription.get("price_id", "").strip()
    if not customer_id or not subscription_id:
        return redirect(return_url, code=303)

    try:
        advancer_plans = get_plans_equal_or_higher(current_plan_name)
        advancer_price_ids = list({price_id for _, price_ids in advancer_plans for price_id in price_ids})

        price_to_product = get_product_ids_for_prices(advancer_price_ids)
        product_id_to_prices: dict[str, list[str]] = {}
        for price_id, product_id in price_to_product.items():
            product_id_to_prices.setdefault(product_id, []).append(price_id)

        configuration = create_or_get_portal_configuration(product_id_to_prices)
        print(f"{configuration=}")

        portal_session = stripe.billing_portal.Session.create(
            customer=customer_id,
            return_url=return_url,
            configuration=configuration.id,
        )
        return redirect(portal_session.url, code=303)

    except stripe.StripeError as e:
        logging.error(f"Stripe API error: {e}")
        return get_data_error_result("Failed to create billing portal session.")
    except Exception as e:
        logging.error(f"Unexpected error: {e}")
        return redirect(return_url, code=303)


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
        return jsonify(success=False)
    if settings.BILLING["stripe_endpoint_secret"]:
        # Only verify the event if there is an endpoint secret defined
        # Otherwise use the basic event deserialized with json
        sig_header = request.headers.get("stripe-signature")
        try:
            event = stripe.Webhook.construct_event(payload, sig_header, settings.BILLING["stripe_endpoint_secret"])
        except stripe.SignatureVerificationError:
            logging.exception("billing_webhook signature verification failed.")
            return jsonify(success=False)

    # Handle the event
    event_type = event["type"]
    if event_type in FOCUSED_STRIPE_WEBHOOK:
        print(f"Passed in {event_type} {event=}")
        _handle_event(event)
        return jsonify(success=True)
    return jsonify(success=True)


@billing_enabled_guard(None)
def _handle_event(event):
    event_handlers = {
        "payment_intent.succeeded": _handle_payment_intent_succeeded,  # one-off
        "invoice.payment_failed": _handle_invoice_payment_failed,  # subscription failed
        "checkout.session.completed": _handle_checkout_session_completed,  # subscription part
        "invoice.paid": _handle_invoice_paid,  # subscription succeeded
        # "customer.subscription.created": _handle_customer_subscription_created,
        "customer.subscription.updated": _handle_customer_subscription_updated,
        "customer.subscription.deleted": _handle_customer_subscription_deleted,
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
        except Exception as e:
            logging.warning(f"Failed to persist webhook event {event_id}: {e}")

    handler = event_handlers.get(event_type)
    if handler:
        handler(event)
    else:
        print(f"{event_payment_type}")
        print("Unhandled event type {}".format(event_type))


def _handle_payment_intent_succeeded(event: dict):
    event_data = event["data"]["object"]

    try:
        intent = IntentSucceed(**event_data)
    except ValidationError as e:
        print("IntentSucceed data validation failed:", e)
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

    if not intent_metadata or price_type != PriceType.USAGE_BASED:
        logging.info(f"{tenant_id} triggered {price_type} product {product_name} in intent succeeded, skipped. May handle in subscription.paid.")
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
    if "storage" in (product_name or "").lower():
        # For storage add-on, Stripe quantity is in GB (see /billing/checkout). Persist internal quota in KB.
        if not quantity_unit:
            quantity_unit = "GB"
        quota_unit = "KB"
        if quantity_unit == "GB":
            quota_quantity = quantity * 1024 * 1024
        elif quantity_unit == "KB":
            quota_quantity = quantity
        else:
            logging.warning(f"Unknown quantity_unit for storage add-on: {quantity_unit!r}, persisting raw quantity as KB.")
            quota_quantity = quantity

    payment_order = {
        "id": get_uuid(),
        "tenant_id": tenant_id,
        "customer_id": customer_id,
        "payment_type": PriceType.USAGE_BASED,
        "product_id": product_id,
        "product_name": product_name,
        "is_prorated": False,
        "amount_cents": amount_cents,
        "currency": currency,
        "payment_method": payment_method,
        "order_id": order_id,
        "price_id": price_id,
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
    print(f"\nintend.succeed parsed payment order {payment_order=}")
    # NOTE: We intentionally do NOT persist to the legacy `billing_usage_based` table.
    # The current system uses:
    # - `billing_payment_order` as the per-purchase ledger/history (needed for spend analytics), and
    # - `billing_purchased_product_overview` as the current remaining quota snapshot.

    purchased_overview = PurchasedProductOverviewService.get_by_product_name_and_tenant_id(product_name, tenant_id)
    if PaymentOrderService.get_by_payment_intent_id(payment_intent_id):
        logging.info(f"Skip duplicated payment_intent for tenant {tenant_id}: {payment_intent_id}")
        return

    try:
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

    except Exception as e:
        logging.warning(f"Handle intent.succeed error, {e}")
    print("above is intent.succeed")


def _handle_invoice_payment_failed(event: dict):
    # The payment failed or the customer does not have a valid payment method.
    # The subscription becomes past_due. Notify your customer and send them to the
    # customer portal to update their payment information.
    event_data = event["data"]["object"]
    subscription_id = (event_data.get("subscription") or "").strip() if isinstance(event_data, dict) else ""
    if subscription_id:
        storage = StorageSubscriptionService.get_by_subscription_id(subscription_id)
        if storage:
            tenant_id = storage.get("tenant_id")
            try:
                stripe_subscription = stripe.Subscription.retrieve(subscription_id)
                _sync_storage_subscription_record(
                    tenant_id,
                    stripe_subscription,
                    clear_pending=False,
                    pending_quantity_gb=storage.get("pending_quantity_gb"),
                    pending_action=storage.get("pending_action", ""),
                    pending_effective_at=storage.get("pending_effective_at"),
                )
            except Exception as e:
                logging.warning(f"Failed to sync storage subscription on invoice.payment_failed: {e}")
    print(event_data)
    print("\n above is invoice_payment.failed")


def _handle_checkout_session_completed(event: dict):
    # NOTE: save customer_id for portal session (front end)
    # Payment is successful and the subscription is created.
    # You should provision the subscription and save the customer ID to your database.

    event_data = event["data"]["object"]

    try:
        checkout_session_completed = CheckoutSessionCompleted(**event_data)
    except ValidationError as e:
        print("CheckoutSessionCompleted data validation failed:", e)
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
            try:
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
                if not PaymentOrderService.get_by_order_id(checkout_session_completed.id):
                    try:
                        PaymentOrderService.save(
                            id=get_uuid(),
                            tenant_id=tenant_id,
                            customer_id=customer_id,
                            payment_type=PriceType.USAGE_BASED,
                            product_id=None,
                            product_name="points_recharge",
                            is_prorated=False,
                            amount_cents=amount_cents,
                            currency=currency,
                            payment_method=PaymentMethod.CARD,
                            order_id=checkout_session_completed.id,
                            price_id="",
                            payment_intent_id=checkout_session_completed.payment_intent_id or "",
                            receipt_url="",
                            payment_channel=PaymentChannel.STRIPE,
                            payment_status=PaymentStatus.SUCCESS.value,
                            stripe_status=checkout_session_completed.payment_status or "",
                            paid=True,
                            captured=True,
                            description=f"Points recharge: {points} points",
                            order_created_at=checkout_session_completed.created,
                            payment_detail={"points_amount": points},
                        )
                    except Exception as e:
                        logging.warning(f"Failed to save points_recharge payment order: {e}")
            except Exception as e:
                logging.warning(f"Failed to process points_recharge for tenant {tenant_id}: {e}")
            return
    elif checkout_session_completed.mode == "subscription":
        metadata = checkout_session_completed.metadata or {}
        print(f"{checkout_session_completed.metadata=}")
        tenant_id = metadata.get("tenant_id")
        plan_name = metadata.get("product_name")
        price_id = metadata.get("price_id", "")
        if not price_id or not tenant_id:
            logging.warning("checkout.session.completed missing required metadata.")
            return
        if is_storage_plan_name(plan_name) or is_storage_price_id(price_id):
            _handle_storage_checkout_session_completed(checkout_session_completed, metadata)
            return

        product_id = get_product_id_by_name(plan_name)
        _amount_cents = checkout_session_completed.amount_total
        _currency = checkout_session_completed.currency
        order_id = checkout_session_completed.id
        _payment_intent_id = checkout_session_completed.payment_intent_id or ""
        subscription_id = checkout_session_completed.subscription_id or ""
        stripe_status = checkout_session_completed.payment_status
        payment_status = normalize_stripe_invoice_status(stripe_status)
        _order_created_at = checkout_session_completed.created

        customer_id = checkout_session_completed.customer_id or ""
        _expires_at = checkout_session_completed.expires_at
        if not customer_id:
            logging.warning("checkout.session.completed missing customer_id.")
            return

        start_time = None
        end_time = None
        referred_subscription = None
        if checkout_session_completed.invoice_id:
            invoice = stripe.Invoice.retrieve(checkout_session_completed.invoice_id)
            if isinstance(invoice, dict):
                lines = invoice.get("lines", {}).get("data", []) or []
            else:
                lines = getattr(getattr(invoice, "lines", None), "data", []) or []

            item = next(
                (li for li in lines if (li.get("amount", 0) if isinstance(li, dict) else getattr(li, "amount", 0)) > 0),
                lines[0] if lines else None,
            )

            if item:
                if isinstance(item, dict):
                    period = item.get("period", {}) or {}
                    start_time = to_utc_datetime(period.get("start"))
                    end_time = to_utc_datetime(period.get("end"))
                else:
                    period = getattr(item, "period", None)
                    if period:
                        start_time = to_utc_datetime(getattr(period, "start", None))
                        end_time = to_utc_datetime(getattr(period, "end", None))

        if not start_time or not end_time:
            if subscription_id:
                referred_subscription = stripe.Subscription.retrieve(subscription_id)
                if isinstance(referred_subscription, dict):
                    if not start_time:
                        start_time = to_utc_datetime(referred_subscription.get("current_period_start"))
                    if not end_time:
                        end_time = to_utc_datetime(referred_subscription.get("current_period_end"))
                else:
                    if not start_time:
                        start_time = to_utc_datetime(getattr(referred_subscription, "current_period_start", None))
                    if not end_time:
                        end_time = to_utc_datetime(getattr(referred_subscription, "current_period_end", None))

                if not start_time or not end_time:
                    logging.warning("checkout.session.completed missing subscription period for fallback.")
            else:
                logging.warning("checkout.session.completed missing subscription_id for period fallback.")
        print(f"{referred_subscription=}")
        print(f"{start_time=}")
        print(f"{end_time=}")

        subscription = SubscriptionService.get_by_tenant_id(tenant_id)
        subscription_status = subscription["subscription_status"]
        assert subscription, f"Expected a subscription for {tenant_id} here."
        subscription_dict = {
            "tenant_id": tenant_id,
            "product_id": product_id,
            "plan_name": plan_name,
            "order_id": order_id,
            "status": SubscriptionStatus.ACTIVE if payment_status == PaymentStatus.SUCCESS.value else SubscriptionStatus.PENDING,
            "customer_id": customer_id,
            "price_id": price_id,
            "subscription_id": subscription_id,
            "subscription_status": subscription_status if subscription_status else SubscriptionStatus.UNKNOWN,
            "start_time": start_time,
            "end_time": end_time,
            "renew_time": None,
            "original_subscription_id": subscription.get("original_subscription_id") or subscription_id,
        }
        print(f"\nlast {subscription=}")
        print(f"\ncheckout session completed parsed subscription dict {subscription_dict=}")

        with DB.atomic():
            SubscriptionService.update_subscription(tenant_id, subscription_dict)

        previous_subscription_id = (metadata.get("previous_subscription_id") or "").strip()
        if previous_subscription_id and previous_subscription_id != subscription_id:
            try:
                stripe.Subscription.delete(previous_subscription_id, prorate=False)
            except Exception as e:
                logging.info(f"Skip canceling previous subscription {previous_subscription_id}: {e}")

    elif checkout_session_completed.mode == "setup":
        # Handle setup for future payments
        pass
    print(event_data)
    print("\n above is checkout.session.completed")


def _handle_storage_checkout_session_completed(checkout_session_completed: CheckoutSessionCompleted, metadata: dict):
    tenant_id = (metadata.get("tenant_id") or "").strip()
    customer_id = (checkout_session_completed.customer_id or "").strip()
    subscription_id = (checkout_session_completed.subscription_id or "").strip()
    if not tenant_id or not subscription_id:
        logging.warning("Storage checkout.session.completed missing tenant_id/subscription_id.")
        return

    try:
        stripe_subscription = stripe.Subscription.retrieve(subscription_id)
    except Exception as e:
        logging.warning(f"Failed to retrieve storage subscription {subscription_id}: {e}")
        return

    _, price_id, stripe_quantity = extract_subscription_item(stripe_subscription)
    target_quantity = safe_int(metadata.get("target_quantity_gb", stripe_quantity), stripe_quantity)
    target_quantity = max(target_quantity, 0)

    _, period_end = extract_subscription_period(stripe_subscription)
    _sync_storage_subscription_record(
        tenant_id,
        stripe_subscription,
        customer_id=customer_id,
        target_quantity_gb=target_quantity if target_quantity > 0 else max(stripe_quantity, 0),
        clear_pending=False,
        pending_quantity_gb=target_quantity if target_quantity > 0 else max(stripe_quantity, 0),
        pending_action="create",
        pending_effective_at=period_end,
    )
    if not price_id:
        StorageSubscriptionService.upsert_by_tenant_id(tenant_id, price_id=(metadata.get("price_id") or "").strip())


def _handle_invoice_paid(event: dict):
    # Continue to provision the subscription as payments continue to be made.
    # Store the status in your database and check when a user accesses your service.
    # This approach helps you avoid hitting rate limits.

    event_data = event["data"]["object"]

    try:
        invoice_paid = InvoicePaid(**event_data)
    except ValidationError as e:
        print("InvoicePaid data validation failed:", e)
        return

    print(f"parsed {invoice_paid.model_dump()=}")

    invoice_id = invoice_paid.id
    line_items = invoice_paid.lines.data
    item = line_items[0]
    for li in line_items:
        if li.amount > 0:
            item = li
            break

    start_time = item.period.start
    end_time = item.period.end
    subscription_detail = item.parent.subscription_item_details

    subscription_id = ""
    if subscription_detail:
        subscription_id = subscription_detail.subscription

    description = invoice_paid.description or invoice_paid.billing_reason or ""
    description += f" {item.description}."

    amount_cents = invoice_paid.amount_paid
    currency = invoice_paid.currency
    order_id = invoice_paid.id
    stripe_status = invoice_paid.status or ""
    status = normalize_stripe_invoice_status(stripe_status)
    order_created_at = invoice_paid.created
    invoice_url = invoice_paid.hosted_invoice_url or ""
    invoice_pdf_url = invoice_paid.invoice_pdf or ""
    customer_id = invoice_paid.customer_id

    tenant_id = ""
    plan_name = ""
    price_id = ""
    product_id = ""
    metadata = invoice_paid.metadata or {}
    print(f"parsed {metadata}")
    if metadata:
        tenant_id = metadata.get("tenant_id", "")
    if not tenant_id:
        tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id)

    # Always derive the effective price / plan from the invoice or subscription,
    # instead of trusting possibly-stale metadata.
    try:
        price_id = item.pricing.price_details.price
    except Exception:
        price_id = ""

    if not price_id and metadata:
        price_id = metadata.get("price_id", "")

    if not price_id and tenant_id:
        subscription = SubscriptionService.get_by_tenant_id(tenant_id)
        if subscription:
            price_id = subscription.get("price_id", "")

    plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(price_id, "")
    if is_storage_price_id(price_id) or is_storage_plan_name(plan_name):
        _handle_storage_invoice_paid(invoice_paid, item, tenant_id=tenant_id, customer_id=customer_id, price_id=price_id, subscription_id=subscription_id)
        return

    product_id = get_product_id_by_name(plan_name)

    print("=======================")
    print(f"{tenant_id=}, {plan_name=}, {product_id=}, {price_id=}")

    payment_order = {
        "id": get_uuid(),
        "tenant_id": tenant_id,
        "customer_id": customer_id,
        "payment_type": PriceType.SUBSCRIPTION,
        "product_id": product_id,
        "product_name": plan_name,
        "is_prorated": True,
        "amount_cents": amount_cents,
        "currency": currency,
        "payment_method": PaymentMethod.CARD,
        "order_id": order_id,
        "price_id": price_id,
        "payment_intent_id": "",
        "payment_subscription_id": subscription_id,
        "receipt_url": invoice_url,
        "receipt_pdf_url": invoice_pdf_url,
        "payment_channel": PaymentChannel.STRIPE,
        "payment_status": status,
        "stripe_status": stripe_status,
        "paid": status == PaymentStatus.SUCCESS.value,
        "captured": status == PaymentStatus.SUCCESS.value,  # paid may not mean captured
        "description": description,
        "order_created_at": order_created_at,
        "payment_detail": {},
    }
    print(f"\n invoice.paid parsed payment order {payment_order=}")

    if PaymentOrderService.get_by_order_id(order_id):
        logging.info(f"Skip duplicated invoice.paid for tenant {tenant_id}: {order_id}")
        return

    subscription = SubscriptionService.get_by_tenant_id(tenant_id)
    assert subscription, f"Expected a subscription for {tenant_id} here."
    subscription_dict = {
        "tenant_id": tenant_id,
        "product_id": product_id,
        "plan_name": plan_name,
        "order_id": payment_order["id"],
        "status": SubscriptionStatus.ACTIVE if payment_order["payment_status"] == PaymentStatus.SUCCESS.value else SubscriptionStatus.PENDING,
        "customer_id": customer_id,
        "price_id": price_id,
        "subscription_id": subscription_id,
        "subscription_status": SubscriptionStatus.ACTIVE if payment_order["payment_status"] == PaymentStatus.SUCCESS.value else SubscriptionStatus.PENDING,
        "invoice_id": invoice_id,
        "invoice_url": invoice_url,
        "invoice_pdf_url": invoice_pdf_url,
        "start_time": start_time,
        "end_time": end_time,
        "renew_time": None,
        "original_subscription_id": subscription.get("original_subscription_id") or subscription_id,
    }
    print(f"last {subscription=}")
    print(f"\n invoice.paid parsed subscription dict {subscription_dict=}")

    try:
        with DB.atomic():
            PaymentOrderService.save(**payment_order)
            SubscriptionService.update_subscription(tenant_id, subscription_dict)
    except Exception as e:
        logging.warning(f"Handle invoice paid error, {e}")

    print("\nabove is invoice.paid")


def _handle_storage_invoice_paid(
    invoice_paid: InvoicePaid,
    item,
    *,
    tenant_id: str,
    customer_id: str,
    price_id: str,
    subscription_id: str,
):
    invoice_id = invoice_paid.id
    existing_order = PaymentOrderService.get_by_order_id(invoice_id)

    if not subscription_id:
        try:
            subscription_id = item.parent.subscription_item_details.subscription
        except Exception:
            subscription_id = ""
    if not price_id:
        try:
            price_id = item.pricing.price_details.price
        except Exception:
            price_id = ""

    if not tenant_id and subscription_id:
        storage_row = StorageSubscriptionService.get_by_subscription_id(subscription_id) or {}
        tenant_id = storage_row.get("tenant_id", "")
        customer_id = customer_id or storage_row.get("customer_id", "")

    product_name = settings.BILLING_PRICEID_TO_PRODUCT.get(price_id, "") or STORAGE_PRODUCT_NAME
    product_id = get_product_id_by_name(product_name)
    status = normalize_stripe_invoice_status(invoice_paid.status or "")
    payment_order = {
        "id": get_uuid(),
        "tenant_id": tenant_id,
        "customer_id": customer_id,
        "payment_type": PriceType.SUBSCRIPTION,
        "product_id": product_id,
        "product_name": product_name,
        "is_prorated": True,
        "amount_cents": invoice_paid.amount_paid,
        "currency": invoice_paid.currency,
        "payment_method": PaymentMethod.CARD,
        "order_id": invoice_id,
        "price_id": price_id,
        "payment_intent_id": "",
        "payment_subscription_id": subscription_id,
        "receipt_url": invoice_paid.hosted_invoice_url or "",
        "receipt_pdf_url": invoice_paid.invoice_pdf or "",
        "payment_channel": PaymentChannel.STRIPE,
        "payment_status": status,
        "stripe_status": invoice_paid.status or "",
        "paid": status == PaymentStatus.SUCCESS.value,
        "captured": status == PaymentStatus.SUCCESS.value,
        "description": f"{invoice_paid.billing_reason or ''} {item.description or ''}".strip(),
        "order_created_at": invoice_paid.created,
        "payment_detail": {"quantity": safe_int(getattr(item, "quantity", 0), 0), "quantity_unit": "GB"},
    }

    stripe_subscription = None
    if subscription_id:
        try:
            stripe_subscription = stripe.Subscription.retrieve(subscription_id)
        except Exception as e:
            logging.warning(f"Failed to retrieve storage subscription {subscription_id} on invoice.paid: {e}")

    try:
        sync_ok = False
        if stripe_subscription:
            sync_ok = _sync_storage_subscription_record(tenant_id, stripe_subscription, customer_id=customer_id, clear_pending=True)
        else:
            storage = StorageSubscriptionService.get_by_tenant_id(tenant_id) or {}
            sync_ok = StorageSubscriptionService.upsert_by_tenant_id(
                tenant_id,
                customer_id=customer_id,
                price_id=price_id,
                status=storage.get("status", "active"),
                pending_quantity_gb=None,
                pending_action="",
                pending_effective_at=None,
            )

        if not sync_ok:
            raise RuntimeError(f"Failed to sync storage subscription state for tenant {tenant_id} on invoice {invoice_id}")

        if existing_order:
            logging.info(f"Skip duplicated storage invoice.paid payment_order for tenant {tenant_id}: {invoice_id}")
            return

        PaymentOrderService.save(**payment_order)
    except IntegrityError:
        logging.info(f"Skip duplicated storage invoice.paid write for tenant {tenant_id}: {invoice_id}")
    except Exception as e:
        logging.warning(f"Handle storage invoice paid error: {e}")


def _set_storage_cancel_at_period_end_sync(tenant_id: str, value: bool = True) -> tuple[bool, dict]:
    storage = StorageSubscriptionService.get_by_tenant_id(tenant_id)
    if not storage or not storage.get("subscription_id"):
        return True, {}

    subscription_id = storage.get("subscription_id")
    updated = stripe.Subscription.modify(subscription_id, cancel_at_period_end=value)
    _, updated_period_end = extract_subscription_period(updated)
    if value:
        _sync_storage_subscription_record(
            tenant_id,
            updated,
            target_quantity_gb=0,
            clear_pending=False,
            pending_quantity_gb=0,
            pending_action="cancel",
            pending_effective_at=updated_period_end,
        )
    else:
        _sync_storage_subscription_record(
            tenant_id,
            updated,
            clear_pending=True,
        )
    return True, {
        "cancel_at_period_end": value,
        "effective_at": updated_period_end,
    }


def _align_storage_cycle_to_main_sync(tenant_id: str, main_period_end) -> tuple[bool, dict]:
    storage = StorageSubscriptionService.get_by_tenant_id(tenant_id)
    if not storage or not storage.get("subscription_id") or not main_period_end:
        return True, {}
    if _has_storage_blocking_pending(tenant_id):
        return False, {"error": "Storage has a pending payment. Please finish current payment first."}

    subscription_id = storage["subscription_id"]
    aligned = stripe.Subscription.modify(
        subscription_id,
        billing_cycle_anchor=int(to_utc_datetime(main_period_end).timestamp()),
        proration_behavior="create_prorations",
        payment_behavior="pending_if_incomplete",
    )
    pending_update = aligned.get("pending_update") if isinstance(aligned, dict) else getattr(aligned, "pending_update", None)
    _sync_storage_subscription_record(
        tenant_id,
        aligned,
        clear_pending=False,
        pending_quantity_gb=storage.get("effective_quantity_gb"),
        pending_action="align" if pending_update else "",
        pending_effective_at=to_utc_datetime(main_period_end) if pending_update else None,
    )
    return True, {"pending_update": bool(pending_update)}


def _period_changed(previous_start, previous_end, current_start, current_end) -> bool:
    if not previous_start or not previous_end or not current_start or not current_end:
        return False
    return int(previous_start.timestamp()) != int(current_start.timestamp()) or int(previous_end.timestamp()) != int(current_end.timestamp())


def _handle_main_subscription_side_effects(
    *,
    tenant_id: str,
    current_plan_name: str,
    previous_main_start,
    previous_main_end,
    current_main_start,
    current_main_end,
):
    if not tenant_id:
        return

    if is_trial_plan_name(current_plan_name):
        try:
            _set_storage_cancel_at_period_end_sync(tenant_id, value=True)
        except Exception as e:
            logging.warning(f"Failed to auto-cancel storage after Trial downgrade for tenant {tenant_id}: {e}")
        return

    if _period_changed(previous_main_start, previous_main_end, current_main_start, current_main_end):
        try:
            _align_storage_cycle_to_main_sync(tenant_id, current_main_end)
        except Exception as e:
            logging.warning(f"Failed to align storage cycle for tenant {tenant_id}: {e}")


def _handle_storage_subscription_updated(subscription_updated: SubscriptionUpdated):
    subscription = subscription_updated.data.object
    subscription_id = subscription.id
    customer_id = subscription.customer_id
    tenant_id = subscription.metadata.get("tenant_id", "")
    if not tenant_id:
        storage_row = StorageSubscriptionService.get_by_subscription_id(subscription_id) or {}
        tenant_id = storage_row.get("tenant_id", "")
    if not tenant_id:
        tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id)
    if not tenant_id:
        logging.warning(f"Skip storage subscription.updated without tenant context: {subscription_id}")
        return

    row = StorageSubscriptionService.get_by_tenant_id(tenant_id) or {}
    _, _, quantity = extract_subscription_item(subscription)
    pending_qty = row.get("pending_quantity_gb")
    pending_action = (row.get("pending_action") or "").strip().lower()
    pending_update = getattr(subscription, "pending_update", None)
    status = (subscription.status or "").strip().lower()
    latest_invoice_paid = is_subscription_latest_invoice_paid_sync(subscription)

    should_clear_pending = bool((not pending_update) and ((pending_qty is not None and quantity == safe_int(pending_qty, -1) and latest_invoice_paid) or status in {"canceled", "incomplete_expired"}))
    _sync_storage_subscription_record(
        tenant_id,
        subscription,
        customer_id=customer_id,
        clear_pending=should_clear_pending,
        pending_quantity_gb=None if should_clear_pending else pending_qty,
        pending_action="" if should_clear_pending else pending_action,
        pending_effective_at=None if should_clear_pending else row.get("pending_effective_at"),
    )


def _handle_customer_subscription_created(event: dict):
    event_data = event["data"]["object"]
    print(event_data)
    print("\n above is customer.subscription.created")


def _handle_customer_subscription_updated(event: dict):
    print("-----------------------------------------")
    print("enter subscription update")

    try:
        subscription_updated = SubscriptionUpdated(**event)
    except ValidationError as e:
        print("Subscription Updated data validation failed:", e)
        return

    print(f"parsed {subscription_updated.model_dump()=}")

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

    storage_row_by_subscription = StorageSubscriptionService.get_by_subscription_id(subscription_id)
    existing_main_subscription = SubscriptionService.get_by_tenant_id(tenant_id) if tenant_id else {}
    previous_main_start = to_utc_datetime(existing_main_subscription.get("start_time")) if existing_main_subscription else None
    previous_main_end = to_utc_datetime(existing_main_subscription.get("end_time")) if existing_main_subscription else None
    print(f"Handling update for subscription: {subscription_id} ({tenant_id=})")

    if (not subscription.items or not subscription.items.data) and not storage_row_by_subscription:
        logging.warning("subscription.updated missing subscription items.")
        return

    first_price_id = subscription.items.data[0].price.id if subscription.items and subscription.items.data else ""
    if storage_row_by_subscription or is_storage_price_id(first_price_id) or is_storage_plan_name(subscription.metadata.get("product_name", "")):
        _handle_storage_subscription_updated(subscription_updated)
        print("\nabove is customer.subscription.updated(storage)")
        return

    if previous and (previous.plan or previous.items):
        old_price = None
        new_price = subscription.items.data[0].price
        new_price_id = new_price.id
        product_name = settings.BILLING_PRICEID_TO_PRODUCT.get(new_price_id, "")
        product_id = get_product_id_by_name(product_name)
        print(f"update subscription {new_price=}, {product_id=}, {product_name=}")

        if previous.items and previous.items.data:
            old_price = previous.items.data[0].price
        elif previous.plan:
            old_price = previous.plan
        else:
            old_price = None
        old_price_id = getattr(old_price, "id", "") if old_price else ""

        def _plan_label(price_id: str, nickname: str | None) -> str:
            return settings.BILLING_PRICEID_TO_PRODUCT.get(price_id, "") or (nickname or "") or "unknown"

        latest_invoice_id = subscription.latest_invoice_id or ""
        stripe_invoice_status = ""
        invoice_url = ""
        invoice_pdf_url = ""
        invoice_created = subscription_updated.created
        if latest_invoice_id:
            existing_payment_order = PaymentOrderService.get_by_order_id(latest_invoice_id)
            if existing_payment_order:
                invoice_url = existing_payment_order.get("receipt_url", "") or ""
                invoice_pdf_url = existing_payment_order.get("receipt_pdf_url", "") or ""
                invoice_created = existing_payment_order.get("order_created_at") or invoice_created
        print(f"update subscription {latest_invoice_id=}, {invoice_url=}, {invoice_pdf_url=}, {invoice_created=}")

        period_start_value = subscription.current_period_start or subscription.items.data[0].current_period_start
        period_end_value = subscription.current_period_end or subscription.items.data[0].current_period_end
        current_period_start = to_utc_datetime(period_start_value)
        current_period_end = to_utc_datetime(period_end_value)
        if not current_period_start or not current_period_end:
            logging.warning(f"subscription.updated missing current period boundaries: {subscription_id=}, {period_start_value=}, {period_end_value=}")
            return

        if old_price:
            existing_payment_order_id = existing_payment_order.get("id") if latest_invoice_id and existing_payment_order else ""
            payment_order_id = existing_payment_order_id or get_uuid()
            should_save_payment_order = bool(latest_invoice_id) and not existing_payment_order_id
            payment_order = {
                "id": payment_order_id,
                "tenant_id": tenant_id,
                "customer_id": customer_id,
                "payment_type": PriceType.SUBSCRIPTION,
                "product_id": product_id,
                "product_name": product_name,
                "is_prorated": True,
                "amount_cents": new_price.unit_amount,
                "currency": new_price.currency,
                "payment_method": PaymentMethod.CARD,
                "order_id": latest_invoice_id,
                "price_id": new_price_id,
                "payment_intent_id": "",
                "payment_subscription_id": subscription_id,
                "receipt_url": invoice_url,
                "receipt_pdf_url": invoice_pdf_url,
                "payment_channel": PaymentChannel.STRIPE,
                "payment_status": normalize_stripe_invoice_status(stripe_invoice_status) if stripe_invoice_status else PaymentStatus.PENDING.value,
                "stripe_status": stripe_invoice_status,
                "paid": stripe_invoice_status and normalize_stripe_invoice_status(stripe_invoice_status) == PaymentStatus.SUCCESS.value,
                "captured": stripe_invoice_status and normalize_stripe_invoice_status(stripe_invoice_status) == PaymentStatus.SUCCESS.value,
                "description": f"Subscription change from {old_price.nickname if old_price else 'unknown'} to {new_price.nickname}",
                "order_created_at": invoice_created,
                "payment_detail": {},
            }

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
                "order_id": payment_order["id"],
                "status": SubscriptionStatus.ACTIVE,
                "customer_id": customer_id,
                "price_id": new_price_id,
                "subscription_id": subscription_id,
                "subscription_status": SubscriptionStatus.ACTIVE,
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
                print(f"UPGRADE from {old_label} to {new_label}")
                payment_order["description"] = f"Upgrade from {old_label} to {new_label}"
                # Additional upgrade-specific logic if needed
            else:
                old_label = _plan_label(old_price_id, getattr(old_price, "nickname", None) if old_price else None)
                new_label = _plan_label(new_price_id, new_price.nickname)
                print(f"DOWNGRADE from {old_label} to {new_label}")
                payment_order["description"] = f"Downgrade from {old_label} to {new_label}"
                # Additional downgrade-specific logic if needed

            try:
                with DB.atomic():
                    if should_save_payment_order:
                        PaymentOrderService.save(**payment_order)
                    SubscriptionService.update_subscription(tenant_id, subscription_dict)
            except Exception as e:
                print(f"Failed to save upgrade/downgrade record: {e}")

            try:
                _handle_main_subscription_side_effects(
                    tenant_id=tenant_id,
                    current_plan_name=product_name,
                    previous_main_start=previous_main_start,
                    previous_main_end=previous_main_end,
                    current_main_start=current_period_start,
                    current_main_end=current_period_end,
                )
            except Exception as e:
                logging.warning(f"Failed handling main subscription side-effects for tenant {tenant_id}: {e}")

            schedule_id = (event.get("data", {}).get("object", {}) or {}).get("schedule") if isinstance(event, dict) else ""
            if schedule_id:
                try:
                    test_clock_id = (event.get("data", {}).get("object", {}) or {}).get("test_clock") if isinstance(event, dict) else ""
                    if test_clock_id:
                        time.sleep(30)
                    stripe.SubscriptionSchedule.release(schedule_id)
                except Exception as e:
                    logging.info(f"Skip releasing subscription schedule {schedule_id}: {e}")

    elif previous and previous.status:
        print(f"Status changed: {previous.status} → {subscription.status}")
        # TODO: handle cancellation, reactivation, etc.

    elif previous and previous.trial_end:
        print(f"Trial end changed: {previous.trial_end} → {subscription.trial_end}")

    else:
        print("Subscription updated, but no actionable fields changed.")

    print("\nabove is customer.subscription.updated")


def _handle_customer_subscription_deleted(event: dict):
    event_data = event["data"]["object"]
    subscription_id = (event_data.get("id") or "").strip() if isinstance(event_data, dict) else ""
    customer_id = (event_data.get("customer") or "").strip() if isinstance(event_data, dict) else ""
    if subscription_id:
        storage = StorageSubscriptionService.get_by_subscription_id(subscription_id)
        if storage:
            tenant_id = storage.get("tenant_id")
            if tenant_id:
                StorageSubscriptionService.upsert_by_tenant_id(
                    tenant_id,
                    customer_id=customer_id or storage.get("customer_id", ""),
                    subscription_id=subscription_id,
                    status="canceled",
                    cancel_at_period_end=False,
                    effective_quantity_gb=0,
                    target_quantity_gb=0,
                    pending_quantity_gb=None,
                    pending_action="",
                    pending_effective_at=None,
                )
                print(event_data)
                print("\n above is customer.subscription.delete(storage)")
                return
    print(event_data)
    print("\n above is customer.subscription.delete")
