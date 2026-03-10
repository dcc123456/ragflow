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
from api.db.joint_services.memory_message_service import init_message_id_sequence, init_memory_size_cache, fix_missing_tokenized_memory
from api.db.services.canvas_service import CanvasTemplateService
from api.db.services.llm_service import LLMService, LLMBundle, get_init_tenant_llm
from api.db.services.tenant_llm_service import LLMFactoriesService, TenantLLMService
from api.db.services.billing_service import LocalPriceService, PricePointService, ProductService, SubscriptionService
from peewee import IntegrityError
from api.db import UserTenantRole
from api.db.services import UserService
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.db.services.memory_service import MemoryService
from api.db.services.user_service import TenantService, UserTenantService
from api.db.services.dialog_service import DialogService
from api.db.joint_services.tenant_model_service import get_tenant_default_model_by_type
from common.constants import LLMType
from common.file_utils import get_project_base_directory
from common import settings
import stripe


DEFAULT_SUPERUSER_NICKNAME = os.getenv("DEFAULT_SUPERUSER_NICKNAME", "admin")
DEFAULT_SUPERUSER_EMAIL = os.getenv("DEFAULT_SUPERUSER_EMAIL", "admin@ragflow.io")
DEFAULT_SUPERUSER_PASSWORD = os.getenv("DEFAULT_SUPERUSER_PASSWORD", "admin")

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

    for fnm in os.listdir(dir):
        try:
            cnvs = json.load(open(os.path.join(dir, fnm), "r",encoding="utf-8"))
            try:
                CanvasTemplateService.save(**cnvs)
            except Exception:
                CanvasTemplateService.update_by_id(cnvs["id"], cnvs)
        except Exception as e:
            logging.exception(f"Add agent templates error: {e}")

def register_webhook():
    """
    https://docs.stripe.com/api/webhook_endpoints/object
    https://dashboard.stripe.com/test/workbench/webhooks
    """
    SUBSCRIPTION_UPDATED = 'customer.subscription.updated'
    SUBSCRIPTION_DELETED = 'customer.subscription.deleted'
    stripe.api_key = settings.BILLING['stripe_api_key']
    webhook_url = settings.BILLING['webhook_url']
    if urlparse(webhook_url).hostname in ['localhost', '127.0.0.1']:
        logging.warning(f'webhook_url {webhook_url} is invalid since it is unreachable')
    else:
        exists = False
        webhook_endpoints = stripe.WebhookEndpoint.list()
        for endpoint in webhook_endpoints.data:
            if endpoint.url == webhook_url and SUBSCRIPTION_UPDATED in endpoint.enabled_events and SUBSCRIPTION_DELETED in endpoint.enabled_events:
                logging.warning(f'webhook_url {webhook_url} already exists')
                exists = True
            else:
                stripe.WebhookEndpoint.delete(endpoint.id)
        if not exists:
            stripe.WebhookEndpoint.create(
                url=webhook_url,
                enabled_events=[SUBSCRIPTION_UPDATED, SUBSCRIPTION_DELETED],
            )
        logging.warning(f'webhook_url {webhook_url} has just been registered')

def handle_undelivered_events():
    """
    https://docs.stripe.com/webhooks/process-undelivered-events
    """
    SUBSCRIPTION_UPDATED = 'customer.subscription.updated'
    SUBSCRIPTION_DELETED = 'customer.subscription.deleted'
    stripe.api_key = settings.BILLING['stripe_api_key']
    starting_after = None
    while True:
        events = stripe.Event.list(delivery_success=False, limit=100, starting_after=starting_after)
        num_events = 0
        for event in events.auto_paging_iter():
            starting_after = event['id']
            num_events += 1
            event_type = event['type']
            if event_type not in [SUBSCRIPTION_UPDATED, SUBSCRIPTION_DELETED]:
                continue
            subscription = event['data']['object']
            # Refers to https://docs.stripe.com/api/subscriptions/object
            subscription_id = subscription['id']
            subscription_status = subscription['status']
            customer_id = subscription['customer']
            price_id = subscription['items']['data'][0]['price']['id']
            plan_name = settings.BILLING['plans'].get(price_id)
            if not plan_name:
                logging.warning(f'handle_undelivered_events could not find plan for price {price_id}')
                continue
            updated_rows = SubscriptionService.update_subscription(customer_id, subscription_id, subscription_status, plan_name)
            if not updated_rows:
                logging.warning(f'handle_undelivered_events could not find tenant for customer {customer_id}')
                continue
            logging.info(f'handle_undelivered_events updated customer {customer_id} subscription {subscription_id} status {subscription_status} plan {plan_name}')
        if num_events == 0:
            break


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
        LocalPriceService.init_data(settings.BILLING_LOCAL_PRICE)
        register_webhook()
        handle_undelivered_events()
        configure_decimal()

    add_graph_templates()
    init_message_id_sequence()
    init_memory_size_cache()
    fix_missing_tokenized_memory()
    fix_empty_tenant_model_id()
    logging.info("init web data success:{}".format(time.time() - start_time))

def init_table():
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
            tenant_llm = TenantLLMService.get_api_key(k[0], k[1])
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
            tenant_llm = TenantLLMService.get_api_key(k[0], k[1])
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
            tenant_llm = TenantLLMService.get_api_key(k[0], k[1])
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
            tenant_llm = TenantLLMService.get_api_key(k[0], k[1])
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
            tenant_llm = TenantLLMService.get_api_key(k[0], k[1])
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
                    tenant_model = TenantLLMService.get_api_key(tenant_dict["id"], tenant_dict[key])
                    if tenant_model:
                        update_dict.update({f"tenant_{key}": tenant_model.id})
            if update_dict:
                update_cnt += TenantService.update_by_id(tenant_dict["id"], update_dict)
        logging.info(f"Update {update_cnt} tenant_model_id in table tenant.")
    logging.info("Fix empty tenant_model_id done.")

if __name__ == '__main__':
    init_web_db()
    init_web_data()
