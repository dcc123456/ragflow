import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import {
  getBillingPointsBalance,
  getBillingPointsHolds,
  getBillingPointsLedger,
  postBillingPointsCheckout,
} from '@/services/price';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
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
    queryKey: ['getPointsBalance', tenantId],
    gcTime: 10000,
    enabled: !!tenantId,
    queryFn: async () => {
      const { data: res } = await getBillingPointsBalance(tenantId);
      if (res.code === 0) {
        const balance = res.data;
        return {
          ...balance,
          available_points: balance.available_points ?? ((balance.available_plan_points ?? 0) + (balance.available_addon_points ?? 0)),
        };
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
    queryKey: ['getPointsLedger', tenantId, page],
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
    queryKey: ['getPointsHolds', tenantId, page],
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
      const { data: res } = await postBillingPointsCheckout({
        tenant_id: tenantId,
        quantity,
      });
      return res;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['getPointsBalance'] });
    },
  });
};
