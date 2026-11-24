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

package permission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/entity"

	"gorm.io/gorm"
)

type enterprisePolicy struct {
	source Source
	db     *gorm.DB
}

type enterpriseMembership struct {
	Membership   *Membership
	SystemRoleID *int32
	RoleValue    string
}

func newPolicyEE(source Source) (policy, bool) {
	database, ok := source.(*databaseSource)
	if !ok || database == nil || database.db == nil {
		return nil, false
	}
	return &enterprisePolicy{source: source, db: database.db}, true
}

func (p *enterprisePolicy) CheckFeature(ctx context.Context, feature Feature, action Action) error {
	var err error
	ctx, err = p.withTenantContext(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(feature)) == "" || !validAction(action) {
		return ErrInvalidPermission
	}
	membership, err := p.loadMembership(ctx)
	if err != nil {
		return err
	}
	return p.checkFeatureRoles(ctx, UserIDFromContext(ctx), membership, feature, action)
}

func (p *enterprisePolicy) CheckTenant(ctx context.Context, tenantID string, requirement TenantRequirement) error {
	var err error
	ctx, err = p.withTenantContext(ctx)
	if err != nil {
		return err
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || tenantID != TenantIDFromContext(ctx) || !validTenantRequirement(requirement) {
		return ErrPermissionDenied
	}
	membership, err := p.loadMembership(ctx)
	if err != nil {
		return err
	}
	if requirement == TenantMember || requirement == TenantNormalMember {
		return nil
	}
	mask, err := p.tenantManagementMask(ctx, membership)
	if err != nil {
		return err
	}
	switch requirement {
	case TenantOwner:
		if membership.RoleValue == "owner" || mask&entity.TenantPermissionOwner != 0 {
			return nil
		}
	case TenantAdmin:
		if membership.RoleValue == "owner" || mask&(entity.TenantPermissionManageMembers|entity.TenantPermissionManageRoles|entity.TenantPermissionOwner) != 0 {
			return nil
		}
	default:
		return ErrInvalidPermission
	}
	return ErrPermissionDenied
}

func (p *enterprisePolicy) CheckResource(ctx context.Context, ref ResourceRef, operation Operation) error {
	if !validOperation(operation) {
		return ErrInvalidPermission
	}
	access, err := p.ResolveAccess(ctx, ref)
	if err != nil {
		return err
	}
	if !access.Allows(operation) {
		return ErrPermissionDenied
	}
	return nil
}

func (p *enterprisePolicy) CheckResourcePermission(ctx context.Context, ref ResourceRef, required int32) error {
	if !validResourcePermission(required) {
		return ErrInvalidPermission
	}
	state, err := p.resolveResourcePermission(ctx, ref)
	if err != nil {
		return err
	}
	if !resourcePermissionAllows(state.mask, required) {
		return ErrPermissionDenied
	}
	return nil
}

func (p *enterprisePolicy) CheckDependency(ctx context.Context, entryRef, dependencyRef ResourceRef, entryOperation, dependencyOperation Operation) error {
	var err error
	ctx, err = p.withTenantContext(ctx)
	if err != nil {
		return err
	}
	if !validOperation(entryOperation) || !validOperation(dependencyOperation) {
		return ErrInvalidPermission
	}
	if err := p.CheckResource(ctx, entryRef, entryOperation); err != nil {
		return err
	}
	if err := p.CheckResource(ctx, dependencyRef, dependencyOperation); err != nil {
		return err
	}
	ownerID, err := p.resourceOwnerUserID(ctx, TenantIDFromContext(ctx), entryRef)
	if err != nil {
		return err
	}
	ownerCtx := WithIdentity(ctx, ownerID, TenantIDFromContext(ctx))
	if err := p.CheckResource(ownerCtx, dependencyRef, dependencyOperation); err != nil {
		if errors.Is(err, ErrPermissionDenied) || errors.Is(err, ErrMembershipNotFound) || errors.Is(err, ErrResourceNotFound) {
			return ErrPermissionDenied
		}
		return err
	}
	return nil
}

type enterpriseResourcePermission struct {
	resource   Resource
	mask       int32
	source     AccessSource
	membership *enterpriseMembership
	ctx        context.Context
}

func (p *enterprisePolicy) resolveResourcePermission(ctx context.Context, ref ResourceRef) (*enterpriseResourcePermission, error) {
	var err error
	ctx, err = p.withTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	resourceType, ok := enterpriseResourceType(ref.Kind)
	if !ok || !validResourceRef(ref) {
		return nil, ErrInvalidPermission
	}
	membership, err := p.loadMembership(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := p.source.GetResources(ctx, []ResourceRef{ref})
	if err != nil {
		return nil, fmt.Errorf("permission: load resource: %w", err)
	}
	if len(resources) != 1 {
		return nil, ErrResourceNotFound
	}
	resource := resources[0]
	if resource.TenantID != TenantIDFromContext(ctx) {
		return nil, ErrResourceNotFound
	}
	if !resource.Active {
		return nil, ErrResourceNotFound
	}
	resourceIDs, err := p.permissionResourceIDs(ctx, ref)
	if err != nil {
		return nil, err
	}
	resourceMask, source, err := p.effectiveResourceMask(ctx, membership, resourceType, resourceIDs)
	if err != nil {
		return nil, err
	}
	isTenantRootFolder := false
	if ref.Kind == ResourceKindFolder {
		isRoot, err := p.isTenantRootFolder(ctx, ref.ID, TenantIDFromContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("permission: check tenant root folder: %w", err)
		}
		isTenantRootFolder = isRoot
		if isRoot {
			resourceMask |= entity.ResourcePermissionRead | entity.ResourcePermissionWrite
			if source == "" {
				source = AccessSourceShared
			}
		}
	}
	if ref.Kind != ResourceKindModelProvider && !isTenantRootFolder && resource.OwnerUserID == UserIDFromContext(ctx) {
		hasOwnerGrant, err := p.hasResourceOwnerGrant(ctx, TenantIDFromContext(ctx), resourceType, resourceIDs)
		if err != nil {
			return nil, err
		}
		if !hasOwnerGrant {
			resourceMask |= entity.ResourcePermissionOwner
			source = AccessSourceOwner
		}
	}
	if ref.Kind == ResourceKindModelProvider && resource.OwnerUserID == UserIDFromContext(ctx) {
		resourceMask = entity.ResourcePermissionRead | entity.ResourcePermissionWrite | entity.ResourcePermissionManage | entity.ResourcePermissionOwner
		source = AccessSourceOwner
	}
	return &enterpriseResourcePermission{
		resource: resource, mask: resourceMask, source: source, membership: membership, ctx: ctx,
	}, nil
}

func (p *enterprisePolicy) ResolveAccess(ctx context.Context, ref ResourceRef) (Access, error) {
	state, err := p.resolveResourcePermission(ctx, ref)
	if err != nil {
		return Access{}, err
	}
	feature, hasFeature := enterpriseFeature(ref.Kind)
	featureMask := entity.RoleActionAll
	if hasFeature {
		featureMask, err = p.effectiveFeatureMask(state.ctx, UserIDFromContext(state.ctx), state.membership, feature)
		if err != nil {
			return Access{}, err
		}
		if featureMask&entity.RoleActionEnable == 0 {
			return Access{}, ErrPermissionDenied
		}
		if feature == "model_provider" {
			providerName, nameErr := p.modelProviderNameForResource(state.ctx, ref)
			if nameErr != nil {
				return Access{}, nameErr
			}
			providerEnabled, providerErr := p.modelProviderEnabledForRole(state.ctx, state.membership, providerName)
			if providerErr != nil {
				return Access{}, providerErr
			}
			if !providerEnabled {
				return Access{}, ErrPermissionDenied
			}
			featureMask |= entity.RoleActionRead | entity.RoleActionWrite | entity.RoleActionShare
		}
	}
	var integrationFeature Feature
	switch ref.Kind {
	case ResourceKindConnector, ResourceKindSyncTask:
		integrationFeature = "data_source"
	case ResourceKindChatChannel:
		integrationFeature = "chat_channel"
	}
	if integrationFeature != "" {
		integrationMask, featureErr := p.effectiveFeatureMask(
			state.ctx,
			UserIDFromContext(state.ctx),
			state.membership,
			integrationFeature,
		)
		if featureErr != nil {
			return Access{}, featureErr
		}
		if integrationMask&entity.RoleActionEnable == 0 {
			return Access{}, ErrPermissionDenied
		}
		item, itemErr := p.integrationItemForResource(state.ctx, ref)
		if itemErr != nil {
			return Access{}, itemErr
		}
		if item != "" {
			itemEnabled, itemErr := p.featureItemEnabledForRole(state.ctx, state.membership, integrationFeature, normalizeFeatureItem(string(integrationFeature), item))
			if itemErr != nil {
				return Access{}, itemErr
			}
			if !itemEnabled {
				return Access{}, ErrPermissionDenied
			}
		}
	}
	operations := operationsForMasks(featureMask, state.mask, hasFeature)
	if ref.Kind == ResourceKindModelProvider && state.resource.OwnerUserID != UserIDFromContext(state.ctx) {
		operations = retainReadOperations(operations)
	}
	if len(operations) == 0 {
		return Access{}, ErrPermissionDenied
	}
	return Access{
		Resource:       ref,
		TenantID:       TenantIDFromContext(state.ctx),
		Source:         state.source,
		PermissionMask: state.mask,
		Operations:     operations,
	}, nil
}

func (p *enterprisePolicy) integrationItemForResource(ctx context.Context, ref ResourceRef) (string, error) {
	var item string
	var query *gorm.DB
	switch ref.Kind {
	case ResourceKindConnector:
		query = p.db.WithContext(ctx).Model(&entity.Connector{}).Select("source").Where("id = ?", ref.ID)
	case ResourceKindSyncTask:
		query = p.db.WithContext(ctx).Model(&entity.SyncLogs{}).
			Select("connector.source").
			Joins("JOIN connector ON connector.id = sync_logs.connector_id").
			Where("sync_logs.id = ?", ref.ID)
	case ResourceKindChatChannel:
		query = p.db.WithContext(ctx).Model(&entity.ChatChannel{}).Select("channel").Where("id = ?", ref.ID)
	default:
		return "", nil
	}
	if err := query.Take(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrResourceNotFound
		}
		return "", fmt.Errorf("permission: resolve integration item for %s: %w", ref.Kind, err)
	}
	item = strings.TrimSpace(item)
	if item == "" {
		return "", ErrPermissionDenied
	}
	return item, nil
}

func (p *enterprisePolicy) ResolveAccesses(ctx context.Context, refs []ResourceRef) (map[ResourceRef]Access, error) {
	var err error
	ctx, err = p.withTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	refs = uniqueRefs(refs)
	if len(refs) == 0 {
		return map[ResourceRef]Access{}, nil
	}
	allMCPServers := true
	for _, ref := range refs {
		if ref.Kind != ResourceKindMCPServer {
			allMCPServers = false
			break
		}
	}
	if allMCPServers {
		return p.resolveMCPServerAccesses(ctx, refs)
	}

	accesses := make(map[ResourceRef]Access, len(refs))
	for _, ref := range refs {
		access, err := p.ResolveAccess(ctx, ref)
		if err == nil {
			accesses[ref] = access
			continue
		}
		if errors.Is(err, ErrPermissionDenied) || errors.Is(err, ErrMembershipNotFound) || errors.Is(err, ErrResourceNotFound) {
			continue
		}
		return nil, err
	}
	return accesses, nil
}

func (p *enterprisePolicy) resolveMCPServerAccesses(ctx context.Context, refs []ResourceRef) (map[ResourceRef]Access, error) {
	accesses := make(map[ResourceRef]Access, len(refs))
	if len(refs) == 0 {
		return accesses, nil
	}
	membership, err := p.loadMembership(ctx)
	if err != nil {
		return nil, err
	}
	featureMask, err := p.effectiveFeatureMask(ctx, UserIDFromContext(ctx), membership, "mcp")
	if err != nil {
		return nil, err
	}
	if featureMask&entity.RoleActionEnable == 0 {
		return accesses, nil
	}

	resources, err := p.source.GetResources(ctx, refs)
	if err != nil {
		return nil, fmt.Errorf("permission: load MCP server resources: %w", err)
	}
	resourcesByRef := make(map[ResourceRef]Resource, len(resources))
	resourceIDs := make([]string, 0, len(resources))
	for _, resource := range resources {
		if resource.Ref.Kind != ResourceKindMCPServer || resource.TenantID != TenantIDFromContext(ctx) || !resource.Active {
			continue
		}
		resourcesByRef[resource.Ref] = resource
		resourceIDs = append(resourceIDs, resource.Ref.ID)
	}
	resourceIDs = uniqueStringValues(resourceIDs)
	if len(resourceIDs) == 0 {
		return accesses, nil
	}

	groupIDs, err := p.memberGroupIDs(ctx, membership)
	if err != nil {
		return nil, err
	}
	grantQuery := p.db.WithContext(ctx).Model(&entity.Permission{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id IN ? AND status = ?",
			TenantIDFromContext(ctx), "mcp", resourceIDs, string(entity.StatusValid))
	memberTargets := p.db.Where("member_id = ? AND (group_id IS NULL OR group_id = '')", membership.Membership.ID)
	if len(groupIDs) > 0 {
		memberTargets = memberTargets.Or("group_id IN ? AND (member_id IS NULL OR member_id = '')", groupIDs)
	}
	var grants []entity.Permission
	if err := grantQuery.Where(memberTargets).Order("id ASC").Find(&grants).Error; err != nil {
		return nil, fmt.Errorf("permission: load MCP server grants: %w", err)
	}
	resourceMasks := make(map[string]int32, len(resourceIDs))
	accessSources := make(map[string]AccessSource, len(resourceIDs))
	for _, grant := range grants {
		if grant.ResourceID == nil {
			continue
		}
		resourceID := *grant.ResourceID
		resourceMasks[resourceID] |= grant.Permission
		if grant.MemberID != nil && *grant.MemberID == membership.Membership.ID && grant.Permission&entity.ResourcePermissionOwner != 0 {
			accessSources[resourceID] = AccessSourceOwner
		} else if accessSources[resourceID] != AccessSourceOwner {
			accessSources[resourceID] = AccessSourceShared
		}
	}

	var ownerGrantIDs []string
	if err := p.db.WithContext(ctx).Model(&entity.Permission{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id IN ? AND status = ? AND (permission & ?) <> 0",
			TenantIDFromContext(ctx), "mcp", resourceIDs, string(entity.StatusValid), entity.ResourcePermissionOwner).
		Distinct("resource_id").Pluck("resource_id", &ownerGrantIDs).Error; err != nil {
		return nil, fmt.Errorf("permission: load MCP server owner grants: %w", err)
	}
	hasOwnerGrant := make(map[string]struct{}, len(ownerGrantIDs))
	for _, resourceID := range ownerGrantIDs {
		hasOwnerGrant[resourceID] = struct{}{}
	}

	for ref, resource := range resourcesByRef {
		resourceMask := resourceMasks[ref.ID]
		source := accessSources[ref.ID]
		if resource.OwnerUserID == UserIDFromContext(ctx) {
			if _, exists := hasOwnerGrant[ref.ID]; !exists {
				resourceMask |= entity.ResourcePermissionOwner
				source = AccessSourceOwner
			}
		}
		operations := operationsForMasks(featureMask, resourceMask, true)
		if len(operations) == 0 {
			continue
		}
		accesses[ref] = Access{
			Resource:       ref,
			TenantID:       TenantIDFromContext(ctx),
			Source:         source,
			PermissionMask: resourceMask,
			Operations:     operations,
		}
	}
	return accesses, nil
}

func (p *enterprisePolicy) isTenantRootFolder(ctx context.Context, folderID, tenantID string) (bool, error) {
	var count int64
	if err := p.db.WithContext(ctx).Model(&entity.File{}).
		Where("id = ? AND tenant_id = ? AND type = ? AND (parent_id IS NULL OR parent_id = '' OR parent_id = id)", folderID, tenantID, "folder").
		Count(&count).Error; err != nil {
		return false, err
	}
	return count == 1, nil
}

func (p *enterprisePolicy) GrantResourceOwner(ctx context.Context, ref ResourceRef) error {
	var err error
	ctx, err = p.withTenantContext(ctx)
	if err != nil {
		return err
	}
	if ref.Kind == ResourceKindModelProvider {
		return nil
	}
	resourceType, ok := enterpriseResourceType(ref.Kind)
	if !ok || !validResourceRef(ref) {
		return ErrInvalidPermission
	}
	membership, err := p.loadMembership(ctx)
	if err != nil {
		return err
	}
	if _, err := p.loadResource(ctx, ref); err != nil {
		return err
	}
	resourceIDs, err := p.permissionResourceIDs(ctx, ref)
	if err != nil {
		return err
	}
	if len(resourceIDs) == 0 {
		return ErrResourceNotFound
	}
	status := string(entity.StatusValid)
	for _, resourceID := range resourceIDs {
		var grants []entity.Permission
		if err := p.db.WithContext(ctx).
			Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ? AND (permission & ?) <> 0",
				TenantIDFromContext(ctx), resourceType, resourceID, status, entity.ResourcePermissionOwner).
			Order("id ASC").Find(&grants).Error; err != nil {
			return fmt.Errorf("permission: check existing resource owner grant: %w", err)
		}
		if len(grants) > 0 {
			owned := false
			for _, grant := range grants {
				if grant.MemberID != nil && *grant.MemberID == membership.Membership.ID &&
					(grant.GroupID == nil || *grant.GroupID == "") {
					owned = true
					continue
				}
				return ErrPermissionDenied
			}
			if owned {
				continue
			}
		}
		resourceIDValue := resourceID
		memberID := membership.Membership.ID
		grant := entity.Permission{
			ID: common.GenerateUUID(), MemberID: &memberID, TenantID: TenantIDFromContext(ctx),
			ResourceType: resourceType, ResourceID: &resourceIDValue,
			Permission: entity.ResourcePermissionOwner, Status: &status,
		}
		if err := p.db.WithContext(ctx).Create(&grant).Error; err != nil {
			return fmt.Errorf("permission: create resource owner grant: %w", err)
		}
		beforeData, err := json.Marshal(map[string]interface{}{"permission": int32(0)})
		if err != nil {
			return fmt.Errorf("permission: encode owner grant audit before data: %w", err)
		}
		afterData, err := json.Marshal(map[string]interface{}{
			"permission":    entity.ResourcePermissionOwner,
			"owner_user_id": UserIDFromContext(ctx),
		})
		if err != nil {
			return fmt.Errorf("permission: encode owner grant audit after data: %w", err)
		}
		actorID, beforeValue, afterValue := UserIDFromContext(ctx), string(beforeData), string(afterData)
		log := entity.PermissionChangeLog{
			ID: common.GenerateUUID(), TenantID: TenantIDFromContext(ctx), OperatorID: membership.Membership.ID,
			TargetType: "member", TargetID: membership.Membership.ID,
			ResourceType: resourceType, ResourceID: resourceID,
			OldPermission: 0, NewPermission: entity.ResourcePermissionOwner,
			ActionType: "create", EventType: "resource.owner.assigned",
			ActorUserID: &actorID, ScopeType: "tenant", BeforeData: &beforeValue, AfterData: &afterValue, Status: &status,
		}
		if err := p.db.WithContext(ctx).Create(&log).Error; err != nil {
			return fmt.Errorf("permission: record resource owner grant: %w", err)
		}
	}
	return nil
}

func (p *enterprisePolicy) Scope(ctx context.Context, query ScopeQuery) (Scope, error) {
	var err error
	ctx, err = p.withTenantContext(ctx)
	if err != nil {
		return Scope{}, err
	}
	if strings.TrimSpace(string(query.Kind)) == "" || !validOperation(query.Operation) {
		return Scope{}, ErrInvalidPermission
	}
	if (query.Parent.ID == "") != (query.Parent.Kind == "") {
		return Scope{}, ErrInvalidPermission
	}
	if query.Entry != nil && (!validResourceRef(*query.Entry) || !validOperation(query.EntryOp)) {
		return Scope{}, ErrInvalidPermission
	}
	if query.Parent.Kind != "" {
		if err := p.CheckResource(ctx, query.Parent, OperationRead); err != nil {
			return Scope{}, err
		}
	}
	if _, err := p.loadMembership(ctx); err != nil {
		return Scope{}, err
	}
	var refs []ResourceRef
	if query.Kind == ResourceKindChat || query.Kind == ResourceKindSearchApp {
		refs, err = p.listTenantResourceRefs(ctx, query.Kind)
	} else {
		refs, err = p.source.ListResourceRefs(ctx, query.Kind, query.Parent)
	}
	if err != nil {
		return Scope{}, fmt.Errorf("permission: list resource candidates: %w", err)
	}
	refs = uniqueRefs(refs)
	scope := Scope{TenantID: TenantIDFromContext(ctx), Kind: query.Kind, Parent: query.Parent, Operation: query.Operation}
	for _, ref := range refs {
		var checkErr error
		if query.Entry == nil {
			checkErr = p.CheckResource(ctx, ref, query.Operation)
		} else {
			checkErr = p.CheckDependency(ctx, *query.Entry, ref, query.EntryOp, query.Operation)
		}
		if checkErr == nil {
			scope.ResourceIDs = append(scope.ResourceIDs, ref.ID)
			continue
		}
		if errors.Is(checkErr, ErrPermissionDenied) || errors.Is(checkErr, ErrMembershipNotFound) || errors.Is(checkErr, ErrResourceNotFound) {
			continue
		}
		return Scope{}, checkErr
	}
	if len(scope.ResourceIDs) == 0 {
		scope.Mode = ScopeNone
	} else {
		scope.Mode = ScopeIDs
	}
	return scope, nil
}

func (p *enterprisePolicy) listTenantResourceRefs(ctx context.Context, kind ResourceKind) ([]ResourceRef, error) {
	tenantID := strings.TrimSpace(TenantIDFromContext(ctx))
	if tenantID == "" {
		return nil, ErrMembershipNotFound
	}

	var ids []string
	var err error
	switch kind {
	case ResourceKindChat:
		err = p.db.WithContext(ctx).Model(&entity.Chat{}).
			Where("tenant_id = ? AND status = ?", tenantID, string(entity.StatusValid)).
			Pluck("id", &ids).Error
	case ResourceKindSearchApp:
		err = p.db.WithContext(ctx).Model(&entity.Search{}).
			Where("tenant_id = ? AND status = ?", tenantID, string(entity.StatusValid)).
			Pluck("id", &ids).Error
	default:
		return nil, ErrInvalidPermission
	}
	if err != nil {
		return nil, err
	}

	refs := make([]ResourceRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, ResourceRef{Kind: kind, ID: id})
	}
	return refs, nil
}

func (p *enterprisePolicy) FilterResources(ctx context.Context, refs []ResourceRef, operation Operation) ([]ResourceRef, error) {
	var err error
	ctx, err = p.withTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	if !validOperation(operation) {
		return nil, ErrInvalidPermission
	}
	if _, err := p.loadMembership(ctx); err != nil {
		return nil, err
	}
	accessible := make([]ResourceRef, 0, len(refs))
	for _, ref := range uniqueRefs(refs) {
		err := p.CheckResource(ctx, ref, operation)
		if err == nil {
			accessible = append(accessible, ref)
			continue
		}
		if errors.Is(err, ErrPermissionDenied) || errors.Is(err, ErrMembershipNotFound) || errors.Is(err, ErrResourceNotFound) {
			continue
		}
		return nil, err
	}
	return accessible, nil
}

func (p *enterprisePolicy) QueryScope(ctx context.Context, query ScopeQuery) (QueryScope, bool, error) {
	if (query.Kind == ResourceKindModelProvider || query.Kind == ResourceKindModelInstance || query.Kind == ResourceKindModel) && strings.TrimSpace(query.TenantID) != "" {
		ctx = WithIdentity(ctx, UserIDFromContext(ctx), strings.TrimSpace(query.TenantID))
	}
	var err error
	ctx, err = p.withTenantContext(ctx)
	if err != nil {
		return QueryScope{}, true, err
	}
	if query.Kind == ResourceKindModelProvider || query.Kind == ResourceKindModelInstance || query.Kind == ResourceKindModel {
		return p.modelResourceReadQueryScope(ctx, query)
	}
	if query.Kind == ResourceKindMCPServer {
		if query.Operation != OperationRead || query.Parent.ID != "" || query.Parent.Kind != "" || query.Entry != nil {
			return QueryScope{}, false, nil
		}
		return p.mcpServerReadQueryScope(ctx)
	}
	if query.Kind == ResourceKindDocument {
		return p.documentReadQueryScope(ctx, query)
	}
	if query.Kind != ResourceKindFile || query.Operation != OperationRead ||
		query.Parent.Kind != ResourceKindFolder || strings.TrimSpace(query.Parent.ID) == "" || query.Entry != nil {
		return QueryScope{}, false, nil
	}
	if err := p.CheckResource(ctx, query.Parent, OperationRead); err != nil {
		return QueryScope{}, true, err
	}
	membership, err := p.loadMembership(ctx)
	if err != nil {
		return QueryScope{}, true, err
	}
	featureMask, err := p.effectiveFeatureMask(ctx, UserIDFromContext(ctx), membership, "file")
	if err != nil {
		return QueryScope{}, true, err
	}
	if featureMask&(entity.RoleActionEnable|entity.RoleActionRead) != entity.RoleActionEnable|entity.RoleActionRead {
		return QueryScope{}, true, nil
	}
	return QueryScope{apply: func(query *gorm.DB) *gorm.DB {
		db := p.db.WithContext(ctx)
		grant := db.Table("permission AS file_grant").Select("1").
			Where("file_grant.tenant_id = file.tenant_id AND file_grant.resource_type = ? AND file_grant.resource_id = file.id AND file_grant.status = ? AND (file_grant.permission & ?) <> 0",
				"file", string(entity.StatusValid), entity.ResourcePermissionRead|entity.ResourcePermissionWrite|entity.ResourcePermissionManage|entity.ResourcePermissionOwner).
			Where("(file_grant.member_id = ? OR EXISTS (SELECT 1 FROM group_member JOIN group_info ON group_info.id = group_member.group_id WHERE group_member.group_id = file_grant.group_id AND group_member.member_id = ? AND group_member.status = ? AND group_info.tenant_id = file.tenant_id AND group_info.status = ?))",
				membership.Membership.ID, membership.Membership.ID, string(entity.StatusValid), string(entity.StatusValid))
		ownerGrant := db.Table("permission AS owner_grant").Select("1").
			Where("owner_grant.tenant_id = file.tenant_id AND owner_grant.resource_type = ? AND owner_grant.resource_id = file.id AND owner_grant.status = ? AND (owner_grant.permission & ?) <> 0",
				"file", string(entity.StatusValid), entity.ResourcePermissionOwner)
		return query.Where("file.tenant_id = ?", TenantIDFromContext(ctx)).
			Where("EXISTS (?) OR ((file.created_by = ? OR (file.created_by = '' AND ?)) AND NOT EXISTS (?))",
				grant, UserIDFromContext(ctx), membership.RoleValue == "owner", ownerGrant)
	}}, true, nil
}

func (p *enterprisePolicy) documentReadQueryScope(ctx context.Context, query ScopeQuery) (QueryScope, bool, error) {
	if query.Operation != OperationRead || query.Parent.Kind != ResourceKindDataset ||
		strings.TrimSpace(query.Parent.ID) == "" || query.Entry != nil {
		return QueryScope{}, false, nil
	}
	if err := p.CheckResource(ctx, query.Parent, OperationRead); err != nil {
		return QueryScope{}, true, err
	}
	membership, err := p.loadMembership(ctx)
	if err != nil {
		return QueryScope{}, true, err
	}
	tenantID := membership.Membership.TenantID
	membershipID := membership.Membership.ID
	userID := UserIDFromContext(ctx)
	datasetID := query.Parent.ID
	const readableDocumentPermissions = entity.ResourcePermissionRead | entity.ResourcePermissionWrite | entity.ResourcePermissionManage | entity.ResourcePermissionOwner
	db := p.db.WithContext(ctx)
	groupMembership := db.Table("group_member AS document_scope_group_member").
		Select("1").
		Joins("JOIN group_info AS document_scope_group ON document_scope_group.id = document_scope_group_member.group_id").
		Where("document_scope_group_member.group_id = document_scope_grant.group_id AND document_scope_group_member.member_id = ? AND document_scope_group_member.status = ? AND document_scope_group.tenant_id = ? AND document_scope_group.status = ?",
			membershipID, string(entity.StatusValid), tenantID, string(entity.StatusValid))
	documentGrant := db.Table("permission AS document_scope_grant").
		Select("1").
		Where("document_scope_grant.tenant_id = ? AND document_scope_grant.resource_type = ? AND document_scope_grant.resource_id = document.id AND document_scope_grant.status = ? AND (document_scope_grant.permission & ?) <> 0",
			tenantID, "document", string(entity.StatusValid), readableDocumentPermissions).
		Where("((document_scope_grant.member_id = ? AND (document_scope_grant.group_id IS NULL OR document_scope_grant.group_id = '')) OR ((document_scope_grant.member_id IS NULL OR document_scope_grant.member_id = '') AND document_scope_grant.group_id IS NOT NULL AND document_scope_grant.group_id <> '' AND EXISTS (?)))",
			membershipID, groupMembership)
	documentOwnerGrant := db.Table("permission AS document_scope_owner_grant").
		Select("1").
		Where("document_scope_owner_grant.tenant_id = ? AND document_scope_owner_grant.resource_type = ? AND document_scope_owner_grant.resource_id = document.id AND document_scope_owner_grant.status = ? AND (document_scope_owner_grant.permission & ?) <> 0 AND document_scope_owner_grant.member_id IS NOT NULL AND document_scope_owner_grant.member_id <> ''",
			tenantID, "document", string(entity.StatusValid), entity.ResourcePermissionOwner)
	return QueryScope{apply: func(query *gorm.DB) *gorm.DB {
		return query.Where("document.kb_id = ?", datasetID).
			Where("EXISTS (?) OR (COALESCE(NULLIF(TRIM(document.user_id), ''), TRIM(document.created_by)) = ? AND (NULLIF(TRIM(document.user_id), '') IS NOT NULL OR NOT EXISTS (?)))",
				documentGrant, userID, documentOwnerGrant)
	}}, true, nil
}

func (p *enterprisePolicy) modelResourceReadQueryScope(ctx context.Context, query ScopeQuery) (QueryScope, bool, error) {
	ownerUserID := strings.TrimSpace(query.OwnerUserID)
	currentUserID := strings.TrimSpace(UserIDFromContext(ctx))
	if ownerUserID != "" && ownerUserID != currentUserID {
		query.OwnerUserID = ""
		currentScope, handled, err := p.modelResourceReadQueryScope(ctx, query)
		if err != nil {
			return QueryScope{}, true, err
		}
		if !handled {
			return QueryScope{}, true, nil
		}

		ownerCtx := WithIdentity(ctx, ownerUserID, TenantIDFromContext(ctx))
		ownerScope, handled, err := p.modelResourceReadQueryScope(ownerCtx, query)
		if errors.Is(err, ErrMembershipNotFound) || errors.Is(err, ErrPermissionDenied) {
			return QueryScope{}, true, nil
		}
		if err != nil {
			return QueryScope{}, true, err
		}
		if !handled {
			return QueryScope{}, true, nil
		}
		return QueryScope{apply: func(dbQuery *gorm.DB) *gorm.DB {
			return ownerScope.Apply(currentScope.Apply(dbQuery))
		}}, true, nil
	}
	if query.Entry != nil || (query.Parent.ID == "") != (query.Parent.Kind == "") {
		return QueryScope{}, false, nil
	}
	switch query.Kind {
	case ResourceKindModelProvider:
		if query.Parent.ID != "" || query.Operation != OperationRead {
			return QueryScope{}, false, nil
		}
	case ResourceKindModelInstance:
		if (query.Operation != OperationRead && query.Operation != OperationUse) || (query.Parent.ID != "" && query.Parent.Kind != ResourceKindModelProvider) {
			return QueryScope{}, false, nil
		}
	case ResourceKindModel:
		if (query.Operation != OperationRead && query.Operation != OperationUse) ||
			(query.Parent.ID != "" && query.Parent.Kind != ResourceKindModelProvider && query.Parent.Kind != ResourceKindModelInstance) {
			return QueryScope{}, false, nil
		}
	default:
		return QueryScope{}, false, nil
	}

	membership, err := p.loadMembership(ctx)
	if err != nil {
		return QueryScope{}, true, err
	}
	featureMask, err := p.effectiveFeatureMask(ctx, UserIDFromContext(ctx), membership, "model_provider")
	if err != nil {
		return QueryScope{}, true, err
	}
	if featureMask&entity.RoleActionEnable == 0 {
		return QueryScope{}, true, nil
	}

	tenantID := membership.Membership.TenantID
	userID := UserIDFromContext(ctx)
	membershipID := membership.Membership.ID
	disabledProviders, err := p.disabledModelProvidersForRole(ctx, membership)
	if err != nil {
		return QueryScope{}, true, err
	}
	db := p.db.WithContext(ctx)
	applyInstanceAccess := func(query *gorm.DB, instanceAlias, providerAlias string) *gorm.DB {
		query = p.applyModelProviderRoleScope(query, providerAlias, disabledProviders)
		groupMembership := db.Table("group_member AS model_scope_group_member").
			Select("1").
			Joins("JOIN group_info AS model_scope_group ON model_scope_group.id = model_scope_group_member.group_id").
			Where("model_scope_group_member.group_id = model_scope_grant.group_id AND model_scope_group_member.member_id = ? AND model_scope_group_member.status = ? AND model_scope_group.tenant_id = ? AND model_scope_group.status = ?",
				membershipID, string(entity.StatusValid), tenantID, string(entity.StatusValid))
		grants := db.Table("permission AS model_scope_grant").
			Select("1").
			Where("model_scope_grant.tenant_id = ? AND model_scope_grant.resource_type = ? AND model_scope_grant.resource_id = "+instanceAlias+".id AND model_scope_grant.status = ? AND (model_scope_grant.permission & ?) <> 0",
				tenantID, "model_instance", string(entity.StatusValid), entity.ResourcePermissionRead|entity.ResourcePermissionOwner).
			Where("((model_scope_grant.member_id = ? AND (model_scope_grant.group_id IS NULL OR model_scope_grant.group_id = '')) OR (model_scope_grant.group_id IS NOT NULL AND model_scope_grant.group_id <> '' AND (model_scope_grant.member_id IS NULL OR model_scope_grant.member_id = '') AND EXISTS (?)))",
				membershipID, groupMembership)
		ownerGrant := db.Table("permission AS model_scope_owner_grant").
			Select("1").
			Where("model_scope_owner_grant.tenant_id = ? AND model_scope_owner_grant.resource_type = ? AND model_scope_owner_grant.resource_id = "+instanceAlias+".id AND model_scope_owner_grant.status = ? AND (model_scope_owner_grant.permission & ?) <> 0",
				tenantID, "model_instance", string(entity.StatusValid), entity.ResourcePermissionOwner)
		return query.Where("(EXISTS (?) OR ("+providerAlias+".user_id = ? AND NOT EXISTS (?)))", grants, userID, ownerGrant)
	}

	kind := query.Kind
	parent := query.Parent
	return QueryScope{apply: func(query *gorm.DB) *gorm.DB {
		switch kind {
		case ResourceKindModelProvider:
			query = p.applyModelProviderRoleScope(query, "tenant_model_provider", disabledProviders)
			readableInstance := db.Table("tenant_model_instance AS model_scope_instance").
				Select("1").
				Joins("JOIN tenant_model_provider AS model_scope_provider ON model_scope_provider.id = model_scope_instance.provider_id").
				Where("model_scope_provider.id = tenant_model_provider.id AND model_scope_provider.tenant_id = ?", tenantID)
			readableInstance = applyInstanceAccess(readableInstance, "model_scope_instance", "model_scope_provider")
			return query.Where("tenant_model_provider.tenant_id = ?", tenantID).
				Where("(tenant_model_provider.user_id = ? OR EXISTS (?))", userID, readableInstance)
		case ResourceKindModelInstance:
			readableProvider := db.Table("tenant_model_provider AS model_scope_provider").
				Select("1").
				Where("model_scope_provider.id = tenant_model_instance.provider_id AND model_scope_provider.tenant_id = ?", tenantID)
			if parent.ID != "" {
				readableProvider = readableProvider.Where("model_scope_provider.id = ?", parent.ID)
			}
			readableProvider = applyInstanceAccess(readableProvider, "tenant_model_instance", "model_scope_provider")
			return query.Where("EXISTS (?)", readableProvider)
		case ResourceKindModel:
			readableInstance := db.Table("tenant_model_instance AS model_scope_instance").
				Select("1").
				Joins("JOIN tenant_model_provider AS model_scope_provider ON model_scope_provider.id = model_scope_instance.provider_id").
				Where("model_scope_instance.id = tenant_model.instance_id AND model_scope_instance.provider_id = tenant_model.provider_id AND model_scope_provider.tenant_id = ?", tenantID)
			if parent.Kind == ResourceKindModelProvider {
				readableInstance = readableInstance.Where("model_scope_provider.id = ?", parent.ID)
			} else if parent.Kind == ResourceKindModelInstance {
				readableInstance = readableInstance.Where("model_scope_instance.id = ?", parent.ID)
			}
			readableInstance = applyInstanceAccess(readableInstance, "model_scope_instance", "model_scope_provider")
			return query.Where("EXISTS (?)", readableInstance)
		default:
			return query.Where("1 = 0")
		}
	}}, true, nil
}

func (p *enterprisePolicy) applyModelProviderRoleScope(query *gorm.DB, providerAlias string, disabledProviders []string) *gorm.DB {
	if len(disabledProviders) == 0 {
		return query
	}
	return query.Where(providerAlias+".provider_name NOT IN ?", disabledProviders)
}

func (p *enterprisePolicy) mcpServerReadQueryScope(ctx context.Context) (QueryScope, bool, error) {
	membership, err := p.loadMembership(ctx)
	if err != nil {
		return QueryScope{}, true, err
	}
	featureMask, err := p.effectiveFeatureMask(ctx, UserIDFromContext(ctx), membership, "mcp")
	if err != nil {
		return QueryScope{}, true, err
	}
	if featureMask&(entity.RoleActionEnable|entity.RoleActionRead) != entity.RoleActionEnable|entity.RoleActionRead {
		return QueryScope{apply: func(query *gorm.DB) *gorm.DB { return query.Where("1 = 0") }}, true, nil
	}
	memberID := membership.Membership.ID
	owner := membership.RoleValue == "owner"
	return QueryScope{apply: func(query *gorm.DB) *gorm.DB {
		db := p.db.WithContext(ctx)
		groupMembership := db.Table("group_member AS current_group_member").
			Select("1").
			Joins("JOIN group_info AS current_group ON current_group.id = current_group_member.group_id").
			Where("current_group_member.group_id = mcp_grant.group_id AND current_group_member.member_id = ? AND current_group_member.status = ? AND current_group.tenant_id = mcp_server.tenant_id AND current_group.status = ?",
				memberID, string(entity.StatusValid), string(entity.StatusValid))
		readGrant := db.Table("permission AS mcp_grant").
			Select("1").
			Where("mcp_grant.tenant_id = mcp_server.tenant_id AND mcp_grant.resource_type = ? AND mcp_grant.resource_id = mcp_server.id AND mcp_grant.status = ?",
				"mcp", string(entity.StatusValid)).
			Where("(mcp_grant.permission & ?) <> 0", entity.ResourcePermissionRead|entity.ResourcePermissionWrite|entity.ResourcePermissionManage|entity.ResourcePermissionOwner).
			Where("((mcp_grant.member_id = ? AND (mcp_grant.group_id IS NULL OR mcp_grant.group_id = '')) OR ((mcp_grant.group_id IS NOT NULL AND mcp_grant.group_id <> '') AND (mcp_grant.member_id IS NULL OR mcp_grant.member_id = '') AND EXISTS (?)))",
				memberID, groupMembership)
		if !owner {
			return query.Where("mcp_server.tenant_id = ?", TenantIDFromContext(ctx)).Where("EXISTS (?)", readGrant)
		}
		ownerGrant := db.Table("permission AS mcp_owner_grant").
			Select("1").
			Where("mcp_owner_grant.tenant_id = mcp_server.tenant_id AND mcp_owner_grant.resource_type = ? AND mcp_owner_grant.resource_id = mcp_server.id AND mcp_owner_grant.status = ? AND (mcp_owner_grant.permission & ?) <> 0",
				"mcp", string(entity.StatusValid), entity.ResourcePermissionOwner)
		return query.Where("mcp_server.tenant_id = ?", TenantIDFromContext(ctx)).
			Where("(EXISTS (?) OR NOT EXISTS (?))", readGrant, ownerGrant)
	}}, true, nil
}

func (p *enterprisePolicy) loadMembership(ctx context.Context) (*enterpriseMembership, error) {
	if err := validateEnterpriseIdentity(ctx); err != nil {
		return nil, err
	}
	membership, err := p.source.GetMembership(ctx, UserIDFromContext(ctx), TenantIDFromContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("permission: load current Tenant membership: %w", err)
	}
	if membership == nil || !membership.Active || membership.UserID != UserIDFromContext(ctx) || membership.TenantID != TenantIDFromContext(ctx) {
		return nil, ErrMembershipNotFound
	}
	var relation entity.UserTenant
	if err := p.db.WithContext(ctx).Select("role").
		Where("id = ? AND user_id = ? AND tenant_id = ? AND status = ?",
			membership.ID, UserIDFromContext(ctx), TenantIDFromContext(ctx), string(entity.StatusValid)).
		Take(&relation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMembershipNotFound
		}
		return nil, fmt.Errorf("permission: load current Tenant role: %w", err)
	}
	var tenant entity.Tenant
	if err := p.db.WithContext(ctx).Select("id", "role_id").Where("id = ?", TenantIDFromContext(ctx)).Take(&tenant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMembershipNotFound
		}
		return nil, fmt.Errorf("permission: load Tenant system role: %w", err)
	}
	return &enterpriseMembership{Membership: membership, SystemRoleID: tenant.RoleID, RoleValue: relation.Role}, nil
}

func (p *enterprisePolicy) loadResource(ctx context.Context, ref ResourceRef) (*Resource, error) {
	resources, err := p.source.GetResources(ctx, []ResourceRef{ref})
	if err != nil {
		return nil, fmt.Errorf("permission: load resource: %w", err)
	}
	if len(resources) != 1 || resources[0].TenantID != TenantIDFromContext(ctx) || !resources[0].Active {
		return nil, ErrResourceNotFound
	}
	return &resources[0], nil
}

func (p *enterprisePolicy) checkFeatureRoles(ctx context.Context, userID string, membership *enterpriseMembership, feature Feature, action Action) error {
	featureName, itemName, hasItem := strings.Cut(string(feature), "::")
	if !hasItem {
		featureName = string(feature)
	}
	mask, err := p.effectiveFeatureMask(ctx, userID, membership, Feature(featureName))
	if err != nil {
		return err
	}
	if mask&entity.RoleActionEnable == 0 {
		return ErrPermissionDenied
	}
	if action != ActionEnable && mask&featureActionMask(action) == 0 {
		return ErrPermissionDenied
	}
	if hasItem {
		itemEnabled, itemErr := p.featureItemEnabledForRole(ctx, membership, Feature(featureName), normalizeFeatureItem(featureName, itemName))
		if itemErr != nil {
			return itemErr
		}
		if !itemEnabled {
			return ErrPermissionDenied
		}
	}
	if featureName == "model_provider" {
		return nil
	}
	return nil
}

func normalizeFeatureItem(feature, item string) string {
	if feature == "data_source" {
		return strings.ReplaceAll(item, "-", "_")
	}
	return item
}

func (p *enterprisePolicy) featureItemEnabledForRole(ctx context.Context, membership *enterpriseMembership, feature Feature, item string) (bool, error) {
	if membership == nil || item == "" {
		return false, nil
	}
	resourceType, ok := enterpriseFeatureRoleType(feature)
	if !ok {
		return false, ErrInvalidPermission
	}
	if membership.SystemRoleID != nil {
		enabled, err := p.roleFeatureItemEnabled(ctx, *membership.SystemRoleID, resourceType, item)
		if err != nil || !enabled {
			return enabled, err
		}
	}
	if membership.RoleValue == "owner" {
		return true, nil
	}
	roleID, err := strconv.ParseInt(membership.RoleValue, 10, 32)
	if err != nil || roleID <= 0 {
		return false, ErrPermissionDenied
	}
	return p.roleFeatureItemEnabled(ctx, int32(roleID), resourceType, item)
}

func (p *enterprisePolicy) roleFeatureItemEnabled(ctx context.Context, roleID int32, resourceType int64, item string) (bool, error) {
	var rows []entity.RoleResource
	if err := p.db.WithContext(ctx).
		Where("role_id = ? AND resource_type = ? AND resource_id IN ?", roleID, resourceType, []string{"", item}).
		Find(&rows).Error; err != nil {
		return false, fmt.Errorf("permission: load role integration item permission: %w", err)
	}
	for _, row := range rows {
		if row.ResourceID == item && row.Action&entity.RoleActionEnable == 0 {
			return false, nil
		}
		if row.ResourceID != "" {
			continue
		}
		items, ok := row.Config["items"].(map[string]interface{})
		if !ok {
			continue
		}
		enabled, exists := items[item].(bool)
		if exists && !enabled {
			return false, nil
		}
	}
	return true, nil
}

func (p *enterprisePolicy) effectiveFeatureMask(ctx context.Context, userID string, membership *enterpriseMembership, feature Feature) (int32, error) {
	resourceType, ok := enterpriseFeatureRoleType(feature)
	if !ok {
		return 0, ErrInvalidPermission
	}
	mask := int32(entity.RoleActionAll)
	if membership.SystemRoleID != nil {
		globalMask, err := p.roleResourceMask(ctx, *membership.SystemRoleID, nil, resourceType)
		if err != nil {
			return 0, err
		}
		mask &= globalMask
	}
	if membership.RoleValue != "owner" {
		roleID, err := strconv.ParseInt(membership.RoleValue, 10, 32)
		if err != nil || roleID <= 0 {
			return 0, ErrPermissionDenied
		}
		tenantID := membership.Membership.TenantID
		localMask, err := p.roleResourceMask(ctx, int32(roleID), &tenantID, resourceType)
		if err != nil {
			return 0, err
		}
		mask &= localMask
	}
	_ = userID
	return mask, nil
}

func (p *enterprisePolicy) roleResourceMask(ctx context.Context, roleID int32, tenantID *string, resourceType int64) (int32, error) {
	var role entity.Role
	query := p.db.WithContext(ctx).Select("id", "tenant_id").Where("id = ?", roleID)
	if tenantID == nil {
		query = query.Where("tenant_id IS NULL")
	} else {
		query = query.Where("tenant_id = ?", *tenantID)
	}
	if err := query.Take(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrPermissionDenied
		}
		return 0, fmt.Errorf("permission: load role %d: %w", roleID, err)
	}
	var rows []entity.RoleResource
	if err := p.db.WithContext(ctx).Where("role_id = ? AND resource_type = ? AND resource_id = ?", roleID, resourceType, "").Order("id ASC").Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("permission: load role feature permission: %w", err)
	}
	var mask int32
	for _, row := range rows {
		mask |= row.Action
	}
	if len(rows) == 0 {
		switch resourceType {
		case entity.RoleResourceTypeModelProvider,
			entity.RoleResourceTypeModelGroup,
			entity.RoleResourceTypeDataSource,
			entity.RoleResourceTypeChatChannel:
			return entity.RoleActionEnable, nil
		}
	}
	return mask, nil
}

func (p *enterprisePolicy) modelProviderNameForResource(ctx context.Context, ref ResourceRef) (string, error) {
	var providerName string
	query := p.db.WithContext(ctx)
	switch ref.Kind {
	case ResourceKindModelProvider:
		query = query.Model(&entity.TenantModelProvider{}).Select("provider_name").Where("id = ?", ref.ID)
	case ResourceKindModelInstance:
		query = query.Table("tenant_model_instance").
			Select("tenant_model_provider.provider_name").
			Joins("JOIN tenant_model_provider ON tenant_model_provider.id = tenant_model_instance.provider_id").
			Where("tenant_model_instance.id = ?", ref.ID)
	case ResourceKindModel:
		query = query.Table("tenant_model").
			Select("tenant_model_provider.provider_name").
			Joins("JOIN tenant_model_provider ON tenant_model_provider.id = tenant_model.provider_id").
			Where("tenant_model.id = ?", ref.ID)
	default:
		return "", nil
	}
	if err := query.Take(&providerName).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrResourceNotFound
		}
		return "", fmt.Errorf("permission: resolve model provider for %s: %w", ref.Kind, err)
	}
	return providerName, nil
}

func (p *enterprisePolicy) modelProviderEnabledForRole(ctx context.Context, membership *enterpriseMembership, providerName string) (bool, error) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" || membership == nil {
		return true, nil
	}
	disabledProviders, err := p.disabledModelProvidersForRole(ctx, membership)
	if err != nil {
		return false, err
	}
	for _, disabledProvider := range disabledProviders {
		if disabledProvider == providerName {
			return false, nil
		}
	}
	return true, nil
}

func (p *enterprisePolicy) disabledModelProvidersForRole(ctx context.Context, membership *enterpriseMembership) ([]string, error) {
	if membership == nil || membership.RoleValue == "owner" {
		if membership == nil || membership.SystemRoleID == nil {
			return nil, nil
		}
	}
	roleIDs := make([]int32, 0, 2)
	if membership.SystemRoleID != nil {
		roleIDs = append(roleIDs, *membership.SystemRoleID)
	}
	if membership.RoleValue != "owner" {
		roleID, err := strconv.ParseInt(membership.RoleValue, 10, 32)
		if err != nil || roleID <= 0 {
			return nil, ErrPermissionDenied
		}
		roleIDs = append(roleIDs, int32(roleID))
	}
	disabled := make(map[string]struct{})
	for _, roleID := range roleIDs {
		var rows []entity.RoleResource
		if err := p.db.WithContext(ctx).
			Where("role_id = ? AND resource_type = ?", roleID, entity.RoleResourceTypeModelProvider).
			Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("permission: load model provider role permission: %w", err)
		}
		for _, row := range rows {
			if row.ResourceID != "" {
				if row.Action&entity.RoleActionEnable == 0 {
					disabled[row.ResourceID] = struct{}{}
				}
				continue
			}
			configuredItems, ok := row.Config["items"].(map[string]interface{})
			if !ok {
				continue
			}
			for provider, rawEnabled := range configuredItems {
				enabled, ok := rawEnabled.(bool)
				if ok && !enabled {
					disabled[provider] = struct{}{}
				}
			}
		}
	}
	disabledProviders := make([]string, 0, len(disabled))
	for provider := range disabled {
		disabledProviders = append(disabledProviders, provider)
	}
	return disabledProviders, nil
}

func (p *enterprisePolicy) permissionResourceIDs(ctx context.Context, ref ResourceRef) ([]string, error) {
	switch ref.Kind {
	case ResourceKindModelInstance:
		return []string{ref.ID}, nil
	case ResourceKindModel:
		var model entity.TenantModel
		if err := p.db.WithContext(ctx).Select("instance_id").Where("id = ?", ref.ID).Take(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrResourceNotFound
			}
			return nil, fmt.Errorf("permission: resolve model instance: %w", err)
		}
		if strings.TrimSpace(model.InstanceID) == "" {
			return nil, ErrResourceNotFound
		}
		return []string{model.InstanceID}, nil
	case ResourceKindAgentSession:
		var session entity.API4Conversation
		if err := p.db.WithContext(ctx).Select("dialog_id").Where("id = ?", ref.ID).Take(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrResourceNotFound
			}
			return nil, fmt.Errorf("permission: resolve agent session canvas: %w", err)
		}
		if strings.TrimSpace(session.DialogID) == "" {
			return nil, ErrResourceNotFound
		}
		return []string{session.DialogID}, nil
	case ResourceKindChatSession:
		var session entity.ChatSession
		if err := p.db.WithContext(ctx).Select("dialog_id").Where("id = ?", ref.ID).Take(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrResourceNotFound
			}
			return nil, fmt.Errorf("permission: resolve chat session: %w", err)
		}
		if strings.TrimSpace(session.DialogID) == "" {
			return nil, ErrResourceNotFound
		}
		return []string{session.DialogID}, nil
	case ResourceKindModelProvider:
		var instanceIDs []string
		if err := p.db.WithContext(ctx).Model(&entity.TenantModelInstance{}).
			Where("provider_id = ?", ref.ID).Pluck("id", &instanceIDs).Error; err != nil {
			return nil, fmt.Errorf("permission: list provider instances: %w", err)
		}
		return uniqueStringValues(instanceIDs), nil
	default:
		return []string{ref.ID}, nil
	}
}

func (p *enterprisePolicy) effectiveResourceMask(ctx context.Context, membership *enterpriseMembership, resourceType string, resourceIDs []string) (int32, AccessSource, error) {
	resourceIDs = uniqueStringValues(resourceIDs)
	if len(resourceIDs) == 0 {
		return 0, "", nil
	}
	var groupIDs []string
	if err := p.db.WithContext(ctx).Table("group_member").Distinct("group_id").
		Joins("JOIN group_info ON group_info.id = group_member.group_id").
		Where("group_member.member_id = ? AND group_member.status = ? AND group_info.tenant_id = ? AND group_info.status = ?",
			membership.Membership.ID, string(entity.StatusValid), membership.Membership.TenantID, string(entity.StatusValid)).
		Pluck("group_member.group_id", &groupIDs).Error; err != nil {
		return 0, "", fmt.Errorf("permission: list current Tenant groups: %w", err)
	}
	query := p.db.WithContext(ctx).Model(&entity.Permission{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id IN ? AND status = ?",
			membership.Membership.TenantID, resourceType, resourceIDs, string(entity.StatusValid))
	targets := p.db.Where("member_id = ? AND (group_id IS NULL OR group_id = '')", membership.Membership.ID)
	if len(groupIDs) > 0 {
		targets = targets.Or("group_id IN ? AND (member_id IS NULL OR member_id = '')", groupIDs)
	}
	var grants []entity.Permission
	if err := query.Where(targets).Order("id ASC").Find(&grants).Error; err != nil {
		return 0, "", fmt.Errorf("permission: load resource grants: %w", err)
	}
	var mask int32
	source := AccessSourceShared
	for _, grant := range grants {
		mask |= grant.Permission
		if grant.MemberID != nil && *grant.MemberID == membership.Membership.ID {
			if grant.Permission&entity.ResourcePermissionOwner != 0 {
				source = AccessSourceOwner
			} else if source != AccessSourceOwner {
				source = AccessSourceShared
			}
		}
	}
	return mask, source, nil
}

func (p *enterprisePolicy) tenantManagementMask(ctx context.Context, membership *enterpriseMembership) (int32, error) {
	if membership.RoleValue == "owner" {
		return entity.TenantPermissionManageMembers | entity.TenantPermissionManageRoles | entity.TenantPermissionOwner, nil
	}
	groupIDs, err := p.memberGroupIDs(ctx, membership)
	if err != nil {
		return 0, err
	}
	query := p.db.WithContext(ctx).Model(&entity.Permission{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ?",
			membership.Membership.TenantID, "tenant", membership.Membership.TenantID, string(entity.StatusValid))
	targets := p.db.Where("member_id = ? AND (group_id IS NULL OR group_id = '')", membership.Membership.ID)
	if len(groupIDs) > 0 {
		targets = targets.Or("group_id IN ? AND (member_id IS NULL OR member_id = '')", groupIDs)
	}
	var grants []entity.Permission
	if err := query.Where(targets).Find(&grants).Error; err != nil {
		return 0, fmt.Errorf("permission: load Tenant management grants: %w", err)
	}
	var mask int32
	for _, grant := range grants {
		mask |= grant.Permission
	}
	return mask, nil
}

func (p *enterprisePolicy) memberGroupIDs(ctx context.Context, membership *enterpriseMembership) ([]string, error) {
	var groupIDs []string
	err := p.db.WithContext(ctx).Table("group_member").Distinct("group_id").
		Joins("JOIN group_info ON group_info.id = group_member.group_id").
		Where("group_member.member_id = ? AND group_member.status = ? AND group_info.tenant_id = ? AND group_info.status = ?",
			membership.Membership.ID, string(entity.StatusValid), membership.Membership.TenantID, string(entity.StatusValid)).
		Pluck("group_member.group_id", &groupIDs).Error
	if err != nil {
		return nil, fmt.Errorf("permission: load Tenant group memberships: %w", err)
	}
	return groupIDs, nil
}

func (p *enterprisePolicy) resourceOwnerUserID(ctx context.Context, tenantID string, ref ResourceRef) (string, error) {
	resourceType, ok := enterpriseResourceType(ref.Kind)
	if !ok {
		return "", ErrInvalidPermission
	}
	resourceIDs, err := p.permissionResourceIDs(ctx, ref)
	if err != nil {
		return "", err
	}
	if len(resourceIDs) == 0 {
		return "", ErrPermissionDenied
	}
	var grant entity.Permission
	if err := p.db.WithContext(ctx).
		Where("tenant_id = ? AND resource_type = ? AND resource_id IN ? AND status = ? AND (permission & ?) <> 0 AND member_id IS NOT NULL AND member_id <> ''",
			tenantID, resourceType, resourceIDs, string(entity.StatusValid), entity.ResourcePermissionOwner).
		Order("id ASC").Take(&grant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			resource, loadErr := p.loadResource(WithIdentity(ctx, UserIDFromContext(ctx), tenantID), ref)
			if loadErr != nil {
				return "", loadErr
			}
			if strings.TrimSpace(resource.OwnerUserID) == "" {
				return "", ErrPermissionDenied
			}
			return resource.OwnerUserID, nil
		}
		return "", fmt.Errorf("permission: load resource owner: %w", err)
	}
	var membership entity.UserTenant
	if err := p.db.WithContext(ctx).Select("user_id").
		Where("id = ? AND tenant_id = ? AND status = ?", *grant.MemberID, tenantID, string(entity.StatusValid)).
		Take(&membership).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrPermissionDenied
		}
		return "", fmt.Errorf("permission: resolve resource owner membership: %w", err)
	}
	return membership.UserID, nil
}

func (p *enterprisePolicy) hasResourceOwnerGrant(ctx context.Context, tenantID, resourceType string, resourceIDs []string) (bool, error) {
	if len(resourceIDs) == 0 {
		return false, nil
	}
	var count int64
	if err := p.db.WithContext(ctx).Model(&entity.Permission{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id IN ? AND status = ? AND (permission & ?) <> 0",
			tenantID, resourceType, resourceIDs, string(entity.StatusValid), entity.ResourcePermissionOwner).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("permission: check resource owner grant: %w", err)
	}
	return count > 0, nil
}

func (p *enterprisePolicy) withTenantContext(ctx context.Context) (context.Context, error) {
	userID := strings.TrimSpace(UserIDFromContext(ctx))
	if userID == "" {
		return nil, ErrUnauthenticated
	}
	if strings.TrimSpace(TenantIDFromContext(ctx)) != "" {
		return ctx, nil
	}
	var tenantIDs []string
	if err := p.db.WithContext(ctx).Model(&entity.UserTenant{}).
		Where("user_id = ? AND status = ?", userID, string(entity.StatusValid)).
		Pluck("tenant_id", &tenantIDs).Error; err != nil {
		return nil, fmt.Errorf("permission: resolve current Tenant: %w", err)
	}
	tenantIDs = uniqueStringValues(tenantIDs)
	if len(tenantIDs) != 1 {
		return nil, ErrInvalidPermission
	}
	return WithIdentity(ctx, userID, tenantIDs[0]), nil
}

func validateEnterpriseIdentity(ctx context.Context) error {
	if strings.TrimSpace(UserIDFromContext(ctx)) == "" {
		return ErrUnauthenticated
	}
	if strings.TrimSpace(TenantIDFromContext(ctx)) == "" {
		return ErrInvalidPermission
	}
	return nil
}

func enterpriseResourceType(kind ResourceKind) (string, bool) {
	switch kind {
	case ResourceKindDataset:
		return "kb", true
	case ResourceKindDocument:
		return "document", true
	case ResourceKindCanvas, ResourceKindAgentSession:
		return "canvas", true
	case ResourceKindMemory:
		return "memory", true
	case ResourceKindFile, ResourceKindFolder, ResourceKindSkillSpace:
		return "file", true
	case ResourceKindSearchApp:
		return "search", true
	case ResourceKindChat, ResourceKindChatSession, ResourceKindChatChannel:
		return "dialog", true
	case ResourceKindConnector, ResourceKindSyncTask:
		return "connector", true
	case ResourceKindModelProvider, ResourceKindModelInstance, ResourceKindModel:
		return "model_instance", true
	case ResourceKindMCPServer:
		return "mcp", true
	default:
		return "", false
	}
}

func enterpriseFeature(kind ResourceKind) (Feature, bool) {
	switch kind {
	case ResourceKindDataset, ResourceKindDocument:
		return "dataset", true
	case ResourceKindCanvas, ResourceKindAgentSession:
		return "agent", true
	case ResourceKindMemory:
		return "memory", true
	case ResourceKindFile, ResourceKindFolder, ResourceKindSkillSpace:
		return "file", true
	case ResourceKindSearchApp:
		return "search", true
	case ResourceKindChat, ResourceKindChatSession, ResourceKindChatChannel:
		return "chat", true
	case ResourceKindConnector, ResourceKindSyncTask:
		return "file", true
	case ResourceKindModelProvider, ResourceKindModelInstance, ResourceKindModel:
		return "model_provider", true
	case ResourceKindMCPServer:
		return "mcp", true
	default:
		return "", false
	}
}

func enterpriseFeatureRoleType(feature Feature) (int64, bool) {
	switch feature {
	case "dataset":
		return entity.RoleResourceTypeDataset, true
	case "chat":
		return entity.RoleResourceTypeChat, true
	case "agent":
		return entity.RoleResourceTypeAgent, true
	case "search":
		return entity.RoleResourceTypeSearch, true
	case "file":
		return entity.RoleResourceTypeFile, true
	case "group":
		return entity.RoleResourceTypeGroup, true
	case "memory":
		return entity.RoleResourceTypeMemory, true
	case "model_provider":
		return entity.RoleResourceTypeModelProvider, true
	case "model_group":
		return entity.RoleResourceTypeModelGroup, true
	case "data_source":
		return entity.RoleResourceTypeDataSource, true
	case "chat_channel":
		return entity.RoleResourceTypeChatChannel, true
	case "mcp":
		return entity.RoleResourceTypeMCP, true
	default:
		return 0, false
	}
}

func featureActionMask(action Action) int32 {
	switch action {
	case ActionEnable:
		return entity.RoleActionEnable
	case ActionRead:
		return entity.RoleActionRead
	case ActionWrite:
		return entity.RoleActionWrite
	case ActionShare:
		return entity.RoleActionShare
	default:
		return 0
	}
}

func operationFeatureAction(operation Operation) Action {
	switch operation {
	case OperationRead, OperationRun, OperationUse:
		return ActionRead
	case OperationCreate, OperationUpdate, OperationDeleteContent:
		return ActionWrite
	case OperationDelete, OperationShare:
		return ActionShare
	default:
		return ActionWrite
	}
}

func operationsForMasks(featureMask, resourceMask int32, hasFeature bool) []Operation {
	var operations []Operation
	if resourceMask&(entity.ResourcePermissionRead|entity.ResourcePermissionWrite|entity.ResourcePermissionManage|entity.ResourcePermissionOwner) == 0 {
		return operations
	}
	allows := func(action Action) bool {
		return !hasFeature || featureMask&entity.RoleActionEnable != 0 && featureMask&featureActionMask(action) != 0
	}
	if resourceMask&(entity.ResourcePermissionRead|entity.ResourcePermissionWrite|entity.ResourcePermissionManage|entity.ResourcePermissionOwner) != 0 && allows(ActionRead) {
		operations = append(operations, OperationRead, OperationRun, OperationUse)
	}
	if resourceMask&(entity.ResourcePermissionWrite|entity.ResourcePermissionManage|entity.ResourcePermissionOwner) != 0 && allows(ActionWrite) {
		operations = append(operations, OperationCreate, OperationUpdate, OperationDeleteContent)
	}
	if resourceMask&(entity.ResourcePermissionManage|entity.ResourcePermissionOwner) != 0 {
		if allows(ActionWrite) {
			operations = append(operations, OperationDelete)
		}
		if allows(ActionShare) {
			operations = append(operations, OperationShare)
		}
	}
	return operations
}

func retainReadOperations(operations []Operation) []Operation {
	for _, operation := range operations {
		if operation == OperationRead {
			return []Operation{OperationRead}
		}
	}
	return nil
}

func (role TenantRole) String() string { return string(role) }

var _ policy = (*enterprisePolicy)(nil)
var _ queryScopePolicy = (*enterprisePolicy)(nil)
var _ accessResolverPolicy = (*enterprisePolicy)(nil)
var _ resourceOwnerGranter = (*enterprisePolicy)(nil)
