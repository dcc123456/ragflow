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

// Package datasetversion appends the dataset version rows that the content
// paths (documents, chunks) produce. The configuration path keeps its own
// writer in the dataset service, because it must compare the state before and
// after a change; a content change always appends, so it shares this simpler
// writer instead. Both write the same table and advance the same dataset
// sequence, so one history holds configuration and content side by side.
package datasetversion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/documentset"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

// Snapshot is the state a version stores: every restorable column plus the
// identity fields the list displays. Derived counters are stored in their own
// columns, and nothing here is recomputed from another table, so a version
// renders and diffs without joining the live dataset.
func Snapshot(kb *entity.Knowledgebase) entity.JSONMap {
	if kb == nil {
		return nil
	}
	snapshot := entity.JSONMap{
		"name":                     kb.Name,
		"avatar":                   stringValue(kb.Avatar),
		"description":              stringValue(kb.Description),
		"language":                 stringValue(kb.Language),
		"embd_id":                  kb.EmbdID,
		"parser_id":                kb.ParserID,
		"pagerank":                 kb.Pagerank,
		"similarity_threshold":     kb.SimilarityThreshold,
		"vector_similarity_weight": kb.VectorSimilarityWeight,
		"permission":               kb.Permission,
		"parser_config":            copyMap(kb.ParserConfig),
	}
	if kb.PipelineID != nil {
		snapshot["pipeline_id"] = *kb.PipelineID
	}
	if kb.PipelineVersionID != nil {
		snapshot["pipeline_version_id"] = *kb.PipelineVersionID
	}
	if kb.TenantEmbdID != nil {
		snapshot["tenant_embd_id"] = *kb.TenantEmbdID
	}
	return snapshot
}

// ConnectorBindings are the data-source bindings a configuration snapshot
// records: which connector is linked to the dataset and whether a sync parses
// automatically. They live in connector2kb rather than on the dataset row, so a
// snapshot has to read them separately. Rows are ordered by connector id, so two
// snapshots of the same bindings serialize identically and compare equal.
func ConnectorBindings(ctx context.Context, db *gorm.DB, kbID string) ([]entity.JSONMap, error) {
	bindings := make([]entity.JSONMap, 0)
	if db == nil || kbID == "" {
		return bindings, nil
	}
	var rows []entity.Connector2Kb
	if err := db.WithContext(ctx).Where("kb_id = ?", kbID).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to load the dataset's data-source bindings: %w", err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ConnectorID < rows[j].ConnectorID })
	for _, row := range rows {
		bindings = append(bindings, entity.JSONMap{
			"id":         row.ConnectorID,
			"auto_parse": row.AutoParse,
		})
	}
	return bindings, nil
}

// SnapshotWithBindings is Snapshot plus the data-source bindings to record. It
// exists for the row that describes a state the transaction no longer holds: the
// baseline of a change that relinked data sources is seeded after the relink has
// already been written, so the caller has to hand in what the bindings were
// before it.
func SnapshotWithBindings(kb *entity.Knowledgebase, bindings []entity.JSONMap) entity.JSONMap {
	snapshot := Snapshot(kb)
	if snapshot == nil {
		return nil
	}
	if bindings == nil {
		bindings = make([]entity.JSONMap, 0)
	}
	snapshot["connectors"] = bindings
	return snapshot
}

// SnapshotWithConnectors is Snapshot plus the dataset's data-source bindings. A
// configuration version records them, so linking or unlinking a connector is a
// change the history shows and a restore can put back.
func SnapshotWithConnectors(ctx context.Context, db *gorm.DB, kb *entity.Knowledgebase) (entity.JSONMap, error) {
	if kb == nil {
		return nil, nil
	}
	bindings, err := ConnectorBindings(ctx, db, kb.ID)
	if err != nil {
		return nil, err
	}
	return SnapshotWithBindings(kb, bindings), nil
}

// ParseConnectorBindings decodes the bindings a configuration snapshot recorded,
// keeping only the fields a restore writes.
//
// The bool reports whether the snapshot records bindings at all. A version
// written before they were part of the snapshot has no key, and a restore has to
// read that as "not recorded" rather than as "the dataset had no data sources":
// the latter would unlink every data source the dataset has.
func ParseConnectorBindings(snapshot entity.JSONMap) ([]entity.JSONMap, bool, error) {
	raw, ok := snapshot["connectors"]
	if !ok || raw == nil {
		return nil, false, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, true, fmt.Errorf("dataset version holds malformed data-source bindings: %w", err)
	}
	var entries []map[string]interface{}
	if err := json.Unmarshal(encoded, &entries); err != nil {
		return nil, true, fmt.Errorf("dataset version holds malformed data-source bindings: %w", err)
	}
	bindings := make([]entity.JSONMap, 0, len(entries))
	positions := make(map[string]int, len(entries))
	for _, entry := range entries {
		id, _ := entry["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		autoParse, _ := entry["auto_parse"].(string)
		if autoParse == "" {
			autoParse = "1"
		}
		if position, seen := positions[id]; seen {
			bindings[position]["auto_parse"] = autoParse
			continue
		}
		positions[id] = len(bindings)
		bindings = append(bindings, entity.JSONMap{"id": id, "auto_parse": autoParse})
	}
	// The snapshot writer already orders the list; sorting here keeps a row
	// edited by hand from making two equal states compare different, and it
	// matches the order ConnectorBindings produces.
	sort.Slice(bindings, func(i, j int) bool {
		return bindingValue(bindings[i], "id") < bindingValue(bindings[j], "id")
	})
	return bindings, true, nil
}

// RestoreConnectorBindings makes connector2kb match the bindings a version
// recorded: a binding the dataset still has keeps its row, one that was linked
// since is removed, and one the version had is put back.
//
// A removed binding's queued sync tasks are canceled, because they would keep
// writing documents into a dataset the user just rolled back - the same thing an
// unlink does. A binding put back does not schedule a sync: the documents the
// earlier binding brought are still in the dataset, and a configuration restore
// records a state rather than producing content.
func RestoreConnectorBindings(ctx context.Context, tx *gorm.DB, kbID string, bindings []entity.JSONMap) error {
	if tx == nil || kbID == "" {
		return nil
	}
	var existing []entity.Connector2Kb
	if err := tx.WithContext(ctx).Where("kb_id = ?", kbID).Find(&existing).Error; err != nil {
		return fmt.Errorf("failed to load the dataset's data-source bindings: %w", err)
	}
	wanted := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		wanted[bindingValue(binding, "id")] = struct{}{}
	}
	kept := make(map[string]struct{}, len(existing))
	removed := make([]string, 0, len(existing))
	for _, row := range existing {
		if _, ok := wanted[row.ConnectorID]; ok {
			kept[row.ConnectorID] = struct{}{}
			continue
		}
		removed = append(removed, row.ConnectorID)
	}
	if len(removed) > 0 {
		if err := tx.WithContext(ctx).
			Where("kb_id = ? AND connector_id IN ?", kbID, removed).
			Delete(&entity.Connector2Kb{}).Error; err != nil {
			return fmt.Errorf("failed to remove the dataset's data-source bindings: %w", err)
		}
		if err := tx.WithContext(ctx).Model(&entity.SyncLogs{}).
			Where("connector_id IN ? AND kb_id = ? AND status IN ?",
				removed, kbID,
				[]string{string(entity.TaskStatusSchedule), string(entity.TaskStatusRunning)}).
			Update("status", string(entity.TaskStatusCancel)).Error; err != nil {
			return fmt.Errorf("failed to cancel the removed data sources' sync tasks: %w", err)
		}
	}
	for _, binding := range bindings {
		id := bindingValue(binding, "id")
		autoParse := bindingValue(binding, "auto_parse")
		if autoParse == "" {
			autoParse = "1"
		}
		if _, exists := kept[id]; exists {
			if err := tx.WithContext(ctx).Model(&entity.Connector2Kb{}).
				Where("connector_id = ? AND kb_id = ?", id, kbID).
				Update("auto_parse", autoParse).Error; err != nil {
				return fmt.Errorf("failed to restore a dataset data-source binding: %w", err)
			}
			continue
		}
		if err := tx.WithContext(ctx).Create(&entity.Connector2Kb{
			ID:          common.GenerateUUID(),
			ConnectorID: id,
			KbID:        kbID,
			AutoParse:   autoParse,
		}).Error; err != nil {
			return fmt.Errorf("failed to restore a dataset data-source binding: %w", err)
		}
	}
	return nil
}

// bindingValue reads one field of a decoded binding. The values are strings in
// every shape a snapshot is read in, so anything else is reported as empty and
// the caller applies its default.
func bindingValue(binding entity.JSONMap, field string) string {
	value, _ := binding[field].(string)
	return value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// copyMap copies the top level of a parser_config so a stored snapshot never
// aliases the live row's map.
func copyMap(source entity.JSONMap) entity.JSONMap {
	if source == nil {
		return entity.JSONMap{}
	}
	copied := make(entity.JSONMap, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

// RecordContentVersion appends one version for a content change (a document
// arrived or was removed, a parse ran, a chunk was edited) inside the caller's
// transaction.
//
// It locks the dataset row and the latest version row, so concurrent writers
// cannot allocate the same number, and reads the counters from the locked row:
// the content write that called it has already bumped doc_num/chunk_num, so the
// version records the counts the change produced.
// ErrVersionConflict is returned when the caller pinned the dataset version and
// the dataset has moved on since that version was read.
var ErrVersionConflict = errors.New("datasetversion: the dataset version changed")

func RecordContentVersion(ctx context.Context, tx *gorm.DB, datasetID, kind, actor string) (int64, error) {
	return RecordVersion(ctx, tx, datasetID, kind, actor, 0, nil)
}

// RecordContentVersionChanges records a content version together with the
// documents and chunks the change touched, inside the transaction of the write
// it describes.
//
// The history is the product here, not an observation of it: a version that
// could not be stored would leave a change no restore can reach, which reads
// back exactly like a change that never happened. So the failure is returned to
// the caller and the write that produced the version fails with it - the two
// commit together or not at all.
//
// The transaction must not hold a document engine call: a snapshot or a chunk
// body is written before the transaction opens, and the values the user sees are
// written after it commits, so the row lock this takes stays inside the metadata
// database.
func RecordContentVersionChanges(ctx context.Context, tx *gorm.DB, datasetID, kind string, changes []entity.DatasetVersionChange) (int64, error) {
	if tx == nil {
		return 0, errors.New("datasetversion: a content version needs the transaction of the write it describes")
	}
	return RecordVersionWithChanges(ctx, tx, datasetID, kind, actorOf(ctx), 0, nil, changes)
}

// maxVersionChanges bounds one version's change set. A bulk delete or a restore
// names one document per file and a batch chunk delete one per chunk, so an
// unbounded set would let a whole-dataset operation write a copy of the dataset
// into one row. The version's counter keeps the true size either way, which is
// what the panel reports when it shows the truncated list.
const maxVersionChanges = 1000

// RecordVersion appends one version of any kind inside the caller's transaction.
// expectedVersion pins the version the caller read: when set and the dataset has
// moved on, the write is refused instead of overwriting someone else's change.
func RecordVersion(ctx context.Context, tx *gorm.DB, datasetID, kind, actor string, restoredFrom int64, expectedVersion *int64) (int64, error) {
	return RecordVersionWithChanges(ctx, tx, datasetID, kind, actor, restoredFrom, expectedVersion, nil)
}

// RecordVersionWithChanges appends one version together with the documents and
// chunks it touched. changes may be nil: a version without a change set records
// that the content moved without claiming which rows did.
func RecordVersionWithChanges(ctx context.Context, tx *gorm.DB, datasetID, kind, actor string, restoredFrom int64, expectedVersion *int64, changes []entity.DatasetVersionChange) (int64, error) {
	return recordVersionWithChanges(ctx, tx, datasetID, kind, actor, restoredFrom, expectedVersion, changes, 0)
}

// RecordVersionWithChangesAtVersion records one version at the number reserved
// for it, together with the state it restores from and the change set.
//
// A restore stamps the chunk history rows its replay writes with the number the
// restore itself will carry (ReserveVersion), so the row that closes the restore
// has to land on that same number: taking the next one would leave the reserved
// number in the history as a gap, and one restore would advance the dataset
// twice - once for the rows it anchored, once for its own row.
//
// A non-positive version is not a pin: the writer takes the next number, exactly
// as RecordVersionWithChanges would. Callers that could not reserve a number
// (no history in the build, or the reservation failed) pass zero rather than
// inventing one.
func RecordVersionWithChangesAtVersion(ctx context.Context, tx *gorm.DB, datasetID, kind, actor string, restoredFrom int64, version int64, changes []entity.DatasetVersionChange) (int64, error) {
	return recordVersionWithChanges(ctx, tx, datasetID, kind, actor, restoredFrom, nil, changes, version)
}

// RecordContentVersionChangesAtVersion records a content version at the number
// already reserved for it.
//
// A parse stamps its chunk baselines with a version number before the run
// finishes (ReserveVersion), and the row that describes the result has to carry
// that same number: taking the next one would leave the reserved number in the
// history as a gap nothing explains. The caller therefore pins it here, and the
// history stays contiguous.
//
// The pin says where the row goes, not when it is written. Runs reserve their
// numbers before their content is done and close in whatever order that content
// finishes, so a run can close after a later number was already recorded; it
// still lands on its own number, because that is the anchor its baselines carry
// and no other writer can take it. What it inherits then follows the version
// before it rather than the head (see recordVersionWithChanges). The only
// refusal left is a number that already carries a row: two versions cannot share
// one.
func RecordContentVersionChangesAtVersion(ctx context.Context, tx *gorm.DB, datasetID, kind string, version int64, changes []entity.DatasetVersionChange) (int64, error) {
	if tx == nil {
		return 0, errors.New("datasetversion: a content version needs the transaction of the write it describes")
	}
	return recordVersionWithChanges(ctx, tx, datasetID, kind, actorOf(ctx), 0, nil, changes, version)
}

// RecordContentVersionChangesAtVersionOwnTx is RecordContentVersionChangesAtVersion
// with its own transaction, for a caller that has no transaction of its own open.
func RecordContentVersionChangesAtVersionOwnTx(ctx context.Context, datasetID, kind string, version int64, changes []entity.DatasetVersionChange) (int64, error) {
	if dao.DB == nil {
		return 0, errors.New("datasetversion: no database is open")
	}
	var recorded int64
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		recorded, err = RecordContentVersionChangesAtVersion(ctx, tx, datasetID, kind, version, changes)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("datasetversion: failed to record the %s version of dataset %s at %d: %w", kind, datasetID, version, err)
	}
	return recorded, nil
}

// PreviousNamesBeforeRenames answers what the given documents were called at
// dataset version version, for those a later rename moved.
//
// The versions at or before it cannot answer: a document renamed only afterwards
// has no rename of its own there, and its row carries the name it has now. The
// first rename recorded after the version can — it kept the name it replaced —
// so the earliest one per document is the name the document had then. A document
// whose first later entry left no previous name (a row written before the field
// existed) is absent rather than guessed at, and so is one no later rename
// touched.
func PreviousNamesBeforeRenames(ctx context.Context, database *gorm.DB, datasetID string, version int64, docIDs []string) map[string]string {
	names := make(map[string]string, len(docIDs))
	if database == nil || datasetID == "" || len(docIDs) == 0 {
		return names
	}
	wanted := make(map[string]struct{}, len(docIDs))
	for _, docID := range docIDs {
		if id := strings.TrimSpace(docID); id != "" {
			wanted[id] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return names
	}
	var rows []*entity.DatasetVersion
	if err := database.WithContext(ctx).
		Select("version", "changes").
		Where("dataset_id = ? AND kind IN ? AND version > ?",
			datasetID,
			[]string{entity.DatasetVersionKindDocumentRename, entity.DatasetVersionKindRestore},
			version).
		Order("version ASC").
		Find(&rows).Error; err != nil {
		common.Warn("Failed to read the renames recorded after a dataset version",
			zap.String("dataset_id", datasetID), zap.Error(err))
		return names
	}
	// settled marks the documents whose first later rename has been read, so a
	// second rename further down the timeline is not mistaken for the one that
	// first moved the document after the version.
	settled := make(map[string]struct{}, len(wanted))
	for _, row := range rows {
		for _, change := range row.Changes {
			docID := strings.TrimSpace(change.DocID)
			if _, ok := wanted[docID]; !ok {
				continue
			}
			if _, ok := settled[docID]; ok {
				continue
			}
			if change.Entity != entity.DatasetVersionChangeEntityDocument ||
				change.Action != entity.DatasetVersionChangeActionRename {
				continue
			}
			settled[docID] = struct{}{}
			if name := strings.TrimSpace(change.PreviousName); name != "" {
				names[docID] = name
			}
		}
	}
	return names
}

// recordVersionWithChanges appends one version. pinnedVersion, when positive, is
// the number the caller reserved for it; zero lets the writer take the next one.
func recordVersionWithChanges(ctx context.Context, tx *gorm.DB, datasetID, kind, actor string, restoredFrom int64, expectedVersion *int64, changes []entity.DatasetVersionChange, pinnedVersion int64) (int64, error) {
	if dao.DB == nil || tx == nil || datasetID == "" {
		return 0, nil
	}

	var kb entity.Knowledgebase
	if err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", datasetID).
		Take(&kb).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, fmt.Errorf("dataset %s not found", datasetID)
		}
		return 0, fmt.Errorf("failed to load the dataset for its version: %w", err)
	}
	if expectedVersion != nil && kb.Version != *expectedVersion {
		return 0, ErrVersionConflict
	}
	// A reserved number advances the dataset row without writing one (see
	// ReserveVersion), so the history table's head can lag behind the dataset's
	// own column. Nothing may reuse a number a chunk history row already carries,
	// so the next number is taken past whichever is higher.
	previousVersion := kb.Version

	versionDAO := dao.NewDatasetVersionDAO()
	latest, err := versionDAO.GetLatestForUpdate(ctx, tx, datasetID)
	latestExisted := err == nil
	version := int64(1)
	// inheritedConfig and inheritedDocuments are the configuration snapshot and
	// the document set this one runs under, as the content hashes that name them. A
	// content change moves neither, so it points at the head's hashes instead of
	// storing a snapshot nobody changed and a set nobody moved.
	var inheritedConfig *string
	var inheritedDocuments *string
	switch {
	case errors.Is(err, dao.ErrDatasetVersionNotFound):
		// No history yet: seed version 0 from the state the content change
		// started from. The dataset row is that state — a content change does
		// not touch the configuration — and this change takes the number after the
		// baseline, whichever baseline ends up there (another writer may have
		// seeded it first).
		baselineConfig, baselineDocuments, baselineErr := writeVersion(ctx, tx, &kb, 0, entity.DatasetVersionKindBaseline, actor, 0, nil, nil, nil)
		if baselineErr != nil {
			if !errors.Is(baselineErr, dao.ErrDatasetVersionExists) {
				return 0, fmt.Errorf("failed to record the dataset baseline version: %w", baselineErr)
			}
			// Another writer seeded the baseline between the lookup and the insert.
			// Its number is the starting point, so the head is read again and this
			// change takes the next free number instead of writing over the same one.
			seeded, seedErr := versionDAO.GetLatestForUpdate(ctx, tx, datasetID)
			if seedErr != nil {
				return 0, fmt.Errorf("failed to load the concurrently seeded dataset version: %w", seedErr)
			}
			version = seeded.Version + 1
			inheritedConfig = configHashOf(seeded)
			inheritedDocuments = seeded.DocumentsHash
			break
		}
		// The baseline just recorded the snapshot and the set, so a change that
		// moves neither can inherit from version 0 rather than record the same
		// state twice.
		inheritedConfig = baselineConfig
		inheritedDocuments = baselineDocuments
	case err != nil:
		return 0, fmt.Errorf("failed to load the latest dataset version: %w", err)
	default:
		version = latest.Version + 1
		inheritedConfig = configHashOf(latest)
		inheritedDocuments = latest.DocumentsHash
	}

	// A run that reserved an earlier number can close after one that reserved a
	// later number: both take their numbers up front and finish in whatever order
	// their content takes, and a parse of one document does not wait for another
	// document's parse. Such a run still lands on its own number - that number is
	// the anchor its chunk history rows carry, and no other writer can take it -
	// but what it inherits is the state the version *before* it carries, not the
	// head's: a restore to this number must bring back the configuration and the
	// document set that applied before it, not those of a version recorded after
	// it.
	//
	// A predecessor always exists: history starts at its baseline (version 0), and
	// the delete path refuses to remove a baseline. A read that finds none is a
	// broken history, and this write is refused rather than landing a version whose
	// configuration nothing can resolve.
	if pinnedVersion > 0 && latestExisted && latest.Version > pinnedVersion {
		previous, prevErr := versionDAO.GetPrevious(ctx, tx, datasetID, pinnedVersion)
		if prevErr != nil {
			return 0, fmt.Errorf("failed to load the version before %d of dataset %s: %w",
				pinnedVersion, datasetID, prevErr)
		}
		inheritedConfig = previous.ConfigHash
		inheritedDocuments = previous.DocumentsHash
	}

	// Reserved numbers live on the dataset row, not in the history, so a writer
	// that takes the next number skips past the reservation rather than reusing
	// it. A pinned number is the opposite case - it *is* the reservation, and the
	// rows anchored at it need the version row on the same number - so it is used
	// as given. Being reserved is all it has to be: it does not have to still be
	// the newest number, which is the part the runs that finish out of order
	// cannot promise. Only a number that already carries a row is refused, and
	// the insert below is what tells.
	if pinnedVersion > 0 {
		version = pinnedVersion
	} else if previousVersion >= version {
		version = previousVersion + 1
	}

	// The dataset's own timestamps move with the version: "updated" on the
	// dataset list means the last versioned change, and a content change touches
	// no other column of this row, so nothing else would advance them.
	//
	// The column only ever moves forward. It is the high-water mark every later
	// number is taken past, so a run closing on an older number must leave it
	// where it is: moving it back would hand the same number out twice.
	highWater := version
	if previousVersion > highWater {
		highWater = previousVersion
	}
	updateTime, updateDate := autoTimestamps()
	if err := tx.WithContext(ctx).
		Model(&entity.Knowledgebase{}).
		Where("id = ?", kb.ID).
		Updates(map[string]interface{}{
			"version":     highWater,
			"update_time": updateTime,
			"update_date": updateDate,
		}).Error; err != nil {
		return 0, fmt.Errorf("failed to advance the dataset version: %w", err)
	}
	kb.Version = highWater

	if _, _, err := writeVersion(ctx, tx, &kb, version, kind, actor, restoredFrom, changes, inheritedConfig, inheritedDocuments); err != nil {
		if errors.Is(err, dao.ErrDatasetVersionExists) {
			// The number already carries a row. A reserved number cannot be taken
			// by anyone else, so this is the one conflict left: the caller would be
			// overwriting a version that is not its own. Name it as a conflict so
			// the caller re-reads the head instead of surfacing a primary-key
			// error.
			return 0, ErrVersionConflict
		}
		return 0, fmt.Errorf("failed to record the dataset version: %w", err)
	}
	return version, nil
}

// ReserveVersion takes the next dataset version number without writing a version
// row, and returns it.
//
// A writer that has to stamp rows with the version they belong to before the
// version exists needs this: the parser writes a batch of chunk baselines while
// the parse is still running, and its version row is recorded once the run
// finishes. The baselines therefore take the number first, and the row that
// eventually describes them takes a later one, so a replay anchored at that row
// includes them.
//
// A restore reserves the number it will record and then pins its version row to
// it (RecordVersionWithChangesAtVersion): the chunk history rows the replay
// writes are what that version describes, so the number is reserved, stamped on
// them, and used for the row instead of being skipped over. Either way the
// reservation is the anchor, and one operation that reserves once advances the
// dataset by one version.
//
// The reservation commits, so a crash between it and the row leaves a number no
// row carries. That is harmless: nothing selects it as an anchor, ordering only
// compares numbers, and RecordVersionWithChanges allocates past the dataset's
// own column instead of reusing it.
func ReserveVersion(ctx context.Context, db *gorm.DB, datasetID string) (int64, error) {
	if dao.DB == nil || db == nil || datasetID == "" {
		return 0, nil
	}
	var reserved int64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var kb entity.Knowledgebase
		if err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", datasetID).
			Take(&kb).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("dataset %s not found", datasetID)
			}
			return fmt.Errorf("failed to load the dataset for a version reservation: %w", err)
		}
		next := kb.Version + 1
		if latest, err := dao.NewDatasetVersionDAO().GetLatestForUpdate(ctx, tx, datasetID); err == nil {
			if latest.Version >= next {
				next = latest.Version + 1
			}
		} else if !errors.Is(err, dao.ErrDatasetVersionNotFound) {
			return fmt.Errorf("failed to load the latest dataset version: %w", err)
		}
		updateTime, updateDate := autoTimestamps()
		if err := tx.WithContext(ctx).
			Model(&entity.Knowledgebase{}).
			Where("id = ?", kb.ID).
			Updates(map[string]interface{}{
				"version":     next,
				"update_time": updateTime,
				"update_date": updateDate,
			}).Error; err != nil {
			return fmt.Errorf("failed to reserve the dataset version: %w", err)
		}
		reserved = next
		return nil
	})
	if err != nil {
		return 0, err
	}
	return reserved, nil
}

// configHashOf reports which configuration snapshot applies to row, as the hash
// that names it in dataset_config_snapshot.
//
// It is how a version that changes no configuration inherits the pointer from
// the row it builds on instead of copying the snapshot: the hash is
// content-addressed, so writing the snapshot again would land on the same row
// anyway.
func configHashOf(row *entity.DatasetVersion) *string {
	if row == nil {
		return nil
	}
	return row.ConfigHash
}

// writeVersion stores one version row and reports the configuration snapshot and
// the document set it names, so a caller that immediately records another version
// under the same state can inherit them. inheritedConfig and inheritedDocuments are
// the hashes a content version adopts; a version whose kind records the state
// writes its own instead.
func writeVersion(ctx context.Context, tx *gorm.DB, kb *entity.Knowledgebase, version int64, kind, actor string, restoredFrom int64, changes []entity.DatasetVersionChange, inheritedConfig *string, inheritedDocuments *string) (*string, *string, error) {
	storedChanges, changesTotal := normalizeChanges(changes)
	row := &entity.DatasetVersion{
		DatasetID:    kb.ID,
		Version:      version,
		Kind:         kind,
		RestoredFrom: restoredFrom,
		CreatedBy:    actor,
		Changes:      storedChanges,
		ChangesTotal: changesTotal,
	}
	if entity.DatasetVersionKindRecordsState(kind) {
		snapshot, snapshotErr := SnapshotWithConnectors(ctx, tx, kb)
		if snapshotErr != nil {
			return nil, nil, snapshotErr
		}
		hash, hashErr := StoreConfigSnapshot(ctx, tx, kb.ID, snapshot)
		if hashErr != nil {
			return nil, nil, hashErr
		}
		row.ConfigHash = &hash
	} else {
		row.ConfigHash = inheritedConfig
	}
	if err := ApplyDocuments(ctx, tx, row, inheritedDocuments, kb.ID); err != nil {
		return nil, nil, err
	}
	if err := dao.NewDatasetVersionDAO().Create(ctx, tx, row); err != nil {
		return nil, nil, err
	}
	return row.ConfigHash, row.DocumentsHash, nil
}

// ApplyDocuments decides whether a version writes the document set or names the
// one the head carries.
//
// A version writes a set only when the kind can have moved it (see
// entity.DatasetVersionKindEstablishesDocuments); every other kind points at the
// head's hash. The set is one entry per document the dataset holds, so writing it
// on every parse and every chunk edit would make the history grow by O(documents)
// per content write - gigabytes for a dataset with many documents - while
// describing a set that did not move.
//
// inherited is the hash the head carries. It is nil when there is nothing to
// inherit from: no history yet. Recording then is the safe answer - the set is
// written anew on this version instead of naming a hash that might not resolve.
//
// A failed read is returned to the caller and the version is not written. Claiming
// to describe a set that was never stored would make every reader of that version
// fail on a dangling hash, which is the state "not recorded" exists to avoid.
func ApplyDocuments(ctx context.Context, db *gorm.DB, row *entity.DatasetVersion, inherited *string, datasetID string) error {
	if row == nil {
		return nil
	}
	if inherited != nil && !entity.DatasetVersionKindEstablishesDocuments(row.Kind) {
		row.DocumentsHash = inherited
		return nil
	}
	raw, err := DocumentSet(ctx, db, datasetID)
	if err != nil {
		return err
	}
	if raw == "" {
		// Nothing was read (no dataset on the row): leave the column NULL, which is
		// its own value for "not recorded". A hash of "[]" would claim a set the
		// version does not have, and a restore would read it as "delete every
		// document the dataset holds".
		return nil
	}
	hash := documentset.Hash([]byte(raw))
	if err := dao.NewDatasetDocumentSetSnapshotDAO().Upsert(ctx, db, datasetID, hash, raw); err != nil {
		return err
	}
	// The row keeps the resolved set in memory even though it is not a column, so a
	// caller inspecting the row it just wrote (a restore records versions as it
	// goes) sees the shape a read would produce.
	row.Documents = raw
	row.DocumentsHash = &hash
	return nil
}

// normalizeChanges turns a caller's change list into the set a version stores:
// entries that name nothing are dropped, repeats collapse, a document entry's
// doc id doubles as its identity, names are cut to the display limit, and the
// set stops at maxVersionChanges so a mass parse cannot dominate the row. The
// result is stored in display order — the documents a version touched before the
// chunks inside them — so a reader renders it as it is stored.
//
// The second result is how many distinct entries the caller named, before the
// set was cut. It is what lets a reader tell a complete change set from a sample:
// a stored set of maxVersionChanges entries says nothing on its own about whether
// the version touched exactly that many rows or a hundred thousand.
func normalizeChanges(changes []entity.DatasetVersionChange) (entity.DatasetVersionChanges, int) {
	if len(changes) == 0 {
		return nil, 0
	}
	normalized := make(entity.DatasetVersionChanges, 0, len(changes))
	seen := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		identity := change.Identity()
		if change.Entity == "" || identity == "" {
			continue
		}
		key := change.Entity + "\x00" + change.Action + "\x00" + identity
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		docID := change.DocID
		if change.Entity == entity.DatasetVersionChangeEntityDocument {
			// A document entry's identity is the document, so the two columns the
			// writer fills cannot disagree: one id names it.
			docID = identity
		}
		normalized = append(normalized, entity.DatasetVersionChange{
			DocID:   docID,
			Entity:  change.Entity,
			Action:  change.Action,
			ChunkID: change.ChunkID,
			Name:    truncateName(change.Name),
			// The name a rename moved away from is display text of the same kind:
			// it is cut by the same bound, and it is kept for the version read that
			// looks backwards through the timeline (see datasetVersionDocumentNames).
			PreviousName: truncateName(change.PreviousName),
			// The page commit an artifact entry points at is copied whole: it is an
			// id the reader opens the edit by, not display text, so cutting it would
			// point at nothing. Dropping it was the older behaviour, and the entry
			// then had no way back to the exact edit it described.
			CommitID:    change.CommitID,
			MetaChanges: boundMetadataChanges(change.MetaChanges),
		})
	}
	total := len(normalized)
	if total > maxVersionChanges {
		// The caller named more rows than one version stores. Its own slice is
		// already this size, so cutting here costs nothing that was not paid for
		// already, and the total survives in the column beside the sample.
		normalized = normalized[:maxVersionChanges]
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return changeRank(normalized[i].Entity) < changeRank(normalized[j].Entity)
	})
	return normalized, total
}

// changeRank orders a stored set for display: documents before the chunks inside
// them, and an unknown entity last instead of dropped.
func changeRank(entityName string) int {
	switch entityName {
	case entity.DatasetVersionChangeEntityDocument:
		return 0
	case entity.DatasetVersionChangeEntityChunk:
		return 1
	default:
		return 2
	}
}

// changeNameLimit bounds the stored name, in characters. Only a document entry
// carries one: a chunk entry stores its id, and its text is replayed from the
// chunk history when the version is read. It counts runes rather than bytes
// because the display does: a byte-based cut silently halved a Chinese file name
// and left the reader with no way to tell why.
const changeNameLimit = 2000

// truncateName keeps a stored name inside its column. Names are display text,
// so cutting from the end loses nothing a reader needs.
func truncateName(name string) string {
	runes := []rune(name)
	if len(runes) <= changeNameLimit {
		return name
	}
	return string(runes[:changeNameLimit])
}

// Metadata change bounds. A metadata value is free text and a document can hold
// many keys, so unlike the ids and names the rest of an entry carries, the diff
// has no natural size. The limits keep one version's change set from growing
// into the largest thing in the row while still naming the edits a reader acts
// on; a value cut short says so, instead of silently reporting a shorter one.
const (
	maxMetadataChanges = 20
	metadataValueLimit = 512
)

// boundMetadataChanges applies the diff's two limits: the number of keys kept,
// and the length of each value. Values are compared and shown as JSON, so a list
// stays a list; a value that is cut is cut mid-text and re-encoded as a string,
// which is the only shape a truncated JSON document can safely take.
func boundMetadataChanges(changes []entity.MetadataFieldChange) []entity.MetadataFieldChange {
	if len(changes) == 0 {
		return nil
	}
	if len(changes) > maxMetadataChanges {
		changes = changes[:maxMetadataChanges]
	}
	bounded := make([]entity.MetadataFieldChange, len(changes))
	for i, change := range changes {
		bounded[i] = entity.MetadataFieldChange{
			Key:    change.Key,
			Before: boundMetadataValue(change.Before),
			After:  boundMetadataValue(change.After),
		}
	}
	return bounded
}

// boundMetadataValue keeps one side of a change inside the value limit. An
// empty side stays empty - it is what says the key was added or removed.
func boundMetadataValue(value json.RawMessage) json.RawMessage {
	if len(value) <= metadataValueLimit {
		return value
	}
	runes := []rune(string(value))
	if len(runes) > metadataValueLimit {
		runes = runes[:metadataValueLimit]
	}
	clipped, err := json.Marshal(string(runes) + "…")
	if err != nil {
		return nil
	}
	return clipped
}

// autoTimestamps returns the pair the dataset row's update columns carry.
func autoTimestamps() (int64, time.Time) {
	now := time.Now().Local()
	return now.UnixMilli(), now.Truncate(time.Second)
}

// ParseDocuments decodes a stored document set into the ids it holds.
//
// The column is written only by DocumentSet, so the shape is the id list: anything
// else is a row edited by hand and is reported instead of guessed at.
func ParseDocuments(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// DocumentSet reads which documents the dataset holds now and returns the canonical
// JSON a version's document set is made of, so a restore knows the document set to
// put back and a reader can see what a version was made of. The documents
// themselves live in the document table or, for a removed one, in its removal
// row.
//
// It records ids only. A name is not part of what a version is: it can change
// without the set moving, it belongs to the document rather than to the set, and
// a reader resolves it when it renders the version. Storing it here would put a
// copy of a mutable field into an immutable snapshot, which is the shape that
// makes a rename show yesterday's name in today's panel.
//
// The JSON is what gets hashed and what the row stores (see documentset.Encode):
// one shape, so the hash and the row cannot disagree.
//
// ApplyDocuments is what decides whether a version calls this: only the kinds that
// can move the set do, and the rest inherit the head's hash.
//
// A failed lookup returns an error, and the version that would have written the
// set is not written: a swallowed error would leave a version naming nothing while
// the panel reads it as a version that held no documents.
//
// The ids are ordered so the same set always hashes the same and stores to the
// same text: two versions made of the same documents then share one row of
// dataset_document_set_snapshot, and a panel that renders the set does not shuffle
// it between reads. A database is free to return rows in any order without an
// ORDER BY, and the order it picks can change with the plan.
func DocumentSet(ctx context.Context, db *gorm.DB, datasetID string) (string, error) {
	if db == nil || datasetID == "" {
		return "", nil
	}
	var ids []string
	if err := db.WithContext(ctx).
		Model(&entity.Document{}).
		Where("kb_id = ?", datasetID).
		Order("id").
		Pluck("id", &ids).Error; err != nil {
		return "", fmt.Errorf("read the documents of dataset %s: %w", datasetID, err)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return "", fmt.Errorf("encode the documents of dataset %s: %w", datasetID, err)
	}
	return documentset.Encode(encoded), nil
}

// RecordContentVersionChangesOwnTx records a content version in a transaction of
// its own, for a flow whose content write has already left the metadata database
// - an engine write, a row that committed earlier.
//
// The transaction covers the version and nothing else, but the failure still
// reaches the caller: a change whose version could not be stored fails the
// operation that produced it, so the history never silently misses a change. A
// caller that still owns a transaction should pass it to
// RecordContentVersionChanges instead, so the two commit together.
func RecordContentVersionChangesOwnTx(ctx context.Context, datasetID, kind string, changes []entity.DatasetVersionChange) (int64, error) {
	if dao.DB == nil {
		return 0, errors.New("datasetversion: no database is open")
	}
	var version int64
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		recorded, err := RecordContentVersionChanges(ctx, tx, datasetID, kind, changes)
		version = recorded
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("datasetversion: failed to record the %s version of dataset %s: %w", kind, datasetID, err)
	}
	return version, nil
}

// actorKey carries the user a background run attributes its history to.
type actorKey struct{}

// WithActor names the user a version write should be attributed to. A background
// run — the ingestion worker closing a parse — executes on behalf of the user who
// queued the task, but its context is the worker's rather than that request's, so
// the identity has no way to travel except explicitly. A request context needs
// nothing: it already carries the authenticated user.
func WithActor(ctx context.Context, actor string) context.Context {
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, actorKey{}, actor)
}

// actorOf returns the user a write is attributed to: the actor the caller named
// when it has one, otherwise the authenticated user of the request.
func actorOf(ctx context.Context) string {
	if ctx != nil {
		if actor, _ := ctx.Value(actorKey{}).(string); actor != "" {
			return actor
		}
	}
	return permission.UserIDFromContext(ctx)
}
