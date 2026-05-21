import { getBillingSession } from '@/services/price';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import {
  isPaymentSuccess,
  type StripePaymentStatus,
} from '../constants/payment-status';
import { BillingQueryKey } from '../constants/query-keys';

export interface SessionData {
  status: StripePaymentStatus;
  amount?: number;
  credits?: number;
  storage_gb?: number;
  invoice_id?: string;
  invoice_url?: string;
  invoice_pdf_url?: string;
  receipt_url?: string;
  currency?: string;
  subscription_id?: string;
  plan_name?: string;
  price_id?: string;
  product_type?: 'subscription' | 'storage' | 'points';
}

interface RawSessionResponse {
  payment_status: StripePaymentStatus;
  amount_cents?: number;
  currency?: string;
  metadata?: {
    points_amount?: string;
    product_type?: 'subscription' | 'storage' | 'points';
  };
  invoice_id?: string;
  invoice_url?: string;
  invoice_pdf_url?: string;
  receipt_url?: string;
  payment_intent_id?: string;
}

const PollingInterval = 3000;

export const useFetchPaymentSession = (
  sessionId: string | null,
  enabled: boolean,
) => {
  const queryClient = useQueryClient();

  const query = useQuery<SessionData>({
    queryKey: [BillingQueryKey.BillingSession, sessionId],
    enabled: enabled && !!sessionId,
    refetchInterval: enabled && !!sessionId ? PollingInterval : false,
    queryFn: async () => {
      const { data: res } = await getBillingSession(sessionId!);
      if (res.code === 0 && res.data) {
        const raw = res.data as RawSessionResponse;
        return {
          status: raw.payment_status,
          amount: raw.amount_cents ? raw.amount_cents / 100 : undefined,
          credits: raw.metadata?.points_amount
            ? Number(raw.metadata.points_amount)
            : undefined,
          currency: raw.currency,
          invoice_id: raw.invoice_id,
          invoice_url: raw.invoice_url,
          invoice_pdf_url: raw.invoice_pdf_url,
          receipt_url: raw.receipt_url,
          product_type: raw.metadata
            ?.product_type as SessionData['product_type'],
        };
      }
      throw new Error(res.message || 'Session check failed');
    },
    retry: false,
  });

  useEffect(() => {
    if (query.data && isPaymentSuccess(query.data.status)) {
      queryClient.invalidateQueries({
        queryKey: [BillingQueryKey.CurrentPlan],
      });
      queryClient.invalidateQueries({
        queryKey: [BillingQueryKey.PlanOverview],
      });
      queryClient.invalidateQueries({
        queryKey: [BillingQueryKey.BaseOverview],
      });
      queryClient.invalidateQueries({
        queryKey: [BillingQueryKey.PointsBalance],
      });
      queryClient.invalidateQueries({
        queryKey: [BillingQueryKey.PointsOverview],
      });
      queryClient.invalidateQueries({
        queryKey: [BillingQueryKey.StorageCurrent],
      });
    }
  }, [query.data, queryClient]);

  return query;
};
