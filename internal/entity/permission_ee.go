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

// Permission stores a bitmask grant for a user or group on one tenant resource.
type Permission struct {
	ID           string  `gorm:"column:id;primaryKey;size:32" json:"id"`
	MemberID     *string `gorm:"column:member_id;size:32" json:"member_id,omitempty"`
	GroupID      *string `gorm:"column:group_id;size:32" json:"group_id,omitempty"`
	TenantID     string  `gorm:"column:tenant_id;size:32;not null" json:"tenant_id"`
	ResourceType string  `gorm:"column:resource_type;size:32;not null" json:"resource_type"`
	ResourceID   *string `gorm:"column:resource_id;size:255" json:"resource_id,omitempty"`
	Permission   int32   `gorm:"column:permission;not null;default:0" json:"permission"`
	Status       *string `gorm:"column:status;size:1" json:"status,omitempty"`
	BaseModel
}

func (Permission) TableName() string { return "permission" }

const (
	ResourcePermissionRead int32 = 1 << iota
	ResourcePermissionWrite
	ResourcePermissionManage
	ResourcePermissionOwner
)

const (
	GroupPermissionAddSubgroup int32 = 1 << iota
	GroupPermissionDeleteSubgroup
	GroupPermissionAddMember
	GroupPermissionRemoveMember
	GroupPermissionConfigureMemberPermissions
	GroupPermissionOwner
)

const (
	TenantPermissionManageMembers int32 = 1 << iota
	TenantPermissionManageRoles
	TenantPermissionOwner
)
