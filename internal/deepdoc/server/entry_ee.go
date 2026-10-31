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

//go:build cgo

package deepdocserver

import (
	"context"
	"fmt"

	"ragflow/internal/common"
	"ragflow/internal/deepdoc/native"
	infnative "ragflow/internal/deepdoc/parser/pdf/inference/native_analyzer"
)

// DefaultPort is the listen port for the DeepDoc inference server.
const DefaultPort = 8001

// RunServer boots the EE DeepDoc HTTP inference server: it switches the
// in-process DeepDoc backend to the EE weights and post-processors (before any
// inference), then serves /predict/{dla,tsr,ocr_det,ocr_rec} and /health. A nil
// or non-positive port keeps DefaultPort.
//
// The DeepDoc inference server is intentionally standalone: it only needs the
// statically-linked ONNX Runtime and MODEL_DIR, never the full RAGFlow
// infrastructure (DB/engine/cache/storage/MQ).
func RunServer(ctx context.Context, serverName string, port *int) error {
	listenPort := DefaultPort
	if port != nil && *port > 0 {
		listenPort = *port
	}

	native.InitCPUEE()
	infnative.InitCPUEE()

	common.Info(fmt.Sprintf("Starting EE DeepDoc inference server %s on port %d", serverName, listenPort))
	return Run(ctx, listenPort)
}
