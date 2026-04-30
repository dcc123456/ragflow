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
import importlib.util
import sys
import types
import warnings

import pytest

warnings.filterwarnings(
    "ignore",
    message="pkg_resources is deprecated as an API.*",
    category=UserWarning,
)


def _install_cv2_stub_if_unavailable():
    try:
        importlib.import_module("cv2")
        return
    except Exception:
        pass

    stub = types.ModuleType("cv2")
    stub.INTER_LINEAR = 1
    stub.INTER_CUBIC = 2
    stub.BORDER_CONSTANT = 0
    stub.BORDER_REPLICATE = 1
    stub.COLOR_BGR2RGB = 0
    stub.COLOR_BGR2GRAY = 1
    stub.COLOR_GRAY2BGR = 2
    stub.IMREAD_IGNORE_ORIENTATION = 128
    stub.IMREAD_COLOR = 1
    stub.RETR_LIST = 1
    stub.CHAIN_APPROX_SIMPLE = 2

    def _missing(*_args, **_kwargs):
        raise RuntimeError("cv2 runtime call is unavailable in this test environment")

    def _module_getattr(name):
        if name.isupper():
            return 0
        return _missing

    stub.__getattr__ = _module_getattr
    sys.modules["cv2"] = stub


def _install_xgboost_stub_if_unavailable():
    if "xgboost" in sys.modules:
        return
    if importlib.util.find_spec("xgboost") is not None:
        return
    sys.modules["xgboost"] = types.ModuleType("xgboost")


_install_cv2_stub_if_unavailable()
_install_xgboost_stub_if_unavailable()

from api.db import PermissionValue
from api.db.services import dialog_service
from common.constants import StatusEnum


class _FakeJoinType:
    LEFT_OUTER = "LEFT_OUTER"


class _Predicate:
    def __init__(self, fn, expr=""):
        self._fn = fn
        self.expr = expr

    def __call__(self, row):
        return self._fn(row)

    def __and__(self, other):
        return _Predicate(lambda row: self(row) and other(row), f"({self.expr} AND {other.expr})")

    def __or__(self, other):
        return _Predicate(lambda row: self(row) or other(row), f"({self.expr} OR {other.expr})")


class _FakeOrderField:
    def __init__(self, field_name, reverse=False):
        self.field_name = field_name
        self.reverse = reverse

    def desc(self):
        return _FakeOrderField(self.field_name, reverse=True)

    def asc(self):
        return _FakeOrderField(self.field_name, reverse=False)


class _FakeField:
    def __init__(self, field_name):
        self.field_name = field_name

    def __eq__(self, other):
        other_field_name = getattr(other, "field_name", other)
        if hasattr(other, "field_name"):
            return _Predicate(
                lambda row: row.get(self.field_name) == row.get(other.field_name),
                f"{self.field_name}=={other.field_name}",
            )
        return _Predicate(lambda row: row.get(self.field_name) == other, f"{self.field_name}=={other_field_name}")

    def in_(self, other):
        other_values = set(other)
        return _Predicate(lambda row: row.get(self.field_name) in other_values)

    def is_null(self, is_null=True):
        if is_null:
            return _Predicate(lambda row: row.get(self.field_name) is None)
        return _Predicate(lambda row: row.get(self.field_name) is not None)

    def alias(self, *_args, **_kwargs):
        return self


class _FakeLowerField:
    def __init__(self, field_name):
        self.field_name = field_name

    def contains(self, needle):
        lowered = needle.lower()
        return _Predicate(lambda row: lowered in str(row.get(self.field_name, "")).lower())


class _FakeFn:
    @staticmethod
    def LOWER(field):
        return _FakeLowerField(field.field_name)


class _FakeQuery:
    def __init__(self, rows):
        self._current = list(rows)
        self.join_calls = []

    def join(self, *args, **kwargs):
        on_expr = None
        if "on" in kwargs:
            on_expr = getattr(kwargs["on"], "expr", None)
        self.join_calls.append({"args": args, "kwargs": kwargs, "on_expr": on_expr})
        return self

    def switch(self, *_args, **_kwargs):
        return self

    def where(self, *predicates, **_kwargs):
        for predicate in predicates:
            self._current = [row for row in self._current if predicate(row)]
        return self

    def order_by(self, field):
        self._current = sorted(
            self._current,
            key=lambda row: row.get(field.field_name),
            reverse=field.reverse,
        )
        return self

    def count(self):
        return len(self._current)

    def paginate(self, page_number, items_per_page):
        if page_number and items_per_page:
            start = (page_number - 1) * items_per_page
            end = start + items_per_page
            self._current = self._current[start:end]
        return self

    def dicts(self):
        return [dict(row) for row in self._current]


class _FakeDialogModel:
    _field_names = [
        "id",
        "tenant_id",
        "name",
        "description",
        "language",
        "llm_id",
        "llm_setting",
        "prompt_type",
        "prompt_config",
        "similarity_threshold",
        "vector_similarity_weight",
        "top_n",
        "top_k",
        "do_refer",
        "rerank_id",
        "kb_ids",
        "icon",
        "status",
        "update_time",
        "create_time",
    ]

    def __init__(self, rows):
        self._rows = rows
        for field_name in self._field_names:
            setattr(self, field_name, _FakeField(field_name))

    def select(self, *_args, **_kwargs):
        self.last_query = _FakeQuery(self._rows)
        return self.last_query

    def getter_by(self, field_name):
        return _FakeOrderField(field_name)


class _FakeSubqueryColumns:
    tenant_id = _FakeField("shared_tenant_id")
    resource_id = _FakeField("shared_resource_id")
    operator_permission = _FakeField("shared_operator_permission")


class _FakeSubquery:
    c = _FakeSubqueryColumns()

    def alias(self, *_args, **_kwargs):
        return self


def _dialog_row(dialog_id, tenant_id, *, name=None, create_time=None):
    return {
        "id": dialog_id,
        "tenant_id": tenant_id,
        "name": name or dialog_id,
        "description": "",
        "language": "English",
        "llm_id": "glm-4",
        "llm_setting": {},
        "prompt_type": "simple",
        "prompt_config": {},
        "similarity_threshold": 0.2,
        "vector_similarity_weight": 0.3,
        "top_n": 6,
        "top_k": 1024,
        "do_refer": True,
        "rerank_id": "",
        "kb_ids": [],
        "icon": "",
        "status": StatusEnum.VALID.value,
        "nickname": tenant_id,
        "tenant_avatar": "",
        "update_time": create_time or 0,
        "create_time": create_time or 0,
    }


def _merge_shared_permissions(rows, shared_permission_rows):
    permission_by_resource = {
        row["resource_id"]: (
            row["operator_permission"],
            row.get("tenant_id"),
        )
        for row in (shared_permission_rows or [])
    }
    merged_rows = []
    for row in rows:
        row_copy = dict(row)
        shared_permission = permission_by_resource.get(row["id"])
        row_copy["shared_resource_id"] = row["id"] if shared_permission is not None else None
        row_copy["shared_tenant_id"] = shared_permission[1] if shared_permission is not None else None
        row_copy["shared_operator_permission"] = shared_permission[0] if shared_permission is not None else None
        merged_rows.append(row_copy)
    return merged_rows


@pytest.fixture
def dialog_service_env(monkeypatch):
    monkeypatch.setattr(dialog_service.DB, "connect", lambda *args, **kwargs: None)
    monkeypatch.setattr(dialog_service.DB, "close", lambda *args, **kwargs: None)
    monkeypatch.setattr(dialog_service, "fn", _FakeFn())
    monkeypatch.setattr(dialog_service, "JOIN", _FakeJoinType(), raising=False)

    def _run(rows, joined_tenant_ids, user_id="tenant-1", shared_permission_rows=None, **kwargs):
        permission_map_calls = []
        permission_subquery_calls = []
        merged_rows = _merge_shared_permissions(rows, shared_permission_rows)
        fake_model = _FakeDialogModel(merged_rows)
        monkeypatch.setattr(dialog_service.DialogService, "model", fake_model)

        def _legacy_permission_map(*_args, **_kwargs):
            raise AssertionError("legacy permission map API should not be used")

        def _build_user_resource_permission_subquery(
            user_id_arg,
            tenant_ids_arg,
            resource_type_arg,
            permission_arg,
        ):
            permission_subquery_calls.append(
                {
                    "user_id": user_id_arg,
                    "tenant_ids": list(tenant_ids_arg),
                    "resource_type": resource_type_arg,
                    "permission": permission_arg,
                }
            )
            return _FakeSubquery()

        monkeypatch.setattr(
            dialog_service.PermissionService,
            "get_user_resource_permission_map",
            _legacy_permission_map,
        )
        monkeypatch.setattr(
            dialog_service.PermissionService,
            "build_user_resource_permission_subquery",
            _build_user_resource_permission_subquery,
        )
        dialogs, total = dialog_service.DialogService.get_by_tenant_ids(
            joined_tenant_ids,
            user_id,
            kwargs.get("page_number", 0),
            kwargs.get("items_per_page", 0),
            kwargs.get("orderby", "create_time"),
            kwargs.get("desc", True),
            kwargs.get("keywords", ""),
            kwargs.get("id"),
            kwargs.get("name"),
        )
        return dialogs, total, permission_map_calls, permission_subquery_calls, fake_model.last_query.join_calls

    return _run


@pytest.mark.p2
def test_get_by_tenant_ids_returns_owned_dialogs_with_owner_permission(dialog_service_env):
    dialogs, total, permission_map_calls, permission_subquery_calls, _join_calls = dialog_service_env(
        rows=[
            _dialog_row("chat-owned", "tenant-1", create_time=3),
            _dialog_row("chat-other", "tenant-2", create_time=2),
        ],
        joined_tenant_ids=["tenant-1"],
    )

    assert total == 1
    assert [dialog["id"] for dialog in dialogs] == ["chat-owned"]
    assert dialogs[0]["operator_permission"] == PermissionValue.PERMISSION_OWNER.value
    assert permission_map_calls == []
    assert permission_subquery_calls == []


@pytest.mark.p2
def test_get_by_tenant_ids_reads_shared_visibility_without_materializing_permission_map(dialog_service_env):
    dialogs, total, permission_map_calls, permission_subquery_calls, join_calls = dialog_service_env(
        rows=[
            _dialog_row("chat-owned", "tenant-1", create_time=4),
            _dialog_row("chat-shared", "tenant-2", create_time=3),
            _dialog_row("chat-unshared", "tenant-2", create_time=2),
        ],
        shared_permission_rows=[
            {
                "resource_id": "chat-shared",
                "tenant_id": "tenant-2",
                "operator_permission": PermissionValue.PERMISSION_READ.value,
            }
        ],
        joined_tenant_ids=["tenant-1", "tenant-2"],
    )

    assert total == 2
    assert [dialog["id"] for dialog in dialogs] == ["chat-owned", "chat-shared"]
    assert dialogs[0]["operator_permission"] == PermissionValue.PERMISSION_OWNER.value
    assert dialogs[1]["operator_permission"] == PermissionValue.PERMISSION_READ.value
    assert permission_map_calls == []
    assert permission_subquery_calls == [
        {
            "user_id": "tenant-1",
            "tenant_ids": ["tenant-2"],
            "resource_type": dialog_service.ResourceType.DIALOG,
            "permission": PermissionValue.PERMISSION_READ,
        }
    ]
    assert "shared_tenant_id==tenant_id" in join_calls[-1]["on_expr"]
    assert "shared_resource_id==id" in join_calls[-1]["on_expr"]
    assert "shared_operator_permission" not in dialogs[0]
    assert "shared_operator_permission" not in dialogs[1]


@pytest.mark.p2
def test_get_by_tenant_ids_excludes_unshared_cross_tenant_dialogs(dialog_service_env):
    dialogs, total, permission_map_calls, permission_subquery_calls, _join_calls = dialog_service_env(
        rows=[
            _dialog_row("chat-owned", "tenant-1", create_time=3),
            _dialog_row("chat-unshared", "tenant-2", create_time=2),
        ],
        joined_tenant_ids=["tenant-1", "tenant-2"],
    )

    assert total == 1
    assert [dialog["id"] for dialog in dialogs] == ["chat-owned"]
    assert permission_map_calls == []
    assert permission_subquery_calls == [
        {
            "user_id": "tenant-1",
            "tenant_ids": ["tenant-2"],
            "resource_type": dialog_service.ResourceType.DIALOG,
            "permission": PermissionValue.PERMISSION_READ,
        }
    ]


@pytest.mark.p2
def test_get_by_tenant_ids_counts_visible_dialogs_before_pagination(dialog_service_env):
    dialogs, total, permission_map_calls, permission_subquery_calls, _join_calls = dialog_service_env(
        rows=[
            _dialog_row("chat-owned", "tenant-1", create_time=5),
            _dialog_row("chat-shared-1", "tenant-2", create_time=4),
            _dialog_row("chat-shared-2", "tenant-2", create_time=3),
        ],
        shared_permission_rows=[
            {
                "resource_id": "chat-shared-1",
                "tenant_id": "tenant-2",
                "operator_permission": PermissionValue.PERMISSION_READ.value,
            },
            {
                "resource_id": "chat-shared-2",
                "tenant_id": "tenant-2",
                "operator_permission": PermissionValue.PERMISSION_READ.value,
            },
        ],
        joined_tenant_ids=["tenant-1", "tenant-2"],
        page_number=1,
        items_per_page=1,
    )

    assert total == 3
    assert [dialog["id"] for dialog in dialogs] == ["chat-owned"]
    assert permission_map_calls == []
    assert permission_subquery_calls == [
        {
            "user_id": "tenant-1",
            "tenant_ids": ["tenant-2"],
            "resource_type": dialog_service.ResourceType.DIALOG,
            "permission": PermissionValue.PERMISSION_READ,
        }
    ]


@pytest.mark.p2
def test_get_by_tenant_ids_excludes_self_tenant_from_permission_lookup(dialog_service_env):
    dialogs, total, permission_map_calls, permission_subquery_calls, _join_calls = dialog_service_env(
        rows=[
            _dialog_row("chat-owned", "tenant-1", create_time=3),
            _dialog_row("chat-shared", "tenant-2", create_time=2),
        ],
        shared_permission_rows=[
            {
                "resource_id": "chat-shared",
                "tenant_id": "tenant-2",
                "operator_permission": PermissionValue.PERMISSION_READ.value,
            }
        ],
        joined_tenant_ids=["tenant-1", "tenant-2"],
    )

    assert total == 2
    assert [dialog["id"] for dialog in dialogs] == ["chat-owned", "chat-shared"]
    assert permission_map_calls == []
    assert permission_subquery_calls == [
        {
            "user_id": "tenant-1",
            "tenant_ids": ["tenant-2"],
            "resource_type": dialog_service.ResourceType.DIALOG,
            "permission": PermissionValue.PERMISSION_READ,
        }
    ]


@pytest.mark.p2
def test_get_by_tenant_ids_skips_permission_lookup_for_self_only_scope(dialog_service_env):
    dialogs, total, permission_map_calls, permission_subquery_calls, _join_calls = dialog_service_env(
        rows=[
            _dialog_row("chat-owned-1", "tenant-1", create_time=3),
            _dialog_row("chat-owned-2", "tenant-1", create_time=2),
        ],
        joined_tenant_ids=["tenant-1"],
    )

    assert total == 2
    assert [dialog["id"] for dialog in dialogs] == ["chat-owned-1", "chat-owned-2"]
    assert permission_map_calls == []
    assert permission_subquery_calls == []


@pytest.mark.p2
def test_get_by_tenant_ids_honors_owner_scope_without_leaking_self_dialogs(dialog_service_env):
    dialogs, total, permission_map_calls, permission_subquery_calls, _join_calls = dialog_service_env(
        rows=[
            _dialog_row("chat-owned", "tenant-1", create_time=3),
            _dialog_row("chat-shared", "tenant-2", create_time=2),
        ],
        shared_permission_rows=[
            {
                "resource_id": "chat-shared",
                "tenant_id": "tenant-2",
                "operator_permission": PermissionValue.PERMISSION_READ.value,
            }
        ],
        joined_tenant_ids=["tenant-2"],
    )

    assert total == 1
    assert [dialog["id"] for dialog in dialogs] == ["chat-shared"]
    assert dialogs[0]["operator_permission"] == PermissionValue.PERMISSION_READ.value
    assert permission_map_calls == []
    assert permission_subquery_calls == [
        {
            "user_id": "tenant-1",
            "tenant_ids": ["tenant-2"],
            "resource_type": dialog_service.ResourceType.DIALOG,
            "permission": PermissionValue.PERMISSION_READ,
        }
    ]


@pytest.mark.p2
def test_get_by_tenant_ids_honors_id_filter_with_shared_subquery(dialog_service_env):
    dialogs, total, permission_map_calls, permission_subquery_calls, _join_calls = dialog_service_env(
        rows=[
            _dialog_row("chat-shared-1", "tenant-2", create_time=3),
            _dialog_row("chat-shared-2", "tenant-2", create_time=2),
        ],
        shared_permission_rows=[
            {
                "resource_id": "chat-shared-1",
                "tenant_id": "tenant-2",
                "operator_permission": PermissionValue.PERMISSION_READ.value,
            },
            {
                "resource_id": "chat-shared-2",
                "tenant_id": "tenant-2",
                "operator_permission": PermissionValue.PERMISSION_READ.value,
            },
        ],
        joined_tenant_ids=["tenant-2"],
        id="chat-shared-2",
    )

    assert total == 1
    assert [dialog["id"] for dialog in dialogs] == ["chat-shared-2"]
    assert permission_map_calls == []
    assert permission_subquery_calls == [
        {
            "user_id": "tenant-1",
            "tenant_ids": ["tenant-2"],
            "resource_type": dialog_service.ResourceType.DIALOG,
            "permission": PermissionValue.PERMISSION_READ,
        }
    ]
