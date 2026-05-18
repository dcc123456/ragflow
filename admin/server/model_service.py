#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
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
from common.misc_utils import get_uuid
from common.constants import StatusEnum, LLMType
from api.db import PermissionActionType, PermissionTargetType, PermissionValue, ResourceType
from api.db.db_models import DB, TenantLLM
from api.db.services.llm_service import LLMService
from api.db.services.tenant_llm_service import (
    TenantLLMService,
    LLMFactoriesService,
    get_enabled_tei_model,
    is_tei_enabled,
)
from api.db.services.user_service import UserTenantService
from api.db.services.dialog_service import DialogService
from api.utils.api_utils import get_allowed_llm_factories
from api.utils.permission_utils import has_permission_for_member
from api.db.services.permission_service import PermissionChangeLogService, PermissionService
from rag.utils.base64_image import test_image
from rag.llm import EmbeddingModel, ChatModel, RerankModel, CvModel, TTSModel, OcrModel, Seq2txtModel


class ModelMgr:
    @staticmethod
    def _filter_builtin_tei_models(llms):
        if not is_tei_enabled():
            return [m for m in llms if m.get("fid") != "Builtin"]
        tei_model = get_enabled_tei_model()
        if tei_model:
            return [m for m in llms if not (m.get("fid") == "Builtin" and m.get("llm_name") != tei_model)]
        return llms

    @staticmethod
    def get_factories():
        factory_objs = get_allowed_llm_factories()
        # Note: Builtin is now supported for TEI models, so it's not filtered out
        factory_list = [f.to_dict() for f in factory_objs if f.name not in ["Youdao", "FastEmbed", "BAAI"]]
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

        for f in factory_list:
            f["model_types"] = list(
                mdl_types.get(
                    f["name"],
                    [LLMType.CHAT, LLMType.EMBEDDING, LLMType.RERANK, LLMType.IMAGE2TEXT, LLMType.SPEECH2TEXT, LLMType.TTS, LLMType.OCR],
                )
            )

        return factory_list

    @staticmethod
    async def set_api_key(tenant_id: str, llm_factory: str, api_key: str, base_url: str, model_type: str=None, llm_name: str=None):
        # test if api key works
        chat_passed, embd_passed, rerank_passed = False, False, False
        extra = {"provider": llm_factory}
        msg = ""
        for llm in LLMService.query(fid=llm_factory):
            if not embd_passed and llm.model_type == LLMType.EMBEDDING.value:
                assert llm_factory in EmbeddingModel, f"Embedding model from {llm_factory} is not supported yet."
                mdl = EmbeddingModel[llm_factory](api_key, llm.llm_name, base_url=base_url)
                try:
                    arr, tc = mdl.encode(["Test if the api key is available"])
                    if len(arr[0]) == 0:
                        raise Exception("Fail")
                    embd_passed = True
                except Exception as e:
                    msg += f"\nFail to access embedding model({llm.llm_name}) using this api key." + str(e)
            elif not chat_passed and llm.model_type == LLMType.CHAT.value:
                assert llm_factory in ChatModel, f"Chat model from {llm_factory} is not supported yet."
                mdl = ChatModel[llm_factory](api_key, llm.llm_name, base_url=base_url, **extra)
                try:
                    m, tc = await mdl.async_chat(None, [{"role": "user", "content": "Hello! How are you doing!"}],
                                                 {"temperature": 0.9, "max_tokens": 50})
                    if m.find("**ERROR**") >= 0:
                        raise Exception(m)
                    chat_passed = True
                except Exception as e:
                    msg += f"\nFail to access model({llm.fid}/{llm.llm_name}) using this api key." + str(e)
            elif not rerank_passed and llm.model_type == LLMType.RERANK:
                assert llm_factory in RerankModel, f"Re-rank model from {llm_factory} is not supported yet."
                mdl = RerankModel[llm_factory](api_key, llm.llm_name, base_url=base_url)
                try:
                    arr, tc = mdl.similarity("What's the weather?", ["Is it sunny today?"])
                    if len(arr) == 0 or tc == 0:
                        raise Exception("Fail")
                    rerank_passed = True
                    logging.debug(f"passed model rerank {llm.llm_name}")
                except Exception as e:
                    msg += f"\nFail to access model({llm.fid}/{llm.llm_name}) using this api key." + str(e)
            if any([embd_passed, chat_passed, rerank_passed]):
                msg = ""
                break

        if msg:
            return False, msg

        llm_config = {"api_key": api_key, "api_base": base_url}
        if model_type:
            llm_config["model_type"] = model_type
        if llm_name:
            llm_config["llm_name"] = llm_name

        for llm in LLMService.query(fid=llm_factory):
            llm_config["max_tokens"] = llm.max_tokens
            if not TenantLLMService.filter_update(
                    [TenantLLM.tenant_id == tenant_id, TenantLLM.llm_factory == llm_factory,
                     TenantLLM.llm_name == llm.llm_name], llm_config):
                TenantLLMService.save(
                    tenant_id=tenant_id,
                    llm_factory=llm_factory,
                    llm_name=llm.llm_name,
                    model_type=llm.model_type,
                    api_key=llm_config["api_key"],
                    api_base=llm_config["api_base"],
                    max_tokens=llm_config["max_tokens"],
                )
        return True, "Successfully set api key."

    @staticmethod
    async def add_llm(tenant_id: str, llm_factory: str, api_key: str, llm_name: str, model_type: str, api_base: str, max_tokens: int):
        if llm_factory not in [f.name for f in get_allowed_llm_factories()]:
            return False, f"LLM factory {llm_factory} is not allowed"

        llm = {
            "tenant_id": tenant_id,
            "llm_factory": llm_factory,
            "model_type": model_type,
            "llm_name": llm_name,
            "api_base": api_base,
            "api_key": api_key,
            "max_tokens": max_tokens,
        }

        msg = ""
        mdl_nm = llm["llm_name"].split("___")[0]
        extra = {"provider": llm_factory}
        model_type = llm["model_type"]
        model_api_key = llm["api_key"]
        model_base_url = llm.get("api_base", "")
        match model_type:
            case LLMType.EMBEDDING.value:
                assert llm_factory in EmbeddingModel, f"Embedding model from {llm_factory} is not supported yet."
                mdl = EmbeddingModel[llm_factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
                try:
                    arr, tc = mdl.encode(["Test if the api key is available"])
                    if len(arr[0]) == 0:
                        raise Exception("Fail")
                except Exception as e:
                    msg += f"\nFail to access embedding model({mdl_nm})." + str(e)
            case LLMType.CHAT.value:
                assert llm_factory in ChatModel, f"Chat model from {llm_factory} is not supported yet."
                mdl = ChatModel[llm_factory](
                    key=model_api_key,
                    model_name=mdl_nm,
                    base_url=model_base_url,
                    **extra,
                )
                try:
                    m, tc = await mdl.async_chat(None, [{"role": "user", "content": "Hello! How are you doing!"}],
                                                 {"temperature": 0.9})
                    if not tc and m.find("**ERROR**:") >= 0:
                        raise Exception(m)
                except Exception as e:
                    msg += f"\nFail to access model({llm_factory}/{mdl_nm})." + str(e)

            case LLMType.RERANK.value:
                assert llm_factory in RerankModel, f"RE-rank model from {llm_factory} is not supported yet."
                try:
                    mdl = RerankModel[llm_factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
                    arr, tc = mdl.similarity("Hello~ RAGFlower!", ["Hi, there!", "Ohh, my friend!"])
                    if len(arr) == 0:
                        raise Exception("Not known.")
                except KeyError:
                    msg += f"{llm_factory} dose not support this model({llm_factory}/{mdl_nm})"
                except Exception as e:
                    msg += f"\nFail to access model({llm_factory}/{mdl_nm})." + str(e)

            case LLMType.IMAGE2TEXT.value:
                assert llm_factory in CvModel, f"Image to text model from {llm_factory} is not supported yet."
                mdl = CvModel[llm_factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
                try:
                    image_data = test_image
                    m, tc = mdl.describe(image_data)
                    if not tc and m.find("**ERROR**:") >= 0:
                        raise Exception(m)
                except Exception as e:
                    msg += f"\nFail to access model({llm_factory}/{mdl_nm})." + str(e)
            case LLMType.TTS.value:
                assert llm_factory in TTSModel, f"TTS model from {llm_factory} is not supported yet."
                mdl = TTSModel[llm_factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
                try:
                    for _ in mdl.tts("Hello~ RAGFlower!"):
                        pass
                except RuntimeError as e:
                    msg += f"\nFail to access model({llm_factory}/{mdl_nm})." + str(e)
            case LLMType.OCR.value:
                assert llm_factory in OcrModel, f"OCR model from {llm_factory} is not supported yet."
                try:
                    mdl = OcrModel[llm_factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
                    ok, reason = mdl.check_available()
                    if not ok:
                        raise RuntimeError(reason or "Model not available")
                except Exception as e:
                    msg += f"\nFail to access model({llm_factory}/{mdl_nm})." + str(e)
            case LLMType.SPEECH2TEXT:
                assert llm_factory in Seq2txtModel, f"Speech model from {llm_factory} is not supported yet."
                try:
                    mdl = Seq2txtModel[llm_factory](key=model_api_key, model_name=mdl_nm, base_url=model_base_url)
                    # TODO: check the availability
                except Exception as e:
                    msg += f"\nFail to access model({llm_factory}/{mdl_nm})." + str(e)
            case _:
                raise RuntimeError(f"Unknown model type: {model_type}")

        if msg:
            return False, msg

        operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=tenant_id)
        if not operator:
            return False, "Unrecognized identification."

        if not TenantLLMService.filter_update([TenantLLM.tenant_id == tenant_id, TenantLLM.llm_factory == llm_factory,
                                               TenantLLM.llm_name == llm["llm_name"]], llm):
            TenantLLMService.save(**llm)

            if not PermissionService.save(
                    id=get_uuid(),
                    member_id=operator.id,
                    tenant_id=tenant_id,
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

        return True, "Successfully added llm."

    @staticmethod
    def delete_factory(tenant_id: str, llm_factory: str):
        TenantLLMService.filter_delete([TenantLLM.tenant_id == tenant_id, TenantLLM.llm_factory == llm_factory])
        with DB.atomic():
            permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(tenant_id=tenant_id, resource_id=llm_factory, resource_type=ResourceType.LLM)
            PermissionService.delete(permission_model_list)

            if not PermissionChangeLogService.save(
                    id=get_uuid(),
                    tenant_id=tenant_id,
                    operator_id=tenant_id,
                    target_type=PermissionTargetType.TARGET_MEMBER,
                    target_id=tenant_id,
                    resource_type=ResourceType.LLM,
                    resource_id=llm_factory,
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
            if llm_factory in dialog.llm_id:
                filtered_dialog_ids.append(dialog.id)

        with DB.atomic():
            for dialog_id in filtered_dialog_ids:
                dialog_permission_model_list = PermissionService.get_permissions_by_tenant_and_resource_id(
                    tenant_id=tenant_id, resource_id=dialog_id, resource_type=ResourceType.DIALOG)
                PermissionService.delete(dialog_permission_model_list)

        return True

    @staticmethod
    def get_my_llms(tenant_id: str, include_details: bool=False):

        if include_details:
            res = {}
            objs = TenantLLMService.query(tenant_id=tenant_id)
            factories = LLMFactoriesService.query(status=StatusEnum.VALID.value)

            # Builtin embedding models are backed by TEI. Hide them when TEI is disabled.
            tei_model = get_enabled_tei_model()
            if not is_tei_enabled():
                objs = [o for o in objs if o.llm_factory != "Builtin"]
            elif tei_model:
                objs = [o for o in objs if not (o.llm_factory == "Builtin" and o.llm_name != tei_model)]

            for o in objs:
                try:
                    o_dict = o.to_dict()

                    factory_tags = None
                    for f in factories:
                        if f.name == o_dict["llm_factory"]:
                            factory_tags = f.tags
                            break

                    if o_dict["llm_factory"] not in res:
                        res[o_dict["llm_factory"]] = {"tags": factory_tags, "llm": []}

                    res[o_dict["llm_factory"]]["llm"].append(
                        {
                            "type": o_dict["model_type"],
                            "name": o_dict["llm_name"],
                            "used_token": o_dict["used_tokens"],
                            "api_base": o_dict["api_base"] or "",
                            "max_tokens": o_dict["max_tokens"] or 8192,
                            "status": o_dict["status"] or "1",
                        }
                    )
                except Exception as e:
                    raise e
        else:
            res = {}
            my_llms_list = TenantLLMService.get_my_llms(tenant_id)
            for o in my_llms_list:
                if o["llm_factory"] not in res:
                    res[o["llm_factory"]] = {"tags": o["tags"], "llm": []}
                res[o["llm_factory"]]["llm"].append(
                    {"type": o["model_type"], "name": o["llm_name"], "used_token": o["used_tokens"],
                     "status": o["status"]})

        return res

    @staticmethod
    def list_app(user_id: str, model_type: str):
        res = {}
        self_deployed = ["Youdao", "FastEmbed", "BAAI", "Ollama", "Xinference", "LocalAI", "LM-Studio", "GPUStack",
                         "OpenAI-API-Compatible"]
        tenants = UserTenantService.get_tenants_by_user_id(user_id=user_id)

        for tenant in tenants:
            tenant_id = tenant["tenant_id"]
            from_other = tenant_id != user_id
            tenant_name = tenant["nickname"]
            member_id = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=user_id)
            try:
                objs = TenantLLMService.query(tenant_id=tenant_id)
                facts = set([o.to_dict()["llm_factory"] for o in objs if o.api_key])
                available_fact = []
                llms = []
                if from_other:
                    for factory in facts:
                        p = has_permission_for_member(operator_id=member_id, tenant_id=tenant_id, resource_id=factory,
                                                      resource_type=ResourceType.LLM,
                                                      permission=PermissionValue.PERMISSION_READ)
                        if p and p[0]:
                            available_fact.append(factory)
                    llms = LLMService.get_all()
                    llms = [m.to_dict() for m in llms if m.status == StatusEnum.VALID.value]
                    for m in llms:
                        m["available"] = m["fid"] in available_fact

                    llms = ModelMgr._filter_builtin_tei_models(llms)
                else:
                    llm_set = set([m["llm_name"] + "@" + m["fid"] for m in llms])
                    for o in objs:
                        if not o.api_key and o.llm_factory not in self_deployed:
                            continue
                        if o.llm_name + "@" + o.llm_factory in llm_set:
                            continue
                        llms.append({"llm_name": o.llm_name, "model_type": o.model_type, "fid": o.llm_factory,
                                     "available": True, "status": StatusEnum.VALID.value})

                llms = ModelMgr._filter_builtin_tei_models(llms)

                for m in llms:
                    m["tenant_id"] = tenant_id
                    m["tenant_name"] = tenant_name

                    if model_type and m["model_type"].find(model_type) < 0:
                        continue
                    if m["fid"] not in res:
                        res[m["fid"]] = []
                    res[m["fid"]].append(m)

            except Exception as e:
                return False, str(e)
        return True, res
