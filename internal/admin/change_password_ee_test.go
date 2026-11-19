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
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"

	"github.com/gin-gonic/gin"
)

func TestChangeOwnAdminPassword(t *testing.T) {
	t.Chdir("../..")
	db := testutil.SetupTestDB(t, &entity.User{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	encode := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }
	hash, err := common.GenerateWerkzeugPasswordHash(encode("old password"))
	if err != nil {
		t.Fatal(err)
	}
	user := entity.User{ID: "admin-id", Email: "admin@example.com", IsSuperuser: new(true), IsActive: "1", Password: &hash, AccessToken: new("old-token")}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	key, err := common.LoadPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	encrypt := func(password string) string {
		t.Helper()
		value, err := rsa.EncryptPKCS1v15(rand.Reader, &key.PublicKey, []byte(encode(password)))
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(value)
	}

	h := NewHandler(NewService())
	engine := gin.New()
	group := engine.Group("/api/v1/admin", func(c *gin.Context) {
		c.Set("user", &user)
		c.Set("user_id", user.ID)
	})
	RegisterEERouter(group, NewRouter(h))
	group.PUT("/users/:username/password", h.ChangePassword)

	for _, tc := range []struct {
		name, path string
		body       map[string]string
		want       common.ErrorCode
	}{
		{"missing current", "/me/password", map[string]string{"new_password": encrypt("new password")}, common.CodeBadRequest},
		{"invalid current", "/me/password", map[string]string{"current_password": encrypt("wrong password"), "new_password": encrypt("new password")}, common.CodeBadRequest},
		{"short password", "/me/password", map[string]string{"current_password": encrypt("old password"), "new_password": encrypt("short")}, common.CodeBadRequest},
		{"same password", "/me/password", map[string]string{"current_password": encrypt("old password"), "new_password": encrypt("old password")}, common.CodeBadRequest},
		{"legacy self change", "/users/admin@example.com/password", map[string]string{"new_password": encrypt("new password")}, common.CodeForbidden},
		{"success", "/me/password", map[string]string{"current_password": encrypt("old password"), "new_password": encrypt("new password")}, common.CodeSuccess},
		{"stale session", "/me/password", map[string]string{"current_password": encrypt("old password"), "new_password": encrypt("another password")}, common.CodeConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPut, "/api/v1/admin"+tc.path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			engine.ServeHTTP(res, req)
			var result struct {
				Code common.ErrorCode `json:"code"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || result.Code != tc.want {
				t.Fatalf("response = %s, want code %d (parse error: %v)", res.Body, tc.want, err)
			}
			var saved entity.User
			if err := db.Take(&saved, "id = ?", user.ID).Error; err != nil {
				t.Fatal(err)
			}
			changed := tc.want == common.CodeSuccess || tc.want == common.CodeConflict
			if changed {
				if *saved.AccessToken == *user.AccessToken || h.userService.VerifyPassword(*saved.Password, encode("old password")) || !h.userService.VerifyPassword(*saved.Password, encode("new password")) {
					t.Fatal("new password or session invalidation did not persist")
				}
			} else if *saved.AccessToken != *user.AccessToken || *saved.Password != hash {
				t.Fatal("failed request modified account credentials")
			}
		})
	}
}

func TestChangeOwnAdminPasswordRequiresAuth(t *testing.T) {
	engine := gin.New()
	NewRouter(NewHandler(NewService())).Setup(engine)
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, httptest.NewRequest(http.MethodPut, "/api/v1/admin/me/password", nil))
	var result struct {
		Code common.ErrorCode `json:"code"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || result.Code != common.CodeUnauthorized {
		t.Fatalf("response = %s, want unauthorized (parse error: %v)", res.Body, err)
	}
}
