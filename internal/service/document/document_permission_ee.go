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

	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func grantDocumentOwnerEE(ctx context.Context, tx *gorm.DB, doc *entity.Document) error {
	if doc == nil || strings.TrimSpace(doc.CreatedBy) == "" {
		return fmt.Errorf("document owner is required")
	}
	var dataset entity.Knowledgebase
	if err := tx.WithContext(ctx).Select("tenant_id").Where("id = ?", doc.KbID).Take(&dataset).Error; err != nil {
		return fmt.Errorf("load document dataset Tenant: %w", err)
	}
	checker := permission.NewDatabaseChecker(tx)
	ctx = permission.WithIdentity(ctx, doc.CreatedBy, dataset.TenantID)
	return checker.GrantResourceOwner(ctx, permission.ResourceRef{Kind: permission.ResourceKindDocument, ID: doc.ID})
}
