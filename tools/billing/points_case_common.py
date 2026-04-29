"""Common helpers for reviewing and reporting points automation cases."""

from __future__ import annotations

from typing import Any

from tools.billing.flow_common import FlowError


_POINTS_CASE_METADATA = {
    "POINT-01": {
        "case_adjusted": True,
        "case_adjustment_notes": [
            "Uses a synthetic signed checkout.session.completed webhook after creating the Checkout Session instead of driving the hosted Stripe Checkout UI.",
        ],
    },
    "POINT-02": {
        "case_adjusted": True,
        "case_adjustment_notes": [
            "Uses synthetic signed checkout.session.completed webhooks for both purchases instead of completing two hosted Stripe Checkout UI sessions.",
        ],
    },
    "POINT-03": {
        "case_adjusted": True,
        "case_adjustment_notes": [
            "Covers API-side rejection and no-mutation guarantees only; frontend validation still needs separate manual verification.",
        ],
    },
    "POINT-04": {
        "case_adjusted": True,
        "case_adjustment_notes": [
            "Uses Stripe Checkout Session expire as the automation proxy for a user-cancelled or abandoned Checkout.",
        ],
    },
    "POINT-05": {
        "case_adjusted": True,
        "case_adjustment_notes": [
            "Replays the same synthetic signed checkout.session.completed payload twice instead of using Stripe dashboard or CLI replay tooling.",
        ],
    },
}


def get_points_case_metadata(case_id: str) -> dict[str, Any]:
    metadata = _POINTS_CASE_METADATA.get(case_id)
    if metadata is None:
        raise ValueError(f"Unknown points case id: {case_id}")
    return {
        "case_id": case_id,
        "case_adjusted": metadata["case_adjusted"],
        "case_adjustment_notes": list(metadata["case_adjustment_notes"]),
    }


def get_checkout_session_amount(session: dict[str, Any]) -> float:
    raw_amount = session.get("amount_total")
    if isinstance(raw_amount, bool):
        raise FlowError(f"checkout session amount_total is invalid: {raw_amount!r}")
    try:
        amount_cents = int(raw_amount)
    except (TypeError, ValueError) as exc:
        raise FlowError(f"checkout session amount_total is invalid: {raw_amount!r}") from exc
    if amount_cents < 0:
        raise FlowError(f"checkout session amount_total must be non-negative, got {amount_cents}")
    return amount_cents / 100
