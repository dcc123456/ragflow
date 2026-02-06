#
#  Copyright 2024 The InfiniFlow Authors. All Rights Reserved.
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

from api.common.exceptions import AdminException, RoleNotFoundError
from api.utils.role_utils import get_permissions_from_action
from api.db import ActionEnum, ResourceTypeEnum, PermissionOperationEnum
from api.db.services.role_service import RoleService, RoleResourceService


def get_role_permissions_by_role_id(role_id: int):
    role_actions = RoleResourceService.get_by_role_id(role_id)
    if not role_actions:
        return {}
    resource_name_map = {
        resource_enum.value: resource_enum.name.lower() for resource_enum in ResourceTypeEnum
    }

    return {
        resource_name_map[role_action["resource_type"]]: get_permissions_from_action(role_action["action"]) for role_action in role_actions
    }


def upsert_role_actions(role_name: str, new_permissions: dict, operation_type: str):
    """

    Args:
        role_name: name of the role
        new_permissions: {
            "dataset": {
                "enable": true,
                "read": true,
                "write": true,
                "share": false
            },
            "file": {
                "enable": true,
                "read": true,
                "write": true
            },
            "agent": {
                "enable": true,
                "read": true,
            }
        }
        operation_type: type of operation, 'grant' or 'revoke'

    Returns:

    """
    # check operation
    if operation_type not in [PermissionOperationEnum.GRANT.value, PermissionOperationEnum.REVOKE.value]:
        raise Exception(f"Invalid operation type '{operation_type}'")
    # check params
    resource_types = [resource_enum.name.lower() for resource_enum in ResourceTypeEnum]
    permission_names = [action_enum.name.lower() for action_enum in ActionEnum]
    if set(new_permissions.keys()) - set(resource_types):
        raise AdminException(f"Unknown resource: {list(set(new_permissions.keys()) - set(resource_types))}")
    operation_permissions = []
    for permission_dict in new_permissions.values():
        operation_permissions.extend(list(permission_dict.keys()))
    if set(operation_permissions) - set(permission_names):
        raise AdminException(f"Unknown permission: {list(set(operation_permissions) - set(permission_names))}")
    # check role
    roles = RoleService.get_by_role_name(role_name)
    if not roles:
        raise RoleNotFoundError(role_name)
    if len(roles) > 1:
        raise AdminException(f"More than one role {role_name} found!")
    # compare & upsert
    role = roles[0]
    role_permissions = RoleResourceService.get_by_role_id(role["id"])
    role_resource_action_map = {r["resource_type"]: r["action"] for r in role_permissions}

    resource_name_type_map = {resource_enum.name.lower(): resource_enum.value for resource_enum in ResourceTypeEnum}
    action_value_map = {action_enum.name.lower(): action_enum.value for action_enum in ActionEnum}
    # calculate new action field
    upsert_dict = {}
    for resource_name, permission_dict in new_permissions.items():
        resource_type = resource_name_type_map[resource_name]
        base_action = role_resource_action_map[
            resource_type] if resource_type in role_resource_action_map.keys() else 0b0000
        new_action = base_action
        for action_name, enable_status in permission_dict.items():
            if operation_type == PermissionOperationEnum.GRANT.value:
                if enable_status:
                    # will grant permissions that are set to 'true'
                    new_action |= action_value_map[action_name]
            else:
                if not enable_status:
                    # will revoke permissions that are set to 'false'
                    new_action &= ~action_value_map[action_name]
        if new_action == base_action:
            continue
        if resource_type == ResourceTypeEnum.MODEL_PROVIDER.value and new_action > ActionEnum.ENABLE.value | ActionEnum.READ.value:
            return {
                "success":  False,
                "message": "Model Provider resource only support 'enable' and 'read' permissions."
            }
        upsert_dict.update({resource_type: new_action})
    if not upsert_dict:
        vt = {
            PermissionOperationEnum.GRANT.value: 'granted',
            PermissionOperationEnum.REVOKE.value: 'revoked',
        }.get(operation_type)
        return {
            "success": True,
            "message": f"Role has already {vt} these permissions."
        }
    try:
        upsert_cnt = RoleResourceService.upsert_role_action_by_id(role["id"], upsert_dict)
        return {
            "success": True,
            "message": f"Role {role_name} updated successfully. {upsert_cnt} rows affected."
        }
    except Exception as e:
        return {
            "success": False,
            "message": str(e)
        }


def delete_role_by_id(role_id):
    _, role = RoleService.get_by_id(role_id)
    if not role:
        return {
            "success": True,
            "message": f"Role {role_id} is already deleted."
        }
    try:
        permission_deleted_cnt = RoleResourceService.delete_by_role_id(role.id)
        role_deleted_cnt = RoleService.delete_by_id(role.id)
        return {
            "success": True,
            "message": f"Role deleted successfully. {permission_deleted_cnt} role permissions and {role_deleted_cnt} role record deleted."
        }
    except Exception as e:
        return {
            "success": False,
            "message": str(e)
        }
