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

package table

import (
	"context"
	"image"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

// tsrClient is the minimal EE TSR surface DeepDocVHTableBuilder depends on.
// The production *client.Client satisfies it; tests inject a fake.
type tsrClient interface {
	TSR(ctx context.Context, img image.Image) ([]pdf.TSRCell, error)
}

// DeepDocVHTableBuilder implements pdf.TableBuilder for the EE 2-class V/H
// separator model served by the deepdoc_cpu_ee (et al.) inference service. It
// is selected only when the DocAnalyzer is the EE HTTP client, so the OSS
// in-process path (DeepDocTableBuilder) is never affected.
//
// GroupCells caches the merged-cell geometry (spanInfo/covered) that
// ExpandGrid derives, so the EE HTML post-processing can render spans without
// going through the OSS geometry-based CalSpans.
type DeepDocVHTableBuilder struct {
	Client tsrClient

	spanInfo map[[2]int][2]int
	covered  map[[2]int]bool
}

func NewDeepDocVHTableBuilder(c tsrClient) *DeepDocVHTableBuilder {
	return &DeepDocVHTableBuilder{Client: c}
}

func (b *DeepDocVHTableBuilder) Name() string { return "deepdoc_vh" }

// DetectCells calls the EE TSR service (2-class v/h separators) and assembles
// the result into spanned cells via VHTableAssembler.
func (b *DeepDocVHTableBuilder) DetectCells(ctx context.Context, cropped image.Image) ([]pdf.TSRCell, error) {
	wire, err := b.Client.TSR(ctx, cropped)
	if err != nil {
		return nil, err
	}
	return VHTableAssembler{}.Assemble(wire, float64(cropped.Bounds().Dy())), nil
}

// GroupCells expands the spanned cell list into the 2D grid that downstream
// HTML rendering consumes, and caches the merged-cell geometry for the EE
// post-processing step.
func (b *DeepDocVHTableBuilder) GroupCells(cells []pdf.TSRCell) [][]pdf.TSRCell {
	grid, covered, spanInfo := VHTableAssembler{}.ExpandGrid(cells)
	b.covered = covered
	b.spanInfo = spanInfo
	return grid
}

// VHSpanInfo returns the cached merged-cell span map (keyed by [row,col]).
// It is only populated after GroupCells has run.
func (b *DeepDocVHTableBuilder) VHSpanInfo() map[[2]int][2]int { return b.spanInfo }

// VHCovered returns the cached covered-position map (keyed by [row,col]).
func (b *DeepDocVHTableBuilder) VHCovered() map[[2]int]bool { return b.covered }

// FinalizeGrid overlays OCR text onto the geometry-driven VH grid using the
// cached spanInfo/covered, so the EE HTML post-processing renders spans from
// a grid whose origin cells carry the right OCR text. This is the real
// deferred OCR-overlay step that the OSS DeepDocTableBuilder performs inline
// (and therefore implements as the identity function).
func (b *DeepDocVHTableBuilder) FinalizeGrid(annotGrid [][]pdf.TSRCell, boxes []pdf.TextBox) [][]pdf.TSRCell {
	return OverlayOCRText(annotGrid, boxes, b.VHSpanInfo(), b.VHCovered())
}

// AttachSpanInfo flags the item so the shared ConstructTable dispatch routes
// it to the EE V/H renderer (ConstructTableVH). The OSS DeepDocTableBuilder
// implements this as a no-op because its TableItem carries no such geometry
// and its RenderVH stays false.
func (b *DeepDocVHTableBuilder) AttachSpanInfo(item *pdf.TableItem) {
	item.RenderVH = true
}
