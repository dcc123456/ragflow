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
"""Regression tests for agent session GET/DELETE (api/apps/restful_apis/agent_api.py)."""

import importlib.util
import sys
from pathlib import Path
from types import ModuleType, SimpleNamespace

import pytest


class _PassthroughManager:
    def route(self, *_args, **_kwargs):
        return lambda func: func


def _stub(monkeypatch, name, **attrs):
    mod = ModuleType(name)
    for key, value in attrs.items():
        setattr(mod, key, value)
    monkeypatch.setitem(sys.modules, name, mod)
    if "." in name:
        parent_name, _, child_name = name.rpartition(".")
        parent_mod = sys.modules.get(parent_name)
        if parent_mod is not None:
            monkeypatch.setattr(parent_mod, child_name, mod, raising=False)
    return mod


def _load_agent_api(monkeypatch, get_by_id_result, delete_calls=None):
    """Load api/apps/restful_apis/agent_api.py with the minimum stubs required."""
    delete_calls = delete_calls if delete_calls is not None else []

    def _delete_by_id(session_id):
        delete_calls.append(session_id)
        return True

    _stub(monkeypatch, "api.apps", QuartAuthUnauthorized=Exception, current_user=SimpleNamespace(id="tenant-1", is_superuser=True), login_required=lambda func: func)
    _stub(monkeypatch, "api.apps.services.canvas_replica_service", CanvasReplicaService=SimpleNamespace())
    quart_stub = ModuleType("quart")
    quart_stub.Response = SimpleNamespace
    quart_stub.g = SimpleNamespace(operator_permission=7)
    quart_stub.jsonify = lambda payload: payload
    quart_stub.request = SimpleNamespace(args={}, method="GET")
    quart_stub.make_response = lambda payload, *_args, **_kwargs: payload
    monkeypatch.setitem(sys.modules, "quart", quart_stub)

    _stub(
        monkeypatch,
        "api.db",
        CanvasCategory=SimpleNamespace(),
        PermissionActionType=SimpleNamespace(),
        PermissionTargetType=SimpleNamespace(),
        PermissionValue=SimpleNamespace(
            PERMISSION_READ=SimpleNamespace(value=1),
            PERMISSION_WRITE=SimpleNamespace(value=2),
            PERMISSION_MANAGE=SimpleNamespace(value=4),
            PERMISSION_OWNER=SimpleNamespace(value=7),
            PERMISSION_NULL=SimpleNamespace(value=0),
        ),
        ResourceType=SimpleNamespace(),
    )
    _stub(monkeypatch, "api.db.db_models", DB=SimpleNamespace(), Task=SimpleNamespace())
    _stub(
        monkeypatch,
        "api.db.services.api_service",
        API4ConversationService=SimpleNamespace(
            get_by_id=lambda _session_id: get_by_id_result,
            save=lambda **_kwargs: True,
            delete_by_id=_delete_by_id,
            query=lambda **_kwargs: [],
        ),
    )
    _stub(
        monkeypatch,
        "api.db.services.role_service",
        RoleResourceService=SimpleNamespace(
            get_by_role_id=lambda _role_id: [
                {
                    "resource_type": 3,
                    "action": 3,
                }
            ]
        ),
    )
    _stub(
        monkeypatch,
        "api.db.services.canvas_service",
        CanvasTemplateService=SimpleNamespace(),
        UserCanvasService=SimpleNamespace(accessible=lambda *_a, **_k: True, query=lambda **_kwargs: []),
        completion=lambda *_a, **_k: None,
        completion_openai=lambda *_a, **_k: None,
    )
    _stub(monkeypatch, "api.db.services.document_service", DocumentService=SimpleNamespace())
    _stub(monkeypatch, "api.db.services.file_service", FileService=SimpleNamespace())
    _stub(monkeypatch, "api.db.services.knowledgebase_service", KnowledgebaseService=SimpleNamespace())
    _stub(monkeypatch, "api.db.services.pipeline_operation_log_service", PipelineOperationLogService=SimpleNamespace())
    _stub(
        monkeypatch,
        "api.db.services.permission_service",
        PermissionChangeLogService=SimpleNamespace(),
        PermissionService=SimpleNamespace(),
    )
    _stub(monkeypatch, "api.db.services.task_service", CANVAS_DEBUG_DOC_ID="", TaskService=SimpleNamespace(), queue_dataflow=lambda *_a, **_k: None)
    _stub(
        monkeypatch,
        "api.db.services.user_service",
        TenantService=SimpleNamespace(),
        UserService=SimpleNamespace(get_by_id=lambda *_a, **_k: (False, None)),
        UserTenantService=SimpleNamespace(),
    )
    _stub(monkeypatch, "api.db.services.user_canvas_version", UserCanvasVersionService=SimpleNamespace())
    _stub(
        monkeypatch,
        "api.utils.permission_utils",
        check_canvas_permission=lambda *_a, **_k: lambda func: func,
        _permission_denied_message=lambda resource_name, permission: f"Only {resource_name} owners or members with {permission} permissions can perform this action.",
    )
    _stub(
        monkeypatch,
        "api.utils.api_utils",
        add_tenant_id_to_kwargs=lambda func: func,
        check_duplicate_ids=lambda ids, _kind="item": (ids, []),
        get_data_error_result=lambda message="Sorry": {"code": 102, "message": message, "data": None},
        get_error_data_result=lambda message="Sorry": {"code": 102, "message": message, "data": None},
        get_json_result=lambda code=0, message="", data=None: {"code": code, "message": message, "data": data},
        get_result=lambda **kwargs: kwargs,
        get_request_json=lambda: {},
        get_resource_insufficient_result=lambda *_a, **_k: {"code": 2000, "message": "insufficient", "data": None},
        server_error_response=lambda exc: {"code": 500, "message": str(exc)},
        validate_request=lambda *_a, **_k: lambda func: func,
    )
    _stub(
        monkeypatch,
        "api.utils.web_utils",
        CONTENT_TYPE_MAP={},
        apply_safe_file_response_headers=lambda response, *_a, **_k: response,
    )
    _stub(monkeypatch, "api.utils.pagination_utils", validate_rest_api_page_size=lambda *_a, **_k: None)
    _stub(monkeypatch, "api.utils.billing", check_dynamic_resources=lambda *_a, **_k: None, get_dynamic_resource_error_result=lambda *_a, **_k: None)
    _stub(monkeypatch, "common.settings", retriever=SimpleNamespace(), kg_retriever=SimpleNamespace())
    _stub(
        monkeypatch,
        "common.role_util",
        CANVAS_API_ACTION_MAP={},
        CANVAS_ROLE_RESOURCE_TYPE=SimpleNamespace(),
        check_role_access=lambda *_a, **_k: lambda func: func,
    )
    _stub(monkeypatch, "common.constants", RetCode=SimpleNamespace())
    _stub(monkeypatch, "common.misc_utils", get_uuid=lambda: "uuid", thread_pool_exec=lambda *_a, **_k: None)
    _stub(monkeypatch, "common.ssrf_guard", assert_host_is_safe=lambda *_a, **_k: None)
    _stub(monkeypatch, "peewee", MySQLDatabase=type("MySQLDatabase", (), {}), PostgresqlDatabase=type("PostgresqlDatabase", (), {}))

    repo_root = Path(__file__).resolve().parents[5]
    module_path = repo_root / "api" / "apps" / "restful_apis" / "agent_api.py"
    spec = importlib.util.spec_from_file_location("test_get_agent_session_agent_api", module_path)
    module = importlib.util.module_from_spec(spec)
    module.manager = _PassthroughManager()
    monkeypatch.setitem(sys.modules, "test_get_agent_session_agent_api", module)
    spec.loader.exec_module(module)
    return module, delete_calls


@pytest.mark.p1
class TestGetAgentSession:
    """Regression for missing sessions and IDOR on GET /agents/<id>/sessions/<sid>."""

    @pytest.mark.p1
    def test_returns_error_when_session_missing(self, monkeypatch):
        module, _ = _load_agent_api(monkeypatch, get_by_id_result=(False, None))

        result = module.get_agent_session(agent_id="agent-1", session_id="does-not-exist", tenant_id="tenant-1")

        assert result == {
            "code": 102,
            "message": "Session not found!",
            "data": None,
        }

    @pytest.mark.p1
    def test_returns_session_dict_when_found(self, monkeypatch):
        """When the session exists, the route returns its `to_dict()` payload."""
        conv = SimpleNamespace(dialog_id="agent-1", to_dict=lambda: {"id": "sess-1", "messages": []})
        module, _ = _load_agent_api(monkeypatch, get_by_id_result=(True, conv))

        result = module.get_agent_session(agent_id="agent-1", session_id="sess-1", tenant_id="tenant-1")

        assert result == {
            "code": 0,
            "message": "",
            "data": {"id": "sess-1", "messages": []},
        }

    @pytest.mark.p1
    def test_get_rejects_session_for_different_agent(self, monkeypatch):
        conv = SimpleNamespace(dialog_id="agent-victim", to_dict=lambda: {"id": "sess-1"})
        module, _ = _load_agent_api(monkeypatch, get_by_id_result=(True, conv))

        result = module.get_agent_session(agent_id="agent-attacker", session_id="sess-1", tenant_id="tenant-1")

        assert result["message"] == "Session not found!"
        assert result["data"] is None


@pytest.mark.p1
class TestDeleteAgentSession:
    """Regression for IDOR on DELETE /agents/<id>/sessions/<sid>."""

    @pytest.mark.p1
    def test_delete_rejects_session_for_different_agent(self, monkeypatch):
        conv = SimpleNamespace(dialog_id="agent-victim")
        module, delete_calls = _load_agent_api(monkeypatch, get_by_id_result=(True, conv))

        result = module.delete_agent_session_item(agent_id="agent-attacker", session_id="sess-1", tenant_id="tenant-1")

        assert result["message"] == "Session not found!"
        assert delete_calls == []

    @pytest.mark.p1
    def test_delete_succeeds_when_session_belongs_to_agent(self, monkeypatch):
        conv = SimpleNamespace(dialog_id="agent-1")
        module, delete_calls = _load_agent_api(monkeypatch, get_by_id_result=(True, conv))

        result = module.delete_agent_session_item(agent_id="agent-1", session_id="sess-1", tenant_id="tenant-1")

        assert result == {"code": 0, "message": "", "data": True}
        assert delete_calls == ["sess-1"]
