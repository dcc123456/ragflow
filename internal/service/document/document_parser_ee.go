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

// dedicatedParserExtension binds a CAD drawing to the cad built-in pipeline.
// The CAD parser is enterprise-only, so this is the only file type the
// enterprise edition adds to the community routing.
//
// The filetype argument is the upload category, and every unrecognised binary
// extension lands in the visual bucket, so a .dxf arrives as FileTypeVISUAL.
// The semantic type therefore has to come from the filename registry, the same
// way the presentation branch resolves .pptx.
func dedicatedParserExtension(_ utility.FileType, filename string) string {
	if utility.GetFileType(filename) == utility.FileTypeCAD {
		return "cad"
	}
	return ""
}
