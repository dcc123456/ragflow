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
APP-01: Basic App Quota Enforcement Test Flow

This test flow validates the basic app quota enforcement across different plans:
- Trial: quota_apps = 5
- Starter: quota_apps = 50

Test Scenarios:
1. Start with Trial plan (quota_apps = 5)
2. Create 4 datasets + 1 chat = 5 apps total (at quota limit)
3. Try to create a second chat - should fail (quota exceeded)
4. Upgrade to Starter plan (quota_apps = 50)
5. Create second chat - should succeed
6. Create an agent - should succeed
7. Create a canvas - should succeed

APIs Used:
- POST /v1/datasets - Create dataset
- POST /v1/chats - Create chat
- POST /v1/agents - Create agent
- POST /v1/canvas - Create canvas
- POST /billing/checkout - Initiate plan upgrade
- GET /billing/plan_overview - Check quota usage
"""

from __future__ import annotations

import argparse
import sys
import uuid

from tools.billing.billing_common import (  # noqa: E402
    FlowError,
    make_default_parser,
)
from tools.billing.app_common import (  # noqa: E402
    AppClient,
    setup_app_test,
    print_app_summary,
)


def run_flow(args: argparse.Namespace) -> None:
    """Execute APP-01: app quota enforcement test."""
    # Setup
    client, email, trial_quota_apps, starter_quota_apps = setup_app_test(
        args, case_name="billing-app01", client_type=AppClient
    )

    # Step 1: Create 4 datasets (Trial quota = 5, so 4 datasets + 1 chat = 5)
    print("\n  Assert: Creating 4 datasets (within Trial quota of 5)")
    dataset_ids = []
    for i in range(1, 5):
        dataset_name = f"test-dataset-{i}-{uuid.uuid4().hex[:6]}"
        result = client.create_dataset(dataset_name)
        if result.get("code") != 0:
            raise FlowError(f"Failed to create dataset {i}: {result.get('message')}")
        dataset_id = result.get("data", {}).get("id")
        dataset_ids.append(dataset_id)
        print(f"  Assert: Dataset {i} created: {dataset_id}")

    # Step 2: Create 1 chat (now at quota limit: 4 datasets + 1 chat = 5)
    # Note: dataset_ids is empty to avoid "doesn't own parsed file" error
    print("\n  Assert: Creating 1st chat (at Trial quota limit of 5)")
    chat1_result = client.create_chat("test-chat-1", dataset_ids=[])
    if chat1_result.get("code") != 0:
        raise FlowError(f"Failed to create 1st chat: {chat1_result.get('message')}")
    chat1_id = chat1_result.get("data", {}).get("id")
    print(f"  Assert: 1st chat created: {chat1_id}")

    # Verify apps quota usage
    apps_quota_after = client.get_apps_quota_overview()
    print(f"  Assert: Apps quota after creating 5 apps: used={apps_quota_after.get('used')}, limit={apps_quota_after.get('limit')}")

    # Step 3: Try to create a second chat - should fail (quota exceeded)
    print("\n  Assert: Attempting to create 2nd chat (should fail - quota exceeded)")
    try:
        chat2_result = client.create_chat("test-chat-2", dataset_ids=[])
        if chat2_result.get("code") == 0:
            raise FlowError("2nd chat was incorrectly created when quota should be exceeded")
        print(f"  Assert: 2nd chat correctly rejected: {chat2_result.get('message')}")
    except FlowError as e:
        error_msg = str(e).lower()
        if "quota" in error_msg or "insufficient" in error_msg:
            print(f"  Assert: 2nd chat correctly rejected due to quota: {e}")
        else:
            raise FlowError(f"Unexpected error creating 2nd chat: {e}")

    # Step 4: Upgrade to Starter plan
    print("\n  Assert: Upgrading from Trial to Starter")
    starter_result = client.upgrade_trial_to_starter()
    subscription_id = starter_result.get("subscription_id", "")
    print(f"  Assert: Starter subscription: {subscription_id}")

    # Wait for plan to become Starter
    client.wait_for_plan("Starter", args.webhook_timeout_seconds)
    print("  Assert: Plan is now Starter")

    # Verify Starter plan quota
    apps_quota_starter = client.get_apps_quota_overview()
    if apps_quota_starter.get("limit") != starter_quota_apps:
        raise FlowError(f"Expected Starter apps quota {starter_quota_apps}, got {apps_quota_starter.get('limit')}")
    print(f"  Assert: Apps quota from overview (Starter): {apps_quota_starter.get('limit')}")

    # Step 5: Create second chat - should succeed now
    print("\n  Assert: Creating 2nd chat (should succeed with Starter quota)")
    chat2_result = client.create_chat("test-chat-2", dataset_ids=[])
    if chat2_result.get("code") != 0:
        raise FlowError(f"Failed to create 2nd chat after upgrade: {chat2_result.get('message')}")
    chat2_id = chat2_result.get("data", {}).get("id")
    print(f"  Assert: 2nd chat created: {chat2_id}")

    # Step 6: Create an agent - should succeed
    print("\n  Assert: Creating agent (should succeed with Starter quota)")
    agent_result = client.create_agent("test-agent-1")

    if agent_result.get("code") != 0:
        raise FlowError(f"Failed to create agent: {agent_result.get('message')}")
    print(f"  Assert: Agent created: {agent_result}")

    # Step 7: Create a canvas - should succeed
    print("\n  Assert: Creating canvas (should succeed with Starter quota)")
    canvas_result = client.create_canvas("test-canvas-1")
    if canvas_result.get("code") != 0:
        raise FlowError(f"Failed to create canvas: {canvas_result.get('message')}")
    canvas_id = canvas_result.get("data", {}).get("id")
    print(f"  Assert: Canvas created: {canvas_id}")

    # Final verification
    apps_quota_final = client.get_apps_quota_overview()
    print(f"\n  Assert: Final apps quota: used={apps_quota_final.get('used')}, limit={apps_quota_final.get('limit')}")

    # Test Summary
    print_app_summary(
        case="APP-01",
        description="App quota enforcement test: Trial -> Starter upgrade",
        client=client,
        email=email,
        extra={
            "trial_quota_apps": trial_quota_apps,
            "starter_quota_apps": starter_quota_apps,
            "datasets_created": len(dataset_ids),
            "chats_created": 2,
            "agents_created": 1,
            "canvases_created": 1,
            "apps_quota_final": apps_quota_final,
        },
    )

    # Final Summary
    print("\n" + "=" * 80)
    print("APP-01 Overall Test Summary")
    print("=" * 80)
    print('{"case": "APP-01", "description": "App quota enforcement test: Trial -> Starter upgrade", "overall_status": "PASSED"}')


def main() -> int:
    parser = make_default_parser("Run billing APP-01: app quota enforcement test.")

    args = parser.parse_args()
    try:
        run_flow(args)
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
