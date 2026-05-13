"""
Flow-specific common utilities for billing test flows.

Re-exports shared utilities from billing_common.py for backward compatibility.
"""

from __future__ import annotations

import time
import uuid
from datetime import datetime, timezone
from typing import Any

import stripe

from tools.billing.billing_common import FlowError, stripe_dict, select_subscription_checkout_session, \
    build_checkout_session_completed_event, create_paid_subscription, list_recent_checkout_sessions, RAGFlowClient


def parse_plan_end(plan: dict[str, Any]) -> int:
    """Extract period end timestamp from plan response, handling multiple formats."""
    value = plan.get("end_time") or plan.get("billing_cycle", {}).get("end")
    if not value:
        raise FlowError(f"plan response is missing end_time: {plan}")
    if isinstance(value, (int, float)):
        return int(value)
    text = str(value).replace("Z", "+00:00")
    if len(text) == 10:
        dt = datetime.fromisoformat(text).replace(tzinfo=timezone.utc)
    else:
        dt = datetime.fromisoformat(text)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
    return int(dt.timestamp())


def find_new_positive_paid_invoice(history: list[dict[str, Any]], previous_invoice_ids: set[str]) -> dict[str, Any]:
    """Find a new paid invoice with positive amount not in previous_invoice_ids."""
    for row in history:
        invoice_id = str(row.get("invoice_id") or "")
        if not invoice_id or invoice_id in previous_invoice_ids:
            continue
        amount_val = float(row.get("amount", 0) or 0)
        if amount_val > 0 and row.get("status") == "paid":
            return row
    raise FlowError(f"no new paid invoice with positive amount found; history={history}")


def complete_trial_checkout_upgrade(
    client: RAGFlowClient,
    *,
    webhook_secret: str,
    tenant_id: str,
    customer_id: str,
    previous_subscription_id: str,
    target_price_id: str,
    target_plan_name: str,
    subscription_ids: set[str],
    webhook_mode: str,
    webhook_wait_seconds: int,
    webhook_timeout_seconds: int,
) -> tuple[str, dict[str, Any], list[dict[str, Any]]]:
    """Complete a Trial→Target plan upgrade with webhook synchronization."""

    upgrade_started_at = int(time.time()) - 5
    checkout_result = client.schedule_plan_change(tenant_id, target_price_id)
    if not checkout_result.get("redirect_to"):
        # Direct upgrade path
        checkout_result.get("invoice_id", "")
        subscription_id = checkout_result.get("subscription_id", "")
        if not subscription_id:
            raise FlowError("Direct upgrade missing subscription_id")
        if checkout_result.get("plan_name") != target_plan_name:
            raise FlowError(f"Unexpected plan after direct upgrade: {checkout_result.get('plan_name')}")
        subscription_ids.add(subscription_id)

        # Manually send invoice.paid webhook
        # client.post_invoice_paid_event(invoice_id)
        client.sync_webhooks(
            customer_id=customer_id,
            subscription_ids=subscription_ids,
            created_gte=upgrade_started_at,
            wait_seconds=webhook_wait_seconds,
        )

        upgraded_plan = client.wait_for_plan(target_plan_name, webhook_timeout_seconds)
        if upgraded_plan.get("plan_name") != target_plan_name:
            raise FlowError(f"Plan did not switch to {target_plan_name}: {upgraded_plan.get('plan_name')}")

        return subscription_id, upgraded_plan, []

    # Checkout flow path
    history_before_upgrade = client.spend_history()
    checkout_sessions = list_recent_checkout_sessions(customer_id, upgrade_started_at)
    checkout_session = select_subscription_checkout_session(
        checkout_sessions,
        tenant_id=tenant_id,
        price_id=target_price_id,
        previous_subscription_id=previous_subscription_id,
    )
    checkout_metadata = dict(checkout_session.get("metadata") or {})
    paid_subscription, since_upgrade = create_paid_subscription(
        customer_id,
        tenant_id,
        target_price_id,
        target_plan_name,
        extra_metadata=checkout_metadata,
    )
    subscription_id = str(paid_subscription.get("id") or "")
    subscription_ids.add(subscription_id)

    latest_invoice = paid_subscription.get("latest_invoice") or {}
    if not isinstance(latest_invoice, dict):
        latest_invoice = stripe_dict(stripe.Invoice.retrieve(str(latest_invoice), expand=["payment_intent"]))
    latest_invoice_id = str(latest_invoice.get("id") or "")
    if not latest_invoice_id:
        raise FlowError(f"{target_plan_name} checkout completion is missing invoice_id")
    payment_intent = latest_invoice.get("payment_intent") or {}
    if isinstance(payment_intent, dict):
        payment_intent_id = str(payment_intent.get("id") or "")
    else:
        payment_intent_id = str(payment_intent or "")

    checkout_completed_event = build_checkout_session_completed_event(
        event_id=f"evt_manual_checkout_{uuid.uuid4().hex[:20]}",
        session_id=str(checkout_session.get("id") or ""),
        customer_id=customer_id,
        subscription_id=subscription_id,
        tenant_id=tenant_id,
        price_id=target_price_id,
        product_name=target_plan_name,
        previous_subscription_id=previous_subscription_id,
        invoice_id=latest_invoice_id,
        payment_intent_id=payment_intent_id,
        amount_total=int(latest_invoice.get("amount_paid") or latest_invoice.get("amount_due") or checkout_session.get("amount_total") or 0),
        currency=str(latest_invoice.get("currency") or checkout_session.get("currency") or "usd"),
        created=int(checkout_session.get("created") or since_upgrade),
        expires_at=int(checkout_session.get("expires_at") or (int(checkout_session.get("created") or since_upgrade) + 86400)),
    )
    client.post_signed_webhook(checkout_completed_event)
    client.sync_webhooks(
        customer_id=customer_id,
        subscription_ids=subscription_ids,
        created_gte=since_upgrade,
        wait_seconds=webhook_wait_seconds,
    )

    upgraded_plan = client.wait_for_plan(target_plan_name, webhook_timeout_seconds)
    client.wait_for_history_count(
        len(history_before_upgrade) + 1,
        webhook_timeout_seconds,
        f"Trial→{target_plan_name} upgrade payment",
    )
    history_after_upgrade = client.spend_history()
    latest = find_new_positive_paid_invoice(
        history_after_upgrade,
        {str(row.get("invoice_id") or "") for row in history_before_upgrade},
    )
    amount_val = float(latest.get("amount", 0) or 0)
    if amount_val <= 0 or latest.get("status") != "paid" or not latest.get("invoice_id"):
        raise FlowError(f"Trial→{target_plan_name} upgrade should create a paid invoice, got {latest}")
    return subscription_id, upgraded_plan, history_after_upgrade


def remove_customer_payment_method(customer_id: str) -> None:
    """Remove all payment methods from customer to trigger payment failure."""
    payment_methods = stripe.PaymentMethod.list(customer=customer_id, type="card")
    for pm in payment_methods.auto_paging_iter():
        stripe.PaymentMethod.detach(pm.id)


def replay_until_payment_order_status(
    client: "RAGFlowClient",
    *,
    webhook_secret: str,
    customer_id: str,
    subscription_ids: set[str],
    created_gte: int,
    order_id: str,
    expected_status: str,
    timeout_seconds: int,
    wait_seconds: int,
) -> dict[str, Any]:
    """Wait for payment order to reach expected status by replaying Stripe events."""
    from api.db.db_models import DB
    from api.db.services.billing_service import PaymentOrderService

    deadline = time.time() + timeout_seconds
    last_payment_order: dict[str, Any] = {}
    while time.time() < deadline:
        client.sync_webhooks(
            customer_id=customer_id,
            subscription_ids=subscription_ids,
            created_gte=created_gte,
            wait_seconds=wait_seconds,
        )
        with DB.connection_context():
            last_payment_order = PaymentOrderService.get_by_order_id(order_id) or {}
        if last_payment_order.get("payment_status") == expected_status:
            return last_payment_order
        time.sleep(2)
    raise FlowError(
        f"timed out waiting for billing_payment_order {order_id} to reach {expected_status}, "
        f"last={last_payment_order}"
    )
