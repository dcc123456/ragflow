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
from quart import request
import numpy as np

from api.db import PermissionValue
from api.db.services.document_service import DocumentService, queue_reembedding_dup_tasks

from api.db.services.llm_service import LLMBundle
from api.db.services.task_service import TaskService
from api.db.joint_services.tenant_model_service import get_model_config_from_provider_instance
from api.utils.api_utils import (
    get_error_data_result,
    validate_request,
    get_request_json,
)
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.utils.api_utils import get_json_result
from api.utils.billing import check_resources
from api.utils.permission_utils import check_kb_permission
from common.role_util import check_role_access, KB_API_ACTION_MAP, KB_ROLE_RESOURCE_TYPE

from rag.nlp import search
from common.constants import RetCode, LLMType
from common import settings
from api.common.priority_provider import get_tenant_priority
from api.apps import login_required, current_user


kb_role_guard = check_role_access(KB_API_ACTION_MAP, KB_ROLE_RESOURCE_TYPE)


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
        base_fields=("docnm_kwd", "doc_id", "content_with_weight", "page_num_int", "position_int", "top_int"),
    ):
        index_nm = search.index_name(tenant_id)

        res0 = docStoreConn.search(
            select_fields=[],
            highlight_fields=[],
            condition={"kb_id": kb_id, "available_int": 1},
            match_expressions=[],
            order_by=OrderByExpr(),
            offset=0,
            limit=1,
            index_names=index_nm,
            knowledgebase_ids=[kb_id],
        )
        total = docStoreConn.get_total(res0)
        if total <= 0:
            return []

        n = min(n, total)
        offsets = sorted(random.sample(range(min(total, 1000)), n))
        out = []

        for off in offsets:
            res1 = docStoreConn.search(
                select_fields=list(base_fields),
                highlight_fields=[],
                condition={"kb_id": kb_id, "available_int": 1},
                match_expressions=[],
                order_by=OrderByExpr(),
                offset=off,
                limit=1,
                index_names=index_nm,
                knowledgebase_ids=[kb_id],
            )
            ids = docStoreConn.get_doc_ids(res1)
            if not ids:
                continue

            cid = ids[0]
            full_doc = docStoreConn.get(cid, index_nm, [kb_id]) or {}
            vec_field = _guess_vec_field(full_doc)
            vec = _as_float_vec(full_doc.get(vec_field))

            out.append(
                {
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
                    "question_kwd": full_doc.get("question_kwd") or [],
                }
            )
        return out

    def _clean(s: str) -> str:
        s = re.sub(r"</?(table|td|caption|tr|th)( [^<>]{0,12})?>", " ", s or "")
        return s if s else "None"

    req = await get_request_json()
    kb_id = req.get("kb_id", "")
    embd_id = req.get("embd_id", "")

    n = int(req.get("check_num", 5))
    _, kb = KnowledgebaseService.get_by_id(kb_id)
    tenant_id = kb.tenant_id
    if embd_id:
        embd_model_config = get_model_config_from_provider_instance(tenant_id, LLMType.EMBEDDING, embd_id)
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
        results.append(
            {
                "chunk_id": ck["chunk_id"],
                "doc_id": ck["doc_id"],
                "doc_name": ck["doc_name"],
                "vector_field": ck["vector_field"],
                "vector_dim": ck["vector_dim"],
                "cos_sim": round(sim, 6),
            }
        )

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
    return get_json_result(
        code=RetCode.NOT_EFFECTIVE,
        message="Embedding model switch failed: the average similarity between old and new vectors is below 0.9, indicating incompatible vector spaces.",
        data={"summary": summary},
    )


@manager.route("/switch_embedding", methods=["POST"])  # noqa: F821
@login_required
@validate_request("kb_id", "embd_id")
@kb_role_guard
@check_kb_permission(permission=PermissionValue.PERMISSION_MANAGE)
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
    e, kb = KnowledgebaseService.create_with_name(name=target_name, tenant_id=current_user.id, **kb)
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
