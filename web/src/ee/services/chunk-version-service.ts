import {
  IChunkVersionDeleteResult,
  IChunkVersionDetail,
  IChunkVersionDiff,
  IChunkVersionList,
} from '@/ee/interfaces/chunk-version';
import api from '@/ee/utils/private-api';
import { ResponseType } from '@/interfaces/database/base';
import request from '@/utils/next-request';
import { AxiosRequestConfig } from 'axios';

const {
  chunkVersions,
  chunkVersion,
  chunkVersionDiff,
  restoreChunkVersion: restoreChunkVersionEndpoint,
  chunkVersionImage,
} = api;

// IChunkVersionTarget names the chunk a version request addresses. version is
// required by the detail and restore calls only.
export interface IChunkVersionTarget {
  kb_id: string;
  doc_id: string;
  chunk_id: string;
  version?: number;
}

// `request` is configured with getResponse:true, so a call resolves to
// { data, response } and the API body lives on `.data`. These helpers unwrap it
// because every caller reads `code`/`data` off the returned value.
export const listChunkVersions = async (target: IChunkVersionTarget) => {
  const { data } = await request.get<ResponseType<IChunkVersionList>>(
    chunkVersions(target.kb_id, target.doc_id, target.chunk_id),
  );
  return data;
};

export const getChunkVersion = async (target: IChunkVersionTarget) => {
  const { data } = await request.get<ResponseType<IChunkVersionDetail>>(
    chunkVersion(
      target.kb_id,
      target.doc_id,
      target.chunk_id,
      target.version ?? 0,
    ),
  );
  return data;
};

// getChunkVersionDiff compares two versions of one chunk. `to` is optional and
// defaults to the latest version on the server.
export const getChunkVersionDiff = async (
  target: IChunkVersionTarget & { from: number; to?: number },
) => {
  const { data } = await request.get<ResponseType<IChunkVersionDiff>>(
    chunkVersionDiff(
      target.kb_id,
      target.doc_id,
      target.chunk_id,
      target.from,
      target.to,
    ),
  );
  return data;
};

export const restoreChunkVersion = async (target: IChunkVersionTarget) => {
  const { data } = await request.post<ResponseType<{ version: number }>>(
    restoreChunkVersionEndpoint(
      target.kb_id,
      target.doc_id,
      target.chunk_id,
      target.version ?? 0,
    ),
  );
  return data;
};

// getChunkVersionImageUrl returns the URL of the picture a stored version
// recorded. It is not fetched here: the endpoint needs the session's auth
// header, which AuthenticatedImg attaches when it renders. A version's picture
// never changes (an edit that swaps the image opens a new version), so the URL
// needs no cache-busting parameter.
export const getChunkVersionImageUrl = (target: IChunkVersionTarget) =>
  chunkVersionImage(
    target.kb_id,
    target.doc_id,
    target.chunk_id,
    target.version ?? 0,
  );

// ChunkVersionDeleteOptions is what the delete call forwards to the request
// layer. The panel renders the refusal itself (a dialog naming the reason), so
// the generic toast would be a second, vaguer copy of the same error.
export interface ChunkVersionDeleteOptions {
  skipGlobalErrorNotification?: boolean;
}

// deleteChunkVersion drops one version from the history. Deleting history does
// not rewrite the chunk: the remaining versions keep their numbers. The server
// refuses the latest version, the parser's baseline, a delete marker, and any
// row a dataset version still restores from.
export const deleteChunkVersion = async (
  target: IChunkVersionTarget & { version: number },
  options: ChunkVersionDeleteOptions = {},
) => {
  const { data } = await request.delete<
    ResponseType<IChunkVersionDeleteResult>
  >(
    chunkVersion(target.kb_id, target.doc_id, target.chunk_id, target.version),
    // The request layer reads this flag off the axios config; axios' own type
    // has no idea about it, so the cast is what carries the seam through.
    options as AxiosRequestConfig,
  );
  return data;
};
