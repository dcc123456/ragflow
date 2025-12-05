import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import billingService, { billinCheckout } from '@/services/price';
import storagePrivate from '@/utils/authorization-private-util';
import storage from '@/utils/authorization-util';
import { useMutation, useQuery } from '@tanstack/react-query';
import { ICurrentPlan, IPlan, IPricePlanWithButton } from '../interface';
export type IChargePlan = {
  subscription_price_id?: string;
  quantity: string;
  usage_based_price_id?: string;
};
export const PriceChargeKey = 'price-charge';
const useCharge = () => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  const url = window.location.href;
  const successUrl = `${url.split('?')[0]}?price-pay-status=success${url.split('?')[1] || ''}`;
  const errorUrl = `${url.split('?')[0]}?price-pay-status=cancel${url.split('?')[1] || ''}`;

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: ['setDialog'],
    mutationFn: async (planId: string) => {
      const { data } = await billinCheckout({
        tenantId: tenantId,
        subscription_price_id: planId,
        payment_type: 'subscription',
        // quantity: chargePlan.quantity,
        quantity: '1',
        // usage_based_price_id: chargePlan.usage_based_price_id,
        usage_based_price_id: 'price_1RRTpfPtsKvwvC5fVsZly0mE',
        session_cancel_url: errorUrl,
        session_success_url: successUrl,
      });
      if (data.code === 0) {
        return data.data;
      }
      return data?.code;
    },
  });

  const charge = async (data: IPricePlanWithButton) => {
    if (data.isUse) {
      return;
    }
    if (data.id === 'Enterprise') {
      window.open('http://www.baidu.com');
    } else {
      const chargeResult = await mutateAsync(data.id);
      if (chargeResult && chargeResult.redirect_to) {
        window.open(chargeResult.redirect_to);
      }
    }
  };
  return { data, loading, charge };
};

const useFetchCurrentPlan = (force = false) => {
  const user = storage.getUserInfo();
  const { data, isFetching: loading } = useQuery<ICurrentPlan>({
    queryKey: ['currentPlan'],
    // initialData: {},
    enabled: !!user,
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await billingService.getCurrentPlan();
      if (res.code === 0) {
        const { data } = res;
        storagePrivate.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  return { data, loading };
};

const useFetchPlanList = (force = false) => {
  const { data, isFetching: loading } = useQuery<IPlan[]>({
    queryKey: ['getPlanList'],
    // initialData: {},
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await billingService.getPlanList();
      if (res.code === 0) {
        const { data } = res;
        // storage.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  return { data, loading };
};

const getNextMonth = {
  getNextMonthFirstDay: () => {
    const today = new Date();
    const nextMonth = new Date(today.getFullYear(), today.getMonth() + 1, 1);
    return nextMonth;
  },

  getNextMonthFirstDayFormatted: () => {
    const nextMonth = getNextMonth.getNextMonthFirstDay();
    const year = nextMonth.getFullYear();
    const month = String(nextMonth.getMonth() + 1).padStart(2, '0');
    const day = String(nextMonth.getDate()).padStart(2, '0');
    return `${year}-${month}-${day}`;
  },
};
export { getNextMonth, useCharge, useFetchCurrentPlan, useFetchPlanList };
