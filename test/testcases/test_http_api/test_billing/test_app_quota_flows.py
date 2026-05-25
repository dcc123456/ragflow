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
Pytest wrapper for APP-01 and APP-02 billing app quota flows.

APP-01: Basic App Quota Enforcement Test - Trial -> Starter upgrade
APP-02: Downgrade Blocked by Resource Usage Test - Starter -> Trial blocked by apps
"""

from __future__ import annotations

import logging
import time
import uuid

import pytest

from test.testcases.test_http_api.test_billing.assertions import expect_failure_with_message
from libs.billing.app_common import AppClient
from libs.billing.billing_common import (
    first_plan_price_id,
    get_starter_quota_apps,
    get_trial_quota_apps,
    load_billing_config,
    stripe_dict,
)

logger = logging.getLogger(__name__)


# -----------------------------------------------------------------------------
# AppClient fixture
# -----------------------------------------------------------------------------

@pytest.fixture
def app_client(
    billing_runtime_config,
    billing_test_args,
    billing_email_factory,
):
    """Create an AppClient with Stripe test clock lifecycle.

    The AppClient extends BillingClient with SDK Bearer token auth for
    dataset/chat/agent/canvas operations.
    """
    import stripe

    # Configure Stripe API key and version
    stripe.api_key = billing_runtime_config["stripe_api_key"]
    stripe.api_version = billing_runtime_config["stripe_api_version"]

    # Use the same test clock as billing_client would create
    clock = stripe.test_helpers.TestClock.create(
        frozen_time=int(time.time()),
        name=f"app-{uuid.uuid4().hex[:8]}",
    )
    clock_id = clock.id

    # Wait for clock to be ready
    deadline = time.time() + 180
    while time.time() < deadline:
        clock_dict = stripe_dict(clock)
        if clock_dict.get("status") == "ready":
            logger.debug("app test clock ready: %s", clock_id)
            break
        time.sleep(1)

    email = billing_email_factory("app-client")
    client = AppClient(
        base_url=billing_test_args.base_url,
        version=billing_test_args.version,
        clock_id=clock_id,
        webhook_secret=billing_runtime_config["webhook_secret"],
    )
    client.wait_until_ready(billing_test_args.ready_timeout_seconds)

    # Bootstrap: register and login
    from libs.billing.storage_common import attach_default_test_card
    user_id, tenant_id = client.register_and_login(email, "Test1234!")
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

    # Initialize SDK Bearer token for app operations
    client.init_sdk_token()

    yield client

    # Cleanup
    try:
        from libs.billing.billing_common import delete_clock
        delete_clock(clock_id)
    except Exception as exc:
        logger.warning("Failed to delete app test clock %s: %s", clock_id, exc)
    if client.session:
        client.session.close()


# -----------------------------------------------------------------------------
# APP-01: Basic App Quota Enforcement Test
# -----------------------------------------------------------------------------

@pytest.mark.billing
def test_app_01_basic_quota_enforcement(app_client):
    """APP-01: Basic app quota enforcement test - Trial -> Starter upgrade.

    Test Scenarios:
    1. Start with Trial plan and fill it exactly to the runtime quota.
    2. Try to create one more app immediately - should fail.
    3. Upgrade to Starter plan - upgrade should take effect immediately.
    4. Keep creating app resources until Starter quota is reached.
    5. Try to create one more app on Starter - should fail.
    """
    logger.info("=" * 80)
    logger.info("APP-01: Basic App Quota Enforcement Test - Trial -> Starter upgrade")
    logger.info("=" * 80)

    client = app_client
    trial_quota_apps = get_trial_quota_apps()
    starter_quota_apps = get_starter_quota_apps()

    logger.info("Assert: Expected quota_apps for Trial: %s", trial_quota_apps)
    logger.info("Assert: Expected quota_apps for Starter: %s", starter_quota_apps)
    logger.info("Assert: Tenant ID: %s", client.tenant_id)
    logger.info("Assert: Customer ID: %s", client.customer_id)

    # Verify Trial plan quota
    apps_quota = client.get_apps_quota_overview()
    assert apps_quota.get("limit") == trial_quota_apps, f"Expected Trial apps quota {trial_quota_apps}, got {apps_quota.get('limit')}"
    logger.info("Assert: Apps quota from overview (Trial): %s/%s", apps_quota.get('used'), apps_quota.get('limit'))

    assert trial_quota_apps >= 1, f"Trial apps quota must be >= 1, got {trial_quota_apps}"
    assert starter_quota_apps > trial_quota_apps, (
        f"Starter apps quota must be greater than Trial for APP-01, got Trial={trial_quota_apps}, Starter={starter_quota_apps}"
    )

    trial_dataset_count = max(trial_quota_apps - 1, 0)

    # Step 1: Create datasets up to Trial quota - 1
    logger.info(
        "Assert: Creating %s datasets to leave 1 slot for a chat under Trial quota %s",
        trial_dataset_count,
        trial_quota_apps,
    )
    dataset_ids = []
    for i in range(1, trial_dataset_count + 1):
        dataset_name = f"test-dataset-{i}-{uuid.uuid4().hex[:6]}"
        result = client.create_dataset(dataset_name)
        assert result.get("code") == 0, f"Failed to create dataset {i}: {result.get('message')}"
        dataset_id = result.get("data", {}).get("id")
        dataset_ids.append(dataset_id)
        logger.info("Assert: Dataset %s created: %s", i, dataset_id)

    # Step 2: Create 1 chat to reach the Trial limit exactly
    logger.info("Assert: Creating 1st chat (at Trial quota limit of %s)", trial_quota_apps)
    chat1_result = client.create_chat("test-chat-1", dataset_ids=[])
    assert chat1_result.get("code") == 0, f"Failed to create 1st chat: {chat1_result.get('message')}"
    chat1_id = chat1_result.get("data", {}).get("id")
    logger.info("Assert: 1st chat created: %s", chat1_id)

    # Verify apps quota usage
    apps_quota_after = client.get_apps_quota_overview()
    logger.info(
        "Assert: Apps quota after filling Trial: used=%s, limit=%s",
        apps_quota_after.get("used"),
        apps_quota_after.get("limit"),
    )

    # Step 3: Try to create one more app - should fail
    logger.info("Assert: Attempting to create 2nd chat (should fail - Trial quota exceeded)")
    error_message = expect_failure_with_message(
        lambda: client.create_chat("test-chat-2", dataset_ids=[]),
        expected_substrings=("quota", "insufficient"),
        success_message="2nd chat was incorrectly created when quota should be exceeded",
        unexpected_message="Unexpected error creating 2nd chat",
    )
    logger.info("Assert: 2nd chat correctly rejected due to quota: %s", error_message)

    # Step 4: Upgrade to Starter plan
    logger.info("Assert: Upgrading from Trial to Starter")
    starter_result = client.upgrade_trial_to_starter()
    subscription_id = starter_result.get("subscription_id", "")
    logger.info("Assert: Starter subscription: %s", subscription_id)

    # Wait for plan to become Starter
    client.wait_for_plan("Starter")
    logger.info("Assert: Plan is now Starter")

    # Verify Starter plan quota
    apps_quota_starter = client.get_apps_quota_overview()
    assert apps_quota_starter.get("limit") == starter_quota_apps, f"Expected Starter apps quota {starter_quota_apps}, got {apps_quota_starter.get('limit')}"
    logger.info("Assert: Apps quota from overview (Starter): %s", apps_quota_starter.get('limit'))

    # Step 5: Create additional app resources until Starter quota is full
    starter_remaining = starter_quota_apps - trial_quota_apps
    assert starter_remaining >= 1, "Starter quota should provide at least one more app slot than Trial"
    creators = [
        ("chat", lambda idx: client.create_chat(f"test-chat-{idx}", dataset_ids=[])),
        ("agent", lambda idx: client.create_agent(f"test-agent-{idx}")),
        ("canvas", lambda idx: client.create_canvas(f"test-canvas-{idx}")),
        ("dataset", lambda idx: client.create_dataset(f"test-dataset-extra-{idx}-{uuid.uuid4().hex[:6]}")),
    ]
    created_after_upgrade = 0
    for name, creator in creators:
        if created_after_upgrade >= starter_remaining:
            break
        logger.info("Assert: Creating %s #%s to consume Starter headroom", name, created_after_upgrade + 1)
        result = creator(created_after_upgrade + 2)
        assert result.get("code") == 0, f"Failed to create {name} after upgrade: {result.get('message')}"
        created_after_upgrade += 1
        logger.info("Assert: %s created successfully after upgrade", name)

    assert created_after_upgrade == starter_remaining, (
        f"Expected to consume {starter_remaining} Starter slots, created {created_after_upgrade}"
    )

    # Step 6: One more app beyond Starter quota should fail
    logger.info("Assert: Attempting to exceed Starter quota with one more chat")
    error_message = expect_failure_with_message(
        lambda: client.create_chat("test-chat-overflow", dataset_ids=[]),
        expected_substrings=("quota", "insufficient", "limit"),
        success_message="Overflow chat was incorrectly created when Starter quota should be full",
        unexpected_message="Unexpected error while checking Starter quota overflow",
    )
    logger.info("Assert: Overflow app correctly rejected due to quota: %s", error_message)

    # Final verification
    apps_quota_final = client.get_apps_quota_overview()
    logger.info("Assert: Final apps quota: used=%s, limit=%s", apps_quota_final.get('used'), apps_quota_final.get('limit'))

    logger.info("APP-01 PASSED")


# -----------------------------------------------------------------------------
# APP-02: Downgrade Blocked by Resource Usage Test
# -----------------------------------------------------------------------------

@pytest.mark.billing
def test_app_02_downgrade_blocked_by_resource_usage(app_client):
    """APP-02: Downgrade blocked by resource usage test - Starter -> Trial.

    Test Scenarios:
    1. Start with Trial plan, upgrade to Starter.
    2. Create exactly Trial quota + 1 apps while staying within Starter quota.
    3. Try to downgrade to Trial - should fail because usage exceeds Trial quota.
    4. Delete one app so usage returns to Trial quota.
    5. Downgrade to Trial - should succeed.
    6. Advance clock to period end, wait for webhook synchronization.
    7. Verify plan is now Trial.
    """
    logger.info("=" * 80)
    logger.info("APP-02: Downgrade Blocked by Resource Usage Test - Starter -> Trial")
    logger.info("=" * 80)

    client = app_client
    trial_quota_apps = get_trial_quota_apps()
    starter_quota_apps = get_starter_quota_apps()

    logger.info("Assert: Expected quota_apps for Trial: %s", trial_quota_apps)
    logger.info("Assert: Expected quota_apps for Starter: %s", starter_quota_apps)
    logger.info("Assert: Tenant ID: %s", client.tenant_id)
    logger.info("Assert: Customer ID: %s", client.customer_id)

    assert starter_quota_apps > trial_quota_apps, (
        f"Starter apps quota must be greater than Trial for APP-02, got Trial={trial_quota_apps}, Starter={starter_quota_apps}"
    )

    # Upgrade to Starter
    starter_subscription_id: str = client.upgrade_trial_to_starter()["subscription_id"]
    logger.info("Assert: Starter subscription ID: %s", starter_subscription_id)

    # Verify Starter plan quota
    apps_quota = client.get_apps_quota_overview()
    assert apps_quota.get("limit") == starter_quota_apps, f"Expected Starter apps quota {starter_quota_apps}, got {apps_quota.get('limit')}"
    logger.info("Assert: Apps quota from overview (Starter): %s/%s", apps_quota.get('used'), apps_quota.get('limit'))

    target_apps = trial_quota_apps + 1
    dataset_target = max(target_apps - 1, 0)

    # Step 1: Create datasets up to target - 1
    logger.info("Assert: Creating %s datasets", dataset_target)
    dataset_ids = []
    for i in range(1, dataset_target + 1):
        dataset_name = f"test-dataset-{i}-{uuid.uuid4().hex[:6]}"
        result = client.create_dataset(dataset_name)
        assert result.get("code") == 0, f"Failed to create dataset {i}: {result.get('message')}"
        dataset_id = result.get("data", {}).get("id")
        dataset_ids.append(dataset_id)
        logger.info("Assert: Dataset %s created: %s", i, dataset_id)

    # Step 2: Create 1 chat to reach Trial quota + 1
    logger.info("Assert: Creating 1 chat to reach %s total apps", target_apps)
    result = client.create_chat("test-chat-1", dataset_ids=[])
    assert result.get("code") == 0, f"Failed to create chat: {result.get('message')}"
    chat_id = result.get("data", {}).get("id")
    logger.info("Assert: Chat created: %s", chat_id)

    # Verify total apps count
    apps_quota_after = client.get_apps_quota_overview()
    total_apps = apps_quota_after.get("used", 0)
    logger.info("Assert: Total apps after creation: %s", total_apps)

    # Step 3: Try to downgrade to Trial - should fail
    logger.info("Assert: Attempting to downgrade to Trial (should fail - apps exceed quota)")
    created_gte = int(time.time()) - 5

    billing_config = load_billing_config()
    trial_price_id = first_plan_price_id(billing_config, "Trial")
    assert trial_price_id, "Trial plan price_id not found in service_conf.yaml"

    error_message = expect_failure_with_message(
        lambda: client.schedule_plan_change(trial_price_id),
        expected_substrings=("quota", "insufficient", "exceed", "resource"),
        success_message="Downgrade should have been rejected due to resource usage exceeding Trial quota",
        unexpected_message="Unexpected error during downgrade",
    )
    logger.info("Assert: Downgrade correctly rejected due to resource usage: %s", error_message)

    # Step 4: Delete one dataset so total apps returns to Trial quota exactly
    logger.info("Assert: Deleting one dataset to reduce apps count")
    dataset_to_delete = dataset_ids.pop()
    delete_result = client.delete_dataset(dataset_to_delete)
    assert delete_result.get("code") == 0, f"Failed to delete dataset: {delete_result.get('message')}"
    logger.info("Assert: Dataset deleted: %s", dataset_to_delete)

    # Verify apps quota after deletion
    apps_quota_after_delete = client.get_apps_quota_overview()
    total_apps_after_delete = apps_quota_after_delete.get("used", 0)
    logger.info("Assert: Total apps after deletion: %s", total_apps_after_delete)

    # Step 5: Downgrade to Trial - should succeed now
    logger.info("Assert: Attempting to downgrade to Trial (should succeed)")
    downgrade_result = client.downgrade_to_trial(starter_subscription_id)
    logger.info("Assert: Downgrade to Trial scheduled: %s", downgrade_result)

    # Verify pending downgrade appears in current_plan
    current_plan = client.current_plan()
    pending_change = current_plan.get("pending_subscription_change", {})
    if not pending_change:
        logger.warning("No pending_subscription_change in current_plan, but downgrade was scheduled")
    else:
        logger.info("Assert: Pending downgrade confirmed: %s", pending_change)

    # Step 6: Advance clock to period end
    logger.info("Assert: Advancing clock to period end")
    client.advance_clock_to_plan_end()
    logger.info("Assert: Clock advanced past period end")

    # Wait for Stripe CLI forwarded webhooks to be processed.
    logger.info("Assert: Waiting for webhook synchronization")
    replayed = client.sync_webhooks(
        subscription_ids={starter_subscription_id},
        created_gte=created_gte,
    )
    logger.info("Assert: Webhook synchronization finished: %s replayed events", replayed)

    # Step 7: Verify plan is now Trial
    logger.info("Assert: Verifying plan is now Trial")
    final_plan = client.wait_for_plan("Trial")
    plan_name = final_plan.get("plan_name", "")
    assert plan_name == "Trial", f"Expected Trial plan after downgrade, got {plan_name}"
    logger.info("Assert: Plan is now Trial: %s", plan_name)

    # Verify Trial plan quota
    apps_quota_final = client.get_apps_quota_overview()
    assert apps_quota_final.get("limit") == trial_quota_apps, f"Expected Trial apps quota {trial_quota_apps}, got {apps_quota_final.get('limit')}"
    logger.info("Assert: Apps quota after downgrade (Trial): %s/%s", apps_quota_final.get('used'), apps_quota_final.get('limit'))

    logger.info("APP-02 PASSED")
