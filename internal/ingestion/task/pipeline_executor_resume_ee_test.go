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

// The resume template's golden entry joins the shared map from this init;
// the shared test file only carries the always-embedded templates.
func init() {
	builtinComponentParamsGolden["resume"] = "{\"File\":{},\"Parser:ResumeParseFile\":{\"enable_vision_enhancement\":false,\"vlm\":{\"llm_id\":\"\"},\"doc\":{},\"docx\":{},\"image\":{\"ocr_enabled\":true},\"pdf\":{\"parse_method\":\"DeepDOC\"},\"text&code\":{}},\"OneChunker:ResumeOneChunk\":{},\"Tokenizer:ResumeTokenizer\":{\"fields\":\"embedding_text\",\"filename_embd_weight\":0,\"search_method\":[\"embedding\",\"full_text\"]},\"Extractor:AutoExtractDefault\":{\"resume\":{\"enabled\":true}}}"
}

func TestBuildComponentParams_Resume(t *testing.T) { assertTemplateComponentParams(t, "resume") }
