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
import { Spin } from '@/components/ui/spin';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { toast } from '@/components/hooks/use-toast';
import {
  useCheckDatasetVersionRestore,
  useDeleteDatasetVersion,
  useFetchDatasetVersion,
  useFetchDatasetVersions,
  useRestoreDatasetVersion,
} from '@/ee/hooks/use-dataset-version';
import {
  IDatasetConfigChange,
  IDatasetDocumentConfigChange,
  IDatasetRestoreResult,
  IDatasetVersion,
  IDatasetVersionChange,
} from '@/ee/interfaces/dataset-version';
import { versionActorLabel } from '@/ee/utils/version-actor';
import { cn } from '@/lib/utils';
import { formatDate } from '@/utils/date';
import type { TFunction } from 'i18next';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  CHANGE_ACTION_LABELS,
  CHANGE_ENTITY_LABELS,
  CHUNK_ACTION_LABELS,
} from './change-labels';

// Only the kinds the backend produces are translated; a kind added later falls
// back to its raw value rather than showing a missing key.
const KIND_LABELS: Record<string, string> = {
  baseline: 'datasetVersion.kindBaseline',
  dataset_config: 'datasetVersion.kindDatasetConfig',
  document_config: 'datasetVersion.kindDocumentConfig',
  document_rename: 'datasetVersion.kindDocumentRename',
  document_meta: 'datasetVersion.kindDocumentMetadata',
  // A document switched on or off. It reads as a document change, not as a
  // chunk one: the chunks follow the document's switch, and the version names
  // the documents it moved. The two directions have their own kinds; the
  // unnamed one stays mapped for rows written before them, and for a switch to
  // a status that is neither enabled nor disabled.
  document_status: 'datasetVersion.kindDocumentStatus',
  document_enable: 'datasetVersion.kindDocumentEnable',
  document_disable: 'datasetVersion.kindDocumentDisable',
  // A hand edit of a compiled wiki page: the page keeps its own commit history,
  // and this is the timeline entry that says the dataset's pages moved.
  artifact_edit: 'datasetVersion.kindArtifactEdit',
  restore: 'datasetVersion.kindRestore',
  upload: 'datasetVersion.kindUpload',
  delete: 'datasetVersion.kindDelete',
  parse: 'datasetVersion.kindParse',
  chunk: 'datasetVersion.kindChunk',
  // A chunk version names the action it recorded, so the timeline says which one
  // instead of one label for all of them. `chunk` stays mapped for the rows
  // written before these kinds existed, and for a batch that mixed actions.
  chunk_add: 'datasetVersion.kindChunkAdd',
  chunk_update: 'datasetVersion.kindChunkUpdate',
  chunk_delete: 'datasetVersion.kindChunkDelete',
  // A chunk restored from its own history. It reads as a chunk change, not as
  // the document-level restore above: the version rebuilds one chunk.
  chunk_restore: 'datasetVersion.kindChunkRestore',
  // A chunk switched on or off. The change set keeps the edit action, so the
  // kind is the only thing that says which way the switch went.
  chunk_enable: 'datasetVersion.kindChunkEnable',
  chunk_disable: 'datasetVersion.kindChunkDisable',
};

// These kinds carry a configuration snapshot of their own, so a restore of one
// writes that configuration back. Every other kind is a content version: its
// change is the documents or chunks it touched and the snapshot beside it is only
// the configuration it happened under - restoring one rebuilds that document set
// instead, which is also how a restore is undone.
const CONFIGURATION_KINDS = new Set(['baseline', 'dataset_config', 'restore']);

// How many versions one request reads. The list grows the request instead of
// paging by cursor, so the panel keeps one array it can select from; older
// versions arrive when the reader scrolls to the end of what is loaded.
const VERSION_PAGE_SIZE = 50;

// How many file names a version's detail lists before it folds the rest behind a
// toggle: a dataset can hold thousands of documents, and the panel is a narrow
// column that would otherwise be nothing but names.
const DOCUMENTS_PREVIEW_LIMIT = 5;

// MAX_VERSION_PAGE mirrors the server's own ceiling (maxDatasetVersionPageSize in
// internal/service/dataset/version_ee.go). A larger limit is clamped there, so
// asking again past it would only re-read the same rows: the list stops instead.
const MAX_VERSION_PAGE = 200;

// VERSION_SCROLL_THRESHOLD is how close to the bottom counts as "the end". It is
// a little ahead of the edge so the next page is already on its way by the time
// the reader gets there, instead of showing an empty strip first.
const VERSION_SCROLL_THRESHOLD = 120;

interface DatasetVersionHistoryProps {
  datasetId: string;
  // kinds restricts the history to these kinds. Empty shows the whole timeline,
  // which is what the versions page wants: configuration and content sit side by
  // side in one sequence.
  kinds?: string[];
  className?: string;
  // listClassName sizes the version table. The sheet and the full-page view have
  // very different room to give, so each caller picks its own width; the page
  // passes a width that grows with the window rather than a fixed column.
  listClassName?: string;
}

// One value as a change line prints it. Shared by the configuration block and
// the change list, and defined here because both of them are above the table:
// a value that is empty reads as the panel's own dash, an object keeps its JSON
// shape, and a boolean says which of the two it is instead of printing a word
// from the reader's language.
const formatValue = (value: unknown): string => {
  if (value === null || value === undefined || value === '') {
    return '—';
  }
  if (typeof value === 'object') {
    return JSON.stringify(value, null, 2);
  }
  if (typeof value === 'boolean') {
    return value ? 'true' : 'false';
  }
  return String(value);
};

// Change entries are labelled by entity and action; both vocabularies are
// closed on the server, and an unknown value falls back to the raw one.
/**
 * VersionChangeList renders the documents and chunks a version touched, one
 * sentence per change ("Added document handbook.pdf"). The set is captured when
 * the change happens and is bounded server side, so a long list means a large
 * change rather than a broken one.
 *
 * A sentence rather than labelled columns: a change row answers one question —
 * what happened to what — and three columns made the reader assemble the
 * sentence themselves, while the badges wrapped awkwardly in the narrow sheet.
 *
 * A chunk entry is one sentence: which chunk of which document moved ("Edited
 * chunk fc0b414f of document handbook.pdf"). The text the chunk held at that
 * version is not repeated here - it is the chunk's own history, one click away,
 * and pasting it under every edit turned a list of changes into pages of prose.
 *
 * Extending a chunk set is read one chunk at a time, because which chunk came in
 * is the point. Shrinking it is not: a removal says nothing about the chunk that
 * carried it, so the removals collapse into one counted line, and the ids and
 * their type stay in the chunk's own history.
 *
 * A wiki page entry carries a diff, replayed from that page's own commit chain:
 * the sentence names the page, and the diff underneath says what moved in it,
 * because that is the question the sentence cannot answer - what does this
 * version actually differ in.
 */
function VersionChangeList({
  changes,
  coveredDocIDs,
}: {
  changes?: IDatasetVersionChange[];
  // coveredDocIDs are the documents the parse settings block below names in its
  // head line: repeating "Changed document X" above it would say it twice.
  coveredDocIDs?: Set<string>;
}) {
  const { t } = useTranslation();
  if (!changes?.length) {
    return null;
  }
  // An entity or action the server adds later shows its raw value.
  const actionLabel = (value: string) =>
    CHANGE_ACTION_LABELS[value] ? t(CHANGE_ACTION_LABELS[value]) : value;
  const entityLabel = (value: string) =>
    CHANGE_ENTITY_LABELS[value] ? t(CHANGE_ENTITY_LABELS[value]) : value;
  // Removing chunks is one fact however many chunks it touched. Listing them one
  // per line turns a bulk delete into a wall of ids that all say the same thing,
  // and the text a removed chunk held is what the chunk's own history is for. The
  // count travels; the ids, their text and their type do not.
  const removedChunkCount = changes.filter(
    (change) => change.entity === 'chunk' && change.action === 'delete',
  ).length;
  return (
    <div className="space-y-2">
      {changes.map((change, index) => {
        if (
          change.entity === 'document' &&
          change.doc_id &&
          coveredDocIDs?.has(change.doc_id)
        ) {
          return null;
        }
        if (change.entity === 'chunk' && change.action === 'delete') {
          return null;
        }
        const action = actionLabel(change.action);
        // A chunk entry keys on its chunk, a document entry on its document:
        // each entry has one id, and it is the one its entity uses.
        const key = `${change.entity}-${change.action}-${change.chunk_id ?? change.doc_id ?? index}`;
        if (change.entity === 'chunk') {
          // "Edited chunk X of document Y": the chunk's text moved, which the
          // document entries' "Changed" does not say on its own.
          const chunkAction = CHUNK_ACTION_LABELS[change.action]
            ? t(CHUNK_ACTION_LABELS[change.action])
            : action;
          return (
            <div key={key}>
              <p className="break-all pl-3 text-sm">
                {t('datasetVersion.chunkChangeLine', {
                  action: chunkAction,
                  chunk: change.chunk_id || '—',
                  // The server names the document on read; a chunk whose
                  // document is gone keeps the id it still knows.
                  document: change.doc_name || change.doc_id || '—',
                })}
                {typeof change.chunk_version === 'number' && (
                  // The number the chunk's own history carries after this change,
                  // so the entry can be read next to the row it produced. The
                  // server resolves it on read; a pruned history leaves it out.
                  <span className="ml-1 text-text-tertiary">
                    {t('datasetVersion.chunkVersionTag', {
                      version: change.chunk_version,
                    })}
                  </span>
                )}
              </p>
            </div>
          );
        }
        // A document entry carries the document's name, so one line says it all.
        // A rename is the exception: the name it landed on says nothing about
        // what moved, so the entry names both sides when the server sent the old
        // one (rows written before the field existed keep the one-sided line).
        // A metadata version says what it moved - "Changed metadata for X" - and
        // then the keys underneath: "the metadata changed" without the values is
        // a fact the reader cannot act on, and the store keeps no diff of its own
        // to link to.
        const renamedFrom =
          change.action === 'rename' ? change.previous_name : undefined;
        return (
          <div key={key} className="space-y-1">
            <p
              className="truncate pl-3 text-sm"
              title={
                renamedFrom ? `${renamedFrom} → ${change.name}` : change.name
              }
            >
              {renamedFrom
                ? t('datasetVersion.changesRenameLine', {
                    action,
                    entity: entityLabel(change.entity),
                    before: renamedFrom,
                    after: change.name || change.doc_id || '—',
                  })
                : change.meta_changes?.length
                  ? t('datasetVersion.changesMetadataLine', {
                      action,
                      name: change.name || change.doc_id || '—',
                    })
                  : t('datasetVersion.changesLine', {
                      action,
                      entity: entityLabel(change.entity),
                      name: change.name || change.doc_id || '—',
                    })}
            </p>
            {change.meta_changes?.length ? (
              <div className="space-y-1 pl-6">
                {change.meta_changes.map((field) => (
                  <p key={field.key} className="break-all text-sm">
                    {t('datasetVersion.configChangeLine', {
                      field: field.key,
                      // An absent side is the edit introducing or removing the
                      // key, and it reads as the panel's dash rather than as an
                      // empty value the key once held.
                      before: formatValue(field.before),
                      after: formatValue(field.after),
                    })}
                  </p>
                ))}
              </div>
            ) : null}
            {/* A wiki page edit stops at its sentence too: the page and the fact
                that it changed are the entry, and the text lives in the page's
                own history. */}
          </div>
        );
      })}
      {removedChunkCount > 0 && (
        <p className="pl-3 text-sm">
          {t('datasetVersion.chunkRemovedLine', { count: removedChunkCount })}
        </p>
      )}
    </div>
  );
}

// The list is a fixed narrow column, so its cells give back the table's default
// 16px side padding: on four columns that is ~64px, which is the difference
// between a change label that fits and one that is cut off. The first and last
// cells keep a little more on the outside edge, matching the table's own rhythm.
const CELL_PADDING = 'px-2 first-of-type:pl-3 last-of-type:pr-3';

// A nested field arrives as a path ("parser_config.chunk_token_num"): the first
// segment is the labelled section, the rest is the parameter's own name, which
// the server sends verbatim. Shared by the configuration block and the
// per-document parse settings, so one parameter reads the same in both.
const configFieldLabel = (field: string, t: TFunction) => {
  const separator = field.indexOf('.');
  if (separator < 0) {
    return t(`datasetVersion.field.${field}`, { defaultValue: field });
  }
  // A nested field is named by its own path alone
  // ("Extractor:AutoExtractDefault.keywords.top_n"): the parameter's name already
  // says which part of the configuration it belongs to, and prefixing the
  // section it lives in only repeats that.
  return field.slice(separator + 1);
};

// An avatar is a base64 data URI: printing it would flood the panel, so the line
// says whether one is set instead of showing its bytes.
const configValueLabel = (field: string, value: unknown, t: TFunction) => {
  if (field !== 'avatar') {
    return formatValue(value);
  }
  return typeof value === 'string' && value !== ''
    ? t('datasetVersion.configAvatarSet')
    : t('datasetVersion.configAvatarEmpty');
};

// A restore moves documents in two directions, and usually only one of them has
// anything in it: undoing an upload restores nothing, and a version whose
// documents are all still there removes nothing. The sentence therefore names
// only what actually moves — "restores 0 documents" reads like a failure, while
// it is the ordinary case of rolling back an upload.
//
// When neither direction moves anything there is nothing to add: the dialog
// shows its title alone rather than a sentence saying that nothing changes.
const RESTORE_PLAN_DOCUMENT_KEYS = {
  both: 'datasetVersion.restoreConfirmBoth',
  restoreOnly: 'datasetVersion.restoreConfirmRestoreOnly',
  deleteOnly: 'datasetVersion.restoreConfirmDeleteOnly',
} as const;

const restoreDocumentMovement = (
  t: TFunction,
  restored: number,
  deleted: number,
  keys: {
    both: string;
    restoreOnly: string;
    deleteOnly: string;
  },
) => {
  if (restored > 0 && deleted > 0) {
    return t(keys.both, { restored, deleted });
  }
  if (restored > 0) {
    return t(keys.restoreOnly, { restored });
  }
  if (deleted > 0) {
    return t(keys.deleteOnly, { deleted });
  }
  return '';
};

// The reasons the server gives for a document it could not put back. The codes
// come from the restore response and are stable; a code this build does not know
// is left out rather than printed raw.
const RESTORE_MISSING_REASON_LABELS: Record<string, string> = {
  no_chunk_history: 'datasetVersion.restoreMissingNoChunkHistory',
  removal_gone: 'datasetVersion.restoreMissingRemovalGone',
  replay_failed: 'datasetVersion.restoreMissingReplayFailed',
  history_unreadable: 'datasetVersion.restoreMissingHistoryUnreadable',
};

// restoreIncompleteDescription says which documents a restore left empty and, when
// the server said why, the distinct reasons behind them.
//
// The names are ids: a document this restore could not put back may have no row
// left to read a name from, and the response carries ids for the same reason. The
// list is bounded the way the panel bounds a document set - a count says the rest.
const restoreIncompleteDescription = (
  t: TFunction,
  result: IDatasetRestoreResult,
) => {
  const missing = result.missing_documents ?? [];
  const shown = missing.slice(0, 5).join(', ');
  const names = missing.length > 5 ? `${shown} +${missing.length - 5}` : shown;
  const reasons = Array.from(
    new Set(
      missing
        .map((id) => result.missing_document_reasons?.[id])
        .map((reason) => (reason ? RESTORE_MISSING_REASON_LABELS[reason] : ''))
        .filter((key) => key !== ''),
    ),
  ).map((key) => t(key));
  if (reasons.length === 0) {
    return t('datasetVersion.restoreIncompleteBody', { names });
  }
  return t('datasetVersion.restoreIncompleteBodyWithReasons', {
    names,
    reasons: reasons.join(', '),
  });
};

/**
 * ConfigChangeList renders the configuration fields a version moved, one line
 * per field ("similarity_threshold: 0.2 → 0.3"). A configuration version records
 * no document or chunk rows, so this is the change list the page lists for it.
 * The same lines serve a document's parse settings, indented one level further.
 */
function ConfigChangeList({
  changes,
  className = 'break-all pl-3 text-sm',
}: {
  changes: IDatasetConfigChange[];
  className?: string;
}) {
  const { t } = useTranslation();
  return (
    <>
      {changes.map((change) => (
        <p key={change.field} className={className}>
          {t('datasetVersion.configChangeLine', {
            field: configFieldLabel(change.field, t),
            before: configValueLabel(change.field, change.before, t),
            after: configValueLabel(change.field, change.after, t),
          })}
        </p>
      ))}
    </>
  );
}

/**
 * documentConfigChanges flattens the per-document parse settings a version moved
 * into one list, dropping a move that repeats: a bulk apply sets the same
 * parameter on every document it touches, and printing that line once per
 * document only hides the settings that differ. The order is the groups' own, so
 * a parameter keeps the place the first document gave it.
 */
function documentConfigChanges(groups: IDatasetDocumentConfigChange[]) {
  const seen = new Set<string>();
  const changes: IDatasetConfigChange[] = [];
  for (const group of groups) {
    for (const change of group.changes ?? []) {
      const key = JSON.stringify([change.field, change.before, change.after]);
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      changes.push(change);
    }
  }
  return changes;
}

/**
 * DocumentConfigChangeList renders what a document-configuration version moved:
 * the parse settings that differ from the ones they ran under before
 * ("chunk_token_num: 512 → 1024"), one line per distinct move.
 *
 * The documents themselves are named by the change sentences above this block,
 * so it does not repeat them as headings: a bulk apply would otherwise print the
 * same setting once per document, and a document whose name the server could not
 * resolve would head a group with its raw id. A document whose settings history
 * has no earlier row to compare against has no group at all.
 */
function DocumentConfigChangeList({
  groups,
}: {
  groups?: IDatasetDocumentConfigChange[];
}) {
  if (!groups?.length) {
    return null;
  }
  const changes = documentConfigChanges(groups);
  if (!changes.length) {
    return null;
  }
  return (
    <ConfigChangeList changes={changes} className="break-all pl-6 text-sm" />
  );
}

interface VersionRowProps {
  item: IDatasetVersion;
  selected: boolean;
  onSelect: (version: number) => void;
}

function VersionRow({ item, selected, onSelect }: VersionRowProps) {
  const { t } = useTranslation();
  const handleSelect = useCallback(
    () => onSelect(item.version),
    [item.version, onSelect],
  );
  // A restore names what it restored from: "Restore" alone does not say which
  // version the row is about, and that target is what a reader needs to judge it.
  // The server omits a zero target (v0 is the baseline), so a restore of the
  // initial state reads as "from v0" rather than losing its label.
  const kindLabel =
    item.kind === 'restore'
      ? t('datasetVersion.restoredFrom', { version: item.restored_from ?? 0 })
      : KIND_LABELS[item.kind]
        ? t(KIND_LABELS[item.kind])
        : item.kind;
  // Who made the change. The server resolves the stored id to a nickname, so the
  // column names the user rather than showing a raw id.
  const actor = versionActorLabel(item, t('datasetVersion.systemActor'));

  return (
    <TableRow
      role="row"
      tabIndex={0}
      aria-current={selected ? 'true' : undefined}
      data-state={selected ? 'selected' : undefined}
      onClick={handleSelect}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          handleSelect();
        }
      }}
      className={cn(
        'cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary',
        // The table's own selected state is the hover colour; a reader needs to
        // tell the two apart, so selection cuts the row back to the page surface.
        'data-[state=selected]:bg-bg-base',
      )}
    >
      {/* A row answers four questions — which version, when, what kind of change
          and who made it — and leaves the change itself to the right-hand
          panel. The cells keep the table's own 14px and take the document list's
          row height (py-3, not the tighter py-2), so the two tables read as one
          list rendered twice. */}
      <TableCell
        className={cn(
          CELL_PADDING,
          'w-[80px] whitespace-nowrap py-3 font-medium tabular-nums',
          // The accent bar has to sit on the cell: the table is not collapsed, so
          // a border on the row itself would not paint.
          selected
            ? 'border-l-2 border-l-primary text-text-primary'
            : 'border-l-2 border-l-transparent text-text-secondary',
        )}
      >
        {item.version}
      </TableCell>
      {/* Same timestamp format as the log pages: DD/MM/YYYY HH:mm:ss, in the
          reader's time zone — the millisecond column carries the instant. The
          timestamp never wraps: a two-line date would break the column rhythm. */}
      <TableCell
        className={cn(
          CELL_PADDING,
          'w-[168px] whitespace-nowrap py-3 tabular-nums text-text-secondary',
        )}
      >
        {formatDate(item.create_time)}
      </TableCell>
      {/* The change column is the flexible one, so it is the one that gets cut
          when the list is narrow: the full label is on the cell's title, and the
          labels themselves are kept short enough to fit at the default width. */}
      <TableCell
        className={cn(
          CELL_PADDING,
          'max-w-0 truncate py-3 text-text-secondary',
        )}
        title={kindLabel}
      >
        {kindLabel}
      </TableCell>
      <TableCell
        className={cn(
          CELL_PADDING,
          'max-w-0 truncate py-3 text-text-secondary',
        )}
        title={actor || undefined}
      >
        {actor || '—'}
      </TableCell>
    </TableRow>
  );
}

function DatasetVersionHistory({
  datasetId,
  kinds,
  className,
  listClassName = 'w-[480px]',
}: DatasetVersionHistoryProps) {
  const { t } = useTranslation();
  const [limit, setLimit] = useState(VERSION_PAGE_SIZE);
  // A different filter is a different timeline, so its page starts over: keeping
  // a limit the reader grew on the previous filter would read a page this list
  // never asked for. The key is the joined kinds so a fresh array per render is
  // not mistaken for a change.
  const kindKey = kinds?.join(',') ?? '';
  useEffect(() => {
    setLimit(VERSION_PAGE_SIZE);
  }, [kindKey]);
  const {
    versions,
    currentVersion,
    loading,
    refetch: refetchVersions,
  } = useFetchDatasetVersions(datasetId, {
    kinds,
    limit,
  });
  const [selectedVersion, setSelectedVersion] = useState<number | undefined>();
  const {
    detail,
    loading: detailLoading,
    missingVersion,
  } = useFetchDatasetVersion(datasetId, selectedVersion);
  // The list scrolls over the rows loaded so far, so reaching its end is what
  // asks for the next page - the reader is already making that gesture, and a
  // button would only make them repeat it. A full page means there is probably
  // more; the server's ceiling is where asking again would return the same rows.
  const listRef = useRef<HTMLDivElement>(null);
  const hasMore = versions.length >= limit && limit < MAX_VERSION_PAGE;
  useEffect(() => {
    const node = listRef.current;
    if (!node || !hasMore) {
      return undefined;
    }
    const handleScroll = () => {
      // A page is already being read: the taller limit is on its way, and asking
      // again would only recompute the same query key.
      if (loading) {
        return;
      }
      const remaining = node.scrollHeight - node.scrollTop - node.clientHeight;
      if (remaining > VERSION_SCROLL_THRESHOLD) {
        return;
      }
      setLimit((current) =>
        Math.min(current + VERSION_PAGE_SIZE, MAX_VERSION_PAGE),
      );
    };
    node.addEventListener('scroll', handleScroll, { passive: true });
    return () => node.removeEventListener('scroll', handleScroll);
  }, [hasMore, loading]);
  const { restoreDatasetVersion, loading: restoring } =
    useRestoreDatasetVersion();
  const { checkRestore } = useCheckDatasetVersionRestore();
  const { deleteDatasetVersion, loading: deleting } = useDeleteDatasetVersion();
  // Why the last restore attempt was refused before it started, if it was: a
  // document is queued or parsing, and its run writes the same chunks the
  // restore would rebuild.
  const [restoreBlocked, setRestoreBlocked] = useState<string | undefined>();
  // The dry run's plan, shown in the confirmation before anything is written.
  const [restorePlan, setRestorePlan] = useState<
    IDatasetRestoreResult | undefined
  >();
  const [restoreOpen, setRestoreOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  // Whether the file list is showing every name or only the head of it. Kept per
  // panel rather than per version, and reset when the reader moves to another
  // version: the fold is about this version's file set.
  const [documentsExpanded, setDocumentsExpanded] = useState(false);

  // What the action bar acts on: the row the list has highlighted.
  const actionVersion = selectedVersion;
  const actionKind = versions.find(
    (item) => item.version === actionVersion,
  )?.kind;

  // Open on the newest version; also covers the list arriving after mount.
  useEffect(() => {
    if (selectedVersion === undefined && versions.length > 0) {
      setSelectedVersion(versions[0].version);
    }
  }, [selectedVersion, versions]);

  // openRestoreConfirm asks the server what the restore would do and shows it.
  // Every kind of version restores the documents as well as the configuration, so
  // a restore can remove the documents uploaded after the version: the plan has to
  // be seen before it is committed, not reported afterwards.
  const openRestoreConfirm = useCallback(async () => {
    if (actionVersion === undefined) {
      return;
    }
    // A restore rebuilds a document's chunks, and a parse writes the same
    // chunks: the two would interleave and leave a partial document behind.
    // The dry run answers whether that is the case right now, so the refusal
    // is shown before anything is written.
    const check = await checkRestore({
      dataset_id: datasetId,
      version: actionVersion,
    }).catch(() => undefined);
    if (check?.code === 0 && check.data?.blocked_reason) {
      setRestoreBlocked(check.data.blocked_reason);
      return;
    }
    setRestorePlan(check?.code === 0 ? check.data : undefined);
    setRestoreOpen(true);
  }, [actionVersion, checkRestore, datasetId]);

  const handleRestore = useCallback(async () => {
    if (actionVersion === undefined) {
      return;
    }
    // The head can move while the panel sits open — an upload, a parse or a
    // delete anywhere else appends a version — and a restore is refused when the
    // version it was pinned to is no longer the head. Re-read it first so the
    // restore is pinned to the dataset as it is now; the server re-checks under
    // its own lock, so a race in between still cannot overwrite anything.
    const fresh = await refetchVersions();
    const freshHead =
      fresh.data?.code === 0 ? fresh.data.data?.current_version : undefined;
    let result;
    try {
      result = await restoreDatasetVersion({
        dataset_id: datasetId,
        version: actionVersion,
        expected_version: freshHead ?? currentVersion,
      });
    } catch {
      // A refusal here means the dataset moved again between the re-read and the
      // write. The list above now misstates the head, so refresh it and leave the
      // question open; the request layer has already reported the failure.
      refetchVersions();
      return;
    }
    // The restore wrote a version of its own: pin the panel to it, so the reader
    // lands on the state the restore produced rather than on whichever row the
    // refreshed list happens to put first.
    if (result?.code === 0 && result.data) {
      // A document the restore could not fill is only named in this answer: the
      // version that records the restore describes the state it produced, and a
      // document left with no chunks is part of that state, not a change. Say it
      // now, while the request is the reader's own, rather than leaving them to
      // notice an empty document later.
      const missing = result.data.missing_documents ?? [];
      if (missing.length > 0) {
        toast({
          variant: 'destructive',
          title: t('datasetVersion.restoreIncompleteTitle', {
            count: missing.length,
          }),
          description: restoreIncompleteDescription(t, result.data),
        });
      }
      setSelectedVersion(result.data.version);
    } else {
      // No version came back: let the refreshed history pick the new head.
      setSelectedVersion(undefined);
    }
  }, [
    actionVersion,
    currentVersion,
    datasetId,
    refetchVersions,
    restoreDatasetVersion,
    t,
  ]);

  const handleDelete = useCallback(async () => {
    if (actionVersion === undefined) {
      return;
    }
    const result = await deleteDatasetVersion({
      dataset_id: datasetId,
      version: actionVersion,
    });
    if (result?.code === 0) {
      setDeleteOpen(false);
      // Fall back to the refreshed newest row: the deleted version's detail can
      // no longer be loaded.
      setSelectedVersion(undefined);
    }
  }, [actionVersion, datasetId, deleteDatasetVersion]);

  const selectedKind = versions.find(
    (item) => item.version === selectedVersion,
  )?.kind;
  // The detail carries the kind even when the version is not in the current
  // page of the list (a restore lands on a version the filter may hide).
  const detailKind = detail?.kind ?? selectedKind;
  const isContentVersion = !!detailKind && !CONFIGURATION_KINDS.has(detailKind);
  // A restore's snapshot is the state the restore produced, not an edit a reader
  // made: what they need from it is the version it came from, not a second copy
  // of the configuration the source version already lists. The source version is
  // reported as what the version did, in the change section, and it is the whole
  // report: the rows underneath would restate what that version's own history
  // records.
  const isRestoreVersion = detailKind === 'restore';
  // The document set the version held. Empty for a version written before the
  // document set was recorded, which hides the block rather than heading an empty
  // list.
  const documents = detail?.documents ?? [];
  // The names the panel shows: the first few, or all of them once the reader
  // opened the fold. The count the heading reports is always the whole set.
  const visibleDocuments = documentsExpanded
    ? documents
    : documents.slice(0, DOCUMENTS_PREVIEW_LIMIT);
  // A new selection folds the list again: the fold is about the set the reader is
  // looking at, not a preference carried between versions.
  useEffect(() => {
    setDocumentsExpanded(false);
  }, [detail?.version]);
  // Both kinds are restorable: a configuration version writes its snapshot
  // back, a content version rebuilds its document set. The server picks the
  // path from the version's kind.
  // A configuration version records no document or chunk change rows: what it
  // changed is the configuration itself, and the block below shows the result.
  // It still gets the change section, holding one line that says so, because a
  // missing section would read as a lost change list rather than as "only the
  // configuration moved".
  // What a configuration version moved, field by field. A configuration version
  // has no document or chunk rows, so these lines are its whole change list; a
  // version whose predecessor is gone has none, and gets the one-line summary.
  const configChanges = detail?.config_changes ?? [];
  // What a document-configuration version moved, one group per document: the
  // version names the documents it touched and the server reads their parse
  // settings history to say which parameters moved under each of them.
  const documentConfigChanges = detail?.document_config_changes ?? [];
  // The documents the parse settings block speaks for, by name rather than by
  // id: the head line names them, and their plain sentences are skipped so the
  // same fact is not stated twice. A document the server could not name is left
  // out of the sentence rather than printed as a raw id - its settings still
  // appear below, and the line falls back to naming no document at all.
  const describedDocuments = documentConfigChanges
    .map((group) => group.doc_name?.trim())
    .filter((name): name is string => !!name);
  const describedDocIDs = new Set(
    documentConfigChanges.map((group) => group.doc_id),
  );
  const configurationChanged =
    detailKind === 'dataset_config' &&
    !detail?.changes?.length &&
    !configChanges.length;
  // The baseline is the state the history starts from rather than a change of its
  // own, so it carries no such line.
  const showChanges =
    !!detail &&
    (!!detail.changes?.length ||
      isContentVersion ||
      configChanges.length > 0 ||
      configurationChanged ||
      isRestoreVersion);
  // The writer stores at most its own limit of entries and keeps the true count in
  // changes_total, so a shorter list is a sample: say so rather than let a bulk
  // operation read as a small one.
  const storedChanges = detail?.changes?.length ?? 0;
  const changesTotal = detail?.changes_total ?? 0;
  const changesTruncated = changesTotal > storedChanges;
  const restorable =
    actionVersion !== undefined && actionVersion !== currentVersion;
  // What the confirmation says below its title. Empty when the plan moves no
  // document: the title names the version, and a sentence about nothing moving
  // would only fill the dialog.
  const restoreMovement = restorePlan
    ? restoreDocumentMovement(
        t,
        restorePlan.restored_documents?.length ?? 0,
        restorePlan.deleted_documents?.length ?? 0,
        RESTORE_PLAN_DOCUMENT_KEYS,
      )
    : '';

  // Two versions cannot be dropped: the current one, because the dataset's
  // version column points at it, and the baseline, because nothing precedes it.
  // The server enforces the same rule — this only keeps the button honest.
  const deletable = restorable && actionKind !== 'baseline';

  return (
    <div className={cn('flex min-h-0 flex-1 flex-col gap-3', className)}>
      <div className="flex min-h-0 flex-1 gap-4">
        <aside
          className={cn(
            'flex min-h-0 shrink-0 flex-col overflow-hidden rounded-md border-0.5 border-border-button',
            listClassName,
          )}
        >
          {loading && versions.length === 0 && (
            <div className="flex justify-center py-8">
              <Spin size="large" />
            </div>
          )}
          {!loading && versions.length === 0 && (
            <p className="p-3 text-sm text-text-secondary">
              {t('datasetVersion.noVersions')}
            </p>
          )}
          {versions.length > 0 && (
            // Same table primitives as the log pages: one sticky header on the
            // card surface, 14px cells, a rule between rows.
            <Table rootRef={listRef} rootClassName="min-h-0 flex-1">
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className={cn(CELL_PADDING, 'w-[80px]')}>
                    {t('datasetVersion.columnVersion')}
                  </TableHead>
                  <TableHead
                    className={cn(CELL_PADDING, 'w-[168px] whitespace-nowrap')}
                  >
                    {t('datasetVersion.columnTime')}
                  </TableHead>
                  <TableHead className={CELL_PADDING}>
                    {t('datasetVersion.columnKind')}
                  </TableHead>
                  {/* The label is a fixed word ("Created by"), so it must not
                      wrap: the width is a hint to the layout, and a squeezed
                      column would break the header over two lines. */}
                  <TableHead
                    className={cn(CELL_PADDING, 'w-[112px] whitespace-nowrap')}
                  >
                    {t('datasetVersion.columnCreatedBy')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {versions.map((item) => (
                  <VersionRow
                    key={item.version}
                    item={item}
                    selected={item.version === selectedVersion}
                    onSelect={setSelectedVersion}
                  />
                ))}
              </TableBody>
            </Table>
          )}
          {/* The next page arrives after a scroll to the bottom, so the tail of
              the list says so instead of looking like the end. */}
          {versions.length > 0 && loading && (
            <div className="flex shrink-0 justify-center py-2">
              <Spin size="small" />
            </div>
          )}
          {/* The actions belong to the row the list has highlighted, so they sit
              under the list rather than under the panel: one place to read, one
              place to act. */}
          {versions.length > 0 && (
            <div className="flex items-center justify-end gap-2 border-t-0.5 border-border-button p-2">
              <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
                <AlertDialogTrigger asChild>
                  <Button
                    variant="outline"
                    size="sm"
                    // The label carries the danger, not only the confirmation
                    // inside: with both labels cut to one word, the two buttons
                    // under the list would otherwise read as the same kind of
                    // action. hover/focus are restated because outline's own
                    // colour outranks the base one.
                    className="text-state-error hover:text-state-error focus-visible:text-state-error"
                    disabled={!deletable || deleting}
                  >
                    {t('datasetVersion.delete')}
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent aria-describedby={undefined}>
                  <AlertDialogHeader>
                    <AlertDialogTitle>
                      {t('datasetVersion.deleteTitle', {
                        version: actionVersion ?? 0,
                      })}
                    </AlertDialogTitle>
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
              {/* A restore moves documents as well as configuration for every
                  kind of version, so it never runs straight from the click: the
                  dry run's plan - what comes back, what is removed - is put in
                  front of the reader first. Restoring a configuration version can
                  therefore remove the documents uploaded after it, visibly, rather
                  than as a surprise afterwards. */}
              <AlertDialog open={restoreOpen} onOpenChange={setRestoreOpen}>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>
                      {t('datasetVersion.restoreConfirmTitle', {
                        version: actionVersion ?? 0,
                      })}
                    </AlertDialogTitle>
                    {restoreMovement !== '' && (
                      <AlertDialogDescription>
                        {restoreMovement}
                      </AlertDialogDescription>
                    )}
                  </AlertDialogHeader>
                  {!!restorePlan?.changes?.length && (
                    <p className="text-sm text-text-secondary">
                      {t('datasetVersion.restoreConfirmConfig', {
                        count: restorePlan.changes.length,
                      })}
                    </p>
                  )}
                  <AlertDialogFooter>
                    <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
                    <AlertDialogAction
                      disabled={restoring}
                      onClick={handleRestore}
                    >
                      {t('common.confirm')}
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
              <Button
                variant="outline"
                size="sm"
                onClick={openRestoreConfirm}
                disabled={restoring || !restorable}
                // The panel no longer carries a note about what a restore does;
                // the one case that needs an explanation - the row selected is
                // already the head, so there is nothing to go back to - says it
                // on the disabled button instead of in a sentence on the page.
                title={
                  !restorable &&
                  actionVersion !== undefined &&
                  actionVersion === currentVersion
                    ? t('datasetVersion.restoreCurrentTip')
                    : undefined
                }
              >
                {t('datasetVersion.restore')}
              </Button>{' '}
              {/* A refusal is its own dialog rather than a toast: it names the
                  documents that have to stop, and the reader needs that list to
                  act on it. */}
              <AlertDialog
                open={restoreBlocked !== undefined}
                onOpenChange={(open) => {
                  if (!open) {
                    setRestoreBlocked(undefined);
                  }
                }}
              >
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>
                      {t('datasetVersion.restoreBlockedTitle')}
                    </AlertDialogTitle>
                    <AlertDialogDescription>
                      {restoreBlocked}
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogAction
                      onClick={() => setRestoreBlocked(undefined)}
                    >
                      {t('common.confirm')}
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </div>
          )}
        </aside>

        {/* The panel reads at the same 14px as the tables beside it (the shared
            table primitive's text-sm): a larger body here made the list look like
            a footnote beside it. Hierarchy comes from weight and colour instead. */}
        <section className="flex min-h-0 min-w-0 flex-1 flex-col">
          <Spin spinning={detailLoading} className="flex-1 min-h-0">
            <div className="flex h-full flex-col gap-2">
              {/* One blank line between blocks: a heading needs air above it
                    or it reads as the last line of the block before. */}
              <div className="flex-1 space-y-4 overflow-y-auto">
                {missingVersion && (
                  <p className="text-sm text-text-secondary">
                    {t('datasetVersion.versionMissing')}
                  </p>
                )}
                {/* Three blocks in one order: what the version covers, what
                      it changed, and the configuration it stored. Each block is
                      a heading over its own value, so a reader scans headings
                      and only reads the block they came for. Who made the
                      change is the list's column, so the panel does not repeat
                      it. */}
                {detail && (
                  <section className="space-y-1 pl-2">
                    {/* One line, never wrapped: the head marker beside the
                          heading it qualifies, not stranded under it when the
                          panel is narrow. */}
                    <div className="flex items-center gap-2">
                      <h3 className="shrink-0 text-sm font-medium text-text-primary">
                        {t('datasetVersion.summaryTitle')}
                      </h3>
                      {/* Whether this version is the head qualifies the whole
                            summary rather than any one value in it, so the chip
                            rides on the heading line: it stays readable when
                            the counts line wraps, and a content version — which
                            has no configuration block below to carry the fact —
                            still shows it. Beside the heading rather than at
                            the panel's edge: the panel is much wider on the
                            full page than in the sheet, and a chip pinned right
                            would drift away from the block it describes. */}
                      {detail.version === currentVersion && (
                        <span className="rounded-sm bg-bg-card px-1 text-sm text-text-secondary">
                          {t('datasetVersion.latest')}
                        </span>
                      )}
                    </div>
                    <p className="pl-3 text-sm">
                      {/* The document set is the counter: it is what the
                            detail response returns, and no separate number is
                            stored to drift from it. */}
                      {t('datasetVersion.counts', {
                        count: documents.length,
                      })}
                    </p>
                  </section>
                )}
                {documents.length > 0 && (
                  <section className="space-y-1 pl-2">
                    <h3 className="text-sm font-medium text-text-primary">
                      {t('datasetVersion.documentsTitle')}
                    </h3>
                    {/* The document set comes before the change set: a reader
                          asks what the version was made of before what moved in
                          it. The names are what a version stores; a document a
                          version held without chunks is not flagged here - a
                          version taken before the document was parsed held none
                          because none had been produced yet, not because a
                          restore lost them. */}
                    <ul className="space-y-1 pl-3">
                      {visibleDocuments.map((document) => (
                        <li
                          key={document.id}
                          className="flex items-baseline gap-2 text-sm"
                        >
                          <span
                            className="min-w-0 flex-1 truncate"
                            title={document.name || document.id}
                          >
                            {document.name || document.id}
                          </span>
                        </li>
                      ))}
                    </ul>
                    {documents.length > DOCUMENTS_PREVIEW_LIMIT && (
                      // The toggle sits under the names at the same indent, so the
                      // list still reads as one block: a big set is folded, not
                      // truncated without saying so.
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="ml-1 self-start text-sm"
                        onClick={() => setDocumentsExpanded((open) => !open)}
                      >
                        {documentsExpanded
                          ? t('datasetVersion.documentsShowLess')
                          : t('datasetVersion.documentsShowAll', {
                              count: documents.length - DOCUMENTS_PREVIEW_LIMIT,
                            })}
                      </Button>
                    )}
                  </section>
                )}
                {showChanges && (
                  <section className="space-y-1 pl-2">
                    <h3 className="text-sm font-medium text-text-primary">
                      {t('datasetVersion.changesTitle')}
                    </h3>
                    {/* A restore's change is the state it put back, so the
                          version it came from says the whole of it: restating
                          the documents it moved would only repeat what that
                          version's own history already records, one line above
                          the document set this version holds. */}
                    {isRestoreVersion ? (
                      <p className="pl-3 text-sm">
                        {t('datasetVersion.restoredFrom', {
                          version: detail?.restored_from ?? 0,
                        })}
                      </p>
                    ) : (
                      <>
                        <VersionChangeList
                          changes={detail?.changes}
                          coveredDocIDs={describedDocIDs}
                        />
                        {changesTruncated && (
                          // The list stops at the writer's limit; the count beside it
                          // is the whole change. Said here, right after the rows, so
                          // the reader knows the list ends early before scrolling on.
                          <p className="pl-3 text-sm text-text-secondary">
                            {t('datasetVersion.changesTruncated', {
                              shown: storedChanges,
                              total: changesTotal,
                            })}
                          </p>
                        )}
                        {/* The head line names what moved, and the fields that
                            moved hang under it: a configuration version's whole
                            change set is the configuration, so the fields are its
                            rows rather than a block of their own. */}
                        {detailKind === 'dataset_config' && (
                          <p className="pl-3 text-sm">
                            {t('datasetVersion.changesConfiguration')}
                          </p>
                        )}
                        {configChanges.length > 0 && (
                          <ConfigChangeList
                            changes={configChanges}
                            className="break-all pl-6 text-sm"
                          />
                        )}
                        {detailKind === 'document_config' &&
                          documentConfigChanges.length > 0 && (
                            <p className="pl-3 text-sm">
                              {describedDocuments.length > 0
                                ? t(
                                    'datasetVersion.changesDocumentConfigurationFor',
                                    {
                                      documents: describedDocuments.join(', '),
                                    },
                                  )
                                : t(
                                    'datasetVersion.changesDocumentConfiguration',
                                  )}
                            </p>
                          )}
                        <DocumentConfigChangeList
                          groups={documentConfigChanges}
                        />
                        {isContentVersion && !detail?.changes?.length && (
                          <p className="pl-3 text-sm text-text-secondary">
                            {t('datasetVersion.changesEmpty')}
                          </p>
                        )}
                      </>
                    )}
                  </section>
                )}
              </div>
            </div>
          </Spin>
        </section>
      </div>
    </div>
  );
}

export default DatasetVersionHistory;
