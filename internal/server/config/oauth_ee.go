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

package config

import "github.com/spf13/viper"

type OAuthConfig struct {
	Channels map[string]OAuthChannel `mapstructure:"oauth"`
}

// OAuthChannel OAuth configuration for a channel.
type OAuthChannel struct {
	DisplayName      string `mapstructure:"display_name"`
	Icon             string `mapstructure:"icon"`
	Type             string `mapstructure:"type"`
	ClientID         string `mapstructure:"client_id"`
	ClientSecret     string `mapstructure:"client_secret"`
	AuthorizationURL string `mapstructure:"authorization_url"`
	TokenURL         string `mapstructure:"token_url"`
	UserinfoURL      string `mapstructure:"userinfo_url"`
	RedirectURI      string `mapstructure:"redirect_uri"`
	Scope            string `mapstructure:"scope"`
	Issuer           string `mapstructure:"issuer"`
}

func (c *Config) ParseOAuthConfig(v *viper.Viper) error {
	if !v.IsSet("oauth") {
		return nil
	}
	sub := v.Sub("oauth")
	if sub == nil {
		return nil
	}
	c.oAuth.Channels = make(map[string]OAuthChannel)
	err := sub.Unmarshal(&c.oAuth.Channels)
	if err != nil {
		return err
	}
	return nil
}
