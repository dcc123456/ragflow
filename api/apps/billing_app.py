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
import uuid
from datetime import datetime, timezone
from decimal import Decimal

import stripe
from peewee import IntegrityError
from pydantic import ValidationError
from quart import jsonify, redirect, request

from api.apps import current_user, login_required
from api.db import PaymentChannel, PaymentMethod, PaymentStatus, PriceType, ProductType, SubscriptionStatus
from api.db.db_models import DB, PaymentOrder, ProductUsageTracing
from api.db.services.billing_service import (
    BillingWebhookEventService,
    LocalPriceService,
    PaymentOrderService,
    PricePointService,
    ProductService,
    PurchasedProductOverviewService,
    SubscriptionService,
    UsageBasedService,
)
from api.utils.api_utils import get_data_error_result, get_json_result, get_request_json, server_error_response
from api.utils.billing import (
    billing_set_customer_id_async,
    cents_to_decimal,
    create_or_get_portal_configuration,
    get_plans_equal_or_higher,
    get_product_ids_for_prices,
    get_receipt_url_from_intent_latest_charge,
    get_trial_price_id,
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
    usage_based_status_from_payment_status,
)
from common.constants import RetCode
from common.misc_utils import get_uuid
from rag.utils.redis_conn import REDIS_CONN

# subscription
INVOICE_PAID = "invoice.paid"  # store 'subscription.id' and 'customer.id'verification.
INVOICE_FAILED = "invoice.payment_failed"  #  notify customers and send them to the customer portal to update their payment method.
CHECKOUT_SESSION_COMPLETED = "checkout.session.completed"
# SUBSCRIPTION_CREATED = "customer.subscription.created"
SUBSCRIPTION_UPDATED = "customer.subscription.updated"
# SUBSCRIPTION_DELETED = "customer.subscription.deleted"
# one-off
PAYMENT_INTENT_SUCCEEDED = "payment_intent.succeeded"

FOCUSED_STRIPE_WEBHOOK = [INVOICE_PAID, INVOICE_FAILED, SUBSCRIPTION_UPDATED, CHECKOUT_SESSION_COMPLETED, PAYMENT_INTENT_SUCCEEDED]
PLANS_CACHE_KEY = settings.BILLING.get("plans_cache_key", "saas:billing:plans:latest")
PLANS_CACHE_TTL_SECONDS = settings.BILLING.get("plans_cache_ttl_seconds", 60 * 60 * 24)
USAGE_BASED_PLANS_CACHE_KEY = settings.BILLING.get("usage_based_plans_cache_key", "saas:billing:usage_based:latest")


def _billing_disabled_response():
    return get_data_error_result(message="Billing is disabled.")


def _billing_disabled_webhook_response():
    logging.info("Billing disabled; ignoring Stripe webhook.")
    return jsonify(success=True)


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
    return get_json_result(data=tenant_plan)


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
                "requests_per_minute": _get_api_request_limit_by_plan(tenant_plan.get("plan_name", "trial")),
            },
        }

        purchased_products = PurchasedProductOverviewService.query(tenant_id=tenant_id)
        now_utc = datetime.now(timezone.utc)
        storage_related_add_on = []
        add_on_storage_quantity = 0
        for product in purchased_products:
            expiry_time = to_utc_datetime(getattr(product, "expiry_time", None))
            if expiry_time and expiry_time < now_utc:
                continue
            product_name = product.product_name.lower() if product.product_name else ""
            quantity = product.quantity if product.quantity else 0
            if "storage" in product_name:
                storage_related_add_on.append(product)
                add_on_storage_quantity += quantity

        if add_on_storage_quantity:
            plan_overview["resources"]["add_on_storage"]["limit"] = add_on_storage_quantity

        if storage_related_add_on and add_on_storage_quantity and storage_used_kb > storage_limit_kb:
            plan_overview["resources"]["plan_storage"]["used"] = storage_limit_kb
            plan_overview["resources"]["add_on_storage"]["used"] = storage_used_kb - storage_limit_kb

        return get_json_result(data=plan_overview)
    except Exception as e:
        return server_error_response(e)


def _get_api_request_limit_by_plan(plan_name: str, limit_type: str = "minute") -> int:
    """
    Get API request limits based on plan type.
    This is a placeholder implementation that should be replaced with actual business logic.

    Args:
        plan_name: Name of the plan (e.g., "trial", "level1", "level2", "enterprise")
        limit_type: Type of limit ("minute" or "daily")

    Returns:
        Request limit as integer
    """
    limits = {
        "trial": {"minute": 10},
        "level1": {"minute": 100},
        "level2": {"minute": 500},
        "enterprise": {"minute": 1000},
    }

    plan_limits = limits.get(plan_name, limits["trial"])
    return plan_limits.get(limit_type, 100 if limit_type == "minute" else 10000)


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

            elif "storage" in product_name:
                usage_overview["storage"]["purchased"] += quantity
                usage_overview["storage"]["remaining"] += quantity

            elif "token" in product_name:
                usage_overview["tokens"]["purchased"] += quantity
                usage_overview["tokens"]["remaining"] += quantity

        num_kb_storage = stripe.FileService.get_total_size_by_tenant_id(tenant_id)
        usage_overview["storage"]["remaining"] -= num_kb_storage

        subscription = SubscriptionService.get_by_tenant_id(tenant_id)
        cycle_start = subscription.get("start_time")
        cycle_end = subscription.get("end_time")
        start_ms = int(to_utc_datetime(cycle_start).timestamp() * 1000) if cycle_start else None
        end_ms = int(to_utc_datetime(cycle_end).timestamp() * 1000) if cycle_end else None

        payg_threshold = Decimal("10")
        deepdoc_product = ProductService.get_by_name("deepdoc") or {}
        deepdoc_product_id = deepdoc_product.get("id")
        deepdoc_currency = None

        paid_pages = Decimal("0")
        unpaid_pages = Decimal("0")
        paid_amount = Decimal("0")
        unpaid_amount = Decimal("0")

        if deepdoc_product_id:
            query = ProductUsageTracing.select(
                ProductUsageTracing.task_quantity,
                ProductUsageTracing.total_cost,
                ProductUsageTracing.currency,
                ProductUsageTracing.status,
                ProductUsageTracing.create_time,
            ).where(
                ProductUsageTracing.tenant_id == tenant_id,
                ProductUsageTracing.product_id == deepdoc_product_id,
            )
            if start_ms is not None and end_ms is not None:
                query = query.where(ProductUsageTracing.create_time.between(start_ms, end_ms))

            for record in query:
                qty = decimal_amount(record.task_quantity)
                amount = decimal_amount(record.total_cost)
                if deepdoc_currency is None and record.currency:
                    deepdoc_currency = record.currency
                if record.status == "success":
                    paid_pages += qty
                    paid_amount += amount
                elif record.status == "waiting":
                    unpaid_pages += qty
                    unpaid_amount += amount

        usage_overview["payg"] = {
            "billing_cycle": {
                "start": to_utc_date_str(cycle_start),
                "end": to_utc_date_str(cycle_end),
            },
            "deepdoc": {
                "pages_total": int(paid_pages + unpaid_pages),
                "pages_paid": int(paid_pages),
                "pages_unpaid": int(unpaid_pages),
                "amount_paid": amount_to_float(paid_amount),
                "amount_unpaid": amount_to_float(unpaid_amount),
                "threshold": amount_to_float(payg_threshold),
                "currency": deepdoc_currency.upper() if deepdoc_currency else "USD",
            },
        }

        return get_json_result(data=usage_overview)
    except Exception as e:
        return server_error_response(e)


@manager.route("/spend_overview", methods=["GET"])  # noqa: F821
@login_required
@billing_enabled_guard(_billing_disabled_response)
async def billing_spend_overview():
    try:
        tenant_id = request.args.get("tenant_id", current_user.id)

        tenant_plan = SubscriptionService.get_by_tenant_id(tenant_id, require_quota_info=True)

        customer_id = tenant_plan["customer_id"]
        if not customer_id:
            return get_data_error_result(message="Internal error, cannot determine customer id")

        start_dt = parse_datetime_arg(request.args.get("start"))
        end_dt = parse_datetime_arg(request.args.get("end"))
        start_ts = int(start_dt.timestamp()) if start_dt else None
        end_ts = int(end_dt.timestamp()) if end_dt else None

        query_filter = {"customer": customer_id}
        if start_ts or end_ts:
            query_filter["created"] = {}
            if start_ts:
                query_filter["created"]["gte"] = start_ts
            if end_ts:
                query_filter["created"]["lte"] = end_ts

        invoices = await stripe.Invoice.list_async(limit=50, **query_filter)

        spend_overview = []
        for inv in invoices:
            # if inv.status != "paid":
            # continue

            spend_overview.append(
                {
                    "invoice_id": inv.id,
                    "amount": inv.amount_paid / 100,
                    "currency": inv.currency.upper() if inv.currency else None,
                    "status": inv.status,
                    "created_at": inv.created,
                    "hosted_invoice_url": inv.hosted_invoice_url,
                    "invoice_pdf_url": inv.invoice_pdf,
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
            price_amount = local_price.get("amount")
            price_currency = local_price.get("currency")
            pricing = {
                "unit": unit,
                "unit_quantity": unit_quantity,
                "price_amount": decimal_amount(price_amount) if price_amount is not None else None,
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
                    PaymentOrder.amount,
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
                order_amount = decimal_amount(order.amount)
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
    if "Trial" not in price_dict:
        price_dict["Trial"] = 0

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
                "quota_api_limits": 100000,
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
            code=RetCode.PARAMETER_ERROR,
        )
    try:
        quantity = int(quantity)
    except (TypeError, ValueError):
        return get_json_result(
            data=False,
            message="Invalid quantity.",
            code=RetCode.PARAMETER_ERROR,
        )
    if quantity <= 0:
        return get_json_result(
            data=False,
            message="Quantity must be a positive integer.",
            code=RetCode.PARAMETER_ERROR,
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
            code=RetCode.PARAMETER_ERROR,
        )
    if payment_type == PriceType.USAGE_BASED and not usage_based_price_id:
        return get_json_result(
            data=False,
            message="Missing required parameters usage_based_price_id.",
            code=RetCode.PARAMETER_ERROR,
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
                    # items = []
                    exists = False
                    print("entering SUBSCRIPTION SECTION")
                    print(f"{subscription_items=}")
                    for item in subscription_items:
                        if item["price"]["id"] == subscription_price_id:
                            exists = True
                            break

                    if exists:
                        msg = f"Tenant {tenant_id} already has an active subscription {subscription_id} on price {subscription_price_id}"
                        return get_json_result(data=False, message=msg, code=RetCode.SUCCESS)

                    if tenant_plan["invoice_url"]:  # valid plan exists
                        current_plan_name = tenant_plan.get("plan_name", "")
                        customer_portal_url = _create_customer_portal(tenant_id, current_plan_name, return_url=session_cancel_url)
                        msg = f"Tenant {tenant_id} already has an active subscription {subscription_id} on price {subscription_price_id}, change plan on customer portal {customer_portal_url}."
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

            usage_metadata = {
                "price_type": PriceType.USAGE_BASED,
                "tenant_id": tenant_id,
                "price_id": usage_based_price_id,
                "product_name": settings.BILLING_PRICEID_TO_PRODUCT.get(usage_based_price_id, ""),
                "quantity": quantity,
            }
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
            code=RetCode.PARAMETER_ERROR,
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

    except stripe.error.StripeError as e:
        logging.error(f"Stripe API error: {e}")
        return return_url
    except Exception as e:
        logging.error(f"Unexpected error: {e}")
        return return_url


@manager.route("/create-portal-session", methods=["POST"])  # noqa: F821
# @login_required // TODO: for testing only
@billing_enabled_guard(_billing_disabled_response)
async def customer_portal():
    req = await get_request_json()
    tenant_id = req.get("tenant_id", "")
    current_plan_name = req.get("current_plan", "")
    return_url = req.get("return_url", settings.BILLING["customer_portal_return_url"])

    # tenant_id = "c3fb861af27a11efa69751e139332ced"  # NOTE: for testing
    # tenant_id = "6fc66a5a415411f0b073c5725c73b90e"  # NOTE: for testing
    current_plan_name = SubscriptionService.get_by_tenant_id(tenant_id).get("plan_name", "")  # NOTE: for testing
    print(f"webhook {tenant_id=}, {current_plan_name=}")
    if not tenant_id or not current_plan_name:
        return get_data_error_result("tenant_id and current plan name need to be provided.")

    subscription = SubscriptionService.get_by_tenant_id(tenant_id)
    print(f"webhook {subscription=}")
    if not subscription:
        return get_data_error_result("Subscription not found.")

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

    except stripe.error.StripeError as e:
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
        except stripe.error.SignatureVerificationError:
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
        # "customer.subscription.deleted": _handle_customer_subscription_deleted,
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
    product_id = ProductService.get_by_name(product_name).get("id", "")

    payment_order = {
        "id": get_uuid(),
        "tenant_id": tenant_id,
        "customer_id": customer_id,
        "payment_type": PriceType.USAGE_BASED,
        "product_id": product_id,
        "product_name": product_name,
        "is_prorated": False,
        "amount": cents_to_decimal(amount_cents, currency),
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
        "payment_detail": {"quantity": quantity},
    }
    print(f"\nintend.succeed parsed payment order {payment_order=}")
    usage_based = {
        "id": get_uuid(),
        "tenant_id": tenant_id,
        "product_id": product_id,
        "product_name": product_name,
        "quantity": quantity,
        "order_id": order_id,
        "status": usage_based_status_from_payment_status(payment_status),
        "customer_id": customer_id,
        "price_id": price_id,
        "payment_id": payment_intent_id,
        "payment_status": payment_status,
        "stripe_status": stripe_status,
    }
    print(f"\nintend.succeed parsed usage_based  {usage_based=}")

    purchased_overview = PurchasedProductOverviewService.get_by_product_name_and_tenant_id(product_name, tenant_id)
    if PaymentOrderService.get_by_payment_intent_id(payment_intent_id):
        logging.info(f"Skip duplicated payment_intent for tenant {tenant_id}: {payment_intent_id}")
        return
    if UsageBasedService.get_by_payment_id(payment_intent_id) or UsageBasedService.get_by_order_id(order_id):
        logging.info(f"Skip duplicated usage_based for tenant {tenant_id}: {payment_intent_id}")
        return

    try:
        expiry_dt = to_utc_datetime(expiry_time) if expiry_time else None
        with DB.atomic():
            PaymentOrderService.save(**payment_order)
            UsageBasedService.save(**usage_based)
            if not purchased_overview:
                purchased_overview_dict = {
                    "id": get_uuid(),
                    "tenant_id": tenant_id,
                    "product_id": product_id,
                    "product_name": product_name,
                    "quantity": quantity,
                    "effective_time": to_utc_datetime(datetime.now(timezone.utc)),
                    "expiry_time": expiry_dt,
                }
                PurchasedProductOverviewService.save(**purchased_overview_dict)
            else:
                ok = PurchasedProductOverviewService.update_quantity(product_name, tenant_id, quantity)
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
        # Handle one-time payment
        pass
    elif checkout_session_completed.mode == "subscription":
        metadata = checkout_session_completed.metadata or {}
        print(f"{checkout_session_completed.metadata=}")
        tenant_id = metadata.get("tenant_id")
        plan_name = metadata.get("product_name")
        price_id = metadata.get("price_id", "")
        if not price_id or not tenant_id:
            logging.warning("checkout.session.completed missing required metadata.")
            return
        product_id = ProductService.get_by_name(plan_name).get("id", "")
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
                items = None
                if isinstance(referred_subscription, dict):
                    items = referred_subscription.get("items", {}).get("data", [])
                else:
                    items = getattr(getattr(referred_subscription, "items", None), "data", []) or []

                if items:
                    item = items[0]
                    if isinstance(item, dict):
                        if not start_time:
                            start_time = to_utc_datetime(item.get("current_period_start"))
                        if not end_time:
                            end_time = to_utc_datetime(item.get("current_period_end"))
                    else:
                        if not start_time:
                            start_time = to_utc_datetime(getattr(item, "current_period_start", None))
                        if not end_time:
                            end_time = to_utc_datetime(getattr(item, "current_period_end", None))
                else:
                    logging.warning("checkout.session.completed missing subscription items for period fallback.")
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

    elif checkout_session_completed.mode == "setup":
        # Handle setup for future payments
        pass
    print(event_data)
    print("\n above is checkout.session.completed")


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
    product_id = ProductService.get_by_name(plan_name).get("id", "")

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
        "amount": cents_to_decimal(amount_cents, currency),
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
    print(f"Handling update for subscription: {subscription_id} ({tenant_id=})")

    if not subscription.items or not subscription.items.data:
        logging.warning("subscription.updated missing subscription items.")
        return

    if previous and (previous.plan or previous.items):
        old_price = None
        new_price = subscription.items.data[0].price
        new_price_id = new_price.id
        product_name = settings.BILLING_PRICEID_TO_PRODUCT.get(new_price_id, "")
        product_id = ProductService.get_by_name(product_name).get("id", "")
        print(f"update subscription {new_price=}, {product_id=}, {product_name=}")

        if previous.items and previous.items.data:
            old_price = previous.items.data[0].price
        elif previous.plan:
            old_price = previous.plan
        else:
            old_price = None

        latest_invoice_id = subscription.latest_invoice_id or ""
        stripe_invoice_status = ""
        invoice_url = ""
        invoice_pdf_url = ""
        invoice_created = subscription_updated.created
        if latest_invoice_id:
            payment_order = PaymentOrderService.get_by_order_id(latest_invoice_id)
            if payment_order:
                invoice_url = payment_order.get("receipt_url", "") or ""
                invoice_pdf_url = payment_order.get("receipt_pdf_url", "") or ""
                invoice_created = payment_order.get("order_created_at") or invoice_created
        print(f"update subscription {latest_invoice_id=}, {invoice_url=}, {invoice_pdf_url=}, {invoice_created=}")

        current_period_start = to_utc_datetime(subscription.items.data[0].current_period_start)
        current_period_end = to_utc_datetime(subscription.items.data[0].current_period_end)

        if old_price:
            if latest_invoice_id and PaymentOrderService.get_by_order_id(latest_invoice_id):
                logging.info(f"Skip duplicated subscription update invoice for tenant {tenant_id}: {latest_invoice_id}")
                return
            payment_order = {
                "id": get_uuid(),
                "tenant_id": tenant_id,
                "customer_id": customer_id,
                "payment_type": PriceType.SUBSCRIPTION,
                "product_id": product_id,
                "product_name": product_name,
                "is_prorated": True,
                "amount": cents_to_decimal(new_price.unit_amount, new_price.currency),
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
                print(f"UPGRADE from {old_price.nickname} to {new_price.nickname}")
                payment_order["description"] = f"Upgrade from {old_price.nickname} to {new_price.nickname}"
                # Additional upgrade-specific logic if needed
            else:
                print(f"DOWNGRADE from {old_price.nickname} to {new_price.nickname}")
                payment_order["description"] = f"Downgrade from {old_price.nickname} to {new_price.nickname}"
                # Additional downgrade-specific logic if needed

            try:
                with DB.atomic():
                    PaymentOrderService.save(**payment_order)
                    SubscriptionService.update_subscription(tenant_id, subscription_dict)
            except Exception as e:
                print(f"Failed to save upgrade/downgrade record: {e}")

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
    print(event_data)
    print("\n above is customer.subscription.delete")
