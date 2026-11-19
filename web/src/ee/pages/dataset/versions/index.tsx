import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import DatasetVersionHistory from '@/ee/pages/dataset/versions/components/dataset-version-history';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router';

// The history is one timeline: a configuration change and a parse of the same
// minute sit next to each other, and a reader restoring a state cares about
// both. The filter narrows that timeline rather than splitting it into tabs, so
// the version numbers stay continuous.
const KIND_FILTERS: Record<string, string[]> = {
  all: [],
  configuration: ['baseline', 'dataset_config', 'restore'],
  // Every kind that is about a document as a whole, rather than the dataset's
  // own configuration: it appeared or left (upload, delete), it was re-parsed
  // (parse - the kind reads "Document parsed", and a parse is a milestone of the
  // document, not of one chunk), or its name, parse settings or metadata moved.
  documents: [
    'upload',
    'delete',
    'parse',
    'document_config',
    'document_rename',
    'document_meta',
    // A document switched on or off: the chunks follow the document, so the
    // version belongs to the document bucket rather than the chunk one.
    // `document_status` is the unnamed kind: rows written before the two below
    // existed, and a switch to a status that is neither enabled nor disabled.
    'document_status',
    'document_enable',
    'document_disable',
  ],
  // A compiled wiki page edited by hand. It gets its own bucket rather than a
  // place under documents: a page is an artifact, not a document - it has no
  // document row - and folding it in would make "Documents only" list something
  // the documents table never shows.
  pages: ['artifact_edit'],
  // One chunk of a document changed by hand. A parse touches chunks too, but it
  // is the document that was parsed: it belongs to the documents bucket, and
  // listing it here as well would show the same version under two filters.
  // `chunk` is the unnamed kind: rows written before the four below existed, and
  // a batch that mixed actions.
  chunks: [
    'chunk',
    'chunk_add',
    'chunk_update',
    'chunk_delete',
    'chunk_restore',
    'chunk_enable',
    'chunk_disable',
  ],
};

const FILTER_LABELS: Record<string, string> = {
  all: 'datasetVersion.filterAll',
  configuration: 'datasetVersion.filterConfiguration',
  documents: 'datasetVersion.filterDocuments',
  pages: 'datasetVersion.filterPages',
  chunks: 'datasetVersion.filterChunks',
};

/**
 * DatasetVersionsPage is the full-page view of a dataset's history: the
 * timeline of every configuration, document and chunk change (preview, field
 * diff, change set, restore).
 */
export default function DatasetVersionsPage() {
  const { id } = useParams();
  const { t } = useTranslation();
  const [filter, setFilter] = useState<string>('all');

  const kinds = useMemo(() => KIND_FILTERS[filter] ?? [], [filter]);

  // Paging belongs to the list, not the page: the list scrolls, so it is the one
  // that knows when the reader reached the end. A filter change is handled there
  // too, since it is the list's page that starts over.
  const handleFilterChange = useCallback((next: string) => {
    setFilter(next);
  }, []);

  return (
    // h-full only works if every layout level above hands down a height, and
    // here they do not: the page sized itself to the detail pane's content, so
    // the panes stopped half way down the page. The app root is the one height
    // that is guaranteed (html/body/#root are 100dvh), so the page floors itself
    // at the viewport minus the chrome above it.
    //
    // It is an inline style rather than a utility class on purpose: an arbitrary
    // Tailwind value has to be generated at build time, and a wrong guess about
    // the syntax silently produces nothing. This declaration cannot be missed,
    // and being a minimum it is still smaller than the space a working height
    // chain hands down, so it never causes overflow.
    <div
      className="flex h-full flex-col gap-3 p-4"
      style={{ minHeight: 'calc(100dvh - 140px)' }}
    >
      {/* Title stacked over its description, as on the retrieval testing page:
          the sentence says what the timeline is, so it is read as part of the
          heading rather than folded into a hover tooltip. */}
      <header className="flex min-w-0 flex-col gap-1">
        <h1 className="text-xl font-bold text-text-primary">
          {t('datasetVersion.pageTitle')}
        </h1>
        <p className="min-w-0 text-sm text-text-secondary">
          {t('datasetVersion.pageDescription')}
        </p>
      </header>

      <div className="flex min-h-0 flex-1 flex-col gap-2">
        <div className="flex flex-wrap items-center justify-end gap-2">
          <Select value={filter} onValueChange={handleFilterChange}>
            <SelectTrigger className="h-8 w-[168px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {Object.keys(KIND_FILTERS).map((key) => (
                <SelectItem key={key} value={key}>
                  {t(FILTER_LABELS[key])}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {id ? (
          <DatasetVersionHistory
            datasetId={id}
            kinds={kinds}
            className="min-h-0 flex-1"
            // The list keeps a fixed column here: it holds four short columns,
            // and a share of a wide page stretched them into empty space. 600px
            // covers the fixed widths (80 + 168 + 112), the compacted cell
            // padding and the ~150px the longest change label needs.
            listClassName="w-[600px]"
          />
        ) : null}
      </div>
    </div>
  );
}
