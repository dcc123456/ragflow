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

import logging
from quart import request, g
from api.db.db_models import DB
from api.db.services.conversation_service import ConversationService
from api.db.services.permission_service import PermissionChangeLogService, PermissionService
from api.utils.permission_utils import has_permission_for_member
from api.db.services import duplicate_name

from common.constants import StatusEnum
from api.db import PermissionActionType, PermissionTargetType, PermissionValue, ResourceType
from api.db.services.dialog_service import DialogService
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.db.services.tenant_llm_service import TenantLLMService
from api.db.services.user_service import TenantService, UserTenantService
from api.utils.api_utils import get_data_error_result, get_json_result, get_request_json, server_error_response, validate_request
from api.utils.tenant_utils import ensure_tenant_model_id_for_params
from common.misc_utils import get_uuid
from common.constants import RetCode
from api.utils.billing import check_resources
from api.apps import login_required, current_user
from api.utils.permission_utils import check_dialog_permission
from common.role_util import check_role_access, DIALOG_API_ACTION_MAP, DIALOG_ROLE_RESOURCE_TYPE

dialog_role_guard = check_role_access(DIALOG_API_ACTION_MAP, DIALOG_ROLE_RESOURCE_TYPE)


@manager.route('/set', methods=['POST'])  # noqa: F821
@validate_request("prompt_config")
@login_required
@dialog_role_guard
@check_dialog_permission(PermissionValue.PERMISSION_MANAGE)
@check_resources(apps=1)
async def set_dialog():
    req = g.req_data
    dialog_info = ensure_tenant_model_id_for_params(current_user.id, req)
    dialog_id = dialog_info.get("dialog_id", "")
    is_create = not dialog_id
    name = dialog_info.get("name", "New Dialog")
    if not isinstance(name, str):
        return get_data_error_result(message="Dialog name must be string.")
    if name.strip() == "":
        return get_data_error_result(message="Dialog name can't be empty.")
    if len(name.encode("utf-8")) > 255:
        return get_data_error_result(message=f"Dialog name length is {len(name)} which is larger than 255")

    name = name.strip()
    if is_create:
        # only for chat creating
        existing_names = {
            d.name.casefold()
            for d in DialogService.query(tenant_id=current_user.id, status=StatusEnum.VALID.value)
            if d.name
        }
        if name.casefold() in existing_names:
            def _name_exists(name: str, **_kwargs) -> bool:
                return name.casefold() in existing_names

            name = duplicate_name(_name_exists, name=name)

    description = dialog_info.get("description", "A helpful dialog")
    icon = dialog_info.get("icon", "")
    top_n = dialog_info.get("top_n", 6)
    top_k = dialog_info.get("top_k", 1024)
    rerank_id = dialog_info.get("rerank_id", "")
    if not rerank_id:
        dialog_info["rerank_id"] = ""
    similarity_threshold = dialog_info.get("similarity_threshold", 0.1)
    vector_similarity_weight = dialog_info.get("vector_similarity_weight", 0.3)
    llm_setting = dialog_info.get("llm_setting", {})
    meta_data_filter = dialog_info.get("meta_data_filter", {})
    prompt_config = dialog_info["prompt_config"]

    # Set default parameters for datasets with knowledge retrieval
    # All datasets with {knowledge} in system prompt need "knowledge" parameter to enable retrieval
    kb_ids = dialog_info.get("kb_ids", [])
    parameters = prompt_config.get("parameters")
    logging.debug(f"set_dialog: kb_ids={kb_ids}, parameters={parameters}, is_create={not is_create}")
    # Check if parameters is missing, None, or empty list
    if kb_ids and not parameters:
        # Check if system prompt uses {knowledge} placeholder
        if "{knowledge}" in prompt_config.get("system", ""):
            # Set default parameters for any dataset with knowledge placeholder
            prompt_config["parameters"] = [{"key": "knowledge", "optional": False}]
            logging.debug(f"Set default parameters for datasets with knowledge placeholder: {kb_ids}")

    if not is_create:
        # only for chat updating
        if not dialog_info.get("kb_ids", []) and not prompt_config.get("tavily_api_key") and "{knowledge}" in prompt_config.get("system", ""):
            return get_data_error_result(message="Please remove `{knowledge}` in system prompt since no dataset / Tavily used here.")

    for p in prompt_config.get("parameters", []):
        if p["optional"]:
            continue
        if prompt_config.get("system", "").find("{%s}" % p["key"]) < 0:
            return get_data_error_result(
                message="Parameter '{}' is not used".format(p["key"]))

    if "operator_permission" in req:
        req.pop("operator_permission", None)
    try:
        e, tenant = TenantService.get_by_id(current_user.id)
        if not e:
            return get_data_error_result(message="Tenant not found!")
        kbs = KnowledgebaseService.get_by_ids(dialog_info.get("kb_ids", []))
        embd_ids = [TenantLLMService.split_model_name_and_factory(kb.embd_id)[0] for kb in kbs]  # remove vendor suffix for comparison
        embd_count = len(set(embd_ids))
        if embd_count > 1:
            return get_data_error_result(message=f'Datasets use different embedding models: {[kb.embd_id for kb in kbs]}"')

        llm_id = dialog_info.get("llm_id", tenant.llm_id)
        tenant_llm_id = dialog_info.get("tenant_llm_id", tenant.tenant_llm_id)
        if not dialog_id:
            tenant_id = current_user.id
            operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id, tenant_id)
            operator_id = operator.id
            dia = {
                "id": get_uuid(),
                "tenant_id": current_user.id,
                "name": name,
                "kb_ids": dialog_info.get("kb_ids", []),
                "description": description,
                "llm_id": llm_id,
                "tenant_llm_id": tenant_llm_id,
                "llm_setting": llm_setting,
                "prompt_config": prompt_config,
                "meta_data_filter": meta_data_filter,
                "top_n": top_n,
                "top_k": top_k,
                "rerank_id": rerank_id,
                "tenant_rerank_id": dialog_info.get("tenant_rerank_id", 0),
                "similarity_threshold": similarity_threshold,
                "vector_similarity_weight": vector_similarity_weight,
                "icon": icon
            }
            with DB.atomic():
                if not DialogService.save(**dia):
                    return get_data_error_result(message="Fail to new a dialog!")
                if not PermissionService.save(
                    id=get_uuid(), member_id=operator_id, tenant_id=tenant_id, resource_type=ResourceType.DIALOG, resource_id=dia["id"], permission=PermissionValue.PERMISSION_OWNER.value
                ):
                    raise ValueError("Permission creation failed")
                if not PermissionChangeLogService.save(
                    id=get_uuid(),
                    tenant_id=tenant_id,
                    operator_id=operator_id,
                    target_type=PermissionTargetType.TARGET_MEMBER,
                    target_id=operator_id,
                    resource_type=ResourceType.DIALOG,
                    resource_id=dia["id"],
                    old_permission=PermissionValue.PERMISSION_NULL.value,
                    new_permission=PermissionValue.PERMISSION_OWNER.value,
                    action_type=PermissionActionType.ACTION_ADD,
                ):
                    raise ValueError("Permission change log creation failed")

            return get_json_result(data=dia)
        else:
            del dialog_info["dialog_id"]
            if "kb_names" in dialog_info:
                del dialog_info["kb_names"]
            if not DialogService.update_by_id(dialog_id, dialog_info):
                return get_data_error_result(message="Dialog not found!")
            e, dia = DialogService.get_by_id(dialog_id)
            if not e:
                return get_data_error_result(message="Fail to update a dialog!")
            dia = dia.to_dict()
            dia.update(dialog_info)
            dia["kb_ids"], dia["kb_names"] = get_kb_names(dia["kb_ids"])
            return get_json_result(data=dia)
    except Exception as e:
        return server_error_response(e)


@manager.route('/get', methods=['GET'])  # noqa: F821
@login_required
@dialog_role_guard
@check_dialog_permission(PermissionValue.PERMISSION_READ)
def get():
    dialog_id = request.args["dialog_id"]
    try:
        e, dia = DialogService.get_by_id(dialog_id)
        if not e:
            return get_data_error_result(message="Dialog not found!")
        dia = dia.to_dict()
        dia["kb_ids"], dia["kb_names"] = get_kb_names(dia["kb_ids"])
        dia["operator_permission"] = g.operator_permission

        return get_json_result(data=dia)
    except Exception as e:
        return server_error_response(e)


def get_kb_names(kb_ids):
    ids, nms = [], []
    for kid in kb_ids:
        e, kb = KnowledgebaseService.get_by_id(kid)
        if not e or kb.status != StatusEnum.VALID.value:
            continue
        ids.append(kid)
        nms.append(kb.name)
    return ids, nms


@manager.route('/list', methods=['GET'])  # noqa: F821
@login_required
@dialog_role_guard
def list_dialogs():
    try:
        dialogs = []
        joined_tenants = TenantService.get_joined_tenants_by_user_id(current_user.id)
        joined_tenant_ids = [tenant["tenant_id"] for tenant in joined_tenants]
        joined_tenant_ids.append(current_user.id)
        for tenant_id in joined_tenant_ids:
            if tenant_id == current_user.id:
                diags = DialogService.query(tenant_id=tenant_id, status=StatusEnum.VALID.value, reverse=True, order_by=DialogService.model.create_time)
                diags = [d.to_dict() for d in diags]
                for d in diags:
                    d["kb_ids"], d["kb_names"] = get_kb_names(d["kb_ids"])
                    d["operator_permission"] = PermissionValue.PERMISSION_OWNER.value
                dialogs.extend(diags)
            else:
                member = UserTenantService.filter_by_tenant_and_user_id(tenant_id, current_user.id)

                diags = DialogService.query(tenant_id=tenant_id, status=StatusEnum.VALID.value, reverse=True, order_by=DialogService.model.create_time)
                for diag in diags:
                    permission = has_permission_for_member(
                        operator_id=getattr(member, "id", None),
                        tenant_id=tenant_id,
                        resource_id=diag.id,
                        resource_type=ResourceType.DIALOG,
                        permission=PermissionValue.PERMISSION_READ,
                    )
                    if permission[0]:
                        diag = diag.to_dict()
                        diag["kb_ids"], diag["kb_names"] = get_kb_names(diag["kb_ids"])
                        diag["operator_permission"] = permission[2]
                        dialogs.append(diag)
        return get_json_result(data=dialogs)
    except Exception as e:
        return server_error_response(e)


@manager.route('/next', methods=['POST'])  # noqa: F821
@login_required
@dialog_role_guard
async def list_dialogs_next():
    args = request.args
    keywords = args.get("keywords", "")
    page_number = int(args.get("page", 0))
    items_per_page = int(args.get("page_size", 0))
    parser_id = args.get("parser_id")
    orderby = args.get("orderby", "create_time")
    if args.get("desc", "true").lower() == "false":
        desc = False
    else:
        desc = True

    tenant_member_memo = {}
    req = await get_request_json()
    owner_ids = req.get("owner_ids", [])
    try:
        if not owner_ids:
            tenants = TenantService.get_joined_tenants_by_user_id(current_user.id)
            tenants = [tenant["tenant_id"] for tenant in tenants]
            dialogs, total = DialogService.get_by_tenant_ids(
                tenants, current_user.id, page_number,
                items_per_page, orderby, desc, keywords, parser_id)
        else:
            tenants = owner_ids
            dialogs, total = DialogService.get_by_tenant_ids(
                tenants, current_user.id, 0,
                0, orderby, desc, keywords, parser_id)
            dialogs = [dialog for dialog in dialogs if dialog["tenant_id"] in tenants]
            total = len(dialogs)

        dialog_list = []
        for dialog in dialogs:
            if dialog["tenant_id"] == current_user.id:
                dialog["kb_ids"], dialog["kb_names"] = get_kb_names(dialog["kb_ids"])
                dialog["operator_permission"] = PermissionValue.PERMISSION_OWNER.value
                dialog_list.append(dialog)
            else:
                member_id = tenant_member_memo.get(dialog["tenant_id"])
                if not member_id:
                    member = UserTenantService.filter_by_tenant_and_user_id(dialog["tenant_id"], current_user.id)
                    member_id = getattr(member, "id", None)
                    tenant_member_memo[dialog["tenant_id"]] = member_id

                permission = has_permission_for_member(
                    operator_id=member_id,
                    tenant_id=dialog["tenant_id"],
                    resource_id=dialog["id"],
                    resource_type=ResourceType.DIALOG,
                    permission=PermissionValue.PERMISSION_READ,
                )
                if permission[0]:
                    dialog["kb_ids"], dialog["kb_names"] = get_kb_names(dialog["kb_ids"])
                    dialog["operator_permission"] = permission[2]
                    dialog_list.append(dialog)

        if page_number and items_per_page:
            dialog_list = dialog_list[(page_number-1)*items_per_page:page_number*items_per_page]

        return get_json_result(data={"dialogs": dialog_list, "total": total})

    except Exception as e:
        return server_error_response(e)


@manager.route('/rm', methods=['POST'])  # noqa: F821
@login_required
@dialog_role_guard
@validate_request("dialog_ids")
@check_dialog_permission(permission=PermissionValue.PERMISSION_OWNER)
async def rm():
    req = await get_request_json()
    dialog_list=[]
    tenants = UserTenantService.query(user_id=current_user.id)
    try:
        for id in req["dialog_ids"]:
            tenant_id = ""
            for tenant in tenants:
                if DialogService.query(tenant_id=tenant.tenant_id, id=id):
                    tenant_id = tenant.tenant_id
                    break
            else:
                return get_json_result(data=False, message="Only owner of dialog authorized for this operation.", code=RetCode.OPERATING_ERROR)
            dialog_list.append({"id": id, "status": StatusEnum.INVALID.value, "tenant_id": tenant_id})
            ConversationService.remove_by(id)

        with DB.atomic():
            DialogService.update_many_by_id(dialog_list)

        for dialog in dialog_list:
            operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=dialog["tenant_id"], user_id=current_user.id)
            if not operator:
                continue
            with DB.atomic():
                permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(tenant_id=dialog["tenant_id"], resource_id=dialog["id"], resource_type=ResourceType.DIALOG)
                PermissionService.delete(permission_model_list)
                if not PermissionChangeLogService.save(
                    id=get_uuid(),
                    tenant_id=operator.tenant_id,
                    operator_id=operator.id,
                    target_type=PermissionTargetType.TARGET_MEMBER,
                    target_id=operator.id,
                    resource_type=ResourceType.DIALOG,
                    resource_id=dialog["id"],
                    old_permission=PermissionValue.PERMISSION_OWNER.value,
                    new_permission=PermissionValue.PERMISSION_NULL.value,
                    action_type=PermissionActionType.ACTION_DELETE,
                ):
                    raise ValueError("Permission change log creation failed")
        return get_json_result(data=True)
    except Exception as e:
        return server_error_response(e)
