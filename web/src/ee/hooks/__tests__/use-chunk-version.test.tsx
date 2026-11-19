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
  useFetchChunkVersionDiff,
  useFetchChunkVersions,
  useRestoreChunkVersion,
} from '@/ee/hooks/use-chunk-version';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';

const mockList = jest.fn();
const mockDiff = jest.fn();
const mockRestore = jest.fn();

jest.mock('@/ee/services/chunk-version-service', () => ({
  __esModule: true,
  listChunkVersions: (...args: unknown[]) => mockList(...args),
  getChunkVersion: jest.fn(),
  getChunkVersionDiff: (...args: unknown[]) => mockDiff(...args),
  restoreChunkVersion: (...args: unknown[]) => mockRestore(...args),
}));

jest.mock('@/components/ui/message', () => ({
  __esModule: true,
  default: { success: jest.fn(), error: jest.fn(), warning: jest.fn() },
}));

// route-hook reads react-router's search params, which jsdom has no router for.
jest.mock('@/hooks/route-hook', () => ({
  useGetKnowledgeSearchParams: () => ({
    type: '',
    documentId: 'doc-1',
    knowledgeId: 'kb-1',
  }),
}));

// route-hook (imported by the hook chain) pulls in the app shell — routes
// constructs a browser router at module scope, which jsdom cannot survive.
jest.mock('@/routes', () => ({ Routes: {} }));

function renderWithQueryClient<T>(hook: () => T) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  // `any` on purpose: files with jest.mock go through esbuild-jest's babel
  // pass, which cannot strip imported bindings used as type annotations.
  function Wrapper({ children }: { children?: any }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  }
  return { ...renderHook(hook, { wrapper: Wrapper }), queryClient };
}

describe('chunk version hooks', () => {
  beforeEach(() => {
    mockList.mockReset();
    mockDiff.mockReset();
    mockRestore.mockReset();
  });

  it('lists a chunk history newest first through the dataset route', async () => {
    mockList.mockResolvedValue({
      code: 0,
      message: 'success',
      status: 200,
      data: {
        chunk_id: 'chunk-1',
        total: 2,
        versions: [
          { version: 1, latest: true, restored_from: 0, created_by: 'user-1' },
          { version: 0, latest: false, restored_from: 0, created_by: 'user-1' },
        ],
      },
    });

    const { result } = renderWithQueryClient(() =>
      useFetchChunkVersions('chunk-1', 'doc-1'),
    );

    await waitFor(() => expect(result.current.versions).toHaveLength(2));
    expect(mockList).toHaveBeenCalledWith({
      kb_id: 'kb-1',
      doc_id: 'doc-1',
      chunk_id: 'chunk-1',
    });
    expect(result.current.versions[0].latest).toBe(true);
  });

  it('reports an empty history when the request fails', async () => {
    mockList.mockResolvedValue({ code: 500, message: 'boom', status: 500 });

    const { result } = renderWithQueryClient(() =>
      useFetchChunkVersions('chunk-1', 'doc-1'),
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.versions).toEqual([]);
  });

  it('compares two versions through the diff route', async () => {
    mockDiff.mockResolvedValue({
      code: 0,
      message: 'success',
      status: 200,
      data: {
        chunk_id: 'chunk-1',
        from: 0,
        to: 1,
        identical: false,
        fields: [
          {
            field: 'content_with_weight',
            kind: 'text',
            diff: '@@ -1,1 +1,1 @@\n-v0 body\n+v1 body\n',
          },
        ],
      },
    });

    const { result } = renderWithQueryClient(() =>
      useFetchChunkVersionDiff('chunk-1', 'doc-1', 0, 1),
    );

    await waitFor(() => expect(result.current.diff?.fields).toHaveLength(1));
    expect(mockDiff).toHaveBeenCalledWith({
      kb_id: 'kb-1',
      doc_id: 'doc-1',
      chunk_id: 'chunk-1',
      from: 0,
      to: 1,
    });
    expect(result.current.diff?.fields[0].kind).toBe('text');
  });

  it('reports a 404 as a missing version rather than an unhandled error', async () => {
    mockDiff.mockRejectedValue({ response: { status: 404 } });

    const { result } = renderWithQueryClient(() =>
      useFetchChunkVersionDiff('chunk-1', 'doc-1', 0, 1),
    );

    await waitFor(() => expect(result.current.missingVersion).toBe(true));
    expect(result.current.diff).toBeUndefined();
    expect(result.current.loading).toBe(false);
  });

  it('does not compare until a from version is chosen', async () => {
    const { result } = renderWithQueryClient(() =>
      useFetchChunkVersionDiff('chunk-1', 'doc-1'),
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockDiff).not.toHaveBeenCalled();
  });

  it('restores a version and refetches the chunk, its history and the list', async () => {
    mockRestore.mockResolvedValue({
      code: 0,
      message: 'success',
      status: 200,
      data: { version: 2 },
    });

    const { result, queryClient } = renderWithQueryClient(() =>
      useRestoreChunkVersion(),
    );
    // Pre-seed the queries the restore must invalidate.
    queryClient.setQueryData(['fetchChunk', 'kb-1', 'doc-1', 'chunk-1'], {});
    queryClient.setQueryData(
      ['fetchChunkVersions', 'kb-1', 'doc-1', 'chunk-1'],
      {},
    );
    queryClient.setQueryData(['fetchChunkList'], {});

    let code: number | undefined;
    await act(async () => {
      code = await result.current.restoreChunkVersion({
        kb_id: 'kb-1',
        doc_id: 'doc-1',
        chunk_id: 'chunk-1',
        version: 0,
      });
    });

    expect(code).toBe(0);
    expect(mockRestore).toHaveBeenCalledWith({
      kb_id: 'kb-1',
      doc_id: 'doc-1',
      chunk_id: 'chunk-1',
      version: 0,
    });
    expect(
      queryClient.getQueryState(['fetchChunk', 'kb-1', 'doc-1', 'chunk-1'])
        ?.isInvalidated,
    ).toBe(true);
    expect(
      queryClient.getQueryState([
        'fetchChunkVersions',
        'kb-1',
        'doc-1',
        'chunk-1',
      ])?.isInvalidated,
    ).toBe(true);
    expect(queryClient.getQueryState(['fetchChunkList'])?.isInvalidated).toBe(
      true,
    );
  });
});
