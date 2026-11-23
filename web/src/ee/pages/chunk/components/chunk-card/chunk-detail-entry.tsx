import { Button } from '@/components/ui/button';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import { Spin } from '@/components/ui/spin';
import { useFetchChunkVersions } from '@/ee/hooks/use-chunk-version';
import type { IChunk } from '@/interfaces/database/dataset';
import { formatDate } from '@/utils/date';
import { Info } from 'lucide-react';
import { MouseEvent, ReactNode, useCallback } from 'react';
import { useTranslation } from 'react-i18next';

export interface ChunkDetailEntryProps {
  chunk: IChunk;
}

// A row is a fixed-width label over its value, the same shape the version
// preview uses, so the values line up instead of drifting with each label.
function DetailRow({
  label,
  children,
}: {
  label: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex items-start gap-2 text-sm">
      <span className="w-16 shrink-0 text-text-secondary">{label}</span>
      <span className="min-w-0 flex-1 break-all text-text-primary">
        {children}
      </span>
    </div>
  );
}

// Both instants are version create times: the parser's baseline, an edit, a
// restore and the switch each append a row, and create_time is when that state
// came to be. So "created" is the oldest row and "updated" the newest. A chunk
// with no history has no rows, and the times stay empty rather than being guessed
// from the document.
function ChunkDetailPanel({ chunk }: ChunkDetailEntryProps) {
  const { t } = useTranslation();
  const { versions, loading } = useFetchChunkVersions(
    chunk.chunk_id,
    chunk.doc_id,
  );

  // The server marks the head row, so the reported instant is the one the card
  // reports even if the list ever arrives in another order.
  const currentVersion = versions.find((item) => item.latest) ?? versions[0];
  const updatedAt = currentVersion?.create_time;
  // The list arrives newest first, so the last row is the first version.
  const createdAt = versions[versions.length - 1]?.create_time;

  return (
    <div className="space-y-2">
      <div className="text-sm font-medium text-text-primary">
        {t('chunk.detailTitle')}
      </div>
      <DetailRow label={t('chunk.detailId')}>
        <span className="font-mono text-xs">{chunk.chunk_id}</span>
      </DetailRow>
      {/* Only the instants wait for the request; the id and status are on the row. */}
      {loading ? (
        <div className="flex justify-center py-2">
          <Spin size="small" />
        </div>
      ) : (
        <>
          <DetailRow label={t('chunk.detailCreatedAt')}>
            {createdAt ? formatDate(createdAt) : '-'}
          </DetailRow>
          <DetailRow label={t('chunk.detailUpdatedAt')}>
            {updatedAt ? formatDate(updatedAt) : '-'}
          </DetailRow>
          <DetailRow label={t('chunk.detailVersion')}>
            {/* The number alone: the label already says it is the version. A
                chunk parsed before the history existed is at 0. */}
            <span className="tabular-nums">{chunk.content_version ?? 0}</span>
          </DetailRow>
        </>
      )}
      <DetailRow label={t('chunk.detailStatus')}>
        {Number(chunk.available_int) === 1
          ? t('chunk.enabled')
          : t('chunk.disabled')}
      </DetailRow>
    </div>
  );
}

/**
 * ChunkDetailEntry opens what the card cannot show on its face: when the chunk
 * was created, when it last changed, and the version the text is at. A popover
 * rather than the history sheet: this only reads the row the card already holds.
 */
export function ChunkDetailEntry({ chunk }: ChunkDetailEntryProps) {
  const { t } = useTranslation();

  // Opening the details must not double as selecting the chunk in the source
  // document, so the click stops at the trigger.
  const stopPropagation = useCallback((event: MouseEvent) => {
    event.stopPropagation();
  }, []);

  return (
    <div className="flex min-w-0 items-center gap-1">
      {/* The id lives beside the entry, so an edition that keeps no chunk history
          prints neither: its stub renders nothing in this slot. */}
      <span
        className="min-w-0 truncate font-mono text-text-secondary"
        title={chunk.chunk_id}
      >
        {chunk.chunk_id}
      </span>
      <Popover>
        {/* The trigger is sized to the card's corner strip, so it does not make that
            row taller than the type label beside it. */}
        <PopoverTrigger asChild>
          {/* The icon is the affordance; the label alone read as one more piece of
            metadata. Decoration only - the accessible name comes from the label. */}
          <Button
            variant="ghost"
            size="xs"
            className="h-5 shrink-0 gap-1 px-1 text-xs text-text-secondary"
            aria-label={t('chunk.detailTitle')}
            title={t('chunk.detailTitle')}
            onClick={stopPropagation}
          >
            <Info className="size-3" />
            <span>{t('chunk.detail')}</span>
          </Button>
        </PopoverTrigger>
        {/* The content mounts only when the popover opens, which is also what
          keeps the version request off the cards the list has not opened. */}
        <PopoverContent align="start" className="w-80">
          <ChunkDetailPanel chunk={chunk} />
        </PopoverContent>
      </Popover>
    </div>
  );
}

export default ChunkDetailEntry;
