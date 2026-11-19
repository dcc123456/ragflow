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

	"github.com/infiniflow/infinity-go-sdk"
	"go.uber.org/zap"

	"ragflow/internal/common"
)

// ChunkHistoryTableSuffix names the table that holds one dataset's chunk version
// bodies, appended to the chunk table those bodies belong to: a dataset's chunks
// live in ragflow_<tenant>_<dataset>, so its snapshots live in
// ragflow_<tenant>_<dataset>_history. The history is therefore dropped with the
// table it belongs to and never read through another dataset's name.
const ChunkHistoryTableSuffix = "_history"

// ChunkHistoryTableName is the table a dataset's version bodies live in: the
// dataset's chunk table with ChunkHistoryTableSuffix appended.
func ChunkHistoryTableName(tenantID, datasetID string) string {
	return buildChunkTableName(chunkTableBaseName(tenantID), datasetID) + ChunkHistoryTableSuffix
}

// chunkHistoryTable resolves the table a call addresses. The tenant and dataset
// name it, the way they name the chunk table it extends; with no tenant the name
// would be a table no chunk table belongs to, so the call is refused instead of
// writing somewhere nobody reads.
func chunkHistoryTable(tenantID, datasetID string) (string, error) {
	if tenantID == "" {
		return "", fmt.Errorf("chunk history: no tenant for the request; the history table extends the tenant's chunk table")
	}
	return ChunkHistoryTableName(tenantID, datasetID), nil
}

func (e *Engine) ensureChunkHistoryTable(ctx context.Context, tableName string) error {
	db, release, err := e.client.checkoutDatabase(ctx, "chunk_history.go")
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}
	defer release()

	exists, err := e.tableExistsWithDB(db, tableName)
	if err != nil {
		return fmt.Errorf("failed to check the chunk history table %s: %w", tableName, err)
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
		{"chunk_id", "varchar"},
		{"version", "integer"},
		{"doc_id", "varchar"},
		{"dataset_id", "varchar"},
		{"snapshot", "varchar"},
	} {
		columns = append(columns, &infinity.ColumnDefinition{Name: column.name, DataType: column.dataType})
	}
	if _, err := db.CreateTable(tableName, columns, infinity.ConflictTypeIgnore); err != nil {
		return fmt.Errorf("failed to create the chunk history table %s: %w", tableName, err)
	}

	table, err := db.GetTable(tableName)
	if err != nil {
		return fmt.Errorf("failed to get the chunk history table %s: %w", tableName, err)
	}
	// The lookups this table serves are "the body of this version" and "every
	// version of this document", so those are the two columns indexed.
	for _, column := range []string{"id", "doc_id"} {
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

// StashChunkHistory writes one row per entry, named by entry["id"].
//
// A row is replaced rather than appended: the address of a version's body is the
// pair the metadata row is keyed by, so rewriting an annotation edit overwrites
// exactly one row and a retried write is the same write.
func (e *Engine) StashChunkHistory(ctx context.Context, tenantID, datasetID string, docs []map[string]interface{}) error {
	if datasetID == "" || len(docs) == 0 {
		return nil
	}
	tableName, err := chunkHistoryTable(tenantID, datasetID)
	if err != nil {
		return err
	}
	if err := e.ensureChunkHistoryTable(ctx, tableName); err != nil {
		return err
	}

	db, release, err := e.client.checkoutDatabase(ctx, "chunk_history.go")
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}
	defer release()

	table, err := db.GetTable(tableName)
	if err != nil {
		return fmt.Errorf("chunk history: failed to get the table %s: %w", tableName, err)
	}

	rows := make([]map[string]interface{}, 0, len(docs))
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		id, _ := doc["id"].(string)
		if id == "" {
			return fmt.Errorf("chunk history: a snapshot arrived without an id")
		}
		ids = append(ids, fmt.Sprintf("id = %s", quoteInfinityValue(id)))
		rows = append(rows, map[string]interface{}{
			"id":         id,
			"chunk_id":   fmt.Sprint(doc["chunk_id"]),
			"version":    doc["version"],
			"doc_id":     fmt.Sprint(doc["doc_id"]),
			"dataset_id": fmt.Sprint(doc["dataset_id"]),
			"snapshot":   fmt.Sprint(doc["snapshot"]),
		})
	}
	// Replacing means removing first: the table has no primary key to conflict
	// on, so the row that already exists is what would otherwise be duplicated.
	if filter := joinBalanced(ids, " OR "); filter != "" {
		if _, err := table.Delete(filter); err != nil {
			return fmt.Errorf("chunk history: failed to replace stored snapshots in %s: %w", tableName, err)
		}
	}
	if _, err := table.Insert(rows); err != nil {
		return fmt.Errorf("chunk history: failed to write snapshots into %s: %w", tableName, err)
	}
	return nil
}

// LoadChunkHistory returns the snapshot text of every id it finds. An id with no
// row is absent from the map: the caller turns that into a missing body instead
// of an empty chunk.
func (e *Engine) LoadChunkHistory(ctx context.Context, tenantID, datasetID string, ids []string) (map[string]string, error) {
	bodies := make(map[string]string, len(ids))
	if datasetID == "" || len(ids) == 0 {
		return bodies, nil
	}
	tableName, err := chunkHistoryTable(tenantID, datasetID)
	if err != nil {
		return nil, err
	}

	db, release, err := e.client.checkoutDatabase(ctx, "chunk_history.go")
	if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}
	defer release()

	exists, err := e.tableExistsWithDB(db, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to check the chunk history table %s: %w", tableName, err)
	}
	if !exists {
		return bodies, nil
	}
	table, err := db.GetTable(tableName)
	if err != nil {
		return nil, fmt.Errorf("chunk history: failed to get the table %s: %w", tableName, err)
	}

	filters := make([]string, 0, len(ids))
	for _, id := range ids {
		filters = append(filters, fmt.Sprintf("id = %s", quoteInfinityValue(id)))
	}
	df, err := table.Output([]string{"id", "snapshot"}).
		Filter(joinBalanced(filters, " OR ")).
		Limit(len(ids)).
		ToDataFrame()
	if err != nil {
		return nil, fmt.Errorf("chunk history: failed to read snapshots from %s: %w", tableName, err)
	}

	rowIDs := columnValues(df.ColumnData["id"])
	snapshots := columnValues(df.ColumnData["snapshot"])
	rowIDs, snapshots = realignLengthPrefixedColumns(rowIDs, snapshots)
	for i, id := range rowIDs {
		if id == "" || i >= len(snapshots) {
			continue
		}
		bodies[id] = snapshots[i]
	}
	common.Debug("Chunk history loaded", zap.String("table", tableName), zap.Int("rows", len(bodies)))
	return bodies, nil
}

// DeleteChunkHistory removes every version a document recorded and reports how
// many rows went.
func (e *Engine) DeleteChunkHistory(ctx context.Context, tenantID, datasetID, docID string) (int64, error) {
	if datasetID == "" || docID == "" {
		return 0, nil
	}
	tableName, err := chunkHistoryTable(tenantID, datasetID)
	if err != nil {
		return 0, err
	}

	db, release, err := e.client.checkoutDatabase(ctx, "chunk_history.go")
	if err != nil {
		return 0, fmt.Errorf("failed to get database: %w", err)
	}
	defer release()

	exists, err := e.tableExistsWithDB(db, tableName)
	if err != nil {
		return 0, fmt.Errorf("failed to check the chunk history table %s: %w", tableName, err)
	}
	if !exists {
		return 0, nil
	}
	table, err := db.GetTable(tableName)
	if err != nil {
		return 0, fmt.Errorf("chunk history: failed to get the table %s: %w", tableName, err)
	}
	resp, err := table.Delete(fmt.Sprintf("doc_id = %s", quoteInfinityValue(docID)))
	if err != nil {
		return 0, fmt.Errorf("chunk history: failed to purge the snapshots of document %s: %w", docID, err)
	}
	if resp == nil {
		return 0, nil
	}
	return int64(resp.DeletedRows), nil
}

// DeleteChunkHistoryIDs removes the named version bodies and reports how many
// rows went.
func (e *Engine) DeleteChunkHistoryIDs(ctx context.Context, tenantID, datasetID string, ids []string) (int64, error) {
	if datasetID == "" || len(ids) == 0 {
		return 0, nil
	}
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		quoted = append(quoted, quoteInfinityValue(id))
	}
	if len(quoted) == 0 {
		return 0, nil
	}
	tableName, err := chunkHistoryTable(tenantID, datasetID)
	if err != nil {
		return 0, err
	}

	db, release, err := e.client.checkoutDatabase(ctx, "chunk_history.go")
	if err != nil {
		return 0, fmt.Errorf("failed to get database: %w", err)
	}
	defer release()

	exists, err := e.tableExistsWithDB(db, tableName)
	if err != nil {
		return 0, fmt.Errorf("failed to check the chunk history table %s: %w", tableName, err)
	}
	if !exists {
		return 0, nil
	}
	table, err := db.GetTable(tableName)
	if err != nil {
		return 0, fmt.Errorf("chunk history: failed to get the table %s: %w", tableName, err)
	}
	resp, err := table.Delete(fmt.Sprintf("id IN (%s)", strings.Join(quoted, ", ")))
	if err != nil {
		return 0, fmt.Errorf("chunk history: failed to purge %d version bodies: %w", len(quoted), err)
	}
	if resp == nil {
		return 0, nil
	}
	return int64(resp.DeletedRows), nil
}

// DropChunkHistory removes a dataset's whole chunk history.
func (e *Engine) DropChunkHistory(ctx context.Context, tenantID, datasetID string) error {
	if datasetID == "" {
		return nil
	}
	tableName, err := chunkHistoryTable(tenantID, datasetID)
	if err != nil {
		return err
	}
	// Only a name this file would have built can be dropped: the chunk table
	// itself is the same name without the suffix, and it must never go with the
	// history.
	if !strings.HasPrefix(tableName, chunkTablePrefix) || !strings.HasSuffix(tableName, ChunkHistoryTableSuffix) {
		return fmt.Errorf("chunk history: refusing to drop %q", tableName)
	}

	db, release, err := e.client.checkoutDatabase(ctx, "chunk_history.go")
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}
	defer release()

	exists, err := e.tableExistsWithDB(db, tableName)
	if err != nil {
		return fmt.Errorf("failed to check the chunk history table %s: %w", tableName, err)
	}
	if !exists {
		return nil
	}
	if _, err := db.DropTable(tableName, infinity.ConflictTypeIgnore); err != nil {
		return fmt.Errorf("chunk history: failed to drop the chunk history table %s: %w", tableName, err)
	}
	return nil
}

// quoteInfinityValue wraps a value as a single-quoted Infinity literal, doubling
// the quotes inside it so a chunk id can never end the literal early.
func quoteInfinityValue(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// realignLengthPrefixedColumns splits the length-prefixed single cell Infinity
// returns for a multi-row VARCHAR projection. Both columns are framed, so both
// must be split or every body looks missing; mismatched counts are left alone.
func realignLengthPrefixedColumns(ids, snapshots []string) ([]string, []string) {
	originalIDs, originalSnapshots := ids, snapshots
	ids = splitSingleCell(ids)
	snapshots = splitSingleCell(snapshots)
	// A column that framed into a different number of rows than its sibling is
	// not one Infinity concatenated, so the split is not applied to either side.
	if len(ids) != len(snapshots) {
		return originalIDs, originalSnapshots
	}
	return ids, snapshots
}

// splitSingleCell expands one concatenated VARCHAR cell into its row values, and
// returns the input untouched when it already is a per-row value.
func splitSingleCell(values []string) []string {
	if len(values) != 1 {
		return values
	}
	segments := splitLengthPrefixedSegments([]byte(values[0]))
	if len(segments) < 2 {
		return values
	}
	aligned := make([]string, 0, len(segments))
	for _, segment := range segments {
		aligned = append(aligned, string(segment))
	}
	return aligned
}

// columnValues renders one column of a DataFrame as strings. Infinity hands
// VARCHAR cells back as either a string or its bytes depending on the transport.
func columnValues(cells []interface{}) []string {
	values := make([]string, 0, len(cells))
	for _, cell := range cells {
		switch typed := cell.(type) {
		case nil:
			values = append(values, "")
		case string:
			values = append(values, typed)
		case []byte:
			values = append(values, string(typed))
		default:
			values = append(values, fmt.Sprint(typed))
		}
	}
	return values
}
