//go:build ee

// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package dao

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

const (
	evaluationMigrationMarker = "mysql_migration.enterprise.evaluation.version"
	evaluationMigrationTarget = "v1.0.0"
)

// migrateEvaluations deliberately adds columns instead of using AutoMigrate on
// historical evaluation tables: the Python JSON and timestamp column types vary.
func migrateEvaluations(ctx context.Context, database *gorm.DB) error {
	db := database.WithContext(ctx)
	version, err := getDatabaseMigrationVersion(ctx, db, evaluationMigrationMarker)
	if err != nil {
		return fmt.Errorf("read evaluation migration version: %w", err)
	}
	if shouldSkipMigration(version, evaluationMigrationTarget) {
		return nil
	}
	m := db.Migrator()
	if err := preflightEvaluations(db); err != nil {
		return err
	}
	if m.HasTable("evaluation_results") {
		var duplicates []struct {
			RunID  string
			CaseID string
			Count  int64
		}
		if err := db.Table("evaluation_results").Select("run_id, case_id, COUNT(*) AS count").Group("run_id, case_id").Having("COUNT(*) > 1").Find(&duplicates).Error; err != nil {
			return err
		}
		if len(duplicates) != 0 {
			return fmt.Errorf("evaluation_results has duplicate run/case pairs (%s/%s); resolve conflicting history before migration", duplicates[0].RunID, duplicates[0].CaseID)
		}
	}
	if m.HasTable("evaluation_datasets") && m.HasTable("evaluation_collections") {
		if err := mergeEvaluationCollections(db); err != nil {
			return err
		}
	} else if m.HasTable("evaluation_datasets") {
		if err := m.RenameTable("evaluation_datasets", "evaluation_collections"); err != nil {
			return err
		}
	}
	for _, table := range []string{"evaluation_cases", "evaluation_runs"} {
		if !m.HasTable(table) {
			continue
		}
		if evaluationHasColumn(db, table, "dataset_id") {
			if evaluationHasColumn(db, table, "collection_id") {
				var conflicts int64
				if err := db.Table(table).Where("dataset_id IS NOT NULL AND dataset_id <> '' AND collection_id IS NOT NULL AND collection_id <> '' AND dataset_id <> collection_id").Count(&conflicts).Error; err != nil {
					return err
				}
				if conflicts > 0 {
					return fmt.Errorf("%s has conflicting dataset_id and collection_id", table)
				}
				if err := db.Table(table).Where("collection_id IS NULL OR collection_id = ''").Update("collection_id", gorm.Expr("dataset_id")).Error; err != nil {
					return err
				}
				if err := relaxEvaluationColumn(db, table, "dataset_id", "varchar(32)"); err != nil {
					return err
				}
			} else if err := renameEvaluationColumn(db, table, "dataset_id", "collection_id"); err != nil {
				return err
			}
		}
	}
	if m.HasTable("evaluation_runs") && evaluationHasColumn(db, "evaluation_runs", "dialog_id") {
		if !evaluationHasColumn(db, "evaluation_runs", "target_id") {
			if err := renameEvaluationColumn(db, "evaluation_runs", "dialog_id", "target_id"); err != nil {
				return err
			}
		} else {
			var conflicts int64
			if err := db.Table("evaluation_runs").Where("dialog_id IS NOT NULL AND dialog_id <> '' AND target_id IS NOT NULL AND target_id <> '' AND dialog_id <> target_id").Count(&conflicts).Error; err != nil {
				return err
			}
			if conflicts > 0 {
				return fmt.Errorf("evaluation_runs has conflicting dialog_id and target_id")
			}
			if err := db.Table("evaluation_runs").Where("target_id IS NULL OR target_id = ''").Update("target_id", gorm.Expr("dialog_id")).Error; err != nil {
				return err
			}
			if err := relaxEvaluationColumn(db, "evaluation_runs", "dialog_id", "varchar(32)"); err != nil {
				return err
			}
		}
	}
	models := []any{&entity.EvaluationCollection{}, &entity.EvaluationCase{}, &entity.EvaluationRun{}, &entity.EvaluationResult{}}
	for _, model := range models {
		if !m.HasTable(model) {
			if err := m.CreateTable(model); err != nil {
				return err
			}
			continue
		}
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return err
		}
		for _, field := range statement.Schema.Fields {
			if field.DBName == "" || m.HasColumn(model, field.DBName) {
				continue
			}
			// Existing rows may have no value; backfill below without rewriting their types.
			added := *field
			added.NotNull = false
			datatype := m.FullDataTypeOf(&added)
			if err := db.Exec("ALTER TABLE "+quoteEvaluationName(db, statement.Table)+" ADD COLUMN "+quoteEvaluationName(db, field.DBName)+" "+datatype.SQL, datatype.Vars...).Error; err != nil {
				return err
			}
		}
	}
	if evaluationHasColumn(db, "evaluation_cases", "question") {
		if err := relaxEvaluationColumn(db, "evaluation_cases", "question", "longtext"); err != nil {
			return err
		}
	}
	if err := migrateEvaluationCaseVariables(db); err != nil {
		return err
	}
	for _, column := range []string{"question", "reference_answer"} {
		if evaluationHasColumn(db, "evaluation_cases", column) {
			archive := "evaluation_legacy_" + column
			if evaluationHasColumn(db, "evaluation_cases", archive) {
				return fmt.Errorf("evaluation_cases contains both %s and %s", column, archive)
			}
			if err := renameEvaluationColumn(db, "evaluation_cases", column, archive); err != nil {
				return err
			}
		}
	}
	if err := normalizeEvaluationIDArrays(db); err != nil {
		return err
	}
	if evaluationHasColumn(db, "evaluation_collections", "kb_ids") {
		if err := relaxEvaluationColumn(db, "evaluation_collections", "kb_ids", "longtext"); err != nil {
			return err
		}
	}
	for _, table := range []string{"evaluation_collections", "evaluation_runs"} {
		if err := db.Table(table).Where("target_type IS NULL OR target_type = ''").Update("target_type", "chat").Error; err != nil {
			return err
		}
	}
	if err := db.Table("evaluation_collections").Where("user_id IS NULL OR user_id = ''").Update("user_id", gorm.Expr("created_by")).Error; err != nil {
		return err
	}
	if err := normalizeEvaluationReferences(db); err != nil {
		return err
	}
	for _, model := range models {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return err
		}
		indexes, err := m.GetIndexes(model)
		if err != nil {
			return err
		}
		for _, wanted := range statement.Schema.ParseIndexes() {
			columns := make([]string, 0, len(wanted.Fields))
			for _, field := range wanted.Fields {
				columns = append(columns, field.DBName)
			}
			slices.Sort(columns)
			found := false
			for _, existing := range indexes {
				actual := slices.Clone(existing.Columns())
				slices.Sort(actual)
				unique, _ := existing.Unique()
				if slices.Equal(columns, actual) && (wanted.Class != "UNIQUE" || unique) {
					found = true
					break
				}
			}
			if !found {
				if err := m.CreateIndex(model, wanted.Name); err != nil {
					return err
				}
			}
		}
	}
	return setDatabaseMigrationVersionForMarker(ctx, db, evaluationMigrationMarker, evaluationMigrationTarget)
}

func quoteEvaluationName(db *gorm.DB, name string) string {
	var quoted strings.Builder
	db.Dialector.QuoteTo(&quoted, name)
	return quoted.String()
}

func mergeEvaluationCollections(db *gorm.DB) error {
	var oldRows, newRows []map[string]any
	if err := db.Table("evaluation_datasets").Find(&oldRows).Error; err != nil {
		return err
	}
	if err := db.Table("evaluation_collections").Find(&newRows).Error; err != nil {
		return err
	}
	existing := map[string]map[string]any{}
	for _, row := range newRows {
		existing[fmt.Sprint(row["id"])] = row
	}
	shared := []string{"id", "tenant_id", "name", "description", "created_by", "status", "create_time", "create_date", "update_time", "update_date"}
	inserts := []map[string]any{}
	for _, row := range oldRows {
		id := fmt.Sprint(row["id"])
		if current, ok := existing[id]; ok {
			for _, key := range shared {
				before, hadBefore := row[key]
				after, hadAfter := current[key]
				if hadBefore && hadAfter && !reflect.DeepEqual(before, after) && fmt.Sprint(before) != fmt.Sprint(after) {
					return fmt.Errorf("conflicting evaluation collection %s field %s; both tables retained", id, key)
				}
			}
			continue
		}
		insert := map[string]any{}
		for _, key := range shared {
			if value, ok := row[key]; ok && evaluationHasColumn(db, "evaluation_collections", key) {
				insert[key] = value
			}
		}
		if evaluationHasColumn(db, "evaluation_collections", "target_type") {
			insert["target_type"] = "chat"
		}
		if evaluationHasColumn(db, "evaluation_collections", "user_id") {
			insert["user_id"] = row["created_by"]
		}
		inserts = append(inserts, insert)
	}
	if db.Migrator().HasTable("evaluation_datasets_archive") {
		return fmt.Errorf("evaluation_datasets_archive already exists; inspect both dataset tables before migration")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, row := range inserts {
			if err := tx.Table("evaluation_collections").Create(row).Error; err != nil {
				return err
			}
		}
		// Preserve obsolete KB associations and any extra columns without a runtime dual path.
		return tx.Migrator().RenameTable("evaluation_datasets", "evaluation_datasets_archive")
	})
}

func relaxEvaluationColumn(db *gorm.DB, table, column, datatype string) error {
	columns, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return err
	}
	for _, field := range columns {
		if field.Name() != column {
			continue
		}
		nullable, known := field.Nullable()
		if known && nullable {
			return nil
		}
		tableName, columnName := quoteEvaluationName(db, table), quoteEvaluationName(db, column)
		switch db.Dialector.Name() {
		case "mysql":
			return db.Exec("ALTER TABLE " + tableName + " MODIFY COLUMN " + columnName + " " + datatype + " NULL").Error
		case "postgres":
			return db.Exec("ALTER TABLE " + tableName + " ALTER COLUMN " + columnName + " DROP NOT NULL").Error
		case "sqlite":
			// SQLite's migrator rebuilds the table while retaining all other columns.
			model := reflect.New(reflect.StructOf([]reflect.StructField{{Name: "Value", Type: reflect.TypeFor[*string](), Tag: reflect.StructTag(`gorm:"column:` + column + `;type:` + datatype + `"`)}})).Interface()
			return db.Table(table).Migrator().AlterColumn(model, "Value")
		default:
			return fmt.Errorf("cannot safely relax %s.%s on %s", table, column, db.Dialector.Name())
		}
	}
	return nil
}

func evaluationJSON(value any, target any) error {
	if value == nil {
		return nil
	}
	var raw []byte
	switch value := value.(type) {
	case []byte:
		raw = value
	case string:
		raw = []byte(value)
	default:
		var err error
		raw, err = json.Marshal(value)
		if err != nil {
			return err
		}
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var quoted string
	if json.Unmarshal(raw, &quoted) == nil {
		raw = []byte(quoted)
	}
	return json.Unmarshal(raw, target)
}

func migrateEvaluationCaseVariables(db *gorm.DB) error {
	var rows []map[string]any
	if err := db.Table("evaluation_cases").Find(&rows).Error; err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			variable := entity.JSONMap{}
			if err := evaluationJSON(row["variable"], &variable); err != nil {
				return fmt.Errorf("evaluation case %v variable: %w", row["id"], err)
			}
			if variable == nil {
				variable = entity.JSONMap{}
			}
			for _, key := range []string{"question", "reference_answer"} {
				value, ok := row[key]
				if !ok || value == nil {
					continue
				}
				if bytes, ok := value.([]byte); ok {
					value = string(bytes)
				}
				if current, exists := variable[key]; exists && fmt.Sprint(current) != fmt.Sprint(value) {
					return fmt.Errorf("evaluation case %v has conflicting variable.%s and %s", row["id"], key, key)
				}
				variable[key] = value
			}
			raw, err := json.Marshal(variable)
			if err != nil {
				return err
			}
			if err := tx.Table("evaluation_cases").Where("id = ?", row["id"]).Update("variable", string(raw)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func normalizeEvaluationReferences(db *gorm.DB) error {
	var rows []map[string]any
	if err := db.Table("evaluation_results").Select("id, retrieved_chunks").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		var value any
		if err := evaluationJSON(row["retrieved_chunks"], &value); err != nil {
			return fmt.Errorf("evaluation result %v references: %w", row["id"], err)
		}
		switch value := value.(type) {
		case nil:
			if err := db.Table("evaluation_results").Where("id = ?", row["id"]).Update("retrieved_chunks", "{}").Error; err != nil {
				return err
			}
		case []any:
			reference := entity.JSONMap{"chunks": value, "doc_aggs": []any{}}
			raw, err := json.Marshal(reference)
			if err != nil {
				return err
			}
			if err := db.Table("evaluation_results").Where("id = ?", row["id"]).Update("retrieved_chunks", string(raw)).Error; err != nil {
				return err
			}
		case map[string]any:
		default:
			return fmt.Errorf("evaluation result %v has unsupported reference shape %T", row["id"], value)
		}
	}
	return nil
}

// MySQL cannot roll back DDL, so reject incompatible data before the first rename.
func preflightEvaluations(db *gorm.DB) error {
	m := db.Migrator()
	for _, table := range []string{"evaluation_cases", "evaluation_runs"} {
		if !m.HasTable(table) {
			continue
		}
		pairs := [][2]string{{"dataset_id", "collection_id"}}
		if table == "evaluation_runs" {
			pairs = append(pairs, [2]string{"dialog_id", "target_id"})
		}
		for _, pair := range pairs {
			if !evaluationHasColumn(db, table, pair[0]) || !evaluationHasColumn(db, table, pair[1]) {
				continue
			}
			var conflicts int64
			condition := pair[0] + " IS NOT NULL AND " + pair[0] + " <> '' AND " + pair[1] + " IS NOT NULL AND " + pair[1] + " <> '' AND " + pair[0] + " <> " + pair[1]
			if err := db.Table(table).Where(condition).Count(&conflicts).Error; err != nil {
				return err
			}
			if conflicts > 0 {
				return fmt.Errorf("%s has conflicting %s and %s", table, pair[0], pair[1])
			}
		}
	}
	if m.HasTable("evaluation_cases") {
		var rows []map[string]any
		if err := db.Table("evaluation_cases").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			variable := entity.JSONMap{}
			if err := evaluationJSON(row["variable"], &variable); err != nil {
				return fmt.Errorf("evaluation case %v variable: %w", row["id"], err)
			}
			for _, key := range []string{"question", "reference_answer"} {
				old, exists := row[key]
				if !exists || old == nil {
					continue
				}
				if bytes, ok := old.([]byte); ok {
					old = string(bytes)
				}
				if current, exists := variable[key]; exists && fmt.Sprint(current) != fmt.Sprint(old) {
					return fmt.Errorf("evaluation case %v has conflicting variable.%s and %s", row["id"], key, key)
				}
			}
			for _, key := range []string{"relevant_doc_ids", "relevant_kb_ids"} {
				var ids any
				if err := evaluationJSON(row[key], &ids); err != nil {
					return fmt.Errorf("evaluation case %v %s: %w", row["id"], key, err)
				}
				switch ids := ids.(type) {
				case nil, []any:
				case map[string]any:
					if len(ids) > 0 {
						return fmt.Errorf("evaluation case %v %s contains a nonempty object, expected an array", row["id"], key)
					}
				default:
					return fmt.Errorf("evaluation case %v %s expected an array", row["id"], key)
				}
			}
		}
	}
	if m.HasTable("evaluation_results") {
		var rows []map[string]any
		if err := db.Table("evaluation_results").Select("id,retrieved_chunks").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			var references any
			if err := evaluationJSON(row["retrieved_chunks"], &references); err != nil {
				return fmt.Errorf("evaluation result %v references: %w", row["id"], err)
			}
			switch references.(type) {
			case nil, []any, map[string]any:
			default:
				return fmt.Errorf("evaluation result %v has unsupported reference shape %T", row["id"], references)
			}
		}
	}
	return nil
}

func normalizeEvaluationIDArrays(db *gorm.DB) error {
	var rows []map[string]any
	if err := db.Table("evaluation_cases").Select("id,relevant_doc_ids,relevant_kb_ids").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		updates := map[string]any{}
		for _, key := range []string{"relevant_doc_ids", "relevant_kb_ids"} {
			var value any
			if err := evaluationJSON(row[key], &value); err != nil {
				return err
			}
			if value == nil {
				continue
			}
			if object, ok := value.(map[string]any); ok {
				if len(object) > 0 {
					return fmt.Errorf("evaluation case %v %s is not an array", row["id"], key)
				}
				value = []any{}
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			updates[key] = string(raw)
		}
		if len(updates) > 0 {
			if err := db.Table("evaluation_cases").Where("id = ?", row["id"]).Updates(updates).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func renameEvaluationColumn(db *gorm.DB, table, oldName, newName string) error {
	return db.Exec("ALTER TABLE " + quoteEvaluationName(db, table) + " RENAME COLUMN " + quoteEvaluationName(db, oldName) + " TO " + quoteEvaluationName(db, newName)).Error
}

func evaluationHasColumn(db *gorm.DB, table, column string) bool {
	return db.Table(table).Migrator().HasColumn(&struct{ ID string }{}, column)
}
