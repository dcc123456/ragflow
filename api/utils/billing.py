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
import os
import re
from contextvars import ContextVar, Token
from decimal import ROUND_HALF_UP, Decimal
from functools import wraps

import stripe

from common import settings
from common.billing_utils import billing_enabled_guard, normalize_stripe_invoice_status, to_utc_datetime
from common.constants import RetCode
from api.db.services.user_service import TenantService
from rag.utils.redis_conn import REDIS_CONN


def moneyfmt(value, places=2, curr="", sep=",", dp=".", pos="", neg="-", trailneg=""):
    """Convert Decimal to a money formatted string.

    places:  required number of places after the decimal point
    curr:    optional currency symbol before the sign (may be blank)
    sep:     optional grouping separator (comma, period, space, or blank)
    dp:      decimal point indicator (comma or period)
             only specify as blank when places is zero
    pos:     optional sign for positive numbers: '+', space or blank
    neg:     optional sign for negative numbers: '-', '(', space or blank
    trailneg:optional trailing minus indicator:  '-', ')', space or blank

    >>> d = Decimal('-1234567.8901')
    >>> moneyfmt(d, curr='$')
    '-$1,234,567.89'
    >>> moneyfmt(d, places=0, sep='.', dp='', neg='', trailneg='-')
    '1.234.568-'
    >>> moneyfmt(d, curr='$', neg='(', trailneg=')')
    '($1,234,567.89)'
    >>> moneyfmt(Decimal(123456789), sep=' ')
    '123 456 789.00'
    >>> moneyfmt(Decimal('-0.02'), neg='<', trailneg='>')
    '<0.02>'

    """
    q = Decimal(10) ** -places  # 2 places --> '0.01'
    sign, digits, _exp = value.quantize(q).as_tuple()
    result = []
    digits = list(map(str, digits))
    build, next = result.append, digits.pop
    if sign:
        build(trailneg)
    for i in range(places):
        build(next() if digits else "0")
    if places:
        build(dp)
    if not digits:
        build("0")
    i = 0
    while digits:
        build(next())
        i += 1
        if i == 3 and digits:
            i = 0
            build(sep)
    build(curr)
    build(neg if sign else pos)
    return "".join(reversed(result))


CURRENCY_DIVISORS = {
    "usd": 100,
}

BILLING_PLAN_TRIAL_NAME = "Trial"
STORAGE_PRODUCT_NAME = "storage"
STRIPE_TEST_CLOCK_HEADER = "X-Stripe-Test-Clock"
_stripe_test_clock_id_context: ContextVar[str] = ContextVar("stripe_test_clock_id", default="")


def set_stripe_test_clock_id_for_current_context(test_clock_id: str) -> Token[str]:
    return _stripe_test_clock_id_context.set((test_clock_id or "").strip())


def reset_stripe_test_clock_id_for_current_context(token: Token[str]) -> None:
    _stripe_test_clock_id_context.reset(token)


def get_stripe_test_clock_id_for_current_context() -> str:
    return (_stripe_test_clock_id_context.get() or "").strip()


def resolve_stripe_test_clock_id(test_clock_id: str = "") -> str:
    return (test_clock_id or get_stripe_test_clock_id_for_current_context() or os.getenv("STRIPE_TEST_CLOCK_ID") or "").strip()


def is_trial_plan_name(plan_name: str) -> bool:
    return (plan_name or "").strip().lower() == BILLING_PLAN_TRIAL_NAME.lower()


def is_storage_plan_name(plan_name: str) -> bool:
    return (plan_name or "").strip().lower() == STORAGE_PRODUCT_NAME


def split_price_ids(price_ids) -> list[str]:
    if not price_ids:
        return []
    if isinstance(price_ids, str):
        return [price_id.strip() for price_id in price_ids.split() if price_id.strip()]
    if isinstance(price_ids, list):
        return [str(price_id).strip() for price_id in price_ids if str(price_id).strip()]
    return []


def is_storage_price_id(price_id: str) -> bool:
    if not price_id:
        return False
    plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(price_id, "")
    return is_storage_plan_name(plan_name)


def get_storage_price_id_from_config() -> str:
    # local import to avoid circular dependency in module import phase
    from api.db.services.billing_service import ProductService

    storage_product = ProductService.get_by_name(STORAGE_PRODUCT_NAME) or {}
    storage_price_ids = split_price_ids(storage_product.get("price_ids", ""))
    if storage_price_ids:
        return storage_price_ids[0]

    storage_plan_info = settings.BILLING_PLAN_TO_INFO.get(STORAGE_PRODUCT_NAME, {}) or {}
    fallback_price_ids = split_price_ids(storage_plan_info.get("price_ids", []))
    return fallback_price_ids[0] if fallback_price_ids else ""


def safe_int(value, default: int = 0) -> int:
    try:
        return int(value)
    except (TypeError, ValueError):
        return default


def safe_float(value, default: float = 0.0) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def get_product_id_by_name(product_name: str) -> str:
    # local import to avoid circular dependency in module import phase
    from api.db.services.billing_service import ProductService

    product = ProductService.get_by_name(product_name) or {}
    return (product.get("id") or "").strip()


def extract_subscription_item(subscription_obj) -> tuple[str, str, int]:
    if isinstance(subscription_obj, dict):
        data = (subscription_obj.get("items") or {}).get("data", []) or []
        if not data:
            return "", "", 0
        item = data[0]
        price_obj = item.get("price", {}) if isinstance(item, dict) else {}
        return (
            item.get("id", "") if isinstance(item, dict) else "",
            price_obj.get("id", "") if isinstance(price_obj, dict) else "",
            safe_int(item.get("quantity", 0) if isinstance(item, dict) else 0, 0),
        )

    data = getattr(getattr(subscription_obj, "items", None), "data", []) or []
    if not data:
        return "", "", 0
    item = data[0]
    price_obj = getattr(item, "price", None)
    return (
        getattr(item, "id", "") or "",
        getattr(price_obj, "id", "") if price_obj else "",
        safe_int(getattr(item, "quantity", 0), 0),
    )


def extract_subscription_period(subscription_obj):
    """
    Stripe may omit subscription-level current_period_* in some responses.
    Fallback to the first subscription item's period boundaries.
    """
    if isinstance(subscription_obj, dict):
        start = to_utc_datetime(subscription_obj.get("current_period_start"))
        end = to_utc_datetime(subscription_obj.get("current_period_end"))
        if start and end:
            return start, end
        item_data = ((subscription_obj.get("items") or {}).get("data") or [])
        first_item = item_data[0] if item_data else {}
        if isinstance(first_item, dict):
            item_start = to_utc_datetime(first_item.get("current_period_start"))
            item_end = to_utc_datetime(first_item.get("current_period_end"))
            return item_start, item_end
        return None, None

    start = to_utc_datetime(getattr(subscription_obj, "current_period_start", None))
    end = to_utc_datetime(getattr(subscription_obj, "current_period_end", None))
    if start and end:
        return start, end
    item_data = getattr(getattr(subscription_obj, "items", None), "data", []) or []
    first_item = item_data[0] if item_data else None
    item_start = to_utc_datetime(getattr(first_item, "current_period_start", None)) if first_item else None
    item_end = to_utc_datetime(getattr(first_item, "current_period_end", None)) if first_item else None
    return item_start, item_end


def extract_invoice_id_and_status(invoice_obj) -> tuple[str, str]:
    return _extract_invoice_id(invoice_obj), _extract_invoice_status_and_url(invoice_obj)[0]


def extract_invoice_hosted_url(invoice_obj) -> str:
    return _extract_invoice_status_and_url(invoice_obj)[1]


def extract_latest_invoice_obj(subscription_obj):
    if isinstance(subscription_obj, dict):
        return subscription_obj.get("latest_invoice")
    latest_invoice = getattr(subscription_obj, "latest_invoice", None)
    # Some typed webhook models expose Stripe's latest invoice as `latest_invoice_id`.
    # Fallback to that field so payment-state checks don't treat "missing invoice" as paid.
    if latest_invoice:
        return latest_invoice
    return getattr(subscription_obj, "latest_invoice_id", None)


def _extract_invoice_id(invoice_obj) -> str:
    if not invoice_obj:
        return ""
    if isinstance(invoice_obj, str):
        return invoice_obj.strip()
    if isinstance(invoice_obj, dict):
        return (invoice_obj.get("id") or "").strip()
    return (getattr(invoice_obj, "id", "") or "").strip()


def _extract_invoice_status_and_url(invoice_obj) -> tuple[str, str]:
    if not invoice_obj or isinstance(invoice_obj, str):
        return "", ""
    if isinstance(invoice_obj, dict):
        return (
            (invoice_obj.get("status") or "").strip().lower(),
            (invoice_obj.get("hosted_invoice_url") or "").strip(),
        )
    return (
        (getattr(invoice_obj, "status", "") or "").strip().lower(),
        (getattr(invoice_obj, "hosted_invoice_url", "") or "").strip(),
    )


def _is_paid_invoice_status(invoice_status: str) -> bool:
    return normalize_stripe_invoice_status(invoice_status) == "success"


async def is_subscription_latest_invoice_paid_async(subscription_obj) -> tuple[bool, str, str, str]:
    latest_invoice = extract_latest_invoice_obj(subscription_obj)
    invoice_id, invoice_status = extract_invoice_id_and_status(latest_invoice)
    invoice_url = extract_invoice_hosted_url(latest_invoice)
    if not invoice_id:
        # No invoice means there's nothing to collect for this update.
        return True, "", "", ""

    if invoice_status and invoice_url:
        return _is_paid_invoice_status(invoice_status), invoice_id, invoice_status, invoice_url

    try:
        invoice = await stripe.Invoice.retrieve_async(invoice_id)
        invoice_status, invoice_url = _extract_invoice_status_and_url(invoice)
        return _is_paid_invoice_status(invoice_status), invoice_id, invoice_status, invoice_url
    except Exception as e:
        logging.warning(f"Failed to retrieve storage latest invoice {invoice_id}: {e}")
        # Fail closed: keep upgrade pending until webhook confirms payment.
        return False, invoice_id, "", ""


def is_subscription_latest_invoice_paid_sync(subscription_obj) -> bool:
    latest_invoice = extract_latest_invoice_obj(subscription_obj)
    invoice_id, invoice_status = extract_invoice_id_and_status(latest_invoice)
    if not invoice_id:
        return True

    if invoice_status:
        return _is_paid_invoice_status(invoice_status)

    try:
        invoice = stripe.Invoice.retrieve(invoice_id)
        invoice_status, _ = _extract_invoice_status_and_url(invoice)
        return _is_paid_invoice_status(invoice_status)
    except Exception as e:
        logging.warning(f"Failed to retrieve storage latest invoice {invoice_id}: {e}")
        return False


def cents_to_decimal(amount_in_cents: int, currency: str = "usd", decimal_places: int = 2) -> Decimal:
    divisor = CURRENCY_DIVISORS.get(currency.lower(), 100)
    amount = Decimal(amount_in_cents) / Decimal(divisor)
    return amount.quantize(Decimal(f"1.{'0' * decimal_places}"), rounding=ROUND_HALF_UP)


def decimal_to_cents(amount: Decimal, currency: str = "usd", decimal_places: int = 4) -> int:
    divisor = CURRENCY_DIVISORS.get(currency.lower(), 100)
    scaled = amount.quantize(Decimal(f"1.{'0' * decimal_places}"), rounding=ROUND_HALF_UP)
    return int((scaled * Decimal(divisor)).quantize(Decimal("1"), rounding=ROUND_HALF_UP))


def parse_storage_size(size_str: str) -> int:
    if not size_str:
        return 0

    size_str = size_str.strip().lower()

    match = re.match(r"^(\d+(?:\.\d+)?)\s*([kmgt]b?)?$", size_str)
    if not match:
        raise ValueError(f"Invalid storage size format: '{size_str}'")

    num = float(match.group(1))
    unit = (match.group(2) or "b").lower()

    units = {
        "b": 1,
        "k": 1000,
        "kb": 1000,
        "m": 1000**2,
        "mb": 1000**2,
        "g": 1000**3,
        "gb": 1000**3,
        "t": 1000**4,
        "tb": 1000**4,
    }

    if unit not in units:
        raise ValueError(f"Unknown unit '{unit}' in storage size")

    return int(num * units[unit])


def get_trial_price_id(plans: list[dict]) -> str:
    trial_plan = next((plan for plan in plans if plan.get("name") == BILLING_PLAN_TRIAL_NAME and plan.get("price_ids")), None)
    return trial_plan["price_ids"].split()[0] if trial_plan else ""


def get_plan_priority_by_price_id(price_id: str) -> int | None:
    plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(price_id, "")
    if not plan_name:
        return None
    plan_info = settings.BILLING_PLAN_TO_INFO.get(plan_name) or {}
    priority = plan_info.get("priority")
    return priority if isinstance(priority, int) else None


def is_downgrade_by_price_id(current_price_id: str, target_price_id: str) -> bool:
    current_priority = get_plan_priority_by_price_id(current_price_id)
    target_priority = get_plan_priority_by_price_id(target_price_id)
    return current_priority is not None and target_priority is not None and target_priority < current_priority


@billing_enabled_guard({})
async def get_pending_subscription_change_async(subscription_id: str) -> dict:
    if not subscription_id:
        return {}

    subscription = await stripe.Subscription.retrieve_async(subscription_id)
    schedule_id = (subscription.get("schedule") or "").strip()
    if not schedule_id:
        return {}

    schedule = await stripe.SubscriptionSchedule.retrieve_async(schedule_id)
    schedule_status = (schedule.get("status") or "").strip()
    if schedule_status in {"released", "canceled", "completed"}:
        return {}

    phases = schedule.get("phases", []) or []
    if len(phases) < 2:
        return {}

    pending_phase = phases[1]
    pending_items = pending_phase.get("items", []) or []
    if not pending_items:
        return {}

    pending_price_id = pending_items[0].get("price", "") if isinstance(pending_items[0], dict) else ""
    pending_price_id = pending_price_id.get("id", "") if isinstance(pending_price_id, dict) else pending_price_id
    pending_price_id = (pending_price_id or "").strip()
    if not pending_price_id:
        return {}

    effective_at = pending_phase.get("start_date")
    return {
        "schedule_id": schedule_id,
        "pending_price_id": pending_price_id,
        "pending_plan_name": settings.BILLING_PRICEID_TO_PRODUCT.get(pending_price_id, ""),
        "effective_at": to_utc_datetime(effective_at),
    }


@billing_enabled_guard({})
async def schedule_subscription_price_change_at_period_end_async(subscription_id: str, target_price_id: str) -> dict:
    if not subscription_id or not target_price_id:
        return {}

    subscription = await stripe.Subscription.retrieve_async(subscription_id)
    period_start = subscription.get("current_period_start")
    period_end = subscription.get("current_period_end")

    subscription_items = (subscription.get("items") or {}).get("data", []) if isinstance(subscription, dict) else subscription["items"]["data"]
    if not subscription_items:
        return {}

    current_item = subscription_items[0]
    current_price = current_item.get("price", {}) if isinstance(current_item, dict) else current_item["price"]
    current_price_id = current_price.get("id", "") if isinstance(current_price, dict) else (getattr(current_price, "id", "") or "")
    current_quantity = current_item.get("quantity", 1) if isinstance(current_item, dict) else (getattr(current_item, "quantity", 1) or 1)

    schedule_id = (subscription.get("schedule") or "").strip()
    schedule = None
    if schedule_id:
        schedule = await stripe.SubscriptionSchedule.retrieve_async(schedule_id)
        schedule_status = (schedule.get("status") or "").strip()
        if schedule_status in {"released", "canceled", "completed"}:
            schedule_id = ""
            schedule = None

    if not schedule:
        schedule = await stripe.SubscriptionSchedule.create_async(from_subscription=subscription_id)

    phases = schedule.get("phases", []) or []
    current_phase = schedule.get("current_phase") or {}
    phase_start_date = (
        current_phase.get("start_date")
        or (phases[0].get("start_date") if phases else None)
        or schedule.get("start_date")
        or period_start
    )
    phase_end_date = current_phase.get("end_date") or (phases[0].get("end_date") if phases else None) or period_end
    if not phase_start_date or not phase_end_date:
        logging.warning(
            "Cannot schedule subscription change due to missing schedule/period boundaries: "
            f"{subscription_id=}, {period_start=}, {period_end=}, {phase_start_date=}, {phase_end_date=}"
        )
        return {}

    updated_schedule = await stripe.SubscriptionSchedule.modify_async(
        schedule.id,
        end_behavior="release",
        phases=[
            {
                "start_date": phase_start_date,
                "end_date": phase_end_date,
                "items": [{"price": current_price_id, "quantity": current_quantity}],
            },
            {
                "start_date": phase_end_date,
                "items": [{"price": target_price_id, "quantity": current_quantity}],
            },
        ],
    )

    return {
        "schedule_id": updated_schedule.id,
        "current_price_id": current_price_id,
        "target_price_id": target_price_id,
        "effective_at": to_utc_datetime(phase_end_date),
    }


@billing_enabled_guard({})
async def schedule_subscription_quantity_change_at_period_end_async(subscription_id: str, target_quantity: int) -> dict:
    if not subscription_id or target_quantity < 0:
        return {}

    subscription = await stripe.Subscription.retrieve_async(subscription_id)
    period_start = subscription.get("current_period_start")
    period_end = subscription.get("current_period_end")

    subscription_items = (subscription.get("items") or {}).get("data", []) if isinstance(subscription, dict) else subscription["items"]["data"]
    if not subscription_items:
        return {}

    current_item = subscription_items[0]
    current_item_id = current_item.get("id", "") if isinstance(current_item, dict) else (getattr(current_item, "id", "") or "")
    current_price = current_item.get("price", {}) if isinstance(current_item, dict) else current_item["price"]
    current_price_id = current_price.get("id", "") if isinstance(current_price, dict) else (getattr(current_price, "id", "") or "")
    current_quantity = current_item.get("quantity", 1) if isinstance(current_item, dict) else (getattr(current_item, "quantity", 1) or 1)

    schedule_id = (subscription.get("schedule") or "").strip()
    schedule = None
    if schedule_id:
        schedule = await stripe.SubscriptionSchedule.retrieve_async(schedule_id)
        schedule_status = (schedule.get("status") or "").strip()
        if schedule_status in {"released", "canceled", "completed"}:
            schedule_id = ""
            schedule = None

    if not schedule:
        schedule = await stripe.SubscriptionSchedule.create_async(from_subscription=subscription_id)

    phases = schedule.get("phases", []) or []
    current_phase = schedule.get("current_phase") or {}
    phase_start_date = (
        current_phase.get("start_date")
        or (phases[0].get("start_date") if phases else None)
        or schedule.get("start_date")
        or period_start
    )
    phase_end_date = current_phase.get("end_date") or (phases[0].get("end_date") if phases else None) or period_end
    if not phase_start_date or not phase_end_date:
        logging.warning(
            "Cannot schedule quantity change due to missing schedule/period boundaries: "
            f"{subscription_id=}, {period_start=}, {period_end=}, {phase_start_date=}, {phase_end_date=}"
        )
        return {}

    updated_schedule = await stripe.SubscriptionSchedule.modify_async(
        schedule.id,
        end_behavior="release",
        phases=[
            {
                "start_date": phase_start_date,
                "end_date": phase_end_date,
                "items": [{"price": current_price_id, "quantity": current_quantity}],
            },
            {
                "start_date": phase_end_date,
                "items": [{"price": current_price_id, "quantity": target_quantity}],
            },
        ],
    )

    return {
        "schedule_id": updated_schedule.id,
        "subscription_item_id": current_item_id,
        "price_id": current_price_id,
        "current_quantity": current_quantity,
        "target_quantity": target_quantity,
        "effective_at": to_utc_datetime(phase_end_date),
    }


@billing_enabled_guard(False)
async def cancel_scheduled_subscription_change_async(subscription_id: str) -> bool:
    if not subscription_id:
        return False

    subscription = await stripe.Subscription.retrieve_async(subscription_id)
    schedule_id = (subscription.get("schedule") or "").strip()
    if not schedule_id:
        return False

    await stripe.SubscriptionSchedule.release_async(schedule_id)
    return True


@billing_enabled_guard(False)
def cancel_scheduled_subscription_change_sync(subscription_id: str) -> bool:
    if not subscription_id:
        return False

    subscription = stripe.Subscription.retrieve(subscription_id)
    if isinstance(subscription, dict):
        schedule_id = (subscription.get("schedule") or "").strip()
    else:
        schedule_id = (getattr(subscription, "schedule", "") or "").strip()
    if not schedule_id:
        return False

    stripe.SubscriptionSchedule.release(schedule_id)
    return True


def get_plans_equal_or_higher(plan_name: str) -> list[tuple[str, list[str]]]:
    """
    return names of equal or higher plans and their price_ids. [name, price_ids]
    """
    if plan_name not in settings.BILLING_PLAN_TO_INFO:
        return []

    plan_info = settings.BILLING_PLAN_TO_INFO.get(plan_name)
    if not plan_info:
        return []

    current_priority = plan_info["priority"]
    result = []

    for priority, plans in sorted(settings.BILLING_PRIORITY_TO_PLANS.items()):
        if priority >= current_priority:
            for plan in plans:
                price_ids = settings.BILLING_PLAN_TO_INFO[plan]["price_ids"]
                result.append((plan, price_ids))

    return result


def _normalize_portal_products(product_id_to_prices: dict[str, list[str]]) -> list[dict]:
    products = []
    for product_id, prices in sorted(product_id_to_prices.items()):
        products.append({"product": product_id, "prices": sorted(prices)})
    return products


def _portal_configuration_cache_key(products: list[dict]) -> str:
    payload = json.dumps(products, separators=(",", ":"), ensure_ascii=True)
    digest = hashlib.sha256(payload.encode("utf-8")).hexdigest()
    return f"saas:billing:portal_config:{digest}"


@billing_enabled_guard(None)
def create_or_get_portal_configuration(product_id_to_prices: dict[str, list[str]]):
    """
    Create a Billing Portal Configuration that restricts price_ids.
    Reuse if needed (e.g., caching in DB).
    """
    products = _normalize_portal_products(product_id_to_prices)
    cache_key = _portal_configuration_cache_key(products)
    if REDIS_CONN.is_alive():
        cached_id = REDIS_CONN.get(cache_key)
        if cached_id:
            try:
                return stripe.billing_portal.Configuration.retrieve(cached_id)
            except Exception:
                logging.warning("Failed to retrieve cached Stripe portal configuration, recreating.")

    configuration = stripe.billing_portal.Configuration.create(
        features={
            "invoice_history": {"enabled": True},
            "subscription_update": {
                "enabled": True,
                "default_allowed_updates": ["price"],
                "proration_behavior": "always_invoice",
                "products": products,
            },
            "payment_method_update": {"enabled": True},
        }
    )
    if REDIS_CONN.is_alive():
        REDIS_CONN.set(cache_key, configuration.id, exp=60 * 60 * 24 * 7)
    return configuration


@billing_enabled_guard({})
def get_product_ids_for_prices(price_ids: list[str]) -> dict[str, str]:
    """
    return {price_id:product_id} from stripe
    """
    if not price_ids:
        return {}

    result = {}
    for price_id in price_ids:
        try:
            price = stripe.Price.retrieve(price_id)
            result[price_id] = price.product
        except Exception:
            logging.warning(f"get_product_ids_for_prices cannot get stripe product_id for {price_id}")
            pass

    return result


@billing_enabled_guard({})
def get_metadata_from_intent(payment_intent_id: str) -> dict:
    try:
        if payment_intent_id:
            payment_intent = stripe.PaymentIntent.retrieve(payment_intent_id)
            if not payment_intent:
                logging.warning("Expecting get metadata from stripe intent, but failed.")
                return {}
            intent_metadata = payment_intent.get("metadata", {})
            return intent_metadata
    except stripe.StripeError as e:
        print(f"An error occurred: {str(e)}")

    return {}


@billing_enabled_guard("")
def get_receipt_url_from_intent_latest_charge(latest_charge_id: str) -> str:
    try:
        if latest_charge_id:
            charge = stripe.Charge.retrieve(latest_charge_id)
            if not charge:
                logging.warning("Expecting get metadata from stripe intent, but failed.")
                return ""
            receipt_url = charge.get("receipt_url", "")
            return receipt_url
    except stripe.StripeError as e:
        print(f"An error occurred: {str(e)}")

    return ""


@billing_enabled_guard({})
def get_metadata_from_subscription(payment_subscription_id: str) -> dict:
    try:
        if payment_subscription_id:
            subscription = stripe.Subscription.retrieve(payment_subscription_id)
            print("*******************************************************")
            print(f"{subscription=}")
            print("*******************************************************")
            if not subscription:
                logging.warning("Expecting get metadata from stripe subscription, but failed.")
                return {}
            subscription_metadata = subscription.get("metadata", {})
            return subscription_metadata
    except stripe.StripeError as e:
        print(f"An error occurred: {str(e)}")

    return {}


@billing_enabled_guard("")
def create_stripe_customer_id(tenant_id: str, test_clock_id: str = "") -> str:
    from api.db.services.user_service import UserService

    user = UserService.filter_by_id(tenant_id)
    if not user:
        logging.warning(f"create_stripe_customer_id: tenant {tenant_id} not found")
        return ""
    test_clock_id = resolve_stripe_test_clock_id(test_clock_id)
    params = {"name": user.nickname, "email": user.email, "metadata": {"tenant_id": tenant_id}}
    if test_clock_id:
        api_key = (getattr(stripe, "api_key", None) or "").strip()
        if api_key.startswith("sk_test_"):
            params["test_clock"] = test_clock_id
        else:
            logging.warning("STRIPE_TEST_CLOCK_ID is set but Stripe API key is not a test key; creating customer without test_clock.")

    customer = stripe.Customer.create(**params)
    logging.info(f"created customer {customer.id} for tenant {tenant_id},name {user.nickname} email {user.email}.")
    return customer.id


@billing_enabled_guard("")
def billing_set_customer_id(tenant_id: str, customer_id: str = "", test_clock_id: str = "") -> str:
    from api.db.services.billing_service import SubscriptionService

    if not customer_id:
        customer_id = create_stripe_customer_id(tenant_id, test_clock_id=test_clock_id)
    if customer_id:
        SubscriptionService.set_customer_id(tenant_id, customer_id)
    return customer_id


async def billing_set_customer_id_async(tenant_id: str, customer_id: str = "", test_clock_id: str = "") -> str:
    import asyncio

    return await asyncio.to_thread(billing_set_customer_id, tenant_id, customer_id, test_clock_id)


def check_resources(**resource_deltas):
    """
    Decorator to check if tenant has sufficient resources before executing a function.

    Args:
        **resource_deltas: Keyword arguments specifying resource types and their delta values.
                          Supported types: seats, apps, storage
                          Example: @check_resources(seats=1, apps=1, storage=1024)

    Usage:
        @check_resources(seats=1)  # Check for 1 additional seat
        @check_resources(apps=1)   # Check for 1 additional app
        @check_resources(storage=1024)  # Check for 1KB additional storage
        @check_resources(seats=1, apps=1)  # Check for both seats and apps
    """

    assert set(resource_deltas.keys()).issubset({"seats", "apps", "storage"}), f"resources should in ['seats', 'apps', 'storage'], get {resource_deltas}"
    from api.db.services.billing_service import PurchasedProductOverviewService

    def decorator(f):
        @wraps(f)
        async def decorated_function(*args, **kwargs):
            try:
                from quart import current_app

                if settings.BILLING_ENABLED:
                    from quart import g, request
                    from api.apps import current_user
                    from api.utils.api_utils import (
                        get_data_error_result,
                        get_resource_insufficient_result,
                        server_error_response,
                    )

                    tenant_id = None

                    if hasattr(g, "tenant_id") and g.tenant_id:
                        tenant_id = g.tenant_id

                    if not tenant_id:
                        tenant_id = request.view_args.get("tenant_id") if request.view_args else None

                    if not tenant_id:
                        content_type = request.content_type or ""

                        if request.method in ["POST", "PUT", "PATCH"]:
                            if "application/json" in content_type:
                                if request.is_json:
                                    req_data = await request.get_json(silent=True) or {}
                                    tenant_id = req_data.get("tenant_id") or kwargs.get("tenant_id")

                            elif "multipart/form-data" in content_type or "application/x-www-form-urlencoded" in content_type:
                                form = await request.form
                                req_data = form or {}
                                tenant_id = req_data.get("tenant_id") or kwargs.get("tenant_id")

                            else:
                                req_data = request.args or {}
                                tenant_id = req_data.get("tenant_id") or kwargs.get("tenant_id")

                        else:  # GET、DELETE
                            req_data = request.args or {}
                            tenant_id = req_data.get("tenant_id") or kwargs.get("tenant_id")

                    if not tenant_id:
                        tenant_id = current_user.id if hasattr(current_user, "id") else None

                    if not tenant_id:
                        return get_data_error_result(message="Unable to determine tenant_id for resource checking.")

                    delta_app = resource_deltas.get("apps", 0)
                    delta_members = resource_deltas.get("seats", 0)
                    delta_storage_bytes = resource_deltas.get("storage", 0)

                    check_ok, check_info = PurchasedProductOverviewService.check_subscription_by_tenant_id(
                        tenant_id, delta_app=delta_app, delta_members=delta_members, delta_kb_storage=delta_storage_bytes
                    )

                    if not check_ok:
                        error_details = check_info.get("details", {})
                        error_messages = []
                        error_code = RetCode.BILLING_RESOURCE_INSUFFICIENT

                        if "quota_apps" in error_details:
                            error_messages.append(f"Insufficient app quota. Current: {error_details['quota_apps']['current']}, Limit: {error_details['quota_apps']['limit']}")
                            error_code = RetCode.BILLING_APPS_INSUFFICIENT

                        if "quota_members" in error_details:
                            error_messages.append(f"Insufficient seat quota. Current: {error_details['quota_members']['current']}, Limit: {error_details['quota_members']['limit']}")
                            error_code = RetCode.BILLING_SEATS_INSUFFICIENT

                        if "quota_kb_storage" in error_details:
                            error_messages.append(
                                f"Insufficient storage quota. Current: {error_details['quota_kb_storage']['current']} bytes, "
                                f"Limit: {error_details['quota_kb_storage']['limit']} bytes"
                            )
                            error_code = RetCode.BILLING_STORAGE_INSUFFICIENT

                        if error_messages:
                            error_msg = "; ".join(error_messages)
                        else:
                            error_msg = "Insufficient resources available. Contact the owner for further assistance."

                        try:
                            ok, tenant = TenantService.get_by_id(tenant_id)
                            if ok:
                                error_msg = f"Insufficient resources in {tenant.name}: {error_msg}"
                        except Exception:
                            pass

                        identified_error_resource = ""
                        match error_code:
                            case RetCode.BILLING_RESOURCE_INSUFFICIENT:
                                pass  # return all details
                            case RetCode.BILLING_APPS_INSUFFICIENT:
                                identified_error_resource = "quota_apps"
                            case RetCode.BILLING_SEATS_INSUFFICIENT:
                                identified_error_resource = "quota_members"
                            case RetCode.BILLING_STORAGE_INSUFFICIENT:
                                identified_error_resource = "quota_kb_storage"

                        if identified_error_resource:
                            error_details = {"current": error_details[identified_error_resource]["current"], "limit": error_details[identified_error_resource]["limit"]}

                        return get_resource_insufficient_result(code=error_code, message=error_msg, detail=error_details)
            except Exception as e:
                from api.utils.api_utils import server_error_response

                return server_error_response(e)
            return await current_app.ensure_async(f)(*args, **kwargs)

        return decorated_function

    return decorator


@billing_enabled_guard((True, {}))
def check_dynamic_resources(tenant_id=None, **resource_deltas):
    """
    Function to check resources dynamically during function execution.

    This function is useful when resource deltas are only known during execution,
    such as file upload sizes that are calculated during the upload process.

    Args:
        tenant_id (str, optional): The tenant ID to check resources for.
                                  If not provided, it will be extracted from Flask context.
        **resource_deltas: Keyword arguments specifying resource types and their delta values.
                          Supported types: seats, apps, storage
                          Example: check_dynamic_resources(tenant_id, storage=file_size_in_bytes)

    Returns:
        tuple: (check_ok, check_info) where check_ok is a boolean indicating if check passed,
               and check_info contains details about the check result.

    Usage:
        # In a function where you calculate file size during execution:
        file_size_bytes = calculate_file_size_in_bytes(file_path)
        check_ok, check_info = check_dynamic_resources(tenant_id, storage=file_size_bytes)
        if not check_ok:
            return get_data_error_result(message=check_info.get("error", "Insufficient storage"))
    """
    assert set(resource_deltas.keys()).issubset({"seats", "apps", "storage"}), f"resources should in ['seats', 'apps', 'storage'], get {resource_deltas}"

    from api.db.services.billing_service import PurchasedProductOverviewService

    if not tenant_id:
        try:
            from flask import g
            from flask_login import current_user

            if hasattr(g, "tenant_id") and g.tenant_id:
                tenant_id = g.tenant_id

            if not tenant_id:
                from flask import request as flask_request

                content_type = flask_request.content_type or ""

                if flask_request.method in ["POST", "PUT", "PATCH"]:
                    if "application/json" in content_type:
                        if flask_request.is_json:
                            req_data = flask_request.get_json(silent=True) or {}
                            tenant_id = req_data.get("tenant_id")

                    elif "multipart/form-data" in content_type or "application/x-www-form-urlencoded" in content_type:
                        req_data = flask_request.form or {}
                        tenant_id = req_data.get("tenant_id")

                    else:
                        req_data = flask_request.args or {}
                        tenant_id = req_data.get("tenant_id")

                else:  # GET、DELETE
                    req_data = flask_request.args or {}
                    tenant_id = req_data.get("tenant_id")

            if not tenant_id:
                tenant_id = current_user.id if hasattr(current_user, "id") else None
        except Exception:
            pass

    if not tenant_id:
        return False, {"error": "Unable to determine tenant_id for resource checking."}

    delta_app = resource_deltas.get("apps", 0)
    delta_members = resource_deltas.get("seats", 0)
    delta_storage_bytes = resource_deltas.get("storage", 0)

    return PurchasedProductOverviewService.check_subscription_by_tenant_id(
        tenant_id,
        delta_app=delta_app,
        delta_members=delta_members,
        delta_kb_storage=delta_storage_bytes,
    )
