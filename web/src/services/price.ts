import { IChargePlan } from '@/pages/price/hook/use-price-hooks';
import api from '@/utils/private-api';
import registerServer from '@/utils/register-server';
import request from '@/utils/request';

// function registerServer() {}

const {
  billin_checkout,
  current_plan,
  cancel_scheduled_subscription_change,
  plan_list,
  plan_spend_overview,
  getUpComming,
  spendHistory,
  usageBasedPlans,
} = api;
const methods = {
  billinCheckout: {
    url: billin_checkout,
    method: 'post',
  },
  getCurrentPlan: {
    url: current_plan,
    method: 'get',
  },
  cancelScheduledSubscriptionChange: {
    url: cancel_scheduled_subscription_change,
    method: 'post',
  },
  getPlanList: {
    url: plan_list,
    method: 'get',
  },
  planSpendOverview: {
    url: plan_spend_overview,
    headers: { 'Content-Type': 'application/json' },
    method: 'get',
  },
  getUpComming: {
    url: getUpComming,
    method: 'post',
  },
  spendHistory: {
    url: spendHistory,
    method: 'get',
  },
  usageBasedPlans: {
    url: usageBasedPlans,
    method: 'get',
  },
};

const billingService = (() => {
  if (import.meta.env.VITE_BILLING_ENABLED === '1') {
    return registerServer<keyof typeof methods>?.(methods, request);
  }
  return null;
})();

export const billinCheckout = (
  data: IChargePlan & {
    payment_type: string;
    tenantId: string;
    session_cancel_url: string;
    session_success_url: string;
  },
) => {
  return request.post(api.billin_checkout, { data });
};
export const getCurrentPlan = () => {
  return request.get(api.current_plan);
};

export const cancelScheduledSubscriptionChange = (tenantId: string) => {
  return request.post(api.cancel_scheduled_subscription_change, {
    data: { tenant_id: tenantId },
  });
};

export const getBllingBaseOverview = ({ tenantId }: { tenantId: string }) => {
  return request.get(api.blling_base_overview, {
    params: { tenant_id: tenantId },
  });
};
export const getBllingPlanPverview = ({ tenantId }: { tenantId: string }) => {
  return request.get(api.plan_overview, {
    params: { tenant_id: tenantId },
  });
};

export default billingService;
