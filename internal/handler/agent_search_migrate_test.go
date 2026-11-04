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

package handler

import (
	"testing"

	"ragflow/internal/entity"
)

// TestHandlerAgentTestDBMigratesSearch guards that the shared agent handler
// test DB helper migrates the Search table. Without it, handler tests that
// exercise the search permission path depend on the integration MySQL schema
// (which currently omits the table) instead of the hermetic in-memory DB.
func TestHandlerAgentTestDBMigratesSearch(t *testing.T) {
	db := setupHandlerAgentsTestDB(t)

	status := "1"
	if err := db.Create(&entity.Search{
		ID:           "s1",
		TenantID:     "t1",
		Name:         "n1",
		CreatedBy:    "u1",
		SearchConfig: entity.JSONMap{},
		Status:       &status,
	}).Error; err != nil {
		t.Fatalf("create Search on helper DB failed (table missing?): %v", err)
	}
	var got entity.Search
	if err := db.First(&got, "id = ?", "s1").Error; err != nil {
		t.Fatalf("read Search on helper DB failed: %v", err)
	}
}
