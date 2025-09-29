import { IChargePlan } from '@/pages/price/hook/use-price-hooks';
import api from '@/utils/private-api';
import registerServer from '@/utils/register-server';
import request from '@/utils/request';

const { billin_checkout, current_plan } = api;
const methods = {
  // 支付
  billinCheckout: {
    url: billin_checkout,
    method: 'post',
  },
  getCurrentPlan: {
    url: current_plan,
    method: 'get',
  },
};
const billingService = registerServer<keyof typeof methods>(methods, request);

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

export default billingService;
