import message from '@/components/ui/message';
import {
  IDatasetVersion,
  IDatasetVersionDetail,
} from '@/ee/interfaces/dataset-version';
import {
  IDatasetVersionTarget,
  deleteDatasetVersion,
  getDatasetVersion,
  listDatasetVersions,
  restoreDatasetVersion,
} from '@/ee/services/dataset-version-service';
import { DatasetPipelineConfigurationKeys } from '@/hooks/use-knowledge-request';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

// The history belongs to the dataset the settings page shows, so the id is
// passed in rather than read from the route: the same page also renders for a
// dataset opened from a document view.
export interface FetchDatasetVersionsOptions {
  // Restricts the list to these kinds. Empty means every kind, which is what
  // the versions page opens on: configuration and content share one timeline.
  kinds?: string[];
  // limit is how many rows to read. The page grows it as the reader scrolls
  // instead of paging by cursor, so the list stays one array.
  limit?: number;
}

export const useFetchDatasetVersions = (
  datasetId?: string,
  options: FetchDatasetVersionsOptions = {},
): {
  versions: IDatasetVersion[];
  currentVersion: number;
  loading: boolean;
  refetch: () => Promise<{
    data?: { code: number; data?: { current_version?: number } };
  }>;
} => {
  const { kinds, limit } = options;
  // A stable key for the filter: the caller may pass a fresh array each render.
  const kindKey = kinds?.length ? kinds.join(',') : '';
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery({
    queryKey: ['fetchDatasetVersions', datasetId, kindKey, limit],
    enabled: !!datasetId,
    queryFn: () =>
      listDatasetVersions({
        dataset_id: datasetId as string,
        kinds: kinds?.length ? kinds : undefined,
        limit,
      }),
  });

  return {
    versions: data?.code === 0 ? (data.data?.versions ?? []) : [],
    currentVersion: data?.code === 0 ? (data.data?.current_version ?? 0) : 0,
    loading,
    // Reading the head again right before a write keeps it from being rejected
    // as stale: the page can sit open while uploads, parses or deletes move the
    // dataset on.
    refetch,
  };
};

export const useFetchDatasetVersion = (
  datasetId?: string,
  version?: number,
): {
  detail?: IDatasetVersionDetail;
  loading: boolean;
  missingVersion: boolean;
} => {
  const {
    data,
    isFetching: loading,
    error,
  } = useQuery({
    queryKey: ['fetchDatasetVersion', datasetId, version],
    enabled: !!datasetId && version !== undefined,
    queryFn: () =>
      getDatasetVersion({ dataset_id: datasetId as string, version }),
  });

  const status = (
    error as { response?: { status?: number } } | null | undefined
  )?.response?.status;

  return {
    detail: data?.code === 0 ? data.data : undefined,
    loading,
    // A version that the history no longer holds (re-parsed dataset) is an
    // expected answer, not an unhandled failure.
    missingVersion: status === 404,
  };
};

// useRestoreDatasetVersion writes a stored configuration back onto the dataset.
// The server appends the restored state as a new version, so the history and the
// settings form both refetch.
export const useRestoreDatasetVersion = () => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { isPending: loading, mutateAsync } = useMutation({
    mutationKey: ['restoreDatasetVersion'],
    mutationFn: async (
      target: IDatasetVersionTarget & { expected_version?: number },
    ) => {
      const data = await restoreDatasetVersion(target);
      if (data.code === 0) {
        message.success(t('datasetVersion.restored'));
        queryClient.invalidateQueries({ queryKey: ['fetchDatasetVersions'] });
        queryClient.invalidateQueries({ queryKey: ['fetchDatasetVersion'] });
        queryClient.invalidateQueries({
          queryKey: DatasetPipelineConfigurationKeys.detail(target.dataset_id),
        });
      }
      return data;
    },
  });

  return { restoreDatasetVersion: mutateAsync, loading };
};

// useCheckDatasetVersionRestore asks the server whether a restore could run
// right now, without writing anything. A dataset whose documents are queued or
// parsing answers with blocked_reason: the run writes the same chunks the
// restore would rebuild, so the panel refuses the action before the user
// commits to it.
export const useCheckDatasetVersionRestore = () => {
  const { isPending: loading, mutateAsync } = useMutation({
    mutationKey: ['checkDatasetVersionRestore'],
    mutationFn: async (target: IDatasetVersionTarget) =>
      restoreDatasetVersion({ ...target, dry_run: true }),
  });

  return { checkRestore: mutateAsync, loading };
};

// useDeleteDatasetVersion drops one version from the history. Rows keep their
// numbers, so the list refetches and shows the gap; the dataset itself is not
// rewritten. A refusal (current version, baseline) arrives as a coded error and
// is surfaced by the request layer.
export const useDeleteDatasetVersion = () => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { isPending: loading, mutateAsync } = useMutation({
    mutationKey: ['deleteDatasetVersion'],
    mutationFn: async (target: IDatasetVersionTarget & { version: number }) => {
      const data = await deleteDatasetVersion(target);
      if (data.code === 0) {
        message.success(t('datasetVersion.deleted'));
        // Awaited: the panel clears its selection as soon as this resolves, and
        // the auto-select effect then takes the first row it can see. Resolving
        // before the refresh lands would hand it the row that was just deleted.
        await queryClient.invalidateQueries({
          queryKey: ['fetchDatasetVersions'],
        });
        // The detail queries are deliberately left alone. The one that matters is
        // the row that just went away, and its re-read can only answer 404; the
        // panel clears its selection as soon as this resolves, so nothing is left
        // pointing at it. No other version's detail changes: a delete moves the
        // document set's pointer, not the set itself.
      }
      return data;
    },
  });

  return { deleteDatasetVersion: mutateAsync, loading };
};
