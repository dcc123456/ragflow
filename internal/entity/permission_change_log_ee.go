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

// PermissionChangeLog records permission and authorization-scope changes.
// The original columns remain because existing installations may contain
// records written by the Python service.
type PermissionChangeLog struct {
	ID            string  `gorm:"column:id;primaryKey;size:32" json:"id"`
	TenantID      string  `gorm:"column:tenant_id;size:32;not null;index" json:"tenant_id"`
	OperatorID    string  `gorm:"column:operator_id;size:32;not null;index" json:"operator_id"`
	TargetType    string  `gorm:"column:target_type;size:20;not null" json:"target_type"`
	TargetID      string  `gorm:"column:target_id;size:32;not null;index" json:"target_id"`
	ResourceType  string  `gorm:"column:resource_type;size:32;not null" json:"resource_type"`
	ResourceID    string  `gorm:"column:resource_id;size:32;not null;index" json:"resource_id"`
	OldPermission int32   `gorm:"column:old_permission;not null;default:0" json:"old_permission"`
	NewPermission int32   `gorm:"column:new_permission;not null;default:0" json:"new_permission"`
	ActionType    string  `gorm:"column:action_type;size:20;not null" json:"action_type"`
	Reason        *string `gorm:"column:reason;type:text" json:"reason,omitempty"`
	EventType     string  `gorm:"column:event_type;size:64;not null;default:'';index" json:"event_type"`
	ActorUserID   *string `gorm:"column:actor_user_id;size:32;index" json:"actor_user_id,omitempty"`
	ScopeType     string  `gorm:"column:scope_type;size:16;not null;default:'tenant';index" json:"scope_type"`
	BeforeData    *string `gorm:"column:before_data;type:longtext" json:"before_data,omitempty"`
	AfterData     *string `gorm:"column:after_data;type:longtext" json:"after_data,omitempty"`
	Status        *string `gorm:"column:status;size:1;index;default:'1'" json:"status,omitempty"`
	BaseModel
}

func (PermissionChangeLog) TableName() string { return "permission_change_log" }
