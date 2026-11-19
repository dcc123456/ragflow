import type { IChunk } from '@/interfaces/database/dataset';

export interface ChunkVersionBadgeProps {
  chunk: IChunk;
}

/**
 * Enterprise seam for the chunk card's version badge: the parser's output is v0
 * and every edit bumps it, so the badge labels the version the text shown is at.
 * Upstream ships the same-path file returning null - the community edition keeps
 * no chunk history, so its chunks have no version to label.
 */
export function ChunkVersionBadge({ chunk }: ChunkVersionBadgeProps) {
  return (
    <span className="py-1 pr-2 font-medium text-text-secondary">
      v{chunk.content_version ?? 0}
    </span>
  );
}

export default ChunkVersionBadge;
