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

package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseTenantRolePagination(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		wantPage     int
		wantPageSize int
		wantValid    bool
	}{
		{name: "defaults", wantPage: 1, wantPageSize: 20, wantValid: true},
		{name: "valid values", query: "page=2&page_size=50", wantPage: 2, wantPageSize: 50, wantValid: true},
		{name: "invalid page", query: "page=nope", wantValid: false},
		{name: "zero page", query: "page=0", wantValid: false},
		{name: "invalid page size", query: "page_size=nope", wantValid: false},
		{name: "oversized page size", query: "page_size=201", wantValid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := tenantRoleTestContext(test.query)
			page, pageSize, valid := parseTenantRolePagination(ctx)
			if valid != test.wantValid {
				t.Fatalf("valid = %v, want %v", valid, test.wantValid)
			}
			if valid && (page != test.wantPage || pageSize != test.wantPageSize) {
				t.Fatalf("pagination = (%d, %d), want (%d, %d)", page, pageSize, test.wantPage, test.wantPageSize)
			}
		})
	}
}

func TestParseTenantPermissionLogTime(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantValue *int64
		wantValid bool
	}{
		{name: "omitted", wantValid: true},
		{name: "zero", query: "start_time=0", wantValue: int64Pointer(0), wantValid: true},
		{name: "positive", query: "start_time=12345", wantValue: int64Pointer(12345), wantValid: true},
		{name: "negative", query: "start_time=-1", wantValid: false},
		{name: "non-integer", query: "start_time=invalid", wantValid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := tenantRoleTestContext(test.query)
			value, valid := parseTenantPermissionLogTime(ctx, "start_time")
			if valid != test.wantValid {
				t.Fatalf("valid = %v, want %v", valid, test.wantValid)
			}
			if test.wantValue == nil {
				if value != nil {
					t.Fatalf("value = %d, want nil", *value)
				}
				return
			}
			if value == nil || *value != *test.wantValue {
				t.Fatalf("value = %v, want %d", value, *test.wantValue)
			}
		})
	}
}

func tenantRoleTestContext(query string) (*gin.Context, *httptest.ResponseRecorder) {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest("GET", "/?"+query, nil)
	return ctx, response
}

func int64Pointer(value int64) *int64 { return &value }
