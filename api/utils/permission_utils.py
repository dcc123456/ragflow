import inspect
from functools import wraps

from common import settings
from common.constants import RetCode
from api.db import PermissionTargetType, PermissionValue, ResourceType
from api.db.services.permission_service import PermissionService
from api.db.services.team_service import DepartmentMemberService, DepartmentService, GroupMemberService, GroupService


def is_valid_permission(permission: int) -> bool:
    return isinstance(permission, int) and 0 <= permission <= PermissionValue.PERMISSION_OWNER.value


def has_permission(user_permission: int, required_permission: PermissionValue) -> bool:
    """
    Check whether the user has the specified permission.
    """
    return user_permission >= required_permission.value


def has_permission_for_member(operator_id, tenant_id, resource_id, resource_type, permission=PermissionValue.PERMISSION_MANAGE):
    from api.db.services.user_service import UserTenantService
    highest_permission = PermissionValue.PERMISSION_NULL.value
    permission_type = None

    # team owner
    operator = UserTenantService.filter_by_id(operator_id)
    if operator and operator.user_id == tenant_id:
        return (True, PermissionTargetType.TARGET_MEMBER, PermissionValue.PERMISSION_OWNER.value)

    # member
    operator_permission = PermissionService.filter_by_member_and_tenant_id_with_resource_id(operator_id, tenant_id, resource_id, resource_type=resource_type)
    if operator_permission and has_permission(operator_permission.permission, permission):
        if operator_permission.permission > highest_permission:
            highest_permission = operator_permission.permission
            permission_type = PermissionTargetType.TARGET_MEMBER

    # groups
    groups = GroupMemberService.get_groups_by_member_id(operator_id)
    group_ids = list({group["group_id"] for group in groups})
    group_permissions = PermissionService.filter_by_groups_and_tenant_id_with_resource_id(group_ids, tenant_id, resource_id, resource_type=resource_type)
    for group_permission in group_permissions:
        if has_permission(group_permission.permission, permission):
            if group_permission.permission > highest_permission:
                highest_permission = group_permission.permission
                permission_type = PermissionTargetType.TARGET_GROUP

    # departments
    departments = DepartmentMemberService.get_all_departments_by_member_id(operator_id)

    department_id_set = set([])
    for department in departments:
        hierarchy_ids = DepartmentService.get_department_hierarchy(department)
        department_id_set.update(hierarchy_ids)

    department_permissions = PermissionService.filter_by_departments_and_tenant_id_with_resource_id(list(department_id_set), tenant_id, resource_id, resource_type=resource_type)
    for department_permission in department_permissions:
        if has_permission(department_permission.permission, permission):
            if department_permission.permission > highest_permission:
                highest_permission = department_permission.permission
                permission_type = PermissionTargetType.TARGET_DEPARTMENT

    if highest_permission != PermissionValue.PERMISSION_NULL.value:
        return (True, permission_type, highest_permission)
    else:
        return (False, None, PermissionValue.PERMISSION_NULL.value)


def wrap_permission_info(permission_info):
    """
    Return format:
    - list of person/group/department entries
      {id, role, name, avatar, tenant_id, resource_type, permissions:{resource_id: permission}}
    """
    from api.db.services.user_service import UserService, UserTenantService
    permissions = []

    for info in permission_info:
        member_id = info.get("member_id")
        group_id = info.get("group_id")
        department_id = info.get("department_id")
        id = ""
        role = ""
        name = ""
        avatar = ""
        wrapped_info = {}

        if member_id:
            member = UserTenantService.filter_by_id(member_id)
            if member:
                user = UserService.filter_by_id(member.user_id)
                if user:
                    name = user.nickname
                    id = member_id
                    avatar = user.avatar
                    role = "member"

        elif group_id:
            group = GroupService.filter_by_id(group_id)
            if group:
                name = group.name
                id = group_id
                avatar = group.avatar
                role = "group"

        elif department_id:
            department = DepartmentService.filter_by_id(department_id)
            if department:
                name = department.name
                id = department_id
                avatar = department.avatar
                role = "department"

        if all([id, role, name]):
            wrapped_info = info.copy()
            wrapped_info["id"] = id
            wrapped_info["role"] = role
            wrapped_info["name"] = name
            wrapped_info["avatar"] = avatar

        for key in ["member_id", "group_id", "department_id"]:
            wrapped_info.pop(key, None)

        if wrapped_info:
            permissions.append(wrapped_info)

    grouped = {}
    for info in permissions:
        key = f"{info.get('role')}:{info.get('id')}"
        entry = grouped.get(key)
        if not entry:
            entry = {
                "id": info.get("id"),
                "role": info.get("role"),
                "name": info.get("name"),
                "avatar": info.get("avatar", ""),
                "tenant_id": info.get("tenant_id"),
                "resource_type": info.get("resource_type"),
                "permissions": {},
            }
            grouped[key] = entry
        entry["permissions"][info.get("resource_id")] = info.get("permission")

    return list(grouped.values())


def check_kb_permission(permission):
    from api.utils.api_utils import get_json_result
    from api.db.services.knowledgebase_service import KnowledgebaseService
    from api.db.services.user_service import UserTenantService, UserService
    """
    ! IMPORTANT:
    The request data is parsed in this decorator.
    In view functions, use `g.req_data` instead of `request.get_json()` or `request.form`.

    Supports:
    - JSON body
    - Form-data / urlencoded
    - URL query parameters
    - URL path parameters (kwargs)
    """

    def decorator(foo):
        from quart import request, g

        @wraps(foo)
        async def wrapper(*args, **kwargs):
            from api.apps import current_user
            content_type = request.headers.get("Content-Type") or ""

            if request.method in ["POST", "PUT", "PATCH"]:
                if "application/json" in content_type:
                    if not request.is_json:
                        return get_json_result(data=False, message="Content-Type must be application/json", code=RetCode.ARGUMENT_ERROR)
                    req_data = await request.get_json(silent=True) or {}

                # Form
                elif "multipart/form-data" in content_type or "application/x-www-form-urlencoded" in content_type:
                    req_data = await request.form or {}

                else:
                    req_data = request.args or {}

            else:  # GET、DELETE
                req_data = request.args or {}

            kb_id = req_data.get("kb_id") or kwargs.get("kb_id")
            if not kb_id:
                return get_json_result(data=False, message="Missing required parameter `kb_id`.", code=RetCode.ARGUMENT_ERROR)

            g.req_data = req_data
            g.kb_id = kb_id

            if settings.ENABLE_ADMIN and UserService.is_admin(current_user.id) and permission == PermissionValue.PERMISSION_READ:
                g.tenant_id = current_user.id
                if inspect.iscoroutinefunction(foo):
                    return await foo(*args, **kwargs)
                return foo(*args, **kwargs)

            user_tenants = UserTenantService.query(user_id=current_user.id) or []

            for user_tenant in user_tenants:
                kb_record = KnowledgebaseService.filter_by_id_and_tenant_id(tenant_id=user_tenant.tenant_id, kb_id=kb_id)
                if kb_record:
                    if (kb_record.created_by == current_user.id) or has_permission_for_member(
                        operator_id=user_tenant.id, tenant_id=user_tenant.tenant_id, resource_id=kb_id, resource_type=ResourceType.KB, permission=permission
                    )[0]:
                        g.tenant_id = user_tenant.tenant_id
                        if inspect.iscoroutinefunction(foo):
                            return await foo(*args, **kwargs)
                        return foo(*args, **kwargs)

            return get_json_result(data=False, message="Only knowledgebase owners or members with management or write permissions can perform this action.", code=RetCode.OPERATING_ERROR)

        return wrapper

    return decorator


def check_dialog_permission(permission):
    from api.utils.api_utils import get_json_result
    from api.db.services.dialog_service import DialogService
    from api.db.services.user_service import UserTenantService
    """
    ! IMPORTANT:
    The request data is parsed in this decorator.
    In view functions, use `g.req_data` instead of `request.get_json()` or `request.form`.

    Supports:
    - JSON body
    - Form-data / urlencoded
    - URL query parameters
    - URL path parameters (kwargs)
    """

    def decorator(foo):
        from quart import request, g

        @wraps(foo)
        async def wrapper(*args, **kwargs):
            from api.apps import current_user
            content_type = request.headers.get("Content-Type") or ""

            if request.method in ["POST", "PUT", "PATCH"]:
                if "application/json" in content_type:
                    if not request.is_json:
                        return get_json_result(data=False, message="Content-Type must be application/json", code=RetCode.ARGUMENT_ERROR)
                    req_data = (await request.get_json(silent=True)) or {}

                # Form
                elif "multipart/form-data" in content_type or "application/x-www-form-urlencoded" in content_type:
                    req_data = (await request.form) or {}

                else:
                    req_data = request.args or {}

            else:  # GET、DELETE
                req_data = request.args or {}

            dialog_id = req_data.get("dialog_id") or kwargs.get("dialog_id")

            g.req_data = req_data
            g.dialog_id = dialog_id

            if not dialog_id:
                if inspect.iscoroutinefunction(foo):
                    return await foo(*args, **kwargs)
                return foo(*args, **kwargs)

            user_tenants = UserTenantService.query(user_id=current_user.id) or []

            for user_tenant in user_tenants:
                dialog_record = DialogService.query(tenant_id=user_tenant.tenant_id, id=dialog_id)
                if dialog_record:
                    permission_info = has_permission_for_member(
                        operator_id=user_tenant.id, tenant_id=user_tenant.tenant_id, resource_id=dialog_id, resource_type=ResourceType.DIALOG, permission=permission
                    )
                    is_team_owner = dialog_record[0].tenant_id == current_user.id
                    if is_team_owner or permission_info[0]:
                        g.tenant_id = user_tenant.tenant_id
                        g.operator_permission = PermissionValue.PERMISSION_OWNER.value if is_team_owner else permission_info[2]
                        g.member_id = user_tenant.id
                        if inspect.iscoroutinefunction(foo):
                            return await foo(*args, **kwargs)
                        return foo(*args, **kwargs)

            return get_json_result(data=[], message="Only Chat/Dialog owners or members with management or write permissions can perform this action", code=RetCode.OPERATING_ERROR)

        return wrapper

    return decorator


def get_owner_id(tid, uid):
    return str(tid).rjust(32, "x") + str(uid).rjust(32, "_")

def tid_uid(owner_id):
    tid, uid = owner_id[:32], owner_id[32:]
    return tid.lstrip("_"), uid.lstrip("_")
