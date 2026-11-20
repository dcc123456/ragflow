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

package admin

import (
	"encoding/json"
	"fmt"
	"ragflow/internal/common"
	"ragflow/internal/utility"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ServerStatusStore is a thread-safe global server status storage
type ServerStatusStore struct {
	mu      sync.RWMutex
	servers map[string]*common.BaseMessage // key: server_id

	CurrentLicense *utility.LicenseV2
	LicenseCode    common.ErrorCode
}

// GlobalServerStatusStore is the global instance
var GlobalServerStatusStore = &ServerStatusStore{
	servers: make(map[string]*common.BaseMessage),
}

// UpdateStatus updates or adds a server status
func (s *ServerStatusStore) UpdateStatus(serverName string, status *common.BaseMessage) (map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get server count
	apiServerCount := 0
	ingestorServerCount := 0
	fileSyncerServerCount := 0
	deepDocCPUServerCount := 0
	deepDocGPUServerCount := 0

	for _, server := range s.servers {
		if server.Ext == nil {
			continue
		}
		if server.ServerType == common.ServerTypeAPI {
			apiServerCount++
		} else if server.ServerType == common.ServerTypeIngestion {
			ingestorServerCount++
		} else if server.ServerType == common.ServerTypeFileSyncer {
			fileSyncerServerCount++
		} else if server.ServerType == common.ServerTypeDeepDocCPU {
			deepDocCPUServerCount++
		} else if server.ServerType == common.ServerTypeDeepDocGPU {
			deepDocGPUServerCount++
		}
	}

	extData, err := json.Marshal(status.Ext)
	if err != nil {
		return nil, err
	}

	var serverStatusResponse map[string]interface{}

	// Get cluster info from heartbeat
	switch status.ServerType {
	case common.ServerTypeAPI:
		// API server
		if s.CurrentLicense.APIQuota.Count > 0 {
			if _, ok := s.servers[serverName]; !ok {
				if apiServerCount+1 > s.CurrentLicense.APIQuota.Count {
					return nil, fmt.Errorf("API server count exceeds quota")
				}
			}
		}

		var apiServerStatus utility.CPUServerStatus
		if err = json.Unmarshal(extData, &apiServerStatus); err != nil {
			return nil, err
		}
		common.Info("Update API server status", zap.String("status", apiServerStatus.PrintAll()))
		apiServerResponse := utility.APIServerHeartbeatResponse{
			CPUNum:         s.CurrentLicense.APIQuota.CPUNum,
			MaxConcurrency: s.CurrentLicense.APIQuota.MaxConcurrency,
			Features:       s.CurrentLicense.Features,
			LicenseType:    string(s.CurrentLicense.Type),
		}
		serverStatusResponse = apiServerResponse.ToMap()

	case common.ServerTypeIngestion:
		// Ingestion server
		if s.CurrentLicense.IngestorQuota.Count > 0 {
			if _, ok := s.servers[serverName]; !ok {
				if ingestorServerCount+1 > s.CurrentLicense.IngestorQuota.Count {
					return nil, fmt.Errorf("ingestor server count exceeds quota")
				}
			}
		}

		var ingestorStatus utility.CPUServerStatus
		if err = json.Unmarshal(extData, &ingestorStatus); err != nil {
			return nil, err
		}
		common.Info("Update Ingestion server status", zap.String("status", ingestorStatus.PrintAll()))
		ingestorResponse := utility.IngestorHeartbeatResponse{
			CPUNum:      s.CurrentLicense.IngestorQuota.CPUNum,
			WorkerNum:   s.CurrentLicense.IngestorQuota.WorkerNum,
			Features:    s.CurrentLicense.Features,
			LicenseType: string(s.CurrentLicense.Type),
		}
		serverStatusResponse = ingestorResponse.ToMap()
	case common.ServerTypeFileSyncer:
		// File syncer server
		if s.CurrentLicense.SyncerQuota.Count > 0 {
			if _, ok := s.servers[serverName]; !ok {
				if fileSyncerServerCount+1 > s.CurrentLicense.SyncerQuota.Count {
					return nil, fmt.Errorf("file syncer server count exceeds quota")
				}
			}
		}

		var fileSyncerStatus utility.CPUServerStatus
		if err = json.Unmarshal(extData, &fileSyncerStatus); err != nil {
			return nil, err
		}
		common.Info("Update File syncer server status", zap.String("status", fileSyncerStatus.PrintAll()))
		fileSyncerResponse := utility.FileSyncerHeartbeatResponse{
			CPUNum:      s.CurrentLicense.SyncerQuota.CPUNum,
			WorkerNum:   s.CurrentLicense.SyncerQuota.WorkerNum,
			Features:    s.CurrentLicense.Features,
			LicenseType: string(s.CurrentLicense.Type),
		}
		serverStatusResponse = fileSyncerResponse.ToMap()
	case common.ServerTypeChatter:
		var chatterStatus utility.CPUServerStatus
		if err = json.Unmarshal(extData, &chatterStatus); err != nil {
			return nil, err
		}
		common.Info("Update Chatter server status", zap.String("status", chatterStatus.PrintAll()))
		chatterResponse := utility.ChatterHeartbeatResponse{
			Features:    s.CurrentLicense.Features,
			LicenseType: string(s.CurrentLicense.Type),
		}
		serverStatusResponse = chatterResponse.ToMap()
	case common.ServerTypeDeepDocCPU:
		// DeepDoc CPU server
		if s.CurrentLicense.DeepDocCPUQuota.Count > 0 {
			if _, ok := s.servers[serverName]; !ok {
				if deepDocCPUServerCount+1 > s.CurrentLicense.DeepDocCPUQuota.Count {
					return nil, fmt.Errorf("deepDocCPU server count exceeds quota")
				}
			}
		}

		var deepDocCPUStatus utility.CPUServerStatus
		if err = json.Unmarshal(extData, &deepDocCPUStatus); err != nil {
			return nil, err
		}
		common.Info("Update DeepDoc CPU server status", zap.String("status", deepDocCPUStatus.PrintAll()))
		deepDocCPUResponse := utility.DeepDocCPUHeartbeatResponse{
			CPUNum:         s.CurrentLicense.DeepDocCPUQuota.CPUNum,
			MaxConcurrency: s.CurrentLicense.DeepDocCPUQuota.MaxConcurrency,
			WorkerNum:      s.CurrentLicense.DeepDocCPUQuota.WorkerNum,
			Features:       s.CurrentLicense.Features,
			LicenseType:    string(s.CurrentLicense.Type),
		}
		serverStatusResponse = deepDocCPUResponse.ToMap()
	case common.ServerTypeDeepDocGPU:
		// DeepDoc GPU server
		if s.CurrentLicense.DeepDocGPUQuota.Count > 0 {
			if _, ok := s.servers[serverName]; !ok {
				if deepDocGPUServerCount+1 > s.CurrentLicense.DeepDocGPUQuota.Count {
					return nil, fmt.Errorf("deepDocGPU server count exceeds quota")
				}
			}
		}

		var deepDocGPUStatus utility.GPUServerStatus
		if err = json.Unmarshal(extData, &deepDocGPUStatus); err != nil {
			return nil, err
		}
		common.Info("Update DeepDoc GPU server status", zap.String("status", deepDocGPUStatus.PrintAll()))
		deepDocGPUResponse := utility.DeepDocGPUHeartbeatResponse{
			CPUNum:         s.CurrentLicense.DeepDocGPUQuota.CPUNum,
			GPUNum:         s.CurrentLicense.DeepDocGPUQuota.GPUNum,
			MaxConcurrency: s.CurrentLicense.DeepDocGPUQuota.MaxConcurrency,
			WorkerNum:      s.CurrentLicense.DeepDocGPUQuota.WorkerNum,
			Features:       s.CurrentLicense.Features,
			LicenseType:    string(s.CurrentLicense.Type),
		}
		serverStatusResponse = deepDocGPUResponse.ToMap()
	default:
		common.Fatal("Unknown server type", zap.Any("serverType", status.ServerType))
		return nil, err
	}
	s.servers[serverName] = status

	return serverStatusResponse, nil
}

// GetStatus gets a single server status
func (s *ServerStatusStore) GetStatus(serverName string) (*common.BaseMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status, ok := s.servers[serverName]
	return status, ok
}

// GetAllStatuses gets all server statuses
func (s *ServerStatusStore) GetAllStatuses() []*common.BaseMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*common.BaseMessage, 0, len(s.servers))
	for _, status := range s.servers {
		result = append(result, status)
	}
	return result
}

// GetStatusesByType gets server statuses by type
func (s *ServerStatusStore) GetStatusesByType(serverType common.ServerType) []*common.BaseMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*common.BaseMessage, 0)
	for _, status := range s.servers {
		if status.ServerType == serverType {
			result = append(result, status)
		}
	}
	return result
}

// RemoveStatus removes a server status
func (s *ServerStatusStore) RemoveStatus(serverID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.servers, serverID)
}

// CleanupStaleStatuses cleans up servers that haven't reported for a specified duration
func (s *ServerStatusStore) CleanupStaleStatuses(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, status := range s.servers {
		if now.Sub(status.Timestamp) > maxAge {
			delete(s.servers, id)
		}
	}
}

func (s *ServerStatusStore) SetLicense(license *utility.LicenseV2) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentLicense = license
	s.LicenseCode = common.CodeLicenseValid
}

func (s *ServerStatusStore) SetLicenseStatusCode(code common.ErrorCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LicenseCode = code
}

func (s *ServerStatusStore) GetLicense() *utility.LicenseV2 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CurrentLicense
}

func (s *ServerStatusStore) GetLicenseCode() common.ErrorCode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LicenseCode
}

func (s *ServerStatusStore) GetFeatures() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CurrentLicense.Features
}
