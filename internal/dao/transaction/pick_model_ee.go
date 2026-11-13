//
// Copyright (c) 2026 InfiniFlow. All rights reserved.
//
// RAGFlow Enterprise Edition — Proprietary and Confidential.
//
// This file is part of the RAGFlow Enterprise Edition and is licensed,
// not sold, under the RAGFlow Enterprise Commercial License Agreement.
// Unauthorized copying, modification, distribution, or disclosure of this
// file, via any medium, is strictly prohibited.
//
// Portions of this file may incorporate code from RAGFlow Community Edition,
// which is licensed under the Apache License, Version 2.0.
//
// For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package transaction

import (
	"context"
	"errors"

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// PickModelFromGroup returns the tenant_model with the smallest position
// among the group's members whose membership row is active in this group,
// whose model is globally active, and whose type bitmask contains modelType.
// A zero modelType skips the type filter, matching the convention of
// ModelFactory.resolveConfigAuthorized. It returns (nil, nil) when the group
// has no such model. Group-local availability lives on the membership row's
// status; the failover loop flips it there, never touching tenant_model.status
// or other groups.
//
// It lives here because it composes reads across two tables that belong to
// different DAOs. It deliberately avoids a JOIN: step 1 resolves member
// order from the small tenant_model_group_map table (index prefix on
// group_id), step 2 probes tenant_model by primary key only, so the plan
// stays deterministic and independent of optimizer statistics on the large
// tenant_model table (STRAIGHT_JOIN is not portable to vastbase).
func PickModelFromGroup(ctx context.Context, db *gorm.DB, groupID string, modelType entity.ModelType) (*entity.TenantModel, error) {
	if len(groupID) == 0 {
		return nil, nil
	}
	// Step 1: the group's active members in position order. A group holds a
	// handful of rows, so this read is cheap and sorts a tiny set.
	members := make([]*entity.TenantModelGroupMap, 0)
	err := db.WithContext(ctx).
		Where("group_id = ? AND status = ?", groupID, entity.ModelGroupMapStatusActive).
		Order("position ASC, id ASC").
		Find(&members).Error
	if err != nil {
		return nil, err
	}
	// Step 2: probe tenant_model by primary key in position order until the
	// first globally active model of the requested type. Dangling map rows
	// (model deleted) are skipped. The type check runs in Go, not SQL, so the
	// bitmask filter stays dialect-independent.
	for _, member := range members {
		var model entity.TenantModel
		err := db.WithContext(ctx).
			Take(&model, "id = ? AND status = ?", member.ModelID, "active").Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if modelType != 0 && !entity.ModelType(model.ModelType).Has(modelType) {
			continue
		}
		return &model, nil
	}
	return nil, nil
}
