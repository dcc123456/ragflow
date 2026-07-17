import pytest

from common.billing_utils import normalize_billing_payment_state, subscription_payment_attention


@pytest.mark.parametrize(
    ("kwargs", "expected"),
    [
        ({"scheduled": True, "invoice_status": "paid"}, "scheduled"),
        ({"invoice_status": "void", "payment_intent_status": "succeeded"}, "failed"),
        ({"payment_intent_status": "canceled"}, "failed"),
        ({"subscription_status": "incomplete_expired"}, "failed"),
        ({"invoice_status": "paid"}, "paid"),
        ({"payment_intent_status": "succeeded"}, "paid"),
        ({"invoice_status": "open", "amount_due": 0}, "paid"),
        ({"payment_intent_status": "requires_action"}, "requires_action"),
        ({"payment_intent_status": "requires_payment_method"}, "requires_action"),
        ({"invoice_status": "open", "amount_due": 100}, "requires_action"),
        ({"subscription_status": "past_due"}, "requires_action"),
        ({"payment_intent_status": "processing"}, "pending"),
        (
            {
                "invoice_status": "open",
                "payment_intent_status": "processing",
                "amount_due": 100,
            },
            "pending",
        ),
        ({"invoice_status": "draft", "amount_due": 100}, "pending"),
        ({"subscription_status": "active"}, "paid"),
        ({}, "pending"),
    ],
)
def test_normalize_billing_payment_state(kwargs, expected):
    assert normalize_billing_payment_state(**kwargs) == expected


@pytest.mark.parametrize(
    ("status", "invoice_url", "expected"),
    [
        (
            "active",
            "https://invoice.stripe.com/ignored",
            {
                "payment_state": "paid",
                "payment_required": False,
                "payment_recoverable": False,
                "payment_recovery_url": "",
            },
        ),
        (
            "past_due",
            "https://invoice.stripe.com/recover",
            {
                "payment_state": "requires_action",
                "payment_required": True,
                "payment_recoverable": True,
                "payment_recovery_url": "https://invoice.stripe.com/recover",
            },
        ),
        (
            "incomplete_expired",
            "https://invoice.stripe.com/stale",
            {
                "payment_state": "failed",
                "payment_required": True,
                "payment_recoverable": False,
                "payment_recovery_url": "",
            },
        ),
    ],
)
def test_subscription_payment_attention(status, invoice_url, expected):
    assert subscription_payment_attention(status, invoice_url) == expected
