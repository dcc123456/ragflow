const billingEnabled = import.meta.env.BILLING_ENABLED === '1';

export const isBillingEnabled = (): boolean => billingEnabled;
