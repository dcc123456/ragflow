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
import logging
import os
import re
import hashlib


class InsufficientResourceError(Exception):
    """Raised when a resource quota check fails, carrying structured detail."""

    def __init__(self, resource: str, current: int, limit: int, message: str, file_size: int | None = None):
        super().__init__(message)
        self.resource = resource
        self.current = current
        self.limit = limit
        self.file_size = file_size
        self.detail = {"current": current, "limit": limit}
        if file_size is not None:
            self.detail["file_size"] = file_size


from contextvars import ContextVar, Token
from decimal import ROUND_HALF_UP, Decimal
from functools import wraps

import stripe

from common import settings
from common.billing_utils import billing_enabled_guard, normalize_stripe_invoice_status, to_utc_datetime
from common.constants import RetCode
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
BYTES_PER_GB = 1000 * 1000 * 1000
STRIPE_TEST_CLOCK_HEADER = "X-Stripe-Test-Clock"
_stripe_test_clock_id_context: ContextVar[str] = ContextVar("stripe_test_clock_id", default="")
_storage_quota_bytes_per_unit: int | None = None


def set_stripe_test_clock_id_for_current_context(test_clock_id: str) -> Token[str]:
    return _stripe_test_clock_id_context.set((test_clock_id or "").strip())


def reset_stripe_test_clock_id_for_current_context(token: Token[str]) -> None:
    _stripe_test_clock_id_context.reset(token)


def get_stripe_test_clock_id_for_current_context() -> str:
    return (_stripe_test_clock_id_context.get() or "").strip()


def resolve_stripe_test_clock_id(test_clock_id: str = "") -> str:
    if test_clock_id:
        return test_clock_id.strip()

    try:
        from quart import has_request_context, request

        if has_request_context():
            request_test_clock_id = (request.headers.get(STRIPE_TEST_CLOCK_HEADER) or "").strip()
            if request_test_clock_id:
                return request_test_clock_id
    except Exception:
        pass

    return (get_stripe_test_clock_id_for_current_context() or os.getenv("STRIPE_TEST_CLOCK_ID") or "").strip()


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


def get_attr_or_item(obj, key: str, default=None):
    if isinstance(obj, dict):
        return obj.get(key, default)
    return getattr(obj, key, default)


def get_nested_attr_or_item(obj, *keys: str, default=None):
    value = obj
    for key in keys:
        value = get_attr_or_item(value, key, None)
        if value is None:
            return default
    return value


def extract_list_data(obj) -> list:
    if not obj:
        return []
    if callable(obj):
        try:
            obj = obj()
        except TypeError:
            return []
    data = get_attr_or_item(obj, "data", []) or []
    return data if isinstance(data, list) else []


def extract_subscription_items_data(obj) -> list:
    """Return Stripe subscription items regardless of dict/object/method style.

    Stripe SDK v14+ may expose nested resources like ``subscription.items`` as a
    callable accessor instead of a plain attribute on some live objects. Older
    objects and typed webhook models may still expose ``items.data`` directly.
    This helper normalizes all known shapes into a list.
    """
    if not obj:
        return []

    return extract_list_data(get_attr_or_item(obj, "items", None))


def extract_latest_invoice_id(subscription_obj) -> str:
    return _extract_invoice_id(extract_latest_invoice_obj(subscription_obj))


def get_product_id_by_name(product_name: str) -> str:
    # local import to avoid circular dependency in module import phase
    from api.db.services.billing_service import ProductService

    product = ProductService.get_by_name(product_name) or {}
    return (product.get("id") or "").strip()


def extract_storage_subscription_item(subscription_obj) -> tuple[str, str, int]:
    """Return the storage subscription item tuple ``(item_id, price_id, quantity)``.

    When a subscription carries multiple items (e.g. a plan item **and** a
    storage add-on), this function selects the storage item instead of assuming
    the first item is the desired one. If no storage item exists, it returns
    an empty tuple payload.
    """
    if isinstance(subscription_obj, dict):
        data = (subscription_obj.get("items") or {}).get("data", []) or []
        if not data:
            return "", "", 0
        item = None
        for _item in data:
            if not isinstance(_item, dict):
                continue
            _price_obj = _item.get("price", {})
            _price_id = _price_obj.get("id", "") if isinstance(_price_obj, dict) else ""
            if is_storage_price_id(_price_id):
                item = _item
                break
        if not item:
            return "", "", 0
        price_obj = item.get("price", {}) if isinstance(item, dict) else {}
        return (
            item.get("id", "") if isinstance(item, dict) else "",
            price_obj.get("id", "") if isinstance(price_obj, dict) else "",
            safe_int(item.get("quantity", 0) if isinstance(item, dict) else 0, 0),
        )

    data = extract_subscription_items_data(subscription_obj)
    if not data:
        return "", "", 0
    item = None
    for _item in data:
        _price_obj = getattr(_item, "price", None)
        _price_id = (getattr(_price_obj, "id", "") or "") if _price_obj else ""
        if is_storage_price_id(_price_id):
            item = _item
            break
    if not item:
        return "", "", 0
    price_obj = getattr(item, "price", None)
    return (
        getattr(item, "id", "") or "",
        getattr(price_obj, "id", "") if price_obj else "",
        safe_int(getattr(item, "quantity", 0), 0),
    )


def extract_plan_subscription_item(subscription_obj) -> tuple[str, str, int]:
    """Return the plan (non-storage) subscription item tuple ``(item_id, price_id, quantity)``.

    When a subscription carries multiple items (e.g. a plan item **and** a
    storage add-on), this function skips storage items and returns the first
    non-storage item.  If every item maps to storage (unlikely but safe), it
    falls back to the first item so the caller always gets a result.
    """
    if isinstance(subscription_obj, dict):
        data = (subscription_obj.get("items") or {}).get("data", []) or []
        if not data:
            return "", "", 0
        plan_item = None
        for _item in data:
            if not isinstance(_item, dict):
                continue
            _price_obj = _item.get("price", {})
            _price_id = _price_obj.get("id", "") if isinstance(_price_obj, dict) else ""
            if not is_storage_price_id(_price_id):
                plan_item = _item
                break
        item = plan_item or data[0]
        price_obj = item.get("price", {}) if isinstance(item, dict) else {}
        return (
            item.get("id", "") if isinstance(item, dict) else "",
            price_obj.get("id", "") if isinstance(price_obj, dict) else "",
            safe_int(item.get("quantity", 0) if isinstance(item, dict) else 0, 0),
        )

    data = extract_subscription_items_data(subscription_obj)
    if not data:
        return "", "", 0
    plan_item = None
    for _item in data:
        _price_obj = getattr(_item, "price", None)
        _price_id = (getattr(_price_obj, "id", "") or "") if _price_obj else ""
        if not is_storage_price_id(_price_id):
            plan_item = _item
            break
    item = plan_item or data[0]
    price_obj = getattr(item, "price", None)
    return (
        getattr(item, "id", "") or "",
        getattr(price_obj, "id", "") if price_obj else "",
        safe_int(getattr(item, "quantity", 0), 0),
    )


def _dedupe_schedule_phase_items(items: list[dict]) -> list[dict]:
    """Return Stripe phase items with one entry per price ID.

    Stripe rejects phases that contain the same price more than once. When a
    caller accidentally builds duplicates, keep the latest item payload for that
    price while preserving the original order slot.
    """
    deduped: list[dict] = []
    seen_indexes: dict[str, int] = {}
    for item in items or []:
        if not isinstance(item, dict):
            continue
        price = item.get("price", "")
        price_id = price.get("id", "") if isinstance(price, dict) else (price or "")
        price_id = (price_id or "").strip()
        if not price_id:
            continue
        normalized_item = {**item, "price": price_id}
        existing_index = seen_indexes.get(price_id)
        if existing_index is None:
            seen_indexes[price_id] = len(deduped)
            deduped.append(normalized_item)
            continue
        deduped[existing_index] = normalized_item
    return deduped


def extract_plan_item_and_price(subscription_obj):
    data = extract_subscription_items_data(subscription_obj)
    if not data:
        return None, None, ""

    fallback_item = data[0]
    fallback_price = get_attr_or_item(fallback_item, "price", None)
    fallback_price_id = (get_attr_or_item(fallback_price, "id", "") or "").strip() if fallback_price else ""

    for item in data:
        price_obj = get_attr_or_item(item, "price", None)
        price_id = (get_attr_or_item(price_obj, "id", "") or "").strip() if price_obj else ""
        if price_id and not is_storage_price_id(price_id):
            return item, price_obj, price_id

    return fallback_item, fallback_price, fallback_price_id


def extract_previous_plan_price(previous_obj):
    previous_data = extract_subscription_items_data(previous_obj)
    fallback_price = None
    if previous_data:
        first_item = previous_data[0]
        fallback_price = get_attr_or_item(first_item, "price", None)
        for item in previous_data:
            price_obj = get_attr_or_item(item, "price", None)
            price_id = (get_attr_or_item(price_obj, "id", "") or "").strip() if price_obj else ""
            if price_id and not is_storage_price_id(price_id):
                return price_obj

    previous_plan = get_attr_or_item(previous_obj, "plan", None)
    previous_plan_id = (get_attr_or_item(previous_plan, "id", "") or "").strip() if previous_plan else ""
    if previous_plan and (not previous_plan_id or not is_storage_price_id(previous_plan_id)):
        return previous_plan

    return fallback_price


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
        item_data = (subscription_obj.get("items") or {}).get("data") or []
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
    item_data = extract_subscription_items_data(subscription_obj)
    first_item = item_data[0] if item_data else None
    item_start = to_utc_datetime(getattr(first_item, "current_period_start", None)) if first_item else None
    item_end = to_utc_datetime(getattr(first_item, "current_period_end", None)) if first_item else None
    return item_start, item_end


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


def _get_nested_invoice_value(data: dict, *keys):
    value = data
    for key in keys:
        if not isinstance(value, dict):
            return None
        value = value.get(key)
    return value


def extract_invoice_subscription_id(invoice: dict) -> str:
    subscription_id = invoice.get("subscription")
    if isinstance(subscription_id, str) and subscription_id.strip():
        return subscription_id.strip()

    candidates = [
        _get_nested_invoice_value(invoice, "parent", "subscription_details", "subscription"),
        _get_nested_invoice_value(invoice, "parent", "subscription_item_details", "subscription"),
    ]
    lines = invoice.get("lines") or {}
    if isinstance(lines, dict):
        for line in lines.get("data") or []:
            candidates.append(_get_nested_invoice_value(line, "parent", "subscription_item_details", "subscription"))

    for candidate in candidates:
        if isinstance(candidate, str) and candidate.strip():
            return candidate.strip()
    return ""


def extract_invoice_failure_context(invoice: dict) -> dict:
    return {
        "invoice_id": (invoice.get("id") or "").strip(),
        "invoice_url": (invoice.get("hosted_invoice_url") or "").strip(),
        "invoice_pdf_url": (invoice.get("invoice_pdf") or "").strip(),
        "invoice_status": (invoice.get("status") or "").strip().lower(),
        "customer_id": (invoice.get("customer") or "").strip(),
        "subscription_id": extract_invoice_subscription_id(invoice),
        "payment_intent_id": (invoice.get("payment_intent") or "").strip(),
        "amount_cents": invoice.get("amount_due") or invoice.get("amount_remaining") or 0,
        "currency": invoice.get("currency") or "usd",
        "attempt_count": invoice.get("attempt_count"),
        "next_payment_attempt": invoice.get("next_payment_attempt"),
        "billing_reason": invoice.get("billing_reason"),
        "created": invoice.get("created"),
    }


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


def _extract_payment_intent_status(invoice_obj) -> str:
    if not invoice_obj or isinstance(invoice_obj, str):
        return ""

    if isinstance(invoice_obj, dict):
        payment_intent = invoice_obj.get("payment_intent")
    else:
        payment_intent = getattr(invoice_obj, "payment_intent", None)

    if isinstance(payment_intent, dict):
        return (payment_intent.get("status") or "").strip().lower()
    return (getattr(payment_intent, "status", "") or "").strip().lower()


async def is_subscription_latest_invoice_paid_async(subscription_obj) -> tuple[bool, str, str, str]:
    latest_invoice = extract_latest_invoice_obj(subscription_obj)
    invoice_id = _extract_invoice_id(latest_invoice)
    invoice_status, invoice_url = _extract_invoice_status_and_url(latest_invoice)
    if not invoice_id:
        # No invoice means there's nothing to collect for this update.
        return True, "", "", ""

    if invoice_status and invoice_url:
        return _is_paid_invoice_status(invoice_status), invoice_id, invoice_status, invoice_url

    invoice = await stripe.Invoice.retrieve_async(invoice_id)
    invoice_status, invoice_url = _extract_invoice_status_and_url(invoice)
    return _is_paid_invoice_status(invoice_status), invoice_id, invoice_status, invoice_url


def is_subscription_latest_invoice_paid_sync(subscription_obj) -> bool:
    latest_invoice = extract_latest_invoice_obj(subscription_obj)
    invoice_id = _extract_invoice_id(latest_invoice)
    invoice_status, _invoice_url = _extract_invoice_status_and_url(latest_invoice)
    if not invoice_id:
        return True

    if invoice_status:
        return _is_paid_invoice_status(invoice_status)

    invoice = stripe.Invoice.retrieve(invoice_id)
    invoice_status, _ = _extract_invoice_status_and_url(invoice)
    return _is_paid_invoice_status(invoice_status)


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


def storage_quantity_to_bytes(quantity: int | str | None) -> int:
    """
    Convert a storage addon quantity (number of units purchased) to bytes.

    Each Stripe quantity unit for storage equals the quota_storage of the
    storage plan (e.g. 1G plan -> 1 unit = 1GB = 1_000_000_000 bytes).
    """
    global _storage_quota_bytes_per_unit
    if _storage_quota_bytes_per_unit is None:
        storage_plan_info = settings.BILLING_PLAN_TO_INFO.get(STORAGE_PRODUCT_NAME, {}) or {}
        quota_storage_str = str(storage_plan_info.get("quota_storage", "0"))
        _storage_quota_bytes_per_unit = parse_storage_size(quota_storage_str)

    qty = max(safe_int(quantity, 0), 0)
    return qty * _storage_quota_bytes_per_unit


def storage_bytes_to_quantity(value: int | str | None) -> int:
    global _storage_quota_bytes_per_unit
    if _storage_quota_bytes_per_unit is None:
        storage_plan_info = settings.BILLING_PLAN_TO_INFO.get(STORAGE_PRODUCT_NAME, {}) or {}
        quota_storage_str = str(storage_plan_info.get("quota_storage", "0"))
        _storage_quota_bytes_per_unit = parse_storage_size(quota_storage_str)

    if not _storage_quota_bytes_per_unit:
        _storage_quota_bytes_per_unit = BYTES_PER_GB

    return max(safe_int(value, 0), 0) // _storage_quota_bytes_per_unit


def get_trial_price_id(plans: list[dict]) -> str:
    trial_plan = next((plan for plan in plans if plan.get("name") == BILLING_PLAN_TRIAL_NAME and plan.get("price_ids")), None)
    return trial_plan["price_ids"].split()[0] if trial_plan else ""


def get_plan_priority_by_price_id(price_id: str) -> int | None:
    plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(price_id, "")
    if not plan_name:
        return None
    plan_info = settings.BILLING_PLAN_TO_INFO.get(plan_name) or {}
    return plan_info.get("priority")


def is_downgrade_by_price_id(current_price_id: str, target_price_id: str) -> bool:
    current_priority = get_plan_priority_by_price_id(current_price_id)
    target_priority = get_plan_priority_by_price_id(target_price_id)
    return current_priority is not None and target_priority is not None and target_priority < current_priority


@billing_enabled_guard({})
async def get_pending_subscription_change_async(subscription_id: str) -> dict:
    if not subscription_id:
        return {}

    subscription = await stripe.Subscription.retrieve_async(subscription_id)
    current_plan_item_id, current_price_id, _current_quantity = extract_plan_subscription_item(subscription)
    if not current_plan_item_id or not current_price_id:
        return {}
    schedule_id = (subscription.get("schedule") or "").strip()
    if not schedule_id:
        return {}

    schedule = await stripe.SubscriptionSchedule.retrieve_async(schedule_id)
    schedule_status = (schedule.get("status") or "").strip()
    if schedule_status in {"released", "canceled", "completed"}:
        return {}

    phases = schedule.get("phases", []) or []
    if len(phases) < 2:
        # Single-phase schedule: check if end_behavior="cancel" signals a pending
        # Trial downgrade.  The schedule's phases[0] items describe the plan being
        # canceled; we derive the pending target from the subscription metadata
        # or fall back to the current phase's plan as the pending target.
        end_behavior = (schedule.get("end_behavior") or "").strip().lower()
        if end_behavior == "cancel":
            current_items = phases[0].get("items", []) if phases else []
            # Use current phase price as pending_price (subscription will end)
            pending_price_id = ""
            for item in current_items:
                if not isinstance(item, dict):
                    continue
                _price = item.get("price", "")
                _price_id = _price.get("id", "") if isinstance(_price, dict) else (_price or "")
                _price_id = (_price_id or "").strip()
                if _price_id and not is_storage_price_id(_price_id):
                    pending_price_id = _price_id
                    break
            if not pending_price_id:
                pending_price_id = current_price_id
            # end_behavior=cancel + single phase means the subscription is
            # scheduled to cancel — pending target is Trial (no price_id in billing)
            pending_plan_name = "Trial"
            return {
                "schedule_id": schedule_id,
                "pending_price_id": pending_price_id,
                "pending_plan_name": pending_plan_name,
                "effective_at": to_utc_datetime((phases[0].get("end_date") if phases else None) or schedule.get("end_date") or 0),
            }
        return {}

    pending_phase = phases[1]
    pending_items = pending_phase.get("items", []) or []
    if not pending_items:
        return {}

    # Extract pending plan price id (first non-storage item).
    pending_price_id = ""
    for item in pending_items:
        if not isinstance(item, dict):
            continue
        _price = item.get("price", "")
        _price_id = _price.get("id", "") if isinstance(_price, dict) else (_price or "")
        _price_id = (_price_id or "").strip()
        if _price_id and not is_storage_price_id(_price_id):
            pending_price_id = _price_id
            break
    if not pending_price_id:
        return {}

    # Stripe can keep a schedule attached briefly after the phase has already
    # taken effect. Once the live subscription's current plan item matches the
    # pending phase target, this is no longer a future change and should not be
    # exposed as pending state.
    if pending_price_id == current_price_id:
        return {}

    # Extract pending storage quantity from the next phase items.
    # Phase items are flat dicts: {"price": "<price_id_or_obj>", "quantity": N}
    # Each Stripe quantity unit = storage plan's quota_storage (e.g. "1G").
    pending_storage_quantity_bytes = None
    for item in pending_items:
        if not isinstance(item, dict):
            continue
        _price = item.get("price", "")
        _price_id = _price.get("id", "") if isinstance(_price, dict) else (_price or "")
        _price_id = (_price_id or "").strip()
        if _price_id and is_storage_price_id(_price_id):
            qty = safe_int(item.get("quantity", 0), 0)
            storage_plan_info = settings.BILLING_PLAN_TO_INFO.get(STORAGE_PRODUCT_NAME, {}) or {}
            quota_storage_str = str(storage_plan_info.get("quota_storage", "0"))
            try:
                bytes_per_unit = parse_storage_size(quota_storage_str)
            except ValueError:
                bytes_per_unit = BYTES_PER_GB
            pending_storage_quantity_bytes = qty * bytes_per_unit
            break

    effective_at = pending_phase.get("start_date")
    result: dict = {
        "schedule_id": schedule_id,
        "pending_price_id": pending_price_id,
        "pending_plan_name": settings.BILLING_PRICEID_TO_PRODUCT.get(pending_price_id, ""),
        "effective_at": to_utc_datetime(effective_at),
    }
    if pending_storage_quantity_bytes is not None:
        result["pending_storage_quantity_bytes"] = pending_storage_quantity_bytes
    return result


@billing_enabled_guard({})
async def schedule_subscription_items_change_at_period_end_async(
    subscription_id: str,
    *,
    current_phase_items: list[dict],
    next_phase_items: list[dict],
) -> dict:
    """Schedule a full subscription-items snapshot change at period end.

    Both ``current_phase_items`` and ``next_phase_items`` must describe the
    complete set of subscription items that should exist in each phase, not only
    the item being changed. In this codebase that means:

    - include the plan item in both phases
    - include the storage item only when the subscription currently has storage
      or the next phase should keep/add storage

    Passing only the delta item is incorrect because Stripe interprets phase
    ``items`` as the full item list for that phase.
    """
    if not subscription_id or not current_phase_items:
        return {}

    # When downgrading to Trial (next_phase_items=[]), omit the second phase entirely —
    # Stripe rejects an empty items array in phases[1]. Instead, pass a single-phase
    # schedule that ends the subscription at period end via end_behavior="cancel".
    if not next_phase_items:
        current_phase_items = _dedupe_schedule_phase_items(current_phase_items)
        if not current_phase_items:
            return {}

        subscription = await stripe.Subscription.retrieve_async(subscription_id)
        period_start = subscription.get("current_period_start")
        period_end = subscription.get("current_period_end")

        schedule_id = (subscription.get("schedule") or "").strip()
        schedule = None
        if schedule_id:
            schedule = await stripe.SubscriptionSchedule.retrieve_async(schedule_id)
            schedule_status = (schedule.get("status") or "").strip().lower()
            if schedule_status in {"released", "canceled", "completed"}:
                schedule = None

        # If an existing schedule is found with phases that may have already ended,
        # release it first and create a fresh schedule so Stripe accepts brand-new
        # phase boundaries instead of rejecting a modification to an ended phase.
        if schedule_id:
            try:
                await stripe.SubscriptionSchedule.release_async(schedule_id)
                logging.info("Released ended schedule %s for Trial downgrade, will replace", schedule_id)
            except Exception:
                pass
            schedule = None

        if not schedule:
            schedule = await stripe.SubscriptionSchedule.create_async(from_subscription=subscription_id)

        phases = schedule.get("phases", []) or []
        phase_start_date = (phases[0].get("start_date") if phases else None) or schedule.get("start_date") or period_start
        phase_end_date = (phases[0].get("end_date") if phases else None) or period_end

        updated_schedule = await stripe.SubscriptionSchedule.modify_async(
            schedule.id,
            end_behavior="cancel",
            phases=[
                {
                    "start_date": phase_start_date,
                    "end_date": phase_end_date,
                    "items": current_phase_items,
                },
            ],
        )

        # pending_price_id / pending_plan_name: derive from current phase (the
        # plan being canceled).  The caller is responsible for mapping this to the
        # actual target plan name when next_phase_items is empty.
        first_item = current_phase_items[0]
        pending_price_id = first_item.get("price", "") if isinstance(first_item, dict) else ""
        pending_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(pending_price_id, "")

        return {
            "schedule_id": updated_schedule.id,
            "pending_price_id": pending_price_id,
            "pending_plan_name": pending_plan_name,
            "effective_at": phase_end_date,
        }

    current_phase_items = _dedupe_schedule_phase_items(current_phase_items)
    next_phase_items = _dedupe_schedule_phase_items(next_phase_items)

    subscription = await stripe.Subscription.retrieve_async(subscription_id)
    period_start = subscription.get("current_period_start")
    period_end = subscription.get("current_period_end")

    schedule_id = (subscription.get("schedule") or "").strip()
    schedule = None
    if schedule_id:
        schedule = await stripe.SubscriptionSchedule.retrieve_async(schedule_id)
        schedule_status = (schedule.get("status") or "").strip().lower()
        if schedule_status in {"released", "canceled", "completed"}:
            schedule = None

    if not schedule:
        schedule = await stripe.SubscriptionSchedule.create_async(from_subscription=subscription_id)

    phases = schedule.get("phases", []) or []
    current_phase = schedule.get("current_phase") or {}
    phase_start_date = current_phase.get("start_date") or (phases[0].get("start_date") if phases else None) or schedule.get("start_date") or period_start
    phase_end_date = current_phase.get("end_date") or (phases[0].get("end_date") if phases else None) or period_end
    if not phase_start_date or not phase_end_date:
        logging.warning(
            f"Cannot schedule subscription item change due to missing schedule/period boundaries: {subscription_id=}, {period_start=}, {period_end=}, {phase_start_date=}, {phase_end_date=}"
        )
        return {}

    updated_schedule = await stripe.SubscriptionSchedule.modify_async(
        schedule.id,
        end_behavior="release",
        phases=[
            {
                "start_date": phase_start_date,
                "end_date": phase_end_date,
                "items": current_phase_items,
            },
            {
                "start_date": phase_end_date,
                "items": next_phase_items,
            },
        ],
    )

    return {
        "schedule_id": updated_schedule.id,
        "effective_at": to_utc_datetime(phase_end_date),
    }


@billing_enabled_guard({})
async def schedule_subscription_price_change_at_period_end_async(
    subscription_id: str,
    target_price_id: str,
    *,
    target_storage_quantity: int | None = None,
) -> dict:
    """Schedule a plan price change at period end.

    Args:
        subscription_id: Stripe subscription ID.
        target_price_id: Stripe price ID for the new plan.
        target_storage_quantity: If provided, override the storage item quantity
            in the next phase.  Pass ``0`` to cancel storage at the same period end
            as the plan change (e.g. when downgrading to Trial).  When ``None``
            (default), the live subscription's storage quantity is preserved.
    """
    if not subscription_id or not target_price_id:
        return {}

    subscription = await stripe.Subscription.retrieve_async(subscription_id)
    subscription_items = (subscription.get("items") or {}).get("data", []) if isinstance(subscription, dict) else subscription["items"]["data"]
    if not subscription_items:
        return {}

    _item_id, current_price_id, current_quantity = extract_plan_subscription_item(subscription)
    target_plan_name = settings.BILLING_PRICEID_TO_PRODUCT.get(target_price_id, "")
    target_is_trial = is_trial_plan_name(target_plan_name)

    current_phase_items = [{"price": current_price_id, "quantity": current_quantity}]
    # Trial: clear all items so subscription becomes inactive with no invoice
    next_phase_items = [] if target_is_trial else [{"price": target_price_id, "quantity": current_quantity}]

    for item in subscription_items:
        price_obj = item.get("price", {}) if isinstance(item, dict) else getattr(item, "price", None)
        price_id = price_obj.get("id", "") if isinstance(price_obj, dict) else (getattr(price_obj, "id", "") if price_obj else "")
        price_id = (price_id or "").strip()
        if not price_id or price_id == current_price_id:
            continue

        quantity = item.get("quantity", 0) if isinstance(item, dict) else getattr(item, "quantity", 0)
        quantity = safe_int(quantity, 0)
        current_phase_items.append({"price": price_id, "quantity": quantity})

        # next_phase_items already [] for Trial — nothing to add
        if target_is_trial:
            continue

        if is_storage_price_id(price_id):
            # Omitting the storage item from the next phase removes it. Do not
            # keep it with quantity=0; Stripe may retain the item instead.
            next_storage_qty = target_storage_quantity if target_storage_quantity is not None else quantity
            if next_storage_qty > 0:
                next_phase_items.append({"price": price_id, "quantity": next_storage_qty})
            continue

        if quantity > 0:
            next_phase_items.append({"price": price_id, "quantity": quantity})

    scheduled = await schedule_subscription_items_change_at_period_end_async(
        subscription_id,
        current_phase_items=current_phase_items,
        next_phase_items=next_phase_items,
    )
    if not scheduled:
        return {}

    if target_is_trial:
        pending_plan_name = target_plan_name
    else:
        pending_plan_name = scheduled.get("pending_plan_name", settings.BILLING_PRICEID_TO_PRODUCT.get(current_price_id, ""))

    return {
        "schedule_id": scheduled["schedule_id"],
        "pending_price_id": scheduled.get("pending_price_id", current_price_id),
        "pending_plan_name": pending_plan_name,
        "current_price_id": current_price_id,
        "target_price_id": target_price_id,
        "effective_at": scheduled["effective_at"],
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


@billing_enabled_guard({})
async def modify_subscription_plan_async(
    *,
    tenant_id: str,
    subscription_id: str,
    target_price_id: str,
    reset_billing_cycle: bool,
    end_trial_now: bool,
) -> dict:
    """Immediately switch a subscription to a new plan price in-place.

    Cancels any pending scheduled change first, then calls Stripe's
    ``Subscription.modify`` with ``proration_behavior="always_invoice"`` so
    the customer is charged (or credited) the prorated difference right away.

    When a subscription has multiple items (e.g. plan + storage add-on), only
    the plan item is replaced; the storage item is left untouched.

    When the resulting invoice requires additional authentication (SCA), Stripe
    returns a ``payment_intent`` with status ``requires_action``.  The caller
    must check ``payment_intent_status`` in the returned dict and redirect the
    customer to ``invoice_url`` to complete 3DS if needed.

    Args:
        tenant_id: Internal tenant identifier; used as part of the idempotency key.
        subscription_id: Stripe subscription ID to modify.
        target_price_id: Stripe price ID for the new plan.
        reset_billing_cycle: If True, anchors the billing cycle to now (used for upgrades from trial).
        end_trial_now: If True, ends the trial period immediately before switching.

    Returns:
        A dict with keys ``subscription``, ``idempotency_key``, ``invoice_id``,
        ``invoice_status``, ``invoice_url``, ``amount_cents``,
        ``currency``, and ``payment_intent_status``; or an error dict
        containing ``error_message`` on failure.
    """
    if not tenant_id or not subscription_id or not target_price_id:
        return {}

    # A stale schedule changes the Stripe write semantics for the next modify
    # call, so cancellation failure must stop the plan change instead of
    # silently continuing with an unknown pending state.
    await cancel_scheduled_subscription_change_async(subscription_id)

    subscription = await stripe.Subscription.retrieve_async(subscription_id)

    subscription_item_id, _current_price_id, current_quantity = extract_plan_subscription_item(subscription)
    if not subscription_item_id:
        logging.warning(f"No subscription item found for subscription {subscription_id}")
        return {}

    request_quantity = max(current_quantity, 1)

    # Build the full items list: update plan item price + preserve all other items
    # (e.g. storage add-on). Stripe replaces all items, so omitting any would drop them.
    modify_items = [{"id": subscription_item_id, "price": target_price_id, "quantity": request_quantity}]
    subscription_items = (subscription.get("items") or {}).get("data", []) if isinstance(subscription, dict) else subscription["items"]["data"]
    for item in subscription_items or []:
        price_obj = item.get("price", {}) if isinstance(item, dict) else getattr(item, "price", None)
        price_id = price_obj.get("id", "") if isinstance(price_obj, dict) else (getattr(price_obj, "id", "") if price_obj else "")
        price_id = (price_id or "").strip()
        if not price_id or price_id == _current_price_id:
            # Skip the plan item we're already replacing
            continue
        quantity = item.get("quantity", 0) if isinstance(item, dict) else getattr(item, "quantity", 0)
        quantity = safe_int(quantity, 0)
        if quantity > 0:
            # Preserve this non-plan item (e.g. storage add-on) as-is
            modify_items.append({"id": item.get("id", "") if isinstance(item, dict) else getattr(item, "id", ""), "price": price_id, "quantity": quantity})

    item_fingerprint = ",".join(f"{item.get('id', '')}:{item.get('price', '')}:{item.get('quantity', 0)}" for item in modify_items)
    item_fingerprint_hash = hashlib.sha1(item_fingerprint.encode("utf-8")).hexdigest()[:12]
    idempotency_key = (
        f"billing:{tenant_id}:plan_change:{subscription_id}:"
        f"{_current_price_id}->{target_price_id}:"
        f"anchor={int(bool(reset_billing_cycle))}:"
        f"trial={int(bool(end_trial_now))}:"
        f"items={item_fingerprint_hash}"
    )

    modify_params = {
        "items": modify_items,
        "proration_behavior": "always_invoice",
        "expand": ["latest_invoice.payment_intent"],
        "payment_behavior": "pending_if_incomplete",
    }
    if reset_billing_cycle:
        modify_params["billing_cycle_anchor"] = "now"
    if end_trial_now:
        modify_params["trial_end"] = "now"

    modified = await stripe.Subscription.modify_async(
        subscription_id,
        idempotency_key=idempotency_key,
        **modify_params,
    )

    latest_invoice = extract_latest_invoice_obj(modified)
    invoice_id = _extract_invoice_id(latest_invoice)
    invoice_status, invoice_url = _extract_invoice_status_and_url(latest_invoice)
    payment_intent_status = _extract_payment_intent_status(latest_invoice)
    amount_cents = latest_invoice.get("amount_due") or latest_invoice.get("amount_paid") or 0
    currency = (latest_invoice.get("currency") or "").upper()
    if not payment_intent_status and invoice_status == "paid":
        payment_intent_status = "succeeded"

    return {
        "subscription": modified,
        "idempotency_key": idempotency_key,
        "invoice_id": invoice_id,
        "invoice_status": invoice_status,
        "invoice_url": invoice_url,
        "amount_cents": amount_cents,
        "currency": currency,
        "payment_intent_status": payment_intent_status,
    }


def _extract_default_payment_method_or_source(obj) -> str:
    if not obj:
        return ""

    if isinstance(obj, dict):
        invoice_settings = obj.get("invoice_settings") or {}
        default_payment_method = (invoice_settings.get("default_payment_method") or "").strip() if isinstance(invoice_settings, dict) else ""
        if default_payment_method:
            return default_payment_method
        return (obj.get("default_source") or obj.get("default_payment_method") or "").strip()

    invoice_settings = getattr(obj, "invoice_settings", None)
    default_payment_method = (getattr(invoice_settings, "default_payment_method", "") or "").strip() if invoice_settings else ""
    if default_payment_method:
        return default_payment_method
    return (getattr(obj, "default_source", "") or getattr(obj, "default_payment_method", "") or "").strip()


@billing_enabled_guard({})
async def has_reusable_payment_method_async(*, customer_id: str = "", subscription=None) -> bool:
    """Return True when Stripe already has a reusable payment method/source for upgrades."""
    if _extract_default_payment_method_or_source(subscription):
        return True
    if not customer_id:
        return False

    try:
        customer = await stripe.Customer.retrieve_async(customer_id)
    except stripe.AuthenticationError:
        logging.warning(
            "Stripe API key missing while checking reusable payment method for customer %s; treating as unavailable.",
            customer_id,
        )
        return False

    return bool(_extract_default_payment_method_or_source(customer))


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


@billing_enabled_guard(None)
def create_or_get_portal_configuration():
    """Create or retrieve a fixed Billing Portal Configuration for self-service payment management."""
    cache_key = "saas:billing:portal_config:payment_method_only"
    if REDIS_CONN.is_alive():
        cached_id = REDIS_CONN.get(cache_key)
        if cached_id:
            return stripe.billing_portal.Configuration.retrieve(cached_id)

    configuration = stripe.billing_portal.Configuration.create(
        features={
            "invoice_history": {"enabled": True},
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
        price = stripe.Price.retrieve(price_id)
        result[price_id] = price.product

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


@billing_enabled_guard("")
async def get_receipt_url_from_intent_latest_charge_async(latest_charge_id: str) -> str:
    try:
        if latest_charge_id:
            charge = await stripe.Charge.retrieve_async(latest_charge_id)
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
                          Example: @check_resources(seats=1, apps=1, storage=1000)

    Usage:
        @check_resources(seats=1)  # Check for 1 additional seat
        @check_resources(apps=1)   # Check for 1 additional app
        @check_resources(storage=1000)  # Check for 1KB additional storage
        @check_resources(seats=1, apps=1)  # Check for both seats and apps
    """

    assert set(resource_deltas.keys()).issubset({"seats", "apps", "storage"}), f"resources should in ['seats', 'apps', 'storage'], get {resource_deltas}"
    from api.db.services.billing_service import SubscriptionService

    def decorator(f):
        @wraps(f)
        async def decorated_function(*args, **kwargs):
            from quart import current_app

            if settings.BILLING_ENABLED:
                from quart import g, request
                from api.apps import current_user
                from api.utils.api_utils import (
                    get_data_error_result,
                    get_resource_insufficient_result,
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

                check_ok, check_info = SubscriptionService.check_by_tenant_id(tenant_id, delta_app=delta_app, delta_members=delta_members, delta_kb_storage=delta_storage_bytes)

                if not check_ok:
                    check_info = _normalize_resource_check_failure(check_info, tenant_id)
                    return get_resource_insufficient_result(
                        code=check_info["code"],
                        message=check_info["message"],
                        detail=check_info["detail"],
                    )
            return await current_app.ensure_async(f)(*args, **kwargs)

        return decorated_function

    return decorator


def _normalize_resource_check_failure(check_info: dict, tenant_id: str) -> dict:
    check_info = dict(check_info or {})
    error_details = check_info.get("details", {}) or {}

    from api.db.services.user_service import UserTenantService

    tenant_email = UserTenantService.get_owner_email(tenant_id)
    raw_error = check_info.get("error", "")

    if "No active subscription" in raw_error:
        error_msg = f"Tenant {tenant_email} subscription is invalid"
        check_info.update(
            {
                "resource": "subscription",
                "code": RetCode.BILLING_SUBSCRIPTION_INVALID,
                "message": error_msg,
                "detail": {},
            }
        )
        return check_info

    resource_map = {
        "quota_points": (RetCode.BILLING_POINTS_INSUFFICIENT, "points"),
        "quota_apps": (RetCode.BILLING_APPS_INSUFFICIENT, "apps"),
        "quota_members": (RetCode.BILLING_SEATS_INSUFFICIENT, "seats"),
        "quota_storage": (RetCode.BILLING_STORAGE_INSUFFICIENT, "storage"),
    }
    error_messages = []
    error_code = RetCode.BILLING_RESOURCE_INSUFFICIENT
    identified_error_resource = ""

    if "quota_points" in error_details:
        error_messages.append(f"Insufficient points quota of tenant {tenant_email}. Current: {error_details['quota_points']['current']}, Limit: {error_details['quota_points']['limit']}")

    if "quota_apps" in error_details:
        error_messages.append(f"Insufficient app quota of tenant {tenant_email}. Current: {error_details['quota_apps']['current']}, Limit: {error_details['quota_apps']['limit']}")

    if "quota_members" in error_details:
        error_messages.append(f"Insufficient seat quota of tenant {tenant_email}. Current: {error_details['quota_members']['current']}, Limit: {error_details['quota_members']['limit']}")

    if "quota_storage" in error_details:
        error_messages.append(
            f"Insufficient storage quota of tenant {tenant_email}. Current: {error_details['quota_storage']['current']} Bytes, Limit: {error_details['quota_storage']['limit']} Bytes"
        )

    if len(error_messages) == 1:
        for detail_key, (code, resource_name) in resource_map.items():
            if detail_key in error_details:
                error_code = code
                identified_error_resource = detail_key
                resource = resource_name
                break
        else:
            resource = "resource"
    else:
        resource = "resource"

    if error_messages:
        error_msg = "; ".join(error_messages)
    else:
        error_msg = "Insufficient resources available. Contact the owner for further assistance."

    if identified_error_resource:
        detail = {
            "current": error_details[identified_error_resource]["current"],
            "limit": error_details[identified_error_resource]["limit"],
        }
    elif error_details:
        first_resource = next(iter(error_details.keys()))
        detail = {
            "current": error_details[first_resource]["current"],
            "limit": error_details[first_resource]["limit"],
        }
        resource = resource_map.get(first_resource, (None, "resource"))[1]
    else:
        detail = {}

    check_info.update(
        {
            "resource": resource,
            "code": error_code,
            "message": error_msg,
            "detail": detail,
        }
    )
    return check_info


def raise_dynamic_resource_error(check_info: dict, tenant_id: str, *, file_size: int | None = None) -> None:
    normalized = _normalize_resource_check_failure(check_info, tenant_id)
    resource = normalized.get("resource", "resource")
    detail = normalized.get("detail") or {}
    message = normalized.get("message") or normalized.get("error", "")

    current = int(detail.get("current", 0))
    limit = int(detail.get("limit", 0))

    if resource == "storage" and file_size is not None and message and "File size:" not in message:
        message = f"{message}. File size: {file_size} Bytes"

    raise InsufficientResourceError(
        resource=resource,
        current=current,
        limit=limit,
        message=message,
        file_size=file_size,
    )


def get_dynamic_resource_error_result(check_info: dict, tenant_id: str, *, file_size: int | None = None):
    normalized = _normalize_resource_check_failure(check_info, tenant_id)
    detail = normalized.get("detail") or {}
    message = normalized.get("message") or normalized.get("error", "")

    if normalized.get("resource") == "storage" and file_size is not None and message and "File size:" not in message:
        message = f"{message}. File size: {file_size} Bytes"

    from api.utils.api_utils import get_resource_insufficient_result

    return get_resource_insufficient_result(
        code=normalized["code"],
        message=message,
        detail=detail,
    )


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

    from api.db.services.billing_service import SubscriptionService

    if not tenant_id:
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

    if not tenant_id:
        return False, {"error": "Unable to determine tenant_id for resource checking."}

    delta_app = resource_deltas.get("apps", 0)
    delta_members = resource_deltas.get("seats", 0)
    delta_storage_bytes = resource_deltas.get("storage", 0)

    check_ok, check_info = SubscriptionService.check_by_tenant_id(
        tenant_id,
        delta_app=delta_app,
        delta_members=delta_members,
        delta_kb_storage=delta_storage_bytes,
    )
    if check_ok:
        return True, check_info
    return False, _normalize_resource_check_failure(check_info, tenant_id)
