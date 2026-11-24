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

package datasetversion

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func setupVersionTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to access sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	if err := db.AutoMigrate(
		&entity.Knowledgebase{},
		&entity.DatasetVersion{},
		// The configuration each version ran under lives in its own table, keyed
		// by content; a version points at it.
		&entity.DatasetConfigSnapshot{},
		// The document set each version describes is stored the same way.
		&entity.DatasetDocumentSetSnapshot{},
		&entity.Document{},
		// A recorded state includes the dataset's data-source bindings, which live
		// in their own table; a restore reconciles it and cancels sync tasks.
		&entity.Connector2Kb{},
		&entity.SyncLogs{},
	); err != nil {
		t.Fatalf("failed to migrate version tables: %v", err)
	}

	oldDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = oldDB })
	return db
}

func insertVersionTestDataset(t *testing.T, db *gorm.DB, id string) {
	t.Helper()

	kb := &entity.Knowledgebase{
		ID:           id,
		TenantID:     "tenant-1",
		Name:         "Dataset",
		CreatedBy:    "tenant-1",
		ParserConfig: entity.JSONMap{"chunk_token_num": float64(128)},
	}
	if err := db.Create(kb).Error; err != nil {
		t.Fatalf("insert test dataset: %v", err)
	}
}

func listVersionRows(t *testing.T, db *gorm.DB, datasetID string) []*entity.DatasetVersion {
	t.Helper()

	var rows []*entity.DatasetVersion
	if err := db.Where("dataset_id = ?", datasetID).Order("version").Find(&rows).Error; err != nil {
		t.Fatalf("list dataset versions: %v", err)
	}
	return rows
}

// countConfigSnapshots is how many distinct configuration snapshots the dataset
// stores: the rows that versions point at, one per distinct content.
func countConfigSnapshots(t *testing.T, db *gorm.DB, datasetID string) int64 {
	t.Helper()

	var count int64
	if err := db.Model(&entity.DatasetConfigSnapshot{}).Where("dataset_id = ?", datasetID).Count(&count).Error; err != nil {
		t.Fatalf("count dataset configuration snapshots: %v", err)
	}
	return count
}

// countDocumentSets is how many distinct document sets the dataset stores: one row
// per distinct content, however many versions name it.
func countDocumentSets(t *testing.T, db *gorm.DB, datasetID string) int64 {
	t.Helper()

	var count int64
	if err := db.Model(&entity.DatasetDocumentSetSnapshot{}).Where("dataset_id = ?", datasetID).Count(&count).Error; err != nil {
		t.Fatalf("count dataset document sets: %v", err)
	}
	return count
}

// storedDocumentSet reads which documents the dataset holds now and returns the
// text a version row stores for them.
func storedDocumentSet(t *testing.T, db *gorm.DB, datasetID string) string {
	t.Helper()

	raw, err := DocumentSet(t.Context(), db, datasetID)
	if err != nil {
		t.Fatalf("read the document set: %v", err)
	}
	return raw
}

// TestRecordContentVersionAppendsAfterContentWrite pins the contract the
// document and chunk paths rely on: the counters a content write already bumped
// land on the version, and the version adopts the new dataset number.
func TestRecordContentVersionAppendsAfterContentWrite(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")

	// The content write happens first, exactly as InsertDocument does it.
	if err := db.Model(&entity.Knowledgebase{}).
		Where("id = ?", "kb-1").
		Updates(map[string]interface{}{
			"doc_num":   gorm.Expr("doc_num + 1"),
			"chunk_num": gorm.Expr("chunk_num + 3"),
		}).Error; err != nil {
		t.Fatalf("bump counters: %v", err)
	}

	var version int64
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		version, err = RecordContentVersion(t.Context(), tx, "kb-1", entity.DatasetVersionKindUpload, "user-1")
		return err
	}); err != nil {
		t.Fatalf("record content version: %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1 (baseline is seeded first)", version)
	}

	rows := listVersionRows(t, db, "kb-1")
	if len(rows) != 2 {
		t.Fatalf("versions = %d, want 2 (baseline, upload)", len(rows))
	}
	if rows[0].Version != 0 || rows[0].Kind != entity.DatasetVersionKindBaseline {
		t.Fatalf("first row = v%d/%s, want v0/baseline", rows[0].Version, rows[0].Kind)
	}
	if rows[1].Version != 1 || rows[1].Kind != entity.DatasetVersionKindUpload {
		t.Fatalf("second row = v%d/%s, want v1/upload", rows[1].Version, rows[1].Kind)
	}
	// The content change did not move the configuration, so the row points at the
	// snapshot the baseline recorded instead of storing a copy.
	if rows[0].ConfigHash == nil {
		t.Fatal("baseline recorded no configuration snapshot")
	}
	if rows[1].ConfigHash == nil || *rows[1].ConfigHash != *rows[0].ConfigHash {
		t.Fatalf("content version config_hash = %v, want the baseline %v", rows[1].ConfigHash, rows[0].ConfigHash)
	}
	if stored := countConfigSnapshots(t, db, "kb-1"); stored != 1 {
		t.Fatalf("stored snapshots = %d, want 1 (the content version copies nothing)", stored)
	}
	resolved, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(t.Context(), db, "kb-1", 1)
	if err != nil {
		t.Fatalf("load the version: %v", err)
	}
	if got := resolved.Config["name"]; got != "Dataset" {
		t.Fatalf("resolved config name = %v, want the live name", got)
	}
	if rows[1].CreatedBy != "user-1" {
		t.Fatalf("created_by = %q, want the acting user", rows[1].CreatedBy)
	}

	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if kb.Version != 1 {
		t.Fatalf("dataset version = %d, want 1", kb.Version)
	}
}

// TestRecordContentVersionAlwaysAppends guards the difference from the
// configuration writer: two content changes with an identical configuration are
// two rows, because the content did change.
func TestRecordContentVersionAlwaysAppends(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")

	for i := 0; i < 2; i++ {
		if err := db.Transaction(func(tx *gorm.DB) error {
			_, err := RecordContentVersion(t.Context(), tx, "kb-1", entity.DatasetVersionKindChunk, "user-1")
			return err
		}); err != nil {
			t.Fatalf("record chunk version: %v", err)
		}
	}

	rows := listVersionRows(t, db, "kb-1")
	if len(rows) != 3 {
		t.Fatalf("versions = %d, want 3 (baseline, chunk, chunk)", len(rows))
	}
	for _, row := range rows[1:] {
		if row.ConfigHash == nil || *row.ConfigHash != *rows[0].ConfigHash {
			t.Fatalf("v%d config_hash = %v, want the baseline's %v", row.Version, row.ConfigHash, rows[0].ConfigHash)
		}
	}
	// Two content versions under one configuration: one stored snapshot between
	// them, not a copy each.
	if stored := countConfigSnapshots(t, db, "kb-1"); stored != 1 {
		t.Fatalf("stored snapshots = %d, want 1", stored)
	}
}

// TestContentVersionInheritsTheLatestConfigAnchor pins the pointer moving with
// the configuration: a content version written after a configuration change
// adopts the new anchor, so it reads the configuration it ran under and never
// an older one.
func TestContentVersionInheritsTheLatestConfigAnchor(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")

	record := func(kind string) int64 {
		t.Helper()
		var version int64
		if err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			version, err = RecordContentVersion(t.Context(), tx, "kb-1", kind, "user-1")
			return err
		}); err != nil {
			t.Fatalf("record %s version: %v", kind, err)
		}
		return version
	}

	if version := record(entity.DatasetVersionKindUpload); version != 1 {
		t.Fatalf("upload version = %d, want 1", version)
	}
	// The configuration moves: the dataset is renamed, which a configuration
	// version records.
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").
		Update("name", "Renamed").Error; err != nil {
		t.Fatalf("rename the dataset: %v", err)
	}
	var configVersion int64
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		configVersion, err = RecordVersion(t.Context(), tx, "kb-1", entity.DatasetVersionKindDatasetConfig, "user-1", 0, nil)
		return err
	}); err != nil {
		t.Fatalf("record the configuration version: %v", err)
	}
	chunkVersion := record(entity.DatasetVersionKindChunk)

	rows := listVersionRows(t, db, "kb-1")
	byVersion := make(map[int64]*entity.DatasetVersion, len(rows))
	for _, row := range rows {
		byVersion[row.Version] = row
	}

	if config := byVersion[configVersion]; config == nil || config.ConfigHash == nil {
		t.Fatalf("configuration version = %#v, want a stored snapshot", config)
	}
	old := byVersion[1]
	if old.ConfigHash == nil || *old.ConfigHash != *byVersion[0].ConfigHash {
		t.Fatalf("upload version config_hash = %v, want the baseline's", old.ConfigHash)
	}
	chunk := byVersion[chunkVersion]
	if chunk.ConfigHash == nil || *chunk.ConfigHash != *byVersion[configVersion].ConfigHash {
		t.Fatalf("chunk version config_hash = %v, want the configuration version's", chunk.ConfigHash)
	}
	if old.ConfigHash != nil && byVersion[configVersion].ConfigHash != nil && *old.ConfigHash == *byVersion[configVersion].ConfigHash {
		t.Fatal("the rename produced the same configuration hash as the baseline")
	}

	resolved, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(t.Context(), db, "kb-1", chunkVersion)
	if err != nil {
		t.Fatalf("load the chunk version: %v", err)
	}
	if got := resolved.Config["name"]; got != "Renamed" {
		t.Fatalf("resolved name = %v, want the configuration it ran under", got)
	}
}

// TestVersionWithMissingConfigAnchorFails stops a dangling pointer from being
// read as an empty configuration: a restore would then wipe the settings
// instead of raising them.
func TestVersionWithMissingConfigAnchorFails(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")

	missing := strings.Repeat("ab", 16)
	if err := db.Create(&entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindChunk,
		ConfigHash: &missing,
	}).Error; err != nil {
		t.Fatalf("seed a version with a dangling hash: %v", err)
	}

	_, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(t.Context(), db, "kb-1", 1)
	if err == nil {
		t.Fatal("a missing configuration snapshot should be an error")
	}
	// The hash is a content id: the message names enough of it to find the row,
	// not all 64 characters.
	if !strings.Contains(err.Error(), missing[:8]) {
		t.Fatalf("error = %v, want the hash named", err)
	}
}

// TestWithActorNamesTheWriterOfABackgroundRun covers the seam the ingestion
// worker uses: its context is the worker's, so the user who pressed Parse has to
// be handed over explicitly or the parse version lands with no author and reads
// as a change nobody made.
func TestWithActorNamesTheWriterOfABackgroundRun(t *testing.T) {
	request := permission.WithIdentity(t.Context(), "user-1", "tenant-1")
	if got := actorOf(request); got != "user-1" {
		t.Fatalf("actorOf(request) = %q, want user-1", got)
	}
	if got := actorOf(t.Context()); got != "" {
		t.Fatalf("actorOf(background) = %q, want empty", got)
	}

	named := WithActor(t.Context(), "user-2")
	if got := actorOf(named); got != "user-2" {
		t.Fatalf("actorOf(named) = %q, want user-2", got)
	}
	// The name wins over whatever the context already carries: the run acts for
	// the user who queued it, not for a user the context happens to hold.
	if got := actorOf(WithActor(request, "user-2")); got != "user-2" {
		t.Fatalf("actorOf(override) = %q, want user-2", got)
	}
	// An unnamed requester changes nothing, so the context's own identity stays.
	if got := actorOf(WithActor(request, "")); got != "user-1" {
		t.Fatalf("actorOf(empty override) = %q, want user-1", got)
	}
	// The name survives the detachment the history write applies to the context.
	if got := actorOf(context.WithoutCancel(named)); got != "user-2" {
		t.Fatalf("actorOf(WithoutCancel) = %q, want user-2", got)
	}
}

// TestParseDocumentsReadsTheIdList pins the shape a row holds: ids, ordered, and
// nothing else. Anything that is not a JSON array is an error rather than an empty
// set - "unreadable" and "held no documents" are different answers.
func TestParseDocumentsReadsTheIdList(t *testing.T) {
	ids, err := ParseDocuments(`["doc-1","doc-2"]`)
	if err != nil {
		t.Fatalf("parse ids: %v", err)
	}
	if len(ids) != 2 || ids[0] != "doc-1" || ids[1] != "doc-2" {
		t.Fatalf("ids = %#v, want doc-1 and doc-2", ids)
	}

	if empty, err := ParseDocuments(""); err != nil || empty != nil {
		t.Fatalf("empty document set = %#v, %v, want nothing", empty, err)
	}
	if none, err := ParseDocuments("[]"); err != nil || len(none) != 0 {
		t.Fatalf("empty list = %#v, %v, want no ids and no error", none, err)
	}
	if _, err := ParseDocuments("{"); err == nil {
		t.Fatal("malformed document set decoded without an error")
	}
}

// TestDocumentSetRecordsIdsOnly pins what a version records: the ids of the
// documents it held, and no name - a name can change without the set moving, so a
// copy inside an immutable snapshot would be stale instead of historical.
func TestDocumentSetRecordsIdsOnly(t *testing.T) {
	db := setupVersionTestDB(t)
	dao.DB = db
	t.Cleanup(func() { dao.DB = nil })
	insertVersionTestDataset(t, db, "kb-1")

	name := "manual.pdf"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ChunkNum: 4, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}

	documentSet := storedDocumentSet(t, db, "kb-1")
	if documentSet == "" {
		t.Fatal("document set = empty, want the document set recorded")
	}
	if strings.Contains(documentSet, "manual.pdf") {
		t.Fatalf("stored set = %q, want no document name in it", documentSet)
	}
	ids, err := ParseDocuments(documentSet)
	if err != nil {
		t.Fatalf("parse document set: %v", err)
	}
	if len(ids) != 1 || ids[0] != "doc-1" {
		t.Fatalf("ids = %#v, want doc-1", ids)
	}
}

// TestDocumentSetOrdersByID keeps the encoded set comparable: the document
// rows come back in whatever order the database picks, so without an explicit
// order two versions made of the same documents could encode differently and a
// reader could see the list shuffle between two reads of one version.
func TestDocumentSetOrdersByID(t *testing.T) {
	db := setupVersionTestDB(t)
	dao.DB = db
	t.Cleanup(func() { dao.DB = nil })
	insertVersionTestDataset(t, db, "kb-1")

	for _, id := range []string{"doc-c", "doc-a", "doc-b"} {
		name := "file-" + id
		if err := db.Create(&entity.Document{
			ID: id, KbID: "kb-1", Name: &name, ChunkNum: 1, ParserConfig: entity.JSONMap{},
		}).Error; err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	first := storedDocumentSet(t, db, "kb-1")
	if first == "" {
		t.Fatal("document set = empty, want the document set recorded")
	}
	ids, err := ParseDocuments(first)
	if err != nil {
		t.Fatalf("parse document set: %v", err)
	}
	if len(ids) != 3 || ids[0] != "doc-a" || ids[1] != "doc-b" || ids[2] != "doc-c" {
		t.Fatalf("ids = %#v, want them ordered by id", ids)
	}

	second := storedDocumentSet(t, db, "kb-1")
	if second != first {
		t.Fatalf("second read = %q, want the same text as the first", second)
	}
}

// TestDocumentSetIsStoredAsText pins the storage shape: the column holds the JSON
// itself, so a hand-written SELECT shows what a version held and no codec sits
// between the hash and the row.
func TestDocumentSetIsStoredAsText(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")

	const documents = 200
	seeded := make([]entity.Document, 0, documents)
	for i := 0; i < documents; i++ {
		name := fmt.Sprintf("manual-%03d.pdf", i)
		seeded = append(seeded, entity.Document{
			ID: fmt.Sprintf("%032x", i), KbID: "kb-1", Name: &name,
			ChunkNum: int64(i), ParserConfig: entity.JSONMap{},
		})
	}
	if err := db.Create(&seeded).Error; err != nil {
		t.Fatalf("seed documents: %v", err)
	}

	documentSet := storedDocumentSet(t, db, "kb-1")
	if documentSet == "" {
		t.Fatal("document set = empty, want the document set recorded")
	}
	if !strings.HasPrefix(documentSet, "[") {
		t.Fatalf("stored document set = %.24q, want the JSON array itself", documentSet)
	}

	ids, err := ParseDocuments(documentSet)
	if err != nil {
		t.Fatalf("read the stored set: %v", err)
	}
	if len(ids) != documents {
		t.Fatalf("ids = %d, want %d", len(ids), documents)
	}
	if ids[0] != fmt.Sprintf("%032x", 0) {
		t.Fatalf("first id = %q, want the first seeded document", ids[0])
	}
}

// TestDocumentSetRecordsAnEmptySet pins the value that must not collapse: a
// dataset with no documents records "[]", which a restore reads as "no documents",
// while an empty column means "not recorded" and refuses the restore.
func TestDocumentSetRecordsAnEmptySet(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")

	empty := storedDocumentSet(t, db, "kb-1")
	if empty != "[]" {
		t.Fatalf("empty set = %q, want the empty JSON array", empty)
	}
	ids, err := ParseDocuments(empty)
	if err != nil || len(ids) != 0 {
		t.Fatalf("empty set = (%#v, %v), want no documents and no error", ids, err)
	}
}

// TestRecordContentVersionSkipsWhenUnconfigured keeps the writer safe in the
// paths that run without a database (CLI helpers, unit tests).
func TestRecordContentVersionSkipsWhenUnconfigured(t *testing.T) {
	oldDB := dao.DB
	dao.DB = nil
	t.Cleanup(func() { dao.DB = oldDB })

	version, err := RecordContentVersion(t.Context(), nil, "kb-1", entity.DatasetVersionKindUpload, "user-1")
	if err != nil || version != 0 {
		t.Fatalf("unconfigured call = (%d, %v), want (0, nil)", version, err)
	}
}

// TestDocumentSetInheritedByVersionsThatCannotMoveIt pins the storage rule: the
// set is one entry per document, so only a version whose kind can move it writes
// one. A parse and a chunk edit leave every document row where it was, so they
// name the head's hash instead - which is what keeps a history of a large dataset
// from growing by O(documents) per content write.
//
// The versions that do write one land on the same row here: none of the writes
// below moved a document, so all five name the same content.
func TestDocumentSetInheritedByVersionsThatCannotMoveIt(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")
	name := "manual.pdf"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ChunkNum: 4, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}

	record := func(kind string) int64 {
		t.Helper()
		var version int64
		if err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			version, err = RecordContentVersion(t.Context(), tx, "kb-1", kind, "user-1")
			return err
		}); err != nil {
			t.Fatalf("record %s version: %v", kind, err)
		}
		return version
	}
	upload := record(entity.DatasetVersionKindUpload)
	chunk := record(entity.DatasetVersionKindChunk)
	parsed := record(entity.DatasetVersionKindParse)
	deleted := record(entity.DatasetVersionKindDelete)

	rows := listVersionRows(t, db, "kb-1")
	byVersion := make(map[int64]*entity.DatasetVersion, len(rows))
	for _, row := range rows {
		byVersion[row.Version] = row
	}

	// Every version names a set, and every one of them names the same set: no
	// write below moved a document, so the content is identical.
	hashes := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.DocumentsHash == nil {
			t.Fatalf("v%d names no document set", row.Version)
		}
		hashes[*row.DocumentsHash] = struct{}{}
	}
	if len(hashes) != 1 {
		t.Fatalf("distinct document hashes = %d, want 1", len(hashes))
	}
	// The baseline and four content writes therefore share one stored set instead
	// of the copies the old anchor chain would have needed.
	if stored := countDocumentSets(t, db, "kb-1"); stored != 1 {
		t.Fatalf("stored document sets = %d, want 1", stored)
	}
	// A chunk edit and a parse cannot move the set, so they name what the upload
	// wrote; the deletion re-reads the documents, finds them unchanged, and lands
	// on the same content.
	for _, version := range []int64{chunk, parsed, deleted} {
		row := byVersion[version]
		if row.DocumentsHash == nil || *row.DocumentsHash != *byVersion[upload].DocumentsHash {
			t.Fatalf("v%d names a different document set than the upload v%d", version, upload)
		}
	}

	// Reading one of them resolves the set through its hash, which is what a
	// restore and the versions panel rely on.
	resolved, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(t.Context(), db, "kb-1", parsed)
	if err != nil {
		t.Fatalf("load the inherited version: %v", err)
	}
	if len(resolved.Documents) == 0 {
		t.Fatal("the inherited version resolved to no document set")
	}
	entries, err := ParseDocuments(resolved.Documents)
	if err != nil || len(entries) != 1 || entries[0] != "doc-1" {
		t.Fatalf("resolved entries = %#v, %v, want doc-1", entries, err)
	}
}

// TestDocumentSetRecordedWhenTheHeadCarriesNoAnchor covers a head that names no
// set at all: it cannot be inherited - carrying the NULL forward would turn "not
// recorded" into a version that claims to describe a state it never read. The
// next version reads the set itself instead.
func TestDocumentSetRecordedWhenTheHeadCarriesNoAnchor(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")
	name := "manual.pdf"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ChunkNum: 2, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}
	// A pre-column row: no documents_hash, and no document set either.
	if err := db.Create(&entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindUpload,
	}).Error; err != nil {
		t.Fatalf("seed the head that names no set: %v", err)
	}
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").
		Update("version", 1).Error; err != nil {
		t.Fatalf("pin the dataset version: %v", err)
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := RecordContentVersion(t.Context(), tx, "kb-1", entity.DatasetVersionKindChunk, "user-1")
		return err
	}); err != nil {
		t.Fatalf("record chunk version: %v", err)
	}

	rows := listVersionRows(t, db, "kb-1")
	latest := rows[len(rows)-1]
	if latest.Version != 2 {
		t.Fatalf("latest version = %d, want 2", latest.Version)
	}
	if latest.DocumentsHash == nil {
		t.Fatal("the version inherited a head that names no document set")
	}
	if stored := countDocumentSets(t, db, "kb-1"); stored != 1 {
		t.Fatalf("stored document sets = %d, want 1: the set is read again here", stored)
	}
}

// TestDanglingDocumentSetAnchorIsReported covers the hash that outlives its row:
// the read fails instead of answering with an empty set, because an empty set asks
// a restore to delete every document the dataset has.
func TestDanglingDocumentSetAnchorIsReported(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")
	missing := strings.Repeat("0", 64)
	if err := db.Create(&entity.DatasetVersion{
		DatasetID: "kb-1", Version: 2, Kind: entity.DatasetVersionKindChunk,
		DocumentsHash: &missing,
	}).Error; err != nil {
		t.Fatalf("seed the dangling hash: %v", err)
	}

	if _, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(t.Context(), db, "kb-1", 2); err == nil {
		t.Fatal("a dangling documents hash resolved without an error")
	}
}

// TestParseConnectorBindingsKeepsOnlyRestorableFields pins what a restore reads
// back: the connector id and the parse switch, ordered and deduped, with an entry
// that names no connector dropped.
func TestParseConnectorBindingsKeepsOnlyRestorableFields(t *testing.T) {
	snapshot := entity.JSONMap{"connectors": []interface{}{
		map[string]interface{}{"id": " conn-b ", "auto_parse": "0", "name": "Drive"},
		map[string]interface{}{"id": ""},
		map[string]interface{}{"id": "conn-a"},
		map[string]interface{}{"id": "conn-b", "auto_parse": "1"},
	}}
	bindings, recorded, err := ParseConnectorBindings(snapshot)
	if err != nil || !recorded {
		t.Fatalf("parse = (%v, recorded %v), want recorded", err, recorded)
	}
	if len(bindings) != 2 {
		t.Fatalf("bindings = %#v, want two", bindings)
	}
	if bindings[0]["id"] != "conn-a" || bindings[0]["auto_parse"] != "1" {
		t.Fatalf("first binding = %#v, want conn-a defaulting to auto_parse 1", bindings[0])
	}
	// A repeated connector is one binding, and the last value is the one the
	// writer stored last.
	if bindings[1]["id"] != "conn-b" || bindings[1]["auto_parse"] != "1" {
		t.Fatalf("second binding = %#v, want conn-b collapsed to its last value", bindings[1])
	}

	// A version written before bindings were recorded has no key at all: that is
	// "not recorded", not "the dataset had no data sources".
	for _, absent := range []entity.JSONMap{{"name": "Dataset"}, nil} {
		if _, recorded, err := ParseConnectorBindings(absent); err != nil || recorded {
			t.Fatalf("snapshot %#v = (recorded %v, %v), want not recorded", absent, recorded, err)
		}
	}
}

// TestRestoreConnectorBindingsReconcilesTheTable pins the write side of a
// configuration restore: the bindings the version recorded are the ones the
// dataset ends up with, a task queued for a binding the restore dropped is
// canceled, and another dataset's bindings are not touched.
func TestRestoreConnectorBindingsReconcilesTheTable(t *testing.T) {
	db := setupVersionTestDB(t)
	for _, link := range []entity.Connector2Kb{
		{ID: "link-a", ConnectorID: "conn-a", KbID: "kb-1", AutoParse: "1"},
		{ID: "link-b", ConnectorID: "conn-b", KbID: "kb-1", AutoParse: "1"},
		{ID: "link-other", ConnectorID: "conn-z", KbID: "kb-2", AutoParse: "1"},
	} {
		if err := db.Create(&link).Error; err != nil {
			t.Fatalf("seed binding: %v", err)
		}
	}
	if err := db.Create(&entity.SyncLogs{
		ID:          "task-1",
		ConnectorID: "conn-b",
		KbID:        "kb-1",
		TaskType:    "sync",
		Status:      string(entity.TaskStatusSchedule),
	}).Error; err != nil {
		t.Fatalf("seed sync task: %v", err)
	}

	bindings := []entity.JSONMap{
		{"id": "conn-a", "auto_parse": "0"},
		{"id": "conn-c", "auto_parse": "1"},
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return RestoreConnectorBindings(t.Context(), tx, "kb-1", bindings)
	}); err != nil {
		t.Fatalf("restore bindings: %v", err)
	}

	var rows []entity.Connector2Kb
	if err := db.Where("kb_id = ?", "kb-1").Find(&rows).Error; err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	restored := make(map[string]string, len(rows))
	for _, row := range rows {
		restored[row.ConnectorID] = row.AutoParse
	}
	if len(restored) != 2 || restored["conn-a"] != "0" || restored["conn-c"] != "1" {
		t.Fatalf("bindings = %#v, want conn-a/0 and conn-c/1", restored)
	}

	var task entity.SyncLogs
	if err := db.Where("id = ?", "task-1").Take(&task).Error; err != nil {
		t.Fatalf("reload sync task: %v", err)
	}
	if task.Status != string(entity.TaskStatusCancel) {
		t.Fatalf("sync task status = %q, want it canceled", task.Status)
	}

	var others int64
	if err := db.Model(&entity.Connector2Kb{}).Where("kb_id = ?", "kb-2").Count(&others).Error; err != nil {
		t.Fatalf("count other dataset's bindings: %v", err)
	}
	if others != 1 {
		t.Fatalf("other dataset bindings = %d, want it untouched", others)
	}
}

// openDBWithoutDocuments opens a database that is missing the document table, so
// a read fails the way a broken connection or a half-migrated database would.
func openDBWithoutDocuments(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open empty sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("access sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// TestDocumentSetReportsAReadFailure pins the contract the version writer relies
// on: a set that could not be read is an error, not an empty set. A nil answer
// would let the writer name a hash that was never stored, and every reader of that
// version would then fail on the dangling pointer.
func TestDocumentSetReportsAReadFailure(t *testing.T) {
	if _, err := DocumentSet(t.Context(), openDBWithoutDocuments(t), "kb-1"); err == nil {
		t.Fatal("reading the document set on a database without documents reported no error")
	}
}

// TestApplyDocumentsDoesNotClaimASetItCouldNotRead covers the write side of that
// contract: the error reaches the caller and the row keeps its hash unset
// ("not recorded") instead of naming a set it does not hold.
func TestApplyDocumentsDoesNotClaimASetItCouldNotRead(t *testing.T) {
	row := &entity.DatasetVersion{
		DatasetID: "kb-1",
		Version:   7,
		Kind:      entity.DatasetVersionKindBaseline,
	}
	if err := ApplyDocuments(t.Context(), openDBWithoutDocuments(t), row, nil, "kb-1"); err == nil {
		t.Fatal("ApplyDocuments reported no error for a failed read")
	}
	if row.DocumentsHash != nil {
		t.Fatalf("documents_hash = %q, want it unset", *row.DocumentsHash)
	}
	if len(row.Documents) != 0 {
		t.Fatalf("documents = %q, want it unset", row.Documents)
	}
}

// TestApplyDocumentsLeavesAReadlessRowUnrecorded covers the other way the read
// returns nothing: no dataset id to read. The row must not name a set.
func TestApplyDocumentsLeavesAReadlessRowUnrecorded(t *testing.T) {
	db := setupVersionTestDB(t)
	row := &entity.DatasetVersion{
		Version: 7,
		Kind:    entity.DatasetVersionKindBaseline,
	}
	if err := ApplyDocuments(t.Context(), db, row, nil, ""); err != nil {
		t.Fatalf("ApplyDocuments without a dataset: %v", err)
	}
	if row.DocumentsHash != nil {
		t.Fatalf("documents_hash = %q, want it unset", *row.DocumentsHash)
	}
	if len(row.Documents) != 0 {
		t.Fatalf("documents = %q, want it unset", row.Documents)
	}
}

// TestReservedVersionIsNotReused pins the contract ReserveVersion exists for: a
// number handed out without a row must not be handed out again. A chunk history
// row carries it as its anchor and that row is already committed by the time the
// next change is recorded, so reusing the number would put a later change on the
// same anchor as an earlier one and a restore could not tell them apart.
func TestReservedVersionIsNotReused(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")

	reserved, err := ReserveVersion(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("ReserveVersion: %v", err)
	}
	if reserved != 1 {
		t.Fatalf("reserved version = %d, want 1 on a fresh dataset", reserved)
	}
	if rows := listVersionRows(t, db, "kb-1"); len(rows) != 0 {
		t.Fatalf("history rows after a reservation = %d, want none: the number is reserved, not recorded", len(rows))
	}

	version, err := RecordVersionWithChanges(t.Context(), db, "kb-1",
		entity.DatasetVersionKindChunk, "tenant-1", 0, nil, nil)
	if err != nil {
		t.Fatalf("RecordVersionWithChanges: %v", err)
	}
	if version <= reserved {
		t.Fatalf("recorded version = %d, want it past the reserved %d", version, reserved)
	}
}

// TestPinnedVersionLandsOnTheReservedNumber covers the number a restore occupies.
// The restore reserves one before it replays any document, stamps the chunk
// history rows the replay writes with it, and records its own row on that same
// number: the rows are what that version describes, and one restore then advances
// the dataset by one version instead of leaving the anchor as a gap nobody
// explains and adding a second number for the row that closes it.
func TestPinnedVersionLandsOnTheReservedNumber(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")

	// The head the restore starts from: version 1 is already recorded, so the
	// reserved number has to be the one after it.
	first, err := RecordVersionWithChanges(t.Context(), db, "kb-1",
		entity.DatasetVersionKindChunk, "user-1", 0, nil, nil)
	if err != nil {
		t.Fatalf("record the version before the restore: %v", err)
	}
	reserved, err := ReserveVersion(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("ReserveVersion: %v", err)
	}
	if reserved != first+1 {
		t.Fatalf("reserved version = %d, want the number after %d", reserved, first)
	}

	version, err := RecordVersionWithChangesAtVersion(t.Context(), db, "kb-1",
		entity.DatasetVersionKindRestore, "user-1", 0, reserved, nil)
	if err != nil {
		t.Fatalf("RecordVersionWithChangesAtVersion: %v", err)
	}
	if version != reserved {
		t.Fatalf("recorded version = %d, want the pinned %d", version, reserved)
	}
	rows := listVersionRows(t, db, "kb-1")
	last := rows[len(rows)-1]
	if last.Version != reserved || last.Kind != entity.DatasetVersionKindRestore {
		t.Fatalf("last row = version %d kind %q, want the restore on %d", last.Version, last.Kind, reserved)
	}

	// The history stays contiguous: the next change takes the number after the
	// restore, so nothing is skipped between the reserved anchor and its row.
	next, err := RecordVersionWithChanges(t.Context(), db, "kb-1",
		entity.DatasetVersionKindChunk, "user-1", 0, nil, nil)
	if err != nil {
		t.Fatalf("record the version after the restore: %v", err)
	}
	if next != reserved+1 {
		t.Fatalf("next version = %d, want %d", next, reserved+1)
	}
}

// TestPinnedVersionAlreadyTakenIsRefused covers the one conflict a pin still has:
// the number it names already carries a row. Two versions cannot share a number,
// and the row that is there is not this run's to overwrite.
func TestPinnedVersionAlreadyTakenIsRefused(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")

	taken, err := RecordVersionWithChanges(t.Context(), db, "kb-1",
		entity.DatasetVersionKindChunk, "user-1", 0, nil, nil)
	if err != nil {
		t.Fatalf("record the version that takes the number: %v", err)
	}

	before := len(listVersionRows(t, db, "kb-1"))
	_, err = RecordVersionWithChangesAtVersion(t.Context(), db, "kb-1",
		entity.DatasetVersionKindRestore, "user-1", 0, taken, nil)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("pinned write on a taken number = %v, want ErrVersionConflict", err)
	}
	if after := len(listVersionRows(t, db, "kb-1")); after != before {
		t.Fatalf("history rows = %d, want the refusal to write nothing (was %d)", after, before)
	}
}

// TestPinnedVersionLandsAfterALaterOne covers the order runs actually finish in:
// both reserve their numbers before their content is done, and the one that
// reserved the later number closes first. The earlier run still closes on its own
// number - that is the anchor its chunk history rows carry - instead of failing a
// parse whose chunks are already indexed, and the dataset column stays at the
// newer number so no later allocation hands that number out twice.
func TestPinnedVersionLandsAfterALaterOne(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")

	if _, err := RecordVersionWithChanges(t.Context(), db, "kb-1",
		entity.DatasetVersionKindChunk, "user-1", 0, nil, nil); err != nil {
		t.Fatalf("record the first version: %v", err)
	}
	before, err := dao.NewDatasetVersionDAO().GetLatestForUpdate(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("load the head: %v", err)
	}

	first, err := ReserveVersion(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("reserve the earlier number: %v", err)
	}
	second, err := ReserveVersion(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("reserve the later number: %v", err)
	}
	if second <= first {
		t.Fatalf("reserved numbers = %d then %d, want increasing", first, second)
	}

	// The dataset's settings move while both runs are in flight, and the run that
	// closes on the newer number records them: the configuration the earlier run
	// inherits must not be this one.
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").
		Update("parser_config", entity.JSONMap{"chunk_token_num": float64(256)}).Error; err != nil {
		t.Fatalf("change the parser configuration: %v", err)
	}
	if _, err := RecordVersionWithChangesAtVersion(t.Context(), db, "kb-1",
		entity.DatasetVersionKindDatasetConfig, "user-1", 0, second, nil); err != nil {
		t.Fatalf("close the later run: %v", err)
	}

	got, err := RecordVersionWithChangesAtVersion(t.Context(), db, "kb-1",
		entity.DatasetVersionKindParse, "user-1", 0, first, nil)
	if err != nil {
		t.Fatalf("close the earlier run after the later one: %v", err)
	}
	if got != first {
		t.Fatalf("earlier run recorded on %d, want its reserved %d", got, first)
	}

	rows := listVersionRows(t, db, "kb-1")
	versions := make([]int64, 0, len(rows))
	byVersion := make(map[int64]*entity.DatasetVersion, len(rows))
	for _, row := range rows {
		versions = append(versions, row.Version)
		byVersion[row.Version] = row
	}
	early, ok := byVersion[first]
	if !ok {
		t.Fatalf("history versions = %v, want %d", versions, first)
	}
	late, ok := byVersion[second]
	if !ok {
		t.Fatalf("history versions = %v, want %d", versions, second)
	}

	// The run closing on the earlier number inherits the state of the version
	// before it: a restore to this number must not bring back settings made after
	// it.
	if early.ConfigHash == nil || before.ConfigHash == nil {
		t.Fatalf("config hashes = %v / %v, want both recorded", early.ConfigHash, before.ConfigHash)
	}
	if *early.ConfigHash != *before.ConfigHash {
		t.Fatalf("earlier run inherited config %q, want the version before it %q",
			*early.ConfigHash, *before.ConfigHash)
	}
	if late.ConfigHash != nil && *late.ConfigHash == *early.ConfigHash {
		t.Fatalf("later run recorded the same config %q, want the changed one", *late.ConfigHash)
	}

	// The column is the high-water mark: a run closing on an older number leaves
	// it where the newer one put it.
	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload the dataset: %v", err)
	}
	if kb.Version != second {
		t.Fatalf("dataset version = %d, want the newer %d", kb.Version, second)
	}

	next, err := RecordVersionWithChanges(t.Context(), db, "kb-1",
		entity.DatasetVersionKindChunk, "user-1", 0, nil, nil)
	if err != nil {
		t.Fatalf("record the version after both runs: %v", err)
	}
	if next != second+1 {
		t.Fatalf("next version = %d, want %d", next, second+1)
	}
}
