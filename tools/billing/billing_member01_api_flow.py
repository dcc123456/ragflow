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
MEMBER-01: Basic Member Quota Enforcement Test Flow

This test flow validates the basic member quota enforcement across different plans:
- Trial: quota_members = 1
- Starter: quota_members = 5
- Pro: quota_members = 20

Test Scenarios:
1. Verify Trial plan allows exactly 1 member (owner only)
2. Verify Starter plan allows up to 5 members
3. Verify Pro plan allows up to 20 members
4. Verify member invitation fails when quota is exceeded

APIs Used:
- POST /tenant/<tenant_id>/user - Invite a member
- GET /tenant/<tenant_id>/user/list - List all members
- DELETE /tenant/<tenant_id>/user/<user_id> - Remove a member
- GET /billing/plan_overview - Check quota usage
"""

from __future__ import annotations

import argparse
import json
import sys
import uuid

import stripe

from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    make_default_parser,
)
from tools.billing.billing_client import create_client_with_type
from tools.billing.member_common import MemberClient, load_member_runtime_config, get_quota_members_limit  # noqa: E402


def test_quota_members(plan_name: str, args: argparse.Namespace) -> None:
    """Test member quota enforcement for a specific plan.

    Args:
        plan_name: Name of the plan to test (Trial, Starter, Pro).
        args: Command line arguments.

    Raises:
        FlowError: If any test assertion fails.
    """
    print("\n" + "=" * 80)
    print(f"Testing {plan_name} plan member quota enforcement")
    print("=" * 80)

    # Load runtime configuration
    config = load_member_runtime_config()
    stripe.api_key = config["stripe_api_key"]
    stripe.api_version = config["stripe_api_version"]
    print("  Assert: Runtime config loaded successfully")

    # Get the expected quota for this plan
    expected_quota = get_quota_members_limit(plan_name)
    print(f"  Assert: Expected quota_members for {plan_name}: {expected_quota}")

    # Create client and setup environment
    email = f"billing-member01-{plan_name.lower()}-{uuid.uuid4().hex[:12]}@example.test"
    client: MemberClient = create_client_with_type(args, email, MemberClient)

    # Upgrade to the target plan if not Trial
    if plan_name == "Starter":
        client.upgrade_trial_to_starter()
    elif plan_name == "Pro":
        # First upgrade Trial -> Starter
        starter_result = client.upgrade_trial_to_starter()
        starter_subscription_id = starter_result.get("subscription_id", "")
        # Then upgrade Starter -> Pro
        client.upgrade_starter_to_pro(starter_subscription_id)

    print(f"  Assert: {plan_name} environment ready")
    print(f"  Assert: Tenant ID: {client.tenant_id}")
    print(f"  Assert: User ID: {client.user_id}")
    print(f"  Assert: Customer ID: {client.customer_id}")

    # Verify initial member count (should be 0, excluding owner)
    members = client.list_members()
    if len(members) != 0:
        raise FlowError(f"Expected 0 members (exclude owner), got {len(members)}")
    print("  Assert: Initial member count is 0 (exclude owner)")

    # Check billing overview for member quota
    overview = client.get_member_quota_overview()
    if not overview:
        raise FlowError(f"Members not found in billing overview: {overview}")
    quota_members = overview["limit"]
    print(f"  Assert: Member quota from overview: {quota_members}")

    if quota_members != expected_quota:
        raise FlowError(f"Expected quota {expected_quota}, got {quota_members} from overview")

    # Calculate how many members we can invite (quota - 1 for owner)
    invite_count = expected_quota - 1  # -1 for owner
    print(f"  Assert: Will invite {invite_count} members (quota {expected_quota} - owner)")

    # Invite members up to quota limit
    test_emails = [f"test_member{idx}-{uuid.uuid4().hex[:6]}@example.test" for idx in range(1, invite_count + 1)]
    member_password = "Test123456"

    for i, member_email in enumerate(test_emails, 1):
        # Register the member user first
        client.register_member_only(member_email, member_password)
        print(f"  Assert: Registered member user {i}: {member_email}")

        # Invite the registered user to the tenant
        result = client.invite_member(member_email)
        if result.get("code") != 0:
            raise FlowError(f"Failed to invite member {i}: {member_email} - {result.get('message')}")
        print(f"  Assert: Invited member {i}: {member_email}")

        # Create a new client for this member and accept the invitation
        member_client = MemberClient(
            base_url=args.base_url,
            version=args.version,
            clock_id="",
            webhook_secret=client.webhook_secret,
            mode=args.webhook_mode,
        )
        member_client.login_as_member(member_email, member_password)
        member_client.accept_invitation(client.tenant_id)
        print(f"  Assert: Member {i} accepted invitation via dedicated client")

    # Verify total member count
    members = client.list_members()
    if len(members) != invite_count:
        raise FlowError(f"Expected {invite_count} members, got {len(members)}")
    print(f"  Assert: Total members after invitations: {len(members) + 1}/{expected_quota} (including owner)")

    # Attempt to invite one more member (should fail - exceeds quota)
    print(f"\n  Assert: Attempting to invite member beyond quota ({expected_quota + 1}th member)")
    extra_email = f"test_member_extra-{uuid.uuid4().hex[:6]}@example.test"

    # Register the extra user first
    client.register_member_only(extra_email, member_password)
    print(f"  Assert: Registered extra member user: {extra_email}")

    # Invite the extra user (invite may succeed, but accept should fail)
    client.invite_member(extra_email)
    print("  Assert: Extra member invited (quota check deferred to accept)")

    # Create a new client for the extra member and try to accept
    extra_client = MemberClient(
        base_url=args.base_url,
        version=args.version,
        clock_id=client.clock_id,
        webhook_secret=client.webhook_secret,
        mode=args.webhook_mode,
    )
    extra_client.login_as_member(extra_email, member_password)

    # Try to accept the invitation - this should fail due to quota exceeded
    try:
        extra_client.accept_invitation(client.tenant_id)
        raise FlowError("Extra member was incorrectly accepted (accept succeeded)")
    except FlowError as e:
        error_msg = str(e).lower()
        if "quota" in error_msg or "seats" in error_msg or "resource" in error_msg:
            print(f"  Assert: Correctly rejected extra member on accept: {e}")
        else:
            raise FlowError(f"Extra member was incorrectly processed: {error_msg}")

    # Test Summary
    overview_final = client.get_member_quota_overview()
    members_final = client.list_members()
    print("\n" + "=" * 80)
    print(f"{plan_name} Plan Test Summary")
    print("=" * 80)
    print(json.dumps({
        "case": "MEMBER-01",
        "plan": plan_name,
        "description": f"Member quota enforcement test for {plan_name} plan",
        "tenant_id": client.tenant_id,
        "email": email,
        "test_clock_id": client.clock_id,
        "customer_id": client.customer_id,
        "expected_quota": expected_quota,
        "actual_quota": overview_final,
        "final_member_count": len(members_final) + 1,  # +1 for owner
        "webhook_mode": args.webhook_mode,
        "status": "PASSED",
    }, indent=2, sort_keys=True))


def run_flow(args: argparse.Namespace) -> None:
    """Execute MEMBER-01: basic member quota enforcement test for all plans."""
    plans = ["Trial", "Starter", "Pro"]

    for plan in plans:
        try:
            test_quota_members(plan, args)
        except Exception as exe:
            raise FlowError(f"failed to test plan:{plan}, exception:{exe}")

    # Final Summary
    print("\n" + "=" * 80)
    print("MEMBER-01 Overall Test Summary")
    print("=" * 80)
    print(json.dumps({
        "case": "MEMBER-01",
        "description": "Basic member quota enforcement test for all plans",
        "plans_tested": plans,
        "overall_status": "PASSED",
    }, indent=2, sort_keys=True))


def main() -> int:
    parser = make_default_parser("Run billing MEMBER-01: basic member quota enforcement test.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
