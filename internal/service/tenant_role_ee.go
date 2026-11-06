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

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var tenantManagementPermissionBits = map[string]int32{
	"manage_members": entity.TenantPermissionManageMembers,
	"manage_roles":   entity.TenantPermissionManageRoles,
}

var tenantRoleResourceTypes = map[string]int64{
	"dataset":        entity.RoleResourceTypeDataset,
	"chat":           entity.RoleResourceTypeChat,
	"agent":          entity.RoleResourceTypeAgent,
	"search":         entity.RoleResourceTypeSearch,
	"file":           entity.RoleResourceTypeFile,
	"group":          entity.RoleResourceTypeGroup,
	"memory":         entity.RoleResourceTypeMemory,
	"model_provider": entity.RoleResourceTypeModelProvider,
	"mcp":            entity.RoleResourceTypeMCP,
}

var tenantRoleResourceNames = []string{
	"dataset",
	"chat",
	"agent",
	"search",
	"file",
	"group",
	"memory",
	"model_provider",
	"mcp",
}

var tenantRoleActionMasks = map[string]int32{
	"enable": entity.RoleActionEnable,
	"read":   entity.RoleActionRead,
	"write":  entity.RoleActionWrite,
	"share":  entity.RoleActionShare,
}

var tenantRoleResourceActions = map[string][]string{
	"dataset":        {"enable", "read", "write", "share"},
	"chat":           {"enable", "read", "write", "share"},
	"agent":          {"enable", "read", "write", "share"},
	"search":         {"enable", "read", "write", "share"},
	"file":           {"enable", "read", "write", "share"},
	"group":          {"enable", "read", "write", "share"},
	"memory":         {"enable", "read", "write", "share"},
	"model_provider": {"enable"},
	"mcp":            {"enable", "read", "write", "share"},
}

const (
	defaultTenantRolePage     = 1
	defaultTenantRolePageSize = 20
	maxTenantRolePageSize     = 200
)

// TenantRoleItem is a Tenant-scoped feature role returned by the API.
type TenantRoleItem struct {
	ID          int32      `json:"id"`
	RoleName    string     `json:"role_name"`
	Description *string    `json:"description,omitempty"`
	IsDefault   bool       `json:"is_default"`
	CreateDate  *time.Time `json:"create_date,omitempty"`
	UpdateDate  *time.Time `json:"update_date,omitempty"`
}

// TenantRoleListResult contains a page of roles belonging to one Tenant.
type TenantRoleListResult struct {
	Roles    []*TenantRoleItem `json:"roles"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

// TenantRolePermissionsResult describes one role's feature permission matrix.
type TenantRolePermissionsResult struct {
	Role        *TenantRoleItem            `json:"role"`
	Permissions map[string]map[string]bool `json:"permissions"`
}

// TenantDefaultRoleResult describes the role assigned to newly joined members.
type TenantDefaultRoleResult struct {
	RoleID *int32          `json:"role_id"`
	Role   *TenantRoleItem `json:"role,omitempty"`
}

// TenantManagementGrant is a management permission assigned to a Tenant member or group.
type TenantManagementGrant struct {
	SubjectType string   `json:"subject_type"`
	UserID      string   `json:"user_id,omitempty"`
	GroupID     string   `json:"group_id,omitempty"`
	GroupName   *string  `json:"group_name,omitempty"`
	Permission  int32    `json:"permission"`
	Permissions []string `json:"permissions"`
}

// TenantManagementGrantListResult contains active management grants in one Tenant.
type TenantManagementGrantListResult struct {
	Grants []*TenantManagementGrant `json:"grants"`
}

// TenantPermissionLogListResult contains the current Tenant's audit records.
type TenantPermissionLogListResult struct {
	Logs     []*entity.PermissionChangeLog `json:"logs"`
	Total    int64                         `json:"total"`
	Page     int                           `json:"page"`
	PageSize int                           `json:"page_size"`
}

// ListTenantRoles returns only the roles owned by tenantID.
func (s *TenantService) ListTenantRoles(ctx context.Context, userID, tenantID, keyword string, page, pageSize int) (*TenantRoleListResult, error) {
	db := dao.DB.WithContext(ctx)
	if _, _, err := tenantMemberPermission(ctx, db, permission.TenantIDFromContext(ctx), 0); err != nil {
		return nil, err
	}
	page, pageSize = normalizeTenantRolePagination(page, pageSize)
	query := db.Model(&entity.Role{}).Where("tenant_id = ?", tenantID)
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		query = query.Where("LOWER(role_name) LIKE LOWER(?)", "%"+keyword+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count Tenant roles: %w", err)
	}
	var roles []entity.Role
	if err := query.Order("role_name ASC").Order("id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("list Tenant roles: %w", err)
	}
	var tenant entity.Tenant
	if err := db.Select("id", "role_id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
		return nil, fmt.Errorf("load Tenant default role: %w", err)
	}
	result := &TenantRoleListResult{Roles: make([]*TenantRoleItem, 0, len(roles)), Total: total, Page: page, PageSize: pageSize}
	for i := range roles {
		result.Roles = append(result.Roles, tenantRoleItem(&roles[i], tenant.RoleID))
	}
	return result, nil
}

// CreateTenantRole creates a role scoped to the current Tenant.
func (s *TenantService) CreateTenantRole(ctx context.Context, userID, tenantID, roleName, description string) (*TenantRoleItem, error) {
	roleName, err := validateTenantRoleName(roleName)
	if err != nil {
		return nil, err
	}
	var created entity.Role
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := tenantMemberPermission(ctx, tx, permission.TenantIDFromContext(ctx), entity.TenantPermissionManageRoles); err != nil {
			return err
		}
		if err := lockTenantRoleContainer(tx, tenantID); err != nil {
			return err
		}
		if err := ensureUniqueTenantRoleName(tx, tenantID, roleName, 0); err != nil {
			return err
		}
		descriptionCopy := description
		created = entity.Role{RoleName: roleName, Description: &descriptionCopy, TenantID: &tenantID}
		if err := tx.Create(&created).Error; err != nil {
			return fmt.Errorf("create Tenant role: %w", err)
		}
		roleID := strconv.FormatInt(int64(created.ID), 10)
		return recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "role.created", "role", roleID, "role", roleID, "create", 0, 0, nil, tenantRoleAuditValue(&created))
	})
	if err != nil {
		return nil, err
	}
	return tenantRoleItem(&created, nil), nil
}

// GetTenantRole returns one role after checking that it belongs to tenantID.
func (s *TenantService) GetTenantRole(ctx context.Context, userID, tenantID string, roleID int32) (*TenantRoleItem, error) {
	db := dao.DB.WithContext(ctx)
	if _, _, err := tenantMemberPermission(ctx, db, permission.TenantIDFromContext(ctx), 0); err != nil {
		return nil, err
	}
	role, err := findTenantRole(db, tenantID, roleID, false)
	if err != nil {
		return nil, err
	}
	var tenant entity.Tenant
	if err := db.Select("id", "role_id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
		return nil, fmt.Errorf("load Tenant default role: %w", err)
	}
	return tenantRoleItem(role, tenant.RoleID), nil
}

// UpdateTenantRole updates a Tenant role's name and description.
func (s *TenantService) UpdateTenantRole(ctx context.Context, userID, tenantID string, roleID int32, roleName, description *string) (*TenantRoleItem, error) {
	if roleName == nil && description == nil {
		return nil, common.NewCodedError(common.CodeBadRequest, "At least one role field is required")
	}
	var updated entity.Role
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := tenantMemberPermission(ctx, tx, permission.TenantIDFromContext(ctx), entity.TenantPermissionManageRoles); err != nil {
			return err
		}
		if err := lockTenantRoleContainer(tx, tenantID); err != nil {
			return err
		}
		role, err := findTenantRole(tx, tenantID, roleID, true)
		if err != nil {
			return err
		}
		before := tenantRoleAuditValue(role)
		updates := map[string]interface{}{}
		if roleName != nil {
			name, err := validateTenantRoleName(*roleName)
			if err != nil {
				return err
			}
			if err := ensureUniqueTenantRoleName(tx, tenantID, name, role.ID); err != nil {
				return err
			}
			updates["role_name"] = name
		}
		if description != nil {
			updates["description"] = strings.TrimSpace(*description)
		}
		if err := tx.Model(&entity.Role{}).Where("id = ? AND tenant_id = ?", role.ID, tenantID).Updates(updates).Error; err != nil {
			return fmt.Errorf("update Tenant role: %w", err)
		}
		if err := tx.Where("id = ? AND tenant_id = ?", role.ID, tenantID).Take(&updated).Error; err != nil {
			return fmt.Errorf("reload Tenant role: %w", err)
		}
		roleIDString := strconv.FormatInt(int64(role.ID), 10)
		return recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "role.updated", "role", roleIDString, "role", roleIDString, "update", 0, 0, before, tenantRoleAuditValue(&updated))
	})
	if err != nil {
		return nil, err
	}
	var tenant entity.Tenant
	if err := dao.DB.WithContext(ctx).Select("id", "role_id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
		return nil, fmt.Errorf("load Tenant default role: %w", err)
	}
	return tenantRoleItem(&updated, tenant.RoleID), nil
}

// DeleteTenantRole deletes an unused role and its permission rows.
func (s *TenantService) DeleteTenantRole(ctx context.Context, userID, tenantID string, roleID int32) error {
	return dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := tenantMemberPermission(ctx, tx, permission.TenantIDFromContext(ctx), entity.TenantPermissionManageRoles); err != nil {
			return err
		}
		var tenant entity.Tenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "role_id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
			return fmt.Errorf("lock Tenant before deleting role: %w", err)
		}
		role, err := findTenantRole(tx, tenantID, roleID, true)
		if err != nil {
			return err
		}
		if tenant.RoleID != nil && *tenant.RoleID == roleID {
			return common.NewCodedError(common.CodeConflict, "Role is the Tenant default and cannot be deleted")
		}
		roleValue := strconv.FormatInt(int64(roleID), 10)
		var assignmentCount int64
		if err := tx.Model(&entity.UserTenant{}).Where("tenant_id = ? AND role = ?", tenantID, roleValue).Count(&assignmentCount).Error; err != nil {
			return fmt.Errorf("check Tenant role assignments: %w", err)
		}
		if assignmentCount > 0 {
			return common.NewCodedError(common.CodeConflict, "Role is assigned to Tenant members and cannot be deleted")
		}
		if err := tx.Where("role_id = ?", roleID).Delete(&entity.RoleResource{}).Error; err != nil {
			return fmt.Errorf("delete Tenant role permissions: %w", err)
		}
		if err := tx.Where("role_id = ?", roleID).Delete(&entity.RoleDefaultModel{}).Error; err != nil {
			return fmt.Errorf("delete Tenant role default models: %w", err)
		}
		if err := tx.Where("id = ? AND tenant_id = ?", roleID, tenantID).Delete(&entity.Role{}).Error; err != nil {
			return fmt.Errorf("delete Tenant role: %w", err)
		}
		return recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "role.deleted", "role", roleValue, "role", roleValue, "delete", 0, 0, tenantRoleAuditValue(role), map[string]interface{}{"deleted": true})
	})
}

// ListTenantRoleResources returns supported feature types and actions.
func (s *TenantService) ListTenantRoleResources(ctx context.Context, userID, tenantID string) (map[string]interface{}, error) {
	if _, _, err := tenantMemberPermission(ctx, dao.DB.WithContext(ctx), permission.TenantIDFromContext(ctx), 0); err != nil {
		return nil, err
	}
	resources := make([]map[string]interface{}, 0, len(tenantRoleResourceNames))
	for _, name := range tenantRoleResourceNames {
		resources = append(resources, map[string]interface{}{"resource_type": name, "actions": tenantRoleResourceActions[name]})
	}
	return map[string]interface{}{"resources": resources}, nil
}

// GetTenantRolePermissions returns the complete permission matrix for a role.
func (s *TenantService) GetTenantRolePermissions(ctx context.Context, userID, tenantID string, roleID int32) (*TenantRolePermissionsResult, error) {
	db := dao.DB.WithContext(ctx)
	if _, _, err := tenantMemberPermission(ctx, db, permission.TenantIDFromContext(ctx), 0); err != nil {
		return nil, err
	}
	role, err := findTenantRole(db, tenantID, roleID, false)
	if err != nil {
		return nil, err
	}
	matrix, err := loadTenantRolePermissionMatrix(db, roleID)
	if err != nil {
		return nil, err
	}
	var tenant entity.Tenant
	if err := db.Select("id", "role_id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
		return nil, fmt.Errorf("load Tenant default role: %w", err)
	}
	return &TenantRolePermissionsResult{Role: tenantRoleItem(role, tenant.RoleID), Permissions: matrix}, nil
}

// SetTenantRolePermissions atomically replaces a role's feature permission matrix.
func (s *TenantService) SetTenantRolePermissions(ctx context.Context, userID, tenantID string, roleID int32, requested map[string]map[string]bool) (*TenantRolePermissionsResult, error) {
	normalized, rows, err := normalizeTenantRolePermissions(roleID, requested)
	if err != nil {
		return nil, err
	}
	var role entity.Role
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := tenantMemberPermission(ctx, tx, permission.TenantIDFromContext(ctx), entity.TenantPermissionManageRoles); err != nil {
			return err
		}
		rolePtr, err := findTenantRole(tx, tenantID, roleID, true)
		if err != nil {
			return err
		}
		role = *rolePtr
		before, err := loadTenantRolePermissionMatrix(tx, roleID)
		if err != nil {
			return err
		}
		changed := false
		for _, resource := range tenantRoleResourceNames {
			if tenantRoleResourceMask(before[resource]) != tenantRoleResourceMask(normalized[resource]) {
				changed = true
				break
			}
		}
		if !changed {
			return nil
		}
		if err := tx.Where("role_id = ?", roleID).Delete(&entity.RoleResource{}).Error; err != nil {
			return fmt.Errorf("replace Tenant role permissions: %w", err)
		}
		if len(rows) > 0 {
			if err := tx.Create(&rows).Error; err != nil {
				return fmt.Errorf("save Tenant role permissions: %w", err)
			}
		}
		roleIDString := strconv.FormatInt(int64(roleID), 10)
		for _, resource := range tenantRoleResourceNames {
			beforeMask := tenantRoleResourceMask(before[resource])
			afterMask := tenantRoleResourceMask(normalized[resource])
			if beforeMask == afterMask {
				continue
			}
			beforeData := map[string]interface{}{"role_id": roleID, "resource_type": resource, "permission": beforeMask}
			afterData := map[string]interface{}{"role_id": roleID, "resource_type": resource, "permission": afterMask}
			if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "role.permissions.updated", "role", roleIDString, "role_permission", roleIDString, "update", beforeMask, afterMask, beforeData, afterData); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var tenant entity.Tenant
	if err := dao.DB.WithContext(ctx).Select("id", "role_id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
		return nil, fmt.Errorf("load Tenant default role: %w", err)
	}
	return &TenantRolePermissionsResult{Role: tenantRoleItem(&role, tenant.RoleID), Permissions: normalized}, nil
}

// GetTenantDefaultRole returns the role assigned to new Tenant members.
func (s *TenantService) GetTenantDefaultRole(ctx context.Context, userID, tenantID string) (*TenantDefaultRoleResult, error) {
	db := dao.DB.WithContext(ctx)
	if _, _, err := tenantMemberPermission(ctx, db, permission.TenantIDFromContext(ctx), 0); err != nil {
		return nil, err
	}
	var tenant entity.Tenant
	if err := db.Select("id", "role_id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
		return nil, fmt.Errorf("load Tenant default role: %w", err)
	}
	result := &TenantDefaultRoleResult{RoleID: tenant.RoleID}
	if tenant.RoleID != nil {
		role, err := findTenantRole(db, tenantID, *tenant.RoleID, false)
		if err != nil {
			return nil, common.NewCodedError(common.CodeConflict, "Tenant default role is invalid")
		}
		result.Role = tenantRoleItem(role, tenant.RoleID)
	}
	return result, nil
}

// SetTenantDefaultRole sets or clears the role assigned to newly joined members.
func (s *TenantService) SetTenantDefaultRole(ctx context.Context, userID, tenantID string, roleID *int32) (*TenantDefaultRoleResult, error) {
	var result TenantDefaultRoleResult
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := tenantMemberPermission(ctx, tx, permission.TenantIDFromContext(ctx), entity.TenantPermissionManageRoles); err != nil {
			return err
		}
		var tenant entity.Tenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "role_id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
			return fmt.Errorf("load Tenant: %w", err)
		}
		if roleID != nil {
			role, err := findTenantRole(tx, tenantID, *roleID, false)
			if err != nil {
				return err
			}
			result.Role = tenantRoleItem(role, roleID)
		}
		beforeID := tenant.RoleID
		if !equalInt32Pointers(beforeID, roleID) {
			if err := tx.Model(&entity.Tenant{}).Where("id = ?", tenantID).Update("role_id", roleID).Error; err != nil {
				return fmt.Errorf("update Tenant default role: %w", err)
			}
			if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "tenant.default_role.updated", "tenant", tenantID, "tenant", tenantID, "update", rolePointerValue(beforeID), rolePointerValue(roleID),
				map[string]interface{}{"role_id": beforeID}, map[string]interface{}{"role_id": roleID}); err != nil {
				return err
			}
		}
		result.RoleID = roleID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTenantManagementPermissions returns active user and group grants on the Tenant resource.
func (s *TenantService) ListTenantManagementPermissions(ctx context.Context, userID, tenantID string) (*TenantManagementGrantListResult, error) {
	db := dao.DB.WithContext(ctx)
	if _, _, err := tenantMemberPermission(ctx, db, permission.TenantIDFromContext(ctx), entity.TenantPermissionManageRoles); err != nil {
		return nil, err
	}
	var rows []entity.Permission
	if err := db.Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ? AND permission <> 0",
		tenantID, "tenant", tenantID, string(entity.StatusValid)).Order("create_time ASC").Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list Tenant management permissions: %w", err)
	}
	memberIDs := make([]string, 0, len(rows))
	groupIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.MemberID != nil {
			memberIDs = append(memberIDs, *row.MemberID)
		}
		if row.GroupID != nil {
			groupIDs = append(groupIDs, *row.GroupID)
		}
	}
	membersByID := make(map[string]string, len(memberIDs))
	if len(memberIDs) > 0 {
		var memberships []entity.UserTenant
		if err := db.Where("tenant_id = ? AND status = ? AND id IN ?", tenantID, string(entity.StatusValid), memberIDs).Find(&memberships).Error; err != nil {
			return nil, fmt.Errorf("load Tenant permission members: %w", err)
		}
		for _, membership := range memberships {
			membersByID[membership.ID] = membership.UserID
		}
	}
	groupsByID := make(map[string]entity.GroupInfo, len(groupIDs))
	if len(groupIDs) > 0 && db.Migrator().HasTable(&entity.GroupInfo{}) {
		var groups []entity.GroupInfo
		if err := db.Where("tenant_id = ? AND status = ? AND id IN ?", tenantID, string(entity.StatusValid), groupIDs).Find(&groups).Error; err != nil {
			return nil, fmt.Errorf("load Tenant permission groups: %w", err)
		}
		for _, group := range groups {
			groupsByID[group.ID] = group
		}
	}
	result := &TenantManagementGrantListResult{Grants: make([]*TenantManagementGrant, 0, len(rows))}
	for _, row := range rows {
		grant := &TenantManagementGrant{Permission: row.Permission, Permissions: tenantManagementPermissionNames(row.Permission)}
		switch {
		case row.MemberID != nil && row.GroupID == nil:
			grant.SubjectType = "user"
			grant.UserID = membersByID[*row.MemberID]
			if grant.UserID == "" {
				continue
			}
		case row.GroupID != nil && row.MemberID == nil:
			group, ok := groupsByID[*row.GroupID]
			if !ok {
				continue
			}
			grant.SubjectType = "group"
			grant.GroupID = group.ID
			grant.GroupName = group.Name
		default:
			continue
		}
		result.Grants = append(result.Grants, grant)
	}
	return result, nil
}

// SetTenantManagementPermissions replaces one member or group's Tenant management grant.
func (s *TenantService) SetTenantManagementPermissions(ctx context.Context, userID, tenantID, targetUserID, groupID string, permissions []string) (*TenantManagementGrant, error) {
	targetUserID = strings.TrimSpace(targetUserID)
	groupID = strings.TrimSpace(groupID)
	if (targetUserID == "") == (groupID == "") {
		return nil, common.NewCodedError(common.CodeBadRequest, "Exactly one of user_id or group_id is required")
	}
	newMask, err := parseTenantManagementPermissionMask(permissions)
	if err != nil {
		return nil, err
	}
	var result *TenantManagementGrant
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := tenantMemberPermission(ctx, tx, permission.TenantIDFromContext(ctx), entity.TenantPermissionOwner); err != nil {
			return err
		}
		var tenant entity.Tenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
			return fmt.Errorf("lock Tenant for permission update: %w", err)
		}

		var membership *entity.UserTenant
		var group *entity.GroupInfo
		var memberID, permissionGroupID *string
		var targetType, targetID string
		if targetUserID != "" {
			var err error
			membership, err = findTenantMembership(tx, tenantID, targetUserID, true)
			if err != nil {
				return err
			}
			if membership.Role == "owner" {
				return common.NewCodedError(common.CodeConflict, "Tenant owner permissions cannot be changed")
			}
			memberID = &membership.ID
			if membership.UserID == userID {
				return common.NewCodedError(common.CodeConflict, "you cannot change your own Tenant permissions")
			}
			targetType, targetID = "user", targetUserID
		} else {
			var found entity.GroupInfo
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND tenant_id = ? AND status = ?", groupID, tenantID, string(entity.StatusValid)).Take(&found).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return common.NewCodedError(common.CodeNotFound, "Tenant group not found")
				}
				return fmt.Errorf("load Tenant group: %w", err)
			}
			group = &found
			permissionGroupID = &group.ID
			targetType, targetID = "group", group.ID
		}

		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ?", tenantID, "tenant", tenantID, string(entity.StatusValid))
		if memberID != nil {
			query = query.Where("member_id = ? AND group_id IS NULL", *memberID)
		} else {
			query = query.Where("group_id = ? AND member_id IS NULL", *permissionGroupID)
		}
		var existing []entity.Permission
		if err := query.Order("id ASC").Find(&existing).Error; err != nil {
			return fmt.Errorf("load current Tenant management grant: %w", err)
		}
		var oldMask int32
		for _, row := range existing {
			oldMask |= row.Permission
		}
		if oldMask == newMask && len(existing) <= 1 {
			result = tenantManagementGrant(targetUserID, group, newMask)
			return nil
		}
		if len(existing) == 0 && newMask != 0 {
			grant := entity.Permission{
				ID: common.GenerateUUID(), MemberID: memberID, GroupID: permissionGroupID, TenantID: tenantID,
				ResourceType: "tenant", ResourceID: &tenantID, Permission: newMask,
			}
			status := string(entity.StatusValid)
			grant.Status = &status
			if err := tx.Create(&grant).Error; err != nil {
				return fmt.Errorf("create Tenant management grant: %w", err)
			}
		} else if len(existing) > 0 {
			status := string(entity.StatusInvalid)
			if newMask != 0 {
				status = string(entity.StatusValid)
			}
			if err := tx.Model(&entity.Permission{}).Where("id = ?", existing[0].ID).
				Updates(map[string]interface{}{"permission": newMask, "status": status}).Error; err != nil {
				return fmt.Errorf("update Tenant management grant: %w", err)
			}
			if len(existing) > 1 {
				if err := tx.Model(&entity.Permission{}).Where("id IN ?", permissionIDs(existing[1:])).
					Updates(map[string]interface{}{"permission": 0, "status": string(entity.StatusInvalid)}).Error; err != nil {
					return fmt.Errorf("consolidate Tenant management grants: %w", err)
				}
			}
		}
		if oldMask != newMask {
			beforeData := tenantManagementGrantAuditValue(targetUserID, group, oldMask)
			afterData := tenantManagementGrantAuditValue(targetUserID, group, newMask)
			if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "tenant.management_permission.updated", targetType, targetID, "tenant", tenantID, "update", oldMask, newMask, beforeData, afterData); err != nil {
				return err
			}
		}
		result = tenantManagementGrant(targetUserID, group, newMask)
		return nil
	})
	return result, err
}

// GetTenantMemberRole returns the role assigned to a member of tenantID.
func (s *TenantService) GetTenantMemberRole(ctx context.Context, userID, tenantID, targetUserID string) (map[string]interface{}, error) {
	db := dao.DB.WithContext(ctx)
	if _, _, err := tenantMemberPermission(ctx, db, permission.TenantIDFromContext(ctx), entity.TenantPermissionManageRoles); err != nil {
		return nil, err
	}
	membership, err := findTenantMembership(db, tenantID, targetUserID, false)
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{"user_id": targetUserID, "role": membership.Role}
	if membership.Role == "owner" {
		result["owner"] = true
		return result, nil
	}
	roleID, err := strconv.ParseInt(membership.Role, 10, 32)
	if err != nil {
		return result, nil
	}
	role, err := findTenantRole(db, tenantID, int32(roleID), false)
	if err != nil {
		return nil, err
	}
	result["role_id"] = role.ID
	result["role_name"] = role.RoleName
	return result, nil
}

// SetTenantMemberRole assigns a Tenant feature role to one joined member.
func (s *TenantService) SetTenantMemberRole(ctx context.Context, userID, tenantID, targetUserID string, roleID int32) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := tenantMemberPermission(ctx, tx, permission.TenantIDFromContext(ctx), entity.TenantPermissionManageRoles); err != nil {
			return err
		}
		role, err := findTenantRole(tx, tenantID, roleID, true)
		if err != nil {
			return err
		}
		membership, err := findTenantMembership(tx, tenantID, targetUserID, true)
		if err != nil {
			return err
		}
		membershipPermission, err := tenantPermissionMask(ctx, tx, membership)
		if err != nil {
			return err
		}
		if membership.Role == "owner" || membershipPermission&entity.TenantPermissionOwner != 0 {
			return common.NewCodedError(common.CodeConflict, "Tenant owner role cannot be changed through member role assignment")
		}
		newRole := strconv.FormatInt(int64(role.ID), 10)
		if membership.Role != newRole {
			before := map[string]interface{}{"user_id": targetUserID, "role": membership.Role}
			after := map[string]interface{}{"user_id": targetUserID, "role": newRole, "role_id": role.ID, "role_name": role.RoleName}
			if err := tx.Model(&entity.UserTenant{}).Where("id = ? AND tenant_id = ?", membership.ID, tenantID).Update("role", newRole).Error; err != nil {
				return fmt.Errorf("assign Tenant role to member: %w", err)
			}
			if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "tenant.member_role.updated", "user", targetUserID, "tenant_role", strconv.FormatInt(int64(role.ID), 10), "update", 0, 0, before, after); err != nil {
				return err
			}
		}
		result = map[string]interface{}{"user_id": targetUserID, "role_id": role.ID, "role_name": role.RoleName}
		return nil
	})
	return result, err
}

// GetTenantMyPermissions returns the caller's feature role and Tenant management permissions.
func (s *TenantService) GetTenantMyPermissions(ctx context.Context, userID, tenantID string) (map[string]interface{}, error) {
	db := dao.DB.WithContext(ctx)
	membership, mask, err := tenantMemberPermission(ctx, db, permission.TenantIDFromContext(ctx), 0)
	if err != nil {
		return nil, err
	}
	var role *entity.Role
	var matrix map[string]map[string]bool
	if membership.Role == "owner" || mask&entity.TenantPermissionOwner != 0 {
		matrix = fullTenantRolePermissionMatrix()
	} else if roleID, parseErr := strconv.ParseInt(membership.Role, 10, 32); parseErr == nil {
		role, err = findTenantRole(db, tenantID, int32(roleID), false)
		if err != nil {
			return nil, err
		}
		matrix, err = loadTenantRolePermissionMatrix(db, role.ID)
		if err != nil {
			return nil, err
		}
	} else {
		matrix = emptyTenantRolePermissionMatrix()
	}
	var roleID interface{}
	var roleName interface{}
	if role != nil {
		roleID = role.ID
		roleName = role.RoleName
	}
	return map[string]interface{}{
		"tenant_id":       tenantID,
		"user_id":         userID,
		"membership_role": membership.Role,
		"role_id":         roleID,
		"role_name":       roleName,
		"permissions":     matrix,
		"tenant_permissions": map[string]bool{
			"manage_members": mask&entity.TenantPermissionManageMembers != 0 || mask&entity.TenantPermissionOwner != 0,
			"manage_roles":   mask&entity.TenantPermissionManageRoles != 0 || mask&entity.TenantPermissionOwner != 0,
			"owner":          mask&entity.TenantPermissionOwner != 0,
		},
	}, nil
}

// ListTenantPermissionLogs returns audit rows scoped to the active Tenant.
func (s *TenantService) ListTenantPermissionLogs(ctx context.Context, userID, tenantID string, filter dao.PermissionChangeLogFilter, page, pageSize int) (*TenantPermissionLogListResult, error) {
	_, mask, err := tenantMemberPermission(ctx, dao.DB.WithContext(ctx), permission.TenantIDFromContext(ctx), 0)
	if err != nil {
		return nil, err
	}
	if mask&(entity.TenantPermissionManageRoles|entity.TenantPermissionManageMembers|entity.TenantPermissionOwner) == 0 {
		return nil, common.NewCodedError(common.CodeForbidden, "Tenant permission denied")
	}
	page, pageSize = normalizeTenantRolePagination(page, pageSize)
	filter.TenantID = tenantID
	filter.ScopeType = "tenant"
	logs, total, err := dao.NewPermissionChangeLogDAO().ListPermissionChangeLogs(ctx, dao.DB, filter, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, fmt.Errorf("list Tenant permission logs: %w", err)
	}
	return &TenantPermissionLogListResult{Logs: logs, Total: total, Page: page, PageSize: pageSize}, nil
}

func tenantMemberPermissionEE(ctx context.Context, db *gorm.DB, _ string, required int32) (*entity.UserTenant, int32, bool, error) {
	userID := strings.TrimSpace(permission.UserIDFromContext(ctx))
	tenantID := strings.TrimSpace(permission.TenantIDFromContext(ctx))
	if userID == "" {
		return nil, 0, true, common.NewCodedError(common.CodeUnauthorized, "User is required")
	}
	if tenantID == "" {
		return nil, 0, true, common.NewCodedError(common.CodeBadRequest, "Current Tenant context is required")
	}
	membership, err := findTenantMembership(db, tenantID, userID, false)
	if err != nil {
		return nil, 0, true, err
	}
	if membership.Role == "owner" {
		mask := entity.TenantPermissionManageMembers | entity.TenantPermissionManageRoles | entity.TenantPermissionOwner
		return membership, mask, true, nil
	}
	mask, err := tenantPermissionMask(ctx, db, membership)
	if err != nil {
		return nil, 0, true, err
	}
	if mask&entity.TenantPermissionOwner != 0 {
		mask |= entity.TenantPermissionManageMembers | entity.TenantPermissionManageRoles
	}
	if required != 0 && mask&required != required {
		return nil, mask, true, common.NewCodedError(common.CodeForbidden, "Tenant permission denied")
	}
	return membership, mask, true, nil
}

func tenantPermissionMask(ctx context.Context, db *gorm.DB, membership *entity.UserTenant) (int32, error) {
	var groupIDs []string
	if db.Migrator().HasTable(&entity.GroupMember{}) && db.Migrator().HasTable(&entity.GroupInfo{}) {
		if err := db.WithContext(ctx).Table("group_member").
			Select("group_member.group_id").
			Joins("JOIN group_info ON group_info.id = group_member.group_id").
			Where("group_member.member_id = ? AND group_member.status = ? AND group_info.tenant_id = ? AND group_info.status = ?",
				membership.ID, string(entity.StatusValid), membership.TenantID, string(entity.StatusValid)).
			Pluck("group_member.group_id", &groupIDs).Error; err != nil {
			return 0, fmt.Errorf("load Tenant permission groups: %w", err)
		}
	}
	if !db.Migrator().HasTable(&entity.Permission{}) {
		return 0, nil
	}
	query := db.WithContext(ctx).Model(&entity.Permission{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ?",
			membership.TenantID, "tenant", membership.TenantID, string(entity.StatusValid))
	targets := db.Where("member_id = ? AND group_id IS NULL", membership.ID)
	if len(groupIDs) > 0 {
		targets = targets.Or("group_id IN ? AND member_id IS NULL", groupIDs)
	}
	var rows []entity.Permission
	if err := query.Where(targets).Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("load Tenant management permissions: %w", err)
	}
	var mask int32
	for _, row := range rows {
		mask |= row.Permission
	}
	return mask, nil
}

func findTenantMembership(db *gorm.DB, tenantID, userID string, lock bool) (*entity.UserTenant, error) {
	query := db.Model(&entity.UserTenant{}).Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, userID, string(entity.StatusValid))
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var membership entity.UserTenant
	if err := query.Take(&membership).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewCodedError(common.CodeForbidden, "User is not an active member of this Tenant")
		}
		return nil, fmt.Errorf("load Tenant membership: %w", err)
	}
	return &membership, nil
}

func lockTenantRoleContainer(db *gorm.DB, tenantID string) error {
	var tenant entity.Tenant
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewCodedError(common.CodeNotFound, "Tenant not found")
		}
		return fmt.Errorf("lock Tenant role container: %w", err)
	}
	return nil
}

func findTenantRole(db *gorm.DB, tenantID string, roleID int32, lock bool) (*entity.Role, error) {
	query := db.Model(&entity.Role{}).Where("id = ? AND tenant_id = ?", roleID, tenantID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var role entity.Role
	if err := query.Take(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewCodedError(common.CodeNotFound, "Tenant role not found")
		}
		return nil, fmt.Errorf("load Tenant role: %w", err)
	}
	return &role, nil
}

func ensureUniqueTenantRoleName(db *gorm.DB, tenantID, roleName string, exceptRoleID int32) error {
	query := db.Model(&entity.Role{}).Where("tenant_id = ? AND LOWER(role_name) = LOWER(?)", tenantID, roleName)
	if exceptRoleID > 0 {
		query = query.Where("id <> ?", exceptRoleID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return fmt.Errorf("check Tenant role name: %w", err)
	}
	if count > 0 {
		return common.NewCodedError(common.CodeConflict, "A Tenant role with this name already exists")
	}
	return nil
}

func validateTenantRoleName(roleName string) (string, error) {
	roleName = strings.TrimSpace(roleName)
	if roleName == "" || utf8.RuneCountInString(roleName) > 64 {
		return "", common.NewCodedError(common.CodeBadRequest, "Role name must contain between 1 and 64 characters")
	}
	return roleName, nil
}

func tenantRoleItem(role *entity.Role, defaultRoleID *int32) *TenantRoleItem {
	return &TenantRoleItem{
		ID:          role.ID,
		RoleName:    role.RoleName,
		Description: role.Description,
		IsDefault:   defaultRoleID != nil && *defaultRoleID == role.ID,
		CreateDate:  role.CreateDate,
		UpdateDate:  role.UpdateDate,
	}
}

func normalizeTenantRolePagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = defaultTenantRolePage
	}
	if pageSize < 1 {
		pageSize = defaultTenantRolePageSize
	}
	if pageSize > maxTenantRolePageSize {
		pageSize = maxTenantRolePageSize
	}
	return page, pageSize
}

func fullTenantRolePermissionMatrix() map[string]map[string]bool {
	matrix := emptyTenantRolePermissionMatrix()
	for _, actions := range matrix {
		for action := range actions {
			actions[action] = true
		}
	}
	return matrix
}

func emptyTenantRolePermissionMatrix() map[string]map[string]bool {
	matrix := make(map[string]map[string]bool, len(tenantRoleResourceNames))
	for _, resource := range tenantRoleResourceNames {
		matrix[resource] = make(map[string]bool, len(tenantRoleResourceActions[resource]))
		for _, action := range tenantRoleResourceActions[resource] {
			matrix[resource][action] = false
		}
	}
	return matrix
}

func loadTenantRolePermissionMatrix(db *gorm.DB, roleID int32) (map[string]map[string]bool, error) {
	var rows []entity.RoleResource
	if err := db.Model(&entity.RoleResource{}).Where("role_id = ?", roleID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load Tenant role permissions: %w", err)
	}
	namesByType := make(map[int64]string, len(tenantRoleResourceTypes))
	for name, resourceType := range tenantRoleResourceTypes {
		namesByType[resourceType] = name
	}
	matrix := emptyTenantRolePermissionMatrix()
	for _, row := range rows {
		name, ok := namesByType[row.ResourceType]
		if !ok {
			return nil, fmt.Errorf("unknown role resource type %d for role %d", row.ResourceType, roleID)
		}
		for _, action := range tenantRoleResourceActions[name] {
			matrix[name][action] = matrix[name][action] || row.Action&tenantRoleActionMasks[action] != 0
		}
	}
	return matrix, nil
}

func normalizeTenantRolePermissions(roleID int32, requested map[string]map[string]bool) (map[string]map[string]bool, []entity.RoleResource, error) {
	if requested == nil {
		return nil, nil, common.NewCodedError(common.CodeBadRequest, "permissions is required")
	}
	matrix := emptyTenantRolePermissionMatrix()
	for resource, actions := range requested {
		resource = strings.ToLower(strings.TrimSpace(resource))
		if _, ok := tenantRoleResourceTypes[resource]; !ok {
			return nil, nil, common.NewCodedError(common.CodeBadRequest, "Unknown role resource: "+resource)
		}
		for action, enabled := range actions {
			action = strings.ToLower(strings.TrimSpace(action))
			if _, ok := tenantRoleActionMasks[action]; !ok {
				return nil, nil, common.NewCodedError(common.CodeBadRequest, fmt.Sprintf("Action %q is not supported for resource %q", action, resource))
			}
			if _, supported := matrix[resource][action]; !supported {
				if enabled {
					return nil, nil, common.NewCodedError(common.CodeBadRequest, fmt.Sprintf("Action %q is not supported for resource %q", action, resource))
				}
				continue
			}
			matrix[resource][action] = enabled
		}
	}
	rows := make([]entity.RoleResource, 0, len(tenantRoleResourceTypes))
	for _, resource := range tenantRoleResourceNames {
		actions := matrix[resource]
		var actionMask int32
		for action, enabled := range actions {
			if enabled {
				actionMask |= tenantRoleActionMasks[action]
			}
		}
		if actionMask != 0 {
			rows = append(rows, entity.RoleResource{RoleID: roleID, ResourceType: tenantRoleResourceTypes[resource], Action: actionMask})
		}
	}
	return matrix, rows, nil
}

func tenantRoleResourceMask(actions map[string]bool) int32 {
	var mask int32
	for action, enabled := range actions {
		if enabled {
			mask |= tenantRoleActionMasks[action]
		}
	}
	return mask
}

func tenantRoleAuditValue(role *entity.Role) map[string]interface{} {
	return map[string]interface{}{"id": role.ID, "role_name": role.RoleName, "description": role.Description}
}

func recordTenantPermissionChangeEE(ctx context.Context, tx *gorm.DB, _ string, eventType, targetType, targetID, resourceType, resourceID, actionType string, beforePermission, afterPermission int32, beforeData, afterData interface{}) error {
	// Identity and scope come from the authenticated request context, so callers
	// cannot write audit events into another Tenant's scope.
	tenantID := permission.TenantIDFromContext(ctx)
	actorUserID := permission.UserIDFromContext(ctx)
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(actorUserID) == "" {
		return common.NewCodedError(common.CodeUnauthorized, "User and Tenant context are required")
	}
	var membership entity.UserTenant
	err := tx.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, actorUserID, string(entity.StatusValid)).
		Take(&membership).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && targetType == "user" && targetID == actorUserID && resourceType == "tenant_membership" && resourceID != "" {
		membership.ID = resourceID
	} else if err != nil {
		return fmt.Errorf("load Tenant audit operator membership: %w", err)
	}
	var beforeJSON, afterJSON *string
	if beforeData != nil {
		value, err := json.Marshal(beforeData)
		if err != nil {
			return fmt.Errorf("encode previous Tenant permission state: %w", err)
		}
		encoded := string(value)
		beforeJSON = &encoded
	}
	if afterData != nil {
		value, err := json.Marshal(afterData)
		if err != nil {
			return fmt.Errorf("encode updated Tenant permission state: %w", err)
		}
		encoded := string(value)
		afterJSON = &encoded
	}
	actorID := actorUserID
	status := string(entity.StatusValid)
	log := &entity.PermissionChangeLog{
		ID:            common.GenerateUUID(),
		TenantID:      tenantID,
		OperatorID:    membership.ID,
		TargetType:    targetType,
		TargetID:      targetID,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		OldPermission: beforePermission,
		NewPermission: afterPermission,
		ActionType:    actionType,
		EventType:     eventType,
		ActorUserID:   &actorID,
		ScopeType:     "tenant",
		BeforeData:    beforeJSON,
		AfterData:     afterJSON,
		Status:        &status,
	}
	if err := tx.Create(log).Error; err != nil {
		return fmt.Errorf("record Tenant permission change: %w", err)
	}
	return nil
}

func rolePointerValue(roleID *int32) int32 {
	if roleID == nil {
		return 0
	}
	return *roleID
}

func equalInt32Pointers(left, right *int32) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func parseTenantManagementPermissionMask(permissions []string) (int32, error) {
	var mask int32
	for _, permission := range permissions {
		permission = strings.ToLower(strings.TrimSpace(permission))
		bit, ok := tenantManagementPermissionBits[permission]
		if !ok {
			return 0, common.NewCodedError(common.CodeBadRequest, fmt.Sprintf("Unknown Tenant management permission %q", permission))
		}
		mask |= bit
	}
	return mask, nil
}

func tenantManagementPermissionNames(mask int32) []string {
	permissions := make([]string, 0, len(tenantManagementPermissionBits))
	for _, permission := range []string{"manage_members", "manage_roles"} {
		if mask&tenantManagementPermissionBits[permission] != 0 {
			permissions = append(permissions, permission)
		}
	}
	return permissions
}

func tenantManagementGrant(userID string, group *entity.GroupInfo, mask int32) *TenantManagementGrant {
	grant := &TenantManagementGrant{Permission: mask, Permissions: tenantManagementPermissionNames(mask)}
	if group != nil {
		grant.SubjectType = "group"
		grant.GroupID = group.ID
		grant.GroupName = group.Name
	} else {
		grant.SubjectType = "user"
		grant.UserID = userID
	}
	return grant
}

func tenantManagementGrantAuditValue(userID string, group *entity.GroupInfo, mask int32) map[string]interface{} {
	value := map[string]interface{}{"permission": mask, "permissions": tenantManagementPermissionNames(mask)}
	if group != nil {
		value["subject_type"] = "group"
		value["group_id"] = group.ID
	} else {
		value["subject_type"] = "user"
		value["user_id"] = userID
	}
	return value
}

func permissionIDs(rows []entity.Permission) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}
