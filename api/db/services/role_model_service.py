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
from common.time_utils import current_timestamp, timestamp_to_date
from api.db.db_models import RoleDefaultModel, DB
from api.db.services.common_service import CommonService

class RoleDefaultModelService(CommonService):
    model = RoleDefaultModel

    @classmethod
    @DB.connection_context()
    def get_by_role_id(cls, role_id: int):
        objs = cls.model.select().where(cls.model.role_id==role_id)
        return list(objs)

    @classmethod
    @DB.connection_context()
    def get_by_role_id_and_model_type(cls, role_id: int, model_type: str):
        obj = cls.model.select().where((cls.model.role_id==role_id) & (cls.model.model_type==model_type)).first()
        return obj

    @classmethod
    @DB.connection_context()
    def update_role_default_model_by_type(cls, role_id: int, model_type: str, model_id: str, tenant_id: str):
        timestamp = current_timestamp()
        date_str = timestamp_to_date(timestamp)
        update_dict = {
            "model_id": model_id,
            "tenant_id": tenant_id,
            "update_time": timestamp,
            "update_date": date_str
        }
        return cls.model.update(**update_dict).where(cls.model.role_id == role_id and cls.model.model_type == model_type).execute()

    @classmethod
    @DB.connection_context()
    def add_role_default_model(cls, role_id: int, model_type: str, model_id: str, tenant_id: str):
        timestamp = current_timestamp()
        date_str = timestamp_to_date(timestamp)
        insert_dict = {
            "role_id": role_id,
            "model_type": model_type,
            "model_id": model_id,
            "tenant_id": tenant_id,
            "create_time": timestamp,
            "create_date": date_str,
            "update_time": timestamp,
            "update_date": date_str
        }
        return cls.model(**insert_dict).save(force_insert=True)

    @classmethod
    @DB.connection_context()
    def delete_by_role_id(cls, role_id: int):
        return cls.model.delete().where(cls.model.role_id == role_id).execute()
