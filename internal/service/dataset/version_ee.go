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

package dataset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	permissionresponse "ragflow/internal/permission/response"
	"ragflow/internal/service/datasetversion"
)

// datasetVersionError carries the HTTP-facing code of a failed version request.
// The handler maps it the same way it maps the chunk endpoints' coded errors.
type datasetVersionError struct {
	code    common.ErrorCode
	message string
}

func (e datasetVersionError) Error() string { return e.message }

func (e datasetVersionError) Code() common.ErrorCode { return e.code }

// restorableDatasetConfigFields are the snapshot keys a configuration restore
// writes back. Access-control columns are deliberately absent:
//
//   - permission is an ACL decision, and silently widening it through a restore
//     would be a security surprise;
//   - user_id / tenant_id / counters / task ids are owned by other paths.
//
// name and avatar are restorable: they are part of what a user sees on a
// version, and rolling them back is what a restore promises. name is unique per
// tenant, so a restore that would move it onto a taken name is refused before
// anything is written (validateRestorableName) instead of failing on the unique
// key.
//
// embd_id is restorable too, and cheap to move: vectors are stored per dimension
// (q_<dim>_vec), so the ones computed under the other model stay in their own
// column - invisible to vector search while the dataset is on the new model,
// usable again once it is restored back. Nothing is re-embedded here, exactly as
// nothing is re-embedded when the model is changed through the update path. The
// only refusal is a model the tenant can no longer resolve
// (validateRestorableEmbedding), which would break every future embedding call.
var restorableDatasetConfigFields = []string{
	"name",
	"avatar",
	"description",
	"language",
	"embd_id",
	"parser_id",
	"pipeline_id",
	"pipeline_version_id",
	"parser_config",
	"pagerank",
	"similarity_threshold",
	"vector_similarity_weight",
}

// datasetVersionComparedFields is what a version diff reports: the columns a
// restore writes, plus the data-source bindings. A field a restore cannot touch
// is deliberately not listed, so the page never promises a change it would not
// apply; the embedding model is listed because a restore may refuse to move it.
var datasetVersionComparedFields = append(
	append([]string{}, restorableDatasetConfigFields...),
	// connectors are not a column: the bindings live in connector2kb, so a
	// restore reconciles that table instead of writing the dataset row. They are
	// compared nevertheless, because linking or unlinking a data source is a
	// change the history has to show.
	"connectors",
)

// restorableDatasetConfigLabels keeps the diff readable: the API answers with
// the snapshot key and the list renders it as-is, so the key is the label.
//
// datasetConfigSnapshot is the state a version stores: every restorable column,
// the identity fields the list displays and the dataset's data-source bindings.
// Derived counters are stored in their own columns. The bindings are rows in
// connector2kb rather than columns of the dataset, which is why the read takes
// the caller's handle: a writer must see the bindings its own transaction just
// changed, and a comparison must read them through the transaction that holds
// the row lock.
func datasetConfigSnapshot(ctx context.Context, db *gorm.DB, kb *entity.Knowledgebase) (entity.JSONMap, error) {
	return datasetversion.SnapshotWithConnectors(ctx, db, kb)
}

// datasetConfigBindingsEE reads the dataset's data-source bindings inside the
// caller's transaction. The update path captures them before a relink overwrites
// them, so the version the change produces can record the state it replaced.
func (d *DatasetService) datasetConfigBindingsEE(ctx context.Context, tx *gorm.DB, kb *entity.Knowledgebase) ([]entity.JSONMap, error) {
	if kb == nil {
		return nil, nil
	}
	return datasetversion.ConnectorBindings(ctx, tx, kb.ID)
}

// writeDatasetVersionTxEE stores one version row using the caller's
// transaction. The caller has already allocated the number and bumped the
// dataset's change sequence.
//
// This writer only records states - the baseline, a configuration change, a
// document-configuration change and the restore itself, so it stores a snapshot
// and points at it. The snapshot is content addressed: a restore of a version
// whose configuration never moved writes the same bytes and therefore lands on
// the same dataset_config_snapshot row, with no copy to clean up later. A content
// change goes through the version recorder instead, which points at the head's
// hash rather than storing anything.
// bindings, when non-nil, are the data-source bindings the row records instead of
// reading connector2kb. The baseline of a change that also relinked data sources
// is seeded after that write, so the table already holds the new bindings; the
// caller hands in the ones read before it. A nil slice means "read the table".
// documentsAnchor, when non-nil, is the version whose document set applies here; a
// version that establishes the set ignores it and records its own.
func (d *DatasetService) writeDatasetVersionTxEE(ctx context.Context, tx *gorm.DB, kb *entity.Knowledgebase, version int64, kind, actor string, restoredFrom int64, documentsHash *string, bindings []entity.JSONMap) error {
	row := &entity.DatasetVersion{
		DatasetID:    kb.ID,
		Version:      version,
		Kind:         kind,
		RestoredFrom: restoredFrom,
		CreatedBy:    actor,
	}
	if entity.DatasetVersionKindRecordsState(kind) {
		var (
			snapshot    entity.JSONMap
			snapshotErr error
		)
		if bindings != nil {
			snapshot = datasetversion.SnapshotWithBindings(kb, bindings)
		} else {
			snapshot, snapshotErr = datasetConfigSnapshot(ctx, tx, kb)
		}
		if snapshotErr != nil {
			return snapshotErr
		}
		hash, hashErr := datasetversion.StoreConfigSnapshot(ctx, tx, kb.ID, snapshot)
		if hashErr != nil {
			return hashErr
		}
		row.ConfigHash = &hash
	}
	if err := datasetversion.ApplyDocuments(ctx, tx, row, documentsHash, kb.ID); err != nil {
		return err
	}
	return dao.NewDatasetVersionDAO().Create(ctx, tx, row)
}

// seedDatasetBaselineVersionTxEE records version 0 for a dataset that has no
// history yet, from the state the dataset currently has. A dataset created after
// the feature existed is seeded at creation; one that predates it is seeded the
// first time a change is recorded, so the change that triggered the seeding
// still has a starting point to diff against.
func (d *DatasetService) seedDatasetBaselineVersionTxEE(ctx context.Context, tx *gorm.DB, kb *entity.Knowledgebase, actor string) error {
	if dao.DB == nil || kb == nil {
		return nil
	}
	versionDAO := dao.NewDatasetVersionDAO()
	if _, err := versionDAO.GetLatest(ctx, tx, kb.ID); err == nil {
		return nil
	} else if !errors.Is(err, dao.ErrDatasetVersionNotFound) {
		return err
	}
	err := d.writeDatasetVersionTxEE(ctx, tx, kb, 0, entity.DatasetVersionKindBaseline, actor, 0, nil, nil)
	if err != nil && (errors.Is(err, dao.ErrDatasetVersionExists) || dao.IsDuplicateKeyErr(err)) {
		// Another writer seeded the same row between the check above and this
		// insert. It is version 0 of the same dataset, written from the state the
		// dataset already had, so there is nothing to do: the starting point the
		// check was looking for is now there.
		return nil
	}
	return err
}

// recordDatasetConfigVersionTxEE allocates the next version for a configuration
// change, stores the state the change produced and advances the dataset's
// change sequence. previous is the row as it stood before the change (the
// baseline is seeded from it when the dataset has no history) and updated is
// the row after it. previousBindings are the data-source bindings as they stood
// before the change, which the table no longer holds when the change relinked
// them; the baseline records those, so restoring it really is the state the
// change started from.
//
// The sequence and the version number are written here, in the same transaction
// as the configuration change, so a committed change can never lack a version.
func (d *DatasetService) recordDatasetConfigVersionTxEE(ctx context.Context, tx *gorm.DB, previous, updated *entity.Knowledgebase, previousBindings []entity.JSONMap, kind, actor string, restoredFrom int64) (int64, error) {
	if dao.DB == nil || updated == nil {
		return 0, nil
	}
	versionDAO := dao.NewDatasetVersionDAO()
	latest, err := versionDAO.GetLatestForUpdate(ctx, tx, updated.ID)
	switch {
	case errors.Is(err, dao.ErrDatasetVersionNotFound):
		// The dataset has no history yet: seed version 0 from the state the
		// change started from, and let this change take the number after it.
		if previous != nil {
			// The head-based dedupe below cannot run here: there is no head until
			// the baseline is written. The same rule applies to the pair, though -
			// a request that rewrites the values the dataset already has is not a
			// change, and with nothing recorded yet it leaves the dataset
			// unversioned. The first real change then seeds the baseline from the
			// state it actually starts from, which is still the diffable one.
			unchanged, unchangedErr := d.datasetConfigUnchangedEE(ctx, tx, previous, updated, previousBindings)
			if unchangedErr != nil {
				return 0, fmt.Errorf("failed to compare the dataset configuration with its previous state: %w", unchangedErr)
			}
			if unchanged {
				return 0, nil
			}
			if err := d.writeDatasetVersionTxEE(ctx, tx, previous, 0, entity.DatasetVersionKindBaseline, actor, 0, nil, previousBindings); err != nil && !errors.Is(err, dao.ErrDatasetVersionExists) {
				return 0, fmt.Errorf("failed to record the dataset baseline version: %w", err)
			}
			// The baseline is read back instead of assumed to be number 0: another
			// writer may have seeded it between the lookup above and this write
			// (the insert is the only step the dataset lock does not order), and
			// this change then still takes the number after whatever is there. A
			// configuration change does not move the document set, so it inherits
			// the baseline's.
			baseline, baselineErr := versionDAO.GetLatestForUpdate(ctx, tx, updated.ID)
			if baselineErr != nil {
				return 0, fmt.Errorf("failed to load the dataset baseline version: %w", baselineErr)
			}
			return d.appendDatasetVersionTxEE(ctx, tx, updated, baseline.Version+1, kind, actor, restoredFrom, baseline.DocumentsHash)
		}
	case err != nil:
		return 0, fmt.Errorf("failed to load the latest dataset version: %w", err)
	default:
		// A request that rewrites the values the dataset already has is not a
		// change: record nothing rather than a version that repeats the head. The
		// comparison is on the canonical hash, which is the same identity the
		// snapshot is stored under - so "the configuration did not move" and "this
		// version would point at the row the head already points at" are one check.
		snapshot, snapshotErr := datasetConfigSnapshot(ctx, tx, updated)
		if snapshotErr != nil {
			return 0, fmt.Errorf("failed to read the dataset configuration for its version: %w", snapshotErr)
		}
		hash, hashErr := datasetversion.ConfigHash(snapshot)
		if hashErr != nil {
			return 0, fmt.Errorf("failed to identify the dataset configuration for its version: %w", hashErr)
		}
		if latest.ConfigHash != nil && *latest.ConfigHash == hash {
			return latest.Version, nil
		}
		return d.appendDatasetVersionTxEE(ctx, tx, updated, latest.Version+1, kind, actor, restoredFrom, latest.DocumentsHash)
	}
	return 0, nil
}

// datasetConfigUnchangedEE reports whether a configuration change produced the
// state it started from. It compares the two states the way a version would be
// identified - by the hash of the snapshot - so "the configuration did not move"
// means exactly "the version this change would store is the one already there".
//
// previousBindings are the bindings as they stood before the change; nil means
// the change did not touch the table, and the bindings the table holds now are
// the ones it started with.
func (d *DatasetService) datasetConfigUnchangedEE(ctx context.Context, tx *gorm.DB, previous, updated *entity.Knowledgebase, previousBindings []entity.JSONMap) (bool, error) {
	var (
		before entity.JSONMap
		err    error
	)
	if previousBindings != nil {
		before = datasetversion.SnapshotWithBindings(previous, previousBindings)
	} else if before, err = datasetConfigSnapshot(ctx, tx, previous); err != nil {
		return false, err
	}
	after, err := datasetConfigSnapshot(ctx, tx, updated)
	if err != nil {
		return false, err
	}
	beforeHash, err := datasetversion.ConfigHash(before)
	if err != nil {
		return false, err
	}
	afterHash, err := datasetversion.ConfigHash(after)
	if err != nil {
		return false, err
	}
	return beforeHash == afterHash, nil
}

// appendDatasetVersionTxEE allocates the next change sequence for the dataset
// and stores the state it holds as a new version. It never dedupes: the caller
// either proved the state changed or is recording a milestone a repeat of the
// same state still has to appear as (a restore).
func (d *DatasetService) appendDatasetVersionTxEE(ctx context.Context, tx *gorm.DB, kb *entity.Knowledgebase, version int64, kind, actor string, restoredFrom int64, documentsHash *string) (int64, error) {
	// "Updated" on the dataset means the last versioned change, so the two
	// timestamps move with the version. The map update cannot rely on the model
	// hooks to fill them in.
	updateTime, updateDate := nowMillisAndDate()
	if err := tx.WithContext(ctx).
		Model(&entity.Knowledgebase{}).
		Where("id = ?", kb.ID).
		Updates(map[string]interface{}{
			"version":     version,
			"update_time": updateTime,
			"update_date": updateDate,
		}).Error; err != nil {
		return 0, fmt.Errorf("failed to advance the dataset version: %w", err)
	}
	kb.Version = version

	if err := d.writeDatasetVersionTxEE(ctx, tx, kb, version, kind, actor, restoredFrom, documentsHash, nil); err != nil {
		if errors.Is(err, dao.ErrDatasetVersionExists) {
			// The number allocated for this change is already on a row: the
			// history and the dataset's version column disagree (a half-restored
			// backup, a row removed by hand). The change itself is fine, so the
			// caller is told to re-read the head and retry rather than getting a
			// 500 with the driver's primary-key text.
			return 0, datasetVersionError{
				code:    common.CodeConflict,
				message: fmt.Sprintf("dataset version %d already exists; reload the dataset and retry", version),
			}
		}
		return 0, fmt.Errorf("failed to record the dataset version: %w", err)
	}
	return version, nil
}

// nowMillisAndDate is the pair the dataset row's update columns carry: the
// millisecond stamp the API exposes as update_time and the second-resolution
// date kept beside it for the legacy date column.
func nowMillisAndDate() (int64, time.Time) {
	now := time.Now().Local()
	return now.UnixMilli(), now.Truncate(time.Second)
}

// ---------------------------------------------------------------------------
// Read side
// ---------------------------------------------------------------------------

// NoDatasetVersion is the version a dry run reports. A preview applies nothing,
// so there is no version to name; real versions start at 0, so the value cannot
// collide with one.
const NoDatasetVersion int64 = -1

// maxDatasetVersionPageSize bounds one page of the history. The page is a
// management screen, not an export: without a ceiling a client could ask for a
// dataset's whole history and make the server assemble every row it has.
const maxDatasetVersionPageSize = 200

// DatasetVersionRequest names one version of one dataset.
type DatasetVersionRequest struct {
	DatasetID string
	Version   int64
}

// ListDatasetVersionsRequest asks for a page of a dataset's history.
type ListDatasetVersionsRequest struct {
	DatasetID     string
	BeforeVersion *int64
	Kinds         []string
	// Limit is the page size. It is clamped to maxDatasetVersionPageSize; 0 takes
	// the store's default.
	Limit int
}

// DatasetVersionSummary is one row of the history list.
type DatasetVersionSummary struct {
	Version      int64  `json:"version"`
	Kind         string `json:"kind"`
	RestoredFrom int64  `json:"restored_from,omitempty"`
	CreatedBy    string `json:"created_by"`
	// CreatedByName is the actor's display name, resolved from CreatedBy so the
	// history reads "By alice" instead of a raw user id. It is empty when the
	// version recorded no actor (content versions written by the system) or the
	// user no longer exists, and the client falls back to the id.
	CreatedByName string `json:"created_by_name,omitempty"`
	// CreateTime is the version's moment in milliseconds, the only timestamp the
	// version records. Clients format it themselves so the display follows the
	// reader's time zone.
	CreateTime int64 `json:"create_time"`
	// ConfigHash names the configuration snapshot this version ran under. Two
	// versions with the same hash describe the same settings, which is what lets a
	// client say "no configuration change since version N" without reading either
	// snapshot. It is empty for a version whose configuration was never recorded.
	ConfigHash string `json:"config_hash,omitempty"`
}

// ListDatasetVersionsResponse is the history page plus the dataset's current
// version, so a client can offer "restore to this version" without a second
// call.
type ListDatasetVersionsResponse struct {
	DatasetID      string                  `json:"dataset_id"`
	CurrentVersion int64                   `json:"current_version"`
	Versions       []DatasetVersionSummary `json:"versions"`
}

// DatasetVersionChangeDetail is one document or chunk the version touched. A
// version may carry no entries even when its kind says the content changed: an
// old version predates the change set, and a mass change is truncated.
type DatasetVersionChangeDetail struct {
	Entity string `json:"entity"`
	Action string `json:"action"`
	DocID  string `json:"doc_id,omitempty"`
	// ChunkID is the chunk a chunk entry names. A document entry has none: its
	// DocID is the document, and repeating it in a second column said nothing.
	ChunkID string `json:"chunk_id,omitempty"`
	// Name is the document name for a document entry. A chunk entry leaves it
	// empty: the text a chunk held at this version is the chunk's own history,
	// which the change list does not repeat (it names the chunk and its document
	// and links there instead).
	Name string `json:"name,omitempty"`
	// PreviousName is what a renamed document was called before this entry moved
	// it. The panel reads a rename as "old name to new name": the name it landed
	// on says nothing about what moved. Empty on entries that are not renames and
	// on rows written before the field existed.
	PreviousName string `json:"previous_name,omitempty"`
	// DocName names the document the entry belongs to, resolved on read. A chunk
	// entry stores only that document's id, and the panel has to say which
	// document the chunk is in; empty when the document is gone.
	DocName string `json:"doc_name,omitempty"`
	// CommitID is the page commit an artifact entry names, as the writer recorded
	// it. Empty on entries written before the change set carried it, and on a
	// deployment whose page history was unavailable at edit time; the reader then
	// resolves the page's newest commit at the version's anchor instead.
	CommitID string `json:"commit_id,omitempty"`
	// MetaChanges is what a metadata edit moved, key by key. The values live in
	// the metadata store, which has no diff of its own, so this entry is the only
	// place the edit is written down: without it the page can say the metadata
	// changed but not what changed. The writer stores it bounded (see
	// datasetversion.normalizeChanges), so it is passed through as saved.
	MetaChanges []entity.MetadataFieldChange `json:"meta_changes,omitempty"`
	// ChunkVersion is the version of the chunk this entry produced: the number
	// the chunk's own history carries for the state after the change, so the page
	// can point the entry at the row it created. Nil when the history has no such
	// row - a deployment that keeps no chunk history, or a history pruned since.
	ChunkVersion *int `json:"chunk_version,omitempty"`
}

// GetDatasetVersionResponse is one version with its full configuration and the
// documents and chunks it touched.
type GetDatasetVersionResponse struct {
	DatasetVersionSummary
	Config map[string]interface{} `json:"config"`
	// Documents is the document set the version held, each with the name that id
	// answers to (see datasetVersionDocumentNames). A version with no document set
	// of its own lists the set it inherited; one with no set at all lists none.
	//
	// A restore version also carries what each of those documents holds now, which
	// is how the page names the ones the restore could not fill (see
	// DatasetVersionDocument).
	Documents []DatasetVersionDocument     `json:"documents"`
	Changes   []DatasetVersionChangeDetail `json:"changes"`
	// ChangesTotal is how many entries the version recorded before the writer
	// truncated the set at maxVersionChanges. Changes shorter than this means the
	// page is showing a sample: a parse of a large document, a bulk delete or a
	// restore of a whole dataset names far more rows than one version stores.
	ChangesTotal int `json:"changes_total"`
	// ConfigChanges is what a configuration version moved, field by field. A
	// configuration version records no document or chunk rows - its change is the
	// settings themselves - so this is the detail the page lists for it. Empty
	// for any other kind, and for a configuration version whose predecessor is
	// gone.
	ConfigChanges []DatasetConfigChange `json:"config_changes,omitempty"`
	// DocumentConfigChanges is what a document-configuration version moved, one
	// group per document: which of that document's parse settings differ from the
	// settings it ran under before. Such a version names the documents it touched
	// but stores no field diff - each document's settings live in its own history
	// - so the diff is read back from there when the version is opened. Empty for
	// any other kind, and for a document whose settings history has no earlier row
	// to compare against.
	DocumentConfigChanges []DocumentConfigChange `json:"document_config_changes,omitempty"`
}

// DocumentConfigChange is one document a version's parse settings moved, with
// the fields that moved.
type DocumentConfigChange struct {
	DocID string `json:"doc_id"`
	// DocName is the document's name, resolved on read; the change entry stores
	// none for a live document.
	DocName string `json:"doc_name,omitempty"`
	// Changes are the fields that differ from the settings the document ran under
	// before, in the order documentConfigComparedFields gives them.
	Changes []DatasetConfigChange `json:"changes"`
}

// DatasetConfigChange is one field a version changed.
type DatasetConfigChange struct {
	Field  string      `json:"field"`
	Before interface{} `json:"before"`
	After  interface{} `json:"after"`
}

// ListDatasetVersions lists a dataset's versions newest first.
func (d *DatasetService) ListDatasetVersions(ctx context.Context, req *ListDatasetVersionsRequest) (*ListDatasetVersionsResponse, error) {
	datasetID, err := datasetVersionTarget(ctx, req.DatasetID, permission.ResourcePermissionRead, permission.ActionRead)
	if err != nil {
		return nil, err
	}
	rows, err := dao.NewDatasetVersionDAO().List(ctx, dao.DB, datasetID, req.BeforeVersion, req.Kinds, datasetVersionPageSize(req.Limit))
	if err != nil {
		return nil, fmt.Errorf("failed to list dataset versions: %w", err)
	}
	response := &ListDatasetVersionsResponse{
		DatasetID: datasetID,
		Versions:  make([]DatasetVersionSummary, 0, len(rows)),
	}
	kb, err := d.kbDAO.GetByID(ctx, dao.DB, datasetID)
	if err == nil && kb != nil {
		response.CurrentVersion = kb.Version
	}
	for _, row := range rows {
		response.Versions = append(response.Versions, datasetVersionSummaryOf(row))
	}
	resolveDatasetVersionActors(ctx, response.Versions)
	return response, nil
}

// datasetVersionPageSize clamps a requested page size. The ceiling is what keeps
// one request from asking the server to assemble an unbounded history.
func datasetVersionPageSize(limit int) int {
	if limit > maxDatasetVersionPageSize {
		return maxDatasetVersionPageSize
	}
	return limit
}

// GetDatasetVersion returns one version including its configuration snapshot.
func (d *DatasetService) GetDatasetVersion(ctx context.Context, req *DatasetVersionRequest) (*GetDatasetVersionResponse, error) {
	datasetID, err := datasetVersionTarget(ctx, req.DatasetID, permission.ResourcePermissionRead, permission.ActionRead)
	if err != nil {
		return nil, err
	}
	row, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(ctx, dao.DB, datasetID, req.Version)
	if err != nil {
		if errors.Is(err, dao.ErrDatasetVersionNotFound) {
			return nil, datasetVersionError{code: common.CodeNotFound, message: fmt.Sprintf("dataset version %d not found", req.Version)}
		}
		return nil, fmt.Errorf("failed to load dataset version: %w", err)
	}
	// The change set arrived with the row, in the order the writer stored it:
	// documents first, then the chunks inside them. Nothing to join and nothing
	// to sort.
	details := make([]DatasetVersionChangeDetail, 0, len(row.Changes))
	for _, change := range row.Changes {
		details = append(details, DatasetVersionChangeDetail{
			Entity:  change.Entity,
			Action:  change.Action,
			DocID:   change.DocID,
			ChunkID: change.ChunkID,
			Name:    change.Name,
			// The rename is the one entry whose meaning needs the name it
			// replaced, and the row is the only place that name survives.
			PreviousName: change.PreviousName,
			CommitID:     change.CommitID,
			// A metadata entry's diff is stored on the entry itself: no history to
			// read it back from, and nothing to bound - the writer already did.
			MetaChanges: change.MetaChanges,
		})
	}
	summaries := []DatasetVersionSummary{datasetVersionSummaryOf(row)}
	resolveDatasetVersionActors(ctx, summaries)
	resolveDatasetVersionChangeDocuments(ctx, details)
	resolveDatasetVersionChangeChunkVersions(ctx, datasetID, row.Version, details)
	// A document-configuration version names the documents it touched but stores
	// no field diff: read each document's settings back from its own history.
	documentConfigChanges := documentConfigVersionChanges(ctx, row, details)
	// A row written before the column existed stores no total; the rows it did
	// store are then all of them, so the change set's own length is the answer.
	changesTotal := row.ChangesTotal
	if changesTotal < len(row.Changes) {
		changesTotal = len(row.Changes)
	}
	return &GetDatasetVersionResponse{
		DatasetVersionSummary: summaries[0],
		Config:                map[string]interface{}(row.Config),
		Documents:             datasetVersionDocuments(ctx, datasetID, row),
		Changes:               details,
		ChangesTotal:          changesTotal,
		ConfigChanges:         configVersionChanges(ctx, datasetID, row),
		DocumentConfigChanges: documentConfigChanges,
	}, nil
}

// configVersionChanges answers which configuration fields a configuration
// version moved, by diffing it against the newest version before it.
//
// The diff is computed on read rather than stored: both snapshots are already
// on their rows, and a stored copy would be one more thing to keep in step with
// them - and one more thing to go stale when history is deleted. Reading it
// against the previous surviving version is also the honest answer to "what did
// this version change": after a history delete, the neighbour is not
// necessarily version-1.
//
// A version with no predecessor, or one whose predecessor's snapshot no longer
// resolves, reports nothing; the page then falls back to its one-line summary.
func configVersionChanges(ctx context.Context, datasetID string, row *entity.DatasetVersion) []DatasetConfigChange {
	if row == nil || row.Kind != entity.DatasetVersionKindDatasetConfig || dao.DB == nil {
		return nil
	}
	previous, err := dao.NewDatasetVersionDAO().GetPrevious(ctx, dao.DB, datasetID, row.Version)
	if err != nil {
		if !errors.Is(err, dao.ErrDatasetVersionNotFound) {
			common.Warn("Failed to load the previous dataset version for its configuration diff", zap.Error(err))
		}
		return nil
	}
	return datasetConfigDisplayChanges(ctx, previous.Config, row.Config)
}

// datasetConfigDisplayFields is what the history panel diffs: the restorable
// fields, plus the mode the dataset parsed under, derived the same way a
// document's settings state names it. It comes first for the same reason it does
// there - it is what the rest of the row is read through.
var datasetConfigDisplayFields = append([]string{"parse_type"}, datasetVersionComparedFields...)

// datasetConfigDisplayChanges compares two configuration snapshots for the panel.
//
// On top of the plain diff it names what an id cannot: the pipeline a side binds,
// the version of it, and the mode each side parsed under, so a switch reads as
// "General -> Q&A" instead of as an id that changed.
func datasetConfigDisplayChanges(ctx context.Context, before, after entity.JSONMap) []DatasetConfigChange {
	diff := datasetConfigFieldChanges(
		datasetConfigDisplayView(before),
		datasetConfigDisplayView(after),
		datasetConfigDisplayFields,
	)
	resolvePipeline := pipelineTitleResolver(ctx)
	resolveVersion := pipelineVersionResolver(ctx)
	changes := make([]DatasetConfigChange, 0, len(diff))
	for _, change := range diff {
		// The blob is never a line of its own: a snapshot without one is a
		// snapshot without configuration, not a setting that moved.
		if change.Field == "parser_config" {
			continue
		}
		switch change.Field {
		case "pipeline_id":
			change.Before = resolvePipeline(change.Before)
			change.After = resolvePipeline(change.After)
		case "pipeline_version_id":
			change.Before = resolveVersion(change.Before)
			change.After = resolveVersion(change.After)
		}
		changes = append(changes, change)
	}
	return changes
}

// datasetConfigDisplayView is one side of that diff: the stored snapshot plus the
// mode it parsed under.
//
// A dataset keeps the built-in method it last ran under when it is moved to a
// pipeline, so that column is cleared while the pipeline is the selection - a
// stale value left in place would both hide the switch and read as current.
func datasetConfigDisplayView(config entity.JSONMap) entity.JSONMap {
	if config == nil {
		return nil
	}
	view := make(entity.JSONMap, len(config)+1)
	for key, value := range config {
		view[key] = value
	}
	id, _ := config["pipeline_id"].(string)
	if strings.TrimSpace(id) == "" {
		view["parse_type"] = "builtin"
		return view
	}
	view["parse_type"] = "pipeline"
	view["parser_id"] = ""
	return view
}

// documentConfigComparedFields is what a document-configuration version reports,
// in the order the panel lists it: the mode the document ran under, the selection
// inside that mode, and the parameters of that selection. parser_config is
// descended into, so one edited parameter reads as that parameter instead of as
// the whole blob.
//
// parse_type comes first because it is what the rest of the row is read through:
// the same parser_id means a built-in method on one row and nothing at all on a
// row a pipeline parses.
var documentConfigComparedFields = []string{
	"parse_type",
	"parser_id",
	"pipeline_id",
	"pipeline_version_id",
	"parser_config",
}

// documentConfigVersionChanges answers which parse settings each document a
// document-configuration version touched moved.
//
// The version stores no diff: it names the documents, and their settings live in
// the per-document states, which record the whole configuration on every write.
// The state the version describes is that document's newest state at or before
// the version's own number, and the state it replaced is the one before it - two
// lookups per document the change set names (the writer bounds that set).
//
// The number is what selects the pair, not the clock: two edits in one
// millisecond are two states of the document, which a millisecond-keyed record
// cannot represent - reading by the version's moment would answer the state
// *after* the change for both sides of the diff, and an edit made in the same
// millisecond as the previous one would report no change at all.
//
// A document with no earlier state - its settings were first recorded by the
// change this version describes - has nothing to compare against and is left
// out; the change set beside this list still names it.
func documentConfigVersionChanges(ctx context.Context, row *entity.DatasetVersion, details []DatasetVersionChangeDetail) []DocumentConfigChange {
	if row == nil || row.Kind != entity.DatasetVersionKindDocumentConfig || dao.DB == nil {
		return nil
	}
	groups := make([]DocumentConfigChange, 0, len(details))
	seen := make(map[string]struct{}, len(details))
	// The pipeline each grouped document runs under, kept for the version-only
	// change below: its state row is the only place that names the canvas.
	pipelines := make(map[string]*string, len(details))
	for _, detail := range details {
		// Only a settings edit is described here: the other actions a change set
		// carries already say everything they have to say on their own line.
		if detail.Entity != entity.DatasetVersionChangeEntityDocument ||
			detail.Action != entity.DatasetVersionChangeActionUpdate {
			continue
		}
		docID := strings.TrimSpace(detail.DocID)
		if docID == "" {
			continue
		}
		if _, ok := seen[docID]; ok {
			continue
		}
		seen[docID] = struct{}{}
		after := documentSettingsAt(ctx, docID, row.Version)
		if after == nil {
			continue
		}
		before := documentSettingsAt(ctx, docID, row.Version-1)
		if before == nil {
			continue
		}
		changes := documentConfigFieldChanges(before, after)
		if len(changes) == 0 {
			continue
		}
		pipelines[docID] = after.PipelineID
		groups = append(groups, DocumentConfigChange{
			DocID:   docID,
			DocName: detail.DocName,
			Changes: changes,
		})
	}
	if len(groups) == 0 {
		return nil
	}
	// A pipeline id is not a name: "3f9c..." says nothing about which pipeline the
	// document moved to, and the panel has no way to resolve one. The document list
	// reads the title the same way, from the canvas the row points at - and a
	// version is named by its alias, which the pipeline version list shows beside
	// the title.
	resolvePipeline := pipelineTitleResolver(ctx)
	resolveVersion := pipelineVersionResolver(ctx)
	for i := range groups {
		for j := range groups[i].Changes {
			change := &groups[i].Changes[j]
			switch change.Field {
			case "pipeline_id":
				change.Before = resolvePipeline(change.Before)
				change.After = resolvePipeline(change.After)
			case "pipeline_version_id":
				change.Before = resolveVersion(change.Before)
				change.After = resolveVersion(change.After)
			}
		}
	}
	// A version switch that leaves the pipeline alone has no pipeline row of its
	// own, and "a -> b" on its own does not say which pipeline was switched
	// inside. Give that line the pipeline it belongs to, so the panel can read the
	// move as one change ("pip (alias: a) -> pip (alias: b)") instead of a stray
	// pair of labels. Nothing moved on the pipeline side, so both sides carry the
	// same name and the version row beside them supplies the two aliases.
	for i := range groups {
		hasPipeline, versionAt := false, -1
		for j := range groups[i].Changes {
			switch groups[i].Changes[j].Field {
			case "pipeline_id":
				hasPipeline = true
			case "pipeline_version_id":
				versionAt = j
			}
		}
		if hasPipeline || versionAt < 0 {
			continue
		}
		pipelineID := pipelines[groups[i].DocID]
		if pipelineID == nil || strings.TrimSpace(*pipelineID) == "" {
			continue
		}
		title := resolvePipeline(*pipelineID)
		if name, ok := title.(string); !ok || name == "" {
			continue
		}
		changes := groups[i].Changes
		groups[i].Changes = append(changes, DatasetConfigChange{})
		copy(groups[i].Changes[versionAt+1:], groups[i].Changes[versionAt:])
		groups[i].Changes[versionAt] = DatasetConfigChange{
			Field: "pipeline_id", Before: title, After: title,
		}
	}
	return groups
}

// pipelineTitleResolver answers a pipeline's title for the panel, keyed by id.
// An id that no longer resolves - the canvas was deleted - is returned as it is:
// the id still tells the reader the document was moved to a pipeline, while an
// empty cell would not.
func pipelineTitleResolver(ctx context.Context) func(interface{}) interface{} {
	titles := make(map[string]string)
	return func(value interface{}) interface{} {
		id, ok := value.(string)
		if !ok || id == "" {
			return value
		}
		if title, done := titles[id]; done {
			if title == "" {
				return value
			}
			return title
		}
		canvas, err := dao.NewUserCanvasDAO().GetByCanvasID(ctx, dao.DB, id)
		if err != nil || canvas == nil || canvas.Title == nil || strings.TrimSpace(*canvas.Title) == "" {
			titles[id] = ""
			return value
		}
		titles[id] = *canvas.Title
		return *canvas.Title
	}
}

// pipelineVersionResolver answers a pipeline version's label for the panel, keyed
// by id: the row stores the id, and a hash says nothing about which version was
// pinned.
//
// The alias is what a version is labelled by - the pipeline version list shows it
// beside the title - so it comes first. A version without one still names itself
// with its title (the pipeline's name when that version was saved), and only then
// is the id the last thing left.
func pipelineVersionResolver(ctx context.Context) func(interface{}) interface{} {
	labels := make(map[string]string)
	return func(value interface{}) interface{} {
		id, ok := value.(string)
		if !ok || id == "" {
			return value
		}
		if label, done := labels[id]; done {
			if label == "" {
				return value
			}
			return label
		}
		label := ""
		if version, err := dao.NewUserCanvasVersionDAO().GetByID(ctx, dao.DB, id); err == nil && version != nil {
			switch {
			case version.Alias != nil && strings.TrimSpace(*version.Alias) != "":
				label = *version.Alias
			case version.Title != nil && strings.TrimSpace(*version.Title) != "":
				label = *version.Title
			}
		}
		labels[id] = label
		if label == "" {
			return value
		}
		return label
	}
}

// documentConfigFieldChanges diffs two settings rows for the panel.
//
// A parser_config parameter the newer row carries is reported - the ones a save
// filled in from the template read as "— → value" - while one it no longer
// carries is not: the panel lists the configuration as it stands, so the whole
// blob is never a line of its own.
//
// The selection fields are kept as the plain diff gives them: pipeline_id and
// parser_id are the two whose absence is itself a value ("the built-in method" /
// "a pipeline"), and a pair empty on both sides says nothing, so it stays out.
func documentConfigFieldChanges(before, after *entity.DocumentVersion) []DatasetConfigChange {
	changes := datasetConfigFieldChanges(
		documentParserSettingsSnapshot(before),
		documentParserSettingsSnapshot(after),
		documentConfigComparedFields,
	)
	reported := make([]DatasetConfigChange, 0, len(changes))
	for _, change := range changes {
		if change.Field == "parser_config" {
			continue
		}
		if strings.HasPrefix(change.Field, "parser_config.") {
			reported = append(reported, change)
			continue
		}
		if datasetConfigValueUnset(change.Before) && datasetConfigValueUnset(change.After) {
			continue
		}
		reported = append(reported, change)
	}
	return reported
}

// datasetConfigValueUnset reports whether a value reads as "—" on the page: a
// field the row did not carry, or one it carried empty.
func datasetConfigValueUnset(value interface{}) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return text == ""
	}
	return false
}

// documentParserSettingsSnapshot puts one state row in the shape the diff reads.
// An unset pipeline is left absent rather than recorded as an empty string, so
// binding one for the first time reads as the change it is.
func documentParserSettingsSnapshot(row *entity.DocumentVersion) entity.JSONMap {
	mode := documentParseMode(row)
	parserID := row.ParserID
	if mode == "pipeline" {
		// The row keeps whatever built-in method it last ran under - binding a
		// pipeline leaves that column alone - but the pipeline is what parses the
		// document now, so the method is no longer part of the selection. Reading it
		// as one would hide the switch behind a value that never moves.
		parserID = ""
	}
	snapshot := entity.JSONMap{
		"parse_type":    mode,
		"parser_id":     parserID,
		"parser_config": map[string]interface{}(row.ParserConfig),
	}
	if row.PipelineID != nil {
		snapshot["pipeline_id"] = *row.PipelineID
	}
	if row.PipelineVersionID != nil {
		snapshot["pipeline_version_id"] = *row.PipelineVersionID
	}
	return snapshot
}

// documentParseMode names the mode a settings state ran under. The mode is not a
// column: a row either carries a pipeline binding or it does not, and a document
// without one is parsed by its built-in method. The diff reads it as a field of
// its own, so a switch between the modes is one line of its own rather than only
// a pipeline binding that appeared.
func documentParseMode(row *entity.DocumentVersion) string {
	if row.PipelineID != nil && strings.TrimSpace(*row.PipelineID) != "" {
		return "pipeline"
	}
	return "builtin"
}

// documentSettingsAt returns the parse settings one document ran under as of a
// dataset version, or nil when nothing recorded them that early.
//
// One record answers: document_version, keyed by (document_id, dataset_version).
// The key is the number, not the clock, so two edits that landed in the same
// millisecond are two states and the diff can tell them apart - beside the older
// side of the pair it asks for version-1, which is the state the document ran
// under before this change.
func documentSettingsAt(ctx context.Context, documentID string, datasetVersion int64) *entity.DocumentVersion {
	if dao.DB == nil || documentID == "" || datasetVersion < 0 {
		return nil
	}
	row, err := dao.NewDocumentVersionDAO().LatestAtVersion(ctx, dao.DB, documentID, datasetVersion, entity.DatasetVersionKindDocumentConfig)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			common.Warn("Failed to read a document's versioned parser settings", zap.Error(err))
		}
		return nil
	}
	return row
}

// DeleteDatasetVersionRequest names one version to drop from a dataset's history.
type DeleteDatasetVersionRequest struct {
	DatasetID string
	Version   int64
}

// DeleteDatasetVersionResponse reports the history after the delete, so the
// caller can refresh without a second call.
type DeleteDatasetVersionResponse struct {
	DatasetID      string `json:"dataset_id"`
	DeletedVersion int64  `json:"deleted_version"`
	CurrentVersion int64  `json:"current_version"`
}

// DeleteDatasetVersion removes one version from a dataset's history.
//
// Deleting history never rewrites the dataset: the version row and the change set
// that explains it go, and the remaining numbers keep their gaps, so no later
// version has to be renumbered. Two versions cannot go, because the dataset
// still depends on them:
//
//   - the current one: the dataset's version column points at it;
//   - the baseline (version 0): nothing precedes it.
//
// Nothing else stands in the way. A version that recorded a configuration used to
// be pinned by the versions that read it; now the snapshot lives in
// dataset_config_snapshot, keyed by content, so deleting the version drops a
// pointer and the later versions keep reading the same snapshot. What no version
// points at any more is reclaimed after the delete, which is the only moment a
// snapshot can become unreachable.
//
// Content versions are deletable for the same reason they always were: they only
// describe what happened to the documents, and the documents themselves are
// untouched.
func (d *DatasetService) DeleteDatasetVersion(ctx context.Context, req *DeleteDatasetVersionRequest) (*DeleteDatasetVersionResponse, error) {
	if req == nil {
		return nil, datasetVersionError{code: common.CodeArgumentError, message: "request is required"}
	}
	datasetID, err := datasetVersionTarget(ctx, req.DatasetID, permission.ResourcePermissionManage, permission.ActionWrite)
	if err != nil {
		return nil, err
	}
	versionDAO := dao.NewDatasetVersionDAO()
	// The row is read once, inside the transaction, where it decides the delete.
	// A pre-check outside it would read the same row - its document set included,
	// which is the largest column on the table - only for the delete to re-read it
	// under the lock anyway.
	var currentVersion int64
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lockedKB, code, lockErr := d.lockAccessibleDatasetForUpdate(ctx, tx, datasetID)
		if lockErr != nil {
			if code != common.CodeSuccess {
				return datasetVersionError{code: code, message: lockErr.Error()}
			}
			return lockErr
		}
		currentVersion = lockedKB.Version

		// Re-read under the lock: a concurrent delete would otherwise let this
		// request report a version it never removed, and the head may have moved.
		target, loadErr := versionDAO.GetByDatasetAndVersion(ctx, tx, datasetID, req.Version)
		if loadErr != nil {
			if errors.Is(loadErr, dao.ErrDatasetVersionNotFound) {
				return datasetVersionError{code: common.CodeNotFound, message: fmt.Sprintf("dataset version %d not found", req.Version)}
			}
			return fmt.Errorf("failed to load dataset version: %w", loadErr)
		}
		if target.Version == lockedKB.Version {
			return datasetVersionError{
				code:    common.CodeConflict,
				message: fmt.Sprintf("dataset version %d is the current version and cannot be deleted", target.Version),
			}
		}
		if target.Kind == entity.DatasetVersionKindBaseline {
			return datasetVersionError{code: common.CodeConflict, message: "the baseline version cannot be deleted"}
		}
		// Nothing has to be handed over before the delete: the configuration
		// snapshot and the document set are both stored by content, so this row
		// takes only its two hashes with it. Every version that shared them still
		// resolves them, and the sweep below reclaims the ones nothing names any
		// more.
		if _, deleteErr := versionDAO.DeleteByDatasetAndVersion(ctx, tx, datasetID, req.Version); deleteErr != nil {
			return fmt.Errorf("failed to delete dataset version: %w", deleteErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// The delete may have left snapshots nothing points at - a configuration or a
	// document set whose last naming version just went away. Reclaiming them is
	// one sweep of the dataset, not two per-table cleanups: "which snapshots are
	// garbage" is a property of the surviving versions, so the same call also
	// collects rows an earlier delete orphaned when its sweep failed.
	//
	// It runs after the delete committed, so a failure costs nothing: the rows are
	// unreachable either way, and a later delete or a dataset purge still removes
	// them.
	if reclaimed, sweepErr := dao.SweepDatasetSnapshots(ctx, dao.DB, datasetID); sweepErr != nil {
		common.Warn("Failed to reclaim content snapshots that no version points at",
			zap.String("dataset_id", datasetID), zap.Error(sweepErr))
	} else if reclaimed.Total() > 0 {
		common.Info("Reclaimed dataset content snapshots",
			zap.String("dataset_id", datasetID),
			zap.Int64("configs", reclaimed.Configs),
			zap.Int64("document_sets", reclaimed.DocumentSets))
	}
	// The version that held a removed document may have been the last one that
	// could restore it. Now that it is gone, the removal row and the objects it
	// kept in storage are unreachable, so they are reclaimed with the version
	// rather than left in the bucket forever - and here, beside the sweep, so
	// every caller of the delete gets the same cleanup, not only the HTTP one.
	//
	// A failure is logged, not returned: the delete itself succeeded, and
	// answering with an error would invite a retry of a delete that would now
	// report the version as missing. The rows a failed pass leaves behind are
	// unreachable but intact, so a later delete or an explicit cleanup still
	// removes them.
	if pruned, pruneErr := PruneUnreferencedRemovedDocuments(ctx, datasetID); pruneErr != nil {
		common.Warn("Failed to prune removed document(s) that no remaining version can restore",
			zap.String("dataset_id", datasetID), zap.Error(pruneErr))
	} else if pruned > 0 {
		common.Info("Pruned removed document(s) that no remaining version can restore",
			zap.String("dataset_id", datasetID), zap.Int("documents", pruned))
	}
	return &DeleteDatasetVersionResponse{
		DatasetID:      datasetID,
		DeletedVersion: req.Version,
		CurrentVersion: currentVersion,
	}, nil
}

// datasetVersionTarget checks the resource grant and tenant role independently.
func datasetVersionTarget(ctx context.Context, datasetID string, resourcePermission int32, roleAction permission.Action) (string, error) {
	normalizedID := strings.TrimSpace(datasetID)
	if normalizedID == "" {
		return "", datasetVersionError{code: common.CodeArgumentError, message: "dataset_id is required"}
	}
	checker := permission.NewDatabaseChecker(dao.DB)
	datasetRef := permission.ResourceRef{Kind: permission.ResourceKindDataset, ID: normalizedID}
	if err := checker.CheckResourcePermission(ctx, datasetRef, resourcePermission); err != nil {
		code, permissionErr := normalizeDatasetVersionPermissionError(ctx, checker, datasetRef, err)
		return "", datasetVersionError{code: code, message: permissionErr.Error()}
	}
	if err := checker.CheckFeature(ctx, permission.FeatureDataset, roleAction); err != nil {
		code, permissionErr := normalizeDatasetVersionPermissionError(ctx, checker, datasetRef, err)
		return "", datasetVersionError{code: code, message: permissionErr.Error()}
	}
	return normalizedID, nil
}

func normalizeDatasetVersionPermissionError(ctx context.Context, checker *permission.Checker, datasetRef permission.ResourceRef, err error) (common.ErrorCode, error) {
	if !permissionresponse.IsPermissionError(err) {
		return permissionresponse.Normalize(err)
	}
	if readErr := checker.CheckResourcePermission(ctx, datasetRef, permission.ResourcePermissionRead); readErr != nil {
		if permissionresponse.IsPermissionError(readErr) {
			return permissionresponse.NormalizeHidden(err)
		}
		return permissionresponse.Normalize(readErr)
	}
	if readErr := checker.CheckFeature(ctx, permission.FeatureDataset, permission.ActionRead); readErr != nil {
		if permissionresponse.IsPermissionError(readErr) {
			return permissionresponse.NormalizeHidden(err)
		}
		return permissionresponse.Normalize(readErr)
	}
	return permissionresponse.Normalize(err)
}

// ResolveDatasetVersionTarget resolves the dataset a version request addresses
// and checks manage permission plus the dataset role's write capability. The content-restore service lives in its own
// package, so it applies the check through this exported entry point instead of
// duplicating the permission wiring.
func ResolveDatasetVersionTarget(ctx context.Context, datasetID string) (string, error) {
	return datasetVersionTarget(ctx, datasetID, permission.ResourcePermissionManage, permission.ActionWrite)
}

// ResolveDatasetVersionReadTarget is the read side of
// ResolveDatasetVersionTarget: listing what a content version can still restore
// is version history, which a reader may see without being able to change it.
func ResolveDatasetVersionReadTarget(ctx context.Context, datasetID string) (string, error) {
	return datasetVersionTarget(ctx, datasetID, permission.ResourcePermissionRead, permission.ActionRead)
}

func datasetVersionSummaryOf(row *entity.DatasetVersion) DatasetVersionSummary {
	if row == nil {
		return DatasetVersionSummary{}
	}
	return DatasetVersionSummary{
		Version:      row.Version,
		Kind:         row.Kind,
		RestoredFrom: row.RestoredFrom,
		CreatedBy:    row.CreatedBy,
		CreateTime:   datasetVersionCreatedAt(row),
		ConfigHash:   stringValueOrEmpty(row.ConfigHash),
	}
}

// stringValueOrEmpty dereferences an optional string for a response field that
// renders absence as "".
func stringValueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// resolveDatasetVersionActors fills CreatedByName in place for the summaries
// that name a user. A version stores the actor's id; turning it into a nickname
// is a display concern, so it is resolved at read time and a missing user (or a
// system-written version with no actor) simply leaves the field empty.
//
// One batched lookup serves a whole page of history; a failure is not fatal: the
// history still lists, the client falls back to the id.
func resolveDatasetVersionActors(ctx context.Context, summaries []DatasetVersionSummary) {
	if dao.DB == nil || len(summaries) == 0 {
		return
	}
	ids := make([]string, 0, len(summaries))
	seen := make(map[string]struct{}, len(summaries))
	for _, summary := range summaries {
		actor := strings.TrimSpace(summary.CreatedBy)
		if actor == "" {
			continue
		}
		if _, ok := seen[actor]; ok {
			continue
		}
		seen[actor] = struct{}{}
		ids = append(ids, actor)
	}
	if len(ids) == 0 {
		return
	}
	names, err := dao.NewUserDAO().GetNicknamesByIDs(ctx, dao.DB, ids)
	if err != nil {
		common.Warn("Failed to resolve dataset version actors", zap.Error(err))
		return
	}
	for i := range summaries {
		if name := strings.TrimSpace(names[summaries[i].CreatedBy]); name != "" {
			summaries[i].CreatedByName = name
		}
	}
}

// resolveDatasetVersionChangeDocuments names the documents a change set points
// at. A chunk entry stores only its document's id, and the panel has to say which
// document the chunk belongs to. The name is read on demand rather than stored
// with the change, so a renamed document shows its current name instead of the
// one it had when the change happened.
//
// Best effort: a document deleted from the dataset no longer has a row, and its
// entries keep the id alone.
func resolveDatasetVersionChangeDocuments(ctx context.Context, details []DatasetVersionChangeDetail) {
	if len(details) == 0 {
		return
	}
	ids := make([]string, 0, len(details))
	for _, detail := range details {
		if docID := strings.TrimSpace(detail.DocID); docID != "" {
			ids = append(ids, docID)
		}
	}
	names := documentNames(ctx, ids)
	for i := range details {
		if name := names[details[i].DocID]; name != "" {
			details[i].DocName = name
		}
	}
}

// resolveDatasetVersionChangeChunkVersions fills the chunk version each chunk
// entry produced.
//
// The number is not stored with the change: a chunk history row is anchored at
// the dataset version it was written under, so that version has to exist first,
// and the writer does not know the chunk's next number when it stores the set.
// It is read back here instead, one query for the whole set, on the pair the
// writer did store - the chunk id and the dataset version.
//
// One dataset version can write two rows for the same chunk: the state the
// change replaced and the one it produced. The highest is the entry's, which is
// what a reader clicking through expects to find.
//
// Best effort, like the document names above: a chunk whose history was pruned,
// or a deployment that keeps none, leaves the entry without a number rather than
// failing the page.
func resolveDatasetVersionChangeChunkVersions(ctx context.Context, datasetID string, datasetVersion int64, details []DatasetVersionChangeDetail) {
	if dao.DB == nil || datasetID == "" || datasetVersion <= 0 || len(details) == 0 {
		return
	}
	ids := make([]string, 0, len(details))
	seen := make(map[string]struct{}, len(details))
	for _, detail := range details {
		if detail.Entity != entity.DatasetVersionChangeEntityChunk {
			continue
		}
		chunkID := strings.TrimSpace(detail.ChunkID)
		if chunkID == "" {
			continue
		}
		if _, ok := seen[chunkID]; ok {
			continue
		}
		seen[chunkID] = struct{}{}
		ids = append(ids, chunkID)
	}
	if len(ids) == 0 {
		return
	}
	versions, err := dao.NewChunkVersionDAO().VersionsAtDatasetVersion(ctx, dao.DB, datasetID, datasetVersion, ids)
	if err != nil {
		common.Warn("Failed to read the chunk versions a dataset version produced",
			zap.String("dataset_id", datasetID),
			zap.Int64("version", datasetVersion),
			zap.Error(err))
		return
	}
	for i := range details {
		if details[i].Entity != entity.DatasetVersionChangeEntityChunk {
			continue
		}
		version, ok := versions[details[i].ChunkID]
		if !ok {
			continue
		}
		details[i].ChunkVersion = &version
	}
}

// documentNames resolves document ids to their current names, one query for the
// whole set. Ids that are empty, repeated, or no longer have a row are dropped
// from the result rather than mapped to an empty name.
func documentNames(ctx context.Context, ids []string) map[string]string {
	names := make(map[string]string, len(ids))
	for id, document := range documentRows(ctx, ids) {
		if document == nil || document.Name == nil {
			continue
		}
		names[id] = strings.TrimSpace(*document.Name)
	}
	return names
}

// documentRows answers the rows of the documents that still exist, keyed by id,
// one query for the whole set. A document the caller named but the dataset no
// longer holds is simply absent: its content state is unknown, which must not be
// read as "empty".
func documentRows(ctx context.Context, ids []string) map[string]*entity.Document {
	rows := make(map[string]*entity.Document, len(ids))
	if dao.DB == nil || len(ids) == 0 {
		return rows
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return rows
	}
	documents, err := dao.NewDocumentDAO().GetByIDs(ctx, dao.DB, unique)
	if err != nil {
		common.Warn("Failed to resolve dataset version documents", zap.Error(err))
		return rows
	}
	for _, document := range documents {
		if document != nil {
			rows[document.ID] = document
		}
	}
	return rows
}

// DatasetVersionDocument is one document a version held, as the panel renders it:
// the id the set recorded and the name that id answers to.
//
// The name is not stored with the set (see datasetversion.DocumentSet): a set is
// an immutable snapshot, a name is a mutable property of the document, and storing
// one inside the other is what makes a rename show up as history. It is resolved
// here instead, the same way changes[].doc_name already is.
//
// ChunkNum and RestoredVersion are filled for a restore version only, and they
// describe the document now, not at this version: what the restore left behind.
// A restore that could not rebuild a document's content leaves it with no chunks
// and the restore's marker on it, but a zero ChunkNum alone does not mean a
// re-parse is owed: a version taken before the document was parsed held no chunk
// for it, and nothing was lost. What the restore failed to fill is named by the
// restore response, not inferred here from the marker. The pair is null for a
// document the dataset no longer holds: its content state is unknown, which is
// not the same as empty.
type DatasetVersionDocument struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// ChunkNum is how many chunks the document holds now. Absent unless the
	// document still exists and the version is a restore.
	ChunkNum *int64 `json:"chunk_num,omitempty"`
	// RestoredVersion is the version the document's content was replayed from, or
	// null for content a parse produced. Absent under the same conditions as
	// ChunkNum.
	RestoredVersion *int64 `json:"restored_version,omitempty"`
}

// datasetVersionDocuments decodes a version's document set and resolves the name of
// every document it holds.
//
// The set arrives resolved, so it may have been recorded by an earlier version -
// the one that last changed the set. The names are therefore the ones the rename
// history says the documents had at this version, falling back to what they are
// called now.
//
// A restore also reports the content state of the documents it put back, which
// describes the document today rather than the version. The rows are read once
// and shared with the name lookup, so the restore answer costs no extra query.
func datasetVersionDocuments(ctx context.Context, datasetID string, row *entity.DatasetVersion) []DatasetVersionDocument {
	if row == nil {
		return []DatasetVersionDocument{}
	}
	ids, err := datasetversion.ParseDocuments(row.Documents)
	if err != nil {
		common.Warn("Failed to decode a dataset version document set", zap.Error(err))
		return []DatasetVersionDocument{}
	}
	if ids == nil {
		return []DatasetVersionDocument{}
	}
	current := documentRows(ctx, ids)
	names := datasetVersionDocumentNames(ctx, datasetID, row.Version, ids, current)
	documents := make([]DatasetVersionDocument, 0, len(ids))
	for _, id := range ids {
		document := DatasetVersionDocument{ID: id, Name: names[id]}
		if row.Kind == entity.DatasetVersionKindRestore {
			if held, ok := current[id]; ok && held != nil {
				chunkNum, restoredVersion := held.ChunkNum, held.RestoredVersion
				document.ChunkNum = &chunkNum
				document.RestoredVersion = restoredVersion
			}
		}
		documents = append(documents, document)
	}
	return documents
}

// datasetVersionDocumentNames answers the display names of a version's documents:
// the name a rename recorded at or before that version, then the name the first
// rename after it replaced, and otherwise the name the document carries now.
//
// The middle step is what makes an older version read correctly: a document
// renamed afterwards has no rename of its own at that number, and its row only
// carries the new name, so without it the version would show the current name
// against the content of the older one.
//
// A document that no longer exists keeps its historical name when a rename recorded
// one, and answers with no name at all when nothing ever named it - the panel then
// shows the id alone rather than inventing a name.
//
// current is the rows the caller already read (see documentRows): the same set
// answers the names and the content state, so a restore's document list costs one
// query rather than two.
func datasetVersionDocumentNames(ctx context.Context, datasetID string, version int64, ids []string, current map[string]*entity.Document) map[string]string {
	names := renamedDocumentNamesBefore(ctx, datasetID, version)
	if len(ids) == 0 {
		return names
	}
	unnamed := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && names[id] == "" {
			unnamed = append(unnamed, id)
		}
	}
	// A document renamed only after this version has no rename of its own here,
	// and its row carries the name it has now. The rename that moved it later is
	// where the earlier name survives, so it is read before the current rows.
	for id, name := range datasetversion.PreviousNamesBeforeRenames(ctx, dao.DB, datasetID, version, unnamed) {
		if names[id] == "" {
			names[id] = name
		}
	}
	for _, id := range unnamed {
		if names[id] != "" {
			continue
		}
		document, ok := current[id]
		if !ok || document == nil || document.Name == nil {
			continue
		}
		names[id] = strings.TrimSpace(*document.Name)
	}
	// A document that left the dataset has no row to read a name from. Its name
	// survives in the change set that recorded it - the upload that added it, the
	// delete that removed it - so read it from there. Only the ids nothing else
	// could name are looked up: the rename replay and the current rows answer for
	// every document that is still there.
	forgotten := make([]string, 0, len(unnamed))
	for _, id := range unnamed {
		if names[id] == "" {
			forgotten = append(forgotten, id)
		}
	}
	if len(forgotten) == 0 {
		return names
	}
	for id, name := range recordedDocumentNamesBefore(ctx, datasetID, version, forgotten) {
		if names[id] == "" {
			names[id] = name
		}
	}
	return names
}

// renamedDocumentNamesBefore replays the rename changes recorded at or before the
// given version, so a document renamed afterwards still answers with the name it
// had then. Restore versions are included for the same reason they carry renames:
// restoring a version renames documents back to what that version recorded.
func renamedDocumentNamesBefore(ctx context.Context, datasetID string, version int64) map[string]string {
	names := make(map[string]string)
	if dao.DB == nil || datasetID == "" || version < 0 {
		return names
	}
	var rows []*entity.DatasetVersion
	if err := dao.DB.WithContext(ctx).
		Select("version", "changes").
		Where("dataset_id = ? AND kind IN ? AND version <= ?",
			datasetID,
			[]string{entity.DatasetVersionKindDocumentRename, entity.DatasetVersionKindRestore},
			version).
		Order("version ASC").
		Find(&rows).Error; err != nil {
		common.Warn("Failed to read the renames of a dataset version",
			zap.String("dataset_id", datasetID), zap.Error(err))
		return names
	}
	for _, row := range rows {
		for _, change := range row.Changes {
			docID := strings.TrimSpace(change.DocID)
			if docID == "" || change.Name == "" {
				continue
			}
			if change.Entity != entity.DatasetVersionChangeEntityDocument ||
				change.Action != entity.DatasetVersionChangeActionRename {
				continue
			}
			names[docID] = change.Name
		}
	}
	return names
}

// recordedDocumentNamesBefore replays the names the dataset's change sets recorded
// for the given documents, up to and including the given version.
//
// A change set captures a document's name at write time - the upload that added
// it, the delete that removed it, the rename - because a deleted document has no
// row left for the panel to read a name from. The rename replay above answers the
// ids whose name a rename moved; this one answers the ids of the documents that
// left the dataset, which a version's set can still hold.
func recordedDocumentNamesBefore(ctx context.Context, datasetID string, version int64, ids []string) map[string]string {
	names := make(map[string]string, len(ids))
	if dao.DB == nil || datasetID == "" || version < 0 || len(ids) == 0 {
		return names
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return names
	}
	var rows []*entity.DatasetVersion
	if err := dao.DB.WithContext(ctx).
		Select("version", "changes").
		Where("dataset_id = ? AND version <= ?", datasetID, version).
		Order("version ASC").
		Find(&rows).Error; err != nil {
		common.Warn("Failed to read the names a dataset version recorded",
			zap.String("dataset_id", datasetID), zap.Error(err))
		return names
	}
	for _, row := range rows {
		for _, change := range row.Changes {
			name := strings.TrimSpace(change.Name)
			if name == "" || change.Entity != entity.DatasetVersionChangeEntityDocument {
				continue
			}
			docID := strings.TrimSpace(change.DocID)
			if _, ok := wanted[docID]; !ok {
				continue
			}
			// Ascending order, so the last record of an id is the newest one at or
			// before this version: a rename wins over the upload that preceded it.
			names[docID] = name
		}
	}
	return names
}

// datasetVersionCreatedAt reports the version's moment in milliseconds. A
// version row carries no other timestamp, so a row without it (written by hand
// before the hook existed) reports 0 rather than a guessed moment.
func datasetVersionCreatedAt(row *entity.DatasetVersion) int64 {
	if row.CreateTime != nil && *row.CreateTime > 0 {
		return *row.CreateTime
	}
	return 0
}

// ---------------------------------------------------------------------------
// Restore
// ---------------------------------------------------------------------------

// RestoreDatasetVersionRequest restores a dataset to a stored version. The
// restore itself runs in the content service (datasetcontent.Restore), which
// owns both halves - the configuration through PlanConfigRestoreTx, and the
// content for the kinds that record it; this type is the request it takes.
type RestoreDatasetVersionRequest struct {
	DatasetID string
	Version   int64
	// ExpectedVersion pins the version the caller read. When set and the dataset
	// has moved on, the restore is rejected before anything is written.
	ExpectedVersion *int64
	// DryRun answers with the changes a restore would apply, without writing.
	DryRun bool
}

// RestoreDatasetVersionResponse reports what a restore did (or would do). Every
// restore fills Changes and the document lists: a version records both halves of
// the dataset's state and the restore puts both back (see
// datasetversion.RestoresContent). MissingDocuments
// names the documents the version held but whose content could not be put back -
// their removal row was cleaned up, no chunk history reaches the anchor, or
// replaying it failed - and MissingDocumentReasons says which of those it was. A
// document named here may still exist (the row came back) with no chunks until it
// is re-parsed.
type RestoreDatasetVersionResponse struct {
	Version           int64    `json:"version"`
	PreVersion        int64    `json:"pre_version"`
	Applied           bool     `json:"applied"`
	RestoredDocuments []string `json:"restored_documents,omitempty"`
	DeletedDocuments  []string `json:"deleted_documents,omitempty"`
	MissingDocuments  []string `json:"missing_documents,omitempty"`
	// RestoredChunks is how many chunks the replay put back with the documents.
	RestoredChunks int `json:"restored_chunks,omitempty"`
	// BlockedReason is why a restore would be refused right now: the dry run
	// answers with it instead of a plan, so a client can refuse the action
	// before the user commits to it. The write path answers with the same
	// sentence as a conflict.
	BlockedReason string `json:"blocked_reason,omitempty"`
	// MissingDocumentReasons explains each entry of MissingDocuments, keyed by
	// document id: why the content could not be put back ("no_chunk_history",
	// "removal_gone", "replay_failed", "history_unreadable"). The list alone says
	// which documents to look at; the code says whether a re-parse would help.
	MissingDocumentReasons map[string]string `json:"missing_document_reasons,omitempty"`
}

// ConfigRestorePlan is what putting a stored configuration back would do: the
// column updates that realise it, the data-source bindings to reconcile, and the
// pagerank the chunks have to mirror once the transaction commits.
type ConfigRestorePlan struct {
	Updates       map[string]interface{}
	PagerankValue *int64
	// ConnectorBindings are the bindings the version recorded, applied only when
	// RestoreConnectors is set: they live in connector2kb rather than in Updates,
	// which carries dataset columns.
	ConnectorBindings []entity.JSONMap
	RestoreConnectors bool
}

// ConfigSnapshot is the configuration a version stores for a live dataset,
// including its data-source bindings. A restore compares the two to decide which
// columns and bindings it may touch.
func ConfigSnapshot(ctx context.Context, db *gorm.DB, kb *entity.Knowledgebase) (entity.JSONMap, error) {
	return datasetConfigSnapshot(ctx, db, kb)
}

// PlanConfigRestoreTx works out how to put a stored configuration back, under a
// row lock and without writing anything. The caller owns the transaction and calls
// this twice: before the content work, to refuse early and to read the head it is
// replacing, and again in the transaction that applies the plan and records the
// restore version, so one version describes the whole restore.
//
// expectedVersion is refused when the dataset has moved on since the caller read
// it; nil means "whatever the head is".
func (d *DatasetService) PlanConfigRestoreTx(
	ctx context.Context,
	tx *gorm.DB,
	datasetID string,
	expectedVersion *int64,
	stored entity.JSONMap,
) (*ConfigRestorePlan, *entity.Knowledgebase, error) {
	var kb entity.Knowledgebase
	if err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", datasetID).
		Take(&kb).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, datasetVersionError{code: common.CodeNotFound, message: "dataset not found"}
		}
		return nil, nil, fmt.Errorf("failed to lock the dataset: %w", err)
	}
	if expectedVersion != nil && kb.Version != *expectedVersion {
		return nil, nil, datasetVersionError{
			code:    common.CodeConflict,
			message: fmt.Sprintf("dataset version changed: expected %d, current is %d", *expectedVersion, kb.Version),
		}
	}

	current, err := datasetConfigSnapshot(ctx, tx, &kb)
	if err != nil {
		return nil, nil, err
	}
	// The columns to write come from comparing whole values: a nested key only one
	// side carries is still part of the configuration a restore puts back, so it
	// has to be written even though it would not be worth a line on the page.
	changedFields := datasetConfigRestoreColumns(current, stored)
	updates, err := datasetConfigRestoreUpdates(stored, changedFields)
	if err != nil {
		return nil, nil, err
	}
	if err := d.validateRestorableName(ctx, tx, &kb, updates); err != nil {
		return nil, nil, err
	}
	if err := d.validateRestorableEmbedding(ctx, &kb, stored); err != nil {
		return nil, nil, err
	}
	if err := d.checkRestorablePipeline(ctx, &kb, stored); err != nil {
		return nil, nil, err
	}

	plan := &ConfigRestorePlan{Updates: updates}
	if _, moved := updates["pagerank"]; moved {
		if value, ok := snapshotInt64(stored["pagerank"]); ok {
			plan.PagerankValue = &value
		}
	}
	if _, moved := changedFields["connectors"]; moved {
		bindings, recorded, bindErr := datasetversion.ParseConnectorBindings(stored)
		if bindErr != nil {
			return nil, nil, datasetVersionError{code: common.CodeDataError, message: "dataset version holds malformed data-source bindings"}
		}
		if recorded {
			plan.RestoreConnectors = true
			plan.ConnectorBindings = bindings
		}
	}
	return plan, &kb, nil
}

// ApplyConfigRestoreTx writes what a plan reported as different: the columns the
// dataset owns, and the data-source bindings, which are rows in connector2kb.
// Only the differences are written, so an unchanged column is never rewritten
// and an unmoved pagerank never triggers a chunk-wide update.
func (d *DatasetService) ApplyConfigRestoreTx(ctx context.Context, tx *gorm.DB, datasetID string, plan *ConfigRestorePlan) error {
	if plan == nil {
		return nil
	}
	if len(plan.Updates) > 0 {
		if err := tx.WithContext(ctx).
			Model(&entity.Knowledgebase{}).
			Where("id = ?", datasetID).
			Updates(plan.Updates).Error; err != nil {
			return err
		}
	}
	if plan.RestoreConnectors {
		return datasetversion.RestoreConnectorBindings(ctx, tx, datasetID, plan.ConnectorBindings)
	}
	return nil
}

// MirrorConfigPagerank copies a restored pagerank onto the stored chunks. Like
// UpdateDataset's mirror, the write lives outside the transaction, so a failure
// is reported rather than rolled back.
func (d *DatasetService) MirrorConfigPagerank(ctx context.Context, kb *entity.Knowledgebase, datasetID string, value int64) error {
	if kb == nil {
		return errors.New("dataset not loaded")
	}
	return d.updateDatasetPagerankChunks(ctx, datasetPagerankUpdate{
		value:     value,
		index:     fmt.Sprintf("ragflow_%s", kb.TenantID),
		datasetID: datasetID,
	})
}

// validateRestorableEmbedding refuses a restore whose recorded model the tenant
// can no longer resolve, instead of pointing the dataset at a model that would
// fail every future embedding call.
//
// Moving the model is otherwise allowed even when the dataset holds chunks: the
// stored vectors live in per-dimension columns (q_<dim>_vec), so the ones the
// other model produced stay where they are - no longer used for vector search
// while the dataset is on this model, usable again if it is restored back. That
// is the same thing changing the model through the update path does, and neither
// re-embeds existing content.
func (d *DatasetService) validateRestorableEmbedding(ctx context.Context, kb *entity.Knowledgebase, snapshot entity.JSONMap) error {
	embdID, ok := snapshotString(snapshot["embd_id"])
	if !ok || embdID == "" || embdID == kb.EmbdID {
		return nil
	}
	modelCtx := permission.WithTenantID(ctx, kb.TenantID)
	if available, message := d.verifyEmbeddingAvailability(modelCtx, embdID, kb.TenantID); !available {
		return datasetVersionError{
			code:    common.CodeDataError,
			message: fmt.Sprintf("cannot restore the embedding model %s: %s", embdID, message),
		}
	}
	return nil
}

// ApplyRestoreEmbeddingSwitch moves the dataset to the embedding model a version
// recorded, and answers with the change it applied (nil when the model already
// matches).
//
// A content restore needs this *before* it rebuilds any chunk: the replay
// recomputes each vector from its text, and those vectors have to belong to the
// model the dataset is being returned to rather than the one it is leaving.
// Applying the switch in the final configuration transaction would be too late -
// the chunks would already carry the other model's vectors, in a column the
// restored model never queries. It runs before the version it produces is
// recorded, and the version it replaces already names the model being left, so
// undoing the restore undoes the switch too; the configuration restore that
// follows finds the column equal and leaves it alone.
func (d *DatasetService) ApplyRestoreEmbeddingSwitch(ctx context.Context, datasetID string, stored entity.JSONMap) (*DatasetConfigChange, error) {
	if dao.DB == nil || datasetID == "" {
		return nil, nil
	}
	var change *DatasetConfigChange
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var kb entity.Knowledgebase
		if lockErr := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", datasetID).
			Take(&kb).Error; lockErr != nil {
			if errors.Is(lockErr, gorm.ErrRecordNotFound) {
				return datasetVersionError{code: common.CodeNotFound, message: "dataset not found"}
			}
			return fmt.Errorf("failed to lock the dataset: %w", lockErr)
		}
		updates, updateErr := datasetConfigRestoreUpdates(stored, map[string]struct{}{"embd_id": {}})
		if updateErr != nil {
			return updateErr
		}
		target, ok := updates["embd_id"]
		if !ok || target == kb.EmbdID {
			return nil
		}
		if validateErr := d.validateRestorableEmbedding(ctx, &kb, stored); validateErr != nil {
			return validateErr
		}
		if writeErr := tx.WithContext(ctx).
			Model(&entity.Knowledgebase{}).
			Where("id = ?", datasetID).
			Updates(updates).Error; writeErr != nil {
			return fmt.Errorf("failed to switch the dataset's embedding model: %w", writeErr)
		}
		change = &DatasetConfigChange{Field: "embd_id", Before: kb.EmbdID, After: target}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return change, nil
}

// RevertRestoreEmbeddingSwitch puts the embedding model back after a content
// restore failed past the point where the switch was applied.
//
// The switch runs in its own transaction, before any chunk is rebuilt, because
// the replay has to recompute vectors with the model being restored; nothing
// after it is transactional with it. A failure there - a document that could not
// be put back, a document that would not delete, the final configuration
// transaction - would therefore leave the dataset on a model no version
// describes, with chunks that are partly the old one's and partly the new one's.
// The revert only writes while the column still holds the model the switch
// applied: a change made meanwhile is the user's, not this restore's to undo.
func (d *DatasetService) RevertRestoreEmbeddingSwitch(ctx context.Context, datasetID string, change *DatasetConfigChange) error {
	if change == nil || change.Field != "embd_id" || change.Before == "" || datasetID == "" || dao.DB == nil {
		return nil
	}
	return dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var kb entity.Knowledgebase
		if err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", datasetID).
			Take(&kb).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("failed to lock the dataset: %w", err)
		}
		if kb.EmbdID != change.After {
			return nil
		}
		return tx.WithContext(ctx).
			Model(&entity.Knowledgebase{}).
			Where("id = ?", datasetID).
			Update("embd_id", change.Before).Error
	})
}

// checkRestorablePipeline refuses a restore whose recorded canvas no longer
// exists, instead of leaving the dataset pointing at nothing.
func (d *DatasetService) checkRestorablePipeline(ctx context.Context, kb *entity.Knowledgebase, snapshot entity.JSONMap) error {
	pipelineID, ok := snapshotString(snapshot["pipeline_id"])
	if !ok || strings.TrimSpace(pipelineID) == "" {
		return nil
	}
	if kb.PipelineID != nil && *kb.PipelineID == pipelineID {
		return nil
	}
	accessible, err := canvasAccessibleForUser(ctx, pipelineID)
	if err != nil {
		return err
	}
	if !accessible {
		return datasetVersionError{
			code:    common.CodeDataError,
			message: fmt.Sprintf("cannot restore the parse pipeline: canvas %s is no longer accessible", pipelineID),
		}
	}
	return nil
}

// validateRestorableName refuses a restore that would move the dataset's name
// onto one the tenant already uses. A name is part of what a version records, so
// restoring it is allowed; the tenant's uniqueness rule still applies, and the
// refusal names the colliding value instead of letting the unique key fail the
// whole transaction with a database error.
//
// The length is re-checked because a snapshot may predate the current column
// limit.
func (d *DatasetService) validateRestorableName(ctx context.Context, tx *gorm.DB, kb *entity.Knowledgebase, updates map[string]interface{}) error {
	nameValue, ok := updates["name"].(string)
	if !ok || kb == nil {
		return nil
	}
	if utf8.RuneCountInString(nameValue) > entity.DatasetNameLimit {
		return datasetVersionError{
			code:    common.CodeDataError,
			message: fmt.Sprintf("dataset name has at most %d characters", entity.DatasetNameLimit),
		}
	}
	available, err := common.NameAvailable(kb.Name, nameValue, func(candidate string) (bool, error) {
		var existing entity.Knowledgebase
		lookupErr := tx.WithContext(ctx).
			Where("LOWER(name) = LOWER(?) AND tenant_id = ? AND status = ?", candidate, kb.TenantID, string(entity.StatusValid)).
			First(&existing).Error
		if lookupErr != nil {
			if dao.IsNotFoundErr(lookupErr) {
				return false, nil
			}
			return false, lookupErr
		}
		return existing.ID != kb.ID, nil
	})
	if err != nil {
		return fmt.Errorf("failed to check the dataset name for the restore: %w", err)
	}
	if !available {
		return datasetVersionError{
			code:    common.CodeDataError,
			message: fmt.Sprintf("Dataset name '%s' already exists", nameValue),
		}
	}
	return nil
}

// datasetConfigRestoreUpdates turns a snapshot into the column updates a
// restore applies. Only restorableDatasetConfigFields are written, and only
// those the caller reports as changed; ACL columns are left alone.
func datasetConfigRestoreUpdates(snapshot entity.JSONMap, changed map[string]struct{}) (map[string]interface{}, error) {
	wanted := func(field string) bool {
		if changed == nil {
			return true
		}
		_, ok := changed[field]
		return ok
	}
	updates := make(map[string]interface{}, len(restorableDatasetConfigFields))
	if wanted("name") {
		if value, ok := snapshotString(snapshot["name"]); ok && strings.TrimSpace(value) != "" {
			updates["name"] = strings.TrimSpace(value)
		}
	}
	if wanted("avatar") {
		// A version written before the snapshot carried the avatar has no key,
		// which means no avatar: the restore clears it rather than skipping it.
		value, _ := snapshotString(snapshot["avatar"])
		// The snapshot was validated when it was written; validating it again
		// keeps a row edited by hand from putting anything but an image in the
		// column. An empty value clears the avatar, which is a valid restore.
		if err := validateDatasetAvatar(value); err != nil {
			return nil, datasetVersionError{
				code:    common.CodeDataError,
				message: fmt.Sprintf("dataset version holds an invalid avatar: %v", err),
			}
		}
		updates["avatar"] = value
	}
	if wanted("description") {
		if value, ok := snapshotString(snapshot["description"]); ok {
			updates["description"] = value
		}
	}
	if wanted("language") {
		if value, ok := snapshotString(snapshot["language"]); ok && value != "" {
			updates["language"] = value
		}
	}
	if wanted("embd_id") {
		if value, ok := snapshotString(snapshot["embd_id"]); ok && value != "" {
			updates["embd_id"] = value
			// tenant_embd_id points at the tenant model row the id resolves to, so
			// it moves with it. The snapshot records it only when it was set;
			// restoring a version recorded without one clears the pointer instead
			// of leaving it on the model this restore replaced.
			tenantValue, ok := snapshotString(snapshot["tenant_embd_id"])
			if ok && tenantValue != "" {
				updates["tenant_embd_id"] = tenantValue
			} else {
				updates["tenant_embd_id"] = nil
			}
		}
	}
	if wanted("parser_id") {
		if value, ok := snapshotString(snapshot["parser_id"]); ok && value != "" {
			updates["parser_id"] = value
		}
	}
	if wanted("pipeline_id") {
		if value, ok := snapshotString(snapshot["pipeline_id"]); ok {
			if value == "" {
				updates["pipeline_id"] = nil
			} else {
				updates["pipeline_id"] = value
			}
		}
	}
	if wanted("pipeline_version_id") {
		if value, ok := snapshotString(snapshot["pipeline_version_id"]); ok {
			if value == "" {
				updates["pipeline_version_id"] = nil
			} else {
				updates["pipeline_version_id"] = value
			}
		}
	}
	if wanted("parser_config") {
		if raw, ok := snapshot["parser_config"]; ok {
			parserConfig, ok := raw.(map[string]interface{})
			if !ok {
				return nil, datasetVersionError{code: common.CodeDataError, message: "dataset version holds a malformed parser_config"}
			}
			updates["parser_config"] = entity.JSONMap(parserConfig)
		}
	}
	if wanted("pagerank") {
		if value, ok := snapshotInt64(snapshot["pagerank"]); ok {
			updates["pagerank"] = value
		}
	}
	if wanted("similarity_threshold") {
		if value, ok := snapshotFloat64(snapshot["similarity_threshold"]); ok {
			updates["similarity_threshold"] = value
		}
	}
	if wanted("vector_similarity_weight") {
		if value, ok := snapshotFloat64(snapshot["vector_similarity_weight"]); ok {
			updates["vector_similarity_weight"] = value
		}
	}
	return updates, nil
}

// datasetConfigRestoreColumns decides which columns a restore writes.
//
// It compares whole values instead of reducing the leaf-level diff it reports:
// inside an object a key only one side carries is deliberately not reported (a
// save re-derives parser_config from the template, so components the older row
// never stored arrive as defaults), yet a restore has to reproduce the stored
// state exactly. Two cases depend on that:
//
//   - a parameter the version did not have is deleted, because the whole column
//     is written;
//   - a parser-method switch replaces the component set wholesale. General and
//     Paper share no chunker or tokenizer id, so the leaf diff names only
//     parser_id; writing that alone would leave the dataset on the restored
//     method holding the other template's component ids, which the runtime
//     ignores.
func datasetConfigRestoreColumns(before, stored entity.JSONMap) map[string]struct{} {
	fields := append(append([]string{}, restorableDatasetConfigFields...), "connectors")
	columns := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		beforeValue, beforeOK := datasetNormalizedJSON(datasetConfigSnapshotValue(before, field))
		afterValue, afterOK := datasetNormalizedJSON(datasetConfigSnapshotValue(stored, field))
		if beforeOK != afterOK || !datasetJSONEqual(beforeValue, afterValue) {
			columns[field] = struct{}{}
		}
	}
	return columns
}

// configDiffMaxDepth bounds how far a diff descends into nested objects. A
// parser config is one level of named parameters plus the occasional nested
// object; deeper than that one edit would read as a wall of paths.
const configDiffMaxDepth = 3

// datasetConfigFieldChanges answers which of the given fields differ between two
// snapshots, in the order the list gives them. A field that holds an object is
// descended into, so one changed parser parameter reads as that parameter instead
// of as the whole blob; appendConfigValueChanges holds the nested rules.
func datasetConfigFieldChanges(before, after entity.JSONMap, fields []string) []DatasetConfigChange {
	changes := make([]DatasetConfigChange, 0)
	for _, field := range fields {
		beforeValue, beforeOK := datasetNormalizedJSON(datasetConfigSnapshotValue(before, field))
		afterValue, afterOK := datasetNormalizedJSON(datasetConfigSnapshotValue(after, field))
		changes = appendConfigValueChanges(changes, field, beforeValue, beforeOK, afterValue, afterOK, 0)
	}
	return changes
}

// datasetConfigSnapshotValue reads a field the way a diff should see it: an
// avatar that a snapshot written before the field existed simply did not have,
// so it compares as the empty one rather than as a missing value. The data-source
// bindings get the same treatment - a snapshot written before they were recorded
// reads as a dataset with none, so its first comparison does not report a relink
// that never happened.
func datasetConfigSnapshotValue(snapshot entity.JSONMap, field string) interface{} {
	switch field {
	case "avatar":
		if _, ok := snapshot[field]; !ok {
			return ""
		}
	case "connectors":
		if _, ok := snapshot[field]; !ok {
			return []interface{}{}
		}
	}
	return snapshot[field]
}

// appendConfigValueChanges records one value's difference, or - when both sides
// hold an object - one line per key below it.
//
// Inside an object a key only one side carries is judged by the newer side: one
// the newer state no longer carries is silent, the panel listing the
// configuration as it stands, while one it newly carries reads as "— → value".
// A top-level field keeps its old reading - one that first appears is a real
// change (binding a pipeline) - and a field whose type changed is reported whole.
func appendConfigValueChanges(changes []DatasetConfigChange, path string, before interface{}, beforeOK bool, after interface{}, afterOK bool, depth int) []DatasetConfigChange {
	if depth > 0 && !afterOK {
		return changes
	}
	// Empty reads as "—" on the page, so a pair empty on both sides would say
	// "— → —"; a value that moved to empty ("text → —") still says something.
	if depth > 0 && !beforeOK && datasetConfigValueUnset(after) {
		return changes
	}
	beforeObject, beforeIsObject := before.(map[string]interface{})
	afterObject, afterIsObject := after.(map[string]interface{})
	if afterIsObject && depth < configDiffMaxDepth && (beforeIsObject || !beforeOK) {
		nested := make([]DatasetConfigChange, 0, len(beforeObject)+len(afterObject))
		for _, key := range configDiffKeys(beforeObject, afterObject) {
			beforeChild, beforeChildOK := beforeObject[key]
			afterChild, afterChildOK := afterObject[key]
			// A key that holds nothing is a key the row did not carry: the same
			// "—" a missing one reads as.
			nested = appendConfigValueChanges(nested, path+"."+key,
				beforeChild, beforeChildOK && beforeChild != nil,
				afterChild, afterChildOK && afterChild != nil,
				depth+1)
		}
		return append(changes, nested...)
	}
	if afterIsObject && depth < configDiffMaxDepth {
		// The value changed type: a scalar became an object, so the two sides are
		// not comparable key by key. Report the field as it stands.
		return append(changes, DatasetConfigChange{Field: path, Before: before, After: after})
	}
	if beforeOK == afterOK && datasetJSONEqual(before, after) {
		return changes
	}
	return append(changes, DatasetConfigChange{Field: path, Before: before, After: after})
}

// configDiffKeys lists the keys of two objects in one sorted pass, so a diff
// renders in a stable order instead of Go's map iteration order.
func configDiffKeys(before, after map[string]interface{}) []string {
	keys := make([]string, 0, len(before)+len(after))
	seen := make(map[string]struct{}, len(before)+len(after))
	for _, object := range []map[string]interface{}{before, after} {
		for key := range object {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// datasetNormalizedJSON reduces a snapshot value to plain JSON types. A missing
// key and an explicit null both answer false, which the diff treats as "absent"
// on both sides.
func datasetNormalizedJSON(value interface{}) (interface{}, bool) {
	if value == nil {
		return nil, false
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value), true
	}
	var normalized interface{}
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return string(encoded), true
	}
	return normalized, true
}

func snapshotString(value interface{}) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case nil:
		return "", true
	default:
		return fmt.Sprintf("%v", typed), true
	}
}

func snapshotInt64(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func snapshotFloat64(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// deleteDatasetVersionsTxEE drops a deleted dataset's history in the deletion
// transaction. Versions name a dataset that no longer exists, so nothing can
// restore them.
func (d *DatasetService) deleteDatasetVersionsTxEE(ctx context.Context, tx *gorm.DB, datasetID string) error {
	if datasetID == "" {
		return nil
	}
	// The change set lives in the rows themselves, so one delete takes the whole
	// history.
	return dao.NewDatasetVersionDAO().DeleteByDataset(ctx, tx, datasetID)
}
