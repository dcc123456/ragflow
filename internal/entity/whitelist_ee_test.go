//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
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
