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

K8S_CI_USE_SILICONFLOW = os.getenv("K8S_CI_USE_SILICONFLOW", "0").lower() in {"1", "true", "yes"}
SILICONFLOW_API_KEY = os.getenv("SILICONFLOW_API_KEY")
SILICONFLOW_BASE_URL = os.getenv("SILICONFLOW_BASE_URL", "https://api.siliconflow.cn/v1")
SILICONFLOW_EMBEDDING_MODEL = os.getenv("SILICONFLOW_EMBEDDING_MODEL", "BAAI/bge-m3")

MARKER_EXPRESSIONS = {
    "p1": "p1",
    "p2": "p1 or p2",
    "p3": "p1 or p2 or p3",
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


def register():
    url = HOST_ADDRESS + f"/api/{VERSION}/users"
    name = "qa"
    register_data = {"email": EMAIL, "nickname": name, "password": PASSWORD}
    res = requests.post(url=url, json=register_data)
    res = res.json()
    if res.get("code") != 0 and "has already registered" not in res.get("message"):
        raise Exception(res.get("message"))


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
    try:
        register()
    except Exception as e:
        print(e)
    auth = login()
    return auth


@pytest.fixture(scope="session")
def token(auth):
    url = HOST_ADDRESS + f"/{VERSION}/system/new_token"
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
    url = HOST_ADDRESS + f"/{VERSION}/llm/my_llms"
    response, _ = _request_json_with_auth_retry("GET", url, auth)
    res = response.json()
    if res.get("code") != 0:
        raise Exception(res.get("message"))
    if name in res.get("data"):
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
                "llm_name":"BAAI/bge-small-en-v1.5",
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


def get_tenant_info(auth):
    url = HOST_ADDRESS + f"/{VERSION}/user/tenant_info"
    response, auth = _request_json_with_auth_retry("GET", url, auth)
    res = response.json()
    if res.get("code") != 0:
        raise Exception(res.get("message"))
    return res["data"].get("tenant_id"), auth


@pytest.fixture(scope="session", autouse=True)
def set_tenant_info(auth):
    tenant_id = None
    try:
        add_models(auth)
        tenant_id, auth = get_tenant_info(auth)
    except Exception as e:
        pytest.exit(f"Error in set_tenant_info: {str(e)}")
    url = HOST_ADDRESS + f"/{VERSION}/user/set_tenant_info"
    embd_id = (
        f"{SILICONFLOW_EMBEDDING_MODEL}@SILICONFLOW"
        if K8S_CI_USE_SILICONFLOW
        else "BAAI/bge-small-en-v1.5___OpenAI-API@OpenAI-API-Compatible"
    )
    tenant_info = {
        "tenant_id": tenant_id,
        "llm_id": "glm-4-flash@ZHIPU-AI",
        "embd_id": embd_id,
        "img2txt_id": "",
        "asr_id": "",
        "tts_id": None,
    }
    response, _ = _request_json_with_auth_retry("POST", url, auth, json_payload=tenant_info)
    res = response.json()
    if res.get("code") != 0:
        raise Exception(res.get("message"))
