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
	"ragflow/internal/common"
	"ragflow/internal/server/local"
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

// CheckLicense check system license
func (s *SystemService) CheckLicense(ctx context.Context) (map[string]interface{}, error) {

	if local.IsAdminAvailable() {
		apiServiceQuota := utility.GetAPIServiceQuota()
		if apiServiceQuota.LicenseType == utility.SAAS {
			return map[string]interface{}{"status": "saas"}, nil
		}
		return map[string]interface{}{"status": "valid"}, nil
	}

	return map[string]interface{}{"status": "invalid"}, nil
}

// GetDeploymentType get system deployment type
func (s *SystemService) GetDeploymentType(ctx context.Context) (string, error) {
	if local.IsAdminAvailable() {
		apiServiceQuota := utility.GetAPIServiceQuota()
		if apiServiceQuota == nil {
			return "", common.ErrNotFound
		}
		return string(apiServiceQuota.LicenseType), nil
	}
	return "", errors.New("admin is unavailable")
}
