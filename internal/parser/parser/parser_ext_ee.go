//go:build ee

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

package parser

import "ragflow/internal/utility"

// init wires the enterprise CAD file type into the neutral OSS registries so
// the community GetFileType/FilenameType and newParser routes never change:
// the EE edition only fills the extension, parser-factory and supported-type
// maps. This is the enterprise-superset split mandated for file-type
// extension (OSS PR #22208).
func init() {
	utility.RegisterExtensionFileType("dwg", utility.FileTypeCAD)
	utility.RegisterExtensionFileType("dxf", utility.FileTypeCAD)
	utility.RegisterExtensionFileType("dxfb", utility.FileTypeCAD)
	RegisterParserFactory(utility.FileTypeCAD,
		func(textOnly bool) (ParseResultProducer, error) { return NewCADParser(textOnly), nil })
}
