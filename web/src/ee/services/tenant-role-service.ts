import {
  ITenantMemberRole,
  ITenantRolePermissionMatrix,
  ITenantRolePermissions,
  ITenantRoleResource,
  ITenantManagementGrant,
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

export const listTenantRoles = (page: number) =>
  request.get<
    Result<{
      roles: ITenantRole[];
      total: number;
    }>
  >(`${api.tenantRoles}?page=${page}&page_size=${TENANT_ROLE_PAGE_SIZE}`);

export const createTenantRole = (payload: {
  role_name: string;
  description: string;
}) => request.post<Result<ITenantRole>>(api.tenantRoles, payload);

export const updateTenantRole = (
  roleId: number,
  payload: { role_name: string; description: string },
) => request.patch<Result<ITenantRole>>(api.tenantRole(roleId), payload);

export const deleteTenantRole = (roleId: number) =>
  request.delete<Result<{ success: boolean }>>(api.tenantRole(roleId));

export const listTenantRoleResources = () =>
  request.get<Result<{ resources: ITenantRoleResource[] }>>(
    api.tenantRoleResources,
  );

export const getTenantRolePermissions = (roleId: number) =>
  request.get<Result<ITenantRolePermissions>>(
    api.tenantRolePermissions(roleId),
  );

export const setTenantRolePermissions = (
  roleId: number,
  permissions: ITenantRolePermissionMatrix,
) =>
  request.put<Result<ITenantRolePermissions>>(
    api.tenantRolePermissions(roleId),
    { permissions },
  );

export const setTenantDefaultRole = (roleId: number) =>
  request.put(api.tenantDefaultRole, { default_role_id: roleId });

export const listTenantManagementPermissions = () =>
  request.get<Result<{ grants: ITenantManagementGrant[] }>>(
    api.tenantManagementPermissions,
  );

export const setTenantManagementPermissions = (
  userId: string,
  permissions: ITenantManagementGrant['permissions'],
) =>
  request.put<Result<ITenantManagementGrant>>(api.tenantManagementPermissions, {
    user_id: userId,
    permissions,
  });

export const getTenantMyPermissions = () =>
  request.get<Result<ITenantPermissions>>(api.tenantMyPermissions);

export const getTenantMemberRole = (userId: string) =>
  request.get<Result<ITenantMemberRole>>(api.tenantMemberRole(userId));

export const setTenantMemberRole = (userId: string, roleId: number) =>
  request.put<Result<ITenantMemberRole>>(api.tenantMemberRole(userId), {
    role_id: roleId,
  });

export const approveTenantMember = (userId: string) =>
  request.post<Result<boolean>>(api.approveTenantMember(userId));
