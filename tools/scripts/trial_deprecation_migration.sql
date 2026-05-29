-- ============================================================
-- Trial Subscription Cleanup Migration
-- Description: Remove subscription_id from Trial plan records
--              to prevent Stripe webhook pollution and ensure
--              Trial tenants use only local quota control.
-- ============================================================
-- This migration is for existing data where Trial tenants may
-- have a leftover subscription_id from the pre-Plan B behavior.
--
-- Run AFTER deploying the Plan B code changes.
-- ============================================================

-- Dry-run first (run without the COMMIT to see what would change):
-- BEGIN;
-- SELECT id, tenant_id, plan_name, subscription_id, subscription_status
-- FROM billing_subscription
-- WHERE plan_name = 'Trial' AND subscription_id != '';
-- ROLLBACK;  -- remove this line to actually apply

BEGIN;

UPDATE billing_subscription
SET
    subscription_id = '',
    subscription_status = ''
WHERE plan_name = 'Trial'
  AND subscription_id IS NOT NULL
  AND subscription_id != '';

-- Verify
SELECT id, tenant_id, plan_name, subscription_id
FROM billing_subscription
WHERE plan_name = 'Trial';

COMMIT;