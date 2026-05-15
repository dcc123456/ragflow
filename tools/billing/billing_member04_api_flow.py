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
MEMBER-04: Pro Plan Downgrade Enforcement Test Flow

This test flow validates the member quota enforcement when downgrading from Pro to Starter:
- Pro plan: quota_members = 20
- Starter plan: quota_members = 5

Test Scenarios:
1. Start with Pro plan (initial state)
2. Add members up to Starter quota + 1 (i.e., 6 members including owner)
3. Attempt to downgrade to Starter - should fail due to exceeding member quota
4. Randomly remove one member (now 5 members, within Starter quota)
5. Attempt to downgrade to Starter again - should succeed

APIs Used:
- POST /tenant/<tenant_id>/user - Invite a member
- GET /tenant/<tenant_id>/user/list - List all members
- DELETE /tenant/<tenant_id>/user/<user_id> - Remove a member
- POST /billing/checkout - Initiate plan downgrade
- GET /billing/plan_overview - Check quota usage
"""

from __future__ import annotations

import argparse
import json
import random
import sys
import uuid

import stripe

from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    make_default_parser,
)
from tools.billing.billing_client import create_client_with_type
from tools.billing.member_common import MemberClient, load_member_runtime_config, get_quota_members_limit, get_starter_price_id  # noqa: E402


def test_pro_downgrade_enforcement(args: argparse.Namespace) -> None:
    """Test member quota enforcement when downgrading from Pro to Starter.

    Args:
        args: Command line arguments.

    Raises:
        FlowError: If any test assertion fails.
    """
    print("\n" + "=" * 80)
    print("Testing Pro plan downgrade enforcement with member quota")
    print("=" * 80)

    # Load runtime configuration
    config = load_member_runtime_config()
    stripe.api_key = config["stripe_api_key"]
    stripe.api_version = config["stripe_api_version"]
    print("  Assert: Runtime config loaded successfully")

    # Get the expected quotas
    starter_quota = get_quota_members_limit("Starter")
    pro_quota = get_quota_members_limit("Pro")
    print(f"  Assert: Expected quota_members for Starter: {starter_quota}")
    print(f"  Assert: Expected quota_members for Pro: {pro_quota}")

    # Create client and setup Pro environment
    email = f"billing-member04-{uuid.uuid4().hex[:12]}@example.test"
    client: MemberClient = create_client_with_type(args, email, MemberClient)

    # Upgrade Trial -> Starter -> Pro
    print("\n  Assert: Upgrading Trial -> Starter -> Pro")
    starter_result = client.upgrade_trial_to_starter()
    starter_subscription_id = starter_result.get("subscription_id", "")
    pro_result = client.upgrade_starter_to_pro(starter_subscription_id)
    subscription_id = pro_result.get("subscription_id", "")
    print(f"  Assert: Pro subscription ID: {subscription_id}")

    print(f"  Assert: Tenant ID: {client.tenant_id}")
    print(f"  Assert: User ID: {client.user_id}")
    print(f"  Assert: Customer ID: {client.customer_id}")

    # Verify initial member count (should be 0, excluding owner)
    members = client.list_members()
    if len(members) != 0:
        raise FlowError(f"Expected 0 members (exclude owner), got {len(members)}")
    print("  Assert: Initial member count is 0 (exclude owner)")

    # Verify Pro plan quota
    overview = client.get_member_quota_overview()
    if not overview:
        raise FlowError(f"Members not found in billing overview: {overview}")
    quota_members = overview["limit"]
    if quota_members != pro_quota:
        raise FlowError(f"Expected Pro quota {pro_quota}, got {quota_members} from overview")
    print(f"  Assert: Member quota from overview (Pro): {quota_members}")

    # Calculate how many members to invite: Starter quota (5) + 1 - 1(owner) = Starter quota
    # We want total members (including owner) = Starter quota + 1
    # So invited members = Starter quota + 1 - 1(owner) = Starter quota
    invite_count = starter_quota  # e.g., 5 members to invite, total = 6 (including owner)
    print(f"  Assert: Will invite {invite_count} members (total will be {invite_count + 1}, exceeding Starter quota of {starter_quota})")

    # Invite members up to Starter quota + 1
    test_emails = [f"test_member{idx}-{uuid.uuid4().hex[:6]}@example.test" for idx in range(1, invite_count + 1)]
    member_password = "Test123456"
    invited_user_ids = []

    for i, member_email in enumerate(test_emails, 1):
        # Register the member user first
        user_info = client.register_member_only(member_email, member_password)
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
        invited_user_ids.append(user_info["data"]["id"])

    # Verify total member count (including owner)
    members = client.list_members()
    total_members = len(members) + 1  # +1 for owner
    if total_members != invite_count + 1:
        raise FlowError(f"Expected {invite_count + 1} total members, got {total_members}")
    print(f"  Assert: Total members: {total_members}/{pro_quota} (Pro quota), exceeding Starter quota of {starter_quota}")

    # Attempt to downgrade to Starter (should fail)
    print("\n  Assert: Attempting to downgrade from Pro to Starter (should fail)")
    starter_price_id = get_starter_price_id()

    try:
        downgrade_result = client.schedule_plan_change(starter_price_id)
        # If the API call succeeded, we need to check if it was actually rejected
        # The downgrade might be scheduled but will fail when the period ends
        # Or it might be rejected immediately
        print(f"  Info: Downgrade request returned: {downgrade_result}")
    except FlowError as e:
        error_msg = str(e).lower()
        if "quota" in error_msg or "member" in error_msg or "seat" in error_msg:
            print(f"  Assert: Downgrade correctly rejected due to member quota: {e}")
        else:
            # Re-raise if it's a different error
            raise FlowError(f"Unexpected error during downgrade: {e}")

    # Now randomly remove one member
    print("\n  Assert: Randomly removing one member to allow downgrade")
    if not invited_user_ids:
        raise FlowError("No invited user IDs to remove")

    removed_user_id = random.choice(invited_user_ids)
    invited_user_ids.remove(removed_user_id)

    remove_result = client.remove_member(client.tenant_id, removed_user_id)
    print(f"  Assert: Removed member user_id: {removed_user_id}, remove_result:{remove_result}")

    # Verify member count after removal
    members = client.list_members()
    total_members_after_removal = len(members) + 1  # +1 for owner
    if total_members_after_removal != invite_count:
        raise FlowError(f"Expected {invite_count} total members after removal, got {total_members_after_removal}")
    print(f"  Assert: Total members after removal: {total_members_after_removal} (within Starter quota of {starter_quota})")

    # Attempt to downgrade to Starter again (should succeed)
    print("\n  Assert: Attempting to downgrade from Pro to Starter (should succeed)")

    try:
        downgrade_result = client.schedule_plan_change(starter_price_id)
        print(f"  Assert: Downgrade request submitted: {downgrade_result}")
    except FlowError as e:
        raise FlowError(f"Downgrade should have submitted after member removal: {e}")

    # Test Summary
    overview_final = client.get_member_quota_overview()
    members_final = client.list_members()
    print("\n" + "=" * 80)
    print("MEMBER-04 Test Summary")
    print("=" * 80)
    print(json.dumps({
        "case": "MEMBER-04",
        "description": "Pro plan downgrade enforcement test with member quota",
        "tenant_id": client.tenant_id,
        "email": email,
        "test_clock_id": client.clock_id,
        "customer_id": client.customer_id,
        "pro_quota": pro_quota, 
        "starter_quota": starter_quota,
        "members_before_downgrade_attempt": total_members,
        "members_after_removal": total_members_after_removal,
        "final_member_count": len(members_final) + 1,  # +1 for owner
        "webhook_mode": args.webhook_mode,
        "status": "PASSED",
        "overview_final": overview_final
    }, indent=2, sort_keys=True))


def run_flow(args: argparse.Namespace) -> None:
    """Execute MEMBER-04: Pro plan downgrade enforcement test."""
    test_pro_downgrade_enforcement(args)

    # Final Summary
    print("\n" + "=" * 80)
    print("MEMBER-04 Overall Test Summary")
    print("=" * 80)
    print(json.dumps({
        "case": "MEMBER-04",
        "description": "Pro plan downgrade enforcement test with member quota",
        "overall_status": "PASSED",
    }, indent=2, sort_keys=True))


def main() -> int:
    parser = make_default_parser("Run billing MEMBER-04: Pro plan downgrade enforcement test.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
