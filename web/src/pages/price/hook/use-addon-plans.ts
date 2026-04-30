import { BillingQueryKey } from '@/pages/billing/constants/query-keys';
import { getAddonPlans } from '@/services/price';
import { useQuery } from '@tanstack/react-query';

export const useFetchAddonPlans = () => {
  const { data, isFetching: loading } = useQuery({
    queryKey: [BillingQueryKey.AddonPlans],
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
