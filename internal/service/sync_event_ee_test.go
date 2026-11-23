//go:build ee

package service

import (
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

func TestListSyncEventsWithoutClickHouse(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	if err := db.AutoMigrate(&entity.Connector{}, &entity.Connector2Kb{}, &entity.SyncLogs{}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&entity.Connector{ID: "source", TenantID: "user-1", Name: "source", Source: "rss", InputType: "poll", Config: entity.ConnectorConfig{}},
		&entity.Knowledgebase{ID: "kb", TenantID: "user-1", Name: "kb", CreatedBy: "user-1", UserID: "user-1", EmbdID: "embd", Permission: string(entity.TenantPermissionMe), Status: new(string(entity.StatusValid))},
		&entity.Connector2Kb{ID: "binding", ConnectorID: "source", KbID: "kb", AutoParse: "1"},
		&entity.SyncLogs{ID: "task", ConnectorID: "source", KbID: "kb", Status: dao.SyncStatusDone},
		&entity.SyncLogs{ID: "running", ConnectorID: "source", KbID: "kb", Status: dao.SyncStatusRunning},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	owner := serviceTestContext(t, "user-1", "user-1")
	for _, tt := range []struct {
		id       string
		terminal bool
	}{
		{id: "task", terminal: true},
		{id: "running"},
	} {
		response, code, err := ListSyncEvents(owner, tt.id, 2, nil, nil)
		if err != nil || code != common.CodeSuccess || response.Terminal != tt.terminal ||
			response.Items == nil || len(response.Items) != 0 || response.OldestID != 0 ||
			response.NewestID != 0 || response.HasMoreBefore || response.HasMoreAfter {
			t.Fatalf("owner timeline for %s = %+v, code = %v, err = %v", tt.id, response, code, err)
		}
	}

	response, code, err := ListSyncEvents(serviceTestContext(t, "user-2", "user-2"), "task", 2, nil, nil)
	if err == nil || code != common.CodeNotFound || response != nil {
		t.Fatalf("cross-tenant timeline = %+v, code = %v, err = %v", response, code, err)
	}
}
