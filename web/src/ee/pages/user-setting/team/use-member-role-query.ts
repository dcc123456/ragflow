import message from '@/components/ui/message';
import {
  ITenantMemberRole,
  ITenantPermissions,
  ITenantRole,
} from '@/ee/interfaces/database/team';
import {
  approveTenantMember,
  getTenantMemberRole,
  getTenantMyPermissions,
  listTenantRoles,
  setTenantMemberRole,
} from '@/ee/services/tenant-role-service';
import { UserSettingApiAction } from '@/hooks/use-user-setting-request';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';

export const MemberRoleKeys = {
  all: ['tenantMemberRole'] as const,
  permissions: (tenantId: string) =>
    [...MemberRoleKeys.all, 'permissions', tenantId] as const,
  member: (tenantId: string, userId: string) =>
    [...MemberRoleKeys.all, 'member', tenantId, userId] as const,
};

export const TenantMemberKeys = {
  list: (tenantId: string) =>
    [UserSettingApiAction.ListTenantUser, tenantId] as const,
};

export const TenantRoleKeys = {
  all: ['tenantRole'] as const,
  list: (tenantId: string) =>
    [...TenantRoleKeys.all, 'list', tenantId] as const,
  resources: (tenantId: string) =>
    [...TenantRoleKeys.all, 'resources', tenantId] as const,
  permissions: (tenantId: string, roleId: number) =>
    [...TenantRoleKeys.all, 'permissions', tenantId, roleId] as const,
};

const firstFetchLoading = (isFetching: boolean, isFetched: boolean) =>
  isFetching && !isFetched;

export const useTenantRoles = (tenantId: string, enabled = true) => {
  const { data, isFetching, isFetched } = useQuery<ITenantRole[]>({
    queryKey: TenantRoleKeys.list(tenantId),
    initialData: [],
    enabled: enabled && !!tenantId,
    queryFn: async () => {
      const roles: ITenantRole[] = [];
      let page = 1;

      while (true) {
        const { data } = await listTenantRoles(page);
        const result = data?.data;
        const pageRoles = result?.roles;

        if (!Array.isArray(pageRoles)) {
          return roles;
        }

        roles.push(...pageRoles);
        if (roles.length >= result.total || pageRoles.length === 0) {
          return roles;
        }

        page += 1;
      }
    },
  });

  return { data, loading: firstFetchLoading(isFetching, isFetched) };
};

export const useTenantMyPermissions = (tenantId: string) => {
  const { data } = useQuery<ITenantPermissions | undefined>({
    queryKey: MemberRoleKeys.permissions(tenantId),
    enabled: !!tenantId,
    queryFn: async () => {
      const { data } = await getTenantMyPermissions();

      return data?.data;
    },
  });

  return { data };
};

export const useTenantMemberRole = (
  tenantId: string,
  userId: string,
  enabled: boolean,
) => {
  const { data, isFetching, isFetched } = useQuery<
    ITenantMemberRole | undefined
  >({
    queryKey: MemberRoleKeys.member(tenantId, userId),
    enabled: enabled && !!tenantId && !!userId,
    queryFn: async () => {
      const { data } = await getTenantMemberRole(userId);

      return data?.data;
    },
  });

  return { data, loading: firstFetchLoading(isFetching, isFetched) };
};

export const useSetTenantMemberRole = (tenantId: string, userId: string) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { isPending: loading, mutateAsync } = useMutation({
    mutationKey: [...MemberRoleKeys.member(tenantId, userId), 'update'],
    mutationFn: async (roleId: number) => {
      const { data } = await setTenantMemberRole(userId, roleId);

      return data?.code;
    },
  });

  const setMemberRole = useCallback(
    async (roleId: number) => {
      const code = await mutateAsync(roleId);

      if (code === 0) {
        message.success(t('message.updated'));
        queryClient.invalidateQueries({
          queryKey: [UserSettingApiAction.ListTenantUser],
        });
        queryClient.invalidateQueries({
          queryKey: MemberRoleKeys.member(tenantId, userId),
        });
      }

      return code;
    },
    [mutateAsync, queryClient, t, tenantId, userId],
  );

  return { loading, setMemberRole };
};

export const useApproveTenantMember = (tenantId: string, userId: string) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { isPending: loading, mutateAsync } = useMutation({
    mutationKey: [...MemberRoleKeys.member(tenantId, userId), 'approve'],
    mutationFn: async () => {
      const { data } = await approveTenantMember(userId);

      return data?.code;
    },
  });

  const approveMember = useCallback(async () => {
    const code = await mutateAsync();

    if (code === 0) {
      message.success(t('permission.memberApproved'));
      queryClient.invalidateQueries({
        queryKey: [UserSettingApiAction.ListTenantUser],
      });
    }

    return code;
  }, [mutateAsync, queryClient, t]);

  return { loading, approveMember };
};
