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

//go:build ee

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	resourcePermissionEventGranted = "resource.permission.granted"
	resourcePermissionEventUpdated = "resource.permission.updated"
	resourcePermissionEventRevoked = "resource.permission.revoked"
	resourceOwnershipEventTransfer = "resource.owner.transferred"
	maxPermissionBatchSize         = 200
)

var resourcePermissionMask = entity.ResourcePermissionRead |
	entity.ResourcePermissionWrite |
	entity.ResourcePermissionManage |
	entity.ResourcePermissionOwner

// ResourcePermissionUpdateRequest replaces the selected targets' grants for
// each resource. A zero permission revokes the grant.
type ResourcePermissionUpdateRequest struct {
	TenantID       string   `json:"tenant_id"`
	ResourceType   string   `json:"resource_type" binding:"required"`
	ResourceIDs    []string `json:"resource_ids"`
	ResourceID     string   `json:"resource_id,omitempty"`
	Permission     *int32   `json:"permission" binding:"required"`
	MemberList     []string `json:"member_list"`
	GroupList      []string `json:"group_list"`
	DepartmentList []string `json:"department_list,omitempty"`
}

// ResourceOwnershipTransferRequest transfers one resource to another Tenant member.
type ResourceOwnershipTransferRequest struct {
	TenantID     string `json:"tenant_id"`
	ResourceType string `json:"resource_type" binding:"required"`
	ResourceID   string `json:"resource_id" binding:"required"`
	UserID       string `json:"user_id" binding:"required"`
}

// ResourcePermissionListRequest selects one or more resources whose direct
// grants should be returned.
type ResourcePermissionListRequest struct {
	TenantID     string   `json:"tenant_id"`
	ResourceType string   `json:"resource_type" binding:"required"`
	ResourceIDs  []string `json:"resource_ids" binding:"required"`
}

// ResourcePermissionTargetListRequest selects one member or group and returns
// all of its direct resource grants in the current Tenant.
type ResourcePermissionTargetListRequest struct {
	TenantID   string `json:"tenant_id"`
	TargetID   string `json:"target_id" binding:"required"`
	TargetType string `json:"target_type" binding:"required"`
}

// ResourcePermissionTarget is the UI-facing view of one member or group and
// the resources granted to it.
type ResourcePermissionTarget struct {
	ID           string           `json:"id"`
	Role         string           `json:"role"`
	Name         string           `json:"name"`
	Avatar       string           `json:"avatar"`
	TenantID     string           `json:"tenant_id"`
	ResourceType string           `json:"resource_type"`
	Permissions  map[string]int32 `json:"permissions"`
}

// ResourcePermissionItem is one resource grant returned by the target query.
type ResourcePermissionItem struct {
	ResourceID      string   `json:"resource_id"`
	ResourceType    string   `json:"resource_type"`
	Name            string   `json:"name"`
	Avatar          string   `json:"avatar"`
	Permission      int32    `json:"permission"`
	PermissionNames []string `json:"permission_names,omitempty"`
	Editable        bool     `json:"editable"`
	ModuleType      string   `json:"module_type"`
}

type permissionResource struct {
	ID          string
	Type        string
	Name        string
	Avatar      string
	OwnerUserID string
}

type resourcePermissionTarget struct {
	ID     string
	Type   string
	UserID string
	Name   string
	Avatar string
}

// UpdateResourcePermissions grants, replaces, or revokes resource permissions
// for Tenant members and groups. Provider aliases expand to existing model
// instances so the permission table only stores instance-level grants.
func (s *TenantService) UpdateResourcePermissions(ctx context.Context, userID, tenantID string, request ResourcePermissionUpdateRequest) error {
	tenantID = strings.TrimSpace(tenantID)
	if request.TenantID != "" && strings.TrimSpace(request.TenantID) != tenantID {
		return common.NewCodedError(common.CodeBadRequest, "tenant_id must match the current Tenant")
	}
	if request.Permission == nil {
		return common.NewCodedError(common.CodeBadRequest, "permission is required")
	}
	if *request.Permission < 0 || *request.Permission&^resourcePermissionMask != 0 {
		return common.NewCodedError(common.CodeBadRequest, "permission contains unsupported bits")
	}
	if *request.Permission&entity.ResourcePermissionOwner != 0 {
		return common.NewCodedError(common.CodeBadRequest, "resource ownership must be changed using the ownership transfer API")
	}
	resourceIDs := uniqueNonEmpty(append(request.ResourceIDs, request.ResourceID))
	if len(resourceIDs) == 0 || len(resourceIDs) > maxPermissionBatchSize {
		return common.NewCodedError(common.CodeBadRequest, "resource_ids must contain between 1 and 200 items")
	}
	if strings.TrimSpace(request.ResourceType) == "" {
		return common.NewCodedError(common.CodeBadRequest, "resource_type is required")
	}
	if isSpecialPermissionResource(request.ResourceType) {
		return common.NewCodedError(common.CodeBadRequest, "group and tenant permissions must use their dedicated management APIs")
	}

	targetIDs := make([]struct{ kind, id string }, 0, len(request.MemberList)+len(request.GroupList)+len(request.DepartmentList))
	for _, id := range uniqueNonEmpty(request.MemberList) {
		targetIDs = append(targetIDs, struct{ kind, id string }{kind: "member", id: id})
	}
	for _, id := range uniqueNonEmpty(append(request.GroupList, request.DepartmentList...)) {
		targetIDs = append(targetIDs, struct{ kind, id string }{kind: "group", id: id})
	}
	if len(targetIDs) == 0 || len(targetIDs) > maxPermissionBatchSize {
		return common.NewCodedError(common.CodeBadRequest, "member_list or group_list must contain between 1 and 200 targets")
	}
	db := dao.DB.WithContext(ctx)
	return db.Transaction(func(tx *gorm.DB) error {
		actor, err := loadPermissionActor(ctx, tx, userID, tenantID)
		if err != nil {
			return err
		}

		resources := make([]permissionResource, 0, len(resourceIDs))
		for _, resourceID := range resourceIDs {
			resolved, err := resolvePermissionResources(tx, tenantID, request.ResourceType, resourceID)
			if err != nil {
				return err
			}
			resources = append(resources, resolved...)
		}
		if len(resources) == 0 {
			return common.NewCodedError(common.CodeNotFound, "No active resource instances were found")
		}
		if len(resources) > maxPermissionBatchSize {
			return common.NewCodedError(common.CodeBadRequest, "expanded resource list exceeds 200 items")
		}

		targets := make([]resourcePermissionTarget, 0, len(targetIDs))
		for _, target := range targetIDs {
			resolved, err := resolvePermissionTarget(tx, tenantID, target.kind, target.id)
			if err != nil {
				return err
			}
			if resolved.Type == "member" && resolved.UserID == userID {
				return common.NewCodedError(common.CodeConflict, "you cannot change your own resource permissions")
			}
			targets = append(targets, resolved)
		}

		for _, resource := range resources {
			if err := requireResourcePermissionManager(ctx, tx, actor, resource, false); err != nil {
				return err
			}
			if err := requireFeatureSharePermission(ctx, tx, actor, resource.Type); err != nil {
				return err
			}
			for _, target := range targets {
				if err := replaceResourcePermission(tx, actor, resource, target, *request.Permission, false); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// TransferResourceOwnership transfers one resource to another active Tenant member.
func (s *TenantService) TransferResourceOwnership(ctx context.Context, userID, tenantID string, request ResourceOwnershipTransferRequest) error {
	tenantID = strings.TrimSpace(tenantID)
	if request.TenantID != "" && strings.TrimSpace(request.TenantID) != tenantID {
		return common.NewCodedError(common.CodeBadRequest, "tenant_id must match the current Tenant")
	}
	if isSpecialPermissionResource(request.ResourceType) {
		return common.NewCodedError(common.CodeBadRequest, "group and tenant ownership use their dedicated management APIs")
	}
	if strings.TrimSpace(request.ResourceType) == "" || strings.TrimSpace(request.ResourceID) == "" || strings.TrimSpace(request.UserID) == "" {
		return common.NewCodedError(common.CodeBadRequest, "resource_type, resource_id, and user_id are required")
	}
	if strings.TrimSpace(request.UserID) == strings.TrimSpace(userID) {
		return common.NewCodedError(common.CodeConflict, "resource ownership cannot be transferred to yourself")
	}

	db := dao.DB.WithContext(ctx)
	return db.Transaction(func(tx *gorm.DB) error {
		actor, err := loadPermissionActor(ctx, tx, userID, tenantID)
		if err != nil {
			return err
		}
		resources, err := resolvePermissionResources(tx, tenantID, request.ResourceType, request.ResourceID)
		if err != nil {
			return err
		}
		if len(resources) != 1 {
			return common.NewCodedError(common.CodeBadRequest, "ownership transfer requires exactly one resource")
		}
		membership, err := findTenantMembership(tx, tenantID, strings.TrimSpace(request.UserID), true)
		if err != nil {
			return err
		}
		target, err := resolvePermissionTarget(tx, tenantID, "member", membership.ID)
		if err != nil {
			return err
		}
		if err := requireResourcePermissionManager(ctx, tx, actor, resources[0], true); err != nil {
			return err
		}
		if err := requireFeatureSharePermission(ctx, tx, actor, resources[0].Type); err != nil {
			return err
		}
		return replaceResourcePermission(tx, actor, resources[0], target, resourcePermissionMask, true)
	})
}

// ListResourcePermissions returns current direct grants for resources the
// caller is allowed to manage.
func (s *TenantService) ListResourcePermissions(ctx context.Context, userID, tenantID string, request ResourcePermissionListRequest) ([]*ResourcePermissionTarget, error) {
	tenantID = strings.TrimSpace(tenantID)
	if request.TenantID != "" && strings.TrimSpace(request.TenantID) != tenantID {
		return nil, common.NewCodedError(common.CodeBadRequest, "tenant_id must match the current Tenant")
	}
	resourceIDs := uniqueNonEmpty(request.ResourceIDs)
	if strings.TrimSpace(request.ResourceType) == "" || len(resourceIDs) == 0 || len(resourceIDs) > maxPermissionBatchSize {
		return nil, common.NewCodedError(common.CodeBadRequest, "resource_type and 1 to 200 resource_ids are required")
	}
	db := dao.DB.WithContext(ctx)
	actor, err := loadPermissionActor(ctx, db, userID, tenantID)
	if err != nil {
		return nil, err
	}
	resources := make([]permissionResource, 0, len(resourceIDs))
	for _, id := range resourceIDs {
		resolved, err := resolvePermissionResources(db, tenantID, request.ResourceType, id)
		if err != nil {
			return nil, err
		}
		resources = append(resources, resolved...)
	}
	if len(resources) == 0 {
		return []*ResourcePermissionTarget{}, nil
	}
	if len(resources) > maxPermissionBatchSize {
		return nil, common.NewCodedError(common.CodeBadRequest, "expanded resource list exceeds 200 items")
	}
	for _, resource := range resources {
		if err := requireResourcePermissionListAccess(ctx, db, actor, resource); err != nil {
			return nil, err
		}
	}

	resourceIDsByType := make(map[string][]string)
	resourceInfo := make(map[string]permissionResource, len(resources))
	for _, resource := range resources {
		resourceIDsByType[resource.Type] = append(resourceIDsByType[resource.Type], resource.ID)
		resourceInfo[resource.Type+":"+resource.ID] = resource
	}
	var grants []entity.Permission
	for resourceType, ids := range resourceIDsByType {
		var rows []entity.Permission
		if err := db.Where("tenant_id = ? AND resource_type = ? AND resource_id IN ? AND status = ? AND permission <> 0",
			tenantID, resourceType, ids, string(entity.StatusValid)).Order("id ASC").Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("list resource permission grants: %w", err)
		}
		grants = append(grants, rows...)
	}

	resultByTarget := make(map[string]*ResourcePermissionTarget)
	for _, grant := range grants {
		resourceID := permissionStringValue(grant.ResourceID)
		resource, ok := resourceInfo[grant.ResourceType+":"+resourceID]
		if !ok {
			continue
		}
		targetType, targetID := resourcePermissionGrantTarget(grant)
		if targetType == "" || targetID == "" {
			continue
		}
		target, err := resolvePermissionTarget(db, tenantID, targetType, targetID)
		if err != nil {
			continue
		}
		key := target.Type + ":" + target.ID
		entry := resultByTarget[key]
		if entry == nil {
			entry = &ResourcePermissionTarget{
				ID: target.ID, Role: target.Type, Name: target.Name, Avatar: target.Avatar,
				TenantID: tenantID, ResourceType: resource.Type, Permissions: make(map[string]int32),
			}
			resultByTarget[key] = entry
		}
		entry.Permissions[resourceID] |= grant.Permission
	}
	result := make([]*ResourcePermissionTarget, 0, len(resultByTarget))
	for _, entry := range resultByTarget {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Role != result[j].Role {
			return result[i].Role < result[j].Role
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

// ListResourcePermissionsByTarget returns the resource grants belonging to a
// single Tenant member or group.
func (s *TenantService) ListResourcePermissionsByTarget(ctx context.Context, userID, tenantID string, request ResourcePermissionTargetListRequest) ([]*ResourcePermissionItem, error) {
	tenantID = strings.TrimSpace(tenantID)
	if request.TenantID != "" && strings.TrimSpace(request.TenantID) != tenantID {
		return nil, common.NewCodedError(common.CodeBadRequest, "tenant_id must match the current Tenant")
	}
	targetType := strings.TrimSpace(request.TargetType)
	if targetType != "member" && targetType != "group" {
		if targetType == "department" {
			targetType = "group"
		} else {
			return nil, common.NewCodedError(common.CodeBadRequest, "target_type must be member or group")
		}
	}
	db := dao.DB.WithContext(ctx)
	actor, err := loadPermissionActor(ctx, db, userID, tenantID)
	if err != nil {
		return nil, err
	}
	if !actor.systemAdmin && actor.membership.Role != TenantRoleOwner {
		if _, _, err := tenantMemberPermission(ctx, db, userID, tenantID, entity.TenantPermissionManageRoles); err != nil {
			return nil, err
		}
	}
	if _, err := resolvePermissionTarget(db, tenantID, targetType, strings.TrimSpace(request.TargetID)); err != nil {
		return nil, err
	}
	var grants []entity.Permission
	query := db.Where("tenant_id = ? AND status = ? AND permission <> 0", tenantID, string(entity.StatusValid))
	if targetType == "member" {
		query = query.Where("member_id = ? AND (group_id IS NULL OR group_id = '')", request.TargetID)
	} else {
		query = query.Where("group_id = ? AND (member_id IS NULL OR member_id = '')", request.TargetID)
	}
	if err := query.Order("resource_type ASC").Order("resource_id ASC").Find(&grants).Error; err != nil {
		return nil, fmt.Errorf("list target resource permissions: %w", err)
	}
	itemsByResource := make(map[string]*ResourcePermissionItem, len(grants))
	for _, grant := range grants {
		resourceID := permissionStringValue(grant.ResourceID)
		if resourceID == "" {
			continue
		}
		resource, err := resolveStoredPermissionResource(db, tenantID, grant.ResourceType, resourceID)
		if err != nil {
			continue
		}
		key := resource.Type + ":" + resource.ID
		item := itemsByResource[key]
		if item == nil {
			item = &ResourcePermissionItem{
				ResourceID: resource.ID, ResourceType: resource.Type, Name: resource.Name,
				Avatar: resource.Avatar, ModuleType: resourceModuleType(resource.Type),
			}
			itemsByResource[key] = item
		}
		item.Permission |= grant.Permission
	}
	items := make([]*ResourcePermissionItem, 0, len(itemsByResource))
	for _, item := range itemsByResource {
		item.Editable = !isSpecialPermissionResource(item.ResourceType) && item.Permission&entity.ResourcePermissionOwner == 0
		if isSpecialPermissionResource(item.ResourceType) {
			item.PermissionNames = specialResourcePermissionNames(item.ResourceType, item.Permission)
		} else if !item.Editable {
			item.PermissionNames = []string{"owner"}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ResourceType != items[j].ResourceType {
			return items[i].ResourceType < items[j].ResourceType
		}
		return items[i].ResourceID < items[j].ResourceID
	})
	return items, nil
}

type permissionActor struct {
	userID      string
	tenantID    string
	systemAdmin bool
	membership  *entity.UserTenant
}

func loadPermissionActor(ctx context.Context, db *gorm.DB, userID, tenantID string) (*permissionActor, error) {
	userID = strings.TrimSpace(userID)
	tenantID = strings.TrimSpace(tenantID)
	if userID == "" || tenantID == "" {
		return nil, common.NewCodedError(common.CodeBadRequest, "user and current Tenant are required")
	}
	var user entity.User
	if err := db.Select("id", "is_superuser").Where("id = ?", userID).Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewCodedError(common.CodeUnauthorized, "User not found")
		}
		return nil, fmt.Errorf("load permission actor: %w", err)
	}
	systemAdmin := user.IsSuperuser != nil && *user.IsSuperuser
	actor := &permissionActor{userID: userID, tenantID: tenantID, systemAdmin: systemAdmin}
	if systemAdmin {
		return actor, nil
	}
	membership, _, err := tenantMemberPermission(ctx, db, userID, tenantID, 0)
	if err != nil {
		return nil, err
	}
	actor.membership = membership
	return actor, nil
}

func requireResourcePermissionManager(ctx context.Context, db *gorm.DB, actor *permissionActor, resource permissionResource, transferOwner bool) error {
	if actor.systemAdmin || actor.membership != nil && actor.membership.Role == TenantRoleOwner {
		return nil
	}
	mask, err := loadEffectiveResourcePermissionMask(ctx, db, actor, resource)
	if err != nil {
		return err
	}
	if mask&(entity.ResourcePermissionManage|entity.ResourcePermissionOwner) != 0 {
		if transferOwner && mask&entity.ResourcePermissionOwner == 0 {
			return common.NewCodedError(common.CodeForbidden, "Only a resource owner can transfer ownership")
		}
		return nil
	}
	if resource.OwnerUserID != "" && actor.userID == resource.OwnerUserID {
		hasOwner, err := resourceHasOwnerGrant(db, actor.tenantID, resource)
		if err != nil {
			return err
		}
		if !hasOwner {
			return nil
		}
	}
	if mask&(entity.ResourcePermissionManage|entity.ResourcePermissionOwner) == 0 {
		return common.NewCodedError(common.CodeForbidden, "Resource permission management denied")
	}
	return nil
}

func requireResourcePermissionListAccess(ctx context.Context, db *gorm.DB, actor *permissionActor, resource permissionResource) error {
	if actor.systemAdmin {
		return nil
	}
	switch resource.Type {
	case "group":
		_, err := requireGroupPermission(ctx, db, actor.userID, actor.tenantID, resource.ID, entity.GroupPermissionConfigureMemberPermissions)
		return err
	case "tenant":
		_, _, err := tenantMemberPermission(ctx, db, actor.userID, actor.tenantID, entity.TenantPermissionManageRoles)
		return err
	default:
		return requireResourcePermissionManager(ctx, db, actor, resource, false)
	}
}

func resourceHasOwnerGrant(db *gorm.DB, tenantID string, resource permissionResource) (bool, error) {
	var count int64
	if err := db.Model(&entity.Permission{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ? AND (permission & ?) <> 0",
			tenantID, resource.Type, resource.ID, string(entity.StatusValid), entity.ResourcePermissionOwner).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("check resource owner grant: %w", err)
	}
	return count > 0, nil
}

func loadEffectiveResourcePermissionMask(ctx context.Context, db *gorm.DB, actor *permissionActor, resource permissionResource) (int32, error) {
	var groupIDs []string
	if err := db.WithContext(ctx).Table("group_member").
		Select("group_member.group_id").
		Joins("JOIN group_info ON group_info.id = group_member.group_id").
		Where("group_member.member_id = ? AND group_member.status = ? AND group_info.tenant_id = ? AND group_info.status = ?",
			actor.membership.ID, entity.GroupMemberStatusJoined, actor.tenantID, string(entity.StatusValid)).
		Pluck("group_member.group_id", &groupIDs).Error; err != nil {
		return 0, fmt.Errorf("load resource permission groups: %w", err)
	}
	query := db.Model(&entity.Permission{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ?",
			actor.tenantID, resource.Type, resource.ID, string(entity.StatusValid))
	targets := db.Where("member_id = ? AND (group_id IS NULL OR group_id = '')", actor.membership.ID)
	if len(groupIDs) > 0 {
		targets = targets.Or("group_id IN ? AND (member_id IS NULL OR member_id = '')", groupIDs)
	}
	var grants []entity.Permission
	if err := query.Where(targets).Find(&grants).Error; err != nil {
		return 0, fmt.Errorf("load resource permission grants: %w", err)
	}
	var mask int32
	for _, grant := range grants {
		mask |= grant.Permission
	}
	return mask, nil
}

func requireFeatureSharePermission(ctx context.Context, db *gorm.DB, actor *permissionActor, resourceType string) error {
	if actor.systemAdmin {
		return nil
	}
	featureName := map[string]permission.Feature{
		"kb": "dataset", "document": "dataset", "dialog": "chat", "canvas": "agent",
		"file": "file", "search": "search", "memory": "memory", "mcp": "mcp",
	}[resourceType]
	if featureName == "" {
		return nil
	}
	ctx = permission.WithIdentity(ctx, actor.userID, actor.tenantID)
	if err := permission.NewDatabaseChecker(db).CheckFeature(ctx, featureName, permission.ActionShare); err != nil {
		if errors.Is(err, permission.ErrPermissionDenied) || errors.Is(err, permission.ErrMembershipNotFound) {
			return common.NewCodedError(common.CodeForbidden, "Tenant role does not allow resource sharing")
		}
		return err
	}
	return nil
}

func replaceResourcePermission(tx *gorm.DB, actor *permissionActor, resource permissionResource, target resourcePermissionTarget, newMask int32, ownershipTransfer bool) error {
	if target.Type == "group" && newMask&entity.ResourcePermissionOwner != 0 {
		return common.NewCodedError(common.CodeBadRequest, "A group cannot own a resource")
	}
	if newMask&entity.ResourcePermissionOwner != 0 && !ownershipTransfer {
		return common.NewCodedError(common.CodeBadRequest, "resource ownership must be changed using the ownership transfer API")
	}
	var rows []entity.Permission
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ?",
			actor.tenantID, resource.Type, resource.ID, string(entity.StatusValid))
	if target.Type == "member" {
		query = query.Where("member_id = ? AND (group_id IS NULL OR group_id = '')", target.ID)
	} else {
		query = query.Where("group_id = ? AND (member_id IS NULL OR member_id = '')", target.ID)
	}
	if err := query.Order("id ASC").Find(&rows).Error; err != nil {
		return fmt.Errorf("load existing resource permission: %w", err)
	}
	var oldMask int32
	for _, row := range rows {
		oldMask |= row.Permission
	}
	if oldMask&entity.ResourcePermissionOwner != 0 && !ownershipTransfer {
		return common.NewCodedError(common.CodeConflict, "resource ownership must be changed using the ownership transfer API")
	}
	if ownershipTransfer && newMask&entity.ResourcePermissionOwner == 0 {
		return common.NewCodedError(common.CodeBadRequest, "ownership transfer must assign the owner permission")
	}
	if oldMask == newMask && len(rows) <= 1 {
		return nil
	}
	if newMask&entity.ResourcePermissionOwner != 0 {
		if err := transferResourceOwner(tx, actor, resource, target); err != nil {
			return err
		}
		// Ownership transfer may have updated the target's existing row.
		rows = nil
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ? AND member_id = ? AND (group_id IS NULL OR group_id = '')",
				actor.tenantID, resource.Type, resource.ID, string(entity.StatusValid), target.ID).
			Order("id ASC").Find(&rows).Error; err != nil {
			return fmt.Errorf("reload target resource permission: %w", err)
		}
		oldMask = 0
		for _, row := range rows {
			oldMask |= row.Permission
		}
		if oldMask == newMask {
			return nil
		}
	}
	if newMask == 0 {
		if len(rows) == 0 {
			return nil
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		if err := tx.Model(&entity.Permission{}).Where("id IN ?", ids).Update("status", string(entity.StatusInvalid)).Error; err != nil {
			return fmt.Errorf("revoke resource permission: %w", err)
		}
	} else if len(rows) == 0 {
		resourceID := resource.ID
		status := string(entity.StatusValid)
		grant := entity.Permission{ID: common.GenerateUUID(), TenantID: actor.tenantID, ResourceType: resource.Type,
			ResourceID: &resourceID, Permission: newMask, Status: &status}
		if target.Type == "member" {
			grant.MemberID = &target.ID
		} else {
			grant.GroupID = &target.ID
		}
		if err := tx.Create(&grant).Error; err != nil {
			return fmt.Errorf("create resource permission: %w", err)
		}
	} else {
		if err := tx.Model(&entity.Permission{}).Where("id = ?", rows[0].ID).Updates(map[string]interface{}{
			"permission": newMask, "status": string(entity.StatusValid),
		}).Error; err != nil {
			return fmt.Errorf("update resource permission: %w", err)
		}
		if len(rows) > 1 {
			ids := make([]string, 0, len(rows)-1)
			for _, row := range rows[1:] {
				ids = append(ids, row.ID)
			}
			if err := tx.Model(&entity.Permission{}).Where("id IN ?", ids).Update("status", string(entity.StatusInvalid)).Error; err != nil {
				return fmt.Errorf("remove duplicate resource permissions: %w", err)
			}
		}
	}
	return recordResourcePermissionChange(tx, actor, resource, target, oldMask, newMask)
}

func transferResourceOwner(tx *gorm.DB, actor *permissionActor, resource permissionResource, target resourcePermissionTarget) error {
	var grants []entity.Permission
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ? AND member_id IS NOT NULL AND member_id <> '' AND (group_id IS NULL OR group_id = '')",
			actor.tenantID, resource.Type, resource.ID, string(entity.StatusValid)).Order("id ASC").Find(&grants).Error; err != nil {
		return fmt.Errorf("load resource owners: %w", err)
	}
	hadOwnerGrant := false
	for _, grant := range grants {
		if grant.Permission&entity.ResourcePermissionOwner == 0 {
			continue
		}
		hadOwnerGrant = true
		if permissionStringValue(grant.MemberID) == target.ID {
			continue
		}
		newMask := grant.Permission &^ entity.ResourcePermissionOwner
		values := map[string]interface{}{"permission": newMask}
		if newMask == 0 {
			values["status"] = string(entity.StatusInvalid)
		}
		if err := tx.Model(&entity.Permission{}).Where("id = ?", grant.ID).Updates(values).Error; err != nil {
			return fmt.Errorf("remove previous resource owner: %w", err)
		}
		previous, err := resolvePermissionTarget(tx, actor.tenantID, "member", permissionStringValue(grant.MemberID))
		if err != nil {
			previous = resourcePermissionTarget{ID: permissionStringValue(grant.MemberID), Type: "member"}
		}
		if err := recordResourcePermissionChange(tx, actor, resource, previous, grant.Permission, newMask); err != nil {
			return err
		}
	}
	if !hadOwnerGrant && resource.OwnerUserID != "" {
		var membership entity.UserTenant
		if err := tx.Where("user_id = ? AND tenant_id = ? AND status = ?", resource.OwnerUserID, actor.tenantID, string(entity.StatusValid)).Take(&membership).Error; err == nil {
			if membership.ID != target.ID {
				previous, err := resolvePermissionTarget(tx, actor.tenantID, "member", membership.ID)
				if err != nil {
					return err
				}
				if err := recordResourcePermissionChange(tx, actor, resource, previous, resourcePermissionMask, 0); err != nil {
					return err
				}
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("load current resource owner's Tenant membership: %w", err)
		}
	}
	return nil
}

func recordResourcePermissionChange(tx *gorm.DB, actor *permissionActor, resource permissionResource, target resourcePermissionTarget, oldMask, newMask int32) error {
	if oldMask == newMask {
		return nil
	}
	actionType := "update"
	eventType := resourcePermissionEventUpdated
	if oldMask == 0 {
		actionType = "add"
		eventType = resourcePermissionEventGranted
	} else if newMask == 0 {
		actionType = "delete"
		eventType = resourcePermissionEventRevoked
	}
	if newMask&entity.ResourcePermissionOwner != 0 && oldMask&entity.ResourcePermissionOwner == 0 ||
		oldMask&entity.ResourcePermissionOwner != 0 && newMask&entity.ResourcePermissionOwner == 0 {
		eventType = resourceOwnershipEventTransfer
	}
	before, err := json.Marshal(map[string]interface{}{"permission": oldMask})
	if err != nil {
		return fmt.Errorf("encode previous resource permission: %w", err)
	}
	after, err := json.Marshal(map[string]interface{}{"permission": newMask})
	if err != nil {
		return fmt.Errorf("encode updated resource permission: %w", err)
	}
	actorID := actor.userID
	tenantScope := "tenant"
	operatorID := ""
	if actor.systemAdmin {
		tenantScope = "system"
	} else if actor.membership != nil {
		operatorID = actor.membership.ID
	}
	beforeValue, afterValue := string(before), string(after)
	status := string(entity.StatusValid)
	log := entity.PermissionChangeLog{
		ID: common.GenerateUUID(), TenantID: actor.tenantID, OperatorID: operatorID,
		TargetType: target.Type, TargetID: target.ID, ResourceType: resource.Type, ResourceID: resource.ID,
		OldPermission: oldMask, NewPermission: newMask, ActionType: actionType, EventType: eventType,
		ActorUserID: &actorID, ScopeType: tenantScope, BeforeData: &beforeValue, AfterData: &afterValue, Status: &status,
	}
	if err := tx.Create(&log).Error; err != nil {
		return fmt.Errorf("record resource permission change: %w", err)
	}
	return nil
}

func resolvePermissionResources(db *gorm.DB, tenantID, resourceType, requestedID string) ([]permissionResource, error) {
	requestedID = strings.TrimSpace(requestedID)
	resourceType = strings.ToLower(strings.TrimSpace(resourceType))
	if requestedID == "" {
		return nil, common.NewCodedError(common.CodeBadRequest, "resource ID cannot be empty")
	}
	switch resourceType {
	case "kb", "dataset":
		var resource entity.Knowledgebase
		if err := db.Where("id = ? AND tenant_id = ? AND status = ?", requestedID, tenantID, string(entity.StatusValid)).Take(&resource).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		return []permissionResource{{ID: resource.ID, Type: "kb", Name: resource.Name, Avatar: pointerValue(resource.Avatar), OwnerUserID: resource.CreatedBy}}, nil
	case "dialog", "chat":
		var resource entity.Chat
		if err := db.Where("id = ? AND tenant_id = ? AND status = ?", requestedID, tenantID, string(entity.StatusValid)).Take(&resource).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		return []permissionResource{{ID: resource.ID, Type: "dialog", Name: pointerValue(resource.Name), Avatar: pointerValue(resource.Icon)}}, nil
	case "document":
		var resource entity.Document
		if err := db.Where("id = ? AND status = ?", requestedID, string(entity.StatusValid)).Take(&resource).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		var dataset entity.Knowledgebase
		if err := db.Select("id", "tenant_id").Where("id = ? AND tenant_id = ? AND status = ?", resource.KbID, tenantID, string(entity.StatusValid)).Take(&dataset).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		return []permissionResource{{ID: resource.ID, Type: "document", Name: pointerValue(resource.Name), Avatar: pointerValue(resource.Thumbnail), OwnerUserID: resource.CreatedBy}}, nil
	case "canvas", "agent":
		var resource entity.UserCanvas
		if err := db.Where("id = ? AND tenant_id = ?", requestedID, tenantID).Take(&resource).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		return []permissionResource{{ID: resource.ID, Type: "canvas", Name: pointerValue(resource.Title), Avatar: pointerValue(resource.Avatar), OwnerUserID: resource.UserID}}, nil
	case "memory":
		var resource entity.Memory
		if err := db.Where("id = ? AND tenant_id = ?", requestedID, tenantID).Take(&resource).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		return []permissionResource{{ID: resource.ID, Type: "memory", Name: resource.Name, Avatar: pointerValue(resource.Avatar)}}, nil
	case "file", "folder":
		var resource entity.File
		if err := db.Where("id = ? AND tenant_id = ?", requestedID, tenantID).Take(&resource).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		if resource.ID == resource.ParentID || resource.ParentID == "" {
			return nil, common.NewCodedError(common.CodeBadRequest, "The Tenant root folder cannot be shared")
		}
		if resourceType == "folder" && resource.Type != "folder" || resourceType == "file" && resource.Type == "folder" {
			return nil, common.NewCodedError(common.CodeNotFound, "Resource is not available in the current Tenant")
		}
		return []permissionResource{{ID: resource.ID, Type: "file", Name: resource.Name, OwnerUserID: resource.CreatedBy}}, nil
	case "search":
		var resource entity.Search
		if err := db.Where("id = ? AND tenant_id = ? AND status = ?", requestedID, tenantID, string(entity.StatusValid)).Take(&resource).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		return []permissionResource{{ID: resource.ID, Type: "search", Name: resource.Name, Avatar: pointerValue(resource.Avatar), OwnerUserID: resource.CreatedBy}}, nil
	case "mcp":
		var resource entity.MCPServer
		if err := db.Where("id = ? AND tenant_id = ?", requestedID, tenantID).Take(&resource).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		return []permissionResource{{ID: resource.ID, Type: "mcp", Name: resource.Name}}, nil
	case "instance", "model_instance":
		resource, err := resolveModelInstance(db, tenantID, requestedID)
		if err != nil {
			return nil, err
		}
		return []permissionResource{resource}, nil
	case "group":
		var resource entity.GroupInfo
		if err := db.Where("id = ? AND tenant_id = ? AND status = ?", requestedID, tenantID, string(entity.StatusValid)).Take(&resource).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		return []permissionResource{{ID: resource.ID, Type: "group", Name: pointerValue(resource.Name), Avatar: pointerValue(resource.Avatar)}}, nil
	case "tenant":
		var resource entity.Tenant
		if err := db.Where("id = ? AND status = ?", requestedID, string(entity.StatusValid)).Take(&resource).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		if resource.ID != tenantID {
			return nil, common.NewCodedError(common.CodeNotFound, "Resource is not available in the current Tenant")
		}
		return []permissionResource{{ID: resource.ID, Type: "tenant", Name: pointerValue(resource.Name)}}, nil
	case "llm", "model_provider":
		var provider entity.TenantModelProvider
		if err := db.Where("tenant_id = ? AND (id = ? OR provider_name = ?)", tenantID, requestedID, requestedID).Take(&provider).Error; err != nil {
			return nil, permissionResourceNotFound(err)
		}
		var instances []entity.TenantModelInstance
		if err := db.Where("provider_id = ? AND status = ?", provider.ID, "active").Order("instance_name ASC").Order("id ASC").Find(&instances).Error; err != nil {
			return nil, fmt.Errorf("list model instances for provider: %w", err)
		}
		resources := make([]permissionResource, 0, len(instances))
		for _, instance := range instances {
			resources = append(resources, permissionResource{ID: instance.ID, Type: "model_instance", Name: instance.InstanceName, OwnerUserID: provider.UserID})
		}
		return resources, nil
	default:
		return nil, common.NewCodedError(common.CodeBadRequest, "Unsupported resource_type")
	}
}

func resolveModelInstance(db *gorm.DB, tenantID, instanceID string) (permissionResource, error) {
	var instance entity.TenantModelInstance
	if err := db.Where("id = ? AND status = ?", instanceID, "active").Take(&instance).Error; err != nil {
		return permissionResource{}, permissionResourceNotFound(err)
	}
	var provider entity.TenantModelProvider
	if err := db.Where("id = ? AND tenant_id = ?", instance.ProviderID, tenantID).Take(&provider).Error; err != nil {
		return permissionResource{}, permissionResourceNotFound(err)
	}
	return permissionResource{ID: instance.ID, Type: "model_instance", Name: instance.InstanceName, OwnerUserID: provider.UserID}, nil
}

func resolveStoredPermissionResource(db *gorm.DB, tenantID, resourceType, resourceID string) (permissionResource, error) {
	switch resourceType {
	case "group", "tenant":
		resources, err := resolvePermissionResources(db, tenantID, resourceType, resourceID)
		if err != nil {
			return permissionResource{}, err
		}
		return resources[0], nil
	case "file":
		var resource entity.File
		if err := db.Where("id = ? AND tenant_id = ?", resourceID, tenantID).Take(&resource).Error; err != nil {
			return permissionResource{}, permissionResourceNotFound(err)
		}
		if resource.ID == resource.ParentID || resource.ParentID == "" {
			return permissionResource{}, common.NewCodedError(common.CodeNotFound, "Resource not found")
		}
		return permissionResource{ID: resource.ID, Type: "file", Name: resource.Name, OwnerUserID: resource.CreatedBy}, nil
	}
	return resolvePermissionResourceByCanonicalType(db, tenantID, resourceType, resourceID)
}

func resolvePermissionResourceByCanonicalType(db *gorm.DB, tenantID, resourceType, resourceID string) (permissionResource, error) {
	resources, err := resolvePermissionResources(db, tenantID, resourceType, resourceID)
	if err != nil {
		return permissionResource{}, err
	}
	if len(resources) == 0 {
		return permissionResource{}, common.NewCodedError(common.CodeNotFound, "Resource not found")
	}
	for _, resource := range resources {
		if resource.ID == resourceID {
			return resource, nil
		}
	}
	return resources[0], nil
}

func resolvePermissionTarget(db *gorm.DB, tenantID, targetType, targetID string) (resourcePermissionTarget, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return resourcePermissionTarget{}, common.NewCodedError(common.CodeBadRequest, "target_id cannot be empty")
	}
	switch targetType {
	case "member":
		var membership entity.UserTenant
		if err := db.Where("id = ? AND tenant_id = ? AND status = ?", targetID, tenantID, string(entity.StatusValid)).Take(&membership).Error; err != nil {
			return resourcePermissionTarget{}, permissionTargetNotFound(err)
		}
		var user entity.User
		if err := db.Select("id", "nickname", "email", "avatar").Where("id = ?", membership.UserID).Take(&user).Error; err != nil {
			return resourcePermissionTarget{}, permissionTargetNotFound(err)
		}
		name := strings.TrimSpace(user.Nickname)
		if name == "" {
			name = user.Email
		}
		return resourcePermissionTarget{ID: membership.ID, Type: "member", UserID: membership.UserID, Name: name, Avatar: pointerValue(user.Avatar)}, nil
	case "group":
		var group entity.GroupInfo
		if err := db.Where("id = ? AND tenant_id = ? AND status = ?", targetID, tenantID, string(entity.StatusValid)).Take(&group).Error; err != nil {
			return resourcePermissionTarget{}, permissionTargetNotFound(err)
		}
		return resourcePermissionTarget{ID: group.ID, Type: "group", Name: pointerValue(group.Name), Avatar: pointerValue(group.Avatar)}, nil
	default:
		return resourcePermissionTarget{}, common.NewCodedError(common.CodeBadRequest, "target_type must be member or group")
	}
}

func permissionResourceNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return common.NewCodedError(common.CodeNotFound, "Resource is not available in the current Tenant")
	}
	return fmt.Errorf("load resource: %w", err)
}

func permissionTargetNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return common.NewCodedError(common.CodeNotFound, "Target is not an active member or group in the current Tenant")
	}
	return fmt.Errorf("load permission target: %w", err)
}

func resourcePermissionGrantTarget(grant entity.Permission) (string, string) {
	memberID := permissionStringValue(grant.MemberID)
	groupID := permissionStringValue(grant.GroupID)
	if memberID != "" && groupID == "" {
		return "member", memberID
	}
	if groupID != "" && memberID == "" {
		return "group", groupID
	}
	return "", ""
}

func resourceModuleType(resourceType string) string {
	return map[string]string{
		"kb": "Dataset", "dialog": "Chat", "document": "Document", "canvas": "Agent",
		"file": "File", "memory": "Memory", "search": "Search", "mcp": "MCP", "model_instance": "Model",
		"group": "Group", "tenant": "Tenant",
	}[resourceType]
}

func isSpecialPermissionResource(resourceType string) bool {
	switch strings.ToLower(strings.TrimSpace(resourceType)) {
	case "group", "tenant":
		return true
	default:
		return false
	}
}

func specialResourcePermissionNames(resourceType string, permission int32) []string {
	switch resourceType {
	case "group":
		return groupPermissionNames(permission)
	case "tenant":
		names := tenantManagementPermissionNames(permission)
		if permission&entity.TenantPermissionOwner != 0 {
			names = append(names, "owner")
		}
		return names
	default:
		return nil
	}
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func permissionStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
