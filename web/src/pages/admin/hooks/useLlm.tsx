import { chain, isEmpty, isEqual, keyBy } from 'lodash';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useMemo } from 'react';

import {
  LlmItem,
  LlmKeys,
  useFetchAllAddedModels,
} from '@/hooks/use-llm-request';
import { IFactory } from '@/interfaces/database/llm';
import { mapModelKey } from '@/pages/user-setting/setting-model/instance-card/available-models';
import {
  addFactory,
  deleteFactory,
  getRoleDefaultModels,
  listAllFactoryLlms,
  listLlmFactories,
  setRoleDefaultModel,
} from '@/services/admin-service';
import '@/services/admin-llm-scope';

import { LlmModelType } from '@/constants/knowledge';

export { LlmModelType };

export type LlmModelTagRaw =
  | 'LLM'
  | 'TEXT EMBEDDING'
  | 'TEXT RE-RANK'
  | 'TTS'
  | 'SPEECH2TEXT'
  | 'IMAGE2TEXT'
  | 'MODERATION'
  | 'EMBEDDING';

export type LlmModelTag =
  | Exclude<LlmModelTagRaw, keyof typeof LLM_MODEL_TAG_MAP>
  | (typeof LLM_MODEL_TAG_MAP)[keyof typeof LLM_MODEL_TAG_MAP];

export type LlmFactory = Omit<IFactory, 'tags' | 'status'> & {
  rank: number;
  tags: LlmModelTagRaw[];
  status: boolean;
  sortedTags: LlmModelTag[];
  model_types: LlmModelType[];
};

export const LLM_MODEL_TAG_MAP = {
  IMAGE2TEXT: 'VLM',
  SPEECH2TEXT: 'ASR',
  'TEXT RE-RANK': 'Rerank',
  'TEXT EMBEDDING': 'Embedding',
  EMBEDDING: 'Embedding',
} as const;

export const LLM_MODEL_TAG_ORDER = {
  LLM: 1,
  EMBEDDING: 2,
  'TEXT EMBEDDING': 2,
  'TEXT RE-RANK': 3,
  TTS: 4,
  SPEECH2TEXT: 5,
  IMAGE2TEXT: 6,
  MODERATION: 7,
} as const;

export const useRoleDefaultModels = (roleName: string) => {
  const {
    data: { model_list, setup_status },
    isFetching,
    refetch,
  } = useQuery({
    queryKey: ['admin/getRoleDefaultModels', roleName],
    queryFn: async () => {
      const { data } = await getRoleDefaultModels(roleName);

      if (data.code !== 0) {
        throw new Error(data.message);
      }

      return {
        model_list: keyBy(data.data.model_list, 'model_type'),
        setup_status: data.data!.setup_status,
      } as {
        model_list: Record<
          AdminService.RoleDefaultModelType,
          AdminService.RoleDefaultModelItem
        >;
        setup_status: AdminService.RoleDefaultModelSetupStatus;
      };
    },
    gcTime: 0,
    initialData: {
      model_list: {} as Record<
        AdminService.RoleDefaultModelType,
        AdminService.RoleDefaultModelItem
      >,
      setup_status: 'not_set',
    },
  });

  const { mutateAsync, isPending: isUpdating } = useMutation({
    mutationKey: ['admin/setRoleDefaultModel', roleName],
    mutationFn: async (input: AdminService.SetRoleDefaultModelInput) => {
      const { data } = await setRoleDefaultModel(roleName, input);
      return data?.data ?? {};
    },
    onSuccess: () => {
      refetch();
    },
  });

  return {
    defaultModels: model_list,
    setupStatus: setup_status,
    setDefaultModel: mutateAsync,
    isFetching,
    isUpdating,
  };
};

export const useLlmFactoryList = () => {
  const { data, isFetching } = useQuery({
    queryKey: ['admin/llmFactoryList'],
    queryFn: async () => {
      const { data } = await listLlmFactories();

      const mappedData =
        data?.data?.map((item) => {
          const rawTags = item.tags
            .split(',')
            .map((tag) => tag.trim() as LlmModelTagRaw);

          return {
            ...item,
            status: item.status === '1',
            tags: rawTags,
            sortedTags: rawTags
              .sort((a, b) => LLM_MODEL_TAG_ORDER[a] - LLM_MODEL_TAG_ORDER[b])
              // @ts-ignore
              .map<LlmModelTag>((tag) => LLM_MODEL_TAG_MAP[tag] ?? tag),
          };
        }) ?? [];

      return mappedData as LlmFactory[];
    },
    gcTime: 0,
    initialData: [],
  });

  const sortedAllTags = useMemo(
    () => [...new Set(data.flatMap((model) => model.sortedTags))],
    [data],
  );

  return {
    data,
    sortedAllTags,
    isFetching,
  };
};

/**
 * Configured (added) models, grouped per provider.
 *
 * Data source: `GET /api/v1/admin/models`. It is reached through the tenant
 * hook `useFetchAllAddedModels` (`GET /api/v1/models`) combined with the
 * route-scoped rewrite in `services/admin-llm-scope`: this page renders
 * under `/admin/roles/...`, so the shared hook's URL is redirected to the
 * admin gateway and no open-source hook or service file has to change.
 *
 * The previous implementation called `GET /admin/llm/my_llms`, which the
 * admin gateway no longer serves (404).
 */
export const useMyLlmList = () => {
  const { data: addedModels, loading: isFetching } = useFetchAllAddedModels();

  const data = useMemo<LlmItem[]>(
    () =>
      chain(addedModels)
        .groupBy('provider_name')
        .map((models, name) => {
          // The card renders one row per `llm` entry keyed by model name, so
          // collapse a model configured under several instances into a single
          // row rather than emitting duplicate React keys.
          const llm = chain(models)
            .uniqBy('name')
            .map((model) => ({
              name: model.name,
              type: model.model_type
                .map((t) => mapModelKey[t as keyof typeof mapModelKey] ?? t)
                .join(', '),
              status: '1' as const,
              used_token: 0,
            }))
            .value();

          // Raw `model_type` keys, as `available-models.tsx` emits them: the
          // card sorts via `sortModelTypes` and labels via `mapModelKey`.
          const tags = [
            ...new Set(models.flatMap((model) => model.model_type)),
          ];

          return {
            name,
            logo: '',
            tags: tags.join(','),
            llm,
          };
        })
        .value(),
    [addedModels],
  );

  return {
    data,
    isFetching,
  };
};

export const useAllFactoryLlmList = () => {
  const { data, isFetching } = useQuery({
    queryKey: ['admin/allFactoryLlmList'],
    queryFn: async () => {
      const { data } = await listAllFactoryLlms();
      return data?.data ?? {};
    },
    gcTime: 0,
    initialData: {},
  });

  return {
    data,
    isFetching,
  };
};

export const useAddFactory = (factoryName: string) => {
  const queryClient = useQueryClient();

  const { mutateAsync, isPending } = useMutation({
    mutationKey: ['admin/addFactory', factoryName],
    mutationFn: async (inputs: AdminService.AddLlmFactoryInput) => {
      const { data } = await addFactory({
        ...inputs,
        llm_factory: factoryName,
      });

      return data?.data ?? {};
    },
    onSuccess: () => {
      // `useMyLlmList` is now backed by `LlmKeys.allModels()`, so that is the
      // key that has to be invalidated for the list to refresh.
      queryClient.invalidateQueries({ queryKey: LlmKeys.allModels() });
      queryClient.invalidateQueries({ queryKey: ['admin/allFactoryLlmList'] });
    },
  });

  return {
    addFactory: mutateAsync,
    isPending,
  };
};

export const useDeleteMyFactory = (factoryName: string) => {
  const queryClient = useQueryClient();

  const { mutateAsync, isPending } = useMutation({
    mutationKey: ['admin/mutateLlm'],
    mutationFn: async () => {
      const { data } = await deleteFactory(factoryName);
      return data?.data ?? {};
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: LlmKeys.allModels() });
      queryClient.invalidateQueries({ queryKey: ['admin/allFactoryLlmList'] });
    },
  });

  return {
    delete: mutateAsync,
    isPending,
  };
};

export const useDefaultModelOptions = () => {
  const { data: llmList } = useAllFactoryLlmList();

  const modelOptions = useMemo(() => {
    if (isEmpty(llmList)) {
      return {};
    }

    const optionsByType = chain(llmList)
      .mapValues((list) => list.filter((x) => x.available))
      .pickBy('length')
      .values()
      .flatten()
      .map((llm) => ({
        ...llm,
        label: llm.llm_name,
        id: `${llm.llm_name}@${llm.fid}`,
        value: `${llm.llm_name}@${llm.fid}`,
        disabled: !llm.available,
        is_tools: llm.is_tools,
      }))
      .orderBy('llm_name')
      .groupBy((llm) =>
        llm.tags?.toLowerCase().includes(LlmModelType.Image2text)
          ? LlmModelType.Image2text
          : llm.model_type,
      )
      .value();

    if (optionsByType.chat && optionsByType.image2text) {
      optionsByType.chat = chain(optionsByType.chat)
        .concat(optionsByType.image2text)
        .uniqWith(isEqual)
        .orderBy('llm_name')
        .value();
    }

    return chain(optionsByType)
      .mapValues((list) =>
        chain(list)
          .groupBy('fid')
          .map((values, fid) => ({
            label: fid,
            options: values,
          }))
          .value(),
      )
      .value();
  }, [llmList]);

  return {
    modelOptions,
  };
};
