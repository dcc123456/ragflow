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

// The resume extractor mode reads its own sub-object key ("resume"), which
// the shared schema does not declare. Whitelist it so an operator-form
// parser_config save keeps the key, and clone it in NormalizeExtractorParams
// like the shared modular sub-objects.
func init() {
	extractorValidParamKeys["resume"] = struct{}{}
	extractorParamCloneKeys = append(extractorParamCloneKeys, "resume")
}
