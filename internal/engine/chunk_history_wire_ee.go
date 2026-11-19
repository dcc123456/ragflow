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

package engine

import (
	"context"
	"fmt"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/elasticsearch"
	"ragflow/internal/engine/history"
	"ragflow/internal/engine/infinity"
	"ragflow/internal/entity"

	"go.uber.org/zap"
)

// The engines that can hold a version body must satisfy the store, and say so
// here: a renamed method would otherwise turn the history off silently at
// startup instead of failing this build.
//
// Chunk versioning supports Elasticsearch and Infinity. The other engines are
// not covered, so they install no store and record no chunk history.
var (
	_ history.ChunkStore = (*elasticsearch.Engine)(nil)
	_ history.ChunkStore = (*infinity.Engine)(nil)
)

// registerChunkHistory installs the document engine as the store of chunk
// version bodies: the history lives beside the chunks it describes, in whichever
// engine this deployment runs, and there is no second copy of it anywhere.
//
// An engine that offers no such store keeps no history - the edition refuses to
// write a version row whose body nobody holds - so the absence is reported at
// startup instead of being discovered by a failed restore.
func registerChunkHistory(docEngine DocEngine) {
	store, ok := docEngine.(history.ChunkStore)
	if !ok {
		common.Warn("Chunk history: the document engine provides no history store, version history is off",
			zap.String("type", GetEngineType()))
		return
	}
	history.SetChunkStore(store)
	// Both stores name their history after the tenant - Elasticsearch keys its
	// index by it, Infinity extends the tenant's chunk table with it - while every
	// row and every caller names the dataset. The lookup belongs to the metadata
	// database, which this package can reach and the history package cannot (the
	// DAO imports it), so it is installed here beside the store it serves.
	history.SetTenantResolver(datasetTenant)
}

// datasetTenant answers which tenant owns a dataset, for the history store that
// is addressed by tenant.
//
// The lookup reads the dataset's owner column alone: the history outlives a
// dataset's status (a delete path purges a history whose dataset is already
// invalid), so it must not be filtered by it.
func datasetTenant(ctx context.Context, datasetID string) (string, error) {
	if dao.DB == nil {
		return "", fmt.Errorf("chunk history: the metadata database is not initialized")
	}
	var tenantID string
	err := dao.DB.WithContext(ctx).
		Model(&entity.Knowledgebase{}).
		Where("id = ?", datasetID).
		Pluck("tenant_id", &tenantID).Error
	if err != nil {
		return "", fmt.Errorf("chunk history: failed to read the owner of dataset %s: %w", datasetID, err)
	}
	return tenantID, nil
}
