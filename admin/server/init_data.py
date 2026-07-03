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
from common.settings import DEFAULT_ROLE
from api.common.exceptions import AdminException
from api.db.db_models import User
from api.db.services.user_service import UserService
from api.db.services.role_service import RoleService


def init_user_role():
    empty_role_user = UserService.query(role_id="")
    if not empty_role_user:
        return

    roles = RoleService.get_by_role_name(DEFAULT_ROLE)
    if not roles:
        raise AdminException(f"Default role {DEFAULT_ROLE} not found!")

    role = roles[0]
    cnt = UserService.filter_update([User.role_id == 0], {"role_id": role["id"]})
    print(f"User role initialized! {cnt} rows updated.")
