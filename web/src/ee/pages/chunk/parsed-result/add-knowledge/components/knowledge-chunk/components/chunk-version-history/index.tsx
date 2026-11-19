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
import { versionActorLabel } from '@/ee/utils/version-actor';
import { useGetKnowledgeSearchParams } from '@/hooks/route-hook';
import { cn } from '@/lib/utils';
import { sanitizeHtmlWithoutImages } from '@/ee/utils/dom-util';
import { ReactNode, useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ChunkVersionDiffPanel from './chunk-version-diff';

type VersionPanelMode = 'preview' | 'diff';

interface ChunkVersionHistoryProps {
  chunkId: string;
  docId: string;
}

interface VersionRowProps {
  item: IChunkVersion;
  selected: boolean;
  onSelect: (version: number) => void;
}

function VersionRow({ item, selected, onSelect }: VersionRowProps) {
  const { t } = useTranslation();
  const actor = versionActorLabel(item, t('chunk.systemActor'));
  const handleSelect = useCallback(
    () => onSelect(item.version),
    [item.version, onSelect],
  );

  return (
    <button
      type="button"
      onClick={handleSelect}
      className={cn(
        'w-full rounded-md px-2 py-1.5 text-left transition-colors',
        'hover:bg-bg-card focus-visible:outline-none',
        selected && 'bg-bg-card',
      )}
    >
      <div className="flex items-center gap-1.5">
        <span className={cn('text-sm', selected && 'text-text-primary')}>
          v{item.version}
        </span>
        {item.latest && (
          <span className="rounded-sm bg-bg-card px-1 text-xs text-text-secondary">
            {t('chunk.latestVersion')}
          </span>
        )}
      </div>
      <div className="truncate text-xs text-text-secondary">
        {item.create_date || ''}
      </div>
      {!!actor && (
        <div className="truncate text-xs text-text-secondary" title={actor}>
          {t('chunk.changedBy', { user: actor })}
        </div>
      )}
      {item.restored_from > 0 && (
        <div className="truncate text-xs text-text-secondary">
          {t('chunk.restoredFrom', { version: item.restored_from })}
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
            v{item.version}
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

  const [mode, setMode] = useState<VersionPanelMode>('diff');
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
  // Who produced the previewed version; empty when the row names nobody.
  const detailActor = detail
    ? versionActorLabel(detail, t('chunk.systemActor'))
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
        <aside className="flex w-[200px] shrink-0 flex-col border-r-0.5 border-border-button pr-2">
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
            changes or the content, not above the version list. The changes come
            first: comparing two versions is what a history is opened for, and
            reading one on its own is the follow-up question. */}
        <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-1">
              <Button
                variant={mode === 'diff' ? 'secondary' : 'ghost'}
                size="sm"
                onClick={() => setMode('diff')}
              >
                {t('chunk.versionChanges')}
              </Button>
              <Button
                variant={mode === 'preview' ? 'secondary' : 'ghost'}
                size="sm"
                onClick={() => setMode('preview')}
              >
                {t('chunk.versionContent')}
              </Button>
            </div>
            {mode === 'diff' && compareTo !== undefined && (
              <div className="flex items-center gap-2">
                {/* Only the base of the comparison is picked. The version on the
                    right is whatever the list selected, shown as it stands so
                    the panel is never comparing a version with itself. */}
                <VersionSelect
                  value={compareFrom}
                  versions={orderedVersions}
                  placeholder={t('chunk.compareFrom')}
                  onChange={setCompareFromOverride}
                />
                <span className="text-sm text-text-secondary">→</span>
                <span className="rounded-sm bg-bg-card px-2 py-1 text-sm text-text-primary">
                  v{compareTo}
                </span>
              </div>
            )}
          </div>

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
                  {/* Which version this pane is showing, before the text itself:
                    the list on the left can be scrolled away from the row. */}
                  {detail && (
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-text-secondary">
                      <span className="font-medium text-text-primary">
                        v{detail.version}
                      </span>
                      {detail.latest && <span>{t('chunk.latestVersion')}</span>}
                      {detail.restored_from > 0 && (
                        <span>
                          {t('chunk.restoredFrom', {
                            version: detail.restored_from,
                          })}
                        </span>
                      )}
                      {!!detail.create_date && (
                        <span className="tabular-nums">
                          {detail.create_date}
                        </span>
                      )}
                      {/* The server resolves the actor id to a nickname; the id
                          is only a fallback for a user that no longer exists. */}
                      {!!detailActor && (
                        <span>
                          {t('chunk.changedBy', { user: detailActor })}
                        </span>
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
