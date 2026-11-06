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
	GroupMemberStatusRemoved  = "0"
	GroupMemberStatusJoined   = "1"
	GroupMemberStatusInvited  = "2"
	GroupMemberStatusRejected = "3"
)

type GroupInfo struct {
	ID            string  `gorm:"column:id;primaryKey;size:32" json:"id"`
	Name          *string `gorm:"column:name;size:100;index" json:"name,omitempty"`
	Description   *string `gorm:"column:description;type:text" json:"description,omitempty"`
	Avatar        *string `gorm:"column:avatar;type:text" json:"avatar,omitempty"`
	OwnerID       string  `gorm:"column:owner_id;size:32;not null;index" json:"owner_id"`
	TenantID      string  `gorm:"column:tenant_id;size:32;not null;index" json:"tenant_id"`
	ParentID      *string `gorm:"column:parent_id;size:32;index" json:"parent_id,omitempty"`
	Path          *string `gorm:"column:path;size:255" json:"path,omitempty"`
	FormattedPath *string `gorm:"column:formatted_path;size:255" json:"formatted_path,omitempty"`
	Status        *string `gorm:"column:status;size:1;index" json:"status,omitempty"`
	BaseModel
}

func (GroupInfo) TableName() string { return "group_info" }

type GroupMember struct {
	ID       string  `gorm:"column:id;primaryKey;size:32" json:"id"`
	MemberID string  `gorm:"column:member_id;size:32;not null;index" json:"member_id"`
	GroupID  string  `gorm:"column:group_id;size:32;not null;index" json:"group_id"`
	Status   *string `gorm:"column:status;size:1;index" json:"status,omitempty"`
	BaseModel
}

func (GroupMember) TableName() string { return "group_member" }
