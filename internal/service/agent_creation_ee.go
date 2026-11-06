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

	"ragflow/internal/dao"
	"ragflow/internal/permission"
)

func prepareAgentCreation(ctx context.Context) (string, error) {
	userID := permission.UserIDFromContext(ctx)
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return "", err
	}
	ctx = permission.WithIdentity(ctx, userID, tenantID)
	if err := permission.NewDatabaseChecker(dao.DB).CheckFeature(ctx, "agent", permission.ActionWrite); err != nil {
		return "", err
	}
	return tenantID, nil
}
