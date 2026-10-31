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


def get_tenant_priority(tenant_id: str) -> int:
    """Return the task queue priority for a tenant based on their billing plan.

    Returns 0 when billing is disabled or No active subscription is found,
    otherwise maps task_priority from the tenant's Product record:
    "high" -> 1, "low" -> 0.
    """
    from common.settings import BILLING_ENABLED

    if not BILLING_ENABLED:
        return 0
    from api.db.db_models import Product, Subscription

    subscription = Subscription.select(Subscription.plan_name).where(Subscription.tenant_id == tenant_id).order_by(Subscription.create_time.desc()).first()
    if not subscription:
        return 0
    product = Product.select(Product.task_priority).where(Product.name == subscription.plan_name).order_by(Product.version.desc()).first()
    return 1 if product and product.task_priority == "high" else 0
