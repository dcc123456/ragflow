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
import inspect
import logging
from functools import wraps
from enum import Enum

try:
    from api.db import ActionEnum, ResourceTypeEnum
except ImportError:
    class ActionEnum(Enum):
        ENABLE = 0b0001
        READ = 0b0010
        WRITE = 0b0100
        SHARE = 0b1000

    class ResourceTypeEnum(Enum):
        DATASET = 1
        CHAT = 2
        AGENT = 3
        SEARCH = 4
        FILE = 5
        TEAM = 6
        MEMORY = 7
        MODEL_PROVIDER = 8
from api.db.services.role_service import RoleResourceService
from api.utils.api_utils import get_json_result
from common.constants import RetCode


async def _invoke_view(func, *args, **kwargs):
    if inspect.iscoroutinefunction(func):
        return await func(*args, **kwargs)
    return func(*args, **kwargs)


def _check_role_permission(func, action_map, resource_type, *args, **kwargs):
    original_name = getattr(inspect.unwrap(func), "__name__", func.__name__)
    from api.apps import QuartAuthUnauthorized, current_user

    required_action = action_map.get(original_name)
    if required_action is None:
        logging.warning(f"Role action not configured for {original_name}")
        return get_json_result(data=False, message="Role permission not configured.", code=RetCode.SERVER_ERROR)
    if not isinstance(required_action, ActionEnum):
        logging.warning(f"Role action misconfigured for {original_name}: {required_action}")
        return get_json_result(data=False, message="Role permission misconfigured.", code=RetCode.SERVER_ERROR)

    if not current_user:
        raise QuartAuthUnauthorized()

    if getattr(current_user, "is_superuser", False):
        print(f"[role-guard] superuser bypass fn={original_name}", flush=True)
        return None

    role_id = getattr(current_user, "role_id", None)
    if role_id is None:
        return get_json_result(data=False, message="User role not found.", code=RetCode.AUTHENTICATION_ERROR)

    role_permissions = RoleResourceService.get_by_role_id(role_id) or []
    action_value = 0
    for role_permission in role_permissions:
        if role_permission.get("resource_type") == resource_type:
            action_value = role_permission.get("action", 0)
            break

    print(f"[role-guard] fn={original_name} role_id={role_id} resource={resource_type} action_value={action_value}", flush=True)
    if not (action_value & ActionEnum.ENABLE.value):
        print(f"[role-guard] feature disabled fn={original_name}", flush=True)
        return get_json_result(data=False, message="Feature is not enabled for this role.", code=RetCode.FEATURE_NOT_ENABLED)

    if not action_value or not (action_value & required_action.value):
        print(f"[role-guard] deny fn={original_name} need={required_action.name}", flush=True)
        return get_json_result(data=False, message="Role has no permission for this operation.", code=RetCode.AUTHENTICATION_ERROR)

    print(f"[role-guard] allow fn={original_name} need={required_action.name}", flush=True)
    return None


def check_role_access(action_map, resource_type):
    """
    A generic role-based access decorator based on RoleResource action bits.
    Place it before permission checks to avoid duplicate request body reads.
    """

    def decorator(func):
        if inspect.iscoroutinefunction(func):
            @wraps(func)
            async def async_wrapper(*args, **kwargs):
                permission_result = _check_role_permission(func, action_map, resource_type, *args, **kwargs)
                if permission_result is not None:
                    return permission_result
                return await _invoke_view(func, *args, **kwargs)

            return async_wrapper

        @wraps(func)
        def sync_wrapper(*args, **kwargs):
            permission_result = _check_role_permission(func, action_map, resource_type, *args, **kwargs)
            if permission_result is not None:
                return permission_result
            return func(*args, **kwargs)

        return sync_wrapper

    return decorator


KB_ROLE_RESOURCE_TYPE = ResourceTypeEnum.DATASET.value
KB_API_ACTION_MAP = {
    "aggregate_tags": ActionEnum.READ,
    "create": ActionEnum.WRITE,
    "delete": ActionEnum.WRITE,
    "update": ActionEnum.WRITE,
    "detail": ActionEnum.READ,
    "list_kbs": ActionEnum.ENABLE,
    "list_datasets": ActionEnum.ENABLE,
    "rm": ActionEnum.WRITE,
    "list_tags": ActionEnum.READ,
    "list_tags_from_kbs": ActionEnum.READ,
    "rm_tags": ActionEnum.WRITE,
    "rename_tags": ActionEnum.WRITE,
    "knowledge_graph": ActionEnum.READ,
    "delete_knowledge_graph": ActionEnum.WRITE,
    "get_meta": ActionEnum.READ,
    "get_basic_info": ActionEnum.READ,
    "list_pipeline_logs": ActionEnum.READ,
    "list_pipeline_dataset_logs": ActionEnum.READ,
    "delete_pipeline_logs": ActionEnum.WRITE,
    "pipeline_log_detail": ActionEnum.READ,
    "run_graphrag": ActionEnum.WRITE,
    "trace_graphrag": ActionEnum.READ,
    "run_raptor": ActionEnum.WRITE,
    "trace_raptor": ActionEnum.READ,
    "run_index": ActionEnum.WRITE,
    "trace_index": ActionEnum.READ,
    "delete_index": ActionEnum.WRITE,
    "list_ingestion_logs": ActionEnum.READ,
    "get_ingestion_log": ActionEnum.READ,
    "get_auto_metadata": ActionEnum.READ,
    "update_auto_metadata": ActionEnum.WRITE,
    "get_knowledge_graph": ActionEnum.READ,
    "run_mindmap": ActionEnum.WRITE,
    "trace_mindmap": ActionEnum.READ,
    "delete_kb_task": ActionEnum.WRITE,
    "embedding": ActionEnum.WRITE,
    "check_embedding": ActionEnum.WRITE,
    "switch_embedding": ActionEnum.WRITE,
    "clone": ActionEnum.WRITE,
}

DIALOG_ROLE_RESOURCE_TYPE = ResourceTypeEnum.CHAT.value
DIALOG_API_ACTION_MAP = {
    "set_dialog": ActionEnum.WRITE,
    "get": ActionEnum.READ,
    "list_dialogs": ActionEnum.ENABLE,
    "list_dialogs_next": ActionEnum.ENABLE,
    "rm": ActionEnum.WRITE,
    "create": ActionEnum.WRITE,
    "list_chats": ActionEnum.ENABLE,
    "get_chat": ActionEnum.READ,
    "update_chat": ActionEnum.WRITE,
    "patch_chat": ActionEnum.WRITE,
    "delete_chat": ActionEnum.WRITE,
    "bulk_delete_chats": ActionEnum.WRITE,
    "create_session": ActionEnum.READ,
    "list_sessions": ActionEnum.READ,
    "get_session": ActionEnum.READ,
    "update_session": ActionEnum.READ,
    "delete_sessions": ActionEnum.READ,
    "delete_session_message": ActionEnum.READ,
    "update_message_feedback": ActionEnum.READ,
    "session_completion": ActionEnum.READ,
}

CANVAS_ROLE_RESOURCE_TYPE = ResourceTypeEnum.AGENT.value
CANVAS_API_ACTION_MAP = {
    "templates": ActionEnum.READ,
    "rm": ActionEnum.WRITE,
    "save": ActionEnum.WRITE,
    "get": ActionEnum.READ,
    "getsse": ActionEnum.READ,
    "run": ActionEnum.WRITE,
    "rerun": ActionEnum.WRITE,
    "cancel": ActionEnum.WRITE,
    "reset": ActionEnum.WRITE,
    "upload": ActionEnum.WRITE,
    "input_form": ActionEnum.READ,
    "debug": ActionEnum.WRITE,
    "test_db_connect": ActionEnum.READ,
    "getlistversion": ActionEnum.READ,
    "getversion": ActionEnum.READ,
    "list_canvas": ActionEnum.ENABLE,
    "setting": ActionEnum.WRITE,
    "trace": ActionEnum.READ,
    "presence_join": ActionEnum.WRITE,
    "presence_heartbeat": ActionEnum.WRITE,
    "presence_leave": ActionEnum.WRITE,
    "presence_list": ActionEnum.READ,
    "sessions": ActionEnum.READ,
    "prompts": ActionEnum.READ,
    "download": ActionEnum.READ,
    "list_agent_sessions": ActionEnum.READ,
    "create_agent_session": ActionEnum.READ,
    "get_agent_session": ActionEnum.READ,
    "delete_agent_session_item": ActionEnum.WRITE,
    "delete_agent_session": ActionEnum.WRITE,
    "download_agent_file": ActionEnum.READ,
    "list_agent_template": ActionEnum.READ,
    "list_agents": ActionEnum.ENABLE,
    "list_agent_tags": ActionEnum.READ,
    "update_agent_tags": ActionEnum.WRITE,
    "create_agent": ActionEnum.WRITE,
    "upload_agent_file": ActionEnum.WRITE,
    "get_agent_component_input_form": ActionEnum.READ,
    "debug_agent_component": ActionEnum.WRITE,
    "get_agent": ActionEnum.READ,
    "list_agent_versions": ActionEnum.READ,
    "get_agent_version": ActionEnum.READ,
    "get_agent_logs": ActionEnum.READ,
    "delete_agent": ActionEnum.WRITE,
    "update_agent": ActionEnum.WRITE,
    "reset_agent": ActionEnum.WRITE,
    "rerun_agent": ActionEnum.WRITE,
    "test_db_connection": ActionEnum.READ,
    "agent_chat_completion": ActionEnum.WRITE,
    "webhook_trace": ActionEnum.READ,
    "download_attachment": ActionEnum.READ,
}

SEARCH_ROLE_RESOURCE_TYPE = ResourceTypeEnum.SEARCH.value
SEARCH_API_ACTION_MAP = {
    "create": ActionEnum.WRITE,
    "update": ActionEnum.WRITE,
    "detail": ActionEnum.READ,
    "list_searches": ActionEnum.ENABLE,
    "delete_search": ActionEnum.WRITE,
}

FILE_ROLE_RESOURCE_TYPE = ResourceTypeEnum.FILE.value
FILE_API_ACTION_MAP = {
    "upload": ActionEnum.WRITE,
    "create": ActionEnum.WRITE,
    "list_files": ActionEnum.ENABLE,
    "get_root_folder": ActionEnum.READ,
    "get_parent_folder": ActionEnum.READ,
    "get_all_parent_folders": ActionEnum.READ,
    "rm": ActionEnum.WRITE,
    "rename": ActionEnum.WRITE,
    "get": ActionEnum.READ,
    "move": ActionEnum.WRITE,
    "link": ActionEnum.WRITE,
    "create_or_upload": ActionEnum.WRITE,
    "convert": ActionEnum.WRITE,
    "delete": ActionEnum.WRITE,
    "download": ActionEnum.READ,
    "parent_folder": ActionEnum.READ,
    "ancestors": ActionEnum.READ,
}

MEMORY_ROLE_RESOURCE_TYPE = ResourceTypeEnum.MEMORY.value
MEMORY_API_ACTION_MAP = {
    "create_memory": ActionEnum.WRITE,
    "update_memory": ActionEnum.WRITE,
    "delete_memory": ActionEnum.WRITE,
    "list_memory": ActionEnum.ENABLE,
    "get_memory_config": ActionEnum.READ,
    "get_memory_messages": ActionEnum.READ,
    "add_message": ActionEnum.WRITE,
    "forget_message": ActionEnum.WRITE,
    "update_message": ActionEnum.WRITE,
    "search_message": ActionEnum.READ,
    "get_messages": ActionEnum.READ,
    "get_message_content": ActionEnum.READ,
}

TEAM_ROLE_RESOURCE_TYPE = ResourceTypeEnum.TEAM.value
TEAM_API_ACTION_MAP = {
    "group_list": ActionEnum.ENABLE,
    "create_group": ActionEnum.WRITE,
    "delete_group": ActionEnum.WRITE,
    "group_change_owner": ActionEnum.WRITE,
    "update_group": ActionEnum.WRITE,
    "list_group_member": ActionEnum.READ,
    "add_group_member": ActionEnum.WRITE,
    "remove_group_member": ActionEnum.WRITE,
    "department_list": ActionEnum.ENABLE,
    "create_department": ActionEnum.WRITE,
    "delete_department": ActionEnum.WRITE,
    "move_department": ActionEnum.WRITE,
    "update_department": ActionEnum.WRITE,
    "list_department_member": ActionEnum.READ,
    "add_department_member": ActionEnum.WRITE,
    "remove_department_member": ActionEnum.WRITE,
    "user_list": ActionEnum.READ,
    "create": ActionEnum.WRITE,
    "rm": ActionEnum.WRITE,
    "tenant_list": ActionEnum.ENABLE,
    "agree": ActionEnum.WRITE,
}
