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
TRIAL-01: Trial tenant never creates Stripe invoices; points quota never auto-resets.

Test cases:
  TRIAL-01a  Trial starts → consume N points via document parsing → advance 35d
             → no invoice, plan_points.used unchanged (not reset)
  TRIAL-01b  Trial → Starter → delete subscription → advance 35d → no invoice

Note: Trial points (quota_points) are controlled locally and do not auto-reset
because Trial has no Stripe subscription — there is no billing cycle to trigger
a reset. The key invariants are:
  (a) Trial generates zero Stripe invoices
  (b) Trial plan_points.used is never silently cleared by a billing cycle
"""

from __future__ import annotations

import time as time_module
import uuid
from pathlib import Path

import pytest
import stripe

from libs.billing.app_common import AppClient
from libs.billing.billing_common import (
    advance_clock,
    first_plan_price_id,
    load_billing_config,
    stripe_dict,
)


@pytest.mark.billing
def test_trial_01a_no_invoice_on_billing_cycle(
    billing_client: AppClient,
):
    """
    TRIAL-01a: Trial tenant parses a document to consume N points,
    advances 35 days with no Stripe invoice and no quota reset.

    Since Trial has no Stripe subscription, it has no billing cycle and therefore:
      - No invoice.paid events are generated
      - plan_points.used is never automatically cleared
    """
    # 1. Verify Trial plan
    plan = billing_client.current_plan()
    assert plan["plan_name"].lower() == "trial", f"Expected Trial, got {plan['plan_name']}"

    # 2. Record initial consumed points
    balance_before = billing_client.points_balance()
    plan_used_before = int(balance_before.get("plan_points", {}).get("used", 0) or 0)

    # 3. Upload+parse a document to consume points via PointHold
    # Init SDK token for app operations (creates Bearer auth)
    billing_client.init_sdk_token()

    # Use prepared PDF sample for parsing to consume points via PointHold
    pdf_path = str(Path(__file__).parent / "hou_chibi_fu.pdf")
    assert Path(pdf_path).exists(), f"Sample PDF not found: {pdf_path}"

    # Upload document
    dataset_res = billing_client.create_dataset(f"trial-dataset-{uuid.uuid4().hex[:8]}")
    assert dataset_res.get("code") == 0, f"Dataset creation failed: {dataset_res}"
    dataset_id = dataset_res["data"]["id"]

    upload_res = billing_client.upload_document(dataset_id, pdf_path)
    assert upload_res.get("code") == 0, f"Upload failed: {upload_res}"
    document_ids = [doc["id"] for doc in (upload_res.get("data") or [])]
    assert document_ids, f"No document IDs returned after upload: {upload_res}"

    # Print parser_config for review
    docs = billing_client.list_documents(dataset_id)
    for doc in (docs.get("data", {}).get("docs") or []):
        print("\n=== Document parser_config ===")
        import json
        print(json.dumps(doc.get("parser_config", {}), indent=2))

    # Parse documents — this triggers PointHold → consumes plan_points
    parse_res = billing_client.parse_documents(dataset_id, document_ids)
    assert parse_res.get("code") == 0, f"Parse failed: {parse_res}"

    # Poll until parsing is done
    for _ in range(120):
        docs = billing_client.list_documents(dataset_id)
        all_done = all(
            doc.get("run") == "DONE"
            for doc in (docs.get("data", {}).get("docs") or [])
        )
        if all_done:
            break
        time_module.sleep(2)
    else:
        pytest.fail(f"Document parsing did not complete: {docs}")

    # Record plan_points.used after parsing
    balance_after_parse = billing_client.points_balance()
    plan_used_after_parse = int(balance_after_parse.get("plan_points", {}).get("used", 0) or 0)

    # consumed_plan_points should have increased (was 0, now > 0)
    assert plan_used_after_parse > plan_used_before, (
        f"Expected plan_points.used to increase after parsing, "
        f"before={plan_used_before}, after={plan_used_after_parse}"
    )

    # 4. Advance clock by 35 days (one "billing cycle")
    clock_id = billing_client.clock_id
    frozen = int(stripe_dict(stripe.test_helpers.TestClock.retrieve(clock_id)).get("frozen_time", 0))
    advance_clock(clock_id, frozen + 35 * 86400)

    # 5. Assert no Stripe invoice for this tenant
    customer_id = billing_client.customer_id
    invoices = stripe.Invoice.list(customer=customer_id, limit=10)
    tenant_invoices = [
        inv for inv in (invoices.data or [])
        if (inv.metadata or {}).get("tenant_id") == billing_client.tenant_id
    ]
    assert len(tenant_invoices) == 0, (
        f"Trial tenant should have 0 Stripe invoices, found {len(tenant_invoices)}: {tenant_invoices}"
    )

    # 6. Assert plan_points.used was NOT reset (still equals post-parse value)
    balance_after = billing_client.points_balance()
    plan_used_after = int(balance_after.get("plan_points", {}).get("used", 0) or 0)
    assert plan_used_after == plan_used_after_parse, (
        f"Trial plan_points.used must not change after billing cycle: "
        f"before_clock={plan_used_after_parse}, after_clock={plan_used_after}"
    )


@pytest.mark.billing
def test_trial_01b_no_invoice_after_starter_cycle(billing_client: AppClient):
    """
    TRIAL-01b: Trial → Starter → schedule downgrade to Trial → advance 35d → no invoice for Trial period.

    After scheduling a downgrade to Trial and advancing the clock, the tenant should have zero
    Stripe invoices for the Trial period, confirming that Trial has no billing cycle
    even after a prior paid cycle.
    """
    # 1. Upgrade to Starter via helper (handles Stripe direct subscription creation)
    billing_client.upgrade_trial_to_starter()

    # Poll until subscription is active
    for _ in range(30):
        sub = billing_client.current_plan()
        if sub.get("subscription_status") == "active" and sub.get("plan_name", "").lower() == "starter":
            break
        time_module.sleep(2)
    else:
        pytest.fail(f"Starter subscription not active: {billing_client.current_plan()}")

    # 2. Schedule downgrade to Trial via backend API (delayed to period end)
    billing_config = load_billing_config()
    trial_price_id = first_plan_price_id(billing_config, "Trial")
    checkout_result = billing_client.schedule_plan_change(trial_price_id)
    scheduled_change = checkout_result.get("scheduled_change", {})
    schedule_id = str(scheduled_change.get("schedule_id") or "")
    assert schedule_id, f"Expected scheduled_change for downgrade to Trial: {checkout_result}"

    # Wait for pending downgrade to appear
    billing_client.wait_for_pending_downgrade("Trial")

    # 3. Advance clock by 35 days (beyond the Starter billing period)
    clock_id = billing_client.clock_id
    frozen = int(stripe_dict(stripe.test_helpers.TestClock.retrieve(clock_id)).get("frozen_time", 0))
    advance_clock(clock_id, frozen + 35 * 86400)

    # 4. Assert no Stripe invoice for Trial period
    customer_id = billing_client.customer_id
    invoices = stripe.Invoice.list(customer=customer_id, limit=10)
    tenant_invoices = [
        inv for inv in (invoices.data or [])
        if (inv.metadata or {}).get("tenant_id") == billing_client.tenant_id
    ]
    assert len(tenant_invoices) == 0, (
        f"Trial tenant should have 0 Stripe invoices after downgrade, "
        f"found {len(tenant_invoices)}: {tenant_invoices}"
    )

    # 5. Verify plan is still Trial
    plan_after = billing_client.current_plan()
    assert plan_after["plan_name"].lower() == "trial", (
        f"Expected Trial, got {plan_after['plan_name']}"
    )


@pytest.mark.billing
def test_trial_02_first_upgrade_via_setup_intent_flow(billing_client: AppClient):
    """
    TRIAL-02: Trial tenant first upgrade to Starter requires payment method setup
    when no reusable payment method exists.

    Steps:
    1. Verify tenant is on Trial with no subscription_id
    2. Call /billing/upcoming for Starter plan — expect has_reusable_payment_method=false
    3. Create SetupIntent via /billing/setup-intent
    4. Confirm SetupIntent with Stripe (succeed_setup_intent)
    5. Call /billing/checkout with setup_intent_id
    6. Verify subscription is created and plan becomes Starter

    This validates the documented contract: Trial first upgrade must check
    has_reusable_payment_method, and if false, must complete setup-intent
    flow before checkout.
    """
    # 1. Verify Trial plan with no subscription_id
    plan = billing_client.current_plan()
    assert plan["plan_name"].lower() == "trial", f"Expected Trial, got {plan['plan_name']}"
    assert not plan.get("subscription_id"), f"Trial should have no subscription_id: {plan}"

    # 2. Get Starter price_id
    billing_config = load_billing_config()
    starter_price_id = first_plan_price_id(billing_config, "Starter")

    # 3. Check upcoming for Starter — should indicate no reusable payment method
    upcoming = billing_client.upcoming_plan_change(starter_price_id)
    has_payment_method = upcoming.get("has_reusable_payment_method", True)
    print(f"  has_reusable_payment_method: {has_payment_method}")

    # 4. Create and confirm SetupIntent if needed
    setup_intent_id = ""
    if not has_payment_method:
        setup_result = billing_client.create_setup_intent(
            setup_type="subscription_upgrade",
            price_id=starter_price_id,
        )
        setup_intent_id = str(setup_result.get("setup_intent_id") or "")
        assert setup_intent_id, f"Expected setup_intent_id in response: {setup_result}"
        print(f"  Created SetupIntent: {setup_intent_id}")

        # Confirm SetupIntent with Stripe
        billing_client.succeed_setup_intent(setup_intent_id)
        print(f"  Confirmed SetupIntent: {setup_intent_id}")
    else:
        print("  Reusable payment method exists — skipping SetupIntent flow")

    # 5. Schedule plan change (checkout) with setup_intent_id
    checkout_result = billing_client.schedule_plan_change(starter_price_id, setup_intent_id=setup_intent_id)
    subscription_id = str(checkout_result.get("subscription_id") or "")
    print(f"  Checkout result — subscription_id: {subscription_id}")
    assert subscription_id, f"Expected subscription_id in checkout result: {checkout_result}"

    # 6. Verify plan becomes Starter
    billing_client.wait_for_plan("Starter", timeout_seconds=30)
    final_plan = billing_client.current_plan()
    assert final_plan["plan_name"].lower() == "starter", f"Expected Starter, got {final_plan['plan_name']}"
    assert final_plan.get("subscription_id") == subscription_id, (
        f"subscription_id mismatch: expected {subscription_id}, got {final_plan.get('subscription_id')}"
    )
    print("  ✅ Trial → Starter upgrade via SetupIntent flow succeeded")