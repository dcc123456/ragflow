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
import logging
import uuid

from libs.billing.billing_common import (  # noqa: E402
    BillingClient,
    FlowError,
    create_client_with_type,
    get_trial_quota_apps,
    get_starter_quota_apps,
)

logger = logging.getLogger(__name__)


class AppClient(BillingClient):
    """Client for testing app quota enforcement."""

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.sdk_auth_header = ""

    def init_sdk_token(self) -> None:
        """Create a tenant API token and switch app operations to Bearer auth."""
        result = self.request_json("POST", "system/new_token")
        token = ((result.get("data") or {}).get("token") or "").strip()
        if not token:
            raise FlowError(f"system/new_token did not return token: {result}")
        self.sdk_auth_header = f"Bearer {token}"

    def sdk_headers(self) -> dict[str, str]:
        headers = self.headers()
        if not self.sdk_auth_header:
            raise FlowError("SDK auth header not initialized")
        headers["Authorization"] = self.sdk_auth_header
        return headers

    def sdk_request_json(self, method: str, path: str, **kwargs) -> dict:
        response = self.session.request(
            method,
            self.url(path, need_api_path=True),
            headers=self.sdk_headers(),
            timeout=60,
            **kwargs,
        )
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"{method} {path} returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"{method} {path} failed status={response.status_code}: {payload}")
        return payload

    def create_dataset(self, name: str, description: str = "") -> dict:
        """Create a dataset via the RESTful API using SDK Bearer auth."""
        payload = {
            "name": name,
            "description": description,
        }
        return self.sdk_request_json("POST", "datasets", json=payload)

    def delete_dataset(self, dataset_id: str) -> dict:
        """Delete a dataset via the RESTful API using SDK Bearer auth."""
        return self.sdk_request_json("DELETE", "datasets", json={"ids": [dataset_id]})

    def create_chat(self, name: str, dataset_ids: list | None = None) -> dict:
        """Create a chat via the SDK API."""
        payload = {
            "name": name,
            "description": "Test chat",
            "dataset_ids": dataset_ids or [],
        }
        return self.sdk_request_json("POST", "chats", json=payload)

    def create_agent(self, title: str, dsl: dict | None = None) -> dict:
        """Create an agent via the SDK API."""
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
        return self.sdk_request_json("POST", "agents", json=payload)

    def create_canvas(self, title: str, dsl: dict | None = None) -> dict:
        """Create a canvas via the web canvas API using SDK Bearer auth."""
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
        response = self.session.request(
            "POST",
            self.url("canvas/set"),
            headers=self.sdk_headers(),
            timeout=60,
            json=payload,
        )
        try:
            result = response.json()
        except ValueError as exc:
            raise FlowError(
                f"POST canvas/set returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or result.get("code") not in (0, None):
            raise FlowError(f"POST canvas/set failed status={response.status_code}: {result}")
        return result

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
    logger.info("Testing %s", case_name)

    # Get expected quotas
    trial_quota_apps = get_trial_quota_apps()
    starter_quota_apps = get_starter_quota_apps()
    logger.info("Assert: Expected quota_apps for Trial: %s", trial_quota_apps)
    logger.info("Assert: Expected quota_apps for Starter: %s", starter_quota_apps)

    # Create client
    email = f"{case_name}-{uuid.uuid4().hex[:12]}@example.test"
    client: AppClient = create_client_with_type(args, email, client_type)
    client.init_sdk_token()

    logger.info("Assert: Tenant ID: %s", client.tenant_id)
    logger.info("Assert: User ID: %s", client.user_id)
    logger.info("Assert: Customer ID: %s", client.customer_id)

    # Verify initial plan quota
    apps_quota = client.get_apps_quota_overview()
    expected_limit = trial_quota_apps if initial_plan == "Trial" else starter_quota_apps
    if apps_quota.get("limit") != expected_limit:
        raise FlowError(f"Expected {initial_plan} apps quota {expected_limit}, got {apps_quota.get('limit')}")
    logger.info("Assert: Apps quota from overview (%s): %s/%s", initial_plan, apps_quota.get('used'), apps_quota.get('limit'))

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
        "status": "PASSED",
    }
    if extra:
        summary.update(extra)

    logger.info("%s Test Summary", case)
    logger.info(json.dumps(summary, indent=2, sort_keys=True))
