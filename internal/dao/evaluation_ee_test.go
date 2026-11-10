//go:build ee

// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package dao

import (
	"errors"
	"gorm.io/gorm"
	"ragflow/internal/entity"
	"testing"
)

func TestEvaluationDAOTenantIsolation(t *testing.T) {
	db := evaluationTestDB(t)
	if err := migrateEvaluations(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{
		&entity.EvaluationCollection{ID: "collection", TenantID: "owner-tenant", Name: "Private", CreatedBy: "user"},
		&entity.EvaluationCase{ID: "case", CollectionID: "collection", Variable: entity.JSONMap{}},
		&entity.EvaluationRun{ID: "run", CollectionID: "collection", Name: "Run", ConfigSnapshot: entity.JSONMap{}, CreatedBy: "user"},
		&entity.EvaluationResult{ID: "result", RunID: "run", CaseID: "case", RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{}},
	} {
		if err := db.Create(model).Error; err != nil {
			t.Fatal(err)
		}
	}
	d := NewEvaluationDAO()
	for _, tenant := range []string{"owner-tenant", "other-tenant", ""} {
		for _, get := range []func() error{
			func() error { _, err := d.GetCollection(t.Context(), db, tenant, "collection"); return err },
			func() error { _, err := d.GetCase(t.Context(), db, tenant, "case"); return err },
			func() error { _, err := d.GetRun(t.Context(), db, tenant, "run"); return err },
			func() error { _, err := d.GetResult(t.Context(), db, tenant, "result"); return err },
		} {
			err := get()
			if tenant == "owner-tenant" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("tenant %q received %v", tenant, err)
			}
		}
	}
	// A corrupt result must not disclose a case belonging to another collection.
	if err := db.Create(&entity.EvaluationCollection{ID: "foreign", TenantID: "other-tenant", Name: "Other", CreatedBy: "user"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.EvaluationCase{ID: "foreign-case", CollectionID: "foreign", Variable: entity.JSONMap{}}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.EvaluationResult{}).Where("id = ?", "result").Update("case_id", "foreign-case").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetResult(t.Context(), db, "owner-tenant", "result"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-collection result exposed: %v", err)
	}
}
