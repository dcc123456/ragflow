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
	Role     string  `gorm:"column:role;size:32;not null;index" json:"role"`
	Status   *string `gorm:"column:status;size:1;index" json:"status,omitempty"`
	BaseModel
}

func (GroupMember) TableName() string { return "group_member" }
