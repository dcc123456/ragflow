import { IModelGroupDetailResult } from '@/ee/interfaces/database/model-group';
import { IAddedModel } from '@/interfaces/database/llm';
import {
  buildCompatibleModelGroupIds,
  buildModelGroupRef,
  normalizeModelTypes,
} from '@/ee/utils/model-group-util';
import { useQueries, useQueryClient } from '@tanstack/react-query';
import { useCallback, useMemo } from 'react';
import {
  fetchModelGroupMembers,
  ModelGroupKeys,
  useModelGroupList,
} from './use-model-group-request';

export function useModelGroupOptions(
  modelTypes: string[],
  allModels: IAddedModel[],
  enabled = true,
  providers?: string[],
) {
  const queryClient = useQueryClient();
  const normalizedTypes = useMemo(
    () => normalizeModelTypes(modelTypes),
    [modelTypes],
  );
  const {
    data: groups,
    isFetched: listFetched,
    isError: listError,
    refetch: refetchGroups,
  } = useModelGroupList(enabled);
  const { details, membersFetched } = useQueries({
    queries: groups.map((group) => ({
      queryKey: ModelGroupKeys.detail(group.id),
      queryFn: () => fetchModelGroupMembers(group.id),
      enabled,
      gcTime: 0,
    })),
    combine: (results) => ({
      details: groups.map((group, index): IModelGroupDetailResult => ({
        group,
        members: results[index]?.data ?? [],
        isError: results[index]?.isError ?? false,
      })),
      membersFetched: results.every((query) => query.isFetched),
    }),
  });
  const compatibleGroupIds = useMemo(
    () =>
      listError
        ? new Set<string>()
        : buildCompatibleModelGroupIds(
            details,
            allModels,
            normalizedTypes,
            providers,
          ),
    [details, allModels, normalizedTypes, providers, listError],
  );
  const isFetched = listFetched && membersFetched;
  const resolveValidIds = useCallback(
    async (freshModels: IAddedModel[]) => {
      const listResult = await refetchGroups();
      if (listResult.isError) throw listResult.error;
      const freshGroups = listResult.data ?? [];
      const freshDetails = await Promise.all(
        freshGroups.map(async (group): Promise<IModelGroupDetailResult> => {
          try {
            const members = await queryClient.fetchQuery({
              queryKey: ModelGroupKeys.detail(group.id),
              queryFn: () => fetchModelGroupMembers(group.id),
              staleTime: 0,
            });
            return { group, members, isError: false };
          } catch {
            return { group, members: [], isError: true };
          }
        }),
      );
      const ids = buildCompatibleModelGroupIds(
        freshDetails,
        freshModels,
        normalizedTypes,
        providers,
      );
      return new Set(Array.from(ids, buildModelGroupRef));
    },
    [normalizedTypes, providers, queryClient, refetchGroups],
  );
  return {
    groups,
    compatibleGroupIds,
    isFetched,
    isError: listError,
    refetch: refetchGroups,
    resolveValidIds,
  };
}
