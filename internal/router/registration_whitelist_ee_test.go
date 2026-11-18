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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/handler"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestRouterSetupEnforcesRegistrationWhitelist(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.WhiteList{}, &entity.SystemSettings{}, &entity.User{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	setting := entity.SystemSettings{Name: dao.WhitelistEnabledSettingName, DataType: "bool", Value: "true"}
	if err := db.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}

	mode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(mode) })
	originalLogger := common.Logger
	common.Logger = zap.NewNop()
	t.Cleanup(func() { common.Logger = originalLogger })

	engine := gin.New()
	r := &Router{
		authHandler: handler.NewAuthHandler(),
		userHandler: handler.NewUserHandler(service.NewUserService(), service.NewUserTenantService()),
	}
	r.Setup(engine)

	// Discover the real signup routes so moving or aliasing them also tests the gate.
	register := reflect.ValueOf(r.userHandler.Register).Pointer()
	var routes []gin.RouteInfo
	for _, route := range engine.Routes() {
		if reflect.ValueOf(route.HandlerFunc).Pointer() == register {
			routes = append(routes, route)
		}
	}
	if len(routes) == 0 {
		t.Fatal("Router.Setup did not register UserHandler.Register")
	}
	for _, route := range routes {
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			// Omit credentials so a bypass reaches validation without creating an account.
			req := httptest.NewRequest(route.Method, route.Path, strings.NewReader(`{"email":"outsider@example.com"}`))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			if response.Code != http.StatusForbidden {
				t.Fatalf("registration status = %d, body = %s; want 403", response.Code, response.Body.String())
			}
			var result struct {
				Code    common.ErrorCode `json:"code"`
				Message string           `json:"message"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatalf("decode registration rejection: %v", err)
			}
			if result.Code != common.CodeForbidden || result.Message != "Email outsider@example.com isn't in whitelist." {
				t.Fatalf("registration rejection = %+v; want whitelist error", result)
			}
		})
	}
	var users int64
	if err := db.Model(&entity.User{}).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatalf("blocked registration created %d users", users)
	}
}
