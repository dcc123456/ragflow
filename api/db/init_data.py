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
from api.db.services.system_settings_service import SystemSettingsService
from api.db.db_models import init_database_tables as init_web_db, LLM
from api.db.joint_services.memory_message_service import init_message_id_sequence, init_memory_size_cache
from api.db.services.canvas_service import CanvasTemplateService
from api.db.services.llm_service import LLMService
from api.db.services.tenant_llm_service import LLMFactoriesService
from api.db.services.billing_service import ProductService, SubscriptionService
from common.file_utils import get_project_base_directory
from common import settings
import stripe
from common.misc_utils import get_uuid
from common.time_utils import get_format_time

DEFAULT_SUPERUSER_NICKNAME = os.getenv("DEFAULT_SUPERUSER_NICKNAME", "admin")
DEFAULT_SUPERUSER_EMAIL = os.getenv("DEFAULT_SUPERUSER_EMAIL", "admin@ragflow.io")
DEFAULT_SUPERUSER_PASSWORD = os.getenv("DEFAULT_SUPERUSER_PASSWORD", "admin")

def init_superuser():
    from api.utils.crypt import decrypt
    from api.db.services.tenant_llm_service import user_register
    user_dict = {
        "access_token": get_uuid(),
        "email": "admin@ragflow.io",
        "nickname": "Admin",
        "password": decrypt("q+vxHmvkOWdq/jgFjkWDR4xu0PB4IZ2zYLYi9CR9Qkz124D86X+WJ38CJ3bp1IEZTaLXuSZxZI6hB/833ok6tDWj9Isp4NJahPVq64ZANJergnmw+Q9sznGIRkP8nYBCoQgFr4rdHgm6Im7mAeLcsI+ltB0j4hzpUz2MSE+tBiXn1xmrzW1VdEB3iQ/xmXEFKNI5YhPDSHPH4lBHauW1MtJU5ZxOkxJ5i5Nd7UtglTlIEsSPR+ZC3JM91o3cis3KsdsEy7Y85ACiPicZA6DGdc9IAfmTxQ8x+4KlJTsAMQ9z0H0QeGEGtyVkN7Q869ElpppnjPdPU5aQygjaO6mdqQ=="),
        "login_channel": "password",
        "last_login_time": get_format_time(),
        "is_superuser": 1,
    }
    user_register("admin", user_dict)
    logging.info("Super user initialized. email: admin@ragflow.io, password: ***. Changing the password after login is strongly recommended.")


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
        # PricePointService.init_data(settings.BILLING_PRICE_POINT)
        # LocalPriceService.init_data(settings.BILLING_LOCAL_PRICE)
        register_webhook()
        handle_undelivered_events()
        configure_decimal()

    add_graph_templates()
    init_message_id_sequence()
    init_memory_size_cache()
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


if __name__ == '__main__':
    init_web_db()
    init_web_data()
