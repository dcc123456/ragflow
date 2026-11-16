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
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const groupPermissionResourceType = "group"

const groupPermissionOperationMask = entity.GroupPermissionAddSubgroup |
	entity.GroupPermissionDeleteSubgroup |
	entity.GroupPermissionAddMember |
	entity.GroupPermissionRemoveMember |
	entity.GroupPermissionConfigureMemberPermissions

var groupPermissionBits = []struct {
	name string
	mask int32
}{
	{name: "add_subgroup", mask: entity.GroupPermissionAddSubgroup},
	{name: "delete_subgroup", mask: entity.GroupPermissionDeleteSubgroup},
	{name: "add_member", mask: entity.GroupPermissionAddMember},
	{name: "remove_member", mask: entity.GroupPermissionRemoveMember},
	{name: "configure_member_permissions", mask: entity.GroupPermissionConfigureMemberPermissions},
	{name: "owner", mask: entity.GroupPermissionOwner},
}

// GroupService contains Tenant group operations available in Enterprise.
type GroupService struct{}

// GroupItem describes a group and the caller's effective management access.
type GroupItem struct {
	ID            string   `json:"id"`
	Name          *string  `json:"name,omitempty"`
	Description   *string  `json:"description,omitempty"`
	Avatar        *string  `json:"avatar,omitempty"`
	OwnerID       string   `json:"owner_id"`
	OwnerUserID   string   `json:"owner_user_id,omitempty"`
	TenantID      string   `json:"tenant_id"`
	ParentID      *string  `json:"parent_id,omitempty"`
	Path          *string  `json:"path,omitempty"`
	FormattedPath *string  `json:"formatted_path,omitempty"`
	Status        string   `json:"status"`
	IsOwner       bool     `json:"is_owner"`
	IsMember      bool     `json:"is_member"`
	Permission    int32    `json:"permission"`
	Permissions   []string `json:"permissions"`
	MemberCount   int64    `json:"member_count"`
}

// GroupMemberItem describes a Tenant user's group membership and permissions.
type GroupMemberItem struct {
	MemberID     string   `json:"member_id"`
	UserID       string   `json:"user_id"`
	Nickname     string   `json:"nickname"`
	Email        string   `json:"email"`
	Avatar       *string  `json:"avatar,omitempty"`
	Status       string   `json:"status"`
	TenantStatus string   `json:"tenant_status"`
	IsOwner      bool     `json:"is_owner"`
	Permission   int32    `json:"permission"`
	Permissions  []string `json:"permissions"`
}

// CreateGroupRequest contains the fields needed to create a root group or subgroup.
type CreateGroupRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Avatar      *string `json:"avatar,omitempty"`
	ParentID    *string `json:"parent_id,omitempty"`
}

// UpdateGroupRequest contains mutable group metadata.
type UpdateGroupRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Avatar      *string `json:"avatar,omitempty"`
}

// SetGroupMemberPermissionsRequest replaces one member's group-management permissions.
type SetGroupMemberPermissionsRequest struct {
	Permissions []string `json:"permissions"`
}

// NewGroupService creates the Tenant group service.
func NewGroupService() *GroupService {
	return &GroupService{}
}

// ListGroups returns groups the user joined or can manage in the current Tenant.
func (s *GroupService) ListGroups(ctx context.Context, userID, tenantID string) ([]*GroupItem, error) {
	db := dao.DB.WithContext(ctx)
	if err := requireGroupFeature(ctx, db, userID, tenantID, permission.ActionRead); err != nil {
		return nil, err
	}
	access, err := loadGroupAccess(ctx, db, userID, tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]*GroupItem, 0)
	for _, group := range access.groups {
		mask := access.masks[group.ID]
		if !access.memberships[group.ID] && mask == 0 {
			continue
		}
		item, err := makeGroupItem(db, &group, access.membership.ID, mask, access.memberships[group.ID])
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

// GetGroup returns one group visible to the user through membership or management access.
func (s *GroupService) GetGroup(ctx context.Context, userID, tenantID, groupID string) (*GroupItem, error) {
	db := dao.DB.WithContext(ctx)
	if err := requireGroupFeature(ctx, db, userID, tenantID, permission.ActionRead); err != nil {
		return nil, err
	}
	access, err := loadGroupAccess(ctx, db, userID, tenantID)
	if err != nil {
		return nil, err
	}
	group, ok := groupByID(access.groups, groupID)
	if !ok {
		return nil, common.NewCodedError(common.CodeNotFound, "Group not found")
	}
	mask := access.masks[group.ID]
	if !access.memberships[group.ID] && mask == 0 {
		return nil, common.NewCodedError(common.CodeNotFound, "Group not found")
	}
	return makeGroupItem(db, group, access.membership.ID, mask, access.memberships[group.ID])
}

// CreateGroup creates a root group or a child group, depending on parent_id.
func (s *GroupService) CreateGroup(ctx context.Context, userID, tenantID string, request CreateGroupRequest) (*GroupItem, error) {
	name, err := validateGroupName(request.Name)
	if err != nil {
		return nil, err
	}
	var group entity.GroupInfo
	var ownerMembershipID string
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		membership, _, err := tenantMemberPermission(ctx, tx, permission.TenantIDFromContext(ctx), 0)
		if err != nil {
			return err
		}
		featureCtx := permission.WithIdentity(ctx, userID, tenantID)
		if err := permission.NewDatabaseChecker(tx).CheckFeature(featureCtx, "group", permission.ActionWrite); err != nil {
			if errors.Is(err, permission.ErrPermissionDenied) {
				return common.NewCodedError(common.CodeForbidden, "Tenant role does not allow group creation")
			}
			return err
		}
		var tenant entity.Tenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NewCodedError(common.CodeNotFound, "Tenant not found")
			}
			return fmt.Errorf("load Tenant for group creation: %w", err)
		}

		var parent *entity.GroupInfo
		if request.ParentID != nil && strings.TrimSpace(*request.ParentID) != "" {
			parent, err = findActiveGroup(tx, tenantID, strings.TrimSpace(*request.ParentID), true)
			if err != nil {
				return err
			}
			if _, err := requireGroupPermission(ctx, tx, userID, tenantID, parent.ID, entity.GroupPermissionAddSubgroup); err != nil {
				return err
			}
		}
		if err := ensureUniqueGroupName(tx, tenantID, parentID(parent), name); err != nil {
			return err
		}

		groupID := common.GenerateUUID()
		path, formattedPath, err := buildGroupPaths(parent, groupID, name)
		if err != nil {
			return err
		}
		status := string(entity.StatusValid)
		group = entity.GroupInfo{
			ID:            groupID,
			Name:          &name,
			Description:   request.Description,
			Avatar:        request.Avatar,
			OwnerID:       membership.ID,
			TenantID:      tenantID,
			ParentID:      parentID(parent),
			Path:          &path,
			FormattedPath: &formattedPath,
			Status:        &status,
		}
		if err := tx.Create(&group).Error; err != nil {
			return fmt.Errorf("create group: %w", err)
		}
		ownerMembershipID = membership.ID
		if err := tx.Create(&entity.GroupMember{
			ID:       common.GenerateUUID(),
			MemberID: membership.ID,
			GroupID:  groupID,
			Status:   groupStringPointer(entity.GroupMemberStatusJoined),
		}).Error; err != nil {
			return fmt.Errorf("add group owner as a member: %w", err)
		}
		if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "group.created", "group", groupID, groupPermissionResourceType, groupID, "create", 0, 0, nil, groupAuditValue(&group)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return makeGroupItem(dao.DB.WithContext(ctx), &group, ownerMembershipID, groupPermissionOperationMask|entity.GroupPermissionOwner, true)
}

type groupAccess struct {
	membership  *entity.UserTenant
	tenantOwner bool
	groups      []entity.GroupInfo
	memberships map[string]bool
	masks       map[string]int32
}

func loadGroupAccess(ctx context.Context, db *gorm.DB, userID, tenantID string) (*groupAccess, error) {
	if err := requireGroupFeature(ctx, db, userID, tenantID, permission.ActionEnable); err != nil {
		return nil, err
	}
	membership, tenantMask, err := tenantMemberPermission(ctx, db, permission.TenantIDFromContext(ctx), 0)
	if err != nil {
		return nil, err
	}
	groups, err := loadActiveTenantGroups(db, tenantID)
	if err != nil {
		return nil, err
	}
	access := &groupAccess{
		membership:  membership,
		tenantOwner: membership.Role == "owner" || tenantMask&entity.TenantPermissionOwner != 0,
		groups:      groups,
		memberships: make(map[string]bool),
		masks:       make(map[string]int32, len(groups)),
	}
	if len(groups) == 0 {
		return access, nil
	}

	groupIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	var groupMembers []entity.GroupMember
	if err := db.Where("member_id = ? AND group_id IN ? AND status = ?", membership.ID, groupIDs, entity.GroupMemberStatusJoined).Find(&groupMembers).Error; err != nil {
		return nil, fmt.Errorf("load group memberships: %w", err)
	}
	for _, groupMember := range groupMembers {
		access.memberships[groupMember.GroupID] = true
	}

	query := db.Model(&entity.Permission{}).Where(
		"tenant_id = ? AND resource_type = ? AND resource_id IN ? AND status = ?",
		tenantID, groupPermissionResourceType, groupIDs, string(entity.StatusValid))
	targets := db.Where("member_id = ? AND (group_id IS NULL OR group_id = '')", membership.ID)
	if len(access.memberships) > 0 {
		memberGroupIDs := make([]string, 0, len(access.memberships))
		for groupID := range access.memberships {
			memberGroupIDs = append(memberGroupIDs, groupID)
		}
		targets = targets.Or("group_id IN ? AND (member_id IS NULL OR member_id = '')", memberGroupIDs)
	}
	var grants []entity.Permission
	if err := query.Where(targets).Find(&grants).Error; err != nil {
		return nil, fmt.Errorf("load group management permissions: %w", err)
	}
	for _, grant := range grants {
		if grant.ResourceID != nil {
			access.masks[*grant.ResourceID] |= grant.Permission
		}
	}
	for _, group := range groups {
		if access.tenantOwner || group.OwnerID == membership.ID || hasAncestorOwner(groups, group, membership.ID) {
			access.masks[group.ID] |= groupPermissionOperationMask | entity.GroupPermissionOwner
		}
		if access.masks[group.ID]&entity.GroupPermissionOwner != 0 {
			access.masks[group.ID] |= groupPermissionOperationMask
		}
	}
	return access, nil
}

func requireGroupFeature(ctx context.Context, db *gorm.DB, userID, tenantID string, action permission.Action) error {
	ctx = permission.WithIdentity(ctx, userID, tenantID)
	err := permission.NewDatabaseChecker(db).CheckFeature(ctx, "group", action)
	if errors.Is(err, permission.ErrPermissionDenied) || errors.Is(err, permission.ErrMembershipNotFound) {
		return common.NewCodedError(common.CodeForbidden, "Tenant role does not allow group access")
	}
	return err
}

func loadActiveTenantGroups(db *gorm.DB, tenantID string) ([]entity.GroupInfo, error) {
	var groups []entity.GroupInfo
	if err := db.Where("tenant_id = ? AND status = ?", tenantID, string(entity.StatusValid)).
		Order("path ASC").Order("create_time ASC").Order("id ASC").Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list Tenant groups: %w", err)
	}
	return groups, nil
}

func hasAncestorOwner(groups []entity.GroupInfo, group entity.GroupInfo, memberID string) bool {
	byID := make(map[string]*entity.GroupInfo, len(groups))
	for index := range groups {
		byID[groups[index].ID] = &groups[index]
	}
	seen := map[string]bool{group.ID: true}
	parentID := group.ParentID
	for parentID != nil && *parentID != "" {
		if seen[*parentID] {
			return false
		}
		seen[*parentID] = true
		parent, ok := byID[*parentID]
		if !ok {
			return false
		}
		if parent.OwnerID == memberID {
			return true
		}
		parentID = parent.ParentID
	}
	return false
}

func makeGroupItem(db *gorm.DB, group *entity.GroupInfo, currentMemberID string, mask int32, isMember bool) (*GroupItem, error) {
	var owner entity.UserTenant
	ownerUserID := ""
	if err := db.Select("user_id").Where("id = ? AND tenant_id = ?", group.OwnerID, group.TenantID).Take(&owner).Error; err == nil {
		ownerUserID = owner.UserID
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("load group owner: %w", err)
	}
	var memberCount int64
	if err := db.Table("group_member").
		Joins("JOIN user_tenant ON user_tenant.id = group_member.member_id").
		Where("group_member.group_id = ? AND group_member.status = ? AND (user_tenant.status = ? OR (user_tenant.status = ? AND user_tenant.role <> ''))",
			group.ID, entity.GroupMemberStatusJoined,
			entity.TenantMembershipStatusJoined, entity.TenantMembershipStatusLeft).
		Count(&memberCount).Error; err != nil {
		return nil, fmt.Errorf("count group members: %w", err)
	}
	return &GroupItem{
		ID:            group.ID,
		Name:          group.Name,
		Description:   group.Description,
		Avatar:        group.Avatar,
		OwnerID:       group.OwnerID,
		OwnerUserID:   ownerUserID,
		TenantID:      group.TenantID,
		ParentID:      group.ParentID,
		Path:          group.Path,
		FormattedPath: group.FormattedPath,
		Status:        groupStringValue(group.Status),
		IsOwner:       group.OwnerID == currentMemberID,
		IsMember:      isMember,
		Permission:    mask,
		Permissions:   groupPermissionNames(mask),
		MemberCount:   memberCount,
	}, nil
}

func groupPermissionNames(mask int32) []string {
	result := make([]string, 0, len(groupPermissionBits))
	for _, permission := range groupPermissionBits {
		if mask&permission.mask != 0 {
			result = append(result, permission.name)
		}
	}
	return result
}

func groupPermissionMask(names []string) (int32, error) {
	var mask int32
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "owner" {
			return 0, common.NewCodedError(common.CodeBadRequest, "The group owner permission is assigned by ownership transfer")
		}
		var found bool
		for _, permission := range groupPermissionBits {
			if permission.name == name {
				mask |= permission.mask
				found = true
				break
			}
		}
		if !found {
			return 0, common.NewCodedError(common.CodeBadRequest, "Unknown group permission: "+name)
		}
	}
	return mask, nil
}

func requireGroupPermission(ctx context.Context, db *gorm.DB, userID, tenantID, groupID string, required int32) (int32, error) {
	access, group, err := loadGroupForTenant(ctx, db, userID, tenantID, groupID)
	if err != nil {
		return 0, err
	}
	mask := access.masks[group.ID]
	if mask&required != required {
		return mask, common.NewCodedError(common.CodeForbidden, "Group permission denied")
	}
	return mask, nil
}

func loadGroupForTenant(ctx context.Context, db *gorm.DB, userID, tenantID, groupID string) (*groupAccess, *entity.GroupInfo, error) {
	access, err := loadGroupAccess(ctx, db, userID, tenantID)
	if err != nil {
		return nil, nil, err
	}
	group, ok := groupByID(access.groups, groupID)
	if !ok {
		return nil, nil, common.NewCodedError(common.CodeNotFound, "Group not found")
	}
	return access, group, nil
}

func findActiveGroup(db *gorm.DB, tenantID, groupID string, lock bool) (*entity.GroupInfo, error) {
	query := db.Model(&entity.GroupInfo{}).Where("id = ? AND tenant_id = ? AND status = ?", groupID, tenantID, string(entity.StatusValid))
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var group entity.GroupInfo
	if err := query.Take(&group).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewCodedError(common.CodeNotFound, "Group not found")
		}
		return nil, fmt.Errorf("load group: %w", err)
	}
	return &group, nil
}

func ensureUniqueGroupName(db *gorm.DB, tenantID string, parentID *string, name string) error {
	query := db.Model(&entity.GroupInfo{}).Where("tenant_id = ? AND status = ? AND LOWER(name) = LOWER(?)",
		tenantID, string(entity.StatusValid), name)
	if parentID == nil || *parentID == "" {
		query = query.Where("(parent_id IS NULL OR parent_id = '')")
	} else {
		query = query.Where("parent_id = ?", *parentID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return fmt.Errorf("check duplicate group name: %w", err)
	}
	if count > 0 {
		return common.NewCodedError(common.CodeConflict, "A group with this name already exists at the same level")
	}
	return nil
}

func validateGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", common.NewCodedError(common.CodeBadRequest, "Group name must contain between 1 and 100 characters")
	}
	return name, nil
}

func buildGroupPaths(parent *entity.GroupInfo, groupID, name string) (string, string, error) {
	if parent == nil {
		path := "/" + groupID + "/"
		formatted := "/" + name + "/"
		if len(path) > 255 || len(formatted) > 255 {
			return "", "", common.NewCodedError(common.CodeBadRequest, "Group path exceeds the maximum length")
		}
		return path, formatted, nil
	}
	path := groupStringValue(parent.Path) + groupID + "/"
	formatted := groupStringValue(parent.FormattedPath) + name + "/"
	if len(path) > 255 || len(formatted) > 255 {
		return "", "", common.NewCodedError(common.CodeBadRequest, "Group path exceeds the maximum length")
	}
	return path, formatted, nil
}

func groupByID(groups []entity.GroupInfo, groupID string) (*entity.GroupInfo, bool) {
	for index := range groups {
		if groups[index].ID == groupID {
			return &groups[index], true
		}
	}
	return nil, false
}

func parentID(group *entity.GroupInfo) *string {
	if group == nil {
		return nil
	}
	return &group.ID
}

func groupAuditValue(group *entity.GroupInfo) map[string]interface{} {
	return map[string]interface{}{
		"id": group.ID, "name": group.Name, "owner_id": group.OwnerID,
		"tenant_id": group.TenantID, "parent_id": group.ParentID,
		"path": group.Path, "formatted_path": group.FormattedPath, "status": group.Status,
	}
}

type groupPath struct {
	path      string
	formatted string
}

func groupSubtree(groups []entity.GroupInfo, rootID string) []entity.GroupInfo {
	byParent := make(map[string][]entity.GroupInfo, len(groups))
	for _, group := range groups {
		if group.ParentID != nil && *group.ParentID != "" {
			byParent[*group.ParentID] = append(byParent[*group.ParentID], group)
		}
	}
	root, ok := groupByID(groups, rootID)
	if !ok {
		return nil
	}
	result := []entity.GroupInfo{*root}
	for index := 0; index < len(result); index++ {
		result = append(result, byParent[result[index].ID]...)
	}
	return result
}

func updatedGroupPaths(groups []entity.GroupInfo, rootID string, newParentID, newName *string) (map[string]groupPath, error) {
	byID := make(map[string]*entity.GroupInfo, len(groups))
	for index := range groups {
		byID[groups[index].ID] = &groups[index]
	}
	root, ok := byID[rootID]
	if !ok {
		return nil, common.NewCodedError(common.CodeNotFound, "Group not found")
	}
	root.ParentID = newParentID
	if newName != nil {
		root.Name = newName
	}

	subtree := groupSubtree(groups, rootID)
	paths := make(map[string]groupPath, len(subtree))
	for _, item := range subtree {
		parent := (*entity.GroupInfo)(nil)
		if item.ParentID != nil && *item.ParentID != "" {
			parent = byID[*item.ParentID]
			if parent == nil {
				return nil, common.NewCodedError(common.CodeConflict, "Group hierarchy contains a missing parent")
			}
		}
		path, formatted, err := buildGroupPaths(parent, item.ID, groupStringValue(item.Name))
		if err != nil {
			return nil, err
		}
		byID[item.ID].Path = &path
		byID[item.ID].FormattedPath = &formatted
		paths[item.ID] = groupPath{path: path, formatted: formatted}
	}
	return paths, nil
}

func groupPathAuditValues(groups []entity.GroupInfo, paths map[string]groupPath) []map[string]interface{} {
	values := make([]map[string]interface{}, 0, len(groups))
	for _, group := range groups {
		value := map[string]interface{}{"id": group.ID, "parent_id": group.ParentID}
		if paths == nil {
			value["path"] = group.Path
			value["formatted_path"] = group.FormattedPath
		} else if path, ok := paths[group.ID]; ok {
			value["path"] = path.path
			value["formatted_path"] = path.formatted
		}
		values = append(values, value)
	}
	return values
}

func requireGroupOwner(ctx context.Context, db *gorm.DB, userID, tenantID, groupID string) (*groupAccess, *entity.GroupInfo, error) {
	access, group, err := loadGroupForTenant(ctx, db, userID, tenantID, groupID)
	if err != nil {
		return nil, nil, err
	}
	if !access.tenantOwner && access.masks[group.ID]&entity.GroupPermissionOwner == 0 {
		return nil, nil, common.NewCodedError(common.CodeForbidden, "Only the group owner or Tenant owner can change group settings")
	}
	return access, group, nil
}

func requireGroupDeletePermission(ctx context.Context, db *gorm.DB, userID, tenantID string, group *entity.GroupInfo, access *groupAccess) error {
	if access.tenantOwner || access.masks[group.ID]&entity.GroupPermissionOwner != 0 {
		return nil
	}
	if group.ParentID != nil && *group.ParentID != "" {
		_, err := requireGroupPermission(ctx, db, userID, tenantID, *group.ParentID, entity.GroupPermissionDeleteSubgroup)
		return err
	}
	return common.NewCodedError(common.CodeForbidden, "Group permission denied")
}

// UpdateGroup changes a group's name and metadata. Only its owner or the Tenant owner may do so.
func (s *GroupService) UpdateGroup(ctx context.Context, userID, tenantID, groupID string, request UpdateGroupRequest) (*GroupItem, error) {
	name, err := validateGroupName(request.Name)
	if err != nil {
		return nil, err
	}
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, group, err := requireGroupOwner(ctx, tx, userID, tenantID, groupID)
		if err != nil {
			return err
		}
		if err := ensureUniqueGroupNameExcept(tx, tenantID, group.ParentID, name, group.ID); err != nil {
			return err
		}
		before := groupAuditValue(group)
		groups, err := loadActiveTenantGroups(tx, tenantID)
		if err != nil {
			return err
		}
		subtree := groupSubtree(groups, group.ID)
		paths, err := updatedGroupPaths(groups, group.ID, group.ParentID, &name)
		if err != nil {
			return err
		}
		for _, item := range subtree {
			updates := map[string]interface{}{"formatted_path": paths[item.ID].formatted}
			if item.ID == group.ID {
				updates["name"] = name
				updates["description"] = request.Description
				updates["avatar"] = request.Avatar
			}
			if err := tx.Model(&entity.GroupInfo{}).Where("id = ? AND tenant_id = ?", item.ID, tenantID).Updates(updates).Error; err != nil {
				return fmt.Errorf("update group %s: %w", item.ID, err)
			}
		}
		group.Name = &name
		group.Description = request.Description
		group.Avatar = request.Avatar
		formattedPath := paths[group.ID].formatted
		group.FormattedPath = &formattedPath
		after := groupAuditValue(group)
		after["subtree"] = groupPathAuditValues(subtree, paths)
		if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "group.updated", "group", groupID, groupPermissionResourceType, groupID, "update", 0, 0, before, after); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetGroup(ctx, userID, tenantID, groupID)
}

// TransferGroupOwnership transfers a group's ownership to another active Tenant member.
func (s *GroupService) TransferGroupOwnership(ctx context.Context, userID, tenantID, groupID, targetUserID string) (*GroupItem, error) {
	targetUserID = strings.TrimSpace(targetUserID)
	if targetUserID == "" {
		return nil, common.NewCodedError(common.CodeBadRequest, "user_id is required")
	}
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		access, group, err := requireGroupOwner(ctx, tx, userID, tenantID, groupID)
		if err != nil {
			return err
		}
		target, err := findTenantMembership(tx, tenantID, targetUserID, true)
		if err != nil {
			return err
		}
		if target.ID == group.OwnerID {
			return nil
		}

		oldOwnerID := group.OwnerID
		oldOwnerUserID := ""
		var oldOwner entity.UserTenant
		if err := tx.Select("id", "user_id").Where("id = ? AND tenant_id = ?", oldOwnerID, tenantID).Take(&oldOwner).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("load previous group owner: %w", err)
			}
		} else {
			oldOwnerUserID = oldOwner.UserID
		}

		var targetGroupMember entity.GroupMember
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id = ? AND member_id = ?", groupID, target.ID).
			Order("create_time DESC").Take(&targetGroupMember).Error
		var targetMembershipBefore interface{}
		if err == nil {
			targetMembershipBefore = map[string]interface{}{"group_member_id": targetGroupMember.ID, "status": targetGroupMember.Status}
			if err := tx.Model(&entity.GroupMember{}).Where("id = ?", targetGroupMember.ID).
				Update("status", entity.GroupMemberStatusJoined).Error; err != nil {
				return fmt.Errorf("activate new group owner membership: %w", err)
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			targetGroupMember = entity.GroupMember{
				ID: common.GenerateUUID(), MemberID: target.ID, GroupID: groupID,
				Status: groupStringPointer(entity.GroupMemberStatusJoined),
			}
			if err := tx.Create(&targetGroupMember).Error; err != nil {
				return fmt.Errorf("add new group owner as a member: %w", err)
			}
		} else {
			return fmt.Errorf("load new group owner membership: %w", err)
		}

		if oldOwnerID != access.membership.ID {
			oldMask, err := loadStoredDirectGroupMemberPermission(tx, tenantID, groupID, oldOwnerID)
			if err != nil {
				return err
			}
			if oldMask&entity.GroupPermissionOwner != 0 {
				if _, err := replaceDirectGroupMemberPermission(ctx, tx, groupID, oldOwnerID, oldMask&^entity.GroupPermissionOwner); err != nil {
					return err
				}
			}
		}
		if _, err := replaceDirectGroupMemberPermission(ctx, tx, groupID, access.membership.ID, groupPermissionOperationMask); err != nil {
			return err
		}

		before := map[string]interface{}{
			"owner_member_id": oldOwnerID, "owner_user_id": oldOwnerUserID,
			"new_owner_membership": targetMembershipBefore,
		}
		after := map[string]interface{}{
			"owner_member_id": target.ID, "owner_user_id": target.UserID,
			"new_owner_membership": map[string]interface{}{"group_member_id": targetGroupMember.ID, "status": entity.GroupMemberStatusJoined},
		}
		if err := tx.Model(&entity.GroupInfo{}).Where("id = ? AND tenant_id = ?", groupID, tenantID).Update("owner_id", target.ID).Error; err != nil {
			return fmt.Errorf("transfer group ownership: %w", err)
		}
		return recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "group.owner.transferred", "group", groupID,
			groupPermissionResourceType, groupID, "transfer", entity.GroupPermissionOwner, entity.GroupPermissionOwner, before, after)
	})
	if err != nil {
		return nil, err
	}
	return s.GetGroup(ctx, userID, tenantID, groupID)
}

// MoveGroup reparents a group and updates its complete descendant path.
func (s *GroupService) MoveGroup(ctx context.Context, userID, tenantID, groupID string, newParentID *string) (*GroupItem, error) {
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		access, group, err := loadGroupForTenant(ctx, tx, userID, tenantID, groupID)
		if err != nil {
			return err
		}
		if newParentID != nil {
			trimmed := strings.TrimSpace(*newParentID)
			if trimmed == "" {
				newParentID = nil
			} else {
				newParentID = &trimmed
			}
		}
		if sameOptionalID(group.ParentID, newParentID) {
			return nil
		}
		subtree := groupSubtree(access.groups, group.ID)
		subtreeIDs := make(map[string]bool, len(subtree))
		for _, item := range subtree {
			subtreeIDs[item.ID] = true
		}
		var newParent *entity.GroupInfo
		if newParentID != nil {
			if subtreeIDs[*newParentID] {
				return common.NewCodedError(common.CodeBadRequest, "A group cannot be moved beneath itself or its descendants")
			}
			newParent, err = findActiveGroup(tx, tenantID, *newParentID, true)
			if err != nil {
				return err
			}
			if _, err := requireGroupPermission(ctx, tx, userID, tenantID, newParent.ID, entity.GroupPermissionAddSubgroup); err != nil {
				return err
			}
		}
		if group.ParentID != nil && *group.ParentID != "" {
			if _, err := requireGroupPermission(ctx, tx, userID, tenantID, *group.ParentID, entity.GroupPermissionDeleteSubgroup); err != nil {
				return err
			}
		} else if !access.tenantOwner && access.masks[group.ID]&entity.GroupPermissionOwner == 0 {
			return common.NewCodedError(common.CodeForbidden, "Only the root group owner or Tenant owner can move this group")
		}
		if err := ensureUniqueGroupNameExcept(tx, tenantID, newParentID, groupStringValue(group.Name), group.ID); err != nil {
			return err
		}

		before := groupPathAuditValues(subtree, nil)
		paths, err := updatedGroupPaths(access.groups, group.ID, newParentID, nil)
		if err != nil {
			return err
		}
		for _, item := range subtree {
			updates := map[string]interface{}{
				"path":           paths[item.ID].path,
				"formatted_path": paths[item.ID].formatted,
			}
			if item.ID == group.ID {
				updates["parent_id"] = newParentID
			}
			if err := tx.Model(&entity.GroupInfo{}).Where("id = ? AND tenant_id = ?", item.ID, tenantID).Updates(updates).Error; err != nil {
				return fmt.Errorf("move group %s: %w", item.ID, err)
			}
		}
		if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "group.moved", "group", groupID, groupPermissionResourceType, groupID, "update", 0, 0,
			map[string]interface{}{"parent_id": group.ParentID, "subtree": before},
			map[string]interface{}{"parent_id": newParentID, "subtree": groupPathAuditValues(subtree, paths)}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetGroup(ctx, userID, tenantID, groupID)
}

// DeleteGroup soft-deletes a group subtree and revokes its memberships and grants.
func (s *GroupService) DeleteGroup(ctx context.Context, userID, tenantID, groupID string) error {
	return dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		access, group, err := loadGroupForTenant(ctx, tx, userID, tenantID, groupID)
		if err != nil {
			return err
		}
		if err := requireGroupDeletePermission(ctx, tx, userID, tenantID, group, access); err != nil {
			return err
		}
		groups := groupSubtree(access.groups, group.ID)
		groupIDs := make([]string, 0, len(groups))
		for _, item := range groups {
			groupIDs = append(groupIDs, item.ID)
		}
		invalid := string(entity.StatusInvalid)

		var members []entity.GroupMember
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id IN ? AND status <> ?", groupIDs, entity.GroupMemberStatusRemoved).Find(&members).Error; err != nil {
			return fmt.Errorf("load group subtree members: %w", err)
		}
		var grants []entity.Permission
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND status = ? AND ((resource_type = ? AND resource_id IN ?) OR group_id IN ?)",
				tenantID, string(entity.StatusValid), groupPermissionResourceType, groupIDs, groupIDs).
			Find(&grants).Error; err != nil {
			return fmt.Errorf("load group subtree permissions: %w", err)
		}

		if err := tx.Model(&entity.GroupInfo{}).Where("tenant_id = ? AND id IN ?", tenantID, groupIDs).Update("status", invalid).Error; err != nil {
			return fmt.Errorf("soft-delete group subtree: %w", err)
		}
		if err := tx.Model(&entity.GroupMember{}).Where("group_id IN ? AND status <> ?", groupIDs, entity.GroupMemberStatusRemoved).
			Update("status", entity.GroupMemberStatusRemoved).Error; err != nil {
			return fmt.Errorf("remove group subtree members: %w", err)
		}
		if len(grants) > 0 {
			grantIDs := make([]string, 0, len(grants))
			for _, grant := range grants {
				grantIDs = append(grantIDs, grant.ID)
			}
			if err := tx.Model(&entity.Permission{}).Where("id IN ?", grantIDs).Update("status", invalid).Error; err != nil {
				return fmt.Errorf("revoke group subtree permissions: %w", err)
			}
		}
		for _, member := range members {
			if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "group.member.removed", "user_tenant", member.MemberID, groupPermissionResourceType, member.GroupID, "revoke", 0, 0,
				map[string]interface{}{"group_member_id": member.ID, "status": member.Status},
				map[string]interface{}{"group_member_id": member.ID, "status": entity.GroupMemberStatusRemoved}); err != nil {
				return err
			}
		}
		for _, grant := range grants {
			targetType, targetID := permissionTarget(grant)
			before := map[string]interface{}{"permission_id": grant.ID, "permission": grant.Permission, "status": grant.Status}
			after := map[string]interface{}{"permission_id": grant.ID, "permission": 0, "status": invalid}
			if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "group.permission.revoked", targetType, targetID, grant.ResourceType, groupStringValue(grant.ResourceID), "revoke", grant.Permission, 0, before, after); err != nil {
				return err
			}
		}
		for _, item := range groups {
			if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "group.deleted", "group", item.ID, groupPermissionResourceType, item.ID, "delete", 0, 0,
				groupAuditValue(&item), map[string]interface{}{"status": invalid}); err != nil {
				return err
			}
		}
		return nil
	})
}

func ensureUniqueGroupNameExcept(db *gorm.DB, tenantID string, parentID *string, name, excludeID string) error {
	query := db.Model(&entity.GroupInfo{}).Where("tenant_id = ? AND status = ? AND LOWER(name) = LOWER(?) AND id <> ?",
		tenantID, string(entity.StatusValid), name, excludeID)
	if parentID == nil || *parentID == "" {
		query = query.Where("(parent_id IS NULL OR parent_id = '')")
	} else {
		query = query.Where("parent_id = ?", *parentID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return fmt.Errorf("check duplicate group name: %w", err)
	}
	if count > 0 {
		return common.NewCodedError(common.CodeConflict, "A group with this name already exists at the same level")
	}
	return nil
}

func sameOptionalID(left, right *string) bool {
	if left == nil || *left == "" {
		return right == nil || *right == ""
	}
	return right != nil && *left == *right
}

func permissionTarget(grant entity.Permission) (string, string) {
	if grant.MemberID != nil {
		return "user_tenant", *grant.MemberID
	}
	if grant.GroupID != nil {
		return "group", *grant.GroupID
	}
	return "resource", groupStringValue(grant.ResourceID)
}

func groupStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

type groupMemberDetails struct {
	MemberID     string  `gorm:"column:member_id"`
	UserID       string  `gorm:"column:user_id"`
	Nickname     string  `gorm:"column:nickname"`
	Email        string  `gorm:"column:email"`
	Avatar       *string `gorm:"column:avatar"`
	Status       string  `gorm:"column:status"`
	TenantStatus string  `gorm:"column:tenant_status"`
}

// ListGroupMembers returns group members and invitation states, including users who left the Tenant.
func (s *GroupService) ListGroupMembers(ctx context.Context, userID, tenantID, groupID string) ([]*GroupMemberItem, error) {
	db := dao.DB.WithContext(ctx)
	if err := requireGroupFeature(ctx, db, userID, tenantID, permission.ActionRead); err != nil {
		return nil, err
	}
	access, group, err := loadGroupForTenant(ctx, db, userID, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if !access.memberships[groupID] && access.masks[groupID] == 0 {
		return nil, common.NewCodedError(common.CodeNotFound, "Group not found")
	}

	var rows []groupMemberDetails
	if err := db.Table("group_member").
		Select("group_member.member_id, user_tenant.user_id, user.nickname, user.email, user.avatar, group_member.status, user_tenant.status AS tenant_status").
		Joins("JOIN user_tenant ON user_tenant.id = group_member.member_id").
		Joins("JOIN user ON user.id = user_tenant.user_id").
		Where("group_member.group_id = ? AND group_member.status IN ? AND (user_tenant.status = ? OR (user_tenant.status = ? AND user_tenant.role <> ''))", groupID,
			[]string{entity.GroupMemberStatusJoined, entity.GroupMemberStatusInvited, entity.GroupMemberStatusRejected},
			entity.TenantMembershipStatusJoined, entity.TenantMembershipStatusLeft).
		Order("group_member.create_time ASC").Order("group_member.id ASC").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}

	result := make([]*GroupMemberItem, 0, len(rows))
	for _, row := range rows {
		mask, err := loadDirectGroupMemberPermission(db, tenantID, groupID, row.MemberID)
		if err != nil {
			return nil, err
		}
		isOwner := group.OwnerID == row.MemberID
		if isOwner {
			mask |= groupPermissionOperationMask | entity.GroupPermissionOwner
		}
		result = append(result, &GroupMemberItem{
			MemberID: row.MemberID, UserID: row.UserID, Nickname: row.Nickname,
			Email: row.Email, Avatar: row.Avatar, Status: row.Status, TenantStatus: row.TenantStatus,
			IsOwner: isOwner, Permission: mask, Permissions: groupPermissionNames(mask),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].IsOwner != result[j].IsOwner {
			return result[i].IsOwner
		}
		leftTenantMembership := result[i].TenantStatus == entity.TenantMembershipStatusLeft
		rightTenantMembership := result[j].TenantStatus == entity.TenantMembershipStatusLeft
		if leftTenantMembership != rightTenantMembership {
			return !leftTenantMembership
		}
		if len(result[i].Permissions) != len(result[j].Permissions) {
			return len(result[i].Permissions) > len(result[j].Permissions)
		}
		leftName := strings.ToLower(result[i].Nickname)
		if leftName == "" {
			leftName = strings.ToLower(result[i].Email)
		}
		rightName := strings.ToLower(result[j].Nickname)
		if rightName == "" {
			rightName = strings.ToLower(result[j].Email)
		}
		if leftName != rightName {
			return leftName < rightName
		}
		return result[i].UserID < result[j].UserID
	})
	return result, nil
}

// AddGroupMember immediately adds a current Tenant member to one group.
func (s *GroupService) AddGroupMember(ctx context.Context, userID, tenantID, groupID, targetUserID string) (*GroupMemberItem, error) {
	targetUserID = strings.TrimSpace(targetUserID)
	if targetUserID == "" {
		return nil, common.NewCodedError(common.CodeBadRequest, "user_id is required")
	}
	var memberID string
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := requireGroupPermission(ctx, tx, userID, tenantID, groupID, entity.GroupPermissionAddMember); err != nil {
			return err
		}
		target, err := findTenantMembership(tx, tenantID, targetUserID, true)
		if err != nil {
			return err
		}
		memberID = target.ID
		var group entity.GroupInfo
		if err := tx.Where("id = ? AND tenant_id = ? AND status = ?", groupID, tenantID, string(entity.StatusValid)).Take(&group).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NewCodedError(common.CodeNotFound, "Group not found")
			}
			return fmt.Errorf("load group for member addition: %w", err)
		}
		if group.OwnerID == memberID {
			return common.NewCodedError(common.CodeConflict, "The group owner is already a group member")
		}
		if group.ParentID != nil && *group.ParentID != "" {
			var parentMembership entity.GroupMember
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("group_id = ? AND member_id = ? AND status = ?", *group.ParentID, memberID, entity.GroupMemberStatusJoined).
				Take(&parentMembership).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NewCodedError(common.CodeForbidden, "User must belong to the parent group before joining this group")
			}
			if err != nil {
				return fmt.Errorf("check parent group membership: %w", err)
			}
		}

		var existing entity.GroupMember
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id = ? AND member_id = ?", groupID, memberID).Order("create_time DESC").Take(&existing).Error
		before := interface{}(nil)
		if err == nil {
			if existing.Status != nil && *existing.Status == entity.GroupMemberStatusJoined {
				return common.NewCodedError(common.CodeConflict, "User is already a group member")
			}
			before = map[string]interface{}{"group_member_id": existing.ID, "status": existing.Status}
			if err := tx.Model(&entity.GroupMember{}).Where("id = ?", existing.ID).
				Update("status", entity.GroupMemberStatusJoined).Error; err != nil {
				return fmt.Errorf("activate group membership: %w", err)
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			existing = entity.GroupMember{
				ID: common.GenerateUUID(), MemberID: memberID, GroupID: groupID,
				Status: groupStringPointer(entity.GroupMemberStatusJoined),
			}
			if err := tx.Create(&existing).Error; err != nil {
				return fmt.Errorf("create group membership: %w", err)
			}
		} else {
			return fmt.Errorf("load existing group membership: %w", err)
		}
		if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "group.member.added", "user", targetUserID,
			groupPermissionResourceType, groupID, "grant", 0, 0, before,
			map[string]interface{}{"group_member_id": existing.ID, "member_id": memberID, "status": entity.GroupMemberStatusJoined, "added_by": userID}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.groupMemberItem(ctx, tenantID, groupID, memberID)
}

// RemoveGroupMember removes a member from a group subtree; members may leave it themselves.
func (s *GroupService) RemoveGroupMember(ctx context.Context, userID, tenantID, groupID, targetUserID string) error {
	targetUserID = strings.TrimSpace(targetUserID)
	return dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		access, group, err := loadGroupForTenant(ctx, tx, userID, tenantID, groupID)
		if err != nil {
			return err
		}
		isSelfLeave := userID == targetUserID
		if !isSelfLeave && access.masks[group.ID]&entity.GroupPermissionRemoveMember == 0 {
			return common.NewCodedError(common.CodeForbidden, "Group permission denied")
		}
		membership, err := findTenantMembership(tx, tenantID, targetUserID, true)
		if err != nil {
			return err
		}
		groups := groupSubtree(access.groups, group.ID)
		groupIDs := make([]string, 0, len(groups))
		for _, item := range groups {
			groupIDs = append(groupIDs, item.ID)
			if item.OwnerID != membership.ID {
				continue
			}
			if item.ID == group.ID {
				return common.NewCodedError(common.CodeConflict, "Transfer group ownership before removing the owner")
			}
			groupName := groupStringValue(item.Name)
			if groupName == "" {
				groupName = item.ID
			}
			return common.NewCodedError(common.CodeConflict, fmt.Sprintf("Transfer ownership of descendant group %q before removing this member", groupName))
		}

		var groupMembers []entity.GroupMember
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id IN ? AND member_id = ? AND status <> ?", groupIDs, membership.ID, entity.GroupMemberStatusRemoved).
			Order("create_time DESC").Find(&groupMembers).Error; err != nil {
			return fmt.Errorf("load group subtree memberships: %w", err)
		}
		memberIDs := make([]string, 0, len(groupMembers))
		hasRootMembership := false
		rootMembershipStatus := ""
		for _, groupMember := range groupMembers {
			memberIDs = append(memberIDs, groupMember.ID)
			if groupMember.GroupID == group.ID {
				hasRootMembership = true
				rootMembershipStatus = groupStringValue(groupMember.Status)
			}
		}
		if !hasRootMembership {
			return common.NewCodedError(common.CodeNotFound, "Group member not found")
		}
		if isSelfLeave && rootMembershipStatus != entity.GroupMemberStatusJoined {
			return common.NewCodedError(common.CodeNotFound, "Joined group member not found")
		}
		if err := tx.Model(&entity.GroupMember{}).Where("id IN ?", memberIDs).Update("status", entity.GroupMemberStatusRemoved).Error; err != nil {
			return fmt.Errorf("remove group subtree memberships: %w", err)
		}
		var grants []entity.Permission
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND resource_type = ? AND resource_id IN ? AND member_id = ? AND status = ? AND (group_id IS NULL OR group_id = '')",
				tenantID, groupPermissionResourceType, groupIDs, membership.ID, string(entity.StatusValid)).
			Find(&grants).Error; err != nil {
			return fmt.Errorf("load member group subtree permissions: %w", err)
		}
		if len(grants) > 0 {
			grantIDs := make([]string, 0, len(grants))
			for _, grant := range grants {
				grantIDs = append(grantIDs, grant.ID)
			}
			if err := tx.Model(&entity.Permission{}).Where("id IN ?", grantIDs).Update("status", string(entity.StatusInvalid)).Error; err != nil {
				return fmt.Errorf("revoke member group subtree permissions: %w", err)
			}
		}
		eventType, actionType := "group.member.removed", "revoke"
		if isSelfLeave {
			eventType, actionType = "group.member.left", "leave"
		}
		for _, groupMember := range groupMembers {
			if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), eventType, "user", targetUserID,
				groupPermissionResourceType, groupMember.GroupID, actionType, 0, 0,
				map[string]interface{}{"group_member_id": groupMember.ID, "status": groupMember.Status},
				map[string]interface{}{"group_member_id": groupMember.ID, "status": entity.GroupMemberStatusRemoved}); err != nil {
				return err
			}
		}
		for _, grant := range grants {
			if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "group.member.permissions.revoked", "user", targetUserID,
				groupPermissionResourceType, groupStringValue(grant.ResourceID), "revoke", grant.Permission, 0,
				map[string]interface{}{"permission_id": grant.ID, "permission": grant.Permission},
				map[string]interface{}{"permission_id": grant.ID, "permission": 0, "status": entity.StatusInvalid}); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetGroupMemberPermissions replaces a joined member's management permissions for the group.
func (s *GroupService) SetGroupMemberPermissions(ctx context.Context, userID, tenantID, groupID, targetUserID string, names []string) (*GroupMemberItem, error) {
	newMask, err := groupPermissionMask(names)
	if err != nil {
		return nil, err
	}
	targetUserID = strings.TrimSpace(targetUserID)
	var targetMemberID string
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireGroupFeature(ctx, tx, userID, tenantID, permission.ActionShare); err != nil {
			return err
		}
		if _, err := requireGroupPermission(ctx, tx, userID, tenantID, groupID, entity.GroupPermissionConfigureMemberPermissions); err != nil {
			return err
		}
		target, err := findTenantMembership(tx, tenantID, targetUserID, true)
		if err != nil {
			return err
		}
		if target.UserID == userID {
			return common.NewCodedError(common.CodeConflict, "you cannot change your own group permissions")
		}
		targetMemberID = target.ID
		var group entity.GroupInfo
		if err := tx.Where("id = ? AND tenant_id = ? AND status = ?", groupID, tenantID, string(entity.StatusValid)).Take(&group).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NewCodedError(common.CodeNotFound, "Group not found")
			}
			return fmt.Errorf("load group for permission update: %w", err)
		}
		if group.OwnerID == target.ID {
			return common.NewCodedError(common.CodeConflict, "The group owner's permissions are determined by ownership")
		}
		var groupMember entity.GroupMember
		if err := tx.Where("group_id = ? AND member_id = ? AND status = ?", groupID, target.ID, entity.GroupMemberStatusJoined).
			Take(&groupMember).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NewCodedError(common.CodeNotFound, "Joined group member not found")
			}
			return fmt.Errorf("load joined group member: %w", err)
		}
		_, err = replaceDirectGroupMemberPermission(ctx, tx, groupID, target.ID, newMask)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.groupMemberItem(ctx, tenantID, groupID, targetMemberID)
}

func (s *GroupService) groupMemberItem(ctx context.Context, tenantID, groupID, memberID string) (*GroupMemberItem, error) {
	db := dao.DB.WithContext(ctx)
	var row groupMemberDetails
	if err := db.Table("group_member").
		Select("group_member.member_id, user_tenant.user_id, user.nickname, user.email, user.avatar, group_member.status, user_tenant.status AS tenant_status").
		Joins("JOIN user_tenant ON user_tenant.id = group_member.member_id").
		Joins("JOIN user ON user.id = user_tenant.user_id").
		Where("group_member.group_id = ? AND group_member.member_id = ? AND (user_tenant.status = ? OR (user_tenant.status = ? AND user_tenant.role <> ''))", groupID, memberID,
			entity.TenantMembershipStatusJoined, entity.TenantMembershipStatusLeft).
		Order("group_member.create_time DESC").Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewCodedError(common.CodeNotFound, "Group member not found")
		}
		return nil, fmt.Errorf("load group member details: %w", err)
	}
	mask, err := loadDirectGroupMemberPermission(db, tenantID, groupID, memberID)
	if err != nil {
		return nil, err
	}
	var group entity.GroupInfo
	if err := db.Select("owner_id").Where("id = ? AND tenant_id = ?", groupID, tenantID).Take(&group).Error; err != nil {
		return nil, fmt.Errorf("load group owner: %w", err)
	}
	isOwner := group.OwnerID == memberID
	if isOwner {
		mask |= groupPermissionOperationMask | entity.GroupPermissionOwner
	}
	return &GroupMemberItem{
		MemberID: row.MemberID, UserID: row.UserID, Nickname: row.Nickname, Email: row.Email,
		Avatar: row.Avatar, Status: row.Status, TenantStatus: row.TenantStatus, IsOwner: isOwner, Permission: mask,
		Permissions: groupPermissionNames(mask),
	}, nil
}

func loadDirectGroupMemberPermission(db *gorm.DB, tenantID, groupID, memberID string) (int32, error) {
	mask, err := loadStoredDirectGroupMemberPermission(db, tenantID, groupID, memberID)
	if err != nil {
		return 0, err
	}
	if mask&entity.GroupPermissionOwner != 0 {
		mask |= groupPermissionOperationMask
	}
	return mask, nil
}

func loadStoredDirectGroupMemberPermission(db *gorm.DB, tenantID, groupID, memberID string) (int32, error) {
	var rows []entity.Permission
	if err := db.Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND member_id = ? AND status = ? AND (group_id IS NULL OR group_id = '')",
		tenantID, groupPermissionResourceType, groupID, memberID, string(entity.StatusValid)).Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("load group member permissions: %w", err)
	}
	var mask int32
	for _, row := range rows {
		mask |= row.Permission
	}
	return mask, nil
}

func replaceDirectGroupMemberPermission(ctx context.Context, tx *gorm.DB, groupID, memberID string, newMask int32) (int32, error) {
	tenantID := permission.TenantIDFromContext(ctx)
	var rows []entity.Permission
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND member_id = ? AND status = ? AND (group_id IS NULL OR group_id = '')",
			tenantID, groupPermissionResourceType, groupID, memberID, string(entity.StatusValid)).
		Order("id ASC").Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("load existing group member permissions: %w", err)
	}
	var oldMask int32
	for _, row := range rows {
		oldMask |= row.Permission
	}
	if newMask == 0 {
		if len(rows) > 0 {
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			if err := tx.Model(&entity.Permission{}).Where("id IN ?", ids).Update("status", string(entity.StatusInvalid)).Error; err != nil {
				return oldMask, fmt.Errorf("revoke group member permissions: %w", err)
			}
		}
	} else if len(rows) == 0 {
		resourceID := groupID
		status := string(entity.StatusValid)
		if err := tx.Create(&entity.Permission{
			ID: common.GenerateUUID(), MemberID: &memberID, TenantID: tenantID,
			ResourceType: groupPermissionResourceType, ResourceID: &resourceID,
			Permission: newMask, Status: &status,
		}).Error; err != nil {
			return oldMask, fmt.Errorf("create group member permissions: %w", err)
		}
	} else {
		if err := tx.Model(&entity.Permission{}).Where("id = ?", rows[0].ID).Updates(map[string]interface{}{
			"permission": newMask, "status": string(entity.StatusValid),
		}).Error; err != nil {
			return oldMask, fmt.Errorf("update group member permissions: %w", err)
		}
		if len(rows) > 1 {
			duplicateIDs := make([]string, 0, len(rows)-1)
			for _, row := range rows[1:] {
				duplicateIDs = append(duplicateIDs, row.ID)
			}
			if err := tx.Model(&entity.Permission{}).Where("id IN ?", duplicateIDs).Update("status", string(entity.StatusInvalid)).Error; err != nil {
				return oldMask, fmt.Errorf("remove duplicate group member permissions: %w", err)
			}
		}
	}
	if oldMask != newMask {
		before := map[string]interface{}{"permission": oldMask, "permissions": groupPermissionNames(oldMask)}
		after := map[string]interface{}{"permission": newMask, "permissions": groupPermissionNames(newMask)}
		if err := recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx), "group.member.permissions.updated", "user_tenant", memberID,
			groupPermissionResourceType, groupID, "update", oldMask, newMask, before, after); err != nil {
			return oldMask, err
		}
	}
	return oldMask, nil
}

func groupStringPointer(value string) *string { return &value }
