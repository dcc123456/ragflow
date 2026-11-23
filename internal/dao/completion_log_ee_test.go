//go:build ee

package dao

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/entity"
)

func TestMigrateCompletionLogs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := autoMigrateRuntimeModels(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	row := entity.CompletionLog{ID: "turn", UserMessage: strings.Repeat("你好", 10000), AssistantResponse: "response", Status: "completed"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := autoMigrateRuntimeModels(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	var saved entity.CompletionLog
	if err := db.Take(&saved, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.UserMessage != row.UserMessage || saved.AssistantResponse != row.AssistantResponse {
		t.Fatal("migration changed log content")
	}
}
