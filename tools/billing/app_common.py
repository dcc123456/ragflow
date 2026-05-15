#!/usr/bin/env python3
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
"""
Common utilities for APP test flows (APP-01, APP-02, etc.).

Provides:
- AppClient: Shared client class for dataset/chat/agent/canvas operations
- setup_app_test: Common initialization (Stripe config, client creation, quota verification)
- print_app_summary: Standardized JSON test summary output
"""

from __future__ import annotations

import json
import uuid

import stripe

from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    get_trial_quota_apps,
    get_starter_quota_apps,
)
from tools.billing.billing_client import create_client_with_type, BillingClient
from tools.billing.member_common import load_member_runtime_config


class AppClient(BillingClient):
    """Client for testing app quota enforcement."""

    def create_dataset(self, name: str, description: str = "") -> dict:
        """Create a dataset via REST API."""
        payload = {
            "name": name,
            "description": description,
        }
        return self.request_json("POST", "datasets", need_api_path=True, json=payload)

    def delete_dataset(self, dataset_id: str) -> dict:
        """Delete a dataset via REST API."""
        return self.request_json("DELETE", "datasets", need_api_path=True, json={"ids": [dataset_id]})

    def create_chat(self, name: str, dataset_ids: list = None) -> dict:
        """Create a chat via SDK API."""
        payload = {
            "name": name,
            "description": "Test chat",
            "dataset_ids": dataset_ids or [],
        }
        return self.request_json("POST", "chats", need_api_path=True, json=payload)

    def create_agent(self, title: str, dsl: dict = None) -> dict:
        """Create an agent via SDK API."""
        if dsl is None:
            dsl = {
                "components": {
                    "begin": {
                        "id": "begin",
                        "obj": {
                            "component_name": "Begin",
                            "params": {},
                        },
                    }
                },
                "history": [],
                "path": [],
                "messages": [],
            }
        payload = {
            "title": title,
            "dsl": dsl,
            "canvas_category": "agent",
        }
        return self.request_json("POST", "agents", need_api_path=True, json=payload)

    def create_canvas(self, title: str, dsl: dict = None) -> dict:
        """Create a canvas via web API."""
        if dsl is None:
            dsl = {
                "components": {
                    "begin": {
                        "id": "begin",
                        "obj": {
                            "component_name": "Begin",
                            "params": {},
                        },
                    }
                },
                "history": [],
                "path": [],
                "messages": [],
            }
        payload = {
            "title": title,
            "dsl": dsl,
            "canvas_category": "agent",
        }
        return self.request_json("POST", "canvas/set", json=payload)

    def get_apps_quota_overview(self) -> dict:
        """Get apps quota overview from billing plan overview."""
        overview = self.plan_overview()
        resources = overview.get("resources", {})
        return resources.get("apps", {})


def setup_app_test(
    args,
    case_name: str,
    client_type: type = AppClient,
    initial_plan: str = "Trial",
) -> tuple[AppClient, str, int, int]:
    """Common setup for APP test flows.

    Args:
        args: Command line arguments.
        case_name: Test case name (e.g., "billing-app01").
        client_type: Client class to instantiate (default: AppClient).
        initial_plan: Expected initial plan name for quota verification.

    Returns:
        Tuple of (client, email, trial_quota_apps, starter_quota_apps).
    """
    print("\n" + "=" * 80)
    print(f"Testing {case_name}")
    print("=" * 80)

    # Load runtime configuration
    config = load_member_runtime_config()
    stripe.api_key = config["stripe_api_key"]
    stripe.api_version = config["stripe_api_version"]
    print("  Assert: Runtime config loaded successfully")

    # Get expected quotas
    trial_quota_apps = get_trial_quota_apps()
    starter_quota_apps = get_starter_quota_apps()
    print(f"  Assert: Expected quota_apps for Trial: {trial_quota_apps}")
    print(f"  Assert: Expected quota_apps for Starter: {starter_quota_apps}")

    # Create client
    email = f"{case_name}-{uuid.uuid4().hex[:12]}@example.test"
    client: AppClient = create_client_with_type(args, email, client_type)

    print(f"  Assert: Tenant ID: {client.tenant_id}")
    print(f"  Assert: User ID: {client.user_id}")
    print(f"  Assert: Customer ID: {client.customer_id}")

    # Verify initial plan quota
    apps_quota = client.get_apps_quota_overview()
    expected_limit = trial_quota_apps if initial_plan == "Trial" else starter_quota_apps
    if apps_quota.get("limit") != expected_limit:
        raise FlowError(f"Expected {initial_plan} apps quota {expected_limit}, got {apps_quota.get('limit')}")
    print(f"  Assert: Apps quota from overview ({initial_plan}): {apps_quota.get('used')}/{apps_quota.get('limit')}")

    return client, email, trial_quota_apps, starter_quota_apps


def print_app_summary(case: str, description: str, client: AppClient, email: str, extra: dict = None) -> None:
    """Print standardized JSON test summary for APP test flows.

    Args:
        case: Test case identifier (e.g., "APP-01").
        description: Human-readable test description.
        client: The AppClient instance.
        email: Test user email.
        extra: Additional fields to include in the summary.
    """
    summary = {
        "case": case,
        "description": description,
        "tenant_id": client.tenant_id,
        "email": email,
        "test_clock_id": client.clock_id,
        "customer_id": client.customer_id,
        "webhook_mode": getattr(client, "mode", None),
        "status": "PASSED",
    }
    if extra:
        summary.update(extra)

    print("\n" + "=" * 80)
    print(f"{case} Test Summary")
    print("=" * 80)
    print(json.dumps(summary, indent=2, sort_keys=True))
