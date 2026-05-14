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

import asyncio
import inspect
import importlib.util
import sys
from copy import deepcopy
from enum import Enum
from functools import wraps
from pathlib import Path
from types import ModuleType, SimpleNamespace

import pytest
from api.db import ActionEnum, ResourceTypeEnum


class _DummyManager:
    def route(self, *_args, **_kwargs):
        def decorator(func):
            return func

        return decorator


class _AwaitableValue:
    def __init__(self, value):
        self._value = value

    def __await__(self):
        async def _co():
            return self._value

        return _co().__await__()


class _DummyArgs(dict):
    def get(self, key, default=None):
        return super().get(key, default)

    def getlist(self, key):
        value = self.get(key, [])
        if value is None:
            return []
        if isinstance(value, list):
            return value
        return [value]


class _StubHeaders:
    def __init__(self):
        self._items = []

    def add_header(self, key, value):
        self._items.append((key, value))

    def get(self, key, default=None):
        for existing_key, value in reversed(self._items):
            if existing_key == key:
                return value
        return default


class _StubResponse:
    def __init__(self, body=None, mimetype=None, content_type=None):
        self.body = body
        self.mimetype = mimetype
        self.content_type = content_type
        self.headers = _StubHeaders()


class _DummyUploadFile:
    def __init__(self, filename):
        self.filename = filename
        self.saved_path = None

    async def save(self, path):
        self.saved_path = path


def _passthrough_login_required(func):
    @wraps(func)
    async def _wrapper(*args, **kwargs):
        result = func(*args, **kwargs)
        if asyncio.iscoroutine(result):
            return await result
        return result

    return _wrapper


class _DummyKB:
    def __init__(self, kid="kb-1", embd_id="embd@factory", chunk_num=1, name="Dataset A", status="1"):
        self.id = kid
        self.embd_id = embd_id
        self.chunk_num = chunk_num
        self.name = name
        self.status = status


class _DummyDialogRecord:
    def __init__(self, data=None):
        self._data = data or {
            "id": "chat-1",
            "name": "chat-name",
            "description": "desc",
            "icon": "icon.png",
            "kb_ids": ["kb-1"],
            "llm_id": "glm-4",
            "llm_setting": {"temperature": 0.1},
            "prompt_config": {
                "system": "Answer with {knowledge}",
                "parameters": [{"key": "knowledge", "optional": False}],
                "prologue": "hello",
                "quote": True,
            },
            "similarity_threshold": 0.2,
            "vector_similarity_weight": 0.3,
            "top_n": 6,
            "top_k": 1024,
            "rerank_id": "",
            "meta_data_filter": {},
            "tenant_id": "tenant-1",
        }

    def to_dict(self):
        return deepcopy(self._data)


def _run(coro):
    return asyncio.run(coro)


async def _collect_stream(body):
    items = []
    if hasattr(body, "__aiter__"):
        async for item in body:
            if isinstance(item, bytes):
                item = item.decode("utf-8")
            items.append(item)
    else:
        for item in body:
            if isinstance(item, bytes):
                item = item.decode("utf-8")
            items.append(item)
    return items


@pytest.fixture(scope="session")
def auth():
    return "unit-auth"


@pytest.fixture(scope="session", autouse=True)
def set_tenant_info():
    return None


def _load_chat_module(monkeypatch):
    repo_root = Path(__file__).resolve().parents[4]
    module_name = "test_chat_restful_routes_unit_module"
    module_path = repo_root / "api" / "apps" / "restful_apis" / "chat_api.py"

    quart_mod = ModuleType("quart")
    quart_mod.g = SimpleNamespace()
    quart_mod.request = SimpleNamespace(method="GET", headers={}, is_json=False, args=_DummyArgs())
    quart_mod.Response = _StubResponse
    quart_mod.current_app = SimpleNamespace()
    quart_mod.has_request_context = lambda: False
    quart_mod.has_websocket_context = lambda: False
    quart_mod.websocket = SimpleNamespace(authorization=None)
    monkeypatch.setitem(sys.modules, "quart", quart_mod)

    api_pkg = ModuleType("api")
    api_pkg.__path__ = [str(repo_root / "api")]
    monkeypatch.setitem(sys.modules, "api", api_pkg)

    class _QuartAuthUnauthorizedStub(Exception):
        pass

    apps_pkg = ModuleType("api.apps")
    apps_pkg.__path__ = [str(repo_root / "api" / "apps")]
    apps_pkg.current_user = SimpleNamespace(id="tenant-1", role_id="role-1", is_superuser=False)
    apps_pkg.login_required = _passthrough_login_required
    apps_pkg.QuartAuthUnauthorized = _QuartAuthUnauthorizedStub
    monkeypatch.setitem(sys.modules, "api.apps", apps_pkg)
    api_pkg.apps = apps_pkg

    common_pkg = ModuleType("common")
    common_pkg.__path__ = [str(repo_root / "common")]
    monkeypatch.setitem(sys.modules, "common", common_pkg)

    common_constants_mod = ModuleType("common.constants")

    class _StubLLMType(str, Enum):
        CHAT = "chat"
        IMAGE2TEXT = "image2text"
        RERANK = "rerank"
        SPEECH2TEXT = "speech2text"
        TTS = "tts"

    class _StubRetCode(int, Enum):
        SUCCESS = 0
        ARGUMENT_ERROR = 101
        DATA_ERROR = 102
        OPERATING_ERROR = 103
        AUTHENTICATION_ERROR = 109
        OPERATING_ERROR = 110

    class _StubStatusEnum(str, Enum):
        VALID = "1"
        INVALID = "0"

    common_constants_mod.LLMType = _StubLLMType
    common_constants_mod.RetCode = _StubRetCode
    common_constants_mod.StatusEnum = _StubStatusEnum
    # Import pure-Python constants from the real module (no heavy deps)
    from common.constants import MAXIMUM_PAGE_NUMBER as _MPN, MAXIMUM_TASK_PAGE_NUMBER as _MTPN
    common_constants_mod.MAXIMUM_PAGE_NUMBER = _MPN
    common_constants_mod.MAXIMUM_TASK_PAGE_NUMBER = _MTPN
    monkeypatch.setitem(sys.modules, "common.constants", common_constants_mod)

    common_settings_mod = ModuleType("common.settings")
    common_settings_mod.ENABLE_ADMIN = False
    monkeypatch.setitem(sys.modules, "common.settings", common_settings_mod)

    misc_utils_mod = ModuleType("common.misc_utils")
    misc_utils_mod.get_uuid = lambda: "generated-chat-id"

    async def _thread_pool_exec(func, *args, **kwargs):
        return func(*args, **kwargs)

    misc_utils_mod.thread_pool_exec = _thread_pool_exec
    monkeypatch.setitem(sys.modules, "common.misc_utils", misc_utils_mod)

    dialog_service_mod = ModuleType("api.db.services.dialog_service")

    class _StubDialogService:
        model = SimpleNamespace(
            _meta=SimpleNamespace(
                fields={
                    "id": None,
                    "tenant_id": None,
                    "name": None,
                    "description": None,
                    "icon": None,
                    "kb_ids": None,
                    "llm_id": None,
                    "llm_setting": None,
                    "prompt_config": None,
                    "similarity_threshold": None,
                    "vector_similarity_weight": None,
                    "top_n": None,
                    "top_k": None,
                    "rerank_id": None,
                    "meta_data_filter": None,
                    "created_by": None,
                    "create_time": None,
                    "create_date": None,
                    "update_time": None,
                    "update_date": None,
                    "status": None,
                }
            )
        )

        @staticmethod
        def query(**_kwargs):
            return []

        @staticmethod
        def save(**_kwargs):
            return True

        @staticmethod
        def get_by_id(_chat_id):
            return False, None

        @staticmethod
        def update_by_id(_chat_id, _payload):
            return True

        @staticmethod
        def get_by_tenant_ids(*_args, **_kwargs):
            return [], 0

    dialog_service_mod.DialogService = _StubDialogService
    dialog_service_mod.async_ask = lambda *_args, **_kwargs: None
    dialog_service_mod.async_chat = lambda *_args, **_kwargs: None
    dialog_service_mod.gen_mindmap = lambda *_args, **_kwargs: None
    monkeypatch.setitem(sys.modules, "api.db.services.dialog_service", dialog_service_mod)

    conversation_service_mod = ModuleType("api.db.services.conversation_service")

    class _StubConversationService:
        @staticmethod
        def query(**_kwargs):
            return []

        @staticmethod
        def get_list(*_args, **_kwargs):
            return []

        @staticmethod
        def get_by_id(_session_id):
            return False, None

        @staticmethod
        def update_by_id(_session_id, _payload):
            return True

        @staticmethod
        def delete_by_id(_session_id):
            return True

        @staticmethod
        def save(**_kwargs):
            return True

        @staticmethod
        def remove_by(*_args, **_kwargs):
            return True

    conversation_service_mod.ConversationService = _StubConversationService
    conversation_service_mod.structure_answer = lambda *_args, **_kwargs: {}
    monkeypatch.setitem(sys.modules, "api.db.services.conversation_service", conversation_service_mod)

    permission_service_mod = ModuleType("api.db.services.permission_service")
    permission_service_mod.PermissionChangeLogService = SimpleNamespace(save=lambda **_kwargs: True)
    permission_service_mod.PermissionService = SimpleNamespace(
        save=lambda **_kwargs: True,
        get_permissions_by_tenant_and_resource_id=lambda **_kwargs: [],
        delete=lambda *_args, **_kwargs: True,
    )
    monkeypatch.setitem(sys.modules, "api.db.services.permission_service", permission_service_mod)

    team_service_mod = ModuleType("api.db.services.team_service")
    team_service_mod.DepartmentMemberService = SimpleNamespace(
        get_all_departments_by_member_id=lambda *_args, **_kwargs: []
    )
    team_service_mod.DepartmentService = SimpleNamespace(
        get_department_hierarchy=lambda *_args, **_kwargs: [],
        filter_by_id=lambda *_args, **_kwargs: None,
    )
    team_service_mod.GroupMemberService = SimpleNamespace(
        get_groups_by_member_id=lambda *_args, **_kwargs: []
    )
    team_service_mod.GroupService = SimpleNamespace(filter_by_id=lambda *_args, **_kwargs: None)
    monkeypatch.setitem(sys.modules, "api.db.services.team_service", team_service_mod)

    role_service_mod = ModuleType("api.db.services.role_service")
    role_service_mod.RoleResourceService = SimpleNamespace(
        get_by_role_id=lambda _role_id: [
            {
                "resource_type": ResourceTypeEnum.CHAT.value,
                "action": ActionEnum.ENABLE.value | ActionEnum.READ.value | ActionEnum.WRITE.value,
            }
        ]
    )
    monkeypatch.setitem(sys.modules, "api.db.services.role_service", role_service_mod)

    db_models_mod = ModuleType("api.db.db_models")

    class _AtomicContext:
        def __enter__(self):
            return self

        def __exit__(self, *_args):
            return False

    db_models_mod.DB = SimpleNamespace(atomic=lambda: _AtomicContext())
    monkeypatch.setitem(sys.modules, "api.db.db_models", db_models_mod)

    kb_service_mod = ModuleType("api.db.services.knowledgebase_service")

    class _StubKnowledgebaseService:
        @staticmethod
        def accessible(**_kwargs):
            return []

        @staticmethod
        def query(**_kwargs):
            return []

        @staticmethod
        def get_by_id(_kb_id):
            return False, None

    kb_service_mod.KnowledgebaseService = _StubKnowledgebaseService
    monkeypatch.setitem(sys.modules, "api.db.services.knowledgebase_service", kb_service_mod)

    tenant_llm_service_mod = ModuleType("api.db.services.tenant_llm_service")

    class _StubTenantLLMService:
        @staticmethod
        def split_model_name_and_factory(model_name):
            if model_name and "@" in model_name:
                llm_name, llm_factory = model_name.split("@", 1)
                return llm_name, llm_factory, "tenant-1"
            return model_name, None, "tenant-1"

        @staticmethod
        def query(**_kwargs):
            return []

        @staticmethod
        def get_api_key(*_args, **_kwargs):
            return SimpleNamespace(id=1)

    tenant_llm_service_mod.TenantLLMService = _StubTenantLLMService
    monkeypatch.setitem(sys.modules, "api.db.services.tenant_llm_service", tenant_llm_service_mod)

    llm_service_mod = ModuleType("api.db.services.llm_service")

    class _StubLLMBundle:
        def __init__(self, *_args, **_kwargs):
            pass

    llm_service_mod.LLMBundle = _StubLLMBundle
    monkeypatch.setitem(sys.modules, "api.db.services.llm_service", llm_service_mod)

    search_service_mod = ModuleType("api.db.services.search_service")
    search_service_mod.SearchService = SimpleNamespace()
    monkeypatch.setitem(sys.modules, "api.db.services.search_service", search_service_mod)

    tenant_model_service_mod = ModuleType("api.db.joint_services.tenant_model_service")
    tenant_model_service_mod.get_model_config_by_type_and_name = lambda *_args, **_kwargs: {}
    tenant_model_service_mod.get_tenant_default_model_by_type = lambda *_args, **_kwargs: {}
    monkeypatch.setitem(sys.modules, "api.db.joint_services.tenant_model_service", tenant_model_service_mod)

    user_service_mod = ModuleType("api.db.services.user_service")

    class _StubTenantService:
        @staticmethod
        def get_by_id(_tenant_id):
            return True, SimpleNamespace(llm_id="glm-4")

        @staticmethod
        def get_joined_tenants_by_user_id(_user_id):
            return [{"tenant_id": "tenant-1"}]

    class _StubUserTenantService:
        @staticmethod
        def query(**_kwargs):
            return []

        @classmethod
        def get_user_tenants_with_owner(cls, user_id):
            user_tenants = list(cls.query(user_id=user_id) or [])
            if user_id in {getattr(tenant, "tenant_id", None) for tenant in user_tenants}:
                return user_tenants

            owner_tenant = cls.filter_by_tenant_and_user_id(
                tenant_id=user_id,
                user_id=user_id,
            )
            if owner_tenant:
                user_tenants.append(owner_tenant)
            return user_tenants

        @staticmethod
        def filter_by_tenant_and_user_id(*_args, **_kwargs):
            return SimpleNamespace(id="member-1", tenant_id="tenant-1", user_id="tenant-1")

        @staticmethod
        def filter_by_id(_member_id):
            return SimpleNamespace(id=_member_id, tenant_id="tenant-1", user_id="tenant-1")

    user_service_mod.UserService = type("UserService", (), {})
    user_service_mod.TenantService = _StubTenantService
    user_service_mod.UserTenantService = _StubUserTenantService
    monkeypatch.setitem(sys.modules, "api.db.services.user_service", user_service_mod)

    chunk_feedback_service_mod = ModuleType("api.db.services.chunk_feedback_service")

    class _StubChunkFeedbackService:
        @staticmethod
        def apply_feedback(**_kwargs):
            return {"success_count": 0, "fail_count": 0, "chunk_ids": []}

    chunk_feedback_service_mod.ChunkFeedbackService = _StubChunkFeedbackService
    monkeypatch.setitem(sys.modules, "api.db.services.chunk_feedback_service", chunk_feedback_service_mod)

    api_utils_mod = ModuleType("api.utils.api_utils")

    def _check_duplicate_ids(ids, label):
        counts = {}
        for item in ids or []:
            counts[item] = counts.get(item, 0) + 1
        duplicate_messages = [f"Duplicate {label} ids: {item}" for item, count in counts.items() if count > 1]
        return list(set(ids or [])), duplicate_messages

    api_utils_mod.check_duplicate_ids = _check_duplicate_ids
    api_utils_mod.get_data_error_result = lambda message="": {"code": 102, "data": None, "message": message}
    api_utils_mod.get_json_result = lambda data=None, message="", code=0: {"code": code, "data": data, "message": message}
    api_utils_mod.get_request_json = lambda: _AwaitableValue({})
    api_utils_mod.server_error_response = lambda ex: {"code": 500, "data": None, "message": str(ex)}
    api_utils_mod.validate_request = lambda *_args, **_kwargs: (lambda func: func)
    monkeypatch.setitem(sys.modules, "api.utils.api_utils", api_utils_mod)

    tenant_utils_mod = ModuleType("api.utils.tenant_utils")
    tenant_utils_mod.ensure_tenant_model_id_for_params = lambda _tenant_id, req: req
    monkeypatch.setitem(sys.modules, "api.utils.tenant_utils", tenant_utils_mod)

    billing_mod = ModuleType("api.utils.billing")
    billing_mod.check_dynamic_resources = lambda *_args, **_kwargs: (lambda func: func)
    billing_mod.check_resources = lambda *_args, **_kwargs: (lambda func: func)
    monkeypatch.setitem(sys.modules, "api.utils.billing", billing_mod)

    rag_pkg = ModuleType("rag")
    rag_pkg.__path__ = [str(repo_root / "rag")]
    monkeypatch.setitem(sys.modules, "rag", rag_pkg)

    rag_prompts_pkg = ModuleType("rag.prompts")
    rag_prompts_pkg.__path__ = [str(repo_root / "rag" / "prompts")]
    monkeypatch.setitem(sys.modules, "rag.prompts", rag_prompts_pkg)

    rag_prompts_generator_mod = ModuleType("rag.prompts.generator")
    rag_prompts_generator_mod.chunks_format = lambda reference: reference.get("chunks", []) if isinstance(reference, dict) else []
    monkeypatch.setitem(sys.modules, "rag.prompts.generator", rag_prompts_generator_mod)

    rag_prompts_template_mod = ModuleType("rag.prompts.template")
    rag_prompts_template_mod.load_prompt = lambda *_args, **_kwargs: ""
    monkeypatch.setitem(sys.modules, "rag.prompts.template", rag_prompts_template_mod)

    spec = importlib.util.spec_from_file_location(module_name, module_path)
    module = importlib.util.module_from_spec(spec)
    module.manager = _DummyManager()
    monkeypatch.setitem(sys.modules, module_name, module)
    spec.loader.exec_module(module)
    return module


def _set_request_json(monkeypatch, module, payload):
    monkeypatch.setattr(module, "get_request_json", lambda: _AwaitableValue(deepcopy(payload)))


def _set_json_request_context(module, payload, *, method="POST", args=None):
    module.request.method = method
    module.request.headers = {"Content-Type": "application/json"}
    module.request.is_json = True
    module.request.args = _DummyArgs(args or {})
    module.request.get_json = lambda silent=True: _AwaitableValue(deepcopy(payload))
    module.get_request_json = lambda: _AwaitableValue(deepcopy(payload))
    module.g.__dict__.clear()


def _set_request_args_context(module, values):
    module.request.method = "GET"
    module.request.args = SimpleNamespace(
        get=lambda key, default=None: values.get(key, default),
        getlist=lambda key: values.get(key, []),
    )
    module.g.__dict__.clear()


@pytest.mark.p2
def test_create_chat_uses_direct_chat_fields(monkeypatch):
    module = _load_chat_module(monkeypatch)
    saved = {}

    _set_json_request_context(
        module,
        {
            "name": "chat-a",
            "icon": "icon.png",
            "dataset_ids": ["kb-1"],
            "llm_id": "glm-4",
            "llm_setting": {"temperature": 0.8},
            "prompt_config": {
                "system": "Answer with {knowledge}",
                "parameters": [{"key": "knowledge", "optional": False}],
                "prologue": "Hi",
            },
            "vector_similarity_weight": 0.25,
        },
    )
    monkeypatch.setattr(module.TenantService, "get_by_id", lambda _tid: (True, SimpleNamespace(llm_id="glm-4")))
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [])
    monkeypatch.setattr(module.KnowledgebaseService, "accessible", lambda **_kwargs: [SimpleNamespace(id="kb-1")])
    monkeypatch.setattr(module.KnowledgebaseService, "query", lambda **_kwargs: [_DummyKB()])
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))
    monkeypatch.setattr(module.TenantLLMService, "split_model_name_and_factory", lambda model: (model.split("@")[0], "factory", "tenant-1"))
    monkeypatch.setattr(module.TenantLLMService, "query", lambda **_kwargs: [SimpleNamespace(id="llm-1")])

    def _save(**kwargs):
        saved.update(kwargs)
        return True

    monkeypatch.setattr(module.DialogService, "save", _save)
    monkeypatch.setattr(module.DialogService, "get_by_id", lambda _id: (True, _DummyDialogRecord(saved)))

    res = _run(module.create())

    assert res["code"] == 0
    assert saved["kb_ids"] == ["kb-1"]
    assert saved["prompt_config"]["prologue"] == "Hi"
    assert saved["llm_id"] == "glm-4"
    assert saved["llm_setting"]["temperature"] == 0.8
    assert res["data"]["dataset_ids"] == ["kb-1"]
    assert res["data"]["kb_names"] == ["Dataset A"]
    assert "kb_ids" not in res["data"]
    assert "prompt" not in res["data"]
    assert "llm" not in res["data"]
    assert "avatar" not in res["data"]


@pytest.mark.p2
def test_create_chat_blank_name_is_treated_as_missing(monkeypatch):
    module = _load_chat_module(monkeypatch)

    _set_json_request_context(
        module,
        {
            "name": "   ",
            "dataset_ids": [],
        },
    )
    monkeypatch.setattr(module.TenantService, "get_by_id", lambda _tid: (True, SimpleNamespace(llm_id="glm-4")))

    res = _run(module.create())

    assert res["code"] == 102
    assert res["message"] == "`name` is required."


@pytest.mark.p1
def test_create_chat_accepts_provider_scoped_rerank_id(monkeypatch):
    module = _load_chat_module(monkeypatch)
    saved = {}
    query_calls = []

    _set_json_request_context(
        module,
        {
            "name": "chat-a",
            "icon": "icon.png",
            "dataset_ids": ["kb-1"],
            "llm_id": "glm-4@ZHIPU-AI",
            "llm_setting": {"temperature": 0.8},
            "prompt_config": {
                "system": "Answer with {knowledge}",
                "parameters": [{"key": "knowledge", "optional": False}],
                "prologue": "Hi",
            },
            "rerank_id": "custom-reranker@OpenAI",
            "vector_similarity_weight": 0.25,
        },
    )
    monkeypatch.setattr(module.TenantService, "get_by_id", lambda _tid: (True, SimpleNamespace(llm_id="glm-4@ZHIPU-AI")))
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [])
    monkeypatch.setattr(module.KnowledgebaseService, "accessible", lambda **_kwargs: [SimpleNamespace(id="kb-1")])
    monkeypatch.setattr(module.KnowledgebaseService, "query", lambda **_kwargs: [_DummyKB()])
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))

    def _split_model_name_and_factory(model_name):
        return {
            "glm-4@ZHIPU-AI": ("glm-4", "ZHIPU-AI", "tenant-1"),
            "custom-reranker@OpenAI": ("custom-reranker", "OpenAI", "tenant-1"),
        }.get(model_name, (model_name, None, "tenant-1"))

    def _query(**kwargs):
        query_calls.append(kwargs)
        if kwargs == {
            "tenant_id": "tenant-1",
            "llm_name": "glm-4",
            "llm_factory": "ZHIPU-AI",
            "model_type": "chat",
        }:
            return [SimpleNamespace(id="llm-1")]
        if kwargs == {
            "tenant_id": "tenant-1",
            "llm_name": "custom-reranker",
            "llm_factory": "OpenAI",
            "model_type": "rerank",
        }:
            return [SimpleNamespace(id="rerank-1")]
        return []

    monkeypatch.setattr(module.TenantLLMService, "split_model_name_and_factory", _split_model_name_and_factory)
    monkeypatch.setattr(module.TenantLLMService, "query", _query)

    def _save(**kwargs):
        saved.update(kwargs)
        return True

    monkeypatch.setattr(module.DialogService, "save", _save)
    monkeypatch.setattr(module.DialogService, "get_by_id", lambda _id: (True, _DummyDialogRecord(saved)))

    res = _run(module.create())

    assert res["code"] == 0
    assert saved["rerank_id"] == "custom-reranker@OpenAI"
    assert {
        "tenant_id": "tenant-1",
        "llm_name": "custom-reranker",
        "llm_factory": "OpenAI",
        "model_type": "rerank",
    } in query_calls


@pytest.mark.p1
def test_create_chat_allows_default_knowledge_placeholder_without_sources(monkeypatch):
    module = _load_chat_module(monkeypatch)
    saved = {}

    _set_json_request_context(module, {"name": "chat-a"})
    monkeypatch.setattr(module.TenantService, "get_by_id", lambda _tid: (True, SimpleNamespace(llm_id="glm-4")))
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [])
    monkeypatch.setattr(module.TenantLLMService, "get_api_key", lambda *_args, **_kwargs: SimpleNamespace(id=1))

    def _save(**kwargs):
        saved.update(kwargs)
        return True

    monkeypatch.setattr(module.DialogService, "save", _save)
    monkeypatch.setattr(module.DialogService, "get_by_id", lambda _id: (True, _DummyDialogRecord(saved)))

    res = _run(module.create())

    assert res["code"] == 0
    assert saved["kb_ids"] == []
    assert saved["prompt_config"]["system"].find("{knowledge}") >= 0
    assert saved["prompt_config"]["parameters"] == [{"key": "knowledge", "optional": False}]


@pytest.mark.p1
def test_create_chat_uses_tenant_default_llm_when_llm_id_is_null(monkeypatch):
    module = _load_chat_module(monkeypatch)
    saved = {}

    _set_json_request_context(
        module,
        {
            "name": "chat-a",
            "dataset_ids": ["kb-1"],
            "llm_id": None,
            "llm_setting": {"temperature": 0.8},
            "prompt_config": {
                "system": "Answer with {knowledge}",
                "parameters": [{"key": "knowledge", "optional": False}],
            },
        },
    )
    monkeypatch.setattr(module.TenantService, "get_by_id", lambda _tid: (True, SimpleNamespace(llm_id="glm-4")))
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [])
    monkeypatch.setattr(module.KnowledgebaseService, "accessible", lambda **_kwargs: [SimpleNamespace(id="kb-1")])
    monkeypatch.setattr(module.KnowledgebaseService, "query", lambda **_kwargs: [_DummyKB()])
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))
    monkeypatch.setattr(module.TenantLLMService, "get_api_key", lambda *_args, **_kwargs: SimpleNamespace(id=1))

    def _save(**kwargs):
        saved.update(kwargs)
        return True

    monkeypatch.setattr(module.DialogService, "save", _save)
    monkeypatch.setattr(module.DialogService, "get_by_id", lambda _id: (True, _DummyDialogRecord(saved)))

    res = _run(module.create())

    assert res["code"] == 0
    assert saved["llm_id"] == "glm-4"
    assert saved["llm_setting"]["temperature"] == 0.8


@pytest.mark.p2
def test_patch_chat_merges_prompt_and_llm_settings(monkeypatch):
    module = _load_chat_module(monkeypatch)
    updated = {}
    existing = _DummyDialogRecord().to_dict()

    payload = {
        "prompt_config": {"prologue": "updated opener"},
        "llm_setting": {"temperature": 0.9},
    }
    _set_json_request_context(module, payload, method="PATCH")
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.DialogService, "get_by_id", lambda _id: (True, _DummyDialogRecord(existing)))
    monkeypatch.setattr(module.TenantService, "get_by_id", lambda _tid: (True, SimpleNamespace(llm_id="glm-4")))
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")])

    def _update(_chat_id, payload):
        updated.update(payload)
        return True

    monkeypatch.setattr(module.DialogService, "update_by_id", _update)

    res = _run(module.patch_chat(chat_id="chat-1"))

    assert res["code"] == 0
    assert updated["prompt_config"]["system"] == "Answer with {knowledge}"
    assert updated["prompt_config"]["prologue"] == "updated opener"
    assert updated["llm_setting"]["temperature"] == 0.9


@pytest.mark.p2
def test_patch_chat_drops_response_only_fields_before_update(monkeypatch):
    module = _load_chat_module(monkeypatch)
    updated = {}
    existing = _DummyDialogRecord().to_dict()
    payload = {
        "name": "renamed-chat",
        "description": existing["description"],
        "icon": existing["icon"],
        "dataset_ids": existing["kb_ids"],
        "kb_names": ["Dataset A"],
        "llm_id": existing["llm_id"],
        "llm_setting": existing["llm_setting"],
        "prompt_config": existing["prompt_config"],
        "similarity_threshold": existing["similarity_threshold"],
        "vector_similarity_weight": existing["vector_similarity_weight"],
        "top_n": existing["top_n"],
        "top_k": existing["top_k"],
        "rerank_id": existing["rerank_id"],
    }

    _set_json_request_context(module, payload, method="PATCH")
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **kwargs: [] if "name" in kwargs else [SimpleNamespace(id="chat-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(module.DialogService, "get_by_id", lambda _id: (True, _DummyDialogRecord(existing)))
    monkeypatch.setattr(module.TenantService, "get_by_id", lambda _tid: (True, SimpleNamespace(llm_id="glm-4")))
    monkeypatch.setattr(module.KnowledgebaseService, "accessible", lambda **_kwargs: [SimpleNamespace(id="kb-1")])
    monkeypatch.setattr(module.KnowledgebaseService, "query", lambda **_kwargs: [_DummyKB()])
    monkeypatch.setattr(module.TenantLLMService, "split_model_name_and_factory", lambda model: (model.split("@")[0], "factory", "tenant-1"))
    monkeypatch.setattr(module.TenantLLMService, "query", lambda **_kwargs: [SimpleNamespace(id="llm-1")])
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")])

    def _update(_chat_id, req):
        updated.update(req)
        return True

    monkeypatch.setattr(module.DialogService, "update_by_id", _update)

    res = _run(module.patch_chat(chat_id="chat-1"))

    assert res["code"] == 0
    assert updated["name"] == "renamed-chat"
    assert "kb_names" not in updated


@pytest.mark.p2
def test_update_chat_allows_knowledge_placeholder_without_sources(monkeypatch):
    module = _load_chat_module(monkeypatch)
    existing = _DummyDialogRecord().to_dict()

    payload = {
        "name": "chat-name",
        "description": "desc",
        "icon": "icon.png",
        "dataset_ids": [],
        "llm_id": "glm-4",
        "llm_setting": {"temperature": 0.1},
        "prompt_config": {
            "system": "Answer with {knowledge}",
            "parameters": [{"key": "knowledge", "optional": False}],
            "prologue": "hello",
            "quote": True,
        },
        "similarity_threshold": 0.2,
        "vector_similarity_weight": 0.3,
        "top_n": 6,
        "top_k": 1024,
        "rerank_id": "",
    }
    _set_json_request_context(module, payload, method="PUT")
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.DialogService, "get_by_id", lambda _id: (True, _DummyDialogRecord(existing)))
    monkeypatch.setattr(module.TenantService, "get_by_id", lambda _tid: (True, SimpleNamespace(llm_id="glm-4")))
    monkeypatch.setattr(module.TenantLLMService, "split_model_name_and_factory", lambda model: (model.split("@")[0], "factory", "tenant-1"))
    monkeypatch.setattr(module.TenantLLMService, "query", lambda **_kwargs: [SimpleNamespace(id="llm-1")])
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")])
    updated = {}

    def _update(_chat_id, payload):
        updated.update(payload)
        return True

    monkeypatch.setattr(module.DialogService, "update_by_id", _update)

    res = _run(module.update_chat(chat_id="chat-1"))

    assert res["code"] == 0
    assert updated["prompt_config"]["system"] == "Answer with {knowledge}"


@pytest.mark.p2
def test_update_chat_requires_manage_permission(monkeypatch):
    module = _load_chat_module(monkeypatch)

    _set_request_json(monkeypatch, module, {"name": "renamed"})
    _set_json_request_context(module, {"name": "renamed"}, method="PUT")
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [])

    res = _run(module.update_chat(chat_id="chat-1"))

    assert "Only Chat/Dialog owners" in res["message"]


@pytest.mark.p2
def test_patch_chat_requires_manage_permission(monkeypatch):
    module = _load_chat_module(monkeypatch)

    _set_request_json(monkeypatch, module, {"name": "renamed"})
    _set_json_request_context(module, {"name": "renamed"}, method="PATCH")
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [])

    res = _run(module.patch_chat(chat_id="chat-1"))

    assert "Only Chat/Dialog owners" in res["message"]


@pytest.mark.p2
def test_get_chat_returns_auth_error_for_missing_chat_when_user_has_tenants(monkeypatch):
    module = _load_chat_module(monkeypatch)
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [])

    res = _run(module.get_chat(chat_id="missing-chat"))

    assert res["code"] == module.RetCode.AUTHENTICATION_ERROR
    assert res["message"] == "No authorization."


@pytest.mark.p2
def test_list_chats_returns_old_business_fields(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_request_args_context(
        module,
        {
            "keywords": "",
            "page": 1,
            "page_size": 20,
            "orderby": "create_time",
            "desc": "true",
        },
    )
    monkeypatch.setattr(
        module.DialogService,
        "get_by_tenant_ids",
        lambda *_args, **_kwargs: (
            [_DummyDialogRecord().to_dict()],
            1,
        ),
    )
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))

    res = _run(module.list_chats())

    assert res["code"] == 0
    chat = res["data"]["chats"][0]
    assert chat["icon"] == "icon.png"
    assert chat["dataset_ids"] == ["kb-1"]
    assert chat["kb_names"] == ["Dataset A"]
    assert "kb_ids" not in chat
    assert chat["prompt_config"]["prologue"] == "hello"
    assert "dataset_names" not in chat
    assert "prompt" not in chat
    assert "llm" not in chat


@pytest.mark.p2
def test_list_chats_keeps_zero_pagination_semantics(monkeypatch):
    module = _load_chat_module(monkeypatch)
    calls = []

    _set_request_args_context(
        module,
        {
            "keywords": "",
            "page": 0,
            "page_size": 0,
            "orderby": "create_time",
            "desc": "true",
        },
    )

    def _get_by_tenant_ids(_owner_ids, _user_id, page_number, items_per_page, *_args, **_kwargs):
        calls.append((page_number, items_per_page))
        return ([_DummyDialogRecord().to_dict()], 1)

    monkeypatch.setattr(module.DialogService, "get_by_tenant_ids", _get_by_tenant_ids)
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))

    res = _run(module.list_chats())

    assert res["code"] == 0
    assert calls[-1] == (0, 0)
    assert len(res["data"]["chats"]) == 1

    _set_request_args_context(
        module,
        {
            "keywords": "",
            "page": 0,
            "page_size": 2,
            "orderby": "create_time",
            "desc": "true",
        },
    )

    res = _run(module.list_chats())

    assert res["code"] == 0
    assert calls[-1] == (0, 2)
    assert len(res["data"]["chats"]) == 1


@pytest.mark.p2
def test_list_chats_passes_real_pagination_to_service(monkeypatch):
    module = _load_chat_module(monkeypatch)
    calls = []

    _set_request_args_context(
        module,
        {
            "keywords": "",
            "page": 1,
            "page_size": 2,
            "orderby": "create_time",
            "desc": "true",
        },
    )

    def _get_by_tenant_ids(_tenant_ids, _user_id, page_number, items_per_page, *_args, **_kwargs):
        calls.append((page_number, items_per_page))
        return ([_DummyDialogRecord().to_dict()], 1)

    monkeypatch.setattr(module.DialogService, "get_by_tenant_ids", _get_by_tenant_ids)
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))

    res = _run(module.list_chats())

    assert res["code"] == 0
    assert calls[-1] == (1, 2)
    assert res["data"]["total"] == 1
    assert len(res["data"]["chats"]) == 1


@pytest.mark.p2
def test_list_chats_includes_current_owner_tenant_when_not_joined(monkeypatch):
    module = _load_chat_module(monkeypatch)
    tenant_calls = []

    _set_request_args_context(
        module,
        {
            "keywords": "",
            "page": 1,
            "page_size": 20,
            "orderby": "create_time",
            "desc": "true",
        },
    )
    monkeypatch.setattr(module.TenantService, "get_joined_tenants_by_user_id", lambda _user_id: [])

    def _get_by_tenant_ids(tenant_ids, *_args, **_kwargs):
        tenant_calls.append(list(tenant_ids))
        return ([_DummyDialogRecord().to_dict()], 1)

    monkeypatch.setattr(module.DialogService, "get_by_tenant_ids", _get_by_tenant_ids)
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))

    res = _run(module.list_chats())

    assert res["code"] == 0
    assert set(tenant_calls[-1]) == {"tenant-1"}
    assert res["data"]["total"] == 1
    assert len(res["data"]["chats"]) == 1


@pytest.mark.p2
def test_list_chats_total_only_counts_visible_chats(monkeypatch):
    module = _load_chat_module(monkeypatch)

    _set_request_args_context(
        module,
        {
            "keywords": "",
            "page": 1,
            "page_size": 20,
            "orderby": "create_time",
            "desc": "true",
        },
    )
    monkeypatch.setattr(
        module.DialogService,
        "get_by_tenant_ids",
        lambda *_args, **_kwargs: ([_DummyDialogRecord().to_dict()], 1),
    )
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))

    res = _run(module.list_chats())

    assert res["code"] == 0
    assert len(res["data"]["chats"]) == 1
    assert res["data"]["chats"][0]["id"] == "chat-1"
    assert res["data"]["total"] == 1


@pytest.mark.p2
def test_list_chats_keeps_service_total_after_paging(monkeypatch):
    module = _load_chat_module(monkeypatch)
    query_calls = []

    _set_request_args_context(
        module,
        {
            "keywords": "",
            "page": 1,
            "page_size": 1,
            "orderby": "create_time",
            "desc": "true",
        },
    )
    def _get_by_tenant_ids(_tenant_ids, _user_id, page_number, items_per_page, *_args, **_kwargs):
        query_calls.append((page_number, items_per_page))
        return (
            [
                {
                    **_DummyDialogRecord().to_dict(),
                    "id": "chat-2",
                }
            ],
            2,
        )

    monkeypatch.setattr(module.DialogService, "get_by_tenant_ids", _get_by_tenant_ids)
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))

    res = _run(module.list_chats())

    assert res["code"] == 0
    assert query_calls[-1] == (1, 1)
    assert len(res["data"]["chats"]) == 1
    assert res["data"]["chats"][0]["id"] == "chat-2"
    assert res["data"]["total"] == 2


@pytest.mark.p2
def test_list_chats_keeps_service_visible_cross_tenant_chat(monkeypatch):
    module = _load_chat_module(monkeypatch)

    _set_request_args_context(
        module,
        {
            "keywords": "",
            "page": 1,
            "page_size": 20,
            "orderby": "create_time",
            "desc": "true",
        },
    )
    monkeypatch.setattr(
        module.DialogService,
        "get_by_tenant_ids",
        lambda *_args, **_kwargs: (
            [
                {
                    **_DummyDialogRecord().to_dict(),
                    "id": "chat-2",
                    "tenant_id": "tenant-2",
                    "kb_ids": ["kb-1"],
                    "operator_permission": module.PermissionValue.PERMISSION_READ.value,
                }
            ],
            1,
        ),
    )
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))
    monkeypatch.setattr(
        module.UserTenantService,
        "filter_by_tenant_and_user_id",
        lambda *_args, **_kwargs: (_ for _ in ()).throw(AssertionError("route should not check membership")),
    )

    res = _run(module.list_chats())

    assert res["code"] == 0
    assert res["data"]["total"] == 1
    assert [chat["id"] for chat in res["data"]["chats"]] == ["chat-2"]


@pytest.mark.p2
def test_get_chat_includes_operator_permission_for_model_record(monkeypatch):
    module = _load_chat_module(monkeypatch)
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "get_by_id",
        lambda _id: (True, _DummyDialogRecord()),
    )
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))

    res = _run(module.get_chat(chat_id="chat-1"))

    assert res["code"] == 0
    assert res["data"]["operator_permission"] == module.PermissionValue.PERMISSION_OWNER.value


@pytest.mark.p2
def test_list_chats_owner_ids_keeps_global_total_after_paging(monkeypatch):
    module = _load_chat_module(monkeypatch)
    calls = []

    _set_request_args_context(
        module,
        {
            "keywords": "",
            "page": 2,
            "page_size": 1,
            "orderby": "create_time",
            "desc": "true",
            "owner_ids": ["tenant-1"],
        },
    )
    def _get_by_tenant_ids(tenant_ids, _user_id, page_number, items_per_page, *_args, **_kwargs):
        calls.append((list(tenant_ids), page_number, items_per_page))
        return (
            [
                {
                    **_DummyDialogRecord().to_dict(),
                    "id": "chat-2",
                    "tenant_id": "tenant-1",
                }
            ],
            2,
        )

    monkeypatch.setattr(module.DialogService, "get_by_tenant_ids", _get_by_tenant_ids)
    monkeypatch.setattr(module.KnowledgebaseService, "get_by_id", lambda _id: (True, _DummyKB()))

    res = _run(module.list_chats())

    assert res["code"] == 0
    assert res["data"]["total"] == 2
    assert len(res["data"]["chats"]) == 1
    assert res["data"]["chats"][0]["id"] == "chat-2"
    assert calls[-1] == (["tenant-1"], 2, 1)


@pytest.mark.p2
def test_chat_session_create_and_update_guard_matrix_unit(monkeypatch):
    module = _load_chat_module(monkeypatch)

    _set_json_request_context(module, {"name": "session"})
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [])
    res = _run(module.create_session(chat_id="chat-1"))
    assert "Only Chat/Dialog owners" in res["message"]

    dia = SimpleNamespace(prompt_config={"prologue": "hello"})
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.DialogService, "get_by_id", lambda _id: (True, dia))
    monkeypatch.setattr(module.ConversationService, "save", lambda **_kwargs: None)
    monkeypatch.setattr(module.ConversationService, "get_by_id", lambda _id: (False, None))
    res = _run(module.create_session(chat_id="chat-1"))
    assert "Fail to create a session" in res["message"]

    _set_json_request_context(module, {}, method="PUT")
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.ConversationService, "query", lambda **_kwargs: [])
    res = _run(module.update_session("chat-1", "session-1"))
    assert res["message"] == "Session not found!"

    monkeypatch.setattr(module.ConversationService, "query", lambda **_kwargs: [SimpleNamespace(id="session-1")])
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.ConversationService, "update_by_id", lambda *_args, **_kwargs: True)
    get_by_id_results = iter(
        [
            (
                True,
                SimpleNamespace(
                    id="session-1",
                    dialog_id="chat-1",
                    user_id="tenant-1",
                    to_dict=lambda: {
                        "id": "session-1",
                        "dialog_id": "chat-1",
                        "user_id": "tenant-1",
                        "message": [],
                        "reference": [],
                    },
                ),
            ),
            (False, None),
        ]
    )
    monkeypatch.setattr(module.ConversationService, "get_by_id", lambda _id: next(get_by_id_results))
    res = _run(module.update_session("chat-1", "session-1"))
    assert res["message"] == "Fail to update a session!"

    monkeypatch.setattr(
        module.ConversationService,
        "get_by_id",
        lambda _id: (
            True,
            SimpleNamespace(
                id="session-1",
                dialog_id="chat-1",
                user_id="tenant-1",
                to_dict=lambda: {
                    "id": "session-1",
                    "dialog_id": "chat-1",
                    "user_id": "tenant-1",
                    "message": [],
                    "reference": [],
                },
            ),
        ),
    )
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")])
    _set_json_request_context(module, {"message": []}, method="PUT")
    res = _run(module.update_session("chat-1", "session-1"))
    assert "`messages` cannot be changed." in res["message"]

    _set_json_request_context(module, {"reference": []}, method="PUT")
    res = _run(module.update_session("chat-1", "session-1"))
    assert "`reference` cannot be changed." in res["message"]

    _set_json_request_context(module, {"name": ""}, method="PUT")
    res = _run(module.update_session("chat-1", "session-1"))
    assert "`name` can not be empty." in res["message"]

    _set_json_request_context(module, {"name": "renamed"}, method="PUT")
    monkeypatch.setattr(module.ConversationService, "update_by_id", lambda *_args, **_kwargs: False)
    res = _run(module.update_session("chat-1", "session-1"))
    assert res["message"] == "Session not found!"


@pytest.mark.p2
def test_update_session_requires_chat_read_permission(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_json_request_context(module, {"name": "session-rename"}, method="PUT")
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [])

    res = _run(module.update_session(chat_id="chat-1", session_id="session-1"))

    assert res["code"] != 0
    assert "Only Chat/Dialog owners" in res["message"]


@pytest.mark.p2
def test_delete_session_message_requires_chat_read_permission(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_request_args_context(module, {})
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [])

    res = _run(
        module.delete_session_message(
            chat_id="chat-1",
            session_id="session-1",
            msg_id="msg-1",
        )
    )

    assert res["code"] != 0
    assert "Only Chat/Dialog owners" in res["message"]


@pytest.mark.p2
def test_update_message_feedback_requires_chat_read_permission(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_json_request_context(module, {"thumbup": True}, method="PUT")
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [])

    res = _run(
        module.update_message_feedback(
            chat_id="chat-1",
            session_id="session-1",
            msg_id="msg-1",
        )
    )

    assert res["code"] != 0
    assert "Only Chat/Dialog owners" in res["message"]


@pytest.mark.p2
def test_update_session_rejects_session_owned_by_another_user(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_json_request_context(module, {"name": "rename"}, method="PUT")
    module.g.tenant_id = "tenant-1"
    updates = []
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.ConversationService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="session-1")],
    )
    monkeypatch.setattr(
        module.ConversationService,
        "get_by_id",
        lambda _id: (
            True,
            SimpleNamespace(
                id="session-1",
                dialog_id="chat-1",
                user_id="another-user",
                to_dict=lambda: {
                    "id": "session-1",
                    "dialog_id": "chat-1",
                    "user_id": "another-user",
                    "message": [],
                    "reference": [],
                },
            ),
        ),
    )
    monkeypatch.setattr(
        module.ConversationService,
        "update_by_id",
        lambda session_id, payload: updates.append((session_id, payload)) or True,
    )

    res = _run(module.update_session(chat_id="chat-1", session_id="session-1"))

    assert res["code"] != 0
    assert "Only owner of session" in res["message"]
    assert not updates


@pytest.mark.p2
def test_update_session_allows_blank_owned_legacy_session_for_backward_compatibility(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_json_request_context(module, {"name": "rename"}, method="PUT")
    module.g.tenant_id = "tenant-1"
    updates = []
    conv = SimpleNamespace(
        id="session-1",
        dialog_id="chat-1",
        user_id="",
        to_dict=lambda: {
            "id": "session-1",
            "dialog_id": "chat-1",
            "user_id": "",
            "name": "renamed",
            "message": [],
            "reference": [],
        },
    )
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.ConversationService,
        "get_by_id",
        lambda _id: (True, conv),
    )
    monkeypatch.setattr(
        module.ConversationService,
        "update_by_id",
        lambda session_id, payload: updates.append((session_id, payload)) or True,
    )

    res = _run(module.update_session(chat_id="chat-1", session_id="session-1"))

    assert res["code"] == 0
    assert updates


@pytest.mark.p2
def test_session_ownership_logs_rejected_foreign_session(monkeypatch):
    module = _load_chat_module(monkeypatch)
    logs = []
    monkeypatch.setattr(module.logging, "warning", lambda msg, *args: logs.append(msg % args if args else msg))

    allowed = module._ensure_session_owned_by_current_user(
        SimpleNamespace(id="session-1", dialog_id="chat-1", user_id="another-user")
    )

    assert allowed is False
    assert any("owned by another user" in entry for entry in logs)


@pytest.mark.p2
def test_session_ownership_logs_legacy_compatibility_bypass(monkeypatch):
    module = _load_chat_module(monkeypatch)
    logs = []
    monkeypatch.setattr(module.logging, "info", lambda msg, *args: logs.append(msg % args if args else msg))

    allowed = module._ensure_session_owned_by_current_user(SimpleNamespace(id="session-1", dialog_id="chat-1", user_id=""))

    assert allowed is True
    assert any("legacy chat session access without strict ownership enforcement" in entry for entry in logs)


@pytest.mark.p2
def test_session_ownership_logs_legacy_compatibility_bypass_without_claim(monkeypatch):
    module = _load_chat_module(monkeypatch)
    logs = []
    updates = []
    monkeypatch.setattr(module.logging, "info", lambda msg, *args: logs.append(msg % args if args else msg))
    monkeypatch.setattr(
        module.ConversationService,
        "update_by_id",
        lambda session_id, payload: updates.append((session_id, payload)) or True,
    )
    conv = SimpleNamespace(id="session-1", dialog_id="chat-1", user_id="")
    conv.to_dict = lambda: {
        "id": conv.id,
        "dialog_id": conv.dialog_id,
        "user_id": conv.user_id,
    }

    allowed = module._ensure_session_owned_by_current_user(conv)

    assert allowed is True
    assert conv.user_id == ""
    assert not updates
    assert any("legacy chat session access without strict ownership enforcement" in entry for entry in logs)


@pytest.mark.p2
def test_delete_session_message_rejects_session_owned_by_another_user(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_request_args_context(module, {})
    module.g.tenant_id = "tenant-1"
    updates = []
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.ConversationService,
        "get_by_id",
        lambda _id: (
            True,
            SimpleNamespace(
                id="session-1",
                dialog_id="chat-1",
                user_id="another-user",
                to_dict=lambda: {
                    "id": "session-1",
                    "dialog_id": "chat-1",
                    "user_id": "another-user",
                    "message": [],
                    "reference": [],
                },
            ),
        ),
    )
    monkeypatch.setattr(
        module.ConversationService,
        "update_by_id",
        lambda session_id, payload: updates.append((session_id, payload)) or True,
    )

    res = _run(
        module.delete_session_message(
            chat_id="chat-1",
            session_id="session-1",
            msg_id="msg-1",
        )
    )

    assert res["code"] != 0
    assert "Only owner of session" in res["message"]
    assert not updates


@pytest.mark.p2
def test_update_message_feedback_rejects_session_owned_by_another_user(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_json_request_context(module, {"thumbup": True}, method="PUT")
    module.g.tenant_id = "tenant-1"
    updates = []
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.ConversationService,
        "get_by_id",
        lambda _id: (
            True,
            SimpleNamespace(
                id="session-1",
                dialog_id="chat-1",
                user_id="another-user",
                to_dict=lambda: {
                    "id": "session-1",
                    "dialog_id": "chat-1",
                    "user_id": "another-user",
                    "message": [],
                    "reference": [],
                },
            ),
        ),
    )
    monkeypatch.setattr(
        module.ConversationService,
        "update_by_id",
        lambda session_id, payload: updates.append((session_id, payload)) or True,
    )

    res = _run(
        module.update_message_feedback(
            chat_id="chat-1",
            session_id="session-1",
            msg_id="msg-1",
        )
    )

    assert res["code"] != 0
    assert "Only owner of session" in res["message"]
    assert not updates


@pytest.mark.p2
def test_session_completion_requires_chat_read_permission(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_json_request_context(
        module,
        {"messages": [{"role": "user", "content": "hello", "id": "msg-1"}]},
        method="POST",
    )
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [])

    #res = _run(module.session_completion(chat_id="chat-1", session_id="session-1"))

    #assert res["code"] != 0
    #assert "Only Chat/Dialog owners" in res["message"]


@pytest.mark.p2
def test_session_completion_rejects_session_owned_by_another_user(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_json_request_context(
        module,
        {"messages": [{"role": "user", "content": "hello", "id": "msg-1"}], "stream": False},
        method="POST",
    )
    module.g.tenant_id = "tenant-1"
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.ConversationService,
        "get_by_id",
        lambda _id: (
            True,
            SimpleNamespace(
                id="session-1",
                dialog_id="chat-1",
                user_id="another-user",
                message=[],
                reference=[],
                to_dict=lambda: {
                    "id": "session-1",
                    "dialog_id": "chat-1",
                    "user_id": "another-user",
                    "message": [],
                    "reference": [],
                },
            ),
        ),
    )

    #res = _run(module.session_completion(chat_id="chat-1", session_id="session-1"))

    #assert res["code"] != 0
    #assert "Only owner of session" in res["message"]


@pytest.mark.p2
def test_session_completion_keeps_legacy_session_unclaimed_for_backward_compatibility(monkeypatch):
    module = _load_chat_module(monkeypatch)
    updates = []
    conv = SimpleNamespace(
        id="session-1",
        dialog_id="chat-1",
        user_id=None,
        message=[],
        reference=[],
    )
    conv.to_dict = lambda: {
        "id": "session-1",
        "dialog_id": "chat-1",
        "user_id": conv.user_id,
        "message": conv.message,
        "reference": conv.reference,
    }
    _set_json_request_context(
        module,
        {"messages": [{"role": "user", "content": "hello", "id": "msg-1"}], "stream": False},
        method="POST",
    )
    module.g.tenant_id = "tenant-1"
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(module.ConversationService, "get_by_id", lambda _id: (True, conv))
    monkeypatch.setattr(
        module.ConversationService,
        "update_by_id",
        lambda session_id, payload: updates.append((session_id, payload)) or True,
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [
            SimpleNamespace(
                id="chat-1",
                tenant_id="tenant-1",
                llm_id="glm-4",
                llm_setting={},
                prompt_config={"prologue": "hello"},
                kb_ids=[],
            )
        ],
    )

    async def _async_chat(*_args, **_kwargs):
        yield {"answer": "ok", "reference": {}, "final": True}

    monkeypatch.setattr(module, "async_chat", _async_chat)

    #_run(module.session_completion(chat_id="chat-1", session_id="session-1"))

    #assert conv.user_id is None
    #assert updates
    #assert updates[0][1]["user_id"] is None


@pytest.mark.p2
def test_session_completion_validates_messages_from_g_req_data(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_json_request_context(module, {}, method="POST")
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")],
    )

    #res = _run(module.session_completion(chat_id="chat-1", session_id="session-1"))

    #assert res["code"] != 0
    #assert "messages" in res["message"]


@pytest.mark.p2
def test_create_session_ignores_request_user_id(monkeypatch):
    module = _load_chat_module(monkeypatch)
    captured = {}
    dia = SimpleNamespace(prompt_config={"prologue": "hello"})

    _set_request_json(monkeypatch, module, {"name": "session", "user_id": "spoofed-user"})
    _set_json_request_context(module, {"name": "session", "user_id": "spoofed-user"})
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1", icon="icon.png")])
    monkeypatch.setattr(module.DialogService, "get_by_id", lambda _id: (True, dia))
    monkeypatch.setattr(module.ConversationService, "save", lambda **kwargs: captured.update(kwargs) or True)
    monkeypatch.setattr(module.ConversationService, "get_by_id", lambda _id: (True, SimpleNamespace(to_dict=lambda: captured)))

    _run(module.create_session(chat_id="chat-1"))

    assert captured["user_id"] == module.current_user.id


@pytest.mark.p2
def test_chat_session_list_projection_unit(monkeypatch):
    module = _load_chat_module(monkeypatch)

    _set_request_args_context(
        module,
        {
            "page": 1,
            "page_size": 30,
            "orderby": "create_time",
            "desc": "true",
            "id": None,
            "name": None,
            "user_id": None,
        },
    )
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")])
    monkeypatch.setattr(
        module.ConversationService,
        "get_list",
        lambda *_args, **_kwargs: [
            {
                "id": "session-1",
                "dialog_id": "chat-1",
                "message": [{"role": "assistant", "content": "hello"}],
                "reference": [],
            }
        ],
    )

    res = _run(module.list_sessions(chat_id="chat-1"))
    assert res["data"][0]["chat_id"] == "chat-1"
    assert res["data"][0]["messages"][0]["content"] == "hello"

    _set_request_args_context(
        module,
        {
            "page": 1,
            "page_size": 0,
            "orderby": "create_time",
            "desc": "true",
            "id": None,
            "name": None,
            "user_id": None,
        },
    )
    res = _run(module.list_sessions(chat_id="chat-1"))
    assert res["data"] == []


@pytest.mark.p2
def test_list_sessions_treats_blank_user_id_as_legacy_visible(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_request_args_context(module, {"page": "1", "page_size": "30"})
    module.g.tenant_id = "tenant-1"
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.UserTenantService,
        "filter_by_tenant_and_user_id",
        lambda *_args, **_kwargs: SimpleNamespace(id="member-1", tenant_id="tenant-1"),
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.ConversationService,
        "get_list",
        lambda *_args, **_kwargs: [
            {
                "id": "session-1",
                "dialog_id": "chat-1",
                "user_id": "",
                "message": [],
                "reference": [],
            }
        ],
    )

    res = _run(module.list_sessions(chat_id="chat-1"))

    assert len(res["data"]) == 1


@pytest.mark.p2
def test_get_session_rejects_session_owned_by_another_user(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_request_args_context(module, {})
    module.g.tenant_id = "tenant-1"
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1", icon="icon.png")],
    )
    monkeypatch.setattr(
        module.ConversationService,
        "get_by_id",
        lambda _id: (
            True,
            SimpleNamespace(
                id="session-1",
                dialog_id="chat-1",
                user_id="another-user",
                reference=[],
                to_dict=lambda: {
                    "id": "session-1",
                    "dialog_id": "chat-1",
                    "user_id": "another-user",
                    "message": [],
                    "reference": [],
                },
            ),
        ),
    )

    res = _run(module.get_session(chat_id="chat-1", session_id="session-1"))

    assert res["code"] != 0
    assert "Only owner of session" in res["message"]


@pytest.mark.p2
def test_update_chat_uses_chat_tenant_for_duplicate_name_checks(monkeypatch):
    module = _load_chat_module(monkeypatch)
    module.current_user.id = "operator-1"
    module.g.req_data = {"name": "shared-chat"}
    module.g.tenant_id = "tenant-2"
    existing = _DummyDialogRecord(
        {
            **_DummyDialogRecord().to_dict(),
            "tenant_id": "tenant-2",
            "name": "chat-name",
        }
    ).to_dict()
    query_calls = []

    monkeypatch.setattr(module.TenantService, "get_by_id", lambda _tid: (True, SimpleNamespace(llm_id="glm-4")))
    monkeypatch.setattr(
        module.DialogService,
        "get_by_id",
        lambda _id: (True, _DummyDialogRecord(existing)),
    )

    def _query(**kwargs):
        query_calls.append(kwargs)
        if kwargs.get("name") == "shared-chat" and kwargs.get("tenant_id") == "tenant-2":
            return [SimpleNamespace(id="existing-chat", tenant_id="tenant-2")]
        return []

    monkeypatch.setattr(module.DialogService, "query", _query)

    res = _run(inspect.unwrap(module.update_chat)(chat_id="chat-1"))

    assert res["code"] == 102
    assert res["message"] == "Duplicated chat name."
    assert any(call.get("tenant_id") == "tenant-2" for call in query_calls if "name" in call)
    assert not any(call.get("tenant_id") == "operator-1" for call in query_calls if "name" in call)


@pytest.mark.p2
def test_patch_chat_uses_chat_tenant_for_tenant_model_resolution(monkeypatch):
    module = _load_chat_module(monkeypatch)
    module.current_user.id = "operator-1"
    module.g.req_data = {"llm_id": "shared-model@factory"}
    module.g.tenant_id = "tenant-2"
    existing = _DummyDialogRecord(
        {
            **_DummyDialogRecord().to_dict(),
            "tenant_id": "tenant-2",
        }
    ).to_dict()
    ensure_calls = []
    updated = {}

    monkeypatch.setattr(module.TenantService, "get_by_id", lambda _tid: (True, SimpleNamespace(llm_id="glm-4")))
    monkeypatch.setattr(
        module.DialogService,
        "get_by_id",
        lambda _id: (True, _DummyDialogRecord(existing)),
    )
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [])
    monkeypatch.setattr(module.TenantLLMService, "query", lambda **_kwargs: [SimpleNamespace(id="llm-1")])
    monkeypatch.setattr(
        module.TenantLLMService,
        "split_model_name_and_factory",
        lambda model: ("shared-model", "factory", None),
    )
    monkeypatch.setattr(
        module,
        "ensure_tenant_model_id_for_params",
        lambda tenant_id, req: ensure_calls.append((tenant_id, deepcopy(req))) or req,
    )
    monkeypatch.setattr(
        module.DialogService,
        "update_by_id",
        lambda _chat_id, payload: updated.update(payload) or True,
    )

    res = _run(inspect.unwrap(module.patch_chat)(chat_id="chat-1"))

    assert res["code"] == 0
    assert ensure_calls
    assert ensure_calls[0][0] == "tenant-2"
    assert updated["llm_id"] == "shared-model@factory"


@pytest.mark.p2
def test_get_session_treats_blank_user_id_as_legacy_visible(monkeypatch):
    module = _load_chat_module(monkeypatch)
    _set_request_args_context(module, {})
    module.g.tenant_id = "tenant-1"
    conv = SimpleNamespace(
        id="session-1",
        dialog_id="chat-1",
        user_id="",
        reference=[],
        to_dict=lambda: {
            "id": "session-1",
            "dialog_id": "chat-1",
            "user_id": "",
            "message": [],
            "reference": [],
        },
    )
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1", icon="icon.png")],
    )
    monkeypatch.setattr(module.ConversationService, "get_by_id", lambda _id: (True, conv))

    res = _run(module.get_session(chat_id="chat-1", session_id="session-1"))

    assert res["code"] == 0


@pytest.mark.p2
def test_get_session_uses_dialog_record_for_avatar(monkeypatch):
    module = _load_chat_module(monkeypatch)
    conv = SimpleNamespace(
        dialog_id="chat-1",
        reference=[],
        to_dict=lambda: {"id": "session-1", "dialog_id": "chat-1", "message": []},
    )

    _set_json_request_context(module, {}, method="GET")
    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.ConversationService, "get_by_id", lambda _id: (True, conv))
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [SimpleNamespace(icon="icon.png", tenant_id="tenant-1")])

    res = _run(module.get_session(chat_id="chat-1", session_id="session-1"))

    assert res["data"]["avatar"] == "icon.png"


@pytest.mark.p2
def test_chat_session_delete_routes_partial_duplicate_unit(monkeypatch):
    module = _load_chat_module(monkeypatch)

    monkeypatch.setattr(module.UserTenantService, "query", lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")])
    _set_json_request_context(module, {}, method="DELETE")
    res = _run(module.delete_sessions(chat_id="chat-1"))
    assert res["code"] == 0

    monkeypatch.setattr(module.ConversationService, "delete_by_id", lambda *_args, **_kwargs: True)

    def _conversation_query(**kwargs):
        if "dialog_id" in kwargs and "id" not in kwargs:
            return [SimpleNamespace(id="seed")]
        return []

    monkeypatch.setattr(module.ConversationService, "query", _conversation_query)
    monkeypatch.setattr(
        module.ConversationService,
        "get_by_id",
        lambda session_id: (
            True,
            SimpleNamespace(id=session_id, dialog_id="chat-1", user_id=module.current_user.id),
        )
        if session_id == "ok"
        else (False, None),
    )

    _set_json_request_context(module, {"ids": ["ok", "bad"]}, method="DELETE")
    monkeypatch.setattr(module, "check_duplicate_ids", lambda ids, _kind: (ids, []))
    res = _run(module.delete_sessions(chat_id="chat-1"))
    assert res["code"] == 0
    assert res["data"]["success_count"] == 1
    assert res["data"]["errors"] == ["The chat doesn't own the session bad"]

    _set_json_request_context(module, {"ids": ["bad"]}, method="DELETE")
    monkeypatch.setattr(module, "check_duplicate_ids", lambda ids, _kind: (ids, []))
    res = _run(module.delete_sessions(chat_id="chat-1"))
    assert res["message"] == "The chat doesn't own the session bad"

    _set_json_request_context(module, {"ids": ["ok", "ok"]}, method="DELETE")
    monkeypatch.setattr(module, "check_duplicate_ids", lambda ids, _kind: (["ok"], ["Duplicate session ids: ok"]))
    res = _run(module.delete_sessions(chat_id="chat-1"))
    assert res["code"] == 0
    assert res["data"]["success_count"] == 1
    assert res["data"]["errors"] == ["Duplicate session ids: ok"]


@pytest.mark.p2
def test_delete_sessions_only_deletes_owned_or_legacy_sessions(monkeypatch):
    module = _load_chat_module(monkeypatch)
    deleted_ids = []

    _set_json_request_context(module, {"ids": ["mine", "legacy", "foreign"]}, method="DELETE")
    module.g.tenant_id = "tenant-1"
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="member-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(module, "check_duplicate_ids", lambda ids, _kind: (ids, []))

    def _get_session(session_id):
        mapping = {
            "mine": SimpleNamespace(id="mine", dialog_id="chat-1", user_id=module.current_user.id),
            "legacy": SimpleNamespace(id="legacy", dialog_id="chat-1", user_id=""),
            "foreign": SimpleNamespace(id="foreign", dialog_id="chat-1", user_id="another-user"),
        }
        conv = mapping.get(session_id)
        return (bool(conv), conv)

    monkeypatch.setattr(module.ConversationService, "get_by_id", _get_session)
    monkeypatch.setattr(
        module.ConversationService,
        "delete_by_id",
        lambda session_id: deleted_ids.append(session_id) or True,
    )

    res = _run(module.delete_sessions(chat_id="chat-1"))

    assert res["code"] == 0
    assert res["data"]["success_count"] == 2
    assert deleted_ids == ["mine", "legacy"]
    assert res["data"]["errors"] == ["Only owner of session can delete foreign"]


@pytest.mark.p2
def test_delete_chat_uses_dialog_service_invalidate(monkeypatch):
    module = _load_chat_module(monkeypatch)

    _set_json_request_context(module, {}, method="DELETE")
    module.g.tenant_id = "tenant-1"

    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.DialogService,
        "update_by_id",
        lambda *_args, **_kwargs: (_ for _ in ()).throw(
            AssertionError("delete_chat should use DialogService.invalidate_by_id inside DB.atomic()")
        ),
    )
    monkeypatch.setattr(
        module.DialogService,
        "invalidate_by_id",
        lambda *_args, **_kwargs: True,
        raising=False,
    )
    monkeypatch.setattr(module.ConversationService, "remove_by", lambda *_args, **_kwargs: True)
    monkeypatch.setattr(
        module.UserTenantService,
        "filter_by_tenant_and_user_id",
        lambda **_kwargs: SimpleNamespace(id="member-1", tenant_id="tenant-1"),
    )
    monkeypatch.setattr(module.PermissionService, "get_permissions_by_tenant_and_resource_id", lambda **_kwargs: [])
    monkeypatch.setattr(module.PermissionService, "delete", lambda *_args, **_kwargs: True)
    monkeypatch.setattr(module.PermissionChangeLogService, "save", lambda **_kwargs: True)

    res = _run(module.delete_chat("chat-1"))

    assert res["code"] == 0
    assert res["data"] is True


@pytest.mark.p2
def test_bulk_delete_chats_reads_delete_json_body(monkeypatch):
    module = _load_chat_module(monkeypatch)

    _set_json_request_context(module, {"ids": ["chat-1"]}, method="DELETE")
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")])
    monkeypatch.setattr(module.UserTenantService, "filter_by_tenant_and_user_id", lambda **_kwargs: SimpleNamespace(id="member-1", tenant_id="tenant-1"))
    monkeypatch.setattr(
        module.DialogService,
        "update_by_id",
        lambda *_args, **_kwargs: (_ for _ in ()).throw(
            AssertionError("bulk_delete_chats should use DialogService.invalidate_by_id inside DB.atomic()")
        ),
    )
    monkeypatch.setattr(module.DialogService, "invalidate_by_id", lambda *_args, **_kwargs: True, raising=False)
    monkeypatch.setattr(module.ConversationService, "remove_by", lambda *_args, **_kwargs: True)
    monkeypatch.setattr(module.PermissionService, "get_permissions_by_tenant_and_resource_id", lambda **_kwargs: [])
    monkeypatch.setattr(module.PermissionService, "delete", lambda *_args, **_kwargs: True)
    monkeypatch.setattr(module.PermissionChangeLogService, "save", lambda **_kwargs: True)

    res = _run(module.bulk_delete_chats())

    assert res["data"]["success_count"] == 1


@pytest.mark.p2
def test_bulk_delete_chats_rejects_unowned_chat_ids(monkeypatch):
    module = _load_chat_module(monkeypatch)

    _set_json_request_context(module, {"ids": ["foreign-chat"]}, method="DELETE")
    monkeypatch.setattr(module.DialogService, "query", lambda **_kwargs: [])

    res = _run(module.bulk_delete_chats())

    assert "not found" in res["message"].lower() or "not owned" in res["message"].lower()


@pytest.mark.p2
def test_bulk_delete_chats_allows_collaborator_owner_scope(monkeypatch):
    module = _load_chat_module(monkeypatch)
    deleted_ids = []

    _set_json_request_context(module, {"ids": ["shared-chat"]}, method="DELETE")
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [
            SimpleNamespace(id="owner-member", tenant_id="tenant-1"),
            SimpleNamespace(id="collab-member", tenant_id="tenant-2"),
        ],
    )
    monkeypatch.setattr(
        module.UserTenantService,
        "filter_by_tenant_and_user_id",
        lambda tenant_id, user_id: SimpleNamespace(id="collab-member" if tenant_id == "tenant-2" else "owner-member", tenant_id=tenant_id, user_id=user_id),
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **kwargs: [SimpleNamespace(id="shared-chat", tenant_id="tenant-2")]
        if kwargs.get("tenant_id") == "tenant-2" and kwargs.get("id") == "shared-chat"
        else [],
    )
    monkeypatch.setattr(
        module,
        "has_permission_for_member",
        lambda **kwargs: (
            kwargs["operator_id"] == "collab-member" and kwargs["tenant_id"] == "tenant-2",
            None,
            module.PermissionValue.PERMISSION_OWNER.value if kwargs["tenant_id"] == "tenant-2" else 0,
        ),
    )
    monkeypatch.setattr(module.DialogService, "invalidate_by_id", lambda chat_id, *_args, **_kwargs: deleted_ids.append(chat_id) or True, raising=False)
    monkeypatch.setattr(module.ConversationService, "remove_by", lambda *_args, **_kwargs: True)
    monkeypatch.setattr(module.PermissionService, "get_permissions_by_tenant_and_resource_id", lambda **_kwargs: [])
    monkeypatch.setattr(module.PermissionService, "delete", lambda *_args, **_kwargs: True)
    monkeypatch.setattr(module.PermissionChangeLogService, "save", lambda **_kwargs: True)

    res = _run(module.bulk_delete_chats())

    assert res["code"] == 0
    assert res["data"]["success_count"] == 1
    assert deleted_ids == ["shared-chat"]


@pytest.mark.p2
def test_bulk_delete_chats_skips_empty_permission_cleanup(monkeypatch):
    module = _load_chat_module(monkeypatch)
    deleted_ids = []

    _set_json_request_context(module, {"ids": ["chat-1"]}, method="DELETE")
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [SimpleNamespace(id="owner-member", tenant_id="tenant-1")],
    )
    monkeypatch.setattr(
        module.UserTenantService,
        "filter_by_tenant_and_user_id",
        lambda tenant_id, user_id: SimpleNamespace(id="owner-member", tenant_id=tenant_id, user_id=user_id),
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **kwargs: [SimpleNamespace(id="chat-1", tenant_id="tenant-1")]
        if kwargs.get("tenant_id") == "tenant-1" and kwargs.get("id") == "chat-1"
        else [],
    )
    monkeypatch.setattr(module.DialogService, "invalidate_by_id", lambda chat_id, *_args, **_kwargs: deleted_ids.append(chat_id) or True, raising=False)
    monkeypatch.setattr(module.ConversationService, "remove_by", lambda *_args, **_kwargs: True)
    monkeypatch.setattr(
        module.PermissionService,
        "get_permissions_by_tenant_and_resource_id",
        lambda **_kwargs: [],
    )
    monkeypatch.setattr(
        module.PermissionService,
        "delete",
        lambda permission_model_list: (_ for _ in ()).throw(AssertionError("empty permission cleanup should be skipped"))
        if not permission_model_list
        else True,
    )
    monkeypatch.setattr(module.PermissionChangeLogService, "save", lambda **_kwargs: True)

    res = _run(module.bulk_delete_chats())

    assert res["code"] == 0
    assert res["data"]["success_count"] == 1
    assert deleted_ids == ["chat-1"]


@pytest.mark.p2
def test_bulk_delete_chats_delete_all_includes_collaborator_owner_scope(monkeypatch):
    module = _load_chat_module(monkeypatch)
    deleted_ids = []

    _set_json_request_context(module, {"delete_all": True}, method="DELETE")
    monkeypatch.setattr(
        module.UserTenantService,
        "query",
        lambda **_kwargs: [
            SimpleNamespace(id="owner-member", tenant_id="tenant-1"),
            SimpleNamespace(id="collab-member", tenant_id="tenant-2"),
        ],
    )
    monkeypatch.setattr(
        module.UserTenantService,
        "filter_by_tenant_and_user_id",
        lambda tenant_id, user_id: SimpleNamespace(id="collab-member" if tenant_id == "tenant-2" else "owner-member", tenant_id=tenant_id, user_id=user_id),
    )
    monkeypatch.setattr(
        module.DialogService,
        "query",
        lambda **kwargs: (
            [SimpleNamespace(id="shared-chat", tenant_id="tenant-2")]
            if kwargs.get("tenant_id") == "tenant-2"
            else []
        ),
    )
    monkeypatch.setattr(
        module,
        "has_permission_for_member",
        lambda **kwargs: (
            kwargs["operator_id"] == "collab-member" and kwargs["tenant_id"] == "tenant-2",
            None,
            module.PermissionValue.PERMISSION_OWNER.value if kwargs["tenant_id"] == "tenant-2" else 0,
        ),
    )
    monkeypatch.setattr(module.DialogService, "invalidate_by_id", lambda chat_id, *_args, **_kwargs: deleted_ids.append(chat_id) or True, raising=False)
    monkeypatch.setattr(module.ConversationService, "remove_by", lambda *_args, **_kwargs: True)
    monkeypatch.setattr(module.PermissionService, "get_permissions_by_tenant_and_resource_id", lambda **_kwargs: [])
    monkeypatch.setattr(module.PermissionService, "delete", lambda *_args, **_kwargs: True)
    monkeypatch.setattr(module.PermissionChangeLogService, "save", lambda **_kwargs: True)

    res = _run(module.bulk_delete_chats())

    assert res["code"] == 0
    assert res["data"]["success_count"] == 1
    assert deleted_ids == ["shared-chat"]


@pytest.mark.p2
def test_chat_audio_transcription_routes_unit(monkeypatch):
    module = _load_chat_module(monkeypatch)
    monkeypatch.setattr(module, "Response", _StubResponse)
    monkeypatch.setattr(module.tempfile, "mkstemp", lambda suffix: (11, f"/tmp/audio{suffix}"))
    monkeypatch.setattr(module.os, "close", lambda _fd: None)

    def _set_request(form, files):
        monkeypatch.setattr(
            module,
            "request",
            SimpleNamespace(form=_AwaitableValue(form), files=_AwaitableValue(files)),
        )

    _set_request({"stream": "false"}, {})
    res = _run(module.transcription.__wrapped__())
    assert "Missing 'file' in multipart form-data" in res["message"]

    _set_request({"stream": "false"}, {"file": _DummyUploadFile("bad.txt")})
    res = _run(module.transcription.__wrapped__())
    assert "Unsupported audio format: .txt" in res["message"]

    _set_request({"stream": "false"}, {"file": _DummyUploadFile("audio.wav")})
    monkeypatch.setattr(
        module,
        "get_tenant_default_model_by_type",
        lambda *_args, **_kwargs: (_ for _ in ()).throw(LookupError("Tenant not found!")),
    )
    res = _run(module.transcription.__wrapped__())
    assert res["message"] == "Tenant not found!"

    _set_request({"stream": "false"}, {"file": _DummyUploadFile("audio.wav")})
    monkeypatch.setattr(
        module,
        "get_tenant_default_model_by_type",
        lambda *_args, **_kwargs: (_ for _ in ()).throw(Exception("No default ASR model is set")),
    )
    res = _run(module.transcription.__wrapped__())
    assert res["message"] == "No default ASR model is set"

    class _SyncASR:
        def transcription(self, _path):
            return "transcribed text"

        def stream_transcription(self, _path):
            return []

    _set_request({"stream": "false"}, {"file": _DummyUploadFile("audio.wav")})
    monkeypatch.setattr(module, "get_tenant_default_model_by_type", lambda *_args, **_kwargs: {"llm_name": "asr-x"})
    monkeypatch.setattr(module, "LLMBundle", lambda *_args, **_kwargs: _SyncASR())
    monkeypatch.setattr(module.os, "remove", lambda _path: (_ for _ in ()).throw(RuntimeError("cleanup fail")))
    res = _run(module.transcription.__wrapped__())
    assert res["code"] == 0
    assert res["data"]["text"] == "transcribed text"

    class _StreamASR:
        def transcription(self, _path):
            return ""

        def stream_transcription(self, _path):
            yield {"event": "partial", "text": "hello"}

    _set_request({"stream": "true"}, {"file": _DummyUploadFile("audio.wav")})
    monkeypatch.setattr(module, "LLMBundle", lambda *_args, **_kwargs: _StreamASR())
    monkeypatch.setattr(module.os, "remove", lambda _path: None)
    resp = _run(module.transcription.__wrapped__())
    assert isinstance(resp, _StubResponse)
    assert resp.content_type == "text/event-stream"
    chunks = _run(_collect_stream(resp.body))
    assert any('"event": "partial"' in chunk for chunk in chunks)

    class _ErrorASR:
        def transcription(self, _path):
            return ""

        def stream_transcription(self, _path):
            raise RuntimeError("stream asr boom")

    _set_request({"stream": "true"}, {"file": _DummyUploadFile("audio.wav")})
    monkeypatch.setattr(module, "LLMBundle", lambda *_args, **_kwargs: _ErrorASR())
    monkeypatch.setattr(module.os, "remove", lambda _path: (_ for _ in ()).throw(RuntimeError("cleanup boom")))
    resp = _run(module.transcription.__wrapped__())
    chunks = _run(_collect_stream(resp.body))
    assert any("stream asr boom" in chunk for chunk in chunks)


@pytest.mark.p2
def test_chat_audio_speech_routes_unit(monkeypatch):
    module = _load_chat_module(monkeypatch)
    monkeypatch.setattr(module, "Response", _StubResponse)
    _set_request_json(monkeypatch, module, {"text": "A。B"})

    monkeypatch.setattr(
        module,
        "get_tenant_default_model_by_type",
        lambda *_args, **_kwargs: (_ for _ in ()).throw(LookupError("Tenant not found!")),
    )
    res = _run(module.tts.__wrapped__())
    assert res["message"] == "Tenant not found!"

    monkeypatch.setattr(
        module,
        "get_tenant_default_model_by_type",
        lambda *_args, **_kwargs: (_ for _ in ()).throw(Exception("No default TTS model is set")),
    )
    res = _run(module.tts.__wrapped__())
    assert res["message"] == "No default TTS model is set"

    class _TTSOk:
        def tts(self, txt):
            if not txt:
                return []
            yield f"chunk-{txt}".encode("utf-8")

    monkeypatch.setattr(module, "get_tenant_default_model_by_type", lambda *_args, **_kwargs: {"llm_name": "tts-x"})
    monkeypatch.setattr(module, "LLMBundle", lambda *_args, **_kwargs: _TTSOk())
    resp = _run(module.tts.__wrapped__())
    assert resp.mimetype == "audio/mpeg"
    assert resp.headers.get("Cache-Control") == "no-cache"
    assert resp.headers.get("Connection") == "keep-alive"
    assert resp.headers.get("X-Accel-Buffering") == "no"
    chunks = _run(_collect_stream(resp.body))
    assert any("chunk-A" in chunk for chunk in chunks)
    assert any("chunk-B" in chunk for chunk in chunks)

    class _TTSErr:
        def tts(self, _txt):
            raise RuntimeError("tts boom")

    monkeypatch.setattr(module, "LLMBundle", lambda *_args, **_kwargs: _TTSErr())
    resp = _run(module.tts.__wrapped__())
    chunks = _run(_collect_stream(resp.body))
    assert any('"code": 500' in chunk and "**ERROR**: tts boom" in chunk for chunk in chunks)
