import {
  ITenantRoleDefaultModels,
  ITenantRolePermissionMatrix,
  ITenantRolePermissions,
  ITenantRoleResource,
  TenantRoleDefaultModelType,
} from '@/ee/interfaces/database/team';
import {
  createTenantRole,
  deleteTenantRole,
  getTenantRoleDefaultModels,
  getTenantRolePermissions,
  listTenantRoleResources,
  setTenantDefaultRole,
  setTenantRoleDefaultModel,
  setTenantRolePermissions,
  updateTenantRole,
} from '@/ee/services/tenant-role-service';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback } from 'react';
import { TenantMemberKeys, TenantRoleKeys } from './use-member-role-query';

type RolePayload = {
  role_name: string;
  description: string;
};

const useRoleMutation = <TVariables>(
  mutationKey: readonly unknown[],
  mutationFn: (variables: TVariables) => Promise<number | undefined>,
  onSuccess?: () => void,
) => {
  const { isPending, mutateAsync } = useMutation({ mutationKey, mutationFn });

  const run = useCallback(
    async (variables: TVariables) => {
      const code = await mutateAsync(variables);

      if (code === 0) {
        onSuccess?.();
      }

      return code;
    },
    [mutateAsync, onSuccess],
  );

  return { loading: isPending, run };
};

const useInvalidateTenantRoles = (tenantId: string) => {
  const queryClient = useQueryClient();

  return useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: TenantRoleKeys.list(tenantId),
    });
  }, [queryClient, tenantId]);
};

export const useTenantRoleResources = (tenantId: string, enabled: boolean) => {
  const { data, isFetching, isFetched, isError } = useQuery<
    ITenantRoleResource[]
  >({
    queryKey: TenantRoleKeys.resources(tenantId),
    initialData: [],
    enabled: enabled && !!tenantId,
    queryFn: async () => {
      const { data } = await listTenantRoleResources();

      return Array.isArray(data?.data?.resources) ? data.data.resources : [];
    },
  });

  return { data, loading: isFetching && !isFetched, isError };
};

export const useTenantRolePermissions = (
  tenantId: string,
  roleId: number,
  enabled: boolean,
) => {
  const { data, isFetching, isFetched, isError } = useQuery<
    ITenantRolePermissions | undefined
  >({
    queryKey: TenantRoleKeys.permissions(tenantId, roleId),
    enabled: enabled && !!tenantId && roleId > 0,
    queryFn: async () => {
      const { data } = await getTenantRolePermissions(roleId);

      return data?.data;
    },
  });

  return { data, loading: isFetching && !isFetched, isError };
};

export const useTenantRoleDefaultModels = (
  tenantId: string,
  roleId: number,
  enabled: boolean,
) => {
  const { data, isFetching, isFetched, isError } = useQuery<
    ITenantRoleDefaultModels | undefined
  >({
    queryKey: TenantRoleKeys.defaultModels(tenantId, roleId),
    enabled: enabled && !!tenantId && roleId > 0,
    queryFn: async () => {
      const { data } = await getTenantRoleDefaultModels(roleId);

      return data?.data;
    },
  });

  return { data, loading: isFetching && !isFetched, isError };
};

export const useSetTenantRoleDefaultModel = (
  tenantId: string,
  roleId: number,
) => {
  const queryClient = useQueryClient();
  const { loading, run } = useRoleMutation(
    [...TenantRoleKeys.defaultModels(tenantId, roleId), 'update'],
    async (payload: {
      model_type: TenantRoleDefaultModelType;
      model_id: string;
    }) => {
      const { data } = await setTenantRoleDefaultModel(roleId, payload);

      return data?.code;
    },
    () => {
      void queryClient.invalidateQueries({
        queryKey: TenantRoleKeys.defaultModels(tenantId, roleId),
      });
    },
  );

  return { loading, setDefaultModel: run };
};

export const useCreateTenantRole = (tenantId: string) => {
  const invalidate = useInvalidateTenantRoles(tenantId);
  const { loading, run } = useRoleMutation(
    [...TenantRoleKeys.all, 'create'],
    async (payload: RolePayload) => {
      const { data } = await createTenantRole(payload);

      return data?.code;
    },
    invalidate,
  );

  return { loading, createRole: run };
};

export const useUpdateTenantRole = (tenantId: string, roleId: number) => {
  const invalidate = useInvalidateTenantRoles(tenantId);
  const { loading, run } = useRoleMutation(
    [...TenantRoleKeys.all, 'update', roleId],
    async (payload: RolePayload) => {
      const { data } = await updateTenantRole(roleId, payload);

      return data?.code;
    },
    invalidate,
  );

  return { loading, updateRole: run };
};

export const useDeleteTenantRole = (tenantId: string) => {
  const invalidate = useInvalidateTenantRoles(tenantId);
  const queryClient = useQueryClient();
  const invalidateAfterDelete = useCallback(() => {
    invalidate();
    void queryClient.invalidateQueries({
      queryKey: TenantMemberKeys.list(tenantId),
    });
  }, [invalidate, queryClient, tenantId]);
  const { loading, run } = useRoleMutation(
    [...TenantRoleKeys.all, 'delete'],
    async (roleId: number) => {
      const { data } = await deleteTenantRole(roleId);

      return data?.code;
    },
    invalidateAfterDelete,
  );

  return { loading, deleteRole: run };
};

export const useSetTenantDefaultRole = (tenantId: string) => {
  const invalidate = useInvalidateTenantRoles(tenantId);
  const { loading, run } = useRoleMutation(
    [...TenantRoleKeys.all, 'setDefault'],
    async (roleId: number) => {
      const { data } = await setTenantDefaultRole(roleId);

      return data?.code;
    },
    invalidate,
  );

  return { loading, setDefaultRole: run };
};

export const useSetTenantRolePermissions = (
  tenantId: string,
  roleId: number,
) => {
  const queryClient = useQueryClient();
  const invalidate = useInvalidateTenantRoles(tenantId);
  const { loading, run } = useRoleMutation(
    [...TenantRoleKeys.permissions(tenantId, roleId), 'update'],
    async (permissions: ITenantRolePermissionMatrix) => {
      const { data } = await setTenantRolePermissions(roleId, permissions);

      if (data?.code === 0 && data.data) {
        queryClient.setQueryData(
          TenantRoleKeys.permissions(tenantId, roleId),
          data.data,
        );
      }

      return data?.code;
    },
    invalidate,
  );

  return { loading, setPermissions: run };
};
