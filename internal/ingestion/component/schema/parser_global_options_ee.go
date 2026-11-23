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

package schema

// ParserGlobalOptions is the enterprise counterpart of the !ee stub. The
// enterprise build keeps the same behaviour: no parser parameter is promoted to
// a global option, so the cleaned params carry every key through the normal
// path. Giving the enterprise build its own global-option set is what this
// seam is for; until then the stub's answers are the correct ones.
func ParserGlobalOptions(map[string]any) map[string]bool { return nil }

// ApplyParserGlobalOptions is the enterprise counterpart of the !ee stub and
// applies nothing, matching ParserGlobalOptions above.
func ApplyParserGlobalOptions(map[string]any, map[string]ParserSetup) {}
