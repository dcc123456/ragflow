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
MEMBER-02: Trial Member Quota Rejection and Starter Acceptance Test Flow

This test flow validates the behavior when a member tries to accept an invitation
under Trial plan (quota_members = 1, owner only), and then succeeds after upgrading
to Starter plan (quota_members = 5).

Test Scenarios:
1. Start with Trial plan (quota_members = 1, owner only)
2. Invite a member, then try to accept - should be rejected due to insufficient quota
3. Upgrade to Starter plan (quota_members = 5)
4. The previously rejected member tries to accept again - should succeed
5. Add more members to verify Starter quota allows additional members

APIs Used:
- POST /tenant/<tenant_id>/user - Invite a member
- GET /tenant/<tenant_id>/user/list - List all members
- PUT /tenant/agree/<tenant_id> - Accept invitation
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


def test_trial_rejection_and_starter_acceptance(args: argparse.Namespace) -> None:
    """Test member quota rejection on Trial and successful acceptance after Starter upgrade.

    Args:
        args: Command line arguments.

    Raises:
        FlowError: If any test assertion fails.
    """
    print("\n" + "=" * 80)
    print("MEMBER-02: Trial Rejection and Starter Acceptance Test")
    print("=" * 80)

    # Load runtime configuration
    config = load_member_runtime_config()
    stripe.api_key = config["stripe_api_key"]
    stripe.api_version = config["stripe_api_version"]
    print("  Assert: Runtime config loaded successfully")

    # Get expected quotas
    trial_quota = get_quota_members_limit("Trial")
    starter_quota = get_quota_members_limit("Starter")
    print(f"  Assert: Trial quota_members: {trial_quota}")
    print(f"  Assert: Starter quota_members: {starter_quota}")

    # Create client and setup environment (starts as Trial)
    email = f"billing-member02-{uuid.uuid4().hex[:12]}@example.test"
    client: MemberClient = create_client_with_type(args, email, MemberClient)

    print("  Assert: Trial environment ready")
    print(f"  Assert: Tenant ID: {client.tenant_id}")
    print(f"  Assert: User ID: {client.user_id}")
    print(f"  Assert: Customer ID: {client.customer_id}")

    # Verify initial member count (should be 0, excluding owner)
    members = client.list_members()
    if len(members) != 0:
        raise FlowError(f"Expected 0 members (exclude owner), got {len(members)}")
    print("  Assert: Initial member count is 0 (exclude owner)")

    # Verify Trial quota
    overview = client.get_member_quota_overview()
    if not overview:
        raise FlowError(f"Members not found in billing overview: {overview}")
    quota_members = overview["limit"]
    if quota_members != trial_quota:
        raise FlowError(f"Expected Trial quota {trial_quota}, got {quota_members} from overview")
    print(f"  Assert: Trial member quota verified: {quota_members}")

    # =============================================================================
    # Step a: Invite a member on Trial plan, then test accept (should be rejected)
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step a: Invite member on Trial plan, accept should be rejected")
    print("=" * 80)

    member_email = f"test_member_trial-{uuid.uuid4().hex[:6]}@example.test"
    member_password = "Test123456"

    # Register the member user first
    client.register_member_only(member_email, member_password)
    print(f"  Assert: Registered member user: {member_email}")

    # Invite the member
    result = client.invite_member(member_email)
    if result.get("code") != 0:
        raise FlowError(f"Failed to invite member: {member_email} - {result.get('message')}")
    print(f"  Assert: Invited member: {member_email}")

    # Create a client for the member and try to accept - should fail due to quota
    member_client = MemberClient(
        base_url=args.base_url,
        version=args.version,
        clock_id=client.clock_id,
        webhook_secret=client.webhook_secret,
        mode=args.webhook_mode,
    )
    member_client.login_as_member(member_email, member_password)

    try:
        member_client.accept_invitation(client.tenant_id)
        raise FlowError("Member was incorrectly accepted on Trial plan (should have been rejected)")
    except FlowError as e:
        error_msg = str(e).lower()
        if "quota" in error_msg or "seats" in error_msg or "resource" in error_msg:
            print(f"  Assert: Correctly rejected member on Trial plan: {e}")
        else:
            raise FlowError(f"Member was incorrectly rejected with unexpected error: {e}")

    # =============================================================================
    # Step b: Upgrade to Starter, previously rejected member should now succeed
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step b: Upgrade to Starter, previously rejected member accepts again")
    print("=" * 80)

    # Upgrade Trial -> Starter
    upgrade_result = client.upgrade_trial_to_starter()
    subscription_id = upgrade_result.get("subscription_id", "")
    print(f"  Assert: Upgraded to Starter, subscription_id: {subscription_id}")

    # Verify Starter quota
    overview_starter = client.get_member_quota_overview()
    starter_quota_actual = overview_starter["limit"]
    if starter_quota_actual != starter_quota:
        raise FlowError(f"Expected Starter quota {starter_quota}, got {starter_quota_actual}")
    print(f"  Assert: Starter member quota verified: {starter_quota_actual}")

    # The previously rejected member tries to accept again - should succeed now
    member_client_retry = MemberClient(
        base_url=args.base_url,
        version=args.version,
        clock_id="",
        webhook_secret=client.webhook_secret,
        mode=args.webhook_mode,
    )
    member_client_retry.login_as_member(member_email, member_password)
    member_client_retry.accept_invitation(client.tenant_id)
    print(f"  Assert: Member successfully accepted invitation after Starter upgrade: {member_email}")

    # Verify member count
    members = client.list_members()
    if len(members) != 1:
        raise FlowError(f"Expected 1 member after acceptance, got {len(members)}")
    print(f"  Assert: Member count after acceptance: {len(members) + 1}/{starter_quota} (including owner)")

    # =============================================================================
    # Step c: Add more members to verify Starter quota allows additional members
    # =============================================================================
    print("\n" + "=" * 80)
    print(f"Step c: Add more members to verify Starter quota (up to {starter_quota} total)")
    print("=" * 80)

    # We already have 2 members (owner + 1), can add up to starter_quota - 2 more
    additional_count = starter_quota - 2  # -1 for owner, -1 for existing member
    print(f"  Assert: Will invite {additional_count} more members")

    for i in range(1, additional_count + 1):
        additional_email = f"test_member_starter{i}-{uuid.uuid4().hex[:6]}@example.test"

        # Register the member user first
        client.register_member_only(additional_email, member_password)
        print(f"  Assert: Registered additional member user: {additional_email}")

        # Invite the member
        result = client.invite_member(additional_email)
        if result.get("code") != 0:
            raise FlowError(f"Failed to invite member {i}: {additional_email} - {result.get('message')}")
        print(f"  Assert: Invited member {i}: {additional_email}")

        # Create a client for this member and accept
        additional_client = MemberClient(
            base_url=args.base_url,
            version=args.version,
            clock_id="",
            webhook_secret=client.webhook_secret,
            mode=args.webhook_mode,
        )
        additional_client.login_as_member(additional_email, member_password)
        additional_client.accept_invitation(client.tenant_id)
        print(f"  Assert: Member {i} accepted invitation: {additional_email}")

    # Verify final member count
    members_final = client.list_members()
    expected_total = starter_quota - 1  # excluding owner
    if len(members_final) != expected_total:
        raise FlowError(f"Expected {expected_total} members, got {len(members_final)}")
    print(f"  Assert: Final member count: {len(members_final) + 1}/{starter_quota} (including owner)")

    # Test Summary
    overview_final = client.get_member_quota_overview()
    print("\n" + "=" * 80)
    print("MEMBER-02 Test Summary")
    print("=" * 80)
    print(json.dumps({
        "case": "MEMBER-02",
        "description": "Trial rejection and Starter acceptance test",
        "tenant_id": client.tenant_id,
        "email": email,
        "test_clock_id": client.clock_id,
        "customer_id": client.customer_id,
        "trial_quota": trial_quota,
        "starter_quota": starter_quota,
        "final_member_count": len(members_final) + 1,  # +1 for owner
        "webhook_mode": args.webhook_mode,
        "status": "PASSED",
        "overview_final": overview_final,
    }, indent=2, sort_keys=True))


def run_flow(args: argparse.Namespace) -> None:
    """Execute MEMBER-02: Trial rejection and Starter acceptance test."""
    test_trial_rejection_and_starter_acceptance(args)

    # Final Summary
    print("\n" + "=" * 80)
    print("MEMBER-02 Overall Test Summary")
    print("=" * 80)
    print(json.dumps({
        "case": "MEMBER-02",
        "description": "Trial rejection and Starter acceptance test",
        "overall_status": "PASSED",
    }, indent=2, sort_keys=True))


def main() -> int:
    parser = make_default_parser("Run billing MEMBER-02: Trial rejection and Starter acceptance test.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
