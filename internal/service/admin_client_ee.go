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

package service

import (
	"encoding/json"
	"fmt"
	"ragflow/internal/common"
	"ragflow/internal/utility"

	"go.uber.org/zap"
)

func (h *AdminClient) InitHTTPClientEE() error {
	return nil
}

func (h *AdminClient) EnrichMessage(message *common.BaseMessage) (*common.BaseMessage, error) {
	fingerprint := utility.NewFingerprintCollector(true)
	var err error

	switch h.serverType {
	case common.ServerTypeAPI, common.ServerTypeIngestion, common.ServerTypeFileSyncer, common.ServerTypeDeepDocCPU:
		message.Ext, err = fingerprint.NewCPUServerStatus()
	case common.ServerTypeDeepDocGPU:
		message.Ext, err = fingerprint.NewGPUServerStatus()
	default:
		return nil, fmt.Errorf("unknown server type: %s", h.serverType)
	}
	if err != nil {
		return nil, err
	}
	return message, nil
}

func (h *AdminClient) UpdateClient(clientInfo any) error {

	bytes, err := json.Marshal(clientInfo)
	if err != nil {
		return err
	}

	switch h.serverType {
	case common.ServerTypeAPI:
		var resp utility.APIServerHeartbeatResponse
		if err = json.Unmarshal(bytes, &resp); err != nil {
			return err
		}
		common.Info("API server heartbeat response", zap.Any("resp", resp))
		utility.UpdateAPIServiceQuota(resp)

	case common.ServerTypeIngestion:
		var resp utility.IngestorHeartbeatResponse
		if err = json.Unmarshal(bytes, &resp); err != nil {
			return err
		}
		common.Info("Ingestion server heartbeat response", zap.Any("resp", resp))
		utility.UpdateIngestorQuota(resp)

	case common.ServerTypeFileSyncer:
		var resp utility.FileSyncerHeartbeatResponse
		if err = json.Unmarshal(bytes, &resp); err != nil {
			return err
		}
		common.Info("FileSyncer server heartbeat response", zap.Any("resp", resp))
		utility.UpdateSyncerQuota(resp)

	case common.ServerTypeDeepDocCPU:
		var resp utility.DeepDocCPUHeartbeatResponse
		if err = json.Unmarshal(bytes, &resp); err != nil {
			return err
		}
		common.Info("DeepDocCPU server heartbeat response", zap.Any("resp", resp))
		utility.UpdateDeepDocCPUQuota(resp)

	case common.ServerTypeDeepDocGPU:
		var resp utility.DeepDocGPUHeartbeatResponse
		if err = json.Unmarshal(bytes, &resp); err != nil {
			return err
		}
		common.Info("DeepDocGPU server heartbeat response", zap.Any("resp", resp))
		utility.UpdateDeepDocGPUQuota(resp)

	default:
		return fmt.Errorf("unknown server type: %s", h.serverType)
	}
	return nil
}
