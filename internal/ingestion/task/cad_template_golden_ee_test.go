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

package task

import "testing"

// The cad template is registered by the enterprise pipeline package, so its
// golden entry is added at init instead of living in the shared table: the
// community build has neither the template nor this entry, and
// TestBuildComponentParams_GoldenCoversAllTemplates stays in lockstep in both
// builds.
func init() {
	builtinComponentParamsGolden["cad"] = `{"CADChunker:SpatialSheet":{},"Extractor:AutoExtractDefault":{"keywords":{"top_n":0},"llm_id":"","metadata":{"built_in_metadata":[],"enabled":false,"metadata":[]},"questions":{"top_n":0},"summary":{"enabled":false},"tags":{"tag_file_id":"","top_n":0}},"File":{},"Parser:PaperTrail":{"cad":{},"enable_vision_enhancement":false,"vlm":{"llm_id":""}},"Tokenizer:IndexText":{"fields":"text","filename_embd_weight":0.1,"search_method":["embedding","full_text"]}}`
}

func TestBuildComponentParams_CAD(t *testing.T) { assertTemplateComponentParams(t, "cad") }
