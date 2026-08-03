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
import base64
import datetime
import logging
import re
import xxhash
from quart import request

from api.db.services.document_service import DocumentService
from api.utils.image_utils import store_chunk_image
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.db.services.llm_service import LLMBundle
from api.db.services.user_service import UserTenantService
from api.db.joint_services.tenant_model_service import get_tenant_default_model_by_type, get_model_config_from_provider_instance
from api.utils.api_utils import (
    get_data_error_result,
    get_json_result,
    server_error_response,
    validate_request,
    get_request_json,
)
from common.misc_utils import thread_pool_exec
from rag.app.qa import beAdoc, rmPrefix
from rag.nlp import rag_tokenizer, search
from common.string_utils import remove_redundant_spaces
from common.constants import RetCode, LLMType, ParserType, PAGERANK_FLD
from common import settings
from api.apps import login_required, current_user


@manager.route("/list", methods=["POST"])  # noqa: F821
@login_required
@validate_request("doc_id")
async def list_chunk():
    req = await get_request_json()
    doc_id = req["doc_id"]
    page = int(req.get("page", 1))
    size = int(req.get("size", 30))
    question = req.get("keywords", "")
    empty_res = None
    try:
        tenant_id = DocumentService.get_tenant_id(req["doc_id"])
        if not tenant_id:
            return get_data_error_result(message="Tenant not found!")
        e, doc = DocumentService.get_by_id(doc_id)
        if not e:
            return get_data_error_result(message="Document not found!")
        kb_ids = KnowledgebaseService.get_kb_ids(tenant_id)
        empty_res = {"total": 0, "chunks": [], "doc": doc.to_dict()}
        query = {"doc_ids": [doc_id], "page": page, "size": size, "question": question, "sort": True}
        if "available_int" in req:
            query["available_int"] = int(req["available_int"])
        sres = await settings.retriever.search(query, search.index_name(tenant_id), kb_ids, highlight=["content_ltks"])
        res = {"total": sres.total, "chunks": [], "doc": doc.to_dict()}
        for id in sres.ids:
            d = {
                "chunk_id": id,
                "content_with_weight": remove_redundant_spaces(sres.highlight[id]) if question and id in sres.highlight else sres.field[id].get("content_with_weight", ""),
                "doc_id": sres.field[id]["doc_id"],
                "docnm_kwd": sres.field[id]["docnm_kwd"],
                "important_kwd": sres.field[id].get("important_kwd", []),
                "question_kwd": sres.field[id].get("question_kwd", []),
                "image_id": sres.field[id].get("img_id", ""),
                "available_int": int(sres.field[id].get("available_int", 1)),
                "positions": sres.field[id].get("position_int", []),
                "doc_type_kwd": sres.field[id].get("doc_type_kwd"),
            }
            assert isinstance(d["positions"], list)
            assert len(d["positions"]) == 0 or (isinstance(d["positions"][0], list) and len(d["positions"][0]) == 5)
            res["chunks"].append(d)
        return get_json_result(data=res)
    except Exception as e:
        if str(e).find("not_found") > 0:
            return get_json_result(data=empty_res)
        return server_error_response(e)


@manager.route("/get", methods=["GET"])  # noqa: F821
@login_required
def get():
    chunk_id = request.args["chunk_id"]
    try:
        chunk = None
        tenants = UserTenantService.query(user_id=current_user.id)
        if not tenants:
            return get_data_error_result(message="Tenant not found!")
        for tenant in tenants:
            kb_ids = KnowledgebaseService.get_kb_ids(tenant.tenant_id)
            chunk = settings.docStoreConn.get(chunk_id, search.index_name(tenant.tenant_id), kb_ids)
            if chunk:
                break
        if chunk is None:
            return server_error_response(Exception("Chunk not found"))

        k = []
        for n in chunk.keys():
            if re.search(r"(_vec$|_sm_|_tks|_ltks)", n):
                k.append(n)
        for n in k:
            del chunk[n]

        return get_json_result(data=chunk)
    except Exception as e:
        if str(e).find("NotFoundError") >= 0:
            return get_json_result(data=False, message="Chunk not found!", code=RetCode.DATA_ERROR)
        return server_error_response(e)


@manager.route("/set", methods=["POST"])  # noqa: F821
@login_required
@validate_request("doc_id", "chunk_id", "content_with_weight")
async def set():
    req = await get_request_json()
    content_with_weight = req["content_with_weight"]
    if not isinstance(content_with_weight, (str, bytes)):
        raise TypeError("expected string or bytes-like object")
    if isinstance(content_with_weight, bytes):
        content_with_weight = content_with_weight.decode("utf-8", errors="ignore")
    d = {"id": req["chunk_id"], "content_with_weight": content_with_weight}
    d["content_ltks"] = rag_tokenizer.tokenize(content_with_weight)
    d["content_sm_ltks"] = rag_tokenizer.fine_grained_tokenize(d["content_ltks"])
    if "important_kwd" in req:
        if not isinstance(req["important_kwd"], list):
            return get_data_error_result(message="`important_kwd` should be a list")
        d["important_kwd"] = req["important_kwd"]
        d["important_tks"] = rag_tokenizer.tokenize(" ".join(req["important_kwd"]))
    if "question_kwd" in req:
        if not isinstance(req["question_kwd"], list):
            return get_data_error_result(message="`question_kwd` should be a list")
        d["question_kwd"] = req["question_kwd"]
        d["question_tks"] = rag_tokenizer.tokenize("\n".join(req["question_kwd"]))
    if "tag_kwd" in req:
        if not isinstance(req["tag_kwd"], list):
            return get_data_error_result(message="`tag_kwd` should be a list")
        if not all(isinstance(t, str) for t in req["tag_kwd"]):
            return get_data_error_result(message="`tag_kwd` must be a list of strings")
        d["tag_kwd"] = req["tag_kwd"]
    if "tag_feas" in req:
        d["tag_feas"] = req["tag_feas"]
    if "available_int" in req:
        d["available_int"] = req["available_int"]

    try:

        def _set_sync():
            tenant_id = DocumentService.get_tenant_id(req["doc_id"])
            if not tenant_id:
                return get_data_error_result(message="Tenant not found!")

            e, doc = DocumentService.get_by_id(req["doc_id"])
            if not e:
                return get_data_error_result(message="Document not found!")

            embd_id = DocumentService.get_embd_id(req["doc_id"])
            if embd_id:
                embd_model_config = get_model_config_from_provider_instance(tenant_id, LLMType.EMBEDDING, embd_id)
            else:
                embd_model_config = get_tenant_default_model_by_type(tenant_id, LLMType.EMBEDDING)
            embd_mdl = LLMBundle(tenant_id, embd_model_config)

            _d = d
            if doc.parser_id == ParserType.QA:
                arr = [t for t in re.split(r"[\n\t]", req["content_with_weight"]) if len(t) > 1]
                q, a = rmPrefix(arr[0]), rmPrefix("\n".join(arr[1:]))
                _d = beAdoc(d, q, a, not any([rag_tokenizer.is_chinese(t) for t in q + a]))

            v, c = embd_mdl.encode([doc.name, content_with_weight if not _d.get("question_kwd") else "\n".join(_d["question_kwd"])])
            v = 0.1 * v[0] + 0.9 * v[1] if doc.parser_id != ParserType.QA else v[1]
            _d["q_%d_vec" % len(v)] = v.tolist()
            settings.docStoreConn.update({"id": req["chunk_id"]}, _d, search.index_name(tenant_id), doc.kb_id)

            # update image
            image_base64 = req.get("image_base64", None)
            img_id = req.get("img_id", "")
            if image_base64 and img_id and "-" in img_id:
                bkt, name = img_id.split("-", 1)
                image_binary = base64.b64decode(image_base64)
                settings.STORAGE_IMPL.put(bkt, name, image_binary, tenant_id)
            return get_json_result(data=True)

        return await thread_pool_exec(_set_sync)
    except Exception as e:
        return server_error_response(e)


@manager.route("/switch", methods=["POST"])  # noqa: F821
@login_required
@validate_request("chunk_ids", "available_int", "doc_id")
async def switch():
    req = await get_request_json()
    try:

        def _switch_sync():
            e, doc = DocumentService.get_by_id(req["doc_id"])
            if not e:
                return get_data_error_result(message="Document not found!")
            for cid in req["chunk_ids"]:
                if not settings.docStoreConn.update({"id": cid}, {"available_int": int(req["available_int"])}, search.index_name(DocumentService.get_tenant_id(req["doc_id"])), doc.kb_id):
                    return get_data_error_result(message="Index updating failure")
            return get_json_result(data=True)

        return await thread_pool_exec(_switch_sync)
    except Exception as e:
        return server_error_response(e)


@manager.route("/rm", methods=["POST"])  # noqa: F821
@login_required
@validate_request("doc_id")
async def rm():
    req = await get_request_json()
    user_id = current_user.id
    try:

        def _rm_sync():
            deleted_chunk_ids = req.get("chunk_ids")
            if isinstance(deleted_chunk_ids, list):
                unique_chunk_ids = list(dict.fromkeys(deleted_chunk_ids))
                has_ids = len(unique_chunk_ids) > 0
            elif deleted_chunk_ids is not None:
                unique_chunk_ids = [deleted_chunk_ids]
                has_ids = deleted_chunk_ids not in (None, "")
            else:
                unique_chunk_ids = []
                has_ids = False
            if not has_ids:
                if req.get("delete_all") is True:
                    e, doc = DocumentService.get_by_id(req["doc_id"])
                    if not e:
                        return get_data_error_result(message="Document not found!")
                    tenant_id = DocumentService.get_tenant_id(req["doc_id"])
                    # Clean up storage assets while index rows still exist for discovery
                    DocumentService.delete_chunk_images(doc, tenant_id)
                    condition = {"doc_id": req["doc_id"]}
                    try:
                        deleted_count = settings.docStoreConn.delete(condition, search.index_name(tenant_id), doc.kb_id)
                    except Exception:
                        return get_data_error_result(message="Chunk deleting failure")
                    if deleted_count > 0:
                        DocumentService.decrement_chunk_num(doc.id, doc.kb_id, 1, deleted_count, 0)
                    return get_json_result(data=True)
                return get_json_result(data=True)

            e, doc = DocumentService.get_by_id(req["doc_id"])
            if not e:
                return get_data_error_result(message="Document not found!")
            condition = {"id": req["chunk_ids"], "doc_id": req["doc_id"]}
            try:
                deleted_count = settings.docStoreConn.delete(condition, search.index_name(DocumentService.get_tenant_id(req["doc_id"])), doc.kb_id)
            except Exception:
                return get_data_error_result(message="Chunk deleting failure")
            if has_ids and deleted_count == 0:
                return get_data_error_result(message="Index updating failure")
            if deleted_count > 0 and deleted_count < len(unique_chunk_ids):
                deleted_count += settings.docStoreConn.delete({"doc_id": req["doc_id"]}, search.index_name(DocumentService.get_tenant_id(req["doc_id"])), doc.kb_id)
            chunk_number = deleted_count
            DocumentService.decrement_chunk_num(doc.id, doc.kb_id, 1, chunk_number, 0)
            for cid in deleted_chunk_ids:
                if settings.STORAGE_IMPL.obj_exist(doc.kb_id, cid, user_id):
                    settings.STORAGE_IMPL.rm(doc.kb_id, cid, user_id)
            return get_json_result(data=True)

        return await thread_pool_exec(_rm_sync)
    except Exception as e:
        return server_error_response(e)


@manager.route("/create", methods=["POST"])  # noqa: F821
@login_required
@validate_request("doc_id", "content_with_weight")
async def create():
    req = await get_request_json()
    req_id = request.headers.get("X-Request-ID")
    chunck_id = xxhash.xxh64((req["content_with_weight"] + req["doc_id"]).encode("utf-8")).hexdigest()
    d = {"id": chunck_id, "content_ltks": rag_tokenizer.tokenize(req["content_with_weight"]), "content_with_weight": req["content_with_weight"]}
    d["content_sm_ltks"] = rag_tokenizer.fine_grained_tokenize(d["content_ltks"])
    d["important_kwd"] = req.get("important_kwd", [])
    if not isinstance(d["important_kwd"], list):
        return get_data_error_result(message="`important_kwd` is required to be a list")
    d["important_tks"] = rag_tokenizer.tokenize(" ".join(d["important_kwd"]))
    d["question_kwd"] = req.get("question_kwd", [])
    if not isinstance(d["question_kwd"], list):
        return get_data_error_result(message="`question_kwd` is required to be a list")
    d["question_tks"] = rag_tokenizer.tokenize("\n".join(d["question_kwd"]))
    d["create_time"] = str(datetime.datetime.now()).replace("T", " ")[:19]
    d["create_timestamp_flt"] = datetime.datetime.now().timestamp()
    if "tag_kwd" in req:
        if not isinstance(req["tag_kwd"], list):
            return get_data_error_result(message="`tag_kwd` is required to be a list")
        if not all(isinstance(t, str) for t in req["tag_kwd"]):
            return get_data_error_result(message="`tag_kwd` must be a list of strings")
        d["tag_kwd"] = req["tag_kwd"]
    if "tag_feas" in req:
        d["tag_feas"] = req["tag_feas"]
    image_base64 = req.get("image_base64", None)

    try:

        def _log_response(resp, code, message):
            logging.info(
                "chunk_create response req_id=%s status=%s code=%s message=%s",
                req_id,
                getattr(resp, "status_code", None),
                code,
                message,
            )

        def _create_sync():
            e, doc = DocumentService.get_by_id(req["doc_id"])
            if not e:
                resp = get_data_error_result(message="Document not found!")
                _log_response(resp, RetCode.DATA_ERROR, "Document not found!")
                return resp
            d["kb_id"] = [doc.kb_id]
            d["docnm_kwd"] = doc.name
            d["title_tks"] = rag_tokenizer.tokenize(doc.name)
            d["doc_id"] = doc.id

            tenant_id = DocumentService.get_tenant_id(req["doc_id"])
            if not tenant_id:
                resp = get_data_error_result(message="Tenant not found!")
                _log_response(resp, RetCode.DATA_ERROR, "Tenant not found!")
                return resp

            e, kb = KnowledgebaseService.get_by_id(doc.kb_id)
            if not e:
                resp = get_data_error_result(message="Knowledgebase not found!")
                _log_response(resp, RetCode.DATA_ERROR, "Knowledgebase not found!")
                return resp
            if kb.pagerank:
                d[PAGERANK_FLD] = kb.pagerank

            embd_id = DocumentService.get_embd_id(req["doc_id"])
            if embd_id:
                embd_model_config = get_model_config_from_provider_instance(tenant_id, LLMType.EMBEDDING, embd_id)
            else:
                embd_model_config = get_tenant_default_model_by_type(tenant_id, LLMType.EMBEDDING)
            embd_mdl = LLMBundle(tenant_id, embd_model_config)

            if image_base64:
                d["img_id"] = "{}-{}".format(doc.kb_id, chunck_id)
                d["doc_type_kwd"] = "image"

            v, c = embd_mdl.encode([doc.name, req["content_with_weight"] if not d["question_kwd"] else "\n".join(d["question_kwd"])])
            v = 0.1 * v[0] + 0.9 * v[1]
            d["q_%d_vec" % len(v)] = v.tolist()
            settings.docStoreConn.insert([d], search.index_name(tenant_id), doc.kb_id)

            if image_base64:
                store_chunk_image(doc.kb_id, chunck_id, base64.b64decode(image_base64), tenant_id)

            DocumentService.increment_chunk_num(doc.id, doc.kb_id, c, 1, 0)
            resp = get_json_result(data={"chunk_id": chunck_id, "image_id": d.get("img_id", "")})
            _log_response(resp, RetCode.SUCCESS, "success")
            return resp

        return await thread_pool_exec(_create_sync)
    except Exception as e:
        logging.info("chunk_create exception req_id=%s error=%r", req_id, e)
        return server_error_response(e)
