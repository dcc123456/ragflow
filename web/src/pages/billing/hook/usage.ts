import billingService from '@/services/price';
import { useQuery } from '@tanstack/react-query';
import { Invoice } from '../interface';

export const useAllSpends = (
  { start, end }: { start: number; end: number },
  force?: boolean,
) => {
  // const { data: tenantInfo } = useFetchTenantInfo();
  //   const tenantId = tenantInfo?.tenant_id;
  const { data, isFetching: loading } = useQuery<Invoice[]>({
    queryKey: ['getAllSpends', start, end],
    // initialData: {},
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await billingService.planSpendOverview({
        start,
        end,
      });
      console.log('spendData', data, res);
      if (res.code === 0) {
        const { data } = res;
        // storage.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  return { data, loading };
};
