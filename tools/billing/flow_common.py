from __future__ import annotations

import json
from urllib.parse import urlparse

import requests


class FlowError(RuntimeError):
    pass


def ensure_webhook_delivery_success(response: requests.Response, event_type: str) -> None:
    if response.status_code >= 400:
        raise FlowError(f"webhook {event_type} failed: status={response.status_code} body={response.text[:500]}")
    try:
        payload = response.json()
    except ValueError as exc:
        raise FlowError(f"webhook {event_type} returned non-JSON status={response.status_code}: {response.text[:500]}") from exc
    if payload.get("success") is False:
        raise FlowError(f"webhook {event_type} was rejected: {payload}")


def assert_portal_subscription_update_url(url: str, subscription_id: str) -> None:
    parsed = urlparse(url)
    if not parsed.scheme or not parsed.netloc:
        raise FlowError(f"expected Stripe Customer Portal URL, got malformed URL: {url}")
    if "stripe.com" not in parsed.netloc:
        raise FlowError(f"expected Stripe Customer Portal URL, got non-Stripe URL: {url}")
    expected_path = f"/subscriptions/{subscription_id}/update"
    if expected_path not in parsed.path:
        raise FlowError(f"expected Stripe Customer Portal subscription update URL containing {expected_path}, got: {url}")


def build_checkout_session_completed_event(
    *,
    event_id: str,
    session_id: str,
    customer_id: str,
    subscription_id: str,
    tenant_id: str,
    price_id: str,
    product_name: str,
    previous_subscription_id: str,
    invoice_id: str,
    payment_intent_id: str,
    amount_total: int,
    currency: str,
    created: int,
    expires_at: int,
) -> dict:
    return {
        "id": event_id,
        "object": "event",
        "type": "checkout.session.completed",
        "created": created,
        "data": {
            "object": {
                "id": session_id,
                "object": "checkout.session",
                "mode": "subscription",
                "payment_status": "paid",
                "customer": customer_id,
                "subscription": subscription_id,
                "invoice": invoice_id,
                "payment_intent": payment_intent_id,
                "amount_total": amount_total,
                "currency": currency,
                "created": created,
                "expires_at": expires_at,
                "metadata": {
                    "price_type": "subscription",
                    "tenant_id": tenant_id,
                    "price_id": price_id,
                    "product_name": product_name,
                    "previous_subscription_id": previous_subscription_id,
                },
            }
        },
    }


def json_dumps_compact(payload: dict) -> str:
    return json.dumps(payload, separators=(",", ":"), sort_keys=False)


def load_persisted_webhook_secret() -> str:
    """Load the locally persisted Stripe webhook signing secret from RAGFlow DB."""
    try:
        from api.db.db_models import DB
        from api.db.services.system_settings_service import SystemSettingsService
    except Exception as exc:  # pragma: no cover - import failures depend on local env setup
        raise FlowError(f"failed to import DB services for billing_webhook_secret lookup: {exc}") from exc

    with DB.connection_context():
        setting = SystemSettingsService.get_by_name("billing_webhook_secret")
        rows = list(setting) if setting else []

    if not rows or not getattr(rows[0], "value", ""):
        raise FlowError("billing_webhook_secret is not persisted in local DB")
    return str(rows[0].value)


def select_subscription_checkout_session(
    sessions: list[dict],
    *,
    tenant_id: str,
    price_id: str,
    previous_subscription_id: str,
) -> dict:
    matching_sessions = []
    for session in sessions:
        metadata = session.get("metadata") or {}
        if session.get("mode") != "subscription":
            continue
        if metadata.get("tenant_id") != tenant_id:
            continue
        if metadata.get("price_id") != price_id:
            continue
        if metadata.get("previous_subscription_id") != previous_subscription_id:
            continue
        matching_sessions.append(session)
    if not matching_sessions:
        raise FlowError(
            "expected a matching Stripe Checkout Session for tenant "
            f"{tenant_id}, price {price_id}, previous subscription {previous_subscription_id}"
        )
    matching_sessions.sort(key=lambda item: (item.get("created", 0), item.get("id", "")), reverse=True)
    return matching_sessions[0]
