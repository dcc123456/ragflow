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

package document

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func grantDocumentOwnerEE(ctx context.Context, tx *gorm.DB, doc *entity.Document) error {
	if doc == nil || strings.TrimSpace(doc.CreatedBy) == "" {
		return fmt.Errorf("document uploader is required")
	}
	var dataset entity.Knowledgebase
	if err := tx.WithContext(ctx).Select("tenant_id", "user_id", "created_by").Where("id = ?", doc.KbID).Take(&dataset).Error; err != nil {
		return fmt.Errorf("load document dataset owner: %w", err)
	}
	datasetOwnerID := strings.TrimSpace(dataset.UserID)
	if datasetOwnerID == "" {
		datasetOwnerID = strings.TrimSpace(dataset.CreatedBy)
	}
	if datasetOwnerID == "" {
		return fmt.Errorf("document dataset owner is required")
	}
	uploaderID := strings.TrimSpace(doc.CreatedBy)
	if doc.UserID != datasetOwnerID {
		if err := tx.WithContext(ctx).Model(&entity.Document{}).Where("id = ?", doc.ID).Update("user_id", datasetOwnerID).Error; err != nil {
			return fmt.Errorf("set document owner to dataset owner: %w", err)
		}
		doc.UserID = datasetOwnerID
	}
	checker := permission.NewDatabaseChecker(tx)
	ownerCtx := permission.WithIdentity(ctx, datasetOwnerID, dataset.TenantID)
	if err := checker.GrantResourceOwner(ownerCtx, permission.ResourceRef{Kind: permission.ResourceKindDocument, ID: doc.ID}); err != nil {
		return fmt.Errorf("grant document ownership to dataset owner: %w", err)
	}
	if uploaderID == datasetOwnerID {
		return nil
	}

	var uploaderMembership entity.UserTenant
	if err := tx.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ? AND status = ?", dataset.TenantID, uploaderID, string(entity.TenantMembershipStatusJoined)).
		Take(&uploaderMembership).Error; err != nil {
		return fmt.Errorf("load document uploader membership: %w", err)
	}
	memberID, resourceID := uploaderMembership.ID, doc.ID
	status := string(entity.StatusValid)
	grant := entity.Permission{
		ID: common.GenerateUUID(), MemberID: &memberID, TenantID: dataset.TenantID,
		ResourceType: "document", ResourceID: &resourceID,
		Permission: entity.ResourcePermissionManage, Status: &status,
	}
	if err := tx.WithContext(ctx).Create(&grant).Error; err != nil {
		return fmt.Errorf("grant document management to uploader: %w", err)
	}
	return nil
}
