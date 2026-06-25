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

import json
import subprocess
import sys
from pathlib import Path
from typing import Any

from libs.billing.billing_common import (
    BillingClient,
    DEFAULT_TEST_PASSWORD_ENCRYPTED,
    FlowError,
    env,
    load_stripe_test_runtime_config,
    prepare_backend_imports,
)

PROJECT_ROOT = Path(__file__).resolve().parents[4]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

DEFAULT_MEMBER_TEST_PASSWORD = "Test123456"
DEFAULT_MEMBER_TEST_PASSWORD_ENCRYPTED = (
    "XR5GxBUSJS13cx9NafOYP/ROYad03zudlCuy+IBCMjelkLICT09LUp1RYzM0AJlLjjs40aMFeckDfVTvUxx1JXABhITTaHUhYDtgL2I+doEyNyGZvz1M8"
    "CvkQEOf5dfKWDrZuttD7iPFRdo14hehoCGgRaQo5e7sivRLcMDROCySa6kGfI7Xob19wS1ts+pgJS9IlAFjVwyHnjWjDzgUX/wf45F2TH/UQ+2zOnCglB"
    "/8TlxQSUT2jJiqiF7mACtsHmAo1V6QzQaj7QTUyM9y8e2DxMTTRs8jBu5g78L1P1epBOyfq/WhZdKpP3gDVVQ2QeNBDWymj9JOTdaesLyABA=="
)


def load_member_runtime_config() -> dict[str, Any]:
    """Load runtime configuration for member test flows."""
    return load_stripe_test_runtime_config(require_test_mode_message="Member automation requires a Stripe test-mode secret key")


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
        prepare_backend_imports()
        from api.db import UserTenantRole

        members_may_include_owner: list[dict[str, Any]] = self.request_json(
            "GET",
            f"/tenants/{self.tenant_id}/users",
            need_api_path=True,
        )["data"]
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
        return self.request_json(
            "POST",
            f"/tenants/{self.tenant_id}/users",
            need_api_path=True,
            json={"email": email},
        )

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
        return self.request_json(
            "DELETE",
            f"/tenants/{tenant_id}/users",
            need_api_path=True,
            json={"user_id": user_id},
        )

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
        return self.request_json(
            "PATCH",
            f"/tenants/{tenant_id}",
            need_api_path=True,
        )

    def register_member_only(self, email: str, password: str = "Test123456") -> dict[str, Any]:
        """Register a new user without logging them in.

        Member tests may need many users under a single Stripe test clock. The
        normal registration API creates a Stripe customer per user, which quickly
        exceeds Stripe's per-clock customer limit. For member-only test accounts
        we create the minimal local user record directly instead.

        Args:
            email: Email address for the new user.
            password: Password for the new user (default: "Test123456").

        Returns:
            Response data from the registration API.

        Raises:
            FlowError: If the registration fails.
        """
        if password == DEFAULT_MEMBER_TEST_PASSWORD:
            pass
        elif password == "Test1234!":
            pass
        else:
            raise FlowError(
                "member billing test helper only supports the fixed default test passwords"
            )

        container_name = (
            env("RAGFLOW_CONTAINER")
            or env("RAGFLOW_SERVICE_CONTAINER")
            or "docker-ragflow-1"
        )
        script = f"""
import base64, json, sys
from common import settings
settings.init_settings()
from api.db.services.role_service import RoleService
from api.db.services.user_service import UserService
from common.misc_utils import get_uuid
from common.time_utils import get_format_time

email = sys.argv[1]
password = sys.argv[2]
existing_users = UserService.query(email=email)
if existing_users:
    existing = existing_users[0]
    print(json.dumps({
        "code": 0,
        "data": {"id": existing.id, "email": existing.email, "nickname": existing.nickname},
        "message": "already registered",
    }))
    raise SystemExit(0)

role_name = settings.DEFAULT_ROLE
roles = RoleService.get_by_role_name(role_name)
if not roles:
    raise RuntimeError("Role not found for lightweight member registration: " + role_name)

user_dict = {
    "access_token": get_uuid(),
    "email": email,
    "nickname": email.split("@", 1)[0],
    "password": base64.b64encode(password.encode("utf-8")).decode("utf-8"),
    "login_channel": "password",
    "last_login_time": get_format_time(),
    "is_superuser": False,
    "role_id": roles[0]["id"],
}
UserService.save(id=get_uuid(), **user_dict)
users = UserService.query(email=email)
if not users:
    raise RuntimeError(f"lightweight register returned no user for {email}")
created = users[0]
print(json.dumps({
    "code": 0,
    "data": {"id": created.id, "email": created.email, "nickname": created.nickname},
    "message": "registered",
}))
"""
        result = subprocess.run(
            ["docker", "exec", container_name, "python", "-c", script, email, password],
            capture_output=True,
            text=True,
            check=False,
        )
        if result.returncode != 0:
            raise FlowError(
                f"lightweight register failed for {email}: "
                f"returncode={result.returncode}, stdout={result.stdout.strip()}, stderr={result.stderr.strip()}"
            )
        stdout = result.stdout.strip()
        if not stdout:
            raise FlowError(f"lightweight register for {email} returned empty stdout")
        try:
            return_data = stdout.splitlines()[-1]
            register_data = json.loads(return_data)
        except Exception as exc:
            raise FlowError(f"lightweight register returned invalid JSON for {email}: {stdout}") from exc
        if register_data.get("code") != 0:
            raise FlowError(f"lightweight register failed for {email}: {register_data}")
        return register_data

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
        if password == DEFAULT_MEMBER_TEST_PASSWORD:
            encrypted_password = DEFAULT_MEMBER_TEST_PASSWORD_ENCRYPTED
        elif password == "Test1234!":
            encrypted_password = DEFAULT_TEST_PASSWORD_ENCRYPTED
        else:
            raise FlowError(
                "member billing test helper only supports the fixed default test passwords"
            )
        login_response = self.session.post(
            self.url("/auth/login", True),
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
        response = self.session.get(
            self.billing_url("/subscription/overview"),
            headers=self.headers(auth=True),
            timeout=60,
        )
        try:
            payload = response.json()
        except ValueError as exc:
            raise FlowError(
                f"GET /billing/subscription/overview returned non-JSON status={response.status_code}: {response.text[:500]}"
            ) from exc
        if response.status_code >= 400 or payload.get("code") not in (0, None):
            raise FlowError(f"GET /billing/subscription/overview failed status={response.status_code}: {payload}")
        return payload["data"]["resources"]["members"]
