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
from datetime import datetime

from api.db.db_models import DB, Role, RoleResource
from api.db.services.common_service import CommonService
from common.misc_utils import get_uuid
from common.time_utils import current_timestamp, datetime_format


class RoleService(CommonService):
    model = Role

    @classmethod
    @DB.connection_context()
    def create_role(cls, role_info: dict):
        role_dict = {
            **role_info,
            "create_time": current_timestamp(),
            "create_date": datetime_format(datetime.now()),
            "update_time": current_timestamp(),
            "update_date": datetime_format(datetime.now()),
        }
        obj = cls.model(**role_dict).save(force_insert=True)
        return obj, role_dict

    @classmethod
    @DB.connection_context()
    def get_by_role_name(cls, role_name: str):
        return list(cls.model.select().where(cls.model.role_name == role_name).dicts())

    @classmethod
    @DB.connection_context()
    def update_role_description(cls, role_id: int, description: str):
        update_dict = {"description": description, "update_time": current_timestamp(), "update_date": datetime_format(datetime.now())}
        return cls.model.update(update_dict).where(cls.model.id == role_id).execute()

    @classmethod
    @DB.connection_context()
    def get_all_roles(cls):
        roles = cls.model.select()
        return list(roles.dicts())


class RoleResourceService(CommonService):
    model = RoleResource

    @classmethod
    @DB.connection_context()
    def save(cls, **kwargs):
        if "id" not in kwargs:
            kwargs["id"] = get_uuid()
        obj = cls.model(**kwargs).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_by_role_id(cls, role_id: int):
        return list(cls.model.select().where(cls.model.role_id == role_id).dicts())

    @classmethod
    @DB.connection_context()
    def upsert_role_action_by_id(cls, role_id: int, new_resource_action_map: dict):
        """
        param: role_id: role.id
        param: new_resource_action_map: {resource_type: action}
        """
        with DB.atomic():
            db_row = cls.model.select().where(cls.model.role_id == role_id).execute()
            exist_resource_map = {row.resource_type: row.action for row in db_row}
            insert_dicts = [
                {
                    "role_id": role_id,
                    "resource_type": k,
                    "action": v,
                    "create_time": current_timestamp(),
                    "create_date": datetime_format(datetime.now()),
                    "update_time": current_timestamp(),
                    "update_date": datetime_format(datetime.now()),
                }
                for k, v in new_resource_action_map.items()
                if k not in exist_resource_map.keys()
            ]

            update_dicts = {
                k: {"action": v, "update_time": current_timestamp(), "update_date": datetime_format(datetime.now())}
                for k, v in new_resource_action_map.items()
                if k in exist_resource_map.keys() and exist_resource_map[k] != v
            }
            # insert & update db
            upsert_cnt = 0
            for insert_dict in insert_dicts:
                cls.model(**insert_dict).save()
                upsert_cnt += 1
            for resource_type, update_dict in update_dicts.items():
                upsert_cnt += cls.model.update(**update_dict).where((cls.model.role_id == role_id) & (cls.model.resource_type == resource_type)).execute()
            return upsert_cnt

    @classmethod
    @DB.connection_context()
    def delete_by_role_id(cls, role_id: int):
        with DB.atomic():
            return cls.model.delete().where(cls.model.role_id == role_id).execute()
