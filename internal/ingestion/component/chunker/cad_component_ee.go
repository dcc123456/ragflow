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

package chunker

import (
	"context"
	"fmt"

	"ragflow/internal/agent/runtime"
	"ragflow/internal/common"
	"ragflow/internal/ingestion/component/schema"

	"gorm.io/gorm"
)

// ComponentNameCADChunker is the DSL component name of the CAD pipeline's
// chunker node. It is registered into the shared canonical name set at init so
// the builtin template passes component-ID validation without adding an
// enterprise name to shared code.
const ComponentNameCADChunker = "CADChunker"

// cadChunkerComponent runs the spatial CAD chunker over the parser's
// structured drawing payload. The spacing/limit knobs live in
// defaultCADChunkerConfig.
type cadChunkerComponent struct{}

// Invoke chunks one parsed drawing. The cad pipeline only runs for CAD
// documents. A missing payload or a drawing without searchable text produces
// an explicit error for the ingestion task.
func (c *cadChunkerComponent) Invoke(ctx context.Context, _ *gorm.DB, inputs map[string]any) (map[string]any, error) {
	upstream, err := decodeChunkerFromUpstream(inputs)
	if err != nil {
		return nil, fmt.Errorf("CADChunker: decode input: %w", err)
	}
	out, handled, err := chunkCAD(ctx, upstream)
	if err != nil {
		return nil, err
	}
	if !handled {
		return nil, fmt.Errorf("CADChunker: upstream carries no structured CAD payload")
	}
	chunks, _ := schema.GetChunks(out)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("CADChunker: no searchable text was found in this CAD drawing")
	}
	return out, nil
}

// init registers the component directly instead of going through
// MustRegisterChunker: newChunkerByName is a shared file, and the CAD chunker
// exists only in the enterprise edition. The image-upload decorator is the
// same wrapper every registered chunker gets.
func init() {
	common.RegisterComponentName(ComponentNameCADChunker)
	runtime.MustRegister(ComponentNameCADChunker, runtime.CategoryIngestion,
		func(_ string, _ map[string]any) (runtime.Component, error) {
			return &imageUploadDecorator{inner: &cadArtifactDecorator{inner: &cadChunkerComponent{}}}, nil
		},
		runtime.Metadata{
			Version: "1.0.0",
			Inputs:  ChunkerInputs,
			Outputs: ChunkerOutputs,
		})
}
