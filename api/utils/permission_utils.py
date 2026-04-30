import inspect
from functools import wraps

from api.db import PermissionActionType, PermissionTargetType, PermissionValue, ResourceType
from api.db.services.permission_service import PermissionService
from api.db.services.team_service import DepartmentMemberService, DepartmentService, GroupMemberService, GroupService
from common import settings
from common.constants import RetCode, StatusEnum
from common.misc_utils import get_uuid


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


def resolve_kb_with_permission(user_id, kb_id, required_permission=PermissionValue.PERMISSION_READ):
    """Resolve a KB for an entrypoint after checking the caller's KB permission.

    Loads the KB and verifies ``user_id`` has at least ``required_permission`` via
    KB authorship or an explicit Permission-table grant (the same paths the
    decorator honors via ``has_permission_for_member``). Returns
    ``(kb, error_message)``; ``kb`` is ``None`` when access is denied.

    Use this from entrypoints where the decorator can't be applied, such as SDK
    routes that receive the API-token owner as a function argument.
    """
    from api.db.services.knowledgebase_service import KnowledgebaseService
    from api.db.services.user_service import UserTenantService
    from common.constants import StatusEnum

    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok or kb.status != StatusEnum.VALID.value:
        return None, f"User '{user_id}' lacks permission for dataset '{kb_id}'"

    user_tenants = UserTenantService.query(user_id=user_id) or []
    for ut in user_tenants:
        if ut.tenant_id != kb.tenant_id:
            continue
        if kb.created_by == user_id:
            return kb, None
        granted, _, _ = has_permission_for_member(
            operator_id=ut.id,
            tenant_id=ut.tenant_id,
            resource_id=kb_id,
            resource_type=ResourceType.KB,
            permission=required_permission,
        )
        if granted:
            return kb, None

    return None, f"User '{user_id}' lacks permission for dataset '{kb_id}'"


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
    from api.db.services.knowledgebase_service import KnowledgebaseService
    from api.db.services.user_service import UserService, UserTenantService
    from api.utils.api_utils import get_json_result

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
        from quart import g, request

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

            kb_id = (
                req_data.get("kb_id")
                or kwargs.get("kb_id")
                or req_data.get("dataset_id")
                or kwargs.get("dataset_id")
            )
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
                        g.kb = kb_record
                        if "tenant_id" in kwargs:
                            kwargs["tenant_id"] = user_tenant.tenant_id
                        if inspect.iscoroutinefunction(foo):
                            return await foo(*args, **kwargs)
                        return foo(*args, **kwargs)

            return get_json_result(data=False, message="Only knowledgebase owners or members with management or write permissions can perform this action.", code=RetCode.OPERATING_ERROR)

        return wrapper

    return decorator


def check_dialog_permission(permission):
    from api.db.services.dialog_service import DialogService
    from api.db.services.user_service import UserTenantService
    from api.utils.api_utils import get_json_result

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
        from quart import g, request

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

            dialog_id = req_data.get("chat_id") or kwargs.get("chat_id") or req_data.get("dialog_id") or kwargs.get("dialog_id")

            g.req_data = req_data
            g.dialog_id = dialog_id

            if not dialog_id:
                if inspect.iscoroutinefunction(foo):
                    return await foo(*args, **kwargs)
                return foo(*args, **kwargs)

            queried_user_tenants = list(UserTenantService.query(user_id=current_user.id) or [])
            user_tenants = UserTenantService.get_user_tenants_with_owner(current_user.id)

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

            if queried_user_tenants:
                return get_json_result(
                    data=False,
                    message="No authorization.",
                    code=RetCode.AUTHENTICATION_ERROR,
                )

            return get_json_result(data=[], message="Only Chat/Dialog owners or members with management or write permissions can perform this action", code=RetCode.OPERATING_ERROR)

        return wrapper

    return decorator


def _filter_accessible_document_ids(tenant_id, operator_id, kb_ids, doc_ids=None):
    """
    Strict document access filter.
    Returns doc_ids the operator can access. Empty list means no access.
    """
    from api.db import PermissionValue, ResourceType
    from api.db.db_models import Document, Permission, UserTenant
    from api.db.services.team_service import DepartmentMemberService, DepartmentService, GroupMemberService
    from common.constants import StatusEnum

    if not kb_ids:
        return []
    if doc_ids == ["-999"]:
        return doc_ids
    if doc_ids:
        doc_ids = list(dict.fromkeys(doc_ids))

    operator = UserTenant.get_or_none((UserTenant.id == operator_id) & (UserTenant.status == StatusEnum.VALID.value))
    # Tenant owner can access all documents under the KBs.
    if operator and operator.user_id == tenant_id:
        if doc_ids:
            return doc_ids
        return [d["id"] for d in Document.select(Document.id).where(Document.kb_id.in_(kb_ids)).dicts()]

    permission_conditions = (
        (Permission.tenant_id == tenant_id)
        & (Permission.resource_type == ResourceType.DOCUMENT)
        & (Permission.permission >= PermissionValue.PERMISSION_READ.value)
        & (Permission.status == StatusEnum.VALID.value)
    )
    if doc_ids:
        permission_conditions &= Permission.resource_id.in_(doc_ids)

    allowed_doc_ids = set()

    member_docs = Permission.select(Permission.resource_id).where(permission_conditions & (Permission.member_id == operator_id))
    allowed_doc_ids.update([r["resource_id"] for r in member_docs.dicts()])

    groups = GroupMemberService.get_groups_by_member_id(operator_id)
    group_ids = list({g["group_id"] for g in groups})
    if group_ids:
        group_docs = Permission.select(Permission.resource_id).where(permission_conditions & (Permission.group_id.in_(group_ids)))
        allowed_doc_ids.update([r["resource_id"] for r in group_docs.dicts()])

    departments = DepartmentMemberService.get_all_departments_by_member_id(operator_id)
    department_id_set = set()
    for department in departments:
        department_id_set.update(DepartmentService.get_department_hierarchy(department))
    if department_id_set:
        dept_docs = Permission.select(Permission.resource_id).where(permission_conditions & (Permission.department_id.in_(list(department_id_set))))
        allowed_doc_ids.update([r["resource_id"] for r in dept_docs.dicts()])

    if not allowed_doc_ids:
        return []

    allowed_doc_ids = set(
        [d["id"] for d in Document.select(Document.id).where((Document.id.in_(list(allowed_doc_ids))) & (Document.kb_id.in_(kb_ids))).dicts()]
    )

    if doc_ids:
        return list(allowed_doc_ids.intersection(set(doc_ids)))
    return list(allowed_doc_ids)


def filter_accessible_doc_ids_for_user(user_id, kb_ids, doc_ids=None):
    """
    Filter document IDs for a user across KBs.
    Returns (filtered_doc_ids, tenant_ids, error_message).
    """
    from api.db.services.knowledgebase_service import KnowledgebaseService
    from api.db.services.user_service import UserTenantService

    permission_error_msg = "Only owner of dataset authorized for this operation."

    if not kb_ids:
        return [], [], ""

    tenants = UserTenantService.query(user_id=user_id)
    if not tenants:
        return [], [], permission_error_msg

    tenant_member_id_map = {tenant.tenant_id: tenant.id for tenant in tenants}
    kb_tenant_map = {}
    tenant_ids = []

    for kb_id in kb_ids:
        for tenant in tenants:
            if KnowledgebaseService.query(tenant_id=tenant.tenant_id, id=kb_id):
                tenant_ids.append(tenant.tenant_id)
                kb_tenant_map[kb_id] = tenant.tenant_id
                break
        else:
            return [], [], permission_error_msg

    filtered_doc_ids = set()
    for kb_tenant_id in set(kb_tenant_map.values()):
        member_id = tenant_member_id_map.get(kb_tenant_id)
        if not member_id:
            return [], [], permission_error_msg
        tenant_kb_ids = [kid for kid, tid in kb_tenant_map.items() if tid == kb_tenant_id]
        allowed_doc_ids = _filter_accessible_document_ids(
            kb_tenant_id,
            member_id,
            tenant_kb_ids,
            doc_ids if doc_ids else None,
        )
        filtered_doc_ids.update(allowed_doc_ids)

    if not filtered_doc_ids:
        return [], list(set(tenant_ids)), ""

    return list(filtered_doc_ids), list(set(tenant_ids)), ""


def check_canvas_permission(permission):
    from api.db.services.canvas_service import UserCanvasService
    from api.db.services.user_service import UserTenantService
    from api.utils.api_utils import get_json_result

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
        from quart import g, request

        @wraps(foo)
        async def wrapper(*args, **kwargs):
            from api.apps import current_user

            content_type = request.headers.get("Content-Type") or ""

            if request.method in ["POST", "PUT", "PATCH"]:
                if "application/json" in content_type:
                    if not request.is_json:
                        return get_json_result(data=False, message="Content-Type must be application/json", code=RetCode.ARGUMENT_ERROR)
                    req_data = (await request.get_json(silent=True)) or {}

                elif "multipart/form-data" in content_type or "application/x-www-form-urlencoded" in content_type:
                    req_data = (await request.form) or {}

                else:
                    req_data = request.args or {}

            else:  # GET, DELETE
                req_data = request.args or {}

            canvas_id = req_data.get("id") or req_data.get("canvas_id") or kwargs.get("canvas_id")

            g.req_data = req_data
            g.canvas_id = canvas_id

            if not canvas_id:
                if inspect.iscoroutinefunction(foo):
                    return await foo(*args, **kwargs)
                return foo(*args, **kwargs)

            e, canvas = UserCanvasService.get_by_canvas_id(canvas_id)
            if not e:
                return get_json_result(data=False, message="Canvas not found.", code=RetCode.OPERATING_ERROR)

            # Owner check
            if canvas["user_id"] == current_user.id:
                g.tenant_id = current_user.id
                g.operator_permission = PermissionValue.PERMISSION_OWNER.value
                g.member_id = None
                if inspect.iscoroutinefunction(foo):
                    return await foo(*args, **kwargs)
                return foo(*args, **kwargs)

            # Permission table check across all tenants the user belongs to
            user_tenants = UserTenantService.query(user_id=current_user.id) or []
            for user_tenant in user_tenants:
                # Only check tenants where the canvas owner is the tenant owner
                if user_tenant.tenant_id != canvas["user_id"]:
                    continue
                permission_info = has_permission_for_member(
                    operator_id=user_tenant.id,
                    tenant_id=user_tenant.tenant_id,
                    resource_id=canvas_id,
                    resource_type=ResourceType.CANVAS,
                    permission=permission,
                )
                if permission_info[0]:
                    g.tenant_id = user_tenant.tenant_id
                    g.operator_permission = permission_info[2]
                    g.member_id = user_tenant.id
                    if inspect.iscoroutinefunction(foo):
                        return await foo(*args, **kwargs)
                    return foo(*args, **kwargs)

            return get_json_result(data=False, message="Only canvas owners or members with sufficient permissions can perform this action.", code=RetCode.OPERATING_ERROR)

        return wrapper

    return decorator


def check_mcp_permission(permission):
    from api.db.services.mcp_server_service import MCPServerService
    from api.db.services.user_service import UserTenantService
    from api.utils.api_utils import get_json_result

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
        from quart import g, request

        @wraps(foo)
        async def wrapper(*args, **kwargs):
            from api.apps import current_user

            content_type = request.headers.get("Content-Type") or ""

            if request.method in ["POST", "PUT", "PATCH"]:
                if "application/json" in content_type:
                    if not request.is_json:
                        return get_json_result(data=False, message="Content-Type must be application/json", code=RetCode.ARGUMENT_ERROR)
                    req_data = (await request.get_json(silent=True)) or {}

                elif "multipart/form-data" in content_type or "application/x-www-form-urlencoded" in content_type:
                    req_data = (await request.form) or {}

                else:
                    req_data = request.args or {}

            else:  # GET, DELETE
                req_data = request.args or {}

            mcp_id = req_data.get("mcp_id") or kwargs.get("mcp_id")

            g.req_data = req_data
            g.mcp_id = mcp_id

            if not mcp_id:
                if inspect.iscoroutinefunction(foo):
                    return await foo(*args, **kwargs)
                return foo(*args, **kwargs)

            e, mcp_server = MCPServerService.get_by_id(mcp_id)
            if not e:
                return get_json_result(data=False, message="MCP server not found.", code=RetCode.OPERATING_ERROR)

            # Owner check
            if mcp_server.tenant_id == current_user.id:
                g.tenant_id = current_user.id
                g.operator_permission = PermissionValue.PERMISSION_OWNER.value
                g.member_id = None
                if inspect.iscoroutinefunction(foo):
                    return await foo(*args, **kwargs)
                return foo(*args, **kwargs)

            # Permission table check across all tenants the user belongs to
            user_tenants = UserTenantService.query(user_id=current_user.id) or []
            for user_tenant in user_tenants:
                if user_tenant.tenant_id != mcp_server.tenant_id:
                    continue
                permission_info = has_permission_for_member(
                    operator_id=user_tenant.id,
                    tenant_id=user_tenant.tenant_id,
                    resource_id=mcp_id,
                    resource_type=ResourceType.MCP,
                    permission=permission,
                )
                if permission_info[0]:
                    g.tenant_id = user_tenant.tenant_id
                    g.operator_permission = permission_info[2]
                    g.member_id = user_tenant.id
                    if inspect.iscoroutinefunction(foo):
                        return await foo(*args, **kwargs)
                    return foo(*args, **kwargs)

            return get_json_result(data=False, message="Only MCP server owners or members with sufficient permissions can perform this action.", code=RetCode.OPERATING_ERROR)

        return wrapper

    return decorator


def build_permission_records_for_target(
    operator_id,
    operator_tenant_id,
    tenant_id,
    resource_type,
    resource_id,
    permission,
    target_type,
    target_id,
):
    """
    Build permission create/update/delete/changelog records for a single (resource, target) pair.

    Args:
        operator_id:        UserTenant.id of the operator performing the change.
        operator_tenant_id: tenant_id that the operator belongs to.
        tenant_id:          tenant scope of the permission.
        resource_type:      ResourceType value.
        resource_id:        ID of the resource (may be None/empty for tenant-wide permissions).
        permission:         Integer permission value to set.
        target_type:        PermissionTargetType – "member", "group", or "department".
        target_id:          ID of the member/group/department UserTenant record.

    Returns:
        tuple: (to_create, to_update, to_delete, changelog, not_found)
            - to_create  : dict ready for PermissionService.insert_many, or None
            - to_update  : model object ready for PermissionService.update_many, or None
            - to_delete  : model object (status set to INVALID) for update_many, or None
            - changelog  : dict ready for PermissionChangeLogService.insert_many, or None
            - not_found  : True when the target entity does not exist in the database
    """
    from api.db.services.user_service import UserTenantService

    to_create = None
    to_update = None
    to_delete = None
    changelog = None
    entity_id = None
    entity_field = None  # key to use in the permission record dict

    # ── resolve target entity ────────────────────────────────────────────────
    if target_type == PermissionTargetType.TARGET_MEMBER:
        entity = UserTenantService.filter_by_id(target_id)
        if not entity:
            return None, None, None, None, True
        entity_id = entity.id
        entity_field = "member_id"

        p = PermissionService.filter_by_member_and_tenant_id_with_resource_id(entity_id, tenant_id, resource_id=resource_id, resource_type=resource_type)

    elif target_type == PermissionTargetType.TARGET_GROUP:
        entity = GroupService.filter_by_id(target_id)
        if not entity:
            return None, None, None, None, True
        entity_id = entity.id
        entity_field = "group_id"

        p = PermissionService.filter_by_group_and_tenant_id_with_resource_id(entity_id, tenant_id, resource_id=resource_id, resource_type=resource_type)

    elif target_type == PermissionTargetType.TARGET_DEPARTMENT:
        entity = DepartmentService.filter_by_id(target_id)
        if not entity:
            return None, None, None, None, True
        entity_id = entity.id
        entity_field = "department_id"

        p = PermissionService.filter_by_department_and_tenant_id_with_resource_id(entity_id, tenant_id, resource_id=resource_id, resource_type=resource_type)

    else:
        # Unknown target_type – treat as not-found so the caller can skip gracefully
        return None, None, None, None, True

    # ── build update / create records ────────────────────────────────────────
    if p:
        changelog = dict(
            id=get_uuid(),
            tenant_id=operator_tenant_id,
            operator_id=operator_id,
            target_type=target_type,
            target_id=entity_id,
            resource_type=resource_type,
            resource_id=resource_id,
            old_permission=p.permission,
            new_permission=permission,
            action_type=PermissionActionType.ACTION_UPDATE,
        )
        setattr(p, "permission", permission)
        if p.permission == PermissionValue.PERMISSION_NULL.value:
            p.status = StatusEnum.INVALID.value
            changelog["action_type"] = PermissionActionType.ACTION_DELETE
            to_delete = p
        else:
            to_update = p
    else:
        # permission=0 with no existing record is a no-op: nothing to delete or create.
        if permission == PermissionValue.PERMISSION_NULL.value:
            return None, None, None, None, False

        to_create = {
            "id": get_uuid(),
            "resource_type": resource_type,
            "resource_id": resource_id if resource_id else None,
            "tenant_id": tenant_id,
            "permission": permission,
            entity_field: entity_id,
        }
        changelog = dict(
            id=get_uuid(),
            tenant_id=operator_tenant_id,
            operator_id=operator_id,
            target_type=target_type,
            target_id=entity_id,
            resource_type=resource_type,
            resource_id=resource_id,
            old_permission=PermissionValue.PERMISSION_NULL.value,
            new_permission=permission,
            action_type=PermissionActionType.ACTION_ADD,
        )

    return to_create, to_update, to_delete, changelog, False


def get_owner_id(tid, uid):
    return str(tid).rjust(32, "x") + str(uid).rjust(32, "_")


def tid_uid(owner_id):
    tid, uid = owner_id[:32], owner_id[32:]
    return tid.lstrip("_"), uid.lstrip("_")
