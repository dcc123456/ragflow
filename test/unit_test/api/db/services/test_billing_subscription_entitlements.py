#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
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
import sys
import warnings

import pytest

warnings.filterwarnings(
    "ignore",
    message="pkg_resources is deprecated as an API.*",
    category=UserWarning,
)
warnings.filterwarnings(
    "ignore",
    message="\\[Errno 13\\] Permission denied\\.  joblib will operate in serial mode",
    category=UserWarning,
)

from api.db.services import billing_service  # noqa: E402
from api.utils import billing as billing_utils  # noqa: E402


@pytest.mark.p2
def test_entitled_main_subscription_statuses_are_allowlist():
    assert billing_service.ENTITLED_MAIN_SUBSCRIPTION_STATUSES == {"active", "trialing"}


@pytest.mark.p2
@pytest.mark.parametrize(
    "status",
    [
        "incomplete",
        "incomplete_expired",
        "past_due",
        "unpaid",
        "canceled",
        "paused",
        "unknown",
        "",
    ],
)
def test_non_entitled_statuses_are_not_in_allowlist(status):
    assert status not in billing_service.ENTITLED_MAIN_SUBSCRIPTION_STATUSES


@pytest.mark.p2
def test_check_dynamic_resources_normalizes_subscription_invalid(monkeypatch):
    monkeypatch.setattr(billing_utils.settings, "BILLING_ENABLED", True, raising=False)
    monkeypatch.setattr(
        billing_service.SubscriptionService,
        "check_by_tenant_id",
        lambda *_args, **_kwargs: (False, {"error": "No active subscription found for tenant tenant-1", "tenant_id": "tenant-1"}),
    )

    from api.db.services.user_service import UserTenantService

    monkeypatch.setattr(UserTenantService, "get_owner_email", lambda _tenant_id: "an.liu@ragflow.io")

    check_ok, check_info = billing_utils.check_dynamic_resources(tenant_id="tenant-1", apps=1)

    assert check_ok is False
    assert check_info["resource"] == "subscription"
    assert check_info["code"] == billing_utils.RetCode.BILLING_SUBSCRIPTION_INVALID
    assert check_info["message"] == "Tenant an.liu@ragflow.io subscription is invalid"
    assert check_info["detail"] == {}


@pytest.mark.p2
def test_check_dynamic_resources_normalizes_app_quota(monkeypatch):
    monkeypatch.setattr(billing_utils.settings, "BILLING_ENABLED", True, raising=False)
    monkeypatch.setattr(
        billing_service.SubscriptionService,
        "check_by_tenant_id",
        lambda *_args, **_kwargs: (
            False,
            {
                "error": "App quota exceeded\n",
                "details": {"quota_apps": {"current": 5, "limit": 5}},
                "tenant_id": "tenant-1",
            },
        ),
    )

    from api.db.services.user_service import UserTenantService

    monkeypatch.setattr(UserTenantService, "get_owner_email", lambda _tenant_id: "an.liu@ragflow.io")

    check_ok, check_info = billing_utils.check_dynamic_resources(tenant_id="tenant-1", apps=1)

    assert check_ok is False
    assert check_info["resource"] == "apps"
    assert check_info["code"] == billing_utils.RetCode.BILLING_APPS_INSUFFICIENT
    assert check_info["message"] == "Insufficient app quota of tenant an.liu@ragflow.io. Current: 5, Limit: 5"
    assert check_info["detail"] == {"current": 5, "limit": 5}


@pytest.mark.p2
def test_get_dynamic_resource_error_result_uses_normalized_payload(monkeypatch):
    monkeypatch.setattr(billing_utils.settings, "BILLING_ENABLED", True, raising=False)

    from types import ModuleType

    from api.db.services.user_service import UserTenantService

    monkeypatch.setattr(UserTenantService, "get_owner_email", lambda _tenant_id: "an.liu@ragflow.io")

    fake_api_utils = ModuleType("api.utils.api_utils")
    fake_api_utils.get_resource_insufficient_result = lambda code=0, message="", detail=None, **_kwargs: {
        "code": code,
        "message": message,
        "detail": detail,
    }
    monkeypatch.setitem(sys.modules, "api.utils.api_utils", fake_api_utils)

    result = billing_utils.get_dynamic_resource_error_result(
        {
            "error": "App quota exceeded\n",
            "details": {"quota_apps": {"current": 5, "limit": 5}},
            "tenant_id": "tenant-1",
        },
        "tenant-1",
    )

    assert result == {
        "code": billing_utils.RetCode.BILLING_APPS_INSUFFICIENT,
        "message": "Insufficient app quota of tenant an.liu@ragflow.io. Current: 5, Limit: 5",
        "detail": {"current": 5, "limit": 5},
    }
