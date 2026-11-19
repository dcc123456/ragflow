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
  useDeleteDatasetVersion,
  useFetchDatasetVersion,
  useFetchDatasetVersions,
} from '@/ee/hooks/use-dataset-version';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';

const mockList = jest.fn();
const mockDetail = jest.fn();
const mockDelete = jest.fn();

jest.mock('@/ee/services/dataset-version-service', () => ({
  __esModule: true,
  listDatasetVersions: (...args: unknown[]) => mockList(...args),
  getDatasetVersion: (...args: unknown[]) => mockDetail(...args),
  diffDatasetVersions: jest.fn(),
  restoreDatasetVersion: jest.fn(),
  deleteDatasetVersion: (...args: unknown[]) => mockDelete(...args),
}));

jest.mock('@/components/ui/message', () => ({
  __esModule: true,
  default: { success: jest.fn(), error: jest.fn(), warning: jest.fn() },
}));

const wrapper = ({ children }: { children: React.ReactNode }) => (
  <QueryClientProvider
    client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
  >
    {children}
  </QueryClientProvider>
);

describe('useFetchDatasetVersions', () => {
  beforeEach(() => {
    mockList.mockReset();
    mockList.mockResolvedValue({
      code: 0,
      data: { versions: [{ version: 1 }], current_version: 1 },
    });
  });

  // The versions page filters the timeline by kind and grows the page size as
  // the reader asks for more; both have to reach the request, or the page shows
  // the wrong slice without saying so.
  it('forwards the kind filter and the limit', async () => {
    const { result } = renderHook(
      () =>
        useFetchDatasetVersions('kb-1', {
          kinds: ['upload', 'delete'],
          limit: 100,
        }),
      { wrapper },
    );

    await waitFor(() => expect(result.current.versions).toHaveLength(1));
    expect(mockList).toHaveBeenCalledWith({
      dataset_id: 'kb-1',
      kinds: ['upload', 'delete'],
      limit: 100,
    });
  });

  it('leaves the filter out when every kind is wanted', async () => {
    const { result } = renderHook(() => useFetchDatasetVersions('kb-1'), {
      wrapper,
    });

    await waitFor(() => expect(result.current.currentVersion).toBe(1));
    expect(mockList).toHaveBeenCalledWith({
      dataset_id: 'kb-1',
      kinds: undefined,
      limit: undefined,
    });
  });

  it('does not fetch without a dataset', () => {
    renderHook(() => useFetchDatasetVersions(undefined), { wrapper });
    expect(mockList).not.toHaveBeenCalled();
  });
});

describe('useDeleteDatasetVersion', () => {
  // One client per test, so an invalidation is observed instead of leaking into
  // the next case through a shared cache.
  let client = new QueryClient();

  const deleteWrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );

  beforeEach(() => {
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    mockDetail.mockReset();
    mockDetail.mockResolvedValue({
      code: 0,
      data: { version: 2, kind: 'parse' },
    });
    mockDelete.mockReset();
    mockDelete.mockResolvedValue({ code: 0, data: { deleted_version: 2 } });
  });

  // A delete removes the row the panel is showing. Re-reading that row answers
  // 404, and the request layer reports it as a failure of a write that succeeded;
  // the panel drops its selection instead, so the detail must be left alone.
  it('does not re-read the version it deleted', async () => {
    const { result } = renderHook(
      () => ({
        detail: useFetchDatasetVersion('kb-1', 2),
        remove: useDeleteDatasetVersion(),
      }),
      { wrapper: deleteWrapper },
    );

    await waitFor(() => expect(result.current.detail.detail).toBeDefined());
    expect(mockDetail).toHaveBeenCalledTimes(1);

    await act(async () => {
      await result.current.remove.deleteDatasetVersion({
        dataset_id: 'kb-1',
        version: 2,
      });
    });

    // Give an invalidation the time to trigger a refetch, so its absence is what
    // the assertion below records rather than a race.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20));
    });

    expect(mockDelete).toHaveBeenCalledWith({
      dataset_id: 'kb-1',
      version: 2,
    });
    expect(mockDetail).toHaveBeenCalledTimes(1);
  });

  // The panel clears its selection as soon as the delete resolves, and the
  // auto-select effect then picks the first row it can see. If the refresh is
  // still in flight at that moment the first row is the version that was just
  // deleted, so the write has to resolve only once the list is current.
  it('resolves only after the list has been refreshed', async () => {
    mockList.mockReset();
    mockList.mockResolvedValueOnce({
      code: 0,
      data: { versions: [{ version: 3 }, { version: 2 }], current_version: 3 },
    });
    // The refresh is held open, so the moment the delete resolves is observable.
    let releaseList = () => {};
    const refresh = new Promise((resolve) => {
      releaseList = () =>
        resolve({
          code: 0,
          data: { versions: [{ version: 3 }], current_version: 3 },
        });
    });
    mockList.mockReturnValueOnce(refresh);

    const { result } = renderHook(
      () => ({
        list: useFetchDatasetVersions('kb-1'),
        remove: useDeleteDatasetVersion(),
      }),
      { wrapper: deleteWrapper },
    );

    await waitFor(() => expect(result.current.list.versions).toHaveLength(2));

    let resolved = false;
    const deletion = result.current.remove
      .deleteDatasetVersion({ dataset_id: 'kb-1', version: 2 })
      .then(() => {
        resolved = true;
      });

    await act(async () => {
      await Promise.resolve();
    });
    expect(resolved).toBe(false);

    await act(async () => {
      releaseList();
      await deletion;
    });

    expect(resolved).toBe(true);
    // The refreshed rows land in the render that follows, so the ordering is what
    // is asserted above; this checks the data the panel will then read.
    await waitFor(() =>
      expect(result.current.list.versions.map((row) => row.version)).toEqual([
        3,
      ]),
    );
  });
});
