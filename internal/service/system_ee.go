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

package service

import (
	"context"
	"ragflow/internal/common"
	"ragflow/internal/utility"
)

// ListFeatures get system features
func (s *SystemService) ListFeatures(ctx context.Context) (map[string]interface{}, error) {
	apiServiceQuota := utility.GetAPIServiceQuota()
	if apiServiceQuota == nil {
		return nil, common.ErrNotFound
	}
	return apiServiceQuota.Features.ToMap(), nil
}
