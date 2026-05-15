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
from common.metadata_utils import turn2jsonschema
from quart import request, g
import numpy as np

from api.db.services.connector_service import Connector2KbService
from api.db import PermissionValue, ResourceType
from api.db.services.document_service import DocumentService, queue_raptor_o_graphrag_tasks, queue_reembedding_dup_tasks

from api.db.services.llm_service import LLMBundle
from api.db.services.doc_metadata_service import DocMetadataService
from api.db.services.pipeline_operation_log_service import PipelineOperationLogService
from api.db.services.task_service import TaskService, GRAPH_RAPTOR_FAKE_DOC_ID
from api.db.services.user_service import UserTenantService
from api.db.joint_services.tenant_model_service import get_model_config_by_type_and_name, get_model_config_by_id
from api.utils.api_utils import (
    get_error_data_result,
    server_error_response,
    get_data_error_result,
    validate_request,
    get_request_json,
)
from api.db import VALID_FILE_TYPES
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.utils.api_utils import get_json_result
from api.utils.billing import check_resources
from api.utils.permission_utils import check_kb_permission, has_permission_for_member
from common.role_util import check_role_access, KB_API_ACTION_MAP, KB_ROLE_RESOURCE_TYPE

from rag.nlp import search
from rag.utils.redis_conn import REDIS_CONN
from common.constants import RetCode, PipelineTaskType, VALID_TASK_STATUS, LLMType
from common import settings
from api.common.priority_provider import get_tenant_priority
from api.apps import login_required, current_user


kb_role_guard = check_role_access(KB_API_ACTION_MAP, KB_ROLE_RESOURCE_TYPE)


"""
Deprecated, todo delete 
@manager.route('/create', methods=['post'])  # noqa: F821
@login_required
@kb_role_guard
@validate_request("name")
@check_resources(apps=1)
async def create():
    req = await get_request_json()
    create_dict = ensure_tenant_model_id_for_params(current_user.id, req)
    e, kb = KnowledgebaseService.create_with_name(
        name = create_dict.pop("name", None),
        tenant_id = current_user.id,
        parser_id = create_dict.pop("parser_id", None),
        **create_dict
    )

    if not e:
        return kb

    tenant_id = current_user.id
    current_user_id = current_user.id
    try:
        operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id, current_user_id)
        with DB.atomic():
            if not KnowledgebaseService.save(**kb):
                raise ValueError("KB creation failed")
            if not PermissionService.save(
                id=get_uuid(),
                member_id=operator.id,
                tenant_id=tenant_id,
                resource_type=ResourceType.KB,
                resource_id=kb["id"],
                permission=PermissionValue.PERMISSION_OWNER.value,
            ):
                raise ValueError("Permission creation failed")
            if not PermissionChangeLogService.save(
                id=get_uuid(),
                tenant_id=operator.tenant_id,
                operator_id=operator.id,
                target_type=PermissionTargetType.TARGET_MEMBER,
                target_id=operator.id,
                resource_type=ResourceType.KB,
                resource_id=kb["id"],
                old_permission=PermissionValue.PERMISSION_NULL.value,
                new_permission=PermissionValue.PERMISSION_OWNER.value,
                action_type=PermissionActionType.ACTION_ADD,
            ):
                raise ValueError("Permission change log creation failed")

        await asyncio.sleep(3)

        return get_json_result(data={"kb_id": kb["id"]})
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
    update_dict = ensure_tenant_model_id_for_params(current_user.id, req)
    if not isinstance(update_dict["name"], str):
        return get_data_error_result(message="Dataset name must be string.")
    if update_dict["name"].strip() == "":
        return get_data_error_result(message="Dataset name can't be empty.")
    if len(update_dict["name"].encode("utf-8")) > DATASET_NAME_LIMIT:
        return get_data_error_result(
            message=f"Dataset name length is {len(update_dict['name'])} which is large than {DATASET_NAME_LIMIT}")
    update_dict["name"] = update_dict["name"].strip()
    if "operator_permission" in req:
        req.pop("operator_permission", None)
    if settings.DOC_ENGINE_INFINITY:
        parser_id = update_dict.get("parser_id")
        if isinstance(parser_id, str) and parser_id.lower() == "tag":
            return get_json_result(
                code=RetCode.OPERATING_ERROR,
                message="The chunking method Tag has not been supported by Infinity yet.",
                data=False,
            )
        if "pagerank" in update_dict and update_dict["pagerank"] > 0:
            return get_json_result(
                code=RetCode.DATA_ERROR,
                message="'pagerank' can only be set when doc_engine is elasticsearch",
                data=False,
            )

    if not KnowledgebaseService.accessible4deletion(update_dict["kb_id"], current_user.id):
        return get_json_result(
            data=False,
            message='No authorization.',
            code=RetCode.AUTHENTICATION_ERROR
        )
    try:
        if not KnowledgebaseService.query(
                created_by=current_user.id, id=update_dict["kb_id"]):
            return get_json_result(
                data=False, message='Only owner of dataset authorized for this operation.',
                code=RetCode.OPERATING_ERROR)

        e, kb = KnowledgebaseService.get_by_id(update_dict["kb_id"])

        # Rename folder in FileService
        if e and update_dict["name"].lower() != kb.name.lower():
            FileService.filter_update(
                [
                    File.tenant_id == kb.tenant_id,
                    File.source_type == FileSource.KNOWLEDGEBASE,
                    File.type == "folder",
                    File.name == kb.name,
                ],
                {"name": update_dict["name"]},
            )

        if not e:
            return get_data_error_result(
                message="Can't find this dataset!")

        if update_dict["name"].lower() != kb.name.lower() \
                and len(
            KnowledgebaseService.query(name=update_dict["name"], tenant_id=current_user.id, status=StatusEnum.VALID.value)) >= 1:
            return get_data_error_result(
                message="Duplicated dataset name.")

        del update_dict["kb_id"]
        connectors = []
        if "connectors" in update_dict:
            connectors = update_dict["connectors"]
            del update_dict["connectors"]
        if not KnowledgebaseService.update_by_id(kb.id, update_dict):
            return get_data_error_result()

        if kb.pagerank != update_dict.get("pagerank", 0):
            if update_dict.get("pagerank", 0) > 0:
                await thread_pool_exec(
                    settings.docStoreConn.update,
                    {"kb_id": kb.id},
                    {PAGERANK_FLD: update_dict["pagerank"]},
                    search.index_name(kb.tenant_id),
                    kb.id,
                )
            else:
                # Elasticsearch requires PAGERANK_FLD be non-zero!
                await thread_pool_exec(
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
        kb.update(update_dict)
        kb["connectors"] = connectors

        return get_json_result(data=kb)
    except Exception as e:
        return server_error_response(e)
"""

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
    kb["parser_config"]["enable_metadata"] = req.get("enable_metadata", True)
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
        if kb["parser_config"].get("metadata"):
            kb["parser_config"]["metadata"] = turn2jsonschema(kb["parser_config"]["metadata"])

        for key in ["graphrag_task_finish_at", "raptor_task_finish_at", "mindmap_task_finish_at"]:
            if finish_at := kb.get(key):
                kb[key] = finish_at.strftime("%Y-%m-%d %H:%M:%S")
        return get_json_result(data=kb)
    except Exception as e:
        return server_error_response(e)

"""
Deprecated, todo delete
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

        kbs, total = KnowledgebaseService.get_unique_kbs_by_tenant_ids(tenant_ids, current_user.id, page_number, items_per_page, orderby, desc, keywords, parser_id)
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
    uid = current_user.id
    if not KnowledgebaseService.accessible4deletion(req["kb_id"], uid):
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
            created_by=uid, id=req["kb_id"])
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
                [
                    File.tenant_id == kbs[0].tenant_id,
                    File.source_type == FileSource.KNOWLEDGEBASE,
                    File.type == "folder",
                    File.name == kbs[0].name,
                ]
            )
            # Delete the table BEFORE deleting the database record
            for kb in kbs:
                try:
                    settings.docStoreConn.delete({"kb_id": kb.id}, search.index_name(kb.tenant_id), kb.id)
                    settings.docStoreConn.delete_idx(search.index_name(kb.tenant_id), kb.id)
                    logging.info(f"Dropped index for dataset {kb.id}")
                except Exception as e:
                    logging.error(f"Failed to drop index for dataset {kb.id}: {e}")

            if not KnowledgebaseService.delete_by_id(req["kb_id"]):
                return get_data_error_result(
                    message="Database error (Knowledgebase removal)!")
            for kb in kbs:
                if hasattr(settings.STORAGE_IMPL, 'remove_bucket'):
                    settings.STORAGE_IMPL.remove_bucket(kb.id)
            return get_json_result(data=True)

        return await thread_pool_exec(_rm_sync)
    except Exception as e:
        return server_error_response(e)
"""

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

"""
Deprecated, todo delete
@manager.route('/<kb_id>/knowledge_graph', methods=['GET'])  # noqa: F821
@login_required
@kb_role_guard
@check_kb_permission(permission=PermissionValue.PERMISSION_READ)
async def knowledge_graph(kb_id):
    if not KnowledgebaseService.accessible(kb_id, current_user.id):
        return get_json_result(
            data=False,
            message='No authorization.',
            code=RetCode.AUTHENTICATION_ERROR
        )
    _, kb = KnowledgebaseService.get_by_id(kb_id)
    req = {"kb_id": [kb_id], "knowledge_graph_kwd": ["graph"]}

    obj = {"graph": {}, "mind_map": {}}
    if not settings.docStoreConn.index_exist(search.index_name(kb.tenant_id), kb_id):
        return get_json_result(data=obj)
    sres = await settings.retriever.search(req, search.index_name(kb.tenant_id), [kb_id])
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
"""

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
    return get_json_result(data=DocMetadataService.get_flatted_meta_by_kbs(kb_ids))


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


"""
Deprecated, todo delete
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

    task_id = queue_raptor_o_graphrag_tasks(sample_doc_id=sample_document, ty="graphrag", priority=get_tenant_priority(current_user.id), fake_doc_id=GRAPH_RAPTOR_FAKE_DOC_ID, doc_ids=list(document_ids))

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

    task_id = queue_raptor_o_graphrag_tasks(sample_doc_id=sample_document, ty="raptor", priority=get_tenant_priority(current_user.id), fake_doc_id=GRAPH_RAPTOR_FAKE_DOC_ID, doc_ids=list(document_ids))

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
"""

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

    task_id = queue_raptor_o_graphrag_tasks(sample_doc_id=sample_document, ty="mindmap", priority=get_tenant_priority(current_user.id), fake_doc_id=GRAPH_RAPTOR_FAKE_DOC_ID, doc_ids=list(document_ids))

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
    if not pipeline_task_type or pipeline_task_type not in [PipelineTaskType.GRAPH_RAG, PipelineTaskType.RAPTOR, PipelineTaskType.MINDMAP, PipelineTaskType.CLONE]:
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
        case PipelineTaskType.CLONE:
            kb_task_id_field = "clone_task_id"
            task_id = kb.clone_task_id
            kb_task_finish_at = "clone_task_finish_at"
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
    import random
    import re
    from common.doc_store.doc_store_base import OrderByExpr

    def _guess_vec_field(src: dict) -> str | None:
        for k in src or {}:
            if k.endswith("_vec"):
                return k
        return None

    def _as_float_vec(v):
        if v is None:
            return []
        if isinstance(v, str):
            return [float(x) for x in v.split("\t") if x != ""]
        if isinstance(v, (list, tuple, np.ndarray)):
            return [float(x) for x in v]
        return []

    def _to_1d(x):
        a = np.asarray(x, dtype=np.float32)
        return a.reshape(-1)

    def _cos_sim(a, b, eps=1e-12):
        a = _to_1d(a)
        b = _to_1d(b)
        na = np.linalg.norm(a)
        nb = np.linalg.norm(b)
        if na < eps or nb < eps:
            return 0.0
        return float(np.dot(a, b) / (na * nb))

    def sample_random_chunks_with_vectors(
        docStoreConn,
        tenant_id: str,
        kb_id: str,
        n: int = 5,
        base_fields=("docnm_kwd","doc_id","content_with_weight","page_num_int","position_int","top_int"),
    ):
        index_nm = search.index_name(tenant_id)

        res0 = docStoreConn.search(
            select_fields=[], highlight_fields=[],
            condition={"kb_id": kb_id, "available_int": 1},
            match_expressions=[], order_by=OrderByExpr(),
            offset=0, limit=1,
            index_names=index_nm, knowledgebase_ids=[kb_id]
        )
        total = docStoreConn.get_total(res0)
        if total <= 0:
            return []

        n = min(n, total)
        offsets = sorted(random.sample(range(min(total,1000)), n))
        out = []

        for off in offsets:
            res1 = docStoreConn.search(
                select_fields=list(base_fields),
                highlight_fields=[],
                condition={"kb_id": kb_id, "available_int": 1},
                match_expressions=[], order_by=OrderByExpr(),
                offset=off, limit=1,
                index_names=index_nm, knowledgebase_ids=[kb_id]
            )
            ids = docStoreConn.get_doc_ids(res1)
            if not ids:
                continue

            cid = ids[0]
            full_doc = docStoreConn.get(cid, index_nm, [kb_id]) or {}
            vec_field = _guess_vec_field(full_doc)
            vec = _as_float_vec(full_doc.get(vec_field))

            out.append({
                "chunk_id": cid,
                "kb_id": kb_id,
                "doc_id": full_doc.get("doc_id"),
                "doc_name": full_doc.get("docnm_kwd"),
                "vector_field": vec_field,
                "vector_dim": len(vec),
                "vector": vec,
                "page_num_int": full_doc.get("page_num_int"),
                "position_int": full_doc.get("position_int"),
                "top_int": full_doc.get("top_int"),
                "content_with_weight": full_doc.get("content_with_weight") or "",
                "question_kwd": full_doc.get("question_kwd") or []
            })
        return out

    def _clean(s: str) -> str:
        s = re.sub(r"</?(table|td|caption|tr|th)( [^<>]{0,12})?>", " ", s or "")
        return s if s else "None"

    req = await get_request_json()
    kb_id = req.get("kb_id", "")
    tenant_embd_id = req.get("tenant_embd_id")
    embd_id = req.get("embd_id", "")

    n = int(req.get("check_num", 5))
    _, kb = KnowledgebaseService.get_by_id(kb_id)
    tenant_id = kb.tenant_id
    if tenant_embd_id:
        embd_model_config = get_model_config_by_id(tenant_embd_id)
    elif embd_id:
        embd_model_config = get_model_config_by_type_and_name(tenant_id, LLMType.EMBEDDING, embd_id)
    else:
        return get_error_data_result("`tenant_embd_id` or `embd_id` is required.")
    emb_mdl = LLMBundle(tenant_id, embd_model_config)
    samples = sample_random_chunks_with_vectors(settings.docStoreConn, tenant_id=tenant_id, kb_id=kb_id, n=n)

    results, eff_sims = [], []
    for ck in samples:
        title = ck.get("doc_name") or "Title"
        txt_in = "\n".join(ck.get("question_kwd") or []) or ck.get("content_with_weight") or ""
        txt_in = _clean(txt_in)
        if not txt_in:
            results.append({"chunk_id": ck["chunk_id"], "reason": "no_text"})
            continue

        if not ck.get("vector"):
            results.append({"chunk_id": ck["chunk_id"], "reason": "no_stored_vector"})
            continue

        try:
            v, _ = emb_mdl.encode([title, txt_in])
            assert len(v[1]) == len(ck["vector"]), f"The dimension ({len(v[1])}) of given embedding model is different from the original ({len(ck['vector'])})"
            sim_content = _cos_sim(v[1], ck["vector"])
            title_w = 0.1
            qv_mix = title_w * v[0] + (1 - title_w) * v[1]
            sim_mix = _cos_sim(qv_mix, ck["vector"])
            sim = sim_content
            mode = "content_only"
            if sim_mix > sim:
                sim = sim_mix
                mode = "title+content"
        except Exception as e:
            return get_error_data_result(message=f"Embedding failure. {e}")

        eff_sims.append(sim)
        results.append({
            "chunk_id": ck["chunk_id"],
            "doc_id": ck["doc_id"],
            "doc_name": ck["doc_name"],
            "vector_field": ck["vector_field"],
            "vector_dim": ck["vector_dim"],
            "cos_sim": round(sim, 6),
        })

    summary = {
        "kb_id": kb_id,
        "model": embd_id,
        "sampled": len(samples),
        "valid": len(eff_sims),
        "avg_cos_sim": round(float(np.mean(eff_sims)) if eff_sims else 0.0, 6),
        "min_cos_sim": round(float(np.min(eff_sims)) if eff_sims else 0.0, 6),
        "max_cos_sim": round(float(np.max(eff_sims)) if eff_sims else 0.0, 6),
        "match_mode": mode,
    }
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
    embd_id = req["embd_id"]
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

    task_id = queue_reembedding_dup_tasks(documents[0]["id"], ty="reembedding", priority=get_tenant_priority(current_user.id), embed_id=embd_id)

    if not KnowledgebaseService.update_by_id(kb.id, {"embed_task_id": task_id, "embd_id": embd_id}):
        logging.warning(f"Cannot save for data {kb.name}")

    return get_json_result(data={"embed_task_id": task_id})


@manager.route("/trace_embedding", methods=["GET"])  # noqa: F821
@login_required
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
@kb_role_guard
@check_resources(apps=1)
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
    target_name = req.get("name") or f"Copy of {nm}"
    e, kb = KnowledgebaseService.create_with_name(
        name=target_name,
        tenant_id=current_user.id,
        ** kb
    )
    if not e:
        return kb

    if not KnowledgebaseService.save(**kb):
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
        # Duplicating an empty knowledge base is still a valid operation.
        # The target KB has already been created successfully, so return success
        # without scheduling a clone task.
        return get_json_result(data={"clone_task_id": None, "kb_id": kb["id"]})

    task_id = queue_reembedding_dup_tasks(documents[0]["id"], ty="clone", priority=get_tenant_priority(current_user.id), target_kb_id=kb["id"])

    if not KnowledgebaseService.update_by_id(kb["id"], {"clone_task_id": task_id}):
        logging.warning(f"Cannot save for data {kb.name}")

    return get_json_result(data={"clone_task_id": task_id})


@manager.route("/trace_clone", methods=["GET"])  # noqa: F821
@login_required
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
