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

package dataset

import (
	"context"
	"strings"

	"ragflow/internal/permission"
)

func accessUserContext(ctx context.Context, accessUserID string) (context.Context, bool) {
	accessUserID = strings.TrimSpace(accessUserID)
	if accessUserID == "" || accessUserID == permission.UserIDFromContext(ctx) {
		return ctx, false
	}
	return permission.WithIdentity(ctx, accessUserID, permission.TenantIDFromContext(ctx)), true
}
