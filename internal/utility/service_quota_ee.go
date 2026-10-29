//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package utility

import (
	"ragflow/internal/common"
	"strings"
	"time"
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

func GetAPIServiceQuota() *APIServiceQuota {
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

func UpdateAPIServiceQuota(quota APIServerHeartbeatResponse) {

	serviceQuotaInstance.apiServiceQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.apiServiceQuota.MaxConcurrency = quota.MaxConcurrency
	serviceQuotaInstance.apiServiceQuota.Features = NewServiceFeatures(quota.Features)
	serviceQuotaInstance.LastUpdateTime = time.Now()

	// 0 will disable concurrency limit
	GetRateLimit().UpdateGlobalConcurrencyLimit(uint64(serviceQuotaInstance.apiServiceQuota.MaxConcurrency))
}

func UpdateIngestorQuota(quota IngestorHeartbeatResponse) {
	serviceQuotaInstance.ingestorQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.ingestorQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.ingestorQuota.Features = NewServiceFeatures(quota.Features)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func UpdateSyncerQuota(quota FileSyncerHeartbeatResponse) {
	serviceQuotaInstance.syncerQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.syncerQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.syncerQuota.Features = NewServiceFeatures(quota.Features)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func UpdateDeepDocCPUQuota(quota DeepDocCPUHeartbeatResponse) {
	serviceQuotaInstance.deepDocCPUQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.deepDocCPUQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.deepDocCPUQuota.MaxConcurrency = quota.MaxConcurrency
	serviceQuotaInstance.deepDocCPUQuota.Features = NewServiceFeatures(quota.Features)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func UpdateDeepDocGPUQuota(quota DeepDocGPUHeartbeatResponse) {
	serviceQuotaInstance.deepDocGPUQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.deepDocGPUQuota.GPUNum = quota.GPUNum
	serviceQuotaInstance.deepDocGPUQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.deepDocGPUQuota.MaxConcurrency = quota.MaxConcurrency
	serviceQuotaInstance.deepDocGPUQuota.Features = NewServiceFeatures(quota.Features)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}
