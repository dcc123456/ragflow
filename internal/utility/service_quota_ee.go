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

package utility

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ragflow/internal/common"

	"go.uber.org/zap"
)

type ServiceFeatures struct {
	Workflow     bool `json:"workflow_enable"`
	Chat         bool `json:"chat_enable"`
	File         bool `json:"file_enable"`
	Search       bool `json:"search_enable"`
	Model        bool `json:"model_enable"`
	UserInTenant bool `json:"user_in_tenant_enable"`
	RBAC         bool `json:"rbac_enable"`
	Group        bool `json:"group_enable"`
	Evaluation   bool `json:"evaluation_enable"`
}

func NewServiceFeatures(features []string) ServiceFeatures {
	f := ServiceFeatures{}
	for _, feature := range features {
		switch strings.ToLower(feature) {
		case "workflow":
			f.Workflow = true
		case "chat":
			f.Chat = true
		case "file":
			f.File = true
		case "search":
			f.Search = true
		case "model":
			f.Model = true
		case "userintenant":
			f.UserInTenant = true
		case "rbac":
			f.RBAC = true
		case "group":
			f.Group = true
		case "evaluation":
			f.Evaluation = true
		}
	}
	return f
}

type APIServiceQuota struct {
	Features       ServiceFeatures `json:"features"`
	MaxConcurrency int             `json:"max_concurrency"`
	CPUNum         int             `json:"cpu_num"`
}

type IngestionServiceQuota struct {
	Features  ServiceFeatures `json:"features"`
	CPUNum    int             `json:"cpu_num"`
	WorkerNum int             `json:"worker_num"`
}

type SyncServiceQuota struct {
	Features  ServiceFeatures `json:"features"`
	CPUNum    int             `json:"cpu_num"`
	WorkerNum int             `json:"worker_num"`
}

type deepDocCPUQuota struct {
	Features       ServiceFeatures `json:"features"`
	MaxConcurrency int             `json:"max_concurrency"`
	CPUNum         int             `json:"cpu_num"`
	WorkerNum      int             `json:"worker_num"`
}

type deepDocGPUQuota struct {
	Features       ServiceFeatures `json:"features"`
	MaxConcurrency int             `json:"max_concurrency"`
	CPUNum         int             `json:"cpu_num"`
	GPUNum         int             `json:"gpu_num"`
	WorkerNum      int             `json:"worker_num"`
}

type ServiceQuota struct {
	ServerType      common.ServerType
	apiServiceQuota *APIServiceQuota
	ingestorQuota   *IngestionServiceQuota
	syncerQuota     *SyncServiceQuota
	deepDocCPUQuota *deepDocCPUQuota
	deepDocGPUQuota *deepDocGPUQuota
	LastUpdateTime  time.Time
}

// GetAPIServiceQuota returns the API server quota snapshot, or nil when this
// process is not an API server.
func GetAPIServiceQuota() any {
	if serviceQuotaInstance == nil || serviceQuotaInstance.apiServiceQuota == nil {
		return nil
	}
	return serviceQuotaInstance.apiServiceQuota
}

var serviceQuotaInstance *ServiceQuota

func InitServiceQuota(serverType common.ServerType) {
	serviceQuotaInstance = &ServiceQuota{
		ServerType: serverType,
	}
	switch serverType {
	case common.ServerTypeAPI:
		serviceQuotaInstance.apiServiceQuota = &APIServiceQuota{}
	case common.ServerTypeIngestion:
		serviceQuotaInstance.ingestorQuota = &IngestionServiceQuota{}
	case common.ServerTypeFileSyncer:
		serviceQuotaInstance.syncerQuota = &SyncServiceQuota{}
	case common.ServerTypeDeepDocCPU:
		serviceQuotaInstance.deepDocCPUQuota = &deepDocCPUQuota{}
	case common.ServerTypeDeepDocGPU:
		serviceQuotaInstance.deepDocGPUQuota = &deepDocGPUQuota{}
	}
}

func updateAPIServiceQuota(quota APIServerHeartbeatResponse) {

	serviceQuotaInstance.apiServiceQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.apiServiceQuota.MaxConcurrency = quota.MaxConcurrency
	serviceQuotaInstance.apiServiceQuota.Features = NewServiceFeatures(quota.Features)
	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func updateIngestorQuota(quota IngestorHeartbeatResponse) {
	serviceQuotaInstance.ingestorQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.ingestorQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.ingestorQuota.Features = NewServiceFeatures(quota.Features)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func updateSyncerQuota(quota FileSyncerHeartbeatResponse) {
	serviceQuotaInstance.syncerQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.syncerQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.syncerQuota.Features = NewServiceFeatures(quota.Features)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func updateDeepDocCPUQuota(quota DeepDocCPUHeartbeatResponse) {
	serviceQuotaInstance.deepDocCPUQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.deepDocCPUQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.deepDocCPUQuota.MaxConcurrency = quota.MaxConcurrency
	serviceQuotaInstance.deepDocCPUQuota.Features = NewServiceFeatures(quota.Features)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func updateDeepDocGPUQuota(quota DeepDocGPUHeartbeatResponse) {
	serviceQuotaInstance.deepDocGPUQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.deepDocGPUQuota.GPUNum = quota.GPUNum
	serviceQuotaInstance.deepDocGPUQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.deepDocGPUQuota.MaxConcurrency = quota.MaxConcurrency
	serviceQuotaInstance.deepDocGPUQuota.Features = NewServiceFeatures(quota.Features)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

// UpdateServiceQuota decodes the heartbeat acknowledgement of the reporting
// server type and refreshes the quota snapshot it carries.
func UpdateServiceQuota(serverType common.ServerType, payload []byte) error {
	switch serverType {
	case common.ServerTypeAPI:
		var resp APIServerHeartbeatResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			return err
		}
		common.Info("API server heartbeat response", zap.Any("resp", resp))
		updateAPIServiceQuota(resp)

	case common.ServerTypeIngestion:
		var resp IngestorHeartbeatResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			return err
		}
		common.Info("Ingestion server heartbeat response", zap.Any("resp", resp))
		updateIngestorQuota(resp)

	case common.ServerTypeFileSyncer:
		var resp FileSyncerHeartbeatResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			return err
		}
		common.Info("FileSyncer server heartbeat response", zap.Any("resp", resp))
		updateSyncerQuota(resp)

	case common.ServerTypeDeepDocCPU:
		var resp DeepDocCPUHeartbeatResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			return err
		}
		common.Info("DeepDocCPU server heartbeat response", zap.Any("resp", resp))
		updateDeepDocCPUQuota(resp)

	case common.ServerTypeDeepDocGPU:
		var resp DeepDocGPUHeartbeatResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			return err
		}
		common.Info("DeepDocGPU server heartbeat response", zap.Any("resp", resp))
		updateDeepDocGPUQuota(resp)

	default:
		return fmt.Errorf("unknown server type: %s", serverType)
	}
	return nil
}
