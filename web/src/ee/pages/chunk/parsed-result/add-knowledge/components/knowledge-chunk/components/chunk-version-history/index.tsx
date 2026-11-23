import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog';
import { Button } from '@/components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { AuthenticatedImg } from '@/components/image';
import { Spin } from '@/components/ui/spin';
import {
  useDeleteChunkVersion,
  useFetchChunkVersion,
  useFetchChunkVersionDiff,
  useFetchChunkVersions,
  useRestoreChunkVersion,
} from '@/ee/hooks/use-chunk-version';
import { IChunkVersion } from '@/ee/interfaces/chunk-version';
import { getChunkVersionImageUrl } from '@/ee/services/chunk-version-service';
import { sanitizeHtmlWithoutImages } from '@/ee/utils/dom-util';
import { versionActorLabel } from '@/ee/utils/version-actor';
import { useGetKnowledgeSearchParams } from '@/hooks/route-hook';
import { cn } from '@/lib/utils';
import { formatDate } from '@/utils/date';
import { ReactNode, useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ChunkVersionDiffPanel from './chunk-version-diff';

type VersionPanelMode = 'preview' | 'diff';

// Which side produced a version, as a label key. The actor line cannot answer
// this on its own: a dataset restore rewrites chunks with nobody's name on them,
// so the preview names the origin beside the actor.
const ORIGIN_LABEL_KEYS: Record<string, string> = {
  parse: 'chunk.originParse',
  chunk: 'chunk.originChunk',
  // dataset_restore has no general label: a replay can only write a restore or
  // a removal, and both name the dataset version below instead.
};

interface ChunkVersionHistoryProps {
  chunkId: string;
  docId: string;
}

interface VersionRowProps {
  item: IChunkVersion;
  selected: boolean;
  onSelect: (version: number) => void;
}

// A version row written before the timestamp column existed carries 0: the
// instant is unknown, so both panes leave the slot empty rather than formatting
// it into the epoch. One rule, so the list and the header cannot disagree.
const versionTime = (millis?: number) => (millis ? formatDate(millis) : '');

// The user a row credits: the person whose request produced it. A system-written
// row names nobody itself, so the server credits whoever triggered the dataset
// version it was written under - the user who asked for the parse or the restore.
// The raw actor is only a fallback, for a row whose dataset version is gone.
type ChunkVersionActorFields = {
  created_by?: string;
  created_by_name?: string;
  triggered_by?: string;
  triggered_by_name?: string;
};

const chunkVersionActor = (item: ChunkVersionActorFields) => ({
  created_by: item.triggered_by || item.created_by,
  created_by_name: item.triggered_by_name || item.created_by_name,
});

const chunkVersionActorLabel = (
  item: ChunkVersionActorFields,
  systemLabel: string,
) => versionActorLabel(chunkVersionActor(item), systemLabel);

// The list is a way into a version, not a table of one: it carries the instant
// the version was written and the user whose request produced it. That user is
// named the way the preview names them - the nickname the server resolved, the raw
// id for a user that no longer exists, and the localised label for a row nothing
// can attribute - so the two panes cannot disagree about who made a change.
// Everything else (the number, the head marker, the state it was restored from) is
// read in the preview beside it, which the selection drives.
function VersionRow({ item, selected, onSelect }: VersionRowProps) {
  const { t } = useTranslation();
  const time = versionTime(item.create_time);
  const actor = chunkVersionActorLabel(item, t('chunk.systemActor'));
  const actorInfo = chunkVersionActor(item);
  // A nickname is not an identity: an administrator who has to act on the row
  // still needs the id, so it stays on the row as a tooltip. A row the system
  // wrote has the marker there instead of a person, so it gets no tooltip.
  const actorId =
    actorInfo.created_by_name?.trim() &&
    actorInfo.created_by?.trim() !== 'system'
      ? actorInfo.created_by?.trim()
      : undefined;
  const handleSelect = useCallback(
    () => onSelect(item.version),
    [item.version, onSelect],
  );

  return (
    <button
      type="button"
      onClick={handleSelect}
      // The number is off the face of the row, so it is the row's accessible
      // name: without it every row would read as an instant and a user, and
      // nothing would say which version is which.
      aria-label={[`v${item.version}`, time, actor].filter(Boolean).join(' ')}
      className={cn(
        'w-full rounded-md px-2 py-1.5 text-left transition-colors',
        'hover:bg-bg-card focus-visible:outline-none',
        selected && 'bg-bg-card',
      )}
    >
      {!!time && (
        <div className="truncate text-sm tabular-nums text-text-primary">
          {time}
        </div>
      )}
      {!!actor && (
        <div
          className="truncate text-xs text-text-secondary"
          title={actorId || undefined}
        >
          {actor}
        </div>
      )}
    </button>
  );
}

interface VersionSelectProps {
  value?: number;
  versions: IChunkVersion[];
  placeholder: string;
  onChange: (version: number) => void;
}

function VersionSelect({
  value,
  versions,
  placeholder,
  onChange,
}: VersionSelectProps) {
  return (
    <Select
      value={value === undefined ? undefined : String(value)}
      onValueChange={(next) => onChange(Number(next))}
    >
      <SelectTrigger className="h-8 w-[104px]">
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {versions.map((item) => (
          <SelectItem key={item.version} value={String(item.version)}>
            {String(item.version)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

// The preview lists the fields in the same order and with the same block shape
// the comparison uses (label above, value below), so reading one version and
// comparing two of them look like the same pane instead of two layouts.
function VersionField({
  label,
  children,
}: {
  label: string;
  children?: ReactNode;
}) {
  return (
    <div className="space-y-1">
      <div className="text-sm font-medium text-text-secondary">{label}</div>
      {children}
    </div>
  );
}

// A meta line is the label-and-value pair the preview uses for the version it
// is showing, laid out on one line: those facts are short and belong above the
// content, not stacked into it the way the fields below are.
function MetaLine({
  label,
  children,
}: {
  label: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex items-baseline gap-2 text-sm">
      <span className="w-16 shrink-0 text-text-secondary">{label}</span>
      <span className="min-w-0 flex-1 break-words text-text-primary">
        {children}
      </span>
    </div>
  );
}

// A list field with nothing in it is left out entirely: the label alone would
// read as a field the version lost rather than one it never had.
function VersionChipsField({
  label,
  values,
}: {
  label: string;
  values?: string[];
}) {
  if (!values?.length) {
    return null;
  }
  return (
    <VersionField label={label}>
      <div className="flex min-w-0 flex-wrap gap-1.5">
        {values.map((value) => (
          <span
            key={value}
            className="rounded-sm bg-bg-card px-1.5 py-0.5 text-sm text-text-primary"
          >
            {value}
          </span>
        ))}
      </div>
    </VersionField>
  );
}

function ChunkVersionHistory({ chunkId, docId }: ChunkVersionHistoryProps) {
  const { t } = useTranslation();
  const { knowledgeId } = useGetKnowledgeSearchParams();
  const { versions, loading } = useFetchChunkVersions(chunkId, docId);
  // The list arrives newest first, and the copy the panel renders first can be
  // the one cached before the last edit. The selection therefore follows the
  // newest row until the user picks one: opening the history right after a save
  // lands on the version that save wrote, instead of the cached previous one.
  // A picked version that disappears (deleted) falls back to the newest too.
  const [pickedVersion, setPickedVersion] = useState<number | undefined>();
  const selectedVersion =
    pickedVersion !== undefined &&
    versions.some((item) => item.version === pickedVersion)
      ? pickedVersion
      : versions[0]?.version;
  const { detail, loading: detailLoading } = useFetchChunkVersion(
    chunkId,
    docId,
    selectedVersion,
  );
  const { restoreChunkVersion, loading: restoring } = useRestoreChunkVersion();
  const { deleteChunkVersion, loading: deleting } = useDeleteChunkVersion();
  // Both actions ask before they run: a restore rewrites the live chunk, and a
  // delete removes a state the history can otherwise keep.
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [restoreOpen, setRestoreOpen] = useState(false);
  // A refused delete closes its confirmation and says why in its own dialog:
  // the reason names the row that still needs this version, which is more than
  // a toast can carry, and the reader has to pick another version anyway.
  // null means "no refusal to show"; an empty string means the server gave no
  // sentence and the generic one is used.
  const [deleteRefusal, setDeleteRefusal] = useState<string | null>(null);

  // The panel opens on the version the list selected; the comparison is a switch
  // away.
  const [mode, setMode] = useState<VersionPanelMode>('preview');
  // The base of the comparison is not state of its own until the user names one:
  // by default it is the version the selection replaced, which is what "the
  // changes of vN" means. A pick from the dropdown overrides it for as long as
  // that version stays selected, and the next pick starts from the default again
  // so a base chosen for one version never silently describes another.
  const [compareFromOverride, setCompareFromOverride] = useState<
    number | undefined
  >();
  // The right-hand side of a comparison is never picked: it is the version the
  // list has selected, so the question the panel answers is always "what does
  // this version change, compared with …".
  const compareTo = selectedVersion;

  const orderedVersions = useMemo(
    () => [...versions].sort((a, b) => a.version - b.version),
    [versions],
  );

  // The state each version replaced. The baseline has none, so its comparison
  // falls back to itself - the panel then says there is nothing to change,
  // which is true, and the dropdown is there to name another base.
  const previousVersions = useMemo(() => {
    const previous = new Map<number, number>();
    orderedVersions.forEach((item, index) => {
      if (index > 0) {
        previous.set(item.version, orderedVersions[index - 1].version);
      }
    });
    return previous;
  }, [orderedVersions]);

  const compareFrom =
    compareFromOverride ??
    (selectedVersion === undefined
      ? undefined
      : (previousVersions.get(selectedVersion) ?? selectedVersion));

  const {
    diff,
    loading: diffLoading,
    missingVersion,
  } = useFetchChunkVersionDiff(chunkId, docId, compareFrom, compareTo);

  const handleSelect = useCallback((version: number) => {
    setPickedVersion(version);
    setCompareFromOverride(undefined);
  }, []);

  const handleRestore = useCallback(async () => {
    if (selectedVersion === undefined) {
      return;
    }
    const code = await restoreChunkVersion({
      kb_id: knowledgeId,
      doc_id: docId,
      chunk_id: chunkId,
      version: selectedVersion,
    });
    if (code === 0) {
      // A refusal keeps the confirmation open, the same way it keeps the delete
      // one open: the reason is shown by the request layer and the reader can
      // pick another version from the list behind it.
      setRestoreOpen(false);
    }
  }, [chunkId, docId, knowledgeId, restoreChunkVersion, selectedVersion]);

  const handleDelete = useCallback(async () => {
    if (selectedVersion === undefined) {
      return;
    }
    const outcome = await deleteChunkVersion({
      kb_id: knowledgeId,
      doc_id: docId,
      chunk_id: chunkId,
      version: selectedVersion,
    });
    // The confirmation is done with either way: on success there is nothing
    // left to confirm, and on a refusal the reason is said in its own dialog so
    // the two sentences never sit on top of each other.
    setDeleteOpen(false);
    if (outcome?.code !== 0) {
      setDeleteRefusal(outcome?.message ?? '');
      return;
    }
    // Fall back to the refreshed newest row: the deleted version's detail can
    // no longer be loaded. A base picked against the deleted version is
    // dropped with it, so the next comparison starts from a version that
    // still exists.
    setPickedVersion(undefined);
    setCompareFromOverride(undefined);
  }, [chunkId, deleteChunkVersion, docId, knowledgeId, selectedVersion]);

  const snapshot = detail?.chunk;
  // An image chunk's body is the picture it holds; the text stored beside it is
  // the caption a match reads, not what a reader opens the panel to see.
  const isImageChunk = snapshot?.doc_type_kwd === 'image';

  // The latest row is the state the live chunk carries, and a parse row is the
  // baseline a replay rebuilds from; a delete row is what tells a replay the
  // chunk was gone. The server refuses the same four, so the buttons only mirror
  // it. Everything else (edits and restores) is plain history.
  const selectedEntry = versions.find(
    (item) => item.version === selectedVersion,
  );
  // The preview header is read from the row the list selected until the detail
  // request answers (and from a detail that names no version, which only a
  // hand-built payload does): the list no longer prints the number, so the pane
  // must not come up blank about which version it is showing.
  const headerVersion = detail?.version !== undefined ? detail : selectedEntry;
  // The header answers only what the list cannot: which version this pane shows
  // and where it came from. Origin keeps both halves in one line: a restore
  // names the state it replayed, and a replay by the dataset versioning says so
  // in the same sentence.
  //
  // An addition and a removal read better from the kind than from the side,
  // which they share with an edit; a removal is the one that can also come from
  // the pipeline, so its origin decides the wording.
  const originKey = headerVersion?.origin
    ? ORIGIN_LABEL_KEYS[headerVersion.origin]
    : undefined;
  const kindOriginKey =
    headerVersion?.kind === 'add'
      ? 'chunk.originAdd'
      : headerVersion?.kind === 'delete' && headerVersion.origin
        ? headerVersion.origin === 'parse'
          ? 'chunk.originRemoveParse'
          : 'chunk.originRemove'
        : undefined;
  const originLabel =
    headerVersion?.kind === 'restore'
      ? headerVersion.origin === 'dataset_restore'
        ? headerVersion.dataset_version !== undefined
          ? t('chunk.restoredFromDatasetVersion', {
              version: headerVersion.restored_from,
              dataset: headerVersion.dataset_version,
            })
          : t('chunk.restoredFromDataset', {
              version: headerVersion.restored_from,
            })
        : t('chunk.restoredFrom', { version: headerVersion.restored_from })
      : headerVersion?.kind === 'delete' &&
          headerVersion.origin === 'dataset_restore'
        ? headerVersion.dataset_version !== undefined
          ? t('chunk.originRestore', {
              dataset: headerVersion.dataset_version,
            })
          : t('chunk.originRestorePlain')
        : kindOriginKey
          ? t(kindOriginKey)
          : originKey
            ? t(originKey)
            : '';
  const selectedKind = detail?.kind ?? selectedEntry?.kind;
  // A removal marker describes no state: it records that the chunk was gone from
  // that version on, and a tombstone carries no snapshot to read. Both panes say
  // so instead of showing an empty body and a comparison of an absent chunk.
  const isDeletedVersion = selectedKind === 'delete';
  const isLatestVersion = detail?.latest ?? selectedEntry?.latest ?? false;
  const restorable = selectedVersion !== undefined && !isLatestVersion;
  const deletable =
    restorable &&
    selectedKind !== 'parse' &&
    selectedKind !== 'add' &&
    selectedKind !== 'delete';

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex min-h-0 flex-1 gap-4">
        {/* Wide enough for the instant and the author side by side: the row
            carries a full timestamp and a nickname, and a narrower column would
            cut one of them in every row. */}
        <aside className="flex w-[240px] shrink-0 flex-col border-r-0.5 border-border-button pr-2">
          <div className="min-h-0 flex-1 space-y-1 overflow-y-auto">
            {loading && versions.length === 0 && (
              <div className="flex justify-center py-8">
                <Spin size="large" />
              </div>
            )}
            {!loading && versions.length === 0 && (
              <p className="py-2 text-sm text-text-secondary">
                {t('chunk.noVersions')}
              </p>
            )}
            {versions.map((item) => (
              <VersionRow
                key={item.version}
                item={item}
                selected={item.version === selectedVersion}
                onSelect={handleSelect}
              />
            ))}
          </div>
          {/* The actions act on the row the list has selected, so they sit under
              the list rather than under the preview, and they stay there in both
              modes: what they act on is the selection, not the view. */}
          {versions.length > 0 && (
            <div className="mt-2 flex shrink-0 flex-col gap-2 border-t-0.5 border-border-button pt-2">
              <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
                <AlertDialogTrigger asChild>
                  <Button
                    variant="outline"
                    size="sm"
                    // The label carries the danger, not only the confirmation
                    // inside: the two buttons under the list would otherwise read
                    // as the same kind of action. hover/focus are restated
                    // because outline's own hover colour outranks the base one.
                    className="w-full justify-center text-state-error hover:text-state-error focus-visible:text-state-error"
                    disabled={!deletable || deleting}
                  >
                    {t('chunk.deleteVersion')}
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>
                      {t('chunk.deleteVersionTitle', {
                        version: selectedVersion ?? 0,
                      })}
                    </AlertDialogTitle>
                    <AlertDialogDescription>
                      {t('chunk.deleteVersionDescription')}
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
                    <AlertDialogAction
                      disabled={deleting}
                      onClick={handleDelete}
                      className="bg-state-error text-text-primary hover:bg-state-error hover:text-text-primary"
                    >
                      {t('common.confirm')}
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
              {/* A refusal is a fact about the row, not a failed request: the
                  sentence the server answers with names what still needs this
                  version, so it is shown until acknowledged instead of fading
                  out after three seconds. */}
              <AlertDialog
                open={deleteRefusal !== null}
                onOpenChange={(open) => {
                  if (!open) {
                    setDeleteRefusal(null);
                  }
                }}
              >
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>
                      {t('chunk.versionNotDeletable')}
                    </AlertDialogTitle>
                    <AlertDialogDescription>
                      {deleteRefusal ||
                        t('chunk.versionNotDeletableDescription')}
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogAction>{t('common.ok')}</AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
              <AlertDialog open={restoreOpen} onOpenChange={setRestoreOpen}>
                <AlertDialogTrigger asChild>
                  <Button
                    variant="outline"
                    size="sm"
                    className="w-full justify-center"
                    disabled={restoring || !restorable}
                  >
                    {t('chunk.restoreVersion')}
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>
                      {t('chunk.restoreVersionTitle', {
                        version: selectedVersion ?? 0,
                      })}
                    </AlertDialogTitle>
                    <AlertDialogDescription>
                      {t('chunk.restoreVersionDescription', {
                        version: selectedVersion ?? 0,
                      })}
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
                    <AlertDialogAction
                      disabled={restoring}
                      onClick={(event) => {
                        // Radix closes the dialog as the action is clicked. The
                        // click is held here instead, so a refusal keeps the
                        // confirmation up and handleRestore closes it only once
                        // the version was written back.
                        event.preventDefault();
                        void handleRestore();
                      }}
                    >
                      {t('common.confirm')}
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </div>
          )}
        </aside>

        {/* The mode switch belongs to the panel it switches, so it sits above the
            changes or the details, not above the version list. The details come
            first: the list selects a version, and reading that version is what the
            selection is for; comparing it with another one is the follow-up. */}
        <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
          <div className="flex items-center gap-1">
            <Button
              variant={mode === 'preview' ? 'secondary' : 'ghost'}
              size="sm"
              onClick={() => setMode('preview')}
            >
              {t('chunk.versionContent')}
            </Button>
            <Button
              variant={mode === 'diff' ? 'secondary' : 'ghost'}
              size="sm"
              onClick={() => setMode('diff')}
            >
              {t('chunk.versionChanges')}
            </Button>
          </div>

          {/* The pair sits under the switch rather than beside it: it describes
              the comparison below, so it reads as its heading instead of as a
              third control in the toolbar. The margin under it is what separates
              the heading from the fields it introduces. Only the base is picked.
              The version on the right is whatever the list selected, shown as it
              stands so the panel is never comparing a version with itself. */}
          {mode === 'diff' && compareTo !== undefined && (
            <div className="mb-2 flex flex-wrap items-center gap-2">
              <VersionSelect
                value={compareFrom}
                versions={orderedVersions}
                placeholder={t('chunk.compareFrom')}
                onChange={setCompareFromOverride}
              />
              <span className="text-sm text-text-secondary">→</span>
              <span className="rounded-sm bg-bg-card px-2 py-1 text-sm text-text-primary">
                {compareTo}
              </span>
            </div>
          )}

          {mode === 'diff' ? (
            <ChunkVersionDiffPanel
              diff={diff}
              loading={diffLoading}
              missingVersion={missingVersion}
              deletedVersion={isDeletedVersion}
              chunkId={chunkId}
              docId={docId}
            />
          ) : (
            <section className="flex min-h-0 min-w-0 flex-1 flex-col">
              <Spin spinning={detailLoading} className="flex-1 min-h-0">
                <div className="h-full space-y-3 overflow-y-auto pr-1">
                  {/* Two facts the list cannot give: which version this pane is
                      showing, and where it came from. The instant and the
                      person are on the row that selected it. */}
                  {headerVersion && (
                    <div className="space-y-1 rounded-md bg-bg-card px-2 py-1.5">
                      <MetaLine label={t('chunk.detailVersion')}>
                        <span className="font-medium tabular-nums">
                          {headerVersion.version}
                        </span>
                        {headerVersion.latest && (
                          <span className="ml-1.5 text-text-secondary">
                            {t('chunk.currentVersion')}
                          </span>
                        )}
                      </MetaLine>
                      {!!originLabel && (
                        <MetaLine label={t('chunk.detailOrigin')}>
                          {originLabel}
                        </MetaLine>
                      )}
                    </div>
                  )}
                  {/* A removal marker carries no snapshot: the row records that
                      the chunk was gone from that version on, so the pane says
                      so rather than presenting an empty body as its content. */}
                  {isDeletedVersion ? (
                    <p className="text-sm text-text-secondary">
                      {t('chunk.versionChunkDeleted')}
                    </p>
                  ) : (
                    <>
                      {/* Content, keywords and questions are three fields of
                        the same version, so they are listed one under another -
                        no rule splitting the body from its metadata. */}
                      <VersionField label={t('chunk.diffFieldContent')}>
                        {/* The chunk body is the thing a reader opens this panel
                          for, so it is body copy rather than a caption: 16px
                          with relaxed leading. It is rendered from the stored
                          HTML (as the chunk card does) so the tables and
                          emphasis the chunk carries keep their shape instead of
                          showing up as raw tags. Images are stripped from the
                          HTML: a picture of the page the chunk was cut from says
                          nothing about the text it holds. */}
                        {snapshot?.content_with_weight && (
                          <div
                            className={cn(
                              'break-words whitespace-pre-wrap text-base leading-relaxed text-text-primary',
                              '[&_em]:not-italic [&_em]:text-accent-primary',
                              '[&_table]:w-full [&_table]:border-collapse [&_table]:text-sm',
                              '[&_th]:border [&_th]:border-border-button [&_th]:px-2 [&_th]:py-1 [&_th]:text-left',
                              '[&_td]:border [&_td]:border-border-button [&_td]:px-2 [&_td]:py-1 [&_td]:whitespace-nowrap',
                            )}
                            dangerouslySetInnerHTML={{
                              __html: sanitizeHtmlWithoutImages(
                                snapshot.content_with_weight,
                              ),
                            }}
                          />
                        )}
                        {/* An image chunk's text is what a match reads; the
                          picture beside it is what the chunk is, so the panel
                          shows both. The URL is the version's own image
                          endpoint: a version's picture never changes, so the
                          version number is the whole address. */}
                        {isImageChunk && snapshot?.img_id && (
                          <AuthenticatedImg
                            src={getChunkVersionImageUrl({
                              kb_id: knowledgeId,
                              doc_id: docId,
                              chunk_id: chunkId,
                              version: selectedVersion ?? 0,
                            })}
                            className="max-h-[320px] w-auto max-w-full object-contain"
                            fallback={
                              <span className="text-sm text-text-secondary">
                                {t('chunk.noImage')}
                              </span>
                            }
                          />
                        )}
                        {selectedVersion !== undefined &&
                          !snapshot?.content_with_weight &&
                          !(isImageChunk && snapshot?.img_id) && (
                            <p className="text-sm text-text-secondary">
                              {t('chunk.versionEmpty')}
                            </p>
                          )}
                      </VersionField>
                      <VersionChipsField
                        label={t('chunk.keyword')}
                        values={snapshot?.important_kwd}
                      />
                      <VersionChipsField
                        label={t('chunk.question')}
                        values={snapshot?.question_kwd}
                      />
                      <VersionChipsField
                        label={t('chunk.tags')}
                        // The list the editor wrote is what the chunk shows;
                        // tag_feas holds the weights a match reads, and a
                        // version recorded before the list was stored carries
                        // only that.
                        values={
                          snapshot?.tag_kwd?.length
                            ? snapshot.tag_kwd
                            : Object.keys(snapshot?.tag_feas ?? {})
                        }
                      />
                      {/* The switch is part of the version: only the hiding 0
                          is stored, so a version without the key was visible. */}
                      <VersionField label={t('chunk.diffFieldVisibility')}>
                        <span className="text-sm text-text-primary">
                          {snapshot?.available_int === 0
                            ? t('chunk.disabled')
                            : t('chunk.enabled')}
                        </span>
                      </VersionField>
                    </>
                  )}
                </div>
              </Spin>
            </section>
          )}
        </div>
      </div>
    </div>
  );
}

export default ChunkVersionHistory;
