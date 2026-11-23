import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useMemo } from 'react';

export interface ChunkHistoryCache {
  /** Call after a chunk edit wrote a version. */
  invalidateAfterEdit: () => void;
  /** Call after a chunk switch opened a version of its own. */
  invalidateAfterSwitch: () => void;
}

/**
 * Enterprise seam for the chunk caches the shared requests keep in step. An edit
 * writes a version and a switch opens one of its own, so an open (or cached)
 * history panel, and the details the card opens beside it, would otherwise keep
 * the values they had before the request - both read the same version list.
 * Upstream ships the same-path file whose callbacks do nothing: the community
 * edition keeps no chunk history.
 */
export function useChunkHistoryCache(): ChunkHistoryCache {
  const queryClient = useQueryClient();

  const invalidateAfterEdit = useCallback(() => {
    // The edit wrote a version; the chunk list is refreshed by the caller.
    queryClient.invalidateQueries({ queryKey: ['fetchChunkVersions'] });
  }, [queryClient]);

  const invalidateAfterSwitch = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['fetchChunkVersions'] });
    queryClient.invalidateQueries({ queryKey: ['fetchChunkList'] });
  }, [queryClient]);

  return useMemo(
    () => ({ invalidateAfterEdit, invalidateAfterSwitch }),
    [invalidateAfterEdit, invalidateAfterSwitch],
  );
}
