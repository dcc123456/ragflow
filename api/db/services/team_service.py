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

from api.db import StatusEnum
from api.db.db_models import DB, Department, DepartmentMember, Group, GroupMember, User, UserTenant
from api.db.services.common_service import CommonService
from api.utils import get_uuid
from common.time_utils import current_timestamp, datetime_format


class GroupService(CommonService):
    model = Group

    @classmethod
    @DB.connection_context()
    def filter_by_id(cls, group_id):
        try:
            group = cls.model.select().where((cls.model.id == group_id) & (cls.model.status == StatusEnum.VALID.value)).get()
            return group
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def query_group(cls, tenant_id, group_name):
        group = cls.model.select().where((cls.model.tenant_id == tenant_id), ((cls.model.name == group_name) & (cls.model.status == StatusEnum.VALID.value))).first()
        return group

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
    def delete_group(cls, group_id):
        """
        ! Use this method under DB.atomic() context
        """
        with DB.atomic():
            cls.model.update({"status": StatusEnum.INVALID.value, "update_time": current_timestamp(), "update_date": datetime_format(datetime.now())}).where(
                (cls.model.id == group_id) & (cls.model.status == StatusEnum.VALID.value)
            ).execute()

    @classmethod
    def update_group(cls, group_id, group_dict):
        """
        ! Use this method under DB.atomic() context
        """
        if group_dict:
            group_dict["update_time"] = current_timestamp()
            group_dict["update_date"] = datetime_format(datetime.now())
            cls.model.update(group_dict).where(cls.model.id == group_id).execute()

    @classmethod
    def update_group_model(cls, group_model):
        """
        ! Use this method under DB.atomic() context
        """
        with DB.atomic():
            if group_model:
                group_model.update_time = current_timestamp()
                group_model.update_date = datetime_format(datetime.now())
                cls.model.save(group_model)

    @classmethod
    @DB.connection_context()
    def get_groups_by_tenant_id(cls, tenant_id):
        fields = [
            cls.model.id.alias("group_id"),
            cls.model.name,
            cls.model.avatar,
            cls.model.owner_id,
            cls.model.tenant_id,
            cls.model.update_date,
            User.nickname.alias("owner_name"),
        ]
        group_list = list(
            cls.model.select(*fields)
            .join(UserTenant, on=((UserTenant.id == Group.owner_id) & (UserTenant.status == StatusEnum.VALID.value)))
            .join(User, on=((User.id == UserTenant.user_id) & (User.status == StatusEnum.VALID.value)))
            .where((cls.model.tenant_id == tenant_id) & (cls.model.status == StatusEnum.VALID.value))
            .order_by(Group.update_date.desc())
            .dicts()
        )
        return group_list


class GroupMemberService(CommonService):
    model = GroupMember

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
        obj = cls.model.create(**kwargs)
        return obj

    @classmethod
    def update_many(cls, member_model_list, allow_to_update, batch_size=50):
        """
        ! Use this method under DB.atomic() context
        """
        for member in member_model_list:
            member.update_time = current_timestamp()
            member.update_date = datetime_format(datetime.now())

        update_fields = set(allow_to_update)
        update_fields.update(["update_time", "update_date"])

        cls.model.bulk_update(member_model_list, fields=list(update_fields), batch_size=batch_size)

    @classmethod
    @DB.connection_context()
    def filter_by_group_and_member_id(cls, group_id, member_id):
        try:
            member = cls.model.select().where((cls.model.group_id == group_id) & (cls.model.status == StatusEnum.VALID.value) & (cls.model.member_id == member_id)).first()
            return member
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def filter_by_group_and_member_id_ignore_validity(cls, group_id, member_id):
        try:
            member = cls.model.select().where((cls.model.group_id == group_id) & (cls.model.member_id == member_id)).first()
            return member
        except peewee.DoesNotExist:
            return None

    @classmethod
    def insert_many(cls, member_list, batch_size=100):
        """
        ! Use this method under DB.atomic() context
        """
        for member in member_list:
            if "id" not in member:
                member["id"] = get_uuid()

            member["create_time"] = current_timestamp()
            member["create_date"] = datetime_format(datetime.now())

        with DB.atomic():
            for i in range(0, len(member_list), batch_size):
                cls.model.insert_many(member_list[i : i + batch_size]).execute()

    @classmethod
    @DB.connection_context()
    def filter_by_group_and_member_ids(cls, group_id, member_ids):
        if not member_ids:
            return None

        members = list(cls.model.select().where((cls.model.group_id == group_id) & (cls.model.status == StatusEnum.VALID.value) & (cls.model.member_id.in_(member_ids))))
        return members if members else None

    @classmethod
    @DB.connection_context()
    def get_by_id(cls, id):
        fields = [
            cls.model.member_id,
            cls.model.role,
            User.nickname,
            User.email,
            User.avatar,
            User.is_authenticated,
            User.is_active,
            User.is_anonymous,
            User.status,
            User.update_date,
            User.is_superuser,
        ]
        return (
            cls.model.select(*fields)
            .join(UserTenant, on=((cls.model.member_id == UserTenant.id) & (UserTenant.status == StatusEnum.VALID.value)))
            .join(User, on=(User.id == UserTenant.user_id))
            .where((cls.model.id == id) & (cls.model.status == StatusEnum.VALID.value))
            .dicts()
            .first()
        )

    @classmethod
    @DB.connection_context()
    def get_by_group_id_with_info(cls, group_id):
        fields = [
            cls.model.member_id,
            cls.model.role,
            User.id.alias("user_id"),
            User.nickname,
            User.email,
            User.avatar,
            User.is_authenticated,
            User.is_active,
            User.is_anonymous,
            User.status,
            User.update_date,
            User.is_superuser,
        ]
        return list(
            cls.model.select(*fields)
            .join(UserTenant, on=((cls.model.member_id == UserTenant.id) & (UserTenant.status == StatusEnum.VALID.value)))
            .join(User, on=(User.id == UserTenant.user_id))
            .join(Group, on=((Group.id == cls.model.group_id) & (Group.status == StatusEnum.VALID.value)))
            .where((cls.model.group_id == group_id) & (cls.model.status == StatusEnum.VALID.value))
            .dicts()
        )

    @classmethod
    @DB.connection_context()
    def get_by_group_id(cls, group_id):
        return list(
            cls.model.select()
            .join(UserTenant, on=((cls.model.member_id == UserTenant.id) & (UserTenant.status == StatusEnum.VALID.value)))
            .join(User, on=(User.id == UserTenant.user_id))
            .join(Group, on=((Group.id == cls.model.group_id) & (Group.status == StatusEnum.VALID.value)))
            .where((cls.model.group_id == group_id) & (cls.model.status == StatusEnum.VALID.value))
        )

    @classmethod
    @DB.connection_context()
    def get_groups_by_member_id(cls, member_id):
        fields = [
            cls.model.group_id,
            Group.name,
            Group.avatar,
            Group.owner_id,
            Group.tenant_id,
            Group.update_date,
            User.nickname.alias("owner_name"),
        ]
        group_list = list(
            cls.model.select(*fields)
            .join(Group, on=((cls.model.group_id == Group.id) & (Group.status == StatusEnum.VALID.value)))
            .join(UserTenant, on=((UserTenant.id == Group.owner_id) & (UserTenant.status == StatusEnum.VALID.value)))
            .join(User, on=((User.id == UserTenant.user_id) & (User.status == StatusEnum.VALID.value)))
            .where((cls.model.member_id == member_id) & (cls.model.status == StatusEnum.VALID.value))
            .order_by(Group.update_date.desc())
            .dicts()
        )
        for group in group_list:
            group["members"] = cls.get_by_group_id_with_info(group["group_id"])

        return group_list

    @classmethod
    def delete(cls, member_model_list):
        """
        ! Use this method under DB.atomic() context
        """
        for member in member_model_list:
            member.update_time = current_timestamp()
            member.update_date = datetime_format(datetime.now())
            member.status = StatusEnum.INVALID.value

        cls.model.bulk_update(member_model_list, fields=[cls.model.status, cls.model.update_time, cls.model.update_date], batch_size=50)


class DepartmentService(CommonService):
    model = Department

    @classmethod
    @DB.connection_context()
    def filter_by_id(cls, department_id):
        try:
            department = cls.model.select().where((cls.model.id == department_id) & (cls.model.status == StatusEnum.VALID.value)).get()
            return department
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def query_department(cls, tenant_id, name):
        department = cls.model.select().where((cls.model.tenant_id == tenant_id), ((cls.model.name == name) & (cls.model.status == StatusEnum.VALID.value))).first()
        return department

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
    def insert_many(cls, department_list, batch_size=50):
        """
        ! Use this method under DB.atomic() context
        """
        for department in department_list:
            if "id" not in department:
                department["id"] = get_uuid()

            department["create_time"] = current_timestamp()
            department["create_date"] = datetime_format(datetime.now())

        with DB.atomic():
            for i in range(0, len(department_list), batch_size):
                cls.model.insert_many(department_list[i : i + batch_size]).execute()

    @classmethod
    def delete_department(cls, department_id):
        """
        ! Use this method under DB.atomic() context
        """
        cls.model.update({"status": StatusEnum.INVALID.value, "update_time": current_timestamp(), "update_date": datetime_format(datetime.now())}).where(
            (cls.model.id == department_id) & (cls.model.status == StatusEnum.VALID.value)
        ).execute()

    @classmethod
    def delete_departments(cls, department_ids):
        """
        ! Use this method under DB.atomic() context
        """
        cls.model.update({"status": StatusEnum.INVALID.value, "update_time": current_timestamp(), "update_date": datetime_format(datetime.now())}).where(
            (cls.model.id.in_(department_ids)) & (cls.model.status == StatusEnum.VALID.value)
        ).execute()

    @classmethod
    def update_department(cls, department_id, department_dict):
        """
        ! Use this method under DB.atomic() context
        """
        with DB.atomic():
            if department_dict:
                department_dict["update_time"] = current_timestamp()
                department_dict["update_date"] = datetime_format(datetime.now())
                cls.model.update(department_dict).where(cls.model.id == department_id).execute()

    @classmethod
    def update_department_model(cls, department_model):
        """
        ! Use this method under DB.atomic() context
        """
        with DB.atomic():
            if department_model:
                department_model.update_time = current_timestamp()
                department_model.update_date = datetime_format(datetime.now())
                cls.model.save(department_model)

    @classmethod
    @DB.connection_context()
    def get_department_hierarchy(cls, department):
        hierarchy_ids = [dept_id for dept_id in department["path"].strip("/").split("/")]
        return hierarchy_ids

    @classmethod
    @DB.connection_context()
    def get_department_depth(cls, department_id):
        department = cls.filter_by_id(department_id)
        if not department or not department.path:
            return 0

        return len(department.path.strip("/").split("/"))

    @classmethod
    @DB.connection_context()
    def get_subdepartments_by_tenant_id(cls, tenant_id, parent_id, fetch_all=False):
        fields = [
            cls.model.id.alias("department_id"),
            cls.model.name,
            cls.model.path,
            cls.model.avatar,
            cls.model.owner_id,
            cls.model.tenant_id,
            cls.model.parent_id,
            cls.model.update_date,
            cls.model.description,
            cls.model.formatted_path,
        ]

        if fetch_all:
            query = cls.model.select(*fields).where((cls.model.tenant_id == tenant_id) & (cls.model.status == StatusEnum.VALID.value))
        else:
            if not parent_id:
                query = cls.model.select(*fields).where((cls.model.tenant_id == tenant_id) & (cls.model.id == cls.model.parent_id) & (cls.model.status == StatusEnum.VALID.value))
            else:
                query = cls.model.select(*fields).where(
                    (cls.model.tenant_id == tenant_id) & (cls.model.id != parent_id) & (cls.model.parent_id == parent_id) & (cls.model.status == StatusEnum.VALID.value)
                )

        query = query.order_by(Department.create_date.desc())

        return list(query.dicts())


class DepartmentMemberService(CommonService):
    model = DepartmentMember

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
    def insert_many(cls, member_list, batch_size=100):
        """
        ! Use this method under DB.atomic() context
        """
        for member in member_list:
            if "id" not in member:
                member["id"] = get_uuid()

            member["create_time"] = current_timestamp()
            member["create_date"] = datetime_format(datetime.now())

        with DB.atomic():
            for i in range(0, len(member_list), batch_size):
                cls.model.insert_many(member_list[i : i + batch_size]).execute()

    @classmethod
    def update_many(cls, member_model_list, allow_to_update, batch_size=50):
        """
        ! Use this method under DB.atomic() context
        """
        with DB.atomic():
            for member in member_model_list:
                member.update_time = current_timestamp()
                member.update_date = datetime_format(datetime.now())

            update_fields = set(allow_to_update)
            update_fields.update(["update_time", "update_date"])

            cls.model.bulk_update(member_model_list, fields=list(update_fields), batch_size=batch_size)

    @classmethod
    @DB.connection_context()
    def filter_by_department_and_member_id(cls, department_id, member_id):
        try:
            member = cls.model.select().where((cls.model.department_id == department_id) & (cls.model.status == StatusEnum.VALID.value) & (cls.model.member_id == member_id)).first()
            return member
        except peewee.DoesNotExist:
            return None

    @classmethod
    @DB.connection_context()
    def get_by_id(cls, id):
        fields = [
            cls.model.member_id,
            cls.model.role,
            User.nickname,
            User.email,
            User.avatar,
            User.is_authenticated,
            User.is_active,
            User.is_anonymous,
            User.status,
            User.update_date,
            User.is_superuser,
        ]
        return (
            cls.model.select(*fields)
            .join(UserTenant, on=((cls.model.member_id == UserTenant.id) & (UserTenant.status == StatusEnum.VALID.value)))
            .join(User, on=(User.id == UserTenant.user_id))
            .where((cls.model.id == id) & (cls.model.status == StatusEnum.VALID.value))
            .dicts()
            .first()
        )

    @classmethod
    @DB.connection_context()
    def get_by_department_id_with_info(cls, department_id):
        fields = [
            cls.model.member_id,
            cls.model.role,
            User.nickname,
            User.email,
            User.avatar,
            User.is_authenticated,
            User.is_active,
            User.is_anonymous,
            User.status,
            User.update_date,
            User.is_superuser,
        ]
        return list(
            cls.model.select(*fields)
            .join(UserTenant, on=((cls.model.member_id == UserTenant.id) & (UserTenant.status == StatusEnum.VALID.value)))
            .join(User, on=(User.id == UserTenant.user_id))
            .join(Department, on=((Department.id == cls.model.department_id) & (Department.status == StatusEnum.VALID.value)))
            .where((cls.model.department_id == department_id) & (cls.model.status == StatusEnum.VALID.value))
            .dicts()
        )

    @classmethod
    @DB.connection_context()
    def get_by_department_id(cls, department_id):
        return list(
            cls.model.select()
            .join(UserTenant, on=((cls.model.member_id == UserTenant.id) & (UserTenant.status == StatusEnum.VALID.value)))
            .join(User, on=(User.id == UserTenant.user_id))
            .join(Department, on=((Department.id == cls.model.department_id) & (Department.status == StatusEnum.VALID.value)))
            .where((cls.model.department_id == department_id) & (cls.model.status == StatusEnum.VALID.value))
        )

    @classmethod
    @DB.connection_context()
    def get_all_departments_by_member_id(cls, member_id):
        fields = [
            cls.model.department_id,
            Department.name,
            Department.path,
            Department.avatar,
            Department.owner_id,
            Department.tenant_id,
            Department.parent_id,
            Department.update_date,
            Department.formatted_path,
        ]
        return list(
            cls.model.select(*fields)
            .join(Department, on=((cls.model.department_id == Department.id) & (Department.status == StatusEnum.VALID.value)))
            .where((cls.model.member_id == member_id) & (cls.model.status == StatusEnum.VALID.value))
            .dicts()
        )

    @classmethod
    @DB.connection_context()
    def get_subdepartments_by_member_id(cls, member_id, parent_id):
        fields = [
            cls.model.department_id,
            Department.name,
            Department.path,
            Department.avatar,
            Department.owner_id,
            Department.tenant_id,
            Department.parent_id,
            Department.update_date,
            Department.description,
            Department.formatted_path,
        ]

        base = cls.model.select(*fields).distinct().join(Department, on=((cls.model.department_id == Department.id) & (Department.status == StatusEnum.VALID.value)))
        if not parent_id:
            query = base.where((cls.model.member_id == member_id) & (Department.id == Department.parent_id))
        else:
            query = base.where((cls.model.member_id == member_id) & (Department.parent_id == parent_id) & (Department.id != parent_id))

        query = query.order_by(Department.create_date.desc())

        return list(query.dicts())

    @classmethod
    @DB.connection_context()
    def delete_department_member(cls, department_id, member_id):
        with DB.atomic():
            cls.model.update({"status": StatusEnum.INVALID.value, "update_time": current_timestamp(), "update_date": datetime_format(datetime.now())}).where(
                (cls.model.department_id == department_id) & (cls.model.member_id == member_id) & (cls.model.status == StatusEnum.VALID.value)
            ).execute()

    @classmethod
    @DB.connection_context()
    def filter_by_group_and_member_ids(cls, department_id, member_ids):
        if not member_ids:
            return None

        members = list(cls.model.select().where((cls.model.department_id == department_id) & (cls.model.status == StatusEnum.VALID.value) & (cls.model.member_id.in_(member_ids))))
        return members if members else None

    @classmethod
    def delete(cls, member_model_list):
        """
        ! Use this method under DB.atomic() context
        """
        for member in member_model_list:
            member.update_time = current_timestamp()
            member.update_date = datetime_format(datetime.now())
            member.status = StatusEnum.INVALID.value

        cls.model.bulk_update(member_model_list, fields=[cls.model.status, cls.model.update_time, cls.model.update_date], batch_size=50)
