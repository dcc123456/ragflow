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
import os
import time

from flask import g, request
from flask_login import current_user, login_required

from api import settings
from api.constants import DATASET_NAME_LIMIT
from api.db import FileSource, PermissionActionType, PermissionTargetType, PermissionValue, ResourceType, StatusEnum
from api.db.db_models import DB, File
from api.db.services import duplicate_name
from api.db.services.dialog_service import DialogService
from api.db.services.document_service import DocumentService
from api.db.services.file2document_service import File2DocumentService
from api.db.services.file_service import FileService
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.db.services.permission_service import PermissionChangeLogService, PermissionService
from api.db.services.user_service import TenantService, UserTenantService
from api.utils.api_utils import server_error_response, get_data_error_result, validate_request, not_allowed_parameters
from api.utils import get_uuid
from api.utils.api_utils import get_json_result
from api.utils.permission_utils import check_kb_permission, has_permission_for_member

from rag.nlp import search
from rag.settings import PAGERANK_FLD
from rag.utils.storage_factory import STORAGE_IMPL


@manager.route('/create', methods=['post'])  # noqa: F821
@login_required
@validate_request("name")
def create():
    req = request.json
    dataset_name = req["name"]
    if not isinstance(dataset_name, str):
        return get_data_error_result(message="Dataset name must be string.")
    if dataset_name == "":
        return get_data_error_result(message="Dataset name can't be empty.")
    if len(dataset_name.encode("utf-8")) >= DATASET_NAME_LIMIT:
        return get_data_error_result(
            message=f"Dataset name length is {len(dataset_name)} which is large than {DATASET_NAME_LIMIT}")

    dataset_name = dataset_name.strip()
    dataset_name = duplicate_name(
        KnowledgebaseService.query,
        name=dataset_name,
        tenant_id=current_user.id,
        status=StatusEnum.VALID.value)
    try:
        req["id"] = get_uuid()
        req["name"] = dataset_name
        req["tenant_id"] = current_user.id
        req["created_by"] = current_user.id
        e, t = TenantService.get_by_id(current_user.id)
        if not e:
            return get_data_error_result(message="Tenant not found.")
        operator = UserTenantService.filter_by_tenant_and_user_id(current_user.id, current_user.id)
        if not operator:
            return get_data_error_result(message="UserTenant not found.")
        req["embd_id"] = t.embd_id

        with DB.atomic():
            if not KnowledgebaseService.save(**req):
                raise ValueError("KB creation failed")
            if not PermissionService.save(
                id=get_uuid(), member_id=operator.id, tenant_id=current_user.id, resource_type=ResourceType.KB, resource_id=req["id"], permission=PermissionValue.PERMISSION_OWNER.value
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
            time.sleep(2)

        return get_json_result(data={"kb_id": req["id"]})

    except ValueError as e:
        return get_data_error_result(message=str(e))
    except Exception as e:
        return server_error_response(e)


@manager.route('/update', methods=['post'])  # noqa: F821
@login_required
@validate_request("kb_id", "name", "description", "parser_id")
@not_allowed_parameters("id", "created_by", "create_time", "update_time", "create_date", "update_date", "created_by")
@check_kb_permission(permission=PermissionValue.PERMISSION_MANAGE)
def update():
    req = g.req_data
    req["name"] = req["name"].strip()
    kb_id = req.get("kb_id")
    tenant_id = g.tenant_id

    if "operator_permission" in req:
        req.pop("operator_permission", None)

    try:
        e, kb = KnowledgebaseService.get_by_id(kb_id)
        if not (e and kb):
            return get_data_error_result(message="Can't find this knowledgebase!")

        if req.get("parser_id", "") == "tag" and os.environ.get("DOC_ENGINE", "elasticsearch") == "infinity":
            return get_json_result(data=False, message="The chunk method Tag has not been supported by Infinity yet.", code=settings.RetCode.OPERATING_ERROR)

        if req["name"].lower() != kb.name.lower() and len(KnowledgebaseService.query(name=req["name"], tenant_id=tenant_id, status=StatusEnum.VALID.value)) > 1:
            return get_data_error_result(message="Duplicated knowledgebase name.")

        del req["kb_id"]
        if not KnowledgebaseService.update_by_id(kb.id, req):
            return get_data_error_result()

        if kb.pagerank != req.get("pagerank", 0):
            if req.get("pagerank", 0) > 0:
                settings.docStoreConn.update({"kb_id": kb.id}, {PAGERANK_FLD: req["pagerank"]},
                                         search.index_name(kb.tenant_id), kb.id)
            else:
                # Elasticsearch requires PAGERANK_FLD be non-zero!
                settings.docStoreConn.update({"exists": PAGERANK_FLD}, {"remove": PAGERANK_FLD},
                                         search.index_name(kb.tenant_id), kb.id)

        e, kb = KnowledgebaseService.get_by_id(kb.id)
        if not (e and kb):
            return get_data_error_result(message="Database error (Knowledgebase rename)!")
        kb = kb.to_dict()
        kb.update(req)

        return get_json_result(data=kb)
    except Exception as e:
        return server_error_response(e)


@manager.route('/detail', methods=['GET'])  # noqa: F821
@login_required
@check_kb_permission(permission=PermissionValue.PERMISSION_READ)
def detail():
    kb_id = request.args["kb_id"]
    tenant_id = g.tenant_id

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
    if not operator:
        return get_data_error_result(message="Unrecognized identification.")

    try:
        kb = KnowledgebaseService.get_detail(kb_id)
        if not kb:
            return get_data_error_result(message="Can't find this knowledgebase!")

        permission = has_permission_for_member(operator_id=operator.id, tenant_id=tenant_id, resource_id=kb_id, resource_type=ResourceType.KB, permission=PermissionValue.PERMISSION_READ)
        if not permission[0]:
            kb["operator_permission"] = PermissionValue.PERMISSION_NULL.value
        else:
            kb["operator_permission"] = permission[2]

        kb["size"] = DocumentService.get_total_size_by_kb_id(kb_id=kb["id"], keywords="", run_status=[], types=[])
        return get_json_result(data=kb)
    except Exception as e:
        return server_error_response(e)


@manager.route('/list', methods=['POST'])  # noqa: F821
@login_required
def list_kbs():
    from api.db.services import UserService
    keywords = request.args.get("keywords", "")
    page_number = int(request.args.get("page", 1))
    items_per_page = int(request.args.get("page_size", 150))
    parser_id = request.args.get("parser_id")
    orderby = request.args.get("orderby", "create_time")
    desc = request.args.get("desc", True)
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
@validate_request("kb_id")
def rm():
    req = request.get_json()
    if not KnowledgebaseService.accessible4deletion(req["kb_id"], current_user.id):
        return get_json_result(data=False, message="No authorization.", code=settings.RetCode.AUTHENTICATION_ERROR)
    operator = UserTenantService.filter_by_tenant_and_user_id(current_user.id, current_user.id)
    if not operator:
        return get_json_result(data=False, message="Unrecognized identification.", code=settings.RetCode.AUTHENTICATION_ERROR)

    try:
        kbs = KnowledgebaseService.query(created_by=current_user.id, id=req["kb_id"])
        if not kbs:
            return get_json_result(data=False, message="Only owner of knowledgebase authorized for this operation.", code=settings.RetCode.OPERATING_ERROR)

        for doc in DocumentService.query(kb_id=req["kb_id"]):
            if not DocumentService.remove_document(doc, kbs[0].tenant_id):
                return get_data_error_result(message="Database error (Document removal)!")
            f2d = File2DocumentService.get_by_document_id(doc.id)
            if f2d:
                FileService.filter_delete([File.source_type == FileSource.KNOWLEDGEBASE, File.id == f2d[0].file_id])
            File2DocumentService.delete_by_document_id(doc.id)
        FileService.filter_delete([File.source_type == FileSource.KNOWLEDGEBASE, File.type == "folder", File.name == kbs[0].name])
        if not KnowledgebaseService.delete_by_id(req["kb_id"]):
            return get_data_error_result(message="Database error (Knowledgebase removal)!")
        for kb in kbs:
            settings.docStoreConn.delete({"kb_id": kb.id}, search.index_name(kb.tenant_id), kb.id)
            settings.docStoreConn.deleteIdx(search.index_name(kb.tenant_id), kb.id)
            STORAGE_IMPL.rm_bucket(kb.id)

        with DB.atomic():
            permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(tenant_id=current_user.id, resource_id=req["kb_id"], resource_type=ResourceType.KB)
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
                dialog_permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(tenant_id=current_user.id, resource_id=dialog_id, resource_type=ResourceType.DIALOG)
                PermissionService.delete(dialog_permission_model_list)

        return get_json_result(data=True)

    except ValueError as e:
        return get_data_error_result(message=str(e))
    except Exception as e:
        return server_error_response(e)


@manager.route('/<kb_id>/tags', methods=['GET'])  # noqa: F821
@login_required
@check_kb_permission(permission=PermissionValue.PERMISSION_READ)
def list_tags(kb_id):
    if not KnowledgebaseService.accessible(kb_id, current_user.id):
        return get_json_result(
            data=False,
            message='No authorization.',
            code=settings.RetCode.AUTHENTICATION_ERROR
        )

    tags = settings.retrievaler.all_tags(current_user.id, [kb_id])
    return get_json_result(data=tags)


@manager.route('/tags', methods=['GET'])  # noqa: F821
@login_required
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
            return get_json_result(data=False, message="No authorization", code=settings.RetCode.AUTHENTICATION_ERROR)

    tags = settings.retrievaler.all_tags(current_user.id, kb_ids)
    return get_json_result(data=tags)


@manager.route('/<kb_id>/rm_tags', methods=['POST'])  # noqa: F821
@login_required
@check_kb_permission(permission=PermissionValue.PERMISSION_MANAGE)
def rm_tags(kb_id):
    req = g.req_data

    e, kb = KnowledgebaseService.get_by_id(kb_id)
    if not (e and kb):
        return get_json_result(data=False, message="Knowledgebase cannot found.")

    for t in req["tags"]:
        settings.docStoreConn.update({"tag_kwd": t, "kb_id": [kb_id]},
                                     {"remove": {"tag_kwd": t}},
                                     search.index_name(kb.tenant_id),
                                     kb_id)
    return get_json_result(data=True)


@manager.route('/<kb_id>/rename_tag', methods=['POST'])  # noqa: F821
@login_required
@check_kb_permission(permission=PermissionValue.PERMISSION_MANAGE)
def rename_tags(kb_id):
    req = g.req_data

    e, kb = KnowledgebaseService.get_by_id(kb_id)

    settings.docStoreConn.update({"tag_kwd": req["from_tag"], "kb_id": [kb_id]},
                                     {"remove": {"tag_kwd": req["from_tag"].strip()}, "add": {"tag_kwd": req["to_tag"]}},
                                     search.index_name(kb.tenant_id),
                                     kb_id)
    return get_json_result(data=True)


@manager.route('/<kb_id>/knowledge_graph', methods=['GET'])  # noqa: F821
@login_required
@check_kb_permission(permission=PermissionValue.PERMISSION_READ)
def knowledge_graph(kb_id):
    _, kb = KnowledgebaseService.get_by_id(kb_id)
    req = {"kb_id": [kb_id], "knowledge_graph_kwd": ["graph"]}

    obj = {"graph": {}, "mind_map": {}}
    if not settings.docStoreConn.indexExist(search.index_name(kb.tenant_id), kb_id):
        return get_json_result(data=obj)
    sres = settings.retrievaler.search(req, search.index_name(kb.tenant_id), [kb_id])
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


@manager.route("/<kb_id>/knowledge_graph", methods=["DELETE"])  # noqa: F821
@login_required
def delete_knowledge_graph(kb_id):
    if not KnowledgebaseService.accessible(kb_id, current_user.id):
        return get_json_result(
            data=False,
            message='No authorization.',
            code=settings.RetCode.AUTHENTICATION_ERROR
        )
    _, kb = KnowledgebaseService.get_by_id(kb_id)
    settings.docStoreConn.delete({"knowledge_graph_kwd": ["graph", "subgraph", "entity", "relation"]}, search.index_name(kb.tenant_id), kb_id)

    return get_json_result(data=True)


@manager.route("/get_meta", methods=["GET"])  # noqa: F821
@login_required
def get_meta():
    kb_ids = request.args.get("kb_ids", "").split(",")
    for kb_id in kb_ids:
        if not KnowledgebaseService.accessible(kb_id, current_user.id):
            return get_json_result(
                data=False,
                message='No authorization.',
                code=settings.RetCode.AUTHENTICATION_ERROR
            )
    return get_json_result(data=DocumentService.get_meta_by_kbs(kb_ids))
