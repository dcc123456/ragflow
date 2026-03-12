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

from api.db import ActionEnum, ResourceTypeEnum
from api.db.services.role_service import RoleResourceService
from api.utils.api_utils import get_json_result
from common.constants import RetCode


async def _invoke_view(func, *args, **kwargs):
    if inspect.iscoroutinefunction(func):
        return await func(*args, **kwargs)
    return func(*args, **kwargs)


def check_role_access(action_map, resource_type):
    """
    A generic role-based access decorator based on RoleResource action bits.
    Place it before permission checks to avoid duplicate request body reads.
    """

    def decorator(func):
        original_name = getattr(inspect.unwrap(func), "__name__", func.__name__)
        from api.apps import current_user

        @wraps(func)
        async def wrapper(*args, **kwargs):
            required_action = action_map.get(original_name)
            if required_action is None:
                logging.warning(f"Role action not configured for {original_name}")
                return get_json_result(data=False, message="Role permission not configured.", code=RetCode.SERVER_ERROR)
            if not isinstance(required_action, ActionEnum):
                logging.warning(f"Role action misconfigured for {original_name}: {required_action}")
                return get_json_result(data=False, message="Role permission misconfigured.", code=RetCode.SERVER_ERROR)

            if getattr(current_user, "is_superuser", False):
                print(f"[role-guard] superuser bypass fn={original_name}", flush=True)
                return await _invoke_view(func, *args, **kwargs)

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
            return await _invoke_view(func, *args, **kwargs)

        return wrapper

    return decorator


KB_ROLE_RESOURCE_TYPE = ResourceTypeEnum.DATASET.value
KB_API_ACTION_MAP = {
    "create": ActionEnum.WRITE,
    "update": ActionEnum.WRITE,
    "detail": ActionEnum.READ,
    "list_kbs": ActionEnum.ENABLE,
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
    "run_mindmap": ActionEnum.WRITE,
    "trace_mindmap": ActionEnum.READ,
    "delete_kb_task": ActionEnum.WRITE,
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
}

SEARCH_ROLE_RESOURCE_TYPE = ResourceTypeEnum.SEARCH.value
SEARCH_API_ACTION_MAP = {
    "create": ActionEnum.WRITE,
    "update": ActionEnum.WRITE,
    "detail": ActionEnum.READ,
    "list_search_app": ActionEnum.ENABLE,
    "rm": ActionEnum.WRITE,
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
}
