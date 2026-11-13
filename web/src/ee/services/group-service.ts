import {
  ITenantGroup,
  ITenantGroupMember,
} from '@/ee/interfaces/database/team';
import api from '@/ee/utils/private-api';
import request from '@/utils/next-request';

type Result<T> = {
  code: number;
  message?: string;
  data: T;
};

export type CreateTenantGroupPayload = {
  name: string;
  description?: string;
  avatar?: string;
  parent_id?: string;
};

export type UpdateTenantGroupPayload = {
  name: string;
  description?: string;
  avatar?: string;
};

export const listTenantGroups = () =>
  request.get<Result<{ groups: ITenantGroup[] }>>(api.tenantGroups);

export const listTenantGroupMembers = (groupId: string) =>
  request.get<Result<{ members: ITenantGroupMember[] }>>(
    api.tenantGroupMembers(groupId),
  );

export const createTenantGroup = (payload: CreateTenantGroupPayload) =>
  request.post<Result<ITenantGroup>>(api.tenantGroups, payload);

export const updateTenantGroup = (
  groupId: string,
  payload: UpdateTenantGroupPayload,
) => request.put<Result<ITenantGroup>>(api.tenantGroup(groupId), payload);

export const moveTenantGroup = (groupId: string, parentId: string | null) =>
  request.put<Result<ITenantGroup>>(api.tenantGroupParent(groupId), {
    parent_id: parentId,
  });

export const deleteTenantGroup = (groupId: string) =>
  request.delete<Result<{ success: boolean }>>(api.tenantGroup(groupId));

export const addTenantGroupMember = (groupId: string, userId: string) =>
  request.post<Result<ITenantGroupMember>>(api.tenantGroupMembers(groupId), {
    user_id: userId,
  });

export const removeTenantGroupMember = (groupId: string, userId: string) =>
  request.delete<Result<{ success: boolean }>>(
    api.tenantGroupMember(groupId, userId),
  );

export const setTenantGroupMemberPermissions = (
  groupId: string,
  userId: string,
  permissions: string[],
) =>
  request.put<Result<ITenantGroupMember>>(
    api.tenantGroupMemberPermissions(groupId, userId),
    { permissions },
  );

export const transferTenantGroupOwner = (groupId: string, userId: string) =>
  request.put<Result<ITenantGroup>>(api.tenantGroupOwner(groupId), {
    user_id: userId,
  });
