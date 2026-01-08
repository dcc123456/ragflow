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
import json
import logging
import asyncio

from quart import request, g

from api.db.services.connector_service import Connector2KbService
from api.db import PermissionActionType, PermissionTargetType, PermissionValue, ResourceType
from api.db.db_models import DB
from api.db.services.dialog_service import DialogService
from api.db.services.document_service import DocumentService, queue_raptor_o_graphrag_tasks, queue_reembedding_dup_tasks
from api.db.services.file2document_service import File2DocumentService
from api.db.services.file_service import FileService
from api.db.services.permission_service import PermissionChangeLogService, PermissionService
from api.db.services.pipeline_operation_log_service import PipelineOperationLogService
from api.db.services.task_service import TaskService, GRAPH_RAPTOR_FAKE_DOC_ID
from api.db.services.user_service import TenantService, UserTenantService
from api.utils.api_utils import get_error_data_result, server_error_response, get_data_error_result, validate_request, not_allowed_parameters, \
    get_request_json
from api.db import VALID_FILE_TYPES
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.db.db_models import File
from api.utils.api_utils import get_json_result
from api.utils.permission_utils import check_kb_permission, has_permission_for_member
from common.role_util import check_role_access, KB_API_ACTION_MAP, KB_ROLE_RESOURCE_TYPE
from common.misc_utils import get_uuid

from rag.nlp import search
from api.constants import DATASET_NAME_LIMIT
from rag.utils.redis_conn import REDIS_CONN
from common.constants import RetCode, PipelineTaskType, StatusEnum, VALID_TASK_STATUS, FileSource, PAGERANK_FLD
from common import settings
from api.apps import login_required, current_user


kb_role_guard = check_role_access(KB_API_ACTION_MAP, KB_ROLE_RESOURCE_TYPE)


@manager.route('/create', methods=['post'])  # noqa: F821
@login_required
@kb_role_guard
@validate_request("name")
async def create():
    req = await get_request_json()
    e, req = KnowledgebaseService.create_with_name(
        name = req.pop("name", None),
        tenant_id = current_user.id,
        parser_id = req.pop("parser_id", None),
        **req
    )

    if not e:
        return req

    tenant_id = current_user.id
    try:
        operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id, tenant_id)
        with DB.atomic():
            if not KnowledgebaseService.save(**req):
                raise ValueError("KB creation failed")
            if not PermissionService.save(
                id=get_uuid(), member_id=operator.id, tenant_id=tenant_id, resource_type=ResourceType.KB, resource_id=req["id"], permission=PermissionValue.PERMISSION_OWNER.value
            ):
                raise ValueError("Permission creation failed")
            if not PermissionChangeLogService.save(
                id=get_uuid(),
                tenant_id=operator.tenant_id,
                operator_id=operator.id,
                target_type=PermissionTargetType.TARGET_MEMBER,
                target_id=operator.id,
                resource_type=ResourceType.KB,
                resource_id=req["id"],
                old_permission=PermissionValue.PERMISSION_NULL.value,
                new_permission=PermissionValue.PERMISSION_OWNER.value,
                action_type=PermissionActionType.ACTION_ADD,
            ):
                raise ValueError("Permission change log creation failed")

        await asyncio.sleep(3)

        return get_json_result(data={"kb_id": req["id"]})
    except Exception as e:
        return server_error_response(e)


@manager.route('/update', methods=['post'])  # noqa: F821
@login_required
@validate_request("kb_id", "name", "description", "parser_id")
@not_allowed_parameters("id", "created_by", "create_time", "update_time", "create_date", "update_date", "created_by")
@kb_role_guard
@check_kb_permission(permission=PermissionValue.PERMISSION_WRITE)
async def update():
    req = await get_request_json()
    if not isinstance(req["name"], str):
        return get_data_error_result(message="Dataset name must be string.")
    if req["name"].strip() == "":
        return get_data_error_result(message="Dataset name can't be empty.")
    if len(req["name"].encode("utf-8")) > DATASET_NAME_LIMIT:
        return get_data_error_result(
            message=f"Dataset name length is {len(req['name'])} which is large than {DATASET_NAME_LIMIT}")
    req["name"] = req["name"].strip()
    if "operator_permission" in req:
        req.pop("operator_permission", None)

    try:
        if not KnowledgebaseService.query(
                created_by=current_user.id, id=req["kb_id"]):
            return get_json_result(
                data=False, message='Only owner of dataset authorized for this operation.',
                code=RetCode.OPERATING_ERROR)

        e, kb = KnowledgebaseService.get_by_id(req["kb_id"])

        # Rename folder in FileService
        if e and req["name"].lower() != kb.name.lower():
            FileService.filter_update(
                [
                    File.tenant_id == kb.tenant_id,
                    File.source_type == FileSource.KNOWLEDGEBASE,
                    File.type == "folder",
                    File.name == kb.name,
                ],
                {"name": req["name"]},
            )

        if not e:
            return get_data_error_result(
                message="Can't find this dataset!")

        if req["name"].lower() != kb.name.lower() \
                and len(
            KnowledgebaseService.query(name=req["name"], tenant_id=current_user.id, status=StatusEnum.VALID.value)) >= 1:
            return get_data_error_result(
                message="Duplicated dataset name.")

        del req["kb_id"]
        connectors = []
        if "connectors" in req:
            connectors = req["connectors"]
            del req["connectors"]
        if not KnowledgebaseService.update_by_id(kb.id, req):
            return get_data_error_result()

        if kb.pagerank != req.get("pagerank", 0):
            if req.get("pagerank", 0) > 0:
                await asyncio.to_thread(
                    settings.docStoreConn.update,
                    {"kb_id": kb.id},
                    {PAGERANK_FLD: req["pagerank"]},
                    search.index_name(kb.tenant_id),
                    kb.id,
                )
            else:
                # Elasticsearch requires PAGERANK_FLD be non-zero!
                await asyncio.to_thread(
                    settings.docStoreConn.update,
                    {"exists": PAGERANK_FLD},
                    {"remove": PAGERANK_FLD},
                    search.index_name(kb.tenant_id),
                    kb.id,
                )

        e, kb = KnowledgebaseService.get_by_id(kb.id)
        if not (e and kb):
            return get_data_error_result(message="Database error (Knowledgebase rename)!")
        errors = Connector2KbService.link_connectors(kb.id, [conn for conn in connectors], current_user.id)
        if errors:
            logging.error("Link KB errors: ", errors)
        kb = kb.to_dict()
        kb.update(req)
        kb["connectors"] = connectors

        return get_json_result(data=kb)
    except Exception as e:
        return server_error_response(e)


@manager.route('/update_metadata_setting', methods=['post'])  # noqa: F821
@login_required
@validate_request("kb_id", "metadata")
async def update_metadata_setting():
    req = await get_request_json()
    e, kb = KnowledgebaseService.get_by_id(req["kb_id"])
    if not e:
        return get_data_error_result(
            message="Database error (Knowledgebase rename)!")
    kb = kb.to_dict()
    kb["parser_config"]["metadata"] = req["metadata"]
    KnowledgebaseService.update_by_id(kb["id"], kb)
    return get_json_result(data=kb)


@manager.route('/detail', methods=['GET'])  # noqa: F821
@login_required
@kb_role_guard
@check_kb_permission(permission=PermissionValue.PERMISSION_READ)
def detail():
    kb_id = request.args["kb_id"]
    tenant_id = g.tenant_id

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
    if not operator:
        return get_data_error_result(message="Unrecognized identification.")

    try:
        tenants = UserTenantService.query(user_id=current_user.id)
        for tenant in tenants:
            if KnowledgebaseService.query(
                    tenant_id=tenant.tenant_id, id=kb_id):
                break
        else:
            return get_json_result(
                data=False, message='Only owner of dataset authorized for this operation.',
                code=RetCode.OPERATING_ERROR)
        kb = KnowledgebaseService.get_detail(kb_id)
        if not kb:
            return get_data_error_result(message="Can't find this knowledgebase!")

        permission = has_permission_for_member(operator_id=operator.id, tenant_id=tenant_id, resource_id=kb_id, resource_type=ResourceType.KB, permission=PermissionValue.PERMISSION_READ)
        if not permission[0]:
            kb["operator_permission"] = PermissionValue.PERMISSION_NULL.value
        else:
            kb["operator_permission"] = permission[2]

        kb["size"] = DocumentService.get_total_size_by_kb_id(kb_id=kb["id"], keywords="", run_status=[], types=[])
        kb["connectors"] = Connector2KbService.list_connectors(kb_id)

        for key in ["graphrag_task_finish_at", "raptor_task_finish_at", "mindmap_task_finish_at"]:
            if finish_at := kb.get(key):
                kb[key] = finish_at.strftime("%Y-%m-%d %H:%M:%S")
        return get_json_result(data=kb)
    except Exception as e:
        return server_error_response(e)


@manager.route('/list', methods=['POST'])  # noqa: F821
@login_required
@kb_role_guard
async def list_kbs():
    from api.db.services import UserService
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
    all_kbs = {}
    tenant_operator_map = {}
    try:
        if settings.ENABLE_ADMIN and UserService.is_admin(current_user.id):
            kbs, total = KnowledgebaseService.all_with_permission(page_number, items_per_page, orderby, desc, keywords, parser_id)
            for kb in kbs:
                kb["operator_permission"] = PermissionValue.PERMISSION_OWNER.value
            return get_json_result(data={"kbs": kbs, "total": total})

        tenants = TenantService.get_joined_tenants_by_user_id(current_user.id)
        tenant_ids = list({tenant["tenant_id"] for tenant in tenants} | {current_user.id})

        kbs, total = KnowledgebaseService.get_uniqune_kbs_by_tenant_ids(tenant_ids, page_number, items_per_page, orderby, desc, keywords, parser_id)
        for kb in kbs:
            kb_id = kb["id"]

            kb_tenant_id = kb["tenant_id"]
            if  kb_tenant_id == current_user.id:
                kb["operator_permission"] = PermissionValue.PERMISSION_OWNER.value
                all_kbs[kb_id] = kb
                continue

            operator_id = ""
            if kb_tenant_id in tenant_operator_map:
                operator_id = tenant_operator_map[kb_tenant_id]
            else:
                operator_id = UserTenantService.filter_by_tenant_and_user_id(kb_tenant_id, current_user.id)

            if not operator_id:
                total -= 1
                continue

            tenant_operator_map[kb_tenant_id] = operator_id
            has_permission, permission_type, highest_permission = has_permission_for_member(
                operator_id=operator_id,
                tenant_id=kb_tenant_id,
                resource_id=kb_id,
                resource_type=ResourceType.KB,
                permission=PermissionValue.PERMISSION_READ,
            )
            if not has_permission:
                total -= 1
                continue

            if kb_id in all_kbs:
                if highest_permission > all_kbs[kb_id]["operator_permission"]:
                    all_kbs[kb_id]["operator_permission"] = highest_permission
            else:
                kb["operator_permission"] = highest_permission
                all_kbs[kb_id] = kb

        all_kbs = list(all_kbs.values())

        return get_json_result(data={"kbs": all_kbs, "total": total})
    except Exception as e:
        return server_error_response(e)


@manager.route('/rm', methods=['post'])  # noqa: F821
@login_required
@kb_role_guard
@validate_request("kb_id")
async def rm():
    req = await get_request_json()
    if not KnowledgebaseService.accessible4deletion(req["kb_id"], current_user.id):
        return get_json_result(
            data=False,
            message='No authorization.',
            code=RetCode.AUTHENTICATION_ERROR
        )
    operator = UserTenantService.filter_by_tenant_and_user_id(current_user.id, current_user.id)
    if not operator:
        return get_json_result(data=False, message="Unrecognized identification.", code=RetCode.AUTHENTICATION_ERROR)
    try:
        kbs = KnowledgebaseService.query(
            created_by=current_user.id, id=req["kb_id"])
        if not kbs:
            return get_json_result(
                data=False, message='Only owner of dataset authorized for this operation.',
                code=RetCode.OPERATING_ERROR)

        def _rm_sync():
            for doc in DocumentService.query(kb_id=req["kb_id"]):
                if not DocumentService.remove_document(doc, kbs[0].tenant_id):
                    return get_data_error_result(
                        message="Database error (Document removal)!")
                f2d = File2DocumentService.get_by_document_id(doc.id)
                if f2d:
                    FileService.filter_delete([File.source_type == FileSource.KNOWLEDGEBASE, File.id == f2d[0].file_id])
                File2DocumentService.delete_by_document_id(doc.id)

            FileService.filter_delete(
                [File.source_type == FileSource.KNOWLEDGEBASE, File.type == "folder", File.name == kbs[0].name])
            if not KnowledgebaseService.delete_by_id(req["kb_id"]):
                return get_data_error_result(
                    message="Database error (Knowledgebase removal)!")
            for kb in kbs:
                settings.docStoreConn.delete({"kb_id": kb.id}, search.index_name(kb.tenant_id), kb.id)
                settings.docStoreConn.delete_idx(search.index_name(kb.tenant_id), kb.id)
                settings.STORAGE_IMPL.rm_bucket(kb.id)

            tenant_id = current_user.id
            with DB.atomic():
                permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(tenant_id=tenant_id, resource_id=req["kb_id"], resource_type=ResourceType.KB)
                PermissionService.delete(permission_model_list)

                PermissionChangeLogService.save(
                    id=get_uuid(),
                    tenant_id=operator.tenant_id,
                    operator_id=operator.id,
                    target_type=PermissionTargetType.TARGET_MEMBER,
                    target_id=operator.id,
                    resource_type=ResourceType.KB,
                    resource_id=req["kb_id"],
                    old_permission=PermissionValue.PERMISSION_OWNER.value,
                    new_permission=PermissionValue.PERMISSION_NULL.value,
                    action_type=PermissionActionType.ACTION_DELETE,
                )

            dialogs = DialogService.query(
                status=StatusEnum.VALID.value,
                tenant_id=current_user.id,
            )
            filtered_dialog_ids = []
            for dialog in dialogs:
                if req["kb_id"] in dialog.kb_ids:
                    filtered_dialog_ids.append(dialog.id)

            with DB.atomic():
                for dialog_id in filtered_dialog_ids:
                    dialog_permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(tenant_id=tenant_id, resource_id=dialog_id, resource_type=ResourceType.DIALOG)
                    PermissionService.delete(dialog_permission_model_list)

            return get_json_result(data=True)

        return await asyncio.to_thread(_rm_sync)
    except Exception as e:
        return server_error_response(e)


@manager.route('/<kb_id>/tags', methods=['GET'])  # noqa: F821
@login_required
@kb_role_guard
@check_kb_permission(permission=PermissionValue.PERMISSION_READ)
def list_tags(kb_id):
    if not KnowledgebaseService.accessible(kb_id, current_user.id):
        return get_json_result(
            data=False,
            message='No authorization.',
            code=RetCode.AUTHENTICATION_ERROR
        )

    tenants = UserTenantService.get_tenants_by_user_id(current_user.id)
    tags = []
    for tenant in tenants:
        tags += settings.retriever.all_tags(tenant["tenant_id"], [kb_id])
    return get_json_result(data=tags)


@manager.route('/tags', methods=['GET'])  # noqa: F821
@login_required
@kb_role_guard
def list_tags_from_kbs():
    kb_ids = request.args.get("kb_ids", "").split(",")
    for kb_id in kb_ids:
        user_tenants = UserTenantService.query(user_id=current_user.id)
        for user_tenant in user_tenants:
            if KnowledgebaseService.query(tenant_id=user_tenant.tenant_id, id=kb_id):
                if has_permission_for_member(operator_id=user_tenant.id, tenant_id=user_tenant.tenant_id, resource_id=kb_id, resource_type=ResourceType.KB, permission=PermissionValue.PERMISSION_READ)[
                    0
                ]:
                    break
        else:
            return get_json_result(data=False, message="No authorization", code=RetCode.AUTHENTICATION_ERROR)

    tenants = UserTenantService.get_tenants_by_user_id(current_user.id)
    tags = []
    for tenant in tenants:
        tags += settings.retriever.all_tags(tenant["tenant_id"], kb_ids)
    return get_json_result(data=tags)


@manager.route('/<kb_id>/rm_tags', methods=['POST'])  # noqa: F821
@login_required
@kb_role_guard
@check_kb_permission(permission=PermissionValue.PERMISSION_MANAGE)
async def rm_tags(kb_id):
    req = await get_request_json()
    if not KnowledgebaseService.accessible(kb_id, current_user.id):
        return get_json_result(
            data=False,
            message='No authorization.',
            code=RetCode.AUTHENTICATION_ERROR
        )
    e, kb = KnowledgebaseService.get_by_id(kb_id)

    for t in req["tags"]:
        settings.docStoreConn.update({"tag_kwd": t, "kb_id": [kb_id]},
                                     {"remove": {"tag_kwd": t}},
                                     search.index_name(kb.tenant_id),
                                     kb_id)
    return get_json_result(data=True)


@manager.route('/<kb_id>/rename_tag', methods=['POST'])  # noqa: F821
@login_required
@kb_role_guard
@check_kb_permission(permission=PermissionValue.PERMISSION_MANAGE)
async def rename_tags(kb_id):
    req = await get_request_json()
    e, kb = KnowledgebaseService.get_by_id(kb_id)

    settings.docStoreConn.update({"tag_kwd": req["from_tag"], "kb_id": [kb_id]},
                                     {"remove": {"tag_kwd": req["from_tag"].strip()}, "add": {"tag_kwd": req["to_tag"]}},
                                     search.index_name(kb.tenant_id),
                                     kb_id)
    return get_json_result(data=True)


@manager.route('/<kb_id>/knowledge_graph', methods=['GET'])  # noqa: F821
@login_required
@kb_role_guard
@check_kb_permission(permission=PermissionValue.PERMISSION_READ)
def knowledge_graph(kb_id):
    _, kb = KnowledgebaseService.get_by_id(kb_id)
    req = {"kb_id": [kb_id], "knowledge_graph_kwd": ["graph"]}

    obj = {"graph": {}, "mind_map": {}}
    if not settings.docStoreConn.index_exist(search.index_name(kb.tenant_id), kb_id):
        return get_json_result(data=obj)
    sres = settings.retriever.search(req, search.index_name(kb.tenant_id), [kb_id])
    if not len(sres.ids):
        return get_json_result(data=obj)

    for id in sres.ids[:1]:
        ty = sres.field[id]["knowledge_graph_kwd"]
        try:
            content_json = json.loads(sres.field[id]["content_with_weight"])
        except Exception:
            continue

        obj[ty] = content_json

    if "nodes" in obj["graph"]:
        obj["graph"]["nodes"] = sorted(obj["graph"]["nodes"], key=lambda x: x.get("pagerank", 0), reverse=True)[:256]
        if "edges" in obj["graph"]:
            node_id_set = { o["id"] for o in obj["graph"]["nodes"] }
            filtered_edges = [o for o in obj["graph"]["edges"] if o["source"] != o["target"] and o["source"] in node_id_set and o["target"] in node_id_set]
            obj["graph"]["edges"] = sorted(filtered_edges, key=lambda x: x.get("weight", 0), reverse=True)[:128]
    return get_json_result(data=obj)


@manager.route('/<kb_id>/knowledge_graph', methods=['DELETE'])  # noqa: F821
@login_required
@kb_role_guard
def delete_knowledge_graph(kb_id):
    if not KnowledgebaseService.accessible(kb_id, current_user.id):
        return get_json_result(
            data=False,
            message='No authorization.',
            code=RetCode.AUTHENTICATION_ERROR
        )
    _, kb = KnowledgebaseService.get_by_id(kb_id)
    settings.docStoreConn.delete({"knowledge_graph_kwd": ["graph", "subgraph", "entity", "relation"]}, search.index_name(kb.tenant_id), kb_id)

    return get_json_result(data=True)


@manager.route("/get_meta", methods=["GET"])  # noqa: F821
@login_required
@kb_role_guard
def get_meta():
    kb_ids = request.args.get("kb_ids", "").split(",")
    for kb_id in kb_ids:
        if not KnowledgebaseService.accessible(kb_id, current_user.id):
            return get_json_result(
                data=False,
                message='No authorization.',
                code=RetCode.AUTHENTICATION_ERROR
            )
    return get_json_result(data=DocumentService.get_meta_by_kbs(kb_ids))


@manager.route("/basic_info", methods=["GET"])  # noqa: F821
@login_required
@kb_role_guard
def get_basic_info():
    kb_id = request.args.get("kb_id", "")
    if not KnowledgebaseService.accessible(kb_id, current_user.id):
        return get_json_result(
            data=False,
            message='No authorization.',
            code=RetCode.AUTHENTICATION_ERROR
        )

    basic_info = DocumentService.knowledgebase_basic_info(kb_id)

    return get_json_result(data=basic_info)


@manager.route("/list_pipeline_logs", methods=["POST"])  # noqa: F821
@login_required
@kb_role_guard
async def list_pipeline_logs():
    kb_id = request.args.get("kb_id")
    if not kb_id:
        return get_json_result(data=False, message='Lack of "KB ID"', code=RetCode.ARGUMENT_ERROR)

    keywords = request.args.get("keywords", "")

    page_number = int(request.args.get("page", 0))
    items_per_page = int(request.args.get("page_size", 0))
    orderby = request.args.get("orderby", "create_time")
    if request.args.get("desc", "true").lower() == "false":
        desc = False
    else:
        desc = True
    create_date_from = request.args.get("create_date_from", "")
    create_date_to = request.args.get("create_date_to", "")
    if create_date_to > create_date_from:
        return get_data_error_result(message="Create data filter is abnormal.")

    req = await get_request_json()

    operation_status = req.get("operation_status", [])
    if operation_status:
        invalid_status = {s for s in operation_status if s not in VALID_TASK_STATUS}
        if invalid_status:
            return get_data_error_result(message=f"Invalid filter operation_status status conditions: {', '.join(invalid_status)}")

    types = req.get("types", [])
    if types:
        invalid_types = {t for t in types if t not in VALID_FILE_TYPES}
        if invalid_types:
            return get_data_error_result(message=f"Invalid filter conditions: {', '.join(invalid_types)} type{'s' if len(invalid_types) > 1 else ''}")

    suffix = req.get("suffix", [])

    try:
        logs, tol = PipelineOperationLogService.get_file_logs_by_kb_id(kb_id, page_number, items_per_page, orderby, desc, keywords, operation_status, types, suffix, create_date_from, create_date_to)
        return get_json_result(data={"total": tol, "logs": logs})
    except Exception as e:
        return server_error_response(e)


@manager.route("/list_pipeline_dataset_logs", methods=["POST"])  # noqa: F821
@login_required
@kb_role_guard
async def list_pipeline_dataset_logs():
    kb_id = request.args.get("kb_id")
    if not kb_id:
        return get_json_result(data=False, message='Lack of "KB ID"', code=RetCode.ARGUMENT_ERROR)

    page_number = int(request.args.get("page", 0))
    items_per_page = int(request.args.get("page_size", 0))
    orderby = request.args.get("orderby", "create_time")
    if request.args.get("desc", "true").lower() == "false":
        desc = False
    else:
        desc = True
    create_date_from = request.args.get("create_date_from", "")
    create_date_to = request.args.get("create_date_to", "")
    if create_date_to > create_date_from:
        return get_data_error_result(message="Create data filter is abnormal.")

    req = await get_request_json()

    operation_status = req.get("operation_status", [])
    if operation_status:
        invalid_status = {s for s in operation_status if s not in VALID_TASK_STATUS}
        if invalid_status:
            return get_data_error_result(message=f"Invalid filter operation_status status conditions: {', '.join(invalid_status)}")

    try:
        logs, tol = PipelineOperationLogService.get_dataset_logs_by_kb_id(kb_id, page_number, items_per_page, orderby, desc, operation_status, create_date_from, create_date_to)
        return get_json_result(data={"total": tol, "logs": logs})
    except Exception as e:
        return server_error_response(e)


@manager.route("/delete_pipeline_logs", methods=["POST"])  # noqa: F821
@login_required
@kb_role_guard
async def delete_pipeline_logs():
    kb_id = request.args.get("kb_id")
    if not kb_id:
        return get_json_result(data=False, message='Lack of "KB ID"', code=RetCode.ARGUMENT_ERROR)

    req = await get_request_json()
    log_ids = req.get("log_ids", [])

    PipelineOperationLogService.delete_by_ids(log_ids)

    return get_json_result(data=True)


@manager.route("/pipeline_log_detail", methods=["GET"])  # noqa: F821
@login_required
@kb_role_guard
def pipeline_log_detail():
    log_id = request.args.get("log_id")
    if not log_id:
        return get_json_result(data=False, message='Lack of "Pipeline log ID"', code=RetCode.ARGUMENT_ERROR)

    ok, log = PipelineOperationLogService.get_by_id(log_id)
    if not ok:
        return get_data_error_result(message="Invalid pipeline log ID")

    return get_json_result(data=log.to_dict())


@manager.route("/run_graphrag", methods=["POST"])  # noqa: F821
@login_required
@kb_role_guard
async def run_graphrag():
    req = await get_request_json()

    kb_id = req.get("kb_id", "")
    if not kb_id:
        return get_error_data_result(message='Lack of "KB ID"')

    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok:
        return get_error_data_result(message="Invalid Knowledgebase ID")

    task_id = kb.graphrag_task_id
    if task_id:
        ok, task = TaskService.get_by_id(task_id)
        if not ok:
            logging.warning(f"A valid GraphRAG task id is expected for kb {kb_id}")

        if task and task.progress not in [-1, 1]:
            return get_error_data_result(message=f"Task {task_id} in progress with status {task.progress}. A Graph Task is already running.")

    documents, _ = DocumentService.get_by_kb_id(
        kb_id=kb_id,
        page_number=0,
        items_per_page=0,
        orderby="create_time",
        desc=False,
        keywords="",
        run_status=[],
        types=[],
        suffix=[],
    )
    if not documents:
        return get_error_data_result(message=f"No documents in Knowledgebase {kb_id}")

    sample_document = documents[0]
    document_ids = [document["id"] for document in documents]

    task_id = queue_raptor_o_graphrag_tasks(sample_doc_id=sample_document, ty="graphrag", priority=0, fake_doc_id=GRAPH_RAPTOR_FAKE_DOC_ID, doc_ids=list(document_ids))

    if not KnowledgebaseService.update_by_id(kb.id, {"graphrag_task_id": task_id}):
        logging.warning(f"Cannot save graphrag_task_id for kb {kb_id}")

    return get_json_result(data={"graphrag_task_id": task_id})


@manager.route("/trace_graphrag", methods=["GET"])  # noqa: F821
@login_required
@kb_role_guard
def trace_graphrag():
    kb_id = request.args.get("kb_id", "")
    if not kb_id:
        return get_error_data_result(message='Lack of "KB ID"')

    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok:
        return get_error_data_result(message="Invalid Knowledgebase ID")

    task_id = kb.graphrag_task_id
    if not task_id:
        return get_json_result(data={})

    ok, task = TaskService.get_by_id(task_id)
    if not ok:
        return get_json_result(data={})

    return get_json_result(data=task.to_dict())


@manager.route("/run_raptor", methods=["POST"])  # noqa: F821
@login_required
@kb_role_guard
async def run_raptor():
    req = await get_request_json()

    kb_id = req.get("kb_id", "")
    if not kb_id:
        return get_error_data_result(message='Lack of "KB ID"')

    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok:
        return get_error_data_result(message="Invalid Knowledgebase ID")

    task_id = kb.raptor_task_id
    if task_id:
        ok, task = TaskService.get_by_id(task_id)
        if not ok:
            logging.warning(f"A valid RAPTOR task id is expected for kb {kb_id}")

        if task and task.progress not in [-1, 1]:
            return get_error_data_result(message=f"Task {task_id} in progress with status {task.progress}. A RAPTOR Task is already running.")

    documents, _ = DocumentService.get_by_kb_id(
        kb_id=kb_id,
        page_number=0,
        items_per_page=0,
        orderby="create_time",
        desc=False,
        keywords="",
        run_status=[],
        types=[],
        suffix=[],
    )
    if not documents:
        return get_error_data_result(message=f"No documents in Knowledgebase {kb_id}")

    sample_document = documents[0]
    document_ids = [document["id"] for document in documents]

    task_id = queue_raptor_o_graphrag_tasks(sample_doc_id=sample_document, ty="raptor", priority=0, fake_doc_id=GRAPH_RAPTOR_FAKE_DOC_ID, doc_ids=list(document_ids))

    if not KnowledgebaseService.update_by_id(kb.id, {"raptor_task_id": task_id}):
        logging.warning(f"Cannot save raptor_task_id for kb {kb_id}")

    return get_json_result(data={"raptor_task_id": task_id})


@manager.route("/trace_raptor", methods=["GET"])  # noqa: F821
@login_required
@kb_role_guard
def trace_raptor():
    kb_id = request.args.get("kb_id", "")
    if not kb_id:
        return get_error_data_result(message='Lack of "KB ID"')

    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok:
        return get_error_data_result(message="Invalid Knowledgebase ID")

    task_id = kb.raptor_task_id
    if not task_id:
        return get_json_result(data={})

    ok, task = TaskService.get_by_id(task_id)
    if not ok:
        return get_error_data_result(message="RAPTOR Task Not Found or Error Occurred")

    return get_json_result(data=task.to_dict())


@manager.route("/run_mindmap", methods=["POST"])  # noqa: F821
@login_required
@kb_role_guard
async def run_mindmap():
    req = await get_request_json()

    kb_id = req.get("kb_id", "")
    if not kb_id:
        return get_error_data_result(message='Lack of "KB ID"')

    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok:
        return get_error_data_result(message="Invalid Knowledgebase ID")

    task_id = kb.mindmap_task_id
    if task_id:
        ok, task = TaskService.get_by_id(task_id)
        if not ok:
            logging.warning(f"A valid Mindmap task id is expected for kb {kb_id}")

        if task and task.progress not in [-1, 1]:
            return get_error_data_result(message=f"Task {task_id} in progress with status {task.progress}. A Mindmap Task is already running.")

    documents, _ = DocumentService.get_by_kb_id(
        kb_id=kb_id,
        page_number=0,
        items_per_page=0,
        orderby="create_time",
        desc=False,
        keywords="",
        run_status=[],
        types=[],
        suffix=[],
    )
    if not documents:
        return get_error_data_result(message=f"No documents in Knowledgebase {kb_id}")

    sample_document = documents[0]
    document_ids = [document["id"] for document in documents]

    task_id = queue_raptor_o_graphrag_tasks(sample_doc_id=sample_document, ty="mindmap", priority=0, fake_doc_id=GRAPH_RAPTOR_FAKE_DOC_ID, doc_ids=list(document_ids))

    if not KnowledgebaseService.update_by_id(kb.id, {"mindmap_task_id": task_id}):
        logging.warning(f"Cannot save mindmap_task_id for kb {kb_id}")

    return get_json_result(data={"mindmap_task_id": task_id})


@manager.route("/trace_mindmap", methods=["GET"])  # noqa: F821
@login_required
@kb_role_guard
def trace_mindmap():
    kb_id = request.args.get("kb_id", "")
    if not kb_id:
        return get_error_data_result(message='Lack of "KB ID"')

    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok:
        return get_error_data_result(message="Invalid Knowledgebase ID")

    task_id = kb.mindmap_task_id
    if not task_id:
        return get_json_result(data={})

    ok, task = TaskService.get_by_id(task_id)
    if not ok:
        return get_error_data_result(message="Mindmap Task Not Found or Error Occurred")

    return get_json_result(data=task.to_dict())


@manager.route("/unbind_task", methods=["DELETE"])  # noqa: F821
@login_required
@kb_role_guard
def delete_kb_task():
    kb_id = request.args.get("kb_id", "")
    if not kb_id:
        return get_error_data_result(message='Lack of "KB ID"')
    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok:
        return get_json_result(data=True)

    pipeline_task_type = request.args.get("pipeline_task_type", "")
    if not pipeline_task_type or pipeline_task_type not in [PipelineTaskType.GRAPH_RAG, PipelineTaskType.RAPTOR, PipelineTaskType.MINDMAP]:
        return get_error_data_result(message="Invalid task type")

    def cancel_task(task_id):
        REDIS_CONN.set(f"{task_id}-cancel", "x")

    kb_task_id_field: str = ""
    kb_task_finish_at: str = ""
    match pipeline_task_type:
        case PipelineTaskType.GRAPH_RAG:
            kb_task_id_field = "graphrag_task_id"
            task_id = kb.graphrag_task_id
            kb_task_finish_at = "graphrag_task_finish_at"
            cancel_task(task_id)
            settings.docStoreConn.delete({"knowledge_graph_kwd": ["graph", "subgraph", "entity", "relation"]}, search.index_name(kb.tenant_id), kb_id)
        case PipelineTaskType.RAPTOR:
            kb_task_id_field = "raptor_task_id"
            task_id = kb.raptor_task_id
            kb_task_finish_at = "raptor_task_finish_at"
            cancel_task(task_id)
            settings.docStoreConn.delete({"raptor_kwd": ["raptor"]}, search.index_name(kb.tenant_id), kb_id)
        case PipelineTaskType.MINDMAP:
            kb_task_id_field = "mindmap_task_id"
            task_id = kb.mindmap_task_id
            kb_task_finish_at = "mindmap_task_finish_at"
            cancel_task(task_id)
        case _:
            return get_error_data_result(message="Internal Error: Invalid task type")


    ok = KnowledgebaseService.update_by_id(kb_id, {kb_task_id_field: "", kb_task_finish_at: None})
    if not ok:
        return server_error_response(f"Internal error: cannot delete task {pipeline_task_type}")

    return get_json_result(data=True)

@manager.route("/check_embedding", methods=["post"])  # noqa: F821
@login_required
@kb_role_guard
async def check_embedding():
    req = await get_request_json()
    kb_id = req.get("kb_id", "")
    embd_id = req.get("embd_id", "")
    try:
        summary = KnowledgebaseService.check_embedding(kb_id, embd_id, int(req.get("check_num", 5)))
    except Exception as e:
        return get_error_data_result(message=f"Embedding failure. {e}")

    if summary["avg_cos_sim"] > 0.9:
        KnowledgebaseService.update_by_id(kb_id, {"embd_id": embd_id})
        return get_json_result(data={"summary": summary, "results": []})
    return get_json_result(code=RetCode.NOT_EFFECTIVE, message="Embedding model switch failed: the average similarity between old and new vectors is below 0.9, indicating incompatible vector spaces.", data={"summary": summary})


@manager.route("/switch_embedding", methods=["POST"])  # noqa: F821
@login_required
@validate_request("kb_id", "embd_id")
@kb_role_guard
async def switch_embedding():
    req = await get_request_json()

    kb_id = req.get("kb_id", "")
    embd_id = req["embed_id"]
    if not kb_id:
        return get_error_data_result(message='Lack of "KB ID"')

    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok:
        return get_error_data_result(message="Invalid Knowledgebase ID")

    task_id = kb.embed_task_id
    if task_id:
        ok, task = TaskService.get_by_id(task_id)
        if not ok:
            logging.warning(f"A valid re-embedding task id is expected for dateset `{kb.name}`")

        if task and task.progress not in [-1, 1]:
            return get_error_data_result(message=f"Re-embedding task in progress with status {task.progress}. A re-embedding task is still running.")

    summary = KnowledgebaseService.check_embedding(kb_id, embd_id)
    if summary["avg_cos_sim"] > 0.9:
        KnowledgebaseService.update_by_id(kb_id, {"embd_id": embd_id})
        return get_json_result(data=True)

    documents, _ = DocumentService.get_by_kb_id(
        kb_id=kb_id,
        page_number=0,
        items_per_page=0,
        orderby="create_time",
        desc=False,
        keywords="",
        run_status=[],
        types=[],
        suffix=[],
    )
    if not documents:
        return get_error_data_result(message=f"No documents in Knowledgebase {kb_id}")

    task_id = queue_reembedding_dup_tasks(documents[0]["id"], ty="reembedding", priority=0, embed_id=embd_id)

    if not KnowledgebaseService.update_by_id(kb.id, {"embed_task_id": task_id, "embd_id": embd_id}):
        logging.warning(f"Cannot save for data {kb.name}")

    return get_json_result(data={"embed_task_id": task_id})


@manager.route("/trace_embedding", methods=["GET"])  # noqa: F821
@login_required
@validate_request("kb_id")
@kb_role_guard
def trace_embedding():
    kb_id = request.args.get("kb_id", "")
    if not kb_id:
        return get_error_data_result(message='Lack of "KB ID"')

    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok:
        return get_error_data_result(message="Invalid Knowledgebase ID")

    task_id = kb.embed_task_id
    if not task_id:
        return get_json_result(data={})

    ok, task = TaskService.get_by_id(task_id)
    if not ok:
        return get_json_result(data={})

    return get_json_result(data=task.to_dict())


@manager.route("/clone", methods=["POST"])  # noqa: F821
@login_required
@validate_request("kb_id")
@kb_role_guard
async def clone():
    req = await get_request_json()

    kb_id = req.get("kb_id", "")
    if not kb_id:
        return get_error_data_result(message='Lack of "KB ID"')

    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok:
        return get_error_data_result(message="Invalid Knowledgebase ID")

    task_id = kb.clone_task_id
    if task_id:
        ok, task = TaskService.get_by_id(task_id)
        if not ok:
            logging.warning(f"A valid re-embedding task id is expected for dateset `{kb.name}`")

        if task and task.progress not in [-1, 1]:
            return get_error_data_result(message=f"Re-embedding task in progress with status {task.progress}. A re-embedding task is still running.")

    kb = kb.to_dict()
    nm = kb.pop("name")
    kb.pop("tenant_id")
    kb.pop("id")
    e, kb = KnowledgebaseService.create_with_name(
        name=f"Copy of {nm}",
        tenant_id=current_user.id,
        ** kb
    )
    if not e:
        return kb

    if not KnowledgebaseService.save(**req):
        raise ValueError("KB creation failed")

    documents, _ = DocumentService.get_by_kb_id(
        kb_id=kb_id,
        page_number=0,
        items_per_page=0,
        orderby="create_time",
        desc=False,
        keywords="",
        run_status=[],
        types=[],
        suffix=[],
    )
    if not documents:
        return get_error_data_result(message=f"No documents in Knowledgebase {kb_id}")

    task_id = queue_reembedding_dup_tasks(documents[0]["id"], ty="clone", priority=0, target_kb_id=kb["id"])

    if not KnowledgebaseService.update_by_id(kb["id"], {"clone_task_id": task_id}):
        logging.warning(f"Cannot save for data {kb.name}")

    return get_json_result(data={"clone_task_id": task_id})


@manager.route("/trace_clone", methods=["GET"])  # noqa: F821
@login_required
@validate_request("kb_id")
@kb_role_guard
def trace_clone():
    kb_id = request.args.get("kb_id", "")
    if not kb_id:
        return get_error_data_result(message='Lack of "KB ID"')

    ok, kb = KnowledgebaseService.get_by_id(kb_id)
    if not ok:
        return get_error_data_result(message="Invalid Knowledgebase ID")

    task_id = kb.clone_task_id
    if not task_id:
        return get_json_result(data={})

    ok, task = TaskService.get_by_id(task_id)
    if not ok:
        return get_json_result(data={})

    return get_json_result(data=task.to_dict())
