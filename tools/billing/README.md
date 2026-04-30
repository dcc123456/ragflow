# Billing Integration Test Suite

Automated end-to-end test suite for RAGFlow's billing API integration with Stripe. Located in `tools/billing/`.

## Overview

This suite contains 5 test scripts (`billing_plan0{1-5}_api_flow.py`) that validate complete billing workflows using Stripe's test clock and webhook replay mechanisms. Each script is self-contained and can be run independently.

### Test Scripts

| Script | Test Scenario | Key Validations |
|--------|--------------|-----------------|
| `billing_plan01_api_flow.py` | Full subscription lifecycle | Trial→Pro→renew→Starter→renew→Trial→renew→Starter |
| `billing_plan02_api_flow.py` | Renewal failure & recovery | Failed renewal → attention banner → invoice recovery → same history row becomes paid |
| `billing_plan03_api_flow.py` | Customer Portal upgrade | Starter→Pro upgrade via Stripe Customer Portal, quota entitlement |
| `billing_plan04_api_flow.py` | Cancel scheduled downgrade | Schedule downgrade Pro→Starter, cancel before period end, verify no change |
| `billing_plan05_api_flow.py` | Unpaid invoice prevents entitlement | Starter→Pro upgrade invoice unpaid means Starter entitlement remains until recovery payment |

## Prerequisites

### 1. Services Running

Start all dependent services:

```bash
docker compose -f docker/docker-compose-base.yml up -d
```

Ensure MySQL, Redis, Elasticsearch/Infinity, and MinIO are healthy.

### 2. Backend Server

Launch the RAGFlow backend:

```bash
source .venv/bin/activate
export PYTHONPATH=$(pwd)
bash docker/launch_backend_service.sh
```

The backend should be accessible at `http://127.0.0.1:9380` (or as configured via `RAGFLOW_BASE_URL`).

### 3. Stripe Test Configuration

- **Stripe Secret Key**: Set `BILLING_STRIPE_API_KEY` to a **test-mode** key (starts with `sk_test_`).
- **API Version**: The suite explicitly uses `2026-02-25.clover`. Set `STRIPE_API_VERSION` to match or leave unset to use the default.
- **Test Clock**: Each test creates an isolated Stripe Test Clock to control time precisely.

### 4. Environment Variables

#### Required

| Variable | Description | Example |
|----------|-------------|---------|
| `BILLING_STRIPE_API_KEY` | Stripe secret key (test mode) | `sk_test_...` |
| `BILLING_PRICE_ID_TRIAL` | Stripe price ID for Trial plan | `price_...` |
| `BILLING_PRICE_ID_STARTER` | Stripe price ID for Starter plan | `price_...` |
| `BILLING_PRICE_ID_PRO` | Stripe price ID for Pro plan | `price_...` |
| `BILLING_POINTS_PRICE_ID` | Stripe price ID for points recharge | `price_...` |
| `RAGFLOW_BASE_URL` | RAGFlow backend URL | `http://127.0.0.1:9380` |
| `RAGFLOW_API_VERSION` | API version (usually `v1`) | `v1` |

#### Optional

| Variable | Default | Description |
|----------|---------|-------------|
| `RAGFLOW_SERVICE_CONF` | `conf/service_conf.yaml` | Path to service config |
| `STRIPE_API_VERSION` | (none) | Must match `2026-02-25.clover` if set |
| `RAGFLOW_TEST_EMAIL` | auto-generated | Email for test user |
| `RAGFLOW_TEST_PASSWORD` | `Test1234!` | Password for test user |
| `BILLING_POINTS_PER_UNIT` | config fallback | Override `billing.points_recharge.points_per_unit` for points flows |
| `BILLING_WEBHOOK_SECRET` / `STRIPE_WEBHOOK_SECRET` | DB fallback | Optional in `manual` mode if `billing_webhook_secret` is already persisted in local DB |
| `RAGFLOW_BILLING_WEBHOOK_MODE` | `manual` | `manual` (replay) or `stripe-cli` (wait) |
| `RAGFLOW_WEBHOOK_WAIT_SECONDS` | `8` | Wait time in auto mode |
| `RAGFLOW_WEBHOOK_TIMEOUT_SECONDS` | `180` | Webhook sync timeout |
| `RAGFLOW_READY_TIMEOUT_SECONDS` | `180` | Server ready timeout |

### 5. Price IDs in Config

The scripts read billing configuration from `conf/service_conf.yaml` (or fallback to `service_conf.yaml` at project root). Ensure it contains:

```yaml
billing:
  stripe_api_version: "2026-02-25.clover"
  billing_plans:
    - name: "Trial"
      price_ids: "price_..."
    - name: "Starter"
      price_ids: "price_..."
    - name: "Pro"
      price_ids: "price_..."
```

## Running Tests

### Basic Usage

```bash
cd tools/billing
python billing_plan01_api_flow.py
```

For points-recharge flows, you can override the recharge Stripe price and unit size directly from the environment:

```bash
export BILLING_POINTS_PRICE_ID=price_...
export BILLING_POINTS_PER_UNIT=100
python billing_point01_api_flow.py
```

### Launching Points Gates From Repo Root

Run the points-recharge validation flows from the repository root:

```bash
cd /home/infiniflow/workspace/close/now_enter
```

Export the minimum required Stripe key:

```bash
export BILLING_STRIPE_API_KEY='sk_test_...'
```

If you want the points flow to use environment overrides instead of `conf/service_conf.yaml`, export these too:

```bash
export BILLING_POINTS_PRICE_ID='price_...'
export BILLING_POINTS_PER_UNIT='100'
```

You can also populate those two variables directly from `conf/service_conf.yaml`:

```bash
eval "$(
./.venv/bin/python - <<'PY'
import yaml
conf = yaml.safe_load(open('conf/service_conf.yaml')) or {}
billing = conf.get('billing') or {}
recharge = billing.get('points_recharge') or {}
print(f"export BILLING_POINTS_PRICE_ID='{recharge.get('price_id', '')}'")
print(f"export BILLING_POINTS_PER_UNIT='{recharge.get('points_per_unit', '')}'")
PY
)"
```

Then run the five points gates one by one:

```bash
./.venv/bin/python -u tools/billing/billing_point01_api_flow.py
./.venv/bin/python -u tools/billing/billing_point02_api_flow.py
./.venv/bin/python -u tools/billing/billing_point03_api_flow.py
./.venv/bin/python -u tools/billing/billing_point04_api_flow.py
./.venv/bin/python -u tools/billing/billing_point05_api_flow.py
```

Notes for points flows:

- `billing_webhook_secret` is loaded automatically from the local DB if `BILLING_WEBHOOK_SECRET` / `STRIPE_WEBHOOK_SECRET` is unset.
- `--webhook-mode manual` is for the `billing_plan0x` scripts, not the `billing_point0x` scripts.
- The `billing_point0x` scripts no longer require `export PYTHONPATH=$(pwd)` when launched from the repo root.

All scripts accept `--help` for options:

```bash
python billing_plan03_api_flow.py --help
```

### Common Arguments

| Argument | Default | Description |
|----------|---------|-------------|
| `--base-url` | `http://127.0.0.1:9380` | RAGFlow backend URL |
| `--version` | `v1` | API version path |
| `--email` | auto-generated | Test user email (must be unique per run) |
| `--password` | `Test1234!` | Test user password |
| `--webhook-mode` | `manual` | `manual` (replay events) or `stripe-cli` |
| `--webhook-wait-seconds` | `8` | Wait duration in auto mode |
| `--webhook-timeout-seconds` | `180` | Webhook sync timeout |
| `--ready-timeout-seconds` | `180` | Server ready timeout |

### Webhook Modes

- **`manual`** (default): The script uses Stripe's Test Clock `replay_events` API to fetch and re-send events to the local webhook endpoint. No external dependencies.
- **`stripe-cli`**: The script waits for Stripe CLI (`stripe listen --forward-to ...`) to deliver events. Use when you want live event forwarding.

### Exit Codes

- `0` — Test passed
- `1` — Test failed (error message printed to stderr)

## Test Descriptions

### PLAN-01: Full Subscription Lifecycle

**File**: `billing_plan01_api_flow.py`

Tests the complete billing journey:
1. Register & login a fresh test tenant and confirm the default plan is `Trial`
2. Upgrade from `Trial` to `Pro` and verify immediate payment + entitlement
3. Advance one billing period and verify successful `Pro` renewal
4. Schedule `Pro` → `Starter`, advance one billing period, and verify the downgrade takes effect at period end
5. Schedule `Starter` → `Trial`, advance one billing period, and verify the tenant becomes `Trial`
6. Verify the `Trial` cycle does not create a paid renewal row
7. Upgrade again from `Trial` to `Starter` and verify immediate payment + entitlement

**Key assertions**: plan transitions, period-end downgrade behavior, billing-cycle advancement, quota transitions, and no paid renewal during the `Trial` cycle.

### PLAN-02: Renewal Failure & Recovery

**File**: `billing_plan02_api_flow.py`

Tests the attention-banner flow:
1. Register a fresh tenant and move it to `Pro`
2. Remove the default payment method to simulate the portal-side payment-method removal / invalidation case
3. Advance one billing period so the renewal invoice fails
4. Verify delinquent state and the red payment-attention signal
5. Recover the same failed invoice by re-attaching a card and paying it
6. Verify the attention state clears and service returns to normal

**Key assertions**: `payment_required`, delinquent subscription status, exactly one failed invoice row before recovery, and in-place history-row update from failed/unpaid to paid.

### PLAN-03: Customer Portal Upgrade

**File**: `billing_plan03_api_flow.py`

Tests the Stripe Customer Portal flow:
1. Prepare a tenant already on `Starter`
2. Call `/billing/checkout` → expects `redirect_to` portal URL (not direct charge)
3. Simulate portal completion using the same always-invoice behavior as the real portal configuration
4. Verify the tenant switches to `Pro` only after payment + webhook sync complete
5. Verify `billing_overview` reflects the configured `Pro` quota
6. Verify billing history records the upgrade invoice

**Key assertions**: Portal redirect URL, payment-gated plan switch, configured `Pro` quota entitlement, and upgrade invoice visibility in billing history.

### PLAN-04: Cancel Scheduled Downgrade

**File**: `billing_plan04_api_flow.py`

Tests downgrade scheduling and cancellation:
1. Start on Pro plan
2. Schedule downgrade to Starter via `/billing/checkout` (at period end)
3. Verify `pending_subscription_change` appears in `current_plan`
4. Cancel the scheduled change via API
5. Verify banner disappears
6. Advance clock to original period end
7. Verify still on Pro (downgrade did not happen)

**Key assertions**: `pending_subscription_change` presence/absence, plan persistence after period end.

### PLAN-05: Unpaid Invoice Prevents Entitlement

**File**: `billing_plan05_api_flow.py`

Tests that upgrade doesn't grant entitlements until invoice paid:
1. Prepare a tenant already on `Starter`
2. Trigger the paid-plan upgrade path (`Starter` → `Pro`) through the Customer Portal redirect flow
3. Simulate the portal-side upgrade while leaving the proration invoice unpaid
4. Immediately check `current_plan` and `plan_overview`
   - **Expected**: plan and entitlement remain at `Starter`, and payment-attention state is recoverable
5. Pay the same unpaid upgrade invoice
6. Verify `Pro` entitlement unlocks only after payment + webhook sync

**Key assertions**: no premature `Pro` entitlement, recoverable unpaid-upgrade state, and post-payment switch to configured `Pro` quota.

## Stripe API Version

All scripts explicitly enforce `stripe.api_version = "2026-02-25.clover"`. This is set in each script's `run_flow()` function with fallback from `service_conf.yaml`. If `STRIPE_API_VERSION` environment variable is set and differs, the script fails immediately to prevent version drift.

## Architecture

### RAGFlowClient

Each script defines a `RAGFlowClient` class encapsulating:
- Base URL construction
- Authentication header management (JWT from login)
- JSON request/response handling with error translation (`FlowError`)
- Webhook event posting with HMAC signature
- Helper methods: `current_plan()`, `plan_overview()`, `spend_history()`, `schedule_plan_change()`

### Test Helpers

Common helper functions (across all scripts):
- `env()` / `require_env()` - environment variable loading
- `load_billing_config()` - YAML config parser with fallbacks
- `stripe_dict()` - Stripe object → dict serializer
- `create_paid_subscription()` - Stripe subscription creation with metadata
- `attach_default_test_card()` - attach `tok_visa` to customer
- `create_clock_customer()` - Stripe customer scoped to test clock
- `bind_local_subscription_customer()` - sync Stripe customer_id to local DB
- `wait_for_plan()` / `wait_for_plan_status()` - polling with timeout
- `parse_plan_end()` - multi-format timestamp parser
- `sync_webhooks()` / `replay_stripe_events()` - webhook delivery
- `advance_clock()` / `wait_for_clock()` - test clock control

### Stripe Test Clock

Each test creates a Test Clock at start:
```python
clock = stripe.test_helpers.TestClock.create(frozen_time=int(time.time()), name=f"ragflow-planXX-{uuid.uuid4().hex[:8]}")
clock_id = stripe_dict(clock)["id"]
```

The clock ID is passed to RAGFlow backend via `X-Test-Clock` header, enabling deterministic time manipulation for subscription transitions.

## Troubleshooting

### "Stripe API version mismatch"

Ensure `BILLING_STRIPE_API_KEY` is a **test key** (`sk_test_`). The check happens early in `run_flow()`:

```python
stripe_api_version = str(billing_config.get("stripe_api_version") or "2026-02-25.clover")
stripe_api_version_override = env("STRIPE_API_VERSION")
if stripe_api_version_override and stripe_api_version_override != stripe_api_version:
    raise FlowError(f"STRIPE_API_VERSION={stripe_api_version_override} does not match service_conf.yaml={stripe_api_version}")
```

Fix: Either unset `STRIPE_API_VERSION` or set it to `2026-02-25.clover`.

### "service config not found"

Set `RAGFLOW_SERVICE_CONF` or ensure `conf/service_conf.yaml` exists at project root. The file must contain `billing:` section with `billing_plans` and `stripe_api_version`.

### "No matching subscription / Webhook timeout"

- Verify the RAGFlow backend is running and reachable at `RAGFLOW_BASE_URL`.
- Check that `billing` feature is enabled in `service_conf.yaml`.
- Ensure Stripe webhook endpoint is configured in RAGFlow and points to `/billing/webhook`.
- In `manual` mode, the script replays events automatically; in `stripe-cli` mode, you must run:
  ```bash
  stripe listen --forward-to localhost:9380/v1/billing/webhook
  ```

### "Invoice already settled" in PLAN-05

This indicates the simulated Customer Portal upgrade did not leave a recoverable unpaid proration invoice. The script expects an unpaid upgrade invoice; if Stripe settles it immediately, the flow no longer matches the target case and the test should fail fast.

### "timed out waiting for..."

All polling helpers (`wait_for_plan`, `wait_for_history_count`, etc.) have configurable timeouts (`--webhook-timeout-seconds`, `--ready-timeout-seconds`). Increase these if your environment is slow.

### Python Dependencies

The scripts use project dependencies. Run inside the project venv:

```bash
source .venv/bin/activate
```

Required packages: `requests`, `stripe`, `pyyaml`, `passlib` (for `crypt`), `ragflow` (local modules from `api/`).

## Code Quality

- **Linting**: `ruff check tools/billing/` — all scripts pass
- **Formatting**: `ruff format tools/billing/` — auto-formatted with 200-char line length
- **Docstrings**: Every public function/class has a concise docstring; inline comments explain non-obvious test logic
- **Type Safety**: Full type hints (`dict[str, Any]`, `set[str]`, etc.). No `# type: ignore` or `as any`.

## Files

```
tools/billing/
├── billing_plan01_api_flow.py   # 27K - Full lifecycle
├── billing_plan02_api_flow.py   # 26K - Renewal failure/recovery
├── billing_plan03_api_flow.py   # 23K - Customer Portal upgrade
├── billing_plan04_api_flow.py   # 25K - Cancel scheduled downgrade
├── billing_plan05_api_flow.py   # 30K - Unpaid invoice guard
└── README.md                    # This file
```

## Notes

- Each test creates a **unique user** via `f"billing-plan0X-{uuid.uuid4().hex[:12]}@example.test"` to avoid collisions.
- Test **clock IDs** are tracked and cleaned up by Stripe (test clocks auto-expire).
- The scripts are **idempotent** in that each run creates fresh isolated state (new user, new subscriptions, new test clock).
- **Do not** run multiple scripts concurrently for the same Stripe account; they may interfere via rate limits.
