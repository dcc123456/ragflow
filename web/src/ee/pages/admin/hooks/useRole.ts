import {
  createRole,
  listRolesWithPermission,
} from '@/ee/services/admin-service';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

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

export function useCreateRole() {
  const queryClient = useQueryClient();

  const { mutateAsync, isPending } = useMutation({
    mutationFn: async (data: { roleName: string; description: string }) => {
      const { data: response } = await createRole(data);
      if (response.code !== 0) throw new Error(response.message);
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
