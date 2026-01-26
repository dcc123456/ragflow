import message from '@/components/ui/message';
import {
  IEvaluationCase,
  IEvaluationDataset,
  IEvaluationRecommendation,
  IEvaluationRun,
  IEvaluationRunResult,
} from '@/interfaces/database/evaluation';
import {
  IEvaluationAddCaseQueryParams,
  IEvaluationCreateDatasetRequestBody,
  IEvaluationStartRunRequestBody,
  IEvaluationUpdateCaseRequestBody,
  IEvaluationUpdateDatasetRequestBody,
  IEvaluationUpdateRunRequestBody,
} from '@/interfaces/request/evaluation';
import evaluationService from '@/services/evaluation-service';
import api from '@/utils/api';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useDebounce } from 'ahooks';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useGetPaginationWithRouter,
  useHandleSearchChange,
} from './logic-hooks';

export const enum EvaluationApiAction {
  // Dataset
  CreateDataset = 'createDataset',
  ListDataset = 'listDataset',
  GetDataset = 'getDataset',
  UpdateDataset = 'updateDataset',
  DeleteDataset = 'deleteDataset',

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

//#region Dataset Hooks

export const useFetchEvaluationDatasetList = () => {
  const { searchString, handleInputChange } = useHandleSearchChange();
  const { pagination, setPagination } = useGetPaginationWithRouter();
  const debouncedSearchString = useDebounce(searchString, { wait: 500 });

  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<{ datasets: IEvaluationDataset[]; total: number }>({
    queryKey: [
      EvaluationApiAction.ListDataset,
      {
        debouncedSearchString,
        ...pagination,
      },
    ],
    initialData: { datasets: [], total: 0 },
    gcTime: 0,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await evaluationService.listDataset(
        {
          params: {
            keywords: debouncedSearchString,
            page_size: pagination.pageSize,
            page: pagination.current,
          },
        },
        true,
      );

      return data?.data ?? { datasets: [], total: 0 };
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

export const useFetchEvaluationDataset = (datasetId: string) => {
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IEvaluationDataset>({
    queryKey: [EvaluationApiAction.GetDataset, datasetId],
    gcTime: 0,
    initialData: {} as IEvaluationDataset,
    enabled: !!datasetId,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await evaluationService.getDataset(
        {
          url: api.evaluationGetDataset(datasetId),
          method: 'get',
        },
        true,
      );

      return data?.data ?? ({} as IEvaluationDataset);
    },
  });

  return { data, loading, refetch };
};

export const useCreateEvaluationDataset = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.CreateDataset],
    mutationFn: async (params: IEvaluationCreateDatasetRequestBody) => {
      const { data } = await evaluationService.createDataset(params);
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListDataset],
        });
        message.success(t('message.created'));
      }
      return data;
    },
  });

  return { data, loading, createEvaluationDataset: mutateAsync };
};

export const useUpdateEvaluationDataset = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.UpdateDataset],
    mutationFn: async ({
      datasetId,
      ...params
    }: { datasetId: string } & IEvaluationUpdateDatasetRequestBody) => {
      const { data } = await evaluationService.updateDataset(
        {
          url: api.evaluationUpdateDataset(datasetId),
          data: params,
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListDataset],
        });
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetDataset, datasetId],
        });
        message.success(t('message.modified'));
      }
      return data;
    },
  });

  return { data, loading, updateEvaluationDataset: mutateAsync };
};

export const useDeleteEvaluationDataset = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.DeleteDataset],
    mutationFn: async (datasetId: string) => {
      const { data } = await evaluationService.deleteDataset(
        {
          url: api.evaluationDeleteDataset(datasetId),
          data: {},
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListDataset],
        });
        message.success(t('message.deleted'));
      }
      return data;
    },
  });

  return { data, loading, deleteEvaluationDataset: mutateAsync };
};

//#endregion

//#region Case Hooks

export const useFetchEvaluationCaseList = (datasetId: string) => {
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
      datasetId,
      {
        debouncedSearchString,
        ...pagination,
      },
    ],
    initialData: { cases: [], total: 0 },
    gcTime: 0,
    refetchOnWindowFocus: false,
    enabled: !!datasetId,
    queryFn: async () => {
      const { data } = await evaluationService.listCase(
        {
          url: api.evaluationListCase(datasetId),
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
      datasetId,
      ...params
    }: { datasetId: string } & IEvaluationAddCaseQueryParams) => {
      const { data } = await evaluationService.addCase(
        {
          url: api.evaluationAddCase(datasetId),
          params: params,
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListCase, datasetId],
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
      datasetId,
      file,
    }: {
      datasetId: string;
      file?: File;
    }) => {
      const formData = new FormData();
      if (file) {
        formData.append('file', file);
      }

      const { data } = await evaluationService.importCase(
        {
          url: api.evaluationImportCase(datasetId),
          data: formData,
          headers: {
            'Content-Type': 'multipart/form-data',
          },
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListCase, datasetId],
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

export const useFetchEvaluationRun = (runId: string) => {
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
      const { data } = await evaluationService.getRun(
        {
          url: api.evaluationGetRun(runId),
          method: 'get',
        },
        true,
      );

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

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.StartRun],
    mutationFn: async (params: IEvaluationStartRunRequestBody) => {
      const { data } = await evaluationService.startRun(params);
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

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.CalculateRunsMetrics],
    mutationFn: async (runId: string) => {
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
