package admin

import (
	"encoding/json"
	"ragflow/internal/common"
	"ragflow/internal/logger"
	"ragflow/internal/utility"
	"sync"
	"time"
)

// ServerStatusStore is a thread-safe global server status storage
type ServerStatusStore struct {
	mu            sync.RWMutex
	servers       map[string]*common.BaseMessage // key: server_id
	clusterInfo   utility.ClusterInfo
	licenseStatus utility.LicenseStatus
}

// GlobalServerStatusStore is the global instance
var GlobalServerStatusStore = &ServerStatusStore{
	servers: make(map[string]*common.BaseMessage),
}

// Get cluster info of the cluster
func (s *ServerStatusStore) GetClusterInfo() utility.ClusterInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clusterInfo
}

// UpdateStatus updates or adds a server status
func (s *ServerStatusStore) UpdateStatus(serverName string, status *common.BaseMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.servers[serverName] = status

	extData, err := json.Marshal(status.Ext)
	if err != nil {
		return
	}

	var clusterInfo utility.ClusterInfo
	if err := json.Unmarshal(extData, &clusterInfo); err != nil {
		return
	}

	systemInfo := clusterInfo.SystemInfos[0]
	var found = false
	for _, info := range s.clusterInfo.SystemInfos {
		if systemInfo.MachineID != info.MachineID {
			continue
		}
		if systemInfo.BoardSerial != info.BoardSerial {
			continue
		}
		if systemInfo.CPU != info.CPU {
			continue
		}

		// Check if the disk info are same
		if systemInfo.Disk != info.Disk {
			continue
		}

		if systemInfo.Network != info.Network {
			continue
		}

		if systemInfo.Memory != info.Memory {
			continue
		}

		found = true
		break
	}

	if !found {
		s.clusterInfo.SystemInfos = append(s.clusterInfo.SystemInfos, systemInfo)
		// Sort system infos, order by MachineID, BoardSerial, CPU, Disk, MAC of Network, and MemoryInfo
		utility.SortSystemInfos(s.clusterInfo.SystemInfos)
		logger.Debug("Adding new system info to cluster info")
	}
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

func (s *ServerStatusStore) SetLicenseStatus(status utility.LicenseStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.licenseStatus = status
}

func (s *ServerStatusStore) GetLicenseStatus() utility.LicenseStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.licenseStatus
}
