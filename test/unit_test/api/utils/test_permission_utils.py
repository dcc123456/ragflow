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

from api.utils.permission_utils import _filter_accessible_document_ids
from common.constants import StatusEnum


class _FakeExpr:
    def __init__(self, op, *parts):
        self.op = op
        self.parts = parts

    def __and__(self, other):
        return _FakeExpr("and", self, other)

    def contains(self, predicate):
        if predicate(self):
            return True
        for part in self.parts:
            if isinstance(part, _FakeExpr) and part.contains(predicate):
                return True
        return False

    def find_in_values(self, field_name):
        if self.op == "in" and len(self.parts) == 2 and self.parts[0] == field_name:
            return set(self.parts[1])
        for part in self.parts:
            if isinstance(part, _FakeExpr):
                values = part.find_in_values(field_name)
                if values is not None:
                    return values
        return None


class _FakeField:
    def __init__(self, name):
        self.name = name

    def __eq__(self, other):
        return _FakeExpr("eq", self.name, other)

    def __ge__(self, other):
        return _FakeExpr("ge", self.name, other)

    def in_(self, values):
        return _FakeExpr("in", self.name, tuple(values))


class _FakeQuery:
    def __init__(self, rows, reject_status_filter=False):
        self.rows = rows
        self.reject_status_filter = reject_status_filter

    def where(self, expr):
        if self.reject_status_filter and expr.contains(
            lambda node: node.op == "eq"
            and len(node.parts) == 2
            and node.parts[0] == "status"
            and node.parts[1] == StatusEnum.VALID.value
        ):
            raise AssertionError("Document status filter should not be applied when resolving visible documents.")
        allowed_ids = expr.find_in_values("id")
        if allowed_ids is not None:
            self.rows = [row for row in self.rows if row.get("id") in allowed_ids or row.get("resource_id") in allowed_ids]
        return self

    def dicts(self):
        return self.rows


class _FakeDocument:
    id = _FakeField("id")
    kb_id = _FakeField("kb_id")
    status = _FakeField("status")

    @staticmethod
    def select(*_args, **_kwargs):
        return _FakeQuery(
            [
                {"id": "enabled-doc"},
                {"id": "disabled-doc"},
            ],
            reject_status_filter=True,
        )


class _FakePermission:
    tenant_id = _FakeField("tenant_id")
    resource_type = _FakeField("resource_type")
    permission = _FakeField("permission")
    status = _FakeField("status")
    resource_id = _FakeField("resource_id")
    member_id = _FakeField("member_id")
    group_id = _FakeField("group_id")
    department_id = _FakeField("department_id")

    @staticmethod
    def select(*_args, **_kwargs):
        return _FakeQuery([{"resource_id": "disabled-doc"}])


class _FakeUserTenant:
    id = _FakeField("id")
    status = _FakeField("status")
    current_user_id = "tenant-owner"

    @classmethod
    def get_or_none(cls, _expr):
        return SimpleNamespace(user_id=cls.current_user_id)


class TestFilterAccessibleDocumentIds:
    def test_owner_keeps_disabled_documents_visible(self, monkeypatch):
        import api.db.db_models as db_models

        _FakeUserTenant.current_user_id = "tenant-owner"
        monkeypatch.setattr(db_models, "Document", _FakeDocument)
        monkeypatch.setattr(db_models, "UserTenant", _FakeUserTenant)

        result = _filter_accessible_document_ids("tenant-owner", "member-1", ["kb-1"])

        assert set(result) == {"enabled-doc", "disabled-doc"}

    def test_permissioned_member_keeps_disabled_documents_visible(self, monkeypatch):
        import api.db.db_models as db_models
        import api.db.services.team_service as team_service

        _FakeUserTenant.current_user_id = "another-user"
        monkeypatch.setattr(db_models, "Document", _FakeDocument)
        monkeypatch.setattr(db_models, "Permission", _FakePermission)
        monkeypatch.setattr(db_models, "UserTenant", _FakeUserTenant)
        monkeypatch.setattr(team_service.GroupMemberService, "get_groups_by_member_id", lambda *_args, **_kwargs: [])
        monkeypatch.setattr(team_service.DepartmentMemberService, "get_all_departments_by_member_id", lambda *_args, **_kwargs: [])
        monkeypatch.setattr(team_service.DepartmentService, "get_department_hierarchy", lambda *_args, **_kwargs: [])

        result = _filter_accessible_document_ids("tenant-owner", "member-1", ["kb-1"])

        assert result == ["disabled-doc"]
