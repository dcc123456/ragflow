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
import sys
import types
import warnings

import pytest

# xgboost pulls in pkg_resources and can fail during import collection in this
# environment; stub it because register_webhook does not depend on it.
warnings.filterwarnings(
    "ignore",
    message="pkg_resources is deprecated as an API.*",
    category=UserWarning,
)


def _stub_module(name, **attrs):
    module = types.ModuleType(name)
    for key, value in attrs.items():
        setattr(module, key, value)
    sys.modules.setdefault(name, module)


sys.modules.setdefault("xgboost", types.ModuleType("xgboost"))
_stub_module(
    "api.db.joint_services.memory_message_service",
    init_message_id_sequence=lambda: None,
    init_memory_size_cache=lambda: None,
)
_stub_module("api.db.services.canvas_service", CanvasTemplateService=object)
_stub_module(
    "api.db.services.llm_service",
    LLMService=object,
    LLMBundle=object,
    get_init_tenant_llm=lambda *args, **kwargs: None,
)
_stub_module(
    "api.db.services.tenant_llm_service",
    LLMFactoriesService=object,
    TenantLLMService=object,
)
_stub_module(
    "api.db.services.billing_service",
    PricePointService=object,
    ProductService=object,
    # register_webhook() does an inline import of api.services.billing_webhook_service,
    # which transitively pulls these names from api.db.services.billing_service.
    BillingWebhookEventService=object,
    PaymentOrderService=object,
    PointAccountService=object,
    PurchasedProductOverviewService=object,
    SubscriptionService=object,
)
_stub_module("api.db.services.knowledgebase_service", KnowledgebaseService=object)
_stub_module("api.db.services.memory_service", MemoryService=object)
_stub_module(
    "api.db.services.user_service",
    UserService=object,
    TenantService=object,
    UserTenantService=object,
)
_stub_module("api.db.services.dialog_service", DialogService=object)
_stub_module(
    "api.db.joint_services.tenant_model_service",
    get_tenant_default_model_by_type=lambda *args, **kwargs: None,
)

from api.db import init_data

# Mirror of api.services.billing_webhook_service.FOCUSED_STRIPE_WEBHOOK. The test
# stubs api.db.services.billing_service, so we cannot import the real constant
# without pulling in the full webhook service module's transitive dependencies.
FOCUSED_STRIPE_WEBHOOK = [
    "invoice.paid",
    "invoice.payment_failed",
    "invoice.payment_action_required",
    "customer.subscription.updated",
    "customer.subscription.deleted",
    "customer.subscription.created",
    "checkout.session.completed",
    "payment_intent.succeeded",
]


@pytest.mark.p2
def test_register_webhook_deletes_duplicate_same_url_endpoint_before_skipping(monkeypatch):
    deleted_ids = []
    created = []
    upserts = []

    monkeypatch.setattr(
        init_data.settings,
        "BILLING",
        {
            "stripe_api_key": "sk_test_123",
            "webhook_url": "https://example.com/api/billing/webhook",
        },
        raising=False,
    )

    webhook_url = "https://example.com/api/billing/webhook"

    def _get_first_by_name(name):
        if name == "billing_webhook_id":
            return SimpleNamespace(value="we_primary")
        if name == "billing_webhook_secret":
            return SimpleNamespace(value="whsec_primary")
        return None

    monkeypatch.setattr(init_data.SystemSettingsService, "get_first_by_name", _get_first_by_name)
    monkeypatch.setattr(
        init_data.stripe.WebhookEndpoint,
        "retrieve",
        lambda webhook_id: SimpleNamespace(
            id=webhook_id,
            url=webhook_url,
            enabled_events=list(FOCUSED_STRIPE_WEBHOOK),
        ),
    )
    monkeypatch.setattr(
        init_data.stripe.WebhookEndpoint,
        "list",
        lambda: SimpleNamespace(
            data=[
                SimpleNamespace(id="we_primary", url=webhook_url, enabled_events=list(FOCUSED_STRIPE_WEBHOOK)),
                SimpleNamespace(id="we_duplicate", url=webhook_url, enabled_events=list(FOCUSED_STRIPE_WEBHOOK)),
            ]
        ),
    )
    monkeypatch.setattr(init_data.stripe.WebhookEndpoint, "delete", lambda endpoint_id: deleted_ids.append(endpoint_id))
    monkeypatch.setattr(
        init_data.stripe.WebhookEndpoint,
        "create",
        lambda **kwargs: created.append(kwargs) or SimpleNamespace(id="we_new", secret="whsec_new"),
    )
    monkeypatch.setattr(init_data, "_upsert_system_setting", lambda name, value: upserts.append((name, value)))

    init_data.register_webhook()

    assert deleted_ids == ["we_duplicate"]
    assert created == []
    assert upserts == []