#
#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
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

import importlib
import os
import sys
import types
from typing import Any


def _make_stub_getattr(module_name):
    def __getattr__(attr_name):
        message = f"{module_name}.{attr_name} is stubbed in tests"

        class _Stub:
            def __init__(self, *_args, **_kwargs):
                raise RuntimeError(message)

            def __call__(self, *_args, **_kwargs):
                raise RuntimeError(message)

            def __getattr__(self, _name):
                raise RuntimeError(message)

        setattr(sys.modules[module_name], attr_name, _Stub)
        return _Stub

    return __getattr__


def _install_rag_llm_stubs():
    rag_llm = sys.modules.get("rag.llm")
    if rag_llm is not None and getattr(rag_llm, "_rag_llm_stubbed", False):
        return

    try:
        rag_pkg = importlib.import_module("rag")
    except Exception:
        rag_pkg = types.ModuleType("rag")
        rag_pkg.__path__ = []
        rag_pkg.__package__ = "rag"
        rag_pkg.__file__ = __file__
        sys.modules["rag"] = rag_pkg

    llm_pkg = types.ModuleType("rag.llm")
    llm_pkg.__path__ = []
    llm_pkg.__package__ = "rag.llm"
    llm_pkg.__file__ = __file__
    sys.modules["rag.llm"] = llm_pkg
    rag_pkg.llm = llm_pkg

    llm_pkg.__getattr__ = _make_stub_getattr("rag.llm")

    for submodule in ("cv_model", "chat_model"):
        full_name = f"rag.llm.{submodule}"
        sub_mod = sys.modules.get(full_name)
        if sub_mod is None or not isinstance(sub_mod, types.ModuleType):
            sub_mod = types.ModuleType(full_name)
            sys.modules[full_name] = sub_mod
        sub_mod.__package__ = "rag.llm"
        sub_mod.__file__ = __file__
        sub_mod.__getattr__ = _make_stub_getattr(full_name)
        setattr(llm_pkg, submodule, sub_mod)

    llm_pkg._rag_llm_stubbed = True


def _install_scholarly_stub():
    if "scholarly" in sys.modules:
        return
    stub = types.ModuleType("scholarly")

    def _stub(*_args, **_kwargs):
        raise RuntimeError("scholarly is stubbed in tests")

    stub.scholarly = _stub
    sys.modules["scholarly"] = stub


_install_rag_llm_stubs()
_install_scholarly_stub()

import pytest
import requests
from configs import EMAIL, HOST_ADDRESS, PASSWORD, VERSION, ZHIPU_AI_API_KEY

BUILTIN_EMBEDDING_MODEL = "BAAI/bge-small-en-v1.5"
BUILTIN_EMBEDDING_PROVIDER = "Builtin"
BUILTIN_EMBEDDING_INSTANCE = "Local"
SILICONFLOW_EMBEDDING_PROVIDER = "SILICONFLOW"
SILICONFLOW_EMBEDDING_INSTANCE = "CI"


def using_siliconflow_byok() -> bool:
    return os.getenv("K8S_CI_USE_SILICONFLOW", "0").lower() in {"1", "true", "yes"}


def siliconflow_embedding_model_name() -> str:
    return os.getenv("SILICONFLOW_EMBEDDING_MODEL", "BAAI/bge-m3")


def embedding_model_id(*, include_instance: bool = True) -> str:
    if using_siliconflow_byok():
        return f"{siliconflow_embedding_model_name()}@{SILICONFLOW_EMBEDDING_INSTANCE}@{SILICONFLOW_EMBEDDING_PROVIDER}"
    if include_instance:
        return f"{BUILTIN_EMBEDDING_MODEL}@{BUILTIN_EMBEDDING_INSTANCE}@{BUILTIN_EMBEDDING_PROVIDER}"
    return f"{BUILTIN_EMBEDDING_MODEL}@{BUILTIN_EMBEDDING_PROVIDER}"


def default_embedding_model_payload() -> dict[str, str]:
    if using_siliconflow_byok():
        return {
            "model_provider": SILICONFLOW_EMBEDDING_PROVIDER,
            "model_instance": SILICONFLOW_EMBEDDING_INSTANCE,
            "model_type": "embedding",
            "model_name": siliconflow_embedding_model_name(),
        }
    return {
        "model_provider": BUILTIN_EMBEDDING_PROVIDER,
        "model_instance": BUILTIN_EMBEDDING_INSTANCE,
        "model_type": "embedding",
        "model_name": BUILTIN_EMBEDDING_MODEL,
    }


def siliconflow_rerank_model_id() -> str:
    return f"BAAI/bge-reranker-v2-m3@{SILICONFLOW_EMBEDDING_INSTANCE}@{SILICONFLOW_EMBEDDING_PROVIDER}"


K8S_CI_USE_SILICONFLOW = using_siliconflow_byok()
SILICONFLOW_API_KEY = os.getenv("SILICONFLOW_API_KEY")
SILICONFLOW_BASE_URL = os.getenv("SILICONFLOW_BASE_URL", "https://api.siliconflow.cn/v1")
SILICONFLOW_EMBEDDING_MODEL = siliconflow_embedding_model_name()
ADMIN_HOST_ADDRESS = os.getenv("ADMIN_HOST_ADDRESS", "http://127.0.0.1:9381")
# password is "admin"
ENCRYPTED_ADMIN_PASSWORD = """WBPsJbL/W+1HN+hchm5pgu1YC3yMEb/9MFtsanZrpKEE9kAj4u09EIIVDtIDZhJOdTjz5pp5QW9TwqXBfQ2qzDqVJiwK7HGcNsoPi4wQPCmnLo0fs62QklMlg7l1Q7fjGRgV+KWtvNUce2PFzgrcAGDqRIuA/slSclKUEISEiK4z62rdDgvHT8LyuACuF1lPUY5wV0m/MbmGijRJlgvglAF8BX0BP8rQr8wZeaJdcnAy/keuODCjltMZDL06tYluN7HoiU+qlhBB+ltqG411oO/+vVhBgWsuVVOHd8uMjJEL320GUWUicprDUZvjlLaSSqVyyOiRMHpqAE9eHEecWg=="""

MARKER_EXPRESSIONS = {
    "p1": "p1 or billing",
    "p2": "p1 or p2 or billing",
    "p3": "p1 or p2 or p3 or billing",
}


def pytest_configure(config: pytest.Config) -> None:
    config.addinivalue_line("markers", "billing: billing integration tests requiring Stripe test mode")
    level = config.getoption("--level")
    config.option.markexpr = MARKER_EXPRESSIONS[level]
    if config.option.verbose > 0:
        print(f"\n[CONFIG] Active test level: {level}")


def pytest_addoption(parser: pytest.Parser) -> None:
    parser.addoption(
        "--level",
        action="store",
        default="p2",
        choices=list(MARKER_EXPRESSIONS.keys()),
        help=f"Test level ({'/'.join(MARKER_EXPRESSIONS)}): p1=smoke, p2=core, p3=full",
    )

    parser.addoption(
        "--client-type",
        action="store",
        default="http",
        choices=["python_sdk", "http", "web"],
        help="Test client type: 'python_sdk', 'http', 'web'",
    )


def _json_response(response: requests.Response, context: str) -> dict[str, Any]:
    try:
        return response.json()
    except ValueError as exc:
        raise Exception(f"{context} returned non-JSON status={response.status_code}: {response.text[:500]}") from exc


def _is_registration_whitelist_error(message: str | None) -> bool:
    return "isn't in whitelist" in (message or "")


def _add_test_email_to_whitelist():
    session = requests.Session()
    login_url = ADMIN_HOST_ADDRESS + f"/api/{VERSION}/admin/login"
    login_response = session.post(
        url=login_url,
        json={"email": "admin@ragflow.io", "password": ENCRYPTED_ADMIN_PASSWORD},
        timeout=30,
    )
    login_payload = _json_response(login_response, "admin login")
    if login_payload.get("code") != 0:
        raise Exception(
            f"admin login failed at {login_url}: code={login_payload.get('code')} message={login_payload.get('message')}"
        )

    auth_header = login_response.headers.get("Authorization", "")
    if auth_header:
        session.headers.update({"Authorization": auth_header})

    whitelist_url = ADMIN_HOST_ADDRESS + f"/api/{VERSION}/admin/whitelist/add"
    whitelist_response = session.post(whitelist_url, json={"email": EMAIL}, timeout=30)
    whitelist_payload = _json_response(whitelist_response, "admin whitelist add")
    if whitelist_payload.get("code") != 0:
        raise Exception(
            f"admin whitelist add failed at {whitelist_url}: "
            f"code={whitelist_payload.get('code')} message={whitelist_payload.get('message')}"
        )

    data = whitelist_payload.get("data") or {}
    if data.get("success") is False:
        raise Exception(f"admin whitelist add returned unsuccessful payload: {whitelist_payload}")


def register():
    url = HOST_ADDRESS + f"/api/{VERSION}/users"
    name = "qa"
    register_data = {"email": EMAIL, "nickname": name, "password": PASSWORD}
    response = requests.post(url=url, json=register_data)
    res = _json_response(response, "register")
    message = res.get("message")
    if res.get("code") != 0 and "has already registered" not in (message or ""):
        raise Exception(message)


def _register_with_whitelist_retry():
    try:
        register()
    except Exception as e:
        message = str(e)
        if not _is_registration_whitelist_error(message):
            raise
        try:
            _add_test_email_to_whitelist()
            register()
        except Exception as bootstrap_error:
            raise Exception(
                "REST API test user registration is blocked by whitelist and automatic admin whitelist bootstrap failed. "
                f"HOST_ADDRESS={HOST_ADDRESS}, ADMIN_HOST_ADDRESS={ADMIN_HOST_ADDRESS}, email={EMAIL}, "
                f"registration_error={message}, bootstrap_error={bootstrap_error}"
            ) from bootstrap_error


def login():
    url = HOST_ADDRESS + f"/api/{VERSION}/auth/login"
    login_data = {"email": EMAIL, "password": PASSWORD}
    response = requests.post(url=url, json=login_data)
    res = response.json()
    if res.get("code") != 0:
        raise Exception(res.get("message"))
    auth = response.headers["Authorization"]
    return auth


def _request_json_with_auth_retry(method, url, auth, *, json_payload=None):
    headers = {"Authorization": auth}
    response = requests.request(method=method, url=url, headers=headers, json=json_payload)
    if response.status_code != 401:
        return response, auth

    refreshed_auth = login()
    headers = {"Authorization": refreshed_auth}
    response = requests.request(method=method, url=url, headers=headers, json=json_payload)
    return response, refreshed_auth


@pytest.fixture(scope="session")
def auth():
    _register_with_whitelist_retry()
    auth = login()
    return auth


@pytest.fixture(scope="session")
def token(auth):
    url = HOST_ADDRESS + f"/api/{VERSION}/system/tokens"
    response, _ = _request_json_with_auth_retry("POST", url, auth)
    res = response.json()
    if res.get("code") != 0:
        error_msg = f"access: {url}, POST method, error code: {res.get('code')}, message: {res.get('message')}"
        raise Exception(error_msg)
    return res["data"].get("token")


@pytest.fixture(scope="session")
def tenant_id(auth):
    tenant_id, _ = get_tenant_info(auth)
    return tenant_id


def get_my_llms(auth, name):
    # todo deprecated
    url = HOST_ADDRESS + f"/{VERSION}/llm/my_llms"
    response, _ = _request_json_with_auth_retry("GET", url, auth)
    res = response.json()
    if res.get("code") != 0:
        raise Exception(res.get("message"))
    if name in res.get("data"):
        return True
    return False


def get_added_models(auth, factory_name):
    url = HOST_ADDRESS + f"/api/v1/providers/{factory_name}/instances/CI"
    authorization = {"Authorization": auth}
    response = requests.get(url=url, headers=authorization)
    res = response.json()
    message = res.get("message", "")
    if res.get("code") != 0:
        if (
            "No provider found for provider" in message
            or "No instance found for provider" in message
        ):
            return False
        raise Exception(message)

    data = res.get("data") or {}
    if not isinstance(data, dict):
        raise Exception(f"Unexpected provider instance payload for {factory_name}: {data!r}")
    return data.get("instance_name") == "CI"


def get_tenant_llm_added(auth, factory_name, model_name, model_type="rerank"):
    """
    Check whether a specific (factory, model_name, model_type) tenant_llm row exists.

    Legacy /v1/llm/my_llms response shape:
        {
            "ZHIPU-AI":     {"tags": ..., "llm": [{"name": ..., "type": ...}, ...]},
            "SILICONFLOW":  {"tags": ..., "llm": [{"name": ..., "type": ...}, ...]},
        }
    so we navigate by factory key first, then look through its llm list.
    """
    url = HOST_ADDRESS + f"/{VERSION}/llm/my_llms"
    authorization = {"Authorization": auth}
    response = requests.get(url=url, headers=authorization)
    res = response.json()
    if res.get("code") != 0:
        return False
    data = res.get("data") or {}
    factory_data = data.get(factory_name) or {}
    for m in factory_data.get("llm", []) or []:
        if m.get("name") != model_name:
            continue
        if model_type is None or m.get("type") == model_type:
            return True
    return False


def add_models(auth):
    set_api_key_url = HOST_ADDRESS + f"/{VERSION}/llm/set_api_key"
    add_llm_url = HOST_ADDRESS + f"/{VERSION}/llm/add_llm"
    set_api_key_models_info = {
        "ZHIPU-AI": {"llm_factory": "ZHIPU-AI", "api_key": ZHIPU_AI_API_KEY}
    }
    if K8S_CI_USE_SILICONFLOW:
        if not SILICONFLOW_API_KEY:
            pytest.exit("Error: Environment variable SILICONFLOW_API_KEY must be set when K8S_CI_USE_SILICONFLOW=1")
        add_llm_models_info = {
            "SILICONFLOW": {
                "llm_factory": "SILICONFLOW",
                "api_key": SILICONFLOW_API_KEY,
                "api_base": SILICONFLOW_BASE_URL,
                "llm_name": SILICONFLOW_EMBEDDING_MODEL,
                "max_tokens": 8192,
                "model_type": "embedding",
            }
        }
    else:
        add_llm_models_info = {
            "OpenAI-API-Compatible":{
                "llm_factory":"OpenAI-API-Compatible",
                "api_base":"http://tei:80",
                "llm_name": BUILTIN_EMBEDDING_MODEL,
                "max_tokens":8192,
                "model_type":"embedding"
            }
        }

    for name, model_info in set_api_key_models_info.items():
        if not get_my_llms(auth, name):
            response, auth = _request_json_with_auth_retry("POST", set_api_key_url, auth, json_payload=model_info)
            res = response.json()
            if res.get("code") != 0:
                pytest.exit(f"Critical error in add_models: {res.get('message')}")

    for name, model_info in add_llm_models_info.items():
        if not get_my_llms(auth, name):
            response, auth = _request_json_with_auth_retry("POST", add_llm_url, auth, json_payload=model_info)
            res = response.json()
            if res.get("code") != 0:
                message = res.get("message", "")
                if "Fail to access embedding model" in message or "Connection error" in message:
                    print(f"[WARN] Skipping embedding model warmup due to backend connectivity issue: {message}")
                    continue
                pytest.exit(f"Critical error in add_models: {message}")


def add_model_instance(auth):
    add_provider_api = HOST_ADDRESS + "/api/v1/providers"
    authorization = {"Authorization": auth}

    # Tracks providers that already existed in the catalog before this test
    # run. Their user-tenant_llm binding is whatever was last configured for
    # this user; the final assertion is downgraded to a warning in that
    # case to keep the suite runnable in partially-seeded environments.
    provider_already_existed = set()

    providers = [("ZHIPU-AI", ZHIPU_AI_API_KEY)]
    if K8S_CI_USE_SILICONFLOW:
        if not SILICONFLOW_API_KEY:
            pytest.exit("Error: Environment variable SILICONFLOW_API_KEY must be set when K8S_CI_USE_SILICONFLOW=1")
        providers.append(("SILICONFLOW", SILICONFLOW_API_KEY))

    for provider_name, api_key in providers:
        if not get_added_models(auth, provider_name):
            add_provider_response = requests.put(url=add_provider_api, headers=authorization, json={"provider_name": provider_name})
            add_provider_res = add_provider_response.json()
            if add_provider_res.get("code") != 0:
                msg = add_provider_res.get("message", "")
                # Provider may already exist in the catalog from a prior run
                # or admin setup but not yet appear in this tenant's
                # `/api/v1/models` listing — treat as success and continue
                # to the instance step. The final assertion below will be
                # downgraded to a warning in that case so the test can run.
                if "duplicated" in msg.lower() or "already exist" in msg.lower():
                    print("Note: provider already exists, skipping")
                    provider_already_existed.add(provider_name)
                else:
                    pytest.exit(f"Critical error in add model provider: {msg}")

        # Register "CI" (used by glm-4-flash@CI@ZHIPU-AI in configs.py
        # and BAAI/bge-reranker-v2-m3@CI@SILICONFLOW).
        instance_name = "CI"
        add_instance_api = HOST_ADDRESS + f"/api/v1/providers/{provider_name}/instances"
        add_instance_response = requests.post(url=add_instance_api, headers=authorization, json={
            "instance_name": instance_name,
            "api_key": api_key,
            "region": "default",
            "base_url": ""
        })
        add_instance_res = add_instance_response.json()
        if add_instance_res.get("code") != 0:
            msg = add_instance_res.get("message", "")
            # Instance may already exist with a different API key from a
            # prior test run; that's fine — skip instead of failing.
            if "Already exist instance" in msg or "already exist" in msg.lower():
                # Avoid emitting the provider/instance name in clear text;
                # CodeQL flags this print because the surrounding function
                # handles API keys (tracked as sensitive data sources).
                print("Note: model instance already exists, skipping")
                continue
            # Python API blocks creating instances named "default".
            # The test_retrieval_parity test handles this by inserting
            # "default" directly into the DB for SILICONFLOW.
            if "cannot be 'default'" in msg:
                print("Note: model instance name is reserved, skipping")
                continue
            pytest.exit(
                f"Critical error in add model instance {provider_name}/{instance_name}: "
                f"{msg}"
            )

        add_success = get_added_models(auth, provider_name)
        if not add_success:
            if provider_name in provider_already_existed:
                # The provider/instances were already there from a prior run
                # but this user's tenant_llm binding is missing — the Go
                # server (post-Python port) doesn't auto-create the binding
                # on PUT. Downgrade to a warning so tests that don't depend
                # on the model can still run; tests that do will fail with
                # a real error rather than this opaque setup crash.
                print(
                    "WARNING: provider already exists in catalog but missing from "
                    "this tenant's /api/v1/models. Tests that depend on it may fail."
                )
                continue
            pytest.exit(f"Critical error in check added model: {provider_name} add model failed")


def add_siliconflow_rerank_llm(auth):
    """
    Register the BAAI/bge-reranker-v2-m3 rerank model under factory=SILICONFLOW / instance=CI.

    This is the model referenced as `BAAI/bge-reranker-v2-m3@CI@SILICONFLOW` in
    test_retrieval_parity.py. The /v1/llm/add_llm endpoint validates the key by
    issuing a real rerank request, so the call requires network access to SiliconFlow
    and a valid SILICONFLOW_API_KEY.
    """
    factory = "SILICONFLOW"
    model_name = "BAAI/bge-reranker-v2-m3"
    if get_tenant_llm_added(auth, factory, model_name, "rerank"):
        return

    url = HOST_ADDRESS + f"/{VERSION}/llm/add_llm"
    authorization = {"Authorization": auth}
    payload = {
        "llm_factory": factory,
        "llm_name": model_name,
        "model_type": "rerank",
        "api_key": SILICONFLOW_API_KEY,
        "api_base": "",
        "max_tokens": 8192,
    }
    response = requests.post(url=url, headers=authorization, json=payload)
    res = response.json()
    if res.get("code") != 0:
        pytest.exit(
            f"Critical error adding {factory} rerank model {model_name}: "
            f"code={res.get('code')} message={res.get('message')} data={res.get('data')}"
        )

    if not get_tenant_llm_added(auth, factory, model_name, "rerank"):
        pytest.exit(f"Failed to confirm {factory}/{model_name} rerank row was added")


def get_tenant_info(auth):
    # todo deprecated
    url = HOST_ADDRESS + f"/api/{VERSION}/users/me/models"
    response, auth = _request_json_with_auth_retry("GET", url, auth)
    res = response.json()
    if res.get("code") != 0:
        raise Exception(res.get("message"))
    return res["data"].get("tenant_id"), auth


@pytest.fixture(scope="session", autouse=True)
def set_tenant_info(auth):
    required_providers = ["ZHIPU-AI"]
    if K8S_CI_USE_SILICONFLOW:
        required_providers.append("SILICONFLOW")
    if any(not get_added_models(auth, provider) for provider in required_providers):
        try:
            add_model_instance(auth)
        except Exception as e:
            pytest.exit(f"Error in set_tenant_info: {str(e)}")
    url = HOST_ADDRESS + "/api/v1/models/default"
    authorization = {"Authorization": auth}
    # set chat model
    set_default_llm_response = requests.patch(
        url=url,
        headers=authorization,
        json={
            "model_provider": "ZHIPU-AI",
            "model_instance": "CI",
            "model_type": "chat",
            "model_name": "glm-4-flash"
        })
    llm_res = set_default_llm_response.json()
    if llm_res.get("code") != 0:
        # The Go server (post-Python port) doesn't yet implement
        # PATCH /api/v1/models/default, so the chat/embedding default
        # can't be set via API. Downgrade to a warning so tests that
        # don't rely on a default LLM can still run; tests that do
        # will fail with their own real error.
        print(
            f"WARNING: failed to set default chat LLM via {url}: "
            f"{llm_res.get('message')!r}. Continuing."
        )
    # set embedding model
    set_default_embedding_response = requests.patch(
        url=url,
        headers=authorization,
        json=default_embedding_model_payload())
    embd_res = set_default_embedding_response.json()
    if embd_res.get("code") != 0:
        print(
            f"WARNING: failed to set default embedding LLM via {url}: "
            f"{embd_res.get('message')!r}. Continuing."
        )


@pytest.fixture(scope="session", autouse=True)
def set_tenant_siliconflow_rerank(auth):
    """
    Ensure the SiliconFlow BAAI/bge-reranker-v2-m3 rerank model is registered
    for the test tenant. Used by test_retrieval_parity.py as
    `BAAI/bge-reranker-v2-m3@CI@SILICONFLOW`.

    Runs after `set_tenant_info` so the SILICONFLOW provider+CI instance
    already exist when the /add_llm call is made.

    If /add_llm is blocked (e.g. factory not in allowed list), the rerank
    model config is resolved from FACTORY_LLM_INFOS at search time, so the
    test can still proceed.
    """
    if not K8S_CI_USE_SILICONFLOW:
        return

    try:
        add_siliconflow_rerank_llm(auth)
    except Exception as e:
        print(f"Note: Could not register SILICONFLOW rerank model via /add_llm: {e}")
        print("The model config will be resolved from FACTORY_LLM_INFOS at runtime.")
