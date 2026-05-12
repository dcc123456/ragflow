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
import asyncio
import logging
import json
import os
from api.db.services.tenant_llm_service import LLMFactoriesService, TenantLLMService
from api.db.services.llm_service import LLMService
from api.utils.api_utils import server_error_response, get_data_error_result, get_json_result, validate_request
from quart import request
from api.apps import login_required, current_user
from common.constants import StatusEnum, LLMType
from common.misc_utils import get_uuid
from api.utils.api_utils import get_allowed_llm_factories, get_request_json
from rag.utils.base64_image import test_image
from api.db import PermissionActionType, PermissionTargetType, PermissionValue, ResourceType
from api.db.db_models import DB, TenantLLM
from api.db.services.dialog_service import DialogService
from api.db.services.permission_service import PermissionChangeLogService, PermissionService
from api.db.services.user_service import UserTenantService
from api.utils.permission_utils import has_permission_for_member
from rag.llm import EmbeddingModel, ChatModel, RerankModel, CvModel, TTSModel, OcrModel, Seq2txtModel

logger = logging.getLogger("ragflow.llm_app")


def _resolve_my_llm_is_tools(o_dict: dict) -> bool:
    decode_api_key_config = getattr(TenantLLMService, "_decode_api_key_config", None)
    if callable(decode_api_key_config):
        _, is_tools, _ = decode_api_key_config(o_dict.get("api_key", ""))
        if is_tools is not None:
            return bool(is_tools)

    try:
        base_name, fid = TenantLLMService.split_model_name_and_factory(o_dict["llm_name"])
        llm_cfg = LLMService.query(llm_name=base_name, fid=fid) if fid else LLMService.query(llm_name=base_name)
        if not llm_cfg and fid:
            llm_cfg = LLMService.query(llm_name=base_name)
        return bool(llm_cfg[0].is_tools) if llm_cfg else False
    except Exception:
        return False


@manager.route("/factories", methods=["GET"])  # noqa: F821
@login_required
async def factories():
    try:
        fac = get_allowed_llm_factories()
        fac = [f.to_dict() for f in fac if f.name not in ["Youdao", "FastEmbed", "BAAI", "Builtin", "siliconflow_intl"]]
        llms = LLMService.get_all()
        mdl_types = {}
        builtin_models = []
        for m in llms:
            if m.status != StatusEnum.VALID.value:
                continue
            if m.fid == 'Builtin':
                builtin_models.append(m.llm_name)
            if m.fid not in mdl_types:
                mdl_types[m.fid] = set([])
            mdl_types[m.fid].add(m.model_type)
        logger.info(f"Builtin models with VALID status: {builtin_models}")
        for f in fac:
            f["model_types"] = list(
                mdl_types.get(
                    f["name"],
                    [LLMType.CHAT, LLMType.EMBEDDING, LLMType.RERANK, LLMType.IMAGE2TEXT, LLMType.SPEECH2TEXT, LLMType.TTS, LLMType.OCR],
                )
            )

        return get_json_result(data=fac)
    except Exception as e:
        return server_error_response(e)


@manager.route("/set_api_key", methods=["POST"])  # noqa: F821
@login_required
@validate_request("llm_factory", "api_key")
async def set_api_key():
    req = await get_request_json()
    # test if api key works
    chat_passed, embd_passed, rerank_passed = False, False, False
    factory = req["llm_factory"]
    base_url = req.get("base_url", "")
    source_factory = req.get("source_fid", factory)
    extra = {"provider": factory}
    timeout_seconds = int(os.environ.get("LLM_TIMEOUT_SECONDS", 10))
    source_llms = list(LLMService.query(fid=source_factory))
    if not source_llms:
        msg = f"No models configured for {factory} (source: {source_factory})."
        if req.get("verify", False):
            return get_json_result(data={"message": msg, "success": False})
        return get_data_error_result(message=msg)

    msg = ""
    for llm in source_llms:
        if not embd_passed and llm.model_type == LLMType.EMBEDDING.value:
            assert factory in EmbeddingModel, f"Embedding model from {factory} is not supported yet."
            mdl = EmbeddingModel[factory](req["api_key"], llm.llm_name, base_url=base_url)
            try:
                arr, tc = await asyncio.wait_for(
                    asyncio.to_thread(mdl.encode, ["Test if the api key is available"]),
                    timeout=timeout_seconds,
                )
                if len(arr[0]) == 0:
                    raise Exception("Fail")
                embd_passed = True
            except Exception as e:
                msg += f"\nFail to access embedding model({llm.llm_name}) using this api key." + str(e)
        elif not chat_passed and llm.model_type == LLMType.CHAT.value:
            assert factory in ChatModel, f"Chat model from {factory} is not supported yet."
            mdl = ChatModel[factory](req["api_key"], llm.llm_name, base_url=base_url, **extra)
            try:
                async def check_streamly():
                    async for chunk in mdl.async_chat_streamly(
                        None,
                        [{"role": "user", "content": "Hi"}],
                        {"temperature": 0.9},
                    ):
                        if chunk and isinstance(chunk, str) and chunk.find("**ERROR**") < 0:
                            return True
                    return False

                result = await asyncio.wait_for(check_streamly(), timeout=timeout_seconds)
                if result:
                    chat_passed = True
                else:
                    raise Exception("No valid response received")
            except Exception as e:
                msg += f"\nFail to access model({llm.fid}/{llm.llm_name}) using this api key." + str(e)
        elif not rerank_passed and llm.model_type == LLMType.RERANK.value:
            assert factory in RerankModel, f"Re-rank model from {factory} is not supported yet."
            mdl = RerankModel[factory](req["api_key"], llm.llm_name, base_url=base_url)
            try:
                arr, tc = await asyncio.wait_for(
                    asyncio.to_thread(mdl.similarity, "What's the weather?", ["Is it sunny today?"]),
                    timeout=timeout_seconds,
                )
                if len(arr) == 0 or tc == 0:
                    raise Exception("Fail")
                rerank_passed = True
                logging.debug(f"passed model rerank {llm.llm_name}")
            except Exception as e:
                msg += f"\nFail to access model({llm.fid}/{llm.llm_name}) using this api key." + str(e)
        if any([embd_passed, chat_passed, rerank_passed]):
            msg = ""
            break

    if req.get("verify", False):
        return get_json_result(data={"message": msg, "success": len(msg.strip())==0})

    if msg:
        return get_data_error_result(message=msg)

    llm_config = {"api_key": req["api_key"], "api_base": base_url}
    for n in ["model_type", "llm_name"]:
        if n in req:
            llm_config[n] = req[n]

    for llm in source_llms:
        llm_config["max_tokens"] = llm.max_tokens
        if not TenantLLMService.filter_update([TenantLLM.tenant_id == current_user.id, TenantLLM.llm_factory == factory, TenantLLM.llm_name == llm.llm_name], llm_config):
            TenantLLMService.save(
                tenant_id=current_user.id,
                llm_factory=factory,
                llm_name=llm.llm_name,
                model_type=llm.model_type,
                api_key=llm_config["api_key"],
                api_base=llm_config["api_base"],
                max_tokens=llm_config["max_tokens"],
            )

    return get_json_result(data=True)


@manager.route("/add_llm", methods=["POST"])  # noqa: F821
@login_required
@validate_request("llm_factory")
async def add_llm():
    req = await get_request_json()
    factory = req["llm_factory"]
    api_key = req.get("api_key", "x")
    llm_name = req.get("llm_name")
    timeout_seconds = int(os.environ.get("LLM_TIMEOUT_SECONDS", 10))

    if factory not in [f.name for f in get_allowed_llm_factories()]:
        return get_data_error_result(message=f"LLM factory {factory} is not allowed")

    def apikey_json(keys):
        nonlocal req
        return json.dumps({k: req.get(k, "") for k in keys})

    if factory == "VolcEngine":
        # For VolcEngine, due to its special authentication method
        # Assemble ark_api_key endpoint_id into api_key
        api_key = apikey_json(["ark_api_key", "endpoint_id"])

    elif factory == "Tencent Cloud":
        req["api_key"] = apikey_json(["tencent_cloud_sid", "tencent_cloud_sk"])
        return await set_api_key()

    elif factory == "Bedrock":
        # For Bedrock, due to its special authentication method
        # Assemble bedrock_ak, bedrock_sk, bedrock_region
        # Write into req["api_key"] to prevent the "existing key" override logic from replacing it
        req["api_key"] = apikey_json(["auth_mode", "bedrock_ak", "bedrock_sk", "bedrock_region", "aws_role_arn"])
        api_key = req["api_key"]

    elif factory == "LocalAI":
        llm_name += "___LocalAI"

    elif factory == "HuggingFace":
        llm_name += "___HuggingFace"

    elif factory == "OpenAI-API-Compatible":
        llm_name += "___OpenAI-API"

    elif factory == "VLLM":
        llm_name += "___VLLM"

    elif factory == "XunFei Spark":
        if req["model_type"] == "chat":
            api_key = req.get("spark_api_password", "")
        elif req["model_type"] == "tts":
            api_key = apikey_json(["spark_app_id", "spark_api_secret", "spark_api_key"])

    elif factory == "BaiduYiyan":
        api_key = apikey_json(["yiyan_ak", "yiyan_sk"])

    elif factory == "Fish Audio":
        api_key = apikey_json(["fish_audio_ak", "fish_audio_refid"])

    elif factory == "Google Cloud":
        api_key = apikey_json(["google_project_id", "google_region", "google_service_account_key"])

    elif factory == "Azure-OpenAI":
        api_key = apikey_json(["api_key", "api_version"])

    elif factory == "OpenRouter":
        api_key = apikey_json(["api_key", "provider_order"])

    elif factory == "MinerU":
        api_key = apikey_json(["api_key", "provider_order"])

    elif factory == "PaddleOCR":
        api_key = apikey_json(["api_key", "provider_order"])

    elif factory == "OpenDataLoader":
        api_key = apikey_json(["api_key", "provider_order"])

    existing_llm = None
    existing_api_key = None
    if req.get("api_key") is None:
        existing_llms = TenantLLMService.query(tenant_id=current_user.id, llm_factory=factory, llm_name=llm_name)
        if existing_llms:
            existing_llm = existing_llms[0]
            existing_api_key, _, existing_api_key_payload = TenantLLMService._decode_api_key_config(existing_llm.api_key)
            if existing_api_key_payload is not None:
                existing_api_key = existing_api_key_payload

    if req.get("api_key") is None:
        api_key = existing_api_key if existing_api_key is not None else "x"

    llm = {
        "tenant_id": current_user.id,
        "llm_factory": factory,
        "model_type": req["model_type"],
        "llm_name": llm_name,
        "api_base": req.get("api_base", ""),
        "api_key": api_key,
        "max_tokens": req.get("max_tokens"),
    }

    msg = ""
    mdl_nm = llm["llm_name"].split("___")[0]
    extra = {"provider": factory}
    model_type = llm["model_type"]
    model_api_key = llm["api_key"]
    model_base_url = llm.get("api_base", "")
    match model_type:
        case LLMType.EMBEDDING.value:
            assert factory in EmbeddingModel, f"Embedding model from {factory} is not supported yet."
            mdl = EmbeddingModel[factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
            try:
                arr, tc = await asyncio.wait_for(
                    asyncio.to_thread(mdl.encode, ["Test if the api key is available"]),
                    timeout=timeout_seconds,
                )
                if len(arr[0]) == 0:
                    raise Exception("Fail")
            except Exception as e:
                msg += f"\nFail to access embedding model({mdl_nm})." + str(e)
        case LLMType.CHAT.value:
            assert factory in ChatModel, f"Chat model from {factory} is not supported yet."
            mdl = ChatModel[factory](
                key=model_api_key,
                model_name=mdl_nm,
                base_url=model_base_url,
                **extra,
            )
            try:
                async def check_streamly():
                    async for chunk in mdl.async_chat_streamly(
                        None,
                        [{"role": "user", "content": "Hi"}],
                        {"temperature": 0.9},
                    ):
                        if chunk and isinstance(chunk, str) and chunk.find("**ERROR**:") < 0:
                            return True
                    return False

                result = await asyncio.wait_for(check_streamly(), timeout=timeout_seconds)
                if not result:
                    raise Exception("No valid response received")
            except Exception as e:
                msg += f"\nFail to access model({factory}/{mdl_nm})." + str(e)

        case LLMType.RERANK.value:
            assert factory in RerankModel, f"RE-rank model from {factory} is not supported yet."
            try:
                mdl = RerankModel[factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
                arr, tc = await asyncio.wait_for(
                    asyncio.to_thread(mdl.similarity, "Hello~ RAGFlower!", ["Hi, there!", "Ohh, my friend!"]),
                    timeout=timeout_seconds,
                )
                if len(arr) == 0:
                    raise Exception("Not known.")
            except KeyError:
                msg += f"{factory} dose not support this model({factory}/{mdl_nm})"
            except Exception as e:
                msg += f"\nFail to access model({factory}/{mdl_nm})." + str(e)

        case LLMType.IMAGE2TEXT.value:
            assert factory in CvModel, f"Image to text model from {factory} is not supported yet."
            mdl = CvModel[factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
            try:
                image_data = test_image
                m, tc = await asyncio.wait_for(
                    asyncio.to_thread(mdl.describe, image_data),
                    timeout=timeout_seconds,
                )
                if not tc and m.find("**ERROR**:") >= 0:
                    raise Exception(m)
            except Exception as e:
                msg += f"\nFail to access model({factory}/{mdl_nm})." + str(e)
        case LLMType.TTS.value:
            assert factory in TTSModel, f"TTS model from {factory} is not supported yet."
            mdl = TTSModel[factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
            try:
                def drain_tts():
                    for _ in mdl.tts("Hello~ RAGFlower!"):
                        pass

                await asyncio.wait_for(
                    asyncio.to_thread(drain_tts),
                    timeout=timeout_seconds,
                )
            except RuntimeError as e:
                msg += f"\nFail to access model({factory}/{mdl_nm})." + str(e)
        case LLMType.OCR.value:
            assert factory in OcrModel, f"OCR model from {factory} is not supported yet."
            try:
                mdl = OcrModel[factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
                ok, reason = await asyncio.wait_for(
                    asyncio.to_thread(mdl.check_available),
                    timeout=timeout_seconds,
                )
                if not ok:
                    raise RuntimeError(reason or "Model not available")
            except Exception as e:
                msg += f"\nFail to access model({factory}/{mdl_nm})." + str(e)
        case LLMType.SPEECH2TEXT.value:
            assert factory in Seq2txtModel, f"Speech model from {factory} is not supported yet."
            try:
                mdl = Seq2txtModel[factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
                # TODO: check the availability
            except Exception as e:
                msg += f"\nFail to access model({factory}/{mdl_nm})." + str(e)
        case _:
            raise RuntimeError(f"Unknown model type: {model_type}")

    if req.get("verify", False):
        return get_json_result(data={"message": msg, "success": len(msg.strip()) == 0})

    if msg:
        return get_data_error_result(message=msg)

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=current_user.id, user_id=current_user.id)
    if not operator:
        return get_data_error_result(message="Unrecognized identification.")

    if "is_tools" in req:
        llm["api_key"] = TenantLLMService._encode_api_key_config(llm["api_key"], bool(req["is_tools"]))

    if not TenantLLMService.filter_update([TenantLLM.tenant_id == current_user.id, TenantLLM.llm_factory == factory, TenantLLM.llm_name == llm["llm_name"]], llm):
        TenantLLMService.save(**llm)

        if not PermissionService.save(
            id=get_uuid(),
            member_id=operator.id,
            tenant_id=current_user.id,
            resource_type=ResourceType.LLM,
            resource_id=mdl_nm,
            permission=PermissionValue.PERMISSION_OWNER.value,
        ):
            raise ValueError("Permission creation failed")

        if not PermissionChangeLogService.save(
            id=get_uuid(),
            tenant_id=operator.tenant_id,
            operator_id=operator.id,
            target_type=PermissionTargetType.TARGET_MEMBER,
            target_id=operator.id,
            resource_type=ResourceType.LLM,
            resource_id=mdl_nm,
            old_permission=PermissionValue.PERMISSION_NULL.value,
            new_permission=PermissionValue.PERMISSION_OWNER.value,
            action_type=PermissionActionType.ACTION_ADD,
        ):
            raise ValueError("Permission change log creation failed")

    return get_json_result(data=True)


@manager.route("/delete_llm", methods=["POST"])  # noqa: F821
@login_required
@validate_request("llm_factory", "llm_name")
async def delete_llm():
    req = await get_request_json()
    tenant_id = current_user.id

    try:
        operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=current_user.id, user_id=current_user.id)
        if not operator:
            return get_data_error_result(message="Unrecognized identification.")
        TenantLLMService.filter_delete([TenantLLM.tenant_id == current_user.id, TenantLLM.llm_factory == req["llm_factory"], TenantLLM.llm_name == req["llm_name"]])
        with DB.atomic():
            permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(tenant_id=tenant_id, resource_id=req["llm_name"], resource_type=ResourceType.LLM)
            PermissionService.delete(permission_model_list)

            if not PermissionChangeLogService.save(
                id=get_uuid(),
                tenant_id=operator.tenant_id,
                operator_id=operator.id,
                target_type=PermissionTargetType.TARGET_MEMBER,
                target_id=operator.id,
                resource_type=ResourceType.LLM,
                resource_id=req["llm_name"],
                old_permission=PermissionValue.PERMISSION_OWNER.value,
                new_permission=PermissionValue.PERMISSION_NULL.value,
                action_type=PermissionActionType.ACTION_DELETE,
            ):
                raise ValueError("Permission change log creation failed")

        dialogs = DialogService.query(
            status=StatusEnum.VALID.value,
            tenant_id=current_user.id,
        )
        filtered_dialog_ids = []
        for dialog in dialogs:
            if req["llm_name"] == dialog.llm_id:
                filtered_dialog_ids.append(dialog.id)

        with DB.atomic():
            for dialog_id in filtered_dialog_ids:
                dialog_permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(tenant_id=tenant_id, resource_id=dialog_id, resource_type=ResourceType.DIALOG)
                PermissionService.delete(dialog_permission_model_list)

        return get_json_result(data=True)

    except Exception as e:
        return server_error_response(e)

@manager.route("/enable_llm", methods=["POST"])  # noqa: F821
@login_required
@validate_request("llm_factory", "llm_name")
async def enable_llm():
    req = await get_request_json()
    TenantLLMService.filter_update(
        [TenantLLM.tenant_id == current_user.id, TenantLLM.llm_factory == req["llm_factory"], TenantLLM.llm_name == req["llm_name"]], {"status": str(req.get("status", "1"))}
    )
    return get_json_result(data=True)


@manager.route("/delete_factory", methods=["POST"])  # noqa: F821
@login_required
@validate_request("llm_factory")
async def delete_factory():
    req = await get_request_json()
    tenant_id = current_user.id
    try:
        TenantLLMService.filter_delete([TenantLLM.tenant_id == tenant_id, TenantLLM.llm_factory == req["llm_factory"]])
        with DB.atomic():
            permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(tenant_id=tenant_id, resource_id=req["llm_factory"], resource_type=ResourceType.LLM)
            PermissionService.delete(permission_model_list)

            if not PermissionChangeLogService.save(
                id=get_uuid(),
                tenant_id=tenant_id,
                operator_id=tenant_id,
                target_type=PermissionTargetType.TARGET_MEMBER,
                target_id=tenant_id,
                resource_type=ResourceType.LLM,
                resource_id=req["llm_factory"],
                old_permission=PermissionValue.PERMISSION_OWNER.value,
                new_permission=PermissionValue.PERMISSION_NULL.value,
                action_type=PermissionActionType.ACTION_DELETE,
            ):
                raise ValueError("Permission change log creation failed")

        dialogs = DialogService.query(
            status=StatusEnum.VALID.value,
            tenant_id=tenant_id,
        )
        filtered_dialog_ids = []
        for dialog in dialogs:
            if req["llm_factory"] in dialog.llm_id:
                filtered_dialog_ids.append(dialog.id)

        with DB.atomic():
            for dialog_id in filtered_dialog_ids:
                dialog_permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(tenant_id=tenant_id, resource_id=dialog_id, resource_type=ResourceType.DIALOG)
                PermissionService.delete(dialog_permission_model_list)

        return get_json_result(data=True)

    except Exception as e:
        return server_error_response(e)


@manager.route("/my_llms", methods=["GET"])  # noqa: F821
@login_required
def my_llms():
    import logging
    logger = logging.getLogger()
    try:
        TenantLLMService.ensure_mineru_from_env(current_user.id)
        TenantLLMService.ensure_opendataloader_from_env(current_user.id)
        include_details = request.args.get("include_details", "false").lower() == "true"

        if include_details:
            res = {}
            objs = TenantLLMService.query(tenant_id=current_user.id)
            factories = LLMFactoriesService.query(status=StatusEnum.VALID.value)

            # For Builtin factory, only show the model specified in TEI_MODEL
            import os
            tei_model = os.getenv("TEI_MODEL", "")
            if tei_model:
                logger.info(f"[my_llms] TEI_MODEL={tei_model}, filtering Builtin models")
                objs = [o for o in objs if not (o.llm_factory == "Builtin" and o.llm_name != tei_model)]

            logger.info(f"[my_llms] tenant_id: {current_user.id}, total tenant_llms: {len(objs)}")
            logger.info(f"[my_llms] tenant_llm factories: {[o.llm_factory for o in objs]}")

            for o in objs:
                try:
                    o_dict = o.to_dict()
                    logger.info(f"[my_llms] Processing tenant_llm: {o_dict}")

                    factory_tags = None
                    for f in factories:
                        if f.name == o_dict["llm_factory"]:
                            factory_tags = f.tags
                            logger.info(f"[my_llms] Found factory {f.name} with tags: {factory_tags}")
                            break

                    if o_dict["llm_factory"] not in res:
                        res[o_dict["llm_factory"]] = {"tags": factory_tags, "llm": []}

                    res[o_dict["llm_factory"]]["llm"].append(
                        {
                            "id": o_dict["id"],
                            "type": o_dict["model_type"],
                            "name": o_dict["llm_name"],
                            "used_token": o_dict["used_tokens"],
                            "api_base": o_dict["api_base"] or "",
                            "max_tokens": o_dict["max_tokens"] or 8192,
                            "status": o_dict["status"] or "1",
                            "is_tools": _resolve_my_llm_is_tools(o_dict),
                        }
                    )
                except Exception as e:
                    logger.error(f"[my_llms] Error processing tenant_llm object: {e}", exc_info=True)
                    raise
        else:
            res = {}
            logger.info(f"[my_llms] Calling get_my_llms for tenant_id: {current_user.id}")
            my_llms_list = TenantLLMService.get_my_llms(current_user.id)
            logger.info(f"[my_llms] get_my_llms returned {len(my_llms_list)} items")
            for o in my_llms_list:
                logger.info(f"[my_llms] get_my_llms item: {o}")
                if o["llm_factory"] not in res:
                    res[o["llm_factory"]] = {"tags": o["tags"], "llm": []}
                res[o["llm_factory"]]["llm"].append({"id": o["id"], "type": o["model_type"], "name": o["llm_name"], "used_token": o["used_tokens"], "status": o["status"]})

        logger.info(f"[my_llms] Final result: {res}")
        return get_json_result(data=res)
    except Exception as e:
        logger.error(f"[my_llms] Exception: {e}", exc_info=True)
        return server_error_response(e)


@manager.route("/list", methods=["GET"])  # noqa: F821
@login_required
def list_app():
    """
    list all available apps for a user, including joined team (tenant)
    """
    res = {}
    self_deployed = ["Youdao", "FastEmbed", "BAAI", "Ollama", "Xinference", "LocalAI", "LM-Studio", "GPUStack", "OpenAI-API-Compatible"]
    tenants = UserTenantService.get_tenants_by_user_id(user_id=current_user.id)
    model_type = request.args.get("model_type")

    for tenant in tenants:
        tenant_id = tenant["tenant_id"]
        from_other = tenant_id != current_user.id
        tenant_name = tenant["nickname"]
        member_id = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
        try:
            objs = TenantLLMService.query(tenant_id=tenant_id)
            facts = set([o.to_dict()["llm_factory"] for o in objs if o.api_key])
            available_fact = []
            llms = []
            if from_other:
                for factory in facts:
                    p = has_permission_for_member(operator_id=member_id, tenant_id=tenant_id, resource_id=factory, resource_type=ResourceType.LLM, permission=PermissionValue.PERMISSION_READ)
                    if p and p[0]:
                        available_fact.append(factory)
                llms = LLMService.get_all()
                llms = [m.to_dict() for m in llms if m.status == StatusEnum.VALID.value]
                for m in llms:
                    m["available"] = m["fid"] in available_fact

                # For Builtin factory, filter models by TEI_MODEL environment variable
                import os
                tei_model = os.getenv("TEI_MODEL", "")
                if tei_model:
                    logger.info(f"[list] TEI_MODEL={tei_model}, filtering Builtin models (from_other)")
                    llms = [m for m in llms if not (m["fid"] == "Builtin" and m["llm_name"] != tei_model)]
            else:
                llm_set = set([m["llm_name"] + "@" + m["fid"] for m in llms])
                for o in objs:
                    if not o.api_key and o.llm_factory not in self_deployed:
                        continue
                    if o.llm_name + "@" + o.llm_factory in llm_set:
                        continue
                    llms.append({"llm_name": o.llm_name, "model_type": o.model_type, "fid": o.llm_factory, "available": True, "status": StatusEnum.VALID.value})

            # For Builtin factory, filter models by TEI_MODEL environment variable
            import os
            tei_model = os.getenv("TEI_MODEL", "")
            if tei_model:
                logger.info(f"[list] TEI_MODEL={tei_model}, filtering Builtin models")
                llms = [m for m in llms if not (m.get("fid") == "Builtin" and m.get("llm_name") != tei_model)]

            for m in llms:
                m["tenant_id"] = tenant_id
                m["tenant_name"] = tenant_name

                if model_type and m["model_type"].find(model_type) < 0:
                    continue
                if m["fid"] not in res:
                    res[m["fid"]] = []
                res[m["fid"]].append(m)

        except Exception as e:
            return server_error_response(e)
    return get_json_result(data=res)


@manager.route('/set_default_llm', methods=['POST'])  # noqa: F821
@login_required
@validate_request("llm_factory", "llm_name")
async def set_default_llm():
    from common import settings
    from api.db.services import UserService
    if not settings.ENABLE_ADMIN or not UserService.is_admin(current_user.id):
        return get_data_error_result(message="Not authorized.")

    req = await request.get_json()
    llm_factory = req["llm_factory"]
    llm_name = req["llm_name"]
    llms = TenantLLMService.query(tenant_id=current_user.id, llm_factory=llm_factory, llm_name=llm_name)
    if not llms:
        return get_data_error_result(message="Can't load this LLM configuration: {}/{}")

    TenantLLMService.reset_all_default_model(llms[0])
    return get_json_result(data=True)
