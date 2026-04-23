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
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone

from api.db.db_models import DB
from api.db import FileType, SubscriptionStatus
from api.db.services.common_service import CommonService
from api.db.services.user_service import UserTenantService
from common import settings
from common.misc_utils import get_uuid
from peewee import fn
from peewee import IntegrityError
from api.db.db_models import (
    BillingWebhookEvent,
    LocalPrice,
    PaymentOrder,
    PointAccount,
    PointHold,
    PointLedger,
    PricePoint,
    Product,
    PurchasedProductOverview,
    StorageSubscription,
    Subscription,
)
from api.db.services.dialog_service import DialogService
from api.db.services.file_service import FileService
from api.db.services.memory_service import MemoryService
from api.utils.file_utils import filename_type
from api.utils.billing import BILLING_PLAN_TRIAL_NAME, create_stripe_customer_id, get_trial_price_id, parse_storage_size
from common.billing_utils import to_utc_datetime
from common.time_utils import current_timestamp
from deepdoc.parser import PdfParser
import stripe

ENTITLED_MAIN_SUBSCRIPTION_STATUSES = {"active", "trialing"}


class ProductService(CommonService):
    model = Product
    VERSION_CHECK_FIELDS = ["quota_apps", "quota_members", "quota_kb_storage", "task_priority", "price_ids", "product_type", "usage_stat_type"]

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
            cls.model.price_ids,
            cls.model.product_type,
            cls.model.usage_stat_type,
            cls.model.version,
        ]
        plan = cls.model.select(*fields).where(cls.model.name == product_name).order_by(cls.model.version.desc()).dicts().first()
        return plan

    @classmethod
    def init_data(cls, billing_plans: list[dict]) -> None:
        try:
            for plan in billing_plans:
                if "quota_kb_storage" in plan and isinstance(plan["quota_kb_storage"], str):
                    plan["quota_kb_storage"] = parse_storage_size(plan["quota_kb_storage"]) // 1024

                ori_product = cls.get_by_name(plan["name"])

                if not ori_product:
                    cls.save(**plan, version=1)
                    logging.info(f"Create billing product {plan}.")
                    continue

                is_outdated = any(plan.get(field) != ori_product.get(field) for field in cls.VERSION_CHECK_FIELDS if field in plan)

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

        tenant_plan = cls._ensure_trial_stripe_subscription(tenant_plan)
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
                + MemoryService.count_by_tenant_id(tenant_id)
            )

            num_members = UserTenantService.get_num_members(tenant_id)
            num_kb_storage = FileService.get_total_size_by_tenant_id(tenant_id) // 1024
            tenant_plan["num_apps"] = num_apps
            tenant_plan["num_members"] = num_members
            tenant_plan["num_kb_storage"] = num_kb_storage
        return tenant_plan

    @classmethod
    def _ensure_trial_stripe_subscription(cls, tenant_plan: dict) -> dict:
        """
        Ensure Trial/Free tenants have a Stripe subscription so we can schedule
        plan changes at period end consistently.
        """
        if not settings.BILLING_ENABLED:
            return tenant_plan
        if tenant_plan.get("plan_name") != BILLING_PLAN_TRIAL_NAME:
            return tenant_plan
        if tenant_plan.get("subscription_id"):
            return tenant_plan

        trial_price_id = get_trial_price_id(settings.BILLING.get("billing_plans", []))
        if not trial_price_id:
            return tenant_plan

        customer_id = (tenant_plan.get("customer_id") or "").strip()
        if not customer_id:
            customer_id = create_stripe_customer_id(tenant_plan.get("tenant_id", ""))
            if not customer_id:
                return tenant_plan

        try:
            stripe_subscription = stripe.Subscription.create(
                customer=customer_id,
                items=[{"price": trial_price_id, "quantity": 1}],
                metadata={
                    "price_type": "subscription",
                    "tenant_id": tenant_plan.get("tenant_id", ""),
                    "price_id": trial_price_id,
                    "product_name": BILLING_PLAN_TRIAL_NAME,
                },
            )
        except Exception as e:
            logging.warning(f"Failed to create Trial Stripe subscription for tenant {tenant_plan.get('tenant_id')}: {e}")
            return tenant_plan

        subscription_id = getattr(stripe_subscription, "id", "") if not isinstance(stripe_subscription, dict) else stripe_subscription.get("id", "")
        subscription_status = getattr(stripe_subscription, "status", "") if not isinstance(stripe_subscription, dict) else stripe_subscription.get("status", "")
        current_period_start = (
            getattr(stripe_subscription, "current_period_start", None) if not isinstance(stripe_subscription, dict) else stripe_subscription.get("current_period_start")
        )
        current_period_end = (
            getattr(stripe_subscription, "current_period_end", None) if not isinstance(stripe_subscription, dict) else stripe_subscription.get("current_period_end")
        )

        update_dict = {
            "customer_id": customer_id,
            "price_id": trial_price_id,
            "subscription_id": subscription_id,
            "subscription_status": subscription_status or SubscriptionStatus.ACTIVE,
            "start_time": to_utc_datetime(current_period_start) or tenant_plan.get("start_time"),
            "end_time": to_utc_datetime(current_period_end) or tenant_plan.get("end_time"),
        }
        if not tenant_plan.get("original_subscription_id"):
            update_dict["original_subscription_id"] = subscription_id

        try:
            with DB.atomic():
                cls.model.update(update_dict).where(cls.model.tenant_id == tenant_plan.get("tenant_id")).execute()
        except Exception as e:
            logging.warning(f"Failed to persist Trial Stripe subscription for tenant {tenant_plan.get('tenant_id')}: {e}")
            return tenant_plan

        tenant_plan.update(update_dict)
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
            .where(
                (cls.model.tenant_id == tenant_id)
                & (cls.model.subscription_status.in_(ENTITLED_MAIN_SUBSCRIPTION_STATUSES))
            )
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
            # Storage add-on quota now comes from recurring storage subscription.
            add_on_storage_kb = StorageSubscriptionService.effective_storage_kb(tenant_id)

            storage_limit_kb = int(tenant_plan_info["quota_kb_storage"] or 0) + add_on_storage_kb
            details["quota_kb_storage"] = {
                "current": num_kb_storage,
                "limit": storage_limit_kb,
            }
            if num_kb_storage + delta_kb_storage > storage_limit_kb:
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
        if subscription_status not in ENTITLED_MAIN_SUBSCRIPTION_STATUSES:
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


class StorageSubscriptionService(CommonService):
    model = StorageSubscription
    BLOCKING_PENDING_ACTIONS = {"create", "increase", "align"}

    @classmethod
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_tenant_id(cls, tenant_id: str) -> dict | None:
        if not tenant_id:
            return None
        return cls.model.select().where(cls.model.tenant_id == tenant_id).dicts().first()

    @classmethod
    @DB.connection_context()
    def get_by_subscription_id(cls, subscription_id: str) -> dict | None:
        if not subscription_id:
            return None
        return cls.model.select().where(cls.model.subscription_id == subscription_id).dicts().first()

    @classmethod
    @DB.connection_context()
    def upsert_by_tenant_id(cls, tenant_id: str, **kwargs) -> bool:
        if not tenant_id:
            return False

        update_dict = kwargs.copy()
        update_dict["update_time"] = current_timestamp()
        update_dict["update_date"] = to_utc_datetime(datetime.now(timezone.utc))

        exists = cls.model.select(cls.model.id).where(cls.model.tenant_id == tenant_id).first()
        if exists:
            cls.model.update(update_dict).where(cls.model.tenant_id == tenant_id).execute()
            return True

        insert_dict = {
            "id": get_uuid(),
            "tenant_id": tenant_id,
            "customer_id": kwargs.get("customer_id", ""),
            "subscription_id": kwargs.get("subscription_id", ""),
            "subscription_item_id": kwargs.get("subscription_item_id", ""),
            "price_id": kwargs.get("price_id", ""),
            "schedule_id": kwargs.get("schedule_id", ""),
            "effective_quantity_gb": kwargs.get("effective_quantity_gb", 0),
            "target_quantity_gb": kwargs.get("target_quantity_gb", 0),
            "pending_quantity_gb": kwargs.get("pending_quantity_gb"),
            "pending_effective_at": kwargs.get("pending_effective_at"),
            "pending_action": kwargs.get("pending_action", ""),
            "current_period_start": kwargs.get("current_period_start"),
            "current_period_end": kwargs.get("current_period_end"),
            "cancel_at_period_end": kwargs.get("cancel_at_period_end", False),
            "status": kwargs.get("status", ""),
        }
        cls.model.insert(**insert_dict).execute()
        return True

    @classmethod
    @DB.connection_context()
    def has_blocking_pending_by_tenant_id(cls, tenant_id: str) -> bool:
        row = cls.get_by_tenant_id(tenant_id)
        if not row:
            return False
        pending_quantity = row.get("pending_quantity_gb")
        pending_action = (row.get("pending_action") or "").strip().lower()
        if pending_quantity is None or pending_action not in cls.BLOCKING_PENDING_ACTIONS:
            return False
        status = (row.get("status") or "").strip().lower()
        if status in {"canceled", "incomplete_expired"}:
            return False
        return True

    @classmethod
    def effective_storage_kb(cls, tenant_id: str) -> int:
        row = cls.get_by_tenant_id(tenant_id)
        if not row:
            return 0
        status = (row.get("status") or "").strip().lower()
        if status in {"canceled", "incomplete_expired"}:
            return 0
        quantity_gb = int(row.get("effective_quantity_gb") or 0)
        return max(quantity_gb, 0) * 1024 * 1024


####################################################################################


# -----------------------------------------------------------------------------
# Deprecated: UsageBasedService
# -----------------------------------------------------------------------------
# The app currently does not use the legacy `billing_usage_based` table for
# usage-based purchases. We keep this code commented out for potential future
# extension (e.g., a dedicated usage-based purchase history table).
#
# If this is ever re-enabled, revisit schema constraints (tenant_id/customer_id
# were unique) and make webhook handling idempotent to avoid transaction
# rollbacks on retries.
#
# class UsageBasedService(CommonService):
#     model = UsageBased
#
#     @classmethod
#     def save(cls, **kwargs):
#         if "id" not in kwargs:
#             kwargs["id"] = get_uuid()
#         obj = cls.model(**kwargs).save(force_insert=True)
#         return obj
#
#     @classmethod
#     @DB.connection_context()
#     def get_by_tenant_id(cls, tenant_id: str, start_time=None) -> dict:
#         fields = [
#             cls.model.tenant_id,
#             cls.model.customer_id,
#             cls.model.payment_id,
#             cls.model.payment_status,
#             cls.model.product_name,
#         ]
#
#         query = cls.model.select(*fields).where(cls.model.tenant_id == tenant_id)
#
#         if start_time:
#             query = query.where(cls.model.create_time >= start_time)
#
#         purchased_usage_based = query.order_by(cls.model.create_time.desc()).dicts().first()
#         return purchased_usage_based
#
#     @classmethod
#     @DB.connection_context()
#     def set_customer_id(cls, tenant_id: str, customer_id: str):
#         _updated = cls.model.update(customer_id=customer_id).where(cls.model.tenant_id == tenant_id).execute()
#
#     @classmethod
#     @DB.connection_context()
#     def get_tenant_id_by_customer_id(cls, customer_id: str) -> str:
#         tenant = cls.model.select(cls.model.tenant_id).where(cls.model.customer_id == customer_id).first()
#         if tenant:
#             return tenant.tenant_id
#         return None
#
#     @classmethod
#     @DB.connection_context()
#     def get_by_payment_id(cls, payment_id: str) -> dict | None:
#         if not payment_id:
#             return None
#         return cls.model.select().where(cls.model.payment_id == payment_id).dicts().first()
#
#     @classmethod
#     @DB.connection_context()
#     def get_by_order_id(cls, order_id: str) -> dict | None:
#         if not order_id:
#             return None
#         return cls.model.select().where(cls.model.order_id == order_id).dicts().first()


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

    @classmethod
    @DB.connection_context()
    def update_by_order_id(cls, order_id: str, update_dict: dict) -> int:
        if not order_id:
            return 0
        update_dict["update_time"] = current_timestamp()
        update_dict["update_date"] = to_utc_datetime(datetime.now(timezone.utc))
        return cls.model.update(update_dict).where(cls.model.order_id == order_id).execute()


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
    VERSION_CHECK_FIELDS = [
        "price_type",
        "billing_frequency",
        "included_free_amount",
        "unit",
        "unit_quantity",
        "consuming_point_amount",
    ]

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
            cls.model.consuming_point_amount,
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
    VERSION_CHECK_FIELDS = ["amount_cents"]

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
            cls.model.amount_cents,
            cls.model.currency,
            cls.model.point_value_int,
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
                # Convert amount (USD float) to cents and point_value to int
                raw_amount = local_price.pop("amount", None)
                raw_point_value = local_price.pop("point_value", None)
                if raw_amount is not None and "amount_cents" not in local_price:
                    local_price["amount_cents"] = int(float(raw_amount) * 100)
                if raw_point_value is not None and "point_value_int" not in local_price:
                    local_price["point_value_int"] = int(float(raw_point_value) * 100)
                if "point_value_int" not in local_price:
                    local_price["point_value_int"] = local_price.get("amount_cents", 0)

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


# ---------------------------------------------------------------------------
# Point system
# ---------------------------------------------------------------------------


class InsufficientPointsError(Exception):
    """Raised when a tenant does not have enough available points."""


class PointAccountService:
    """Manages per-tenant point balances with atomic transactions."""

    @classmethod
    def get_or_create(cls, tenant_id: str) -> dict:
        """Must be called within an existing DB connection context or atomic block."""
        account = PointAccount.get_or_none(PointAccount.tenant_id == tenant_id)
        if account:
            return {
                "id": account.id,
                "tenant_id": account.tenant_id,
                "available_points": account.available_points,
                "held_points": account.held_points,
            }
        account_id = get_uuid()
        try:
            PointAccount.insert(
                id=account_id,
                tenant_id=tenant_id,
                available_points=0,
                held_points=0,
                create_time=current_timestamp(),
                update_time=current_timestamp(),
            ).execute()
        except IntegrityError:
            pass
        account = PointAccount.get(PointAccount.tenant_id == tenant_id)
        return {
            "id": account.id,
            "tenant_id": account.tenant_id,
            "available_points": account.available_points,
            "held_points": account.held_points,
        }

    @classmethod
    def recharge(cls, tenant_id: str, points: int, idempotency_key: str, description: str = "", metadata: dict | None = None) -> dict:
        with DB.atomic():
            cls.get_or_create(tenant_id)
            account = PointAccount.select().where(PointAccount.tenant_id == tenant_id).for_update().get()
            try:
                ledger = PointLedger.create(
                    id=get_uuid(),
                    tenant_id=tenant_id,
                    event_type="recharge",
                    points=points,
                    idempotency_key=idempotency_key,
                    description=description,
                    metadata=metadata or {},
                    create_time=current_timestamp(),
                    update_time=current_timestamp(),
                )
            except IntegrityError:
                # Idempotent: already recharged
                return PointLedger.get(PointLedger.idempotency_key == idempotency_key).__data__
            account.available_points += points
            account.update_time = current_timestamp()
            account.save()
            return ledger.__data__

    @classmethod
    def hold(cls, tenant_id: str, doc_id: str, points: int, idempotency_key: str) -> dict:
        with DB.atomic():
            cls.get_or_create(tenant_id)
            account = PointAccount.select().where(PointAccount.tenant_id == tenant_id).for_update().get()

            # Idempotency check
            existing_hold = PointHold.get_or_none(PointHold.idempotency_key == idempotency_key)
            if existing_hold:
                return existing_hold.__data__

            if account.available_points < points:
                raise InsufficientPointsError(
                    f"Tenant {tenant_id} has {account.available_points} points, need {points}"
                )

            hold = PointHold.create(
                id=get_uuid(),
                tenant_id=tenant_id,
                doc_id=doc_id,
                points=points,
                status="held",
                idempotency_key=idempotency_key,
                create_time=current_timestamp(),
                update_time=current_timestamp(),
            )
            PointLedger.create(
                id=get_uuid(),
                tenant_id=tenant_id,
                event_type="hold_created",
                points=-points,
                idempotency_key=f"hold:{hold.id}",
                related_hold_id=hold.id,
                create_time=current_timestamp(),
                update_time=current_timestamp(),
            )
            account.available_points -= points
            account.held_points += points
            account.update_time = current_timestamp()
            account.save()
            return hold.__data__

    @classmethod
    def commit_hold(cls, hold_id: str) -> bool:
        with DB.atomic():
            hold = PointHold.get_or_none(PointHold.id == hold_id)
            if not hold:
                return False
            if hold.status in ("committed", "released"):
                return True
            if hold.status != "held":
                return False
            account = PointAccount.select().where(PointAccount.tenant_id == hold.tenant_id).for_update().get()
            account.held_points -= hold.points
            account.update_time = current_timestamp()
            account.save()
            PointLedger.create(
                id=get_uuid(),
                tenant_id=hold.tenant_id,
                event_type="consume",
                points=-hold.points,
                idempotency_key=f"commit:{hold_id}",
                related_hold_id=hold_id,
                create_time=current_timestamp(),
                update_time=current_timestamp(),
            )
            hold.status = "committed"
            hold.update_time = current_timestamp()
            hold.save()
            return True

    @classmethod
    def release_hold(cls, hold_id: str) -> bool:
        with DB.atomic():
            hold = PointHold.get_or_none(PointHold.id == hold_id)
            if not hold:
                return False
            if hold.status == "released":
                return True
            if hold.status != "held":
                return False
            account = PointAccount.select().where(PointAccount.tenant_id == hold.tenant_id).for_update().get()
            account.available_points += hold.points
            account.held_points -= hold.points
            account.update_time = current_timestamp()
            account.save()
            PointLedger.create(
                id=get_uuid(),
                tenant_id=hold.tenant_id,
                event_type="release",
                points=hold.points,
                idempotency_key=f"release:{hold_id}",
                related_hold_id=hold_id,
                create_time=current_timestamp(),
                update_time=current_timestamp(),
            )
            hold.status = "released"
            hold.update_time = current_timestamp()
            hold.save()
            return True

    @classmethod
    @DB.connection_context()
    def get_balance(cls, tenant_id: str) -> dict:
        account = PointAccount.get_or_none(PointAccount.tenant_id == tenant_id)
        if not account:
            return {"available_points": 0, "held_points": 0, "total_points": 0}
        return {
            "available_points": account.available_points,
            "held_points": account.held_points,
            "total_points": account.available_points + account.held_points,
        }


@dataclass(frozen=True)
class ParseBillingQuote:
    product_name: str
    units: int
    points: int


class BaseParseBillingEstimator:
    product_name = ""

    def quote(self, filename: str, blob: bytes) -> ParseBillingQuote | None:
        raise NotImplementedError


class PdfPageParseBillingEstimator(BaseParseBillingEstimator):
    product_name = "deepdoc"

    def quote(self, filename: str, blob: bytes) -> ParseBillingQuote | None:
        if filename_type(filename) != FileType.PDF.value:
            return None

        pages = PdfParser.total_page_number(filename, blob)
        pages = max(int(pages or 0), 0)
        if pages <= 0:
            return None

        price_point = PricePointService.get_by_name(self.product_name) or {}
        consuming_point_amount = int(price_point.get("consuming_point_amount") or 1)
        return ParseBillingQuote(
            product_name=self.product_name,
            units=pages,
            points=pages * consuming_point_amount,
        )


class ParseBillingService:
    ESTIMATORS = [PdfPageParseBillingEstimator()]

    @classmethod
    def build_hold_key(cls, doc_id: str) -> str:
        return f"parse:{doc_id}:{get_uuid()}"

    @classmethod
    def register_estimator(cls, estimator: BaseParseBillingEstimator) -> None:
        cls.ESTIMATORS.append(estimator)

    @classmethod
    def quote_parse(cls, filename: str, blob: bytes) -> list[ParseBillingQuote]:
        quotes = []
        for estimator in cls.ESTIMATORS:
            quote = estimator.quote(filename, blob)
            if quote:
                quotes.append(quote)
        return quotes

    @classmethod
    def resolve_file_ref(cls, file_ref: dict | None) -> tuple[str, bytes] | None:
        if not file_ref:
            return None

        filename = file_ref.get("name") or file_ref.get("filename")
        blob = file_ref.get("blob")
        if filename and isinstance(blob, (bytes, bytearray)):
            return filename, bytes(blob)

        file_id = file_ref.get("id")
        created_by = file_ref.get("created_by")
        if filename and file_id and created_by:
            return filename, FileService.get_blob(created_by, file_id)

        return None

    @classmethod
    def hold_for_parse(
        cls,
        tenant_id: str,
        doc_id: str,
        filename: str,
        blob: bytes,
        idempotency_key: str | None = None,
    ) -> dict | None:
        quotes = cls.quote_parse(filename, blob)
        points = sum(quote.points for quote in quotes)
        if points <= 0:
            return None

        return PointAccountService.hold(
            tenant_id=tenant_id,
            doc_id=doc_id,
            points=points,
            idempotency_key=idempotency_key or cls.build_hold_key(doc_id),
        )

    @classmethod
    def hold_for_file_ref(cls, tenant_id: str, doc_id: str, file_ref: dict | None) -> dict | None:
        resolved = cls.resolve_file_ref(file_ref)
        if not resolved:
            return None
        filename, blob = resolved
        return cls.hold_for_parse(
            tenant_id=tenant_id,
            doc_id=doc_id,
            filename=filename,
            blob=blob,
        )

    @classmethod
    def finalize_hold(cls, hold_id: str, success: bool) -> bool:
        if success:
            return PointAccountService.commit_hold(hold_id)
        return PointAccountService.release_hold(hold_id)


class PointHoldService:
    """Queries for PointHold records."""

    @classmethod
    @DB.connection_context()
    def get_by_doc_id(cls, doc_id: str) -> dict | None:
        hold = (
            PointHold.select()
            .where(PointHold.doc_id == doc_id, PointHold.status == "held")
            .order_by(PointHold.create_time.desc())
            .first()
        )
        return hold.__data__ if hold else None

    @classmethod
    @DB.connection_context()
    def get_by_id(cls, hold_id: str) -> dict | None:
        hold = PointHold.get_or_none(PointHold.id == hold_id)
        return hold.__data__ if hold else None
