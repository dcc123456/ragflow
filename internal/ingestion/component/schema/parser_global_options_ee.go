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

package schema

import "ragflow/internal/parser/parser"

// ParserGlobalOptions returns the edition's explicitly set parser-wide options
// for persistence.
func ParserGlobalOptions(params map[string]any) map[string]bool {
	if enabled, ok := parser.WordTextFilterSetting(params); ok {
		return map[string]bool{parser.WordTextFilterOption: enabled}
	}
	return nil
}

// ApplyParserGlobalOptions passes parser-wide settings to the applicable
// format parsers. The parser-wide value also determines the disabled default.
func ApplyParserGlobalOptions(params map[string]any, setups map[string]ParserSetup) {
	enabled, _ := parser.WordTextFilterSetting(params)
	for _, family := range [...]string{"doc", "docx"} {
		setups[family][parser.WordTextFilterOption] = enabled
	}
}
