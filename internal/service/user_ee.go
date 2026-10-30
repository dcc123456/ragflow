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

package service

import (
	"context"
	"sort"
	"strings"

	"ragflow/internal/common"
)

type LoginChannel struct {
	Channel     string `json:"channel"`
	DisplayName string `json:"display_name"`
	Icon        string `json:"icon"`
}

// GetLoginChannels gets all enabled authentication channels. The result is
// sorted by channel name so the login page renders a stable order across
// requests (Go map iteration order is randomized).
//
// The caller is the render-facing handler, which has no request-scoped purpose
// of its own to thread through, so the settings read uses a background context.
func (s *UserService) GetLoginChannels() ([]*LoginChannel, common.ErrorCode, error) {
	configured := loadSSOChannels(context.Background())
	channels := make([]*LoginChannel, 0, len(configured))
	for channel := range configured {
		channels = append(channels, &LoginChannel{
			Channel:     channel,
			DisplayName: strings.Title(channel),
			Icon:        "sso",
		})
	}
	sort.Slice(channels, func(i, j int) bool {
		return channels[i].Channel < channels[j].Channel
	})

	return channels, common.CodeSuccess, nil
}
