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

//go:build cgo && ee

package native

import (
	"fmt"
	"strings"

	ort "github.com/infiniflow/onnxruntime_go"
)

// Temporary CPU-only device-seam placeholder until the EE GPU execution
// provider implementation lands.

func normalizeDevice(_ DeviceConfig) DeviceConfig {
	return DeviceConfig{}
}

func parseDeviceHook(spec string) (DeviceConfig, error) {
	s := strings.ToLower(strings.TrimSpace(spec))
	if s == "" || s == "cpu" {
		return DeviceConfig{}, nil
	}
	return DeviceConfig{}, fmt.Errorf("deepdoc device %q is not supported yet in this build (use cpu)", spec)
}

func applyDeviceHook(_ *ort.SessionOptions) error {
	return nil
}

func registerEPHook() error {
	return nil
}
