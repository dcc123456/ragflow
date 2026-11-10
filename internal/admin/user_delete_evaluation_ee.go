//go:build ee

package admin

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"ragflow/internal/entity"
)

func deleteEvaluationsEE(ctx context.Context, tx *gorm.DB, userID, owner string, result *DeleteUserResult) error {
	db := tx.WithContext(ctx)
	if !db.Migrator().HasTable(&entity.EvaluationCollection{}) {
		return nil
	}
	var collectionIDs, runIDs []string
	q := db.Model(&entity.EvaluationCollection{}).Where("created_by = ?", userID)
	if owner != "" {
		q = q.Or("tenant_id = ?", owner)
	}
	if err := q.Pluck("id", &collectionIDs).Error; err != nil {
		return err
	}
	if err := db.Model(&entity.EvaluationRun{}).Where("collection_id IN ?", collectionIDs).Pluck("id", &runIDs).Error; err != nil {
		return err
	}
	for _, item := range []struct {
		model     any
		condition string
		args      []any
		label     string
	}{
		{&entity.EvaluationResult{}, "run_id IN ?", []any{runIDs}, "evaluation results"},
		{&entity.EvaluationRun{}, "id IN ?", []any{runIDs}, "evaluation runs"},
		{&entity.EvaluationCase{}, "collection_id IN ?", []any{collectionIDs}, "evaluation cases"},
		{&entity.EvaluationCollection{}, "id IN ?", []any{collectionIDs}, "evaluation collections"},
	} {
		deleted := db.Unscoped().Where(item.condition, item.args...).Delete(item.model)
		if deleted.Error != nil {
			return fmt.Errorf("delete %s: %w", item.label, deleted.Error)
		}
		if deleted.RowsAffected > 0 {
			result.DeletedDetails = append(result.DeletedDetails, fmt.Sprintf("- Deleted %d %s records.", deleted.RowsAffected, item.label))
		}
	}
	return nil
}
