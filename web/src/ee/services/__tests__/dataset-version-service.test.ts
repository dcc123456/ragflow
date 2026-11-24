/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

import {
  deleteDatasetVersion,
  listDatasetVersions,
  restoreDatasetVersion,
} from '@/ee/services/dataset-version-service';
import request from '@/utils/next-request';

jest.mock('@/utils/next-request', () => ({
  __esModule: true,
  default: { get: jest.fn(), post: jest.fn(), delete: jest.fn() },
}));

const mockedRequest = request as unknown as {
  get: jest.Mock;
  post: jest.Mock;
  delete: jest.Mock;
};

describe('dataset version service', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('omits the query string when the list is unpaged and unfiltered', async () => {
    mockedRequest.get.mockResolvedValue({
      data: { code: 0, data: { versions: [] } },
    });

    await listDatasetVersions({ dataset_id: 'ds-1' });

    expect(mockedRequest.get).toHaveBeenCalledWith(
      '/api/v1/datasets/ds-1/versions',
    );
  });

  it('passes the kind filter as one comma separated value', async () => {
    mockedRequest.get.mockResolvedValue({
      data: { code: 0, data: { versions: [] } },
    });

    await listDatasetVersions({
      dataset_id: 'ds-1',
      kinds: ['dataset_config', 'restore'],
      limit: 20,
    });

    expect(mockedRequest.get).toHaveBeenCalledWith(
      '/api/v1/datasets/ds-1/versions?kind=dataset_config%2Crestore&limit=20',
    );
  });

  it('pins a restore to the version the caller read', async () => {
    mockedRequest.post.mockResolvedValue({
      data: {
        code: 0,
        data: { version: 5, pre_version: 4, applied: true },
      },
    });

    await restoreDatasetVersion({
      dataset_id: 'ds-1',
      version: 3,
      expected_version: 4,
    });

    // expected_version is what stops a restore from overwriting an edit made
    // after the history was read.
    expect(mockedRequest.post).toHaveBeenCalledWith(
      '/api/v1/datasets/ds-1/versions/3/restore',
      { expected_version: 4, dry_run: undefined },
    );
  });

  it('deletes one version through the version resource', async () => {
    mockedRequest.delete.mockResolvedValue({
      data: {
        code: 0,
        data: { dataset_id: 'ds-1', deleted_version: 3, current_version: 5 },
      },
    });

    await deleteDatasetVersion({ dataset_id: 'ds-1', version: 3 });

    // DELETE on the same resource the detail reads: one version, not the history.
    expect(mockedRequest.delete).toHaveBeenCalledWith(
      '/api/v1/datasets/ds-1/versions/3',
    );
  });
});
