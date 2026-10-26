//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

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
