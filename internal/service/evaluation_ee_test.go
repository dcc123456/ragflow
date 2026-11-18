//go:build ee

package service

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/glebarez/sqlite"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
	"os"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"reflect"
	"testing"
)

func TestEvaluationSpreadsheetHeadersAndSheets(t *testing.T) {
	cases, failed, err := ReadEvaluationCases("cases.csv", []byte(" Question ,REFERENCE_ANSWER,custom\n hello ,world,x\n,invalid,y\n,,\n"))
	if err != nil || len(cases) != 1 || failed != 1 || cases[0].Variable["question"] != "hello" || cases[0].Variable["custom"] != "x" {
		t.Fatalf("cases=%+v failed=%d err=%v", cases, failed, err)
	}
	if _, _, err = ReadEvaluationCases("bad.csv", []byte("question,other\nq,a")); err == nil {
		t.Fatal("missing required reference header accepted")
	}
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetRow("Sheet1", "A1", &[]any{"question", "reference_answer"})
	f.SetSheetRow("Sheet1", "A2", &[]any{"first", "one"})
	f.NewSheet("Second")
	f.SetSheetRow("Second", "A1", &[]any{"question", "reference_answer"})
	f.SetSheetRow("Second", "A2", &[]any{"second", "two"})
	b, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	cases, failed, err = ReadEvaluationCases("cases.xlsx", b.Bytes())
	if err != nil || len(cases) != 2 || failed != 0 {
		t.Fatalf("xlsx cases=%+v failed=%d err=%v", cases, failed, err)
	}
	exported, err := EvaluationWorkbook([]string{"question"}, [][]any{{"=1+1"}})
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(exported))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	formula, err := book.GetCellFormula("Sheet1", "A2")
	if err != nil || formula != "" {
		t.Fatalf("formula injection: %q %v", formula, err)
	}
}

func evaluationTestService(t *testing.T) (*EvaluationService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	err = db.AutoMigrate(&entity.EvaluationCollection{}, &entity.EvaluationCase{}, &entity.EvaluationRun{}, &entity.EvaluationResult{}, &entity.User{}, &entity.Tenant{}, &entity.UserTenant{}, &entity.Role{}, &entity.RoleResource{})
	if err != nil {
		t.Fatal(err)
	}
	old := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = old })
	valid := "1"
	if err = db.Create(&entity.User{ID: "owner", Email: "owner@test.invalid", Nickname: "owner"}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&entity.UserTenant{ID: "membership", UserID: "owner", TenantID: "tenant", Role: "owner", Status: &valid}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&entity.Tenant{ID: "tenant", ParserIDs: ""}).Error; err != nil {
		t.Fatal(err)
	}
	return NewEvaluationService(), db
}

func TestEvaluationCollectionExportVisibleFields(t *testing.T) {
	s, _ := evaluationTestService(t)
	ctx := permission.WithIdentity(t.Context(), "owner", "tenant")
	collection, err := s.CreateCollection(ctx, "Cases", "", "chat", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCase(ctx, collection.ID, EvaluationCaseInput{Question: new("what?"), ReferenceAnswer: new("answer")}); err != nil {
		t.Fatal(err)
	}
	data, name, err := s.ExportCollection(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Cases.xlsx" {
		t.Fatalf("filename = %q", name)
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	rows, err := book.GetRows("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"question", "reference_answer"}, {"what?", "answer"}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("export rows = %v, want %v", rows, want)
	}
}

func TestEvaluationTenantIsolationAndInvalidation(t *testing.T) {
	s, db := evaluationTestService(t)
	ctx := permission.WithIdentity(t.Context(), "owner", "tenant")
	collection, err := s.CreateCollection(ctx, "Cases", "", "chat", nil)
	if err != nil {
		t.Fatal(err)
	}
	other := permission.WithIdentity(t.Context(), "owner", "other")
	if _, err = s.Collection(other, collection.ID); err == nil {
		t.Fatal("cross tenant collection visible")
	}
	c, err := s.CreateCase(ctx, collection.ID, EvaluationCaseInput{Question: new("what?"), ReferenceAnswer: new("answer")})
	if err != nil {
		t.Fatal(err)
	}
	run := entity.EvaluationRun{ID: "run", CollectionID: collection.ID, TargetID: "chat", TargetType: "chat", Name: "test", Status: "COMPLETED", ConfigSnapshot: entity.JSONMap{}, MetricsSummary: entity.JSONMap{"faithfulness": 1.0}}
	if err = db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	result := entity.EvaluationResult{ID: "result", RunID: run.ID, CaseID: c.ID, GeneratedAnswer: "answer", RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{"faithfulness": 1.0}}
	if err = db.Create(&result).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateCase(ctx, collection.ID, c.ID, EvaluationCaseInput{Question: new("updated")}); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&entity.EvaluationResult{}).Count(&count)
	if count != 0 {
		t.Fatal("edited case retained stale results")
	}
	db.First(&run, "id = ?", run.ID)
	if len(run.MetricsSummary) != 0 {
		t.Fatalf("stale summary %+v", run.MetricsSummary)
	}
	db.Model(&run).Update("status", "RUNNING")
	if err = s.DeleteCollection(ctx, []string{collection.ID}); err == nil {
		t.Fatal("deleted active evaluation")
	}
}

func TestEvaluationUpdateRunCollection(t *testing.T) {
	s, db := evaluationTestService(t)
	ctx := permission.WithIdentity(t.Context(), "owner", "tenant")
	for _, id := range []string{"original", "replacement"} {
		if err := db.Create(&entity.EvaluationCollection{ID: id, TenantID: "tenant", TargetType: "chat", Name: id, CreatedBy: "owner"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	run := entity.EvaluationRun{
		ID: "run", CollectionID: "original", TargetType: "chat", TargetID: "chat", Name: "test",
		ConfigSnapshot: entity.JSONMap{}, MetricsSummary: entity.JSONMap{"faithfulness": 1.0},
		Status: "COMPLETED", CompleteTime: new(int64(100)), Progress: 100, Error: "old error",
		TaskID: "old-task", ExecutionConfig: entity.JSONMap{"case_ids": []string{"old-case"}},
		ExecutionGeneration: 1, LeaseOwner: "old-owner", LeaseExpiresAt: new(int64(100)),
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"run", "other-run"} {
		if err := db.Create(&entity.EvaluationResult{ID: id + "-result", RunID: id, CaseID: "old-case", RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{}}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, patch := range []entity.JSONMap{{"name": "renamed"}, {"collection_id": "original"}} {
		updated, err := s.UpdateRun(ctx, run.ID, patch)
		if err != nil {
			t.Fatal(err)
		}
		if updated.CollectionID != "original" || updated.Status != "COMPLETED" || len(updated.MetricsSummary) == 0 || updated.CompleteTime == nil {
			t.Fatalf("unchanged collection lost saved state: %+v", updated)
		}
		var count int64
		if err := db.Model(&entity.EvaluationResult{}).Where("run_id = ?", run.ID).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("unchanged collection lost results: count=%d err=%v", count, err)
		}
	}
	updated, err := s.UpdateRun(ctx, run.ID, entity.JSONMap{"collection_id": "replacement"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CollectionID != "replacement" || updated.Name != "renamed" || updated.TargetID != run.TargetID {
		t.Fatalf("collection change did not persist: %+v", updated)
	}
	if updated.Status != "PENDING" || len(updated.MetricsSummary) != 0 || updated.CompleteTime != nil || updated.Progress != 0 || updated.Error != "" || updated.TaskID != "" || len(updated.ExecutionConfig) != 0 || updated.LeaseOwner != "" || updated.LeaseExpiresAt != nil || updated.ExecutionGeneration != 1 {
		t.Fatalf("collection change retained old execution state: %+v", updated)
	}
	var results []entity.EvaluationResult
	if err = db.Find(&results).Error; err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].RunID != "other-run" {
		t.Fatalf("unexpected results after collection change: %+v", results)
	}
	reloaded, err := s.Run(ctx, run.ID)
	if err != nil || reloaded.CollectionID != "replacement" {
		t.Fatalf("saved collection=%+v err=%v", reloaded, err)
	}
}

func TestEvaluationUpdateRunCollectionValidation(t *testing.T) {
	s, db := evaluationTestService(t)
	ctx := permission.WithIdentity(t.Context(), "owner", "tenant")
	for _, collection := range []entity.EvaluationCollection{
		{ID: "original", TenantID: "tenant", TargetType: "chat"},
		{ID: "replacement", TenantID: "tenant", TargetType: "chat"},
		{ID: "foreign", TenantID: "other", TargetType: "chat"},
		{ID: "agent", TenantID: "tenant", TargetType: "agent"},
		{ID: "deleted", TenantID: "tenant", TargetType: "chat"},
	} {
		if err := db.Create(&collection).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&entity.EvaluationCollection{}).Where("id = ?", "deleted").Update("status", 0).Error; err != nil {
		t.Fatal(err)
	}
	run := entity.EvaluationRun{ID: "run", CollectionID: "original", TargetType: "chat", Name: "test", ConfigSnapshot: entity.JSONMap{}, Status: "COMPLETED"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		value any
		err   error
	}{
		{"empty", "", ErrEvaluationInvalid},
		{"blank", " ", ErrEvaluationInvalid},
		{"null", nil, ErrEvaluationInvalid},
		{"number", 1, ErrEvaluationInvalid},
		{"missing", "missing", gorm.ErrRecordNotFound},
		{"foreign", "foreign", gorm.ErrRecordNotFound},
		{"deleted", "deleted", gorm.ErrRecordNotFound},
		{"wrong target", "agent", ErrEvaluationInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := s.UpdateRun(ctx, run.ID, entity.JSONMap{"name": "must not persist", "collection_id": test.value}); !errors.Is(err, test.err) {
				t.Fatalf("update error=%v, want %v", err, test.err)
			}
			saved, err := s.Run(ctx, run.ID)
			if err != nil || saved.CollectionID != "original" || saved.Name != "test" {
				t.Fatalf("invalid change mutated run=%+v err=%v", saved, err)
			}
		})
	}
	for _, status := range []string{"RUNNING", "QUEUED", "PENDING"} {
		t.Run(status, func(t *testing.T) {
			if err := db.Model(&entity.EvaluationRun{}).Where("id = ?", run.ID).Updates(map[string]any{"status": status, "task_id": "active-task"}).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := s.UpdateRun(ctx, run.ID, entity.JSONMap{"collection_id": "replacement"}); !errors.Is(err, ErrEvaluationActive) {
				t.Fatalf("active collection change: %v", err)
			}
		})
	}
}

func TestEvaluationLegacyXLSImport(t *testing.T) {
	data, err := os.ReadFile("testdata/evaluation_cases.xls")
	if err != nil {
		t.Fatal(err)
	}
	cases, failed, err := ReadEvaluationCases("cases.xls", data)
	if err != nil || failed != 0 || len(cases) != 3 {
		t.Fatalf("XLS cases=%+v failed=%d err=%v", cases, failed, err)
	}
	if cases[0].Variable["question"] != "Apples" || cases[2].Variable["reference_answer"] != "BIFF8" {
		t.Fatalf("XLS values changed: %+v", cases)
	}
}

func TestEvaluationPartialCaseEditAndQueuedProtection(t *testing.T) {
	s, db := evaluationTestService(t)
	ctx := permission.WithIdentity(t.Context(), "owner", "tenant")
	c, err := s.CreateCollection(ctx, "Cases", "", "chat", nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := s.CreateCase(ctx, c.ID, EvaluationCaseInput{Question: new("q"), ReferenceAnswer: new("preserved")})
	if err != nil {
		t.Fatal(err)
	}
	edited, err := s.UpdateCase(ctx, c.ID, value.ID, EvaluationCaseInput{Metadata: entity.JSONMap{"key": "value"}})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Variable["reference_answer"] != "preserved" {
		t.Fatalf("PATCH erased reference answer: %+v", edited.Variable)
	}
	edited, err = s.UpdateCase(ctx, c.ID, value.ID, EvaluationCaseInput{ReferenceAnswer: new("")})
	if err != nil || edited.Variable["reference_answer"] != "" {
		t.Fatalf("explicit clear failed: %+v %v", edited, err)
	}
	if err = db.Create(&entity.EvaluationRun{ID: "queued", CollectionID: c.ID, TargetType: "chat", TargetID: "chat", Name: "queued", ConfigSnapshot: entity.JSONMap{}, Status: "PENDING", TaskID: "task"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateCase(ctx, c.ID, value.ID, EvaluationCaseInput{Question: new("race")}); !errors.Is(err, ErrEvaluationActive) {
		t.Fatalf("queued case mutation: %v", err)
	}
	if err = s.DeleteCollection(ctx, []string{c.ID}); !errors.Is(err, ErrEvaluationActive) {
		t.Fatalf("queued collection deletion: %v", err)
	}
}

func TestEvaluationReadOnlyRoleCannotMutateOrStart(t *testing.T) {
	s, db := evaluationTestService(t)
	tenant := "tenant"
	roleID := int32(7)
	valid := "1"
	for _, row := range []any{&entity.Role{ID: roleID, TenantID: &tenant, RoleName: "reader"}, &entity.RoleResource{RoleID: roleID, ResourceType: entity.RoleResourceTypeChat, Action: entity.RoleActionEnable | entity.RoleActionRead}, &entity.User{ID: "reader", Email: "reader@test.invalid", Nickname: "reader"}, &entity.UserTenant{ID: "reader-member", UserID: "reader", TenantID: tenant, Role: "7", Status: &valid}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	owner := permission.WithIdentity(t.Context(), "owner", tenant)
	collection, err := s.CreateCollection(owner, "Read only", "", "chat", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := permission.WithIdentity(t.Context(), "reader", tenant)
	if _, err = s.Collection(ctx, collection.ID); err != nil {
		t.Fatalf("read denied: %v", err)
	}
	if _, err = s.CreateCase(ctx, collection.ID, EvaluationCaseInput{Question: new("q")}); !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("read-only create case: %v", err)
	}
	if _, err = s.StartExecution(ctx, "missing", "generate", nil, nil); !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("read-only execute: %v", err)
	}
	if err = s.CancelExecution(ctx, "missing"); !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("read-only cancel: %v", err)
	}
}

func TestEvaluationDefaultRunVersionsUseTargetAndCollection(t *testing.T) {
	_, db := evaluationTestService(t)
	for i, name := range []string{"Chat_v2", "Chat_v3(1)", "Chat_vinvalid"} {
		row := entity.EvaluationRun{ID: fmt.Sprint(i), CollectionID: "collection", TargetType: "chat", TargetID: "target", Name: name, ConfigSnapshot: entity.JSONMap{}, CreatedBy: "owner"}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	run := &entity.EvaluationRun{CollectionID: "collection", TargetType: "chat", TargetID: "target"}
	name, err := evaluationNextRunName(db, run, "Chat")
	if err != nil || name != "Chat_v3" {
		t.Fatalf("next version=%q err=%v", name, err)
	}
	run.CollectionID = "other"
	name, err = evaluationNextRunName(db, run, "Chat")
	if err != nil || name != "Chat_v1" {
		t.Fatalf("cross-collection version=%q err=%v", name, err)
	}
}

func TestEvaluationDeleteCasePreservesForeignRunResults(t *testing.T) {
	s, db := evaluationTestService(t)
	ctx := permission.WithIdentity(t.Context(), "owner", "tenant")
	collection, err := s.CreateCollection(ctx, "Cases", "", "chat", nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCase(ctx, collection.ID, EvaluationCaseInput{Question: new("q"), ReferenceAnswer: new("a")})
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{
		&entity.EvaluationCollection{ID: "foreign", TenantID: "other", CreatedBy: "other"},
		&entity.EvaluationRun{ID: "foreign-run", CollectionID: "foreign", ConfigSnapshot: entity.JSONMap{}, Status: "COMPLETED"},
		&entity.EvaluationRun{ID: "owned-run", CollectionID: collection.ID, ConfigSnapshot: entity.JSONMap{}, Status: "COMPLETED"},
		&entity.EvaluationResult{ID: "foreign-result", RunID: "foreign-run", CaseID: c.ID, RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{}},
		&entity.EvaluationResult{ID: "owned-result", RunID: "owned-run", CaseID: c.ID, RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{}},
	} {
		if err = db.Create(model).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = s.DeleteCase(ctx, collection.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	var ids []string
	if err = db.Model(&entity.EvaluationResult{}).Pluck("id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "foreign-result" {
		t.Fatalf("unexpected survivors: %v", ids)
	}
}
