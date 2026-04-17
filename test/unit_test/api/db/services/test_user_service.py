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
from types import SimpleNamespace

import pytest

from api.db.services.user_service import DB
from api.db.services.user_service import UserTenantService


@pytest.fixture(autouse=True)
def user_service_env(monkeypatch):
    monkeypatch.setattr(DB, "connect", lambda *args, **kwargs: None)
    monkeypatch.setattr(DB, "close", lambda *args, **kwargs: None)


def test_get_user_tenants_with_owner_appends_missing_owner(monkeypatch):
    monkeypatch.setattr(
        UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        UserTenantService,
        "filter_by_tenant_and_user_id",
        lambda tenant_id, user_id: SimpleNamespace(id="owner-member", tenant_id=tenant_id, user_id=user_id),
    )

    user_tenants = UserTenantService.get_user_tenants_with_owner("tenant-owner")

    assert [(tenant.id, tenant.tenant_id) for tenant in user_tenants] == [
        ("member-1", "tenant-1"),
        ("owner-member", "tenant-owner"),
    ]


def test_get_user_tenants_with_owner_keeps_existing_owner(monkeypatch):
    query_calls = []

    monkeypatch.setattr(
        UserTenantService,
        "query",
        lambda **kwargs: query_calls.append(kwargs) or [SimpleNamespace(id="owner-member", tenant_id="tenant-owner")],
    )
    monkeypatch.setattr(
        UserTenantService,
        "filter_by_tenant_and_user_id",
        lambda *_args, **_kwargs: (_ for _ in ()).throw(AssertionError("owner lookup should not run when owner tenant already exists")),
    )

    user_tenants = UserTenantService.get_user_tenants_with_owner("tenant-owner")

    assert query_calls == [{"user_id": "tenant-owner"}]
    assert [(tenant.id, tenant.tenant_id) for tenant in user_tenants] == [
        ("owner-member", "tenant-owner"),
    ]
