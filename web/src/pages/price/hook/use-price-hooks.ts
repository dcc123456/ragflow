import { Modal } from '@/components/ui/modal/modal';
import { useFetchTenantData } from '@/hooks/use-user-setting-request';
import billingService, { billinCheckout } from '@/services/price';
import storagePrivate from '@/utils/authorization-private-util';
import storage from '@/utils/authorization-util';
import { useMutation, useQuery } from '@tanstack/react-query';
import React from 'react';
import { ICurrentPlan, IPlan, IPricePlanWithButton } from '../interface';
import { showModal } from '../price-modal/show-modal';
export type IChargePlan = {
  subscription_price_id?: string;
  quantity: string;
  usage_based_price_id?: string;
};
export const PriceChargeKey = 'price-charge';
const useCharge = () => {
  const { data: tenantInfo } = useFetchTenantData();
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
    mutationFn: async ({
      price_id,
      quantity,
      payment_type,
    }: {
      price_id: string;
      quantity: string;
      payment_type: 'subscription' | 'usage_based';
    }) => {
      const { data } = await billinCheckout({
        tenantId: tenantId,
        subscription_price_id:
          payment_type === 'subscription' ? price_id : undefined,
        payment_type: payment_type,
        // quantity: chargePlan.quantity,
        quantity: quantity,
        // usage_based_price_id: chargePlan.usage_based_price_id,
        usage_based_price_id:
          payment_type === 'usage_based' ? price_id : undefined,
        // 'price_1RRTpfPtsKvwvC5fVsZly0mE',
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
      // window.open('http://www.baidu.com');
    } else {
      const chargeResult = await mutateAsync({
        price_id: data.id,
        quantity: '1',
        payment_type: 'subscription',
      });
      if (chargeResult && chargeResult.redirect_to) {
        window.open(chargeResult.redirect_to);
      } else if (chargeResult && chargeResult.scheduled_change) {
        const effectiveAt = chargeResult?.scheduled_change?.effective_at;
        const modal = showModal({
          children: React.createElement(
            Modal,
            {
              open: true,
              title: 'Downgrade scheduled',
              onOpenChange: (open: boolean) => {
                if (!open) {
                  modal.destroy();
                }
              },
              className: '!w-[400px]',
              footer: React.createElement(
                'div',
                { className: 'flex justify-end gap-2' },
                React.createElement(
                  'button',
                  {
                    type: 'button',
                    onClick: () => modal.destroy(),
                    className:
                      'px-2 py-1 bg-primary text-primary-foreground rounded-md hover:bg-primary/90',
                  },
                  'OK',
                ),
              ),
            },
            React.createElement(
              'div',
              { className: 'h-32' },
              `Your plan will downgrade at the end of the current billing period${effectiveAt ? ` (${effectiveAt})` : ''}.`,
            ),
          ),
        });
      }
    }
  };
  return { data, loading, charge, checkout: mutateAsync };
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
