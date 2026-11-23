//go:build ee

//
// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package parser

// wordParserOptions is the enterprise counterpart of the !ee stub. The
// enterprise build keeps the same behaviour: no extra Word configuration is
// applied and the IR passes through unchanged, so the shared DOC/DOCX parsers
// produce the same result in both editions. The type exists here only because
// the stub is excluded under the ee tag; putting the enterprise-only Word
// options in this file is what the seam is for.
type wordParserOptions struct{}

func (*wordParserOptions) configure(map[string]any) {}

func (wordParserOptions) prepareIR(irJSON string, _ []byte) (string, bool) {
	return irJSON, false
}
