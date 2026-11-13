import { ITenantManagementGrant } from '@/ee/interfaces/database/team';
import {
  listTenantManagementPermissions,
  setTenantManagementPermissions,
} from '@/ee/services/tenant-role-service';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

const TenantManagementPermissionKeys = {
  list: (tenantId: string) =>
    ['tenantManagementPermissions', tenantId] as const,
};

export const useTenantManagementPermissions = (
  tenantId: string,
  enabled: boolean,
) => {
  const { data, isFetching, isFetched } = useQuery<ITenantManagementGrant[]>({
    queryKey: TenantManagementPermissionKeys.list(tenantId),
    initialData: [],
    enabled: enabled && !!tenantId,
    queryFn: async () => {
      const { data } = await listTenantManagementPermissions();

      return Array.isArray(data?.data?.grants) ? data.data.grants : [];
    },
  });

  return { data, loading: isFetching && !isFetched };
};

export const useSetTenantManagementPermissions = (tenantId: string) => {
  const queryClient = useQueryClient();
  const { isPending: loading, mutateAsync } = useMutation({
    mutationKey: ['tenantManagementPermissions', tenantId, 'update'],
    mutationFn: async ({
      userId,
      permissions,
    }: {
      userId: string;
      permissions: ITenantManagementGrant['permissions'];
    }) => {
      const { data } = await setTenantManagementPermissions(
        userId,
        permissions,
      );

      return data?.code;
    },
  });

  const updatePermissions = async (
    userId: string,
    permissions: ITenantManagementGrant['permissions'],
  ) => {
    const code = await mutateAsync({ userId, permissions });

    if (code === 0) {
      queryClient.invalidateQueries({
        queryKey: TenantManagementPermissionKeys.list(tenantId),
      });
    }

    return code;
  };

  return { loading, updatePermissions };
};
