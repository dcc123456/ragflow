import { BillingQueryKey } from '@/pages/billing/constants/query-keys';
import { ICheckoutResult } from '@/pages/price/interface';
import { getDirectSubscriptionPaymentStatus } from '@/services/price';
import { useQuery } from '@tanstack/react-query';
import { useCallback, useEffect, useState } from 'react';

export const DirectPaymentAttemptKey = 'billing-direct-payment-attempt';
export const DirectPaymentAttemptEvent = 'billing-direct-payment-attempt';

export interface DirectPaymentStatus extends ICheckoutResult {
  subscription_id: string;
  payment_state: NonNullable<ICheckoutResult['payment_state']>;
  entitlement_ready: boolean;
}

export const directPaymentRefetchInterval = (status?: DirectPaymentStatus) => {
  if (!status || status.payment_state === 'pending') return 3000;
  if (status.payment_state === 'requires_action') return 3000;
  if (status.payment_state === 'paid' && !status.entitlement_ready) return 3000;
  return false;
};

type DirectPaymentAttempt = Pick<
  ICheckoutResult,
  | 'subscription_id'
  | 'invoice_id'
  | 'plan_name'
  | 'price_id'
  | 'amount_cents'
  | 'currency'
  | 'product_type'
>;

const readAttempt = (): DirectPaymentAttempt | null => {
  if (typeof window === 'undefined') return null;
  const raw = sessionStorage.getItem(DirectPaymentAttemptKey);
  if (!raw) return null;
  try {
    const attempt = JSON.parse(raw) as DirectPaymentAttempt;
    return attempt.subscription_id ? attempt : null;
  } catch {
    sessionStorage.removeItem(DirectPaymentAttemptKey);
    return null;
  }
};

export const trackDirectPaymentAttempt = (result?: ICheckoutResult) => {
  if (!result?.subscription_id) return;
  const attempt: DirectPaymentAttempt = {
    subscription_id: result.subscription_id,
    invoice_id: result.invoice_id,
    plan_name: result.plan_name,
    price_id: result.price_id,
    amount_cents: result.amount_cents,
    currency: result.currency,
    product_type: result.product_type,
  };
  sessionStorage.setItem(DirectPaymentAttemptKey, JSON.stringify(attempt));
  window.dispatchEvent(
    new CustomEvent(DirectPaymentAttemptEvent, { detail: attempt }),
  );
};

export const useDirectPaymentStatus = () => {
  const [attempt, setAttempt] = useState<DirectPaymentAttempt | null>(() =>
    readAttempt(),
  );

  useEffect(() => {
    const handleAttempt = (event: Event) => {
      setAttempt((event as CustomEvent<DirectPaymentAttempt>).detail);
    };
    window.addEventListener(DirectPaymentAttemptEvent, handleAttempt);
    return () =>
      window.removeEventListener(DirectPaymentAttemptEvent, handleAttempt);
  }, []);

  const query = useQuery<DirectPaymentStatus>({
    queryKey: [
      BillingQueryKey.DirectPaymentStatus,
      attempt?.subscription_id,
      attempt?.invoice_id,
    ],
    enabled: !!attempt?.subscription_id,
    queryFn: async () => {
      const { data: response } = await getDirectSubscriptionPaymentStatus(
        attempt!.subscription_id!,
        attempt?.invoice_id,
      );
      if (response?.code === 0 && response.data) {
        return response.data as DirectPaymentStatus;
      }
      throw new Error('Direct payment status request failed');
    },
    refetchInterval: (query) => directPaymentRefetchInterval(query.state.data),
    retry: false,
  });

  const clear = useCallback(() => {
    sessionStorage.removeItem(DirectPaymentAttemptKey);
    setAttempt(null);
  }, []);

  return { ...query, attempt, clear };
};
