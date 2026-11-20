/**
 * Types of a source chunk's version history. The feature only exists in the
 * enterprise edition, so the types live here instead of the shared
 * `@/interfaces/database/dataset` module.
 */

// IChunkVersion is one entry of a version history. Versions are append-only:
// every edit adds one, and restoring an old version adds another.
export interface IChunkVersion {
  version: number;
  created_by: string;
  // created_by_name is the actor's display name, resolved server side from
  // created_by. It is empty for a version the system wrote (the parse baseline,
  // a delete marker) or when the user no longer exists; render created_by only
  // as the fallback.
  created_by_name?: string;
  // restored_from is the version a restore copied from. 0 is both a plain edit
  // and a restore of the v0 baseline, so check kind === 'restore' rather than
  // the numeric value.
  restored_from: number;
  // create_time is the version's instant in milliseconds — the only timestamp
  // a version records, so format it in the reader's time zone like the dataset
  // version list does.
  create_time: number;
  // create_date is the legacy server-formatted string, kept for older clients.
  create_date?: string;
  // latest marks the newest version, i.e. the state the last edit left.
  latest: boolean;
  // kind is what produced the row: parse (the parser's baseline), add (a chunk
  // added by hand), edit, delete (a removal marker) or restore.
  kind?: string;
}

export interface IChunkVersionList {
  chunk_id: string;
  total: number;
  versions: IChunkVersion[];
}

// IChunkVersionSnapshot is the versioned field set a version stored. Tokens and
// the embedding vector are not versioned: they are derived from the text, so a
// restore recomputes them.
//
// available_int is versioned: enabling or disabling a chunk opens a version of
// its own and a restore puts the switch back. Only the hiding 0 is stored, so a
// version without the key describes a visible chunk - which is also the default
// a restore applies to a history written before visibility was versioned.
export interface IChunkVersionSnapshot {
  content_with_weight?: string;
  important_kwd?: string[];
  question_kwd?: string[];
  tag_feas?: Record<string, number>;
  // available_int is 0 for a hidden chunk; absent means visible.
  available_int?: number;
  // tag_kwd is the tag list the editor shows; tag_feas carries the weights a
  // match reads. A version stores both, so a restore puts back what the chunk
  // showed as well as what it matched.
  tag_kwd?: string[];
  // img_id is the picture this version recorded; empty when it had none.
  img_id?: string;
  // doc_type_kwd is "image" for a chunk that is a picture (and "text"
  // otherwise). The preview reads it to show the picture as the body instead of
  // the caption stored in content_with_weight.
  doc_type_kwd?: string;
}

export interface IChunkVersionDetail {
  version: number;
  created_by: string;
  // created_by_name is resolved the same way as in the list entry.
  created_by_name?: string;
  // Same meaning as the list: 0 can be a restore of v0, so use kind.
  restored_from: number;
  // Same raw millisecond instant as the list; the client formats it locally.
  create_time: number;
  // create_date is the legacy server-formatted string, kept for older clients.
  create_date?: string;
  latest: boolean;
  // kind is what produced the row: parse (the parser's baseline), add (a chunk
  // added by hand), edit, delete (a removal marker) or restore. The baselines
  // and the removal marker are protected from deletion.
  kind?: string;
  chunk: IChunkVersionSnapshot;
}

// How a changed field is rendered: a unified diff for text, element-level
// additions/removals for lists, before/after for scalars, and a changed flag
// only for structured values (tag weights, positions).
export type ChunkVersionDiffFieldKind = 'text' | 'list' | 'scalar' | 'json';

// IChunkVersionFieldDiff is one field that changed between two versions. Only
// changed fields are returned, so an entry itself means "this field differs".
export interface IChunkVersionFieldDiff {
  field: string;
  kind: ChunkVersionDiffFieldKind;
  // changed is false for a list field included only to show the values both
  // versions kept. Optional because a server built before the flag existed
  // omits it; list rendering derives the same answer from added/removed.
  changed?: boolean;
  diff?: string;
  added?: string[];
  removed?: string[];
  // unchanged holds the list values both versions kept, so the whole list can
  // be rendered with the changes marked.
  unchanged?: string[];
  before?: unknown;
  after?: unknown;
}

export interface IChunkVersionDiff {
  chunk_id: string;
  from: number;
  to: number;
  identical: boolean;
  fields: IChunkVersionFieldDiff[];
}

// IChunkVersionDeleteResult reports the history after a delete. Versions keep
// their numbers, so the list refreshes with a gap rather than a renumber.
export interface IChunkVersionDeleteResult {
  chunk_id: string;
  deleted_version: number;
  latest_version: number;
}
