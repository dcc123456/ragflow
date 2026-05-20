# Stripe Subscription Upgrade Payment Method Flow Plan

This document has been consolidated into [docs/billing/billing.md](./billing/billing.md).

The active design record now lives in:

- `docs/billing/billing.md`, section `系统设计`

That section captures the current conclusions that were previously tracked here:

- a tenant may have multiple Stripe subscriptions over time, but at most one should be non-canceled at a time,
- the local database stores one current subscription snapshot per tenant,
- webhook handling must be race-safe across old and replacement subscriptions,
- plan and storage should remain on one current Stripe subscription with multiple items,
- `/billing/upcoming` is the shared preview contract for both plan upgrades and storage addon increases,
- `has_reusable_payment_method` is the routing hint for deciding whether the frontend can submit immediately or must first collect a payment method in-app,
- in-app amount-first preview plus `SetupIntent` collection is the preferred primary path for no-payment-method immediate paid changes,
- setup-only Checkout remains a fallback rather than the preferred user experience.

Keep this file only as a redirect to avoid duplicate architecture notes drifting apart.
