import {
  ITenantGroup,
  ITenantGroupMember,
} from '@/ee/interfaces/database/team';
import {
  CreateTenantGroupPayload,
  UpdateTenantGroupPayload,
  createTenantGroup,
  deleteTenantGroup,
  addTenantGroupMember,
  listTenantGroupMembers,
  listTenantGroups,
  moveTenantGroup,
  removeTenantGroupMember,
  setTenantGroupMemberPermissions,
  transferTenantGroupOwner,
  updateTenantGroup,
} from '@/ee/services/group-service';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback } from 'react';

export const GroupKeys = {
  all: ['tenantGroup'] as const,
  list: () => [...GroupKeys.all, 'list'] as const,
  members: (groupId: string) => [...GroupKeys.all, 'members', groupId] as const,
  memberLists: () => [...GroupKeys.all, 'members'] as const,
  action: (action: string, groupId: string) =>
    [...GroupKeys.all, 'action', action, groupId] as const,
};

const useInvalidateGroupQueries = (
  groupId?: string,
  allMemberLists = false,
) => {
  const queryClient = useQueryClient();

  return useCallback(() => {
    queryClient.invalidateQueries({ queryKey: GroupKeys.list() });

    if (allMemberLists) {
      queryClient.invalidateQueries({ queryKey: GroupKeys.memberLists() });
    } else if (groupId) {
      queryClient.invalidateQueries({ queryKey: GroupKeys.members(groupId) });
    }
  }, [allMemberLists, groupId, queryClient]);
};

const useGroupMutation = <TVariables>(
  mutationKey: readonly unknown[],
  mutationFn: (variables: TVariables) => Promise<number | undefined>,
  invalidate: () => void,
) => {
  const { isPending, mutateAsync } = useMutation({
    mutationKey,
    mutationFn,
  });

  const run = useCallback(
    async (variables: TVariables) => {
      const code = await mutateAsync(variables);

      if (code === 0) {
        invalidate();
      }

      return code;
    },
    [invalidate, mutateAsync],
  );

  return { loading: isPending, run };
};

/**
 * `loading` only reflects the first fetch. Background refetches (a new observer
 * mounting, or a post-mutation invalidation) must not blank the list: the table
 * swaps in a skeleton while "loading", which unmounts every row and any dialog
 * rendered inside one.
 */
const firstFetchLoading = (isFetching: boolean, isFetched: boolean) =>
  isFetching && !isFetched;

export const useTenantGroups = () => {
  const { data, isFetching, isFetched, refetch } = useQuery<ITenantGroup[]>({
    queryKey: GroupKeys.list(),
    initialData: [],
    queryFn: async () => {
      const { data } = await listTenantGroups();
      const groups = data?.data?.groups;

      return Array.isArray(groups) ? groups : [];
    },
  });

  return { data, loading: firstFetchLoading(isFetching, isFetched), refetch };
};

export const useTenantGroupMembers = (groupId: string, enabled: boolean) => {
  const { data, isFetching, isFetched } = useQuery<ITenantGroupMember[]>({
    queryKey: GroupKeys.members(groupId),
    initialData: [],
    enabled: enabled && !!groupId,
    queryFn: async () => {
      const { data } = await listTenantGroupMembers(groupId);
      const members = data?.data?.members;

      return Array.isArray(members) ? members : [];
    },
  });

  return { data, loading: firstFetchLoading(isFetching, isFetched) };
};

export const useCreateTenantGroup = () => {
  const invalidate = useInvalidateGroupQueries();
  const { loading, run } = useGroupMutation(
    GroupKeys.action('create', ''),
    async (payload: CreateTenantGroupPayload) => {
      const { data } = await createTenantGroup(payload);

      return data?.code;
    },
    invalidate,
  );

  return { loading, createGroup: run };
};

export const useUpdateTenantGroup = (groupId: string) => {
  const invalidate = useInvalidateGroupQueries(groupId);
  const { loading, run } = useGroupMutation(
    GroupKeys.action('update', groupId),
    async (payload: UpdateTenantGroupPayload) => {
      const { data } = await updateTenantGroup(groupId, payload);

      return data?.code;
    },
    invalidate,
  );

  return { loading, updateGroup: run };
};

export const useMoveTenantGroup = (groupId: string) => {
  const invalidate = useInvalidateGroupQueries(groupId);
  const { loading, run } = useGroupMutation(
    GroupKeys.action('move', groupId),
    async (parentId: string | null) => {
      const { data } = await moveTenantGroup(groupId, parentId);

      return data?.code;
    },
    invalidate,
  );

  return { loading, moveGroup: run };
};

export const useDeleteTenantGroup = (groupId: string) => {
  const invalidate = useInvalidateGroupQueries(groupId);
  const { loading, run } = useGroupMutation<void>(
    GroupKeys.action('delete', groupId),
    async () => {
      const { data } = await deleteTenantGroup(groupId);

      return data?.code;
    },
    invalidate,
  );

  return { loading, deleteGroup: run };
};

export const useAddTenantGroupMember = (groupId: string) => {
  const invalidate = useInvalidateGroupQueries(groupId);
  const { loading, run } = useGroupMutation(
    GroupKeys.action('addMember', groupId),
    async (userId: string) => {
      const { data } = await addTenantGroupMember(groupId, userId);

      return data?.code;
    },
    invalidate,
  );

  return { loading, addMember: run };
};

export const useRemoveTenantGroupMember = (groupId: string) => {
  const invalidate = useInvalidateGroupQueries(groupId, true);
  const { loading, run } = useGroupMutation(
    GroupKeys.action('removeMember', groupId),
    async (userId: string) => {
      const { data } = await removeTenantGroupMember(groupId, userId);

      return data?.code;
    },
    invalidate,
  );

  return { loading, removeMember: run };
};

export const useSetTenantGroupMemberPermissions = (groupId: string) => {
  const invalidate = useInvalidateGroupQueries(groupId);
  const { loading, run } = useGroupMutation(
    GroupKeys.action('memberPermissions', groupId),
    async ({
      userId,
      permissions,
    }: {
      userId: string;
      permissions: string[];
    }) => {
      const { data } = await setTenantGroupMemberPermissions(
        groupId,
        userId,
        permissions,
      );

      return data?.code;
    },
    invalidate,
  );

  return { loading, setMemberPermissions: run };
};

export const useTransferTenantGroupOwner = (groupId: string) => {
  const invalidate = useInvalidateGroupQueries(groupId);
  const { loading, run } = useGroupMutation(
    GroupKeys.action('transferOwner', groupId),
    async (userId: string) => {
      const { data } = await transferTenantGroupOwner(groupId, userId);

      return data?.code;
    },
    invalidate,
  );

  return { loading, transferOwner: run };
};
