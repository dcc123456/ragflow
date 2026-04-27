import { useQuery } from '@tanstack/react-query';
import { getAddonPlans } from '@/services/price';

export const useFetchAddonPlans = () => {
  const { data, isFetching: loading } = useQuery({
    queryKey: ['addonPlans'],
    queryFn: async () => {
      const { data: res } = await getAddonPlans();
      if (res?.code === 0) return res.data;
      return [];
    },
  });

  const storagePlan = (data || []).find((p: any) => p.name === 'storage');
  const pricePerGB = storagePlan?.feature?.price_per_gb ?? 0;

  return { data, loading, pricePerGB };
};
