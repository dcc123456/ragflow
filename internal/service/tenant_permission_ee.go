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
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func resolveDefaultTenantIDEE(context.Context) (string, bool, error) {
	return "", false, nil
}

func resolveCurrentUserTenantEE(ctx context.Context, userID string) (*entity.UserTenant, bool, error) {
	ctx = permission.WithIdentity(ctx, userID, permission.TenantIDFromContext(ctx))
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return nil, true, err
	}
	membership, err := dao.NewUserTenantDAO().FilterByUserIDAndTenantID(ctx, dao.DB, userID, tenantID)
	return membership, true, err
}
