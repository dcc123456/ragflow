// src/interfaces/subscription.ts

import { SubscriptionStatus } from './contant';

export interface IApiRequestLimits {
  requests_per_day: number;
  requests_per_minute: number;
}

export interface IResourceDetail {
  limit: number;
  unit: string;
  used: number;
}

export interface IResources {
  apps: IResourceDetail;
  members: IResourceDetail;
  plan_storage: IResourceDetail;
  add_on_storage: IResourceDetail;
}

// export type SubscriptionStatus = 'active' | 'inactive' | 'cancelled' | 'expired';

export interface ISubscriptionData {
  api_request_limits: IApiRequestLimits;
  plan_name: string;
  resources: IResources;
  subscription_status: SubscriptionStatus;
}
