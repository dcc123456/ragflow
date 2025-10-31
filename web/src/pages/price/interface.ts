import { PriceName } from './contant';

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
