import {
  IChunkVersion,
  IChunkVersionDetail,
  IChunkVersionDiff,
} from '@/ee/interfaces/chunk-version';
import {
  IChunkVersionTarget,
  deleteChunkVersion,
  getChunkVersion,
  getChunkVersionDiff,
  listChunkVersions,
  restoreChunkVersion,
} from '@/ee/services/chunk-version-service';
import { useGetKnowledgeSearchParams } from '@/hooks/route-hook';
import message from '@/components/ui/message';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

// The history the hooks read belongs to the chunk named by the route, so the
// dataset id comes from the page params instead of the component props.
const useVersionTarget = (docId?: string, chunkId?: string) => {
  const { knowledgeId } = useGetKnowledgeSearchParams();
  return { kb_id: knowledgeId, doc_id: docId ?? '', chunk_id: chunkId ?? '' };
};

// useFetchChunkVersions lists a chunk's stored versions, newest first.
export const useFetchChunkVersions = (
  chunkId?: string,
  docId?: string,
): { versions: IChunkVersion[]; loading: boolean } => {
  const target = useVersionTarget(docId, chunkId);
  const { data, isFetching: loading } = useQuery({
    queryKey: ['fetchChunkVersions', target.kb_id, docId, chunkId],
    enabled: !!target.kb_id && !!docId && !!chunkId,
    queryFn: () => listChunkVersions(target),
  });

  return {
    versions: data?.code === 0 ? (data.data?.versions ?? []) : [],
    loading,
  };
};

export const useFetchChunkVersion = (
  chunkId?: string,
  docId?: string,
  version?: number,
): { detail?: IChunkVersionDetail; loading: boolean } => {
  const target = useVersionTarget(docId, chunkId);
  const { data, isFetching: loading } = useQuery({
    queryKey: ['fetchChunkVersion', target.kb_id, docId, chunkId, version],
    enabled: !!target.kb_id && !!docId && !!chunkId && version !== undefined,
    queryFn: () => getChunkVersion({ ...target, version }),
  });

  return {
    detail: data?.code === 0 ? data.data : undefined,
    loading,
  };
};

// useFetchChunkVersionDiff compares two versions of a chunk field by field.
// `to` is optional; the server compares against the latest version when it is
// omitted. missingVersion tells the panel that the 404 is the expected answer
// for a stale version number rather than an unhandled failure.
export const useFetchChunkVersionDiff = (
  chunkId?: string,
  docId?: string,
  from?: number,
  to?: number,
): { diff?: IChunkVersionDiff; loading: boolean; missingVersion: boolean } => {
  const target = useVersionTarget(docId, chunkId);
  const {
    data,
    isFetching: loading,
    error,
  } = useQuery({
    queryKey: ['fetchChunkVersionDiff', target.kb_id, docId, chunkId, from, to],
    enabled: !!target.kb_id && !!docId && !!chunkId && from !== undefined,
    queryFn: () => getChunkVersionDiff({ ...target, from: from as number, to }),
  });

  const status = (
    error as { response?: { status?: number } } | null | undefined
  )?.response?.status;

  return {
    diff: data?.code === 0 ? data.data : undefined,
    loading,
    missingVersion: status === 404,
  };
};

// useRestoreChunkVersion writes a stored version back onto the live chunk. The
// restore itself becomes a new version, so the history, the shared chunk detail
// (which the editor form resets from) and the chunk list all refetch.
export const useRestoreChunkVersion = () => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { isPending: loading, mutateAsync } = useMutation({
    mutationKey: ['restoreChunkVersion'],
    mutationFn: async (target: IChunkVersionTarget) => {
      const data = await restoreChunkVersion(target);
      if (data.code === 0) {
        message.success(t('chunk.versionRestored'));
        queryClient.invalidateQueries({ queryKey: ['fetchChunkVersions'] });
        queryClient.invalidateQueries({ queryKey: ['fetchChunk'] });
        queryClient.invalidateQueries({ queryKey: ['fetchChunkList'] });
      }
      return data?.code;
    },
  });

  return { restoreChunkVersion: mutateAsync, loading };
};

// ChunkVersionDeleteOutcome is what one delete attempt resolved to. The panel
// needs the reason and not only "it failed": a refusal is a fact about the row
// (the latest version, the parse baseline, a delete marker, or a state a
// dataset version still restores from) that the reader can act on by picking
// another version, so it is rendered as a dialog rather than a toast.
export interface ChunkVersionDeleteOutcome {
  code: number;
  message: string;
}

// deleteRefusal reads the coded error a refusal arrives as. The request layer
// rejects on a non-2xx status, so the code and the sentence live under
// response.data; a failure without a response (network) keeps -1 and an empty
// message, which the panel renders as a generic sentence.
const deleteRefusal = (error: unknown): ChunkVersionDeleteOutcome => {
  const response = (
    error as {
      response?: {
        status?: number;
        data?: { code?: number; message?: string };
      };
    }
  )?.response;
  const body = response?.data;
  return {
    code: typeof body?.code === 'number' ? body.code : (response?.status ?? -1),
    message: typeof body?.message === 'string' ? body.message : '',
  };
};

// useDeleteChunkVersion drops one version from a chunk's history. Rows keep
// their numbers, so the list refetches and shows the gap; the chunk itself is
// not rewritten, so only the history and a comparison that named the deleted
// version need refreshing. A refusal comes back as an outcome carrying the
// server's reason instead of a toast, so the caller can explain it in place.
export const useDeleteChunkVersion = () => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { isPending: loading, mutateAsync } = useMutation({
    mutationKey: ['deleteChunkVersion'],
    mutationFn: async (
      target: IChunkVersionTarget & { version: number },
    ): Promise<ChunkVersionDeleteOutcome> => {
      try {
        const data = await deleteChunkVersion(target, {
          skipGlobalErrorNotification: true,
        });
        if (data?.code === 0) {
          message.success(t('chunk.versionDeleted'));
          queryClient.invalidateQueries({ queryKey: ['fetchChunkVersions'] });
          queryClient.invalidateQueries({ queryKey: ['fetchChunkVersion'] });
          queryClient.invalidateQueries({
            queryKey: ['fetchChunkVersionDiff'],
          });
          return { code: 0, message: '' };
        }
        return { code: data?.code ?? -1, message: data?.message ?? '' };
      } catch (error) {
        return deleteRefusal(error);
      }
    },
  });

  return { deleteChunkVersion: mutateAsync, loading };
};
