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

// EE DeepDoc runtime activation for the native analyzer package.
//
// Compiled in BOTH the OSS and EE cgo builds (only gated by `cgo`, never by a
// feature tag). InitCPUEE is only called by the EE entry point
// (cmd/deepdoc_server_ee.go's runDeepDoc, via ragflow_server --deepdoc) at
// startup; the OSS build never calls it, so the OSS TSR→cell mapping is
// preserved. See internal/deepdoc/native/inference_ee.go for the same pattern
// on the other seams.
package infnative

import (
	"ragflow/internal/deepdoc/native"
	deepdoctype "ragflow/internal/deepdoc/parser/type"
)

// tsrCellsFromBoxesEE is the EE override of tsrCellsFn. It has the same seam
// signature as the OSS default (a native.TSRResult in, clamped internally) but
// additionally carries the detection Score (native.TSRBox.Score), which the EE
// deployment's downstream table assembly consumes.
func tsrCellsFromBoxesEE(res native.TSRResult) []deepdoctype.TSRCell {
	out := make([]deepdoctype.TSRCell, 0, len(res.Boxes))
	for _, b := range res.ClampBoxes() {
		out = append(out, deepdoctype.TSRCell{
			X0: float64(b.X0), Y0: float64(b.Top),
			X1: float64(b.X1), Y1: float64(b.Bottom),
			Label: b.Label, Score: float64(b.Score),
		})
	}
	return out
}

// InitCPUEE overrides the shared tsrCellsFn with the EE mapping, which carries
// the detection Score in addition to the OSS geometry. It must be called once at
// startup, before any inference, and never mutated during serving.
func InitCPUEE() {
	tsrCellsFn = tsrCellsFromBoxesEE
}
