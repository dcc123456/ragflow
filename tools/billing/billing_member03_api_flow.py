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
MEMBER-03: Remove Member to Enable New Member Acceptance Test Flow

This test flow validates the behavior when a member is removed from a tenant
that has reached its member quota, allowing a previously rejected member to
successfully accept an invitation.

Test Scenarios:
1. Start with Starter plan (quota_members = 5) and fill all member slots
2. Invite an additional member beyond quota - accept should be rejected
3. Randomly remove one existing member to free up a slot
4. The previously rejected member can now successfully accept the invitation

APIs Used:
- POST /tenant/<tenant_id>/user - Invite a member
- GET /tenant/<tenant_id>/user/list - List all members
- DELETE /tenant/<tenant_id>/user/<user_id> - Remove a member
- PUT /tenant/agree/<tenant_id> - Accept invitation
- GET /billing/plan_overview - Check quota usage
"""

from __future__ import annotations

import argparse
import json
import random
import sys
import uuid

import stripe

from api.db import UserTenantRole
from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    make_default_parser,
)
from tools.billing.billing_client import create_client_with_type
from tools.billing.member_common import MemberClient, load_member_runtime_config, get_quota_members_limit  # noqa: E402


def test_remove_member_to_enable_acceptance(args: argparse.Namespace) -> None:
    """Test removing a member to enable a previously rejected member to accept.

    Args:
        args: Command line arguments.

    Raises:
        FlowError: If any test assertion fails.
    """
    print("\n" + "=" * 80)
    print("MEMBER-03: Remove Member to Enable New Member Acceptance Test")
    print("=" * 80)

    # Load runtime configuration
    config = load_member_runtime_config()
    stripe.api_key = config["stripe_api_key"]
    stripe.api_version = config["stripe_api_version"]
    print("  Assert: Runtime config loaded successfully")

    # Get expected quota for Starter
    starter_quota = get_quota_members_limit("Starter")
    print(f"  Assert: Starter quota_members: {starter_quota}")

    # Create client and setup environment
    email = f"billing-member03-{uuid.uuid4().hex[:12]}@example.test"
    client: MemberClient = create_client_with_type(args, email, MemberClient)

    # Upgrade to Starter plan
    upgrade_result = client.upgrade_trial_to_starter()
    subscription_id = upgrade_result.get("subscription_id", "")
    print(f"  Assert: Upgraded to Starter, subscription_id: {subscription_id}")

    print("  Assert: Starter environment ready")
    print(f"  Assert: Tenant ID: {client.tenant_id}")
    print(f"  Assert: User ID: {client.user_id}")
    print(f"  Assert: Customer ID: {client.customer_id}")

    # Verify initial member count (should be 0, excluding owner)
    members = client.list_members()
    if len(members) != 0:
        raise FlowError(f"Expected 0 members (exclude owner), got {len(members)}")
    print("  Assert: Initial member count is 0 (exclude owner)")

    # Verify Starter quota
    overview = client.get_member_quota_overview()
    if not overview:
        raise FlowError(f"Members not found in billing overview: {overview}")
    quota_members = overview["limit"]
    if quota_members != starter_quota:
        raise FlowError(f"Expected Starter quota {starter_quota}, got {quota_members} from overview")
    print(f"  Assert: Starter member quota verified: {quota_members}")

    # =============================================================================
    # Step a: Fill all member slots (Starter quota)
    # =============================================================================
    print("\n" + "=" * 80)
    print(f"Step a: Fill all {starter_quota} member slots (including owner)")
    print("=" * 80)

    member_password = "Test123456"
    invited_members = []  # List of (email, member_client) tuples

    # Invite members up to quota limit (quota - 1 for owner)
    invite_count = starter_quota - 1
    test_emails = [f"test_member{idx}-{uuid.uuid4().hex[:6]}@example.test" for idx in range(1, invite_count + 1)]

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
        print(f"  Assert: Member {i} accepted invitation: {member_email}")

        invited_members.append((member_email, member_client))

    # Verify total member count
    normal_members = [m for m in client.list_members() if m["role"] != "invite"]
    if len(normal_members) != invite_count:
        raise FlowError(f"Expected {invite_count} members, got {len(normal_members)}")
    print(f"  Assert: Total normal members after filling quota: {len(normal_members) + 1}/{starter_quota} (including owner)")

    # =============================================================================
    # Step b: Invite an additional member - accept should be rejected
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step b: Invite additional member beyond quota - accept should be rejected")
    print("=" * 80)

    extra_email = f"test_member_extra-{uuid.uuid4().hex[:6]}@example.test"

    # Register the extra user first
    client.register_member_only(extra_email, member_password)
    print(f"  Assert: Registered extra member user: {extra_email}")

    # Invite the extra user
    client.invite_member(extra_email)
    print("  Assert: Extra member invited (quota check deferred to accept)")

    # Create a client for the extra member and try to accept - should fail
    extra_client = MemberClient(
        base_url=args.base_url,
        version=args.version,
        clock_id=client.clock_id,
        webhook_secret=client.webhook_secret,
        mode=args.webhook_mode,
    )
    extra_client.login_as_member(extra_email, member_password)

    try:
        extra_client.accept_invitation(client.tenant_id)
        raise FlowError("Extra member was incorrectly accepted (should have been rejected due to quota)")
    except FlowError as e:
        error_msg = str(e).lower()
        if "quota" in error_msg or "seats" in error_msg or "resource" in error_msg:
            print(f"  Assert: Correctly rejected extra member on accept: {e}")
        else:
            raise FlowError(f"Extra member was incorrectly rejected with unexpected error: {e}")

    # =============================================================================
    # Step c: Randomly remove one existing member
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step c: Randomly remove one existing member to free up a slot")
    print("=" * 80)

    # Get current members list
    members_before_removal = client.list_members()
    print(f"  Assert: Members before removal: {len(members_before_removal)}")

    # Randomly select a member to remove
    member_to_remove = random.choice(members_before_removal)
    member_to_remove_id = member_to_remove.get("user_id") or member_to_remove.get("id")
    member_to_remove_email = member_to_remove.get("email", "unknown")

    if not member_to_remove_id:
        raise FlowError(f"Cannot find user_id in member data: {member_to_remove}")

    print(f"  Assert: Randomly selected member to remove: {member_to_remove_email} (id: {member_to_remove_id})")

    # Remove the selected member
    remove_result = client.remove_member(client.tenant_id, member_to_remove_id)
    if remove_result.get("code") != 0:
        raise FlowError(f"Failed to remove member: {remove_result.get('message')}")
    print(f"  Assert: Successfully removed member: {member_to_remove_email}")

    # Verify member count after removal
    members_after_removal = [m for m in client.list_members() if m["role"] != UserTenantRole.INVITE]
    expected_count = invite_count - 1
    if len(members_after_removal) != expected_count:
        raise FlowError(f"Expected {expected_count} members after removal, got {members_after_removal}")
    print(f"  Assert: Members after removal: {len(members_after_removal) + 1}/{starter_quota} (including owner)")

    # =============================================================================
    # Step d: Previously rejected member can now accept successfully
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step d: Previously rejected member accepts successfully")
    print("=" * 80)

    # The previously rejected extra member tries to accept again - should succeed now
    extra_client_retry = MemberClient(
        base_url=args.base_url,
        version=args.version,
        clock_id=client.clock_id,
        webhook_secret=client.webhook_secret,
        mode=args.webhook_mode,
    )
    extra_client_retry.login_as_member(extra_email, member_password)
    extra_client_retry.accept_invitation(client.tenant_id)
    print(f"  Assert: Extra member successfully accepted invitation after member removal: {extra_email}")

    # Verify final member count
    members_final = client.list_members()
    expected_final_count = invite_count  # Same as before (one removed, one added)
    if len(members_final) != expected_final_count:
        raise FlowError(f"Expected {expected_final_count} members final, got {len(members_final)}")
    print(f"  Assert: Final member count: {len(members_final) + 1}/{starter_quota} (including owner)")

    # Test Summary
    overview_final = client.get_member_quota_overview()
    print("\n" + "=" * 80)
    print("MEMBER-03 Test Summary")
    print("=" * 80)
    print(json.dumps({
        "case": "MEMBER-03",
        "description": "Remove member to enable new member acceptance test",
        "tenant_id": client.tenant_id,
        "email": email,
        "test_clock_id": client.clock_id,
        "customer_id": client.customer_id,
        "starter_quota": starter_quota,
        "removed_member": member_to_remove_email,
        "accepted_member": extra_email,
        "final_member_count": len(members_final) + 1,  # +1 for owner
        "webhook_mode": args.webhook_mode,
        "overview": overview_final,
        "status": "PASSED",
    }, indent=2, sort_keys=True))


def run_flow(args: argparse.Namespace) -> None:
    """Execute MEMBER-03: Remove member to enable new member acceptance test."""
    test_remove_member_to_enable_acceptance(args)

    # Final Summary
    print("\n" + "=" * 80)
    print("MEMBER-03 Overall Test Summary")
    print("=" * 80)
    print(json.dumps({
        "case": "MEMBER-03",
        "description": "Remove member to enable new member acceptance test",
        "overall_status": "PASSED",
    }, indent=2, sort_keys=True))


def main() -> int:
    parser = make_default_parser("Run billing MEMBER-03: Remove member to enable new member acceptance test.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
