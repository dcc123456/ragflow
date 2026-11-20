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
	"reflect"
	"strings"
	"time"

	"ragflow/internal/common"

	"go.uber.org/zap"
)

type ServiceFeatures struct {
	SingleTenant  bool `json:"single_tenant"`
	RBAC          bool `json:"rbac"`
	GroupAndShare bool `json:"group_and_share"`
	DeepDocPro    bool `json:"deepdoc_pro"`
	DeepDocGPU    bool `json:"deepdoc_gpu"`
	WPS           bool `json:"wps"`
	CAD           bool `json:"cad"`
	Evaluation    bool `json:"evaluation"`
	MultiModal    bool `json:"multi_modal"`
	Stats         bool `json:"stats"`
	AnalyticsLog  bool `json:"analytics_log"`
}

func (f *ServiceFeatures) IsSingleTenantEnabled() bool {
	return f.SingleTenant
}

func (f *ServiceFeatures) IsRBACEnabled() bool {
	return f.RBAC
}

func (f *ServiceFeatures) IsGroupAndShareEnabled() bool {
	return f.GroupAndShare
}

func (f *ServiceFeatures) IsDeepDocProEnabled() bool {
	return f.DeepDocPro
}

func (f *ServiceFeatures) IsDeepDocGPUEnabled() bool {
	return f.DeepDocGPU
}

func (f *ServiceFeatures) IsWPSEnabled() bool {
	return f.WPS
}

func (f *ServiceFeatures) IsCADEnabled() bool {
	return f.CAD
}

func (f *ServiceFeatures) IsEvaluationEnabled() bool {
	return f.Evaluation
}

func (f *ServiceFeatures) IsMultiModalEnabled() bool {
	return f.MultiModal
}

func (f *ServiceFeatures) IsStatsEnabled() bool {
	return f.Stats
}

func (f *ServiceFeatures) IsAnalyticsLogEnabled() bool {
	return f.AnalyticsLog
}

func NewServiceFeatures(features map[string]bool) ServiceFeatures {
	f := ServiceFeatures{}
	for feature, enable := range features {
		switch strings.ToLower(feature) {
		case "single_tenant":
			f.SingleTenant = enable
		case "rbac":
			f.RBAC = enable
		case "group_and_share":
			f.GroupAndShare = enable
		case "deepdoc_pro":
			f.DeepDocPro = enable
		case "deepdoc_gpu":
			f.DeepDocGPU = enable
		case "wps":
			f.WPS = enable
		case "cad":
			f.CAD = enable
		case "evaluation":
			f.Evaluation = enable
		case "multi_modal":
			f.MultiModal = enable
		case "stats":
			f.Stats = enable
		case "analytics_log":
			f.AnalyticsLog = enable
		}
	}
	return f
}

func (f *ServiceFeatures) ToMap() map[string]interface{} {
	out := make(map[string]interface{})
	t := reflect.TypeOf(*f)
	v := reflect.ValueOf(*f)
	for i := 0; i < t.NumField(); i++ {
		jsonName := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if jsonName == "" || jsonName == "-" {
			continue
		}
		out[jsonName] = v.Field(i).Interface()
	}
	return out
}

type APIServiceQuota struct {
	LicenseType    LicenseType     `json:"license_type"`
	Features       ServiceFeatures `json:"features"`
	MaxConcurrency int             `json:"max_concurrency"`
	CPUNum         int             `json:"cpu_num"`
}

func (f *APIServiceQuota) IsSaaSMode() bool {
	return f.LicenseType == SAAS
}

type IngestionServiceQuota struct {
	LicenseType LicenseType     `json:"license_type"`
	Features    ServiceFeatures `json:"features"`
	CPUNum      int             `json:"cpu_num"`
	WorkerNum   int             `json:"worker_num"`
}

type SyncServiceQuota struct {
	LicenseType LicenseType     `json:"license_type"`
	Features    ServiceFeatures `json:"features"`
	CPUNum      int             `json:"cpu_num"`
	WorkerNum   int             `json:"worker_num"`
}

type ChatterServiceQuota struct {
	LicenseType LicenseType     `json:"license_type"`
	Features    ServiceFeatures `json:"features"`
}

type deepDocCPUQuota struct {
	LicenseType    LicenseType     `json:"license_type"`
	Features       ServiceFeatures `json:"features"`
	MaxConcurrency int             `json:"max_concurrency"`
	CPUNum         int             `json:"cpu_num"`
	WorkerNum      int             `json:"worker_num"`
}

type deepDocGPUQuota struct {
	LicenseType    LicenseType     `json:"license_type"`
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
	chatterQuota    *ChatterServiceQuota
	deepDocCPUQuota *deepDocCPUQuota
	deepDocGPUQuota *deepDocGPUQuota
	LastUpdateTime  time.Time
}

// GetAPIServiceQuota returns the API server quota snapshot, or nil when this
// process is not an API server.
func GetAPIServiceQuota() *APIServiceQuota {
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
	case common.ServerTypeChatter:
		serviceQuotaInstance.chatterQuota = &ChatterServiceQuota{}
	}
}

func updateAPIServiceQuota(quota APIServerHeartbeatResponse) {

	serviceQuotaInstance.apiServiceQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.apiServiceQuota.MaxConcurrency = quota.MaxConcurrency
	serviceQuotaInstance.apiServiceQuota.Features = NewServiceFeatures(quota.Features)
	serviceQuotaInstance.apiServiceQuota.LicenseType = LicenseType(quota.LicenseType)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func updateIngestorQuota(quota IngestorHeartbeatResponse) {
	serviceQuotaInstance.ingestorQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.ingestorQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.ingestorQuota.Features = NewServiceFeatures(quota.Features)
	serviceQuotaInstance.ingestorQuota.LicenseType = LicenseType(quota.LicenseType)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func updateSyncerQuota(quota FileSyncerHeartbeatResponse) {
	serviceQuotaInstance.syncerQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.syncerQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.syncerQuota.Features = NewServiceFeatures(quota.Features)
	serviceQuotaInstance.syncerQuota.LicenseType = LicenseType(quota.LicenseType)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func updateChatterQuota(quota ChatterHeartbeatResponse) {
	serviceQuotaInstance.chatterQuota.Features = NewServiceFeatures(quota.Features)
	serviceQuotaInstance.chatterQuota.LicenseType = LicenseType(quota.LicenseType)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func updateDeepDocCPUQuota(quota DeepDocCPUHeartbeatResponse) {
	serviceQuotaInstance.deepDocCPUQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.deepDocCPUQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.deepDocCPUQuota.MaxConcurrency = quota.MaxConcurrency
	serviceQuotaInstance.deepDocCPUQuota.Features = NewServiceFeatures(quota.Features)
	serviceQuotaInstance.deepDocCPUQuota.LicenseType = LicenseType(quota.LicenseType)

	serviceQuotaInstance.LastUpdateTime = time.Now()
}

func updateDeepDocGPUQuota(quota DeepDocGPUHeartbeatResponse) {
	serviceQuotaInstance.deepDocGPUQuota.CPUNum = quota.CPUNum
	serviceQuotaInstance.deepDocGPUQuota.GPUNum = quota.GPUNum
	serviceQuotaInstance.deepDocGPUQuota.WorkerNum = quota.WorkerNum
	serviceQuotaInstance.deepDocGPUQuota.MaxConcurrency = quota.MaxConcurrency
	serviceQuotaInstance.deepDocGPUQuota.Features = NewServiceFeatures(quota.Features)
	serviceQuotaInstance.deepDocGPUQuota.LicenseType = LicenseType(quota.LicenseType)

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

	case common.ServerTypeChatter:
		var resp ChatterHeartbeatResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			return err
		}
		common.Info("Chatter server heartbeat response", zap.Any("resp", resp))
		updateChatterQuota(resp)

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
