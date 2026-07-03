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

from typing import List

from common.time_utils import current_timestamp, datetime_format
from api.db.db_models import DB, WhiteList
from api.db.services.common_service import CommonService


class WhiteListService(CommonService):
    model = WhiteList

    @classmethod
    @DB.connection_context()
    def create_white_list_row(cls, email: str):
        row_info = {
            "email": email,
            "create_time": current_timestamp(),
            "create_date": datetime_format(datetime.now()),
            "update_time": current_timestamp(),
            "update_date": datetime_format(datetime.now()),
        }
        obj = cls.model(**row_info).save(force_insert=True)
        return obj

    @classmethod
    @DB.connection_context()
    def get_white_list_by_email(cls, email: str):
        return cls.model.select().where(cls.model.email == email).first()

    @classmethod
    @DB.connection_context()
    def get_all_white_lists(cls):
        objs = cls.model.select()
        return list(objs.dicts())

    @classmethod
    @DB.connection_context()
    def update_white_list_row_by_id(cls, row_id, email):
        update_dict = {"email": email, "update_time": current_timestamp(), "update_date": datetime_format(datetime.now())}
        return cls.model.update(update_dict).where(cls.model.id == row_id).execute()

    @classmethod
    @DB.connection_context()
    def batch_create_white_list_row(cls, email_list: List[str]):
        emails = list(set(email_list))
        exist_email_rows = cls.model.select().where(cls.model.email in emails).execute()
        exist_emails = [row.email for row in exist_email_rows]
        row_list = [
            {
                "email": email,
                "create_time": current_timestamp(),
                "create_date": datetime_format(datetime.now()),
                "update_time": current_timestamp(),
                "update_date": datetime_format(datetime.now()),
            }
            for email in emails
            if email not in exist_emails
        ]
        insert_cnt = 0
        for row in row_list:
            insert_cnt += cls.model(**row).save()
        return insert_cnt
