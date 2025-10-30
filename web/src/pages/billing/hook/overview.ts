import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import { getBllingBaseOverview, getBllingPlanPverview } from '@/services/price';
import { useQuery } from '@tanstack/react-query';
import { ISubscriptionData } from '../interface';

export const useFetchBaseOverview = (force = false) => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  const { data, isFetching: loading } = useQuery({
    queryKey: ['getBaseOverview', tenantId],
    // initialData: {},
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await getBllingBaseOverview({ tenantId });
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
  const { data, isFetching: loading } = useQuery<ISubscriptionData>({
    queryKey: ['getPlanOverview', tenantId],
    // initialData: {},
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await getBllingPlanPverview({ tenantId });
      if (res.code === 0) {
        const { data } = res;
        // storage.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  return { data, loading };
};
