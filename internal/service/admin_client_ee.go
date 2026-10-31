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

package service

import (
	"encoding/json"
	"fmt"

	"ragflow/internal/common"
	"ragflow/internal/utility"
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

// UpdateClient applies the heartbeat acknowledgement the admin server sends
// back to the reporting process.
func (h *AdminClient) UpdateClient(clientInfo any) error {
	payload, err := json.Marshal(clientInfo)
	if err != nil {
		return err
	}
	return utility.UpdateServiceQuota(h.serverType, payload)
}
