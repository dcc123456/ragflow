import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import {
  getBillingPointsBalance,
  getBillingPointsHolds,
  getBillingPointsLedger,
  postBillingPointsCheckout,
} from '@/services/price';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { BillingQueryKey } from '../../constants/query-keys';
import {
  IPointBalance,
  IPointHoldItem,
  IPointLedgerItem,
} from '../../interface';

export const useFetchPointsBalance = () => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IPointBalance>({
    queryKey: [BillingQueryKey.PointsBalance, tenantId],
    gcTime: 10000,
    enabled: !!tenantId,
    queryFn: async () => {
      const { data: res } = await getBillingPointsBalance(tenantId);
      if (res.code === 0) {
        return res.data;
      }
    },
  });
  return { data, loading, refetch };
};

export const useFetchPointsLedger = () => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  const [page, setPage] = useState(1);
  const PAGE_SIZE = 20;

  const { data, isFetching: loading } = useQuery<{
    total: number;
    items: IPointLedgerItem[];
  }>({
    queryKey: [BillingQueryKey.PointsLedger, tenantId, page],
    gcTime: 10000,
    enabled: !!tenantId,
    queryFn: async () => {
      const { data: res } = await getBillingPointsLedger({
        tenant_id: tenantId,
        page,
        page_size: PAGE_SIZE,
      });
      if (res.code === 0) return res.data;
      return { total: 0, items: [] };
    },
  });

  return { data, loading, page, setPage, pageSize: PAGE_SIZE };
};

export const useFetchPointsHolds = () => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  const [page, setPage] = useState(1);
  const PAGE_SIZE = 20;

  const { data, isFetching: loading } = useQuery<{
    total: number;
    items: IPointHoldItem[];
  }>({
    queryKey: [BillingQueryKey.PointsHolds, tenantId, page],
    gcTime: 10000,
    enabled: !!tenantId,
    queryFn: async () => {
      const { data: res } = await getBillingPointsHolds({
        tenant_id: tenantId,
        page,
        page_size: PAGE_SIZE,
      });
      if (res.code === 0) return res.data;
      return { total: 0, items: [] };
    },
  });

  return { data, loading, page, setPage, pageSize: PAGE_SIZE };
};

export const usePointsCheckout = () => {
  const queryClient = useQueryClient();
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;

  return useMutation({
    mutationFn: async (quantity: number) => {
      const url = window.location.href;
      const successUrl = `${url.split('?')[0]}?price-pay-status=success${url.split('?')[1] ? '&' + url.split('?')[1] : ''}`;
      const errorUrl = `${url.split('?')[0]}?price-pay-status=cancel${url.split('?')[1] ? '&' + url.split('?')[1] : ''}`;
      const { data: res } = await postBillingPointsCheckout({
        tenant_id: tenantId,
        quantity,
        session_success_url: successUrl,
        session_cancel_url: errorUrl,
      });
      return res;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: [BillingQueryKey.PointsBalance],
      });
    },
  });
};
