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
from api.db.services.tenant_llm_service import (
    LLMFactoriesService,
    TenantLLMService,
    get_enabled_tei_model,
    is_tei_enabled,
)
from api.db.services.llm_service import LLMService
from api.utils.api_utils import server_error_response, get_data_error_result, get_json_result, validate_request
from quart import request
from api.apps import login_required, current_user
from common.constants import StatusEnum
from api.db import PermissionValue, ResourceType
from api.db.services.user_service import UserTenantService
from api.utils.permission_utils import has_permission_for_member

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


def _filter_builtin_tei_models(llms):
    if not is_tei_enabled():
        return [m for m in llms if m.get("fid") != "Builtin"]
    tei_model = get_enabled_tei_model()
    if tei_model:
        logger.info(f"TEI_MODEL={tei_model}, filtering Builtin models")
        return [m for m in llms if not (m.get("fid") == "Builtin" and m.get("llm_name") != tei_model)]
    return llms


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

            # Builtin embedding models are backed by TEI. Hide them when TEI is disabled.
            tei_model = get_enabled_tei_model()
            if not is_tei_enabled():
                objs = [o for o in objs if o.llm_factory != "Builtin"]
            elif tei_model:
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

                llms = _filter_builtin_tei_models(llms)
            else:
                llm_set = set([m["llm_name"] + "@" + m["fid"] for m in llms])
                for o in objs:
                    if not o.api_key and o.llm_factory not in self_deployed:
                        continue
                    if o.llm_name + "@" + o.llm_factory in llm_set:
                        continue
                    llms.append({"llm_name": o.llm_name, "model_type": o.model_type, "fid": o.llm_factory, "available": True, "status": StatusEnum.VALID.value})

            llms = _filter_builtin_tei_models(llms)

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


@manager.route("/set_default_llm", methods=["POST"])  # noqa: F821
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
