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

import uuid

import pytest
from test.testcases.conftest import embedding_model_id


def _memory_embedding_id() -> str:
    return embedding_model_id()


@pytest.fixture
def memory_cleanup(rest_client):
    created_ids: list[str] = []

    def _cleanup():
        cleanup_errors = []
        for memory_id in created_ids:
            delete_res = rest_client.delete(f"/memories/{memory_id}")
            if delete_res.status_code != 200:
                cleanup_errors.append((memory_id, delete_res.status_code, delete_res.text))
                continue
            delete_payload = delete_res.json()
            if delete_payload["code"] not in (0, 103, 404):
                cleanup_errors.append((memory_id, delete_res.status_code, delete_payload))
        assert not cleanup_errors, f"Memory cleanup failed: {cleanup_errors}"

    yield created_ids
    _cleanup()


@pytest.fixture
def create_memory_resource(rest_client, memory_cleanup):
    def _create(name_prefix: str = "restful_memory") -> str:
        payload = {
            "name": f"{name_prefix}_{uuid.uuid4().hex[:8]}",
            "memory_type": ["raw"],
            "embd_id": _memory_embedding_id(),
            "llm_id": "glm-4-flash@CI@ZHIPU-AI",
        }
        res = rest_client.post("/memories", json=payload)
        assert res.status_code == 200
        res_payload = res.json()
        assert res_payload["code"] == 0, res_payload
        memory_id = res_payload["data"]["id"]
        memory_cleanup.append(memory_id)
        return memory_id

    yield _create


@pytest.mark.p1
def test_memory_crud_cycle(rest_client, create_memory_resource):
    memory_id = create_memory_resource("restful_memory_crud")

    list_res = rest_client.get("/memories")
    assert list_res.status_code == 200
    list_payload = list_res.json()
    assert list_payload["code"] == 0, list_payload
    assert any(item["id"] == memory_id for item in list_payload["data"]["memory_list"]), list_payload

    config_res = rest_client.get(f"/memories/{memory_id}/config")
    assert config_res.status_code == 200
    config_payload = config_res.json()
    assert config_payload["code"] == 0, config_payload
    assert config_payload["data"]["id"] == memory_id, config_payload

    update_res = rest_client.put(
        f"/memories/{memory_id}",
        json={"name": f"updated_{uuid.uuid4().hex[:6]}", "permissions": "me"},
    )
    assert update_res.status_code == 200
    update_payload = update_res.json()
    assert update_payload["code"] == 0, update_payload

    delete_res = rest_client.delete(f"/memories/{memory_id}")
    assert delete_res.status_code == 200
    delete_payload = delete_res.json()
    assert delete_payload["code"] == 0, delete_payload


@pytest.mark.p2
def test_memory_create_missing_required_fields(rest_client):
    res = rest_client.post("/memories", json={"name": "missing_models", "memory_type": ["raw"]})
    assert res.status_code == 200
    payload = res.json()
    assert payload["code"] == 400, payload


@pytest.mark.p2
def test_messages_recent_requires_memory_ids(rest_client):
    res = rest_client.get("/messages")
    assert res.status_code == 200
    payload = res.json()
    assert payload["code"] == 101, payload
    assert "memory_ids is required" in payload["message"], payload
