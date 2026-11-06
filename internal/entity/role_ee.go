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

const (
	RoleResourceTypeDataset int64 = iota + 1
	RoleResourceTypeChat
	RoleResourceTypeAgent
	RoleResourceTypeSearch
	RoleResourceTypeFile
	RoleResourceTypeGroup
	RoleResourceTypeMemory
	RoleResourceTypeModelProvider
	RoleResourceTypeMCP
)

const (
	RoleActionEnable int32 = 1 << iota
	RoleActionRead
	RoleActionWrite
	RoleActionShare
	RoleActionAll = RoleActionEnable | RoleActionRead | RoleActionWrite | RoleActionShare
)

type Role struct {
	ID          int32   `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RoleName    string  `gorm:"column:role_name;size:64;not null;index" json:"role_name"`
	Description *string `gorm:"column:description;type:text" json:"description,omitempty"`
	TenantID    *string `gorm:"column:tenant_id;size:32" json:"tenant_id,omitempty"`
	BaseModel
}

func (Role) TableName() string { return "role" }

type RoleResource struct {
	ID           int32 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RoleID       int32 `gorm:"column:role_id;not null;index" json:"role_id"`
	ResourceType int64 `gorm:"column:resource_type;not null" json:"resource_type"`
	Action       int32 `gorm:"column:action;not null" json:"action"`
	BaseModel
}

func (RoleResource) TableName() string { return "role_resource" }

// RoleDefaultModel stores default model selections for a role.
type RoleDefaultModel struct {
	ID        int32  `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RoleID    int32  `gorm:"column:role_id;not null;index" json:"role_id"`
	ModelType string `gorm:"column:model_type;size:128;not null;index" json:"model_type"`
	ModelID   string `gorm:"column:model_id;size:128;not null;index" json:"model_id"`
	TenantID  string `gorm:"column:tenant_id;size:32;not null;index" json:"tenant_id"`
	BaseModel
}

func (RoleDefaultModel) TableName() string { return "role_default_model" }
