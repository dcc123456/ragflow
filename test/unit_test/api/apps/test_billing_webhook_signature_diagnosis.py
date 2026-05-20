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
import importlib.util
import sys
import types
from pathlib import Path
from types import SimpleNamespace

import pytest
from quart import Blueprint


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


@pytest.mark.p2
def test_diagnose_unverified_stripe_event_reports_missing_event(monkeypatch):
    def _raise(event_id):
        raise billing_app.stripe.InvalidRequestError(
            message=f"No such event: {event_id}",
            param="id",
            code="resource_missing",
        )

    monkeypatch.setattr(billing_app.stripe.Event, "retrieve", _raise)

    result = billing_app._diagnose_unverified_stripe_event("evt_missing")

    assert result == {
        "checked": True,
        "exists_in_configured_account": False,
        "reason": "event_not_found",
        "error_code": "resource_missing",
    }


@pytest.mark.p2
def test_diagnose_unverified_stripe_event_reports_existing_event(monkeypatch):
    monkeypatch.setattr(
        billing_app.stripe.Event,
        "retrieve",
        lambda event_id: SimpleNamespace(id=event_id, type="invoice.paid", livemode=False),
    )

    result = billing_app._diagnose_unverified_stripe_event("evt_present")

    assert result == {
        "checked": True,
        "exists_in_configured_account": True,
        "event_id": "evt_present",
        "event_type": "invoice.paid",
        "livemode": False,
    }


@pytest.mark.p2
def test_diagnose_unverified_stripe_event_reports_lookup_failure(monkeypatch):
    monkeypatch.setattr(
        billing_app.stripe.Event,
        "retrieve",
        lambda _event_id: (_ for _ in ()).throw(RuntimeError("stripe timeout")),
    )

    result = billing_app._diagnose_unverified_stripe_event("evt_broken")

    assert result == {
        "checked": True,
        "exists_in_configured_account": None,
        "reason": "stripe_lookup_failed",
        "error_type": "RuntimeError",
        "error": "stripe timeout",
    }
