//go:build ee

package service

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"ragflow/internal/entity"
	"testing"
)

func TestDeleteEvaluationsPreservesCrossTenantResults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&entity.EvaluationCollection{}, &entity.EvaluationCase{}, &entity.EvaluationRun{}, &entity.EvaluationResult{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"owner", "other"} {
		for _, model := range []any{
			&entity.EvaluationCollection{ID: id, TenantID: id, CreatedBy: id},
			&entity.EvaluationCase{ID: id, CollectionID: id, Variable: entity.JSONMap{}},
			&entity.EvaluationRun{ID: id, CollectionID: id, CreatedBy: id, ConfigSnapshot: entity.JSONMap{}},
			&entity.EvaluationResult{ID: id, RunID: id, CaseID: id, RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{}},
		} {
			if err = db.Create(model).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = db.Model(&entity.EvaluationResult{}).Where("id = ?", "other").Update("case_id", "owner").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Transaction(func(tx *gorm.DB) error {
		return deleteEvaluationsEE(t.Context(), tx, "owner", "owner", &DeleteUserResult{})
	}); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&entity.EvaluationCollection{}, &entity.EvaluationCase{}, &entity.EvaluationRun{}, &entity.EvaluationResult{}} {
		var ids []string
		if err = db.Model(model).Pluck("id", &ids).Error; err != nil {
			t.Fatal(err)
		}
		if len(ids) != 1 || ids[0] != "other" {
			t.Fatalf("unexpected survivors: %v", ids)
		}
	}
}
