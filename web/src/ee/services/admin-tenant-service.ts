import axios from 'axios';

import apiPrivate from '@/ee/utils/api-private';
import { Authorization } from '@/constants/authorization';
import { adminStorage } from '@/utils/authorization-util';

export type AdminTenantListItem = {
  tenant_id: string;
  name?: string | null;
  owner_email?: string | null;
  owner_nickname?: string | null;
  member_count: number;
  role_id: number | null;
  role_name: string | null;
  status?: string | null;
};

export type AdminTenantListResult = {
  tenants: AdminTenantListItem[];
  total: number;
  page: number;
  page_size: number;
};

export type AdminTenantListParams = {
  keyword?: string;
  page: number;
  page_size: number;
};

type AdminResponse<T> = {
  code: number;
  message: string;
  data: T;
};

const adminRequest = axios.create({ timeout: 300000 });

adminRequest.interceptors.request.use((config) => {
  config.headers.set(Authorization, adminStorage.getAuthorization());
  return config;
});

export const listAdminTenants = (params: AdminTenantListParams) =>
  adminRequest.get<AdminResponse<AdminTenantListResult>>(
    apiPrivate.adminListTenants,
    { params },
  );

export const updateAdminTenantRole = (tenantId: string, roleId: number) =>
  adminRequest.put<
    AdminResponse<{
      tenant_id: string;
      role_id: number | null;
      role_name: string | null;
    }>
  >(apiPrivate.adminUpdateTenantRole(tenantId), { role_id: roleId });

export const updateAdminTenantStatus = (tenantId: string, status: string) =>
  adminRequest.put<AdminResponse<{ tenant_id: string; status: string }>>(
    apiPrivate.adminUpdateTenantStatus(tenantId),
    { status },
  );
