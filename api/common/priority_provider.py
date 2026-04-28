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

    Returns 0 when billing is disabled or no active subscription is found,
    otherwise maps task_priority from the tenant's Product record:
    "high" -> 1, "low" -> 0.
    """
    from common.settings import BILLING_ENABLED
    if not BILLING_ENABLED:
        return 0
    from api.db.services.billing_service import SubscriptionService
    return SubscriptionService.get_priority(tenant_id)
