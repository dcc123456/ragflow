//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package dao

import (
	"context"
	"fmt"

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// permissionDataModelsEE returns the enterprise authorization entities.
func permissionDataModelsEE() []interface{} {
	return []interface{}{
		&entity.Role{},
		&entity.RoleResource{},
		&entity.RoleDefaultModel{},
		&entity.GroupInfo{},
		&entity.GroupMember{},
		&entity.Permission{},
		&entity.PermissionChangeLog{},
		&entity.TenantModelGroup{},
		&entity.TenantModelGroupMap{},
	}
}

// permissionRuntimeModelsEE ensures role permission configuration columns are
// present before the role-management APIs start handling writes.
func permissionRuntimeModelsEE() []interface{} {
	return []interface{}{&entity.RoleResource{}}
}

// chunkVersionDataModelsEE returns the chunk history table. Only the edition
// that keeps a per-chunk edit history creates it.
func chunkVersionDataModelsEE() []interface{} {
	return chunkVersionRuntimeModelsEE()
}

// chunkVersionRuntimeModelsEE returns the same table for the server-mode
// migration path. It is written at runtime — every chunk edit appends a version
// row — while the data-model migration only runs as the standalone --migrate
// action. A server process started against a database that never ran the new
// --migrate would otherwise lose the history silently: version writes are
// best-effort and only log a warning.
func chunkVersionRuntimeModelsEE() []interface{} {
	return []interface{}{
		&entity.ChunkVersion{},
	}
}

// datasetVersionDataModelsEE returns the dataset history table. It is created
// by the same two paths as the chunk history: the data-model migration and the
// server-mode runtime migration. The latter matters because every dataset
// configuration change appends a row, and version writes are best effort — a
// server started against a database that never ran the new --migrate would lose
// the history silently instead of failing the configuration change.
func datasetVersionDataModelsEE() []interface{} {
	return datasetVersionRuntimeModelsEE()
}

// datasetVersionRuntimeModelsEE returns the same tables for the server-mode
// migration path. The removed-document table is created alongside the history
// because the two are written together: a version says which documents existed,
// and the removal row is what makes a removed one restorable.
func datasetVersionRuntimeModelsEE() []interface{} {
	return []interface{}{
		// The change set rides in the version row, so the history is one table.
		&entity.DatasetVersion{},
		// The configuration snapshots a version points at live beside the
		// history rather than inside it: deleting a version drops a pointer, and
		// the snapshots the survivors read stay.
		&entity.DatasetConfigSnapshot{},
		// The document sets are stored the same way, by content: it is what made
		// deleting a version a plain delete instead of a copy of the set onto a
		// survivor.
		&entity.DatasetDocumentSetSnapshot{},
		&entity.DatasetRemovedDocument{},
		// The per-document states are what a restore reads to put a document's
		// parse settings and metadata values back. They are keyed by the version
		// that produced them, which is what keeps two edits in one millisecond
		// apart, and they are written best effort by the same paths that append a
		// version.
		&entity.DocumentVersion{},
	}
}

// requireDatasetVersionColumnEE fails a server-mode start when a column the
// versioning writes depend on is missing.
//
// Neither knowledgebase.version nor document.restored_version is one of the
// columns a server converges on its own: both are created by the full --migrate
// path. Every versioned change — a chunk edit, an upload, a dataset configuration
// change — updates the dataset's version column, and a content restore records the
// version it replayed in the document's. Without this check the server starts,
// answers reads, and then fails the first write with "Unknown column ... in 'field
// list'" — for the restore that is after the chunks are already back. Naming the
// cause at startup costs two information_schema lookups and saves the hunt.
func requireDatasetVersionColumnEE(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return nil
	}
	migrator := db.WithContext(ctx).Migrator()
	if !migrator.HasTable(&entity.Knowledgebase{}) {
		return fmt.Errorf("dataset versions need migration: the knowledgebase table is missing; run ragflow_server --migrate")
	}
	if !migrator.HasColumn(&entity.Knowledgebase{}, "version") {
		return fmt.Errorf("dataset versions need migration: knowledgebase.version is missing; run ragflow_server --migrate")
	}
	if !migrator.HasColumn(&entity.Document{}, "restored_version") {
		return fmt.Errorf("dataset versions need migration: document.restored_version is missing; run ragflow_server --migrate")
	}
	return nil
}

func initializeSystemSettingsEE(ctx context.Context, db *gorm.DB) error {
	return ensureWhitelistSetting(ctx, db)
}
