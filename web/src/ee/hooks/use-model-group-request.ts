import type {
  IModelGroup,
  IModelGroupMember,
} from '@/ee/interfaces/database/model-group';
import {
  IAddModelToGroupRequest,
  ICreateModelGroupRequest,
  IRemoveModelFromGroupRequest,
} from '@/ee/interfaces/request/model-group';
import modelGroupService from '@/ee/services/model-group-service';
import { normalizeModelTypes } from '@/ee/utils/model-group-util';
import {
  QueryClient,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import { useCallback } from 'react';

export const ModelGroupKeys = {
  all: () => ['modelGroup'] as const,
  list: () => [...ModelGroupKeys.all(), 'list'] as const,
  detail: (groupName: string) =>
    [...ModelGroupKeys.all(), 'detail', groupName] as const,
  optionsRoot: () => [...ModelGroupKeys.all(), 'options'] as const,
  options: (modelTypes: string[]) =>
    [...ModelGroupKeys.optionsRoot(), normalizeModelTypes(modelTypes)] as const,
  validIdsRoot: () => [...ModelGroupKeys.all(), 'validIds'] as const,
  validIds: (modelTypes: string[]) =>
    [
      ...ModelGroupKeys.validIdsRoot(),
      normalizeModelTypes(modelTypes),
    ] as const,
  action: (action: string, groupName = '') =>
    [...ModelGroupKeys.all(), 'action', action, groupName] as const,
};

async function fetchModelGroupList() {
  const { data } = await modelGroupService.list();
  if (data.code !== 0) throw new Error(data.message || 'Failed to list groups');
  return data.data ?? [];
}
export async function fetchModelGroupMembers(groupName: string) {
  const { data } = await modelGroupService.members(groupName);
  if (data.code !== 0)
    throw new Error(data.message || 'Failed to list members');
  return data.data ?? [];
}
export function useModelGroupList(enabled = true) {
  const query = useQuery<IModelGroup[]>({
    queryKey: ModelGroupKeys.list(),
    queryFn: fetchModelGroupList,
    initialData: [],
    enabled,
    gcTime: 0,
  });
  return { ...query, loading: query.isFetching && !query.isFetched };
}
export function useModelGroupMembers(groupName: string, enabled: boolean) {
  const query = useQuery<IModelGroupMember[]>({
    queryKey: ModelGroupKeys.detail(groupName),
    queryFn: () => fetchModelGroupMembers(groupName),
    initialData: [],
    enabled: enabled && !!groupName,
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
export function useDeleteModelGroup(groupName: string) {
  return useModelGroupMutation<void>(
    'delete',
    async () => (await modelGroupService.remove(groupName)).data.code,
    (queryClient) => {
      queryClient.invalidateQueries({ queryKey: ModelGroupKeys.list() });
      queryClient.removeQueries({
        queryKey: ModelGroupKeys.detail(groupName),
      });
      invalidateOptionCaches(queryClient);
    },
  );
}
export function useAddModelGroupMember(groupName: string) {
  return useModelGroupMutation<IAddModelToGroupRequest>(
    'addMember',
    async (body) =>
      (await modelGroupService.addMember(groupName, body)).data.code,
    (queryClient) => {
      queryClient.invalidateQueries({
        queryKey: ModelGroupKeys.detail(groupName),
      });
      invalidateOptionCaches(queryClient);
    },
  );
}
export function useRemoveModelGroupMember(groupName: string) {
  return useModelGroupMutation<IRemoveModelFromGroupRequest>(
    'removeMember',
    async (body) =>
      (await modelGroupService.removeMember(groupName, body)).data.code,
    (queryClient) => {
      queryClient.invalidateQueries({
        queryKey: ModelGroupKeys.detail(groupName),
      });
      invalidateOptionCaches(queryClient);
    },
  );
}
