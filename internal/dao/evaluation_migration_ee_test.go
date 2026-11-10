//go:build ee

// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package dao

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"ragflow/internal/entity"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func evaluationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

func evaluationExec(t *testing.T, db *gorm.DB, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
}

func oldEvaluationTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	evaluationExec(t, db,
		`CREATE TABLE evaluation_datasets (id varchar(32) PRIMARY KEY, tenant_id varchar(32) NOT NULL, user_id varchar(32) NOT NULL DEFAULT '', name varchar(255) NOT NULL, description text, kb_ids text NOT NULL, created_by varchar(32) NOT NULL, create_time bigint NOT NULL, update_time bigint NOT NULL, status bigint DEFAULT 1)`,
		`CREATE TABLE evaluation_cases (id varchar(32) PRIMARY KEY, dataset_id varchar(32) NOT NULL, question text NOT NULL, reference_answer text, relevant_doc_ids text, relevant_chunk_ids text, metadata text, create_time bigint NOT NULL)`,
		`CREATE TABLE evaluation_runs (id varchar(32) PRIMARY KEY, dataset_id varchar(32) NOT NULL, dialog_id varchar(32) NOT NULL, name varchar(255) NOT NULL, config_snapshot text NOT NULL, metrics_summary text, status varchar(32) NOT NULL DEFAULT 'PENDING', created_by varchar(32) NOT NULL, create_time bigint NOT NULL, complete_time bigint)`,
		`CREATE TABLE evaluation_results (id varchar(32) PRIMARY KEY, run_id varchar(32) NOT NULL, case_id varchar(32) NOT NULL, generated_answer text NOT NULL, retrieved_chunks text NOT NULL, metrics text NOT NULL, execution_time real NOT NULL, token_usage text, create_time bigint NOT NULL)`)
}

func TestEvaluationMigrationPreservesOldDataAndAllowsNewWrites(t *testing.T) {
	db := evaluationTestDB(t)
	oldEvaluationTables(t, db)
	evaluationExec(t, db,
		`INSERT INTO evaluation_datasets VALUES ('col','tenant','','Historical','description','["kb"]','owner',123,456,1)`,
		`INSERT INTO evaluation_cases VALUES ('case','col','question','answer','["doc"]','["chunk"]','{"tag":"old"}',124)`,
		`INSERT INTO evaluation_runs VALUES ('run','col','chat','History','{"llm_id":"model","metrics":["blue_score"]}','{"avg_blue_score":0.5}','COMPLETED','owner',125,126)`,
		`INSERT INTO evaluation_results VALUES ('result','run','case','generated','{"chunks":[{"id":"chunk"}],"doc_aggs":[{"doc_id":"doc"}]}','{"blue_score":0.5}',1.5,'{"total_tokens":7}',127)`)
	for range 2 {
		if err := migrateEvaluations(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	var collection entity.EvaluationCollection
	if err := db.Take(&collection, "id = ?", "col").Error; err != nil {
		t.Fatal(err)
	}
	if collection.TenantID != "tenant" || collection.UserID != "owner" || collection.TargetType != "chat" || *collection.CreateTime != 123 || *collection.UpdateTime != 456 {
		t.Fatalf("collection changed: %#v", collection)
	}
	var c entity.EvaluationCase
	if err := db.Take(&c, "id = ?", "case").Error; err != nil {
		t.Fatal(err)
	}
	if c.CollectionID != "col" || c.Variable["question"] != "question" || c.Variable["reference_answer"] != "answer" || len(c.RelevantDocIDs) != 1 || c.Metadata["tag"] != "old" {
		t.Fatalf("case changed: %#v", c)
	}
	var run entity.EvaluationRun
	if err := db.Take(&run, "id = ?", "run").Error; err != nil {
		t.Fatal(err)
	}
	if run.TargetID != "chat" || run.CollectionID != "col" || run.MetricsSummary["avg_blue_score"] != 0.5 || *run.CompleteTime != 126 {
		t.Fatalf("run changed: %#v", run)
	}
	var result entity.EvaluationResult
	if err := db.Take(&result, "id = ?", "result").Error; err != nil {
		t.Fatal(err)
	}
	if result.Metrics["blue_score"] != 0.5 || len(result.RetrievedChunks["doc_aggs"].([]any)) != 1 || result.TokenUsage["total_tokens"] != float64(7) || *result.CreateTime != 127 {
		t.Fatalf("result changed: %#v", result)
	}
	if err := db.Create(&entity.EvaluationCollection{ID: "new-col", TenantID: "tenant", Name: "New", CreatedBy: "owner"}).Error; err != nil {
		t.Fatalf("new collection write: %v", err)
	}
	if err := db.Create(&entity.EvaluationCase{ID: "new-case", CollectionID: "new-col", Variable: entity.JSONMap{"question": "new"}}).Error; err != nil {
		t.Fatalf("new case write: %v", err)
	}
	if err := db.Create(&entity.EvaluationRun{ID: "new-run", CollectionID: "new-col", TargetID: "chat", Name: "New", ConfigSnapshot: entity.JSONMap{}, CreatedBy: "owner"}).Error; err != nil {
		t.Fatalf("new run write: %v", err)
	}
	var preserved string
	if err := db.Table("evaluation_collections").Select("kb_ids").Where("id = ?", "col").Scan(&preserved).Error; err != nil || preserved != `["kb"]` {
		t.Fatalf("original KB references lost: %q %v", preserved, err)
	}
}

func TestEvaluationMigrationSourceSchemaAndArrayReferences(t *testing.T) {
	db := evaluationTestDB(t)
	evaluationExec(t, db,
		`CREATE TABLE evaluation_collections (id varchar(32) PRIMARY KEY, tenant_id varchar(32) NOT NULL, target_type varchar(16) NOT NULL DEFAULT 'chat', name text NOT NULL, description text, created_by varchar(32) NOT NULL, status integer DEFAULT 1, create_time bigint, update_time bigint, create_date datetime, update_date datetime)`,
		`CREATE TABLE evaluation_cases (id varchar(32) PRIMARY KEY, collection_id varchar(32), variable text NOT NULL, relevant_doc_ids text, relevant_kb_ids text, metadata text)`,
		`CREATE TABLE evaluation_results (id varchar(32) PRIMARY KEY, run_id varchar(32), case_id varchar(32), generated_answer text NOT NULL, retrieved_chunks text NOT NULL, metrics text NOT NULL, execution_time real, token_usage text)`,
		`INSERT INTO evaluation_collections VALUES ('col','tenant','chat','Source',NULL,'owner',1,123,456,'2026-10-30 10:00:00','2026-10-30 11:00:00')`,
		`INSERT INTO evaluation_cases VALUES ('case','col','{"question":"你好","reference_answer":"世界","custom":"value"}','["doc"]','["kb"]','{}')`,
		`INSERT INTO evaluation_results VALUES ('result','run','case','','[]','{}',0,NULL)`)
	for range 2 {
		if err := migrateEvaluations(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	var c entity.EvaluationCase
	if err := db.Take(&c, "id = ?", "case").Error; err != nil {
		t.Fatal(err)
	}
	if c.Variable["custom"] != "value" || c.Variable["question"] != "你好" || c.RelevantKbIDs[0] != "kb" {
		t.Fatalf("source case changed: %#v", c)
	}
	var result entity.EvaluationResult
	if err := db.Take(&result, "id = ?", "result").Error; err != nil {
		t.Fatal(err)
	}
	if len(result.RetrievedChunks["chunks"].([]any)) != 0 || len(result.RetrievedChunks["doc_aggs"].([]any)) != 0 {
		t.Fatalf("placeholder normalization: %#v", result.RetrievedChunks)
	}
}

func TestEvaluationMigrationConflictingCollectionsAndDuplicatesFail(t *testing.T) {
	t.Run("collection conflict", func(t *testing.T) {
		db := evaluationTestDB(t)
		oldEvaluationTables(t, db)
		if err := db.Migrator().CreateTable(&entity.EvaluationCollection{}); err != nil {
			t.Fatal(err)
		}
		evaluationExec(t, db, `INSERT INTO evaluation_datasets VALUES ('same','tenant','','Old',NULL,'[]','owner',1,2,1)`)
		if err := db.Create(&entity.EvaluationCollection{ID: "same", TenantID: "tenant", Name: "Different", CreatedBy: "owner"}).Error; err != nil {
			t.Fatal(err)
		}
		err := migrateEvaluations(t.Context(), db)
		if err == nil || !strings.Contains(err.Error(), "conflicting evaluation collection same") {
			t.Fatalf("expected precise conflict, got %v", err)
		}
		var rows int64
		db.Table("evaluation_datasets").Count(&rows)
		if rows != 1 {
			t.Fatal("old table lost data")
		}
		var c entity.EvaluationCollection
		db.Take(&c, "id = ?", "same")
		if c.Name != "Different" {
			t.Fatal("new table overwritten")
		}
	})
	t.Run("duplicate results", func(t *testing.T) {
		db := evaluationTestDB(t)
		oldEvaluationTables(t, db)
		evaluationExec(t, db, `INSERT INTO evaluation_results VALUES ('a','run','case','a','{}','{}',1,NULL,1)`, `INSERT INTO evaluation_results VALUES ('b','run','case','b','{}','{}',2,NULL,2)`)
		err := migrateEvaluations(t.Context(), db)
		if err == nil || !strings.Contains(err.Error(), "duplicate run/case pairs") {
			t.Fatalf("expected conflict, got %v", err)
		}
		var rows int64
		db.Table("evaluation_results").Count(&rows)
		if rows != 2 {
			t.Fatal("duplicate history discarded")
		}
	})
}

func TestEvaluationMigrationMergesDisjointCollectionsWithoutLosingExtraFields(t *testing.T) {
	db := evaluationTestDB(t)
	oldEvaluationTables(t, db)
	if err := db.Migrator().CreateTable(&entity.EvaluationCollection{}); err != nil {
		t.Fatal(err)
	}
	evaluationExec(t, db, `INSERT INTO evaluation_datasets VALUES ('old','tenant','','Old',NULL,'["historical-kb"]','owner',1,2,1)`)
	if err := db.Create(&entity.EvaluationCollection{ID: "source", TenantID: "tenant", Name: "Source", CreatedBy: "owner"}).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrateEvaluations(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	db.Table("evaluation_collections").Count(&count)
	if count != 2 {
		t.Fatalf("merged count %d", count)
	}
	var archive []map[string]any
	if err := db.Table("evaluation_datasets_archive").Find(&archive).Error; err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(archive)
	if !strings.Contains(string(raw), "historical-kb") {
		t.Fatal("obsolete associations discarded")
	}
}

func TestEvaluationMigrationDoesNotOverwriteSubsequentCaseEdits(t *testing.T) {
	db := evaluationTestDB(t)
	oldEvaluationTables(t, db)
	evaluationExec(t, db, `INSERT INTO evaluation_cases VALUES ('case','col','original','answer','{}',NULL,'{}',124)`)
	if err := migrateEvaluations(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("evaluation_cases").Where("id = ?", "case").Update("variable", `{"question":"edited","reference_answer":"changed"}`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateEvaluations(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	var c entity.EvaluationCase
	if err := db.Take(&c, "id = ?", "case").Error; err != nil {
		t.Fatal(err)
	}
	if c.Variable["question"] != "edited" || c.Variable["reference_answer"] != "changed" || len(c.RelevantDocIDs) != 0 {
		t.Fatalf("subsequent case edit overwritten: %#v", c)
	}
}

func TestEvaluationMigrationPreflightsBeforeDDL(t *testing.T) {
	for _, fixture := range []string{
		`ALTER TABLE evaluation_cases ADD COLUMN variable text`,
		`UPDATE evaluation_results SET retrieved_chunks = 'invalid-json'`,
	} {
		db := evaluationTestDB(t)
		oldEvaluationTables(t, db)
		evaluationExec(t, db, `INSERT INTO evaluation_cases VALUES ('case','col','old','answer','[]',NULL,'{}',124)`,
			`INSERT INTO evaluation_results VALUES ('result','run','case','','{}','{}',0,NULL,1)`, fixture)
		if strings.HasPrefix(fixture, "ALTER") {
			evaluationExec(t, db, `UPDATE evaluation_cases SET variable = '{"question":"conflict"}'`)
		}
		if err := migrateEvaluations(t.Context(), db); err == nil {
			t.Fatal("invalid history accepted")
		}
		if !db.Migrator().HasTable("evaluation_datasets") || db.Migrator().HasTable("evaluation_collections") {
			t.Fatal("schema changed before detecting incompatible historical data")
		}
	}
}

func TestEvaluationMigrationVersionGate(t *testing.T) {
	db := evaluationTestDB(t)
	if err := db.AutoMigrate(&entity.SystemSettings{}); err != nil {
		t.Fatal(err)
	}
	if err := migrateEvaluations(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	version, err := getDatabaseMigrationVersion(t.Context(), db, evaluationMigrationMarker)
	if err != nil || version != evaluationMigrationTarget {
		t.Fatalf("version = %q, err = %v", version, err)
	}
	// Invalid legacy data would fail preflight if the completed migration reran.
	evaluationExec(t, db, `INSERT INTO evaluation_cases (id,collection_id,variable,relevant_doc_ids) VALUES ('new-case','collection','{}','invalid-json')`)
	for _, version := range []string{evaluationMigrationTarget, "v2.0.0"} {
		if err := setDatabaseMigrationVersionForMarker(t.Context(), db, evaluationMigrationMarker, version); err != nil {
			t.Fatal(err)
		}
		if err := migrateEvaluations(t.Context(), db); err != nil {
			t.Fatalf("completed version %s reran: %v", version, err)
		}
		var raw string
		if err := db.Table("evaluation_cases").Select("relevant_doc_ids").Where("id = ?", "new-case").Scan(&raw).Error; err != nil {
			t.Fatal(err)
		}
		if raw != "invalid-json" {
			t.Fatalf("skipped migration altered data: %q", raw)
		}
	}
}

func TestEvaluationMigrationFailureDoesNotAdvanceVersion(t *testing.T) {
	db := evaluationTestDB(t)
	if err := db.AutoMigrate(&entity.SystemSettings{}); err != nil {
		t.Fatal(err)
	}
	oldEvaluationTables(t, db)
	evaluationExec(t, db, `INSERT INTO evaluation_results VALUES ('one','run','case','answer','{}','{}',0,NULL,1)`, `INSERT INTO evaluation_results VALUES ('two','run','case','answer','{}','{}',0,NULL,1)`)
	if err := migrateEvaluations(t.Context(), db); err == nil {
		t.Fatal("duplicate history accepted")
	}
	version, err := getDatabaseMigrationVersion(t.Context(), db, evaluationMigrationMarker)
	if err != nil || version != "" {
		t.Fatalf("failed migration advanced version: %q, %v", version, err)
	}
	evaluationExec(t, db, `DELETE FROM evaluation_results WHERE id = 'two'`)
	if err := migrateEvaluations(t.Context(), db); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	version, err = getDatabaseMigrationVersion(t.Context(), db, evaluationMigrationMarker)
	if err != nil || version != evaluationMigrationTarget {
		t.Fatalf("retry version = %q, err = %v", version, err)
	}
}
