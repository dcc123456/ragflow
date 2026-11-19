import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { ClipboardClock } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import ChunkVersionHistory from '../chunk-version-history';

interface ChunkVersionEntryProps {
  chunkId: string;
  docId: string;
}

// ChunkVersionEntry is the chunk card's history button: it opens the edit history
// of the chunk over the list the card belongs to. The card only offers the icon,
// because it shares a column with the edit and delete buttons; the editor does not
// carry a history entry of its own. Upstream ships the same-path file returning
// null.
export function ChunkVersionEntry({ chunkId, docId }: ChunkVersionEntryProps) {
  const { t } = useTranslation();

  return (
    <Sheet modal={false}>
      {/* The entry is mounted by the shared chunk card, which the tests and any
          host that renders the card on its own mount without the app shell, so
          the provider travels with the entry instead of being assumed. */}
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <SheetTrigger asChild>
              {/* icon-xs keeps the button the same weight as the edit and delete
                  buttons in the column it shares with them. */}
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={t('chunk.versionHistory')}
                title={t('chunk.versionHistory')}
              >
                <ClipboardClock className="size-4" />
              </Button>
            </SheetTrigger>
          </TooltipTrigger>
          <TooltipContent>{t('chunk.versionHistoryTip')}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <SheetContent className="flex w-[720px] max-w-[92vw] flex-col">
        <SheetHeader>
          <SheetTitle>{t('chunk.versionHistory')}</SheetTitle>
        </SheetHeader>
        <ChunkVersionHistory chunkId={chunkId} docId={docId} />
      </SheetContent>
    </Sheet>
  );
}

export default ChunkVersionEntry;
