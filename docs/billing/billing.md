# RAGFlow Billing System Documentation

## 1. Overview

RAGFlow uses Stripe as its payment provider with a **single-subscription-per-tenant** model. Each tenant has one subscription that can contain multiple products (plan + storage addon). All billing operations go through Stripe Checkout or direct Stripe API calls, with webhook handlers synchronizing state back to the local database.

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
| `checkout.session.completed` | New subscription or upgrade initiated |
| `invoice.paid` | Payment received (renewal, upgrade, addon) |
| `invoice.payment_failed` | Payment failure → delinquent status |
| `customer.subscription.updated` | Plan/storage change effective |
| `customer.subscription.deleted` | Subscription cancelled |
| `payment_intent.succeeded` | One-off payment (points recharge) |

---

## 2. Subscription Plans

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
| `quota_apps` | 5 | 100 | 999999999 |
| `quota_members` | 3 | 10 | 999999999 |
| `quota_storage` | 1GB | 10GB | 100GB |
| `quota_points` | 100 | 1000 | 10000 |

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

## 3. Plan Upgrade/Downgrade Flow

### 3.1 Trial → Starter (Upgrade)

**Trigger**: User completes Stripe Checkout session.

**Process**:
1. Frontend calls `POST /billing/checkout` with Starter `price_id`
2. Backend creates Stripe Checkout session → returns `checkout_url`
3. User pays on Stripe → `checkout.session.completed` webhook fires
4. Webhook handler creates subscription in DB, sets plan to "Starter"
5. Billing cycle starts immediately (new cycle from now)

**Prerequisites**:
- Tenant must have a Stripe `customer_id`
- Valid payment method required

**Effective Time**: Immediate (starts new billing cycle)

### 3.2 Starter → Pro (Upgrade)

**Trigger**: User calls `POST /billing/checkout` with Pro `price_id`.

**Process**:
1. Backend calls `modify_subscription_plan_async()` on Stripe
2. Stripe charges prorated difference immediately (`proration_behavior: always_invoice`)
3. `invoice.paid` webhook confirms payment
4. DB updated to "Pro" plan

**Prerequisites**:
- Active Starter subscription
- Valid payment method on file

**Effective Time**: Immediate (prorated charge billed today)

### 3.3 Pro → Starter (Downgrade)

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

### 3.4 Starter → Trial (Downgrade)

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
  - `points_used ≤ Trial quota_points`

**Effective Time**: At current billing period end

**Important**: Trial plan does NOT support storage addons. Any existing storage addon is cancelled when downgrading to Trial.

---

## 4. Resource Compatibility Check

Before any downgrade, `_check_downgrade_resource_compatibility()` validates:

```python
conflicts = []
if storage_used > target_quota_storage + addon_storage:
    conflicts.append({"resource": "storage", ...})
if points_used > target_quota_points:
    conflicts.append({"resource": "points", ...})
if members_used > target_quota_members:
    conflicts.append({"resource": "members", ...})
if apps_used > target_quota_apps:
    conflicts.append({"resource": "apps", ...})
```

**Response on conflict**:
```json
{
  "code": 40006,
  "data": {"resource_conflicts": [...]},
  "message": "Resource usage exceeds Trial quota: storage, apps. ..."
}
```

---

## 5. Storage Addon

### 5.1 Overview

Storage addon is a **line item on the same subscription** as the plan. It is NOT a separate subscription.

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
2. Stripe creates invoice for prorated amount
3. `invoice.paid` webhook updates DB

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

## 6. Points System

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
2. User pays → `payment_intent.succeeded` webhook fires
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
- DeepDoc page parsing consumes points based on `consuming_point_amount`

---

## 7. Payment Failure & Recovery

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
- Existing resources remain accessible (read-only may apply)
- No new resources can be created
- Downgrade is blocked until payment recovered or resources reduced

---

## 8. API Endpoints Summary

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/billing/status` | Check if billing is enabled |
| GET | `/billing/current_plan` | Current plan + pending changes |
| GET | `/billing/plan_overview` | Full overview with resource usage |
| POST | `/billing/checkout` | Initiate plan change |
| GET | `/billing/all_plans` | List available plans with quotas |
| POST | `/billing/upcoming` | Preview upgrade invoice |
| GET | `/billing/storage/current` | Current storage status |
| POST | `/billing/storage/set-target` | Change storage quantity |
| POST | `/billing/points/checkout` | Purchase points |
| GET | `/billing/points/balance` | Points balance |
| GET | `/billing/points/ledger` | Points transaction history |
| GET | `/billing/spend_overview` | Billing history |
| POST | `/billing/webhook` | Stripe webhook handler |

---

## 9. Test Flow Reference

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
