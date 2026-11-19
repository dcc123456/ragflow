import {
  IDatasetDeleteResult,
  IDatasetRestoreResult,
  IDatasetVersionDetail,
  IDatasetVersionList,
} from '@/ee/interfaces/dataset-version';
import api from '@/ee/utils/private-api';
import { ResponseType } from '@/interfaces/database/base';
import request from '@/utils/next-request';

// IDatasetVersionTarget names the dataset a version request addresses. version
// is required by the detail and restore calls only.
export interface IDatasetVersionTarget {
  dataset_id: string;
  version?: number;
}

// `request` is configured with the app's response shape, so a call resolves to
// { data, response } and the API body lives on `.data`.
export const listDatasetVersions = async (params: {
  dataset_id: string;
  before_version?: number;
  kinds?: string[];
  limit?: number;
}) => {
  const { data } = await request.get<ResponseType<IDatasetVersionList>>(
    api.datasetVersions(params.dataset_id, {
      before_version: params.before_version,
      kind: params.kinds?.length ? params.kinds.join(',') : undefined,
      limit: params.limit,
    }),
  );
  return data;
};

export const getDatasetVersion = async (target: IDatasetVersionTarget) => {
  const { data } = await request.get<ResponseType<IDatasetVersionDetail>>(
    api.datasetVersion(target.dataset_id, target.version ?? 0),
  );
  return data;
};

// restoreDatasetVersion writes a stored configuration back onto the live
// dataset. expected_version pins the version the caller read; the server rejects
// the restore when the dataset has moved on since. dry_run answers with the
// changes a restore would apply without writing anything.
export const restoreDatasetVersion = async (
  target: IDatasetVersionTarget & {
    expected_version?: number;
    dry_run?: boolean;
  },
) => {
  const { data } = await request.post<ResponseType<IDatasetRestoreResult>>(
    api.restoreDatasetVersion(target.dataset_id, target.version ?? 0),
    {
      expected_version: target.expected_version,
      dry_run: target.dry_run,
    },
  );
  return data;
};

// deleteDatasetVersion drops one version from the history. Deleting history does
// not rewrite the dataset: the remaining versions keep their numbers. The server
// refuses the current version and the baseline.
export const deleteDatasetVersion = async (
  target: IDatasetVersionTarget & { version: number },
) => {
  const { data } = await request.delete<ResponseType<IDatasetDeleteResult>>(
    api.datasetVersion(target.dataset_id, target.version),
  );
  return data;
};
