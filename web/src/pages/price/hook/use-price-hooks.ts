import { useFetchTenantInfo } from '@/hooks/user-setting-hooks';
import billingService, { billinCheckout } from '@/services/price';
import storage from '@/utils/authorization-util';
import { useQuery } from '@tanstack/react-query';
export type IChargePlan = {
  subscription_price_id: string;
  quantity: string;
  usage_based_price_id: string;
};
const useCharge = (chargePlan: IChargePlan) => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  // 获取当前url
  const url = window.location.href;
  const successUrl = `${url.split('?')[0]}?price-pay-status=success${url.split('?')[1] || ''}`;
  const errorUrl = `${url.split('?')[0]}?price-pay-status=cancel${url.split('?')[1] || ''}`;
  const { data, isFetching: loading } = useQuery<{
    redirect_to: string;
  }>({
    queryKey: [
      tenantId,
      chargePlan.subscription_price_id,
      chargePlan.usage_based_price_id,
    ],
    // initialData: { docs: [], total: 0 },
    enabled:
      !!tenantId &&
      !!chargePlan.subscription_price_id &&
      !!chargePlan.usage_based_price_id,
    queryFn: async () => {
      const ret = await billinCheckout({
        tenantId: tenantId,
        subscription_price_id: chargePlan.subscription_price_id,
        payment_type: 'subscription',
        quantity: chargePlan.quantity,
        usage_based_price_id: chargePlan.usage_based_price_id,
        session_cancel_url: errorUrl,
        session_success_url: successUrl,
      });
      if (ret.data.code === 0) {
        return ret.data.data;
      }

      return {};
    },
  });
  return { data, loading };
};

const useFetchCurrentPlan = (force = false) => {
  const { data, isFetching: loading } = useQuery({
    queryKey: ['currentPlan'],
    // initialData: {},
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await billingService.getCurrentPlan();
      if (res.code === 0) {
        const { data } = res;
        storage.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  return { data, loading };
};
export { useCharge, useFetchCurrentPlan };
