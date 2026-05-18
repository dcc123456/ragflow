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
from collections import defaultdict
import os
import json
import secrets
import logging
from datetime import date

from common.constants import RAG_FLOW_SERVICE_NAME
from common.file_utils import get_project_base_directory
from common.config_utils import get_base_config, decrypt_database_config
from common.misc_utils import pip_install_torch
from common.constants import SVR_QUEUE_NAME, Storage

import rag.utils
import rag.utils.es_conn
import rag.utils.infinity_conn
import rag.utils.ob_conn
import rag.utils.opensearch_conn
from rag.utils.azure_sas_conn import RAGFlowAzureSasBlob
from rag.utils.azure_spn_conn import RAGFlowAzureSpnBlob
from rag.utils.gcs_conn import RAGFlowGCS
from rag.utils.minio_conn import RAGFlowMinio
from rag.utils.opendal_conn import OpenDALStorage
from rag.utils.redis_conn import REDIS_CONN
from rag.utils.s3_conn import RAGFlowS3
from rag.utils.oss_conn import RAGFlowOSS

from rag.nlp import search
import memory.utils.es_conn as memory_es_conn
import memory.utils.infinity_conn as memory_infinity_conn
import memory.utils.ob_conn as memory_ob_conn

TIMEZONE = os.getenv("TZ", "Asia/Shanghai")

HOSTNAME = os.environ.get("HOSTNAME", "ragflow")

DEFAULT_ROLE = os.environ.get("DEFAULT_ROLE", "owner")
ENABLE_WHITELIST = int(os.environ.get("ENABLE_WHITELIST", "0"))
ENABLE_ADMIN = int(os.environ.get("ENABLE_ADMIN", "0"))
BILLING_ENABLED = int(os.environ.get("BILLING_ENABLED", "0"))
BILLING = {}
BILLING_PRICEID_TO_PRODUCT = {}
BILLING_PRIORITY_TO_PLANS = defaultdict(list)
BILLING_PLAN_TO_INFO = {}
BILLING_PRICE_POINT = {}

LLM = None
LLM_FACTORY = None
LLM_BASE_URL = None
CHAT_MDL = ""
EMBEDDING_MDL = ""
RERANK_MDL = ""
ASR_MDL = ""
IMAGE2TEXT_MDL = ""


CHAT_CFG = ""
EMBEDDING_CFG = ""
RERANK_CFG = ""
ASR_CFG = ""
IMAGE2TEXT_CFG = ""
API_KEY = None
PARSERS = None
HOST_IP = None
HOST_PORT = None
SECRET_KEY = None
FACTORY_LLM_INFOS = None
ALLOWED_LLM_FACTORIES = None

DATABASE_TYPE = os.getenv("DB_TYPE", "mysql")
DATABASE = decrypt_database_config(name=DATABASE_TYPE)

# authentication
AUTHENTICATION_CONF = None

# client
CLIENT_AUTHENTICATION = None
HTTP_APP_KEY = None
GITHUB_OAUTH = None
FEISHU_OAUTH = None
OAUTH_CONFIG = None
DOC_ENGINE = os.getenv("DOC_ENGINE", "elasticsearch")
DOC_ENGINE_INFINITY = (DOC_ENGINE.lower() == "infinity")
DOC_ENGINE_OCEANBASE = (DOC_ENGINE.lower() == "oceanbase")


docStoreConn = None
msgStoreConn = None

retriever = None
kg_retriever = None

# user registration switch
REGISTER_ENABLED = 1
LOCAL_EMBD = None
RABBIT_CONF = None

# SSO-only mode: hide password login form
DISABLE_PASSWORD_LOGIN = False

# sandbox-executor-manager
SANDBOX_HOST = None
STRONG_TEST_COUNT = int(os.environ.get("STRONG_TEST_COUNT", "8"))

SMTP_CONF = None
MAIL_SERVER = ""
MAIL_PORT = 000
MAIL_USE_SSL = True
MAIL_USE_TLS = False
MAIL_USERNAME = ""
MAIL_PASSWORD = ""
MAIL_DEFAULT_SENDER = ()
MAIL_FRONTEND_URL = ""

# move from rag.settings
ES = {}
INFINITY = {}
AZURE = {}
S3 = {}
MINIO = {}
OB = {}
OSS = {}
OS = {}
GCS = {}

DOC_MAXIMUM_SIZE: int = 128 * 1024 * 1024
DOC_BULK_SIZE: int = 4
EMBEDDING_BATCH_SIZE: int = 16

PARALLEL_DEVICES: int = 0

STORAGE_IMPL_TYPE = os.getenv("STORAGE_IMPL", "MINIO")
STORAGE_IMPL = None


def get_svr_queue_name(priority: int) -> str:
    if priority == 0:
        return SVR_QUEUE_NAME
    return f"{SVR_QUEUE_NAME}_{priority}"


def get_svr_queue_names():
    return [get_svr_queue_name(priority) for priority in [1, 0]]

def init_secret_key():
    secret_key = os.environ.get("RAGFLOW_SECRET_KEY")
    if secret_key and len(secret_key) >= 32:
        return secret_key

    # Check if there's a configured secret key
    configured_key = get_base_config(RAG_FLOW_SERVICE_NAME, {}).get("secret_key")
    if configured_key and configured_key != str(date.today()) and len(configured_key) >= 32:
        return configured_key
    return None


def get_secret_key():
    global SECRET_KEY
    if SECRET_KEY is None:
        return _get_or_create_secret_key()
    return SECRET_KEY

def _get_or_create_secret_key():
    # secret_key = os.environ.get("RAGFLOW_SECRET_KEY")
    # if secret_key and len(secret_key) >= 32:
    #     return secret_key
    #
    # # Check if there's a configured secret key
    # configured_key = get_base_config(RAG_FLOW_SERVICE_NAME, {}).get("secret_key")
    # if configured_key and configured_key != str(date.today()) and len(configured_key) >= 32:
    #     return configured_key

    # Generate a new secure key and warn about it
    import logging

    generated_key = secrets.token_hex(32)
    secret_key = REDIS_CONN.get_or_create_secret_key("ragflow:system:secret_key", generated_key)
    if generated_key == secret_key:
        logging.warning("SECURITY WARNING: Using auto-generated SECRET_KEY.")
    return secret_key

class StorageFactory:
    storage_mapping = {
        Storage.MINIO: RAGFlowMinio,
        Storage.AZURE_SPN: RAGFlowAzureSpnBlob,
        Storage.AZURE_SAS: RAGFlowAzureSasBlob,
        Storage.AWS_S3: RAGFlowS3,
        Storage.OSS: RAGFlowOSS,
        Storage.OPENDAL: OpenDALStorage,
        Storage.GCS: RAGFlowGCS,
    }

    @classmethod
    def create(cls, storage: Storage):
        return cls.storage_mapping[storage]()


def init_settings():
    global DATABASE_TYPE, DATABASE
    DATABASE_TYPE = os.getenv("DB_TYPE", "mysql")
    DATABASE = decrypt_database_config(name=DATABASE_TYPE)

    global ALLOWED_LLM_FACTORIES, LLM_FACTORY, LLM_BASE_URL
    llm_settings = get_base_config("user_default_llm", {}) or {}
    llm_default_models = llm_settings.get("default_models", {}) or {}
    LLM_FACTORY = llm_settings.get("factory", "") or ""
    LLM_BASE_URL = llm_settings.get("base_url", "") or ""
    ALLOWED_LLM_FACTORIES = llm_settings.get("allowed_factories", None)

    global REGISTER_ENABLED
    try:
        REGISTER_ENABLED = int(os.environ.get("REGISTER_ENABLED", "1"))
    except Exception:
        pass

    global DISABLE_PASSWORD_LOGIN
    try:
        env_val = os.environ.get("DISABLE_PASSWORD_LOGIN", "").lower()
        if env_val in ("1", "true", "yes"):
            DISABLE_PASSWORD_LOGIN = True
        else:
            authentication_conf = get_base_config("authentication", {})
            DISABLE_PASSWORD_LOGIN = bool(authentication_conf.get("disable_password_login", False))
    except Exception:
        pass

    global FACTORY_LLM_INFOS
    try:
        with open(os.path.join(get_project_base_directory(), "conf", "llm_factories.json"), "r") as f:
            FACTORY_LLM_INFOS = json.load(f)["factory_llm_infos"]
    except Exception:
        FACTORY_LLM_INFOS = []

    global API_KEY
    API_KEY = llm_settings.get("api_key")

    global PARSERS
    PARSERS = llm_settings.get(
        "parsers", "naive:General,qa:Q&A,resume:Resume,manual:Manual,table:Table,paper:Paper,book:Book,laws:Laws,presentation:Presentation,picture:Picture,one:One,audio:Audio,email:Email,tag:Tag"
    )

    global CHAT_MDL, EMBEDDING_MDL, RERANK_MDL, ASR_MDL, IMAGE2TEXT_MDL
    chat_entry = _parse_model_entry(llm_default_models.get("chat_model", CHAT_MDL))
    embedding_entry = _parse_model_entry(llm_default_models.get("embedding_model", EMBEDDING_MDL))
    rerank_entry = _parse_model_entry(llm_default_models.get("rerank_model", RERANK_MDL))
    asr_entry = _parse_model_entry(llm_default_models.get("asr_model", ASR_MDL))
    image2text_entry = _parse_model_entry(llm_default_models.get("image2text_model", IMAGE2TEXT_MDL))

    global CHAT_CFG, EMBEDDING_CFG, RERANK_CFG, ASR_CFG, IMAGE2TEXT_CFG
    CHAT_CFG = _resolve_per_model_config(chat_entry, LLM_FACTORY, API_KEY, LLM_BASE_URL)
    EMBEDDING_CFG = _resolve_per_model_config(embedding_entry, LLM_FACTORY, API_KEY, LLM_BASE_URL)
    RERANK_CFG = _resolve_per_model_config(rerank_entry, LLM_FACTORY, API_KEY, LLM_BASE_URL)
    ASR_CFG = _resolve_per_model_config(asr_entry, LLM_FACTORY, API_KEY, LLM_BASE_URL)
    IMAGE2TEXT_CFG = _resolve_per_model_config(image2text_entry, LLM_FACTORY, API_KEY, LLM_BASE_URL)

    CHAT_MDL = CHAT_CFG.get("model", "") or ""
    EMBEDDING_MDL = EMBEDDING_CFG.get("model", "") or ""
    tei_enabled = os.getenv("TEI_ENABLED")
    compose_profiles = os.getenv("COMPOSE_PROFILES", "")
    if (tei_enabled is not None and tei_enabled.lower() in ("1", "true", "yes")) or (
        tei_enabled is None and "tei-" in compose_profiles
    ):
        EMBEDDING_MDL = os.getenv("TEI_MODEL", EMBEDDING_MDL or "BAAI/bge-small-en-v1.5")
    RERANK_MDL = RERANK_CFG.get("model", "") or ""
    ASR_MDL = ASR_CFG.get("model", "") or ""
    IMAGE2TEXT_MDL = IMAGE2TEXT_CFG.get("model", "") or ""

    global HOST_IP, HOST_PORT
    HOST_IP = get_base_config(RAG_FLOW_SERVICE_NAME, {}).get("host", "127.0.0.1")
    HOST_PORT = get_base_config(RAG_FLOW_SERVICE_NAME, {}).get("http_port")

    global SECRET_KEY
    SECRET_KEY = init_secret_key()


    # authentication
    authentication_conf = get_base_config("authentication", {})

    global CLIENT_AUTHENTICATION, HTTP_APP_KEY
    # client
    CLIENT_AUTHENTICATION = authentication_conf.get("client", {}).get("switch", False)
    HTTP_APP_KEY = authentication_conf.get("client", {}).get("http_app_key")

    global DOC_ENGINE, DOC_ENGINE_INFINITY, DOC_ENGINE_OCEANBASE, docStoreConn, ES, OB, OS, INFINITY
    
    # Whitelist of supported doc engines
    SUPPORTED_DOC_ENGINES = {"elasticsearch", "infinity", "opensearch", "oceanbase", "seekdb"}
    
    doc_engine_raw = os.environ.get("DOC_ENGINE", "elasticsearch").strip()
    
    # Parse multiple doc engines (comma-separated: "elasticsearch,infinity")
    # First engine is primary, rest are shadow databases
    doc_engines = [e.strip().lower() for e in doc_engine_raw.split(",") if e.strip()]
    
    # Validate all engines against whitelist
    for engine in doc_engines:
        if engine not in SUPPORTED_DOC_ENGINES:
            raise ValueError(f"Invalid doc engine '{engine}'. Supported engines: {', '.join(sorted(SUPPORTED_DOC_ENGINES))}")
    
    primary_doc_engine = doc_engines[0]
    shadow_doc_engines = doc_engines[1:]
    
    # Set DOC_ENGINE to primary engine only for backward compatibility
    DOC_ENGINE = primary_doc_engine
    DOC_ENGINE_INFINITY = (primary_doc_engine == "infinity")
    DOC_ENGINE_OCEANBASE = (primary_doc_engine == "oceanbase")
    
    def _create_doc_store_connection(engine_name: str):
        """Create a document store connection for the given engine name."""
        if engine_name == "elasticsearch":
            return rag.utils.es_conn.ESConnection()
        elif engine_name == "infinity":
            return rag.utils.infinity_conn.InfinityConnection()
        elif engine_name == "opensearch":
            return rag.utils.opensearch_conn.OSConnection()
        elif engine_name == "oceanbase":
            return rag.utils.ob_conn.OBConnection()
        elif engine_name == "seekdb":
            return rag.utils.ob_conn.OBConnection()
        else:
            raise Exception(f"Not supported doc engine: {engine_name}")
    
    # Create primary connection
    if primary_doc_engine == "elasticsearch":
        ES = get_base_config("es", {})
        # If ES is a string (e.g., from environment variable), try to parse it as JSON
        if isinstance(ES, str):
            try:
                ES = json.loads(ES)
            except json.JSONDecodeError:
                # If not valid JSON, treat it as hosts string
                ES = {"hosts": ES}
    elif primary_doc_engine == "infinity":
        INFINITY = get_base_config("infinity", {
            "uri": "infinity:23817",
            "postgres_port": 5432,
            "db_name": "default_db"
        })
    elif primary_doc_engine == "opensearch":
        OS = get_base_config("os", {})
    elif primary_doc_engine in ["oceanbase", "seekdb"]:
        OB = get_base_config(primary_doc_engine, {})
    
    primary_conn = _create_doc_store_connection(primary_doc_engine)
    
    # Create shadow connections if any
    shadow_conns = []
    for shadow_engine in shadow_doc_engines:
        try:
            # Load config for shadow engine if needed
            if shadow_engine == "elasticsearch":
                ES = get_base_config("es", {})
                if isinstance(ES, str):
                    try:
                        ES = json.loads(ES)
                    except json.JSONDecodeError:
                        ES = {"hosts": ES}
            elif shadow_engine == "infinity":
                INFINITY = get_base_config("infinity", {
                    "uri": "infinity:23817",
                    "postgres_port": 5432,
                    "db_name": "default_db"
                })
            elif shadow_engine == "opensearch":
                OS = get_base_config("os", {})
            elif shadow_engine in ["oceanbase", "seekdb"]:
                OB = get_base_config(shadow_engine, {})
            
            shadow_conn = _create_doc_store_connection(shadow_engine)
            shadow_conns.append(shadow_conn)
            logging.info(f"Added shadow doc engine: {shadow_engine}")
        except Exception as e:
            logging.warning(f"Failed to create shadow connection for {shadow_engine}: {e}")
    
    # Wrap with ShadowWriteProxy if we have shadow connections
    if shadow_conns:
        from common.doc_store.shadow_write_proxy import ShadowWriteProxy
        docStoreConn = ShadowWriteProxy(primary_conn, shadow_conns)
        logging.info(f"ShadowWriteProxy enabled with {len(shadow_conns)} shadow(s)")
    else:
        docStoreConn = primary_conn

    global msgStoreConn
    # use the same engine for message store (based on primary engine)
    if primary_doc_engine == "elasticsearch":
        msgStoreConn = memory_es_conn.ESConnection()
    elif primary_doc_engine == "infinity":
        msgStoreConn = memory_infinity_conn.InfinityConnection()
    elif primary_doc_engine in ["oceanbase", "seekdb"]:
        msgStoreConn = memory_ob_conn.OBConnection()

    global AZURE, S3, MINIO, OSS, GCS
    if STORAGE_IMPL_TYPE in ["AZURE_SPN", "AZURE_SAS"]:
        AZURE = get_base_config("azure", {})
    elif STORAGE_IMPL_TYPE == "AWS_S3":
        S3 = get_base_config("s3", {})
    elif STORAGE_IMPL_TYPE == "MINIO":
        # Prefer the multi-node minio_0/minio_1/... layout, but keep backward
        # compatibility with older single-node `minio:` configs.
        MINIO = []
        for i in range(12):
            try:
                MINIO.append(decrypt_database_config(name=f"minio_{i}"))
            except Exception:
                break
        if not MINIO:
            try:
                MINIO.append(decrypt_database_config(name="minio"))
            except Exception:
                MINIO = []
    elif STORAGE_IMPL_TYPE == "OSS":
        OSS = get_base_config("oss", {})
    elif STORAGE_IMPL_TYPE == "GCS":
        GCS = get_base_config("gcs", {})

    global LOCAL_EMBD
    LOCAL_EMBD = get_base_config("local_embd", {})

    global RABBIT_CONF
    try:
        RABBIT_CONF = get_base_config("rabbitmq", {})

    except Exception:
        RABBIT_CONF = {}
        pass

    global STORAGE_IMPL
    storage_impl = StorageFactory.create(Storage[STORAGE_IMPL_TYPE])

    # Define crypto settings
    crypto_enabled = os.environ.get("RAGFLOW_CRYPTO_ENABLED", "false").lower() == "true"

    # Check if encryption is enabled
    if crypto_enabled:
        try:
            from rag.utils.encrypted_storage import create_encrypted_storage

            algorithm = os.environ.get("RAGFLOW_CRYPTO_ALGORITHM", "aes-256-cbc")
            crypto_key = os.environ.get("RAGFLOW_CRYPTO_KEY")

            STORAGE_IMPL = create_encrypted_storage(storage_impl, algorithm=algorithm, key=crypto_key, encryption_enabled=crypto_enabled)
        except Exception as e:
            logging.error(f"Failed to initialize encrypted storage: {e}")
            STORAGE_IMPL = storage_impl
    else:
        STORAGE_IMPL = storage_impl

    global retriever, kg_retriever
    retriever = search.Dealer(docStoreConn)
    from rag.graphrag import search as kg_search

    kg_retriever = kg_search.KGSearch(docStoreConn)

    global SANDBOX_HOST
    if int(os.environ.get("SANDBOX_ENABLED", "0")):
        SANDBOX_HOST = os.environ.get("SANDBOX_HOST", "sandbox-executor-manager")

    global SMTP_CONF
    SMTP_CONF = get_base_config("smtp", {})

    global MAIL_SERVER, MAIL_PORT, MAIL_USE_SSL, MAIL_USE_TLS, MAIL_USERNAME, MAIL_PASSWORD, MAIL_DEFAULT_SENDER, MAIL_FRONTEND_URL
    MAIL_SERVER = SMTP_CONF.get("mail_server", "")
    MAIL_PORT = SMTP_CONF.get("mail_port", 000)
    MAIL_USE_SSL = SMTP_CONF.get("mail_use_ssl", True)
    MAIL_USE_TLS = SMTP_CONF.get("mail_use_tls", False)
    MAIL_USERNAME = SMTP_CONF.get("mail_username", "")
    MAIL_PASSWORD = SMTP_CONF.get("mail_password", "")
    mail_default_sender = SMTP_CONF.get("mail_default_sender", [])
    if mail_default_sender and len(mail_default_sender) >= 2:
        MAIL_DEFAULT_SENDER = (mail_default_sender[0], mail_default_sender[1])
    MAIL_FRONTEND_URL = SMTP_CONF.get("mail_frontend_url", "")

    global BILLING, BILLING_PRICEID_TO_PRODUCT, BILLING_PRIORITY_TO_PLANS, BILLING_PLAN_TO_INFO, BILLING_PRICE_POINT

    BILLING = get_base_config("billing", {})
    BILLING_PRICE_POINT = BILLING.get("price_point", [])
    for plan in BILLING.get("billing_plans", []):
        plan_name = plan.get("name")
        price_ids = plan.get("price_ids", "").split()
        api_request_limit_per_minute = plan.get("api_request_limit_per_minute")
        for price_id in price_ids:
           BILLING_PRICEID_TO_PRODUCT[price_id] = plan_name

        task_priority = plan.get("task_priority", "low")
        priority_value = plan.get("priority")
        if priority_value is None:
            raise ValueError(f"Billing plan '{plan_name}' is missing required priority in billing.billing_plans")
        try:
            priority_int = int(priority_value)
        except (TypeError, ValueError) as exc:
            raise ValueError(f"Billing plan '{plan_name}' has invalid priority: {priority_value!r}") from exc
        quota_points = plan.get("quota_points", 0)
        BILLING_PRIORITY_TO_PLANS[priority_int].append(plan_name)
        quota_storage = plan.get("quota_storage", 0)
        if isinstance(quota_storage, str) and quota_storage:
            from api.utils.billing import parse_storage_size
            quota_storage = parse_storage_size(quota_storage)
        BILLING_PLAN_TO_INFO[plan_name] = {
            "priority": priority_int,
            "task_priority": task_priority,
            "price_ids": price_ids,
            "api_request_limit_per_minute": api_request_limit_per_minute,
            "quota_points": quota_points,
            "quota_storage": quota_storage,
            "quota_members": plan.get("quota_members", 0),
            "quota_apps": plan.get("quota_apps", 0),
            "product_type": plan.get("product_type"),
        }

    global DOC_MAXIMUM_SIZE, DOC_BULK_SIZE, EMBEDDING_BATCH_SIZE
    DOC_MAXIMUM_SIZE = int(os.environ.get("MAX_CONTENT_LENGTH", 128 * 1024 * 1024))
    DOC_BULK_SIZE = int(os.environ.get("DOC_BULK_SIZE", 4))
    EMBEDDING_BATCH_SIZE = int(os.environ.get("EMBEDDING_BATCH_SIZE", 16))

    os.environ["DOTNET_SYSTEM_GLOBALIZATION_INVARIANT"] = "1"


def check_and_install_torch():
    global PARALLEL_DEVICES
    try:
        pip_install_torch()
        import torch.cuda

        PARALLEL_DEVICES = torch.cuda.device_count()
        logging.info(f"found {PARALLEL_DEVICES} gpus")
    except Exception:
        logging.info("can't import package 'torch'")


def _parse_model_entry(entry):
    if isinstance(entry, str):
        return {"name": entry, "factory": None, "api_key": None, "base_url": None}
    if isinstance(entry, dict):
        name = entry.get("name") or entry.get("model") or ""
        return {
            "name": name,
            "factory": entry.get("factory"),
            "api_key": entry.get("api_key"),
            "base_url": entry.get("base_url"),
        }
    return {"name": "", "factory": None, "api_key": None, "base_url": None}


def _resolve_per_model_config(entry_dict, backup_factory, backup_api_key, backup_base_url):
    name = (entry_dict.get("name") or "").strip()
    m_factory = entry_dict.get("factory") or backup_factory or ""
    m_api_key = entry_dict.get("api_key") or backup_api_key or ""
    m_base_url = entry_dict.get("base_url") or backup_base_url or ""

    if name and "@" not in name and m_factory:
        name = f"{name}@{m_factory}"

    return {
        "model": name,
        "factory": m_factory,
        "api_key": m_api_key,
        "base_url": m_base_url,
    }

def print_rag_settings():
    logging.info(f"MAX_CONTENT_LENGTH: {DOC_MAXIMUM_SIZE}")
    logging.info(f"MAX_FILE_COUNT_PER_USER: {int(os.environ.get('MAX_FILE_NUM_PER_USER', 0))}")


def rout_key(priority: int, suffix="common") -> str:
    return "te.{}.{}".format(priority, suffix)
