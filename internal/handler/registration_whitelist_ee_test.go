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

package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"

	"github.com/gin-gonic/gin"
)

func TestRegistrationWhitelist(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.WhiteList{}, &entity.SystemSettings{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	setting := entity.SystemSettings{Name: dao.WhitelistEnabledSettingName, DataType: "bool", Value: "true"}
	if err := db.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.WhiteList{Email: "allowed@example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"Case@Example.COM", "a..b@example.com", "用户@例子.公司"} {
		if err := db.Create(&entity.WhiteList{Email: email}).Error; err != nil {
			t.Fatal(err)
		}
	}
	mode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(mode) })
	engine := gin.New()
	engine.Use(RegistrationWhitelist())
	called := false
	engine.POST("/api/v1/users", func(c *gin.Context) {
		called = true
		var request struct{ Email, Password string }
		if err := c.ShouldBindJSON(&request); err != nil || request.Password != "encrypted-password" {
			t.Errorf("registration body was not preserved: %+v, error = %v", request, err)
		}
		c.Status(http.StatusNoContent)
	})
	engine.POST("/api/v1/auth/login", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	engine.POST("/api/v1/admin/users", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := func(path, body string, want int, wantCalled bool) string {
		t.Helper()
		called = false
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(response, req)
		if response.Code != want || called != wantCalled {
			t.Fatalf("POST %s status = %d, called = %v, body = %s; want %d, called %v", path, response.Code, called, response.Body.String(), want, wantCalled)
		}
		if want != http.StatusNoContent {
			var result struct {
				Code    int
				Message string
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Code != want {
				t.Fatalf("request failure = %s, error = %v; want code %d", response.Body.String(), err, want)
			}
			return result.Message
		}
		return ""
	}
	if message := request("/api/v1/users", `{"email":"outsider@example.com","password":"encrypted-password"}`, http.StatusForbidden, false); message != "Email outsider@example.com isn't in whitelist." {
		t.Fatalf("whitelist rejection message = %q", message)
	}
	for _, email := range []string{"notallowed@example.com", "allowed@sub.example.com", "allowed@example.com.evil"} {
		request("/api/v1/users", fmt.Sprintf(`{"email":%q,"password":"encrypted-password"}`, email), http.StatusForbidden, false)
	}
	for _, email := range []string{"allowed@example.com", "Case@Example.COM", "a..b@example.com", "用户@例子.公司"} {
		request("/api/v1/users", fmt.Sprintf(`{"email":%q,"password":"encrypted-password"}`, email), http.StatusNoContent, true)
	}
	for _, email := range []string{"ALLOWED@Example.com", " ALLOWED@Example.com ", "Alice <allowed@example.com>", "a@b.c", "allowed+tag@example.com", "allowed@exam_ple.com", "álice@example.com"} {
		body, err := json.Marshal(map[string]string{"email": email, "password": "encrypted-password"})
		if err != nil {
			t.Fatal(err)
		}
		request("/api/v1/users", string(body), http.StatusForbidden, false)
	}
	request("/api/v1/users", `{`, http.StatusBadRequest, false)
	request("/api/v1/auth/login", `{"email":"outsider@example.com"}`, http.StatusNoContent, false)
	request("/api/v1/admin/users", `{"email":"outsider@example.com"}`, http.StatusNoContent, false)
	if err := db.Model(&setting).Update("value", "false").Error; err != nil {
		t.Fatal(err)
	}
	request("/api/v1/users", `{"email":"outsider@example.com","password":"encrypted-password"}`, http.StatusNoContent, true)
	if err := db.Model(&setting).Update("value", "true").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("email = ?", "allowed@example.com").Delete(&entity.WhiteList{}).Error; err != nil {
		t.Fatal(err)
	}
	if message := request("/api/v1/users", `{"email":"allowed@example.com","password":"encrypted-password"}`, http.StatusForbidden, false); message != "Email allowed@example.com isn't in whitelist." {
		t.Fatalf("removed whitelist email rejection message = %q", message)
	}
	if err := db.Migrator().DropTable(&entity.WhiteList{}); err != nil {
		t.Fatal(err)
	}
	request("/api/v1/users", `{"email":"allowed@example.com"}`, http.StatusInternalServerError, false)
	request("/api/v1/auth/login", `{}`, http.StatusNoContent, false)
	if err := db.Model(&setting).Update("value", "bad-setting").Error; err != nil {
		t.Fatal(err)
	}
	request("/api/v1/users", `{"email":"allowed@example.com"}`, http.StatusInternalServerError, false)
}
