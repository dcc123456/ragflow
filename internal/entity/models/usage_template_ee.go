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

package models

import (
	"time"

	"ragflow/internal/common"
)

func init() {
	usageTemplateResolver = resolveUsageTemplate
}

// resolveUsageTemplate resolves the usage record a typed model forwards to
// its driver for one call. An explicit usage wins; its model-group identity is
// topped up from the model info so every call path (including callers that
// build their own usage record) records which gp- group mediated the call.
// A nil usage falls back to the construction-time template the factory
// installed on the model info, cloned with the call-scoped fields reset, so
// call sites that pass nil still produce an attributable usage row (tenant,
// user, provider, instance, model, group).
func resolveUsageTemplate(info *ModelInfo, usage *common.ModelUsage) *common.ModelUsage {
	if usage != nil {
		if usage.GroupID == "" && info != nil && info.GroupID != "" {
			usage.GroupID = info.GroupID
			usage.GroupName = info.GroupName
		}
		return usage
	}
	if info == nil || info.UsageTemplate == nil {
		return nil
	}
	clone := *info.UsageTemplate
	clone.InputTokens, clone.OutputTokens, clone.TotalTokens = 0, 0, 0
	clone.RequestID = ""
	clone.ResponseTimeMS = 0
	clone.ErrorMessage = ""
	clone.StartAt = time.Time{}
	return &clone
}
