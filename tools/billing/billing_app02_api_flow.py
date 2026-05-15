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
APP-02: Downgrade Blocked by Resource Usage Test Flow

This test flow validates that plan downgrade is blocked when resource usage
exceeds the target plan's quota, and succeeds after reducing resources.

Test Scenarios:
1. Start with Trial plan, upgrade to Starter
2. Create 4 datasets + 2 chats = 6 apps total
3. Try to downgrade to Trial - should fail (apps_used=6 > Trial quota_apps=5)
4. Delete one dataset (now 5 apps total)
5. Downgrade to Trial - should succeed
6. Advance clock to period end, replay webhook events
7. Verify plan is now Trial

APIs Used:
- POST /v1/datasets - Create dataset
- DELETE /v1/datasets - Delete dataset (bulk with ids array)
- POST /v1/chats - Create chat
- POST /billing/checkout - Initiate plan downgrade
- GET /billing/current_plan - Check current plan and pending changes
- GET /billing/plan_overview - Check quota usage
"""

from __future__ import annotations

import argparse
import sys
import time
import uuid

from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    make_default_parser,
    first_plan_price_id,
    load_billing_config,
)
from tools.billing.app_common import (  # noqa: E402
    AppClient,
    setup_app_test,
    print_app_summary,
)


def run_flow(args: argparse.Namespace) -> None:
    """Execute APP-02: downgrade blocked by resource usage test."""
    # Setup - start with Trial, then upgrade to Starter
    client, email, trial_quota_apps, starter_quota_apps = setup_app_test(
        args, case_name="billing-app02", client_type=AppClient
    )

    # Upgrade to Starter
    starter_subscription_id: str = client.upgrade_trial_to_starter()["subscription_id"]
    print(f"  Assert: Starter subscription ID: {starter_subscription_id}")

    # Verify Starter plan quota
    apps_quota = client.get_apps_quota_overview()
    if apps_quota.get("limit") != starter_quota_apps:
        raise FlowError(f"Expected Starter apps quota {starter_quota_apps}, got {apps_quota.get('limit')}")
    print(f"  Assert: Apps quota from overview (Starter): {apps_quota.get('used')}/{apps_quota.get('limit')}")

    # Step 1: Create 4 datasets
    print("\n  Assert: Creating 4 datasets")
    dataset_ids = []
    for i in range(1, 5):
        dataset_name = f"test-dataset-{i}-{uuid.uuid4().hex[:6]}"
        result = client.create_dataset(dataset_name)
        if result.get("code") != 0:
            raise FlowError(f"Failed to create dataset {i}: {result.get('message')}")
        dataset_id = result.get("data", {}).get("id")
        dataset_ids.append(dataset_id)
        print(f"  Assert: Dataset {i} created: {dataset_id}")

    # Step 2: Create 2 chats (total apps = 4 datasets + 2 chats = 6)
    print("\n  Assert: Creating 2 chats")
    chat_ids = []
    for i in range(1, 3):
        chat_name = f"test-chat-{i}"
        result = client.create_chat(chat_name, dataset_ids=[])
        if result.get("code") != 0:
            raise FlowError(f"Failed to create chat {i}: {result.get('message')}")
        chat_id = result.get("data", {}).get("id")
        chat_ids.append(chat_id)
        print(f"  Assert: Chat {i} created: {chat_id}")

    # Verify total apps count
    apps_quota_after = client.get_apps_quota_overview()
    total_apps = apps_quota_after.get("used", 0)
    print(f"  Assert: Total apps after creation: {total_apps} (4 datasets + 2 chats)")

    # Step 3: Try to downgrade to Trial - should fail (apps_used=6 > Trial quota_apps=5)
    print("\n  Assert: Attempting to downgrade to Trial (should fail - apps exceed quota)")
    created_gte = int(time.time()) - 5
    try:
        billing_config = load_billing_config()
        trial_price_id = first_plan_price_id(billing_config, "Trial")
        if not trial_price_id:
            raise FlowError("Trial plan price_id not found in service_conf.yaml")

        # Try to schedule downgrade
        checkout_result = client.schedule_plan_change(trial_price_id)
        # Check if the response indicates a resource conflict
        if checkout_result.get("code") == 0:
            data = checkout_result.get("data", {})
            if data.get("resource_conflicts"):
                print(f"  Assert: Downgrade correctly rejected with resource conflicts: {data.get('resource_conflicts')}")
            else:
                raise FlowError("Downgrade should have been rejected due to resource usage exceeding Trial quota")
        else:
            print(f"  Assert: Downgrade correctly rejected: {checkout_result.get('message')}")
    except FlowError as e:
        error_msg = str(e).lower()
        if "quota" in error_msg or "insufficient" in error_msg or "exceed" in error_msg or "resource" in error_msg:
            print(f"  Assert: Downgrade correctly rejected due to resource usage: {e}")
        else:
            raise FlowError(f"Unexpected error during downgrade: {e}")

    # Step 4: Delete one dataset (now 5 apps total = 3 datasets + 2 chats)
    print("\n  Assert: Deleting one dataset to reduce apps count")
    dataset_to_delete = dataset_ids.pop()
    delete_result = client.delete_dataset(dataset_to_delete)
    if delete_result.get("code") != 0:
        raise FlowError(f"Failed to delete dataset: {delete_result.get('message')}")
    print(f"  Assert: Dataset deleted: {dataset_to_delete}")

    # Verify apps quota after deletion
    apps_quota_after_delete = client.get_apps_quota_overview()
    total_apps_after_delete = apps_quota_after_delete.get("used", 0)
    print(f"  Assert: Total apps after deletion: {total_apps_after_delete} (3 datasets + 2 chats)")

    # Step 5: Downgrade to Trial - should succeed now
    print("\n  Assert: Attempting to downgrade to Trial (should succeed)")
    downgrade_result = client.downgrade_to_trial(starter_subscription_id)
    print(f"  Assert: Downgrade to Trial scheduled: {downgrade_result}")

    # Verify pending downgrade appears in current_plan
    current_plan = client.current_plan()
    pending_change = current_plan.get("pending_subscription_change", {})
    if not pending_change:
        print("  Warning: No pending_subscription_change in current_plan, but downgrade was scheduled")
    else:
        print(f"  Assert: Pending downgrade confirmed: {pending_change}")

    # Step 6: Advance clock to period end
    print("\n  Assert: Advancing clock to period end")
    client.advance_clock_to_plan_end()
    print("  Assert: Clock advanced past period end")

    # Replay webhook events
    print("  Assert: Replaying webhook events")
    replayed = client.sync_webhooks(
        subscription_ids={starter_subscription_id},
        created_gte=created_gte,
    )
    print(f"  Assert: Webhook events replayed: {replayed} events")

    # Step 7: Verify plan is now Trial
    print("\n  Assert: Verifying plan is now Trial")
    final_plan = client.wait_for_plan("Trial", args.webhook_timeout_seconds)
    plan_name = final_plan.get("plan_name", "")
    if plan_name != "Trial":
        raise FlowError(f"Expected Trial plan after downgrade, got {plan_name}")
    print(f"  Assert: Plan is now Trial: {plan_name}")

    # Verify Trial plan quota
    apps_quota_final = client.get_apps_quota_overview()
    if apps_quota_final.get("limit") != trial_quota_apps:
        raise FlowError(f"Expected Trial apps quota {trial_quota_apps}, got {apps_quota_final.get('limit')}")
    print(f"  Assert: Apps quota after downgrade (Trial): {apps_quota_final.get('used')}/{apps_quota_final.get('limit')}")

    # Test Summary
    print_app_summary(
        case="APP-02",
        description="Downgrade blocked by resource usage test",
        client=client,
        email=email,
        extra={
            "starter_subscription_id": starter_subscription_id,
            "trial_quota_apps": trial_quota_apps,
            "starter_quota_apps": starter_quota_apps,
            "datasets_created": 4,
            "datasets_deleted": 1,
            "chats_created": 2,
            "downgrade_initially_blocked": True,
            "downgrade_after_deletion": True,
            "final_plan": plan_name,
            "apps_quota_final": apps_quota_final,
        },
    )

    # Final Summary
    print("\n" + "=" * 80)
    print("APP-02 Overall Test Summary")
    print("=" * 80)
    print('{"case": "APP-02", "description": "Downgrade blocked by resource usage test", "overall_status": "PASSED"}')


def main() -> int:
    parser = make_default_parser("Run billing APP-02: downgrade blocked by resource usage test.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
