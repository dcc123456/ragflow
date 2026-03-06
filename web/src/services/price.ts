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
  storageCurrent,
  storageSetTarget,
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
  storageCurrent: {
    url: storageCurrent,
    method: 'get',
  },
  storageSetTarget: {
    url: storageSetTarget,
    method: 'post',
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

export interface IStorageSubscriptionCurrent {
  tenant_id: string;
  plan_name: string;
  trial_forbidden: boolean;
  unit_price: number;
  effective_quantity_gb: number;
  target_quantity_gb: number;
  pending_quantity_gb: number | null;
  pending_action: string;
  pending_effective_at: string | null;
  decrease_effective_at: string | null;
  subscription_id: string;
  price_id: string;
  schedule_id: string;
  status: string;
  cancel_at_period_end: boolean;
  current_period_start: string | null;
  current_period_end: string | null;
}

export const getBillingStorageCurrent = (tenantId?: string) => {
  return request.get(api.storageCurrent, {
    params: tenantId ? { tenant_id: tenantId } : undefined,
  });
};

export const postBillingStorageSetTarget = (data: {
  tenant_id?: string;
  target_quantity_gb: number;
  session_cancel_url?: string;
  session_success_url?: string;
}) => {
  return request.post(api.storageSetTarget, { data });
};

export const postBillingStorageAbandonPending = (data: {
  tenant_id?: string;
}) => {
  return request.post(api.storageAbandonPending, { data });
};

export const getBillingDeepDocUsage = (tenantId?: string) =>
  request.get(api.deepdocUsage, {
    params: tenantId ? { tenant_id: tenantId } : undefined,
  });

export const postBillingPointsCheckout = (data: {
  tenant_id?: string;
  points: number;
}) => request.post(api.pointsCheckout, { data });

export const getBillingPointsBalance = (tenantId?: string) =>
  request.get(api.pointsBalance, {
    params: tenantId ? { tenant_id: tenantId } : undefined,
  });

export const getBillingPointsLedger = (params: {
  tenant_id?: string;
  page?: number;
  page_size?: number;
  event_type?: string;
}) => request.get(api.pointsLedger, { params });

export const getBillingPointsHolds = (params: {
  tenant_id?: string;
  page?: number;
  page_size?: number;
  status?: string;
}) => request.get(api.pointsHolds, { params });

export default billingService;
