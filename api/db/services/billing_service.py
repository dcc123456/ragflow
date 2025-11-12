#
#  Copyright 2024 The InfiniFlow Authors. All Rights Reserved.
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
from api.db.db_models import BillingPlan, TenantPlan
from api.db.services.common_service import CommonService
from api.db.services.user_service import UserTenantService
from api.db.services.knowledgebase_service import KnowledgebaseService
from common.misc_utils import get_uuid
import logging
import copy

class BillingPlanService(CommonService):
    model = BillingPlan

    @classmethod
    @DB.connection_context()
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_name(cls, plan_name: str) -> dict:
        fields = [
            cls.model.name,
            cls.model.quota_members,
            cls.model.quota_docs,
            cls.model.quota_chunks,
            cls.model.task_priority,
        ]
        plan = cls.model.select(*fields).where(cls.model.name == plan_name).dicts().first()
        return plan

    @classmethod
    @DB.connection_context()
    def init_data(cls, billing_plans: list[dict]):
        cls.model.delete().execute()
        for plan in billing_plans:
            cls.save(**plan)


class TenantPlanService(CommonService):
    model = TenantPlan
    default_plan = None

    @classmethod
    @DB.connection_context()
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_tenant_id(cls, tenant_id: str) -> dict:
        fields = [
            cls.model.tenant_id,
            cls.model.customer_id,
            cls.model.subscription_id,
            cls.model.subscription_status,
            cls.model.plan_name,
        ]
        tenant_plan = (
            cls.model.select(*fields)
            .where(cls.model.tenant_id == tenant_id)
            .order_by(cls.model.getter_by("create_time").desc())
            .dicts()
            .first()
        )
        if not tenant_plan:
            logging.warning(f"Tenant {tenant_id} plan not found, use trial plan")
            tenant_plan = {
                "tenant_id": tenant_id,
                "customer_id": "",
                "subscription_id": "",
                "subscription_status": "",
                "plan_name": "trial",
            }
        subscription_status = tenant_plan["subscription_status"]
        if subscription_status != "active":
            logging.warning(f"Tenant {tenant_id} subscription_status {subscription_status}, use trial plan")
            tenant_plan["plan_name"] = "trial"
        billing_plan = BillingPlanService.get_by_name(tenant_plan["plan_name"])
        assert billing_plan is not None
        plan_name = billing_plan.pop("name")
        billing_plan["plan_name"] = plan_name
        tenant_plan.update(billing_plan)

        num_members = UserTenantService.get_num_members(tenant_id)
        num_docs = 0
        kb_ids = KnowledgebaseService.get_kb_ids(tenant_id)
        if kb_ids:
            doc_ids = KnowledgebaseService.list_documents_by_ids(kb_ids)
            if doc_ids:
                num_docs = len(doc_ids)
        tenant_plan["num_members"] = num_members
        tenant_plan["num_docs"] = num_docs
        # Fill later with DocStoreConnection.count_chunks()
        tenant_plan["num_chunks"] = 0
        return tenant_plan

    @classmethod
    @DB.connection_context()
    def set_customer_id(cls, tenant_id: str, customer_id: str):
        updated = (
            cls.model.update(customer_id=customer_id, plan_name="trial")
            .where(cls.model.tenant_id == tenant_id)
            .execute()
        )
        if not updated:
            tenant_plan = {
                "id":get_uuid(),
                "tenant_id": tenant_id,
                "customer_id": customer_id,
                "plan_name": "trial",
            }
            cls.model.insert(**tenant_plan).execute()

    @classmethod
    @DB.connection_context()
    def get_tenant_id_by_customer_id(cls, customer_id: str) -> str:
        tenant = (
            cls.model.select(cls.model.tenant_id)
            .where(cls.model.customer_id == customer_id)
            .first()
        )
        if tenant:
            return tenant.tenant_id
        return None

    @classmethod
    @DB.connection_context()
    def update_subscription(
        cls,
        customer_id: str,
        subscription_id: str,
        subscription_status: str,
        plan_name: str,
    ) -> int:
        return (
            cls.model.update(
                subscription_id=subscription_id,
                subscription_status=subscription_status,
                plan_name=plan_name,
            )
            .where(cls.model.customer_id == customer_id)
            .execute()
        )

    @classmethod
    @DB.connection_context()
    def check_by_tenant_id(
        cls,
        tenant_id: str,
        delta_members: int = 0,
        delta_docs: int = 0,
        delta_chunks: int = 0,
    ):
        fields = [
            cls.model.tenant_id,
            BillingPlan.name,
            BillingPlan.quota_members,
            BillingPlan.quota_docs,
            BillingPlan.quota_chunks,
            BillingPlan.task_priority,
        ]
        tenant_plan = (
            cls.model.select(*fields)
            .join(BillingPlan, on=(cls.model.plan_name == BillingPlan.name))
            .where(cls.model.tenant_id == tenant_id)
            .order_by(cls.model.getter_by("create_time").desc())
            .dicts()
            .first()
        )
        if not tenant_plan:
            logging.warning(f"Tenant {tenant_id} plan not found, use trial plan")
            if cls.default_plan is None:
                cls.default_plan = BillingPlanService.get_by_name("trial")
                assert cls.default_plan is not None
            tenant_plan = copy.deepcopy(cls.default_plan)
            tenant_plan["tenant_id"] = tenant_id
        if delta_members > 0:
            num_members = UserTenantService.get_num_members(tenant_id)
            if num_members + delta_members > tenant_plan["quota_members"]:
                raise Exception(
                    f"Tenant {tenant_id} plan {tenant_plan['name']} quota exceeded. Max members: {tenant_plan['quota_members']}, current members: {num_members}, delta members: {delta_members}"
                )
        if delta_docs > 0:
            num_docs = 0
            kb_ids = KnowledgebaseService.get_kb_ids(tenant_id)
            if kb_ids:
                doc_ids = KnowledgebaseService.list_documents_by_ids(kb_ids)
                if doc_ids:
                    num_docs = len(doc_ids)
            if num_docs + delta_docs > tenant_plan["quota_docs"]:
                raise Exception(
                    f"Tenant {tenant_id} plan {tenant_plan['name']} quota exceeded. Max docs: {tenant_plan['quota_docs']}, current docs: {num_docs}, delta docs: {delta_docs}"
                )
        if delta_chunks > 0:
            if delta_chunks > tenant_plan["quota_chunks"]:
                raise Exception(
                    f"Tenant {tenant_id} plan {tenant_plan['name']} quota exceeded. Max chunks: {tenant_plan['quota_chunks']}, current chunks: {delta_chunks}"
                )
        return None

    @classmethod
    @DB.connection_context()
    def get_priority(cls, tenant_id: str) -> int:
        fields = [
            cls.model.tenant_id,
            cls.model.subscription_status,
            cls.model.plan_name,
        ]
        tenant_plan = (
            cls.model.select(*fields)
            .where(cls.model.tenant_id == tenant_id)
            .order_by(cls.model.getter_by("create_time").desc())
            .dicts()
            .first()
        )
        if not tenant_plan:
            return 0
        subscription_status = tenant_plan["subscription_status"]
        if subscription_status != "active":
            return 0
        return 1
