// src/interfaces/subscription.ts

import { PaygStatusEnum, SubscriptionStatus } from './contant';
export interface IApiRequestLimits {
  requests_per_day?: number;
  requests_per_minute?: number;
  requests_per_month?: number;
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
  addon_storage: IResourceDetail;
  plan_points?: IResourceDetail;
  addon_points?: IResourceDetail;
}

// export type SubscriptionStatus = 'active' | 'inactive' | 'cancelled' | 'expired';

export interface ISubscriptionData {
  api_request_limits: IApiRequestLimits;
  plan_name: string;
  resources: IResources;
  subscription_status: SubscriptionStatus;
  payment_required?: boolean;
  payment_recoverable?: boolean;
  payment_recovery_url?: string;
  billing_cycle: {
    start: string;
    end: string;
  };
}

export interface ITotalSpendLineChart {
  data: Array<{ name: string; spend: number }>;
  title: string;
  value: number;
}

export interface ICategoriesChart {
  title: string;
  desc: string;
  series: Array<{ name: string; spend: number; quantity: number }>;
  // pages: number;
  // value: number;
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
  product_id?: string;
  product?: string;
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

export interface SpendSeries {
  date: string;
  spend: number;
}

export interface QuantitySeries {
  date: string;
  quantity: number;
}

export interface Category {
  product_name: string;
  unit: string;
  total_spend: number;
  total_quantity: number;
  series: SpendSeries[];
  quantity_series: QuantitySeries[];
}

export interface UsageData {
  tenant_id: string;
  currency: string;
  total_spend: number;
  series: SpendSeries[];
  categories: Category[];
}

export interface IPaygDeepDocUsage {
  pages_paid: number;
  pages_unpaid: number;
  amount_paid: number;
  amount_unpaid: number;
  threshold: number;
  currency: string;
}

export interface IPayStatusData {
  status: PaygStatusEnum;
  subscription_id: string;
  current_period_start: string;
  current_period_end: string;
  deepdoc: IPaygDeepDocUsage;
}

export interface IPaygDisableResponse {
  blocked: boolean;
  outstanding_amount: number;
  currency: string;
  message: string;
}

export interface IPointBalance {
  plan_points: {
    used: number;
    limit: number;
    remaining?: number;
    unit: string;
  };
  addon_points: {
    used: number;
    limit: number;
    remaining?: number;
    unit: string;
  };
}

export interface IPointLedgerItem {
  id: string;
  tenant_id: string;
  event_type: string;
  points: number;
  idempotency_key: string;
  related_hold_id: string | null;
  description: string | null;
  create_time: number;
}

export interface IPointHoldItem {
  id: string;
  tenant_id: string;
  doc_id: string;
  points: number;
  status: string;
  idempotency_key: string;
  create_time: number;
  expired_at: string | null;
}
