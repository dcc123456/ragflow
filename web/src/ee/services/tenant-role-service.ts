import {
  ITenantMemberRole,
  ITenantPermissions,
  ITenantRole,
} from '@/ee/interfaces/database/team';
import api from '@/ee/utils/private-api';
import request from '@/utils/next-request';

type Result<T> = {
  code: number;
  message?: string;
  data: T;
};

const TENANT_ROLE_PAGE_SIZE = 200;

export const listTenantRoles = () =>
  request.get<Result<{ roles: ITenantRole[] }>>(
    `${api.tenantRoles}?page=1&page_size=${TENANT_ROLE_PAGE_SIZE}`,
  );

export const getTenantMyPermissions = () =>
  request.get<Result<ITenantPermissions>>(api.tenantMyPermissions);

export const getTenantMemberRole = (userId: string) =>
  request.get<Result<ITenantMemberRole>>(api.tenantMemberRole(userId));

export const setTenantMemberRole = (userId: string, roleId: number) =>
  request.put<Result<ITenantMemberRole>>(api.tenantMemberRole(userId), {
    role_id: roleId,
  });
