//
// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

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
