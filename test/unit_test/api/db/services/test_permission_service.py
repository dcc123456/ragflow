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

from api.db import PermissionValue, ResourceType
from api.db.services.permission_service import PermissionService


@pytest.fixture
def permission_service_env(monkeypatch):
    monkeypatch.setattr("api.db.services.permission_service.DB.connect", lambda *args, **kwargs: None)
    monkeypatch.setattr("api.db.services.permission_service.DB.close", lambda *args, **kwargs: None)


@pytest.mark.p2
def test_build_user_resource_permission_subquery_groups_by_resource(permission_service_env, monkeypatch):
    monkeypatch.setattr(
        "api.db.services.user_service.UserTenantService.query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-2")],
    )
    monkeypatch.setattr(
        "api.db.services.team_service.GroupMemberService.get_groups_by_member_id",
        lambda _member_id: [],
    )
    monkeypatch.setattr(
        "api.db.services.team_service.DepartmentMemberService.get_all_departments_by_member_id",
        lambda _member_id: [],
    )

    query = PermissionService.build_user_resource_permission_subquery(
        "user-1",
        ["tenant-2"],
        ResourceType.DIALOG,
        PermissionValue.PERMISSION_READ,
    )

    sql, params = query.sql()

    assert "GROUP BY" in sql
    assert "MAX" in sql
    assert "tenant_id" in sql
    assert "resource_id" in sql
    assert "operator_permission" in sql
    assert "tenant-2" in params
