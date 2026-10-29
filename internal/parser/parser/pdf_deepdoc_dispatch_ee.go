//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package parser

import (
	"os"

	"ragflow/internal/deepdoc/client"
	deepdoctype "ragflow/internal/deepdoc/parser/type"
)

// deepDocEEBaseURL mirrors the DEEPDOC_EE_URL endpoint for the EE DeepDoc
// inference service. The parser resolves external analyzers through the generic
// <METHOD>_URL environment convention (see externalAnalyzerURL), so this
// variable is a convenience mirror kept in sync by SetDeepDocEEURL and used by
// tests to save/restore state. It is inert unless a PDFParser is configured
// with parse_method "DeepDoc_EE" and the endpoint is set.
var deepDocEEBaseURL = os.Getenv("DEEPDOC_EE_URL")

// SetDeepDocEEURL configures the EE DeepDoc inference endpoint. It is the
// single setter the EE deployment and tests use; the parser's dispatch reads it
// back through the generic DEEPDOC_EE_URL env convention.
func SetDeepDocEEURL(url string) {
	deepDocEEBaseURL = url
	if url == "" {
		os.Unsetenv("DEEPDOC_EE_URL")
	} else {
		os.Setenv("DEEPDOC_EE_URL", url)
	}
}

func init() {
	// Register DeepDoc_EE as an external analyzer method routed to the EE HTTP
	// inference service (ragflow/internal/deepdoc/server, spoken to via
	// client). The OSS in-process "deepdoc" path is untouched, and this
	// registration is inert unless a PDFParser is configured with parse_method
	// "DeepDoc_EE" and DEEPDOC_EE_URL is set.
	RegisterAnalyzerFactory("deepdoc_ee", func(baseURL string) (deepdoctype.DocAnalyzer, error) {
		return client.New(baseURL), nil
	})
}
