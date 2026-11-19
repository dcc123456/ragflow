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
// structured drawing payload. Blocks are cut at rendered ink gutters; there are
// no spacing or character knobs.
type cadChunkerComponent struct{}

// Invoke chunks one parsed drawing. The cad pipeline only runs for CAD
// documents. A missing payload is a wiring defect and fails the task; a
// drawing without searchable text (pure 3D solids, symbol-only sheets)
// yields per-sheet geometry summary chunks, and only a drawing with no
// decoded geometry at all succeeds with zero chunks and a warning, so the
// rendered preview that was already built above stays available instead of
// failing the document.
func (c *cadChunkerComponent) Invoke(ctx context.Context, db *gorm.DB, inputs map[string]any) (map[string]any, error) {
	upstream, err := decodeChunkerFromUpstream(inputs)
	if err != nil {
		return nil, fmt.Errorf("CADChunker: decode input: %w", err)
	}
	drawing, err := decodeCADDrawing(ctx, upstream)
	if err != nil {
		return nil, err
	}
	if drawing == nil {
		return nil, fmt.Errorf("CADChunker: upstream carries no structured CAD payload")
	}

	// Parse and render once, before chunking: chunk boundaries need the sheet
	// ink, and the same render feeds chunk crops and the document preview.
	docCAD, _, loadErr := loadCADDocument(ctx, db, upstream)
	var preview *cadPreviewResult
	var ink *cadInkIndex
	if loadErr != nil {
		common.Warn("CADChunker: load source: " + loadErr.Error())
	} else if docCAD != nil {
		preview, err = buildCADPreview(ctx, docCAD, drawing.Sheets, cadPreviewPageWidth)
		if err != nil {
			common.Warn("CADChunker: build preview for ink: " + err.Error())
			preview = nil
		} else {
			ink = buildCADInk(preview.Pages)
		}
	}

	out, _, err := chunkCADDrawing(ctx, upstream, drawing, ink)
	if err != nil {
		return nil, err
	}
	chunks, _ := schema.GetChunks(out)
	if len(chunks) == 0 {
		warning := "CADChunker: no searchable text was found in this CAD drawing; only the rendered preview is kept"
		common.Warn(warning)
		runtime.ReportProgressMessage(ctx, ComponentNameCADChunker, "WARNING: "+warning)
		out, err = attachCADArtifacts(ctx, db, inputs, upstream, out, chunks, docCAD, preview)
		if err != nil {
			return nil, err
		}
		out["warnings"] = []string{warning}
		return out, nil
	}
	return attachCADArtifacts(ctx, db, inputs, upstream, out, chunks, docCAD, preview)
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
