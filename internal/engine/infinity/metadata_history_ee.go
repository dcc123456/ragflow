//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package infinity

import (
	"context"
	"fmt"
	"strings"

	"ragflow/internal/common"

	infinity "github.com/infiniflow/infinity-go-sdk"
	"go.uber.org/zap"
)

// MetadataHistoryTableSuffix names the table that holds a tenant's document
// metadata snapshots, appended to the doc_meta table those snapshots belong to:
// a tenant's metadata lives in ragflow_doc_meta_<tenant>, so its snapshots live
// in ragflow_doc_meta_<tenant>_history. dataset_id narrows every read to one
// dataset's slice of it.
//
// The table follows the one it belongs to rather than the dataset, the way the
// chunk history follows its dataset's chunk table.
const MetadataHistoryTableSuffix = "_history"

// MetadataHistoryTableName is the table a tenant's metadata snapshots live in:
// the tenant's doc_meta table with MetadataHistoryTableSuffix appended.
func MetadataHistoryTableName(tenantID string) string {
	return buildMetadataTableName(tenantID) + MetadataHistoryTableSuffix
}

func (e *Engine) ensureMetadataHistoryTable(ctx context.Context, tableName string) error {
	db, release, err := e.client.checkoutDatabase(ctx, "metadata_history.go")
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}
	defer release()

	exists, err := e.tableExistsWithDB(db, tableName)
	if err != nil {
		return fmt.Errorf("failed to check the metadata history table %s: %w", tableName, err)
	}
	if exists {
		return nil
	}

	var columns infinity.TableSchema
	for _, column := range []struct {
		name     string
		dataType string
	}{
		{"id", "varchar"},
		{"document_id", "varchar"},
		{"dataset_id", "varchar"},
		{"snapshot", "varchar"},
	} {
		columns = append(columns, &infinity.ColumnDefinition{Name: column.name, DataType: column.dataType})
	}
	if _, err := db.CreateTable(tableName, columns, infinity.ConflictTypeIgnore); err != nil {
		return fmt.Errorf("failed to create the metadata history table %s: %w", tableName, err)
	}

	table, err := db.GetTable(tableName)
	if err != nil {
		return fmt.Errorf("failed to get the metadata history table %s: %w", tableName, err)
	}
	// The lookups this table serves are "the snapshot with this id", "every
	// snapshot of this document" and "every snapshot of this dataset", so those are
	// the columns indexed.
	for _, column := range []string{"id", "document_id", "dataset_id"} {
		if _, err := table.CreateIndex(
			fmt.Sprintf("idx_%s_%s", tableName, column),
			infinity.NewIndexInfo(column, infinity.IndexTypeSecondary, nil),
			infinity.ConflictTypeIgnore,
			"",
		); err != nil {
			return fmt.Errorf("failed to create the %s index of %s: %w", column, tableName, err)
		}
	}
	return nil
}

// StashMetadataHistory writes one row per entry, named by entry["id"].
//
// A row is replaced rather than appended: the address of a snapshot is content
// derived, so rewriting the same values overwrites exactly one row and a retried
// write is the same write.
func (e *Engine) StashMetadataHistory(ctx context.Context, tenantID, datasetID string, docs []map[string]interface{}) error {
	if tenantID == "" || datasetID == "" || len(docs) == 0 {
		return nil
	}
	tableName := MetadataHistoryTableName(tenantID)
	if err := e.ensureMetadataHistoryTable(ctx, tableName); err != nil {
		return err
	}

	db, release, err := e.client.checkoutDatabase(ctx, "metadata_history.go")
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}
	defer release()

	table, err := db.GetTable(tableName)
	if err != nil {
		return fmt.Errorf("metadata history: failed to get the table %s: %w", tableName, err)
	}

	rows := make([]map[string]interface{}, 0, len(docs))
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		id, _ := doc["id"].(string)
		if id == "" {
			return fmt.Errorf("metadata history: a snapshot arrived without an id")
		}
		ids = append(ids, fmt.Sprintf("id = %s", quoteInfinityValue(id)))
		rows = append(rows, map[string]interface{}{
			"id":          id,
			"document_id": fmt.Sprint(doc["document_id"]),
			"dataset_id":  fmt.Sprint(doc["dataset_id"]),
			"snapshot":    fmt.Sprint(doc["snapshot"]),
		})
	}
	// Replacing means removing first: the table has no primary key to conflict
	// on, so the row that already exists is what would otherwise be duplicated.
	if filter := joinBalanced(ids, " OR "); filter != "" {
		if _, err := table.Delete(filter); err != nil {
			return fmt.Errorf("metadata history: failed to replace stored snapshots in %s: %w", tableName, err)
		}
	}
	if _, err := table.Insert(rows); err != nil {
		return fmt.Errorf("metadata history: failed to write snapshots into %s: %w", tableName, err)
	}
	return nil
}

// LoadMetadataHistory returns the values JSON of every id it finds. An id with no
// row is absent from the map: the caller turns that into a missing snapshot instead
// of an empty one.
func (e *Engine) LoadMetadataHistory(ctx context.Context, tenantID, datasetID string, ids []string) (map[string]string, error) {
	bodies := make(map[string]string, len(ids))
	if tenantID == "" || datasetID == "" || len(ids) == 0 {
		return bodies, nil
	}
	tableName := MetadataHistoryTableName(tenantID)

	db, release, err := e.client.checkoutDatabase(ctx, "metadata_history.go")
	if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}
	defer release()

	exists, err := e.tableExistsWithDB(db, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to check the metadata history table %s: %w", tableName, err)
	}
	if !exists {
		return bodies, nil
	}
	table, err := db.GetTable(tableName)
	if err != nil {
		return nil, fmt.Errorf("metadata history: failed to get the table %s: %w", tableName, err)
	}

	filters := make([]string, 0, len(ids))
	for _, id := range ids {
		filters = append(filters, fmt.Sprintf("id = %s", quoteInfinityValue(id)))
	}
	// The table holds every dataset of the tenant, so the lookups are narrowed to
	// this one: an id is only unique within a dataset's history.
	scoped := fmt.Sprintf("(%s) AND dataset_id = %s", joinBalanced(filters, " OR "), quoteInfinityValue(datasetID))
	df, err := table.Output([]string{"id", "snapshot"}).
		Filter(scoped).
		Limit(len(ids)).
		ToDataFrame()
	if err != nil {
		return nil, fmt.Errorf("metadata history: failed to read snapshots from %s: %w", tableName, err)
	}

	rowIDs := columnValues(df.ColumnData["id"])
	snapshots := columnValues(df.ColumnData["snapshot"])
	// Infinity concatenates a multi-row VARCHAR column into one cell with a
	// length-prefixed segment per row; split it back when that is the shape that
	// came back.
	if len(rowIDs) == 1 && len(snapshots) == 1 && len(snapshots[0]) > 0 {
		if segments := splitLengthPrefixedSegments([]byte(snapshots[0])); len(segments) > 1 {
			snapshots = make([]string, 0, len(segments))
			for _, segment := range segments {
				snapshots = append(snapshots, string(segment))
			}
		}
	}
	for i, id := range rowIDs {
		if id == "" || i >= len(snapshots) {
			continue
		}
		bodies[id] = snapshots[i]
	}
	common.Debug("Metadata history loaded", zap.String("table", tableName), zap.Int("rows", len(bodies)))
	return bodies, nil
}

// DeleteMetadataHistory removes every snapshot a document recorded and reports how
// many rows went.
func (e *Engine) DeleteMetadataHistory(ctx context.Context, tenantID, datasetID, documentID string) (int64, error) {
	if tenantID == "" || datasetID == "" || documentID == "" {
		return 0, nil
	}
	tableName := MetadataHistoryTableName(tenantID)

	db, release, err := e.client.checkoutDatabase(ctx, "metadata_history.go")
	if err != nil {
		return 0, fmt.Errorf("failed to get database: %w", err)
	}
	defer release()

	exists, err := e.tableExistsWithDB(db, tableName)
	if err != nil {
		return 0, fmt.Errorf("failed to check the metadata history table %s: %w", tableName, err)
	}
	if !exists {
		return 0, nil
	}
	table, err := db.GetTable(tableName)
	if err != nil {
		return 0, fmt.Errorf("metadata history: failed to get the table %s: %w", tableName, err)
	}
	// The document id alone is not the address: the table holds every dataset of
	// the tenant, so a purge names the dataset too and cannot take another
	// dataset's snapshots.
	resp, err := table.Delete(fmt.Sprintf("document_id = %s AND dataset_id = %s",
		quoteInfinityValue(documentID), quoteInfinityValue(datasetID)))
	if err != nil {
		return 0, fmt.Errorf("metadata history: failed to purge the snapshots of document %s: %w", documentID, err)
	}
	if resp == nil {
		return 0, nil
	}
	return int64(resp.DeletedRows), nil
}

// DropMetadataHistory removes a dataset's whole metadata history.
//
// The table belongs to the tenant, not to the dataset, so it stays: dropping it
// would take every other dataset of the tenant's history with it. What goes is
// this dataset's rows.
func (e *Engine) DropMetadataHistory(ctx context.Context, tenantID, datasetID string) error {
	if tenantID == "" || datasetID == "" {
		return nil
	}
	tableName := MetadataHistoryTableName(tenantID)
	// Only a name this file would have built can be dropped: the doc_meta table
	// itself is the same name without the suffix, and it must never go with the
	// history.
	if !strings.HasPrefix(tableName, "ragflow_doc_meta_") || !strings.HasSuffix(tableName, MetadataHistoryTableSuffix) {
		return fmt.Errorf("metadata history: refusing to drop %q", tableName)
	}

	db, release, err := e.client.checkoutDatabase(ctx, "metadata_history.go")
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}
	defer release()

	exists, err := e.tableExistsWithDB(db, tableName)
	if err != nil {
		return fmt.Errorf("failed to check the metadata history table %s: %w", tableName, err)
	}
	if !exists {
		return nil
	}
	table, err := db.GetTable(tableName)
	if err != nil {
		return fmt.Errorf("metadata history: failed to get the table %s: %w", tableName, err)
	}
	if _, err := table.Delete(fmt.Sprintf("dataset_id = %s", quoteInfinityValue(datasetID))); err != nil {
		return fmt.Errorf("metadata history: failed to purge the snapshots of dataset %s: %w", datasetID, err)
	}
	return nil
}
