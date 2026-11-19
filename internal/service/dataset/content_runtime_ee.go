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

package dataset

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
)

// deleteDatasetChunkRuntimeTxEE drops the content-history rows a deleted
// dataset leaves behind, in the deletion transaction.
//
// A dataset delete removes its documents directly instead of going through the
// per-document removal, so none of the usual collectors ever see them:
//
//   - chunk_version holds the chunk text and image ids of every edit; nothing
//     else references it once the dataset is gone, and the removed-document
//     cleanup only walks removal rows, which a dataset delete never writes;
//   - dataset_removed_document would still name the removed documents, and its
//     rows are the only thing that lets a restore put them back into a dataset
//     that no longer exists;
//   - document_version holds the per-document settings and metadata states the
//     versions produced for documents that no longer exist, and a restore of
//     that dataset can never read them again.
func deleteDatasetChunkRuntimeTxEE(ctx context.Context, tx *gorm.DB, datasetID string) error {
	if tx == nil || datasetID == "" {
		return nil
	}
	if err := dao.NewChunkVersionDAO().DeleteByDataset(ctx, tx, datasetID); err != nil {
		return fmt.Errorf("delete the chunk history of dataset %s: %w", datasetID, err)
	}
	if err := dao.NewDatasetRemovedDocumentDAO().DeleteByDataset(ctx, tx, datasetID); err != nil {
		return fmt.Errorf("delete the removed documents of dataset %s: %w", datasetID, err)
	}
	// The states name documents of a dataset that no longer exists; nothing can
	// restore them, and a later dataset reusing the id must not inherit them.
	if err := dao.NewDocumentVersionDAO().DeleteByDataset(ctx, tx, datasetID); err != nil {
		return fmt.Errorf("delete the document versions of dataset %s: %w", datasetID, err)
	}
	return nil
}

// deleteDatasetMetadataStatesEE drops the metadata values a deleted dataset kept
// in the document engine.
//
// It runs after the deletion transaction rather than in it, because the engine
// is not part of that transaction: by then the rows that named the states are
// gone, so a failure here leaves states no surviving row reads - litter instead
// of the data loss a half-dropped history would be.
func deleteDatasetMetadataStatesEE(ctx context.Context, datasetID string) {
	if datasetID == "" {
		return
	}
	if err := history.DeleteMetadataDataset(ctx, datasetID); err != nil &&
		!errors.Is(err, history.ErrMetadataUnavailable) {
		common.Warn("Delete dataset: failed to remove the metadata history states",
			zap.String("dataset_id", datasetID), zap.Error(err))
	}
}
