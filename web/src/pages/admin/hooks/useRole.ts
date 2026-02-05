import {
  assignRolePermissions,
  createRole,
  deleteRole,
  listResources,
  listRolesWithPermission,
  revokeRolePermissions,
  updateRoleDescription,
} from '@/services/admin-service';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CreateRoleFormData } from '../forms/role-form';

export function useRoleList() {
  const {
    data: roleList,
    isFetching,
    refetch,
  } = useQuery({
    queryKey: ['admin/listRolesWithPermission'],
    queryFn: async () => (await listRolesWithPermission())?.data?.data?.roles,
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
        (x) => x.toLowerCase() as AdminService.RoleResourceName,
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
    mutationFn: (data: {
      resourceName: string;
      permissionType: AdminService.PermissionType;
      value: boolean;
    }) => {
      const permissionDiffData = {
        [data.resourceName]: {
          [data.permissionType]: data.value,
        },
      };

      return data.value
        ? assignRolePermissions(roleName, permissionDiffData)
        : revokeRolePermissions(roleName, permissionDiffData);
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
