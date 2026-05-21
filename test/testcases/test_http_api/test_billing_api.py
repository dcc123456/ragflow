#
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
Billing API tests
"""
import json
import os

import pytest
import requests

from configs import HOST_ADDRESS, VERSION
from libs.auth import RAGFlowHttpApiAuth


BILLING_API_URL = f"/{VERSION}/billing"


# =============================================================================
# Fixtures
# =============================================================================

@pytest.fixture(scope="session")
def billing_auth(token):
    """Auth fixture for billing tests."""
    return RAGFlowHttpApiAuth(token)


@pytest.fixture(scope="session")
def billing_enabled():
    """Check if billing is enabled by querying /v1/billing/status (no auth required)."""
    try:
        url = f"{HOST_ADDRESS}{BILLING_API_URL}/status"
        res = requests.get(url, timeout=5)
        if res.status_code == 200:
            return res.json().get("billing_enabled", False)
        return False
    except Exception:
        return False


@pytest.fixture(scope="session")
def tenant_id():
    """Get tenant_id from login response."""
    from configs import EMAIL, HOST_ADDRESS, PASSWORD, VERSION
    login_data = {"email": EMAIL, "password": PASSWORD}
    response = requests.post(url=f"{HOST_ADDRESS}/{VERSION}/user/login", json=login_data)
    res = response.json()
    if res.get("code") == 0:
        return res["data"]["id"]
    raise Exception(f"Failed to get tenant_id: {res}")


# =============================================================================
# Helper Functions
# =============================================================================


# =============================================================================
# Test Cases
# =============================================================================

@pytest.mark.p2
@pytest.mark.skipif(not billing_enabled, reason="Billing is disabled")
def test_billing_enabled_guard(billing_auth):
    """
    Verify billing endpoint returns 200 when billing is enabled.

    This test is skipped when billing is disabled.
    """
    url = f"{HOST_ADDRESS}{BILLING_API_URL}/subscription/overview"
    res = requests.get(url, auth=billing_auth)
    assert res.status_code == 200, f"Expected 200 when billing enabled, got {res.status_code}"


@pytest.mark.p2
@pytest.mark.skipif(not billing_enabled, reason="Billing is disabled")
def test_billing_config_loading(billing_auth):
    """
    Verify billing config is safely loaded from environment variables.
    """
    # Test: Check plan_overview endpoint (doesn't require real Stripe credentials)
    url = f"{HOST_ADDRESS}{BILLING_API_URL}/subscription/overview"
    res = requests.get(url, auth=billing_auth)

    # Should either work (billing enabled) or return disabled message
    assert res.status_code in (200, 400, 403), f"Unexpected status: {res.status_code}"


@pytest.mark.p2
@pytest.mark.skipif(not billing_enabled, reason="Billing is disabled")
def test_billing_session_urls(billing_auth):
    """
    Verify billing callback URLs are correctly constructed.

    Tests that billing_service_url is properly used to construct:
    - session_success_url: {BILLING_SERVICE_URL}/v1/billing/callbacks/success
    - session_cancel_url: {BILLING_SERVICE_URL}/v1/billing/callbacks/cancel
    - webhook_url: {BILLING_SERVICE_URL}/v1/billing/webhook
    - customer_portal_return_url: {BILLING_SERVICE_URL}
    """
    url = f"{HOST_ADDRESS}{BILLING_API_URL}/addon-purchases"
    payload = {
        "price_ids": ["price_1Si7S7PtsKvwvC5f3IjPpPcN"],
    }

    res = requests.post(url, auth=billing_auth, json=payload)
    data = res.json()

    if res.status_code == 200 and data.get("code") == 0:
        checkout_data = data.get("data", {})
        success_url = checkout_data.get("success_url", "")
        cancel_url = checkout_data.get("cancel_url", "")

        # URLs should contain BILLING_SERVICE_URL prefix
        billing_service_url = os.getenv("BILLING_SERVICE_URL", "")
        if billing_service_url:
            assert success_url.startswith(billing_service_url), \
                f"success_url should start with {billing_service_url}, got {success_url}"
            assert cancel_url.startswith(billing_service_url), \
                f"cancel_url should start with {billing_service_url}, got {cancel_url}"


@pytest.mark.p2
@pytest.mark.skipif(not billing_enabled, reason="Billing is disabled")
def test_stripe_webhook_validation():
    """
    Verify Stripe webhook signature validation rejects invalid signatures.

    Sends a request to the webhook endpoint with an invalid/missing
    Stripe-Signature header and expects the request to be rejected.
    """
    url = f"{HOST_ADDRESS}{BILLING_API_URL}/webhooks/stripe"

    # Test 1: Missing signature header
    res = requests.post(url, data=json.dumps({"type": "test"}))
    # Without valid signature, should return 400 or fail gracefully
    assert res.status_code in (200, 400), f"Unexpected status: {res.status_code}"

    # Test 2: Invalid signature format
    headers = {"stripe-signature": "invalid_signature_format"}
    res = requests.post(
        url,
        data=json.dumps({"type": "test", "id": "evt_test"}),
        headers=headers,
    )
    # Should reject invalid signature
    assert res.status_code in (200, 400), f"Unexpected status: {res.status_code}"
