# RAGFlow Billing System Documentation

For business rules, see [billing_spec_zh.md](./billing_spec_zh.md). This document records system implementation, interfaces, and test flows.

## 1. Overview

RAGFlow uses Stripe as its payment provider. A tenant may have multiple Stripe subscriptions over time, but the system assumes that **at most one subscription is non-canceled for a tenant at any given time**. The local database stores a single current subscription snapshot per tenant, and Stripe webhooks synchronize that snapshot.

### Key Components

| Component | Description |
|-----------|-------------|
| `billing_app.py` | Quart API endpoints for billing operations |
| `billing_service.py` | Database service layer for subscription/orders |
| `billing_client.py` | Test client for billing API flows |
| `billing_common.py` | Shared utilities (config loading, Stripe helpers) |
| `points_common.py` | Points-specific utilities |
| `storage_common.py` | Storage-specific utilities |

### Stripe Webhook Events

| Event | Purpose |
|-------|---------|
| `checkout.session.completed` | Subscription Checkout completed; points recharge is credited directly on success |
| `invoice.paid` | Payment received for subscriptions or addons (renewal, upgrade, storage addon) |
| `invoice.payment_failed` | Payment failure → delinquent status |
| `customer.subscription.updated` | Subscription status, plan, or storage changes synced back locally |
| `customer.subscription.deleted` | Subscription cancelled |
| `payment_intent.succeeded` | One-off payment success event retained for compatibility addon flows |

---

## 2. 系统设计

### 2.1 Current Subscription Model

The billing data model is intentionally simpler than Stripe's full object graph.

- Stripe may contain multiple historical subscriptions for one tenant.
- The application stores one current subscription snapshot per tenant in `billing_subscription`.
- The current snapshot is the subscription currently responsible for entitlement, storage alignment, and billing state shown by `/billing/current_plan`.

This leads to the working system assumption:

- a tenant can have multiple Stripe subscriptions over time,
- but at the same moment there should be at most one subscription whose status is not `canceled`.

This assumption is looser than the earlier "single-subscription-per-tenant" design and better matches real migration flows such as:

- cancel old subscription,
- create replacement subscription,
- receive webhooks for both the old and new subscription during a transition window.

### 2.2 Webhook Authority Rules

Webhook processing is the source of truth for local subscription state, but it must be race-safe when old and new subscriptions coexist briefly in Stripe history.

The intended authority rules are:

- a non-canceled subscription event may become the tenant's current snapshot,
- a canceled subscription must not overwrite a different non-canceled current subscription,
- a stale `customer.subscription.deleted` event for an old subscription must be ignored once the tenant already points at a newer active subscription,
- plan and storage synchronization must be derived from the subscription carried by the webhook event itself, not from an implicit assumption that one tenant only ever has one relevant Stripe subscription.

Operationally, this means:

- `customer.subscription.updated` is responsible for synchronizing current plan, current Stripe status, current billing period, and storage item quantity,
- `invoice.paid` is responsible for payment confirmation and payment-order bookkeeping,
- `invoice.payment_failed` is responsible for delinquent state and recovery links,
- `customer.subscription.deleted` only clears the local current snapshot when the deleted subscription is still the one currently represented locally.

### 2.3 Plan And Storage Shape

The preferred Stripe shape remains:

- one current Stripe subscription,
- multiple subscription items,
- one main plan item plus an optional storage addon item.

This is preferred because plan and storage should stay aligned on the same billing cycle, especially for:

- immediate prorated upgrades,
- period-end downgrades,
- Trial downgrade behavior that automatically removes storage,
- consistent invoice previews and webhook reconciliation.

The system should still behave correctly if a replacement subscription is created, but the steady-state target remains one active subscription with multiple items rather than multiple concurrently active subscriptions.

### 2.4 Immediate Paid Change Flow

Immediate paid mutations include:

- Trial to paid plan upgrades,
- paid plan to higher paid plan upgrades,
- storage addon increases that take effect immediately.

These flows require both:

- accurate preview of what is due now,
- a reusable payment method before the actual Stripe subscription modification happens.

Important implementation lesson:

- `stripe.Subscription.modify_async(...)` is a backend-only mutation.
- It does not create any interactive payment page by itself.
- It is appropriate only when the customer already has a reusable payment method that Stripe can charge for the immediate prorated invoice.
- If no usable payment method exists, this call may fail before any user-facing payment interaction is created.

Relevant Stripe documentation:

- Stripe API `Update a subscription`: `https://docs.stripe.com/api/subscriptions/update`
- Stripe note on default payment method resolution for subscriptions and invoices: `https://docs.stripe.com/api/subscriptions/update#update_subscription-default_payment_method`
- Stripe pending updates behavior when payment cannot complete immediately: `https://docs.stripe.com/billing/subscriptions/pending-updates`

The current preferred flow is:

1. Frontend calls `/billing/upcoming`.
2. Backend returns invoice preview data plus `has_reusable_payment_method`.
3. If `has_reusable_payment_method=true`, the backend can directly perform the paid subscription mutation.
4. If `has_reusable_payment_method=false`, frontend collects a payment method in-app using Stripe Elements / Payment Element and a `SetupIntent`.
5. Backend verifies the completed `SetupIntent`, saves the payment method as reusable/default, and then performs the one real subscription modification.
6. Final subscription state is confirmed by webhook-synchronized backend data rather than by frontend optimism.

This same API and UX contract should be shared by:

- plan upgrade preview and confirmation,
- storage addon preview and confirmation.

### 2.5 Hosted Fallbacks

The current design conclusions are:

- setup-only Stripe Checkout is operationally safe but weaker in UX because it asks for card details without showing the actual charge amount,
- Stripe Customer Portal is a useful hosted fallback for simpler cases but is not the preferred primary product flow,
- Hosted Invoice Page remains a research option and is not currently treated as the reliable primary path for no-payment-method subscription upgrades.

Important implementation lessons about Customer Portal:

- Customer Portal is configuration-driven and weaker than direct backend control for product-specific upgrade semantics.
- It is not a good universal primary flow when we need tight control over proration behavior, exact preview semantics, storage alignment, or future custom billing logic.
- It has practical capability boundaries for more complex subscription shapes, especially when a single current subscription may contain multiple items and may also interact with subscription schedules or downgrade rules.

Relevant Stripe documentation:

- Stripe Customer Portal overview and limitations: `https://docs.stripe.com/customer-management`
- Stripe Customer Portal for subscriptions, including explicit limitations for multiple products and subscription schedules: `https://docs.stripe.com/billing/subscriptions/integrating-customer-portal`

Therefore:

- in-app amount-first preview plus `SetupIntent` collection is the preferred primary path,
- setup-only Checkout remains the lowest-level fallback,
- webhook-driven backend state remains authoritative after either path.

---

## 3. Subscription Plans

### Plan Hierarchy

```
Trial → Starter → Pro
  ↑        ↓
  └────────┘ (downgrade at period end)
```

### Plan Quotas

Each plan defines resource limits in `service_conf.yaml`:

| Resource | Trial | Starter | Pro |
|----------|-------|---------|-----|
| `quota_apps` | 5 | 50 | 100000 |
| `quota_members` | 1 | 5 | 20 |
| `quota_storage` | 100MB | 5GB | 50GB |
| `quota_points` | 500 | 5000 | 20000 |

### Subscription Statuses

| Status | Entitled? | Description |
|--------|-----------|-------------|
| `active` | ✅ | Normal operation |
| `trialing` | ✅ | Trial period |
| `past_due` | ❌ | Payment failed, recoverable |
| `incomplete` | ❌ | Initial payment pending |
| `incomplete_expired` | ❌ | Payment window expired |
| `unpaid` | ❌ | Payment failed after retries |
| `canceled` | ❌ | Subscription cancelled |
| `paused` | ❌ | Subscription paused |

---

## 4. Plan Upgrade/Downgrade Flow

### 4.1 Trial → Starter (Upgrade)

**Trigger**: User completes Stripe Checkout session.

**Process**:
1. Frontend calls `POST /billing/checkout` with Starter `price_id`
2. Backend creates Stripe Checkout session → returns a redirect URL
3. User completes subscription Checkout on Stripe → `checkout.session.completed` fires
4. Subscription state is synchronized locally by `customer.subscription.updated`
5. Billing cycle starts immediately (new cycle from now)

**Prerequisites**:
- Tenant must have a Stripe `customer_id`
- Valid payment method required

**Effective Time**: Immediate (starts new billing cycle)

### 4.2 Starter → Pro (Upgrade)

**Trigger**: User calls `POST /billing/checkout` with Pro `price_id`.

**Process**:
1. Backend calls `modify_subscription_plan_async()` on Stripe
2. Stripe charges prorated difference immediately (`proration_behavior: always_invoice`)
3. `invoice.paid` and `customer.subscription.updated` synchronize payment and subscription state
4. DB updated to "Pro" plan

**Prerequisites**:
- Active Starter subscription
- Valid payment method on file

**Effective Time**: Immediate (prorated charge billed today)

### 4.3 Pro → Starter (Downgrade)

**Trigger**: User calls `POST /billing/checkout` with Starter `price_id`.

**Process**:
1. Backend calls `schedule_subscription_price_change_at_period_end_async()`
2. Stripe creates a `SubscriptionSchedule` with pending change
3. `pending_subscription_change` appears in `GET /billing/current_plan`
4. At period end → `customer.subscription.updated` webhook fires
5. DB updated to "Starter" plan, new billing cycle starts

**Prerequisites**:
- Active Pro subscription
- No resource conflicts (apps ≤ Starter quota, etc.)

**Effective Time**: At current billing period end

### 4.4 Starter → Trial (Downgrade)

**Trigger**: User calls `POST /billing/checkout` with Trial `price_id`.

**Process**:
1. Backend validates resource compatibility via `_check_downgrade_resource_compatibility()`
2. If usage exceeds Trial quota → returns error with conflict details
3. If compatible → schedules downgrade at period end
4. **Storage addon is automatically cancelled** (quantity set to 0)
5. At period end → plan changes to "Trial"

**Prerequisites**:
- Active paid subscription
- All resource usage within Trial limits:
  - `apps_used ≤ Trial quota_apps`
  - `members_used ≤ Trial quota_members`
  - `storage_used ≤ Trial quota_storage` (addon storage NOT retained)

**Effective Time**: At current billing period end

**Important**: Trial plan does NOT support storage addons. Any existing storage addon is cancelled when downgrading to Trial.

---

## 5. Resource Compatibility Check

Before any downgrade, `_check_downgrade_resource_compatibility()` validates:

```python
conflicts = []
if storage_used > total_storage_limit:
    conflicts.append({"resource": "storage", ...})
if members_used > target_quota_members:
    conflicts.append({"resource": "members", ...})
if apps_used > target_quota_apps:
    conflicts.append({"resource": "apps", ...})
```

Note: the current implementation does not check points usage here. When the target plan is Trial, storage compatibility is evaluated against the Trial base storage quota only, without retaining purchased storage addons.

**Response on conflict**:
```json
{
  "code": 40006,
  "data": {"resource_conflicts": [...]},
  "message": "Resource usage exceeds Trial quota: storage, apps. ..."
}
```

---

## 6. Storage Addon

### 5.1 Overview

Storage addon is a **line item on the same current subscription** as the plan in the steady-state design. It is not intended to be modeled as a separate long-lived active subscription.

| Property | Value |
|----------|-------|
| Price | $10/GB/month (configurable) |
| Minimum | 1GB increments |
| Billing | Prorated for mid-cycle changes |
| Trial Support | ❌ Not allowed on Trial plan |

### 5.2 Add Storage

**Endpoint**: `POST /billing/storage/set-target`

**Process**:
1. If `target > current` → immediate Stripe modification with proration
2. The system immediately syncs the new target storage amount and returns a Stripe invoice redirect URL
3. `invoice.paid` and `customer.subscription.updated` later synchronize payment and subscription state

**Prerequisites**:
- Active paid subscription (not Trial)
- Valid payment method

### 5.3 Reduce/Cancel Storage

**Process**:
1. If `target < current` → scheduled at period end via SubscriptionSchedule
2. `target_storage_bytes` updated in DB immediately
3. At period end → quantity change takes effect

**Special Case — Trial Downgrade**:
When downgrading to Trial, storage is **automatically cancelled** as part of the same schedule call (atomic operation).

### 5.4 Storage Lifecycle

```
Add (immediate, prorated)
  ↓
Active (billed with plan at renewal)
  ↓
Reduce/Cancel (scheduled at period end)
  ↓
Effective (quantity = 0 at period end)
```

---

## 7. Points System

### 6.1 Overview

Points are a **consumable currency** separate from plan quotas. Two types:

| Type | Source | Expiry |
|------|--------|--------|
| Plan Points | Included in plan quota | Resets each billing cycle |
| Addon Points | Purchased via recharge | No expiry |

### 6.2 Points Recharge

**Endpoint**: `POST /billing/points/checkout`

**Process**:
1. Creates Stripe Checkout session (`mode=payment`, not subscription)
2. User pays successfully → `checkout.session.completed` fires in `mode=payment`
3. Points credited to tenant's `PointAccount`
4. Ledger entry created with `event_type=recharge`

**Pricing**: Configured in `service_conf.yaml`:
```yaml
points_recharge:
  price_id: "price_xxx"
  points_per_unit: 100  # 1 unit = 100 points
```

### 6.3 Points Idempotency

Webhook handlers use `BillingWebhookEventService` to track processed events. Replaying the same event is **idempotent** — no duplicate credits.

### 6.4 Points Consumption

- Plan points consumed first (within quota)
- Addon points consumed when plan quota exhausted
- DeepDoc page parsing consumes `page_count × consuming_point_amount`
- In the current configuration, `consuming_point_amount = 100`, so one DeepDoc PDF page costs 100 points

---

## 8. Payment Failure & Recovery

### 7.1 Failure Flow

1. Renewal invoice fails → `invoice.payment_failed` webhook
2. Subscription status → `past_due`
3. `payment_required=true` in `GET /billing/plan_overview`
4. Frontend shows attention banner with invoice URL

### 7.2 Recovery

1. User updates payment method via Customer Portal
2. Stripe retries payment → `invoice.paid` webhook
3. Subscription status → `active`
4. `payment_required=false`

### 7.3 Delinquent Subscription Handling

When a subscription becomes delinquent:
- The main subscription is treated as non-entitled and is no longer in `active` / `trialing`
- If the user still wants to change to another paid plan, the API returns the current unpaid invoice URL and requires payment recovery first
- If the user changes to Trial, the system immediately cancels the delinquent subscription and also clears the storage addon target

---

## 9. API Endpoints Summary

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/billing/status` | Check if billing is enabled |
| GET | `/billing/current_plan` | Current plan + pending changes |
| GET | `/billing/plan_overview` | Full overview with resource usage |
| GET | `/billing/addon_overview` | Addon resource overview |
| POST | `/billing/checkout` | Initiate plan change |
| GET | `/billing/plans` | List available plans with quotas |
| GET | `/billing/addon_plans` | List purchasable addons |
| POST | `/billing/upcoming` | Preview upgrade invoice |
| GET | `/billing/storage/current` | Current storage status |
| POST | `/billing/storage/set-target` | Change storage quantity |
| POST | `/billing/points/checkout` | Purchase points |
| GET | `/billing/points/price` | View points recharge unit price |
| GET | `/billing/deepdoc/usage` | View DeepDoc parsing usage |
| GET | `/billing/points/balance` | Points balance |
| GET | `/billing/points/ledger` | Points transaction history |
| GET | `/billing/points/holds` | Point hold records |
| GET | `/billing/spend_overview` | Billing history |
| GET | `/billing/spend_metrics` | Aggregated billing metrics |
| POST | `/billing/webhook` | Stripe webhook handler |

---

## 10. Test Flow Reference

| Test | Description |
|------|-------------|
| `billing_plan01` | Full lifecycle: Trial→Starter→Pro→Starter→Trial→Starter |
| `billing_plan02` | Renewal failure → delinquent → recovery |
| `billing_plan03` | Plan upgrade with resource validation |
| `billing_plan04` | Downgrade with resource conflict detection |
| `billing_plan05` | Direct Stripe API plan change |
| `billing_storage01` | Storage addon purchase with proration |
| `billing_storage02` | Storage lifecycle + plan downgrade auto-cancel |
| `billing_point01` | Points purchase (100 points) |
| `billing_point05` | Points webhook idempotency |
| `billing_app01` | App quota enforcement with billing |
| `billing_app02` | Downgrade blocked by resource usage |
| `billing_member01-05` | Member quota enforcement flows |
