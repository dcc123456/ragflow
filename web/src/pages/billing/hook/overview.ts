import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import {
  getBillingPaygStatus,
  getBllingBaseOverview,
  getBllingPlanPverview,
  postBillingPaygDisable,
  postBillingPaygEnable,
} from '@/services/price';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { IPaygStatusData, ISubscriptionData } from '../interface';

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

export const useFetchPaygStatus = () => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IPaygStatusData>({
    queryKey: ['getPaygStatus', tenantId],
    gcTime: 30000,
    enabled: !!tenantId,
    queryFn: async () => {
      const { data: res } = await getBillingPaygStatus(tenantId);
      if (res.code === 0) {
        return res.data;
      }
    },
  });
  return { data, loading, refetch };
};

export const usePaygEnable = () => {
  const queryClient = useQueryClient();
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  return useMutation({
    mutationFn: async () => {
      const { data: res } = await postBillingPaygEnable({
        tenant_id: tenantId,
      });
      return res;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['getPaygStatus'] });
    },
  });
};

export const usePaygDisable = () => {
  const queryClient = useQueryClient();
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  return useMutation({
    mutationFn: async (params?: { confirmed?: boolean }) => {
      const { data: res } = await postBillingPaygDisable({
        tenant_id: tenantId,
        ...params,
      });
      return res;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['getPaygStatus'] });
    },
  });
};
