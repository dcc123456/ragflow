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

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"

	"github.com/gin-gonic/gin"
)

type adminUserAgentsTestResponse struct {
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Total   *int64          `json:"total"`
}

type adminAgentDetail struct {
	ID             string          `json:"id"`
	TenantID       *string         `json:"tenant_id"`
	TenantName     string          `json:"tenant_name"`
	Avatar         *string         `json:"avatar"`
	Title          string          `json:"title"`
	Permission     json.RawMessage `json:"permission"`
	CanvasCategory string          `json:"canvas_category"`
	CreateDate     *time.Time      `json:"create_date"`
	UpdateDate     *time.Time      `json:"update_date"`
}

func setupAdminUserAgentsTest(t *testing.T) *Handler {
	t.Helper()
	db := testutil.SetupTestDB(t, &entity.User{}, &entity.UserTenant{}, &entity.UserCanvas{}, &entity.Tenant{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	userID := "user-1"
	statusValid := string(entity.StatusValid)
	user := entity.User{
		ID: userID, Nickname: "User One", Email: "user@example.com",
		IsAuthenticated: "1", IsActive: "1", IsAnonymous: "0", Status: &statusValid,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	membership := entity.UserTenant{
		ID: "membership-1", UserID: userID, TenantID: "tenant-2",
		Role: "normal", InvitedBy: "tenant-2", Status: &statusValid,
	}
	if err := db.Create(&membership).Error; err != nil {
		t.Fatalf("create membership: %v", err)
	}
	tenants := []entity.Tenant{
		{ID: "canvas-tenant-user-1", Name: testutil.StrPtr("Personal workspace")},
		{ID: "canvas-tenant-tenant-2", Name: testutil.StrPtr("Team workspace")},
	}
	if err := db.Create(&tenants).Error; err != nil {
		t.Fatalf("create tenants: %v", err)
	}
	now := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	updatedAt := now.Add(time.Hour)
	createdAtMillis := now.UnixMilli()
	otherCreatedAtMillis := createdAtMillis - 1000
	agents := []entity.UserCanvas{
		{ID: "agent-team", UserID: "tenant-2", Title: testutil.StrPtr("Team agent"), Permission: "team", CanvasCategory: "agent_canvas"},
		{ID: "agent-own", UserID: userID, Title: testutil.StrPtr("Own agent"), Permission: "me", CanvasCategory: "agent_canvas"},
		{ID: "agent-own-shared", UserID: userID, Title: testutil.StrPtr("Own shared agent"), Permission: "team", CanvasCategory: "agent_canvas"},
		{ID: "agent-private", UserID: "tenant-2", Title: testutil.StrPtr("Private agent"), Permission: "me", CanvasCategory: "agent_canvas"},
		{ID: "agent-outside", UserID: "tenant-3", Title: testutil.StrPtr("Outside agent"), Permission: "team", CanvasCategory: "agent_canvas"},
		{ID: "pipeline-team", UserID: "tenant-2", Title: testutil.StrPtr("Team pipeline"), Permission: "team", CanvasCategory: "dataflow_canvas"},
		{ID: "pipeline-own", UserID: userID, Title: testutil.StrPtr("Own pipeline"), Permission: "me", CanvasCategory: "dataflow_canvas"},
		{ID: "pipeline-own-shared", UserID: userID, Title: testutil.StrPtr("Own shared pipeline"), Permission: "team", CanvasCategory: "dataflow_canvas"},
		{ID: "pipeline-private", UserID: "tenant-2", Title: testutil.StrPtr("Private pipeline"), Permission: "me", CanvasCategory: "dataflow_canvas"},
		{ID: "pipeline-outside", UserID: "tenant-3", Title: testutil.StrPtr("Outside pipeline"), Permission: "team", CanvasCategory: "dataflow_canvas"},
		{ID: "near-own", UserID: userID, Title: testutil.StrPtr("Own near match"), Permission: "me", CanvasCategory: "agent_canvas_extra"},
		{ID: "near-team", UserID: "tenant-2", Title: testutil.StrPtr("Team near match"), Permission: "team", CanvasCategory: "agent_canvas_extra"},
	}
	for i := range agents {
		agents[i].TenantID = testutil.StrPtr("canvas-tenant-" + agents[i].UserID)
		agents[i].Avatar = testutil.StrPtr("avatar.png")
		agents[i].BaseModel = entity.BaseModel{CreateTime: &createdAtMillis, CreateDate: &now, UpdateDate: &updatedAt}
		if agents[i].UserID != userID {
			agents[i].CreateTime = &otherCreatedAtMillis
		}
	}
	if err := dao.DB.Create(&agents).Error; err != nil {
		t.Fatalf("create agents: %v", err)
	}

	return NewHandler(NewService())
}

func callAdminUserAgentsHandler(t *testing.T, handler func(*gin.Context), requestURL string) adminUserAgentsTestResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "username", Value: "user@example.com"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, requestURL, nil)
	handler(ctx)

	var response adminUserAgentsTestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != int(common.CodeSuccess) {
		t.Fatalf("response code = %d, want %d; body = %s", response.Code, common.CodeSuccess, recorder.Body.String())
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	return response
}

func TestListUserAgentsRejectsInvalidQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		query   string
		message string
	}{
		{"", "canvas_category is required"},
		{"?canvas_category=", "canvas_category is required"},
		{"?canvas_category=%20%09%0A", "canvas_category is required"},
		{"?canvas_category=unknown", "canvas_category must be agent_canvas or dataflow_canvas"},
		{"?canvas_category=agent_canavs", "canvas_category must be agent_canvas or dataflow_canvas"},
		{"?canvas_category=AGENT_CANVAS", "canvas_category must be agent_canvas or dataflow_canvas"},
		{"?canvas_category=agent_canvas_extra", "canvas_category must be agent_canvas or dataflow_canvas"},
		{"?canvas_category=agent_canvas&page=0", "page must be between 1 and 1000000"},
		{"?canvas_category=agent_canvas&page=-1", "page must be between 1 and 1000000"},
		{"?canvas_category=agent_canvas&page=invalid", "page must be between 1 and 1000000"},
		{"?canvas_category=agent_canvas&page=1000001", "page must be between 1 and 1000000"},
		{"?canvas_category=agent_canvas&page=999999999999999999999999", "page must be between 1 and 1000000"},
		{"?canvas_category=agent_canvas&page_size=0", "page_size must be between 1 and 100"},
		{"?canvas_category=agent_canvas&page_size=101", "page_size must be between 1 and 100"},
		{"?canvas_category=agent_canvas&page_size=invalid", "page_size must be between 1 and 100"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Params = gin.Params{{Key: "username", Value: "user@example.com"}}
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/user@example.com/agents"+tt.query, nil)
			(&Handler{}).ListUserAgents(ctx)

			var response adminUserAgentsTestResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusBadRequest || response.Code != int(common.CodeBadRequest) ||
				response.Message != tt.message || string(response.Data) != "null" {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestListUserAgentsReturnsOnlyCreatedCanvases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupAdminUserAgentsTest(t)
	createdAt := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	tests := []struct {
		queryCategory string
		category      string
		want          map[string]string
	}{
		{" \tagent_canvas\n", "agent_canvas", map[string]string{"Own agent": "agent-own", "Own shared agent": "agent-own-shared"}},
		{" \tdataflow_canvas\n", "dataflow_canvas", map[string]string{"Own pipeline": "pipeline-own", "Own shared pipeline": "pipeline-own-shared"}},
	}
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			response := callAdminUserAgentsHandler(t, handler.ListUserAgents, "/api/v1/admin/users/user@example.com/agents?canvas_category="+url.QueryEscape(tt.queryCategory))
			var agents []adminAgentDetail
			if err := json.Unmarshal(response.Data, &agents); err != nil {
				t.Fatal(err)
			}
			if response.Message != "success" || response.Total == nil || *response.Total != int64(len(tt.want)) || agents == nil || len(agents) != len(tt.want) {
				t.Fatalf("unexpected response: %+v, data = %s", response, response.Data)
			}
			for _, agent := range agents {
				wantID, ok := tt.want[agent.Title]
				if !ok || agent.ID != wantID || agent.Permission != nil || agent.CanvasCategory != tt.category ||
					agent.Avatar == nil || *agent.Avatar != "avatar.png" ||
					agent.CreateDate == nil || !agent.CreateDate.Equal(createdAt) ||
					agent.UpdateDate == nil || !agent.UpdateDate.Equal(updatedAt) {
					t.Fatalf("unexpected canvas in %s", response.Data)
				}
				if agent.TenantID == nil || *agent.TenantID != "canvas-tenant-user-1" || agent.TenantName != "Personal workspace" {
					t.Fatalf("unexpected tenant fields in %s", response.Data)
				}
				delete(tt.want, agent.Title)
			}
		})
	}
}

func TestListUserAgentsPaginatesCreatedCanvases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupAdminUserAgentsTest(t)
	if err := dao.DB.Migrator().DropTable(&entity.UserTenant{}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		category string
		ids      []string
	}{
		{"agent_canvas", []string{"agent-own", "agent-own-shared"}},
		{"dataflow_canvas", []string{"pipeline-own", "pipeline-own-shared"}},
	} {
		t.Run(tt.category, func(t *testing.T) {
			for _, page := range []int{1, 2, 3, 1_000_000} {
				pageSize := 1
				if page == 1_000_000 {
					pageSize = 100
				}
				response := callAdminUserAgentsHandler(t, handler.ListUserAgents, "/api/v1/admin/users/user@example.com/agents?canvas_category="+tt.category+"&page_size="+strconv.Itoa(pageSize)+"&page="+strconv.Itoa(page))
				var agents []adminAgentDetail
				if err := json.Unmarshal(response.Data, &agents); err != nil {
					t.Fatal(err)
				}
				if response.Total == nil || *response.Total != int64(len(tt.ids)) || agents == nil {
					t.Fatalf("page %d: unexpected total or data: %+v, %s", page, response, response.Data)
				}
				if page <= len(tt.ids) {
					if len(agents) != 1 || agents[0].ID != tt.ids[page-1] {
						t.Fatalf("page %d: data = %s, want %q", page, response.Data, tt.ids[page-1])
					}
				} else if len(agents) != 0 {
					t.Fatalf("page %d: data = %s, want []", page, response.Data)
				}
			}
		})
	}
}

func TestListUserAgentsPreservesNullFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupAdminUserAgentsTest(t)
	tenants := []entity.Tenant{
		{ID: "user-1", Name: testutil.StrPtr("Owner fallback must not be used")},
		{ID: "unnamed-tenant"},
		{ID: "empty-name-tenant", Name: testutil.StrPtr("")},
	}
	if err := dao.DB.Create(&tenants).Error; err != nil {
		t.Fatal(err)
	}
	if err := dao.DB.Where("id <> ?", "agent-own").Delete(&entity.UserCanvas{}).Error; err != nil {
		t.Fatal(err)
	}
	const category = "agent_canvas"
	for _, tt := range []struct {
		name     string
		tenantID *string
	}{
		{"null tenant", nil},
		{"empty tenant", testutil.StrPtr("")},
		{"missing tenant", testutil.StrPtr("missing-tenant")},
		{"null name", testutil.StrPtr("unnamed-tenant")},
		{"empty name", testutil.StrPtr("empty-name-tenant")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := dao.DB.Model(&entity.UserCanvas{}).Where("id = ?", "agent-own").UpdateColumns(map[string]interface{}{
				"tenant_id": tt.tenantID, "avatar": nil, "create_date": nil, "update_date": nil,
			}).Error; err != nil {
				t.Fatal(err)
			}
			response := callAdminUserAgentsHandler(t, handler.ListUserAgents, "/api/v1/admin/users/user@example.com/agents?canvas_category="+url.QueryEscape(category))
			var agents []map[string]interface{}
			if err := json.Unmarshal(response.Data, &agents); err != nil {
				t.Fatal(err)
			}
			var tenantID interface{}
			if tt.tenantID != nil {
				tenantID = *tt.tenantID
			}
			want := []map[string]interface{}{{
				"id": "agent-own", "tenant_id": tenantID, "tenant_name": "", "title": "Own agent", "canvas_category": category,
				"avatar": nil, "create_date": nil, "update_date": nil,
			}}
			if response.Total == nil || *response.Total != 1 || !reflect.DeepEqual(agents, want) {
				t.Fatalf("data = %s, want %+v", response.Data, want)
			}
		})
	}
}

func TestUserDatasetsRouteRequiresEE(t *testing.T) {
	engine := gin.New()
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(&Handler{}))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/user%40example.com/datasets", nil)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("HTTP status = %d, want %d; body = %s", response.Code, http.StatusNotImplemented, response.Body.String())
	}

	var result struct {
		Code    common.ErrorCode `json:"code"`
		Data    json.RawMessage  `json:"data"`
		Message string           `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Code != common.CodeNotImplemented || string(result.Data) != "null" || result.Message != "listing user datasets is only available in the enterprise edition" {
		t.Fatalf("response = %s, want enterprise edition error with null data", response.Body.String())
	}
}
