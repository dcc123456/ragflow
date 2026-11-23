//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package dao

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type adminIngestionQueryLogger struct {
	logger.Interface
	queries []string
}

func (l *adminIngestionQueryLogger) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	query, _ := sql()
	l.queries = append(l.queries, query)
}

func setupAdminIngestionTaskDB(t *testing.T) (*gorm.DB, *adminIngestionQueryLogger) {
	t.Helper()
	queryLogger := &adminIngestionQueryLogger{Interface: logger.Default.LogMode(logger.Silent)}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: queryLogger})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&entity.User{}, &entity.Document{}, &entity.Knowledgebase{},
		&entity.IngestionTask{}, &entity.PipelineOperationLog{}); err != nil {
		t.Fatal(err)
	}
	return db, queryLogger
}

func insertAdminIngestionRow(t *testing.T, db *gorm.DB, row interface{}) {
	t.Helper()
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
}

func adminTaskString(value string) *string { return &value }
func adminTaskTime(value int64) *int64     { return &value }

func defaultAdminTaskFilter() AdminIngestionTaskFilter {
	return AdminIngestionTaskFilter{Page: 1, PageSize: 20, Desc: true}
}

func TestAdminIngestionTasksFilterUsesCurrentMetadata(t *testing.T) {
	db, _ := setupAdminIngestionTaskDB(t)
	insertAdminIngestionRow(t, db, &entity.User{ID: "user-1", Nickname: "Alice", Email: "alice@example.test"})
	for _, kb := range []*entity.Knowledgebase{
		{ID: "dataset-1", TenantID: "user-1", Name: "Guides 100%_!", EmbdID: "embedding"},
		{ID: "dataset-2", TenantID: "other-user", Name: "Guides 100xy", EmbdID: "embedding"},
	} {
		insertAdminIngestionRow(t, db, kb)
	}
	insertAdminIngestionRow(t, db, &entity.PipelineOperationLog{
		ID: "bound-run", DocumentID: "doc-1", TenantID: "user-1", KbID: "dataset-1",
		PipelineID: adminTaskString("original-pipeline"), PipelineTitle: adminTaskString("Original pipeline"),
		ParserID: "naive", OperationStatus: common.RUNNING,
	})
	for _, doc := range []*entity.Document{
		{ID: "doc-1", KbID: "dataset-1", ParserID: "naive", PipelineID: adminTaskString("current-pipeline"),
			Name: adminTaskString("Current 100%_!.pdf"), ParserConfig: entity.JSONMap{}, SourceType: "local", Type: "pdf", CreatedBy: "user-1", Suffix: "pdf"},
		{ID: "doc-2", KbID: "dataset-2", ParserID: "naive", Name: adminTaskString("Current 100xy.pdf"),
			ParserConfig: entity.JSONMap{}, SourceType: "local", Type: "pdf", CreatedBy: "other-user", Suffix: "pdf"},
	} {
		insertAdminIngestionRow(t, db, doc)
	}
	insertAdminIngestionRow(t, db, &entity.IngestionTask{ID: "task-a", UserID: "user-1", DocumentID: "doc-1", DatasetID: "dataset-1", Status: common.RUNNING,
		PipelineLogID: adminTaskString("bound-run"), BaseModel: entity.BaseModel{CreateTime: adminTaskTime(1000), UpdateTime: adminTaskTime(1500)}})
	insertAdminIngestionRow(t, db, &entity.IngestionTask{ID: "task-b", UserID: "other-user", DocumentID: "doc-2", DatasetID: "dataset-2", Status: common.FAILED,
		BaseModel: entity.BaseModel{CreateTime: adminTaskTime(2000)}})

	tests := []struct {
		name   string
		filter func(*AdminIngestionTaskFilter)
		want   int
	}{
		{"email", func(f *AdminIngestionTaskFilter) { f.UserEmail = "alice@example.test" }, 1},
		{"status", func(f *AdminIngestionTaskFilter) { f.Status = common.RUNNING }, 1},
		{"dataset id", func(f *AdminIngestionTaskFilter) { f.DatasetID = "dataset-1" }, 1},
		{"dataset id must be exact", func(f *AdminIngestionTaskFilter) { f.DatasetID = "dataset" }, 0},
		{"dataset id and status mismatch", func(f *AdminIngestionTaskFilter) { f.DatasetID, f.Status = "dataset-1", common.FAILED }, 0},
		{"dataset id and name mismatch", func(f *AdminIngestionTaskFilter) { f.DatasetID, f.DatasetName = "dataset-1", "100xy" }, 0},
		{"dataset id bound parameter", func(f *AdminIngestionTaskFilter) { f.DatasetID = "dataset-1' OR 1=1 --" }, 0},
		{"dataset substring", func(f *AdminIngestionTaskFilter) { f.DatasetName = "IdEs 100%_!" }, 1},
		{"document substring", func(f *AdminIngestionTaskFilter) { f.DocumentName = "RENT 100%_!" }, 1},
		{"literal dataset percent", func(f *AdminIngestionTaskFilter) { f.DatasetName = "%" }, 1},
		{"literal dataset underscore", func(f *AdminIngestionTaskFilter) { f.DatasetName = "_" }, 1},
		{"literal dataset escape", func(f *AdminIngestionTaskFilter) { f.DatasetName = "!" }, 1},
		{"literal document percent", func(f *AdminIngestionTaskFilter) { f.DocumentName = "%" }, 1},
		{"literal document underscore", func(f *AdminIngestionTaskFilter) { f.DocumentName = "_" }, 1},
		{"literal document escape", func(f *AdminIngestionTaskFilter) { f.DocumentName = "!" }, 1},
		{"combined", func(f *AdminIngestionTaskFilter) {
			f.UserEmail, f.DatasetID, f.DatasetName, f.DocumentName, f.Status = "alice@example.test", "dataset-1", "Guides", "Current", common.RUNNING
		}, 1},
		{"combined mismatch", func(f *AdminIngestionTaskFilter) { f.UserEmail, f.Status = "alice@example.test", common.FAILED }, 0},
		{"document bound parameter", func(f *AdminIngestionTaskFilter) { f.DocumentName = "current' OR 1=1 --" }, 0},
		{"email bound parameter", func(f *AdminIngestionTaskFilter) { f.UserEmail = "alice@example.test' OR 1=1 --" }, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filter := defaultAdminTaskFilter()
			filter.PageSize = 1
			test.filter(&filter)
			tasks, total, err := NewIngestionTaskDAO().ListForAdmin(t.Context(), db, filter)
			if err != nil || total != int64(test.want) || len(tasks) != test.want {
				t.Fatalf("ListForAdmin() = (%+v, %d, %v), want %d matching rows", tasks, total, err, test.want)
			}
			if test.want == 0 {
				if tasks == nil {
					t.Fatal("empty page must be an empty slice")
				}
				return
			}
			row := tasks[0]
			if row.ID != "task-a" || row.PipelineID == nil || *row.PipelineID != "original-pipeline" || row.PipelineTitle == nil || *row.PipelineTitle != "Original pipeline" ||
				row.DocumentName == nil || *row.DocumentName != "Current 100%_!.pdf" || row.Nickname == nil || *row.Nickname != "Alice" ||
				row.Email == nil || *row.Email != "alice@example.test" || row.DatasetName == nil || *row.DatasetName != "Guides 100%_!" {
				t.Fatalf("unexpected metadata: %+v", row)
			}
		})
	}

	filter := defaultAdminTaskFilter()
	filter.UserEmail, filter.Status = "alice' OR 1=1 --", common.RUNNING
	filter.DatasetID = "dataset-1' OR 1=1 --"
	filter.DatasetName, filter.DocumentName = "Guides%_!", "current%_!"
	probe := adminIngestionTasksQuery(t.Context(), db.Session(&gorm.Session{DryRun: true}), filter).Find(&[]AdminIngestionTaskRow{})
	wantArgs := []interface{}{filter.UserEmail, filter.Status, filter.DatasetID, "%guides!%!_!!%", "%current!%!_!!%"}
	if !strings.Contains(probe.Statement.SQL.String(), "usr.email = ?") || strings.Contains(probe.Statement.SQL.String(), filter.UserEmail) ||
		!strings.Contains(probe.Statement.SQL.String(), "task.dataset_id = ?") || strings.Contains(probe.Statement.SQL.String(), filter.DatasetID) ||
		!strings.Contains(probe.Statement.SQL.String(), "LIKE ? ESCAPE '!'") || !reflect.DeepEqual(probe.Statement.Vars, wantArgs) {
		t.Fatalf("filters must bind IDs and escaped name patterns: sql=%s vars=%v", probe.Statement.SQL.String(), probe.Statement.Vars)
	}
	if err := db.Where("id = ?", "doc-1").Delete(&entity.Document{}).Error; err != nil {
		t.Fatal(err)
	}
	filter = defaultAdminTaskFilter()
	filter.DatasetID = "dataset-1"
	tasks, total, err := NewIngestionTaskDAO().ListForAdmin(t.Context(), db, filter)
	if err != nil || total != 1 || len(tasks) != 1 || tasks[0].ID != "task-a" || tasks[0].DocumentName != nil || tasks[0].PipelineTitle == nil || *tasks[0].PipelineTitle != "Original pipeline" ||
		tasks[0].PipelineID == nil || *tasks[0].PipelineID != "original-pipeline" {
		t.Fatalf("deleted document must retain the task and its run metadata: tasks=%+v total=%d err=%v", tasks, total, err)
	}
	filter.DocumentName = "Current"
	tasks, total, err = NewIngestionTaskDAO().ListForAdmin(t.Context(), db, filter)
	if err != nil || total != 0 || len(tasks) != 0 {
		t.Fatalf("deleted document must not match a name filter: tasks=%+v total=%d err=%v", tasks, total, err)
	}
}

func TestAdminIngestionTasksPageAndSortRetainMissingMetadata(t *testing.T) {
	db, _ := setupAdminIngestionTaskDB(t)
	for _, id := range []string{"task-c", "task-a", "task-b"} {
		created, updated := int64(2000), int64(1000)
		if id == "task-c" {
			created, updated = 1000, 3000
		}
		insertAdminIngestionRow(t, db, &entity.IngestionTask{ID: id, UserID: "missing-user", DocumentID: "doc-" + id, DatasetID: "missing-dataset",
			Status: common.CREATED, BaseModel: entity.BaseModel{CreateTime: &created, UpdateTime: &updated}})
	}
	filter := defaultAdminTaskFilter()
	filter.PageSize = 2
	for _, order := range []struct {
		column string
		desc   bool
		pages  [][]string
	}{
		{"", true, [][]string{{"task-c", "task-b"}, {"task-a"}, {}}},
		{"create_time", true, [][]string{{"task-b", "task-a"}, {"task-c"}, {}}},
		{"create_time", false, [][]string{{"task-c", "task-a"}, {"task-b"}, {}}},
		{"update_time", true, [][]string{{"task-c", "task-b"}, {"task-a"}, {}}},
		{"update_time", false, [][]string{{"task-a", "task-b"}, {"task-c"}, {}}},
	} {
		filter.OrderBy, filter.Desc = order.column, order.desc
		for page, wantIDs := range order.pages {
			filter.Page = page + 1
			tasks, total, err := NewIngestionTaskDAO().ListForAdmin(t.Context(), db, filter)
			if err != nil || total != 3 || len(tasks) != len(wantIDs) {
				t.Fatalf("order=%q desc=%t page %d: tasks=%+v total=%d err=%v", order.column, filter.Desc, filter.Page, tasks, total, err)
			}
			for i, task := range tasks {
				if task.ID != wantIDs[i] || task.Nickname != nil || task.Email != nil || task.DatasetName != nil || task.DocumentName != nil || task.PipelineTitle != nil {
					t.Fatalf("order=%q desc=%t page %d row %d = %+v, want %s with absent metadata", order.column, filter.Desc, filter.Page, i, task, wantIDs[i])
				}
			}
		}
	}
}

func TestAdminIngestionTasksOutOfRangePageOnlyCounts(t *testing.T) {
	db, queryLogger := setupAdminIngestionTaskDB(t)
	for _, id := range []string{"task-a", "task-b", "task-c"} {
		insertAdminIngestionRow(t, db, &entity.IngestionTask{
			ID: id, UserID: "user-1", DocumentID: "doc-" + id, DatasetID: "dataset-1",
			Status: common.RUNNING, PipelineLogID: adminTaskString("run-" + id),
		})
	}
	for _, test := range []struct {
		name           string
		page, pageSize int
		status         string
		wantTotal      int64
	}{
		{"after partial first page", 2, 20, "", 3},
		{"after partial last page", 3, 2, "", 3},
		{"offset equals total", 4, 1, "", 3},
		{"maximum page", 1_000_000, 1, "", 3},
		{"maximum page and page size", 1_000_000, 100, "", 3},
		{"no matching tasks", 1, 20, common.STOPPED, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			queryLogger.queries = nil
			filter := defaultAdminTaskFilter()
			filter.Page, filter.PageSize, filter.Status = test.page, test.pageSize, test.status
			tasks, total, err := NewIngestionTaskDAO().ListForAdmin(t.Context(), db, filter)
			if err != nil || total != test.wantTotal || tasks == nil || len(tasks) != 0 {
				t.Fatalf("ListForAdmin() = (%+v, %d, %v), want empty slice and total %d", tasks, total, err, test.wantTotal)
			}
			if len(queryLogger.queries) != 1 || !strings.Contains(strings.ToUpper(queryLogger.queries[0]), "COUNT(") {
				t.Fatalf("queries = %v, want only the task count without a page query", queryLogger.queries)
			}
		})
	}
}

func TestAdminIngestionTasksRejectInvalidPaginationAndOrder(t *testing.T) {
	db, queryLogger := setupAdminIngestionTaskDB(t)
	queryLogger.queries = nil
	for _, filter := range []AdminIngestionTaskFilter{
		{Page: 0, PageSize: 20}, {Page: 1, PageSize: 0},
		{Page: 1_000_001, PageSize: 20},
		{Page: 1, PageSize: 101},
		{Page: 1, PageSize: 20, OrderBy: "id"},
		{Page: 1, PageSize: 20, OrderBy: "process_begin_at"},
		{Page: 1, PageSize: 20, OrderBy: "create_time; DROP TABLE ingestion_task"},
	} {
		if _, _, err := NewIngestionTaskDAO().ListForAdmin(t.Context(), db, filter); err == nil {
			t.Fatalf("expected validation error for %+v", filter)
		}
	}
	if len(queryLogger.queries) != 0 {
		t.Fatalf("invalid input must not issue SQL: %v", queryLogger.queries)
	}
}
