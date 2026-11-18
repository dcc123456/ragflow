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
	"encoding/json"
	"fmt"
	"maps"

	"ragflow/internal/agent/runtime"
	"ragflow/internal/common"
	"ragflow/internal/ingestion/component"
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
func (c *cadChunkerComponent) Invoke(ctx context.Context, db *gorm.DB, inputs map[string]any) (map[string]any, error) {
	upstream, err := decodeChunkerFromUpstream(inputs)
	if err != nil {
		return nil, fmt.Errorf("CADChunker: decode input: %w", err)
	}
	out, drawing, err := chunkCAD(ctx, upstream)
	if err != nil {
		return nil, err
	}
	if drawing == nil {
		return nil, fmt.Errorf("CADChunker: upstream carries no structured CAD payload")
	}
	chunks, _ := schema.GetChunks(out)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("CADChunker: no searchable text was found in this CAD drawing")
	}
	return attachCADArtifacts(ctx, db, inputs, upstream, drawing.Sheets, out, chunks)
}

// cadChunkerParam configures optional enhancement of rendered chunk images.
type cadChunkerParam struct {
	Vision component.CADVisionParam `json:"vision"`
}

type cadVisionDecorator struct {
	inner runtime.Component
	param component.CADVisionParam
}

func (d *cadVisionDecorator) Invoke(ctx context.Context, db *gorm.DB, inputs map[string]any) (map[string]any, error) {
	out, err := d.inner.Invoke(ctx, db, inputs)
	if err != nil || out == nil || !d.param.Enabled {
		return out, err
	}
	chunks, ok := schema.GetChunks(out)
	if !ok {
		return nil, fmt.Errorf("CADChunker: expected chunks after upload")
	}
	warnings, err := component.EnhanceCADChunks(ctx, db, inputs, chunks, d.param)
	if err != nil {
		return nil, err
	}
	out["chunks"] = chunks
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	return out, nil
}

// Enhancement runs after rendering and upload, preserving stable chunk IDs.
func init() {
	outputs := maps.Clone(ChunkerOutputs)
	outputs["warnings"] = "Optional list of visual enhancement warnings."
	common.RegisterComponentName(ComponentNameCADChunker)
	runtime.MustRegister(ComponentNameCADChunker, runtime.CategoryIngestion,
		func(_ string, params map[string]any) (runtime.Component, error) {
			var param cadChunkerParam
			data, err := json.Marshal(params)
			if err == nil {
				err = json.Unmarshal(data, &param)
			}
			if err != nil {
				return nil, fmt.Errorf("CADChunker: invalid configuration: %w", err)
			}
			return &cadVisionDecorator{
				inner: &imageUploadDecorator{inner: &cadChunkerComponent{}},
				param: param.Vision,
			}, nil
		},
		runtime.Metadata{Version: "1.0.0", Inputs: ChunkerInputs, Outputs: outputs})
}
