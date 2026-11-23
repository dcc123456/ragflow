import { AuthenticatedImg } from '@/components/image';
import { Spin } from '@/components/ui/spin';
import TextDiff from '@/ee/components/text-diff';
import {
  IChunkVersionDiff,
  IChunkVersionFieldDiff,
} from '@/ee/interfaces/chunk-version';
import { getChunkVersionImageUrl } from '@/ee/services/chunk-version-service';
import { useGetKnowledgeSearchParams } from '@/hooks/route-hook';
import { useTranslation } from 'react-i18next';

// Field names the backend reports map to the same labels the version detail
// already uses, so a comparison reads like the preview it replaces. position_int
// is not among them: the backend does not compare coordinates.
const FIELD_LABELS: Record<string, string> = {
  content_with_weight: 'chunk.diffFieldContent',
  important_kwd: 'chunk.keyword',
  question_kwd: 'chunk.question',
  tag_feas: 'chunk.tags',
  doc_type_kwd: 'chunk.diffFieldType',
  img_id: 'chunk.diffFieldImage',
  available_int: 'chunk.diffFieldVisibility',
};

interface ChunkVersionDiffProps {
  diff?: IChunkVersionDiff;
  loading: boolean;
  // missingVersion renders the 404 as "this version does not exist" instead of
  // the "pick two versions" prompt, which would read as if nothing was chosen.
  missingVersion?: boolean;
  // deletedVersion is the comparison whose right-hand side is a removal marker.
  // Such a row holds no state to compare, so the pane reports the removal
  // instead of diffing a chunk against nothing.
  deletedVersion?: boolean;
  // The chunk the pair belongs to. A version's picture is read through the
  // version that recorded it, so comparing two of them needs both numbers and
  // the chunk they are versions of.
  chunkId: string;
  docId: string;
}

function ListFieldDiff({ field }: { field: IChunkVersionFieldDiff }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {field.removed?.map((value) => (
        <span
          key={`removed-${value}`}
          className="rounded-sm bg-[#D8494B]/10 px-1.5 py-0.5 text-sm line-through"
        >
          {value}
        </span>
      ))}
      {field.unchanged?.map((value) => (
        <span
          key={`unchanged-${value}`}
          className="rounded-sm bg-bg-card px-1.5 py-0.5 text-sm"
        >
          {value}
        </span>
      ))}
      {field.added?.map((value) => (
        <span
          key={`added-${value}`}
          className="rounded-sm bg-[#3BA05C]/10 px-1.5 py-0.5 text-sm"
        >
          +{value}
        </span>
      ))}
    </div>
  );
}

function ScalarFieldDiff({ field }: { field: IChunkVersionFieldDiff }) {
  const display = (value: unknown) => {
    if (value === undefined || value === null || value === '') {
      return '—';
    }
    return String(value);
  };

  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="rounded-sm bg-[#D8494B]/10 px-1.5 py-0.5 line-through">
        {display(field.before)}
      </span>
      <span className="text-text-secondary">→</span>
      <span className="rounded-sm bg-[#3BA05C]/10 px-1.5 py-0.5">
        {display(field.after)}
      </span>
    </div>
  );
}

// Visibility reads as the state it describes rather than as the number the
// server sends: 1 is a chunk that takes part in retrieval, 0 one that does not.
function VisibilityFieldDiff({ field }: { field: IChunkVersionFieldDiff }) {
  const { t } = useTranslation();
  const label = (value: unknown) =>
    Number(value) === 0 ? t('chunk.disabled') : t('chunk.enabled');

  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="rounded-sm bg-[#D8494B]/10 px-1.5 py-0.5 line-through">
        {label(field.before)}
      </span>
      <span className="text-text-secondary">→</span>
      <span className="rounded-sm bg-[#3BA05C]/10 px-1.5 py-0.5">
        {label(field.after)}
      </span>
    </div>
  );
}

// The picture each side recorded, side by side. The img_id values a scalar
// comparison would print are storage keys, and what a reader wants to see when a
// chunk's picture changed is the two pictures. A side that recorded none shows a
// dash where its picture would be, which is also what an image that has since
// been cleared from storage falls back to.
function ImageFieldDiff({
  field,
  from,
  to,
  chunkId,
  docId,
}: {
  field: IChunkVersionFieldDiff;
  from: number;
  to: number;
  chunkId: string;
  docId: string;
}) {
  const { t } = useTranslation();
  const { knowledgeId } = useGetKnowledgeSearchParams();

  const recorded = (value: unknown) =>
    typeof value === 'string' && value.trim() !== '';

  const picture = (version: number, hasPicture: boolean) => (
    <figure className="flex min-w-0 flex-1 flex-col items-center gap-1 rounded-md border-0.5 border-border-button p-2">
      {hasPicture ? (
        <AuthenticatedImg
          src={getChunkVersionImageUrl({
            kb_id: knowledgeId,
            doc_id: docId,
            chunk_id: chunkId,
            version,
          })}
          className="max-h-[240px] w-auto max-w-full object-contain"
          fallback={
            <span className="py-6 text-sm text-text-secondary">
              {t('chunk.noImage')}
            </span>
          }
        />
      ) : (
        <span className="py-6 text-sm text-text-secondary">
          {t('chunk.noImage')}
        </span>
      )}
      <figcaption className="text-sm tabular-nums text-text-secondary">
        {version}
      </figcaption>
    </figure>
  );

  return (
    <div className="flex flex-wrap items-stretch gap-2">
      {picture(from, recorded(field.before))}
      <span className="self-center text-text-secondary">→</span>
      {picture(to, recorded(field.after))}
    </div>
  );
}

function FieldDiff({
  field,
  from,
  to,
  chunkId,
  docId,
}: {
  field: IChunkVersionFieldDiff;
  from: number;
  to: number;
  chunkId: string;
  docId: string;
}) {
  const { t } = useTranslation();
  return (
    <div className="space-y-1">
      <div className="flex items-center gap-2 text-sm font-medium text-text-secondary">
        <span>{t(FIELD_LABELS[field.field] ?? field.field)}</span>
      </div>
      {field.kind === 'text' && field.diff && <TextDiff diff={field.diff} />}
      {field.kind === 'list' && <ListFieldDiff field={field} />}
      {field.kind === 'scalar' &&
        (field.field === 'img_id' ? (
          <ImageFieldDiff
            field={field}
            from={from}
            to={to}
            chunkId={chunkId}
            docId={docId}
          />
        ) : field.field === 'available_int' ? (
          <VisibilityFieldDiff field={field} />
        ) : (
          <ScalarFieldDiff field={field} />
        ))}
      {field.kind === 'json' && (
        <div className="text-sm text-text-secondary">
          {t('chunk.diffChanged')}
        </div>
      )}
    </div>
  );
}

// listFieldChanged reports whether a list field moved. It reads the elements
// rather than the server's flag: a server built before the flag existed omits
// it, and a missing value must not read as "unchanged".
function listFieldChanged(field: IChunkVersionFieldDiff) {
  return (field.added?.length ?? 0) > 0 || (field.removed?.length ?? 0) > 0;
}

// ChunkVersionDiffPanel renders the field-level comparison of two versions.
// Only what moved is listed: a keyword list both versions kept is not a change,
// and printing it under "Unchanged" restates the version instead of reviewing
// it.
//
// The pair being compared is not named here: the version on the right is the
// list selection and the base is the dropdown above the panel, so a line
// restating "v0 vs v1" would say what the header already shows.
function ChunkVersionDiffPanel({
  diff,
  loading,
  missingVersion,
  deletedVersion,
  chunkId,
  docId,
}: ChunkVersionDiffProps) {
  const { t } = useTranslation();
  // Coordinates are dropped here as well as in the server: they are not something
  // a reader reviews, and a server built before that decision still reports them.
  // A list field neither side moved is dropped for the same reason: the server
  // reports the whole keyword set either way, and the panel is a review, not a
  // repeat of the chunk.
  const fields = (diff?.fields ?? []).filter(
    (field) =>
      field.field !== 'position_int' &&
      !(field.kind === 'list' && !listFieldChanged(field)),
  );
  // A comparison of a version with itself reports nothing to review, so it says
  // so instead of listing every field as unchanged: the list fields are context
  // only while something else actually changed.
  const nothingToReview =
    diff !== undefined && (diff.identical || !fields.length);

  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col">
      <Spin spinning={loading} className="flex-1 min-h-0">
        <div className="h-full space-y-3 overflow-y-auto">
          {deletedVersion ? (
            <p className="py-2 text-sm text-text-secondary">
              {t('chunk.versionChunkDeleted')}
            </p>
          ) : nothingToReview ? (
            <p className="py-2 text-sm text-text-secondary">
              {t('chunk.noDiff')}
            </p>
          ) : (
            fields.map((field) => (
              <FieldDiff
                key={field.field}
                field={field}
                from={diff?.from ?? 0}
                to={diff?.to ?? 0}
                chunkId={chunkId}
                docId={docId}
              />
            ))
          )}
          {missingVersion && !diff && !loading && (
            <p className="py-2 text-sm text-text-secondary">
              {t('chunk.versionMissing')}
            </p>
          )}
          {!diff && !loading && !missingVersion && (
            <p className="py-2 text-sm text-text-secondary">
              {t('chunk.compareTip')}
            </p>
          )}
        </div>
      </Spin>
    </section>
  );
}

export default ChunkVersionDiffPanel;
