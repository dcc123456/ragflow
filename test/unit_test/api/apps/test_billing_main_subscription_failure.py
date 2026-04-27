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
import asyncio
from contextlib import nullcontext
import importlib.util
import sys
import types
import warnings
from datetime import datetime, timezone
from pathlib import Path
from types import SimpleNamespace

import pytest
from quart import Blueprint

warnings.filterwarnings(
    "ignore",
    message="pkg_resources is deprecated as an API.*",
    category=UserWarning,
)
warnings.filterwarnings(
    "ignore",
    message="\\[Errno 13\\] Permission denied\\.  joblib will operate in serial mode",
    category=UserWarning,
)
warnings.filterwarnings("ignore", category=ResourceWarning)
warnings.filterwarnings("ignore", category=DeprecationWarning)


def _load_billing_app_module():
    module_name = "api.apps.billing"
    if module_name in sys.modules:
        return sys.modules[module_name]

    project_root = Path(__file__).resolve().parents[4]
    apps_dir = project_root / "api" / "apps"
    billing_path = apps_dir / "billing_app.py"

    api_apps_stub = types.ModuleType("api.apps")
    api_apps_stub.__file__ = str(apps_dir / "__init__.py")
    api_apps_stub.__package__ = "api.apps"
    api_apps_stub.__path__ = [str(apps_dir)]
    api_apps_stub.current_user = SimpleNamespace(id="test-user")
    api_apps_stub.login_required = lambda func: func
    sys.modules["api.apps"] = api_apps_stub

    spec = importlib.util.spec_from_file_location(module_name, billing_path)
    module = importlib.util.module_from_spec(spec)
    module.manager = Blueprint("billing", module_name)
    sys.modules[module_name] = module
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


billing_app = _load_billing_app_module()


@pytest.fixture(autouse=True)
def _stub_billing_db_atomic(monkeypatch):
    monkeypatch.setattr(billing_app.DB, "atomic", lambda: nullcontext())


def _stripe_subscription(status="active", price_id="price_pro", subscription_id="sub_main", customer_id="cus_main"):
    return SimpleNamespace(
        id=subscription_id,
        customer=customer_id,
        customer_id=customer_id,
        status=status,
        latest_invoice_id="in_latest",
        current_period_start=1710000000,
        current_period_end=1712592000,
        metadata={},
        trial_end=None,
        pending_update=None,
        items=SimpleNamespace(
            data=[
                SimpleNamespace(
                    price=SimpleNamespace(id=price_id, unit_amount=1000, currency="usd", nickname=price_id),
                    quantity=1,
                    current_period_start=1710000000,
                    current_period_end=1712592000,
                )
            ]
        ),
    )


@pytest.mark.p2
def test_main_subscription_status_policy_sets_are_complete():
    assert billing_app.MAIN_SUBSCRIPTION_ENTITLED_STATUSES == {"active", "trialing"}
    assert billing_app.MAIN_SUBSCRIPTION_RECOVERABLE_STATUSES == {"incomplete", "past_due", "unpaid"}
    for status in ["incomplete", "incomplete_expired", "past_due", "unpaid", "canceled", "paused"]:
        assert status in billing_app.MAIN_SUBSCRIPTION_DELINQUENT_STATUSES


@pytest.mark.p2
def test_normalize_subscription_status_trims_and_lowercases():
    assert billing_app._normalize_subscription_status(" Past_Due ") == "past_due"
    assert billing_app._normalize_subscription_status(None) == ""


@pytest.mark.p2
def test_safe_payment_order_datetime_ignores_invalid_zero_date():
    assert billing_app._safe_payment_order_created_at("0000-00-00 00:00:00", "in_bad") is None


@pytest.mark.p2
def test_extract_invoice_context_for_failed_invoice():
    invoice = {
        "id": "in_123",
        "hosted_invoice_url": "https://pay.example/in_123",
        "invoice_pdf": "https://pay.example/in_123.pdf",
        "status": "open",
        "customer": "cus_123",
        "subscription": "sub_123",
        "payment_intent": "pi_123",
        "amount_due": 5900,
        "currency": "usd",
        "attempt_count": 2,
        "next_payment_attempt": 1710100000,
        "billing_reason": "subscription_cycle",
        "created": 1710000001,
    }

    context = billing_app._extract_invoice_failure_context(invoice)

    assert context == {
        "invoice_id": "in_123",
        "invoice_url": "https://pay.example/in_123",
        "invoice_pdf_url": "https://pay.example/in_123.pdf",
        "invoice_status": "open",
        "customer_id": "cus_123",
        "subscription_id": "sub_123",
        "payment_intent_id": "pi_123",
        "amount_cents": 5900,
        "currency": "usd",
        "attempt_count": 2,
        "next_payment_attempt": 1710100000,
        "billing_reason": "subscription_cycle",
        "created": 1710000001,
    }


@pytest.mark.p2
def test_extract_invoice_context_uses_parent_subscription_details():
    invoice = {
        "id": "in_123",
        "customer": "cus_123",
        "parent": {
            "type": "subscription_details",
            "subscription_details": {
                "subscription": "sub_123",
            },
        },
    }

    context = billing_app._extract_invoice_failure_context(invoice)

    assert context["subscription_id"] == "sub_123"


@pytest.mark.p2
def test_invoice_payment_failed_syncs_main_subscription_to_past_due(monkeypatch):
    updates = []
    saved_orders = []

    monkeypatch.setattr(billing_app.StorageSubscriptionService, "get_by_subscription_id", lambda _subscription_id: None)
    monkeypatch.setattr(billing_app.SubscriptionService, "get_tenant_id_by_customer_id", lambda _customer_id: "tenant_1")
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "customer_id": "cus_main",
            "subscription_id": "sub_main",
            "subscription_status": "active",
            "product_id": "prod_pro",
            "plan_name": "Pro",
            "price_id": "price_pro",
            "order_id": "order_old",
            "invoice_id": "",
            "invoice_url": "",
            "invoice_pdf_url": "",
            "start_time": None,
            "end_time": None,
            "original_subscription_id": "sub_main",
        },
    )
    monkeypatch.setattr(billing_app.SubscriptionService, "update_subscription", lambda tenant_id, data: updates.append((tenant_id, data)))
    monkeypatch.setattr(billing_app.stripe.Subscription, "retrieve", lambda _subscription_id: _stripe_subscription(status="active"))
    monkeypatch.setattr(billing_app.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_pro": "Pro"}, raising=False)
    monkeypatch.setattr(billing_app, "get_product_id_by_name", lambda _plan_name: "prod_pro")
    monkeypatch.setattr(billing_app.PaymentOrderService, "get_by_order_id", lambda _order_id: None)
    monkeypatch.setattr(billing_app.PaymentOrderService, "save", lambda **kwargs: saved_orders.append(kwargs))

    event = {
        "type": "invoice.payment_failed",
        "created": 1710000002,
        "data": {
            "object": {
                "id": "in_failed",
                "customer": "cus_main",
                "subscription": "sub_main",
                "hosted_invoice_url": "https://pay.example/in_failed",
                "invoice_pdf": "https://pay.example/in_failed.pdf",
                "status": "open",
                "payment_intent": "pi_failed",
                "amount_due": 15900,
                "currency": "usd",
                "attempt_count": 1,
                "billing_reason": "subscription_cycle",
                "created": 1710000001,
            }
        },
    }

    billing_app._handle_invoice_payment_failed(event)

    assert updates[-1][0] == "tenant_1"
    assert updates[-1][1]["subscription_status"] == "past_due"
    assert updates[-1][1]["invoice_id"] == "in_failed"
    assert updates[-1][1]["invoice_url"] == "https://pay.example/in_failed"
    assert saved_orders[-1]["order_id"] == "in_failed"
    assert saved_orders[-1]["payment_status"] == "failed"
    assert saved_orders[-1]["paid"] is False


@pytest.mark.p2
def test_invoice_payment_failed_uses_parent_subscription_details(monkeypatch):
    updates = []
    saved_orders = []
    retrieved_subscription_ids = []

    monkeypatch.setattr(billing_app.StorageSubscriptionService, "get_by_subscription_id", lambda _subscription_id: None)
    monkeypatch.setattr(billing_app.SubscriptionService, "get_tenant_id_by_customer_id", lambda _customer_id: "tenant_1")
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "customer_id": "cus_main",
            "subscription_id": "sub_main",
            "subscription_status": "active",
            "product_id": "prod_pro",
            "plan_name": "Pro",
            "price_id": "price_pro",
            "order_id": "order_old",
            "invoice_id": "",
            "invoice_url": "",
            "invoice_pdf_url": "",
            "start_time": None,
            "end_time": None,
            "original_subscription_id": "sub_main",
        },
    )
    monkeypatch.setattr(billing_app.SubscriptionService, "update_subscription", lambda tenant_id, data: updates.append((tenant_id, data)))
    monkeypatch.setattr(billing_app.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_pro": "Pro"}, raising=False)
    monkeypatch.setattr(billing_app, "get_product_id_by_name", lambda _plan_name: "prod_pro")
    monkeypatch.setattr(billing_app.PaymentOrderService, "get_by_order_id", lambda _order_id: None)
    monkeypatch.setattr(billing_app.PaymentOrderService, "save", lambda **kwargs: saved_orders.append(kwargs))

    def retrieve_subscription(subscription_id):
        retrieved_subscription_ids.append(subscription_id)
        return _stripe_subscription(status="past_due", subscription_id=subscription_id)

    monkeypatch.setattr(billing_app.stripe.Subscription, "retrieve", retrieve_subscription)

    event = {
        "type": "invoice.payment_failed",
        "created": 1710000002,
        "data": {
            "object": {
                "id": "in_failed",
                "customer": "cus_main",
                "parent": {
                    "type": "subscription_details",
                    "subscription_details": {
                        "subscription": "sub_main",
                    },
                },
                "hosted_invoice_url": "https://pay.example/in_failed",
                "invoice_pdf": "https://pay.example/in_failed.pdf",
                "status": "open",
                "amount_due": 15900,
                "currency": "usd",
                "attempt_count": 1,
                "billing_reason": "subscription_cycle",
                "created": 1710000001,
            }
        },
    }

    billing_app._handle_invoice_payment_failed(event)

    assert retrieved_subscription_ids == ["sub_main"]
    assert updates[-1][1]["subscription_status"] == "past_due"
    assert saved_orders[-1]["payment_subscription_id"] == "sub_main"


@pytest.mark.p2
def test_main_subscription_payment_order_converts_unix_created_at(monkeypatch):
    saved_orders = []

    monkeypatch.setattr(billing_app.PaymentOrderService, "get_by_order_id", lambda _order_id: None)
    monkeypatch.setattr(billing_app.PaymentOrderService, "save", lambda **kwargs: saved_orders.append(kwargs))

    billing_app._upsert_main_subscription_payment_order(
        tenant_id="tenant_1",
        customer_id="cus_main",
        subscription_id="sub_main",
        invoice_id="in_failed",
        price_id="price_pro",
        product_id="prod_pro",
        product_name="Pro",
        amount_cents=15900,
        currency="usd",
        invoice_url="https://pay.example/in_failed",
        invoice_pdf_url="https://pay.example/in_failed.pdf",
        payment_status="failed",
        stripe_status="open",
        paid=False,
        order_created_at=1710000001,
    )

    assert saved_orders[-1]["order_created_at"] == datetime(2024, 3, 9, 16, 0, 1, tzinfo=timezone.utc)


@pytest.mark.p2
def test_invoice_payment_action_required_uses_same_main_failure_path(monkeypatch):
    updates = []

    monkeypatch.setattr(billing_app.StorageSubscriptionService, "get_by_subscription_id", lambda _subscription_id: None)
    monkeypatch.setattr(billing_app.SubscriptionService, "get_tenant_id_by_customer_id", lambda _customer_id: "tenant_1")
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "customer_id": "cus_main",
            "subscription_id": "sub_main",
            "subscription_status": "active",
            "product_id": "prod_pro",
            "plan_name": "Pro",
            "price_id": "price_pro",
            "order_id": "order_old",
            "invoice_id": "",
            "invoice_url": "",
            "invoice_pdf_url": "",
            "start_time": None,
            "end_time": None,
            "original_subscription_id": "sub_main",
        },
    )
    monkeypatch.setattr(billing_app.SubscriptionService, "update_subscription", lambda tenant_id, data: updates.append((tenant_id, data)))
    monkeypatch.setattr(billing_app.stripe.Subscription, "retrieve", lambda _subscription_id: _stripe_subscription(status="past_due"))
    monkeypatch.setattr(billing_app.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_pro": "Pro"}, raising=False)
    monkeypatch.setattr(billing_app, "get_product_id_by_name", lambda _plan_name: "prod_pro")
    monkeypatch.setattr(billing_app.PaymentOrderService, "get_by_order_id", lambda _order_id: None)
    monkeypatch.setattr(billing_app.PaymentOrderService, "save", lambda **_kwargs: None)

    event = {
        "type": "invoice.payment_action_required",
        "created": 1710000002,
        "data": {
            "object": {
                "id": "in_action",
                "customer": "cus_main",
                "subscription": "sub_main",
                "hosted_invoice_url": "https://pay.example/in_action",
                "status": "open",
                "amount_due": 15900,
                "currency": "usd",
            }
        },
    }

    billing_app._handle_invoice_payment_action_required(event)

    assert updates[-1][1]["subscription_status"] == "past_due"
    assert updates[-1][1]["invoice_id"] == "in_action"


@pytest.mark.p2
def test_invoice_paid_updates_existing_failed_order_and_restores_active(monkeypatch):
    payment_order_updates = []
    subscription_updates = []

    class _Period:
        start = 1710000000
        end = 1712592000

    item = SimpleNamespace(
        amount=15900,
        description="Pro subscription",
        quantity=1,
        period=_Period(),
        pricing=SimpleNamespace(price_details=SimpleNamespace(price="price_pro")),
        parent=SimpleNamespace(subscription_item_details=SimpleNamespace(subscription="sub_main")),
    )
    invoice_paid = SimpleNamespace(
        id="in_failed",
        lines=SimpleNamespace(data=[item]),
        description="",
        billing_reason="subscription_cycle",
        amount_paid=15900,
        currency="usd",
        status="paid",
        created=1710100000,
        hosted_invoice_url="https://pay.example/in_failed",
        invoice_pdf="https://pay.example/in_failed.pdf",
        customer_id="cus_main",
        metadata={},
        model_dump=lambda: {},
    )

    monkeypatch.setattr(billing_app, "InvoicePaid", lambda **_kwargs: invoice_paid)
    monkeypatch.setattr(billing_app.SubscriptionService, "get_tenant_id_by_customer_id", lambda _customer_id: "tenant_1")
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "customer_id": "cus_main",
            "subscription_id": "sub_main",
            "subscription_status": "past_due",
            "product_id": "prod_pro",
            "plan_name": "Pro",
            "price_id": "price_pro",
            "order_id": "order_old",
            "invoice_id": "in_failed",
            "invoice_url": "https://pay.example/in_failed",
            "invoice_pdf_url": "https://pay.example/in_failed.pdf",
            "start_time": None,
            "end_time": None,
            "original_subscription_id": "sub_main",
        },
    )
    monkeypatch.setattr(billing_app.SubscriptionService, "update_subscription", lambda tenant_id, data: subscription_updates.append((tenant_id, data)))
    monkeypatch.setattr(billing_app.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_pro": "Pro"}, raising=False)
    monkeypatch.setattr(billing_app, "get_product_id_by_name", lambda _plan_name: "prod_pro")
    monkeypatch.setattr(
        billing_app.PaymentOrderService,
        "get_by_order_id",
        lambda _order_id: {"id": "po_failed", "order_id": "in_failed", "payment_status": "failed"},
    )
    monkeypatch.setattr(billing_app.PaymentOrderService, "update_by_order_id", lambda order_id, data: payment_order_updates.append((order_id, data)))
    monkeypatch.setattr(billing_app.PaymentOrderService, "save", lambda **_kwargs: (_ for _ in ()).throw(AssertionError("should update existing order")))
    monkeypatch.setattr(billing_app.PointAccountService, "sync_plan_points_on_subscription_paid", lambda _tenant_id, _plan_quota: None)

    event = {"type": "invoice.paid", "data": {"object": {"id": "in_failed"}}}

    billing_app._handle_invoice_paid(event)

    assert payment_order_updates[-1][0] == "in_failed"
    assert payment_order_updates[-1][1]["payment_status"] == "success"
    assert payment_order_updates[-1][1]["paid"] is True
    assert subscription_updates[-1][1]["subscription_status"] == "active"
    assert subscription_updates[-1][1]["order_id"] == "po_failed"


@pytest.mark.p2
@pytest.mark.parametrize("status", ["past_due", "unpaid", "canceled", "paused", "incomplete", "incomplete_expired", "active"])
def test_customer_subscription_updated_status_only_syncs_main_subscription(monkeypatch, status):
    updates = []

    event_model = SimpleNamespace(
        created=1710000003,
        data=SimpleNamespace(
            object=_stripe_subscription(status=status),
            previous_attributes=SimpleNamespace(status="active", plan=None, items=None, trial_end=None),
        ),
        model_dump=lambda: {},
    )

    monkeypatch.setattr(billing_app, "SubscriptionUpdated", lambda **_kwargs: event_model)
    monkeypatch.setattr(billing_app.StorageSubscriptionService, "get_by_subscription_id", lambda _subscription_id: None)
    monkeypatch.setattr(billing_app.SubscriptionService, "get_tenant_id_by_customer_id", lambda _customer_id: "tenant_1")
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "customer_id": "cus_main",
            "subscription_id": "sub_main",
            "subscription_status": "active",
            "product_id": "prod_pro",
            "plan_name": "Pro",
            "price_id": "price_pro",
            "order_id": "order_old",
            "invoice_id": "",
            "invoice_url": "",
            "invoice_pdf_url": "",
            "start_time": None,
            "end_time": None,
            "original_subscription_id": "sub_main",
        },
    )
    monkeypatch.setattr(billing_app.SubscriptionService, "update_subscription", lambda tenant_id, data: updates.append((tenant_id, data)))
    monkeypatch.setattr(billing_app.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_pro": "Pro"}, raising=False)
    monkeypatch.setattr(billing_app, "get_product_id_by_name", lambda _plan_name: "prod_pro")

    event = {"type": "customer.subscription.updated", "data": {"object": {"id": "sub_main"}}}

    billing_app._handle_customer_subscription_updated(event)

    assert updates[-1][1]["subscription_status"] == status


@pytest.mark.p2
def test_customer_subscription_deleted_syncs_main_subscription_to_canceled(monkeypatch):
    updates = []

    monkeypatch.setattr(billing_app.StorageSubscriptionService, "get_by_subscription_id", lambda _subscription_id: None)
    monkeypatch.setattr(billing_app.SubscriptionService, "get_tenant_id_by_customer_id", lambda _customer_id: "tenant_1")
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "customer_id": "cus_main",
            "subscription_id": "sub_main",
            "subscription_status": "active",
            "product_id": "prod_pro",
            "plan_name": "Pro",
            "price_id": "price_pro",
            "order_id": "order_old",
            "invoice_id": "",
            "invoice_url": "",
            "invoice_pdf_url": "",
            "start_time": None,
            "end_time": None,
            "original_subscription_id": "sub_main",
            "id": "prod_pro",
            "quota_apps": 100,
            "quota_members": 10,
            "quota_kb_storage": 10485760,
            "task_priority": "high",
            "price_ids": "price_pro",
            "product_type": "subscription",
            "usage_stat_type": "",
            "version": 1,
        },
    )
    monkeypatch.setattr(billing_app.SubscriptionService, "update_subscription", lambda tenant_id, data: updates.append((tenant_id, data)))

    event = {
        "type": "customer.subscription.deleted",
        "data": {"object": {"id": "sub_main", "customer": "cus_main"}},
    }

    billing_app._handle_customer_subscription_deleted(event)

    assert updates[-1][0] == "tenant_1"
    assert updates[-1][1] == {
        "status": "canceled",
        "subscription_status": "canceled",
        "subscription_id": "sub_main",
        "customer_id": "cus_main",
    }


@pytest.mark.p2
def test_customer_subscription_deleted_ignores_stale_main_subscription(monkeypatch):
    updates = []

    monkeypatch.setattr(billing_app.StorageSubscriptionService, "get_by_subscription_id", lambda _subscription_id: None)
    monkeypatch.setattr(billing_app.SubscriptionService, "get_tenant_id_by_customer_id", lambda _customer_id: "tenant_1")
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "customer_id": "cus_main",
            "subscription_id": "sub_new",
            "subscription_status": "active",
            "product_id": "prod_starter",
            "plan_name": "Starter",
            "price_id": "price_starter",
            "invoice_url": "https://pay.example/current",
            "invoice_pdf_url": "https://pay.example/current.pdf",
            "start_time": None,
            "end_time": None,
            "original_subscription_id": "sub_old",
            "id": "prod_starter",
            "quota_apps": 100,
            "quota_members": 10,
            "quota_kb_storage": 10485760,
            "task_priority": "high",
            "price_ids": "price_starter",
            "product_type": "subscription",
            "usage_stat_type": "",
            "version": 1,
        },
    )
    monkeypatch.setattr(billing_app.SubscriptionService, "update_subscription", lambda tenant_id, data: updates.append((tenant_id, data)))

    event = {
        "type": "customer.subscription.deleted",
        "data": {"object": {"id": "sub_old", "customer": "cus_main"}},
    }

    billing_app._handle_customer_subscription_deleted(event)

    assert updates == []


@pytest.mark.p2
def test_subscription_updated_with_pending_update_does_not_upgrade_entitlement(monkeypatch):
    updates = []
    subscription = _stripe_subscription(status="active", price_id="price_pro")
    subscription.pending_update = {"expires_at": 1710100000}
    previous = SimpleNamespace(
        status=None,
        trial_end=None,
        plan=None,
        items=SimpleNamespace(data=[SimpleNamespace(price=SimpleNamespace(id="price_starter", unit_amount=500, currency="usd", nickname="price_starter"), subscription_id="sub_main")]),
    )
    event_model = SimpleNamespace(
        created=1710000003,
        data=SimpleNamespace(object=subscription, previous_attributes=previous),
        model_dump=lambda: {},
    )

    monkeypatch.setattr(billing_app, "SubscriptionUpdated", lambda **_kwargs: event_model)
    monkeypatch.setattr(billing_app.StorageSubscriptionService, "get_by_subscription_id", lambda _subscription_id: None)
    monkeypatch.setattr(billing_app.SubscriptionService, "get_tenant_id_by_customer_id", lambda _customer_id: "tenant_1")
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "customer_id": "cus_main",
            "subscription_id": "sub_main",
            "subscription_status": "active",
            "product_id": "prod_starter",
            "plan_name": "Starter",
            "price_id": "price_starter",
            "order_id": "order_old",
            "invoice_id": "",
            "invoice_url": "",
            "invoice_pdf_url": "",
            "start_time": None,
            "end_time": None,
            "original_subscription_id": "sub_main",
        },
    )
    monkeypatch.setattr(billing_app.SubscriptionService, "update_subscription", lambda tenant_id, data: updates.append((tenant_id, data)))
    monkeypatch.setattr(billing_app.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_starter": "Starter", "price_pro": "Pro"}, raising=False)
    monkeypatch.setattr(billing_app, "get_product_id_by_name", lambda plan_name: f"prod_{plan_name.lower()}")

    billing_app._handle_customer_subscription_updated({"type": "customer.subscription.updated", "data": {"object": {"id": "sub_main"}}})

    assert updates[-1][1]["plan_name"] == "Starter"
    assert updates[-1][1]["subscription_status"] == "past_due"


@pytest.mark.p2
def test_scheduled_downgrade_price_change_still_updates_plan(monkeypatch):
    updates = []
    saved_orders = []
    subscription = _stripe_subscription(status="active", price_id="price_starter")
    subscription.pending_update = None
    previous = SimpleNamespace(
        status=None,
        trial_end=None,
        plan=None,
        items=SimpleNamespace(data=[SimpleNamespace(price=SimpleNamespace(id="price_pro", unit_amount=2000, currency="usd", nickname="price_pro"), subscription_id="sub_main")]),
    )
    event_model = SimpleNamespace(
        created=1710000003,
        data=SimpleNamespace(object=subscription, previous_attributes=previous),
        model_dump=lambda: {},
    )

    monkeypatch.setattr(billing_app, "SubscriptionUpdated", lambda **_kwargs: event_model)
    monkeypatch.setattr(billing_app.StorageSubscriptionService, "get_by_subscription_id", lambda _subscription_id: None)
    monkeypatch.setattr(billing_app.SubscriptionService, "get_tenant_id_by_customer_id", lambda _customer_id: "tenant_1")
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "customer_id": "cus_main",
            "subscription_id": "sub_main",
            "subscription_status": "active",
            "product_id": "prod_pro",
            "plan_name": "Pro",
            "price_id": "price_pro",
            "order_id": "order_old",
            "invoice_id": "",
            "invoice_url": "",
            "invoice_pdf_url": "",
            "start_time": None,
            "end_time": None,
            "original_subscription_id": "sub_main",
        },
    )
    monkeypatch.setattr(billing_app.SubscriptionService, "update_subscription", lambda tenant_id, data: updates.append((tenant_id, data)))
    monkeypatch.setattr(billing_app.PaymentOrderService, "get_by_order_id", lambda _invoice_id: None)
    monkeypatch.setattr(billing_app.PaymentOrderService, "save", lambda **kwargs: saved_orders.append(kwargs))
    monkeypatch.setattr(billing_app.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_starter": "Starter", "price_pro": "Pro"}, raising=False)
    monkeypatch.setattr(billing_app, "get_product_id_by_name", lambda plan_name: f"prod_{plan_name.lower()}")
    monkeypatch.setattr(billing_app, "get_plan_priority_by_price_id", lambda price_id: {"price_starter": 1, "price_pro": 2}.get(price_id), raising=False)
    monkeypatch.setattr(billing_app, "is_subscription_latest_invoice_paid_sync", lambda _subscription: True)

    billing_app._handle_customer_subscription_updated({"type": "customer.subscription.updated", "data": {"object": {"id": "sub_main"}}})

    assert updates[-1][1]["plan_name"] == "Starter"
    assert updates[-1][1]["subscription_status"] == "active"
    assert saved_orders == []


@pytest.mark.p2
def test_storage_cancel_async_skips_canceled_subscription(monkeypatch):
    modify_calls = []

    monkeypatch.setattr(
        billing_app.StorageSubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "subscription_id": "sub_canceled",
            "status": "canceled",
        },
    )

    async def modify_async(subscription_id, **kwargs):
        modify_calls.append((subscription_id, kwargs))
        raise AssertionError("canceled storage subscription should not be updated")

    monkeypatch.setattr(billing_app.stripe.Subscription, "modify_async", modify_async)

    ok, data = asyncio.run(billing_app._set_storage_cancel_at_period_end_async("tenant_1", value=True))

    assert ok is True
    assert data == {}
    assert modify_calls == []


@pytest.mark.p2
def test_storage_cancel_async_releases_schedule_and_retries_on_schedule_managed_error(monkeypatch):
    release_calls = []
    modify_calls = []
    sync_calls = []

    monkeypatch.setattr(
        billing_app.StorageSubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "subscription_id": "sub_sched_managed",
            "status": "active",
            "effective_quantity_gb": 2,
            "schedule_id": "sub_sched_123",
        },
    )

    async def cancel_scheduled_change(subscription_id):
        release_calls.append(subscription_id)
        return True

    async def modify_async(subscription_id, **kwargs):
        modify_calls.append((subscription_id, kwargs))
        if len(modify_calls) == 1:
            raise billing_app.stripe.InvalidRequestError(
                "The subscription is managed by the subscription schedule `sub_sched_123`, "
                "and updating any cancelation behavior directly is not allowed. Please update the schedule instead.",
                param=None,
            )
        return {
            "id": subscription_id,
            "customer": "cus_1",
            "status": "active",
            "schedule": "",
            "cancel_at_period_end": True,
            "current_period_start": 1710000000,
            "current_period_end": 1712592000,
            "items": {
                "data": [
                    {
                        "id": "si_1",
                        "price": {"id": "price_storage"},
                        "quantity": 2,
                    }
                ]
            },
        }

    monkeypatch.setattr(billing_app, "cancel_scheduled_subscription_change_async", cancel_scheduled_change)
    monkeypatch.setattr(billing_app.stripe.Subscription, "modify_async", modify_async)
    monkeypatch.setattr(
        billing_app,
        "_sync_storage_subscription_record",
        lambda tenant_id, subscription_obj, **kwargs: sync_calls.append((tenant_id, subscription_obj, kwargs)),
    )

    ok, data = asyncio.run(billing_app._set_storage_cancel_at_period_end_async("tenant_1", value=True))

    assert ok is True
    assert release_calls == ["sub_sched_managed"]
    assert len(modify_calls) == 2
    assert all(call[1] == {"cancel_at_period_end": True} for call in modify_calls)
    assert sync_calls[0][0] == "tenant_1"
    assert sync_calls[0][2]["pending_action"] == "cancel"
    assert data == {
        "cancel_at_period_end": True,
        "effective_at": datetime(2024, 4, 8, 16, 0, tzinfo=timezone.utc),
    }


@pytest.mark.p2
def test_storage_cancel_sync_skips_canceled_subscription(monkeypatch):
    modify_calls = []

    monkeypatch.setattr(
        billing_app.StorageSubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "subscription_id": "sub_canceled",
            "status": "canceled",
        },
    )

    def modify(subscription_id, **kwargs):
        modify_calls.append((subscription_id, kwargs))
        raise AssertionError("canceled storage subscription should not be updated")

    monkeypatch.setattr(billing_app.stripe.Subscription, "modify", modify)

    ok, data = billing_app._set_storage_cancel_at_period_end_sync("tenant_1", value=True)

    assert ok is True
    assert data == {}
    assert modify_calls == []


@pytest.mark.p2
def test_storage_cancel_sync_releases_schedule_and_retries_on_schedule_managed_error(monkeypatch):
    release_calls = []
    modify_calls = []
    sync_calls = []

    monkeypatch.setattr(
        billing_app.StorageSubscriptionService,
        "get_by_tenant_id",
        lambda _tenant_id: {
            "tenant_id": "tenant_1",
            "subscription_id": "sub_sched_managed",
            "status": "active",
            "effective_quantity_gb": 2,
            "schedule_id": "sub_sched_123",
        },
    )

    def cancel_scheduled_change(subscription_id):
        release_calls.append(subscription_id)
        return True

    def modify(subscription_id, **kwargs):
        modify_calls.append((subscription_id, kwargs))
        if len(modify_calls) == 1:
            raise billing_app.stripe.InvalidRequestError(
                "The subscription is managed by the subscription schedule `sub_sched_123`, "
                "and updating any cancelation behavior directly is not allowed. Please update the schedule instead.",
                param=None,
            )
        return {
            "id": subscription_id,
            "customer": "cus_1",
            "status": "active",
            "schedule": "",
            "cancel_at_period_end": True,
            "current_period_start": 1710000000,
            "current_period_end": 1712592000,
            "items": {
                "data": [
                    {
                        "id": "si_1",
                        "price": {"id": "price_storage"},
                        "quantity": 2,
                    }
                ]
            },
        }

    monkeypatch.setattr(billing_app, "cancel_scheduled_subscription_change_sync", cancel_scheduled_change)
    monkeypatch.setattr(billing_app.stripe.Subscription, "modify", modify)
    monkeypatch.setattr(
        billing_app,
        "_sync_storage_subscription_record",
        lambda tenant_id, subscription_obj, **kwargs: sync_calls.append((tenant_id, subscription_obj, kwargs)),
    )

    ok, data = billing_app._set_storage_cancel_at_period_end_sync("tenant_1", value=True)

    assert ok is True
    assert release_calls == ["sub_sched_managed"]
    assert len(modify_calls) == 2
    assert all(call[1] == {"cancel_at_period_end": True} for call in modify_calls)
    assert sync_calls[0][0] == "tenant_1"
    assert sync_calls[0][2]["pending_action"] == "cancel"
    assert data == {
        "cancel_at_period_end": True,
        "effective_at": datetime(2024, 4, 8, 16, 0, tzinfo=timezone.utc),
    }


@pytest.mark.p2
def test_payment_state_marks_delinquent_subscription_as_recoverable():
    state = billing_app._main_subscription_payment_state(
        {"subscription_status": "past_due", "invoice_url": "https://pay.example/in_123"}
    )
    assert state == {
        "payment_required": True,
        "payment_recoverable": True,
        "payment_recovery_url": "https://pay.example/in_123",
    }


@pytest.mark.p2
def test_payment_state_marks_active_subscription_as_not_required():
    state = billing_app._main_subscription_payment_state({"subscription_status": "active", "invoice_url": ""})
    assert state == {
        "payment_required": False,
        "payment_recoverable": False,
        "payment_recovery_url": "",
    }
