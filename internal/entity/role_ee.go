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
