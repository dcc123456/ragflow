# Billing API REST — Current State

This document describes the **current** endpoint shape of `api/apps/billing_app.py` after the REST migration.

It is a **live state document**, not a migration plan. All REST paths listed here are implemented and serving in production.

## Implemented Routes

### Subscription

| Route | Method | Handler | Status |
|---|---|---|---|
| `/billing/subscription` | GET | `billing_current_plan` | ✅ Implemented |
| `/billing/subscription` | PATCH | `billing_checkout` | ✅ Implemented |
| `/billing/subscription/preview` | POST | `billing_upcoming` | ✅ Implemented |

### Storage

| Route | Method | Handler | Status |
|---|---|---|---|
| `/billing/storage` | GET | `billing_storage_current` | ✅ Implemented |
| `/billing/storage` | PATCH | `billing_storage_set_target` | ✅ Implemented |

### Add-ons

| Route | Method | Handler | Status |
|---|---|---|---|
| `/billing/addons` | GET | `billing_all_addon_plans` | ✅ Implemented |

### Checkout Workflows

| Route | Method | Handler | Status |
|---|---|---|---|
| `/billing/subscription` | POST | `billing_checkout` | ✅ Implemented |
| `/billing/addon-purchases` | POST | `billing_checkout` | ✅ Implemented |

### Setup & Portal

| Route | Method | Handler | Status |
|---|---|---|---|
| `/billing/setup-intents` | POST | `billing_create_setup_intent` | ✅ Implemented |
| `/billing/portal-sessions` | POST | `customer_portal` | ✅ Implemented |

### Callbacks

| Route | Method | Handler | Status |
|---|---|---|---|
| `/billing/callbacks/success` | GET | `billing_success` | ✅ Implemented |
| `/billing/callbacks/cancel` | GET | `billing_cancel` | ✅ Implemented |

### Session Status & Webhook

| Route | Method | Handler | Status |
|---|---|---|---|
| `/billing/checkouts/<session_id>` | GET | `billing_session_status` | ✅ Implemented |
| `/billing/webhooks/stripe` | POST | `billing_webhook` | ✅ Implemented |

### Points

| Route | Method | Handler | Status |
|---|---|---|---|
| `/billing/points/checkout` | POST | `billing_points_checkout` | ✅ Implemented |
| `/billing/points/price` | GET | `billing_points_price` | ✅ Implemented |
| `/billing/points/balance` | GET | `billing_points_balance` | ✅ Implemented |
| `/billing/points/overview` | GET | `billing_points_balance` | ✅ Implemented |
| `/billing/points/ledger` | GET | `billing_points_ledger` | ✅ Implemented |
| `/billing/points/holds` | GET | `billing_points_holds` | ✅ Implemented |

### Overview & Metrics

| Route | Method | Handler | Status |
|---|---|---|---|
| `/billing/subscription/overview` | GET | `billing_plan_overview` | ✅ Implemented |
| `/billing/addons/overview` | GET | `billing_addon_overview` | ✅ Implemented |
| `/billing/spend/overview` | GET | `billing_spend_overview` | ✅ Implemented |
| `/billing/spend_metrics` | GET | `billing_spend_metrics` | ✅ Implemented |
| `/billing/usages/deepdoc` | GET | `billing_deepdoc_usage` | ✅ Implemented |
| `/billing/status` | GET | `billing_status` | ✅ Implemented |

## Intentional Exclusions

The following endpoints were **intentionally not removed** even though they are callback-style redirects (they still serve Stripe's redirect flow):

- `GET /billing/callbacks/success` — Stripe redirects here on checkout success
- `GET /billing/callbacks/cancel` — Stripe redirects here on checkout cancel

The alternative (fully frontend-owned redirect handling) would require removing these endpoints and configuring Stripe to redirect directly to the frontend. This was considered out of scope for the current migration.

## Checkout (single handler, multiple routes)

The `billing_checkout` handler serves two checkout routes — it dispatches internally based on `payment_type` in the request body:

| Route | Method | Handler | payment_type |
|---|---|---|---|
| `/billing/subscription` | PATCH | `billing_checkout` | `subscription` (upgrade/downgrade/modify) |
| `/billing/subscription` | POST | `billing_checkout` | new subscription checkout |
| `/billing/addon-purchases` | POST | `billing_checkout` | `addon` |

See `_validate_billing_checkout_request` at line ~2086 for the dispatch logic.