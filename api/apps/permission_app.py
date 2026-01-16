import logging
from api.db.services.dialog_service import DialogService
from quart import request
from api.apps import login_required, current_user
from api.db import VALID_RESOURCE_TYPES, ActionEnum, PermissionActionType, PermissionTargetType, PermissionValue, ResourceType, ResourceTypeEnum
from api.db.db_models import DB
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.db.services.document_service import DocumentService
from api.db.services.tenant_llm_service import TenantLLMService
from api.db.services.permission_service import PermissionChangeLogService, PermissionService
from api.db.services.role_service import RoleResourceService
from api.db.services.team_service import DepartmentMemberService, DepartmentService, GroupMemberService, GroupService
from api.db.services.user_service import UserTenantService
from api.utils.api_utils import get_data_error_result, get_json_result, get_request_json, server_error_response, validate_request
from api.utils.permission_utils import has_permission_for_member, is_valid_permission, wrap_permission_info
from common.misc_utils import get_uuid
from common.constants import StatusEnum


@manager.route("/update", methods=["PUT"])  # noqa: F821
@login_required
@validate_request("tenant_id", "permission", "resource_type")
async def update_permission():
    """
    Update permission entries

    Request Body:
        {
            "permission": 4,
            "resource_type": "team", "kb", "llm", "dialog" or "document"
            "resource_id": "67c47bd8f2e078c384fe4a72",
            "tenant_id": "c4049de0f27a11efa69751e139332ced",

            "member_list": [
                "67bed779548836545cdf37b5",
                ...
            ],
            "group_list": [
                "67bed779548836545cdf37b5",
                "67bed99cf57aa5b4c309e305",
            ],
            "department_list": [
                "2958ddbafb3a11ef99932387352663ce"
                ...
            ],
        }

    Returns:
        JSON: Success message confirming update
    """
    req = await get_request_json()
    tenant_id = req.get("tenant_id")
    if not tenant_id:
        return get_data_error_result(message="Missing required field `tenant_id`.")
    permission = req.get("permission")
    if permission < PermissionValue.PERMISSION_NULL.value or permission > PermissionValue.PERMISSION_OWNER.value:
        return get_data_error_result(message="Missing required field `permission`.")
    if not is_valid_permission(permission):
        return get_data_error_result(message="Invalid permission.")
    resource_type = req.get("resource_type")
    if not resource_type:
        return get_data_error_result(message="Missing required field `resource_type`.")
    if resource_type not in VALID_RESOURCE_TYPES:
        return get_data_error_result(message="Invalid resource type.")

    resource_ids = req.get("resource_ids", [])
    if isinstance(resource_ids, str):
        resource_ids = [rid.strip() for rid in resource_ids.split(",") if rid.strip()]

    if resource_type in (ResourceType.TEAM, ResourceType.DOCUMENT) and not resource_ids:
        return get_data_error_result(message="Missing required valid field `resource_ids`.")

    member_list = req.get("member_list", [])
    group_list = req.get("group_list", [])
    department_list = req.get("department_list", [])
    if not any([member_list, group_list, department_list]):
        return get_data_error_result(message="Missing required one of fields: `group_list`, `member_list`, `department_list`.")

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id, current_user.id)
    if not operator:
        return get_data_error_result(message="Unrecognized identification.")

    llm_factories = set()
    if resource_type == ResourceType.LLM:
        llms = TenantLLMService.get_my_llms_group_by_factory(tenant_id=tenant_id)
        llm_factories = {llm.get("llm_factory") for llm in llms}

    role_resource_map = {
        ResourceType.KB: ResourceTypeEnum.DATASET.value,
        ResourceType.DIALOG: ResourceTypeEnum.CHAT.value,
        ResourceType.DOCUMENT: ResourceTypeEnum.DATASET.value,
    }
    role_id = getattr(current_user, "role_id", None)
    target_resource_value = role_resource_map.get(resource_type)
    if role_id and target_resource_value:
        role_permissions = RoleResourceService.get_by_role_id(role_id) or []
        action_value = 0
        for role_permission in role_permissions:
            if role_permission.get("resource_type") == target_resource_value:
                action_value = role_permission.get("action", 0)
                break
        if not (action_value & ActionEnum.SHARE.value):
            return get_data_error_result(message="Role has no share permission for this resource.")

    # handle
    permission_to_create = []
    permission_to_update = []
    permission_to_delete = []
    permission_not_updated = []
    permission_changelog_to_create = []
    entity_info_list = {key: value for key, value in {"members": member_list, "groups": group_list, "departments": department_list}.items() if value}
    for resource_id in resource_ids:
        if resource_id:
            if resource_type == ResourceType.KB:
                kb = KnowledgebaseService.filter_by_id_and_tenant_id(tenant_id=tenant_id, kb_id=resource_id)
                if not kb:
                    return get_data_error_result(message=f"Resource KB {resource_id} is not available.")
            elif resource_type == ResourceType.LLM:
                if resource_id not in llm_factories:
                    return get_data_error_result(message=f"Resource LLM {resource_id} is not available.")
            elif resource_type == ResourceType.DIALOG:
                dialog = DialogService.query(id=resource_id, status=StatusEnum.VALID.value)
                if not dialog:
                    return get_data_error_result(message=f"Resource Dialog {resource_id} is not available.")
            elif resource_type == ResourceType.DOCUMENT:
                document_tenant_id = DocumentService.get_tenant_id(resource_id)
                if not document_tenant_id or document_tenant_id != tenant_id:
                    return get_data_error_result(message=f"Resource Document {resource_id} is not available.")
                document_kb_id = DocumentService.get_knowledgebase_id(resource_id)
                if not document_kb_id:
                    return get_data_error_result(message=f"Resource Document {resource_id} is not available.")
            else:
                return get_data_error_result(message="Un-supported resource type.")

        has_manage_permission = has_permission_for_member(operator.id, tenant_id, resource_id, resource_type=resource_type, permission=PermissionValue.PERMISSION_MANAGE)[0]
        if not has_manage_permission and resource_type == ResourceType.DOCUMENT and resource_id:
            document_kb_id = DocumentService.get_knowledgebase_id(resource_id)
            if document_kb_id:
                has_manage_permission = has_permission_for_member(
                    operator.id,
                    tenant_id,
                    document_kb_id,
                    resource_type=ResourceType.KB,
                    permission=PermissionValue.PERMISSION_MANAGE,
                )[0]
        if not (operator.tenant_id == current_user.id or has_manage_permission):
            return get_data_error_result(message="Permission denied.")

        for entity_key, entity_list in entity_info_list.items():
            for entity in entity_list:
                if entity_key == "members":
                    member = UserTenantService.filter_by_id(entity)
                    if not member:
                        permission_not_updated.append(entity)
                        continue

                    if resource_id:
                        p = PermissionService.filter_by_member_and_tenant_id_with_resource_id(member.id, tenant_id, resource_id=resource_id, resource_type=resource_type)
                    else:
                        p = PermissionService.filter_by_member_and_tenant_id(member.id, tenant_id)
                    if p:
                        member_permission_changelog = dict(
                            id=get_uuid(),
                            tenant_id=operator.tenant_id,
                            operator_id=operator.id,
                            target_type=PermissionTargetType.TARGET_MEMBER,
                            target_id=member.id,
                            resource_type=resource_type,
                            resource_id=resource_id,
                            old_permission=p.permission,
                            new_permission=permission,
                            action_type=PermissionActionType.ACTION_UPDATE,
                        )
                        setattr(p, "permission", permission)

                        if p.permission == PermissionValue.PERMISSION_NULL.value:
                            p.status = StatusEnum.INVALID.value
                            member_permission_changelog["action_type"] = PermissionActionType.ACTION_DELETE
                            permission_to_delete.append(p)
                        else:
                            permission_to_update.append(p)
                        permission_changelog_to_create.append(member_permission_changelog)
                        continue

                    member_permission = {
                        "id": get_uuid(),
                        "resource_type": resource_type,
                        "resource_id": resource_id if resource_id else None,
                        "tenant_id": tenant_id,
                        "permission": permission,
                        "member_id": member.id,
                    }
                    permission_to_create.append(member_permission)

                    member_permission_changelog = dict(
                        id=get_uuid(),
                        tenant_id=operator.tenant_id,
                        operator_id=operator.id,
                        target_type=PermissionTargetType.TARGET_MEMBER,
                        target_id=member.id,
                        resource_type=resource_type,
                        resource_id=resource_id,
                        old_permission=PermissionValue.PERMISSION_NULL.value,
                        new_permission=permission,
                        action_type=PermissionActionType.ACTION_ADD,
                    )
                    permission_changelog_to_create.append(member_permission_changelog)

                elif entity_key == "groups":
                    group = GroupService.filter_by_id(entity)
                    if not group:
                        permission_not_updated.append(entity)
                        continue

                    if resource_id:
                        p = PermissionService.filter_by_group_and_tenant_id_with_resource_id(group.id, tenant_id, resource_id=resource_id, resource_type=resource_type)
                    else:
                        p = PermissionService.filter_by_group_and_tenant_id(group.id, tenant_id)
                    if p:
                        group_permission_changelog = dict(
                            id=get_uuid(),
                            tenant_id=operator.tenant_id,
                            operator_id=operator.id,
                            target_type=PermissionTargetType.TARGET_GROUP,
                            target_id=group.id,
                            resource_type=resource_type,
                            resource_id=resource_id,
                            old_permission=p.permission,
                            new_permission=permission,
                            action_type=PermissionActionType.ACTION_UPDATE,
                        )
                        setattr(p, "permission", permission)

                        if p.permission == PermissionValue.PERMISSION_NULL.value:
                            p.status = StatusEnum.INVALID.value
                            group_permission_changelog["action_type"] = PermissionActionType.ACTION_DELETE
                            permission_to_delete.append(p)
                        else:
                            permission_to_update.append(p)
                        permission_changelog_to_create.append(group_permission_changelog)
                        continue

                    group_permission = {
                        "id": get_uuid(),
                        "resource_type": resource_type,
                        "resource_id": resource_id if resource_id else None,
                        "tenant_id": tenant_id,
                        "permission": permission,
                        "group_id": group.id,
                    }
                    permission_to_create.append(group_permission)

                    group_permission_changelog = dict(
                        id=get_uuid(),
                        tenant_id=operator.tenant_id,
                        operator_id=operator.id,
                        target_type=PermissionTargetType.TARGET_GROUP,
                        target_id=group.id,
                        resource_type=resource_type,
                        resource_id=resource_id,
                        old_permission=PermissionValue.PERMISSION_NULL.value,
                        new_permission=permission,
                        action_type=PermissionActionType.ACTION_ADD,
                    )
                    permission_changelog_to_create.append(group_permission_changelog)

                elif entity_key == "departments":
                    department = DepartmentService.filter_by_id(entity)
                    if not department:
                        permission_not_updated.append(entity)
                        continue

                    if resource_id:
                        p = PermissionService.filter_by_department_and_tenant_id_with_resource_id(department.id, tenant_id, resource_id=resource_id, resource_type=resource_type)
                    else:
                        p = PermissionService.filter_by_department_and_tenant_id(department.id, tenant_id)
                    if p:
                        department_permission_changelog = dict(
                            id=get_uuid(),
                            tenant_id=operator.tenant_id,
                            operator_id=operator.id,
                            target_type=PermissionTargetType.TARGET_DEPARTMENT,
                            target_id=department.id,
                            resource_type=resource_type,
                            resource_id=resource_id,
                            old_permission=p.permission,
                            new_permission=permission,
                            action_type=PermissionActionType.ACTION_UPDATE,
                        )
                        setattr(p, "permission", permission)

                        if p.permission == PermissionValue.PERMISSION_NULL.value:
                            p.status = StatusEnum.INVALID.value
                            department_permission_changelog["action_type"] = PermissionActionType.ACTION_DELETE
                            permission_to_delete.append(p)
                        else:
                            permission_to_update.append(p)
                        permission_changelog_to_create.append(department_permission_changelog)
                        continue

                    department_permission = {
                        "id": get_uuid(),
                        "resource_type": resource_type,
                        "resource_id": resource_id if resource_id else None,
                        "tenant_id": tenant_id,
                        "permission": permission,
                        "department_id": department.id,
                    }
                    permission_to_create.append(department_permission)

                    department_permission_changelog = dict(
                        id=get_uuid(),
                        tenant_id=operator.tenant_id,
                        operator_id=operator.id,
                        target_type=PermissionTargetType.TARGET_DEPARTMENT,
                        target_id=department.id,
                        resource_type=resource_type,
                        resource_id=resource_id,
                        old_permission=PermissionValue.PERMISSION_NULL.value,
                        new_permission=permission,
                        action_type=PermissionActionType.ACTION_ADD,
                    )
                    permission_changelog_to_create.append(department_permission_changelog)
    try:
        filtered_dialog_ids = []
        if permission_to_delete:
            dialogs = DialogService.query(
                status=StatusEnum.VALID.value,
                tenant_id=tenant_id,
            )
            deleted_resource_ids = {item.resource_id for item in permission_to_delete if item.resource_id}

            if resource_type == ResourceType.KB:
                for dialog in dialogs:
                    if any(resource_id in (dialog.kb_ids or []) for resource_id in deleted_resource_ids):
                        filtered_dialog_ids.append(dialog.id)

            elif resource_type == ResourceType.LLM:
                for dialog in dialogs:
                    if any(resource_id in (dialog.llm_id or []) for resource_id in deleted_resource_ids):
                        filtered_dialog_ids.append(dialog.id)

            elif resource_type == ResourceType.DIALOG:
                # need to do nothing
                pass

        with DB.atomic():
            if permission_to_create:
                PermissionService.insert_many(permission_to_create)
            if permission_to_update:
                PermissionService.update_many(permission_to_update)
            if permission_changelog_to_create:
                PermissionChangeLogService.insert_many(permission_changelog_to_create)
            if permission_to_delete:
                PermissionService.update_many(permission_to_delete)

            for dialog_id in filtered_dialog_ids:
                dialog_permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(tenant_id=tenant_id, resource_id=dialog_id, resource_type=ResourceType.DIALOG)
                PermissionService.delete(dialog_permission_model_list)

        return get_json_result(data=True)
    except Exception as e:
        return server_error_response(e)


@manager.route("/list", methods=["POST"])  # noqa: F821
@login_required
async def list_permissions():
    req = await get_request_json()
    tenant_id = req.get("tenant_id")
    resource_type = req.get("resource_type")
    resource_ids = req.get("resource_ids") or []

    if not all([tenant_id, resource_type]):
        return get_data_error_result(message="Missing required field `tenant_id`, `resource_type`.")
    if resource_type not in VALID_RESOURCE_TYPES:
        return get_data_error_result(message="Invalid resource type.")
    if not resource_ids:
        return get_data_error_result(message="Missing required field `resource_ids`.")

    resource_ids = list(dict.fromkeys(resource_ids))

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id, current_user.id)
    if not operator:
        return get_data_error_result(message="Unrecognized identification.")

    if resource_type == ResourceType.KB:
        for kb_id in resource_ids:
            kb = KnowledgebaseService.filter_by_id_and_tenant_id(tenant_id=tenant_id, kb_id=kb_id)
            if not kb:
                return get_data_error_result(message="Resource is not available.")
            if not (operator.tenant_id == current_user.id or has_permission_for_member(operator.id, tenant_id, kb_id, resource_type=resource_type, permission=PermissionValue.PERMISSION_MANAGE)[0]):
                return get_data_error_result(message="Permission denied.")
    elif resource_type == ResourceType.LLM:
        llms = TenantLLMService.get_my_llms_group_by_factory(tenant_id=tenant_id)
        llm_factories = {llm.get("llm_factory") for llm in llms}
        for llm_factory in resource_ids:
            if llm_factory not in llm_factories:
                return get_data_error_result(message="Resource is not available.")
            if not (operator.tenant_id == current_user.id or has_permission_for_member(operator.id, tenant_id, llm_factory, resource_type=resource_type, permission=PermissionValue.PERMISSION_MANAGE)[0]):
                return get_data_error_result(message="Permission denied.")
    elif resource_type == ResourceType.DOCUMENT:
        for document_id in resource_ids:
            document_tenant_id = DocumentService.get_tenant_id(document_id)
            if not document_tenant_id or document_tenant_id != tenant_id:
                return get_data_error_result(message="Resource is not available.")
            document_kb_id = DocumentService.get_knowledgebase_id(document_id)
            has_manage_permission = has_permission_for_member(
                operator.id,
                tenant_id,
                document_id,
                resource_type=resource_type,
                permission=PermissionValue.PERMISSION_MANAGE,
            )[0]
            if not has_manage_permission and document_kb_id:
                has_manage_permission = has_permission_for_member(
                    operator.id,
                    tenant_id,
                    document_kb_id,
                    resource_type=ResourceType.KB,
                    permission=PermissionValue.PERMISSION_MANAGE,
                )[0]
            if not (operator.tenant_id == current_user.id or has_manage_permission):
                return get_data_error_result(message="Permission denied.")

    permissions = PermissionService.get_permissions_by_tenant_and_resource_ids_with_info(tenant_id=tenant_id, resource_ids=resource_ids, resource_type=resource_type)
    permissions = wrap_permission_info(permissions)
    return get_json_result(data=permissions)


@manager.route("/share_dialog", methods=["PUT"])  # noqa: F821
@login_required
@validate_request("tenant_id", "permission", "resource_type", "kbs")  # noqa: F821
async def share_dialog():
    """
    Sharing dialog with permission

    Request Body:
        {
            "permission": READ or MANAGE,
            "resource_type": "dialog" # dialog only
            "resource_id": "67c47bd8f2e078c384fe4a72",
            "kbs": ["67c47bd8f2e078c384fe4a72"]
            "llm_factory": "factory_name",
            "tenant_id": "c4049de0f27a11efa69751e139332ced",

            "member_list": [
                "67bed779548836545cdf37b5",
                ...
            ],
            "group_list": [
                "67bed779548836545cdf37b5",
                "67bed99cf57aa5b4c309e305",
            ],
            "department_list": [
                "2958ddbafb3a11ef99932387352663ce"
                ...
            ],
        }

    Returns:
        JSON: Success message confirming update
    """
    req = await request.get_json()
    tenant_id = req.get("tenant_id")
    if not tenant_id:
        return get_data_error_result(message="Missing required field `tenant_id`.")
    permission = req.get("permission")
    if not permission:
        return get_data_error_result(message="Missing required field `permission`.")
    if not is_valid_permission(permission):
        return get_data_error_result(message="Invalid permission.")
    resource_type = req.get("resource_type")
    if not resource_type:
        return get_data_error_result(message="Missing required field `resource_type`.")
    if resource_type != ResourceType.DIALOG:
        return get_data_error_result(message="Invalid resource type.")

    resource_id = req.get("resource_id")
    if resource_type == ResourceType.TEAM and not resource_id:
        return get_data_error_result(message="Missing required valid field `resource_id`.")
    kbs = req.get("kbs", [])

    member_list = req.get("member_list", [])
    group_list = req.get("group_list", [])
    department_list = req.get("department_list", [])
    if not any([member_list, group_list, department_list]):
        return get_data_error_result(message="Missing required one of fields: `group_list`, `member_list`, `department_list`.")

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id, current_user.id)
    if not operator:
        return get_data_error_result(message="Unrecognized identification.")

    available_kbs = {}
    for kb_id in set(kbs):
        kb = KnowledgebaseService.filter_by_id_and_tenant_id(tenant_id=tenant_id, kb_id=kb_id)
        if kb:
            available_kbs[kb_id] = {"name": kb.name}

    dialog = DialogService.query(id=resource_id, status=StatusEnum.VALID.value)
    if not dialog:
        return get_data_error_result(message=f"Resource dialog {dialog.name} is not available.")

    if not (operator.tenant_id == current_user.id or has_permission_for_member(operator.id, tenant_id, resource_id, resource_type=resource_type, permission=PermissionValue.PERMISSION_MANAGE)[0]):
        return get_data_error_result(message="Permission denied.")

    role_id = getattr(current_user, "role_id", None)
    if role_id:
        role_permissions = RoleResourceService.get_by_role_id(role_id) or []
        action_value = 0
        for role_permission in role_permissions:
            if role_permission.get("resource_type") == ResourceTypeEnum.CHAT.value:
                action_value = role_permission.get("action", 0)
                break
        if not (action_value & ActionEnum.SHARE.value):
            return get_data_error_result(message="Role has no share permission for dialog resource.")

    available_groups = []
    available_departments = []
    for group_id in group_list:
        group = GroupService.filter_by_id(group_id)
        if not group:
            logging.warning(f"Group {group_id} not found.")
            continue
        available_groups.append(group_id)
    for department_id in department_list:
        department = DepartmentService.filter_by_id(department_id)
        if not department:
            logging.warning(f"Department {department_id} not found.")
            continue
        available_departments.append(department_id)
    def collect_member_ids(member_source_func=None, *args):
        if member_source_func:
            members = member_source_func(*args) if args else []
            return list(dict.fromkeys(member.member_id for member in members))
        return []

    all_member_ids = set()
    for group_id in available_groups:
        member_ids = collect_member_ids(GroupMemberService.get_by_group_id, group_id)
        all_member_ids.update(member_ids)
    for department_id in available_departments:
        member_ids = collect_member_ids(DepartmentMemberService.get_by_department_id, department_id)
        all_member_ids.update(member_ids)
    if member_list:
        all_member_ids.update(member_list)

    need_to_share = list(all_member_ids)
    print(f"{need_to_share=}", flush=True)

    permission_to_create = []
    permission_to_update = []
    permission_changelog_to_create = []
    for need_to_share_id in need_to_share:
        p = PermissionService.filter_by_member_and_tenant_id_with_resource_id(need_to_share_id, tenant_id, resource_id=resource_id, resource_type=resource_type)
        if p:
            setattr(p, "permission", permission)
            permission_to_update.append(p)

            member_permission_changelog = dict(
                id=get_uuid(),
                tenant_id=operator.tenant_id,
                operator_id=operator.id,
                target_type=PermissionTargetType.TARGET_MEMBER,
                target_id=need_to_share_id,
                resource_type=resource_type,
                resource_id=resource_id,
                old_permission=p.permission,
                new_permission=permission,
                action_type=PermissionActionType.ACTION_UPDATE,
            )

            if p.permission == PermissionValue.PERMISSION_NULL.value:
                member_permission_changelog["action_type"] = PermissionActionType.ACTION_DELETE
            permission_changelog_to_create.append(member_permission_changelog)
            continue

        member_permission = {
            "id": get_uuid(),
            "resource_type": resource_type,
            "resource_id": resource_id if resource_id else None,
            "tenant_id": tenant_id,
            "permission": permission,
            "member_id": need_to_share_id,
        }
        permission_to_create.append(member_permission)

        member_permission_changelog = dict(
            id=get_uuid(),
            tenant_id=operator.tenant_id,
            operator_id=operator.id,
            target_type=PermissionTargetType.TARGET_MEMBER,
            target_id=need_to_share_id,
            resource_type=resource_type,
            resource_id=resource_id,
            old_permission=PermissionValue.PERMISSION_NULL.value,
            new_permission=permission,
            action_type=PermissionActionType.ACTION_ADD,
        )
        permission_changelog_to_create.append(member_permission_changelog)

    try:
        with DB.atomic():
            if permission_to_create:
                PermissionService.insert_many(permission_to_create)
            if permission_to_update:
                PermissionService.update_many(permission_to_update)
            if permission_changelog_to_create:
                PermissionChangeLogService.insert_many(permission_changelog_to_create)
        return get_json_result(
            data={
                "successful": need_to_share,
            }
        )
    except Exception as e:
        return server_error_response(e)
