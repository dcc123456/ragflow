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
import operator
from datetime import datetime
from functools import reduce

import peewee

from api.db import VALID_RESOURCE_TYPES, PermissionTargetType, PermissionValue, ResourceType
from api.db.db_models import DB, Dialog, Knowledgebase, MCPServer, Memory, Permission, PermissionChangeLog, UserCanvas
from api.db.services.common_service import CommonService
from common.constants import StatusEnum
from common.misc_utils import get_uuid
from common.time_utils import current_timestamp, datetime_format


class PermissionService(CommonService):
    model = Permission

    @classmethod
    def _build_user_target_conditions(cls, user_id, tenant_ids):
        from api.db.services.team_service import DepartmentMemberService, DepartmentService, GroupMemberService
        from api.db.services.user_service import UserTenantService

        tenant_id_set = set(tenant_ids or [])
        if not tenant_id_set:
            return []

        tenant_conditions = []
        user_tenants = UserTenantService.query(user_id=user_id) or []
        for user_tenant in user_tenants:
            tenant_id = getattr(user_tenant, "tenant_id", None)
            if tenant_id not in tenant_id_set:
                continue

            target_conditions = [cls.model.member_id == user_tenant.id]

            groups = GroupMemberService.get_groups_by_member_id(user_tenant.id)
            group_ids = list({group["group_id"] for group in groups})
            if group_ids:
                target_conditions.append(cls.model.group_id.in_(group_ids))

            department_ids = set()
            departments = DepartmentMemberService.get_all_departments_by_member_id(user_tenant.id)
            for department in departments:
                department_ids.update(DepartmentService.get_department_hierarchy(department))
            if department_ids:
                target_conditions.append(cls.model.department_id.in_(list(department_ids)))

            tenant_conditions.append((cls.model.tenant_id == tenant_id) & reduce(operator.or_, target_conditions))

        return tenant_conditions

    @classmethod
    @DB.connection_context()
    def build_user_resource_permission_subquery(
        cls,
        user_id,
        tenant_ids,
        resource_type,
        permission=PermissionValue.PERMISSION_READ,
    ):
        tenant_conditions = cls._build_user_target_conditions(user_id, tenant_ids)
        if not tenant_conditions:
            return (
                cls.model.select(
                    cls.model.resource_id.alias("resource_id"),
                    cls.model.permission.alias("operator_permission"),
                )
                .where(cls.model.id == "__ragflow_no_permission__")
            )

        required_permission = permission.value if isinstance(permission, PermissionValue) else permission

        permission_conditions = (
            (cls.model.status == StatusEnum.VALID.value)
            & (cls.model.resource_type == resource_type)
            & (cls.model.permission >= required_permission)
            & reduce(operator.or_, tenant_conditions)
        )

        return (
            cls.model.select(
                cls.model.tenant_id.alias("tenant_id"),
                cls.model.resource_id.alias("resource_id"),
                peewee.fn.MAX(cls.model.permission).alias("operator_permission"),
            )
            .where(permission_conditions)
            .group_by(cls.model.tenant_id, cls.model.resource_id)
        )

    @classmethod
    @DB.connection_context()
    def get_user_resource_permission_map(cls, user_id, tenant_ids, resource_type, permission=PermissionValue.PERMISSION_READ):
        """
        Return resource_id -> highest permission granted to a user through direct member,
        group, or department permissions across the specified tenants.
        """
        permissions = cls.build_user_resource_permission_subquery(
            user_id,
            tenant_ids,
            resource_type,
            permission,
        ).dicts()

        permission_map = {}
        for permission_record in permissions:
            permission_map[permission_record["resource_id"]] = permission_record["operator_permission"]

        return permission_map

    @classmethod
    @DB.connection_context()
    def filter_by_id(cls, permission_id):
        try:
            permission = cls.model.select().where((cls.model.id == permission_id) & (cls.model.status == StatusEnum.VALID.value)).get()
            return permission
        except peewee.DoesNotExist:
            return None

    @classmethod
    def get_permissions_by_tenant_and_member_id_with_types(cls, tenant_id, member_id, resource_types=VALID_RESOURCE_TYPES):
        """
        ! Use this method under DB.atomic() context
        """
        try:
            permissions = cls.model.select().where(
                (cls.model.tenant_id == tenant_id)
                & (cls.model.member_id == member_id)
                & (cls.model.resource_type.in_(resource_types))
                & (cls.model.status == StatusEnum.VALID.value)
                & (cls.model.permission != PermissionValue.PERMISSION_NULL.value)
            )
            permissions = list(permissions)
            return list(permissions)
        except peewee.DoesNotExist:
            return []

    @classmethod
    def get_permissions_by_tenant_and_department_id_with_types(cls, tenant_id, department_id, resource_types=VALID_RESOURCE_TYPES):
        """
        ! Use this method under DB.atomic() context
        """
        try:
            permissions = cls.model.select().where(
                (cls.model.tenant_id == tenant_id)
                & (cls.model.department_id == department_id)
                & (cls.model.resource_type.in_(resource_types))
                & (cls.model.status == StatusEnum.VALID.value)
                & (cls.model.permission != PermissionValue.PERMISSION_NULL.value)
            )
            return list(permissions)
        except peewee.DoesNotExist:
            return []

    @classmethod
    def get_permissions_by_tenant_and_group_id_with_types(cls, tenant_id, group_id, resource_types=VALID_RESOURCE_TYPES):
        """
        ! Use this method under DB.atomic() context
        """
        try:
            permissions = cls.model.select().where(
                (cls.model.tenant_id == tenant_id)
                & (cls.model.group_id == group_id)
                & (cls.model.resource_type.in_(resource_types))
                & (cls.model.status == StatusEnum.VALID.value)
                & (cls.model.permission != PermissionValue.PERMISSION_NULL.value)
            )
            return list(permissions)
        except peewee.DoesNotExist:
            return []

    @classmethod
    def get_permissions_by_tenant_and_resource_id(cls, tenant_id, resource_id, resource_type=ResourceType.KB):
        """
        ! Use this method under DB.atomic() context
        """
        try:
            permissions = cls.model.select().where(
                (cls.model.tenant_id == tenant_id) & (cls.model.resource_id == resource_id) & (cls.model.resource_type == resource_type) & (cls.model.status == StatusEnum.VALID.value)
                # & (cls.model.permission != PermissionValue.PERMISSION_NULL.value)
            )
            return list(permissions)
        except peewee.DoesNotExist:
            return []

    @classmethod
    @DB.connection_context()
    def get_permissions_by_tenant_and_resource_id_with_info(cls, tenant_id, resource_id, resource_type=ResourceType.KB):
        fields = [cls.model.id, cls.model.member_id, cls.model.group_id, cls.model.department_id, cls.model.tenant_id, cls.model.resource_type, cls.model.resource_id, cls.model.permission]
        try:
            permissions = list(
                cls.model.select(*fields)
                .where(
                    (cls.model.tenant_id == tenant_id)
                    & (cls.model.resource_id == resource_id)
                    & (cls.model.resource_type == resource_type)
                    & (cls.model.status == StatusEnum.VALID.value)
                    & (cls.model.permission != PermissionValue.PERMISSION_NULL.value)
                )
                .dicts()
            )
            return permissions
        except peewee.DoesNotExist:
            return []

    @classmethod
    @DB.connection_context()
    def get_permissions_by_tenant_and_resource_ids_with_info(cls, tenant_id, resource_ids, resource_type=ResourceType.KB):
        if not resource_ids:
            return []

        fields = [cls.model.id, cls.model.member_id, cls.model.group_id, cls.model.department_id, cls.model.tenant_id, cls.model.resource_type, cls.model.resource_id, cls.model.permission]
        try:
            permissions = list(
                cls.model.select(*fields)
                .where(
                    (cls.model.tenant_id == tenant_id)
                    & (cls.model.resource_id.in_(resource_ids))
                    & (cls.model.resource_type == resource_type)
                    & (cls.model.status == StatusEnum.VALID.value)
                    & (cls.model.permission != PermissionValue.PERMISSION_NULL.value)
                )
                .dicts()
            )
            return permissions
        except peewee.DoesNotExist:
            return []

    @classmethod
    @DB.connection_context()
    def get_target_resource_permissions(cls, tenant_id: str, target_type: str, target_id: str) -> list[dict]:
        """
        Returns all non-null permission records for a given target
        (member / group / department), enriched with the resource's display
        name, avatar, and module type.

        Args:
            tenant_id:   Tenant ID.
            target_type: One of ``"member"``, ``"group"``, ``"department"``.
            target_id:   ID of the member (UserTenant.id), group, or department.

        Returns:
            A list of dicts with keys:
            ``resource_id``, ``resource_type``, ``name``, ``avatar``,
            ``permission``, ``module_type``.
        """
        from collections import defaultdict

        # ── 1. fetch raw permission records ───────────────────────────────────
        if target_type == PermissionTargetType.TARGET_MEMBER:
            permissions = cls.get_permissions_by_tenant_and_member_id_with_types(tenant_id, target_id)
        elif target_type == PermissionTargetType.TARGET_GROUP:
            permissions = cls.get_permissions_by_tenant_and_group_id_with_types(tenant_id, target_id)
        elif target_type == PermissionTargetType.TARGET_DEPARTMENT:
            permissions = cls.get_permissions_by_tenant_and_department_id_with_types(tenant_id, target_id)
        else:
            return []

        if not permissions:
            return []

        # ── 2. group resource IDs by type ─────────────────────────────────────
        ids_by_type: dict[str, list[str]] = defaultdict(list)
        for p in permissions:
            ids_by_type[p.resource_type].append(p.resource_id)

        # ── 3. bulk-fetch resource info ───────────────────────────────────────
        resource_info: dict[str, dict] = {}  # resource_id -> {name, avatar}

        if ResourceType.KB in ids_by_type:
            rows = list(Knowledgebase.select(Knowledgebase.id, Knowledgebase.name, Knowledgebase.avatar).where(Knowledgebase.id.in_(ids_by_type[ResourceType.KB])))
            for r in rows:
                resource_info[r.id] = {"name": r.name or "", "avatar": r.avatar or ""}

        if ResourceType.CANVAS in ids_by_type:
            rows = list(UserCanvas.select(UserCanvas.id, UserCanvas.title, UserCanvas.avatar).where(UserCanvas.id.in_(ids_by_type[ResourceType.CANVAS])))
            for r in rows:
                resource_info[r.id] = {"name": r.title or "", "avatar": r.avatar or ""}

        if ResourceType.DIALOG in ids_by_type:
            rows = list(Dialog.select(Dialog.id, Dialog.name, Dialog.icon).where((Dialog.id.in_(ids_by_type[ResourceType.DIALOG])) & (Dialog.status == StatusEnum.VALID.value)))
            for r in rows:
                resource_info[r.id] = {"name": r.name or "", "avatar": r.icon or ""}

        if ResourceType.MCP in ids_by_type:
            rows = list(MCPServer.select(MCPServer.id, MCPServer.name).where(MCPServer.id.in_(ids_by_type[ResourceType.MCP])))
            for r in rows:
                resource_info[r.id] = {"name": r.name or "", "avatar": ""}

        if ResourceType.MEMORY in ids_by_type:
            rows = list(Memory.select(Memory.id, Memory.name, Memory.avatar).where(Memory.id.in_(ids_by_type[ResourceType.MEMORY])))
            for r in rows:
                resource_info[r.id] = {"name": r.name or "", "avatar": r.avatar or ""}

        # LLM: the resource_id IS the factory name – no extra lookup needed
        for factory_name in ids_by_type.get(ResourceType.LLM, []):
            resource_info[factory_name] = {"name": factory_name, "avatar": ""}

        # ── 4. build result ───────────────────────────────────────────────────
        _module_type_map = {
            ResourceType.KB: "Dataset",
            ResourceType.CANVAS: "Agent",
            ResourceType.DIALOG: "Chat",
            ResourceType.MCP: "MCP",
            ResourceType.MEMORY: "Memory",
            ResourceType.LLM: "Model",
        }

        result = []
        for p in permissions:
            info = resource_info.get(p.resource_id, {"name": p.resource_id, "avatar": ""})
            result.append(
                {
                    "resource_id": p.resource_id,
                    "resource_type": p.resource_type,
                    "name": info["name"],
                    "avatar": info["avatar"],
                    "permission": p.permission,
                    "module_type": _module_type_map.get(p.resource_type, p.resource_type),
                }
            )

        return result

    @classmethod
    def save(cls, **kwargs):
        """
        ! Use this method under DB.atomic() context
        """
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()

        kwargs["create_time"] = current_timestamp()
        kwargs["create_date"] = datetime_format(datetime.now())
        kwargs["update_time"] = current_timestamp()
        kwargs["update_date"] = datetime_format(datetime.now())
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def filter_by_group_and_tenant_id(cls, group_id, tenant_id, resource_type=ResourceType.TEAM):
        try:
            permission = (
                cls.model.select()
                .where((cls.model.group_id == group_id) & (cls.model.status == StatusEnum.VALID.value) & (cls.model.tenant_id == tenant_id) & (cls.model.resource_type == resource_type))
                .first()
            )
            return permission
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def filter_by_groups_and_tenant_id(cls, group_ids, tenant_id, resource_type=ResourceType.TEAM):
        if not group_ids:
            return []
        try:
            permissions = cls.model.select().where(
                (cls.model.group_id.in_(group_ids)) & (cls.model.status == StatusEnum.VALID.value) & (cls.model.tenant_id == tenant_id) & (cls.model.resource_type == resource_type)
            )
            return list(permissions)
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def filter_by_group_and_tenant_id_with_resource_id(cls, group_id, tenant_id, resource_id, resource_type=ResourceType.KB):
        try:
            permission = (
                cls.model.select()
                .where(
                    (cls.model.group_id == group_id)
                    & (cls.model.status == StatusEnum.VALID.value)
                    & (cls.model.tenant_id == tenant_id)
                    & (cls.model.resource_id == resource_id)
                    & (cls.model.resource_type == resource_type)
                )
                .first()
            )
            return permission
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def filter_by_groups_and_tenant_id_with_resource_id(cls, group_ids, tenant_id, resource_id, resource_type=ResourceType.KB):
        if not group_ids:
            return []

        try:
            permissions = cls.model.select().where(
                (cls.model.group_id.in_(group_ids))
                & (cls.model.status == StatusEnum.VALID.value)
                & (cls.model.tenant_id == tenant_id)
                & (cls.model.resource_id == resource_id)
                & (cls.model.resource_type == resource_type)
            )
            return list(permissions)
        except peewee.DoesNotExist:
            return []

    @classmethod
    @DB.connection_context()
    def filter_by_member_and_tenant_id(cls, member_id, tenant_id, resource_type=ResourceType.TEAM):
        try:
            permission = (
                cls.model.select()
                .where((cls.model.member_id == member_id) & (cls.model.status == StatusEnum.VALID.value) & (cls.model.tenant_id == tenant_id) & (cls.model.resource_type == resource_type))
                .first()
            )
            return permission
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def filter_by_member_and_tenant_id_with_resource_id(cls, member_id, tenant_id, resource_id, resource_type=ResourceType.KB):
        try:
            permission = (
                cls.model.select()
                .where(
                    (cls.model.member_id == member_id)
                    & (cls.model.status == StatusEnum.VALID.value)
                    & (cls.model.tenant_id == tenant_id)
                    & (cls.model.resource_id == resource_id)
                    & (cls.model.resource_type == resource_type)
                )
                .first()
            )
            return permission
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def filter_by_departments_and_tenant_id(cls, department_ids, tenant_id, resource_type=ResourceType.TEAM):
        if not department_ids:
            return []
        try:
            permissions = cls.model.select().where(
                (cls.model.department_id.in_(department_ids)) & (cls.model.status == StatusEnum.VALID.value) & (cls.model.tenant_id == tenant_id) & (cls.model.resource_type == resource_type)
            )
            return list(permissions)
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def filter_by_department_and_tenant_id(cls, department_id, tenant_id, resource_type=ResourceType.TEAM):
        try:
            permission = (
                cls.model.select()
                .where((cls.model.department_id == department_id) & (cls.model.status == StatusEnum.VALID.value) & (cls.model.tenant_id == tenant_id) & (cls.model.resource_type == resource_type))
                .first()
            )
            return permission
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def filter_by_department_and_tenant_id_with_resource_id(cls, department_id, tenant_id, resource_id, resource_type=ResourceType.KB):
        try:
            permission = (
                cls.model.select()
                .where(
                    (cls.model.department_id == department_id)
                    & (cls.model.status == StatusEnum.VALID.value)
                    & (cls.model.tenant_id == tenant_id)
                    & (cls.model.resource_id == resource_id)
                    & (cls.model.resource_type == resource_type)
                )
                .first()
            )
            return permission
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def filter_by_departments_and_tenant_id_with_resource_id(cls, department_ids, tenant_id, resource_id, resource_type=ResourceType.KB):
        if not department_ids:
            return []

        try:
            permissions = cls.model.select().where(
                (cls.model.department_id.in_(department_ids))
                & (cls.model.status == StatusEnum.VALID.value)
                & (cls.model.tenant_id == tenant_id)
                & (cls.model.resource_id == resource_id)
                & (cls.model.resource_type == resource_type)
            )
            return list(permissions)
        except peewee.DoesNotExist:
            return []

    @classmethod
    def update_many(cls, entity_model_list, batch_size=50):
        """
        ! Use this method under DB.atomic() context
        """
        for entity in entity_model_list:
            entity.update_time = current_timestamp()
            entity.update_date = datetime_format(datetime.now())
            cls.model.bulk_update(entity_model_list, fields=["permission", "status"], batch_size=batch_size)

    @classmethod
    def insert_many(
        cls,
        entity_list,
        batch_size=100,
        fields=["id", "resource_type", "resource_id", "tenant_id", "permission", "member_id", "group_id", "department_id", "create_time", "create_date", "update_time", "update_date"],
    ):
        """
        ! Use this method under DB.atomic() context
        """
        for entity in entity_list:
            if "id" not in entity:
                entity["id"] = get_uuid()

            entity["create_time"] = current_timestamp()
            entity["create_date"] = datetime_format(datetime.now())

        for i in range(0, len(entity_list), batch_size):
            cls.model.insert_many(entity_list[i : i + batch_size], fields=fields).execute()

    @classmethod
    def delete(cls, permission_model_list):
        """
        ! Use this method under DB.atomic() context
        """
        for member in permission_model_list:
            member.update_time = current_timestamp()
            member.update_date = datetime_format(datetime.now())
            member.status = StatusEnum.INVALID.value
            member.permission = PermissionValue.PERMISSION_NULL.value
        return cls.model.bulk_update(permission_model_list, fields=[cls.model.status, cls.model.update_time, cls.model.update_date, cls.model.permission], batch_size=50)


class PermissionChangeLogService(CommonService):
    model = PermissionChangeLog

    @classmethod
    def save(cls, **kwargs):
        """
        ! Use this method under DB.atomic() context
        """
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()

        kwargs["create_time"] = current_timestamp()
        kwargs["create_date"] = datetime_format(datetime.now())
        kwargs["update_time"] = current_timestamp()
        kwargs["update_date"] = datetime_format(datetime.now())
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_permission_change_logs_by_tenant_and_resource_id(cls, tenant_id, resource_id, resource_type=ResourceType.KB):
        try:
            permission_change_logs = cls.model.select().where((cls.model.tenant_id == tenant_id) & (cls.model.resource_id == resource_id) & (cls.model.resource_type == resource_type))
            return list(permission_change_logs)
        except peewee.DoesNotExist:
            return []

    @classmethod
    @DB.connection_context()
    def delete(cls, permission_change_log_model_list):
        for member in permission_change_log_model_list:
            member.update_time = current_timestamp()
            member.update_date = datetime_format(datetime.now())
            member.status = StatusEnum.INVALID.value
        return cls.model.bulk_update(permission_change_log_model_list, fields=[cls.model.update_time, cls.model.update_date], batch_size=50)

    @classmethod
    def insert_many(cls, entity_list, batch_size=100):
        """
        ! Use this method under DB.atomic() context
        """
        for entity in entity_list:
            if "id" not in entity:
                entity["id"] = get_uuid()

            entity["create_time"] = current_timestamp()
            entity["create_date"] = datetime_format(datetime.now())

        for i in range(0, len(entity_list), batch_size):
            cls.model.insert_many(entity_list[i : i + batch_size]).execute()
