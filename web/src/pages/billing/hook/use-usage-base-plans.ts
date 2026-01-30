import billingService from '@/services/price';
import { useQuery } from '@tanstack/react-query';

export const useFetchUsageBasedPlans = (force = false) => {
  const { data, isFetching: loading } = useQuery({
    queryKey: ['getUsageBasedPlans'],
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await billingService?.usageBasedPlans();
      if (res.code === 0) {
        const { data } = res;
        return data;
      }
    },
  });

  return { data, loading };
};
