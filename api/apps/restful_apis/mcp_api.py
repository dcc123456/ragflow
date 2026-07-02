#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
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
from datetime import datetime

from quart import Response, request

from api.apps import current_user, login_required
from api.db import PermissionActionType, PermissionTargetType, PermissionValue, ResourceType
from api.db.db_models import MCPServer, DB
from api.db.services.mcp_server_service import MCPServerService
from api.db.services.permission_service import PermissionChangeLogService, PermissionService
from api.db.services.user_service import TenantService, UserTenantService
from api.utils.api_utils import get_data_error_result, get_json_result, get_mcp_tools, get_request_json, server_error_response, validate_request
from api.utils.pagination_utils import validate_rest_api_page_size
from api.utils.permission_utils import _permission_denied_message, has_permission_for_member
from api.utils.web_utils import get_float, safe_json_parse
from common.constants import RetCode, VALID_MCP_SERVER_TYPES
from common.mcp_tool_call_conn import MCPToolCallSession, close_multiple_mcp_toolcall_sessions
from common.misc_utils import get_uuid, thread_pool_exec
from common.time_utils import current_timestamp, datetime_format
from common.ssrf_guard import assert_url_is_safe, pin_dns_global

def _get_mcp_ids_from_args() -> list[str]:
    mcp_ids = request.args.getlist("mcp_ids")
    if mcp_ids:
        return [mcp_id for item in mcp_ids for mcp_id in item.split(",") if mcp_id]
    mcp_ids = request.args.get("mcp_id", "")
    return [mcp_id for mcp_id in mcp_ids.split(",") if mcp_id]


def _export_mcp_servers(mcp_ids: list[str]) -> dict | None:
    exported_servers = {}
    for mcp_id in mcp_ids:
        e, mcp_server = MCPServerService.get_by_id(mcp_id)
        if e and mcp_server.tenant_id == current_user.id:
            server_key = mcp_server.name
            exported_servers[server_key] = {
                "type": mcp_server.server_type,
                "url": mcp_server.url,
                "name": mcp_server.name,
                "authorization_token": mcp_server.variables.get("authorization_token", ""),
                "tools": mcp_server.variables.get("tools", {}),
            }

    if not exported_servers:
        return None

    return {"mcpServers": exported_servers}


def _assert_mcp_url_is_safe(url, invalid_message: str = "Invalid url.") -> tuple[str, str, str | None]:
    if not isinstance(url, str) or not url:
        return "", "", invalid_message
    try:
        hostname, resolved_ip = assert_url_is_safe(url)
    except ValueError as exc:
        return "", "", str(exc)
    return hostname, resolved_ip, None


def _fill_mcp_timestamps(data: dict) -> dict:
    timestamp = current_timestamp()
    cur_datetime = datetime_format(datetime.now())
    data["create_time"] = timestamp
    data["create_date"] = cur_datetime
    data["update_time"] = timestamp
    data["update_date"] = cur_datetime
    return data


def _insert_mcp_server(data: dict) -> None:
    model_insert = getattr(MCPServer, "insert", None)
    if callable(model_insert):
        model_insert(**data).execute()
        return
    if not MCPServerService.insert(**data):
        raise ValueError("Failed to create MCP server.")


def _grant_mcp_owner_permission(mcp_id: str, operator) -> None:
    if not PermissionService.save(
        id=get_uuid(),
        member_id=operator.id,
        tenant_id=operator.tenant_id,
        resource_type=ResourceType.MCP,
        resource_id=mcp_id,
        permission=PermissionValue.PERMISSION_OWNER.value,
    ):
        raise ValueError("Permission creation failed")
    try:
        if not PermissionChangeLogService.save(
            id=get_uuid(),
            tenant_id=operator.tenant_id,
            operator_id=operator.id,
            target_type=PermissionTargetType.TARGET_MEMBER,
            target_id=operator.id,
            resource_type=ResourceType.MCP,
            resource_id=mcp_id,
            old_permission=PermissionValue.PERMISSION_NULL.value,
            new_permission=PermissionValue.PERMISSION_OWNER.value,
            action_type=PermissionActionType.ACTION_ADD,
        ):
            logging.warning("Failed to create MCP permission change log for resource_id=%s", mcp_id)
    except Exception as exc:
        logging.warning("Failed to create MCP permission change log for resource_id=%s: %s", mcp_id, exc)


@manager.route("/mcp/servers", methods=["GET"])  # noqa: F821
@login_required
async def list_mcp() -> Response:
    keywords = request.args.get("keywords", "")
    page_number = int(request.args.get("page", 0))
    items_per_page = validate_rest_api_page_size(int(request.args.get("page_size", 0)))
    orderby = request.args.get("orderby", "create_time")
    if request.args.get("desc", "true").lower() == "false":
        desc = False
    else:
        desc = True

    mcp_ids = _get_mcp_ids_from_args()
    try:
        servers = MCPServerService.get_servers(current_user.id, mcp_ids, 0, 0, orderby, desc, keywords) or []
        total = len(servers)

        from api.db.services.user_service import UserTenantService

        user_tenants = UserTenantService.query(user_id=current_user.id) or []
        for server in servers:
            tenant_id = server.get("tenant_id")
            if not tenant_id:
                continue

            if tenant_id == current_user.id:
                server["operator_permission"] = PermissionValue.PERMISSION_OWNER.value
                continue

            highest_permission = PermissionValue.PERMISSION_NULL.value
            for user_tenant in user_tenants:
                if user_tenant.tenant_id != tenant_id:
                    continue
                permission_info = has_permission_for_member(
                    operator_id=user_tenant.id,
                    tenant_id=tenant_id,
                    resource_id=server["id"],
                    resource_type=ResourceType.MCP,
                    permission=PermissionValue.PERMISSION_READ,
                )
                if permission_info[0] and permission_info[2] > highest_permission:
                    highest_permission = permission_info[2]

            server["operator_permission"] = highest_permission

        if page_number and items_per_page:
            servers = servers[(page_number - 1) * items_per_page : page_number * items_per_page]

        return get_json_result(data={"mcp_servers": servers, "total": total})
    except Exception as e:
        return server_error_response(e)


@manager.route("/mcp/servers/<mcp_id>", methods=["GET"])  # noqa: F821
@login_required
def detail(mcp_id: str) -> Response:
    try:
        if request.args.get("mode") == "download":
            exported_servers = _export_mcp_servers([mcp_id])
            if exported_servers is None:
                return get_data_error_result(message=f"Cannot find MCP server {mcp_id} for user {current_user.id}")
            return get_json_result(data=exported_servers)

        mcp_server = MCPServerService.get_or_none(id=mcp_id)
        if mcp_server is None:
            return get_data_error_result(message=f"Cannot find MCP server {mcp_id} for user {current_user.id}")
        if not MCPServerService.is_accessible(mcp_id, current_user.id, PermissionValue.PERMISSION_READ):
            return get_json_result(
                data=False,
                message=_permission_denied_message("MCP server", PermissionValue.PERMISSION_READ),
                code=RetCode.PERMISSION_ERROR,
            )

        return get_json_result(data=mcp_server.to_dict())
    except Exception as e:
        return server_error_response(e)


@manager.route("/mcp/servers", methods=["POST"])  # noqa: F821
@login_required
@validate_request("name", "url", "server_type")
async def create() -> Response:
    req = await get_request_json()

    server_type = req.get("server_type", "")
    if server_type not in VALID_MCP_SERVER_TYPES:
        return get_data_error_result(message="Unsupported MCP server type.")

    server_name = req.get("name", "")
    if not server_name or len(server_name.encode("utf-8")) > 255:
        return get_data_error_result(message=f"Invalid MCP name or length is {len(server_name)} which is large than 255.")

    e, _ = MCPServerService.get_by_name_and_tenant(name=server_name, tenant_id=current_user.id)
    if e:
        return get_data_error_result(message="Duplicated MCP server name.")

    url = req.get("url", "")
    hostname, resolved_ip, url_error = _assert_mcp_url_is_safe(url)
    if url_error:
        return get_data_error_result(message=url_error)

    headers = safe_json_parse(req.get("headers", {}))
    req["headers"] = headers
    variables = safe_json_parse(req.get("variables", {}))
    variables.pop("tools", None)

    timeout = get_float(req, "timeout", 10)

    try:
        req["id"] = get_uuid()
        req["tenant_id"] = current_user.id

        e, _ = TenantService.get_by_id(current_user.id)
        if not e:
            return get_data_error_result(message="Tenant not found.")
        operator = UserTenantService.filter_by_tenant_and_user_id(current_user.id, current_user.id)
        if not operator:
            return get_data_error_result(message="Tenant not found.")

        mcp_server = MCPServer(id=server_name, name=server_name, url=url, server_type=server_type, variables=variables, headers=headers)
        with pin_dns_global(hostname, resolved_ip):
            server_tools, err_message = await thread_pool_exec(get_mcp_tools, [mcp_server], timeout)
        if err_message:
            return get_data_error_result(message=err_message)

        tools = server_tools[server_name]
        tools = {tool["name"]: tool for tool in tools if isinstance(tool, dict) and "name" in tool}
        variables["tools"] = tools
        req["variables"] = variables
        _fill_mcp_timestamps(req)

        try:
            with DB.atomic():
                _insert_mcp_server(req)
                _grant_mcp_owner_permission(req["id"], operator)
        except ValueError as exc:
            return get_data_error_result(message=str(exc))

        return get_json_result(data=req)
    except Exception as e:
        return server_error_response(e)


@manager.route("/mcp/servers/<mcp_id>", methods=["PUT"])  # noqa: F821
@login_required
async def update(mcp_id: str) -> Response:
    req = await get_request_json()

    e, mcp_server = MCPServerService.get_by_id(mcp_id)
    if not e:
        return get_data_error_result(message=f"Cannot find MCP server {mcp_id} for user {current_user.email}.")
    if not MCPServerService.is_accessible(mcp_id, current_user.id, PermissionValue.PERMISSION_WRITE):
        return get_data_error_result(message=f"Cannot access MCP server {mcp_server.name} for user {current_user.email} with write permission.")

    server_type = req.get("server_type", mcp_server.server_type)
    if server_type and server_type not in VALID_MCP_SERVER_TYPES:
        return get_data_error_result(message="Unsupported MCP server type.")
    server_name = req.get("name", mcp_server.name)
    if server_name and len(server_name.encode("utf-8")) > 255:
        return get_data_error_result(message=f"Invalid MCP name or length is {len(server_name)} which is large than 255.")
    url = req.get("url", mcp_server.url)
    hostname, resolved_ip, url_error = _assert_mcp_url_is_safe(url)
    if url_error:
        return get_data_error_result(message=url_error)

    headers = safe_json_parse(req.get("headers", mcp_server.headers))
    req["headers"] = headers

    variables = safe_json_parse(req.get("variables", mcp_server.variables))
    variables.pop("tools", None)

    timeout = get_float(req, "timeout", 10)

    try:
        mcp_server = MCPServer(id=server_name, name=server_name, url=url, server_type=server_type, variables=variables, headers=headers)
        with pin_dns_global(hostname, resolved_ip):
            server_tools, err_message = await thread_pool_exec(get_mcp_tools, [mcp_server], timeout)
        if err_message:
            return get_data_error_result(message=err_message)

        tools = server_tools[server_name]
        tools = {tool["name"]: tool for tool in tools if isinstance(tool, dict) and "name" in tool}
        variables["tools"] = tools
        req["variables"] = variables

        allowed_fields = {"name", "tenant_id", "url", "server_type", "description", "variables", "headers"}
        update_data = {k: v for k, v in req.items() if k in allowed_fields}

        if not MCPServerService.filter_update([MCPServer.id == mcp_id], update_data):
            return get_data_error_result(message="Failed to updated MCP server.")

        e, updated_mcp = MCPServerService.get_by_id(mcp_id)
        if not e:
            return get_data_error_result(message="Failed to fetch updated MCP server.")

        return get_json_result(data=updated_mcp.to_dict())
    except Exception as e:
        return server_error_response(e)


@manager.route("/mcp/servers/<mcp_id>", methods=["DELETE"])  # noqa: F821
@login_required
async def rm(mcp_id: str) -> Response:
    try:
        e, mcp_server = MCPServerService.get_by_id(mcp_id)
        if not e:
            return get_data_error_result(message=f"Cannot find MCP server {mcp_id} for user {current_user.id}")
        if not MCPServerService.is_accessible(mcp_id, current_user.id, PermissionValue.PERMISSION_OWNER):
            return get_data_error_result(message=f"Cannot access MCP server {mcp_server.name} for user {current_user.email} with owner permission.")

        # enterprise edition
        with DB.atomic():
            permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(
                tenant_id=mcp_server.tenant_id, resource_id=mcp_id, resource_type=ResourceType.MCP)
            if permission_model_list:
                PermissionService.delete(permission_model_list)

        if not MCPServerService.delete_by_ids([mcp_id]):
            return get_data_error_result(message=f"Failed to delete MCP servers {[mcp_id]}")

        return get_json_result(data=True)
    except Exception as e:
        return server_error_response(e)


@manager.route("/mcp/servers/import", methods=["POST"])  # noqa: F821
@login_required
@validate_request("mcpServers")
async def import_multiple() -> Response:
    req = await get_request_json()
    servers = req.get("mcpServers", {})
    if not servers:
        return get_data_error_result(message="No MCP servers provided.")

    timeout = get_float(req, "timeout", 10)

    results = []
    try:
        operator = UserTenantService.filter_by_tenant_and_user_id(current_user.id, current_user.id)
        if not operator:
            return get_data_error_result(message="Tenant not found.")
        for server_name, config in servers.items():
            if not all(key in config for key in {"type", "url"}):
                results.append({"server": server_name, "success": False, "message": "Missing required fields (type or url)"})
                continue

            if not server_name or len(server_name.encode("utf-8")) > 255:
                results.append({"server": server_name, "success": False, "message": f"Invalid MCP name or length is {len(server_name)} which is large than 255."})
                continue
            if config["type"] not in VALID_MCP_SERVER_TYPES:
                results.append({"server": server_name, "success": False, "message": "Unsupported MCP server type."})
                continue
            hostname, resolved_ip, url_error = _assert_mcp_url_is_safe(config["url"])
            if url_error:
                results.append({"server": server_name, "success": False, "message": url_error})
                continue

            base_name = server_name
            new_name = base_name
            counter = 0

            while True:
                e, _ = MCPServerService.get_by_name_and_tenant(name=new_name, tenant_id=current_user.id)
                if not e:
                    break
                new_name = f"{base_name}_{counter}"
                counter += 1

            create_data = {
                "id": get_uuid(),
                "tenant_id": current_user.id,
                "name": new_name,
                "url": config["url"],
                "server_type": config["type"],
                "variables": {"authorization_token": config.get("authorization_token", "")},
            }

            headers = {"authorization_token": config["authorization_token"]} if "authorization_token" in config else {}
            variables = {k: v for k, v in config.items() if k not in {"type", "url", "headers"}}
            mcp_server = MCPServer(id=new_name, name=new_name, url=config["url"], server_type=config["type"], variables=variables, headers=headers)
            with pin_dns_global(hostname, resolved_ip):
                server_tools, err_message = await thread_pool_exec(get_mcp_tools, [mcp_server], timeout)
            if err_message:
                results.append({"server": base_name, "success": False, "message": err_message})
                continue

            tools = server_tools[new_name]
            tools = {tool["name"]: tool for tool in tools if isinstance(tool, dict) and "name" in tool}
            create_data["variables"]["tools"] = tools
            _fill_mcp_timestamps(create_data)

            try:
                with DB.atomic():
                    _insert_mcp_server(create_data)
                    _grant_mcp_owner_permission(create_data["id"], operator)
                result = {"server": server_name, "success": True, "action": "created", "id": create_data["id"], "new_name": new_name}
                if new_name != base_name:
                    result["message"] = f"Renamed from '{base_name}' to '{new_name}' avoid duplication"
                results.append(result)
            except Exception:
                logging.exception("Failed to create MCP server %s", server_name)
                results.append({"server": server_name, "success": False, "message": "Failed to create MCP server."})

        return get_json_result(data={"results": results})
    except Exception as e:
        return server_error_response(e)


@manager.route("/mcp/servers/<mcp_id>/test", methods=["POST"])  # noqa: F821
@login_required
@validate_request("url", "server_type")
async def test_mcp(mcp_id: str) -> Response:
    req = await get_request_json()

    url = req.get("url", "")
    if not isinstance(url, str) or not url:
        return get_data_error_result(message="Invalid MCP url.")

    server_type = req.get("server_type", "")
    if server_type not in VALID_MCP_SERVER_TYPES:
        return get_data_error_result(message="Unsupported MCP server type.")

    hostname, resolved_ip, url_error = _assert_mcp_url_is_safe(url, "Invalid MCP url.")
    if url_error:
        return get_data_error_result(message=url_error)

    timeout = get_float(req, "timeout", 10)
    headers = safe_json_parse(req.get("headers", {}))
    variables = safe_json_parse(req.get("variables", {}))

    mcp_server = MCPServer(id=mcp_id, server_type=server_type, url=url, headers=headers, variables=variables)

    result = []
    try:
        with pin_dns_global(hostname, resolved_ip):
            tool_call_session = MCPToolCallSession(mcp_server, mcp_server.variables)

            try:
                tools = await thread_pool_exec(tool_call_session.get_tools, timeout)
            except Exception as e:
                return get_data_error_result(message=f"Test MCP error: {e}")
            finally:
                await thread_pool_exec(close_multiple_mcp_toolcall_sessions, [tool_call_session])

        for tool in tools:
            tool_dict = tool.model_dump()
            tool_dict["enabled"] = True
            result.append(tool_dict)

        return get_json_result(data=result)
    except Exception as e:
        return server_error_response(e)
