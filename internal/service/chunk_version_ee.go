//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package service

import "errors"

// ErrChunkVersionNotFound reports a version number the chunk's history does not
// hold, or a history row that belongs to another document. Handlers map it to
// 404 instead of the generic 500.
var ErrChunkVersionNotFound = errors.New("chunk version not found")

// ErrChunkVersionImageNotFound reports a version that recorded no picture, or
// one whose bytes are no longer in object storage.
var ErrChunkVersionImageNotFound = errors.New("chunk version image not found")

// ChunkVersionRequest names one version of a chunk.
type ChunkVersionRequest struct {
	DatasetID  string `json:"dataset_id"`
	DocumentID string `json:"document_id"`
	ChunkID    string `json:"chunk_id"`
	Version    int    `json:"version"`
	// ExpectedVersion pins the version the caller read as current; a restore
	// that arrives after another edit of the same chunk answers with a
	// conflict instead of overwriting it.
	ExpectedVersion *int `json:"expected_version,omitempty"`
}

// The origins of a chunk history row: where a change came from, as opposed to what
// it did. Kind says what happened, Origin says who made it happen - a removal a
// re-parse produced and one a dataset restore produced are both actorless "delete"
// rows.
const (
	// ChunkVersionOriginParse is a row the ingestion pipeline wrote: the parse
	// baseline, the tombstone of a chunk a re-parse replaced, and the cleanup of
	// a run that failed while indexing.
	ChunkVersionOriginParse = "parse"
	// ChunkVersionOriginChunk is a row a user produced on the chunk side: the
	// chunk page or the API adding, editing, enabling, disabling, removing or
	// restoring one chunk.
	ChunkVersionOriginChunk = "chunk"
	// ChunkVersionOriginRestore is a row the replay of a dataset version restore
	// wrote: the content of a document was rolled back, or a chunk uploaded
	// after the anchor was dropped, without anyone touching that chunk.
	ChunkVersionOriginRestore = "dataset_restore"
)

// ChunkVersionSummary is the list entry of one chunk version. The snapshot is
// omitted here; it is loaded per version when a caller previews or restores it.
type ChunkVersionSummary struct {
	Version   int    `json:"version"`
	CreatedBy string `json:"created_by"`
	// CreatedByName is the actor's display name, resolved from CreatedBy when the
	// history is read so the list does not show a raw user id. It is empty for a
	// system-written row (the parse baseline, a tombstone) and for an actor whose
	// user no longer exists; the client falls back to CreatedBy then.
	CreatedByName string `json:"created_by_name,omitempty"`
	RestoredFrom  int    `json:"restored_from"`
	// CreateTime is the version's moment in milliseconds, the only timestamp the
	// version records. Clients format it themselves so the display follows the
	// reader's time zone, as the dataset version list does.
	CreateTime int64 `json:"create_time"`
	// CreateDate is the legacy server-formatted display string. Kept for
	// compatibility; the web UI reads CreateTime instead.
	CreateDate string `json:"create_date,omitempty"`
	// Latest marks the highest version, which is the state the last edit left
	// in the document engine.
	Latest bool `json:"latest"`
	// Kind is what produced the row (parse, edit, delete or restore). A parse
	// row is the baseline a replay rebuilds from and a delete row is what marks
	// the chunk as removed, so both are protected from deletion.
	Kind string `json:"kind"`
	// Origin is the ChunkVersionOrigin* value saying whether the ingestion
	// pipeline, a user, or a dataset version restore caused this row. Empty when
	// the history cannot say - a removal whose dataset version row is gone.
	Origin string `json:"origin,omitempty"`
	// TriggeredBy is the user whose request produced the row, which is what a
	// history list means by "who changed this". It differs from CreatedBy for a
	// system-written row only: that row is attributed to whoever triggered the
	// dataset version it was written under. Empty when that version is gone.
	TriggeredBy string `json:"triggered_by,omitempty"`
	// TriggeredByName is TriggeredBy resolved to a nickname, empty for a user that
	// no longer exists (the client falls back to the id) and for a row with no
	// trigger at all.
	TriggeredByName string `json:"triggered_by_name,omitempty"`
	// DatasetVersion is the dataset version this row belongs to - the state of the
	// dataset the chunk was in when the row was written.
	DatasetVersion int64 `json:"dataset_version"`
}

// ListChunkVersionsResponse is the version history of one chunk, newest first.
type ListChunkVersionsResponse struct {
	ChunkID  string                `json:"chunk_id"`
	Total    int                   `json:"total"`
	Versions []ChunkVersionSummary `json:"versions"`
}

// GetChunkVersionResponse is one chunk version, able to be previewed and
// restored. Chunk carries the versioned fields in the same shape the chunk
// endpoints return, so clients reuse their chunk mapping.
type GetChunkVersionResponse struct {
	Version   int    `json:"version"`
	CreatedBy string `json:"created_by"`
	// CreatedByName is the actor's display name, resolved the same way as in the
	// list; empty when the actor has no user row.
	CreatedByName string `json:"created_by_name,omitempty"`
	RestoredFrom  int    `json:"restored_from"`
	// CreateTime is the same raw millisecond instant as in the list; the client
	// formats it in the reader's time zone.
	CreateTime int64 `json:"create_time"`
	// CreateDate is the legacy server-formatted display string, kept for
	// compatibility; the web UI reads CreateTime instead.
	CreateDate string `json:"create_date,omitempty"`
	Latest     bool   `json:"latest"`
	Kind       string `json:"kind"`
	// Origin is the ChunkVersionOrigin* value of the row, resolved the same way
	// as in the list: it says whether a user acting on this chunk or a dataset
	// version restore produced the change.
	Origin string `json:"origin,omitempty"`
	// TriggeredBy and TriggeredByName are the user whose request produced the row,
	// resolved the same way as in the list.
	TriggeredBy     string `json:"triggered_by,omitempty"`
	TriggeredByName string `json:"triggered_by_name,omitempty"`
	// DatasetVersion is the version the list reports for this row: the dataset
	// state it belongs to.
	DatasetVersion int64                  `json:"dataset_version"`
	Chunk          map[string]interface{} `json:"chunk"`
}

// ChunkVersionDiffRequest names the two versions to compare. To is optional:
// nil compares From against the chunk's latest stored version.
type ChunkVersionDiffRequest struct {
	DatasetID  string `json:"dataset_id"`
	DocumentID string `json:"document_id"`
	ChunkID    string `json:"chunk_id"`
	From       int    `json:"from"`
	To         *int   `json:"to,omitempty"`
}

// The kinds of difference a field can report. text carries a unified diff,
// list carries element additions/removals, scalar carries before/after, and
// json only signals that a structured field changed.
const (
	ChunkVersionDiffText   = "text"
	ChunkVersionDiffList   = "list"
	ChunkVersionDiffScalar = "scalar"
	ChunkVersionDiffJSON   = "json"
)

// ChunkVersionFieldDiff is one field's difference between two versions. Text,
// scalar and structured fields are reported only when they changed; a list
// field is also reported when both versions kept the same values, so the
// comparison can show the whole list (Changed is false in that case).
type ChunkVersionFieldDiff struct {
	Field string `json:"field"`
	Kind  string `json:"kind"`
	// Changed reports whether the field actually differs. It is false for a
	// list field that is included only to show the values both versions kept.
	Changed bool `json:"changed"`
	// Diff is the unified diff of a text field.
	Diff string `json:"diff,omitempty"`
	// Added, Removed and Unchanged hold the element-level difference of a list
	// field. Unchanged is what both versions kept, so a client can render the
	// whole list with the changes marked instead of only the deletions.
	Added     []string `json:"added,omitempty"`
	Removed   []string `json:"removed,omitempty"`
	Unchanged []string `json:"unchanged,omitempty"`
	// Before and After describe a scalar field.
	Before interface{} `json:"before,omitempty"`
	After  interface{} `json:"after,omitempty"`
}

// ChunkVersionDiffResponse is the field-level comparison of two versions.
// Identical is true when no versioned field changed.
type ChunkVersionDiffResponse struct {
	ChunkID   string                  `json:"chunk_id"`
	From      int                     `json:"from"`
	To        int                     `json:"to"`
	Identical bool                    `json:"identical"`
	Fields    []ChunkVersionFieldDiff `json:"fields"`
}

// RestoreChunkVersionResponse reports the version a restore appended. The
// target version keeps its own row; the restore is recorded as a new one.
type RestoreChunkVersionResponse struct {
	Version int `json:"version"`
}

// DeleteChunkVersionResponse reports the history after a delete. Versions keep
// their numbers, so the list refreshes with a gap rather than a renumber.
type DeleteChunkVersionResponse struct {
	ChunkID        string `json:"chunk_id"`
	DeletedVersion int    `json:"deleted_version"`
	LatestVersion  int    `json:"latest_version"`
}
