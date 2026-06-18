import { UploadFormSchemaType } from '@/components/file-upload-dialog';
import message from '@/components/ui/message';
import { NewEvaluationRunId, RunningStatus } from '@/constants/evaluation';
import {
  EvaluationDetailList,
  IEvaluationCase,
  IEvaluationCollection,
  IEvaluationRecommendation,
  IEvaluationRun,
  IEvaluationRunResultData,
} from '@/interfaces/database/evaluation';
import {
  IEvaluationAddCaseQueryParams,
  IEvaluationCreateRunRequestBody,
  IEvaluationStartRunRequestBody,
  IEvaluationUpdateCaseRequestBody,
  IEvaluationUpdateCollectionRequestBody,
  IEvaluationUpdateRunRequestBody,
} from '@/interfaces/request/evaluation';
import evaluationService, {
  getListEvaluationDetailFile,
} from '@/services/evaluation-service';
import api from '@/utils/api';
import { downloadFileFromBlob } from '@/utils/file-util';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useDebounce } from 'ahooks';
import { get } from 'lodash';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router';
import { useSetModalState } from './common-hooks';
import {
  useGetPagination,
  useGetPaginationWithRouter,
  useHandleSearchChange,
} from './logic-hooks';
import { useEvaluationUrl } from './use-evaluation-url';

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
  FetchEvaluationDetailList = 'fetchEvaluationDetailList',

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
  GetRecommendations = 'getRecommendations',
  ExportRun = 'exportRun',
}

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

export const useUploadEvaluationFile = () => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.CreateCollection],
    mutationFn: async (params: { fileList: File[] }) => {
      const fileList = params.fileList;
      const formData = new FormData();
      fileList.forEach((file: any) => {
        formData.append('file', file);
      });
      try {
        const ret = await evaluationService.createCollection(formData);
        if (ret?.data.code === 0) {
          message.success(t('message.uploaded'));
          queryClient.invalidateQueries({
            queryKey: [EvaluationApiAction.ListCollection],
          });
        }
        return ret?.data?.code;
      } catch (error) {
        console.log('🚀 ~ useUploadEvaluationFile ~ error:', error);
      }
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
    mutationFn: async (collectionId: string | string[]) => {
      if (typeof collectionId === 'string') {
        collectionId = [collectionId];
      }
      const { data } = await evaluationService.deleteCollection(
        {
          url: api.evaluationDeleteCollection,
          data: { collection_ids: collectionId },
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

export const useFetchEvaluationFileContent = (id: string) => {
  const { pagination, changePagination } = useGetPagination();
  const { data, isFetching: loading } = useQuery<EvaluationDetailList>({
    queryKey: [
      EvaluationApiAction.FetchEvaluationDetailList,
      id,
      {
        ...pagination,
      },
    ],
    initialData: { cases: [], total: 0 },
    enabled: !!id,
    gcTime: 0,
    queryFn: async () => {
      const { data } = await getListEvaluationDetailFile(id as string, {
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
  const debouncedSearchString = useDebounce(searchString, { wait: 500 });
  const { id } = useParams();

  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<{ runs: IEvaluationRun[]; total: number }>({
    queryKey: [
      EvaluationApiAction.ListRun,
      {
        debouncedSearchString,
        // ...pagination,
        id,
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
            page_size: 100000,
            page: 1,
            target_id: id,
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
    queryKey: [EvaluationApiAction.GetRun, runId],
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
    queryKey: [EvaluationApiAction.GetRunResults, runId, pagination],
    gcTime: 0,
    initialData: {} as IEvaluationRunResultData,
    enabled: isSavedRun,
    refetchOnWindowFocus: false,
    refetchInterval: (query) => {
      // Continue polling when status is RUNNING, otherwise stop polling
      const data = query.state.data;
      return data?.run?.status === RunningStatus.RUNNING ? 5000 : false;
    },
    queryFn: async () => {
      const { data } = await evaluationService.getRunResults(
        {
          url: api.evaluationGetRunResults(runId),
          params: {
            page_size: pagination.pageSize,
            page: pagination.current,
          },
        },
        true,
      );

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
      const { data } = await evaluationService.startRun(
        {
          url: api.evaluationStartRun(runId!),
          data: params,
        },
        true,
      );
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRunResults],
        });
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.GetRun],
        });
        message.success(t('message.created'));
      }
      return data;
    },
  });

  return { data, loading, startEvaluationRun: mutateAsync };
};

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
      });
      if (data.code === 0) {
        setRunId(data.data?.run_id || '');
        queryClient.invalidateQueries({
          queryKey: [EvaluationApiAction.ListRun],
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
  const { type } = useEvaluationUrl();

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
          data: {
            name: get(params, 'config_snapshot.target.name', ''),
            target_type: type,
            ...params,
          },
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
          queryKey: [EvaluationApiAction.GetRun],
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
  const { runId } = useEvaluationUrl();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationApiAction.ExportRun],
    mutationFn: async () => {
      const { data } = await evaluationService.exportRun(
        {
          url: api.evaluationExportRun(runId),
          data: {},
          responseType: 'blob',
        },
        true,
      );
      if (data) {
        const blob = new Blob([data], {
          type: data.type,
        });

        downloadFileFromBlob(blob);

        message.success(t('message.operated'));
      }
      return data;
    },
  });

  return { data, loading, exportEvaluationRun: mutateAsync };
};

//#endregion
