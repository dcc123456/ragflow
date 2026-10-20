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

package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	roleActionEnable int32 = 1 << iota
	roleActionRead
	roleActionWrite
	roleActionShare
)

var roleResourceTypes = map[string]int64{
	"dataset":        1,
	"chat":           2,
	"agent":          3,
	"search":         4,
	"file":           5,
	"team":           6,
	"memory":         7,
	"model_provider": 8,
}

var roleResourceNamesByType = map[int64]string{
	1: "dataset",
	2: "chat",
	3: "agent",
	4: "search",
	5: "file",
	6: "team",
	7: "memory",
	8: "model_provider",
}

var roleResourceNames = []string{
	"dataset",
	"chat",
	"agent",
	"search",
	"file",
	"team",
	"memory",
	"model_provider",
}

var roleActionMasks = map[string]int32{
	"enable": roleActionEnable,
	"read":   roleActionRead,
	"write":  roleActionWrite,
	"share":  roleActionShare,
}

var roleDefaultModelTypes = map[string]entity.ModelType{
	"llm":       entity.ModelTypeChat | entity.ModelTypeImage2Text,
	"embedding": entity.ModelTypeEmbedding,
	"vlm":       entity.ModelTypeImage2Text,
	"asr":       entity.ModelTypeSpeech2Text,
	"rerank":    entity.ModelTypeRerank,
	"tts":       entity.ModelTypeTTS,
}

// ListRoles returns only system-level roles. Tenant roles are managed in the
// Tenant context and are deliberately excluded from the system admin API.
func (s *Service) ListRoles(ctx context.Context) ([]map[string]interface{}, error) {
	var roles []entity.Role
	if err := dao.DB.WithContext(ctx).Where("tenant_id IS NULL").Order("id ASC").Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("failed to list system roles: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(roles))
	for _, role := range roles {
		result = append(result, roleResponse(&role))
	}
	return result, nil
}

// CreateRole creates a system-level role.
func (s *Service) CreateRole(ctx context.Context, roleName, description string) (map[string]interface{}, error) {
	roleName = strings.TrimSpace(roleName)
	if roleName == "" || utf8.RuneCountInString(roleName) > 64 {
		return nil, common.NewCodedError(common.CodeBadRequest, "Role name must contain between 1 and 64 characters")
	}

	var role entity.Role
	if err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&entity.Role{}).
			Where("role_name = ? AND tenant_id IS NULL", roleName).
			Count(&count).Error; err != nil {
			return fmt.Errorf("failed to check system role name: %w", err)
		}
		if count > 0 {
			return common.NewCodedError(common.CodeConflict, "A system role with this name already exists")
		}

		descriptionCopy := description
		role = entity.Role{RoleName: roleName, Description: &descriptionCopy}
		if err := tx.Create(&role).Error; err != nil {
			return fmt.Errorf("failed to create system role: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return roleResponse(&role), nil
}

// ShowRole returns a system-level role by name.
func (s *Service) ShowRole(ctx context.Context, roleName string) (map[string]interface{}, error) {
	role, err := findSystemRole(dao.DB.WithContext(ctx), roleName, false)
	if err != nil {
		return nil, err
	}
	return roleResponse(role), nil
}

// UpdateRole updates a system role description. Role names remain stable so
// existing role assignments are not affected by a display-name change.
func (s *Service) UpdateRole(ctx context.Context, roleName, description string) (map[string]interface{}, error) {
	var role *entity.Role
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		role, err = findSystemRole(tx, roleName, true)
		if err != nil {
			return err
		}

		if err := tx.Model(&entity.Role{}).
			Where("id = ? AND tenant_id IS NULL", role.ID).
			Update("description", description).Error; err != nil {
			return fmt.Errorf("failed to update system role description: %w", err)
		}
		role, err = findSystemRole(tx, roleName, false)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return roleResponse(role), nil
}

// DropRole removes a system role that is not assigned to any Tenant.
func (s *Service) DropRole(ctx context.Context, roleName string) (map[string]interface{}, error) {
	var deletedRole *entity.Role
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		role, err := findSystemRole(tx, roleName, true)
		if err != nil {
			return err
		}

		var tenantCount int64
		if err := tx.Model(&entity.Tenant{}).Where("role_id = ?", role.ID).Count(&tenantCount).Error; err != nil {
			return fmt.Errorf("failed to check system role assignments: %w", err)
		}
		if tenantCount > 0 {
			return common.NewCodedError(common.CodeConflict, "Role is assigned to a Tenant and cannot be deleted")
		}

		if err := tx.Where("role_id = ?", role.ID).Delete(&entity.RoleResource{}).Error; err != nil {
			return fmt.Errorf("failed to delete system role permissions: %w", err)
		}
		if err := tx.Where("role_id = ?", role.ID).Delete(&entity.RoleDefaultModel{}).Error; err != nil {
			return fmt.Errorf("failed to delete system role default models: %w", err)
		}
		result := tx.Where("id = ? AND tenant_id IS NULL", role.ID).Delete(&entity.Role{})
		if result.Error != nil {
			return fmt.Errorf("failed to delete system role: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return common.NewCodedError(common.CodeNotFound, "System role not found")
		}
		deletedRole = role
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "role": roleResponse(deletedRole)}, nil
}

// ShowRolePermission returns a complete permission matrix for one system role.
func (s *Service) ShowRolePermission(ctx context.Context, roleName string) (map[string]interface{}, error) {
	role, err := findSystemRole(dao.DB.WithContext(ctx), roleName, false)
	if err != nil {
		return nil, err
	}

	var rows []entity.RoleResource
	if err := dao.DB.WithContext(ctx).Where("role_id = ?", role.ID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list system role permissions: %w", err)
	}
	permissions, err := permissionMatrix(rows)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"role":        roleResponse(role),
		"permissions": permissions,
	}, nil
}

// GrantRolePermission adds the requested permission bits for one resource.
func (s *Service) GrantRolePermission(ctx context.Context, roleName string, actions []string, resource string) (map[string]interface{}, error) {
	return s.changeRolePermission(ctx, roleName, actions, resource, true)
}

// RevokeRolePermission removes the requested permission bits for one resource.
func (s *Service) RevokeRolePermission(ctx context.Context, roleName string, actions []string, resource string) (map[string]interface{}, error) {
	return s.changeRolePermission(ctx, roleName, actions, resource, false)
}

func (s *Service) changeRolePermission(ctx context.Context, roleName string, actions []string, resource string, grant bool) (map[string]interface{}, error) {
	resource = strings.ToLower(strings.TrimSpace(resource))
	resourceType, ok := roleResourceTypes[resource]
	if !ok {
		return nil, common.NewCodedError(common.CodeBadRequest, "Unknown role resource")
	}
	mask, err := roleActionMask(resource, actions)
	if err != nil {
		return nil, err
	}

	var updatedMask int32
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		role, err := findSystemRole(tx, roleName, true)
		if err != nil {
			return err
		}

		var rows []entity.RoleResource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("role_id = ? AND resource_type = ?", role.ID, resourceType).
			Order("id ASC").Find(&rows).Error; err != nil {
			return fmt.Errorf("failed to read system role permissions: %w", err)
		}

		var current int32
		for _, row := range rows {
			current |= row.Action
		}
		if grant {
			updatedMask = current | mask
		} else {
			updatedMask = current &^ mask
		}
		if len(rows) == 1 && updatedMask == current {
			return nil
		}

		if len(rows) == 0 {
			if updatedMask == 0 {
				return nil
			}
			return tx.Create(&entity.RoleResource{
				RoleID:       role.ID,
				ResourceType: resourceType,
				Action:       updatedMask,
			}).Error
		}

		if updatedMask == 0 {
			return tx.Where("role_id = ? AND resource_type = ?", role.ID, resourceType).
				Delete(&entity.RoleResource{}).Error
		}

		primaryRow := rows[0]
		if err := tx.Model(&entity.RoleResource{}).Where("id = ?", primaryRow.ID).Update("action", updatedMask).Error; err != nil {
			return fmt.Errorf("failed to update system role permissions: %w", err)
		}
		if len(rows) > 1 {
			if err := tx.Where("role_id = ? AND resource_type = ? AND id <> ?", role.ID, resourceType, primaryRow.ID).
				Delete(&entity.RoleResource{}).Error; err != nil {
				return fmt.Errorf("failed to consolidate duplicate system role permissions: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success":    true,
		"role_name":  strings.TrimSpace(roleName),
		"resource":   resource,
		"permission": updatedMask,
	}, nil
}

// ListResources lists feature resource types supported by role permissions.
func (s *Service) ListResources(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{"resource_types": roleResourceNames}, nil
}

// ListRolesWithPermission returns system roles and their full permission matrix.
func (s *Service) ListRolesWithPermission(ctx context.Context) ([]map[string]interface{}, error) {
	db := dao.DB.WithContext(ctx)
	var roles []entity.Role
	if err := db.Where("tenant_id IS NULL").Order("id ASC").Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("failed to list system roles: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(roles))
	if len(roles) == 0 {
		return result, nil
	}

	roleIDs := make([]int32, 0, len(roles))
	for _, role := range roles {
		roleIDs = append(roleIDs, role.ID)
	}
	var rows []entity.RoleResource
	if err := db.Where("role_id IN ?", roleIDs).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list system role permissions: %w", err)
	}

	rowsByRole := make(map[int32][]entity.RoleResource, len(roles))
	for _, row := range rows {
		rowsByRole[row.RoleID] = append(rowsByRole[row.RoleID], row)
	}
	for _, role := range roles {
		permissions, err := permissionMatrix(rowsByRole[role.ID])
		if err != nil {
			return nil, err
		}
		roleMap := roleResponse(&role)
		roleMap["permissions"] = permissions
		result = append(result, roleMap)
	}
	return result, nil
}

func roleActionMask(resource string, actions []string) (int32, error) {
	if len(actions) == 0 {
		return 0, common.NewCodedError(common.CodeBadRequest, "At least one permission action is required")
	}

	var mask int32
	for _, action := range actions {
		action = strings.ToLower(strings.TrimSpace(action))
		bit, ok := roleActionMasks[action]
		if !ok || (resource == "model_provider" && action != "enable") {
			return 0, common.NewCodedError(common.CodeBadRequest, fmt.Sprintf("Action %q is not supported for resource %q", action, resource))
		}
		mask |= bit
	}
	return mask, nil
}

func permissionMatrix(rows []entity.RoleResource) (map[string]map[string]bool, error) {
	permissions := make(map[string]map[string]bool, len(roleResourceNames))
	for _, resource := range roleResourceNames {
		permissions[resource] = map[string]bool{
			"enable": false,
			"read":   false,
			"write":  false,
			"share":  false,
		}
	}

	for _, row := range rows {
		resource, ok := roleResourceNamesByType[row.ResourceType]
		if !ok {
			return nil, fmt.Errorf("unknown role resource type %d for role %d", row.ResourceType, row.RoleID)
		}
		actions := permissions[resource]
		actions["enable"] = actions["enable"] || row.Action&roleActionEnable != 0
		actions["read"] = actions["read"] || row.Action&roleActionRead != 0
		actions["write"] = actions["write"] || row.Action&roleActionWrite != 0
		actions["share"] = actions["share"] || row.Action&roleActionShare != 0
	}
	return permissions, nil
}

func findSystemRole(db *gorm.DB, roleName string, lock bool) (*entity.Role, error) {
	roleName = strings.TrimSpace(roleName)
	if roleName == "" {
		return nil, common.NewCodedError(common.CodeBadRequest, "Role name is required")
	}

	query := db.Model(&entity.Role{}).Where("role_name = ? AND tenant_id IS NULL", roleName).Order("id ASC")
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var roles []entity.Role
	if err := query.Limit(2).Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("failed to find system role: %w", err)
	}
	if len(roles) == 0 {
		return nil, common.NewCodedError(common.CodeNotFound, "System role not found")
	}
	if len(roles) > 1 {
		return nil, common.NewCodedError(common.CodeConflict, "Multiple system roles have this name")
	}
	return &roles[0], nil
}

func roleResponse(role *entity.Role) map[string]interface{} {
	return map[string]interface{}{
		"id":          role.ID,
		"role_name":   role.RoleName,
		"description": role.Description,
		"create_date": role.CreateDate,
		"update_date": role.UpdateDate,
	}
}

// ShowRoleDefaultModels returns the default models configured on a system role.
func (s *Service) ShowRoleDefaultModels(ctx context.Context, roleName string) (map[string]interface{}, error) {
	db := dao.DB.WithContext(ctx)
	role, err := findSystemRole(db, roleName, false)
	if err != nil {
		return nil, err
	}

	var rows []entity.RoleDefaultModel
	if err := db.Where("role_id = ?", role.ID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list role default models: %w", err)
	}

	items := make([]map[string]interface{}, 0, len(rows))
	configured := make(map[string]struct{}, len(roleDefaultModelTypes))
	for _, row := range rows {
		if _, ok := roleDefaultModelTypes[row.ModelType]; !ok {
			continue
		}
		if _, duplicate := configured[row.ModelType]; duplicate {
			continue
		}
		configured[row.ModelType] = struct{}{}
		items = append(items, roleDefaultModelResponse(&row))
	}

	status := "partial"
	switch len(configured) {
	case 0:
		status = "not_set"
	case len(roleDefaultModelTypes):
		status = "complete"
	}
	return map[string]interface{}{
		"model_list":   items,
		"setup_status": status,
	}, nil
}

// SetRoleDefaultModel sets one model type on a system role. An empty model ID
// clears that type, matching the optional model controls in the admin UI.
func (s *Service) SetRoleDefaultModel(ctx context.Context, roleName, modelID, modelType string) (bool, error) {
	modelType = strings.TrimSpace(modelType)
	modelTypeBit, ok := roleDefaultModelTypes[modelType]
	if !ok {
		return false, common.NewCodedError(common.CodeBadRequest, "Unsupported default model type")
	}

	db := dao.DB.WithContext(ctx)
	err := db.Transaction(func(tx *gorm.DB) error {
		role, err := findSystemRole(tx, roleName, true)
		if err != nil {
			return err
		}

		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			if err := tx.Where("role_id = ? AND model_type = ?", role.ID, modelType).
				Delete(&entity.RoleDefaultModel{}).Error; err != nil {
				return fmt.Errorf("failed to clear role default model: %w", err)
			}
			return nil
		}

		var model entity.TenantModel
		if err := tx.Where("id = ?", modelID).Take(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NewCodedError(common.CodeNotFound, "Model not found")
			}
			return fmt.Errorf("failed to find default model: %w", err)
		}
		if model.Status != "active" {
			return common.NewCodedError(common.CodeBadRequest, "Model is not active")
		}
		if !entity.ModelType(model.ModelType).Has(modelTypeBit) {
			return common.NewCodedError(common.CodeBadRequest, "Model does not support the requested type")
		}

		var instance entity.TenantModelInstance
		if err := tx.Where("id = ? AND provider_id = ?", model.InstanceID, model.ProviderID).Take(&instance).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NewCodedError(common.CodeBadRequest, "Model instance not found")
			}
			return fmt.Errorf("failed to find model instance: %w", err)
		}
		if instance.Status != "active" {
			return common.NewCodedError(common.CodeBadRequest, "Model instance is not active")
		}

		var provider entity.TenantModelProvider
		if err := tx.Where("id = ?", model.ProviderID).Take(&provider).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NewCodedError(common.CodeBadRequest, "Model provider not found")
			}
			return fmt.Errorf("failed to find model provider: %w", err)
		}
		if provider.UserID == "" {
			return common.NewCodedError(common.CodeConflict, "Model provider has no owner")
		}

		var existing []entity.RoleDefaultModel
		if err := tx.Where("role_id = ? AND model_type = ?", role.ID, modelType).
			Order("id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&existing).Error; err != nil {
			return fmt.Errorf("failed to find existing role default model: %w", err)
		}
		if len(existing) == 0 {
			return tx.Create(&entity.RoleDefaultModel{
				RoleID: role.ID, ModelType: modelType, ModelID: model.ID, TenantID: provider.UserID,
			}).Error
		}

		if err := tx.Model(&entity.RoleDefaultModel{}).Where("id = ?", existing[0].ID).
			Updates(map[string]interface{}{"model_id": model.ID, "tenant_id": provider.UserID}).Error; err != nil {
			return fmt.Errorf("failed to update role default model: %w", err)
		}
		if len(existing) > 1 {
			duplicateIDs := make([]int32, 0, len(existing)-1)
			for _, row := range existing[1:] {
				duplicateIDs = append(duplicateIDs, row.ID)
			}
			if err := tx.Where("id IN ?", duplicateIDs).Delete(&entity.RoleDefaultModel{}).Error; err != nil {
				return fmt.Errorf("failed to remove duplicate role default models: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// ResetRoleDefaultModel clears one model type from a system role.
func (s *Service) ResetRoleDefaultModel(ctx context.Context, roleName, modelType string) (bool, error) {
	modelType = strings.TrimSpace(modelType)
	if _, ok := roleDefaultModelTypes[modelType]; !ok {
		return false, common.NewCodedError(common.CodeBadRequest, "Unsupported default model type")
	}

	return true, dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		role, err := findSystemRole(tx, roleName, true)
		if err != nil {
			return err
		}
		if err := tx.Where("role_id = ? AND model_type = ?", role.ID, modelType).
			Delete(&entity.RoleDefaultModel{}).Error; err != nil {
			return fmt.Errorf("failed to reset role default model: %w", err)
		}
		return nil
	})
}

func roleDefaultModelResponse(row *entity.RoleDefaultModel) map[string]interface{} {
	return map[string]interface{}{
		"role_id":    row.RoleID,
		"model_type": row.ModelType,
		"model_id":   row.ModelID,
		// This column retains its historical name in the shared schema; it
		// stores the user who owns the model provider in the Go model schema.
		"tenant_id": row.TenantID,
	}
}
