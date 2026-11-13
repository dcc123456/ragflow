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

// GetByGroupName resolves a group by its tenant-scoped name, the same key
// Create enforces uniqueness on. A group name therefore uniquely identifies a
// group inside a tenant; user_id is ownership metadata, not part of the key.
func (dao *TenantModelGroupDAO) GetByGroupName(ctx context.Context, db *gorm.DB, tenantID, groupName string) (*entity.TenantModelGroup, error) {
	if len(tenantID) == 0 || len(groupName) == 0 {
		return nil, nil
	}
	var group entity.TenantModelGroup
	tx := db.WithContext(ctx).Where("tenant_id = ? AND group_name = ?", tenantID, groupName).Find(&group)
	if tx.Error != nil {
		return nil, tx.Error
	}
	if tx.RowsAffected == 0 {
		return nil, nil
	}
	return &group, nil
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
	err := db.WithContext(ctx).Where("group_id = ?", groupID).Order("position").Find(&instances).Error
	if err != nil {
		return nil, err
	}
	return instances, nil
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
