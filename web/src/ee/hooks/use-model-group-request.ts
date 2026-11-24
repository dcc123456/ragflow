import type {
  IModelGroup,
  IModelGroupMember,
} from '@/ee/interfaces/database/model-group';
import {
  IAddModelToGroupRequest,
  ICreateModelGroupRequest,
  IReorderModelGroupMembersRequest,
  IRemoveModelFromGroupRequest,
  IUpdateModelGroupMemberStatusRequest,
} from '@/ee/interfaces/request/model-group';
import modelGroupService from '@/ee/services/model-group-service';
import { normalizeModelTypes } from '@/ee/utils/model-group-util';
import { ModelResourceAccessUserIdContext } from '@/hooks/use-llm-request';
import {
  QueryClient,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import { useCallback, useContext } from 'react';

export const ModelGroupKeys = {
  all: () => ['modelGroup'] as const,
  list: (accessUserId?: string | null) =>
    [...ModelGroupKeys.all(), 'list', accessUserId ?? ''] as const,
  detail: (groupId: string) =>
    [...ModelGroupKeys.all(), 'detail', groupId] as const,
  optionsRoot: () => [...ModelGroupKeys.all(), 'options'] as const,
  options: (modelTypes: string[]) =>
    [...ModelGroupKeys.optionsRoot(), normalizeModelTypes(modelTypes)] as const,
  validIdsRoot: () => [...ModelGroupKeys.all(), 'validIds'] as const,
  validIds: (modelTypes: string[]) =>
    [
      ...ModelGroupKeys.validIdsRoot(),
      normalizeModelTypes(modelTypes),
    ] as const,
  action: (action: string, groupId = '') =>
    [...ModelGroupKeys.all(), 'action', action, groupId] as const,
};

async function fetchModelGroupList(accessUserId?: string) {
  const { data } = await modelGroupService.list(accessUserId);
  if (data.code !== 0) throw new Error(data.message || 'Failed to list groups');
  return data.data ?? [];
}
export async function fetchModelGroupMembers(groupId: string) {
  const { data } = await modelGroupService.members(groupId);
  if (data.code !== 0)
    throw new Error(data.message || 'Failed to list members');
  return data.data ?? [];
}
export function useModelGroupList(
  enabled = true,
  accessUserId?: string | null,
) {
  const contextAccessUserId = useContext(ModelResourceAccessUserIdContext);
  const waitingForAccessUser =
    accessUserId === null ||
    (accessUserId === undefined && contextAccessUserId === null);
  const resolvedAccessUserId =
    accessUserId === undefined
      ? (contextAccessUserId ?? undefined)
      : (accessUserId ?? undefined);
  const query = useQuery<IModelGroup[]>({
    queryKey: ModelGroupKeys.list(
      waitingForAccessUser ? null : resolvedAccessUserId,
    ),
    queryFn: () => fetchModelGroupList(resolvedAccessUserId ?? undefined),
    initialData: [],
    enabled: enabled && !waitingForAccessUser,
    gcTime: 0,
  });
  return { ...query, loading: query.isFetching && !query.isFetched };
}
export function useModelGroupMembers(groupId: string, enabled: boolean) {
  const query = useQuery<IModelGroupMember[]>({
    queryKey: ModelGroupKeys.detail(groupId),
    queryFn: () => fetchModelGroupMembers(groupId),
    initialData: [],
    enabled: enabled && !!groupId,
    gcTime: 0,
  });
  return { ...query, loading: query.isFetching && !query.isFetched };
}
function invalidateOptionCaches(queryClient: QueryClient) {
  queryClient.invalidateQueries({ queryKey: ModelGroupKeys.optionsRoot() });
  queryClient.invalidateQueries({ queryKey: ModelGroupKeys.validIdsRoot() });
}
function useModelGroupMutation<TVariables>(
  action: string,
  mutationFn: (variables: TVariables) => Promise<number | undefined>,
  invalidate: (queryClient: QueryClient, variables: TVariables) => void,
) {
  const queryClient = useQueryClient();
  const { isPending, mutateAsync } = useMutation({
    mutationKey: ModelGroupKeys.action(action),
    mutationFn,
    onSuccess: (code, variables) => {
      if (code === 0) invalidate(queryClient, variables);
    },
  });
  const run = useCallback(
    (variables: TVariables) => mutateAsync(variables),
    [mutateAsync],
  );
  return { loading: isPending, run };
}
export function useCreateModelGroup() {
  return useModelGroupMutation<ICreateModelGroupRequest>(
    'create',
    async (body) => (await modelGroupService.create(body)).data.code,
    (queryClient) => {
      queryClient.invalidateQueries({ queryKey: ModelGroupKeys.list() });
      invalidateOptionCaches(queryClient);
    },
  );
}
export function useDeleteModelGroup(groupId: string) {
  return useModelGroupMutation<void>(
    'delete',
    async () => (await modelGroupService.remove(groupId)).data.code,
    (queryClient) => {
      queryClient.setQueriesData<IModelGroup[]>(
        { queryKey: ModelGroupKeys.list() },
        (groups) => groups?.filter((group) => group.id !== groupId),
      );
      queryClient.invalidateQueries({ queryKey: ModelGroupKeys.list() });
      queryClient.removeQueries({
        queryKey: ModelGroupKeys.detail(groupId),
      });
      invalidateOptionCaches(queryClient);
    },
  );
}
export function useUpdateModelGroupMemberStatus(groupId: string) {
  return useModelGroupMutation<IUpdateModelGroupMemberStatusRequest>(
    'updateMemberStatus',
    async (body) =>
      (await modelGroupService.updateMemberStatus(groupId, body)).data.code,
    (queryClient) => {
      queryClient.invalidateQueries({
        queryKey: ModelGroupKeys.detail(groupId),
      });
      invalidateOptionCaches(queryClient);
    },
  );
}
export function useAddModelGroupMember(groupId: string) {
  return useModelGroupMutation<IAddModelToGroupRequest>(
    'addMember',
    async (body) =>
      (await modelGroupService.addMember(groupId, body)).data.code,
    (queryClient) => {
      queryClient.invalidateQueries({
        queryKey: ModelGroupKeys.detail(groupId),
      });
      invalidateOptionCaches(queryClient);
    },
  );
}
export function useRemoveModelGroupMember(groupId: string) {
  return useModelGroupMutation<IRemoveModelFromGroupRequest>(
    'removeMember',
    async (body) =>
      (await modelGroupService.removeMember(groupId, body)).data.code,
    (queryClient) => {
      queryClient.invalidateQueries({
        queryKey: ModelGroupKeys.detail(groupId),
      });
      invalidateOptionCaches(queryClient);
    },
  );
}
export function useReorderModelGroupMembers(groupId: string) {
  return useModelGroupMutation<IReorderModelGroupMembersRequest>(
    'reorderMembers',
    async (body) =>
      (await modelGroupService.reorderMembers(groupId, body)).data.code,
    (queryClient) => {
      queryClient.invalidateQueries({
        queryKey: ModelGroupKeys.detail(groupId),
      });
      invalidateOptionCaches(queryClient);
    },
  );
}
