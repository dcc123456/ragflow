import axios from 'axios';

import { Authorization } from '@/constants/authorization';
import apiPrivate from '@/ee/utils/api-private';
import { adminStorage } from '@/utils/authorization-util';

export type DefaultTenantRoleSetting = {
  role_id: number | null;
  role_name: string | null;
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

export const getDefaultTenantSystemRole = () =>
  adminRequest.get<AdminResponse<DefaultTenantRoleSetting>>(
    apiPrivate.adminDefaultTenantRole,
  );

export const setDefaultTenantSystemRole = (roleId: number) =>
  adminRequest.put<AdminResponse<DefaultTenantRoleSetting>>(
    apiPrivate.adminDefaultTenantRole,
    { role_id: roleId },
  );
