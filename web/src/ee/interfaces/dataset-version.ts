/**
 * Types of a dataset's configuration history. The feature only exists in the
 * enterprise edition, so the types live here instead of the shared
 * `@/interfaces/database/dataset` module.
 *
 * Versions are append-only: every configuration change adds one, and restoring
 * an old version adds another (the state it produced, so a restore is undone by
 * restoring the version before it).
 */

// DatasetVersionKind records what produced a version: the configuration kinds
// (baseline, dataset_config, document_config, restore) and the content kinds
// (upload, delete, parse, chunk, chunk_add, chunk_update, chunk_delete,
// chunk_restore, chunk_enable, chunk_disable, document_rename, document_meta,
// document_status, document_enable, document_disable, artifact_edit).
//
// The values fit the backend's varchar(16) kind columns, which is why the
// metadata kind is spelled document_meta rather than document_metadata.
export type DatasetVersionKind =
  | 'baseline'
  | 'dataset_config'
  | 'restore'
  | 'upload'
  | 'delete'
  | 'parse'
  // The unnamed chunk kind: rows written before the four below existed, and a
  // batch that mixed actions, which no single kind can describe.
  | 'chunk'
  | 'chunk_add'
  | 'chunk_update'
  | 'chunk_delete'
  // A chunk restored from its own history. It is not the document-level
  // 'restore': a replay of this version rebuilds one chunk, not a document set.
  | 'chunk_restore'
  // A chunk switched on or off: its visibility is what left or rejoined
  // retrieval, and the chunk's own row is what the version recorded. The change
  // set keeps the edit action, so the direction is the kind's alone.
  | 'chunk_enable'
  | 'chunk_disable'
  | 'document_config'
  | 'document_rename'
  | 'document_meta'
  // A document switched on or off: its chunks leave or rejoin retrieval, and the
  // version's change set names the documents it moved. `document_status` is the
  // unnamed kind: rows written before the two below existed, and a switch to a
  // status that is neither enabled nor disabled.
  | 'document_status'
  // The two directions of that switch. A batch names one status for every
  // document it moves, so one kind describes the whole version.
  | 'document_enable'
  | 'document_disable'
  // A hand edit of a compiled wiki page (an artifact): the page keeps its own
  // commit history, and the version is the timeline entry beside the documents.
  | 'artifact_edit';

// IDatasetVersion is one row of the history list. It carries no counters: how
// many documents the version held is the length of its document set, which the
// detail response returns.
export interface IDatasetVersion {
  version: number;
  kind: DatasetVersionKind | string;
  // restored_from is the version a restore copied from; absent for a plain
  // configuration change.
  restored_from?: number;
  created_by: string;
  // created_by_name is the actor's display name, resolved server side from
  // created_by. It is empty for a version the system wrote, or when the user no
  // longer exists; render created_by only as the fallback.
  created_by_name?: string;
  // create_time is the version's instant in milliseconds — the only timestamp a
  // version records, so format it in the reader's time zone.
  create_time: number;
}

// IDatasetVersionList is the history page plus the dataset's current version,
// so the panel can mark the head and disable restoring onto itself.
export interface IDatasetVersionList {
  dataset_id: string;
  current_version: number;
  versions: IDatasetVersion[];
}

// IDatasetVersionChange is one document or chunk a version touched. A content
// version may carry none: an old version predates the change set, and a mass
// change is truncated server side.
export interface IDatasetVersionChange {
  entity: 'document' | 'chunk' | string;
  action:
    | 'add'
    | 'update'
    | 'delete'
    | 'parse'
    | 'restore'
    | 'enable'
    | 'disable'
    | string;
  doc_id?: string;
  // chunk_id names the chunk a chunk entry points at. A document entry has none:
  // its doc_id is the document, and repeating it in a second column said nothing.
  chunk_id?: string;
  // chunk_version is the chunk version this entry produced - the number the
  // chunk's own history carries for the state after the change. The server reads
  // it back on the pair it stored (chunk id and dataset version), because the
  // history row is anchored at the version and cannot exist when the set is
  // written. Absent when the history has no such row.
  chunk_version?: number;
  // name is a document's name, captured when the change happened because a
  // deleted document has no row left to read one from. A chunk entry carries
  // none: it names the chunk and its document, and the text that chunk held at
  // this version is read from the chunk's own history rather than repeated here.
  name?: string;
  // previous_name is what the document was called before a rename entry moved
  // it. Only rename entries carry it, and only rows written since the field
  // exists: a rename reads "old name to new name" because the new one alone says
  // nothing about what moved.
  previous_name?: string;
  // doc_name names the document a chunk entry belongs to; the server resolves it
  // on read, and leaves it empty when the document is gone.
  doc_name?: string;
  // meta_changes is what a metadata version moved, key by key. Only those
  // versions carry it: the values live in the metadata store, which has no diff
  // of its own, so the change set is the only place the edit is written down.
  // The server bounds it (twenty keys, and each value to a display length).
  meta_changes?: IMetadataFieldChange[];
  // commit_id names the page commit an artifact entry points at. Stored rather
  // than read from the entry's own body so the panel can open the exact edit.
  commit_id?: string;
}

// IMetadataFieldChange is one metadata key a version moved: the value before the
// edit and the value after it. One side is absent when the edit introduced the
// key (no before) or removed it (no after) - an absent side and an empty value
// are different things, so the page reads the missing field, not falsiness.
export interface IMetadataFieldChange {
  key: string;
  // Raw JSON as the API returned it: a string, a number, a boolean or a list.
  before?: unknown;
  after?: unknown;
}

// IDatasetVersionDocument is one document the version held: its id and the name
// it had. A version stores the document set only when the set itself moves, so a
// count stored with it would belong to the version that recorded the set rather
// than to this one.
export interface IDatasetVersionDocument {
  id: string;
  name?: string;
  // chunk_num is how many chunks the document holds now, and restored_version is
  // the version its content was replayed from. Both are reported for a restore
  // version only, and they describe the document today, not at this version. A
  // zero chunk count is not a warning the panel draws: a version taken before the
  // document was parsed held no chunk for it, which is that version's state, and
  // the restore response already names the documents it could not fill. A
  // document the dataset no longer holds has neither: its content is unknown,
  // which is not the same as empty.
  chunk_num?: number;
  restored_version?: number;
}

// IDatasetVersionDetail is one version with its full configuration. The keys
// are the snapshot's own names (name, description, parser_config, ...); the
// panel renders them as field labels.
export interface IDatasetVersionDetail extends IDatasetVersion {
  config: Record<string, unknown>;
  documents?: IDatasetVersionDocument[];
  changes?: IDatasetVersionChange[];
  // changes_total is how many entries the version recorded before the writer cut
  // the stored set at its storage limit. A `changes` shorter than this is a
  // sample: the panel says so instead of presenting the first hundred rows of a
  // hundred-thousand-row change as the whole change.
  changes_total?: number;
  // config_changes is what a configuration version moved, field by field: such a
  // version records no document or chunk rows, so this is its change list.
  config_changes?: IDatasetConfigChange[];
  // document_config_changes is what a document-configuration version moved, one
  // group per document: which of that document's parse settings differ from the
  // settings it ran under before. The version itself stores only the documents,
  // so the server reads their settings history when the panel opens.
  document_config_changes?: IDatasetDocumentConfigChange[];
}

// IDatasetDocumentConfigChange is one document whose parse settings a version
// moved, with the fields that moved.
export interface IDatasetDocumentConfigChange {
  doc_id: string;
  // doc_name is the document's name, resolved server side; it is empty when the
  // document is gone, so render doc_id only as the fallback.
  doc_name?: string;
  // changes are the settings that differ from what the document ran under
  // before, in the server's order.
  changes: IDatasetConfigChange[];
}

// IDatasetConfigChange is one field that differs between two versions.
export interface IDatasetConfigChange {
  field: string;
  before: unknown;
  after: unknown;
}

// IDatasetRestoreResult reports what a restore did (or, with dry_run, would do).
// pre_version is the head the restore replaced: restoring that version is how the
// restore is undone.
//
// A restore puts the whole recorded state back, so the answer describes the
// documents that move; the configuration itself is not itemised.
// `missing_documents` names the documents the version held but whose content
// could not be put back: their removal row was cleaned up, the version predates
// the document set, or replaying their chunks failed. Such a document may still
// exist with no chunks until it is re-parsed.
export interface IDatasetRestoreResult {
  version: number;
  pre_version: number;
  applied: boolean;
  restored_documents?: string[];
  deleted_documents?: string[];
  missing_documents?: string[];
  // missing_document_reasons explains each entry of missing_documents: why the
  // content could not be put back (no_chunk_history, removal_gone, replay_failed,
  // history_unreadable). The list says which documents to look at; the code says
  // whether a re-parse would help.
  missing_document_reasons?: Record<string, string>;
  // restored_chunks counts the chunks replayed from the chunk history, which is
  // what puts a restored document's content back without a re-parse.
  restored_chunks?: number;
  // blocked_reason says why a restore is refused right now: a document is
  // queued or parsing, and its run writes the chunks the restore would rebuild.
  // A dry run answers with it instead of a plan, so the panel can refuse the
  // action before the user commits.
  blocked_reason?: string;
}

// IDatasetDeleteResult reports what the history holds after a delete. Versions
// keep their numbers, so the list refreshes with a gap rather than a renumber.
export interface IDatasetDeleteResult {
  dataset_id: string;
  deleted_version: number;
  current_version: number;
}
