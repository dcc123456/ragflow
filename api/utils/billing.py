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
import re
from decimal import ROUND_HALF_UP, Decimal
from functools import wraps

import stripe

from common import settings
from common.billing_utils import billing_enabled_guard
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


def cents_to_decimal(amount_in_cents: int, currency: str = "usd", decimal_places: int = 4) -> Decimal:
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
        "k": 1024,
        "kb": 1024,
        "m": 1024**2,
        "mb": 1024**2,
        "g": 1024**3,
        "gb": 1024**3,
        "t": 1024**4,
        "tb": 1024**4,
    }

    if unit not in units:
        raise ValueError(f"Unknown unit '{unit}' in storage size")

    return int(num * units[unit])


def get_trial_price_id(plans: list[dict]) -> str:
    from api.db.services.billing_service import BILLING_PLAN_TRIAL_NAME

    trial_plan = next((plan for plan in plans if plan.get("name") == BILLING_PLAN_TRIAL_NAME and plan.get("price_ids")), None)
    return trial_plan["price_ids"].split()[0] if trial_plan else ""


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
    except stripe.error.StripeError as e:
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
    except stripe.error.StripeError as e:
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
    except stripe.error.StripeError as e:
        print(f"An error occurred: {str(e)}")

    return {}


@billing_enabled_guard("")
def create_stripe_customer_id(tenant_id: str) -> str:
    from api.db.services.user_service import UserService

    user = UserService.filter_by_id(tenant_id)
    if not user:
        logging.warning(f"create_stripe_customer_id: tenant {tenant_id} not found")
        return ""
    customer = stripe.Customer.create(name=user.nickname, email=user.email, metadata={"tenant_id": tenant_id})
    logging.info(f"created customer {customer.id} for tenant {tenant_id},name {user.nickname} email {user.email}.")
    return customer.id


@billing_enabled_guard("")
def billing_set_customer_id(tenant_id: str, customer_id: str = "") -> str:
    from api.db.services.billing_service import SubscriptionService

    if not customer_id:
        customer_id = create_stripe_customer_id(tenant_id)
    if customer_id:
        SubscriptionService.set_customer_id(tenant_id, customer_id)
    return customer_id


async def billing_set_customer_id_async(tenant_id: str, customer_id: str = "") -> str:
    import asyncio

    return await asyncio.to_thread(billing_set_customer_id, tenant_id, customer_id)


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
                    delta_kb_storage = resource_deltas.get("storage", 0)

                    check_ok, check_info = PurchasedProductOverviewService.check_subscription_by_tenant_id(
                        tenant_id, delta_app=delta_app, delta_members=delta_members, delta_kb_storage=delta_kb_storage
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
                            error_messages.append(f"Insufficient storage quota. Current: {error_details['quota_kb_storage']['current']} KB, Limit: {error_details['quota_kb_storage']['limit']} KB")
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
                          Example: check_dynamic_resources(tenant_id, storage=file_size_in_kb)

    Returns:
        tuple: (check_ok, check_info) where check_ok is a boolean indicating if check passed,
               and check_info contains details about the check result.

    Usage:
        # In a function where you calculate file size during execution:
        file_size_kb = calculate_file_size_in_kb(file_path)
        check_ok, check_info = check_dynamic_resources(tenant_id, storage=file_size_kb)
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
    delta_kb_storage = resource_deltas.get("storage", 0)

    return PurchasedProductOverviewService.check_subscription_by_tenant_id(tenant_id, delta_app=delta_app, delta_members=delta_members, delta_kb_storage=delta_kb_storage)
