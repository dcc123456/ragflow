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
import os
import json
import logging
import time
from copy import deepcopy

import peewee
from peewee import IntegrityError
from langfuse import Langfuse

from common import settings
from peewee import JOIN

from api.db import PermissionValue, ResourceType, UserTenantRole
from api.db.db_models import Permission
from common.constants import MINERU_DEFAULT_CONFIG, MINERU_ENV_KEYS, LLMType
from api.db.db_models import DB, LLMFactories, TenantLLM
from api.db.services.common_service import CommonService
from api.db.services.langfuse_service import TenantLangfuseService
from api.utils.billing import billing_set_customer_id
from api.db.services.user_service import UserTenantService
from common.constants import StatusEnum
from api.db.services.user_service import TenantService
from common.misc_utils import get_uuid
from rag.llm import ChatModel, CvModel, EmbeddingModel, OcrModel, RerankModel, Seq2txtModel, TTSModel


class LLMFactoriesService(CommonService):
    model = LLMFactories

    @classmethod
    @DB.connection_context()
    def factory_with_permission(cls, user_id):
        permission_conditions = (Permission.permission >= PermissionValue.PERMISSION_READ.value) & (Permission.status == StatusEnum.VALID.value) & (Permission.resource_type == ResourceType.LLM)
        query = (
            cls.model.select(cls.model.name, Permission.tenant_id)
            .join(Permission, JOIN.LEFT_OUTER, on=((Permission.resource_id == cls.model.name) & (Permission.member_id.endswith(peewee.fn.CONCAT("\_", user_id))) & (permission_conditions)))
            .where(
                (cls.model.status == 1)
                & (Permission.id.is_null(False))
            )
        )
        return list(query.dicts())


class TenantLLMService(CommonService):
    model = TenantLLM

    @classmethod
    @DB.connection_context()
    def get_api_key(cls, tenant_id, model_name):
        mdlnm, fid, _ = TenantLLMService.split_model_name_and_factory(model_name)
        if not fid:
            objs = cls.query(tenant_id=tenant_id, llm_name=mdlnm)
        else:
            objs = cls.query(tenant_id=tenant_id, llm_name=mdlnm, llm_factory=fid)

        if (not objs) and fid:
            if fid == "LocalAI":
                mdlnm += "___LocalAI"
            elif fid == "HuggingFace":
                mdlnm += "___HuggingFace"
            elif fid == "OpenAI-API-Compatible":
                mdlnm += "___OpenAI-API"
            elif fid == "VLLM":
                mdlnm += "___VLLM"
            objs = cls.query(tenant_id=tenant_id, llm_name=mdlnm, llm_factory=fid)
        if not objs:
            return None
        return objs[0]

    @classmethod
    @DB.connection_context()
    def get_my_llms(cls, tenant_id):
        import os
        fields = [cls.model.llm_factory, LLMFactories.logo, LLMFactories.tags, cls.model.model_type, cls.model.llm_name,
                  cls.model.used_tokens, cls.model.status]
        query = cls.model.select(*fields).join(LLMFactories, on=(cls.model.llm_factory == LLMFactories.name)).where(
            cls.model.tenant_id == tenant_id, ~cls.model.api_key.is_null())

        # For Builtin factory, only return the model specified in TEI_MODEL environment variable
        tei_model = os.getenv("TEI_MODEL", "")
        if tei_model:
            # Filter out other Builtin models that don't match TEI_MODEL
            query = query.where(
                ~((cls.model.llm_factory == "Builtin") & (cls.model.llm_name != tei_model))
            )

        objs = query.dicts()
        return list(objs)

    @classmethod
    @DB.connection_context()
    def get_my_llms_group_by_factory(cls, tenant_id):
        fields = [cls.model.llm_factory, LLMFactories.logo, LLMFactories.tags, cls.model.model_type, cls.model.llm_name, cls.model.used_tokens]
        objs = (
            cls.model.select(*fields)
            .join(LLMFactories, on=(cls.model.llm_factory == LLMFactories.name))
            .where(cls.model.tenant_id == tenant_id, ~cls.model.api_key.is_null())
            .group_by(cls.model.llm_factory)
            .dicts()
        )

        return list(objs)

    @staticmethod
    def split_model_name_and_factory(model_name):
        arr = model_name.split("@")
        if len(arr) < 2:
            return model_name, None, None

        model, factory_part = arr[0], arr[1]

        if "#" in factory_part:
            factory_and_tenant = factory_part.split("#")
            if len(factory_and_tenant) == 2:
                factory, tenant_id = factory_and_tenant
            elif len(factory_and_tenant) == 1:
                factory, tenant_id = factory_and_tenant[0], None
            return model, factory, tenant_id
        else:
            factory, tenant_id = factory_part, None

        try:
            model_factories = settings.FACTORY_LLM_INFOS
            model_providers = set([f["name"] for f in model_factories])
            if factory not in model_providers:
                return model_name, None, None
            return model, factory, tenant_id
        except Exception as e:
            logging.exception(f"TenantLLMService.split_model_name_and_factory got unexpected exception: {e}")
        return model_name, None, None

    @classmethod
    @DB.connection_context()
    def get_model_config(cls, tenant_id, llm_type, llm_name=None):
        from api.db.services.llm_service import LLMService
        e, tenant = TenantService.get_by_id(tenant_id)
        if not e:
            raise LookupError("Tenant not found")

        if llm_type == LLMType.EMBEDDING.value:
            mdlnm = tenant.embd_id if not llm_name else llm_name
        elif llm_type == LLMType.SPEECH2TEXT.value:
            mdlnm = tenant.asr_id if not llm_name else llm_name
        elif llm_type == LLMType.IMAGE2TEXT.value:
            mdlnm = tenant.img2txt_id if not llm_name else llm_name
        elif llm_type == LLMType.CHAT.value:
            mdlnm = tenant.llm_id if not llm_name else llm_name
        elif llm_type == LLMType.RERANK:
            mdlnm = tenant.rerank_id if not llm_name else llm_name
        elif llm_type == LLMType.TTS:
            mdlnm = tenant.tts_id if not llm_name else llm_name
        elif llm_type == LLMType.OCR:
            if not llm_name:
                raise LookupError("OCR model name is required")
            mdlnm = llm_name
        else:
            assert False, "LLM type error"

        mdlnm, fid, other_tenant_id = TenantLLMService.split_model_name_and_factory(mdlnm)
        if other_tenant_id:
            model_config = cls.get_api_key(other_tenant_id, mdlnm)
        else:
            model_config = cls.get_api_key(tenant_id, mdlnm)
        if model_config:
            model_config = model_config.to_dict()
            llm = LLMService.query(llm_name=mdlnm) if not fid else LLMService.query(llm_name=mdlnm, fid=fid)
            if not llm and fid:  # for some cases seems fid mismatch
                llm = LLMService.query(llm_name=mdlnm)
            if llm:
                model_config["is_tools"] = llm[0].is_tools
        if not model_config:
            if llm_type in [LLMType.EMBEDDING, LLMType.RERANK]:
                llm = LLMService.query(llm_name=mdlnm) if not fid else LLMService.query(llm_name=mdlnm, fid=fid)
                if llm and llm[0].fid in ["Youdao", "FastEmbed", "BAAI"]:
                    model_config = {"llm_factory": llm[0].fid, "api_key": "", "llm_name": mdlnm, "api_base": ""}
            if not model_config:
                if mdlnm in ["bge-large-zh-v1.5", "bge-large-en-v1.5", "bge-m3"]:
                    model_config = {"llm_factory": "BAAI", "api_key": "", "llm_name": mdlnm, "api_base": ""}
                else:
                    if not mdlnm:
                        raise LookupError(f"Type of {llm_type} model is not set.")
                    raise LookupError("Model({}/{}) not authorized".format(fid, mdlnm))
        return model_config

    @classmethod
    @DB.connection_context()
    def model_instance(cls, tenant_id, llm_type, llm_name=None, lang="Chinese", **kwargs):
        model_config = TenantLLMService.get_model_config(tenant_id, llm_type, llm_name)
        kwargs.update({"provider": model_config["llm_factory"]})
        if llm_type == LLMType.EMBEDDING.value:
            if model_config["llm_factory"] not in EmbeddingModel:
                return None
            return EmbeddingModel[model_config["llm_factory"]](model_config["api_key"], model_config["llm_name"],
                                                               base_url=model_config["api_base"])

        elif llm_type == LLMType.RERANK:
            if model_config["llm_factory"] not in RerankModel:
                return None
            return RerankModel[model_config["llm_factory"]](model_config["api_key"], model_config["llm_name"],
                                                            base_url=model_config["api_base"])

        elif llm_type == LLMType.IMAGE2TEXT.value:
            if model_config["llm_factory"] not in CvModel:
                return None
            return CvModel[model_config["llm_factory"]](model_config["api_key"], model_config["llm_name"], lang,
                                                        base_url=model_config["api_base"], **kwargs)

        elif llm_type == LLMType.CHAT.value:
            if model_config["llm_factory"] not in ChatModel:
                return None
            return ChatModel[model_config["llm_factory"]](model_config["api_key"], model_config["llm_name"],
                                                          base_url=model_config["api_base"], **kwargs)

        elif llm_type == LLMType.SPEECH2TEXT:
            if model_config["llm_factory"] not in Seq2txtModel:
                return None
            return Seq2txtModel[model_config["llm_factory"]](key=model_config["api_key"],
                                                             model_name=model_config["llm_name"], lang=lang,
                                                             base_url=model_config["api_base"])
        elif llm_type == LLMType.TTS:
            if model_config["llm_factory"] not in TTSModel:
                return None
            return TTSModel[model_config["llm_factory"]](
                model_config["api_key"],
                model_config["llm_name"],
                base_url=model_config["api_base"],
            )

        elif llm_type == LLMType.OCR:
            if model_config["llm_factory"] not in OcrModel:
                return None
            return OcrModel[model_config["llm_factory"]](
                key=model_config["api_key"],
                model_name=model_config["llm_name"],
                base_url=model_config.get("api_base", ""),
                **kwargs,
            )

        return None

    @classmethod
    @DB.connection_context()
    def increase_usage(cls, tenant_id, llm_type, used_tokens, llm_name=None):
        e, tenant = TenantService.get_by_id(tenant_id)
        if not e:
            logging.error(f"Tenant not found: {tenant_id}")
            return 0

        llm_map = {
            LLMType.EMBEDDING.value: tenant.embd_id if not llm_name else llm_name,
            LLMType.SPEECH2TEXT.value: tenant.asr_id,
            LLMType.IMAGE2TEXT.value: tenant.img2txt_id,
            LLMType.CHAT.value: tenant.llm_id if not llm_name else llm_name,
            LLMType.RERANK.value: tenant.rerank_id if not llm_name else llm_name,
            LLMType.TTS.value: tenant.tts_id if not llm_name else llm_name,
            LLMType.OCR.value: llm_name,
        }

        mdlnm = llm_map.get(llm_type)
        if mdlnm is None:
            logging.error(f"LLM type error: {llm_type}")
            return 0

        llm_name, llm_factory, _ = TenantLLMService.split_model_name_and_factory(mdlnm)

        try:
            num = (
                cls.model.update(used_tokens=cls.model.used_tokens + used_tokens)
                .where(cls.model.tenant_id == tenant_id, cls.model.llm_name == llm_name,
                       cls.model.llm_factory == llm_factory if llm_factory else True)
                .execute()
            )
        except Exception:
            logging.exception(
                "TenantLLMService.increase_usage got exception,Failed to update used_tokens for tenant_id=%s, llm_name=%s",
                tenant_id, llm_name)
            return 0

        return num

    @classmethod
    @DB.connection_context()
    def get_openai_models(cls):
        objs = cls.model.select().where((cls.model.llm_factory == "OpenAI"),
                                        ~(cls.model.llm_name == "text-embedding-3-small"),
                                        ~(cls.model.llm_name == "text-embedding-3-large")).dicts()
        return list(objs)

    @classmethod
    def _collect_mineru_env_config(cls) -> dict | None:
        cfg = MINERU_DEFAULT_CONFIG
        found = False
        for key in MINERU_ENV_KEYS:
            val = os.environ.get(key)
            if val:
                found = True
                cfg[key] = val
        return cfg if found else None

    @classmethod
    @DB.connection_context()
    def ensure_mineru_from_env(cls, tenant_id: str) -> str | None:
        """
        Ensure a MinerU OCR model exists for the tenant if env variables are present.
        Return the existing or newly created llm_name, or None if env not set.
        """
        cfg = cls._collect_mineru_env_config()
        if not cfg:
            return None

        saved_mineru_models = cls.query(tenant_id=tenant_id, llm_factory="MinerU", model_type=LLMType.OCR.value)

        def _parse_api_key(raw: str) -> dict:
            try:
                return json.loads(raw or "{}")
            except Exception:
                return {}

        for item in saved_mineru_models:
            api_cfg = _parse_api_key(item.api_key)
            normalized = {k: api_cfg.get(k, MINERU_DEFAULT_CONFIG.get(k)) for k in MINERU_ENV_KEYS}
            if normalized == cfg:
                return item.llm_name

        used_names = {item.llm_name for item in saved_mineru_models}
        idx = 1
        base_name = "mineru-from-env"
        while True:
            candidate = f"{base_name}-{idx}"
            if candidate in used_names:
                idx += 1
                continue

            try:
                cls.save(
                    tenant_id=tenant_id,
                    llm_factory="MinerU",
                    llm_name=candidate,
                    model_type=LLMType.OCR.value,
                    api_key=json.dumps(cfg),
                    api_base="",
                    max_tokens=0,
                )
                return candidate
            except IntegrityError:
                logging.warning("MinerU env model %s already exists for tenant %s, retry with next name", candidate, tenant_id)
                used_names.add(candidate)
                idx += 1
                continue

    @classmethod
    @DB.connection_context()
    def delete_by_tenant_id(cls, tenant_id):
        return cls.model.delete().where(cls.model.tenant_id == tenant_id).execute()

    @staticmethod
    def llm_id2llm_type(llm_id: str) -> str | None:
        from api.db.services.llm_service import LLMService
        llm_id, *_ = TenantLLMService.split_model_name_and_factory(llm_id)
        llm_factories = settings.FACTORY_LLM_INFOS
        for llm_factory in llm_factories:
            for llm in llm_factory["llm"]:
                if llm_id == llm["llm_name"]:
                    return llm["model_type"].split(",")[-1]

        for llm in LLMService.query(llm_name=llm_id):
            return llm.model_type

        llm = TenantLLMService.get_or_none(llm_name=llm_id)
        if llm:
            return llm.model_type
        for llm in TenantLLMService.query(llm_name=llm_id):
            return llm.model_type
        return None

    @classmethod
    @DB.connection_context()
    def all_llm(cls):
        fields = [
            cls.model.llm_name,
            cls.model.model_type,
            cls.model.llm_factory.alias("fid")
        ]
        objs = cls.model.select(*fields).distinct().dicts()
        return list(objs)

    @classmethod
    @DB.connection_context()
    def reset_all_default_model(cls, llm):
        cls.model.update(
            llm_factory=llm.llm_factory,
            llm_name=llm.llm_name,
            model_type=llm.model_type,
            api_key=llm.api_key,
            api_base=llm.api_base,
            max_tokens=llm.max_tokens
        ).where(
            cls.model.llm_factory == llm.llm_factory,
            cls.model.llm_name == llm.llm_name
        ).execute()
        _llm = llm.to_dict()
        llm_type = llm.model_type
        llm_name = llm.llm_name + "@" + llm.llm_factory
        info = {}
        if llm_type == LLMType.EMBEDDING.value:
            info["embd_id"] = llm_name
        elif llm_type == LLMType.SPEECH2TEXT.value:
            info["asr_id"] = llm_name
        elif llm_type == LLMType.IMAGE2TEXT.value:
            info["img2txt_id"] = llm_name
        elif llm_type == LLMType.CHAT.value:
            info["llm_id"] = llm_name
        elif llm_type == LLMType.RERANK.value:
            info["rerank_id"] = llm_name
        elif llm_type == LLMType.TTS.value:
            info["tts_id"] = llm_name
        else:
            assert False, "LLM type error"
        for t in TenantService.get_all():
            if t.id == llm.tenant_id:
                continue

            _info = deepcopy(info)
            for k in _info.keys():
                _info[k] += "#" + t.id
            TenantService.update_by_id(t.id, _info)
            
            if cls.model.select().where(
                cls.model.tenant_id == t.id,
                cls.model.llm_factory == llm.llm_factory,
                cls.model.llm_name == llm.llm_name
            ).count() > 0:
                continue
            _llm["tenant_id"] = t.id
            cls.save(**_llm)


class LLM4Tenant:
    def __init__(self, tenant_id, llm_type, llm_name=None, lang="Chinese", **kwargs):
        self.tenant_id = tenant_id
        self.llm_type = llm_type
        self.llm_name = llm_name
        self.mdl = TenantLLMService.model_instance(tenant_id, llm_type, llm_name, lang=lang, **kwargs)
        assert self.mdl, "Can't find model for {}/{}/{}".format(tenant_id, llm_type, llm_name)
        model_config = TenantLLMService.get_model_config(tenant_id, llm_type, llm_name)
        self.max_length = model_config.get("max_tokens", 8192)

        self.is_tools = model_config.get("is_tools", False)
        self.verbose_tool_use = kwargs.get("verbose_tool_use")

        e, tenant = TenantService.get_by_id(tenant_id)
        if not e or not tenant:
            raise ValueError("Internal error")
        if llm_name:
            llm_id = llm_name
        elif llm_type == LLMType.EMBEDDING:
            llm_id = tenant.embd_id
        elif llm_type == LLMType.SPEECH2TEXT:
            llm_id = tenant.asr_id
        elif llm_type == LLMType.IMAGE2TEXT:
            llm_id = tenant.img2txt_id
        elif llm_type == LLMType.CHAT:
            llm_id = tenant.llm_id
        elif llm_type == LLMType.RERANK:
            llm_id = tenant.rerank_id
        elif llm_type == LLMType.TTS:
            llm_id = tenant.tts_id
        elif llm_type == LLMType.OCR:
            llm_id = llm_name
        else:
            assert False, "LLM type error"
        self.llm_name, factory, other_tenant_id = TenantLLMService.split_model_name_and_factory(llm_id)

        langfuse_keys = TenantLangfuseService.filter_by_tenant(tenant_id=tenant_id)
        self.langfuse = None
        if langfuse_keys:
            langfuse = Langfuse(public_key=langfuse_keys.public_key, secret_key=langfuse_keys.secret_key,
                                host=langfuse_keys.host)
            if langfuse.auth_check():
                self.langfuse = langfuse
                trace_id = self.langfuse.create_trace_id()
                self.trace_context = {"trace_id": trace_id}

        if not other_tenant_id or other_tenant_id == tenant_id:
            self.tenant_id = tenant_id
            self.mdl = TenantLLMService.model_instance(tenant_id=self.tenant_id, llm_type=self.llm_type, llm_name=self.llm_name, lang=lang)
            assert self.mdl, "Can't find model for {}/{}/{}".format(self.tenant_id, self.llm_type, self.llm_name)
            model_config = TenantLLMService.get_model_config(self.tenant_id, self.llm_type, self.llm_name)
            self.max_length = model_config.get("max_tokens", 8192)
        else:
            member = UserTenantService.filter_by_tenant_and_user_id(tenant_id=other_tenant_id, user_id=tenant_id)
            if not member:
                raise ValueError("Unrecognized identification.")

            self.tenant_id = other_tenant_id
            self.mdl = TenantLLMService.model_instance(tenant_id=other_tenant_id, llm_type=self.llm_type, llm_name=self.llm_name, lang=lang)
            assert self.mdl, "Can't find model for {}/{}/{}".format(other_tenant_id, self.llm_type, self.llm_name)
            model_config = TenantLLMService.get_model_config(other_tenant_id, self.llm_type, self.llm_name)
            self.max_length = model_config.get("max_tokens", 8192)



def user_register(user_id, user):
    from api.db.services.file_service import FileService
    from api.db import FileType
    from api.db.services import UserService
    from api.db.services.white_list_service import WhiteListService
    from api.db.services.llm_service import get_init_tenant_llm
    from common.settings import ENABLE_WHITELIST

    if ENABLE_WHITELIST and user["email"] != "admin@ragflow.io":
        user_email = user["email"]
        whitelist_row = WhiteListService.get_white_list_by_email(user_email)
        if not whitelist_row:
            raise ValueError(f"Email {user_email} isn't in whitelist.")

    user["id"] = user_id
    try:
        if not UserService.save(**user):
            return
    except Exception:
        return

    tenant = {
        "id": user_id,
        "name": user["nickname"] + "‘s Kingdom",
        "llm_id": settings.CHAT_MDL,
        "embd_id": settings.EMBEDDING_MDL,
        "asr_id": settings.ASR_MDL,
        "parser_ids": settings.PARSERS,
        "img2txt_id": settings.IMAGE2TEXT_MDL,
        "rerank_id": settings.RERANK_MDL,
    }
    usr_tenant = {
        "tenant_id": user_id,
        "user_id": user_id,
        "invited_by": user_id,
        "role": UserTenantRole.OWNER,
    }
    file_id = get_uuid()
    file = {
        "id": file_id,
        "parent_id": file_id,
        "tenant_id": user_id,
        "created_by": user_id,
        "name": "/",
        "type": FileType.FOLDER.value,
        "size": 0,
        "location": "",
    }
    tenant_llm = get_init_tenant_llm(user_id)
    TenantService.insert(**tenant)
    UserTenantService.insert(**usr_tenant)
    TenantLLMService.insert_many(tenant_llm)
    FileService.insert(file)
    billing_set_customer_id(user_id)
    time.sleep(3)
    return UserService.query(email=user["email"])
