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

package dao

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ReclaimedSnapshots is what one sweep of a dataset reclaimed, per table.
//
// The counts are reported together because a sweep is one decision about one
// dataset, not two independent cleanups: whoever asks "did anything become
// garbage when this version went away" wants the answer in a single line of log.
type ReclaimedSnapshots struct {
	// Configs is the number of configuration snapshots removed.
	Configs int64
	// DocumentSets is the number of document set snapshots removed.
	DocumentSets int64
}

// Total is the number of rows reclaimed by the sweep.
func (r ReclaimedSnapshots) Total() int64 {
	return r.Configs + r.DocumentSets
}

// SweepDatasetSnapshots reclaims every content-addressed row of a dataset that no
// surviving version names.
//
// It is the dataset-level garbage collection the history needs, and the only
// entry to reclamation: a version row carries nothing but two hashes, so deleting
// one is the only way a snapshot can become unreachable, and the answer to "which
// snapshots are garbage now" is a property of the dataset's surviving versions
// rather than of the row that went away. One sweep therefore reclaims everything
// a delete orphaned - including rows orphaned by an earlier delete whose sweep
// failed - and running it twice reclaims nothing the second time.
//
// It runs after the deleting transaction commits, never inside it. The set of
// rows it considers is a snapshot of committed state, and a writer commits its
// version row and its snapshot in the same transaction, so the pair is never seen
// apart; inside the caller's transaction a driver error would instead poison a
// delete that has nothing to do with the reclamation.
//
// Both tables are swept even when the first one fails: a failure to reclaim
// configuration snapshots says nothing about document sets, and leaving one of
// them unattempted because of the other would keep garbage alive for no reason.
// The caller gets whatever was reclaimed plus the joined error.
func SweepDatasetSnapshots(ctx context.Context, db *gorm.DB, datasetID string) (ReclaimedSnapshots, error) {
	var reclaimed ReclaimedSnapshots
	if db == nil || datasetID == "" {
		return reclaimed, nil
	}
	configs, configErr := NewDatasetConfigSnapshotDAO().deleteUnreferenced(ctx, db, datasetID)
	reclaimed.Configs = configs
	sets, setErr := NewDatasetDocumentSetSnapshotDAO().deleteUnreferenced(ctx, db, datasetID)
	reclaimed.DocumentSets = sets
	return reclaimed, errors.Join(configErr, setErr)
}
