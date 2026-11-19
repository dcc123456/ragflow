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

package chunkversion

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/storage"
)

// PurgeDocumentHistoryEE drops the chunk history a removed document kept alive:
// the version rows, the pictures they hold and the embedding state. The document
// is gone and its removal row is being deleted, so nothing can reference them
// again.
//
// The rows are read and dropped through db, which is the caller's transaction:
// the cleanup holds the removal rows while it runs, and a failure has to roll
// them back so the request can be retried.
//
// A failed picture removal only logs: the rows are dropped regardless, and the
// leftover object is unreachable.
//
// It lives with the history table rather than with the chunk service because the
// caller is the dataset cleanup, and the chunk service reaches back into the
// document service: importing it from there would close a cycle between the two.
func PurgeDocumentHistoryEE(ctx context.Context, db *gorm.DB, datasetID, docID string) error {
	if db == nil || docID == "" {
		return nil
	}

	versionDAO := dao.NewChunkVersionDAO()
	imageIDs, err := versionDAO.ListImageIDs(ctx, db, docID, nil, true)
	if err != nil {
		return fmt.Errorf("list the chunk version images of document %s: %w", docID, err)
	}
	if storageImpl := storage.GetStorageFactory().GetStorage(); storageImpl != nil {
		for _, imageID := range imageIDs {
			objectKey := ImageObjectName(datasetID, imageID)
			if objectKey == "" {
				continue
			}
			if removeErr := storageImpl.Remove(ctx, datasetID, objectKey); removeErr != nil {
				common.Warn("Content purge: failed to remove a chunk version image",
					zap.String("image_id", imageID), zap.Error(removeErr))
			}
		}
	}

	// The states the rows name live in the document engine now, one document per
	// version. They go with the rows: the engine is not part of this transaction,
	// so a body removal that fails is returned rather than left for nobody to
	// find - the caller rolls back and retries, and the removal is idempotent.
	if err := versionDAO.DeleteByDocID(ctx, db, datasetID, docID); err != nil {
		return fmt.Errorf("purge the chunk history of document %s: %w", docID, err)
	}
	return nil
}
