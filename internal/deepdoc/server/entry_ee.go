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
func RunServer(ctx context.Context, serverName string, port *int, ready func() error) error {
	listenPort := DefaultPort
	if port != nil && *port > 0 {
		listenPort = *port
	}

	native.InitCPUEE()
	infnative.InitCPUEE()

	common.Info(fmt.Sprintf("Starting EE DeepDoc inference server %s on port %d", serverName, listenPort))
	return run(ctx, listenPort, ready)
}
