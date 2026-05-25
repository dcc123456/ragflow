#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
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
Pytest configuration and fixtures for billing tests.

Provides:
- billing_runtime_config: Stripe test runtime config from service_conf.yaml
- billing_enabled_or_skip: skips if billing not enabled
- billing_email_factory: generates unique test emails
- billing_client: BillingClient with test clock lifecycle
- points_client: PointsClient with test clock lifecycle
- cleanup_test_clock: finalizer for test clock cleanup
"""

from __future__ import annotations

import logging
import time
import uuid
from typing import Any, TypeVar

import pytest
import stripe

# Import billing helpers - these must already have print→logging converted
from libs.billing.billing_common import (
    BillingClient,
    FlowError,
    delete_clock,
    load_stripe_test_runtime_config,
    stripe_dict,
)
from libs.billing.points_common import PointsClient

logger = logging.getLogger(__name__)

T = TypeVar("T")


# -----------------------------------------------------------------------------
# Runtime config
# -----------------------------------------------------------------------------

@pytest.fixture(scope="session")
def billing_runtime_config() -> dict[str, Any]:
    """Load Stripe runtime config for billing test automation."""
    return load_stripe_test_runtime_config(
        require_test_mode_message="Billing P3 tests require a Stripe test-mode secret key"
    )


# -----------------------------------------------------------------------------
# Billing enabled check
# -----------------------------------------------------------------------------

@pytest.fixture(scope="session")
def billing_enabled_or_skip(billing_runtime_config: dict[str, Any]) -> None:
    """Skip if billing is not enabled in the test environment."""
    # billing_runtime_config is already loaded; if we get here, billing is configured
    # The actual skip would check /v1/billing/status but for P3 tests that need
    # Stripe test mode, we just ensure the config is present
    pass


@pytest.fixture(scope="session", autouse=True)
def set_tenant_info() -> None:
    """Override the global autouse tenant bootstrap for billing tests.

    Billing P3 cases provision their own tenants and Stripe customers, so they
    must not inherit the shared qa@infiniflow.org bootstrap from the parent
    test tree.
    """
    return None


# -----------------------------------------------------------------------------
# Email factory
# -----------------------------------------------------------------------------

@pytest.fixture
def billing_email_factory():
    """Generate unique test email addresses for billing flows."""
    def _make_email(prefix: str = "billing-p3") -> str:
        return f"{prefix}-{uuid.uuid4().hex[:12]}@example.test"
    return _make_email


# -----------------------------------------------------------------------------
# Test clock management
# -----------------------------------------------------------------------------

@pytest.fixture
def billing_test_args(billing_runtime_config: dict[str, Any]) -> Any:
    """Provide a namespace compatible with billing_common helpers."""
    # Import here to avoid circular deps at module level
    from argparse import Namespace
    from libs.billing.billing_common import default_base_url

    return Namespace(
        base_url=default_base_url(),
        version="v1",
        ready_timeout_seconds=60,
    )


def _configure_stripe(runtime_config: dict[str, Any]) -> None:
    """Configure Stripe API key and version from runtime config."""
    stripe.api_key = runtime_config["stripe_api_key"]
    stripe.api_version = runtime_config["stripe_api_version"]


def _create_test_clock() -> str:
    """Create a Stripe test clock and return its ID."""
    clock = stripe.test_helpers.TestClock.create(
        frozen_time=int(time.time()),
        name=f"p3-{uuid.uuid4().hex[:8]}",
    )
    # Wait for clock to be ready
    deadline = time.time() + 180
    while time.time() < deadline:
        clock_dict = stripe_dict(clock)
        if clock_dict.get("status") == "ready":
            logger.debug("test clock ready: %s", clock.id)
            return clock.id
        time.sleep(1)
    raise FlowError(f"test clock {clock.id} did not become ready")


# -----------------------------------------------------------------------------
# BillingClient fixture
# -----------------------------------------------------------------------------

class BillingClientWithClock:
    """Wrapper that manages BillingClient lifecycle with test clock cleanup."""

    def __init__(self, client: BillingClient, clock_id: str):
        self.client = client
        self.clock_id = clock_id
        self._clock_deleted = False

    def close(self) -> None:
        if self.clock_id and not self._clock_deleted:
            try:
                delete_clock(self.clock_id)
                self._clock_deleted = True
            except Exception as exc:
                logger.warning("Failed to delete test clock %s: %s", self.clock_id, exc)
        if self.client and self.client.session:
            self.client.session.close()


@pytest.fixture
def billing_client(
    billing_runtime_config: dict[str, Any],
    billing_test_args: Any,
    billing_email_factory,
) -> BillingClient:
    """Create a BillingClient with Stripe test clock lifecycle.

    The client is cleaned up via finalizer after each test.
    """
    _configure_stripe(billing_runtime_config)
    clock_id = _create_test_clock()

    email = billing_email_factory("billing-client")
    client = BillingClient(
        base_url=billing_test_args.base_url,
        version=billing_test_args.version,
        clock_id=clock_id,
        webhook_secret=billing_runtime_config["webhook_secret"],
    )
    client.wait_until_ready(billing_test_args.ready_timeout_seconds)

    # Bootstrap: register and login
    from libs.billing.storage_common import attach_default_test_card
    _user_id, tenant_id = client.register_and_login(email, "Test1234!")
    client.customer_id = str(client.customer_id or "")

    # Attach default test card
    initial_plan = client.current_plan()
    if not client.customer_id:
        client.customer_id = str(initial_plan.get("customer_id") or "")
    if client.customer_id:
        try:
            attach_default_test_card(client.customer_id)
        except Exception as exc:
            logger.warning("Failed to attach default test card: %s", exc)

    wrapper = BillingClientWithClock(client, clock_id)
    yield client

    wrapper.close()


# -----------------------------------------------------------------------------
# PointsClient fixture
# -----------------------------------------------------------------------------

@pytest.fixture
def points_client(
    billing_runtime_config: dict[str, Any],
    billing_test_args: Any,
    billing_email_factory,
) -> PointsClient:
    """Create a PointsClient with Stripe test clock lifecycle."""
    _configure_stripe(billing_runtime_config)
    clock_id = _create_test_clock()

    email = billing_email_factory("points-client")
    client = PointsClient(
        base_url=billing_test_args.base_url,
        version=billing_test_args.version,
        clock_id=clock_id,
        webhook_secret=billing_runtime_config["webhook_secret"],
    )
    client.wait_until_ready(billing_test_args.ready_timeout_seconds)

    # Bootstrap
    from libs.billing.storage_common import attach_default_test_card
    _user_id, tenant_id = client.register_and_login(email, "Test1234!")
    client.customer_id = str(client.customer_id or "")

    initial_plan = client.current_plan()
    if not client.customer_id:
        client.customer_id = str(initial_plan.get("customer_id") or "")
    if client.customer_id:
        try:
            attach_default_test_card(client.customer_id)
        except Exception as exc:
            logger.warning("Failed to attach default test card: %s", exc)

    wrapper = BillingClientWithClock(client, clock_id)
    yield client

    wrapper.close()
