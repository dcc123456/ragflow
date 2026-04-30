import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import {
  getBillingDeepDocUsage,
  getBillingPlanOverview,
} from '@/services/price';
import { useQuery } from '@tanstack/react-query';
import { BillingQueryKey } from '../constants/query-keys';
import { IPayStatusData, ISubscriptionData } from '../interface';

export const useFetchBaseOverview = (force = false) => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  const { data, isFetching: loading } = useQuery({
    queryKey: [BillingQueryKey.BaseOverview, tenantId],
    // initialData: {},
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await getBillingPlanOverview({ tenantId });
      if (res.code === 0) {
        const { data } = res;
        // storage.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  return { data, loading };
};
export const useFetchPlanOverview = (force = false) => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<ISubscriptionData>({
    queryKey: [BillingQueryKey.PlanOverview, tenantId],
    // initialData: {},
    staleTime: force ? 0 : Infinity,
    gcTime: force ? 0 : 50000,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data: res } = await getBillingPlanOverview({ tenantId });
      if (res.code === 0) {
        const { data } = res;
        // storage.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  return { data, loading, refetch };
};

export const useFetchDeepDocUsage = () => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IPayStatusData>({
    queryKey: [BillingQueryKey.DeepDocUsage, tenantId],
    gcTime: 30000,
    enabled: !!tenantId,
    queryFn: async () => {
      const { data: res } = await getBillingDeepDocUsage(tenantId);
      if (res.code === 0) return res.data;
    },
  });
  return { data, loading, refetch };
};
