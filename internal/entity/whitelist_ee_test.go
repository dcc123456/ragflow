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
//

//go:build ee

package entity

import (
	"strings"
	"testing"
)

func TestValidateWhitelistEmail(t *testing.T) {
	for _, test := range []struct {
		value string
		valid bool
	}{
		{"alice@example.com", true},
		{"Alice@Example.COM", true},
		{"alice.smith_2-test@sub.example.co", true},
		{"a@b.co", true},
		{strings.Repeat("a", 243) + "@example.com", true},
		{strings.Repeat("字", 243) + "@example.com", true},
		{"a..b@example.com", true},
		{".alice@example.com", true},
		{"alice.@example.com", true},
		{"alice@exam_ple.com", true},
		{"alice@-example.com", true},
		{"álice@example.com", true},
		{"用户@例子.公司", true},
		{"²user@example.com", true},
		{"", false},
		{" alice@example.com", false},
		{"alice@example.com ", false},
		{"\talice@example.com\n", false},
		{"a@b.c", false},
		{"alice+tag@example.com", false},
		{"Alice <alice@example.com>", false},
		{`"alice"@example.com`, false},
		{"alice(comment)@example.com", false},
		{"alice@example.com.", false},
		{"alice@localhost", false},
		{"e\u0301@example.com", false},
		{strings.Repeat("a", 244) + "@example.com", false},
		{strings.Repeat("字", 244) + "@example.com", false},
	} {
		t.Run(test.value, func(t *testing.T) {
			if err := ValidateWhitelistEmail(test.value); (err == nil) != test.valid {
				t.Fatalf("email validation = %v, want valid %v", err, test.valid)
			}
		})
	}
}
