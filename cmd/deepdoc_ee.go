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

package main

import (
	"context"

	deepdocserver "ragflow/internal/deepdoc/server"
	"ragflow/internal/server/bootstrap"
)

// runDeepDoc boots the EE DeepDoc inference server for the --deepdoc mode: the
// enterprise counterpart of stub.go's runDeepDoc. The package resolves the port,
// and reports the capability as unavailable when this tree is built without cgo.
func runDeepDoc(ctx context.Context, serverName string, args *bootstrap.ServerArgs) error {
	return deepdocserver.RunServer(ctx, serverName, args.Port)
}
