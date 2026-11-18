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
	case common.ServerTypeAPI, common.ServerTypeIngestion, common.ServerTypeFileSyncer, common.ServerTypeChatter, common.ServerTypeDeepDocCPU:
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
