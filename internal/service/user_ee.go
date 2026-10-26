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

package service

import (
	"sort"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/server"
)

type LoginChannel struct {
	Channel     string `json:"channel"`
	DisplayName string `json:"display_name"`
	Icon        string `json:"icon"`
}

// GetLoginChannels gets all supported authentication channels. The result is
// sorted by channel name so the login page renders a stable order across
// requests (Go map iteration order is randomized).
func (s *UserService) GetLoginChannels() ([]*LoginChannel, common.ErrorCode, error) {
	channels := make([]*LoginChannel, 0)

	cfg := server.GetConfig()
	if cfg != nil {
		for channel, oauthCfg := range cfg.GetOAuthChannels() {
			displayName := oauthCfg.DisplayName
			if displayName == "" {
				displayName = strings.Title(channel)
			}
			icon := oauthCfg.Icon
			if icon == "" {
				icon = "sso"
			}
			channels = append(channels, &LoginChannel{
				Channel:     channel,
				DisplayName: displayName,
				Icon:        icon,
			})
		}
	}
	sort.Slice(channels, func(i, j int) bool {
		return channels[i].Channel < channels[j].Channel
	})

	return channels, common.CodeSuccess, nil
}
