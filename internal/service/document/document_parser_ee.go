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

package document

import "ragflow/internal/utility"

// dedicatedParserExtension binds enterprise drawing formats (CAD and Visio)
// to their dedicated built-in pipelines.
//
// The filetype argument is the upload category, and every unrecognised binary
// extension lands in the visual bucket, so a .dxf arrives as FileTypeVISUAL.
// The semantic type therefore has to come from the filename registry, the same
// way the presentation branch resolves .pptx.
func dedicatedParserExtension(_ utility.FileType, filename string) string {
	switch utility.GetFileType(filename) {
	case utility.FileTypeCAD:
		return "cad"
	case utility.FileTypeVSDX:
		return "visio"
	}
	return ""
}

// inheritFallbackParserOptions is the enterprise counterpart of the !ee stub.
// The enterprise build inherits nothing from the dataset setting here, so the
// cleaned params keep only what the caller already put in them. Binding the
// dataset's fallback parser options to the enterprise pipeline's Extractor node
// is what this seam is for; until then the stub's no-op is the correct answer.
func inheritFallbackParserOptions(map[string]any, map[string]any, string) {}
