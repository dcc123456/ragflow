// Copyright (c) 2026 InfiniFlow. All rights reserved.
//go:build ee

package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"

	excel "github.com/jloor/go-excel-reader"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrEvaluationInvalid = errors.New("invalid evaluation request")
)

// EvaluationService manages saved evaluation resources; execution uses the existing task worker.
type EvaluationService struct{ db *gorm.DB }

func NewEvaluationService() *EvaluationService { return &EvaluationService{db: dao.DB} }
func (s *EvaluationService) authorize(ctx context.Context, write bool) error {
	if permission.UserIDFromContext(ctx) == "" || permission.TenantIDFromContext(ctx) == "" {
		return permission.ErrUnauthenticated
	}
	action := permission.ActionRead
	if write {
		action = permission.ActionWrite
	}
	return permission.NewDatabaseChecker(s.db).CheckFeature(ctx, "chat", action)
}
func evaluationInvalid(message string) error {
	return fmt.Errorf("%w: %s", ErrEvaluationInvalid, message)
}
func evaluationName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 {
		return "", evaluationInvalid("name must contain 1 to 128 bytes")
	}
	return name, nil
}
func evaluationID() string                           { return common.GenerateUUID() }
func evaluationActive(status string) bool            { return status == "RUNNING" || status == "QUEUED" }
func evaluationPagination(page, size int) (int, int) { return max(page, 1), min(max(size, 1), 200) }
func (s *EvaluationService) collection(db *gorm.DB, ctx context.Context, id string, lock bool) (*entity.EvaluationCollection, error) {
	if lock {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return dao.NewEvaluationDAO().GetCollection(ctx, db, permission.TenantIDFromContext(ctx), id)
}
func (s *EvaluationService) Collection(ctx context.Context, id string) (*entity.EvaluationCollection, error) {
	if err := s.authorize(ctx, false); err != nil {
		return nil, err
	}
	return s.collection(s.db, ctx, id, false)
}
func (s *EvaluationService) Collections(ctx context.Context, keyword string, page, size int) (entity.JSONMap, error) {
	if err := s.authorize(ctx, false); err != nil {
		return nil, err
	}
	page, size = evaluationPagination(page, size)
	q := s.db.WithContext(ctx).Model(&entity.EvaluationCollection{}).Where("tenant_id = ? AND status = ?", permission.TenantIDFromContext(ctx), 1)
	if keyword != "" {
		q = q.Where("name LIKE ?", "%"+keyword+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	values := []entity.EvaluationCollection{}
	err := q.Order("create_time DESC, id ASC").Offset((page - 1) * size).Limit(size).Find(&values).Error
	return entity.JSONMap{"collections": values, "total": total}, err
}
func evaluationUniqueName(db *gorm.DB, model any, name string, where string, args ...any) (string, error) {
	candidate := name
	for i := 1; ; i++ {
		var count int64
		if err := db.Model(model).Where(where, args...).Where("name = ?", candidate).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
		suffix := "(" + strconv.Itoa(i) + ")"
		base := name
		for len(base)+len(suffix) > 128 {
			_, n := utf8.DecodeLastRuneInString(base)
			base = base[:len(base)-n]
		}
		candidate = base + suffix
	}
}

func evaluationNextRunName(db *gorm.DB, run *entity.EvaluationRun, base string) (string, error) {
	var names []string
	if err := db.Model(&entity.EvaluationRun{}).Where("collection_id = ? AND target_type = ? AND target_id = ?", run.CollectionID, run.TargetType, run.TargetID).Pluck("name", &names).Error; err != nil {
		return "", err
	}
	prefix, version := base+"_v", 0
	for _, name := range names {
		if suffix, ok := strings.CutPrefix(name, prefix); ok {
			if number, err := strconv.Atoi(suffix); err == nil {
				version = max(version, number)
			}
		}
	}
	return evaluationName(prefix + strconv.Itoa(version+1))
}
func (s *EvaluationService) CreateCollection(ctx context.Context, name, description, targetType string, cases []EvaluationCaseInput) (*entity.EvaluationCollection, error) {
	if err := s.authorize(ctx, true); err != nil {
		return nil, err
	}
	var err error
	name, err = evaluationName(name)
	if err != nil {
		return nil, err
	}
	if targetType == "" {
		targetType = "chat"
	}
	if targetType != "chat" {
		return nil, evaluationInvalid("only chat evaluations are supported")
	}
	value := &entity.EvaluationCollection{ID: evaluationID(), UserID: permission.UserIDFromContext(ctx), TenantID: permission.TenantIDFromContext(ctx), CreatedBy: permission.UserIDFromContext(ctx), Name: name, Description: &description, TargetType: targetType, Status: 1}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		value.Name, err = evaluationUniqueName(tx, &entity.EvaluationCollection{}, name, "tenant_id = ? AND status = ?", value.TenantID, 1)
		if err != nil {
			return err
		}
		if err = tx.Create(value).Error; err != nil {
			return err
		}
		for _, input := range cases {
			row, e := s.caseValue(ctx, value.ID, input)
			if e != nil {
				return e
			}
			if e = tx.Create(row).Error; e != nil {
				return e
			}
		}
		return nil
	})
	return value, err
}
func (s *EvaluationService) UpdateCollection(ctx context.Context, id string, patch entity.JSONMap) (*entity.EvaluationCollection, error) {
	if err := s.authorize(ctx, true); err != nil {
		return nil, err
	}
	value, err := s.collection(s.db, ctx, id, false)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{}
	for key, v := range patch {
		switch key {
		case "name":
			text, ok := v.(string)
			if !ok {
				return nil, evaluationInvalid("name must be a string")
			}
			name, e := evaluationName(text)
			if e != nil {
				return nil, e
			}
			var count int64
			if e = s.db.Model(&entity.EvaluationCollection{}).Where("tenant_id = ? AND status = ? AND name = ? AND id <> ?", value.TenantID, 1, name, id).Count(&count).Error; e != nil {
				return nil, e
			}
			if count > 0 {
				return nil, evaluationInvalid("collection name already exists")
			}
			updates[key] = name
		case "description":
			if v != nil {
				if _, ok := v.(string); !ok {
					return nil, evaluationInvalid("description must be a string")
				}
			}
			updates[key] = v
		default:
			return nil, evaluationInvalid("unsupported collection field: " + key)
		}
	}
	if len(updates) > 0 {
		if err = s.db.WithContext(ctx).Model(value).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return s.collection(s.db, ctx, id, false)
}
func (s *EvaluationService) assertIdle(tx *gorm.DB, collectionID string) error {
	var count int64
	if err := tx.Model(&entity.EvaluationRun{}).Where("collection_id = ? AND (status IN ? OR (status = ? AND task_id <> ?))", collectionID, []string{"RUNNING", "QUEUED"}, "PENDING", "").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrEvaluationActive
	}
	return nil
}
func (s *EvaluationService) DeleteCollection(ctx context.Context, ids []string) error {
	if err := s.authorize(ctx, true); err != nil {
		return err
	}
	if len(ids) == 0 {
		return evaluationInvalid("ids are required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, id := range ids {
			if _, err := s.collection(tx, ctx, id, true); err != nil {
				return err
			}
			if err := s.assertIdle(tx, id); err != nil {
				return err
			}
			runIDs := tx.Model(&entity.EvaluationRun{}).Select("id").Where("collection_id = ?", id)
			if err := tx.Where("run_id IN (?)", runIDs).Delete(&entity.EvaluationResult{}).Error; err != nil {
				return err
			}
			if err := tx.Where("collection_id = ?", id).Delete(&entity.EvaluationRun{}).Error; err != nil {
				return err
			}
			if err := tx.Where("collection_id = ?", id).Delete(&entity.EvaluationCase{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", id).Delete(&entity.EvaluationCollection{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

type EvaluationCaseInput struct {
	Question        *string          `json:"question"`
	ReferenceAnswer *string          `json:"reference_answer"`
	Variable        entity.JSONMap   `json:"variable"`
	RelevantDocIDs  entity.JSONSlice `json:"relevant_doc_ids"`
	RelevantKbIDs   entity.JSONSlice `json:"relevant_kb_ids"`
	Metadata        entity.JSONMap   `json:"metadata"`
}

func (s *EvaluationService) caseValue(ctx context.Context, collectionID string, input EvaluationCaseInput) (*entity.EvaluationCase, error) {
	variable := maps.Clone(input.Variable)
	if variable == nil {
		variable = entity.JSONMap{}
	}
	if input.Question != nil {
		variable["question"] = *input.Question
	}
	if input.ReferenceAnswer != nil {
		variable["reference_answer"] = *input.ReferenceAnswer
	}
	question, ok := variable["question"].(string)
	question = strings.TrimSpace(question)
	if !ok || question == "" {
		return nil, evaluationInvalid("question is required")
	}
	variable["question"] = question
	if value, exists := variable["reference_answer"]; exists {
		if _, ok = value.(string); !ok {
			return nil, evaluationInvalid("reference_answer must be a string")
		}
	} else {
		variable["reference_answer"] = ""
	}
	checker := permission.NewDatabaseChecker(s.db)
	for _, group := range []struct {
		ids  entity.JSONSlice
		kind permission.ResourceKind
	}{{input.RelevantDocIDs, permission.ResourceKindDocument}, {input.RelevantKbIDs, permission.ResourceKindDataset}} {
		for _, v := range group.ids {
			id, ok := v.(string)
			if !ok || id == "" {
				return nil, evaluationInvalid("reference IDs must be nonempty strings")
			}
			if err := checker.CheckResource(ctx, permission.ResourceRef{Kind: group.kind, ID: id}, permission.OperationUse); err != nil {
				return nil, err
			}
		}
	}
	return &entity.EvaluationCase{ID: evaluationID(), CollectionID: collectionID, Variable: variable, RelevantDocIDs: input.RelevantDocIDs, RelevantKbIDs: input.RelevantKbIDs, Metadata: input.Metadata}, nil
}
func (s *EvaluationService) Cases(ctx context.Context, id string, page, size int) (entity.JSONMap, error) {
	if _, err := s.Collection(ctx, id); err != nil {
		return nil, err
	}
	page, size = evaluationPagination(page, size)
	q := s.db.WithContext(ctx).Model(&entity.EvaluationCase{}).Where("collection_id = ?", id)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	values := []entity.EvaluationCase{}
	err := q.Order("create_time ASC,id ASC").Offset((page - 1) * size).Limit(size).Find(&values).Error
	return entity.JSONMap{"cases": values, "total": total, "page": page, "page_size": size}, err
}
func (s *EvaluationService) CreateCase(ctx context.Context, id string, input EvaluationCaseInput) (*entity.EvaluationCase, error) {
	if err := s.authorize(ctx, true); err != nil {
		return nil, err
	}
	var value *entity.EvaluationCase
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.collection(tx, ctx, id, true); err != nil {
			return err
		}
		if err := s.assertIdle(tx, id); err != nil {
			return err
		}
		var err error
		value, err = s.caseValue(ctx, id, input)
		if err != nil {
			return err
		}
		return tx.Create(value).Error
	})
	return value, err
}
func (s *EvaluationService) ImportCases(ctx context.Context, id string, inputs []EvaluationCaseInput) (entity.JSONMap, error) {
	if err := s.authorize(ctx, true); err != nil {
		return nil, err
	}
	success, failed := 0, 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.collection(tx, ctx, id, true); err != nil {
			return err
		}
		if err := s.assertIdle(tx, id); err != nil {
			return err
		}
		for _, input := range inputs {
			row, err := s.caseValue(ctx, id, input)
			if errors.Is(err, ErrEvaluationInvalid) {
				failed++
				continue
			}
			if err != nil {
				return err
			}
			if err = tx.Create(row).Error; err != nil {
				return err
			}
			success++
		}
		return nil
	})
	return entity.JSONMap{"success_count": success, "failure_count": failed, "total": len(inputs)}, err
}
func (s *EvaluationService) UpdateCase(ctx context.Context, collectionID, id string, input EvaluationCaseInput) (*entity.EvaluationCase, error) {
	if err := s.authorize(ctx, true); err != nil {
		return nil, err
	}
	var value entity.EvaluationCase
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.collection(tx, ctx, collectionID, true); err != nil {
			return err
		}
		if err := s.assertIdle(tx, collectionID); err != nil {
			return err
		}
		if err := tx.Where("id = ? AND collection_id = ?", id, collectionID).Take(&value).Error; err != nil {
			return err
		}
		if input.Variable == nil {
			input.Variable = maps.Clone(value.Variable)
		}
		if input.Question != nil {
			input.Variable["question"] = *input.Question
		}
		if input.ReferenceAnswer != nil {
			input.Variable["reference_answer"] = *input.ReferenceAnswer
		}
		if input.RelevantDocIDs == nil {
			input.RelevantDocIDs = value.RelevantDocIDs
		}
		if input.RelevantKbIDs == nil {
			input.RelevantKbIDs = value.RelevantKbIDs
		}
		if input.Metadata == nil {
			input.Metadata = value.Metadata
		}
		updated, err := s.caseValue(ctx, collectionID, input)
		if err != nil {
			return err
		}
		if err = tx.Model(&value).Updates(map[string]any{"variable": updated.Variable, "relevant_doc_ids": updated.RelevantDocIDs, "relevant_kb_ids": updated.RelevantKbIDs, "metadata": updated.Metadata}).Error; err != nil {
			return err
		}
		runIDs := tx.Model(&entity.EvaluationRun{}).Select("id").Where("collection_id = ?", collectionID)
		if err = tx.Where("case_id = ? AND run_id IN (?)", id, runIDs).Delete(&entity.EvaluationResult{}).Error; err != nil {
			return err
		}
		return s.refreshCollectionSummaries(tx, collectionID)
	})
	if err != nil {
		return nil, err
	}
	err = s.db.Where("id = ?", id).Take(&value).Error
	return &value, err
}
func (s *EvaluationService) DeleteCase(ctx context.Context, collectionID, id string) error {
	if err := s.authorize(ctx, true); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.collection(tx, ctx, collectionID, true); err != nil {
			return err
		}
		if err := s.assertIdle(tx, collectionID); err != nil {
			return err
		}
		result := tx.Where("id = ? AND collection_id = ?", id, collectionID).Delete(&entity.EvaluationCase{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Where("case_id = ? AND run_id IN (?)", id, tx.Model(&entity.EvaluationRun{}).Select("id").Where("collection_id = ?", collectionID)).Delete(&entity.EvaluationResult{}).Error; err != nil {
			return err
		}
		return s.refreshCollectionSummaries(tx, collectionID)
	})
}
func (s *EvaluationService) refreshCollectionSummaries(tx *gorm.DB, id string) error {
	var runs []entity.EvaluationRun
	if err := tx.Where("collection_id = ?", id).Find(&runs).Error; err != nil {
		return err
	}
	for _, run := range runs {
		if err := s.refreshSummary(tx, run.ID); err != nil {
			return err
		}
	}
	return nil
}
func (s *EvaluationService) refreshSummary(tx *gorm.DB, id string) error {
	var rows []entity.EvaluationResult
	if err := tx.Where("run_id = ?", id).Find(&rows).Error; err != nil {
		return err
	}
	return tx.Model(&entity.EvaluationRun{}).Where("id = ?", id).Update("metrics_summary", ComputeEvaluationSummary(rows)).Error
}

// ReadEvaluationCases reads every sheet and preserves additional variable columns.
func ReadEvaluationCases(filename string, data []byte) ([]EvaluationCaseInput, int, error) {
	var sheets [][][]string
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".csv":
		reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
		reader.FieldsPerRecord = -1
		rows, err := reader.ReadAll()
		if err != nil {
			return nil, 0, evaluationInvalid("invalid CSV: " + err.Error())
		}
		sheets = append(sheets, rows)
	case ".xlsx":
		book, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 64 << 20, UnzipXMLSizeLimit: 16 << 20})
		if err != nil {
			return nil, 0, evaluationInvalid("invalid XLSX workbook")
		}
		defer book.Close()
		for _, sheet := range book.GetSheetList() {
			rows, e := book.GetRows(sheet)
			if e != nil {
				return nil, 0, e
			}
			sheets = append(sheets, rows)
		}
	case ".xls":
		reader, err := excel.OpenXLS(bytes.NewReader(data), excel.WithErrorValues(true))
		if err != nil {
			return nil, 0, evaluationInvalid("invalid XLS workbook")
		}
		defer reader.Close()
		for reader.NextSheet() {
			var rows [][]string
			for reader.Read() {
				row := make([]string, reader.FieldCount())
				for i := range row {
					if v := reader.GetValue(i); v != nil {
						row[i] = fmt.Sprint(v)
					}
				}
				rows = append(rows, row)
			}
			sheets = append(sheets, rows)
		}
		if err = reader.Err(); err != nil {
			return nil, 0, evaluationInvalid("invalid XLS workbook: " + err.Error())
		}
	default:
		return nil, 0, evaluationInvalid("supported file types are CSV, XLS and XLSX")
	}
	values := []EvaluationCaseInput{}
	failed := 0
	for _, rows := range sheets {
		for len(rows) > 0 && evaluationBlankRow(rows[0]) {
			rows = rows[1:]
		}
		if len(rows) == 0 {
			continue
		}
		header := make([]string, len(rows[0]))
		for i, v := range rows[0] {
			header[i] = strings.ToLower(strings.TrimSpace(v))
		}
		if !slices.Contains(header, "question") || !slices.Contains(header, "reference_answer") {
			return nil, 0, evaluationInvalid("every nonempty sheet requires question and reference_answer columns")
		}
		for _, row := range rows[1:] {
			if evaluationBlankRow(row) {
				continue
			}
			variable := entity.JSONMap{}
			for i, key := range header {
				if key != "" && i < len(row) {
					variable[key] = strings.TrimSpace(row[i])
				}
			}
			question, _ := variable["question"].(string)
			if question == "" {
				failed++
				continue
			}
			if _, ok := variable["reference_answer"]; !ok {
				variable["reference_answer"] = ""
			}
			values = append(values, EvaluationCaseInput{Variable: variable})
		}
	}
	if len(sheets) == 0 {
		return nil, 0, evaluationInvalid("workbook has no sheets")
	}
	return values, failed, nil
}
func evaluationBlankRow(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
func EvaluationWorkbook(headers []string, rows [][]any) ([]byte, error) {
	book := excelize.NewFile()
	defer book.Close()
	head := make([]any, len(headers))
	for i, v := range headers {
		head[i] = v
	}
	if err := book.SetSheetRow("Sheet1", "A1", &head); err != nil {
		return nil, err
	}
	for i, row := range rows {
		clean := slices.Clone(row)
		for j, v := range clean {
			if text, ok := v.(string); ok {
				text = strings.Map(func(r rune) rune {
					if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
						return -1
					}
					return r
				}, text)
				runes := []rune(text)
				if len(runes) > 32767 {
					text = string(runes[:32767])
				}
				clean[j] = text
			}
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := book.SetSheetRow("Sheet1", cell, &clean); err != nil {
			return nil, err
		}
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
func (s *EvaluationService) ExportCollection(ctx context.Context, id string) ([]byte, string, error) {
	value, err := s.Collection(ctx, id)
	if err != nil {
		return nil, "", err
	}
	var cases []entity.EvaluationCase
	if err = s.db.WithContext(ctx).Where("collection_id = ?", id).Order("create_time ASC,id ASC").Find(&cases).Error; err != nil {
		return nil, "", err
	}
	rows := make([][]any, 0, len(cases))
	for _, c := range cases {
		timestamp := any("")
		if c.CreateTime != nil {
			timestamp = *c.CreateTime
		}
		rows = append(rows, []any{c.ID, c.Variable["question"], c.Variable["reference_answer"], timestamp})
	}
	data, err := EvaluationWorkbook([]string{"id", "question", "reference_answer", "create_time"}, rows)
	return data, value.Name + ".xlsx", err
}

type EvaluationRunInput struct {
	CollectionID   string         `json:"collection_id"`
	TargetType     string         `json:"target_type"`
	TargetID       string         `json:"target_id"`
	Name           string         `json:"name"`
	ConfigSnapshot entity.JSONMap `json:"config_snapshot"`
	SourceRunID    string         `json:"source_run_id"`
}

func (s *EvaluationService) run(db *gorm.DB, ctx context.Context, id string, lock bool) (*entity.EvaluationRun, error) {
	if lock {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return dao.NewEvaluationDAO().GetRun(ctx, db, permission.TenantIDFromContext(ctx), id)
}
func (s *EvaluationService) Run(ctx context.Context, id string) (*entity.EvaluationRun, error) {
	if err := s.authorize(ctx, false); err != nil {
		return nil, err
	}
	return s.run(s.db, ctx, id, false)
}
func (s *EvaluationService) Runs(ctx context.Context, targetID, collectionID, keyword string, page, size int) (entity.JSONMap, error) {
	if err := s.authorize(ctx, false); err != nil {
		return nil, err
	}
	page, size = evaluationPagination(page, size)
	q := s.db.WithContext(ctx).Model(&entity.EvaluationRun{}).Where("collection_id IN (?)", s.db.Model(&entity.EvaluationCollection{}).Select("id").Where("tenant_id = ? AND status = ?", permission.TenantIDFromContext(ctx), 1))
	if targetID != "" {
		q = q.Where("target_id = ?", targetID)
	}
	if collectionID != "" {
		q = q.Where("collection_id = ?", collectionID)
	}
	if keyword != "" {
		q = q.Where("name LIKE ?", "%"+keyword+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	values := []entity.EvaluationRun{}
	err := q.Order("create_time DESC,id ASC").Offset((page - 1) * size).Limit(size).Find(&values).Error
	return entity.JSONMap{"runs": values, "total": total}, err
}

var evaluationTargetFields = []string{"name", "llm_id", "llm_setting", "prompt_type", "prompt_config", "meta_data_filter", "similarity_threshold", "vector_similarity_weight", "top_n", "top_k", "rerank_candidates_count", "do_refer", "rerank_id", "kb_ids", "decision_id"}

func (s *EvaluationService) runSnapshot(ctx context.Context, input EvaluationRunInput) (entity.JSONMap, string, error) {
	if input.TargetType != "" && input.TargetType != "chat" {
		return nil, "", evaluationInvalid("only chat evaluations are supported")
	}
	checker := permission.NewDatabaseChecker(s.db)
	if err := checker.CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindChat, ID: input.TargetID}, permission.OperationUse); err != nil {
		return nil, "", err
	}
	var chat entity.Chat
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", input.TargetID, permission.TenantIDFromContext(ctx)).Take(&chat).Error; err != nil {
		return nil, "", err
	}
	b, err := json.Marshal(chat)
	if err != nil {
		return nil, "", err
	}
	all := entity.JSONMap{}
	if err = json.Unmarshal(b, &all); err != nil {
		return nil, "", err
	}
	target := entity.JSONMap{}
	for _, key := range evaluationTargetFields {
		if v, ok := all[key]; ok {
			target[key] = v
		}
	}
	if supplied, ok := input.ConfigSnapshot["target"]; ok && supplied != nil {
		provided := evaluationMap(supplied)
		if len(provided) == 0 {
			return nil, "", evaluationInvalid("target must be a nonempty object")
		}
		for key, v := range provided {
			if !slices.Contains(evaluationTargetFields, key) {
				return nil, "", evaluationInvalid("unsupported target snapshot field: " + key)
			}
			target[key] = v
		}
	}
	if err = applyEvaluationTarget(&chat, target); err != nil {
		return nil, "", evaluationInvalid(err.Error())
	}
	if chat.LLMID != "" {
		if _, e := NewModelFactory().ResolveInfo(ctx, entity.ModelTypeChat, chat.LLMID); e != nil {
			return nil, "", evaluationInvalid("generation model: " + e.Error())
		}
	}
	for _, v := range chat.KBIDs {
		id, ok := v.(string)
		if !ok {
			return nil, "", evaluationInvalid("kb_ids must contain strings")
		}
		if err = checker.CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindDataset, ID: id}, permission.OperationUse); err != nil {
			return nil, "", err
		}
	}
	metrics := evaluationMap(input.ConfigSnapshot["metrics"])
	var membership entity.UserTenant
	if err = s.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", permission.TenantIDFromContext(ctx), permission.UserIDFromContext(ctx)).Take(&membership).Error; err != nil {
		return nil, "", err
	}
	if len(metrics) == 0 {
		for _, key := range []string{"context_relevance", "faithfulness", "semantic_similarity"} {
			metrics[key] = entity.JSONMap{"enable": true, "llm_id": membership.LLMID}
		}
	}
	for key, value := range metrics {
		if !slices.Contains([]string{"context_relevance", "faithfulness", "semantic_similarity"}, key) {
			return nil, "", evaluationInvalid("unknown metric: " + key)
		}
		setting := evaluationMap(value)
		enable, ok := setting["enable"].(bool)
		if !ok {
			return nil, "", evaluationInvalid("metric enable must be boolean")
		}
		if setting["llm_id"] == nil || setting["llm_id"] == "" {
			setting["llm_id"] = membership.LLMID
		}
		model, ok := setting["llm_id"].(string)
		if !ok {
			return nil, "", evaluationInvalid("metric llm_id must be a string")
		}
		if enable && model == "" {
			return nil, "", evaluationInvalid("enabled metrics require a judge model")
		}
		if enable {
			if _, e := NewModelFactory().ResolveInfo(ctx, entity.ModelTypeChat, model); e != nil {
				return nil, "", evaluationInvalid("judge model: " + e.Error())
			}
		}
		metrics[key] = setting
	}
	name := "Evaluation"
	if chat.Name != nil {
		name = *chat.Name
	}
	return entity.JSONMap{"target": target, "metrics": metrics}, name, nil
}
func (s *EvaluationService) CreateRun(ctx context.Context, input EvaluationRunInput) (*entity.EvaluationRun, error) {
	if err := s.authorize(ctx, true); err != nil {
		return nil, err
	}
	if input.SourceRunID != "" {
		source, err := s.Run(ctx, input.SourceRunID)
		if err != nil {
			return nil, err
		}
		input.CollectionID = source.CollectionID
		input.TargetType = source.TargetType
		input.TargetID = source.TargetID
		input.ConfigSnapshot = source.ConfigSnapshot
		if input.Name == "" {
			input.Name = source.Name
		}
	}
	collection, err := s.Collection(ctx, input.CollectionID)
	if err != nil {
		return nil, err
	}
	if input.TargetType == "" {
		input.TargetType = "chat"
	}
	if input.TargetType != collection.TargetType {
		return nil, evaluationInvalid("target type does not match collection")
	}
	snapshot, targetName, err := s.runSnapshot(ctx, input)
	if err != nil {
		return nil, err
	}
	automaticName := strings.TrimSpace(input.Name) == ""
	if automaticName {
		input.Name = targetName + "_v1"
	}
	name, err := evaluationName(input.Name)
	if err != nil {
		return nil, err
	}
	run := &entity.EvaluationRun{ID: evaluationID(), CollectionID: input.CollectionID, TargetType: input.TargetType, TargetID: input.TargetID, Name: name, ConfigSnapshot: snapshot, Status: "PENDING", CreatedBy: permission.UserIDFromContext(ctx)}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := s.collection(tx, ctx, input.CollectionID, true); e != nil {
			return e
		}
		var e error
		if automaticName {
			run.Name, e = evaluationNextRunName(tx, run, targetName)
		} else {
			run.Name, e = evaluationUniqueName(tx, &entity.EvaluationRun{}, name, "collection_id = ? AND target_type = ? AND target_id = ?", run.CollectionID, run.TargetType, run.TargetID)
		}
		if e != nil {
			return e
		}
		return tx.Create(run).Error
	})
	return run, err
}
func (s *EvaluationService) UpdateRun(ctx context.Context, id string, patch entity.JSONMap) (*entity.EvaluationRun, error) {
	if err := s.authorize(ctx, true); err != nil {
		return nil, err
	}
	var updated *entity.EvaluationRun
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, e := s.run(tx, ctx, id, true)
		if e != nil {
			return e
		}
		if evaluationActive(run.Status) || (run.Status == "PENDING" && run.TaskID != "") {
			return ErrEvaluationActive
		}
		updates := map[string]any{}
		for key, value := range patch {
			switch key {
			case "name":
				name, ok := value.(string)
				if !ok {
					return evaluationInvalid("name must be a string")
				}
				name, e = evaluationName(name)
				if e != nil {
					return e
				}
				updates[key] = name
			case "config_snapshot":
				snapshot, _, e := s.runSnapshot(ctx, EvaluationRunInput{TargetID: run.TargetID, TargetType: run.TargetType, ConfigSnapshot: evaluationMap(value)})
				if e != nil {
					return e
				}
				updates[key] = snapshot
			default:
				return evaluationInvalid("unsupported run field: " + key)
			}
		}
		if len(updates) > 0 {
			if e = tx.Model(run).Updates(updates).Error; e != nil {
				return e
			}
		}
		updated, e = s.run(tx, ctx, id, false)
		return e
	})
	return updated, err
}
func (s *EvaluationService) DeleteRun(ctx context.Context, id string) error {
	if err := s.authorize(ctx, true); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := s.run(tx, ctx, id, true)
		if err != nil {
			return err
		}
		if evaluationActive(run.Status) || (run.Status == "PENDING" && run.TaskID != "") {
			return ErrEvaluationActive
		}
		if err = tx.Where("run_id = ?", id).Delete(&entity.EvaluationResult{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&entity.EvaluationRun{}).Error
	})
}

type EvaluationResultView struct {
	entity.EvaluationResult
	Variable entity.JSONMap `json:"variable"`
}

func (s *EvaluationService) Results(ctx context.Context, id string, page, size int) (entity.JSONMap, error) {
	run, err := s.Run(ctx, id)
	if err != nil {
		return nil, err
	}
	page, size = evaluationPagination(page, size)
	q := s.db.WithContext(ctx).Model(&entity.EvaluationResult{}).Where("run_id = ?", id)
	var total int64
	if err = q.Count(&total).Error; err != nil {
		return nil, err
	}
	rows := []EvaluationResultView{}
	err = q.Select("evaluation_results.*, evaluation_cases.variable").Joins("JOIN evaluation_cases ON evaluation_cases.id = evaluation_results.case_id AND evaluation_cases.collection_id = ?", run.CollectionID).Order("evaluation_results.create_time ASC, evaluation_results.id ASC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return entity.JSONMap{"run": run, "results": rows, "total": total}, err
}
func (s *EvaluationService) ClearResult(ctx context.Context, runID, caseID, part, metric string) error {
	if err := s.authorize(ctx, true); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := s.run(tx, ctx, runID, true)
		if err != nil {
			return err
		}
		if evaluationActive(run.Status) || (run.Status == "PENDING" && run.TaskID != "") {
			return ErrEvaluationActive
		}
		var row entity.EvaluationResult
		if err = tx.Where("run_id = ? AND case_id = ?", runID, caseID).Take(&row).Error; err != nil {
			return err
		}
		updates := map[string]any{}
		switch part {
		case "result", "answer":
			updates = map[string]any{"generated_answer": "", "retrieved_chunks": entity.JSONMap{}, "metrics": entity.JSONMap{}, "execution_time": 0, "token_usage": nil, "status": "PENDING", "error": ""}
		case "metrics":
			if metric == "" {
				updates["metrics"] = entity.JSONMap{}
			} else {
				if !slices.Contains([]string{"context_relevance", "faithfulness", "semantic_similarity"}, metric) {
					return evaluationInvalid("unknown metric")
				}
				delete(row.Metrics, metric)
				delete(row.Metrics, metric+"_reason")
				updates["metrics"] = row.Metrics
			}
		default:
			return evaluationInvalid("unknown result component")
		}
		if err = tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		return s.refreshSummary(tx, runID)
	})
}
func (s *EvaluationService) ExportRun(ctx context.Context, id string, caseIDs []string) ([]byte, string, error) {
	run, err := s.Run(ctx, id)
	if err != nil {
		return nil, "", err
	}
	var results []EvaluationResultView
	q := s.db.WithContext(ctx).Model(&entity.EvaluationResult{}).Select("evaluation_results.*, evaluation_cases.variable").Joins("JOIN evaluation_cases ON evaluation_cases.id = evaluation_results.case_id AND evaluation_cases.collection_id = ?", run.CollectionID).Where("evaluation_results.run_id = ?", id)
	if len(caseIDs) > 0 {
		q = q.Where("evaluation_results.case_id IN ?", caseIDs)
	}
	if err = q.Order("evaluation_results.create_time ASC,evaluation_results.id ASC").Find(&results).Error; err != nil {
		return nil, "", err
	}
	metrics := []string{}
	for _, row := range results {
		for key, value := range row.Metrics {
			if _, ok := value.(float64); ok && !slices.Contains(metrics, key) {
				metrics = append(metrics, key)
			}
		}
	}
	slices.Sort(metrics)
	headers := append([]string{"question", "reference_answer", "generated_answer", "execution_time", "token_usage", "retrieved_chunks_count", "retrieved_doc_ids"}, metrics...)
	rows := [][]any{}
	for _, row := range results {
		chunks := evaluationMap(row.RetrievedChunks)
		chunkList, _ := chunks["chunks"].([]any)
		docIDs := []string{}
		for _, chunk := range chunkList {
			m := evaluationMap(chunk)
			if doc, ok := m["doc_id"].(string); ok && !slices.Contains(docIDs, doc) {
				docIDs = append(docIDs, doc)
			}
		}
		tokens := ""
		if row.TokenUsage != nil {
			b, _ := json.Marshal(row.TokenUsage)
			tokens = string(b)
		}
		values := []any{row.Variable["question"], row.Variable["reference_answer"], row.GeneratedAnswer, row.ExecutionTime, tokens, len(chunkList), strings.Join(docIDs, ",")}
		for _, key := range metrics {
			values = append(values, row.Metrics[key])
		}
		rows = append(rows, values)
	}
	data, err := EvaluationWorkbook(headers, rows)
	return data, run.Name + ".xlsx", err
}

func (s *EvaluationService) StartExecution(ctx context.Context, id, mode string, caseIDs, metricIDs []string) (*entity.EvaluationRun, error) {
	if err := s.authorize(ctx, true); err != nil {
		return nil, err
	}
	return NewEvaluationRunner(s.db, NewMessageQueueTaskPublisher()).StartExecution(ctx, id, mode, caseIDs, metricIDs)
}
func (s *EvaluationService) CancelExecution(ctx context.Context, id string) error {
	if err := s.authorize(ctx, true); err != nil {
		return err
	}
	return NewEvaluationRunner(s.db, NewMessageQueueTaskPublisher()).CancelExecution(ctx, id)
}
