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

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"testing"

	"ragflow/internal/common"

	"github.com/gin-gonic/gin"
)

func TestWhitelistRoutes(t *testing.T) {
	originalMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })

	handler := &Handler{}
	type routeMatch struct {
		Handler string
		ID      string
	}
	engine := gin.New()
	group := engine.Group("/api/v1/admin", func(c *gin.Context) {
		// Observe route dispatch independently of service behavior.
		c.AbortWithStatusJSON(http.StatusOK, routeMatch{
			Handler: c.HandlerName(), ID: c.Param("id"),
		})
	})
	RegisterEERouter(group, NewRouter(handler))

	protected := gin.New()
	RegisterEERouter(protected.Group("/api/v1/admin", handler.AuthMiddleware()), NewRouter(handler))

	for _, tc := range []struct {
		name    string
		method  string
		path    string
		handler gin.HandlerFunc
		id      string
	}{
		{name: "list", method: http.MethodGet, path: "/whitelist", handler: handler.ShowWhiteList},
		{name: "add", method: http.MethodPost, path: "/whitelist", handler: handler.AddWhiteList},
		{name: "import", method: http.MethodPost, path: "/whitelist/batch", handler: handler.BatchAddWhiteList},
		{name: "update by id", method: http.MethodPut, path: "/whitelist/42", handler: handler.UpdateWhiteList, id: "42"},
		{name: "delete by id", method: http.MethodDelete, path: "/whitelist/42", handler: handler.DeleteWhiteList, id: "42"},
		{name: "batch delete", method: http.MethodDelete, path: "/whitelist/batch", handler: handler.BatchDeleteWhiteList},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := "/api/v1/admin" + tc.path
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(tc.method, path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("%s %s status = %d, body = %s", tc.method, path, response.Code, response.Body.String())
			}
			var got routeMatch
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := routeMatch{Handler: runtime.FuncForPC(reflect.ValueOf(tc.handler).Pointer()).Name(), ID: tc.id}
			if got != want {
				t.Fatalf("%s %s matched %+v, want %+v", tc.method, path, got, want)
			}

			response = httptest.NewRecorder()
			protected.ServeHTTP(response, httptest.NewRequest(tc.method, path, nil))
			var result struct {
				Code common.ErrorCode `json:"code"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Code != common.CodeUnauthorized {
				t.Fatalf("%s %s without authorization = %s, want unauthorized", tc.method, path, response.Body.String())
			}
		})
	}
}
