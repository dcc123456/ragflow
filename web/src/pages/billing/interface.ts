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

export interface ITotalSpendLineChart {
  data: Array<{ name: string; spend: number }>;
  title: string;
  value: number;
}

export interface IDeepDocSpendLineChart {
  data: Array<{ name: string; spend: number; pages: number }>;
  pages: number;
  value: number;
}
export interface IEmbeddingSpendLineChart {
  data: Array<{ name: string; spend: number; tokens: number }>;
  tokens: number;
  value: number;
}

export interface Invoice {
  amount: number;
  created_at: number;
  currency?: string;
  hosted_invoice_url?: string;
  invoice_id: string;
  invoice_pdf_url: string;
  status: 'paid' | 'unpaid' | 'pending';
}

export interface ITableInvoice {
  id: string;
  createDate: string;
  product: string;
  status: string;
  amount: string;
  invoiceLink?: string;
}
