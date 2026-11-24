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

package admin

import (
	"context"
	"encoding/json"
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

var roleResourceTypes = map[string]int64{
	"dataset":        entity.RoleResourceTypeDataset,
	"chat":           entity.RoleResourceTypeChat,
	"agent":          entity.RoleResourceTypeAgent,
	"search":         entity.RoleResourceTypeSearch,
	"file":           entity.RoleResourceTypeFile,
	"group":          entity.RoleResourceTypeGroup,
	"memory":         entity.RoleResourceTypeMemory,
	"model_provider": entity.RoleResourceTypeModelProvider,
	"model_group":    entity.RoleResourceTypeModelGroup,
	"data_source":    entity.RoleResourceTypeDataSource,
	"chat_channel":   entity.RoleResourceTypeChatChannel,
	"mcp":            entity.RoleResourceTypeMCP,
	"invitation":     entity.RoleResourceTypeInvitation,
}

var roleResourceNamesByType = map[int64]string{
	entity.RoleResourceTypeDataset:       "dataset",
	entity.RoleResourceTypeChat:          "chat",
	entity.RoleResourceTypeAgent:         "agent",
	entity.RoleResourceTypeSearch:        "search",
	entity.RoleResourceTypeFile:          "file",
	entity.RoleResourceTypeGroup:         "group",
	entity.RoleResourceTypeMemory:        "memory",
	entity.RoleResourceTypeModelProvider: "model_provider",
	entity.RoleResourceTypeModelGroup:    "model_group",
	entity.RoleResourceTypeDataSource:    "data_source",
	entity.RoleResourceTypeChatChannel:   "chat_channel",
	entity.RoleResourceTypeMCP:           "mcp",
	entity.RoleResourceTypeInvitation:    "invitation",
}

var roleResourceNames = []string{
	"dataset",
	"chat",
	"agent",
	"search",
	"file",
	"group",
	"memory",
	"model_provider",
	"model_group",
	"data_source",
	"chat_channel",
	"mcp",
	"invitation",
}

var roleActionMasks = map[string]int32{
	"enable": entity.RoleActionEnable,
	"read":   entity.RoleActionRead,
	"write":  entity.RoleActionWrite,
	"share":  entity.RoleActionShare,
}

var roleDefaultModelTypes = map[string]entity.ModelType{
	"chat":      entity.ModelTypeChat,
	"embedding": entity.ModelTypeEmbedding,
	"asr":       entity.ModelTypeSpeech2Text,
	"vision":    entity.ModelTypeImage2Text,
	"rerank":    entity.ModelTypeRerank,
	"tts":       entity.ModelTypeTTS,
	"ocr":       entity.ModelTypeOCR,
	"decision":  entity.ModelTypeDecision,
	"scoring":   entity.ModelTypeScoring,
}

func canonicalRoleDefaultModelType(modelType string) string {
	switch modelType {
	case "llm":
		return "chat"
	case "vlm":
		return "vision"
	default:
		return modelType
	}
}

func roleDefaultModelStorageTypes(modelType string) []string {
	switch modelType {
	case "chat":
		return []string{"chat", "llm"}
	case "vision":
		return []string{"vision", "vlm"}
	default:
		return []string{modelType}
	}
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

// GetDefaultTenantSystemRole returns the system role assigned to new tenants.
func (s *Service) GetDefaultTenantSystemRole(ctx context.Context) (*entity.Role, error) {
	return dao.GetDefaultTenantSystemRole(ctx, dao.DB)
}

// SetDefaultTenantSystemRole changes the default system role assigned to new tenants.
func (s *Service) SetDefaultTenantSystemRole(ctx context.Context, roleID *int32) (*entity.Role, error) {
	if roleID == nil || *roleID <= 0 {
		return nil, common.NewCodedError(common.CodeBadRequest, "A default system role is required")
	}
	if err := dao.SetDefaultTenantSystemRole(ctx, dao.DB, roleID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewCodedError(common.CodeNotFound, "System role not found")
		}
		return nil, err
	}
	return dao.GetDefaultTenantSystemRole(ctx, dao.DB)
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

// DropRole removes a system role, moving Tenants that use it to the configured default role.
func (s *Service) DropRole(ctx context.Context, roleName string) (map[string]interface{}, error) {
	var deletedRole *entity.Role
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		role, err := findSystemRole(tx, roleName, true)
		if err != nil {
			return err
		}
		defaultRole, err := dao.GetDefaultTenantSystemRole(ctx, tx)
		if err != nil {
			return err
		}
		if defaultRole != nil && defaultRole.ID == role.ID {
			return common.NewCodedError(common.CodeConflict, "Role is the default for new tenants and cannot be deleted")
		}

		var memberRoleCount int64
		if err := tx.Model(&entity.Tenant{}).Where("default_role_id = ?", role.ID).Count(&memberRoleCount).Error; err != nil {
			return fmt.Errorf("failed to check Tenant member role assignments: %w", err)
		}
		if memberRoleCount > 0 {
			return common.NewCodedError(common.CodeConflict, "Role is assigned as a Tenant member role and cannot be deleted")
		}

		var tenantSystemRoleCount int64
		if err := tx.Model(&entity.Tenant{}).Where("role_id = ?", role.ID).Count(&tenantSystemRoleCount).Error; err != nil {
			return fmt.Errorf("failed to check Tenant system role assignments: %w", err)
		}
		if tenantSystemRoleCount > 0 {
			if defaultRole == nil {
				return common.NewCodedError(common.CodeConflict, "Default Tenant role is not configured; the assigned role cannot be deleted")
			}
			if err := tx.Model(&entity.Tenant{}).Where("role_id = ?", role.ID).
				Update("role_id", defaultRole.ID).Error; err != nil {
				return fmt.Errorf("failed to move Tenants to the default system role: %w", err)
			}
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
func (s *Service) GrantRolePermission(ctx context.Context, actorUserID, roleName string, actions []string, resource string) (map[string]interface{}, error) {
	return s.changeRolePermission(ctx, actorUserID, roleName, actions, resource, true)
}

// RevokeRolePermission removes the requested permission bits for one resource.
func (s *Service) RevokeRolePermission(ctx context.Context, actorUserID, roleName string, actions []string, resource string) (map[string]interface{}, error) {
	return s.changeRolePermission(ctx, actorUserID, roleName, actions, resource, false)
}

// SetRolePermissionMatrix replaces the complete system role permission matrix.
func (s *Service) SetRolePermissionMatrix(ctx context.Context, actorUserID, roleName string, requested map[string]map[string]bool, expandedItems []RolePermissionItem) (map[string]interface{}, error) {
	if strings.TrimSpace(actorUserID) == "" {
		return nil, common.NewCodedError(common.CodeUnauthorized, "Admin user is required")
	}
	if requested == nil {
		return nil, common.NewCodedError(common.CodeBadRequest, "permissions is required")
	}
	permissions := make(map[string]map[string]bool, len(requested))
	for key, actions := range requested {
		key = strings.TrimSpace(key)
		resource := strings.ToLower(key)
		if _, ok := roleResourceTypes[resource]; !ok {
			return nil, common.NewCodedError(common.CodeBadRequest, "Unknown role resource: "+resource)
		}
		clean := make(map[string]bool, len(actions))
		for action, enabled := range actions {
			action = strings.ToLower(strings.TrimSpace(action))
			_, supported := roleActionMasks[action]
			if !supported || ((resource == "model_provider" || resource == "invitation" || resource == "model_group" || resource == "data_source" || resource == "chat_channel") && action != "enable") {
				if !enabled {
					continue
				}
				return nil, common.NewCodedError(common.CodeBadRequest, fmt.Sprintf("Action %q is not supported for resource %q", action, resource))
			}
			clean[action] = enabled
		}
		permissions[resource] = clean
	}
	for _, expanded := range expandedItems {
		resource := strings.ToLower(strings.TrimSpace(expanded.ResourceType))
		item := strings.TrimSpace(expanded.Item)
		if (resource != "model_provider" && resource != "data_source" && resource != "chat_channel") || item == "" {
			return nil, common.NewCodedError(common.CodeBadRequest, "Invalid expanded role permission item")
		}
		if permissions[resource] == nil {
			permissions[resource] = map[string]bool{}
		}
		permissions[resource][item] = expanded.Enabled
	}
	for _, resource := range []string{"model_provider", "data_source", "chat_channel"} {
		for item, enabled := range permissions[resource] {
			if _, isAction := roleActionMasks[item]; !isAction && enabled {
				permissions[resource]["enable"] = true
			}
		}
	}
	for resource, actions := range permissions {
		if actions["write"] || actions["share"] {
			actions["enable"] = true
			actions["read"] = true
		} else if actions["read"] {
			actions["enable"] = true
		} else if enabled, specified := actions["enable"]; specified && !enabled {
			actions["read"] = false
			actions["write"] = false
			actions["share"] = false
		}
		permissions[resource] = actions
	}

	var role *entity.Role
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		role, err = findSystemRole(tx, roleName, true)
		if err != nil {
			return err
		}
		rows := make([]entity.RoleResource, 0, len(roleResourceNames))
		for _, resource := range roleResourceNames {
			actions := permissions[resource]
			var mask int32
			for action, enabled := range actions {
				if enabled {
					if bit, ok := roleActionMasks[action]; ok {
						mask |= bit
					}
				}
			}
			config := entity.JSONMap{}
			if resource == "model_provider" || resource == "data_source" || resource == "chat_channel" {
				items := make(map[string]interface{})
				for item, enabled := range actions {
					if _, isAction := roleActionMasks[item]; !isAction {
						items[item] = enabled
					}
				}
				if len(items) > 0 {
					config["items"] = items
				}
			}
			if mask != 0 || len(config) > 0 || resource == "model_provider" || resource == "model_group" || resource == "data_source" || resource == "chat_channel" {
				rows = append(rows, entity.RoleResource{RoleID: role.ID, ResourceType: roleResourceTypes[resource], Action: mask, Config: config})
			}
		}
		if err := tx.Where("role_id = ?", role.ID).Delete(&entity.RoleResource{}).Error; err != nil {
			return fmt.Errorf("delete system role permissions: %w", err)
		}
		if len(rows) > 0 {
			if err := tx.Create(&rows).Error; err != nil {
				return fmt.Errorf("save system role permissions: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.ShowRolePermission(ctx, role.RoleName)
}

type RolePermissionItem struct {
	ResourceType string `json:"resource_type"`
	Item         string `json:"item"`
	Enabled      bool   `json:"enabled"`
}

func (s *Service) changeRolePermission(ctx context.Context, actorUserID, roleName string, actions []string, resource string, grant bool) (map[string]interface{}, error) {
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return nil, common.NewCodedError(common.CodeUnauthorized, "Admin user is required")
	}
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
			if updatedMask&(entity.RoleActionWrite|entity.RoleActionShare) != 0 {
				updatedMask |= entity.RoleActionRead | entity.RoleActionEnable
			} else if updatedMask&entity.RoleActionRead != 0 {
				updatedMask |= entity.RoleActionEnable
			}
		} else {
			updatedMask = current &^ mask
			if mask&entity.RoleActionEnable != 0 {
				updatedMask = 0
			} else if updatedMask&entity.RoleActionRead == 0 {
				updatedMask &^= entity.RoleActionWrite | entity.RoleActionShare
			}
		}
		if len(rows) == 1 && updatedMask == current {
			return nil
		}

		if len(rows) == 0 {
			if updatedMask != 0 || resource == "model_provider" || resource == "model_group" || resource == "data_source" || resource == "chat_channel" {
				if err := tx.Create(&entity.RoleResource{
					RoleID:       role.ID,
					ResourceType: resourceType,
					Action:       updatedMask,
				}).Error; err != nil {
					return fmt.Errorf("failed to create system role permission: %w", err)
				}
			}
		} else if updatedMask == 0 {
			if err := tx.Where("role_id = ? AND resource_type = ?", role.ID, resourceType).
				Delete(&entity.RoleResource{}).Error; err != nil {
				return fmt.Errorf("failed to delete system role permissions: %w", err)
			}
		} else {
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
		}
		if updatedMask != current {
			if err := recordRolePermissionChange(tx, actorUserID, role, resource, grant, current, updatedMask); err != nil {
				return err
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

func recordRolePermissionChange(tx *gorm.DB, actorUserID string, role *entity.Role, resource string, grant bool, before, after int32) error {
	beforeData, err := json.Marshal(map[string]interface{}{
		"role_id":    role.ID,
		"role_name":  role.RoleName,
		"resource":   resource,
		"permission": before,
	})
	if err != nil {
		return fmt.Errorf("encode previous role permission: %w", err)
	}
	afterData, err := json.Marshal(map[string]interface{}{
		"role_id":    role.ID,
		"role_name":  role.RoleName,
		"resource":   resource,
		"permission": after,
	})
	if err != nil {
		return fmt.Errorf("encode updated role permission: %w", err)
	}
	actionType := "grant"
	eventType := "role.permission.granted"
	if !grant {
		actionType = "revoke"
		eventType = "role.permission.revoked"
	}
	actorID := actorUserID
	status := string(entity.StatusValid)
	roleID := fmt.Sprint(role.ID)
	log := &entity.PermissionChangeLog{
		ID:            common.GenerateUUID(),
		TargetType:    "role",
		TargetID:      roleID,
		ResourceType:  "role_permission",
		ResourceID:    roleID,
		OldPermission: before,
		NewPermission: after,
		ActionType:    actionType,
		EventType:     eventType,
		ActorUserID:   &actorID,
		ScopeType:     "system",
		BeforeData:    stringPointer(string(beforeData)),
		AfterData:     stringPointer(string(afterData)),
		Status:        &status,
	}
	if err := tx.Create(log).Error; err != nil {
		return fmt.Errorf("record system role permission change: %w", err)
	}
	return nil
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
	type tenantRoleCount struct {
		RoleID int32 `gorm:"column:role_id"`
		Count  int64 `gorm:"column:count"`
	}
	var tenantCounts []tenantRoleCount
	if err := db.Model(&entity.Tenant{}).
		Select("role_id, COUNT(*) AS count").
		Where("role_id IS NOT NULL").
		Group("role_id").
		Scan(&tenantCounts).Error; err != nil {
		return nil, fmt.Errorf("failed to count Tenants by system role: %w", err)
	}
	tenantCountByRole := make(map[int32]int64, len(tenantCounts))
	for _, count := range tenantCounts {
		tenantCountByRole[count.RoleID] = count.Count
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
		roleMap["tenant_count"] = tenantCountByRole[role.ID]
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
		if !ok || ((resource == "model_provider" || resource == "model_group" || resource == "data_source" || resource == "chat_channel" || resource == "invitation") && action != "enable") {
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
	seen := make(map[string]bool, len(roleResourceNames))
	for _, row := range rows {
		resource, ok := roleResourceNamesByType[row.ResourceType]
		if !ok {
			return nil, fmt.Errorf("unknown role resource type %d for role %d", row.ResourceType, row.RoleID)
		}
		actions := permissions[resource]
		seen[resource] = true
		actions["enable"] = actions["enable"] || row.Action&entity.RoleActionEnable != 0
		actions["read"] = actions["read"] || row.Action&entity.RoleActionRead != 0
		actions["write"] = actions["write"] || row.Action&entity.RoleActionWrite != 0
		actions["share"] = actions["share"] || row.Action&entity.RoleActionShare != 0
		if resource == "model_provider" || resource == "data_source" || resource == "chat_channel" {
			items, ok := row.Config["items"].(map[string]interface{})
			if ok {
				prefix := resource + "::"
				for item, rawEnabled := range items {
					enabled, ok := rawEnabled.(bool)
					if !ok {
						return nil, fmt.Errorf("invalid %s permission config item %q for role %d", resource, item, row.RoleID)
					}
					permissions[prefix+item] = map[string]bool{"enable": enabled}
				}
			}
		}
	}
	for _, resource := range []string{"model_provider", "model_group", "data_source", "chat_channel"} {
		if !seen[resource] {
			permissions[resource]["enable"] = true
		}
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
		modelType := canonicalRoleDefaultModelType(row.ModelType)
		if _, ok := roleDefaultModelTypes[modelType]; !ok {
			continue
		}
		if _, duplicate := configured[modelType]; duplicate {
			continue
		}
		configured[modelType] = struct{}{}
		row.ModelType = modelType
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
	modelType = canonicalRoleDefaultModelType(strings.TrimSpace(modelType))
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
			if err := tx.Where("role_id = ? AND model_type IN ?", role.ID, roleDefaultModelStorageTypes(modelType)).
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
		if provider.UserID != "" || provider.TenantID != "" {
			return common.NewCodedError(common.CodeConflict, "System role defaults require a system model provider")
		}

		var existing []entity.RoleDefaultModel
		if err := tx.Where("role_id = ? AND model_type IN ?", role.ID, roleDefaultModelStorageTypes(modelType)).
			Order("id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&existing).Error; err != nil {
			return fmt.Errorf("failed to find existing role default model: %w", err)
		}
		if len(existing) == 0 {
			return tx.Create(&entity.RoleDefaultModel{
				RoleID: role.ID, ModelType: modelType, ModelID: model.ID,
			}).Error
		}

		if err := tx.Model(&entity.RoleDefaultModel{}).Where("id = ?", existing[0].ID).
			Updates(map[string]interface{}{"model_type": modelType, "model_id": model.ID, "tenant_id": ""}).Error; err != nil {
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
	modelType = canonicalRoleDefaultModelType(strings.TrimSpace(modelType))
	if _, ok := roleDefaultModelTypes[modelType]; !ok {
		return false, common.NewCodedError(common.CodeBadRequest, "Unsupported default model type")
	}

	return true, dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		role, err := findSystemRole(tx, roleName, true)
		if err != nil {
			return err
		}
		if err := tx.Where("role_id = ? AND model_type IN ?", role.ID, roleDefaultModelStorageTypes(modelType)).
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
		// This legacy column is empty for system-level model providers.
		"tenant_id": row.TenantID,
	}
}
