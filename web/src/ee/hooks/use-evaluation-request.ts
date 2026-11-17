import { UploadFormSchemaType } from '@/components/file-upload-dialog';
import message from '@/components/ui/message';
import { NewEvaluationRunId, RunningStatus } from '@/ee/constants/evaluation';
import {
  EvaluationDetailList,
  IEvaluationCase,
  IEvaluationCollection,
  IEvaluationRun,
  IEvaluationRunResultData,
} from '@/ee/interfaces/database/evaluation';
import {
  IEvaluationAddCaseQueryParams,
  IEvaluationCreateRunRequestBody,
  IEvaluationImportCaseRequestBody,
  IEvaluationStartRunRequestBody,
  IEvaluationUpdateCaseRequestBody,
  IEvaluationUpdateCollectionRequestBody,
  IEvaluationUpdateRunRequestBody,
} from '@/ee/interfaces/request/evaluation';
import evaluationService, {
  evaluationExportBlob,
} from '@/ee/services/evaluation-service';
import { downloadFileFromBlob } from '@/utils/file-util';
import { removeUselessFieldsFromValues } from '@/utils/form';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useDebounce } from 'ahooks';
import { get, pick } from 'lodash';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router';
import { useSetModalState } from '@/hooks/common-hooks';
import { useHandleSearchChange } from '@/hooks/logic-hooks';
import {
  useGetPagination,
  useGetPaginationWithRouter,
} from '@/ee/hooks/use-pagination';
import { useEvaluationUrl } from '@/ee/hooks/use-evaluation-url';

type Pagination = { page?: number; pageSize?: number; total?: number };
export const enum EvaluationApiAction {
  // Collection
  CreateCollection = 'createCollection',
  ListCollection = 'listCollection',
  ListAllCollection = 'listAllCollection',
  GetCollection = 'getCollection',
  UpdateCollection = 'updateCollection',
  DeleteCollection = 'deleteCollection',
  // Detail

  // Case
  AddCase = 'addCase',
  ImportCase = 'importCase',
  ListCase = 'listCase',
  UpdateCase = 'updateCase',
  DeleteCase = 'deleteCase',

  // Run
  Run = 'run',
  StartRun = 'startRun',
  GetRun = 'getRun',
  GetRunResults = 'getRunResults',
  ListRun = 'listRun',
  UpdateRun = 'updateRun',
  DeleteRun = 'deleteRun',
  DuplicateRun = 'duplicateRun',
  CancelRun = 'cancelRun',

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
  ExportRun = 'exportRun',
}

export const EvaluationKeys = {
  action: (action: EvaluationApiAction, ...parameters: unknown[]) =>
    [action, ...parameters] as const,
};

//#region Collection Hooks

export const useFetchEvaluationCollectionList = () => {
  const { searchString, handleInputChange } = useHandleSearchChange();
  const { pagination, changePagination } = useGetPagination();
  const debouncedSearchString = useDebounce(searchString, { wait: 500 });

  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<{ collections: IEvaluationCollection[]; total: number }>({
    queryKey: EvaluationKeys.action(EvaluationApiAction.ListCollection, {
      debouncedSearchString,
      ...pagination,
    }),
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

      changePagination({
        page: pagination.current,
        pageSize: pagination.pageSize,
        total: data?.data.total ?? 0,
      });
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
    pagination,
    setPagination: changePagination,
  };
};

export function useFetchAllEvaluationCollection() {
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<{ collections: IEvaluationCollection[]; total: number }>({
    queryKey: EvaluationKeys.action(EvaluationApiAction.ListAllCollection),
    initialData: { collections: [], total: 0 },
    gcTime: 0,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const collections: IEvaluationCollection[] = [];
      let total = 0;
      for (let page = 1; ; page++) {
        const { data } = await evaluationService.listCollection(
          {
            params: { page_size: 200, page },
          },
          true,
        );
        if (data.code !== 0) throw new Error(data.message);
        const batch = data.data.collections ?? [];
        collections.push(...batch);
        total = data.data.total;
        if (!batch.length || collections.length >= total) break;
      }
      return { collections, total };
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
    queryKey: EvaluationKeys.action(
      EvaluationApiAction.GetCollection,
      collectionId,
    ),
    gcTime: 0,
    initialData: {} as IEvaluationCollection,
    enabled: !!collectionId,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await evaluationService.getCollection(collectionId);

      return data?.data ?? ({} as IEvaluationCollection);
    },
  });

  return { data, loading, refetch };
};

export const useUploadEvaluationFile = () => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.CreateCollection],
    mutationFn: async (params: Pick<UploadFormSchemaType, 'fileList'>) => {
      let code = 0;
      for (const file of params.fileList) {
        const formData = new FormData();
        formData.append('file', file instanceof File ? file : file.file);
        const { data } = await evaluationService.createCollection(formData);
        if (data.code !== 0) {
          code = data.code;
          break;
        }
      }
      queryClient.invalidateQueries({
        queryKey: EvaluationKeys.action(EvaluationApiAction.ListCollection),
      });
      queryClient.invalidateQueries({
        queryKey: EvaluationKeys.action(EvaluationApiAction.ListAllCollection),
      });
      if (code === 0) message.success(t('message.uploaded'));
      return code;
    },
  });

  return { data, loading, uploadFile: mutateAsync };
};

export const useCreateEvaluationCollection = () => {
  const {
    visible: fileUploadVisible,
    hideModal: hideFileUploadModal,
    showModal: showFileUploadModal,
  } = useSetModalState();
  const { uploadFile, loading } = useUploadEvaluationFile();

  const onFileUploadOk = useCallback(
    async ({ fileList }: UploadFormSchemaType): Promise<number | undefined> => {
      if (fileList.length > 0) {
        const ret: number = await uploadFile({ fileList });
        if (ret === 0) {
          hideFileUploadModal();
        }
        return ret;
      }
    },
    [uploadFile, hideFileUploadModal],
  );

  return {
    fileUploadLoading: loading,
    onFileUploadOk,
    fileUploadVisible,
    hideFileUploadModal,
    showFileUploadModal,
  };
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
        collectionId,
        params,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(EvaluationApiAction.ListCollection),
        });
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.ListAllCollection,
          ),
        });
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.GetCollection,
            collectionId,
          ),
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
    mutationFn: async (collectionId: string | string[]) => {
      if (typeof collectionId === 'string') {
        collectionId = [collectionId];
      }
      const { data } = await evaluationService.deleteCollection(
        {
          data: { ids: collectionId },
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(EvaluationApiAction.ListCollection),
        });
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.ListAllCollection,
          ),
        });
        message.success(t('message.deleted'));
      }
      return data;
    },
  });

  return { data, loading, deleteEvaluationCollection: mutateAsync };
};

export const useFetchEvaluationFileContent = (id: string) => {
  const { pagination, changePagination } = useGetPagination();
  const { data, isFetching: loading } = useQuery<EvaluationDetailList>({
    queryKey: EvaluationKeys.action(EvaluationApiAction.ListCase, id, {
      ...pagination,
    }),
    initialData: { cases: [], total: 0 },
    enabled: !!id,
    gcTime: 0,
    queryFn: async () => {
      const { data } = await evaluationService.listCase(id, {
        page: pagination.current,
        page_size: pagination.pageSize,
      });
      changePagination({
        page: pagination.current,
        pageSize: pagination.pageSize,
        total: data?.data.total ?? 0,
      });

      return data?.data;
    },
  });

  const onPageChange = ({ page, pageSize, total }: Pagination) => {
    changePagination({
      ...pagination,
      page: page || pagination.current,
      pageSize,
      total,
    });
  };

  return {
    data,
    pagination,
    setPagination: changePagination,
    onPageChange,
    loading,
  };
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
    queryKey: EvaluationKeys.action(
      EvaluationApiAction.ListCase,
      collectionId,
      {
        debouncedSearchString,
        ...pagination,
      },
    ),
    initialData: { cases: [], total: 0 },
    gcTime: 0,
    refetchOnWindowFocus: false,
    enabled: !!collectionId,
    queryFn: async () => {
      const { data } = await evaluationService.listCase(collectionId, {
        keywords: debouncedSearchString,
        page_size: pagination.pageSize,
        page: pagination.current,
      });

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
      const { data } = await evaluationService.addCase(collectionId, params);
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.ListCase,
            collectionId,
          ),
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
      cases,
    }: { collectionId: string } & IEvaluationImportCaseRequestBody) => {
      const { data } = await evaluationService.importCase(collectionId, {
        cases,
      });
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.ListCase,
            collectionId,
          ),
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
      collectionId,
      caseId,
      ...params
    }: {
      collectionId: string;
      caseId: string;
    } & IEvaluationUpdateCaseRequestBody) => {
      const { data } = await evaluationService.updateCase(
        collectionId,
        caseId,
        params,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          exact: false,
          queryKey: EvaluationKeys.action(EvaluationApiAction.ListCase),
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
    mutationFn: async ({
      collectionId,
      caseId,
    }: {
      collectionId: string;
      caseId: string;
    }) => {
      const { data } = await evaluationService.deleteCase(collectionId, caseId);
      if (data.code === 0) {
        queryClient.invalidateQueries({
          exact: false,
          queryKey: EvaluationKeys.action(EvaluationApiAction.ListCase),
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
  const debouncedSearchString = useDebounce(searchString, { wait: 500 });
  const { id } = useParams();

  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<{ runs: IEvaluationRun[]; total: number }>({
    queryKey: EvaluationKeys.action(EvaluationApiAction.ListRun, {
      debouncedSearchString,
      // ...pagination,
      id,
    }),
    initialData: { runs: [], total: 0 },
    gcTime: 0,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const runs: IEvaluationRun[] = [];
      let total = 0;
      for (let page = 1; ; page++) {
        const { data } = await evaluationService.listRun(
          {
            params: {
              keywords: debouncedSearchString,
              page_size: 200,
              page,
              target_id: id,
            },
          },
          true,
        );
        if (data.code !== 0) throw new Error(data.message);
        const batch = data.data.runs ?? [];
        runs.push(...batch);
        total = data.data.total;
        if (!batch.length || runs.length >= total) break;
      }
      return { runs, total };
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
    pagination: { total: data?.total },
  };
};

export const useFetchEvaluationRun = () => {
  const { runId } = useEvaluationUrl();
  const isSavedRun = !!runId && runId !== NewEvaluationRunId;
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IEvaluationRun>({
    queryKey: EvaluationKeys.action(EvaluationApiAction.GetRun, runId),
    gcTime: 0,
    initialData: {} as IEvaluationRun,
    enabled: isSavedRun,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await evaluationService.getRun(runId);

      return data?.data ?? ({} as IEvaluationRun);
    },
  });

  return { data, loading, refetch };
};

export const useFetchEvaluationRunResults = () => {
  const { runId } = useEvaluationUrl();
  const isSavedRun = !!runId && runId !== NewEvaluationRunId;
  const { pagination, setPagination } = useGetPaginationWithRouter();
  const { t } = useTranslation();

  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IEvaluationRunResultData>({
    queryKey: EvaluationKeys.action(
      EvaluationApiAction.GetRunResults,
      runId,
      pagination,
    ),
    gcTime: 0,
    initialData: {} as IEvaluationRunResultData,
    enabled: isSavedRun,
    refetchOnWindowFocus: false,
    refetchInterval: (query) => {
      // Continue polling when the run has actually been queued or started.
      const data = query.state.data;
      return (data?.run?.status === RunningStatus.PENDING &&
        !!data.run.task_id) ||
        data?.run?.status === RunningStatus.RUNNING
        ? 5000
        : false;
    },
    queryFn: async () => {
      const { data } = await evaluationService.getRunResults(runId, {
        page_size: pagination.pageSize,
        page: pagination.current,
      });

      const resultData = data?.data ?? {};
      const status = resultData.run?.status;

      if (status === RunningStatus.FAILED) {
        message.error(t('evaluation.failed'));
      } else if (status === RunningStatus.COMPLETED) {
        message.success(t('evaluation.completed'));
      }

      return resultData ?? ({} as IEvaluationRunResultData);
    },
  });

  return { data, loading, refetch, setPagination, pagination };
};

export const useStartEvaluationRun = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const { runId } = useEvaluationUrl();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation<unknown, unknown, IEvaluationStartRunRequestBody>({
    mutationKey: [EvaluationApiAction.StartRun],
    mutationFn: async (params) => {
      const { data } = await evaluationService.startRun(runId!, {
        mode: 'generate_and_score',
        ...params,
      });
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(EvaluationApiAction.GetRunResults),
        });
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(EvaluationApiAction.GetRun),
        });
        message.success(t('message.created'));
      }
      return data;
    },
  });

  return { data, loading, startEvaluationRun: mutateAsync };
};

function evaluationSnapshot(
  snapshot: IEvaluationCreateRunRequestBody['config_snapshot'],
) {
  if (!snapshot) return snapshot;
  const target = removeUselessFieldsFromValues(snapshot.target, 'llm_setting.');
  return {
    ...snapshot,
    target: {
      ...pick(target, [
        'name',
        'llm_id',
        'llm_setting',
        'prompt_type',
        'prompt_config',
        'meta_data_filter',
        'similarity_threshold',
        'vector_similarity_weight',
        'top_n',
        'top_k',
        'rerank_candidates_count',
        'do_refer',
        'rerank_id',
        'kb_ids',
        'decision_id',
      ]),
      ...(Array.isArray(target.dataset_ids)
        ? { kb_ids: target.dataset_ids }
        : {}),
      ...(typeof target.keywords_similarity_weight === 'number'
        ? { vector_similarity_weight: 1 - target.keywords_similarity_weight }
        : {}),
    },
  };
}

// create run
export const useCreateRunEvaluation = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const { id } = useParams();
  const { type, setRunId } = useEvaluationUrl();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.Run],
    mutationFn: async (params: Partial<IEvaluationCreateRunRequestBody>) => {
      const { data } = await evaluationService.run({
        target_id: id!,
        target_type: type,
        name: get(params, 'config_snapshot.target.name', ''),
        ...params,
        config_snapshot: evaluationSnapshot(params.config_snapshot),
      });
      if (data.code === 0) {
        setRunId(data.data?.id || '');
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(EvaluationApiAction.ListRun),
        });
        message.success(t('message.created'));
      }
      return data;
    },
  });

  return { data, loading, createRunEvaluation: mutateAsync };
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
      const { data } = await evaluationService.updateRun(runId, {
        ...pick(params, ['name', 'collection_id']),
        ...(params.config_snapshot
          ? { config_snapshot: evaluationSnapshot(params.config_snapshot) }
          : {}),
      });
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(EvaluationApiAction.ListRun),
        });
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(EvaluationApiAction.GetRun, runId),
        });
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.GetRunResults,
            runId,
          ),
        });
        message.success(t('message.modified'));
      } else {
        message.error(data.message);
      }
      return data;
    },
  });

  return { data, loading, updateEvaluationRun: mutateAsync };
};

export const useDeleteEvaluationRun = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const { runId: selectedRunId, setRunId } = useEvaluationUrl();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.DeleteRun],
    mutationFn: async (runId: string) => {
      const { data } = await evaluationService.deleteRun(runId);
      if (data.code === 0) {
        if (selectedRunId === runId) {
          setRunId('');
        }
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(EvaluationApiAction.ListRun),
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
      const { data } = await evaluationService.duplicateRun({
        source_run_id: runId,
      });
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(EvaluationApiAction.ListRun),
        });
        message.success(t('message.created'));
      }
      return data;
    },
  });

  return { data, loading, duplicateEvaluationRun: mutateAsync };
};

export const useCancelEvaluationRun = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const { runId } = useEvaluationUrl();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.CancelRun],
    mutationFn: async () => {
      const { data } = await evaluationService.evaluationCancelRun(runId);
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(EvaluationApiAction.GetRun),
        });
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.GetRunResults,
            runId,
          ),
        });
        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, cancelEvaluationRun: mutateAsync };
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
      const { data } = await evaluationService.startRun(runId, {
        mode: 'generate',
      });
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.GetRunResults,
            runId,
          ),
        });
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(EvaluationApiAction.GetRun, runId),
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
      const { data } = await evaluationService.startRun(runId, {
        mode: 'generate',
        case_ids: [caseId],
      });
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.GetRunResults,
            runId,
          ),
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
      metric,
    }: {
      runId: string;
      caseId: string;
      metric: string;
    }) => {
      const { data } = await evaluationService.startRun(runId, {
        mode: 'score',
        case_ids: [caseId],
        metrics: [metric],
      });
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.GetRunResults,
            runId,
          ),
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
      const { data } = await evaluationService.startRun(runId, {
        mode: 'score',
      });
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.GetRunResults,
            runId,
          ),
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
      const { data } = await evaluationService.startRun(runId, {
        mode: 'score',
        case_ids: [caseId],
      });
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.GetRunResults,
            runId,
          ),
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
      const { data } = await evaluationService.clearCaseResult(runId, caseId);
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.GetRunResults,
            runId,
          ),
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
      metric,
    }: {
      runId: string;
      caseId: string;
      metric: string;
    }) => {
      const { data } = await evaluationService.clearCaseMetricResult(
        runId,
        caseId,
        metric,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.GetRunResults,
            runId,
          ),
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
      const { data } = await evaluationService.clearCaseAnswer(runId, caseId);
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: EvaluationKeys.action(
            EvaluationApiAction.GetRunResults,
            runId,
          ),
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

export const useExportEvaluationRun = () => {
  const { t } = useTranslation();
  const { runId } = useEvaluationUrl();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.ExportRun],
    onError: (error: Error) => message.error(error.message),
    mutationFn: async (caseIds?: string[]) => {
      const { data } = await evaluationService.exportRun(runId, caseIds);
      if (data) {
        const blob = await evaluationExportBlob(data);

        downloadFileFromBlob(blob);

        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, exportEvaluationRun: mutateAsync };
};

//#endregion
