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
	CreateTime    *int64 `json:"create_time,omitempty"`
	CreateDate    string `json:"create_date,omitempty"`
	// Latest marks the highest version, which is the state the last edit left
	// in the document engine.
	Latest bool `json:"latest"`
	// Kind is what produced the row (parse, edit, delete or restore). A parse
	// row is the baseline a replay rebuilds from and a delete row is what marks
	// the chunk as removed, so both are protected from deletion.
	Kind string `json:"kind"`
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
	CreatedByName string                 `json:"created_by_name,omitempty"`
	RestoredFrom  int                    `json:"restored_from"`
	CreateDate    string                 `json:"create_date,omitempty"`
	Latest        bool                   `json:"latest"`
	Kind          string                 `json:"kind"`
	Chunk         map[string]interface{} `json:"chunk"`
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
