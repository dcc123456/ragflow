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
Pytest wrapper for MEMBER-01 through MEMBER-05 billing member quota flows.

MEMBER-01: Basic Member Quota Enforcement Test - Trial/Starter/Pro plans
MEMBER-02: Trial Member Quota Rejection and Starter Acceptance Test
MEMBER-03: Remove Member to Enable New Member Acceptance Test
MEMBER-04: Pro Plan Downgrade Enforcement Test
MEMBER-05: Member Quota Persistence Through Upgrade/Downgrade Test
"""

from __future__ import annotations

import logging
import random
import time
import uuid

import pytest

from api.db import UserTenantRole
from test.testcases.test_http_api.test_billing.assertions import (
    expect_failure_with_message,
    fail_on_flow_error,
)
from libs.billing.billing_common import (
    get_quota_members_limit,
    get_starter_price_id,
)
from libs.billing.member_common import MemberClient

logger = logging.getLogger(__name__)


# -----------------------------------------------------------------------------
# Helper: create member client with test clock
# -----------------------------------------------------------------------------

def _create_member_client(billing_client, billing_email_factory):
    """Create a MemberClient with the same test clock as billing_client."""
    billing_email_factory("member-test")  # call to ensure unique state
    client = MemberClient(
        base_url=billing_client.base_url,
        version=billing_client.version,
        clock_id=billing_client.clock_id,
        webhook_secret=billing_client.webhook_secret,
    )
    client.user_id = billing_client.user_id
    client.tenant_id = billing_client.tenant_id
    client.customer_id = billing_client.customer_id
    client.auth_header = billing_client.auth_header
    return client


def _register_and_invite(client, member_email, member_password):
    """Register a member user and invite them to the tenant."""
    client.register_member_only(member_email, member_password)
    result = client.invite_member(member_email)
    assert result.get("code") == 0, f"Failed to invite member {member_email}: {result.get('message')}"
    return result


def _accept_invitation_as(client, member_email, member_password, target_tenant_id):
    """Create a member client and accept invitation."""
    member_client = MemberClient(
        base_url=client.base_url,
        version=client.version,
        clock_id="",
        webhook_secret=client.webhook_secret,
    )
    member_client.login_as_member(member_email, member_password)
    member_client.accept_invitation(target_tenant_id)
    return member_client


# -----------------------------------------------------------------------------
# MEMBER-01: Basic Member Quota Enforcement Test
# -----------------------------------------------------------------------------

@pytest.mark.billing
def test_member_01_trial_quota(billing_client, billing_email_factory):
    """MEMBER-01 Trial: Verify member quota enforcement on the runtime Trial quota.

    Trial plan allows only the owner at the current runtime quota. Inviting one
    more member and trying to accept should fail due to insufficient quota.
    """
    logger.info("=" * 80)
    logger.info("MEMBER-01 Trial: Testing member quota enforcement on Trial plan")
    logger.info("=" * 80)

    client = _create_member_client(billing_client, billing_email_factory)
    expected_quota = get_quota_members_limit("Trial")

    logger.info("Assert: Expected quota_members for Trial: %s", expected_quota)
    logger.info("Assert: Tenant ID: %s", client.tenant_id)
    logger.info("Assert: Customer ID: %s", client.customer_id)

    # Verify initial member count (should be 0, excluding owner)
    members = client.list_members()
    assert len(members) == 0, f"Expected 0 members (exclude owner), got {len(members)}"
    logger.info("Assert: Initial member count is 0 (exclude owner)")

    # Verify Trial quota from overview
    overview = client.get_member_quota_overview()
    assert overview, f"Members not found in billing overview: {overview}"
    quota_members = overview["limit"]
    assert quota_members == expected_quota, f"Expected Trial quota {expected_quota}, got {quota_members}"
    logger.info("Assert: Member quota from overview (Trial): %s", quota_members)

    # Trial only keeps the owner under the current quota.
    logger.info("Assert: No members to invite on Trial beyond owner-only quota")

    # Attempt to invite 1 member beyond quota - should fail on accept
    extra_email = f"test_member_extra-{uuid.uuid4().hex[:6]}@example.test"
    _register_and_invite(client, extra_email, "Test123456")
    logger.info("Assert: Extra member invited (quota check deferred to accept)")

    extra_client = MemberClient(
        base_url=client.base_url,
        version=client.version,
        clock_id=client.clock_id,
        webhook_secret=client.webhook_secret,
    )
    extra_client.login_as_member(extra_email, "Test123456")

    error_message = expect_failure_with_message(
        lambda: extra_client.accept_invitation(client.tenant_id),
        expected_substrings=("quota", "seats", "resource"),
        success_message="Extra member was incorrectly accepted (accept succeeded)",
        unexpected_message="Extra member was incorrectly processed",
    )
    logger.info("Assert: Correctly rejected extra member on accept: %s", error_message)

    logger.info("MEMBER-01 Trial PASSED")


@pytest.mark.billing
def test_member_01_starter_quota(billing_client, billing_email_factory):
    """MEMBER-01 Starter: Verify member quota enforcement on the runtime Starter quota.

    Starter plan allows up to the configured runtime member quota. The test
    fills `quota - 1` invite slots beyond the owner, then verifies one more
    invited member is rejected on accept.
    """
    logger.info("=" * 80)
    logger.info("MEMBER-01 Starter: Testing member quota enforcement on Starter plan")
    logger.info("=" * 80)

    client = _create_member_client(billing_client, billing_email_factory)
    expected_quota = get_quota_members_limit("Starter")

    # Upgrade Trial -> Starter
    starter_result = client.upgrade_trial_to_starter()
    subscription_id = starter_result.get("subscription_id", "")
    logger.info("Assert: Upgraded to Starter, subscription_id: %s", subscription_id)

    logger.info("Assert: Expected quota_members for Starter: %s", expected_quota)
    logger.info("Assert: Tenant ID: %s", client.tenant_id)

    # Verify initial member count (should be 0, excluding owner)
    members = client.list_members()
    assert len(members) == 0, f"Expected 0 members (exclude owner), got {len(members)}"
    logger.info("Assert: Initial member count is 0 (exclude owner)")

    # Verify Starter quota from overview
    overview = client.get_member_quota_overview()
    assert overview, f"Members not found in billing overview: {overview}"
    quota_members = overview["limit"]
    assert quota_members == expected_quota, f"Expected Starter quota {expected_quota}, got {quota_members}"
    logger.info("Assert: Member quota from overview (Starter): %s", quota_members)

    # Invite members up to quota limit
    invite_count = expected_quota - 1
    logger.info("Assert: Will invite %s members (quota %s - owner)", invite_count, expected_quota)

    test_emails = [f"test_member{idx}-{uuid.uuid4().hex[:6]}@example.test" for idx in range(1, invite_count + 1)]
    member_password = "Test123456"

    for i, member_email in enumerate(test_emails, 1):
        _register_and_invite(client, member_email, member_password)
        logger.info("Assert: Invited member %s: %s", i, member_email)
        _accept_invitation_as(client, member_email, member_password, client.tenant_id)
        logger.info("Assert: Member %s accepted invitation", i)

    # Verify total member count
    members = client.list_members()
    assert len(members) == invite_count, f"Expected {invite_count} members, got {len(members)}"
    logger.info("Assert: Total members after invitations: %s/%s (including owner)", len(members) + 1, expected_quota)

    # Attempt to invite one more member (should fail - exceeds quota)
    extra_email = f"test_member_extra-{uuid.uuid4().hex[:6]}@example.test"
    _register_and_invite(client, extra_email, member_password)
    logger.info("Assert: Extra member invited (quota check deferred to accept)")

    extra_client = MemberClient(
        base_url=client.base_url,
        version=client.version,
        clock_id=client.clock_id,
        webhook_secret=client.webhook_secret,
    )
    extra_client.login_as_member(extra_email, member_password)

    error_message = expect_failure_with_message(
        lambda: extra_client.accept_invitation(client.tenant_id),
        expected_substrings=("quota", "seats", "resource"),
        success_message="Extra member was incorrectly accepted (accept succeeded)",
        unexpected_message="Extra member was incorrectly processed",
    )
    logger.info("Assert: Correctly rejected extra member on accept: %s", error_message)

    logger.info("MEMBER-01 Starter PASSED")


@pytest.mark.billing
def test_member_01_pro_quota(billing_client, billing_email_factory):
    """MEMBER-01 Pro: Verify member quota behavior on the runtime Pro quota.

    To keep CI execution time reasonable, this case samples a subset of the Pro
    quota instead of exhausting it, and confirms additional sampled invites can
    still be accepted while remaining within the runtime quota.
    """
    logger.info("=" * 80)
    logger.info("MEMBER-01 Pro: Testing member quota enforcement on Pro plan")
    logger.info("=" * 80)

    client = _create_member_client(billing_client, billing_email_factory)
    expected_quota = get_quota_members_limit("Pro")

    # Upgrade Trial -> Starter -> Pro
    starter_result = client.upgrade_trial_to_starter()
    starter_subscription_id = starter_result.get("subscription_id", "")
    client.upgrade_starter_to_pro(starter_subscription_id)
    logger.info("Assert: Upgraded to Pro")

    logger.info("Assert: Expected quota_members for Pro: %s", expected_quota)
    logger.info("Assert: Tenant ID: %s", client.tenant_id)

    # Verify initial member count (should be 0, excluding owner)
    members = client.list_members()
    assert len(members) == 0, f"Expected 0 members (exclude owner), got {len(members)}"
    logger.info("Assert: Initial member count is 0 (exclude owner)")

    # Verify Pro quota from overview
    overview = client.get_member_quota_overview()
    assert overview, f"Members not found in billing overview: {overview}"
    quota_members = overview["limit"]
    assert quota_members == expected_quota, f"Expected Pro quota {expected_quota}, got {quota_members}"
    logger.info("Assert: Member quota from overview (Pro): %s", quota_members)

    # For Pro, test with fewer members to keep test time reasonable while
    # preserving at least one free seat for the extra sampled acceptance.
    invite_count = min(5, expected_quota - 2)
    assert invite_count >= 1, f"Pro member quota must allow at least 2 invited seats, got {expected_quota}"
    logger.info("Assert: Will invite %s members (testing quota enforcement)", invite_count)

    test_emails = [f"test_member{idx}-{uuid.uuid4().hex[:6]}@example.test" for idx in range(1, invite_count + 1)]
    member_password = "Test123456"

    for i, member_email in enumerate(test_emails, 1):
        _register_and_invite(client, member_email, member_password)
        logger.info("Assert: Invited member %s: %s", i, member_email)
        _accept_invitation_as(client, member_email, member_password, client.tenant_id)
        logger.info("Assert: Member %s accepted invitation", i)

    # Verify total member count
    members = client.list_members()
    assert len(members) == invite_count, f"Expected {invite_count} members, got {len(members)}"
    logger.info("Assert: Total members after invitations: %s/%s (including owner)", len(members) + 1, expected_quota)

    # Invite one more sampled member; this should still succeed because we are
    # intentionally not exhausting the full Pro quota in CI.
    extra_email = f"test_member_extra-{uuid.uuid4().hex[:6]}@example.test"
    _register_and_invite(client, extra_email, member_password)
    logger.info("Assert: Extra sampled member invited")

    extra_client = MemberClient(
        base_url=client.base_url,
        version=client.version,
        clock_id=client.clock_id,
        webhook_secret=client.webhook_secret,
    )
    extra_client.login_as_member(extra_email, member_password)
    extra_client.accept_invitation(client.tenant_id)
    logger.info("Assert: Extra sampled member accepted within Pro quota")

    members = client.list_members()
    assert len(members) == invite_count + 1, (
        f"Expected {invite_count + 1} members after extra sampled invite, got {len(members)}"
    )
    logger.info(
        "Assert: Total sampled members after extra acceptance: %s/%s (including owner)",
        len(members) + 1,
        expected_quota,
    )

    logger.info("MEMBER-01 Pro PASSED")


# -----------------------------------------------------------------------------
# MEMBER-02: Trial Member Quota Rejection and Starter Acceptance Test
# -----------------------------------------------------------------------------

@pytest.mark.billing
def test_member_02_trial_rejection_starter_acceptance(billing_client, billing_email_factory):
    """MEMBER-02: Verify member is rejected on Trial but accepted after upgrade to Starter.

    Test Scenarios:
    1. Start with Trial plan at the runtime member quota (owner only initially)
    2. Invite a member, then try to accept - should be rejected due to insufficient quota
    3. Upgrade to Starter plan at the runtime member quota
    4. The previously rejected member tries to accept again - should succeed
    5. Add more members to verify Starter quota allows additional members
    """
    logger.info("=" * 80)
    logger.info("MEMBER-02: Trial Rejection and Starter Acceptance Test")
    logger.info("=" * 80)

    client = _create_member_client(billing_client, billing_email_factory)
    trial_quota = get_quota_members_limit("Trial")
    starter_quota = get_quota_members_limit("Starter")

    logger.info("Assert: Trial quota_members: %s", trial_quota)
    logger.info("Assert: Starter quota_members: %s", starter_quota)
    logger.info("Assert: Tenant ID: %s", client.tenant_id)

    # Verify Trial quota
    overview = client.get_member_quota_overview()
    assert overview, f"Members not found in billing overview: {overview}"
    quota_members = overview["limit"]
    assert quota_members == trial_quota, f"Expected Trial quota {trial_quota}, got {quota_members}"
    logger.info("Assert: Trial member quota verified: %s", quota_members)

    # Step a: Invite a member on Trial plan, accept should be rejected
    logger.info("=" * 80)
    logger.info("Step a: Invite member on Trial plan, accept should be rejected")
    logger.info("=" * 80)

    member_email = f"test_member_trial-{uuid.uuid4().hex[:6]}@example.test"
    member_password = "Test123456"

    _register_and_invite(client, member_email, member_password)
    logger.info("Assert: Invited member: %s", member_email)

    error_message = expect_failure_with_message(
        lambda: _accept_invitation_as(client, member_email, member_password, client.tenant_id),
        expected_substrings=("quota", "seats", "resource"),
        success_message="Member was incorrectly accepted on Trial plan (should have been rejected)",
        unexpected_message="Member was incorrectly rejected with unexpected error",
    )
    logger.info("Assert: Correctly rejected member on Trial plan: %s", error_message)

    # Step b: Upgrade to Starter, previously rejected member should now succeed
    logger.info("=" * 80)
    logger.info("Step b: Upgrade to Starter, previously rejected member accepts again")
    logger.info("=" * 80)

    upgrade_result = client.upgrade_trial_to_starter()
    subscription_id = upgrade_result.get("subscription_id", "")
    logger.info("Assert: Upgraded to Starter, subscription_id: %s", subscription_id)

    # Verify Starter quota
    overview_starter = client.get_member_quota_overview()
    starter_quota_actual = overview_starter["limit"]
    assert starter_quota_actual == starter_quota, f"Expected Starter quota {starter_quota}, got {starter_quota_actual}"
    logger.info("Assert: Starter member quota verified: %s", starter_quota_actual)

    # The previously rejected member tries to accept again - should succeed now
    _accept_invitation_as(client, member_email, member_password, client.tenant_id)
    logger.info("Assert: Member successfully accepted invitation after Starter upgrade: %s", member_email)

    # Verify member count
    members = client.list_members()
    assert len(members) == 1, f"Expected 1 member after acceptance, got {len(members)}"
    logger.info("Assert: Member count after acceptance: %s/%s (including owner)", len(members) + 1, starter_quota)

    # Step c: Add more members to verify Starter quota allows additional members
    logger.info("=" * 80)
    logger.info("Step c: Add more members to verify Starter quota (up to %s total)", starter_quota)
    logger.info("=" * 80)

    additional_count = starter_quota - 2  # -1 for owner, -1 for existing member
    logger.info("Assert: Will invite %s more members", additional_count)

    for i in range(1, additional_count + 1):
        additional_email = f"test_member_starter{i}-{uuid.uuid4().hex[:6]}@example.test"
        _register_and_invite(client, additional_email, member_password)
        logger.info("Assert: Invited member %s: %s", i, additional_email)
        _accept_invitation_as(client, additional_email, member_password, client.tenant_id)
        logger.info("Assert: Member %s accepted invitation: %s", i, additional_email)

    # Verify final member count
    members_final = client.list_members()
    expected_total = starter_quota - 1  # excluding owner
    assert len(members_final) == expected_total, f"Expected {expected_total} members, got {len(members_final)}"
    logger.info("Assert: Final member count: %s/%s (including owner)", len(members_final) + 1, starter_quota)

    logger.info("MEMBER-02 PASSED")


# -----------------------------------------------------------------------------
# MEMBER-03: Remove Member to Enable New Member Acceptance Test
# -----------------------------------------------------------------------------

@pytest.mark.billing
def test_member_03_remove_to_enable_acceptance(billing_client, billing_email_factory):
    """MEMBER-03: Verify that removing a member enables a previously rejected member to accept.

    Test Scenarios:
    1. Start with Starter plan and fill all runtime member slots
    2. Invite an additional member beyond quota - accept should be rejected
    3. Randomly remove one existing member to free up a slot
    4. The previously rejected member can now successfully accept the invitation
    """
    logger.info("=" * 80)
    logger.info("MEMBER-03: Remove Member to Enable New Member Acceptance Test")
    logger.info("=" * 80)

    client = _create_member_client(billing_client, billing_email_factory)
    starter_quota = get_quota_members_limit("Starter")

    # Upgrade to Starter plan
    upgrade_result = client.upgrade_trial_to_starter()
    subscription_id = upgrade_result.get("subscription_id", "")
    logger.info("Assert: Upgraded to Starter, subscription_id: %s", subscription_id)
    logger.info("Assert: Starter quota_members: %s", starter_quota)
    logger.info("Assert: Tenant ID: %s", client.tenant_id)

    # Verify initial member count (should be 0, excluding owner)
    members = client.list_members()
    assert len(members) == 0, f"Expected 0 members (exclude owner), got {len(members)}"
    logger.info("Assert: Initial member count is 0 (exclude owner)")

    # Verify Starter quota
    overview = client.get_member_quota_overview()
    assert overview, f"Members not found in billing overview: {overview}"
    quota_members = overview["limit"]
    assert quota_members == starter_quota, f"Expected Starter quota {starter_quota}, got {quota_members}"
    logger.info("Assert: Starter member quota verified: %s", quota_members)

    # Step a: Fill all member slots (Starter quota)
    logger.info("=" * 80)
    logger.info("Step a: Fill all %s member slots (including owner)", starter_quota)
    logger.info("=" * 80)

    member_password = "Test123456"
    invited_members = []

    invite_count = starter_quota - 1
    test_emails = [f"test_member{idx}-{uuid.uuid4().hex[:6]}@example.test" for idx in range(1, invite_count + 1)]

    for i, member_email in enumerate(test_emails, 1):
        _register_and_invite(client, member_email, member_password)
        logger.info("Assert: Invited member %s: %s", i, member_email)
        _accept_invitation_as(client, member_email, member_password, client.tenant_id)
        logger.info("Assert: Member %s accepted invitation: %s", i, member_email)
        invited_members.append(member_email)

    # Verify total member count
    normal_members = [m for m in client.list_members() if m["role"] != UserTenantRole.INVITE]
    assert len(normal_members) == invite_count, f"Expected {invite_count} members, got {len(normal_members)}"
    logger.info("Assert: Total normal members after filling quota: %s/%s (including owner)", len(normal_members) + 1, starter_quota)

    # Step b: Invite an additional member - accept should be rejected
    logger.info("=" * 80)
    logger.info("Step b: Invite additional member beyond quota - accept should be rejected")
    logger.info("=" * 80)

    extra_email = f"test_member_extra-{uuid.uuid4().hex[:6]}@example.test"
    _register_and_invite(client, extra_email, member_password)
    logger.info("Assert: Extra member invited (quota check deferred to accept)")

    error_message = expect_failure_with_message(
        lambda: _accept_invitation_as(client, extra_email, member_password, client.tenant_id),
        expected_substrings=("quota", "seats", "resource"),
        success_message="Extra member was incorrectly accepted (should have been rejected due to quota)",
        unexpected_message="Extra member was incorrectly rejected with unexpected error",
    )
    logger.info("Assert: Correctly rejected extra member on accept: %s", error_message)

    # Step c: Randomly remove one existing member
    logger.info("=" * 80)
    logger.info("Step c: Randomly remove one existing member to free up a slot")
    logger.info("=" * 80)

    members_before_removal = [m for m in client.list_members() if m["role"] != UserTenantRole.INVITE]
    logger.info("Assert: Members before removal: %s", len(members_before_removal))

    member_to_remove = random.choice(members_before_removal)
    member_to_remove_id = member_to_remove.get("user_id") or member_to_remove.get("id")
    member_to_remove_email = member_to_remove.get("email", "unknown")

    assert member_to_remove_id, f"Cannot find user_id in member data: {member_to_remove}"
    logger.info("Assert: Randomly selected member to remove: %s (id: %s)", member_to_remove_email, member_to_remove_id)

    remove_result = client.remove_member(client.tenant_id, member_to_remove_id)
    assert remove_result.get("code") == 0, f"Failed to remove member: {remove_result.get('message')}"
    logger.info("Assert: Successfully removed member: %s", member_to_remove_email)

    # Verify member count after removal
    members_after_removal = [m for m in client.list_members() if m["role"] != UserTenantRole.INVITE]
    expected_count = invite_count - 1
    assert len(members_after_removal) == expected_count, f"Expected {expected_count} members after removal, got {len(members_after_removal)}"
    logger.info("Assert: Members after removal: %s/%s (including owner)", len(members_after_removal) + 1, starter_quota)

    # Step d: Previously rejected member can now accept successfully
    logger.info("=" * 80)
    logger.info("Step d: Previously rejected member accepts successfully")
    logger.info("=" * 80)

    _accept_invitation_as(client, extra_email, member_password, client.tenant_id)
    logger.info("Assert: Extra member successfully accepted invitation after member removal: %s", extra_email)

    # Verify final member count
    members_final = client.list_members()
    expected_final_count = invite_count  # Same as before (one removed, one added)
    assert len(members_final) == expected_final_count, f"Expected {expected_final_count} members final, got {len(members_final)}"
    logger.info("Assert: Final member count: %s/%s (including owner)", len(members_final) + 1, starter_quota)

    logger.info("MEMBER-03 PASSED")


# -----------------------------------------------------------------------------
# MEMBER-04: Pro Plan Downgrade Enforcement Test
# -----------------------------------------------------------------------------

@pytest.mark.billing
def test_member_04_pro_downgrade_enforcement(billing_client, billing_email_factory):
    """MEMBER-04: Verify member quota enforcement when downgrading from Pro to Starter.

    Test Scenarios:
    1. Start with Pro plan at the runtime Pro member quota
    2. Add members up to Starter quota + 1 including the owner
    3. Attempt to downgrade to Starter - should fail due to exceeding member quota
    4. Randomly remove one member so usage falls back within Starter quota
    5. Attempt to downgrade to Starter again - should succeed
    """
    logger.info("=" * 80)
    logger.info("MEMBER-04: Pro Plan Downgrade Enforcement Test")
    logger.info("=" * 80)

    client = _create_member_client(billing_client, billing_email_factory)
    starter_quota = get_quota_members_limit("Starter")
    pro_quota = get_quota_members_limit("Pro")

    logger.info("Assert: Expected quota_members for Starter: %s", starter_quota)
    logger.info("Assert: Expected quota_members for Pro: %s", pro_quota)

    # Upgrade Trial -> Starter -> Pro
    starter_result = client.upgrade_trial_to_starter()
    starter_subscription_id = starter_result.get("subscription_id", "")
    pro_result = client.upgrade_starter_to_pro(starter_subscription_id)
    subscription_id = pro_result.get("subscription_id", "")
    logger.info("Assert: Pro subscription ID: %s", subscription_id)
    logger.info("Assert: Tenant ID: %s", client.tenant_id)

    # Verify initial member count (should be 0, excluding owner)
    members = client.list_members()
    assert len(members) == 0, f"Expected 0 members (exclude owner), got {len(members)}"
    logger.info("Assert: Initial member count is 0 (exclude owner)")

    # Verify Pro plan quota
    overview = client.get_member_quota_overview()
    assert overview, f"Members not found in billing overview: {overview}"
    quota_members = overview["limit"]
    assert quota_members == pro_quota, f"Expected Pro quota {pro_quota}, got {quota_members}"
    logger.info("Assert: Member quota from overview (Pro): %s", quota_members)

    # Invite members up to Starter quota + 1
    invite_count = starter_quota
    logger.info("Assert: Will invite %s members (total will be %s, exceeding Starter quota of %s)", invite_count, invite_count + 1, starter_quota)

    test_emails = [f"test_member{idx}-{uuid.uuid4().hex[:6]}@example.test" for idx in range(1, invite_count + 1)]
    member_password = "Test123456"
    invited_user_ids: list = []

    for i, member_email in enumerate(test_emails, 1):
        member_data = client.register_member_only(member_email, member_password)["data"]
        logger.info("Assert: Registered member user %s: %s", i, member_email)

        result = client.invite_member(member_email)
        assert result.get("code") == 0, f"Failed to invite member {i}: {member_email} - {result.get('message')}"
        logger.info("Assert: Invited member %s: %s", i, member_email)

        _accept_invitation_as(client, member_email, member_password, client.tenant_id)
        logger.info("Assert: Member %s accepted invitation via dedicated client", i)
        invited_user_ids.append(member_data["id"])

    # Verify total member count (including owner)
    members = client.list_members()
    total_members = len(members) + 1  # +1 for owner
    assert total_members == invite_count + 1, f"Expected {invite_count + 1} total members, got {total_members}"
    logger.info("Assert: Total members: %s/%s (Pro quota), exceeding Starter quota of %s", total_members, pro_quota, starter_quota)

    # Attempt to downgrade to Starter (should fail)
    logger.info("Assert: Attempting to downgrade from Pro to Starter (should fail)")
    starter_price_id = get_starter_price_id()

    error_message = expect_failure_with_message(
        lambda: client.schedule_plan_change(starter_price_id),
        expected_substrings=("quota", "member", "seat"),
        success_message="Downgrade should have been rejected due to member quota",
        unexpected_message="Unexpected error during downgrade",
    )
    logger.info("Assert: Downgrade correctly rejected due to member quota: %s", error_message)

    # Now randomly remove one member
    logger.info("Assert: Randomly removing one member to allow downgrade")
    assert invited_user_ids, "No invited user IDs to remove"

    removed_user_id = random.choice(invited_user_ids)
    invited_user_ids.remove(removed_user_id)

    remove_result = client.remove_member(client.tenant_id, removed_user_id)
    logger.info("Assert: Removed member user_id: %s, remove_result: %s", removed_user_id, remove_result)

    # Verify member count after removal
    members = client.list_members()
    total_members_after_removal = len(members) + 1  # +1 for owner
    assert total_members_after_removal == invite_count, f"Expected {invite_count} total members after removal, got {total_members_after_removal}"
    logger.info("Assert: Total members after removal: %s (within Starter quota of %s)", total_members_after_removal, starter_quota)

    # Attempt to downgrade to Starter again (should succeed)
    logger.info("Assert: Attempting to downgrade from Pro to Starter (should succeed)")

    downgrade_result = fail_on_flow_error(
        "Downgrade should have submitted after member removal",
        lambda: client.schedule_plan_change(starter_price_id),
    )
    logger.info("Assert: Downgrade request submitted: %s", downgrade_result)

    logger.info("MEMBER-04 PASSED")


# -----------------------------------------------------------------------------
# MEMBER-05: Member Quota Persistence Through Upgrade/Downgrade Test
# -----------------------------------------------------------------------------

@pytest.mark.billing
def test_member_05_persistence_upgrade_downgrade(billing_client, billing_email_factory):
    """MEMBER-05: Verify members are retained when upgrading and downgrading plans.

    Test Scenarios:
    1. Start with Starter plan
    2. Add a small runtime-safe number of members within Starter quota
    3. Upgrade to Pro plan - verify members remain unchanged
    4. Downgrade back to Starter - verify members remain unchanged while still within Starter quota
    """
    logger.info("=" * 80)
    logger.info("MEMBER-05: Member persistence through Starter -> Pro -> Starter upgrade/downgrade")
    logger.info("=" * 80)

    client = _create_member_client(billing_client, billing_email_factory)
    starter_quota = get_quota_members_limit("Starter")
    pro_quota = get_quota_members_limit("Pro")

    logger.info("Assert: Expected quota_members for Starter: %s", starter_quota)
    logger.info("Assert: Expected quota_members for Pro: %s", pro_quota)

    # Upgrade Trial -> Starter
    starter_result = client.upgrade_trial_to_starter()
    subscription_id = starter_result.get("subscription_id", "")
    logger.info("Assert: Upgraded Trial -> Starter, subscription_id: %s", subscription_id)
    logger.info("Assert: Tenant ID: %s", client.tenant_id)
    logger.info("Assert: Customer ID: %s", client.customer_id)

    # Verify initial member count (should be 0, excluding owner)
    members = client.list_members()
    assert len(members) == 0, f"Expected 0 members (exclude owner), got {len(members)}"
    logger.info("Assert: Initial member count is 0 (exclude owner)")

    # Verify Starter plan quota
    overview = client.get_member_quota_overview()
    assert overview, f"Members not found in billing overview: {overview}"
    quota_members = overview["limit"]
    assert quota_members == starter_quota, f"Expected Starter quota {starter_quota}, got {quota_members} from overview"
    logger.info("Assert: Member quota from overview (Starter): %s", quota_members)

    # Add a small number of members while remaining within Starter quota.
    invite_count = min(3, starter_quota - 1)
    assert invite_count >= 1, f"Starter member quota must allow at least one invited member, got {starter_quota}"
    logger.info("Assert: Inviting %s members (within Starter quota of %s)", invite_count, starter_quota)

    test_emails = [f"test_member{idx}-{uuid.uuid4().hex[:6]}@example.test" for idx in range(1, invite_count + 1)]
    member_password = "Test123456"

    for i, member_email in enumerate(test_emails, 1):
        client.register_member_only(member_email, member_password)
        logger.info("Assert: Registered member user %s: %s", i, member_email)

        result = client.invite_member(member_email)
        assert result.get("code") == 0, f"Failed to invite member {i}: {member_email} - {result.get('message')}"
        logger.info("Assert: Invited member %s: %s", i, member_email)

        _accept_invitation_as(client, member_email, member_password, client.tenant_id)
        logger.info("Assert: Member %s accepted invitation via dedicated client", i)

    # Verify total member count (including owner)
    members = client.list_members()
    total_members = len(members) + 1  # +1 for owner
    assert total_members == invite_count + 1, f"Expected {invite_count + 1} total members, got {total_members}"
    logger.info("Assert: Total members after invitations: %s/%s (Starter quota)", total_members, starter_quota)

    # Step: Upgrade to Pro plan
    logger.info("Assert: Upgrading from Starter to Pro")
    upgrade_result = client.upgrade_starter_to_pro(subscription_id)
    new_subscription_id = upgrade_result.get("subscription_id", "")
    logger.info("Assert: Upgrade to Pro succeeded, new subscription ID: %s", new_subscription_id)

    # Verify members remain unchanged after upgrade
    members_after_upgrade = client.list_members()
    total_members_after_upgrade = len(members_after_upgrade) + 1  # +1 for owner
    assert total_members_after_upgrade == total_members, f"Expected {total_members} members after upgrade, got {total_members_after_upgrade}"
    logger.info("Assert: Members after upgrade to Pro: %s (unchanged)", total_members_after_upgrade)

    # Verify Pro plan quota
    overview_pro = client.get_member_quota_overview()
    quota_members_pro = overview_pro["limit"]
    assert quota_members_pro == pro_quota, f"Expected Pro quota {pro_quota}, got {quota_members_pro} from overview"
    logger.info("Assert: Member quota from overview (Pro): %s", quota_members_pro)

    # Step: Downgrade back to Starter plan
    logger.info("Assert: Downgrading from Pro to Starter")
    starter_price_id = get_starter_price_id()
    subscription_ids = {new_subscription_id}

    downgrade_created_gte = int(time.time()) - 5
    downgrade_result = fail_on_flow_error(
        f"Downgrade should have succeeded (members {total_members_after_upgrade - 1} <= Starter quota {starter_quota})",
        lambda: client.schedule_plan_change(starter_price_id),
    )
    logger.info("Assert: Downgrade to Starter requested: %s", downgrade_result)

    # Verify members remain unchanged after scheduling downgrade
    members_after_downgrade = client.list_members()
    total_members_after_downgrade = len(members_after_downgrade) + 1  # +1 for owner
    assert total_members_after_downgrade == total_members, f"Expected {total_members} members after scheduling downgrade, got {total_members_after_downgrade}"
    logger.info("Assert: Members after scheduling downgrade: %s (unchanged)", total_members_after_downgrade)

    # Advance clock to plan end to apply the downgrade
    logger.info("Assert: Advancing clock to plan end for downgrade to take effect")
    client.advance_clock_to_plan_end()

    # Sync webhooks after clock advance
    client.sync_webhooks(
        subscription_ids=subscription_ids,
        created_gte=downgrade_created_gte,
        wait_seconds=8,
    )
    logger.info("Assert: Webhooks synced after clock advance")

    # Wait for plan to become Starter
    logger.info("Waiting for plan to become Starter")
    client.wait_for_plan("Starter")
    logger.info("Assert: Plan is now Starter")

    # Verify members remain unchanged after downgrade takes effect
    members_after_downgrade_effective = client.list_members()
    total_members_after_downgrade_effective = len(members_after_downgrade_effective) + 1  # +1 for owner
    assert total_members_after_downgrade_effective == total_members, f"Expected {total_members} members after downgrade takes effect, got {total_members_after_downgrade_effective}"
    logger.info("Assert: Members after downgrade takes effect: %s (unchanged)", total_members_after_downgrade_effective)

    # Verify Starter plan quota
    overview_starter = client.get_member_quota_overview()
    quota_members_starter = overview_starter["limit"]
    assert quota_members_starter == starter_quota, f"Expected Starter quota {starter_quota}, got {quota_members_starter} from overview"
    logger.info("Assert: Member quota from overview (Starter): %s", quota_members_starter)

    logger.info("MEMBER-05 PASSED")
