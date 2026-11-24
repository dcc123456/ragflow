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

import (
	"ragflow/internal/common"
	"ragflow/internal/parser/parser"
	"ragflow/internal/utility"
)

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
	case utility.FileTypeVisio:
		return "visio"
	}
	return ""
}

// A document assigned a different Word pipeline keeps the source Parser's
// explicit filter setting. For multiple source nodes, an opt-in wins.
func inheritFallbackParserOptions(target, source map[string]any, family string) {
	if family != "doc" && family != "docx" {
		return
	}
	var enabled, specified bool
	for id, raw := range source {
		if !common.IsParserComponentID(id) {
			continue
		}
		params, _ := nodeParams(raw)
		if sourceEnabled, ok := parser.WordTextFilterSetting(params); ok {
			specified = true
			enabled = enabled || sourceEnabled
		}
	}
	if !specified {
		return
	}
	for id, raw := range target {
		if !common.IsParserComponentID(id) {
			continue
		}
		if params, ok := nodeParams(raw); ok {
			params[parser.WordTextFilterOption] = enabled
		}
	}
}
