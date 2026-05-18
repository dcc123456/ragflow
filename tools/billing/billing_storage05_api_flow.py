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
API-adjusted driver for STORAGE-05.
Tests: plan upgrade/downgrade with existing storage addon.
"""

from __future__ import annotations

import json
import sys
import time
import uuid
from datetime import datetime, timezone

import stripe  # type: ignore[reportMissingImports]

clock_id = ""

from tools.billing.flow_common import (  # noqa: E402
    FlowError,
)
from tools.billing.storage_common import (  # noqa: E402
    RAGFlowClient,
    advance_clock,
    attach_default_test_card,
    delete_clock,
    first_plan_price_id,
    gb_to_bytes,
    load_billing_config,
    make_default_parser,
    replay_until_payment_order_status,
    replace_storage_subscription_quantity,
    replace_subscription_price,
    setup_starter,
    stripe_dict,
    sync_webhooks,
    wait_for_plan,
    wait_for_storage_status,
)

BYTES_PER_GB = 1000 * 1000 * 1000


def run_flow(args) -> None:
    # =============================================================================
    # Steps 1-5: Setup Starter environment using shared utility
    # =============================================================================
    print("=" * 80)
    print("Steps 1-5: Setup Starter environment using shared utility")
    print("=" * 80)

    email = args.email or f"billing-storage05-{uuid.uuid4().hex[:12]}@example.test"
    setup = setup_starter(
        base_url=args.base_url,
        version=args.version,
        email=email,
        password=args.password,
        ready_timeout_seconds=args.ready_timeout_seconds,
        webhook_wait_seconds=args.webhook_wait_seconds,
        webhook_timeout_seconds=args.webhook_timeout_seconds,
    )

    client: RAGFlowClient = setup["client"]
    tenant_id: str = setup["tenant_id"]
    customer_id: str = setup["customer_id"]
    starter_subscription_id: str = setup["subscription_id"]
    global clock_id
    clock_id = setup["clock_id"]
    webhook_secret: str = setup["webhook_secret"]
    starter_price_id: str = setup["starter_price_id"]

    print("  Assert: Starter environment ready")
    print(f"  Assert: Tenant ID: {tenant_id}")
    print(f"  Assert: Customer ID: {customer_id}")
    print(f"  Assert: Starter subscription ID: {starter_subscription_id}")

    # Load Pro price ID for later upgrade
    billing_config = load_billing_config()
    pro_price_id = first_plan_price_id(billing_config, "Pro")
    if not pro_price_id:
        raise FlowError("Pro plan price_id not found in config")
    print(f"  Assert: Pro plan price_id found: {pro_price_id[:20]}...")

    subscription_ids: set[str] = {starter_subscription_id}

    # =============================================================================
    # Step 6: Purchase storage addon (30GB) on Starter plan via direct modification
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 6: Purchase storage addon (30GB) on Starter plan")
    print("=" * 80)

    initial_storage = client.storage_current(tenant_id)
    initial_addon_bytes = int(initial_storage.get("addon_storage_bytes") or 0)
    print(f"  Assert: Initial addon storage: {initial_addon_bytes} bytes")

    storage_gb = 30
    target_quantity_bytes = gb_to_bytes(storage_gb)
    print(f"  Info: Adding {storage_gb}GB storage addon to subscription {starter_subscription_id}")

    replace_storage_subscription_quantity(
        client=client,
        tenant_id=tenant_id,
        new_quantity_gb=storage_gb,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids={starter_subscription_id},
    )
    print("  Assert: Storage addon modification sent")

    wait_for_storage_status(client, tenant_id, "active", timeout_seconds=30)
    print("  Assert: Storage subscription is active")

    after_addon_storage = client.storage_current(tenant_id)
    after_addon_addon_bytes = int(after_addon_storage.get("addon_storage_bytes") or 0)
    if after_addon_addon_bytes != target_quantity_bytes:
        raise FlowError(f"addon_storage_bytes should be {target_quantity_bytes} after purchase, got {after_addon_addon_bytes}")
    print(f"  Assert: Addon storage equals target: {after_addon_addon_bytes} bytes")

    # Verify subscription has two items (plan + storage)
    sub = stripe.Subscription.retrieve(starter_subscription_id)
    items = sub.get("items", {}).get("data", [])
    if len(items) != 2:
        raise FlowError(f"expected 2 subscription items, got {len(items)}")
    print(f"  Assert: Subscription has {len(items)} items (plan + storage)")

    # =============================================================================
    # Step 7: Upgrade plan from Starter to Pro
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 7: Upgrade plan from Starter to Pro (PLAN-05 mode)")
    print("=" * 80)

    subscription_id = client.current_plan().get("subscription_id", "")
    if not subscription_id:
        raise FlowError("no active subscription found for plan upgrade")
    print(f"  Assert: Current subscription ID: {subscription_id}")

    print(f"  Calling replace_subscription_price: sub={subscription_id}, price={pro_price_id}")
    updated_sub = replace_subscription_price(
        subscription_id,
        pro_price_id,
        proration_behavior="always_invoice",
        payment_behavior="error_if_incomplete",
        expand=["latest_invoice.payment_intent"],
    )
    updated_sub_dict = stripe_dict(updated_sub)
    subscription_id = updated_sub_dict.get("id", "")
    subscription_ids.add(subscription_id)
    print(f"  Assert: Updated subscription ID: {subscription_id}")

    latest_invoice = updated_sub_dict.get("latest_invoice") or {}
    if isinstance(latest_invoice, dict):
        pro_invoice_id = str(latest_invoice.get("id") or "")
        payment_intent = latest_invoice.get("payment_intent") or {}
        if isinstance(payment_intent, dict):
            pro_payment_intent_id = str(payment_intent.get("id") or "")
        else:
            pro_payment_intent_id = str(payment_intent or "")
    else:
        pro_invoice_id = f"in_{uuid.uuid4().hex[:24]}"
        pro_payment_intent_id = f"pi_{uuid.uuid4().hex[:24]}"
    print(f"  Assert: Pro invoice ID: {pro_invoice_id}")
    print(f"  Assert: Pro payment intent ID: {pro_payment_intent_id}")

    updated_event = {
        "id": f"evt_storage05_upgrade_{uuid.uuid4().hex[:20]}",
        "object": "event",
        "api_version": stripe.api_version,
        "created": int(datetime.now().timestamp()),
        "data": {"object": updated_sub_dict},
        "livemode": False,
        "pending_webhooks": 0,
        "type": "customer.subscription.updated",
    }
    client.post_signed_webhook(updated_event, webhook_secret)
    print("  Sent customer.subscription.updated webhook for plan upgrade")

    sync_webhooks(
        client,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=int(time.time()) - 10,
        wait_seconds=args.webhook_wait_seconds,
    )
    print("  Assert: Webhooks synced after subscription update")

    if pro_invoice_id and pro_invoice_id.startswith("in_"):
        invoice = stripe_dict(stripe.Invoice.retrieve(pro_invoice_id))
        invoice_status = invoice.get("status", "")
        print(f"  Info: Invoice status after upgrade: {invoice_status}")

        if invoice_status in {"open", "uncollectible", "unpaid", "draft"}:
            pay_started_at = int(time.time()) - 5
            print(f"  Info: Paying invoice {pro_invoice_id}")
            pm_id = attach_default_test_card(customer_id)
            try:
                pay_result = stripe.Invoice.pay(pro_invoice_id, payment_method=pm_id)
                pay_dict = stripe_dict(pay_result)
                if pay_dict.get("status") != "paid":
                    raise FlowError(f"expected Stripe to pay invoice, got {pay_dict}")
                print("  Assert: Invoice paid successfully")
            except Exception as exc:
                raise FlowError(f"failed to pay invoice {pro_invoice_id}: {exc}") from exc

            print(f"  Info: Waiting for payment order {pro_invoice_id} to reach success")
            replay_until_payment_order_status(
                client,
                webhook_secret=webhook_secret,
                customer_id=customer_id,
                subscription_ids=subscription_ids,
                created_gte=pay_started_at,
                order_id=pro_invoice_id,
                expected_status="success",
                timeout_seconds=args.webhook_timeout_seconds,
                wait_seconds=args.webhook_wait_seconds,
            )
            print("  Assert: Payment order reached success status")
        elif invoice_status == "paid":
            print("  Info: Invoice already paid")

    if pro_invoice_id and pro_invoice_id.startswith("in_"):
        invoice = stripe_dict(stripe.Invoice.retrieve(pro_invoice_id))
        if invoice.get("status") == "paid":
            invoice_paid_event = {
                "id": f"evt_manual_invoice_{uuid.uuid4().hex[:20]}",
                "object": "event",
                "type": "invoice.paid",
                "api_version": stripe.api_version,
                "created": int(time.time()),
                "data": {"object": invoice}
            }
            client.post_signed_webhook(invoice_paid_event, webhook_secret)
            print("  Sent manual invoice.paid webhook for plan upgrade")

    print("  Waiting for plan to become Pro...")
    for retry in range(30):
        current_plan = client.current_plan()
        if current_plan.get("plan_name") == "Pro":
            print(f"  ✅ Plan upgraded to Pro after {retry+1} attempts")
            break
        time.sleep(2)
    else:
        raise FlowError("plan should be Pro after upgrade, got Starter")

    after_upgrade_plan = client.current_plan()
    after_upgrade_plan_name = after_upgrade_plan.get("plan_name", "")
    after_upgrade_plan_overview = client.plan_overview()
    after_upgrade_storage = client.storage_current(tenant_id)
    after_upgrade_addon_bytes = int(after_upgrade_storage.get("addon_storage_bytes") or 0)

    if after_upgrade_plan_name != "Pro":
        raise FlowError(f"plan should be Pro after upgrade, got {after_upgrade_plan_name}")
    print(f"  Assert: Plan upgraded to Pro: {after_upgrade_plan_name}")

    plan_storage_after_upgrade = after_upgrade_plan_overview.get("resources", {}).get("plan_storage", {})
    plan_storage_limit_after_upgrade = int(plan_storage_after_upgrade.get("limit") or 0)
    print(f"  Assert: Plan storage limit after upgrade: {plan_storage_limit_after_upgrade} bytes")

    if after_upgrade_addon_bytes != target_quantity_bytes:
        raise FlowError(f"addon_storage_bytes should remain {target_quantity_bytes} after plan upgrade, got {after_upgrade_addon_bytes}")
    print(f"  Assert: Addon storage unchanged after upgrade: {after_upgrade_addon_bytes} bytes")

    # Step 8: Downgrade plan Pro -> Starter
    print("\n" + "=" * 80)
    print("Step 8: Downgrade plan from Pro to Starter (PLAN-05 mode)")
    print("=" * 80)

    if not subscription_id:
        raise FlowError("no active subscription found for plan downgrade")
    print(f"  Assert: Current subscription ID: {subscription_id}")

    print(f"  Calling replace_subscription_price: sub={subscription_id}, price={starter_price_id}")
    updated_sub = replace_subscription_price(
        subscription_id,
        starter_price_id,
        proration_behavior="always_invoice",
        payment_behavior="error_if_incomplete",
        expand=["latest_invoice.payment_intent"],
    )
    updated_sub_dict = stripe_dict(updated_sub)
    downgrade_subscription_id = updated_sub_dict.get("id", "")
    subscription_ids.add(downgrade_subscription_id)
    print(f"  Assert: Updated subscription ID: {downgrade_subscription_id}")

    latest_invoice = updated_sub_dict.get("latest_invoice") or {}
    if isinstance(latest_invoice, dict):
        downgrade_invoice_id = str(latest_invoice.get("id") or "")
        payment_intent = latest_invoice.get("payment_intent") or {}
        if isinstance(payment_intent, dict):
            downgrade_payment_intent_id = str(payment_intent.get("id") or "")
        else:
            downgrade_payment_intent_id = str(payment_intent or "")
    else:
        downgrade_invoice_id = f"in_{uuid.uuid4().hex[:24]}"
        downgrade_payment_intent_id = f"pi_{uuid.uuid4().hex[:24]}"
    print(f"  Assert: Downgrade invoice ID: {downgrade_invoice_id}")
    print(f"  Assert: Downgrade payment intent ID: {downgrade_payment_intent_id}")

    updated_event = {
        "id": f"evt_storage05_downgrade_{uuid.uuid4().hex[:20]}",
        "object": "event",
        "api_version": stripe.api_version,
        "created": int(datetime.now().timestamp()),
        "data": {"object": updated_sub_dict},
        "livemode": False,
        "pending_webhooks": 0,
        "type": "customer.subscription.updated",
    }
    client.post_signed_webhook(updated_event, webhook_secret)
    print("  Sent customer.subscription.updated webhook for plan downgrade")

    sync_webhooks(
        client,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=int(time.time()) - 10,
        wait_seconds=args.webhook_wait_seconds,
    )
    print("  Assert: Webhooks synced after subscription downgrade")

    after_downgrade_plan = wait_for_plan(client, "Starter", args.webhook_timeout_seconds)
    after_downgrade_plan_name = after_downgrade_plan.get("plan_name", "")
    after_downgrade_plan_overview = client.plan_overview()
    after_downgrade_storage = client.storage_current(tenant_id)
    after_downgrade_addon_bytes = int(after_downgrade_storage.get("addon_storage_bytes") or 0)

    if after_downgrade_plan_name != "Starter":
        raise FlowError(f"plan should be Starter after downgrade, got {after_downgrade_plan_name}")
    print(f"  Assert: Plan downgraded to Starter: {after_downgrade_plan_name}")

    plan_storage_after_downgrade = after_downgrade_plan_overview.get("resources", {}).get("plan_storage", {})
    plan_storage_limit_after_downgrade = int(plan_storage_after_downgrade.get("limit") or 0)
    print(f"  Assert: Plan storage limit after downgrade: {plan_storage_limit_after_downgrade} bytes")

    if after_downgrade_addon_bytes != target_quantity_bytes:
        raise FlowError(f"addon_storage_bytes should remain {target_quantity_bytes} after plan downgrade, got {after_downgrade_addon_bytes}")
    print(f"  Assert: Addon storage unchanged after downgrade: {after_downgrade_addon_bytes} bytes")

    # =============================================================================
    # Step 9: Verify quota after downgrade takes effect
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 9: Verify quota after downgrade takes effect")
    print("=" * 80)

    current_plan = client.current_plan()
    plan_end = current_plan.get("end_time")
    if not plan_end:
        raise FlowError(f"plan response is missing end_time: {current_plan}")

    if isinstance(plan_end, (int, float)):
        plan_end_ts = int(plan_end)
    else:
        plan_end_str = str(plan_end).replace("Z", "+00:00")
        plan_end_dt = datetime.fromisoformat(plan_end_str)
        if plan_end_dt.tzinfo is None:
            plan_end_dt = plan_end_dt.replace(tzinfo=timezone.utc)
        plan_end_ts = int(plan_end_dt.timestamp())

    current_ts = int(time.time())
    advance_seconds = plan_end_ts - current_ts - 86400
    if advance_seconds > 0:
        print(f"  Info: Advancing clock by {advance_seconds} seconds to near plan end")
        advance_clock(clock_id, current_ts + advance_seconds)
        print("  Assert: Clock advanced near plan end")
    else:
        print("  Info: Already near plan end, skipping advance")

    after_downgrade_effective_storage = client.storage_current(tenant_id)
    after_downgrade_effective_addon_bytes = int(after_downgrade_effective_storage.get("addon_storage_bytes") or 0)

    if after_downgrade_effective_addon_bytes != target_quantity_bytes:
        raise FlowError(f"addon_storage_bytes should remain {target_quantity_bytes} after downgrade takes effect, got {after_downgrade_effective_addon_bytes}")
    print(f"  Assert: Addon storage unchanged after downgrade takes effect: {after_downgrade_effective_addon_bytes} bytes")


    final_plan_overview = client.plan_overview()
    final_resources = final_plan_overview.get("resources", {})
    final_plan_storage = final_resources.get("plan_storage", {})
    final_plan_storage_limit = int(final_plan_storage.get("limit") or 0)
    final_addon_storage = final_resources.get("addon_storage", {})
    final_addon_storage_limit = int(final_addon_storage.get("limit") or 0)

    print(f"  Assert: Final plan storage limit: {final_plan_storage_limit} bytes")
    print(f"  Assert: Final addon storage limit: {final_addon_storage_limit} bytes")

    total_storage_after_downgrade = final_plan_storage_limit + final_addon_storage_limit
    expected_total = plan_storage_limit_after_downgrade + target_quantity_bytes
    if total_storage_after_downgrade != expected_total:
        raise FlowError(f"total storage should be {expected_total} bytes (5GB plan + 30GB addon), got {total_storage_after_downgrade} bytes")
    print(f"  Assert: Total storage quota: {total_storage_after_downgrade} bytes (5GB plan + 30GB addon)")

    # =============================================================================
    # Step 10: Downgrade plan from Starter to Trial (addon should be invalidated)
    # =============================================================================
    print("\n" + "=" * 80)
    print("Step 10: Downgrade plan from Starter to Trial (addon invalidation)")
    print("=" * 80)

    history_before_trial_downgrade = client.spend_history()
    print(f"  Assert: Billing history rows before Trial downgrade: {len(history_before_trial_downgrade)}")

    trial_price_id = first_plan_price_id(billing_config, "Trial")
    if not trial_price_id:
        raise FlowError("Trial plan price_id not found in config")
    print(f"  Info: Scheduling downgrade to Trial with price_id: {trial_price_id}...")
    schedule_result = client.schedule_plan_change(tenant_id, trial_price_id)

    scheduled_change = schedule_result.get("pending_subscription_change") or {}
    schedule_id = scheduled_change.get("schedule_id", "")
    if schedule_id:
        print(f"  Assert: Scheduled change ID: {schedule_id}")
    else:
        print(f"  Info: Schedule result: {schedule_result}")

    before_period_end_plan = client.current_plan()
    before_period_end_plan_name = before_period_end_plan.get("plan_name", "")
    if before_period_end_plan_name != "Starter":
        raise FlowError(f"plan changed prematurely to {before_period_end_plan_name}, expected Starter")
    print(f"  Assert: Plan remains Starter before period end: {before_period_end_plan_name}")

    before_period_end_storage = client.storage_current(tenant_id)
    before_period_end_addon_bytes = int(before_period_end_storage.get("addon_storage_bytes") or 0)
    if before_period_end_addon_bytes != target_quantity_bytes:
        raise FlowError(f"addon_storage_bytes should remain {target_quantity_bytes} before period end, got {before_period_end_addon_bytes}")
    print(f"  Assert: Addon storage unchanged before period end: {before_period_end_addon_bytes} bytes")

    current_plan = client.current_plan()
    plan_end = current_plan.get("end_time")
    if not plan_end:
        raise FlowError(f"plan response is missing end_time: {current_plan}")

    if isinstance(plan_end, (int, float)):
        plan_end_ts = int(plan_end)
    else:
        plan_end_str = str(plan_end).replace("Z", "+00:00")
        plan_end_dt = datetime.fromisoformat(plan_end_str)
        if plan_end_dt.tzinfo is None:
            plan_end_dt = plan_end_dt.replace(tzinfo=timezone.utc)
        plan_end_ts = int(plan_end_dt.timestamp())

    current_ts = int(time.time())
    advance_seconds = plan_end_ts - current_ts + 120
    if advance_seconds > 0:
        print(f"  Info: Advancing clock by {advance_seconds} seconds to after plan end")
        advance_clock(clock_id, current_ts + advance_seconds)
        print("  Assert: Clock advanced to after plan end")
    else:
        print("  Info: Already past plan end, skipping advance")

    sync_webhooks(
        client,
        webhook_secret=webhook_secret,
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=current_ts - 10,
        wait_seconds=args.webhook_wait_seconds,
    )
    print("  Assert: Webhooks synced after period end")

    after_trial_plan = wait_for_plan(client, "Trial", args.webhook_timeout_seconds)
    after_trial_plan_name = after_trial_plan.get("plan_name", "")
    if after_trial_plan_name != "Trial":
        raise FlowError(f"plan should be Trial after period end, got {after_trial_plan_name}")
    print(f"  Assert: Plan changed to Trial after period end: {after_trial_plan_name}")

    after_trial_storage = client.storage_current(tenant_id)
    after_trial_addon_bytes = int(after_trial_storage.get("addon_storage_bytes") or 0)
    after_trial_plan_storage = int(after_trial_storage.get("plan_storage_bytes") or 0)

    trial_storage_gb = 0
    expected_trial_plan_storage = trial_storage_gb * BYTES_PER_GB
    print(f"  Assert: Plan storage after Trial downgrade: {after_trial_plan_storage} bytes (expected: {expected_trial_plan_storage})")

    if after_trial_addon_bytes != 0:
        raise FlowError(f"addon_storage_bytes should be 0 after Trial downgrade (no base plan), got {after_trial_addon_bytes}")
    print(f"  Assert: Addon storage invalidated after Trial downgrade: {after_trial_addon_bytes} bytes")

    history_after_trial_downgrade = client.spend_history()
    new_rows = len(history_after_trial_downgrade) - len(history_before_trial_downgrade)
    if new_rows > 0:
        new_paid_rows = [
            row for row in history_after_trial_downgrade
            if float(row.get("amount", 0) or 0) > 0 and row not in history_before_trial_downgrade
        ]
        if new_paid_rows:
            raise FlowError(f"Trial period should not create paid charges, got: {new_paid_rows}")
    print("  Assert: No charges made during Trial period")

    final_overview = client.plan_overview()
    final_resources = final_overview.get("resources", {})
    final_plan_storage_limit = int(final_resources.get("plan_storage", {}).get("limit") or 0)
    final_addon_storage_limit = int(final_resources.get("addon_storage", {}).get("limit") or 0)

    print(f"  Assert: Final plan storage limit: {final_plan_storage_limit} bytes")
    print(f"  Assert: Final addon storage limit: {final_addon_storage_limit} bytes")

    total_storage_trial = final_plan_storage_limit + final_addon_storage_limit
    print(f"  Assert: Total storage quota after Trial downgrade: {total_storage_trial} bytes")

    print(
        json.dumps(
            {
                "case": "STORAGE-05",
                "description": "Plan change with existing addon (PLAN-05 mode)",
                "tenant_id": tenant_id,
                "email": email,
                "initial_plan": "Starter",
                "storage_gb": storage_gb,
                "addon_bytes_after_purchase": after_addon_addon_bytes,
                "plan_after_upgrade": after_upgrade_plan_name,
                "plan_subscription_id": subscription_id,
                "addon_bytes_after_upgrade": after_upgrade_addon_bytes,
                "plan_after_downgrade": after_downgrade_plan_name,
                "addon_bytes_after_downgrade": after_downgrade_addon_bytes,
                "addon_bytes_after_downgrade_effective": after_downgrade_effective_addon_bytes,
                "plan_storage_after_downgrade_effective": final_plan_storage_limit,
                "total_storage_quota": total_storage_after_downgrade,
                "plan_after_trial_downgrade": after_trial_plan_name,
                "addon_bytes_after_trial_downgrade": after_trial_addon_bytes,
                "plan_storage_after_trial_downgrade": after_trial_plan_storage,
                "total_storage_after_trial_downgrade": total_storage_trial,
            },
            indent=2,
            sort_keys=True,
        )
    )


def main() -> int:
    parser = make_default_parser("Run billing STORAGE-05: plan change with existing addon.")
    try:
        run_flow(parser.parse_args())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    finally:
        print("=" * 80)
        delete_clock(clock_id)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
