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

package pipeline

import "testing"

// The resume builtin template lives in template_ee/, which the shared
// template-integration tests do not glob.
func TestBuiltinTemplateResume_LoadsAndCompiles(t *testing.T) {
	reg, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry: %v", err)
	}
	if !reg.IsValid("resume") {
		t.Fatal("DefaultRegistry.IsValid(\"resume\") = false, want true")
	}
	dsl, err := LoadBuiltinDSL("resume")
	if err != nil {
		t.Fatalf("LoadBuiltinDSL(resume): %v", err)
	}
	pipe, err := NewPipelineFromDSL([]byte(dsl), "resume-compile-check")
	if err != nil {
		t.Fatalf("NewPipelineFromDSL(resume): %v", err)
	}
	if pipe == nil {
		t.Fatal("NewPipelineFromDSL(resume) returned nil pipeline")
	}
	cfg, err := BuildParserConfig([]byte(dsl), nil)
	if err != nil {
		t.Fatalf("BuildParserConfig(resume): %v", err)
	}
	extCfg, ok := cfg["Extractor:AutoExtractDefault"].(map[string]any)
	if !ok {
		t.Fatalf("Extractor:AutoExtractDefault missing from parser_config: %#v", cfg)
	}
	resumeCfg, ok := extCfg["resume"].(map[string]any)
	if !ok || resumeCfg["enabled"] != true {
		t.Fatalf("Extractor resume config = %#v, want enabled=true", extCfg["resume"])
	}
	tokCfg, ok := cfg["Tokenizer:ResumeTokenizer"].(map[string]any)
	if !ok || tokCfg["fields"] != "embedding_text" {
		t.Fatalf("Tokenizer config = %#v, want fields=embedding_text", cfg["Tokenizer:ResumeTokenizer"])
	}

	// The operator form must keep the resume sub-object: it is whitelisted
	// at init (extractor_params_ee.go), not by the shared schema.
	maintained, err := BuildParserConfig([]byte(dsl), map[string]interface{}{
		"Extractor:AutoExtractDefault": map[string]interface{}{
			"resume": map[string]any{"enabled": false},
		},
	})
	if err != nil {
		t.Fatalf("BuildParserConfig(resume, operator form): %v", err)
	}
	extOverride, ok := maintained["Extractor:AutoExtractDefault"].(map[string]any)
	if !ok {
		t.Fatalf("parser_config after override: %#v", maintained)
	}
	if resumeOverride, ok := extOverride["resume"].(map[string]any); !ok || resumeOverride["enabled"] != false {
		t.Fatalf("operator-form resume override was dropped: %#v", extOverride["resume"])
	}
}
