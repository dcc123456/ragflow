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

package codexagent

import (
	"testing"

	codexgo "github.com/yuzhichang/codex-app-server-go"
)

// The configured posture must reach thread/start verbatim, and anything unrecognized
// must not widen the surface beyond the read-only default.
func TestThreadSandboxMode(t *testing.T) {
	cases := []struct {
		configured string
		want       codexgo.SandboxMode
	}{
		{"", codexgo.SandboxReadOnly},
		{"read-only", codexgo.SandboxReadOnly},
		{"workspace-write", codexgo.SandboxWorkspaceWrite},
		{"danger-full-access", codexgo.SandboxFullAccess},
		{"DANGER-FULL-ACCESS", codexgo.SandboxReadOnly},
		{"bogus", codexgo.SandboxReadOnly},
	}
	for _, tc := range cases {
		if got := threadSandboxMode(tc.configured); got != tc.want {
			t.Errorf("threadSandboxMode(%q) = %q, want %q", tc.configured, got, tc.want)
		}
	}
}
