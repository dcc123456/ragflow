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
	"ragflow/internal/deepdoc/client"
	deepdoctype "ragflow/internal/deepdoc/parser/type"
)

// eeDeepDocMethods are the EE DeepDoc parse methods routed to the EE HTTP
// inference service (a single binary serving cpu/gpu/ascend variants). They
// share one client factory -- only the endpoint (<METHOD>_URL) and the UI label
// differ -- so they are registered by the same loop in init. Every method is
// fully suffixed (cpu/gpu/ascend): there is no un-suffixed "deepdoc_ee" alias and
// no DEEPDOC_EE_URL, so every EE identifier carries its hardware suffix. Kept as
// data (not inline) so the test suite can assert dispatch for exactly these
// methods and never drift from the registration.
var eeDeepDocMethods = []string{"deepdoc_cpu_ee", "deepdoc_gpu_ee", "deepdoc_ascend_ee"}

func init() {
	// Register the EE DeepDoc variants (cpu/gpu/ascend) as external analyzer
	// methods. They are served by the same EE inference binary; only the UI
	// label and the endpoint (<METHOD>_URL) differ, so they share one client
	// factory. Each variant resolves through the generic <METHOD>_URL env
	// convention (see externalAnalyzerURL), and fails loudly (never the
	// in-process native path) when its URL is unset.
	//
	// There is deliberately no un-suffixed "deepdoc_ee" / "DeepDoc_EE" alias and
	// no DEEPDOC_EE_URL: the UI selects an explicit hardware variant, and the
	// parser only speaks suffixed methods.
	for _, m := range eeDeepDocMethods {
		RegisterAnalyzerFactory(m, func(baseURL string) (deepdoctype.DocAnalyzer, error) {
			return client.New(baseURL), nil
		})
	}
}
