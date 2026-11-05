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

package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/handler"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/service"
	"ragflow/internal/utility"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
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

func TestRouterSetupLimitsRegistrationBeforeWhitelist(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.WhiteList{}, &entity.SystemSettings{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	if err := db.Create(&entity.SystemSettings{Name: dao.WhitelistEnabledSettingName, DataType: "bool", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.WhiteList{Email: "allowed@example.com"}).Error; err != nil {
		t.Fatal(err)
	}

	mode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(mode) })
	originalLogger := common.Logger
	common.Logger = zap.NewNop()
	t.Cleanup(func() { common.Logger = originalLogger })

	limiter := utility.GetRateLimit()
	originalRate, _ := limiter.GlobalRateLimitStatus()
	originalConcurrency, _ := limiter.GlobalConcurrencyLimitStatus()
	limiter.DisableGlobalRateLimit()
	limiter.DisableGlobalConcurrencyLimit()
	t.Cleanup(func() {
		if originalRate > 0 {
			limiter.UpdateGlobalRateLimit(originalRate)
		} else {
			limiter.DisableGlobalRateLimit()
		}
		limiter.UpdateGlobalConcurrencyLimit(originalConcurrency)
	})

	engine := gin.New()
	r := &Router{
		authHandler: handler.NewAuthHandler(),
		userHandler: handler.NewUserHandler(service.NewUserService(), service.NewUserTenantService()),
	}
	r.Setup(engine)

	var queries atomic.Int64
	if err := db.Callback().Query().After("gorm:query").Register("test:count_whitelist_queries", func(*gorm.DB) {
		queries.Add(1)
	}); err != nil {
		t.Fatal(err)
	}

	register := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}
	assertLimited := func(t *testing.T) {
		t.Helper()
		queries.Store(0)
		for _, body := range []string{`{"email":"outsider@example.com"}`, `{"email":"allowed@example.com"}`, `{`} {
			response := register(body)
			if response.Code != http.StatusTooManyRequests {
				t.Errorf("registration %s status = %d, body = %s; want 429", body, response.Code, response.Body.String())
			}
		}
		if count := queries.Load(); count != 0 {
			t.Errorf("limited registration requests executed %d database queries", count)
		}
	}

	t.Run("rate", func(t *testing.T) {
		limiter.UpdateGlobalRateLimit(rate.Every(time.Hour))
		t.Cleanup(limiter.DisableGlobalRateLimit)
		response := register(`{"email":"outsider@example.com"}`)
		if response.Code != http.StatusForbidden {
			t.Fatalf("first registration status = %d, body = %s; want 403", response.Code, response.Body.String())
		}
		assertLimited(t)
	})

	t.Run("concurrency", func(t *testing.T) {
		limiter.UpdateGlobalConcurrencyLimit(1)
		t.Cleanup(limiter.DisableGlobalConcurrencyLimit)
		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan *httptest.ResponseRecorder, 1)
		engine.GET("/test/hold-concurrency-slot", func(c *gin.Context) {
			close(entered)
			<-release
			c.Status(http.StatusNoContent)
		})
		t.Cleanup(func() {
			close(release)
			select {
			case response := <-done:
				if response.Code != http.StatusNoContent {
					t.Errorf("holding request status = %d; want 204", response.Code)
				}
			case <-time.After(5 * time.Second):
				t.Error("holding request did not finish")
			}
		})
		go func() {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/test/hold-concurrency-slot", nil))
			done <- response
		}()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("holding request did not acquire the concurrency slot")
		}
		assertLimited(t)
	})
}
