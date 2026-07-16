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
from quart import g
from common.misc_utils import thread_pool_exec
from api.utils.api_utils import get_json_result, validate_request
from api.utils.permission_utils import check_canvas_permission
from api.db import PermissionValue

from api.apps import login_required, current_user
from api.apps.services.canvas_replica_service import CanvasReplicaService
from common.role_util import check_role_access, CANVAS_API_ACTION_MAP, CANVAS_ROLE_RESOURCE_TYPE

canvas_role_guard = check_role_access(CANVAS_API_ACTION_MAP, CANVAS_ROLE_RESOURCE_TYPE)


# Presence APIs share the tenant and permission context prepared by the decorator.
@manager.route("/presence/join", methods=["POST"])  # noqa: F821
@validate_request("canvas_id", "tab_id", "presence_session_id")
@login_required
@canvas_role_guard
@check_canvas_permission(PermissionValue.PERMISSION_READ)
async def presence_join():
    req = g.req_data
    tenant_id = str(g.tenant_id)
    canvas_id = str(req["canvas_id"])

    data = CanvasReplicaService.join_presence(
        canvas_id=canvas_id,
        tenant_id=tenant_id,
        runtime_user_id=str(current_user.id),
        tab_id=str(req["tab_id"]),
        presence_session_id=str(req["presence_session_id"]),
    )
    return get_json_result(data=data)


@manager.route("/presence/heartbeat", methods=["POST"])  # noqa: F821
@validate_request("canvas_id", "tab_id", "presence_session_id")
@login_required
@canvas_role_guard
@check_canvas_permission(PermissionValue.PERMISSION_READ)
async def presence_heartbeat():
    req = g.req_data
    tenant_id = str(g.tenant_id)
    canvas_id = str(req["canvas_id"])

    data = CanvasReplicaService.heartbeat_presence(
        canvas_id=canvas_id,
        tenant_id=tenant_id,
        runtime_user_id=str(current_user.id),
        tab_id=str(req["tab_id"]),
        presence_session_id=str(req["presence_session_id"]),
    )
    return get_json_result(data=data)


@manager.route("/presence/leave", methods=["POST"])  # noqa: F821
@validate_request("canvas_id", "tab_id", "presence_session_id")
@login_required
@canvas_role_guard
@check_canvas_permission(PermissionValue.PERMISSION_READ)
async def presence_leave():
    req = g.req_data
    tenant_id = str(g.tenant_id)
    canvas_id = str(req["canvas_id"])

    data = CanvasReplicaService.leave_presence(
        canvas_id=canvas_id,
        tenant_id=tenant_id,
        runtime_user_id=str(current_user.id),
        tab_id=str(req["tab_id"]),
        presence_session_id=str(req["presence_session_id"]),
    )
    return get_json_result(data=data)


@manager.route("/<canvas_id>/presence", methods=["GET"])  # noqa: F821
@login_required
@canvas_role_guard
@check_canvas_permission(PermissionValue.PERMISSION_READ)
async def presence_list(canvas_id):
    data = await thread_pool_exec(
        CanvasReplicaService.list_presence,
        canvas_id=str(canvas_id),
        tenant_id=str(g.tenant_id),
        operator_permission=g.operator_permission,
    )
    return get_json_result(data=data)
