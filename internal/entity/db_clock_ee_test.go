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

package entity

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// clockTolerance bounds how far the database clock may sit from the test
// process's clock. It is generous on purpose: the assertion is that the write
// reads a real clock, not that the two clocks agree to the millisecond.
const clockTolerance = 10 * time.Second

func setupClockDB(t *testing.T, models ...interface{}) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if len(models) > 0 {
		if err := db.AutoMigrate(models...); err != nil {
			t.Fatalf("auto-migrate sqlite: %v", err)
		}
	}
	return db
}

func TestDBTimeMillisReadsTheDatabaseClock(t *testing.T) {
	db := setupClockDB(t)

	got, err := DBTimeMillis(db)
	if err != nil {
		t.Fatalf("DBTimeMillis: %v", err)
	}
	if delta := time.Since(time.UnixMilli(got)); delta > clockTolerance || delta < -clockTolerance {
		t.Fatalf("database clock = %d, off by %v from this process", got, delta)
	}
}

func TestDBTimeMillisNeedsADatabase(t *testing.T) {
	if _, err := DBTimeMillis(nil); err == nil {
		t.Fatal("DBTimeMillis(nil) reported no error")
	}
}

// TestDatasetVersionStampsTheDatabaseClock covers the anchor side: the moment a
// version was appended has to come from the same clock as the chunk rows it will
// select, or a restore compares two clocks.
func TestDatasetVersionStampsTheDatabaseClock(t *testing.T) {
	db := setupClockDB(t, &DatasetVersion{})

	row := &DatasetVersion{DatasetID: "ds-1", Version: 0, Kind: DatasetVersionKindBaseline}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create the version: %v", err)
	}
	if row.CreateTime == nil {
		t.Fatal("create_time is nil, want the database clock")
	}
	if delta := time.Since(time.UnixMilli(*row.CreateTime)); delta > clockTolerance || delta < -clockTolerance {
		t.Fatalf("create_time = %d, off by %v from this process", *row.CreateTime, delta)
	}

	explicit := int64(1700000000123)
	kept := &DatasetVersion{DatasetID: "ds-1", Version: 1, Kind: DatasetVersionKindDatasetConfig, CreateTime: &explicit}
	if err := db.Create(kept).Error; err != nil {
		t.Fatalf("create the version: %v", err)
	}
	var reloaded DatasetVersion
	if err := db.Where("dataset_id = ? AND version = ?", "ds-1", 1).Take(&reloaded).Error; err != nil {
		t.Fatalf("load the version: %v", err)
	}
	if reloaded.CreateTime == nil || *reloaded.CreateTime != explicit {
		t.Fatalf("create_time = %v, want the explicit %d kept", reloaded.CreateTime, explicit)
	}
}

// TestChunkVersionStampsTheDatabaseClock covers the replay side: a row whose
// stamp came from another clock would be selected by the wrong anchor.
func TestChunkVersionStampsTheDatabaseClock(t *testing.T) {
	db := setupClockDB(t, &ChunkVersion{})

	row := &ChunkVersion{
		ChunkID: "chunk-1", Version: 0, DocID: "doc-1", DatasetID: "ds-1", DatasetVersion: 1,
		Kind: ChunkVersionKindAdd, Snapshot: JSONMap{"content_with_weight": "text"},
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create the chunk version: %v", err)
	}
	if row.CreateTime == nil {
		t.Fatal("create_time is nil, want the database clock")
	}
	if delta := time.Since(time.UnixMilli(*row.CreateTime)); delta > clockTolerance || delta < -clockTolerance {
		t.Fatalf("create_time = %d, off by %v from this process", *row.CreateTime, delta)
	}
}

// TestChunkVersionRequiresItsDatasetVersion pins the guard that keeps a history
// row on the timeline. A replay selects "dataset_version <= anchor", so a row
// written without a number - the zero value, which names the baseline - would be
// read by every anchor and could not be ordered against the row beside it.
func TestChunkVersionRequiresItsDatasetVersion(t *testing.T) {
	db := setupClockDB(t, &ChunkVersion{})

	row := &ChunkVersion{
		ChunkID: "chunk-1", Version: 0, DocID: "doc-1", DatasetID: "ds-1",
		Kind: ChunkVersionKindAdd, Snapshot: JSONMap{"content_with_weight": "text"},
	}
	if err := db.Create(row).Error; err == nil {
		t.Fatal("Create without a dataset version succeeded, want it refused")
	}

	row.DatasetVersion = 1
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("Create with a dataset version: %v", err)
	}
}
