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
	"errors"
	"fmt"
	"ragflow/internal/entity"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TenantModelGroupDAO tenant model group data access object
type TenantModelGroupDAO struct{}

// NewTenantModelGroupDAO create tenant model group DAO
func NewTenantModelGroupDAO() *TenantModelGroupDAO {
	return &TenantModelGroupDAO{}
}

// GetByID resolves one group by primary key. It returns (nil, nil) when no
// row matches.
func (dao *TenantModelGroupDAO) GetByID(ctx context.Context, db *gorm.DB, id string) (*entity.TenantModelGroup, error) {
	if len(id) == 0 {
		return nil, nil
	}
	var group entity.TenantModelGroup
	err := db.WithContext(ctx).Where("id = ?", id).Take(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// GetByIDAndTenantID resolves a group by ID within one tenant.
func (dao *TenantModelGroupDAO) GetByIDAndTenantID(ctx context.Context, db *gorm.DB, tenantID, id string) (*entity.TenantModelGroup, error) {
	if len(tenantID) == 0 || len(id) == 0 {
		return nil, nil
	}
	var group entity.TenantModelGroup
	err := db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).Take(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func (dao *TenantModelGroupDAO) Create(ctx context.Context, db *gorm.DB, group *entity.TenantModelGroup) error {
	db = db.WithContext(ctx)
	create := func(tx *gorm.DB) error {
		var existingGroup entity.TenantModelGroup
		err := tx.Where("tenant_id = ? AND group_name = ?", group.TenantID, group.GroupName).First(&existingGroup).Error
		if err == nil {
			return fmt.Errorf("group %s already exists", group.GroupName)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(group).Error
	}
	if _, inTransaction := db.Statement.ConnPool.(interface {
		Commit() error
		Rollback() error
	}); inTransaction {
		return create(db)
	}
	return db.Transaction(create)
}

// ListByTenantIDAndUserID returns the model groups owned by userID inside
// tenantID, newest first.
func (dao *TenantModelGroupDAO) ListByTenantIDAndUserID(ctx context.Context, db *gorm.DB, tenantID, userID string) ([]*entity.TenantModelGroup, error) {
	instances := make([]*entity.TenantModelGroup, 0)
	if len(tenantID) == 0 || len(userID) == 0 {
		return instances, nil
	}
	// Order newest first with id as tiebreaker so the list is stable across
	// calls: create_time is millisecond precision and can collide.
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ?", tenantID, userID).
		Order("create_time DESC, id DESC").
		Find(&instances).Error
	if err != nil {
		return nil, err
	}
	return instances, nil
}

func (dao *TenantModelGroupDAO) DeleteByIDs(ctx context.Context, db *gorm.DB, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := db.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&entity.TenantModelGroup{})
	return result.RowsAffected, result.Error
}

// TenantModelGroupMapDAO tenant model group map data access object
type TenantModelGroupMapDAO struct{}

// NewTenantModelGroupMapDAO create tenant model group map DAO
func NewTenantModelGroupMapDAO() *TenantModelGroupMapDAO {
	return &TenantModelGroupMapDAO{}
}

func (dao *TenantModelGroupMapDAO) AddModelToGroupTxn(ctx context.Context, db *gorm.DB, groupID string, m *entity.TenantModelGroupMap) error {
	txn := func(tx *gorm.DB) error {
		instances := make([]*entity.TenantModelGroupMap, 0)
		err := db.WithContext(ctx).Where("group_id = ?", groupID).Order("position").Find(&instances).Error
		if err != nil {
			return err
		}

		if len(instances) == 0 {
			m.Position = 0
		} else {
			var maxPosition int
			err = tx.Model(&entity.TenantModelGroupMap{}).
				Select("MAX(position)").
				Where("group_id = ?", groupID).
				Scan(&maxPosition).Error
			if err != nil {
				return err
			}
			m.Position = int64(maxPosition + 1)
		}

		return tx.Create(m).Error
	}
	return db.WithContext(ctx).Transaction(txn)
}

func (dao *TenantModelGroupMapDAO) DeleteByModelIDs(ctx context.Context, db *gorm.DB, groupID string, modelIds []string) (int64, error) {
	if len(modelIds) == 0 {
		return 0, nil
	}
	result := db.WithContext(ctx).Unscoped().Where("group_id = ? AND model_id IN ?", groupID, modelIds).Delete(&entity.TenantModelGroupMap{})
	return result.RowsAffected, result.Error
}

func (dao *TenantModelGroupMapDAO) DeleteByGroupID(ctx context.Context, db *gorm.DB, groupID string) (int64, error) {
	result := db.WithContext(ctx).Unscoped().Where("group_id = ?", groupID).Delete(&entity.TenantModelGroupMap{})
	return result.RowsAffected, result.Error
}

func (dao *TenantModelGroupMapDAO) ListByGroupID(ctx context.Context, db *gorm.DB, groupID string) ([]*entity.TenantModelGroupMap, error) {
	instances := make([]*entity.TenantModelGroupMap, 0)
	if len(groupID) == 0 {
		return instances, nil
	}
	err := db.WithContext(ctx).Where("group_id = ?", groupID).Order("position ASC, id ASC").Find(&instances).Error
	if err != nil {
		return nil, err
	}
	return instances, nil
}

func (dao *TenantModelGroupMapDAO) ReorderByModelIDs(ctx context.Context, db *gorm.DB, groupID string, modelIDs []string) error {
	if groupID == "" || len(modelIDs) == 0 {
		return errors.New("group ID and model IDs are required")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var members []*entity.TenantModelGroupMap
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "model_id", "position").
			Where("group_id = ?", groupID).
			Order("position ASC, id ASC").
			Find(&members).Error; err != nil {
			return err
		}
		if len(members) != len(modelIDs) {
			return errors.New("model IDs must include every group member exactly once")
		}
		membersByModelID := make(map[string]*entity.TenantModelGroupMap, len(members))
		for _, member := range members {
			membersByModelID[member.ModelID] = member
		}
		seen := make(map[string]struct{}, len(modelIDs))
		for position, modelID := range modelIDs {
			member := membersByModelID[modelID]
			if member == nil {
				return errors.New("model IDs must include every group member exactly once")
			}
			if _, exists := seen[modelID]; exists {
				return errors.New("model IDs must include every group member exactly once")
			}
			seen[modelID] = struct{}{}
			if member.Position == int64(position) {
				continue
			}
			if err := tx.Model(&entity.TenantModelGroupMap{}).
				Where("id = ? AND group_id = ?", member.ID, groupID).
				Update("position", position).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// SetModelStatus overwrites the group-local status of one membership row and
// reports whether the model is a member of the group at all. Only the status
// column changes: position ordering and tenant_model.status stay untouched, so
// a flip never reorders the group and never affects other groups.
func (dao *TenantModelGroupMapDAO) SetModelStatus(ctx context.Context, db *gorm.DB, groupID, modelID, status string) (bool, error) {
	if len(groupID) == 0 {
		return false, errors.New("group id is required")
	}
	if len(modelID) == 0 {
		return false, errors.New("model id is required")
	}
	var membership entity.TenantModelGroupMap
	err := db.WithContext(ctx).Select("id", "status").
		Where("group_id = ? AND model_id = ?", groupID, modelID).
		Take(&membership).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if membership.Status == status {
		return true, nil
	}
	err = db.WithContext(ctx).Model(&entity.TenantModelGroupMap{}).
		Where("group_id = ? AND model_id = ?", groupID, modelID).
		Update("status", status).Error
	return true, err
}
