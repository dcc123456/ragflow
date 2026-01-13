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
from api.db.db_models import DB
from peewee import fn
from api.db.services.common_service import CommonService
from api.db.services.user_service import UserTenantService
from common import settings
from common.misc_utils import get_uuid
import logging
from datetime import datetime, timedelta, timezone

from api.db import SubscriptionStatus
from api.db.db_models import (
    BillingWebhookEvent,
    LocalPrice,
    PaymentOrder,
    PricePoint,
    Product,
    PurchasedProductOverview,
    Subscription,
    UsageBased,
)
from api.db.services.dialog_service import DialogService
from api.db.services.document_service import DocumentService
from api.utils.billing import create_stripe_customer_id, get_trial_price_id, parse_storage_size
from common.billing_utils import to_utc_datetime
from common.time_utils import current_timestamp

BILLING_PLAN_TRIAL_NAME = "Trial"


class ProductService(CommonService):
    model = Product
    VERSION_CHECK_FIELDS = ["quota_apps", "quota_members", "quota_kb_storage"]

    @classmethod
    @DB.connection_context()
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_name(cls, product_name: str) -> dict | None:
        fields = [
            cls.model.id,
            cls.model.name,
            cls.model.quota_apps,
            cls.model.quota_members,
            cls.model.quota_kb_storage,
            cls.model.task_priority,
            cls.model.version,
        ]
        plan = cls.model.select(*fields).where(cls.model.name == product_name).order_by(cls.model.version.desc()).dicts().first()
        return plan

    @classmethod
    def init_data(cls, billing_plans: list[dict]) -> None:
        try:
            for plan in billing_plans:
                if "quota_kb_storage" in plan and isinstance(plan["quota_kb_storage"], str):
                    plan["quota_kb_storage"] = parse_storage_size(plan["quota_kb_storage"])

                ori_product = cls.get_by_name(plan["name"])

                if not ori_product:
                    cls.save(**plan, version=1)
                    logging.info(f"Create billing product {plan}.")
                    continue

                is_outdated = any(plan.get(field, "") != ori_product.get(field, "") for field in cls.VERSION_CHECK_FIELDS if plan.get(field, ""))

                if is_outdated:
                    # may have race condition, if launch multiple product-changed config instance concurrently.
                    new_version = ori_product["version"] + 1
                    cls.save(**plan, version=new_version)
                    logging.info(f"Billing product \n\t{ori_product} updated to \n\t{plan}.")
        except Exception as e:
            logging.warning(f"Init product data error for {plan['name']}: {e}")

    @classmethod
    @DB.connection_context()
    def get_latest_by_type(cls, product_type: str):
        max_versions = (
            cls.model.select(
                cls.model.name,
                fn.MAX(cls.model.version).alias("max_version"),
            )
            .where(cls.model.product_type == product_type)
            .group_by(cls.model.name)
        )
        return (
            cls.model.select()
            .join(
                max_versions,
                on=(
                    (cls.model.name == max_versions.c.name)
                    & (cls.model.version == max_versions.c.max_version)
                ),
            )
            .where(cls.model.product_type == product_type)
        )


class SubscriptionService(CommonService):
    model = Subscription

    @classmethod
    @DB.connection_context()
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_tenant_id(cls, tenant_id: str, require_quota_info: bool = False) -> dict:
        fields = [
            cls.model.tenant_id,
            cls.model.customer_id,
            cls.model.subscription_id,
            cls.model.subscription_status,
            cls.model.product_id,
            cls.model.plan_name,
            cls.model.price_id,
            cls.model.start_time,
            cls.model.end_time,
            cls.model.invoice_url,
            cls.model.invoice_pdf_url,
            cls.model.original_subscription_id,
        ]
        tenant_plan = cls.model.select(*fields).where(cls.model.tenant_id == tenant_id).order_by(cls.model.getter_by("create_time").desc()).dicts().first()
        if not tenant_plan:
            logging.warning(f"Tenant {tenant_id} plan not found, use trial plan")
            customer_id = create_stripe_customer_id(tenant_id) if settings.BILLING_ENABLED else ""
            tenant_plan = cls._build_trial_subscription(tenant_id, customer_id)
            if settings.BILLING_ENABLED and customer_id:
                SubscriptionService.save(**tenant_plan)
            else:
                return tenant_plan
        print(f"{tenant_plan=}")
        billing_plan = ProductService.get_by_name(tenant_plan["plan_name"])
        assert billing_plan is not None
        plan_name = billing_plan.pop("name")
        billing_plan["plan_name"] = plan_name
        tenant_plan.update(billing_plan)

        if require_quota_info:  # app = Chat, Search, Agent
            from api.db.services.canvas_service import UserCanvasService
            from api.db.services.knowledgebase_service import KnowledgebaseService
            from api.db.services.search_service import SearchService

            num_apps = (
                DialogService.count_by_tenant_id(tenant_id)
                + KnowledgebaseService.count_by_tenant_id(tenant_id)
                + UserCanvasService.count_by_tenant_id(tenant_id)
                + SearchService.count_by_tenant_id(tenant_id)
            )

            num_members = UserTenantService.get_num_members(tenant_id)
            num_kb_storage = 0
            kb_ids = KnowledgebaseService.get_kb_ids(tenant_id)
            if kb_ids:
                for kb_id in kb_ids:
                    num_kb_storage += DocumentService.get_total_size_by_kb_id(kb_id=kb_id, keywords="", run_status=[], types=[])
            tenant_plan["num_apps"] = num_apps
            tenant_plan["num_members"] = num_members
            tenant_plan["num_kb_storage"] = num_kb_storage
        return tenant_plan

    @classmethod
    @DB.connection_context()
    def set_customer_id(cls, tenant_id: str, customer_id: str):
        tenant_plan = cls.get_by_tenant_id(tenant_id)
        if not tenant_plan.get("customer_id"):
            updated = cls.model.update(customer_id=customer_id, plan_name=BILLING_PLAN_TRIAL_NAME).where(cls.model.tenant_id == tenant_id).execute()
            if not updated:
                tenant_plan = cls._build_trial_subscription(tenant_id, customer_id)
                cls.model.insert(**tenant_plan).execute()

    @classmethod
    def _build_trial_subscription(cls, tenant_id: str, customer_id: str) -> dict:
        now = datetime.now(timezone.utc)
        trial_product = ProductService.get_by_name(BILLING_PLAN_TRIAL_NAME) or {}
        trial_price_id = get_trial_price_id(settings.BILLING.get("billing_plans", []))
        price_ids = trial_product.get("price_ids", "")
        fallback_price_id = price_ids.split()[0] if price_ids else ""
        return {
            "id": get_uuid(),
            "tenant_id": tenant_id,
            "customer_id": customer_id or "",
            "product_id": trial_product.get("id", ""),
            "plan_name": BILLING_PLAN_TRIAL_NAME,
            "order_id": f"trial_{get_uuid()}",
            "status": SubscriptionStatus.ACTIVE,
            "price_id": trial_price_id or fallback_price_id,
            "subscription_id": "",
            "subscription_status": SubscriptionStatus.ACTIVE,
            "invoice_id": "",
            "invoice_url": "",
            "invoice_pdf_url": "",
            "start_time": to_utc_datetime(now),
            "end_time": to_utc_datetime(now + timedelta(days=365)),
            "renew_time": None,
            "original_subscription_id": "",
        }

    @classmethod
    @DB.connection_context()
    def get_tenant_id_by_customer_id(cls, customer_id: str) -> str:
        tenant = cls.model.select(cls.model.tenant_id).where(cls.model.customer_id == customer_id).first()
        if tenant:
            return tenant.tenant_id
        return None

    @classmethod
    @DB.connection_context()
    def check_by_tenant_id(
        cls,
        tenant_id: str,
        # customer_id: str  # QUESTION: is that possible many tenant_ids share the same customer_id?
        delta_app=0,
        delta_members: int = 0,
        delta_kb_storage: int = 0,
    ) -> tuple[bool, dict]:
        subscription = cls.get_by_tenant_id(tenant_id, require_quota_info=True)
        if not subscription:
            return (
                False,
                {
                    "error": f"No valid activate Subscription found for tenant {tenant_id}",
                    "tenant_id": tenant_id,
                },
            )
        plan_name = subscription.get("plan_name", "")
        num_apps = subscription.get("num_apps", 0)
        num_members = subscription.get("num_members", 0)
        num_kb_storage = subscription.get("num_kb_storage", 0)

        fields = [
            cls.model.id,
            cls.model.tenant_id,
            cls.model.end_time,
            Product.name,
            Product.id.alias("product_id"),
            Product.quota_apps,
            Product.quota_members,
            Product.quota_kb_storage,
            Product.task_priority,
            Product.version,
        ]
        tenant_plan_info = (
            cls.model.select(*fields)
            .join(Product, on=(cls.model.plan_name == Product.name))
            .where((cls.model.tenant_id == tenant_id) & (cls.model.subscription_status == SubscriptionStatus.ACTIVE))
            .order_by(
                Product.version.desc(),
                cls.model.create_time.desc(),
            )
            .dicts()
            .first()
        )

        if not tenant_plan_info:
            return (
                False,
                {
                    "error": f"No valid activate Subscription found for tenant {tenant_id} and subscription {plan_name}",
                    "plan_name": plan_name,
                    "tenant_id": tenant_id,
                },
            )

        error_message = ""
        check_pass = True
        details = {}

        if delta_app > 0:
            details["quota_apps"] = {
                "current": num_apps,
                "limit": tenant_plan_info["quota_apps"],
            }
            if num_apps + delta_app > tenant_plan_info["quota_apps"]:
                error_message += "App quota exceeded\n"
                check_pass = False

        if delta_members > 0:
            details["quota_members"] = {
                "current": num_members,
                "limit": tenant_plan_info["quota_members"],
            }
            if num_members + delta_members > tenant_plan_info["quota_members"]:
                error_message += "Members quota exceeded\n"
                check_pass = False

        if delta_kb_storage > 0:
            details["quota_kb_storage"] = {
                "current": num_kb_storage,
                "limit": tenant_plan_info["quota_kb_storage"],
            }
            if num_kb_storage + delta_kb_storage > tenant_plan_info["quota_kb_storage"]:
                error_message += "KB storage quota exceeded\n"
                check_pass = False

        if not check_pass:
            return (
                False,
                {
                    "error": error_message,
                    "details": details,
                    "plan_name": plan_name,
                    "tenant_id": tenant_id,
                },
            )

        return (
            True,
            {
                "product_id": tenant_plan_info["product_id"],
                "plan_name": plan_name,
                "end_time": tenant_plan_info["end_time"],
                "details": details,
                "tenant_id": tenant_id,
            },
        )

    @classmethod
    @DB.connection_context()
    def get_priority(cls, tenant_id: str) -> int:
        fields = [
            cls.model.tenant_id,
            cls.model.subscription_status,
            cls.model.plan_name,
        ]
        tenant_plan = cls.model.select(*fields).where(cls.model.tenant_id == tenant_id).order_by(cls.model.getter_by("create_time").desc()).dicts().first()
        if not tenant_plan:
            return 0
        subscription_status = tenant_plan["subscription_status"]
        if subscription_status != "active":
            return 0
        return 1

    @classmethod
    def update_subscription(cls, tenant_id, subscription_dict):
        """
        ! Use this method under DB.atomic() context
        """
        if subscription_dict:
            subscription_dict["update_time"] = current_timestamp()
            subscription_dict["update_date"] = to_utc_datetime(datetime.now(timezone.utc))
            cls.model.update(subscription_dict).where(cls.model.tenant_id == tenant_id).execute()


####################################################################################


class UsageBasedService(CommonService):
    model = UsageBased

    @classmethod
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_tenant_id(cls, tenant_id: str, start_time=None) -> dict:
        fields = [
            cls.model.tenant_id,
            cls.model.customer_id,
            cls.model.payment_id,
            cls.model.payment_status,
            cls.model.product_name,
        ]

        query = cls.model.select(*fields).where(cls.model.tenant_id == tenant_id)

        if start_time:
            query = query.where(cls.model.create_time >= start_time)

        purchased_usage_based = query.order_by(cls.model.create_time.desc()).dicts().first()
        return purchased_usage_based

    @classmethod
    @DB.connection_context()
    def set_customer_id(cls, tenant_id: str, customer_id: str):
        _updated = cls.model.update(customer_id=customer_id).where(cls.model.tenant_id == tenant_id).execute()

    @classmethod
    @DB.connection_context()
    def get_tenant_id_by_customer_id(cls, customer_id: str) -> str:
        tenant = cls.model.select(cls.model.tenant_id).where(cls.model.customer_id == customer_id).first()
        if tenant:
            return tenant.tenant_id
        return None

    @classmethod
    @DB.connection_context()
    def get_by_payment_id(cls, payment_id: str) -> dict | None:
        if not payment_id:
            return None
        return cls.model.select().where(cls.model.payment_id == payment_id).dicts().first()

    @classmethod
    @DB.connection_context()
    def get_by_order_id(cls, order_id: str) -> dict | None:
        if not order_id:
            return None
        return cls.model.select().where(cls.model.order_id == order_id).dicts().first()


class PaymentOrderService(CommonService):
    model = PaymentOrder

    @classmethod
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_order_id(cls, order_id: str) -> dict | None:
        if not order_id:
            return None
        return cls.model.select().where(cls.model.order_id == order_id).dicts().first()

    @classmethod
    @DB.connection_context()
    def get_by_payment_intent_id(cls, payment_intent_id: str) -> dict | None:
        if not payment_intent_id:
            return None
        return cls.model.select().where(cls.model.payment_intent_id == payment_intent_id).dicts().first()


class BillingWebhookEventService(CommonService):
    model = BillingWebhookEvent

    @classmethod
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_event_id(cls, event_id: str) -> dict | None:
        if not event_id:
            return None
        return cls.model.select().where(cls.model.event_id == event_id).dicts().first()


class PricePointService(CommonService):
    model = PricePoint
    VERSION_CHECK_FIELDS = ["unit_quantity"]

    @classmethod
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_name(cls, price_point_product_name: str) -> dict | None:
        fields = [
            cls.model.id,
            cls.model.product_id,
            cls.model.product_name,
            cls.model.price_type,
            cls.model.billing_frequency,
            cls.model.unit,
            cls.model.unit_quantity,
            cls.model.effective_time,
            cls.model.expiry_time,
        ]
        now_utc = datetime.now(timezone.utc)
        price_point = (
            cls.model.select(*fields)
            .where(
                (cls.model.product_name == price_point_product_name)
                & (cls.model.expiry_time.is_null(True) | (cls.model.expiry_time >= now_utc))
            )
            .order_by(cls.model.effective_time.desc())
            .dicts()
            .first()
        )
        return price_point

    @classmethod
    def init_data(cls, price_point_list: list[dict]) -> None:
        try:
            for price_point in price_point_list:
                ori_price_point = cls.get_by_name(price_point["product_name"])

                product_id = ProductService.get_by_name(price_point["product_name"]).get("id", "")
                if not ori_price_point:
                    cls.save(**price_point, product_id=product_id, effective_time=to_utc_datetime(datetime.now(timezone.utc)))
                    logging.info(f"Create billing price point {price_point}.")
                    continue

                is_outdated = any(price_point.get(field, "") != ori_price_point.get(field, "") for field in cls.VERSION_CHECK_FIELDS if price_point.get(field, ""))

                if is_outdated:
                    cls.save(
                        **price_point,
                        product_id=ori_price_point.get("product_id", ""),
                        effective_time=to_utc_datetime(datetime.now(timezone.utc)),
                    )
                    logging.info(f"Billing price point \n\t{ori_price_point} updated to \n\t{price_point}.")
        except Exception as e:
            logging.warning(f"Init billing price point data error for {price_point['product_name']}: {e}")


class LocalPriceService(CommonService):
    model = LocalPrice
    VERSION_CHECK_FIELDS = ["amount"]

    @classmethod
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_name(cls, local_price_product_name: str) -> dict | None:
        fields = [
            cls.model.id,
            cls.model.price_point_id,
            cls.model.product_name,
            cls.model.amount,
            cls.model.currency,
            cls.model.point_value,
            cls.model.effective_time,
            cls.model.expiry_time,
        ]
        now_utc = datetime.now(timezone.utc)
        local_price = (
            cls.model.select(*fields)
            .where(
                (cls.model.product_name == local_price_product_name)
                & (cls.model.expiry_time.is_null(True) | (cls.model.expiry_time >= now_utc))
            )
            .order_by(cls.model.effective_time.desc())
            .dicts()
            .first()
        )
        return local_price

    @classmethod
    def init_data(cls, local_price_list: list[dict]) -> None:
        try:
            for local_price in local_price_list:
                ori_local_price = cls.get_by_name(local_price["product_name"])

                price_point_id = PricePointService.get_by_name(local_price["product_name"]).get("id", "")
                product_id = ProductService.get_by_name(local_price["product_name"]).get("id", "")

                if not ori_local_price:
                    cls.save(
                        **local_price,
                        price_point_id=price_point_id,
                        product_id=product_id,
                        effective_time=to_utc_datetime(datetime.now(timezone.utc)),
                    )
                    logging.info(f"Create billing local price {local_price}.")
                    continue

                is_outdated = any(local_price.get(field, "") != ori_local_price.get(field, "") for field in cls.VERSION_CHECK_FIELDS if local_price.get(field, ""))

                if is_outdated:
                    cls.save(
                        **local_price,
                        price_point_id=price_point_id,
                        product_id=product_id,
                        effective_time=to_utc_datetime(datetime.now(timezone.utc)),
                    )
                    logging.info(f"Billing local price \n\t{ori_local_price} updated to \n\t{local_price}.")
        except Exception as e:
            logging.warning(f"Init billing local price data error for {local_price['product_name']}: {e}")


class PurchasedProductOverviewService(CommonService):
    model = PurchasedProductOverview

    @classmethod
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_product_name_and_tenant_id(cls, product_name: str, tenant_id: str) -> dict | None:
        fields = [
            cls.model.id,
            cls.model.tenant_id,
            cls.model.product_name,
            cls.model.quantity,
            cls.model.effective_time,
            cls.model.expiry_time,
        ]
        purchased_overview = cls.model.select(*fields).where((cls.model.product_name == product_name) & (cls.model.tenant_id == tenant_id)).order_by(cls.model.effective_time.desc()).dicts().first()
        return purchased_overview

    @classmethod
    def update_quantity(cls, product_name: str, tenant_id: str, delta: int) -> bool:
        try:
            updated = (
                cls.model.update(quantity=cls.model.quantity + delta).where((cls.model.product_name == product_name) & (cls.model.tenant_id == tenant_id) & (cls.model.quantity + delta >= 0)).execute()
            )

            return updated > 0
        except Exception as e:
            logging.error(f"Update overview quantity error: {e}")
            return False

    @classmethod
    @DB.connection_context()
    def check_usage_based_by_tenant_id(
        cls,
        tenant_id: str,
        # customer_id: str  # QUESTION: is that possible many tenant_ids share the same customer_id?
        product_name: str,
        delta_page: int = 0,
        delta_token: int = 0,
    ) -> tuple[bool, dict]:
        """
        (is_pass, info)
        """
        if delta_page < 0 or delta_token < 0:
            return (
                False,
                {"error": "Delta values must be non-negative."},
            )

        fields = [
            cls.model.id,
            cls.model.tenant_id,
            cls.model.product_name,
            cls.model.quantity,
            cls.model.effective_time,
            cls.model.expiry_time,
        ]

        purchased_overview = (
            cls.model.select(*fields)
            .where((cls.model.product_name == product_name) & (cls.model.tenant_id == tenant_id) & (cls.model.quantity > 0))
            .order_by(cls.model.effective_time.desc())
            .dicts()
            .first()
        )

        if not purchased_overview:
            return (
                False,
                {
                    "error": f"No valid purchased product found for tenant {tenant_id} and product {product_name}",
                    "product_name": product_name,
                    "tenant_id": tenant_id,
                },
            )

        now = datetime.now(timezone.utc)
        expiry_time = to_utc_datetime(purchased_overview["expiry_time"])
        if expiry_time and expiry_time < now:
            return (
                False,
                {
                    "error": "Product has expired.",
                    "expiry_time": expiry_time,
                    "current_time": now,
                },
            )

        remaining_quantity = purchased_overview["quantity"]

        if "deepdoc" in product_name.lower():
            is_enough = remaining_quantity >= delta_page
            remaining = remaining_quantity - delta_page
            resource_type = "page"
        elif "token" in product_name.lower():
            is_enough = remaining_quantity >= delta_token
            remaining = remaining_quantity - delta_token
            resource_type = "token"
        else:
            logging.error(f"Unhandled product_name in purchased overview check_usage_based_by_tenant_id. {tenant_id=}, {product_name=}")
            return (
                False,
                {
                    "error": "internal error",
                },
            )

        if not is_enough:
            return (
                False,
                {
                    "error": f"Insufficient {resource_type}.",
                    "requested": delta_page if "page" in product_name.lower() else delta_token,
                    "remaining": remaining_quantity,
                    "product_id": purchased_overview["id"],
                    "product_name": product_name,
                    "tenant_id": tenant_id,
                },
            )

        return (
            True,
            {
                "remaining": remaining,
                "product_id": purchased_overview["id"],
                "product_name": product_name,
                "expiry_time": purchased_overview["expiry_time"],
                "tenant_id": tenant_id,
            },
        )

    @classmethod
    @DB.connection_context()
    def check_subscription_by_tenant_id(
        cls,
        tenant_id: str,
        # customer_id: str  # QUESTION: is that possible many tenant_ids share the same customer_id?
        delta_app=0,
        delta_members: int = 0,
        delta_kb_storage: int = 0,
    ) -> tuple[bool, dict]:
        """
        alias of Subscription.check_by_tenant_id
        """
        return SubscriptionService.check_by_tenant_id(tenant_id, delta_app, delta_members, delta_kb_storage)
