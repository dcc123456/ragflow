//go:build integration && ee

// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package dao

import (
	"fmt"
	"os"
	"testing"
	"time"

	"ragflow/internal/entity"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// The test owns a new database, never the database named in the supplied DSN.
func TestEvaluationMigrationMySQL(t *testing.T) {
	dsn := os.Getenv("RAGFLOW_EVALUATION_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("RAGFLOW_EVALUATION_TEST_MYSQL_DSN is required")
	}
	config, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test MySQL DSN")
	}
	config.DBName = ""
	admin, err := gorm.Open(mysql.Open(config.FormatDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal("connect to test MySQL")
	}
	sqlAdmin, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlAdmin.Close() })
	name := fmt.Sprintf("evaluation_migration_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE DATABASE `" + name + "`").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE `" + name + "`").Error; err != nil {
			t.Errorf("drop disposable database: %v", err)
		}
	})
	config.DBName = name
	config.ParseTime = true
	db, err := gorm.Open(mysql.Open(config.FormatDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal("connect to disposable MySQL database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	oldEvaluationTables(t, db)
	evaluationExec(t, db,
		`INSERT INTO evaluation_datasets VALUES ('col','tenant','','Historical','description','["kb"]','owner',123,456,1)`,
		`INSERT INTO evaluation_cases VALUES ('case','col','question','answer','["doc"]','["chunk"]','{"tag":"old"}',124)`,
		`INSERT INTO evaluation_runs VALUES ('run','col','chat','History','{"llm_id":"model"}','{"avg_blue_score":0.5}','COMPLETED','owner',125,126)`,
		`INSERT INTO evaluation_results VALUES ('result','run','case','generated','{"chunks":[{"id":"chunk"}],"doc_aggs":[]}','{"blue_score":0.5}',1.5,'{"total_tokens":7}',127)`)
	for range 2 {
		if err := migrateEvaluations(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	var c entity.EvaluationCase
	if err := db.Take(&c, "id = ?", "case").Error; err != nil {
		t.Fatal(err)
	}
	if c.Variable["question"] != "question" || c.Variable["reference_answer"] != "answer" || *c.CreateTime != 124 {
		t.Fatalf("historical case changed: %#v", c)
	}
	for _, model := range []any{
		&entity.EvaluationCollection{ID: "new-col", TenantID: "tenant", Name: "New", CreatedBy: "owner"},
		&entity.EvaluationCase{ID: "new-case", CollectionID: "new-col", Variable: entity.JSONMap{"question": "new"}},
		&entity.EvaluationRun{ID: "new-run", CollectionID: "new-col", TargetID: "chat", Name: "New", ConfigSnapshot: entity.JSONMap{}, CreatedBy: "owner"},
		&entity.EvaluationResult{ID: "new-result", RunID: "new-run", CaseID: "new-case", RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{}},
	} {
		if err := db.Create(model).Error; err != nil {
			t.Fatalf("new evaluation write: %v", err)
		}
	}
	var result entity.EvaluationResult
	if err := db.Take(&result, "id = ?", "result").Error; err != nil {
		t.Fatal(err)
	}
	if result.Metrics["blue_score"] != 0.5 || result.TokenUsage["total_tokens"] != float64(7) || *result.CreateTime != 127 {
		t.Fatalf("historical result changed: %#v", result)
	}
	// The new unique index must reject a second result for the same run/case.
	duplicate := &entity.EvaluationResult{ID: "duplicate", RunID: "new-run", CaseID: "new-case", RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{}}
	if err := db.Create(duplicate).Error; err == nil {
		t.Fatal("duplicate result accepted")
	}
}
