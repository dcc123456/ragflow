//go:build ee

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const evaluationTaskType = "evaluation"
const evaluationLeaseTTL = 2 * time.Minute

var ErrEvaluationActive = errors.New("evaluation already has an active execution")
var errEvaluationLeaseLost = errors.New("evaluation execution ownership lost")

// EvaluationRunner uses the ingestion consumer; SQL is authoritative for ownership.
type EvaluationRunner struct {
	db        *gorm.DB
	publisher TaskPublisher
	generate  func(context.Context, *entity.Chat, *entity.EvaluationCase) (*entity.EvaluationResult, error)
	score     func(context.Context, string, entity.JSONMap, *entity.EvaluationCase, *entity.EvaluationResult, []string) (entity.JSONMap, error)
}

func NewEvaluationRunner(db *gorm.DB, publisher TaskPublisher) *EvaluationRunner {
	r := &EvaluationRunner{db: db, publisher: publisher}
	r.generate = r.generateAnswer
	r.score = EvaluateEvaluationMetrics
	return r
}

func evaluationStrings(v any) []string {
	b, _ := json.Marshal(v)
	var out []string
	_ = json.Unmarshal(b, &out)
	return out
}
func evaluationMap(v any) entity.JSONMap {
	b, _ := json.Marshal(v)
	out := entity.JSONMap{}
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = entity.JSONMap{}
	}
	return out
}

// StartExecution freezes the chat and selection without changing the saved chat.
func (r *EvaluationRunner) StartExecution(ctx context.Context, runID, mode string, caseIDs, metricIDs []string) (*entity.EvaluationRun, error) {
	if mode != "generate_and_score" && mode != "generate" && mode != "score" {
		return nil, evaluationInvalid("invalid execution mode")
	}
	userID, tenantID := permission.UserIDFromContext(ctx), permission.TenantIDFromContext(ctx)
	if userID == "" || tenantID == "" {
		return nil, permission.ErrUnauthenticated
	}
	var run entity.EvaluationRun
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", runID).Take(&run).Error; err != nil {
			return err
		}
		var collection entity.EvaluationCollection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ? AND status = ?", run.CollectionID, tenantID, 1).Take(&collection).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND collection_id = ?", runID, collection.ID).Take(&run).Error; err != nil {
			return err
		}
		if run.Status == "RUNNING" || (run.Status == "PENDING" && run.TaskID != "") {
			return ErrEvaluationActive
		}
		if run.TargetType != "chat" {
			return evaluationInvalid("only chat evaluations are supported")
		}
		if err := permission.NewDatabaseChecker(tx).CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindChat, ID: run.TargetID}, permission.OperationUse); err != nil {
			return err
		}
		var chat entity.Chat
		if err := tx.Where("id = ? AND tenant_id = ?", run.TargetID, tenantID).Take(&chat).Error; err != nil {
			return err
		}
		if err := applyEvaluationTarget(&chat, evaluationMap(run.ConfigSnapshot["target"])); err != nil {
			return err
		}
		if err := validateChatDatasetAccess(ctx, chat.KBIDs); err != nil {
			return err
		}
		var cases []entity.EvaluationCase
		q := tx.Where("collection_id = ?", run.CollectionID)
		if len(caseIDs) > 0 {
			q = q.Where("id IN ?", caseIDs)
		}
		if err := q.Order("create_time, id").Find(&cases).Error; err != nil {
			return err
		}
		if len(cases) == 0 {
			return evaluationInvalid("no evaluation cases selected")
		}
		if len(caseIDs) > 0 && len(cases) != len(caseIDs) {
			return evaluationInvalid("invalid or duplicate case selection")
		}
		caseIDs = nil
		for _, c := range cases {
			caseIDs = append(caseIDs, c.ID)
		}
		for _, metric := range metricIDs {
			if !slices.Contains(evaluationMetricNames, metric) {
				return evaluationInvalid(fmt.Sprintf("unknown metric %q", metric))
			}
		}
		if mode != "generate" {
			chosen := metricIDs
			if len(chosen) == 0 {
				chosen = evaluationMetricNames
			}
			enabled := false
			for _, name := range chosen {
				cfg := evaluationMap(evaluationMap(run.ConfigSnapshot["metrics"])[name])
				if value, ok := cfg["enable"].(bool); len(cfg) > 0 && (!ok || value) {
					enabled = true
				}
			}
			if !enabled {
				return evaluationInvalid("no enabled scoring metrics selected")
			}
		}
		now := time.Now().UnixMilli()
		run.TaskID = common.GenerateUUID()
		run.ExecutionGeneration++
		run.Status = "PENDING"
		run.Progress = 0
		run.Error = ""
		run.CompleteTime = nil
		run.LeaseOwner = ""
		run.LeaseExpiresAt = nil
		run.ExecutionConfig = entity.JSONMap{"mode": mode, "case_ids": caseIDs, "metric_ids": metricIDs, "user_id": userID, "tenant_id": tenantID, "snapshot": run.ConfigSnapshot, "chat": evaluationMap(chat)}
		result := tx.Model(&entity.EvaluationRun{}).Where("id = ? AND execution_generation = ? AND status <> 'RUNNING' AND (status <> 'PENDING' OR task_id IS NULL OR task_id = '')", run.ID, run.ExecutionGeneration-1).Updates(map[string]any{"task_id": run.TaskID, "execution_generation": run.ExecutionGeneration, "execution_config": run.ExecutionConfig, "status": run.Status, "progress": 0, "error": "", "complete_time": nil, "lease_owner": "", "lease_expires_at": nil, "update_time": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrEvaluationActive
		}
		for _, c := range cases {
			if mode == "score" {
				var existing entity.EvaluationResult
				if err := tx.Where("run_id = ? AND case_id = ?", run.ID, c.ID).Take(&existing).Error; err != nil {
					return evaluationInvalid(fmt.Sprintf("case %s has no generated answer", c.ID))
				}
				if existing.GeneratedAnswer == "" {
					return evaluationInvalid(fmt.Sprintf("case %s has no generated answer", c.ID))
				}
				continue
			}
			blank := entity.EvaluationResult{ID: common.GenerateUUID(), RunID: run.ID, CaseID: c.ID, RetrievedChunks: entity.JSONMap{}, Metrics: entity.JSONMap{}, TokenUsage: entity.JSONMap{}, ExecutionTime: -1, Status: "PENDING", Generation: run.ExecutionGeneration}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "run_id"}, {Name: "case_id"}}, DoUpdates: clause.AssignmentColumns([]string{"generated_answer", "retrieved_chunks", "metrics", "token_usage", "execution_time", "status", "error", "generation", "update_time", "update_date"})}).Create(&blank).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// A failed publish is recoverable: Reconcile republishes the committed SQL task.
	if r.publisher != nil {
		_ = r.publisher.PublishTaskMessage(common.TaskSubject, common.TaskMessage{TaskID: run.TaskID, TaskType: evaluationTaskType})
	}
	return &run, nil
}

func applyEvaluationTarget(chat *entity.Chat, target entity.JSONMap) error {
	// Identity and ownership fields are intentionally excluded from saved overrides.
	allowed := map[string]bool{"llm_id": true, "llm_setting": true, "prompt_type": true, "prompt_config": true, "similarity_threshold": true, "vector_similarity_weight": true, "top_n": true, "top_k": true, "rerank_candidates_count": true, "do_refer": true, "rerank_id": true, "kb_ids": true, "meta_data_filter": true, "decision_id": true}
	patch := entity.JSONMap{}
	for k, v := range target {
		if k == "dataset_ids" {
			k = "kb_ids"
		}
		if allowed[k] {
			patch[k] = v
		}
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, chat)
}

func (r *EvaluationRunner) Reconcile(ctx context.Context) error {
	if r.publisher == nil {
		return nil
	}
	var runs []entity.EvaluationRun
	now := time.Now().UnixMilli()
	if err := r.db.WithContext(ctx).Where("task_id <> '' AND (status = 'PENDING' OR (status = 'RUNNING' AND (lease_expires_at IS NULL OR lease_expires_at < ?)))", now).Limit(100).Find(&runs).Error; err != nil {
		return err
	}
	for _, run := range runs {
		if err := r.publisher.PublishTaskMessage(common.TaskSubject, common.TaskMessage{TaskID: run.TaskID, TaskType: evaluationTaskType}); err != nil {
			return err
		}
	}
	return nil
}

func (r *EvaluationRunner) owned(ctx context.Context, run *entity.EvaluationRun, owner string) *gorm.DB {
	return r.db.WithContext(ctx).Model(&entity.EvaluationRun{}).Where("id = ? AND task_id = ? AND execution_generation = ? AND lease_owner = ? AND status = 'RUNNING' AND lease_expires_at >= ?", run.ID, run.TaskID, run.ExecutionGeneration, owner, time.Now().UnixMilli())
}

// Handle acknowledges terminal/obsolete tasks and leaves interrupted tasks for recovery.
func (r *EvaluationRunner) Handle(ctx context.Context, taskID, owner string) (bool, error) {
	var run entity.EvaluationRun
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Take(&run).Error; err != nil {
		return errors.Is(err, gorm.ErrRecordNotFound), err
	}
	if run.Status != "PENDING" && run.Status != "RUNNING" {
		return true, nil
	}
	now := time.Now().UnixMilli()
	until := time.Now().Add(evaluationLeaseTTL).UnixMilli()
	claim := r.db.WithContext(ctx).Model(&entity.EvaluationRun{}).Where("id = ? AND task_id = ? AND execution_generation = ? AND status IN ? AND (lease_owner = '' OR lease_owner IS NULL OR lease_expires_at IS NULL OR lease_expires_at < ?)", run.ID, taskID, run.ExecutionGeneration, []string{"PENDING", "RUNNING"}, now).Updates(map[string]any{"lease_owner": owner, "lease_expires_at": until, "status": "RUNNING"})
	if claim.Error != nil {
		return false, claim.Error
	}
	if claim.RowsAffected != 1 {
		return false, nil
	}
	identity := run.ExecutionConfig
	userID, _ := identity["user_id"].(string)
	tenantID, _ := identity["tenant_id"].(string)
	metricCount := len(evaluationStrings(identity["metric_ids"]))
	if metricCount == 0 {
		metricCount = 3
	}
	timeout := (len(evaluationStrings(identity["case_ids"])) + 1) * metricCount * 60
	workCtx, cancel := context.WithTimeout(permission.WithIdentity(ctx, userID, tenantID), time.Duration(timeout)*time.Second)
	defer cancel()
	stop := make(chan struct{})
	var leaseWG sync.WaitGroup
	leaseWG.Go(func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-workCtx.Done():
				return
			case <-ticker.C:
				q := r.owned(workCtx, &run, owner).Updates(map[string]any{"lease_expires_at": time.Now().Add(evaluationLeaseTTL).UnixMilli()})
				if q.Error != nil || q.RowsAffected != 1 {
					cancel()
					return
				}
			}
		}
	})
	err := r.execute(workCtx, &run, owner)
	close(stop)
	leaseWG.Wait()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if workCtx.Err() != nil && err == nil {
		err = workCtx.Err()
	}
	finalCtx, finalCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finalCancel()
	status := "COMPLETED"
	errorText := ""
	if err != nil {
		status = "FAILED"
		errorText = err.Error()
	}
	summary, summaryErr := r.summary(finalCtx, run.ID)
	if summaryErr != nil {
		return false, summaryErr
	}
	update := r.owned(finalCtx, &run, owner).Updates(map[string]any{"status": status, "error": errorText, "metrics_summary": summary, "complete_time": time.Now().UnixMilli(), "lease_owner": "", "lease_expires_at": nil})
	if update.Error != nil {
		return false, update.Error
	}
	if update.RowsAffected == 0 {
		return true, nil
	}
	return true, err
}

func (r *EvaluationRunner) execute(ctx context.Context, run *entity.EvaluationRun, owner string) error {
	config := run.ExecutionConfig
	mode, _ := config["mode"].(string)
	tenantID, _ := config["tenant_id"].(string)
	if err := permission.NewDatabaseChecker(r.db).CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindChat, ID: run.TargetID}, permission.OperationUse); err != nil {
		return err
	}
	var chat entity.Chat
	b, _ := json.Marshal(config["chat"])
	if err := json.Unmarshal(b, &chat); err != nil {
		return err
	}
	ids := evaluationStrings(config["case_ids"])
	metrics := evaluationStrings(config["metric_ids"])
	concurrency := 4
	if n, err := strconv.Atoi(os.Getenv("RAGFLOW_EVALUATION_MAX_CONCURRENCY")); err == nil {
		concurrency = max(1, n)
	}
	jobs := make(chan string)
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex
	for range min(concurrency, len(ids)) {
		wg.Go(func() {
			for id := range jobs {
				if workCtx.Err() != nil {
					return
				}
				if err := r.executeCase(workCtx, run, owner, &chat, id, tenantID, mode, metrics); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					cancel()
					return
				}
			}
		})
	}
dispatch:
	for _, id := range ids {
		select {
		case jobs <- id:
		case <-workCtx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func (r *EvaluationRunner) executeCase(ctx context.Context, run *entity.EvaluationRun, owner string, chat *entity.Chat, id, tenantID, mode string, metrics []string) error {
	var c entity.EvaluationCase
	if err := r.db.WithContext(ctx).Where("id = ? AND collection_id = ?", id, run.CollectionID).Take(&c).Error; err != nil {
		return err
	}
	var result entity.EvaluationResult
	lookup := r.db.WithContext(ctx).Where("run_id = ? AND case_id = ?", run.ID, id).Take(&result).Error
	if lookup != nil && !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return lookup
	}
	if result.Generation == run.ExecutionGeneration && result.Status == "COMPLETED" {
		return nil
	}
	if mode == "score" && errors.Is(lookup, gorm.ErrRecordNotFound) {
		return fmt.Errorf("case %s has no generated answer", id)
	}
	if mode != "score" && !(result.Generation == run.ExecutionGeneration && result.Status == "GENERATED") {
		generated, err := r.generate(ctx, chat, &c)
		if err != nil {
			return err
		}
		if result.ID == "" {
			result.ID = common.GenerateUUID()
		}
		result.GeneratedAnswer = generated.GeneratedAnswer
		result.RetrievedChunks = generated.RetrievedChunks
		result.ExecutionTime = generated.ExecutionTime
		result.TokenUsage = generated.TokenUsage
		result.Metrics = entity.JSONMap{}
		result.RunID = run.ID
		result.CaseID = id
		result.Generation = run.ExecutionGeneration
		result.Status = "GENERATED"
		result.Error = ""
		if err := r.saveResult(ctx, run, owner, &result, false); err != nil {
			return err
		}
	}
	if mode != "generate" {
		scores, err := r.score(ctx, tenantID, evaluationMap(configSnapshot(run)["metrics"]), &c, &result, metrics)
		if err != nil {
			return err
		}
		if len(scores) == 0 {
			return fmt.Errorf("no evaluation metrics calculated")
		}
		if result.Metrics == nil {
			result.Metrics = entity.JSONMap{}
		}
		for k, v := range scores {
			result.Metrics[k] = v
		}
	}
	result.RunID = run.ID
	result.CaseID = id
	result.Generation = run.ExecutionGeneration
	result.Status = "COMPLETED"
	result.Error = ""
	return r.saveResult(ctx, run, owner, &result, true)
}

func (r *EvaluationRunner) saveResult(ctx context.Context, run *entity.EvaluationRun, owner string, result *entity.EvaluationResult, complete bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current entity.EvaluationRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND task_id = ? AND execution_generation = ? AND lease_owner = ? AND status = 'RUNNING' AND lease_expires_at >= ?", run.ID, run.TaskID, run.ExecutionGeneration, owner, time.Now().UnixMilli()).Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errEvaluationLeaseLost
			}
			return err
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "run_id"}, {Name: "case_id"}}, DoUpdates: clause.AssignmentColumns([]string{"generated_answer", "retrieved_chunks", "metrics", "execution_time", "token_usage", "status", "error", "generation", "update_time", "update_date"})}).Create(result).Error; err != nil {
			return err
		}
		if complete {
			return tx.Model(&entity.EvaluationRun{}).Where("id = ?", run.ID).Update("progress", gorm.Expr("progress + 1")).Error
		}
		return nil
	})
}

func configSnapshot(run *entity.EvaluationRun) entity.JSONMap {
	return evaluationMap(run.ExecutionConfig["snapshot"])
}

func (r *EvaluationRunner) generateAnswer(ctx context.Context, chat *entity.Chat, c *entity.EvaluationCase) (*entity.EvaluationResult, error) {
	// Clone maps: the production pipeline normalizes prompt settings in memory.
	b, _ := json.Marshal(chat)
	var isolated entity.Chat
	if err := json.Unmarshal(b, &isolated); err != nil {
		return nil, err
	}
	kwargs := map[string]any{}
	for k, v := range c.Variable {
		if k != "question" && k != "reference_answer" {
			kwargs[k] = v
		}
	}
	question, _ := c.Variable["question"].(string)
	start := time.Now()
	ch, err := NewChatPipelineService().AsyncChat(ctx, &isolated, []map[string]any{{"role": "user", "content": question}}, false, kwargs)
	if err != nil {
		return nil, err
	}
	var final AsyncChatResult
	found := false
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case item, ok := <-ch:
			if !ok {
				if !found {
					return nil, fmt.Errorf("chat pipeline ended without a final answer")
				}
				return &entity.EvaluationResult{GeneratedAnswer: final.Answer, RetrievedChunks: entity.JSONMap(final.Reference), ExecutionTime: time.Since(start).Seconds(), TokenUsage: evaluationMap(final.Usage)}, nil
			}
			if strings.HasPrefix(item.Answer, "**ERROR**:") {
				return nil, errors.New(item.Answer)
			}
			if item.Final {
				final = item
				found = true
			}
		}
	}
}

func (r *EvaluationRunner) summary(ctx context.Context, runID string) (entity.JSONMap, error) {
	var results []entity.EvaluationResult
	if err := r.db.WithContext(ctx).Where("run_id = ?", runID).Find(&results).Error; err != nil {
		return nil, err
	}
	return ComputeEvaluationSummary(results), nil
}

// CancelExecution invalidates ownership before any late provider can persist.
func (r *EvaluationRunner) CancelExecution(ctx context.Context, runID string) error {
	tenantID := permission.TenantIDFromContext(ctx)
	if tenantID == "" {
		return permission.ErrUnauthenticated
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run entity.EvaluationRun
		if err := tx.Joins("JOIN evaluation_collections ec ON ec.id = evaluation_runs.collection_id").Where("evaluation_runs.id = ? AND ec.tenant_id = ?", runID, tenantID).Take(&run).Error; err != nil {
			return err
		}
		return tx.Model(&entity.EvaluationRun{}).Where("id = ? AND status IN ?", run.ID, []string{"PENDING", "RUNNING"}).Updates(map[string]any{"status": "CANCEL", "lease_owner": "", "lease_expires_at": nil, "complete_time": time.Now().UnixMilli()}).Error
	})
}
