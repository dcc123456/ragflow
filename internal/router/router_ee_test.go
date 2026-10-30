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

package router

import (
	"strings"
	"testing"

	"ragflow/internal/handler"

	"github.com/gin-gonic/gin"
)

// TestEENoAuthRoutesHaveNoOAuthCallbackShadow guards the OAuth callback table.
//
// gin resolves a static segment before a parameter at the same position, so a
// concrete "/auth/oauth/<provider>/callback" registration silently shadows the
// "/auth/oauth/:channel/callback" route for that provider. When the shadowing
// handler was a not-implemented stub, every provider login completed at the
// identity provider and then failed on the callback.
func TestEENoAuthRoutesHaveNoOAuthCallbackShadow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// Route registration only takes method values, so a zero-value handler is
	// enough to build the table without wiring services.
	r := &Router{userHandler: &handler.UserHandler{}}
	RegisterEENoAuthRouter(engine, engine.Group("/api/v1"), r)

	// A shadow has to occupy the same segment slot as :channel, so compare
	// against that route's shape rather than matching a prefix: the shorter
	// "/auth/oauth/callback" route is a distinct path, not a shadow.
	paramSegments := strings.Split("/api/v1/auth/oauth/:channel/callback", "/")

	var hasParamCallback bool
	var shadows []string
	for _, route := range engine.Routes() {
		if route.Path == "/api/v1/auth/oauth/:channel/callback" {
			hasParamCallback = true
			continue
		}

		segments := strings.Split(route.Path, "/")
		if len(segments) != len(paramSegments) {
			continue
		}
		matches := true
		for i := range segments {
			if paramSegments[i] != ":channel" && segments[i] != paramSegments[i] {
				matches = false
				break
			}
		}
		if matches {
			shadows = append(shadows, route.Path)
		}
	}

	if !hasParamCallback {
		t.Fatal("the generic OAuth callback route is missing")
	}
	if len(shadows) > 0 {
		t.Fatalf("static OAuth callback routes shadow :channel: %v", shadows)
	}
}
