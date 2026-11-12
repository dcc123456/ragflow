//go:build ee

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func TestEvaluationAbsentConfigIsWritable(t *testing.T) {
	for _, input := range []any{nil, map[string]any(nil)} {
		config := evaluationMap(input)
		if config == nil {
			t.Fatal("absent configuration must become an empty object")
		}
		config["faithfulness"] = true
	}
}

func evaluationRunnerFixture(t *testing.T) (*EvaluationRunner, *entity.EvaluationRun, *entity.Chat) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err = db.AutoMigrate(
		&entity.EvaluationCollection{}, &entity.EvaluationRun{}, &entity.EvaluationCase{}, &entity.EvaluationResult{},
		&entity.Chat{}, &entity.User{}, &entity.Tenant{}, &entity.UserTenant{}, &entity.Role{}, &entity.RoleResource{}, &entity.Permission{},
	); err != nil {
		t.Fatal(err)
	}
	valid := string(entity.StatusValid)
	if err = db.Create(&entity.Tenant{ID: "tenant", ParserIDs: ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&entity.User{ID: "owner", Email: "owner@test.invalid", Nickname: "owner"}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&entity.UserTenant{ID: "owner-membership", UserID: "owner", TenantID: "tenant", Role: "owner", Status: &valid}).Error; err != nil {
		t.Fatal(err)
	}
	c := entity.EvaluationCase{ID: "case", CollectionID: "collection", Variable: entity.JSONMap{"question": "question", "reference_answer": "reference"}}
	run := entity.EvaluationRun{ID: "run", CollectionID: "collection", TargetID: "chat", TaskID: "task", Status: "RUNNING", LeaseOwner: "worker", LeaseExpiresAt: new(time.Now().Add(time.Minute).UnixMilli()), ExecutionGeneration: 2, ConfigSnapshot: entity.JSONMap{}, ExecutionConfig: entity.JSONMap{"snapshot": entity.JSONMap{"metrics": entity.JSONMap{}}, "case_ids": []string{"case"}}}
	if err = db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	r := NewEvaluationRunner(db, nil)
	r.generate = func(context.Context, *entity.Chat, *entity.EvaluationCase) (*entity.EvaluationResult, error) {
		return &entity.EvaluationResult{GeneratedAnswer: "fresh", RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{}, ExecutionTime: 1}, nil
	}
	r.score = func(context.Context, string, entity.JSONMap, *entity.EvaluationCase, *entity.EvaluationResult, []string) (entity.JSONMap, error) {
		return entity.JSONMap{"faithfulness": .8}, nil
	}
	return r, &run, &entity.Chat{}
}

func TestEvaluationPreservesGeneratedAnswerOnJudgeFailureAndRecovery(t *testing.T) {
	r, run, chat := evaluationRunnerFixture(t)
	r.score = func(context.Context, string, entity.JSONMap, *entity.EvaluationCase, *entity.EvaluationResult, []string) (entity.JSONMap, error) {
		return nil, errors.New("provider down")
	}
	if err := r.executeCase(t.Context(), run, "worker", chat, "case", "tenant", "generate_and_score", nil); err == nil {
		t.Fatal("judge failure ignored")
	}
	var got entity.EvaluationResult
	if err := r.db.Where("run_id = ?", run.ID).Take(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.GeneratedAnswer != "fresh" || got.Status != "GENERATED" {
		t.Fatalf("lost partial answer: %+v", got)
	}
	r.generate = func(context.Context, *entity.Chat, *entity.EvaluationCase) (*entity.EvaluationResult, error) {
		t.Fatal("recovery regenerated answer")
		return nil, nil
	}
	r.score = func(context.Context, string, entity.JSONMap, *entity.EvaluationCase, *entity.EvaluationResult, []string) (entity.JSONMap, error) {
		return entity.JSONMap{"faithfulness": .8}, nil
	}
	if err := r.executeCase(t.Context(), run, "worker", chat, "case", "tenant", "generate_and_score", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.executeCase(t.Context(), run, "worker", chat, "case", "tenant", "generate_and_score", nil); err != nil {
		t.Fatal(err)
	}
	var persisted entity.EvaluationRun
	r.db.Where("id = ?", run.ID).Take(&persisted)
	if persisted.Progress != 1 {
		t.Fatalf("duplicate progress %d", persisted.Progress)
	}
}

func TestEvaluationCancellationFencesLateGeneratedAnswers(t *testing.T) {
	r, run, chat := evaluationRunnerFixture(t)
	r.generate = func(context.Context, *entity.Chat, *entity.EvaluationCase) (*entity.EvaluationResult, error) {
		if err := r.db.Model(&entity.EvaluationRun{}).Where("id = ?", run.ID).Updates(map[string]any{"status": "CANCEL", "lease_owner": ""}).Error; err != nil {
			t.Fatal(err)
		}
		return &entity.EvaluationResult{GeneratedAnswer: "late"}, nil
	}
	if err := r.executeCase(t.Context(), run, "worker", chat, "case", "tenant", "generate", nil); !errors.Is(err, errEvaluationLeaseLost) {
		t.Fatalf("late write not fenced: %v", err)
	}
	var count int64
	r.db.Model(&entity.EvaluationResult{}).Count(&count)
	if count != 0 {
		t.Fatal("cancelled worker wrote answer")
	}
}

func TestEvaluationScoreOnlyRetainsAnswerAndUnselectedScores(t *testing.T) {
	r, run, chat := evaluationRunnerFixture(t)
	result := entity.EvaluationResult{ID: "result", RunID: run.ID, CaseID: "case", GeneratedAnswer: "saved", RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{"semantic_similarity": .3}, Generation: 1, Status: "COMPLETED"}
	if err := r.db.Create(&result).Error; err != nil {
		t.Fatal(err)
	}
	r.generate = func(context.Context, *entity.Chat, *entity.EvaluationCase) (*entity.EvaluationResult, error) {
		t.Fatal("score-only generated")
		return nil, nil
	}
	if err := r.executeCase(t.Context(), run, "worker", chat, "case", "tenant", "score", []string{"faithfulness"}); err != nil {
		t.Fatal(err)
	}
	var got entity.EvaluationResult
	r.db.Where("id = ?", result.ID).Take(&got)
	if got.GeneratedAnswer != "saved" || got.Metrics["semantic_similarity"] != .3 || got.Metrics["faithfulness"] != .8 {
		t.Fatalf("score-only altered unselected data: %+v", got)
	}
}

func TestEvaluationObsoleteGenerationCannotOverwriteResult(t *testing.T) {
	r, run, _ := evaluationRunnerFixture(t)
	if err := r.db.Model(&entity.EvaluationRun{}).Where("id = ?", run.ID).Update("execution_generation", 3).Error; err != nil {
		t.Fatal(err)
	}
	result := entity.EvaluationResult{ID: "result", RunID: run.ID, CaseID: "case", RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{}}
	if err := r.saveResult(t.Context(), run, "worker", &result, true); !errors.Is(err, errEvaluationLeaseLost) {
		t.Fatalf("obsolete generation saved: %v", err)
	}
}

type evaluationPublisher struct{ messages []common.TaskMessage }

func (p *evaluationPublisher) PublishTaskMessage(_ string, message common.TaskMessage) error {
	p.messages = append(p.messages, message)
	return nil
}
func TestEvaluationReconcileOnlyDueAndDuplicateClaim(t *testing.T) {
	r, run, _ := evaluationRunnerFixture(t)
	p := &evaluationPublisher{}
	r.publisher = p
	expires := time.Now().Add(time.Minute).UnixMilli()
	r.db.Model(&entity.EvaluationRun{}).Where("id = ?", run.ID).Update("lease_expires_at", expires)
	if err := r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(p.messages) != 0 {
		t.Fatal("published live lease")
	}
	if ack, err := r.Handle(t.Context(), run.TaskID, "duplicate"); ack || err != nil {
		t.Fatalf("duplicate claimed: ack=%v err=%v", ack, err)
	}
	r.db.Model(&entity.EvaluationRun{}).Where("id = ?", run.ID).Update("lease_expires_at", time.Now().Add(-time.Minute).UnixMilli())
	if err := r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(p.messages) != 1 || p.messages[0].TaskID != run.TaskID {
		t.Fatalf("lost interrupted task: %+v", p.messages)
	}
}

func TestEvaluationTargetCannotOverrideIdentity(t *testing.T) {
	chat := entity.Chat{ID: "chat", TenantID: "tenant", UserID: "owner", KBIDs: entity.JSONSlice{"old"}}
	if err := applyEvaluationTarget(&chat, entity.JSONMap{"id": "attacker", "tenant_id": "other", "user_id": "attacker", "dataset_ids": []string{"new"}, "top_n": 3}); err != nil {
		t.Fatal(err)
	}
	if chat.ID != "chat" || chat.TenantID != "tenant" || chat.UserID != "owner" || chat.KBIDs[0] != "new" || chat.TopN != 3 {
		t.Fatalf("unsafe override: %+v", chat)
	}
}

func TestEvaluationInterruptedWorkerResumesPartialAnswer(t *testing.T) {
	_, db := evaluationTestService(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&entity.Chat{}, &entity.GroupInfo{}, &entity.GroupMember{}, &entity.Permission{}); err != nil {
		t.Fatal(err)
	}
	chat := entity.Chat{ID: "chat", TenantID: "tenant", UserID: "owner", LLMSetting: entity.JSONMap{}, PromptConfig: entity.JSONMap{}, KBIDs: entity.JSONSlice{}, Status: new("1")}
	run := entity.EvaluationRun{ID: "run", CollectionID: "collection", TargetID: chat.ID, TaskID: "task", Status: "PENDING", ExecutionGeneration: 1,
		ConfigSnapshot: entity.JSONMap{}, ExecutionConfig: entity.JSONMap{"user_id": "owner", "tenant_id": "tenant", "mode": "generate_and_score", "case_ids": []string{"case"}, "chat": evaluationMap(chat)}}
	for _, row := range []any{&chat, &run, &entity.EvaluationCase{ID: "case", CollectionID: "collection", Variable: entity.JSONMap{"question": "question"}},
		&entity.Permission{ID: "chat-grant", TenantID: "tenant", MemberID: new("membership"), ResourceType: "dialog", ResourceID: new(chat.ID), Permission: entity.ResourcePermissionOwner, Status: new("1")}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	r := NewEvaluationRunner(db, nil)
	r.generate = func(context.Context, *entity.Chat, *entity.EvaluationCase) (*entity.EvaluationResult, error) {
		return &entity.EvaluationResult{GeneratedAnswer: "persist before interruption", RetrievedChunks: entity.JSONMap{}}, nil
	}
	ctx, cancel := context.WithCancel(permission.WithIdentity(t.Context(), "owner", "tenant"))
	defer cancel()
	r.score = func(context.Context, string, entity.JSONMap, *entity.EvaluationCase, *entity.EvaluationResult, []string) (entity.JSONMap, error) {
		cancel()
		return nil, context.Canceled
	}
	if ack, err := r.Handle(ctx, run.TaskID, "interrupted"); ack || !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted task acknowledged: ack=%v err=%v", ack, err)
	}
	var partial entity.EvaluationResult
	if err := db.Where("run_id = ?", run.ID).Take(&partial).Error; err != nil || partial.Status != "GENERATED" {
		t.Fatalf("partial answer lost: status=%s err=%v", partial.Status, err)
	}
	if err := db.Model(&entity.EvaluationRun{}).Where("id = ?", run.ID).Update("lease_expires_at", time.Now().Add(-time.Minute).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	r.generate = func(context.Context, *entity.Chat, *entity.EvaluationCase) (*entity.EvaluationResult, error) {
		t.Fatal("recovered task regenerated persisted answer")
		return nil, nil
	}
	r.score = func(context.Context, string, entity.JSONMap, *entity.EvaluationCase, *entity.EvaluationResult, []string) (entity.JSONMap, error) {
		return entity.JSONMap{"faithfulness": 1.0}, nil
	}
	if ack, err := r.Handle(t.Context(), run.TaskID, "recovered"); !ack || err != nil {
		t.Fatalf("recovery failed: ack=%v err=%v", ack, err)
	}
	var completed entity.EvaluationRun
	if err := db.Where("id = ?", run.ID).Take(&completed).Error; err != nil || completed.Status != "COMPLETED" || completed.Progress != 1 || completed.LeaseOwner != "" {
		t.Fatalf("recovery did not complete exactly once: status=%s progress=%d err=%v", completed.Status, completed.Progress, err)
	}
}
