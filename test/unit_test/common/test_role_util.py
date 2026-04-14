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

import importlib
import sys
from types import ModuleType, SimpleNamespace

from api.db import ActionEnum


def test_dialog_action_map_covers_restful_chat_handlers(monkeypatch):
    api_utils_mod = ModuleType("api.utils.api_utils")
    api_utils_mod.get_json_result = lambda data=None, message="", code=0: {
        "code": code,
        "data": data,
        "message": message,
    }
    monkeypatch.setitem(sys.modules, "api.utils.api_utils", api_utils_mod)

    role_service_mod = ModuleType("api.db.services.role_service")
    role_service_mod.RoleResourceService = SimpleNamespace(get_by_role_id=lambda _role_id: [])
    monkeypatch.setitem(sys.modules, "api.db.services.role_service", role_service_mod)

    role_util = importlib.reload(importlib.import_module("common.role_util"))
    dialog_api_action_map = role_util.DIALOG_API_ACTION_MAP

    assert dialog_api_action_map["create"] == ActionEnum.WRITE
    assert dialog_api_action_map["list_chats"] == ActionEnum.ENABLE
    assert dialog_api_action_map["get_chat"] == ActionEnum.READ
    assert dialog_api_action_map["update_chat"] == ActionEnum.WRITE
    assert dialog_api_action_map["patch_chat"] == ActionEnum.WRITE
    assert dialog_api_action_map["delete_chat"] == ActionEnum.WRITE
    assert dialog_api_action_map["bulk_delete_chats"] == ActionEnum.WRITE
    assert dialog_api_action_map["create_session"] == ActionEnum.READ
    assert dialog_api_action_map["list_sessions"] == ActionEnum.READ
    assert dialog_api_action_map["get_session"] == ActionEnum.READ
    assert dialog_api_action_map["delete_sessions"] == ActionEnum.READ


def test_dialog_role_action_map_covers_restful_session_mutations(monkeypatch):
    api_utils_mod = ModuleType("api.utils.api_utils")
    api_utils_mod.get_json_result = lambda data=None, message="", code=0: {
        "code": code,
        "data": data,
        "message": message,
    }
    monkeypatch.setitem(sys.modules, "api.utils.api_utils", api_utils_mod)

    role_service_mod = ModuleType("api.db.services.role_service")
    role_service_mod.RoleResourceService = SimpleNamespace(get_by_role_id=lambda _role_id: [])
    monkeypatch.setitem(sys.modules, "api.db.services.role_service", role_service_mod)

    role_util = importlib.reload(importlib.import_module("common.role_util"))

    expected = {
        "update_session": ActionEnum.READ,
        "delete_session_message": ActionEnum.READ,
        "update_message_feedback": ActionEnum.READ,
        "session_completion": ActionEnum.READ,
    }

    for route_name, action in expected.items():
        assert role_util.DIALOG_API_ACTION_MAP.get(route_name) == action
