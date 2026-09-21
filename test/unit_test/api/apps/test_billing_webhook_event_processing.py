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
import json
from contextlib import nullcontext
import importlib.util
import sys
import types
from pathlib import Path
from types import SimpleNamespace

import pytest
from peewee import IntegrityError
from quart import Blueprint, Quart


def _billing_app_stub():
    project_root = Path(__file__).resolve().parents[4]
    apps_dir = project_root / "api" / "apps"
    stub = types.ModuleType("api.apps")
    stub.__file__ = str(apps_dir / "__init__.py")
    stub.__package__ = "api.apps"
    stub.__path__ = [str(apps_dir)]
    stub.current_user = SimpleNamespace(id="test-user")
    stub.login_required = lambda func: func
    return stub


@pytest.fixture
def billing_app(monkeypatch):
    module_name = "api.apps.billing"
    project_root = Path(__file__).resolve().parents[4]
    apps_dir = project_root / "api" / "apps"
    billing_path = apps_dir / "billing_app.py"

    monkeypatch.setitem(sys.modules, "api.apps", _billing_app_stub())

    spec = importlib.util.spec_from_file_location(module_name, billing_path)
    module = importlib.util.module_from_spec(spec)
    module.manager = Blueprint("billing", module_name)
    monkeypatch.setitem(sys.modules, module_name, module)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


billing_webhook_service = importlib.import_module("api.services.billing_webhook_service")


def _stripe_subscription(subscription_id: str, price_id: str) -> dict:
    return {
        "id": subscription_id,
        "items": {"data": [{"id": "si_test", "price": {"id": price_id}, "quantity": 1}]},
    }


@pytest.mark.p2
def test_unpaid_trial_replacement_preserves_trial_entitlement(monkeypatch):
    existing = {
        "subscription_id": "",
        "plan_name": "Trial",
        "price_id": "price_trial",
    }
    incoming = _stripe_subscription("sub_paid", "price_starter")
    monkeypatch.setattr(billing_webhook_service, "is_trial_plan_name", lambda name: name == "Trial")
    monkeypatch.setattr(billing_webhook_service.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_starter": "Starter"})

    assert billing_webhook_service._should_preserve_existing_plan_for_unpaid_upgrade(existing, incoming, latest_invoice_paid=False)


@pytest.mark.p2
def test_paid_trial_replacement_does_not_preserve_trial_entitlement(monkeypatch):
    existing = {
        "subscription_id": "",
        "plan_name": "Trial",
        "price_id": "price_trial",
    }
    incoming = _stripe_subscription("sub_paid", "price_starter")
    monkeypatch.setattr(billing_webhook_service, "is_trial_plan_name", lambda name: name == "Trial")
    monkeypatch.setattr(billing_webhook_service.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_starter": "Starter"})

    assert not billing_webhook_service._should_preserve_existing_plan_for_unpaid_upgrade(existing, incoming, latest_invoice_paid=True)


@pytest.mark.p2
def test_unpaid_upgrade_tracks_payment_state_without_revoking_entitlement(monkeypatch):
    existing = {
        "subscription_id": "sub_existing",
        "subscription_status": "active",
        "status": "active",
        "plan_name": "Starter",
        "price_id": "price_starter",
        "product_id": "prod_starter",
    }
    incoming = {
        "id": "sub_existing",
        "customer": "cus_1",
        "status": "past_due",
    }
    captured = {}
    monkeypatch.setattr(
        billing_webhook_service.SubscriptionService,
        "get_raw_by_tenant_id",
        lambda _tenant_id: existing,
    )
    monkeypatch.setattr(
        billing_webhook_service.SubscriptionService,
        "upsert_subscription",
        lambda _tenant_id, data: captured.update(data),
    )
    monkeypatch.setattr(
        billing_webhook_service,
        "extract_plan_subscription_item",
        lambda _subscription: ("si_1", "price_pro", 1),
    )
    monkeypatch.setattr(
        billing_webhook_service,
        "extract_subscription_period",
        lambda _subscription: (1710000000, 1712678400),
    )
    monkeypatch.setattr(
        billing_webhook_service.settings,
        "BILLING_PRICEID_TO_PRODUCT",
        {"price_pro": "Pro"},
    )
    monkeypatch.setattr(billing_webhook_service, "get_product_id_by_name", lambda name: f"prod_{name.lower()}")
    monkeypatch.setattr(billing_webhook_service, "_sync_api_token_rate_limit_for_subscription", lambda **_kwargs: None)

    billing_webhook_service._sync_main_subscription_from_stripe(
        tenant_id="tenant_1",
        stripe_subscription=incoming,
        subscription_status="past_due",
        invoice_id="in_unpaid",
        invoice_url="https://invoice.stripe.com/unpaid",
        preserve_existing_plan=True,
    )

    assert captured["status"] == "past_due"
    assert captured["subscription_status"] == "active"
    assert captured["plan_name"] == "Starter"
    assert captured["price_id"] == "price_starter"
    assert captured["subscription_id"] == "sub_existing"
    assert captured["invoice_id"] == "in_unpaid"


@pytest.mark.p2
def test_durable_payment_attention_uses_stripe_lifecycle_status(billing_app):
    result = billing_app._main_subscription_payment_state(
        {
            "status": "past_due",
            "subscription_status": "active",
            "invoice_url": "https://invoice.stripe.com/unpaid",
        }
    )

    assert result == {
        "payment_state": "requires_action",
        "payment_required": True,
        "payment_recoverable": True,
        "payment_recovery_url": "https://invoice.stripe.com/unpaid",
    }


@pytest.mark.p2
def test_subscription_checkout_result_requires_action_is_recoverable(billing_app, monkeypatch):
    monkeypatch.setattr(billing_app, "get_json_result", lambda **kwargs: kwargs)
    subscription = {
        "id": "sub_3ds",
        "status": "incomplete",
        "latest_invoice": {
            "id": "in_3ds",
            "status": "open",
            "amount_due": 12900,
            "currency": "usd",
            "hosted_invoice_url": "https://invoice.stripe.com/test",
            "payment_intent": {"status": "requires_action"},
        },
    }

    result = billing_app._subscription_checkout_result(subscription, plan_name="Starter", price_id="price_starter")

    assert result["code"] == billing_app.RetCode.SUCCESS
    assert result["data"]["payment_state"] == "requires_action"
    assert result["data"]["redirect_to"] == "https://invoice.stripe.com/test"


@pytest.mark.p2
def test_stripe_payment_error_result_is_sanitized(billing_app, monkeypatch):
    monkeypatch.setattr(billing_app, "get_json_result", lambda **kwargs: kwargs)
    exc = billing_app.stripe.CardError(
        message="raw provider message",
        param=None,
        code="card_declined",
        json_body={"error": {"code": "card_declined", "decline_code": "do_not_honor"}},
    )

    result = billing_app._stripe_payment_error_result(exc)
    serialized = json.dumps(result)

    assert result["code"] == billing_app.RetCode.BILLING_PAYMENT_FAILED
    assert result["data"] == {"error_type": "payment_declined"}
    assert "raw provider message" not in serialized
    assert "request_id" not in serialized


@pytest.mark.p2
@pytest.mark.parametrize(
    ("exc_factory", "expected_error_type"),
    [
        (
            lambda stripe_module: stripe_module.CardError(
                message="authentication required",
                param=None,
                code="subscription_payment_intent_requires_action",
            ),
            "payment_action_required",
        ),
        (
            lambda stripe_module: stripe_module.APIConnectionError("connection failed"),
            "payment_service_unavailable",
        ),
        (
            lambda stripe_module: stripe_module.InvalidRequestError(
                message="bad request",
                param=None,
                code="resource_missing",
            ),
            "payment_failed",
        ),
    ],
)
def test_stripe_payment_error_result_classifies_provider_errors(
    billing_app,
    monkeypatch,
    exc_factory,
    expected_error_type,
):
    monkeypatch.setattr(billing_app, "get_json_result", lambda **kwargs: kwargs)

    result = billing_app._stripe_payment_error_result(exc_factory(billing_app.stripe))

    assert result["data"] == {"error_type": expected_error_type}


@pytest.mark.p2
def test_find_existing_target_subscription_uses_tenant_metadata(billing_app, monkeypatch):
    candidate = {
        "id": "sub_existing",
        "customer": {"id": "cus_1"},
        "status": "incomplete",
        "metadata": {"tenant_id": "tenant_1"},
        "items": {"data": [{"id": "si_1", "price": {"id": "price_starter"}, "quantity": 1}]},
    }
    monkeypatch.setattr(billing_app.SubscriptionService, "get_raw_by_tenant_id", lambda _tenant_id: {})

    async def list_async(**_kwargs):
        return {"data": [candidate]}

    async def retrieve_async(subscription_id, **_kwargs):
        assert subscription_id == "sub_existing"
        return candidate

    monkeypatch.setattr(billing_app.stripe.Subscription, "list_async", list_async)
    monkeypatch.setattr(billing_app.stripe.Subscription, "retrieve_async", retrieve_async)

    result = asyncio.run(
        billing_app._find_existing_target_subscription_async(
            tenant_id="tenant_1",
            customer_id="cus_1",
            target_price_id="price_starter",
        )
    )

    assert result["id"] == "sub_existing"


@pytest.mark.p2
def test_find_existing_target_subscription_scans_all_pages(billing_app, monkeypatch):
    matching = {
        "id": "sub_matching",
        "customer": "cus_1",
        "status": "incomplete",
        "metadata": {"tenant_id": "tenant_1"},
        "items": {"data": [{"id": "si_2", "price": {"id": "price_starter"}, "quantity": 1}]},
    }
    calls = []
    monkeypatch.setattr(billing_app.SubscriptionService, "get_raw_by_tenant_id", lambda _tenant_id: {})

    async def list_async(**kwargs):
        calls.append(kwargs)
        if "starting_after" not in kwargs:
            return {
                "data": [
                    {
                        "id": "sub_first_page",
                        "customer": "cus_1",
                        "status": "active",
                        "metadata": {"tenant_id": "tenant_1"},
                        "items": {"data": [{"id": "si_1", "price": {"id": "price_other"}, "quantity": 1}]},
                    }
                ],
                "has_more": True,
            }
        assert kwargs["starting_after"] == "sub_first_page"
        return {"data": [matching], "has_more": False}

    async def retrieve_async(subscription_id, **_kwargs):
        assert subscription_id == "sub_matching"
        return matching

    monkeypatch.setattr(billing_app.stripe.Subscription, "list_async", list_async)
    monkeypatch.setattr(billing_app.stripe.Subscription, "retrieve_async", retrieve_async)

    result = asyncio.run(
        billing_app._find_existing_target_subscription_async(
            tenant_id="tenant_1",
            customer_id="cus_1",
            target_price_id="price_starter",
        )
    )

    assert result["id"] == "sub_matching"
    assert len(calls) == 2


@pytest.mark.p2
def test_create_subscription_duplicate_candidates_use_payment_error_contract(billing_app, monkeypatch):
    monkeypatch.setattr(billing_app, "get_json_result", lambda **kwargs: kwargs)
    monkeypatch.setattr(billing_app.settings, "BILLING", {"billing_plans": []})

    async def retrieve_customer(_customer_id):
        return {"invoice_settings": {"default_payment_method": "pm_test"}}

    async def find_existing(**_kwargs):
        raise ValueError("duplicate")

    monkeypatch.setattr(billing_app.stripe.Customer, "retrieve_async", retrieve_customer)
    monkeypatch.setattr(billing_app, "_find_existing_target_subscription_async", find_existing)

    result = asyncio.run(
        billing_app._create_subscription_checkout(
            tenant_id="tenant_1",
            customer_id="cus_1",
            subscription_price_id="price_starter",
            quantity=1,
            session_success_url="https://example.com/success",
            session_cancel_url="https://example.com/cancel",
        )
    )

    assert result["code"] == billing_app.RetCode.BILLING_PAYMENT_FAILED
    assert result["data"] == {"error_type": "duplicate_subscription"}


@pytest.mark.p2
def test_create_subscription_uses_default_incomplete_and_idempotency(billing_app, monkeypatch):
    created = {}
    monkeypatch.setattr(billing_app, "get_trial_price_id", lambda _plans: "price_trial")
    monkeypatch.setattr(billing_app, "get_json_result", lambda **kwargs: kwargs)
    monkeypatch.setattr(
        billing_app.settings,
        "BILLING_PRICEID_TO_PRODUCT",
        {"price_starter": "Starter"},
    )
    monkeypatch.setattr(billing_app.settings, "BILLING", {"billing_plans": []})

    async def find_existing(**_kwargs):
        return None

    async def create_session(**kwargs):
        created.update(kwargs)
        return {
            "id": "cs_new",
            "url": "https://checkout.stripe.com/pay/cs_new",
        }

    monkeypatch.setattr(billing_app, "_find_existing_target_subscription_async", find_existing)
    monkeypatch.setattr(billing_app.stripe.checkout.Session, "create_async", create_session)

    result = asyncio.run(
        billing_app._create_subscription_checkout(
            tenant_id="tenant_1",
            customer_id="cus_1",
            subscription_price_id="price_starter",
            quantity=1,
            session_success_url="https://example.com/success",
            session_cancel_url="https://example.com/cancel",
        )
    )

    assert created["mode"] == "subscription"
    assert created["customer"] == "cus_1"
    assert created["line_items"] == [{"price": "price_starter", "quantity": 1}]
    assert created["metadata"]["tenant_id"] == "tenant_1"
    assert created["subscription_data"]["metadata"]["price_id"] == "price_starter"
    assert "session_id=" in created["success_url"]
    assert created["cancel_url"] == "https://example.com/cancel"
    assert result["data"]["redirect_to"] == "https://checkout.stripe.com/pay/cs_new"
    assert result["data"]["session_id"] == "cs_new"
    assert result["data"]["product_type"] == "subscription"


@pytest.mark.p2
def test_create_subscription_reuses_existing_incomplete(billing_app, monkeypatch):
    existing = {
        "id": "sub_existing",
        "status": "incomplete",
        "latest_invoice": {
            "id": "in_existing",
            "status": "open",
            "amount_due": 12900,
            "currency": "usd",
            "hosted_invoice_url": "https://invoice.stripe.com/existing",
            "payment_intent": {"status": "requires_action"},
        },
    }
    monkeypatch.setattr(billing_app, "get_trial_price_id", lambda _plans: "price_trial")
    monkeypatch.setattr(billing_app, "get_json_result", lambda **kwargs: kwargs)
    monkeypatch.setattr(billing_app.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_starter": "Starter"})
    monkeypatch.setattr(billing_app.settings, "BILLING", {"billing_plans": []})

    async def retrieve_customer(_customer_id):
        return {"invoice_settings": {"default_payment_method": ""}}

    async def find_existing(**_kwargs):
        return existing

    async def fail_create(**_kwargs):
        raise AssertionError("must not create a second subscription")

    monkeypatch.setattr(billing_app.stripe.Customer, "retrieve_async", retrieve_customer)
    monkeypatch.setattr(billing_app, "_find_existing_target_subscription_async", find_existing)
    monkeypatch.setattr(billing_app.stripe.Subscription, "create_async", fail_create)

    result = asyncio.run(
        billing_app._create_subscription_checkout(
            tenant_id="tenant_1",
            customer_id="cus_1",
            subscription_price_id="price_starter",
            quantity=1,
            session_success_url="https://example.com/success",
            session_cancel_url="https://example.com/cancel",
        )
    )

    assert result["data"]["subscription_id"] == "sub_existing"
    assert result["data"]["payment_state"] == "requires_action"


@pytest.mark.p2
def test_create_subscription_reuses_existing_incomplete_without_new_session(billing_app, monkeypatch):
    existing = {
        "id": "sub_existing",
        "status": "incomplete",
        "latest_invoice": {"id": "in_existing"},
    }
    monkeypatch.setattr(billing_app, "get_trial_price_id", lambda _plans: "price_trial")
    monkeypatch.setattr(billing_app, "get_json_result", lambda **kwargs: kwargs)
    monkeypatch.setattr(billing_app.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_starter": "Starter"})
    monkeypatch.setattr(billing_app.settings, "BILLING", {"billing_plans": []})

    async def find_existing(**_kwargs):
        return existing

    async def fail_create_session(**_kwargs):
        raise AssertionError("must not create a new checkout session when an existing subscription matches")

    monkeypatch.setattr(billing_app, "_find_existing_target_subscription_async", find_existing)
    monkeypatch.setattr(billing_app.stripe.checkout.Session, "create_async", fail_create_session)

    result = asyncio.run(
        billing_app._create_subscription_checkout(
            tenant_id="tenant_1",
            customer_id="cus_1",
            subscription_price_id="price_starter",
            quantity=1,
            session_success_url="https://example.com/success",
            session_cancel_url="https://example.com/cancel",
        )
    )

    assert result["data"]["subscription_id"] == "sub_existing"
    assert result["data"]["price_id"] == "price_starter"
    assert result["data"]["product_type"] == "subscription"


@pytest.mark.p2
def test_subscription_checkout_dispatches_matching_incomplete_to_recovery(billing_app, monkeypatch):
    stripe_subscription = {
        "id": "sub_incomplete",
        "status": "incomplete",
        "metadata": {"tenant_id": "tenant_1"},
        "items": {"data": [{"id": "si_1", "price": {"id": "price_starter"}, "quantity": 1}]},
    }
    recovered = {"recovered": True}

    async def retrieve_subscription(*_args, **_kwargs):
        return stripe_subscription

    async def create_subscription(**kwargs):
        assert kwargs["tenant_id"] == "tenant_1"
        assert kwargs["subscription_price_id"] == "price_starter"
        assert kwargs["setup_intent_id"] == "seti_retry"
        return recovered

    monkeypatch.setattr(billing_app.stripe.Subscription, "retrieve_async", retrieve_subscription)
    monkeypatch.setattr(billing_app, "_create_subscription_checkout", create_subscription)

    result = asyncio.run(
        billing_app._handle_subscription_checkout(
            tenant_id="tenant_1",
            tenant_plan={
                "subscription_id": "sub_incomplete",
                "subscription_status": "active",
            },
            customer_id="cus_1",
            subscription_price_id="price_starter",
            quantity=1,
            setup_intent_id="seti_retry",
            session_success_url="https://app.example/success",
            session_cancel_url="https://app.example/cancel",
        )
    )

    assert result is recovered


@pytest.mark.p2
def test_recoverable_checkout_returns_canonical_redirect_contract(billing_app, monkeypatch):
    monkeypatch.setattr(billing_app, "get_json_result", lambda **kwargs: kwargs)

    result = asyncio.run(
        billing_app._handle_recoverable_subscription_checkout(
            tenant_id="tenant_1",
            tenant_plan={"invoice_url": "https://invoice.stripe.com/unpaid"},
            subscription_id="sub_unpaid",
            subscription_status="past_due",
            subscription_price_id="price_other",
        )
    )

    assert result["data"]["payment_state"] == "requires_action"
    assert result["data"]["redirect_to"] == "https://invoice.stripe.com/unpaid"


@pytest.mark.p2
def test_direct_payment_status_requires_customer_ownership(billing_app, monkeypatch):
    monkeypatch.setattr(billing_app.settings, "BILLING_ENABLED", True)
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_raw_by_tenant_id",
        lambda _tenant_id: {"customer_id": "cus_owner"},
    )

    async def retrieve_subscription(_subscription_id, **_kwargs):
        return {
            "id": "sub_other",
            "customer": "cus_other",
            "status": "incomplete",
            "metadata": {"tenant_id": "other-tenant"},
        }

    monkeypatch.setattr(billing_app.stripe.Subscription, "retrieve_async", retrieve_subscription)
    app = Quart(__name__)

    async def run_request():
        async with app.test_request_context("/billing/subscriptions/sub_other/payment-status"):
            return await billing_app.billing_direct_subscription_payment_status("sub_other")

    result = asyncio.run(run_request())

    assert result["code"] == billing_app.RetCode.FORBIDDEN
    assert result["data"] is False


@pytest.mark.p2
def test_direct_payment_status_waits_for_local_entitlement(billing_app, monkeypatch):
    monkeypatch.setattr(billing_app.settings, "BILLING_ENABLED", True)
    local_plan = {
        "customer_id": "cus_owner",
        "subscription_id": "sub_paid",
        "subscription_status": "incomplete",
        "price_id": "price_trial",
    }
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_raw_by_tenant_id",
        lambda _tenant_id: local_plan,
    )
    monkeypatch.setattr(billing_app.settings, "BILLING_PRICEID_TO_PRODUCT", {"price_starter": "Starter"})
    subscription = {
        "id": "sub_paid",
        "customer": {"id": "cus_owner"},
        "status": "active",
        "metadata": {"tenant_id": "test-user"},
        "items": {"data": [{"id": "si_1", "price": {"id": "price_starter"}, "quantity": 1}]},
        "latest_invoice": {
            "id": "in_paid",
            "customer": {"id": "cus_owner"},
            "subscription": {"id": "sub_paid"},
            "status": "paid",
            "amount_due": 0,
            "payment_intent": {"status": "succeeded"},
        },
    }

    async def retrieve_subscription(_subscription_id, **_kwargs):
        return subscription

    monkeypatch.setattr(billing_app.stripe.Subscription, "retrieve_async", retrieve_subscription)
    app = Quart(__name__)

    async def run_request():
        async with app.test_request_context("/billing/subscriptions/sub_paid/payment-status"):
            return await billing_app.billing_direct_subscription_payment_status("sub_paid")

    before = asyncio.run(run_request())
    local_plan.update(subscription_status="active", price_id="price_starter")
    after = asyncio.run(run_request())

    assert before["data"]["payment_state"] == "paid"
    assert before["data"]["entitlement_ready"] is False
    assert after["data"]["entitlement_ready"] is True


@pytest.mark.p2
@pytest.mark.parametrize(
    ("subscription_status", "invoice_status", "intent_status", "expected_state"),
    [
        ("incomplete", "open", "requires_action", "requires_action"),
        ("incomplete", "open", "processing", "pending"),
        ("incomplete_expired", "open", "canceled", "failed"),
    ],
)
def test_direct_payment_status_returns_canonical_unpaid_states(
    billing_app,
    monkeypatch,
    subscription_status,
    invoice_status,
    intent_status,
    expected_state,
):
    monkeypatch.setattr(billing_app.settings, "BILLING_ENABLED", True)
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_raw_by_tenant_id",
        lambda _tenant_id: {
            "customer_id": "cus_owner",
            "subscription_id": "sub_payment",
            "subscription_status": "active",
            "price_id": "price_trial",
        },
    )
    subscription = {
        "id": "sub_payment",
        "customer": "cus_owner",
        "status": subscription_status,
        "metadata": {"tenant_id": "test-user"},
        "items": {"data": [{"id": "si_1", "price": {"id": "price_starter"}, "quantity": 1}]},
        "latest_invoice": {
            "id": "in_payment",
            "status": invoice_status,
            "amount_due": 12900,
            "hosted_invoice_url": "https://invoice.stripe.com/payment",
            "payment_intent": {"status": intent_status},
        },
    }

    async def retrieve_subscription(_subscription_id, **_kwargs):
        return subscription

    monkeypatch.setattr(billing_app.stripe.Subscription, "retrieve_async", retrieve_subscription)
    app = Quart(__name__)

    async def run_request():
        async with app.test_request_context("/billing/subscriptions/sub_payment/payment-status"):
            return await billing_app.billing_direct_subscription_payment_status("sub_payment")

    result = asyncio.run(run_request())

    assert result["data"]["payment_state"] == expected_state
    assert result["data"]["entitlement_ready"] is False


@pytest.mark.p2
def test_direct_payment_status_rejects_invoice_from_another_subscription(billing_app, monkeypatch):
    monkeypatch.setattr(billing_app.settings, "BILLING_ENABLED", True)
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_raw_by_tenant_id",
        lambda _tenant_id: {"customer_id": "cus_owner"},
    )

    async def retrieve_subscription(_subscription_id, **_kwargs):
        return {
            "id": "sub_owner",
            "customer": "cus_owner",
            "status": "incomplete",
            "metadata": {"tenant_id": "test-user"},
        }

    async def retrieve_invoice(_invoice_id, **_kwargs):
        return {
            "id": "in_other",
            "customer": "cus_owner",
            "subscription": "sub_other",
        }

    monkeypatch.setattr(billing_app.stripe.Subscription, "retrieve_async", retrieve_subscription)
    monkeypatch.setattr(billing_app.stripe.Invoice, "retrieve_async", retrieve_invoice)
    app = Quart(__name__)

    async def run_request():
        async with app.test_request_context("/billing/subscriptions/sub_owner/payment-status?invoice_id=in_other"):
            return await billing_app.billing_direct_subscription_payment_status("sub_owner")

    result = asyncio.run(run_request())

    assert result["code"] == billing_app.RetCode.FORBIDDEN
    assert result["data"] is False


@pytest.mark.p2
def test_direct_payment_status_sanitizes_stripe_errors(billing_app, monkeypatch):
    monkeypatch.setattr(billing_app.settings, "BILLING_ENABLED", True)
    monkeypatch.setattr(
        billing_app.SubscriptionService,
        "get_raw_by_tenant_id",
        lambda _tenant_id: {"customer_id": "cus_owner"},
    )

    async def retrieve_subscription(_subscription_id, **_kwargs):
        raise billing_app.stripe.CardError(
            message="raw provider failure",
            param=None,
            code="card_declined",
            json_body={"error": {"decline_code": "do_not_honor"}},
        )

    monkeypatch.setattr(billing_app.stripe.Subscription, "retrieve_async", retrieve_subscription)
    app = Quart(__name__)

    async def run_request():
        async with app.test_request_context("/billing/subscriptions/sub_failed/payment-status"):
            return await billing_app.billing_direct_subscription_payment_status("sub_failed")

    result = asyncio.run(run_request())
    serialized = json.dumps(result)

    assert result["code"] == billing_app.RetCode.BILLING_PAYMENT_FAILED
    assert result["data"] == {"error_type": "payment_declined"}
    assert "raw provider failure" not in serialized
    assert "do_not_honor" not in serialized


@pytest.fixture(autouse=True)
def _stub_billing_db_atomic(monkeypatch, billing_app):
    monkeypatch.setattr(billing_app, "get_json_result", lambda **kwargs: kwargs)
    monkeypatch.setattr(billing_app.DB, "atomic", lambda: nullcontext())
    monkeypatch.setattr(billing_webhook_service.DB, "atomic", lambda: nullcontext())


def _build_event(event_type="invoice.paid", event_id="evt_test", created=1710000000):
    return {
        "id": event_id,
        "type": event_type,
        "created": created,
        "data": {
            "object": {
                "id": "obj_123",
                "object": "invoice",
            }
        },
    }


@pytest.mark.p2
def test_handle_event_skips_duplicate_completed_event_and_updates_checkpoint(monkeypatch):
    event = _build_event()
    checkpoint_updates = []
    handler_calls = []

    monkeypatch.setattr(
        billing_webhook_service.BillingWebhookEventService,
        "save",
        lambda **_kwargs: (_ for _ in ()).throw(IntegrityError()),
    )
    monkeypatch.setattr(
        billing_webhook_service.BillingWebhookEventService,
        "get_by_event_id",
        lambda _event_id: {"processing_status": billing_webhook_service.WEBHOOK_EVENT_STATUS_COMPLETED},
    )
    monkeypatch.setattr(
        billing_webhook_service,
        "_update_webhook_event_checkpoint",
        lambda ts, event_id=None: checkpoint_updates.append(ts),
    )
    monkeypatch.setattr(
        billing_webhook_service,
        "_handle_invoice_paid",
        lambda _event: asyncio.sleep(0, result=handler_calls.append("called")),
    )

    asyncio.run(billing_webhook_service.handle_billing_webhook_event(event))

    assert handler_calls == []
    assert checkpoint_updates == [event["created"]]


@pytest.mark.p2
def test_handle_event_retries_failed_duplicate_and_marks_completed(monkeypatch):
    event = _build_event()
    checkpoint_updates = []
    handler_calls = []
    processing_marks = []
    completed_marks = []

    monkeypatch.setattr(
        billing_webhook_service.BillingWebhookEventService,
        "save",
        lambda **_kwargs: (_ for _ in ()).throw(IntegrityError()),
    )
    monkeypatch.setattr(
        billing_webhook_service.BillingWebhookEventService,
        "get_by_event_id",
        lambda _event_id: {"processing_status": billing_webhook_service.WEBHOOK_EVENT_STATUS_FAILED},
    )
    monkeypatch.setattr(
        billing_webhook_service.BillingWebhookEventService,
        "mark_processing",
        lambda event_id: processing_marks.append(event_id),
    )
    monkeypatch.setattr(
        billing_webhook_service.BillingWebhookEventService,
        "mark_completed",
        lambda event_id, status=billing_webhook_service.WEBHOOK_EVENT_STATUS_COMPLETED: completed_marks.append((event_id, status)),
    )
    monkeypatch.setattr(
        billing_webhook_service,
        "_update_webhook_event_checkpoint",
        lambda ts, event_id=None: checkpoint_updates.append(ts),
    )
    monkeypatch.setattr(
        billing_webhook_service,
        "_handle_invoice_paid",
        lambda _event: asyncio.sleep(0, result=handler_calls.append("called")),
    )

    asyncio.run(billing_webhook_service.handle_billing_webhook_event(event))

    assert handler_calls == ["called"]
    assert processing_marks == [event["id"]]
    assert completed_marks == [(event["id"], billing_webhook_service.WEBHOOK_EVENT_STATUS_COMPLETED)]
    assert checkpoint_updates == [event["created"]]


@pytest.mark.p2
def test_handle_event_marks_failed_and_reraises_on_handler_error(monkeypatch):
    event = _build_event()
    failed_marks = []
    checkpoint_updates = []

    monkeypatch.setattr(billing_webhook_service.BillingWebhookEventService, "save", lambda **_kwargs: None)
    monkeypatch.setattr(
        billing_webhook_service.BillingWebhookEventService,
        "mark_failed",
        lambda event_id, error_message: failed_marks.append((event_id, error_message)),
    )
    monkeypatch.setattr(
        billing_webhook_service,
        "_update_webhook_event_checkpoint",
        lambda ts, event_id=None: checkpoint_updates.append(ts),
    )

    async def _raise_handler(_event):
        raise ValueError("boom")

    monkeypatch.setattr(billing_webhook_service, "_handle_invoice_paid", _raise_handler)

    with pytest.raises(ValueError, match="boom"):
        asyncio.run(billing_webhook_service.handle_billing_webhook_event(event))

    assert failed_marks == [(event["id"], "boom")]
    assert checkpoint_updates == []


@pytest.mark.p2
def test_handle_event_marks_unhandled_events_and_updates_checkpoint(monkeypatch):
    event = _build_event(event_type="customer.created")
    checkpoint_updates = []
    completed_marks = []

    monkeypatch.setattr(billing_webhook_service.BillingWebhookEventService, "save", lambda **_kwargs: None)
    monkeypatch.setattr(
        billing_webhook_service.BillingWebhookEventService,
        "mark_completed",
        lambda event_id, status=billing_webhook_service.WEBHOOK_EVENT_STATUS_COMPLETED: completed_marks.append((event_id, status)),
    )
    monkeypatch.setattr(
        billing_webhook_service,
        "_update_webhook_event_checkpoint",
        lambda ts, event_id=None: checkpoint_updates.append(ts),
    )

    asyncio.run(billing_webhook_service.handle_billing_webhook_event(event))

    assert completed_marks == [(event["id"], billing_webhook_service.WEBHOOK_EVENT_STATUS_UNHANDLED)]
    assert checkpoint_updates == [event["created"]]


@pytest.mark.p2
def test_update_webhook_event_checkpoint_is_monotonic(monkeypatch):
    writes = {}

    def get_singleton_by_exact_name(name):
        value = writes.get(name)
        if value is None:
            return None
        return SimpleNamespace(value=value)

    def upsert_singleton_by_exact_name(*, name, source, data_type, value):
        writes[name] = value

    def delete_by_exact_name(name):
        writes.pop(name, None)

    mock_ss_class = type(
        "MockSystemSettingsService",
        (),
        {
            "get_singleton_by_exact_name": get_singleton_by_exact_name,
            "upsert_singleton_by_exact_name": upsert_singleton_by_exact_name,
            "delete_by_exact_name": delete_by_exact_name,
        },
    )
    monkeypatch.setattr(billing_webhook_service, "SystemSettingsService", mock_ss_class)

    billing_webhook_service._update_webhook_event_checkpoint(200, event_id="evt_new")
    billing_webhook_service._update_webhook_event_checkpoint(150, event_id="evt_old")

    assert json.loads(writes["billing_webhook_event_checkpoint"]) == {
        "created": 200,
        "id": "evt_new",
    }


@pytest.mark.p2
def test_handle_undelivered_events_filters_types_and_uses_ending_before_checkpoint(monkeypatch, billing_app):
    captured = {}
    processed = []

    class _Events:
        def auto_paging_iter(self):
            yield {"id": "evt_a", "type": "customer.subscription.updated", "created": 1710000000, "data": {"object": {"id": "sub_a", "object": "subscription"}}}
            yield {"id": "evt_b", "type": "invoice.paid", "created": 1710000001, "data": {"object": {"id": "in_b", "object": "invoice"}}}

    def list_events(**kwargs):
        captured.update(kwargs)
        return _Events()

    async def fake_handle(event, retry_inflight=False):
        processed.append((event["id"], retry_inflight))

    class _MockSystemSettingsService:
        @staticmethod
        def get_singleton_by_exact_name(name):
            if name == "billing_webhook_event_checkpoint":
                return SimpleNamespace(value='{"created":1710000100,"id":"evt_checkpoint"}')
            return None

    monkeypatch.setattr(billing_webhook_service, "SystemSettingsService", _MockSystemSettingsService)
    monkeypatch.setattr(billing_app.settings, "BILLING", {"stripe_api_key": "sk_test"}, raising=False)
    monkeypatch.setattr(billing_app.stripe.Event, "list", list_events)
    billing_webhook_service.stripe.Event.retrieve = lambda id: SimpleNamespace(id=id)
    monkeypatch.setattr(
        importlib.import_module("api.services.billing_webhook_service"),
        "handle_billing_webhook_event",
        fake_handle,
    )

    billing_app.handle_undelivered_events()

    assert captured["delivery_success"] is False
    assert captured["created"] == {"gte": 1709999500}
    assert captured["ending_before"] == "evt_checkpoint"
    assert set(captured["types"]) == {
        "invoice.paid",
        "invoice.payment_failed",
        "invoice.payment_action_required",
        "customer.subscription.updated",
        "customer.subscription.deleted",
        "customer.subscription.created",
        "checkout.session.completed",
        "payment_intent.succeeded",
    }
    assert processed == [("evt_a", True), ("evt_b", True)]
