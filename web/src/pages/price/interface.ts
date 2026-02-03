import { PriceName } from './constant';

export interface IFeatureProps {
  apps: string;
  teamMembers: string;
  datasetStorage: string;
  apiRequests: string;
}
export interface IPricePlan {
  id: string;
  title: string;
  description: string;
  price: string;
  feature: IFeatureProps;
}

export type IPricePlanWithButton = IPricePlan & {
  isPopular?: boolean;
  buttonLabel: string;
  isUse: boolean;
  icon: () => JSX.Element;
};

export type IConfirmPlan = IPricePlan & {
  priceDifference: string;
};

export interface IPlanFeature {
  quota_api_limits: number;
  quota_apps: number;
  quota_kb_storage: number;
  quota_members: number;
}

export interface IPlan {
  description: null | string;
  feature: IPlanFeature;
  id: string;
  name: PriceName;
  price: number;
  price_ids: string;
}

export interface ICurrentPlan {
  customer_id: string;
  end_time: Date | string;
  id: string;
  invoice_pdf_url: string | null;
  invoice_url: string | null;
  original_subscription_id: string;
  pending_subscription_change?: {
    schedule_id: string;
    pending_price_id: string;
    pending_plan_name: string;
    effective_at: string | null;
  };
  plan_name: string;
  price_id: string;
  product_id: string;
  quota_apps: number;
  quota_kb_storage: number;
  quota_members: number;
  start_time: Date | string;
  subscription_id: string;
  subscription_status: string;
  task_priority: string;
  tenant_id: string;
  version: number;
}
