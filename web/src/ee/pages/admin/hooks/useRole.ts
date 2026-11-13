import {
  assignRolePermissions,
  createRole,
  deleteRole,
  listResources,
  listRolesWithPermission,
  revokeRolePermissions,
  updateRoleDescription,
} from '@/ee/services/admin-service';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { CreateRoleFormData } from '@/ee/pages/admin/forms/role-form';
import type {
  PermissionType,
  RoleResourceName,
} from '@/ee/pages/admin/roles/role-permissions';

export function useRoleList() {
  const {
    data: roleList,
    isFetching,
    refetch,
  } = useQuery({
    queryKey: ['admin/listRolesWithPermission'],
    queryFn: async () => (await listRolesWithPermission())?.data?.data,
    retry: false,
    initialData: [],
  });

  return {
    roleList,
    isFetching,
    refetch,
  };
}

export function useRoleResourceTypes() {
  const {
    data: resourceTypes,
    isFetching,
    refetch,
  } = useQuery({
    queryKey: ['admin/resourceTypes'],
    queryFn: async () =>
      (await listResources()).data.data.resource_types.map(
        (x) => x.toLowerCase() as RoleResourceName,
      ),
    retry: false,
    initialData: [],
  });

  return {
    resourceTypes,
    isFetching,
    refetch,
  };
}

export function useCreateRole() {
  const queryClient = useQueryClient();

  const { mutateAsync, isPending } = useMutation({
    mutationFn: async (data: CreateRoleFormData) => {
      const { data: { data: createdRoleDetail } = {} } = await createRole({
        roleName: data.name,
        description: data.description,
      });

      if (!createdRoleDetail) {
        throw new Error();
      }

      await assignRolePermissions(data.name, data.permissions);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['admin/listRolesWithPermission'],
      });
    },
    retry: false,
  });

  return {
    createRole: mutateAsync,
    isPending,
  };
}

export function useMutateRole(roleName: string) {
  const queryClient = useQueryClient();
  const updateRoleDescriptionMutation = useMutation({
    mutationFn: (description?: string) =>
      updateRoleDescription(roleName, description?.trim() ?? ''),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['admin/listRolesWithPermission'],
      });
    },
    retry: false,
  });

  const updateRolePermissionMutation = useMutation({
    mutationFn: async (data: {
      resourceName: string;
      permissions: Partial<Record<PermissionType, boolean>>;
    }) => {
      const toGrant: Partial<AdminService.PermissionData> = {};
      const toRevoke: Partial<AdminService.PermissionData> = {};
      for (const [action, enabled] of Object.entries(data.permissions) as [
        PermissionType,
        boolean,
      ][]) {
        if (enabled) {
          toGrant[action] = true;
        } else {
          toRevoke[action] = false;
        }
      }

      const revokeData = {
        [data.resourceName]: toRevoke,
      } as Partial<AdminService.RevokeRolePermissionInput>;
      const assignData = {
        [data.resourceName]: toGrant,
      } as Partial<AdminService.AssignRolePermissionsInput>;

      if (Object.keys(toRevoke).length > 0) {
        await revokeRolePermissions(roleName, revokeData);
      }
      if (Object.keys(toGrant).length > 0) {
        await assignRolePermissions(roleName, assignData);
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['admin/listRolesWithPermission'],
      });
    },
    retry: false,
  });

  const deleteRoleMutation = useMutation({
    mutationFn: () => deleteRole(roleName),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['admin/listRolesWithPermission'],
      });
    },
    retry: false,
  });

  return {
    updateDescription: updateRoleDescriptionMutation.mutateAsync,
    isUpdatingDescription: updateRoleDescriptionMutation.isPending,

    updatePermission: updateRolePermissionMutation.mutateAsync,
    isUpdatingPermission: updateRolePermissionMutation.isPending,

    delete: deleteRoleMutation.mutateAsync,
    isDeleting: deleteRoleMutation.isPending,
  };
}
