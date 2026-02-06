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
import logging

from typing import Dict, Any

from common.constants import ModelType
from api.common.exceptions import AdminException, RoleAlreadyExistsError, RoleNotFoundError, UserNotFoundError
from api.db import PermissionOperationEnum, ResourceTypeEnum, RoleDefaultModelSetUpStatusEnum
from api.db.services.user_service import UserService
from api.db.services.role_service import RoleService
from api.db.services.role_model_service import RoleDefaultModelService
from api.db.joint_services.user_role_service import get_role_permissions_by_role_id, upsert_role_actions, delete_role_by_id


class RoleMgr:
    @staticmethod
    def list_resources():
        return [resource_type.name for resource_type in ResourceTypeEnum]

    @staticmethod
    def create_role(role_name: str, description: str):
        if not role_name:
            raise AdminException("Role name cannot be empty!")
        # Check if the role name is already exist
        if RoleService.get_by_role_name(role_name):
            raise RoleAlreadyExistsError(role_name)
        role_info = {
            "role_name": role_name,
            "description": description
        }
        try:
            if RoleService.create_role(role_info):
                inserted_roles = RoleService.get_by_role_name(role_name)
                inserted_role = inserted_roles[0]
                return {
                    "success": True,
                    "role_info": {
                        "id": inserted_role["id"],
                        "role_name": inserted_role["role_name"],
                        "description": inserted_role["description"]
                    }
                }
            else:
                return {
                    "success": False,
                    "message": "Create role failed."
                }
        except Exception as e:
            logging.error(e)
            return {
                "success": False,
                "message": str(e)
            }

    @staticmethod
    def update_role_description(role_name: str, description: str) -> Dict[str, Any]:
        if not role_name:
            raise AdminException("Role name cannot be empty!")
        # Check if the role exist
        roles = RoleService.get_by_role_name(role_name)
        if not roles:
            raise RoleNotFoundError(role_name)
        if len(roles) > 1:
            raise AdminException(f"More than one role {role_name} found!")
        role = roles[0]
        if description == role["description"]:
            return {
                "success": True,
                "message": "Same description, no need to update!"
            }
        RoleService.update_role_description(role["id"], description)
        return {
            "success": True,
            "message": "Description updated successfully!"
        }

    @staticmethod
    def delete_role(role_name: str) -> Dict[str, Any]:
        roles = RoleService.get_by_role_name(role_name)
        if not roles:
            raise RoleNotFoundError(role_name)
        if len(roles) > 1:
            raise AdminException(f"More than one role {role_name} found!")
        role = roles[0]
        user_in_use = UserService.query(role_id=role["id"])
        if user_in_use:
            user_noun = 'user' if len(user_in_use) == 1 else 'users'
            raise AdminException(f"Role {role_name} is in use, {len(user_in_use)} {user_noun} are {role_name}, cannot delete it!")
        return delete_role_by_id(role["id"])

    @staticmethod
    def list_roles() -> Dict[str, Any]:
        roles = RoleService.get_all_roles()
        return {
            "roles": [{
                "id": role["id"],
                "role_name": role["role_name"],
                "description": role["description"],
                "create_date": role["create_date"],
                "update_date": role["update_date"]
            } for role in roles],
            "total": len(roles)
        }

    @staticmethod
    def list_roles_with_permission() -> Dict[str, Any]:
        roles = RoleService.get_all_roles()
        return {
            "roles": [{
                "id": role["id"],
                "role_name": role["role_name"],
                "description": role["description"],
                "create_date": role["create_date"],
                "update_date": role["update_date"],
                "permissions": get_role_permissions_by_role_id(role["id"])
            } for role in roles],
            "total": len(roles)
        }

    @staticmethod
    def get_role_permission(role_name: str) -> Dict[str, Any]:
        roles = RoleService.get_by_role_name(role_name)
        if not roles:
            raise RoleNotFoundError(role_name)
        if len(roles) > 1:
            raise AdminException(f"More than one role {role_name} found!")
        role = roles[0]
        permissions = get_role_permissions_by_role_id(role["id"])
        return {
            "role": {
                "id": role["id"],
                "role_name": role["role_name"],
                "description": role["description"],
            },
            "permissions": permissions
        }

    @staticmethod
    def grant_role_permission(role_name: str, new_permissions: dict) -> Dict[str, Any]:
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
            } will grant permissions that are set to 'true'

        Returns:

        """
        return upsert_role_actions(role_name, new_permissions, PermissionOperationEnum.GRANT.value)

    @staticmethod
    def revoke_role_permission(role_name: str, revoke_permissions: dict) -> Dict[str, Any]:
        """

        Args:
            role_name: name of the role
            revoke_permissions: {
                "dataset": {
                    "enable": true,
                    "read": true,
                    "write": false,
                    "share": false
                },
                "file": {
                    "enable": true,
                    "read": true,
                    "write": false
                },
                "agent": {
                    "enable": true,
                    "read": true,
                }
            } will revoke permissions that are set to 'false'

        Returns:

        """
        return upsert_role_actions(role_name, revoke_permissions, PermissionOperationEnum.REVOKE.value)

    @staticmethod
    def update_user_role(user_name: str, role_name: str) -> Dict[str, Any]:
        # check user
        users = UserService.query_user_by_email(user_name)
        if not users:
            raise UserNotFoundError(user_name)
        if len(users) > 1:
            raise AdminException(f"More than one user {user_name} found!")
        user = users[0]

        roles = RoleService.get_by_role_name(role_name)
        if not roles:
            raise RoleNotFoundError(role_name)
        if len(roles) > 1:
            raise AdminException(f"More than one role {role_name} found!")
        role = roles[0]
        if user.role_id == role["id"]:
            return {
                "success": True,
                "message": f"User {user_name} has already updated to role {role_name}."
            }

        try:
            UserService.update_user(user.id, {"role_id": role["id"]})
            return {
                "success": True,
                "message": "User updated successfully!"
            }
        except Exception as e:
            return {
                "success": False,
                "message": str(e)
            }

    @staticmethod
    def get_user_permission(user_name: str) -> Dict[str, Any]:
        # check user
        users = UserService.query_user_by_email(user_name)
        if not users:
            raise UserNotFoundError(user_name)
        if len(users) > 1:
            raise AdminException(f"More than one user {user_name} found!")
        user = users[0]
        _, role = RoleService.get_by_id(user.role_id)
        if not role:
            raise RoleNotFoundError(user.role_id)
        permissions = get_role_permissions_by_role_id(user.role_id)
        return {
            "user": {
                "id": user.id,
                "username": user.email,
                "role": role.role_name,
                "description": role.description,
            },
            "role_permissions": permissions
        }


class RoleModelMgr:

    @staticmethod
    def get_role_default_models(role_name: str) -> dict:
        roles = RoleService.get_by_role_name(role_name)
        if not roles:
            raise RoleNotFoundError(role_name)
        if len(roles) > 1:
            raise AdminException(f"More than one role {role_name} found!")
        role = roles[0]
        role_default_models = RoleDefaultModelService.get_by_role_id(role["id"])
        if not role_default_models:
            return {
                "model_list": [],
                "setup_status": RoleDefaultModelSetUpStatusEnum.NOT_SET
            }

        setup_model_types = {m.model_type for m in role_default_models if m.model_id}
        not_setup_types = {mt.value for mt in ModelType} - setup_model_types
        setup_status = RoleDefaultModelSetUpStatusEnum.COMPLETE if not not_setup_types else RoleDefaultModelSetUpStatusEnum.PARTIAL
        return {
            "model_list": [{
                "role_id": m.role_id,
                "model_type": m.model_type,
                "model_id": m.model_id,
                "tenant_id": m.tenant_id
            } for m in role_default_models],
            "setup_status": setup_status
        }

    @staticmethod
    def set_role_default_model(role_name: str, model_type: str, model_id: str, tenant_id: str):
        roles = RoleService.get_by_role_name(role_name)
        if not roles:
            raise RoleNotFoundError(role_name)
        if len(roles) > 1:
            raise AdminException(f"More than one role {role_name} found!")
        role = roles[0]
        role_default_model_config = RoleDefaultModelService.get_by_role_id_and_model_type(role["id"], model_type)
        if role_default_model_config:
            RoleDefaultModelService.update_role_default_model_by_type(role["id"], model_type, model_id, tenant_id)
        else:
            RoleDefaultModelService.add_role_default_model(role["id"], model_type, model_id, tenant_id)

    @staticmethod
    def delete_role_default_model(role_name: str):
        roles = RoleService.get_by_role_name(role_name)
        if not roles:
            raise RoleNotFoundError(role_name)
        if len(roles) > 1:
            raise AdminException(f"More than one role {role_name} found!")
        role = roles[0]
        return RoleDefaultModelService.delete_by_role_id(role["id"])
