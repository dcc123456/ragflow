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
import asyncio
from functools import partial
import json
import logging
import time
from datetime import datetime, timezone

import stripe

# Reduce Stripe SDK verbosity — INFO-level messages from the stripe http client
# (e.g., "Request to Stripe api", "Stripe API response") are noisy in production.
logging.getLogger("stripe").setLevel(logging.WARNING)

from peewee import IntegrityError
from pydantic import ValidationError

from api.db import PaymentChannel, PaymentMethod, PaymentStatus, PriceType, ProductType, SubscriptionStatus
from api.db.db_models import DB, Subscription
from api.db.services.billing_service import (
    BillingWebhookEventService,
    PaymentOrderService,
    PointAccountService,
    PurchasedProductOverviewService,
    SubscriptionService,
)
from api.db.services.system_settings_service import SystemSettingsService
from api.utils.billing import (
    extract_invoice_failure_context,
    extract_latest_invoice_id,
    extract_list_data,
    extract_plan_subscription_item,
    extract_storage_subscription_item,
    extract_subscription_items_data,
    extract_subscription_period,
    get_attr_or_item,
    get_nested_attr_or_item,
    get_product_id_by_name,
    get_receipt_url_from_intent_latest_charge_async,
    is_downgrade_by_price_id,
    is_storage_plan_name,
    is_storage_price_id,
    is_subscription_latest_invoice_paid_async,
    is_trial_plan_name,
    get_pending_subscription_change_async,
    normalize_stripe_invoice_status,
    safe_int,
    storage_quantity_to_bytes,
    to_utc_datetime,
)
from api.utils.billing_schema import CheckoutSessionCompleted, IntentSucceed, InvoicePaid, SubscriptionUpdated
from common import settings
from common.billing_rate_limit_sync import sync_tenant_rate_limit
from common.billing_utils import normalize_stripe_payment_intent_status
from common.misc_utils import get_uuid


PAYMENT_INTENT_SUCCEEDED = "payment_intent.succeeded"
INVOICE_FAILED = "invoice.payment_failed"
INVOICE_PAYMENT_ACTION_REQUIRED = "invoice.payment_action_required"
CHECKOUT_SESSION_COMPLETED = "checkout.session.completed"
INVOICE_PAID = "invoice.paid"
SUBSCRIPTION_UPDATED = "customer.subscription.updated"
SUBSCRIPTION_DELETED = "customer.subscription.deleted"
SUBSCRIPTION_CREATED = "customer.subscription.created"

# All Stripe event types that the billing webhook handler cares about.
# Used both for filtering handle_undelivered_events and for registering the webhook endpoint.
FOCUSED_STRIPE_WEBHOOK = [
    INVOICE_PAID,
    INVOICE_FAILED,
    INVOICE_PAYMENT_ACTION_REQUIRED,
    SUBSCRIPTION_UPDATED,
    SUBSCRIPTION_DELETED,
    SUBSCRIPTION_CREATED,
    CHECKOUT_SESSION_COMPLETED,
    PAYMENT_INTENT_SUCCEEDED,
]

WEBHOOK_EVENT_STATUS_PROCESSING = "processing"
WEBHOOK_EVENT_STATUS_COMPLETED = "completed"
WEBHOOK_EVENT_STATUS_FAILED = "failed"
WEBHOOK_EVENT_STATUS_UNHANDLED = "unhandled"
WEBHOOK_EVENT_TERMINAL_STATUSES = {
    WEBHOOK_EVENT_STATUS_COMPLETED,
    WEBHOOK_EVENT_STATUS_UNHANDLED,
}

MAIN_SUBSCRIPTION_RECOVERABLE_STATUSES = {"incomplete", "past_due", "unpaid"}
WEBHOOK_CHECKPOINT_SETTING_NAME = "billing_webhook_event_checkpoint"


def _normalize_subscription_status(status: str | None) -> str:
    return (status or "").strip().lower()



def _load_webhook_checkpoint() -> dict[str, str | int]:
    record = SystemSettingsService.get_singleton_by_exact_name(WEBHOOK_CHECKPOINT_SETTING_NAME)
    if record and record.value:
        try:
            payload = json.loads(record.value)
            if isinstance(payload, dict):
                created = payload.get("created")
                event_id = payload.get("id") or ""
                return {
                    "created": int(created) if created is not None else 0,
                    "id": str(event_id or ""),
                }
        except (TypeError, ValueError, json.JSONDecodeError):
            logging.warning("Invalid billing webhook checkpoint JSON, ignoring persisted value.")

    legacy_created = SystemSettingsService.get_singleton_by_exact_name("billing_webhook_event_checkpoint")
    legacy_event_id = SystemSettingsService.get_singleton_by_exact_name("billing_webhook_event_checkpoint_event_id")
    created_value = 0
    event_id_value = ""
    if legacy_created and legacy_created.value:
        try:
            created_value = int(legacy_created.value)
        except (TypeError, ValueError):
            created_value = 0
    if legacy_event_id and legacy_event_id.value:
        event_id_value = str(legacy_event_id.value or "")

    if created_value or event_id_value:
        payload = {"created": created_value, "id": event_id_value}
        SystemSettingsService.upsert_singleton_by_exact_name(
            name=WEBHOOK_CHECKPOINT_SETTING_NAME,
            source="billing",
            data_type="json",
            value=json.dumps(payload, separators=(",", ":")),
        )
        SystemSettingsService.delete_by_exact_name("billing_webhook_event_checkpoint_event_id")
        return payload

    return {"created": 0, "id": ""}


def _is_non_canceled_subscription_status(status: str | None) -> bool:
    normalized = _normalize_subscription_status(status)
    return bool(normalized) and normalized != SubscriptionStatus.CANCELED


def _should_preview_as_new_subscription(current_plan_name: str, target_plan_name: str) -> bool:
    return is_trial_plan_name(current_plan_name) and bool(target_plan_name) and not is_trial_plan_name(target_plan_name)


def _safe_payment_order_created_at(value, order_id: str = ""):
    try:
        return to_utc_datetime(value)
    except (TypeError, ValueError, OSError, OverflowError) as exc:
        logging.warning("Ignore invalid payment order created_at for %s: %r, %s", order_id or "unknown order", value, exc)
        return None


def _sync_main_subscription_from_stripe(
    *,
    tenant_id: str,
    stripe_subscription,
    subscription_status: str = "",
    invoice_id: str = "",
    invoice_url: str = "",
    invoice_pdf_url: str = "",
    preserve_existing_plan: bool = False,
) -> None:
    if not tenant_id:
        logging.warning("Main subscription sync skipped without tenant_id.")
        return

    existing = SubscriptionService.get_raw_by_tenant_id(tenant_id) or {}

    _item_id, price_id, _quantity = extract_plan_subscription_item(stripe_subscription)
    plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(price_id, "") or existing.get("plan_name", "")
    product_id = get_product_id_by_name(plan_name)

    # Only clear target_plan_name when the plan price actually changes.
    # A SubscriptionSchedule creation/modification also triggers
    # customer.subscription.updated with the same price_id — in that
    # case the downgrade guard's marker must be preserved.
    existing_price_id = (existing.get("price_id") or "").strip() if existing else ""
    plan_changed = bool(existing_price_id) and existing_price_id != (price_id or "").strip()

    if preserve_existing_plan and existing.get("price_id") and existing.get("plan_name"):
        preserved_price_id = (existing.get("price_id") or "").strip()
        preserved_plan_name = (existing.get("plan_name") or "").strip()
        preserved_product_id = (existing.get("product_id") or "").strip() or get_product_id_by_name(preserved_plan_name)
        if preserved_price_id and preserved_plan_name and preserved_product_id:
            logging.info(
                "Preserving local entitled plan while upgrade invoice is unpaid: tenant_id=%s preserved_price_id=%s incoming_price_id=%s",
                tenant_id,
                preserved_price_id,
                price_id,
            )
            price_id = preserved_price_id
            plan_name = preserved_plan_name
            product_id = preserved_product_id

    subscription_id = (get_attr_or_item(stripe_subscription, "id", "") or existing.get("subscription_id", "") or "").strip()
    customer_id = (get_attr_or_item(stripe_subscription, "customer", "") or existing.get("customer_id", "") or "").strip()
    stripe_status = _normalize_subscription_status(get_attr_or_item(stripe_subscription, "status", ""))
    period_start, period_end = extract_subscription_period(stripe_subscription)
    final_status = _normalize_subscription_status(subscription_status or stripe_status or existing.get("subscription_status"))

    if not price_id:
        logging.warning("Main subscription sync skipped without price_id: tenant_id=%s subscription_id=%s", tenant_id, subscription_id)
        return
    if not plan_name or not product_id:
        logging.warning(
            "Main subscription sync skipped because price_id could not be resolved to a billing product: tenant_id=%s subscription_id=%s price_id=%s plan_name=%s product_id=%s",
            tenant_id,
            subscription_id,
            price_id,
            plan_name,
            product_id,
        )
        return
    if not period_start:
        logging.warning(
            "Main subscription sync skipped without period_start: tenant_id=%s subscription_id=%s price_id=%s",
            tenant_id,
            subscription_id,
            price_id,
        )
        return

    subscription_dict = {
        "tenant_id": tenant_id,
        "product_id": product_id,
        "plan_name": plan_name,
        "order_id": existing.get("order_id", "") or subscription_id or invoice_id or f"stripe_sync_{tenant_id}",
        "status": final_status,
        "customer_id": customer_id,
        "price_id": price_id,
        "subscription_id": subscription_id,
        "subscription_status": final_status,
        "invoice_id": invoice_id or existing.get("invoice_id", ""),
        "invoice_url": invoice_url or existing.get("invoice_url", ""),
        "invoice_pdf_url": invoice_pdf_url or existing.get("invoice_pdf_url", ""),
        "start_time": period_start,
        "end_time": period_end or existing.get("end_time"),
        "renew_time": None,
        "original_subscription_id": existing.get("original_subscription_id") or subscription_id,
        **({"target_plan_name": None} if plan_changed else {}),
    }

    with DB.atomic():
        SubscriptionService.upsert_subscription(tenant_id, subscription_dict)
    _sync_tenant_rate_limit_for_subscription(
        tenant_id=tenant_id,
        plan_name=subscription_dict.get("plan_name"),
        subscription_status=subscription_dict.get("subscription_status"),
    )


def _sync_tenant_rate_limit_for_subscription(
    *,
    tenant_id: str,
    plan_name: str | None,
    subscription_status: str | None,
) -> None:
    if not tenant_id:
        return
    normalized_status = _normalize_subscription_status(subscription_status)
    rate_limit_plan_name = plan_name if normalized_status == SubscriptionStatus.ACTIVE else "Trial"
    try:
        sync_tenant_rate_limit(tenant_id, rate_limit_plan_name)
    except Exception:
        logging.exception(
            "Failed to sync tenant rate limit after subscription update: tenant_id=%s plan_name=%s subscription_status=%s",
            tenant_id,
            rate_limit_plan_name,
            normalized_status,
        )


def _should_preserve_existing_plan_for_unpaid_upgrade(
    existing_main_subscription: dict | None,
    stripe_subscription,
    latest_invoice_paid: bool,
) -> bool:
    if latest_invoice_paid or not existing_main_subscription:
        return False

    existing_subscription_id = (existing_main_subscription.get("subscription_id") or "").strip()
    incoming_subscription_id = (get_attr_or_item(stripe_subscription, "id", "") or "").strip()
    if not existing_subscription_id or existing_subscription_id != incoming_subscription_id:
        return False

    existing_price_id = (existing_main_subscription.get("price_id") or "").strip()
    _item_id, incoming_price_id, _quantity = extract_plan_subscription_item(stripe_subscription)
    incoming_price_id = (incoming_price_id or "").strip()
    if not existing_price_id or not incoming_price_id or existing_price_id == incoming_price_id:
        return False

    # Downgrades should not be preserved at the higher old tier. For unpaid
    # upgrades, keep the currently entitled local plan until payment succeeds.
    return not is_downgrade_by_price_id(existing_price_id, incoming_price_id)


def _sync_storage_subscription_record(
    tenant_id: str,
    subscription_obj,
    customer_id: str = "",
    *,
    target_storage_bytes: int | None = None,
) -> bool:
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

    with DB.atomic():
        Subscription.update(
            addon_subscription_item_id=item_id or None,
            addon_storage_bytes=quantity_bytes,
            target_storage_bytes=update_dict.get("target_storage_bytes", quantity_bytes),
        ).where(Subscription.tenant_id == tenant_id).execute()

    logging.info(
        "Synced storage subscription record: tenant_id=%s subscription_id=%s customer_id=%s item_id=%s price_id=%s quantity=%s addon_storage_bytes=%s target_storage_bytes=%s cancel_at_period_end=%s status=%s",
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


async def _handle_main_subscription_invoice_not_paid(event: dict, description: str) -> None:
    event_data = event["data"]["object"]
    if not isinstance(event_data, dict):
        logging.warning("Main subscription invoice failure skipped because event data object is not a dict.")
        return

    context = extract_invoice_failure_context(event_data)
    subscription_id = context["subscription_id"]
    customer_id = context["customer_id"]
    tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id) if customer_id else ""

    if not tenant_id:
        logging.warning("Main subscription invoice failure missing tenant context: subscription_id=%s, customer_id=%s", subscription_id, customer_id)
        return

    try:
        stripe_subscription = await stripe.Subscription.retrieve_async(subscription_id)
    except stripe.AuthenticationError:
        logging.warning(
            "Stripe API key missing while retrieving subscription %s for invoice failure; trying sync fallback.",
            subscription_id,
        )
        stripe_subscription = stripe.Subscription.retrieve(subscription_id)
    stripe_status = _normalize_subscription_status(get_attr_or_item(stripe_subscription, "status", ""))
    local_status = stripe_status
    if local_status in {"", "active", "trialing"}:
        local_status = "past_due"
    latest_invoice_paid, latest_invoice_id, latest_invoice_status, latest_invoice_url = await is_subscription_latest_invoice_paid_async(stripe_subscription)
    existing = SubscriptionService.get_raw_by_tenant_id(tenant_id) or {}
    preserve_existing_plan = _should_preserve_existing_plan_for_unpaid_upgrade(existing, stripe_subscription, latest_invoice_paid)

    _sync_main_subscription_from_stripe(
        tenant_id=tenant_id,
        stripe_subscription=stripe_subscription,
        subscription_status=local_status,
        invoice_id=context["invoice_id"] or latest_invoice_id,
        invoice_url=context["invoice_url"] or latest_invoice_url,
        invoice_pdf_url=context["invoice_pdf_url"],
        preserve_existing_plan=preserve_existing_plan,
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
        stripe_status=context["invoice_status"] or latest_invoice_status,
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

def _prepare_webhook_event_processing(
    event_id,
    event_type,
    object_id,
    payload,
    payload_created_at,
    received_at,
    payload_created,
    retry_inflight=False,
):
    try:
        with DB.atomic():
            BillingWebhookEventService.save(
                event_id=event_id,
                event_type=event_type,
                object_id=object_id,
                payload=payload,
                created_at=payload_created_at,
                received_at=received_at,
                processing_status=WEBHOOK_EVENT_STATUS_PROCESSING,
                processing_started_at=received_at,
                processed_at=None,
                failed_at=None,
                last_error="",
            )
            return True
    except IntegrityError:
        existing = BillingWebhookEventService.get_by_event_id(event_id) or {}
        existing_status = (existing.get("processing_status") or WEBHOOK_EVENT_STATUS_COMPLETED).strip().lower()

        if existing_status in WEBHOOK_EVENT_TERMINAL_STATUSES:
            logging.warning("Skip duplicated webhook event: %s (%s, status=%s)", event_id, event_type, existing_status)
            _update_webhook_event_checkpoint(payload_created, event_id=event_id)
            return False

        if existing_status == WEBHOOK_EVENT_STATUS_FAILED or retry_inflight:
            BillingWebhookEventService.mark_processing(event_id)
            return True

        raise RuntimeError(f"Webhook event already processing: {event_id} ({event_type})")


def _update_webhook_event_checkpoint(event_timestamp, event_id: str = ""):
    if not event_timestamp:
        return
    try:
        event_timestamp_int = int(event_timestamp)
        checkpoint = _load_webhook_checkpoint()
        existing_timestamp_int = int(checkpoint.get("created") or 0)
        existing_event_id = str(checkpoint.get("id") or "")

        # Keep the checkpoint monotonic even if startup replay processes
        # broader historical events than intended.
        if existing_timestamp_int is not None and event_timestamp_int < existing_timestamp_int:
            logging.info(
                "Skip billing webhook checkpoint regression: existing=%s incoming=%s event_id=%s",
                existing_timestamp_int,
                event_timestamp_int,
                event_id or "",
            )
            return

        next_payload = {
            "created": event_timestamp_int,
            "id": event_id or existing_event_id,
        }
        if (
            event_timestamp_int == existing_timestamp_int
            and next_payload["id"] == existing_event_id
        ):
            return

        SystemSettingsService.upsert_singleton_by_exact_name(
            name=WEBHOOK_CHECKPOINT_SETTING_NAME,
            source="billing",
            data_type="json",
            value=json.dumps(next_payload, separators=(",", ":")),
        )
        SystemSettingsService.delete_by_exact_name("billing_webhook_event_checkpoint_event_id")
    except Exception:
        logging.warning("Failed to update billing_webhook_event_checkpoint")


def handle_undelivered_events():
    from urllib.parse import urlparse

    webhook_url = settings.BILLING.get("webhook_url", "")
    if urlparse(webhook_url).hostname in ["localhost", "127.0.0.1"]:
        logging.info(
            "Local webhook URL '%s' is unreachable from Stripe; skipping undelivered events replay.",
            webhook_url,
        )
        return

    stripe.api_key = settings.BILLING["stripe_api_key"]
    stripe.api_version = settings.BILLING.get("stripe_api_version", "2026-04-22.dahlia")
    focused_event_types = FOCUSED_STRIPE_WEBHOOK

    checkpoint = _load_webhook_checkpoint()
    ending_before = str(checkpoint.get("id") or "")

    if ending_before:
        try:
            stripe.Event.retrieve(ending_before)
        except stripe.error.InvalidRequestError as exc:
            if "No such" in str(exc):
                logging.warning(
                    "Stripe checkpoint event '%s' no longer exists, resetting checkpoint.",
                    ending_before,
                )
                ending_before = ""
                checkpoint = {"created": 0, "id": ""}

    last_timestamp = int(checkpoint.get("created") or 0) or None
    if last_timestamp:
        start_time = max(0, last_timestamp - 600)
    else:
        start_time = int(time.time()) - 60 * 60 * 24 * 30

    list_kwargs = {
        "delivery_success": False,
        "created": {"gte": start_time},
        "types": focused_event_types,
    }
    if ending_before:
        list_kwargs["ending_before"] = ending_before

    events = stripe.Event.list(**list_kwargs)

    async def _process_events():
        ordered_events = sorted(
            list(events.auto_paging_iter()),
            key=lambda event: (
                int(getattr(event, "created", None) or event.get("created") or 0),
                str(getattr(event, "id", None) or event.get("id") or ""),
            ),
        )
        for event in ordered_events:
            try:
                await handle_billing_webhook_event(event, retry_inflight=True)
            except ValueError as exc:
                event_data = getattr(event, "data", None) or event.get("data") or {}
                event_object = getattr(event_data, "object", None) or event_data.get("object") or {}
                logging.warning(
                    "Skip invalid undelivered Stripe event during startup replay: event_id=%s type=%s event_created=%s object_id=%s customer_id=%s subscription_id=%s error=%s",
                    getattr(event, "id", None) or event.get("id") or "",
                    getattr(event, "type", None) or event.get("type") or "",
                    getattr(event, "created", None) or event.get("created") or "",
                    getattr(event_object, "id", None) or event_object.get("id") or "",
                    getattr(event_object, "customer", None) or event_object.get("customer") or "",
                    getattr(event_object, "subscription", None) or event_object.get("subscription") or "",
                    exc,
                )
            except stripe.error.InvalidRequestError as exc:
                event_data = getattr(event, "data", None) or event.get("data") or {}
                event_object = getattr(event_data, "object", None) or event_data.get("object") or {}
                logging.warning(
                    "Skip invalid undelivered Stripe event during startup replay due to missing/invalid remote resource: event_id=%s type=%s event_created=%s object_id=%s customer_id=%s subscription_id=%s error=%s",
                    getattr(event, "id", None) or event.get("id") or "",
                    getattr(event, "type", None) or event.get("type") or "",
                    getattr(event, "created", None) or event.get("created") or "",
                    getattr(event_object, "id", None) or event_object.get("id") or "",
                    getattr(event_object, "customer", None) or event_object.get("customer") or "",
                    getattr(event_object, "subscription", None) or event_object.get("subscription") or "",
                    exc,
                )
            except Exception:
                event_data = getattr(event, "data", None) or event.get("data") or {}
                event_object = getattr(event_data, "object", None) or event_data.get("object") or {}
                logging.exception(
                    "Skip undelivered Stripe event during startup replay after handler failure: event_id=%s type=%s event_created=%s object_id=%s customer_id=%s subscription_id=%s",
                    getattr(event, "id", None) or event.get("id") or "",
                    getattr(event, "type", None) or event.get("type") or "",
                    getattr(event, "created", None) or event.get("created") or "",
                    getattr(event_object, "id", None) or event_object.get("id") or "",
                    getattr(event_object, "customer", None) or event_object.get("customer") or "",
                    getattr(event_object, "subscription", None) or event_object.get("subscription") or "",
                )

    asyncio.run(_process_events())


async def _handle_payment_intent_succeeded(event: dict):
    event_data = event["data"]["object"]

    try:
        intent = IntentSucceed(**event_data)
    except ValidationError as exc:
        logging.warning("IntentSucceed data validation failed: %s", exc)
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
        quantity_unit = ""

    if not intent_metadata or price_type != PriceType.ADDON:
        logging.info("%s triggered %s product %s in intent succeeded, skipped. May handle in subscription.paid.", tenant_id, price_type, product_name)
        return

    from api.db.services.billing_service import ProductService

    valid_price_ids = []
    latest_addon_products = ProductService.get_latest_by_type(ProductType.ADDON)
    for product in latest_addon_products:
        if product.price_ids:
            valid_price_ids.extend(product.price_ids.split())

    if price_id not in valid_price_ids:
        logging.info("%s triggered price_type %s product %s with unhandled price_id %s, skipped.", tenant_id, price_type, product_name, price_id)
        return

    amount_cents = intent.amount
    amount_received = intent.amount_received
    currency = intent.currency
    order_id = intent.id
    payment_intent_id = intent.id
    stripe_status = intent.status
    payment_status = normalize_stripe_payment_intent_status(stripe_status)
    latest_charge_id = intent.latest_charge_id or ""
    receipt_url = await get_receipt_url_from_intent_latest_charge_async(latest_charge_id) if latest_charge_id else ""
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
        "payment_method": PaymentMethod.CARD,
        "order_id": order_id,
        "payment_intent_id": payment_intent_id,
        "receipt_url": receipt_url,
        "payment_channel": PaymentChannel.STRIPE,
        "payment_status": payment_status,
        "stripe_status": stripe_status,
        "paid": bool(amount_received),
        "captured": bool(amount_received),
        "description": "",
        "order_created_at": intent.created,
        "payment_detail": {"quantity": quantity, "quantity_unit": quantity_unit, "quota_quantity": quota_quantity, "quota_unit": quota_unit},
    }

    purchased_overview = PurchasedProductOverviewService.get_by_product_name_and_tenant_id(product_name, tenant_id)
    if PaymentOrderService.get_by_payment_intent_id(payment_intent_id):
        logging.info("Skip duplicated payment_intent for tenant %s: %s", tenant_id, payment_intent_id)
        return

    expiry_dt = to_utc_datetime(expiry_time) if expiry_time else None
    with DB.atomic():
        PaymentOrderService.save(**payment_order)
        if not purchased_overview:
            PurchasedProductOverviewService.save(
                id=get_uuid(),
                tenant_id=tenant_id,
                product_id=product_id,
                product_name=product_name,
                quantity=quota_quantity,
                effective_time=to_utc_datetime(datetime.now(timezone.utc)),
                expiry_time=expiry_dt,
            )
        else:
            ok = PurchasedProductOverviewService.update_quantity(product_name, tenant_id, quota_quantity)
            if not ok:
                logging.warning("Customer %s with tenant_id %s, purchased %s %s, but update to purchase overview failed.", customer_id, tenant_id, quantity, product_name)
            if expiry_dt:
                prev_expiry = to_utc_datetime(purchased_overview.get("expiry_time"))
                if not prev_expiry or expiry_dt > prev_expiry:
                    PurchasedProductOverviewService.model.update(expiry_time=expiry_dt).where(
                        (PurchasedProductOverviewService.model.product_name == product_name)
                        & (PurchasedProductOverviewService.model.tenant_id == tenant_id)
                    ).execute()


async def _handle_checkout_session_completed(event: dict):
    event_data = event["data"]["object"]
    try:
        checkout_session_completed = CheckoutSessionCompleted(**event_data)
    except ValidationError as exc:
        logging.warning("CheckoutSessionCompleted data validation failed: %s", exc)
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
                logging.warning("checkout.session.completed(points_recharge) invalid points_amount for tenant %s.", tenant_id)
                return
            idempotency_key = f"checkout:{checkout_session_completed.id}"
            PointAccountService.recharge(
                tenant_id=tenant_id,
                points=points,
                idempotency_key=idempotency_key,
                description="Stripe checkout recharge",
                metadata={"session_id": checkout_session_completed.id},
            )
            customer_id = (checkout_session_completed.customer_id or "").strip()
            amount_cents = checkout_session_completed.amount_total or 0
            currency = checkout_session_completed.currency or "usd"
            receipt_url = ""
            payment_intent_id = checkout_session_completed.payment_intent_id or ""
            if payment_intent_id:
                payment_intent = await stripe.PaymentIntent.retrieve_async(payment_intent_id)
                charges = extract_list_data(get_attr_or_item(payment_intent, "charges", None))
                if charges:
                    receipt_url = (get_attr_or_item(charges[0], "receipt_url", "") or "").strip()
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
        logging.info("checkout.session.completed subscription mode for tenant %s: handled by customer.subscription.updated", tenant_id)
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
        setup_intent = await stripe.SetupIntent.retrieve_async(setup_intent_id)
        payment_method_id = (getattr(setup_intent, "payment_method", None) or "").strip()
        if not payment_method_id:
            logging.warning("checkout.session.completed(setup) missing payment_method for setup_intent_id=%s", setup_intent_id)
            return
        await stripe.Customer.modify_async(customer_id, invoice_settings={"default_payment_method": payment_method_id})
        logging.info(
            "checkout.session.completed(setup) saved default payment method for customer %s (tenant_id=%s)",
            customer_id,
            (metadata.get("tenant_id") or "").strip(),
        )


async def _resolve_tenant_context_for_invoice_line(*, invoice_paid: InvoicePaid, item, tenant_id: str, customer_id: str, subscription_id: str) -> tuple[str, str]:
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
            stripe_subscription = await stripe.Subscription.retrieve_async(resolved_subscription_id)

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


async def _handle_invoice_paid(event: dict):
    event_data = event["data"]["object"]
    try:
        invoice_paid = InvoicePaid(**event_data)
    except ValidationError as exc:
        logging.warning("InvoicePaid data validation failed: %s", exc)
        return

    line_items = extract_list_data(get_attr_or_item(invoice_paid, "lines", None))
    customer_id = invoice_paid.customer_id
    tenant_id = (invoice_paid.metadata or {}).get("tenant_id", "") if invoice_paid.metadata else ""
    if not tenant_id:
        tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id)

    # Guard: skip invoice.paid for Trial tenant renewals (Phase 1C - Plan B).
    # Allow upgrade invoices (price_id belongs to a paid plan) even if the
    # subscription object still references Trial — Stripe delivers upgrade
    # invoices before the subscription swap is complete.
    invoice_subscription_id = getattr(invoice_paid, "subscription", None) or ""
    if invoice_subscription_id:
        sub = SubscriptionService.get_by_subscription_id(invoice_subscription_id)
        if sub and is_trial_plan_name(sub.get("plan_name", "")):
            # Check whether this invoice is for a paid plan upgrade (Starter/Pro/etc).
            # If so, it is NOT a Trial renewal — let it through to create the
            # billing history row for the upgrade.
            _invoice_price_id = (invoice_paid.metadata or {}).get("price_id", "") if invoice_paid.metadata else ""
            if not _invoice_price_id:
                line_items = extract_list_data(get_attr_or_item(invoice_paid, "lines", None))
                if line_items:
                    first_price = getattr(line_items[0], "price", None) or {}
                    _invoice_price_id = getattr(first_price, "id", "") or ""
            if _invoice_price_id and not is_trial_plan_name(settings.BILLING_PRICEID_TO_PRODUCT.get(_invoice_price_id, "")):
                logging.info(
                    "Allowing upgrade invoice.paid for Trial subscription %s: invoice price_id=%s is a paid plan",
                    invoice_subscription_id,
                    _invoice_price_id,
                )
            else:
                logging.info("Skipping invoice.paid for Trial subscription %s", invoice_subscription_id)
                return

    order_id = invoice_paid.id
    stripe_status = invoice_paid.status or ""
    status = normalize_stripe_invoice_status(stripe_status)
    invoice_url = invoice_paid.hosted_invoice_url or ""
    invoice_pdf_url = invoice_paid.invoice_pdf or ""
    aggregated_product_ids = []
    aggregated_product_names = []
    aggregated_product_quantities = []
    aggregated_product_amount_cents = []
    aggregated_price_ids = []
    aggregated_descriptions = []
    aggregated_payment_details = []
    storage_subscription_id = ""
    resolved_tenant_id = tenant_id
    resolved_customer_id = customer_id

    for item in line_items:
        item_description = item.description or ""
        item_subscription_detail = get_nested_attr_or_item(item, "parent", "subscription_item_details")
        item_subscription_id = get_attr_or_item(item_subscription_detail, "subscription", "") if item_subscription_detail else ""
        item_price_id = getattr(getattr(getattr(item, "pricing", None), "price_details", None), "price", "") or ((invoice_paid.metadata or {}).get("price_id", "") if invoice_paid.metadata else "")
        item_tenant_id, item_customer_id = await _resolve_tenant_context_for_invoice_line(
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
        aggregated_descriptions.append(((invoice_paid.description or invoice_paid.billing_reason or "") + f" {item_description}").strip())
        if item_is_storage:
            aggregated_payment_details.append({"type": "storage", "quantity": item_quantity})
            if item_subscription_id:
                storage_subscription_id = item_subscription_id
        else:
            aggregated_payment_details.append({"type": "plan", "quantity": item_quantity})

    if storage_subscription_id:
        stripe_subscription = await stripe.Subscription.retrieve_async(storage_subscription_id)
        _sync_storage_subscription_record(resolved_tenant_id, stripe_subscription, customer_id=resolved_customer_id)

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
        "order_created_at": invoice_paid.created,
        "payment_detail": {"line_items": aggregated_payment_details},
    }

    if existing_order and existing_order.get("id"):
        if existing_order.get("payment_status") != PaymentStatus.SUCCESS.value:
            payment_order.pop("id", None)
            PaymentOrderService.update_by_order_id(order_id, payment_order)
        else:
            logging.info("invoice.paid payment_order already successful for tenant %s: %s", resolved_tenant_id, order_id)
    else:
        try:
            PaymentOrderService.save(**payment_order)
        except IntegrityError:
            logging.info("Skip duplicated invoice.paid payment_order for tenant %s: %s", resolved_tenant_id, order_id)


def _period_changed(previous_start, previous_end, current_start, current_end) -> bool:
    if not previous_start or not previous_end or not current_start or not current_end:
        return False
    return int(previous_start.timestamp()) != int(current_start.timestamp()) or int(previous_end.timestamp()) != int(current_end.timestamp())


def _should_apply_subscription_event(
    existing_main_subscription: dict | None,
    *,
    event_subscription_id: str,
    event_status: str,
    event_period_start=None,
    event_period_end=None,
    allow_same_subscription_backfill: bool = False,
) -> bool:
    if not existing_main_subscription:
        return True

    current_subscription_id = (existing_main_subscription.get("subscription_id") or "").strip()
    if not current_subscription_id:
        return True
    if not event_subscription_id:
        return True

    existing_start = to_utc_datetime(existing_main_subscription.get("start_time"))
    existing_end = to_utc_datetime(existing_main_subscription.get("end_time"))
    current_start = to_utc_datetime(event_period_start)
    current_end = to_utc_datetime(event_period_end)

    if event_subscription_id == current_subscription_id:
        # Stripe may emit a same-subscription snapshot without actionable
        # previous_attributes. In that case we still want to resync the current
        # Stripe state instead of getting stuck on a stale local placeholder.
        if allow_same_subscription_backfill:
            return True
        # Startup replay of Stripe undelivered events can surface older
        # customer.subscription.updated snapshots for the same subscription.
        # Never let an older snapshot roll back a newer local one.
        if existing_end and current_end and current_end < existing_end:
            return False
        if existing_start and current_start and current_start < existing_start and (
            not current_end or (existing_end and current_end <= existing_end)
        ):
            return False
        return True

    if not _is_non_canceled_subscription_status(event_status):
        return False

    current_status = existing_main_subscription.get("subscription_status") or existing_main_subscription.get("status") or ""
    if not _is_non_canceled_subscription_status(current_status):
        return True

    if existing_end and current_end and current_end < existing_end:
        return False
    if existing_start and current_start and current_start < existing_start and (not current_end or (existing_end and current_end <= existing_end)):
        return False
    return True


def _is_same_billing_period(existing_main_subscription: dict | None, event_period_start=None, event_period_end=None) -> bool:
    if not existing_main_subscription:
        return False
    existing_start = to_utc_datetime(existing_main_subscription.get("start_time"))
    existing_end = to_utc_datetime(existing_main_subscription.get("end_time"))
    current_start = to_utc_datetime(event_period_start)
    current_end = to_utc_datetime(event_period_end)
    return bool(
        existing_start
        and existing_end
        and current_start
        and current_end
        and existing_start == current_start
        and existing_end == current_end
    )


async def _handle_storage_subscription_updated(subscription_updated: SubscriptionUpdated):
    subscription = get_nested_attr_or_item(subscription_updated, "data", "object")
    subscription_id = get_attr_or_item(subscription, "id", "")
    customer_id = get_attr_or_item(subscription, "customer_id", "") or get_attr_or_item(subscription, "customer", "")
    tenant_id = (get_attr_or_item(subscription, "metadata", {}) or {}).get("tenant_id", "")
    if not tenant_id:
        plan_sub = SubscriptionService.get_by_subscription_id(subscription_id) or {}
        tenant_id = plan_sub.get("tenant_id", "")
    if not tenant_id:
        tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id)
    if not tenant_id:
        logging.warning("Skip storage subscription.updated without tenant context: %s", subscription_id)
        return
    _sync_storage_subscription_record(tenant_id, subscription, customer_id=customer_id)


async def _handle_customer_subscription_updated(event: dict):
    event_type = event.get("type", "")
    logging.info("Handling %s", event_type)

    # customer.subscription.created does not match SubscriptionUpdated's type literal,
    # so we extract the subscription_id directly from the event payload.
    event_data_object = (event.get("data") or {}).get("object") or {}
    subscription_id = get_attr_or_item(event_data_object, "id", "")
    if not subscription_id:
        logging.warning("Skip %s without subscription_id in event payload.", event_type)
        return

    try:
        subscription = await stripe.Subscription.retrieve_async(subscription_id)
        logging.info(
            "Using authoritative Stripe subscription state for %s: subscription_id=%s",
            event_type,
            subscription_id,
        )
    except Exception:
        logging.exception(
            "Failed to retrieve authoritative Stripe subscription for %s: subscription_id=%s",
            event_type,
            subscription_id,
        )
        return
    customer_id = get_attr_or_item(subscription, "customer_id", "") or get_attr_or_item(subscription, "customer", "")
    tenant_id = (get_attr_or_item(subscription, "metadata", {}) or {}).get("tenant_id", "")
    if not tenant_id:
        tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id)
    if not tenant_id:
        logging.warning("Skip %s without tenant context: subscription_id=%s", event_type, subscription_id)
        return

    existing_main_subscription = SubscriptionService.get_raw_by_tenant_id(tenant_id) if tenant_id else {}
    previous_main_start = to_utc_datetime(existing_main_subscription.get("start_time")) if existing_main_subscription else None
    previous_main_end = to_utc_datetime(existing_main_subscription.get("end_time")) if existing_main_subscription else None
    previous_subscription_id = (existing_main_subscription.get("subscription_id", "") or "").strip() if existing_main_subscription else ""
    previous_addon_item_id = (existing_main_subscription.get("addon_subscription_item_id", "") or "").strip() if existing_main_subscription else ""
    previous_addon_storage_bytes = safe_int(existing_main_subscription.get("addon_storage_bytes", 0), 0) if existing_main_subscription else 0
    subscription_items = extract_subscription_items_data(subscription)
    if not subscription_items:
        logging.warning("subscription.%s missing subscription items; running fallback sync.", event_type.replace("customer.", ""))
        _sync_main_subscription_from_stripe(
            tenant_id=tenant_id,
            stripe_subscription=subscription,
            subscription_status=_normalize_subscription_status(subscription.status),
            invoice_id=extract_latest_invoice_id(subscription),
        )
        return

    latest_invoice_id = extract_latest_invoice_id(subscription)
    subscription_status = _normalize_subscription_status(subscription.status)
    current_period_start, current_period_end = extract_subscription_period(subscription)
    if not current_period_start or not current_period_end:
        logging.warning(
            "subscription.%s authoritative Stripe state missing current period boundaries: subscription_id=%s",
            event_type.replace("customer.", ""),
            subscription_id,
        )
        return

    latest_invoice_paid, resolved_invoice_id, _resolved_invoice_status, resolved_invoice_url = await is_subscription_latest_invoice_paid_async(subscription)
    preserve_existing_plan = _should_preserve_existing_plan_for_unpaid_upgrade(existing_main_subscription, subscription, latest_invoice_paid)

    # Capture pre-sync price_id for plan_changed detection (needed by the
    # final-defense check below).
    _, new_price_id, _ = extract_plan_subscription_item(subscription)
    old_price_id = (existing_main_subscription.get("price_id") or "").strip() if existing_main_subscription else ""
    plan_changed = bool(old_price_id) and old_price_id != (new_price_id or "").strip()

    _sync_main_subscription_from_stripe(
        tenant_id=tenant_id,
        stripe_subscription=subscription,
        subscription_status=subscription_status,
        invoice_id=latest_invoice_id or resolved_invoice_id,
        invoice_url=resolved_invoice_url,
        preserve_existing_plan=preserve_existing_plan,
    )

    # ── Webhook final defense: detect downgrade effective but quota exceeded ──
    try:
        from api.services.downgrade_guard import check_downgrade_effective_exceeded, _inc_metric

        exceed_info = check_downgrade_effective_exceeded(
            tenant_id, existing_main_subscription, plan_changed=plan_changed,
        )
        if exceed_info:
            logging.critical(
                "DOWNGRADE EFFECTIVE BUT QUOTA EXCEEDED: tenant=%s old_plan=%s "
                "target_plan=%s storage=%d limit=%d members=%d apps=%d",
                tenant_id,
                (existing_main_subscription.get("plan_name") or "").strip(),
                (existing_main_subscription.get("target_plan_name") or ""),
                exceed_info.get("storage_used", 0) or 0,
                exceed_info.get("storage_limit", 0) or 0,
                exceed_info.get("members_used", 0) or 0,
                exceed_info.get("apps_used", 0) or 0,
            )
            _inc_metric("webhook_violations_total")
            target_plan = (existing_main_subscription.get("target_plan_name") or "") if existing_main_subscription else ""
            old_plan = (existing_main_subscription.get("plan_name") or "").strip() if existing_main_subscription else ""
            await _send_downgrade_effective_exceeded_email(
                tenant_id, old_plan, target_plan, exceed_info,
            )
    except Exception:
        logging.exception("Webhook final defense check failed for tenant %s", tenant_id)

    try:
        pending_change = await get_pending_subscription_change_async(subscription_id)
        logging.info(
            "Resolved pending subscription change from authoritative Stripe state: tenant_id=%s subscription_id=%s pending_change=%s",
            tenant_id,
            subscription_id,
            pending_change,
        )
    except Exception:
        logging.exception(
            "Failed to resolve pending subscription change from Stripe schedule: tenant_id=%s subscription_id=%s",
            tenant_id,
            subscription_id,
        )

    storage_item_id, _storage_price_id, _storage_qty = extract_storage_subscription_item(subscription)
    if storage_item_id:
        await _handle_storage_subscription_updated(subscription)
    elif subscription_id == previous_subscription_id and (previous_addon_item_id or previous_addon_storage_bytes > 0):
        logging.info("Storage item absent from subscription.%s for tenant %s; clearing storage fields in DB.", event_type.replace("customer.", ""), tenant_id)
        with DB.atomic():
            Subscription.update(addon_subscription_item_id=None, addon_storage_bytes=0, target_storage_bytes=0).where(Subscription.tenant_id == tenant_id).execute()

    if tenant_id and _period_changed(previous_main_start, previous_main_end, current_period_start, current_period_end):
        # Guard: skip quota reset for Trial tenants (Phase 1B - Plan B)
        tenant_sub = SubscriptionService.get_by_tenant_id(tenant_id)
        if tenant_sub and is_trial_plan_name(tenant_sub.get("plan_name", "")):
            logging.info("Trial plan tenant %s period changed, skipping quota reset", tenant_id)
        else:
            PointAccountService.reset_plan_consumed_points_at_cycle_start(tenant_id)


async def _send_downgrade_effective_exceeded_email(
    tenant_id: str, old_plan: str, target_plan: str | None, exceed_info: dict,
) -> None:
    from api.services.downgrade_guard import _send_guard_email

    try:
        await _send_guard_email(
            tenant_id,
            {"plan_name": old_plan, "target_plan_name": target_plan},
            exceed_info,
            template_key="downgrade_effective_exceeded",
            subject="Downgrade Effective — Usage Exceeds New Quota",
        )
        logging.info("Sent downgrade-effective-exceeded email to tenant %s", tenant_id)
    except Exception:
        logging.exception("Failed to send downgrade-effective-exceeded email to tenant %s", tenant_id)


async def _handle_customer_subscription_deleted(event: dict):
    event_data = event["data"]["object"]
    subscription_id = (event_data.get("id") or "").strip() if isinstance(event_data, dict) else ""
    customer_id = (event_data.get("customer") or "").strip() if isinstance(event_data, dict) else ""
    tenant_id = SubscriptionService.get_tenant_id_by_customer_id(customer_id) if customer_id else ""
    if tenant_id:
        existing = SubscriptionService.get_by_tenant_id(tenant_id)
        if existing:
            # Guard: skip fallback for Trial tenants (Phase 1D - Plan B)
            if is_trial_plan_name(existing.get("plan_name", "")):
                logging.info(
                    "customer.subscription.deleted for Trial tenant %s: no fallback needed, subscription already local",
                    tenant_id,
                )
                return
            current_subscription_id = (existing.get("subscription_id") or "").strip()
            if subscription_id and current_subscription_id and subscription_id != current_subscription_id:
                logging.info(
                    "Skip stale customer.subscription.deleted for tenant %s: event subscription %s does not match current main subscription %s.",
                    tenant_id,
                    subscription_id,
                    current_subscription_id,
                )
                return
            trial_subscription = SubscriptionService._build_trial_subscription(
                tenant_id,
                customer_id or existing.get("customer_id", ""),
                existing.get("original_subscription_id", "") or ""
            )
            SubscriptionService.upsert_subscription(tenant_id, trial_subscription)
            PointAccountService.reset_plan_consumed_points_at_cycle_start(tenant_id)
            _sync_tenant_rate_limit_for_subscription(
                tenant_id=tenant_id,
                plan_name=trial_subscription.get("plan_name"),
                subscription_status=trial_subscription.get("subscription_status"),
            )
        logging.info("Handled main customer.subscription.deleted by reverting tenant %s to Trial", tenant_id)
        return
    logging.info("customer.subscription.deleted without tenant context: subscription_id=%s", subscription_id)


def _default_event_handlers():
    return {
        PAYMENT_INTENT_SUCCEEDED: _handle_payment_intent_succeeded,
        INVOICE_FAILED: partial(
            _handle_main_subscription_invoice_not_paid,
            description="Main subscription invoice payment failed",
        ),
        INVOICE_PAYMENT_ACTION_REQUIRED: partial(
            _handle_main_subscription_invoice_not_paid,
            description="Main subscription invoice payment action required",
        ),
        CHECKOUT_SESSION_COMPLETED: _handle_checkout_session_completed,
        INVOICE_PAID: _handle_invoice_paid,
        SUBSCRIPTION_CREATED: _handle_customer_subscription_updated,
        SUBSCRIPTION_UPDATED: _handle_customer_subscription_updated,
        SUBSCRIPTION_DELETED: _handle_customer_subscription_deleted,
    }


async def handle_billing_webhook_event(event, retry_inflight=False):
    handlers = _default_event_handlers()
    event_type = event["type"]
    event_data = event["data"]
    event_data_object = event_data["object"]
    event_payment_type = event_data_object["object"]
    event_id = event.get("id", "")
    payload_created = event.get("created")
    payload_created_at = to_utc_datetime(payload_created) if payload_created else None
    object_id = event_data_object.get("id", "")
    payload = event
    if not isinstance(event, dict):
        if hasattr(event, "to_dict"):
            payload = event.to_dict()
        else:
            payload = json.loads(json.dumps(event))
    received_at = to_utc_datetime(datetime.now(timezone.utc))

    if event_id:
        should_process = _prepare_webhook_event_processing(
            event_id=event_id,
            event_type=event_type,
            object_id=object_id,
            payload=payload,
            payload_created_at=payload_created_at,
            received_at=received_at,
            payload_created=payload_created,
            retry_inflight=retry_inflight,
        )
        if not should_process:
            logging.info(
                "Webhook event skipped (duplicate or already processed): event_id=%s event_type=%s object_id=%s",
                event_id,
                event_type,
                object_id,
            )
            return

    handler = handlers.get(event_type)
    if not handler:
        logging.warning("Unhandled Stripe event: type=%s, object=%s", event_type, event_payment_type)
        if event_id:
            BillingWebhookEventService.mark_completed(event_id, WEBHOOK_EVENT_STATUS_UNHANDLED)
        _update_webhook_event_checkpoint(payload_created, event_id=event_id)
        return

    handler_start = time.monotonic()
    handler_error: str | None = None
    try:
        await handler(event)
    except Exception as exc:
        handler_error = str(exc)
        if event_id:
            BillingWebhookEventService.mark_failed(event_id, handler_error)
        raise
    finally:
        handler_duration_ms = (time.monotonic() - handler_start) * 1000
        if event_id:
            if handler_error:
                logging.warning(
                    "Webhook handler failed: event_id=%s event_type=%s object_id=%s duration_ms=%.1f error=%s",
                    event_id,
                    event_type,
                    object_id,
                    handler_duration_ms,
                    handler_error,
                )
            else:
                logging.info(
                    "Webhook handler completed: event_id=%s event_type=%s object_id=%s duration_ms=%.1f",
                    event_id,
                    event_type,
                    object_id,
                    handler_duration_ms,
                )
                BillingWebhookEventService.mark_completed(event_id)
                _update_webhook_event_checkpoint(payload_created, event_id=event_id)
        else:
            if handler_error:
                logging.warning(
                    "Webhook handler failed (no event_id): event_type=%s object_id=%s duration_ms=%.1f error=%s",
                    event_type,
                    object_id,
                    handler_duration_ms,
                    handler_error,
                )
            else:
                logging.info(
                    "Webhook handler completed (no event_id): event_type=%s object_id=%s duration_ms=%.1f",
                    event_type,
                    object_id,
                    handler_duration_ms,
                )
