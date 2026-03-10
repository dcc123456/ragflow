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
from types import SimpleNamespace
import warnings

import pytest

# xgboost imports pkg_resources and emits a deprecation warning that is promoted
# to error in our pytest configuration; ignore it for this unit test module.
warnings.filterwarnings(
    "ignore",
    message="pkg_resources is deprecated as an API.*",
    category=UserWarning,
)

from api.db.joint_services import tenant_model_service
from common.constants import LLMType


@pytest.mark.p2
def test_get_model_config_by_type_and_name_uses_shared_tenant_suffix(monkeypatch):
    calls = []

    def _get_api_key(tenant_id, model_name):
        calls.append((tenant_id, model_name))
        return SimpleNamespace(
            to_dict=lambda: {
                "id": 1,
                "llm_name": "bge-m3",
                "llm_factory": "Builtin",
                "model_type": LLMType.EMBEDDING.value,
                "api_key": "k",
                "api_base": "http://example.com",
            }
        )

    monkeypatch.setattr(
        tenant_model_service.TenantLLMService,
        "split_model_name_and_factory",
        lambda model_name: ("bge-m3", "Builtin", "shared-tenant"),
    )
    monkeypatch.setattr(tenant_model_service.TenantLLMService, "get_api_key", _get_api_key)
    monkeypatch.setattr(tenant_model_service.LLMService, "query", lambda **_kwargs: [])

    config = tenant_model_service.get_model_config_by_type_and_name(
        "current-tenant",
        LLMType.EMBEDDING,
        "bge-m3@Builtin#shared-tenant",
    )

    assert config["llm_name"] == "bge-m3"
    assert calls == [("shared-tenant", "bge-m3@Builtin#shared-tenant")]


@pytest.mark.p2
def test_get_model_config_by_type_and_name_falls_back_to_plain_name_on_shared_tenant(monkeypatch):
    calls = []

    def _get_api_key(tenant_id, model_name):
        calls.append((tenant_id, model_name))
        if model_name == "bge-m3":
            return SimpleNamespace(
                to_dict=lambda: {
                    "id": 1,
                    "llm_name": "bge-m3",
                    "llm_factory": "Builtin",
                    "model_type": LLMType.EMBEDDING.value,
                    "api_key": "k",
                    "api_base": "http://example.com",
                }
            )
        return None

    monkeypatch.setattr(
        tenant_model_service.TenantLLMService,
        "split_model_name_and_factory",
        lambda model_name: ("bge-m3", "Builtin", "shared-tenant"),
    )
    monkeypatch.setattr(tenant_model_service.TenantLLMService, "get_api_key", _get_api_key)
    monkeypatch.setattr(tenant_model_service.LLMService, "query", lambda **_kwargs: [])

    config = tenant_model_service.get_model_config_by_type_and_name(
        "current-tenant",
        LLMType.EMBEDDING,
        "bge-m3@Builtin#shared-tenant",
    )

    assert config["llm_name"] == "bge-m3"
    assert calls == [
        ("shared-tenant", "bge-m3@Builtin#shared-tenant"),
        ("shared-tenant", "bge-m3"),
    ]
