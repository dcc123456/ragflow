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
import asyncio
import json
import os
import time
from copy import deepcopy
from decimal import getcontext, ROUND_HALF_UP
from urllib.parse import urlparse
import uuid
from api.common.base64 import encode_to_base64
from api.db.services.system_settings_service import SystemSettingsService
from api.db.db_models import init_database_tables as init_web_db, LLM, Knowledgebase, Dialog, Memory
from api.db.joint_services.memory_message_service import init_message_id_sequence, init_memory_size_cache
from api.db.services.canvas_service import CanvasTemplateService
from api.db.services.llm_service import LLMService, LLMBundle, get_init_tenant_llm
from api.db.services.tenant_llm_service import LLMFactoriesService, TenantLLMService
from api.db.services.billing_service import PricePointService, ProductService
from peewee import IntegrityError
from api.db import UserTenantRole
from api.db.services import UserService
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.db.services.memory_service import MemoryService
from api.db.services.user_service import TenantService, UserTenantService
from api.db.services.dialog_service import DialogService
from api.db.template_utils import normalize_canvas_template_categories
from api.db.joint_services.tenant_model_service import get_tenant_default_model_by_type
from common.constants import LLMType
from common.file_utils import get_project_base_directory
from common import settings
import stripe


DEFAULT_SUPERUSER_NICKNAME = os.getenv("DEFAULT_SUPERUSER_NICKNAME", "admin")
DEFAULT_SUPERUSER_EMAIL = os.getenv("DEFAULT_SUPERUSER_EMAIL", "admin@ragflow.io")
DEFAULT_SUPERUSER_PASSWORD = os.getenv("DEFAULT_SUPERUSER_PASSWORD", "admin")


def _describe_webhook_secret(secret: str | None) -> str:
    if not secret:
        return "missing"
    suffix = secret[-6:] if len(secret) > 6 else secret
    return f"present(len={len(secret)}, suffix={suffix})"

def init_superuser(nickname=DEFAULT_SUPERUSER_NICKNAME, email=DEFAULT_SUPERUSER_EMAIL, password=DEFAULT_SUPERUSER_PASSWORD, role=UserTenantRole.OWNER):
    if UserService.query(email=email):
        logging.info("User with email %s already exists, skipping initialization.", email)
        return

    user_info = {
        "id": uuid.uuid1().hex,
        "password": encode_to_base64(password),
        "nickname": nickname,
        "is_superuser": True,
        "email": email,
        "creator": "system",
        "status": "1",
    }
    tenant = {
        "id": user_info["id"],
        "name": user_info["nickname"] + "‘s Kingdom",
        "llm_id": settings.CHAT_MDL,
        "embd_id": settings.EMBEDDING_MDL,
        "asr_id": settings.ASR_MDL,
        "parser_ids": settings.PARSERS,
        "img2txt_id": settings.IMAGE2TEXT_MDL,
        "rerank_id": settings.RERANK_MDL,
    }
    usr_tenant = {
        "tenant_id": user_info["id"],
        "user_id": user_info["id"],
        "invited_by": user_info["id"],
        "role": role
    }
    tenant_llm = get_init_tenant_llm(user_info["id"])

    try:
        if not UserService.save(**user_info):
            logging.error("can't init admin.")
            return
    except IntegrityError:
        logging.info("User with email %s already exists, skipping.", email)
        return
    TenantService.insert(**tenant)
    UserTenantService.insert(**usr_tenant)
    TenantLLMService.insert_many(tenant_llm)
    logging.info(
        f"Super user initialized. email: {email},A default password has been set; changing the password after login is strongly recommended.")

    if tenant["llm_id"]:
        chat_model_config = get_tenant_default_model_by_type(tenant["id"], LLMType.CHAT)
        chat_mdl = LLMBundle(tenant["id"], chat_model_config)
        msg = asyncio.run(chat_mdl.async_chat(system="", history=[{"role": "user", "content": "Hello!"}], gen_conf={}))
        if msg.find("ERROR: ") == 0:
            logging.error("'{}' doesn't work. {}".format( tenant["llm_id"], msg))

    if tenant["embd_id"]:
        embd_model_config = get_tenant_default_model_by_type(tenant["id"], LLMType.EMBEDDING)
        embd_mdl = LLMBundle(tenant["id"], embd_model_config)
        v, c = embd_mdl.encode(["Hello!"])
        if c == 0:
            logging.error("'{}' doesn't work!".format(tenant["embd_id"]))


def init_default_roles():
    from api.db import ResourceTypeEnum, ActionEnum
    from api.db.services.role_service import RoleService, RoleResourceService

    # create 'owner'
    owner_roles = RoleService.get_by_role_name("owner")
    if not owner_roles:
        # ask admin to update description manually
        if RoleService.create_role({"role_name": "owner", "description": ""}):
            owner_rows = RoleService.get_by_role_name("owner")
            role_id = owner_rows[0]["id"]
            action = ActionEnum.ENABLE.value | ActionEnum.READ.value | ActionEnum.WRITE.value | ActionEnum.SHARE.value
            RoleResourceService.upsert_role_action_by_id(role_id, {resource_type.value: action for resource_type in ResourceTypeEnum})
    # create 'public'
    public_roles = RoleService.get_by_role_name("public")
    if not public_roles:
        # ask admin to update description manually
        if RoleService.create_role({"role_name": "public", "description": ""}):
            public_rows = RoleService.get_by_role_name("public")
            role_id = public_rows[0]["id"]
            action = ActionEnum.ENABLE.value | ActionEnum.READ.value
            RoleResourceService.upsert_role_action_by_id(role_id, {resource_type.value: action for resource_type in ResourceTypeEnum})


def init_llm_factory():
    LLMFactoriesService.filter_delete([1 == 1])
    factory_llm_infos = settings.FACTORY_LLM_INFOS
    for factory_llm_info in factory_llm_infos:
        info = deepcopy(factory_llm_info)
        llm_infos = info.pop("llm")
        try:
            LLMFactoriesService.save(**info)
        except Exception:
            pass
        LLMService.filter_delete([LLM.fid == factory_llm_info["name"]])
        for llm_info in llm_infos:
            llm_info["fid"] = factory_llm_info["name"]
            try:
                LLMService.save(**llm_info)
            except Exception:
                pass
    #TenantService.filter_update([1 == 1], {
    #    "parser_ids": "naive:General,qa:Q&A,resume:Resume,manual:Manual,table:Table,paper:Paper,book:Book,laws:Laws,presentation:Presentation,picture:Picture,one:One,audio:Audio,email:Email,tag:Tag"})


def add_graph_templates():
    dir = os.path.join(get_project_base_directory(), "agent", "templates")
    CanvasTemplateService.filter_delete([1 == 1])
    if not os.path.exists(dir):
        logging.warning("Missing agent templates!")
        return

    for fnm in sorted(os.listdir(dir)):
        if not fnm.endswith(".json"):
            logging.debug("Skipping non-json template file in %s: %s", dir, fnm)
            continue
        template_path = os.path.join(dir, fnm)
        try:
            with open(template_path, "r", encoding="utf-8") as f:
                cnvs = normalize_canvas_template_categories(json.load(f))
            logging.info("Loaded and normalized template file: %s", template_path)
            try:
                CanvasTemplateService.save(**cnvs)
            except Exception:
                CanvasTemplateService.update_by_id(cnvs["id"], cnvs)
        except Exception as e:
            logging.exception("Add agent templates error for %s: %s", template_path, e)


def register_webhook():
    INVOICE_PAID = "invoice.paid"
    INVOICE_FAILED = "invoice.payment_failed"
    CHECKOUT_SESSION_COMPLETED = "checkout.session.completed"
    SUBSCRIPTION_UPDATED = "customer.subscription.updated"
    SUBSCRIPTION_DELETED = "customer.subscription.deleted"
    PAYMENT_INTENT_SUCCEEDED = "payment_intent.succeeded"
    FOCUSED_STRIPE_WEBHOOK = [INVOICE_PAID, INVOICE_FAILED, SUBSCRIPTION_UPDATED, SUBSCRIPTION_DELETED, CHECKOUT_SESSION_COMPLETED, PAYMENT_INTENT_SUCCEEDED]

    """
    https://docs.stripe.com/api/webhook_endpoints/object
    https://dashboard.stripe.com/test/workbench/webhooks
    """
    stripe.api_key = settings.BILLING['stripe_api_key']
    webhook_url = settings.BILLING['webhook_url']
    if urlparse(webhook_url).hostname in ['localhost', '127.0.0.1']:
        logging.warning(f'webhook_url {webhook_url} is invalid since it is unreachable')
        return

    # Load stored webhook state from SystemSettingsService
    stored_id = SystemSettingsService.get_first_by_name("billing_webhook_id")
    webhook_id = stored_id.value if stored_id and stored_id.value else None
    stored_secret = SystemSettingsService.get_first_by_name("billing_webhook_secret")
    webhook_secret = stored_secret.value if stored_secret and stored_secret.value else None
    logging.info(
        "register_webhook start: url=%s stored_id=%s stored_secret=%s",
        webhook_url,
        webhook_id or "",
        _describe_webhook_secret(webhook_secret),
    )

    verified_endpoint = None
    stored_endpoint_matches_url = False

    # Verify the stored webhook_id still exists in Stripe
    if webhook_id:
        try:
            endpoint = stripe.WebhookEndpoint.retrieve(webhook_id)
            if endpoint:
                verified_endpoint = endpoint
                stored_endpoint_matches_url = endpoint.url == webhook_url
                logging.info(
                    "verified stored Stripe webhook endpoint: id=%s endpoint_url=%s matches_target=%s",
                    webhook_id,
                    endpoint.url,
                    stored_endpoint_matches_url,
                )
        except stripe.error.InvalidRequestError:
            logging.info(f'webhook_id {webhook_id} not found in Stripe, will re-register')
        except Exception as e:
            logging.warning(f'webhook_id {webhook_id} verification failed: {e}, will re-register')

    # Always reconcile same-URL endpoints before deciding whether registration can be skipped.
    webhook_endpoints = stripe.WebhookEndpoint.list()
    duplicate_endpoint_ids = []
    for endpoint in webhook_endpoints.data:
        if endpoint.url == webhook_url:
            if webhook_id and endpoint.id == webhook_id:
                logging.info("keeping stored Stripe webhook endpoint: id=%s url=%s", endpoint.id, webhook_url)
                continue
            duplicate_endpoint_ids.append(endpoint.id)

    for duplicate_id in duplicate_endpoint_ids:
        logging.warning(
            "deleting duplicate Stripe webhook endpoint: stored_id=%s duplicate_id=%s url=%s",
            webhook_id or "",
            duplicate_id,
            webhook_url,
        )
        stripe.WebhookEndpoint.delete(duplicate_id)

    if stored_endpoint_matches_url and webhook_secret:
        logging.info(
            "stored Stripe webhook endpoint is usable after reconciliation; skipping registration: id=%s url=%s duplicates_removed=%s",
            webhook_id,
            webhook_url,
            len(duplicate_endpoint_ids),
        )
        return

    if stored_endpoint_matches_url and verified_endpoint and not webhook_secret:
        logging.warning(
            "stored Stripe webhook endpoint matches target URL but persisted secret is missing; recreating endpoint: id=%s url=%s",
            webhook_id,
            webhook_url,
        )
        stripe.WebhookEndpoint.delete(verified_endpoint.id)

    # No existing endpoint with this URL - register new one
    endpoint = stripe.WebhookEndpoint.create(
        url=webhook_url,
        enabled_events=FOCUSED_STRIPE_WEBHOOK,
    )
    new_webhook_id = endpoint.id
    new_webhook_secret = endpoint.secret

    # Persist webhook_id and webhook_secret
    _upsert_system_setting("billing_webhook_id", new_webhook_id)
    _upsert_system_setting("billing_webhook_secret", new_webhook_secret)
    logging.info(f'webhook_url {webhook_url} registered with id={new_webhook_id} and secret saved')


def _upsert_system_setting(name, value):
    existing = SystemSettingsService.get_first_by_name(name)
    if existing:
        SystemSettingsService.update_by_name(name, {"value": value})
    else:
        SystemSettingsService.insert(
            name=name,
            source="billing",
            data_type="string",
            value=value
        )


def handle_undelivered_events():
    from api.services.billing_webhook_service import (
        handle_undelivered_events as _handle_undelivered_events,
    )

    return _handle_undelivered_events()



def configure_decimal():
    ctx = getcontext()
    ctx.prec = 28
    ctx.rounding = ROUND_HALF_UP


def init_web_data():
    start_time = time.time()

    init_table()

    init_llm_factory()
    if settings.ENABLE_ADMIN:
        init_superuser()

    init_default_roles()

    if settings.BILLING_ENABLED:
        ProductService.init_data(settings.BILLING["billing_plans"])
        PricePointService.init_data(settings.BILLING_PRICE_POINT)
        register_webhook()
        handle_undelivered_events()
        configure_decimal()

    add_graph_templates()
    init_message_id_sequence()
    init_memory_size_cache()
    # fix_missing_tokenized_memory()
    # fix_empty_tenant_model_id()
    logging.info("init web data success:{}".format(time.time() - start_time))

def init_table():
    # init default roles and permissions
    init_default_roles()

    # init system_settings
    with open(os.path.join(get_project_base_directory(), "conf", "system_settings.json"), "r") as f:
        records_from_file = json.load(f)["system_settings"]

    record_index = {}
    records_from_db = SystemSettingsService.get_all()
    for index, record in enumerate(records_from_db):
        record_index[record.name] = index

    to_save = []
    for record in records_from_file:
        setting_name = record["name"]
        if setting_name not in record_index:
            to_save.append(record)

    len_to_save = len(to_save)
    if len_to_save > 0:
        # not initialized
        try:
            SystemSettingsService.insert_many(to_save, len_to_save)
        except Exception as e:
            logging.exception("System settings init error: {}".format(e))
            raise e


def fix_empty_tenant_model_id():
    # knowledgebase
    empty_tenant_embd_id_kbs = KnowledgebaseService.get_null_tenant_embd_id_row()
    if empty_tenant_embd_id_kbs:
        logging.info(f"Found {len(empty_tenant_embd_id_kbs)} empty tenant_embd_id knowledgebase.")
        kb_groups: dict = {}
        for obj in empty_tenant_embd_id_kbs:
            if kb_groups.get((obj.tenant_id, obj.embd_id)):
                kb_groups[(obj.tenant_id, obj.embd_id)].append(obj.id)
            else:
                kb_groups[(obj.tenant_id, obj.embd_id)] = [obj.id]
        update_cnt = 0
        for k, v in kb_groups.items():
            try:
                tenant_llm = TenantLLMService.get_api_key(k[0], k[1])
            except Exception as e:
                logging.warning(f"Failed to get_api_key for tenant={k[0]}, model_name={k[1]!r}: {e}")
                continue
            if tenant_llm:
                update_cnt += KnowledgebaseService.filter_update([Knowledgebase.id.in_(v)], {"tenant_embd_id": tenant_llm.id})
        logging.info(f"Update {update_cnt} tenant_embd_id in table knowledgebase.")
    # dialog
    empty_tenant_llm_id_dialog = DialogService.get_null_tenant_llm_id_row()
    if empty_tenant_llm_id_dialog:
        logging.info(f"Found {len(empty_tenant_llm_id_dialog)} empty tenant_llm_id dialogs.")
        dialog_groups: dict = {}
        for obj in empty_tenant_llm_id_dialog:
            if dialog_groups.get((obj.tenant_id, obj.llm_id)):
                dialog_groups[(obj.tenant_id, obj.llm_id)].append(obj.id)
            else:
                dialog_groups[(obj.tenant_id, obj.llm_id)] = [obj.id]
        update_cnt = 0
        for k, v in dialog_groups.items():
            try:
                tenant_llm = TenantLLMService.get_api_key(k[0], k[1])
            except Exception as e:
                logging.warning(f"Failed to get_api_key for tenant={k[0]}, model_name={k[1]!r}: {e}")
                continue
            if tenant_llm:
                update_cnt += DialogService.filter_update([Dialog.id.in_(v)], {"tenant_llm_id": tenant_llm.id})
        logging.info(f"Update {update_cnt} tenant_llm_id in table dialog.")

    empty_tenant_rerank_id_dialog = DialogService.get_null_tenant_rerank_id_row()
    if empty_tenant_rerank_id_dialog:
        logging.info(f"Found {len(empty_tenant_rerank_id_dialog)} empty tenant_rerank_id dialogs.")
        dialog_groups: dict = {}
        for obj in empty_tenant_rerank_id_dialog:
            if dialog_groups.get((obj.tenant_id, obj.rerank_id)):
                dialog_groups[(obj.tenant_id, obj.rerank_id)].append(obj.id)
            else:
                dialog_groups[(obj.tenant_id, obj.rerank_id)] = [obj.id]
        update_cnt = 0
        for k, v in dialog_groups.items():
            try:
                tenant_llm = TenantLLMService.get_api_key(k[0], k[1])
            except Exception as e:
                logging.warning(f"Failed to get_api_key for tenant={k[0]}, model_name={k[1]!r}: {e}")
                continue
            if tenant_llm:
                update_cnt += DialogService.filter_update([Dialog.id.in_(v)], {"tenant_rerank_id": tenant_llm.id})
        logging.info(f"Update {update_cnt} tenant_rerank_id in table dialog.")
    # memory
    empty_tenant_embd_id_memories = MemoryService.get_null_tenant_embd_id_row()
    if empty_tenant_embd_id_memories:
        logging.info(f"Found {len(empty_tenant_embd_id_memories)} empty tenant_embd_id memories.")
        memory_groups: dict = {}
        for obj in empty_tenant_embd_id_memories:
            if memory_groups.get((obj.tenant_id, obj.embd_id)):
                memory_groups[(obj.tenant_id, obj.embd_id)].append(obj.id)
            else:
                memory_groups[(obj.tenant_id, obj.embd_id)] = [obj.id]
        update_cnt = 0
        for k, v in memory_groups.items():
            try:
                tenant_llm = TenantLLMService.get_api_key(k[0], k[1])
            except Exception as e:
                logging.warning(f"Failed to get_api_key for tenant={k[0]}, model_name={k[1]!r}: {e}")
                continue
            if tenant_llm:
                update_cnt += MemoryService.filter_update([Memory.id.in_(v)], {"tenant_embd_id": tenant_llm.id})
        logging.info(f"Update {update_cnt} tenant_embd_id in table memory.")

    empty_tenant_llm_id_memories = MemoryService.get_null_tenant_llm_id_row()
    if empty_tenant_llm_id_memories:
        logging.info(f"Found {len(empty_tenant_llm_id_memories)} empty tenant_llm_id memories.")
        memory_groups: dict = {}
        for obj in empty_tenant_llm_id_memories:
            if memory_groups.get((obj.tenant_id, obj.llm_id)):
                memory_groups[(obj.tenant_id, obj.llm_id)].append(obj.id)
            else:
                memory_groups[(obj.tenant_id, obj.llm_id)] = [obj.id]
        update_cnt = 0
        for k, v in memory_groups.items():
            try:
                tenant_llm = TenantLLMService.get_api_key(k[0], k[1])
            except Exception as e:
                logging.warning(f"Failed to get_api_key for tenant={k[0]}, model_name={k[1]!r}: {e}")
                continue
            if tenant_llm:
                update_cnt += MemoryService.filter_update([Memory.id.in_(v)], {"tenant_llm_id": tenant_llm.id})
        logging.info(f"Update {update_cnt} tenant_llm_id in table memory.")
    # tenant
    empty_tenant_model_id_tenants = TenantService.get_null_tenant_model_id_rows()
    if empty_tenant_model_id_tenants:
        logging.info(f"Found {len(empty_tenant_model_id_tenants)} empty tenant_model_id tenants.")
        update_cnt = 0
        for obj in empty_tenant_model_id_tenants:
            tenant_dict = obj.to_dict()
            update_dict = {}
            for key in ["llm_id", "embd_id", "asr_id", "img2txt_id", "rerank_id", "tts_id"]:
                if tenant_dict.get(key) and not tenant_dict.get(f"tenant_{key}"):
                    try:
                        tenant_model = TenantLLMService.get_api_key(tenant_dict["id"], tenant_dict[key])
                    except Exception as e:
                        logging.warning(f"Failed to get_api_key for tenant={tenant_dict['id']}, model_name={tenant_dict[key]!r}: {e}")
                        continue
                    if tenant_model:
                        update_dict.update({f"tenant_{key}": tenant_model.id})
            if update_dict:
                update_cnt += TenantService.update_by_id(tenant_dict["id"], update_dict)
        logging.info(f"Update {update_cnt} tenant_model_id in table tenant.")
    logging.info("Fix empty tenant_model_id done.")

if __name__ == '__main__':
    init_web_db()
    init_web_data()
