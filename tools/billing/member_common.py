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
Member client utilities for billing test flows.

Provides MemberClient class extending RAGFlowClient with member/tenant management APIs.
"""
from __future__ import annotations

import sys
from pathlib import Path
from typing import Any

from api.db import UserTenantRole
from api.utils.crypt import crypt
from tools.billing.billing_common import (
    FlowError,
    env,
    load_billing_config,
    load_persisted_webhook_secret,
)
from tools.billing.billing_client import BillingClient

PROJECT_ROOT = Path(__file__).resolve().parents[2]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))


def load_member_runtime_config() -> dict[str, Any]:
    """Load runtime configuration for member test flows."""
    billing_config = load_billing_config()
    stripe_api_key = env("BILLING_STRIPE_API_KEY", env("STRIPE_API_KEY", str(billing_config.get("stripe_api_key") or "")))
    stripe_api_version = str(billing_config.get("stripe_api_version") or "2026-02-25.clover")
    stripe_api_version_override = env("STRIPE_API_VERSION")
    if stripe_api_version_override and stripe_api_version_override != stripe_api_version:
        raise FlowError(
            f"STRIPE_API_VERSION={stripe_api_version_override} does not match service_conf.yaml={stripe_api_version}"
        )
    if not stripe_api_key:
        raise FlowError("BILLING_STRIPE_API_KEY or STRIPE_API_KEY is required")
    if not stripe_api_key.startswith("sk_test_"):
        raise FlowError("Member automation requires a Stripe test-mode secret key")

    webhook_secret = env("BILLING_WEBHOOK_SECRET", env("STRIPE_WEBHOOK_SECRET"))
    if not webhook_secret:
        webhook_secret = load_persisted_webhook_secret()

    return {
        "billing_config": billing_config,
        "stripe_api_key": stripe_api_key,
        "stripe_api_version": stripe_api_version,
        "webhook_secret": webhook_secret,
    }


class MemberClient(BillingClient):
    """HTTP client for RAGFlow tenant/member APIs used by member flows.

    Extends RAGFlowClient with member-specific operations:
    - list_members: GET /tenant/<tenant_id>/user/list
    - invite_member: POST /tenant/<tenant_id>/user
    - remove_member: DELETE /tenant/<tenant_id>/user/<user_id>
    - accept_invitation: PUT /tenant/agree/<tenant_id>
    - get_member_quota_overview: GET /billing/plan_overview (includes quota_members)
    """

    def list_members(self) -> list[dict[str, Any]]:
        """List all members of a tenant.

        Returns:
            List of member dictionaries.

        Raises:
            FlowError: If the API request fails.
        """
        members_may_include_owner: list[dict[str, Any]] = self.request_json("GET", f"/tenant/{self.tenant_id}/user/list")["data"]
        return [m for m in members_may_include_owner if m["role"] != UserTenantRole.OWNER]


    def invite_member(self, email: str) -> dict[str, Any]:
        """Invite a new member to the tenant.

        Args:
            email: Email address of the user to invite.

        Returns:
            Response data including invited user details.

        Raises:
            FlowError: If the API request fails (e.g., user not found, already in team).
        """
        return self.request_json("POST", f"/tenant/{self.tenant_id}/user", json={"email": email})

    def remove_member(self, tenant_id: str, user_id: str) -> dict[str, Any]:
        """Remove a member from the tenant.

        Args:
            tenant_id: The tenant ID to remove from.
            user_id: The user ID to remove.

        Returns:
            Response data confirming removal.

        Raises:
            FlowError: If the API request fails.
        """
        return self.request_json("DELETE", f"/tenant/{tenant_id}/user/{user_id}")

    def accept_invitation(self, tenant_id: str) -> dict[str, Any]:
        """Accept an invitation to join a tenant.

        This endpoint is decorated with @check_resources(seats=1) which validates
        that the tenant has sufficient member quota before allowing the acceptance.

        Args:
            tenant_id: The tenant ID to accept invitation for.

        Returns:
            Response data confirming acceptance.

        Raises:
            FlowError: If the API request fails (e.g., insufficient quota).
        """
        return self.request_json("PUT", f"/tenant/agree/{tenant_id}")

    def register_member_only(self, email: str, password: str = "Test123456") -> dict[str, Any]:
        """Register a new user without logging them in.

        This is used to create user accounts that can then be invited to a tenant.
        The invite_member API requires the user to already exist in the system.

        Args:
            email: Email address for the new user.
            password: Password for the new user (default: "Test123456").

        Returns:
            Response data from the registration API.

        Raises:
            FlowError: If the registration fails.
        """
        encrypted_password = crypt(password)
        register_payload = {
            "email": email,
            "nickname": email.split("@", 1)[0],
            "password": encrypted_password,
        }
        response = self.session.post(
            self.url("/user/register"),
            headers=self.headers(auth=False),
            json=register_payload,
            timeout=60,
        )
        try:
            data = response.json()
        except ValueError as exc:
            raise FlowError(
                f"register returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if data.get("code") != 0 and "has already registered" not in (data.get("message") or ""):
            raise FlowError(f"register failed for {email}: {data}")
        return data

    def login_as_member(self, email: str, password: str = "Test123456") -> tuple[str, str]:
        """Login as a member user and return (user_id, tenant_id).

        This is used to switch context to a member user so they can accept
        the invitation to join the tenant.

        Args:
            email: Email address of the member.
            password: Password of the member.

        Returns:
            Tuple of (user_id, tenant_id).

        Raises:
            FlowError: If login fails.
        """
        encrypted_password = crypt(password)
        login_response = self.session.post(
            self.url("/user/login"),
            headers=self.headers(auth=False),
            json={"email": email, "password": encrypted_password},
            timeout=60,
        )
        try:
            login_data = login_response.json()
        except ValueError as exc:
            raise FlowError(
                f"login returned non-JSON status={login_response.status_code}: {login_response.text[:500]}"
            ) from exc
        if login_data.get("code") != 0:
            raise FlowError(f"login failed for {email}: {login_data}")
        auth_header = login_response.headers.get("Authorization", "")
        if not auth_header:
            raise FlowError("login succeeded without Authorization header")
        # Set the auth header so subsequent requests use the member's credentials
        self.auth_header = auth_header
        data = login_data.get("data") or {}
        user_id = data.get("id") or data.get("user_id") or ""
        tenant_id = data.get("tenant_id") or data.get("tenantId") or user_id
        if not user_id or not tenant_id:
            raise FlowError(f"login response missing ids: {login_data}")
        return user_id, tenant_id

    def accept_invitation_as_user(self, email: str, password: str, target_tenant_id: str) -> dict[str, Any]:
        """Register, login, and accept invitation for a member user.

        This is a convenience method that combines register, login, and accept.

        Args:
            email: Email address of the member.
            password: Password of the member.
            target_tenant_id: The tenant ID to accept invitation for.

        Returns:
            Response data from the accept invitation API.

        Raises:
            FlowError: If any step fails.
        """
        # Step 1: Register the user (may already exist, which is OK)
        self.register_member_only(email, password)

        # Step 2: Save current auth state
        saved_auth_header = self.auth_header

        # Step 3: Login as the member user
        self.login_as_member(email, password)

        # Step 4: Accept the invitation
        try:
            result = self.accept_invitation(target_tenant_id)
        except FlowError:
            # Restore auth state
            self.auth_header = saved_auth_header
            raise

        # Step 5: Restore auth state back to the original owner
        self.auth_header = saved_auth_header
        return result

    def get_member_quota_overview(self) -> dict[str, Any]:
        """Get the billing overview including member quota information.

        Returns:
            Dictionary containing quota_members, members_used, and other quota info.

        Raises:
            FlowError: If the API request fails.
        """
        return self.request_json("GET", "/billing/plan_overview")["data"]["resources"]["members"]

def get_trial_price_id() -> str:
    """Get the Trial plan price ID from billing configuration.

    Returns:
        Stripe price ID for the Trial plan.

    Raises:
        FlowError: If Trial plan is not found in configuration.
    """
    config = load_billing_config()
    plans = config.get("billing_plans", [])
    for plan in plans:
        if plan.get("name") == "Trial":
            price_ids = plan.get("price_ids", [])
            if isinstance(price_ids, list) and price_ids:
                return price_ids[0]
            if isinstance(price_ids, str) and price_ids:
                return price_ids
    raise FlowError("Trial plan not found in billing configuration")

def get_starter_price_id() -> str:
    """Get the Starter plan price ID from billing configuration.

    Returns:
        Stripe price ID for the Starter plan.

    Raises:
        FlowError: If Starter plan is not found in configuration.
    """
    config = load_billing_config()
    plans = config.get("billing_plans", [])
    for plan in plans:
        if plan.get("name") == "Starter":
            price_ids = plan.get("price_ids", [])
            if isinstance(price_ids, list) and price_ids:
                return price_ids[0]
            if isinstance(price_ids, str) and price_ids:
                return price_ids
    raise FlowError("Starter plan not found in billing configuration")

def get_pro_price_id() -> str:
    """Get the Pro plan price ID from billing configuration.

    Returns:
        Stripe price ID for the Pro plan.

    Raises:
        FlowError: If Pro plan is not found in configuration.
    """
    config = load_billing_config()
    plans = config.get("billing_plans", [])
    for plan in plans:
        if plan.get("name") == "Pro":
            price_ids = plan.get("price_ids", [])
            if isinstance(price_ids, list) and price_ids:
                return price_ids[0]
            if isinstance(price_ids, str) and price_ids:
                return price_ids
    raise FlowError("Pro plan not found in billing configuration")

def get_quota_members_limit(plan_name: str) -> int:
    """Get the member quota limit for a specific plan from billing configuration.

    Args:
        plan_name: Name of the plan (Trial, Starter, Pro).

    Returns:
        Member quota limit for the plan.

    Raises:
        FlowError: If the plan is not found in configuration.
    """
    config = load_billing_config()
    plans = config.get("billing_plans", [])
    for plan in plans:
        if plan.get("name") == plan_name:
            return int(plan.get("quota_members", 0))
    raise FlowError(f"Plan '{plan_name}' not found in billing configuration")
