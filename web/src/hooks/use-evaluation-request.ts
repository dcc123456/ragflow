import message from '@/components/ui/message';
import {
  IEvaluationCase,
  IEvaluationCollection,
  IEvaluationRecommendation,
  IEvaluationRun,
  IEvaluationRunResult,
} from '@/interfaces/database/evaluation';
import {
  IEvaluationAddCaseQueryParams,
  IEvaluationCreateCollectionRequestBody,
  IEvaluationStartRunRequestBody,
  IEvaluationUpdateCaseRequestBody,
  IEvaluationUpdateCollectionRequestBody,
  IEvaluationUpdateRunRequestBody,
} from '@/interfaces/request/evaluation';
import evaluationService from '@/services/evaluation-service';
import api from '@/utils/api';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useDebounce } from 'ahooks';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router';
import {
  useGetPaginationWithRouter,
  useHandleSearchChange,
} from './logic-hooks';
import { useEvaluationUrl } from './use-evaluation-url';

export const enum EvaluationApiAction {
  // Collection
  CreateCollection = 'createCollection',
  ListCollection = 'listCollection',
  ListAllCollection = 'listAllCollection',
  GetCollection = 'getCollection',
  UpdateCollection = 'updateCollection',
  DeleteCollection = 'deleteCollection',

  // Case
  AddCase = 'addCase',
  ImportCase = 'importCase',
  ListCase = 'listCase',
  UpdateCase = 'updateCase',
  DeleteCase = 'deleteCase',

  // Run
  StartRun = 'startRun',
  GetRun = 'getRun',
  GetRunResults = 'getRunResults',
  ListRun = 'listRun',
  UpdateRun = 'updateRun',
  DeleteRun = 'deleteRun',
  DuplicateRun = 'duplicateRun',

  // Execute
  ExecuteAll = 'executeAll',
  ExecuteCase = 'executeCase',

  // Metrics
  CalculateCaseMetric = 'calculateCaseMetric',
  CalculateRunsMetrics = 'calculateRunsMetrics',
  CalculateRunsMetric = 'calculateRunsMetric',
  CalculateCaseMetrics = 'calculateCaseMetrics',

  // Clear Results
  ClearCaseResult = 'clearCaseResult',
  ClearCaseMetricResult = 'clearCaseMetricResult',
  ClearCaseAnswer = 'clearCaseAnswer',

  // Other
  GetRecommendations = 'getRecommendations',
  ExportRun = 'exportRun',
}

//#region Collection Hooks

export const useFetchEvaluationCollectionList = () => {
  const { searchString, handleInputChange } = useHandleSearchChange();
  const { pagination, setPagination } = useGetPaginationWithRouter();
  const debouncedSearchString = useDebounce(searchString, { wait: 500 });

  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<{ collections: IEvaluationCollection[]; total: number }>({
    queryKey: [
      EvaluationApiAction.ListCollection,
      {
        debouncedSearchString,
        ...pagination,
      },
    ],
    initialData: { collections: [], total: 0 },
    gcTime: 0,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await evaluationService.listCollection(
        {
          params: {
            keywords: debouncedSearchString,
            page_size: pagination.pageSize,
            page: pagination.current,
          },
        },
        true,
      );

      return data?.data ?? { collections: [], total: 0 };
    },
  });

  const onInputChange: React.ChangeEventHandler<HTMLInputElement> = useCallback(
    (e) => {
      handleInputChange(e);
    },
    [handleInputChange],
  );

  return {
    data,
    loading,
    refetch,
    searchString,
    handleInputChange: onInputChange,
    pagination: { ...pagination, total: data?.total },
    setPagination,
  };
};

export function useFetchAllEvaluationCollection() {
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<{ collections: IEvaluationCollection[]; total: number }>({
    queryKey: [EvaluationApiAction.ListAllCollection],
    initialData: { collections: [], total: 0 },
    gcTime: 0,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await evaluationService.listCollection(
        {
          params: {
            page_size: 100000000,
            page: 1,
          },
        },
        true,
      );

      return data?.data ?? { collections: [], total: 0 };
    },
  });

  return { data, loading, refetch };
}

export const useFetchEvaluationCollection = (collectionId: string) => {
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IEvaluationCollection>({
    queryKey: [EvaluationApiAction.GetCollection, collectionId],
    gcTime: 0,
    initialData: {} as IEvaluationCollection,
    enabled: !!collectionId,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await evaluationService.getCollection(
        {
          url: api.evaluationGetCollection(collectionId),
          method: 'get',
        },
        true,
      );

      return data?.data ?? ({} as IEvaluationCollection);
    },
  });

  return { data, loading, refetch };
};

export const useCreateEvaluationCollection = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.CreateCollection],
    mutationFn: async (params: IEvaluationCreateCollectionRequestBody) => {
      const { data } = await evaluationService.createCollection(params);
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListCollection],
        });
        message.success(t('message.created'));
      }
      return data;
    },
  });

  return { data, loading, createEvaluationCollection: mutateAsync };
};

export const useUpdateEvaluationCollection = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.UpdateCollection],
    mutationFn: async ({
      collectionId,
      ...params
    }: { collectionId: string } & IEvaluationUpdateCollectionRequestBody) => {
      const { data } = await evaluationService.updateCollection(
        {
          url: api.evaluationUpdateCollection(collectionId),
          data: params,
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListCollection],
        });
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetCollection, collectionId],
        });
        message.success(t('message.modified'));
      }
      return data;
    },
  });

  return { data, loading, updateEvaluationCollection: mutateAsync };
};

export const useDeleteEvaluationCollection = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.DeleteCollection],
    mutationFn: async (collectionId: string) => {
      const { data } = await evaluationService.deleteCollection(
        {
          url: api.evaluationDeleteCollection(collectionId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListCollection],
        });
        message.success(t('message.deleted'));
      }
      return data;
    },
  });

  return { data, loading, deleteEvaluationCollection: mutateAsync };
};

//#endregion

//#region Case Hooks

export const useFetchEvaluationCaseList = (collectionId: string) => {
  const { searchString, handleInputChange } = useHandleSearchChange();
  const { pagination, setPagination } = useGetPaginationWithRouter();
  const debouncedSearchString = useDebounce(searchString, { wait: 500 });

  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<{ cases: IEvaluationCase[]; total: number }>({
    queryKey: [
      EvaluationApiAction.ListCase,
      collectionId,
      {
        debouncedSearchString,
        ...pagination,
      },
    ],
    initialData: { cases: [], total: 0 },
    gcTime: 0,
    refetchOnWindowFocus: false,
    enabled: !!collectionId,
    queryFn: async () => {
      const { data } = await evaluationService.listCase(
        {
          url: api.evaluationListCase(collectionId),
          method: 'get',
          params: {
            keywords: debouncedSearchString,
            page_size: pagination.pageSize,
            page: pagination.current,
          },
        },
        true,
      );

      return data?.data ?? { cases: [], total: 0 };
    },
  });

  const onInputChange: React.ChangeEventHandler<HTMLInputElement> = useCallback(
    (e) => {
      handleInputChange(e);
    },
    [handleInputChange],
  );

  return {
    data,
    loading,
    refetch,
    searchString,
    handleInputChange: onInputChange,
    pagination: { ...pagination, total: data?.total },
    setPagination,
  };
};

export const useAddEvaluationCase = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.AddCase],
    mutationFn: async ({
      collectionId,
      ...params
    }: { collectionId: string } & IEvaluationAddCaseQueryParams) => {
      const { data } = await evaluationService.addCase(
        {
          url: api.evaluationAddCase(collectionId),
          params: params,
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListCase, collectionId],
        });
        message.success(t('message.created'));
      }
      return data;
    },
  });

  return { data, loading, addEvaluationCase: mutateAsync };
};

export const useImportEvaluationCase = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.ImportCase],
    mutationFn: async ({
      collectionId,
      file,
    }: {
      collectionId: string;
      file?: File;
    }) => {
      const formData = new FormData();
      if (file) {
        formData.append('file', file);
      }

      const { data } = await evaluationService.importCase(
        {
          url: api.evaluationImportCase(collectionId),
          data: formData,
          headers: {
            'Content-Type': 'multipart/form-data',
          },
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListCase, collectionId],
        });
        message.success(t('message.uploaded'));
      }
      return data;
    },
  });

  return { data, loading, importEvaluationCase: mutateAsync };
};

export const useUpdateEvaluationCase = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.UpdateCase],
    mutationFn: async ({
      caseId,
      ...params
    }: { caseId: string } & IEvaluationUpdateCaseRequestBody) => {
      const { data } = await evaluationService.updateCase(
        {
          url: api.evaluationUpdateCase(caseId),
          data: params,
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          exact: false,
          queryKey: [EvaluationApiAction.ListCase],
        });
        message.success(t('message.modified'));
      }
      return data;
    },
  });

  return { data, loading, updateEvaluationCase: mutateAsync };
};

export const useDeleteEvaluationCase = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.DeleteCase],
    mutationFn: async (caseId: string) => {
      const { data } = await evaluationService.deleteCase(
        {
          url: api.evaluationDeleteCase(caseId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          exact: false,
          queryKey: [EvaluationApiAction.ListCase],
        });
        message.success(t('message.deleted'));
      }
      return data;
    },
  });

  return { data, loading, deleteEvaluationCase: mutateAsync };
};

//#endregion

//#region Run Hooks

export const useFetchEvaluationRunList = () => {
  const { searchString, handleInputChange } = useHandleSearchChange();
  const { pagination, setPagination } = useGetPaginationWithRouter();
  const debouncedSearchString = useDebounce(searchString, { wait: 500 });

  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<{ runs: IEvaluationRun[]; total: number }>({
    queryKey: [
      EvaluationApiAction.ListRun,
      {
        debouncedSearchString,
        ...pagination,
      },
    ],
    initialData: { runs: [], total: 0 },
    gcTime: 0,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await evaluationService.listRun(
        {
          params: {
            keywords: debouncedSearchString,
            page_size: pagination.pageSize,
            page: pagination.current,
          },
        },
        true,
      );

      return data?.data ?? { runs: [], total: 0 };
    },
  });

  const onInputChange: React.ChangeEventHandler<HTMLInputElement> = useCallback(
    (e) => {
      handleInputChange(e);
    },
    [handleInputChange],
  );

  return {
    data,
    loading,
    refetch,
    searchString,
    handleInputChange: onInputChange,
    pagination: { ...pagination, total: data?.total },
    setPagination,
  };
};

export const useFetchEvaluationRun = () => {
  const { runId } = useEvaluationUrl();
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IEvaluationRun>({
    queryKey: [EvaluationApiAction.GetRun, runId],
    gcTime: 0,
    initialData: {} as IEvaluationRun,
    enabled: !!runId,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await evaluationService.getRun(runId);

      return data?.data ?? ({} as IEvaluationRun);
    },
  });

  return { data, loading, refetch };
};

export const useFetchEvaluationRunResults = (runId: string) => {
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IEvaluationRunResult>({
    queryKey: [EvaluationApiAction.GetRunResults, runId],
    gcTime: 0,
    initialData: {} as IEvaluationRunResult,
    enabled: !!runId,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await evaluationService.getRunResults(
        {
          url: api.evaluationGetRunResults(runId),
          method: 'get',
        },
        true,
      );

      return data?.data ?? ({} as IEvaluationRunResult);
    },
  });

  return { data, loading, refetch };
};

export const useStartEvaluationRun = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const { id } = useParams();
  const { type } = useEvaluationUrl();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.StartRun],
    mutationFn: async (params: Partial<IEvaluationStartRunRequestBody>) => {
      const { data } = await evaluationService.startRun({
        target_id: id!,
        target_type: type,
        ...params,
      });
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListRun],
        });
        message.success(t('message.created'));
      }
      return data;
    },
  });

  return { data, loading, startEvaluationRun: mutateAsync };
};

export const useUpdateEvaluationRun = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.UpdateRun],
    mutationFn: async ({
      runId,
      ...params
    }: { runId: string } & IEvaluationUpdateRunRequestBody) => {
      const { data } = await evaluationService.updateRun(
        {
          url: api.evaluationUpdateRun(runId),
          data: params,
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListRun],
        });
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRun, runId],
        });
        message.success(t('message.modified'));
      }
      return data;
    },
  });

  return { data, loading, updateEvaluationRun: mutateAsync };
};

export const useDeleteEvaluationRun = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.DeleteRun],
    mutationFn: async (runId: string) => {
      const { data } = await evaluationService.deleteRun(
        {
          url: api.evaluationDeleteRun(runId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListRun],
        });
        message.success(t('message.deleted'));
      }
      return data;
    },
  });

  return { data, loading, deleteEvaluationRun: mutateAsync };
};

export const useDuplicateEvaluationRun = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.DuplicateRun],
    mutationFn: async (runId: string) => {
      const { data } = await evaluationService.duplicateRun(
        {
          url: api.evaluationDuplicateRun(runId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListRun],
        });
        message.success(t('message.created'));
      }
      return data;
    },
  });

  return { data, loading, duplicateEvaluationRun: mutateAsync };
};

//#endregion

//#region Execute Hooks

export const useExecuteAllEvaluation = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.ExecuteAll],
    mutationFn: async (runId: string) => {
      const { data } = await evaluationService.executeAll(
        {
          url: api.evaluationExecuteAll(runId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRunResults, runId],
        });
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRun, runId],
        });
        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, executeAllEvaluation: mutateAsync };
};

export const useExecuteEvaluationCase = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.ExecuteCase],
    mutationFn: async ({
      runId,
      caseId,
    }: {
      runId: string;
      caseId: string;
    }) => {
      const { data } = await evaluationService.executeCase(
        {
          url: api.evaluationExecuteCase(runId, caseId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRunResults, runId],
        });
        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, executeEvaluationCase: mutateAsync };
};

//#endregion

//#region Metrics Hooks

export const useCalculateEvaluationCaseMetric = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.CalculateCaseMetric],
    mutationFn: async ({
      runId,
      caseId,
    }: {
      runId: string;
      caseId: string;
    }) => {
      const { data } = await evaluationService.calculateCaseMetric(
        {
          url: api.evaluationCalculateCaseMetric(runId, caseId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRunResults, runId],
        });
        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, calculateEvaluationCaseMetric: mutateAsync };
};

export const useCalculateEvaluationRunsMetrics = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const { runId } = useEvaluationUrl();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.CalculateRunsMetrics],
    mutationFn: async () => {
      const { data } = await evaluationService.calculateRunsMetrics(
        {
          url: api.evaluationCalculateRunsMetrics(runId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRunResults, runId],
        });
        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, calculateEvaluationRunsMetrics: mutateAsync };
};

export const useCalculateEvaluationCaseMetrics = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.CalculateCaseMetrics],
    mutationFn: async ({
      runId,
      caseId,
    }: {
      runId: string;
      caseId: string;
    }) => {
      const { data } = await evaluationService.calculateCaseMetrics(
        {
          url: api.evaluationCalculateCaseMetrics(runId, caseId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRunResults, runId],
        });
        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, calculateEvaluationCaseMetrics: mutateAsync };
};

//#endregion

//#region Clear Results Hooks

export const useClearEvaluationCaseResult = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.ClearCaseResult],
    mutationFn: async ({
      runId,
      caseId,
    }: {
      runId: string;
      caseId: string;
    }) => {
      const { data } = await evaluationService.clearCaseResult(
        {
          url: api.evaluationClearCaseResult(runId, caseId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRunResults, runId],
        });
        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, clearEvaluationCaseResult: mutateAsync };
};

export const useClearEvaluationCaseMetric = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.ClearCaseMetricResult],
    mutationFn: async ({
      runId,
      caseId,
    }: {
      runId: string;
      caseId: string;
    }) => {
      const { data } = await evaluationService.clearCaseMetricResult(
        {
          url: api.evaluationClearCaseMetricResult(runId, caseId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRunResults, runId],
        });
        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, clearEvaluationCaseMetric: mutateAsync };
};

export const useClearEvaluationCaseAnswer = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.ClearCaseAnswer],
    mutationFn: async ({
      runId,
      caseId,
    }: {
      runId: string;
      caseId: string;
    }) => {
      const { data } = await evaluationService.clearCaseAnswer(
        {
          url: api.evaluationClearCaseAnswer(runId, caseId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRunResults, runId],
        });
        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, clearEvaluationCaseAnswer: mutateAsync };
};

//#endregion

//#region Other Hooks

export const useFetchEvaluationRecommendations = (runId: string) => {
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IEvaluationRecommendation[]>({
    queryKey: [EvaluationApiAction.GetRecommendations, runId],
    gcTime: 0,
    initialData: [],
    enabled: !!runId,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await evaluationService.getRecommendations(
        {
          url: api.evaluationGetRecommendations(runId),
          method: 'get',
        },
        true,
      );

      return (data?.data as IEvaluationRecommendation[]) ?? [];
    },
  });

  return { data, loading, refetch };
};

export const useExportEvaluationRun = () => {
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.ExportRun],
    mutationFn: async (runId: string) => {
      const { data } = await evaluationService.exportRun(
        {
          url: api.evaluationExportRun(runId),
          data: {},
          responseType: 'blob',
        },
        true,
      );
      if (data) {
        // Create download link
        const url = window.URL.createObjectURL(new Blob([data]));
        const link = document.createElement('a');
        link.href = url;
        link.setAttribute('download', `evaluation-run-${runId}.xlsx`);
        document.body.appendChild(link);
        link.click();
        link.remove();
        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, exportEvaluationRun: mutateAsync };
};

//#endregion
