/**
 * Runtime billing status service.
 * Fetches billing enabled state from backend at runtime, instead of relying on
 * the build-time VITE_BILLING_ENABLED environment variable.
 */

let billingEnabled: boolean | null = null;

export const fetchBillingStatus = async (): Promise<boolean> => {
  try {
    const res = await fetch('/v1/billing/status');
    const data = await res.json();
    billingEnabled = data.billing_enabled === true;
    return billingEnabled;
  } catch {
    billingEnabled = false;
    return false;
  }
};

export const isBillingEnabled = (): boolean | null => billingEnabled;
