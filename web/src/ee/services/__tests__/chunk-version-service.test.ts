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
  deleteChunkVersion,
  getChunkVersionImageUrl,
} from '@/ee/services/chunk-version-service';
import request from '@/utils/next-request';

jest.mock('@/utils/next-request', () => ({
  __esModule: true,
  default: { get: jest.fn(), post: jest.fn(), delete: jest.fn() },
}));

const mockedRequest = request as unknown as { delete: jest.Mock };

describe('chunk version service', () => {
  it('addresses the version image route rather than the document image route', () => {
    const url = getChunkVersionImageUrl({
      kb_id: 'kb-1',
      doc_id: 'doc-1',
      chunk_id: 'chunk-1',
      version: 3,
    });

    // An old picture is no longer referenced by the live chunk, so the live
    // document route cannot authorize it.
    expect(url).toContain(
      '/api/v1/datasets/kb-1/documents/doc-1/chunks/chunk-1/versions/3/image',
    );
    expect(url).not.toContain('/documents/doc-1/images/');
  });

  it('defaults a target without a version to version 0', () => {
    const url = getChunkVersionImageUrl({
      kb_id: 'kb-1',
      doc_id: 'doc-1',
      chunk_id: 'chunk-1',
    });

    expect(url).toContain('/versions/0/image');
  });

  it('deletes one version through the version resource', async () => {
    mockedRequest.delete.mockResolvedValue({
      data: {
        code: 0,
        data: { chunk_id: 'chunk-1', deleted_version: 1, latest_version: 3 },
      },
    });

    await deleteChunkVersion({
      kb_id: 'kb-1',
      doc_id: 'doc-1',
      chunk_id: 'chunk-1',
      version: 1,
    });

    // DELETE on the same resource the detail reads: one version, not the history.
    expect(mockedRequest.delete).toHaveBeenCalledWith(
      '/api/v1/datasets/kb-1/documents/doc-1/chunks/chunk-1/versions/1',
      {},
    );
  });

  it('forwards the option that lets the caller render the refusal itself', async () => {
    mockedRequest.delete.mockResolvedValue({ data: { code: 0 } });

    await deleteChunkVersion(
      { kb_id: 'kb-1', doc_id: 'doc-1', chunk_id: 'chunk-1', version: 1 },
      { skipGlobalErrorNotification: true },
    );

    // The panel explains a refusal in a dialog, so the request layer must not
    // also raise its generic "Request error 409" toast for the same answer.
    expect(mockedRequest.delete).toHaveBeenCalledWith(
      '/api/v1/datasets/kb-1/documents/doc-1/chunks/chunk-1/versions/1',
      { skipGlobalErrorNotification: true },
    );
  });
});
