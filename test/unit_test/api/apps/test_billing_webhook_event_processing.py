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
from quart import Blueprint


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
    if module_name in sys.modules:
        return sys.modules[module_name]

    project_root = Path(__file__).resolve().parents[4]
    apps_dir = project_root / "api" / "apps"
    billing_path = apps_dir / "billing_app.py"

    monkeypatch.setitem(sys.modules, "api.apps", _billing_app_stub())

    spec = importlib.util.spec_from_file_location(module_name, billing_path)
    module = importlib.util.module_from_spec(spec)
    module.manager = Blueprint("billing", module_name)
    sys.modules[module_name] = module
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


billing_webhook_service = importlib.import_module("api.services.billing_webhook_service")


@pytest.fixture(autouse=True)
def _stub_billing_db_atomic(monkeypatch, billing_app):
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
