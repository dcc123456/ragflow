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

//go:build ee

package common

// EEDeepDocBackends enumerates the EE HTTP DeepDoc inference services the
// deployment can route to. Every entry carries the parse-method id, the env
// var holding its base URL, and the UI label. Both the handler's analyzer
// probe (internal/handler/system_doc_analyzers_ee.go) and the parser's
// dispatch (internal/parser/parser/pdf_deepdoc_dispatch_ee.go) read this
// single list, so the advertised analyzers and the parser registration can
// never drift apart.
var EEDeepDocBackends = []struct {
	ID     string
	URLEnv string
	Label  string
}{
	{"deepdoc_cpu_ee", "DEEPDOC_CPU_EE_URL", "DeepDoc EE (CPU)"},
	{"deepdoc_gpu_ee", "DEEPDOC_GPU_EE_URL", "DeepDoc EE (GPU)"},
	{"deepdoc_ascend_ee", "DEEPDOC_ASCEND_EE_URL", "DeepDoc EE (Ascend)"},
}
