//go:build cgo

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
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, OR THIS SOFTWARE
//  DISTRIBUTED UNDER THE LICENSE IS PROVIDED ON AN "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package main

import (
	"context"
	"fmt"

	"ragflow/internal/common"
	"ragflow/internal/deepdoc/native"
	infnative "ragflow/internal/deepdoc/parser/pdf/inference/native_analyzer"
	deepdocserver "ragflow/internal/deepdoc/server"
	"ragflow/internal/server/bootstrap"
)

// runDeepDoc boots the EE DeepDoc HTTP inference server. It is the single
// runtime activation point for the EE deployment: it switches the in-process
// DeepDoc backend to the EE weights and post-processors (the OSS build never
// calls it), then serves /predict/{dla,tsr,ocr_det,ocr_rec} and /health.
//
// The DeepDoc inference server is intentionally standalone: it only needs the
// statically-linked ONNX Runtime and MODEL_DIR, never the full RAGFlow
// infrastructure (DB/engine/cache/storage/MQ). See the early-return branch in
// cmd/ragflow_server.go's main() that calls this and then exits before the
// heavy infrastructure init runs.
func runDeepDoc(ctx context.Context, serverName string, args *bootstrap.ServerArgs) error {
	// Switch the in-process DeepDoc backend to the EE weights and
	// post-processors. This must run before any inference.
	native.InitCPUEE()
	infnative.InitCPUEE()

	port := deepdocserver.DefaultPort
	if args.Port != nil && *args.Port > 0 {
		port = *args.Port
	}

	common.Info(fmt.Sprintf("Starting EE DeepDoc inference server %s on port %d", serverName, port))
	return deepdocserver.Run(ctx, port)
}
