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
from common.constants import LLMType
from api.db.services.tenant_llm_service import TenantLLMService


MODEL_PARAM_TYPE_MAP = {
    "llm_id": LLMType.CHAT,
    "embd_id": LLMType.EMBEDDING,
    "asr_id": LLMType.SPEECH2TEXT,
    "img2txt_id": LLMType.IMAGE2TEXT,
    "rerank_id": LLMType.RERANK,
    "tts_id": LLMType.TTS,
}


def ensure_tenant_model_id_for_params(tenant_id: str, param_dict: dict) -> dict:
    for key, model_type in MODEL_PARAM_TYPE_MAP.items():
        tenant_key = f"tenant_{key}"
        model_name = param_dict.get(key)
        if not model_name or param_dict.get(tenant_key):
            continue

        _, _, other_tenant_id = TenantLLMService.split_model_name_and_factory(model_name)
        lookup_tenant_id = other_tenant_id or tenant_id
        tenant_model = TenantLLMService.get_api_key(lookup_tenant_id, model_name, model_type)
        if not tenant_model:
            raise LookupError(
                f"Tenant model for '{key}' with name '{model_name}' is not found or not authorized."
            )
        param_dict[tenant_key] = tenant_model.id
    return param_dict
