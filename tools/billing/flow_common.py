"""
Flow-specific common utilities for billing test flows.

Re-exports shared utilities from billing_common.py for backward compatibility.
"""

from __future__ import annotations

from typing import Any

import stripe

# Re-export from billing_common.py for backward compatibility
from tools.billing.billing_common import (
    FlowError,
    assert_portal_subscription_update_url,
    build_checkout_session_completed_event,
    ensure_webhook_delivery_success,
    get_starter_quota_apps,
    get_trial_quota_apps,
    json_dumps_compact,
    load_persisted_webhook_secret,
    select_subscription_checkout_session, stripe_dict,
)

__all__ = [
    "FlowError",
    "assert_portal_subscription_update_url",
    "build_checkout_session_completed_event",
    "ensure_webhook_delivery_success",
    "get_starter_quota_apps",
    "get_trial_quota_apps",
    "json_dumps_compact",
    "load_persisted_webhook_secret",
    "select_subscription_checkout_session",
]

from tools.billing.storage_common import advance_clock


def ensure_invoice_finalized(clock_id: str, subscription_id: str) -> dict[str, Any] | None:
    """Ensure the latest subscription invoice is finalized (not draft). Returns invoice dict or None."""
    for attempt in range(3):
        subscription = stripe.Subscription.retrieve(subscription_id, expand=["latest_invoice"])
        subscription_dict = stripe_dict(subscription)
        latest_invoice = subscription_dict.get("latest_invoice")
        if not latest_invoice:
            return None
        invoice = latest_invoice if isinstance(latest_invoice, dict) else stripe.Invoice.retrieve(str(latest_invoice))
        invoice_dict = stripe_dict(invoice)
        status = invoice_dict.get("status")
        if status in {"paid", "void", "uncollectible"}:
            return invoice_dict
        if status == "draft":
            finalize_at = invoice_dict.get("automatically_finalizes_at") or int(invoice_dict.get("created", 0)) + 3660
            clock = stripe_dict(stripe.test_helpers.TestClock.retrieve(clock_id))
            frozen = int(clock.get("frozen_time", 0))
            if finalize_at and int(finalize_at) > frozen:
                print(f"[DEBUG] Invoice draft, advancing clock from {frozen} to {finalize_at} for auto-finalize...")
                advance_clock(clock_id, int(finalize_at))
                continue
            # We're at or past auto-finalize time but still draft; finalize manually
            print("[DEBUG] Invoice still draft after time advance, finalizing manually...")
            try:
                finalized = stripe.Invoice.finalize_invoice(invoice_dict["id"])
                return stripe_dict(finalized)
            except Exception as e:
                print(f"[DEBUG] Finalize failed: {e}")
                return invoice_dict
        return invoice_dict
    return None