from datetime import datetime, timedelta, timezone
from decimal import Decimal
from typing import Any, Optional
import inspect
from functools import wraps

import logging
import stripe

from common import settings


def to_utc_datetime(value: Any) -> Optional[datetime]:
    if value is None:
        return None
    if isinstance(value, datetime):
        if value.tzinfo is None:
            return value.replace(tzinfo=timezone.utc)
        return value.astimezone(timezone.utc)
    if isinstance(value, (int, float)):
        return datetime.fromtimestamp(value, tz=timezone.utc)
    if isinstance(value, str):
        text = value.strip()
        if not text:
            return None
        if text.endswith("Z"):
            text = f"{text[:-1]}+00:00"
        parsed = datetime.fromisoformat(text)
        if parsed.tzinfo is None:
            return parsed.replace(tzinfo=timezone.utc)
        return parsed.astimezone(timezone.utc)
    raise TypeError(f"Unsupported datetime value: {type(value)!r}")


def to_utc_date_str(value: Any) -> str:
    dt = to_utc_datetime(value)
    return dt.date().isoformat() if dt else ""


def to_utc_isoformat(value: datetime | str | None) -> str:
    dt = to_utc_datetime(value)
    if not dt:
        return ""
    dt = dt.replace(microsecond=0)
    return dt.isoformat().replace("+00:00", "Z")


def milliseconds_to_timestamp_seconds(timestamp_ms: int | float | str | None) -> Optional[int]:
    if timestamp_ms is None:
        return None
    if isinstance(timestamp_ms, str):
        text = timestamp_ms.strip()
        if not text:
            return None
        timestamp_ms = int(text)
    return int(float(timestamp_ms) / 1000)


def parse_datetime_arg(value: Any) -> Optional[datetime]:
    if value is None:
        return None
    if isinstance(value, str) and value.isdigit():
        return datetime.fromtimestamp(int(value), tz=timezone.utc)
    return to_utc_datetime(value)


def build_date_keys(start_dt: datetime, end_dt: datetime) -> list[str]:
    start_date = start_dt.date()
    end_date = end_dt.date()
    if end_date < start_date:
        return []
    days = (end_date - start_date).days
    return [(start_date + timedelta(days=i)).isoformat() for i in range(days + 1)]


def decimal_amount(value: Any) -> Decimal:
    if value is None:
        return Decimal("0")
    if isinstance(value, Decimal):
        return value
    return Decimal(str(value))


def amount_to_float(value: Any, places: int = 2) -> float:
    quantize_format = Decimal(f"1.{'0' * places}")
    return float(decimal_amount(value).quantize(quantize_format))


def normalize_stripe_payment_intent_status(status: str) -> str:
    if status == "succeeded":
        return "success"
    if status == "canceled":
        return "failed"
    return "pending"


def normalize_stripe_invoice_status(status: str) -> str:
    if status == "paid":
        return "success"
    if status in ("void", "uncollectible"):
        return "failed"
    return "pending"


def usage_based_status_from_payment_status(status: str) -> str:
    if status == "success":
        return "active"
    if status == "failed":
        return "canceled"
    return "pending"


def billing_enabled_guard(default):
    def decorator(func):
        if inspect.iscoroutinefunction(func):

            @wraps(func)
            async def async_wrapper(*args, **kwargs):
                if not settings.BILLING_ENABLED:
                    return default() if callable(default) else default
                return await func(*args, **kwargs)

            return async_wrapper

        @wraps(func)
        def sync_wrapper(*args, **kwargs):
            if not settings.BILLING_ENABLED:
                return default() if callable(default) else default
            return func(*args, **kwargs)

        return sync_wrapper

    return decorator


@billing_enabled_guard(None)
def init_stripe_api_key() -> None:
    api_key = settings.BILLING.get("stripe_api_key")
    api_version = settings.BILLING.get("stripe_api_version", "2026-04-22.dahlia")
    if api_key:
        stripe.api_key = api_key
        stripe.api_version = api_version
    else:
        logging.error("Stripe api_key is missing in billing settings.")
