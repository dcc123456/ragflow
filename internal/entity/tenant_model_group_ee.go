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

package entity

type TenantModelGroup struct {
	ID        string `gorm:"column:id;primaryKey;size:32" json:"id"`
	UserID    string `gorm:"column:user_id;size:32;index:idx_tenant_model_group_user_id,priority:1;not null" json:"user_id"`
	GroupName string `gorm:"column:group_name;size:128;index:idx_tenant_model_group_group_name,priority:2;not null" json:"group_name"`
	Status    string `gorm:"column:status;size:32;default:'active'" json:"status"`
	Extra     string `gorm:"column:extra;size:512;default:'{}'" json:"extra"`
	BaseModel
}

// TableName specify table name
func (TenantModelGroup) TableName() string {
	return "tenant_model_group"
}

// TenantModelGroupMap tenant model group map table
type TenantModelGroupMap struct {
	ID       string `gorm:"column:id;primaryKey;size:32" json:"id"`
	GroupID  string `gorm:"column:group_id;size:32;index:idx_tenant_model_group_map_id,unique,priority:1;not null" json:"group_id"`
	ModelID  string `gorm:"column:model_id;size:32;index:idx_tenant_model_group_map_id,unique,priority:2;not null" json:"model_id"`
	Position int64  `gorm:"column:position;not null" json:"position"`
	Status   string `gorm:"column:status;size:32;default:'active'" json:"status"`
	Extra    string `gorm:"column:extra;size:512;default:'{}'" json:"extra"`
	BaseModel
}

// TableName specify table name
func (TenantModelGroupMap) TableName() string {
	return "tenant_model_group_map"
}
