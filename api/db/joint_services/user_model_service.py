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
from common.constants import ModelType
from api.db.services.user_service import UserService, TenantService
from api.db.services.role_model_service import RoleDefaultModelService


def get_user_default_model(user_id: str, model_type: str):
    tenant_attr_name_map: dict = {ModelType.LLM: "llm_id", ModelType.EMBEDDING: "embd_id", ModelType.VLM: "img2txt_id", ModelType.ASR: "asr_id", ModelType.RERANK: "rerank_id", ModelType.TTS: "tts_id"}
    tenant_info_list = TenantService.get_info_by(user_id)
    if tenant_info_list:
        tenant_info = tenant_info_list[0]
        model_id = tenant_info.get(tenant_attr_name_map[model_type], None)
        if model_id:
            return model_id, user_id
    user = UserService.get_by_id(user_id)
    role_default_model = RoleDefaultModelService.get_by_role_id_and_model_type(user.role_id, model_type)
    if role_default_model:
        return role_default_model.model_id, role_default_model.tenant_id
    return None
