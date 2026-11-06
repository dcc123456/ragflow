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
