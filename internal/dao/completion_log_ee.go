//go:build ee

package dao

import "ragflow/internal/entity"

func completionLogModelsEE() []any {
	return []any{&entity.CompletionLog{}}
}
