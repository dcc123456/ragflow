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
from datetime import datetime

import peewee

from api.db import VALID_RESOURCE_TYPES, PermissionValue, ResourceType
from api.db.db_models import DB, Permission, PermissionChangeLog
from api.db.services.common_service import CommonService
from common.misc_utils import get_uuid
from common.time_utils import current_timestamp, datetime_format
from common.constants import StatusEnum


class PermissionService(CommonService):
    model = Permission

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
                .where((cls.model.member_id == department_id) & (cls.model.status == StatusEnum.VALID.value) & (cls.model.tenant_id == tenant_id) & (cls.model.resource_type == resource_type))
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
