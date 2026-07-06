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

from peewee import fn

from common.constants import StatusEnum
from api.db import PermissionValue, ResourceType
from api.db.db_models import DB, Search, User
from api.db.services.common_service import CommonService
from api.db.services.permission_service import PermissionService
from common.time_utils import current_timestamp, datetime_format


class SearchService(CommonService):
    model = Search

    @classmethod
    def save(cls, **kwargs):
        current_ts = current_timestamp()
        current_date = datetime_format(datetime.now())

        kwargs["create_time"] = current_ts
        kwargs["create_date"] = current_date
        kwargs["update_time"] = current_ts
        kwargs["update_date"] = current_date
        obj = cls.model.create(**kwargs)
        return obj

    @classmethod
    @DB.connection_context()
    def accessible4deletion(cls, search_id, user_id) -> bool:
        search = (
            cls.model.select(cls.model.id)
            .where(
                cls.model.id == search_id,
                cls.model.created_by == user_id,
                cls.model.status == StatusEnum.VALID.value,
            )
            .first()
        )
        return search is not None

    @classmethod
    @DB.connection_context()
    def get_detail(cls, search_id):
        fields = [
            cls.model.id,
            cls.model.avatar,
            cls.model.tenant_id,
            cls.model.name,
            cls.model.description,
            cls.model.created_by,
            cls.model.search_config,
            cls.model.update_time,
            User.nickname,
            User.avatar.alias("tenant_avatar"),
        ]
        search = (
            cls.model.select(*fields)
            .join(User, on=((User.id == cls.model.tenant_id) & (User.status == StatusEnum.VALID.value)))
            .where((cls.model.id == search_id) & (cls.model.status == StatusEnum.VALID.value))
            .first()
        )
        if not search:
            return {}
        return search.to_dict()

    @classmethod
    @DB.connection_context()
    def count_by_tenant_id(cls, tenant_id) -> int:
        return cls.model.select().where((cls.model.tenant_id == tenant_id) & (cls.model.status == StatusEnum.VALID.value)).count()

    @classmethod
    @DB.connection_context()
    def get_by_tenant_ids(
        cls,
        joined_tenant_ids,
        user_id,
        page_number,
        items_per_page,
        orderby,
        desc,
        keywords,
        accessible_ids=None,
        permission_map=None,
    ):
        fields = [
            cls.model.id,
            cls.model.avatar,
            cls.model.tenant_id,
            cls.model.name,
            cls.model.description,
            cls.model.created_by,
            cls.model.status,
            cls.model.update_time,
            cls.model.create_time,
            User.nickname,
            User.avatar.alias("tenant_avatar"),
        ]
        query = (
            cls.model.select(*fields)
            .join(User, on=(cls.model.tenant_id == User.id))
            .where(((cls.model.tenant_id.in_(joined_tenant_ids)) | (cls.model.tenant_id == user_id)) & (cls.model.status == StatusEnum.VALID.value))
        )

        if accessible_ids is not None:
            query = query.where((cls.model.created_by == user_id) | (cls.model.id.in_(accessible_ids)))

        if keywords:
            query = query.where(fn.LOWER(cls.model.name).contains(keywords.lower()))
        if desc:
            query = query.order_by(cls.model.getter_by(orderby).desc())
        else:
            query = query.order_by(cls.model.getter_by(orderby).asc())

        count = query.count()

        if page_number and items_per_page:
            query = query.paginate(page_number, items_per_page)

        search_apps = list(query.dicts())
        if permission_map is None:
            permission_map = PermissionService.get_user_resource_permission_map(
                user_id,
                joined_tenant_ids,
                ResourceType.SEARCH,
                PermissionValue.PERMISSION_READ,
            )
        for search in search_apps:
            if search["tenant_id"] == user_id:
                search["operator_permission"] = PermissionValue.PERMISSION_OWNER.value
            else:
                search["operator_permission"] = permission_map.get(
                    search["id"],
                    PermissionValue.PERMISSION_NULL.value,
                )

        return search_apps, count

    @classmethod
    @DB.connection_context()
    def delete_by_tenant_id(cls, tenant_id):
        return cls.model.delete().where(cls.model.tenant_id == tenant_id).execute()
