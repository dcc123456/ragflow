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

package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestChannelCallbackCodeReadsDingtalkAuthCode pins the one provider-specific
// query spelling the callback accepts: DingTalk names the authorization code
// `authCode` where every other provider uses `code`, and `code` wins when both
// are present.
func TestChannelCallbackCodeReadsDingtalkAuthCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "standard code", raw: "/cb?code=abc&state=s", want: "abc"},
		{name: "dingtalk authCode", raw: "/cb?authCode=abc&state=s", want: "abc"},
		{name: "code wins over authCode", raw: "/cb?code=std&authCode=dt", want: "std"},
		{name: "empty query", raw: "/cb", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", tc.raw, nil)
			if got := channelCallbackCode(c); got != tc.want {
				t.Errorf("channelCallbackCode = %q, want %q", got, tc.want)
			}
		})
	}
}
