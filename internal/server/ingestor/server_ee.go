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

package ingestor

// init adds the enterprise CAD extensions to the ingestor's accepted document
// set so CAD uploads pass the supported-type gate. The community base set is
// ["pdf","docx","txt"] (extensions); the CAD extensions are appended to the
// neutral OSS extraSupportedTypes map (OSS PR #22208), leaving the community
// base unchanged.
func init() {
	RegisterSupportedType("dwg")
	RegisterSupportedType("dxf")
	RegisterSupportedType("dxfb")
}
