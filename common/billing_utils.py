from datetime import datetime, timedelta, timezone
from decimal import Decimal
from typing import Any, Optional
import inspect
from functools import wraps

import logging
import stripe

from common import settings
from common.misc_utils import get_uuid
from common.time_utils import current_timestamp, datetime_format
from api.db import FileType
from api.db.db_models import DB, ProductUsageTracing


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


def record_deepdoc_usage(task: dict, pages: int) -> None:
    if pages <= 0:
        return
    if task.get("type") != FileType.PDF.value:
        return
    if task.get("parser_config", {}).get("layout_recognize", "DeepDOC") != "DeepDOC":
        return

    from api.db.services.billing_service import LocalPriceService, PricePointService, ProductService

    product_name = "deepdoc"
    product = ProductService.get_by_name(product_name) or {}
    price_point = PricePointService.get_by_name(product_name) or {}
    local_price = LocalPriceService.get_by_name(product_name) or {}

    product_id = product.get("id")
    price_point_id = price_point.get("id")
    local_price_id = local_price.get("id")
    if not product_id or not price_point_id or not local_price_id:
        logging.warning("Skip usage tracing: missing billing config for deepdoc.")
        return

    unit_quantity = price_point.get("unit_quantity") or 0
    price_amount = local_price.get("amount")
    currency = local_price.get("currency") or "usd"
    total_cost = decimal_amount(price_amount) * decimal_amount(pages) / decimal_amount(unit_quantity) if price_amount and unit_quantity else decimal_amount(0)

    usage_record = {
        "id": get_uuid(),
        "tenant_id": task.get("tenant_id", ""),
        "product_id": product_id,
        "price_point_id": price_point_id,
        "local_price_id": local_price_id,
        "task_quantity": pages,
        "total_cost": total_cost,
        "currency": currency,
        "status": "waiting",
        "description": "",
        "create_time": current_timestamp(),
        "create_date": datetime_format(datetime.now(timezone.utc)),
        "update_time": current_timestamp(),
        "update_date": datetime_format(datetime.now(timezone.utc)),
    }
    try:
        with DB.connection_context():
            ProductUsageTracing.insert(**usage_record).execute()
    except Exception as e:
        logging.warning(f"Failed to record deepdoc usage: {e}")

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
    if api_key:
        stripe.api_key = api_key
    else:
        logging.error("Stripe api_key is missing in billing settings.")
