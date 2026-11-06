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
	"fmt"

	"ragflow/internal/common"
	"ragflow/internal/dao"
)

// CheckRegistrationWhitelist controls new self-service accounts, never existing logins.
func CheckRegistrationWhitelist(ctx context.Context, email string) error {
	enabled, err := dao.WhitelistEnabled(ctx, dao.DB)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	allowed, err := dao.WhitelistContains(ctx, dao.DB, email)
	if err != nil {
		return fmt.Errorf("check registration whitelist: %w", err)
	}
	if !allowed {
		return common.NewCodedError(common.CodeForbidden, fmt.Sprintf("Email %s isn't in whitelist.", email))
	}
	return nil
}
