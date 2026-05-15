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
MEMBER-05: Member Quota Persistence Through Upgrade/Downgrade Test Flow

This test flow validates that members are retained when upgrading and downgrading plans:
- Starter plan: quota_members = 5
- Pro plan: quota_members = 20

Test Scenarios:
1. Start with Starter plan
2. Add 3 members (within Starter quota of 5)
3. Upgrade to Pro plan - verify members remain unchanged
4. Downgrade back to Starter - verify members remain unchanged (3 <= 5)

APIs Used:
- POST /tenant/<tenant_id>/user - Invite a member
- GET /tenant/<tenant_id>/user/list - List all members
- POST /billing/checkout - Initiate plan upgrade/downgrade
- GET /billing/plan_overview - Check quota usage
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import uuid

import stripe

from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    make_default_parser,
)
from tools.billing.billing_client import create_client_with_type
from tools.billing.member_common import MemberClient, load_member_runtime_config, get_quota_members_limit, get_starter_price_id


def test_member_persistence_upgrade_downgrade(args: argparse.Namespace) -> None:
    """Test member quota persistence through upgrade and downgrade.

    Args:
        args: Command line arguments.

    Raises:
        FlowError: If any test assertion fails.
    """
    print("\n" + "=" * 80)
    print("Testing member persistence through Starter -> Pro -> Starter upgrade/downgrade")
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

    # Create client and setup Starter environment
    email = f"billing-member05-{uuid.uuid4().hex[:12]}@example.test"
    client: MemberClient = create_client_with_type(args, email, MemberClient)

    # Upgrade Trial -> Starter
    print("\n  Assert: Upgrading Trial -> Starter")
    starter_result = client.upgrade_trial_to_starter()
    subscription_id = starter_result.get("subscription_id", "")
    print(f"  Assert: Starter subscription ID: {subscription_id}")

    print(f"  Assert: Tenant ID: {client.tenant_id}")
    print(f"  Assert: User ID: {client.user_id}")
    print(f"  Assert: Customer ID: {client.customer_id}")

    # Verify initial member count (should be 0, excluding owner)
    members = client.list_members()
    if len(members) != 0:
        raise FlowError(f"Expected 0 members (exclude owner), got {len(members)}")
    print("  Assert: Initial member count is 0 (exclude owner)")

    # Verify Starter plan quota
    overview = client.get_member_quota_overview()
    if not overview:
        raise FlowError(f"Members not found in billing overview: {overview}")
    quota_members = overview["limit"]
    if quota_members != starter_quota:
        raise FlowError(f"Expected Starter quota {starter_quota}, got {quota_members} from overview")
    print(f"  Assert: Member quota from overview (Starter): {quota_members}")

    # Add 3 members (within Starter quota of 5)
    invite_count = 3
    print(f"\n  Assert: Inviting {invite_count} members (within Starter quota of {starter_quota})")

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

    # Verify total member count (including owner)
    members = client.list_members()
    total_members = len(members) + 1  # +1 for owner
    if total_members != invite_count + 1:
        raise FlowError(f"Expected {invite_count + 1} total members, got {total_members}")
    print(f"  Assert: Total members after invitations: {total_members}/{starter_quota} (Starter quota)")

    # Step: Upgrade to Pro plan
    print("\n  Assert: Upgrading from Starter to Pro")
    upgrade_result = client.upgrade_starter_to_pro(subscription_id)
    new_subscription_id = upgrade_result.get("subscription_id", "")
    print(f"  Assert: Upgrade to Pro succeeded, new subscription ID: {new_subscription_id}")

    # Verify members remain unchanged after upgrade
    members_after_upgrade = client.list_members()
    total_members_after_upgrade = len(members_after_upgrade) + 1  # +1 for owner
    if total_members_after_upgrade != total_members:
        raise FlowError(f"Expected {total_members} members after upgrade, got {total_members_after_upgrade}")
    print(f"  Assert: Members after upgrade to Pro: {total_members_after_upgrade} (unchanged)")

    # Verify Pro plan quota
    overview_pro = client.get_member_quota_overview()
    quota_members_pro = overview_pro["limit"]
    if quota_members_pro != pro_quota:
        raise FlowError(f"Expected Pro quota {pro_quota}, got {quota_members_pro} from overview")
    print(f"  Assert: Member quota from overview (Pro): {quota_members_pro}")

    # Step: Downgrade back to Starter plan
    print("\n  Assert: Downgrading from Pro to Starter")
    starter_price_id = get_starter_price_id()
    subscription_ids = {new_subscription_id}

    try:
        downgrade_created_gte = int(time.time()) - 5
        downgrade_result = client.schedule_plan_change(starter_price_id)
        print(f"  Assert: Downgrade to Starter requested: {downgrade_result}")
    except FlowError as e:
        raise FlowError(f"Downgrade should have succeeded (members {total_members_after_upgrade - 1} <= Starter quota {starter_quota}): {e}")

    # Verify members remain unchanged after scheduling downgrade
    members_after_downgrade = client.list_members()
    total_members_after_downgrade = len(members_after_downgrade) + 1  # +1 for owner
    if total_members_after_downgrade != total_members:
        raise FlowError(f"Expected {total_members} members after scheduling downgrade, got {total_members_after_downgrade}")
    print(f"  Assert: Members after scheduling downgrade: {total_members_after_downgrade} (unchanged)")

    # Advance clock to plan end to apply the downgrade
    print("  Assert: Advancing clock to plan end for downgrade to take effect")
    client.advance_clock_to_plan_end()

    # Sync webhooks after clock advance
    client.sync_webhooks(
        subscription_ids=subscription_ids,
        created_gte=downgrade_created_gte,
        wait_seconds=8,
    )
    print("  Assert: Webhooks synced after clock advance")

    # Wait for plan to become Starter
    print("  Waiting for plan to become Starter")
    client.wait_for_plan("Starter", args.webhook_timeout_seconds)
    print("  Assert: Plan is now Starter")

    # Verify members remain unchanged after downgrade takes effect
    members_after_downgrade_effective = client.list_members()
    total_members_after_downgrade_effective = len(members_after_downgrade_effective) + 1  # +1 for owner
    if total_members_after_downgrade_effective != total_members:
        raise FlowError(f"Expected {total_members} members after downgrade takes effect, got {total_members_after_downgrade_effective}")
    print(f"  Assert: Members after downgrade takes effect: {total_members_after_downgrade_effective} (unchanged)")

    # Verify Starter plan quota
    overview_starter = client.get_member_quota_overview()
    quota_members_starter = overview_starter["limit"]
    if quota_members_starter != starter_quota:
        raise FlowError(f"Expected Starter quota {starter_quota}, got {quota_members_starter} from overview")
    print(f"  Assert: Member quota from overview (Starter): {quota_members_starter}")

    # Test Summary
    print("\n" + "=" * 80)
    print("MEMBER-05 Test Summary")
    print("=" * 80)
    print(json.dumps({
        "case": "MEMBER-05",
        "description": "Member persistence through Starter -> Pro -> Starter upgrade/downgrade",
        "tenant_id": client.tenant_id,
        "email": email,
        "test_clock_id": client.clock_id,
        "customer_id": client.customer_id,
        "starter_quota": starter_quota,
        "pro_quota": pro_quota,
        "members_added": invite_count,
        "total_members": total_members,
        "members_after_upgrade": total_members_after_upgrade,
        "members_after_downgrade": total_members_after_downgrade,
        "webhook_mode": args.webhook_mode,
        "status": "PASSED",
    }, indent=2, sort_keys=True))


def run_flow(args: argparse.Namespace) -> None:
    """Execute MEMBER-05: member persistence through upgrade/downgrade test."""
    try:
        test_member_persistence_upgrade_downgrade(args)
    except Exception as exe:
        raise FlowError(f"failed to test MEMBER-05, exception:{exe}")

    # Final Summary
    print("\n" + "=" * 80)
    print("MEMBER-05 Overall Test Summary")
    print("=" * 80)
    print(json.dumps({
        "case": "MEMBER-05",
        "description": "Member persistence through Starter -> Pro -> Starter upgrade/downgrade",
        "overall_status": "PASSED",
    }, indent=2, sort_keys=True))


def main() -> int:
    parser = make_default_parser("Run billing MEMBER-05: member persistence through upgrade/downgrade test.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
