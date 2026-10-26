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
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
			if recorder.Code != http.StatusOK || response.Code != int(common.CodeBadRequest) ||
				response.Message != tt.message || response.Data != nil {
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

type eeUserDataset struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	TenantID   *string         `json:"tenant_id"`
	TenantName *string         `json:"tenant_name"`
	Tenant     json.RawMessage `json:"tenant"`
	FileSize   *int64          `json:"file_size"`
	Permission json.RawMessage `json:"permission"`
}

type eeUserDatasetPage struct {
	Code    common.ErrorCode `json:"code"`
	Message string           `json:"message"`
	Data    []eeUserDataset  `json:"data"`
	Total   *int64           `json:"total"`
}

func setupEEUserDatasetsTest(t *testing.T) (*Handler, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testutil.SetupTestDB(t,
		&entity.User{}, &entity.UserTenant{}, &entity.Knowledgebase{},
		&entity.Document{}, &entity.Tenant{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	status := string(entity.StatusValid)
	user := entity.User{
		ID: "user-1", Email: "user@example.com", Nickname: "Dataset user",
		IsAuthenticated: "1", IsActive: "1", IsAnonymous: "0", Status: &status,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	memberships := []entity.UserTenant{
		{ID: "membership-own", UserID: user.ID, TenantID: user.ID, Role: "owner", InvitedBy: user.ID, Status: &status},
		{ID: "membership-team", UserID: user.ID, TenantID: "tenant-2", Role: "normal", InvitedBy: "tenant-2", Status: &status},
		{ID: "membership-team-owner", UserID: "tenant-2", TenantID: "tenant-2", Role: "owner", InvitedBy: "tenant-2", Status: &status},
	}
	if err := db.Create(&memberships).Error; err != nil {
		t.Fatalf("create memberships: %v", err)
	}
	tenants := []entity.Tenant{
		{ID: user.ID, Name: testutil.StrPtr("Personal workspace")},
		{ID: "tenant-2", Name: testutil.StrPtr("Team workspace")},
	}
	if err := db.Create(&tenants).Error; err != nil {
		t.Fatalf("create tenants: %v", err)
	}
	ownTime, teamTime, hiddenTime := int64(100), int64(200), int64(50)
	datasets := []entity.Knowledgebase{
		{ID: "dataset-own", TenantID: user.ID, Name: "Own dataset", EmbdID: "embedding", Permission: "me", CreatedBy: user.ID, Status: &status, BaseModel: entity.BaseModel{CreateTime: &ownTime}},
		{ID: "dataset-team", TenantID: "tenant-2", Name: "Team dataset", EmbdID: "embedding", Permission: "team", CreatedBy: user.ID, Status: &status, BaseModel: entity.BaseModel{CreateTime: &teamTime}},
		{ID: "dataset-other-shared", TenantID: "tenant-2", Name: "Other shared dataset", EmbdID: "embedding", Permission: "team", CreatedBy: "tenant-2", Status: &status, BaseModel: entity.BaseModel{CreateTime: &hiddenTime}},
		{ID: "dataset-private", TenantID: "tenant-2", Name: "Private dataset", EmbdID: "embedding", Permission: "me", CreatedBy: "tenant-2", Status: &status, BaseModel: entity.BaseModel{CreateTime: &hiddenTime}},
		{ID: "dataset-other-own-tenant", TenantID: user.ID, Name: "Other creator in own workspace", EmbdID: "embedding", Permission: "me", CreatedBy: "other-user", Status: &status, BaseModel: entity.BaseModel{CreateTime: &hiddenTime}},
		{ID: "dataset-invalid", TenantID: user.ID, Name: "Invalid dataset", EmbdID: "embedding", Permission: "me", CreatedBy: user.ID, Status: testutil.StrPtr(string(entity.StatusInvalid)), BaseModel: entity.BaseModel{CreateTime: &hiddenTime}},
	}
	if err := db.Create(&datasets).Error; err != nil {
		t.Fatalf("create datasets: %v", err)
	}
	return NewHandler(NewService()), db
}

func requestEEUserDatasets(handler *Handler, query string) *httptest.ResponseRecorder {
	engine := gin.New()
	engine.GET("/api/v1/admin/users/:username/datasets", handler.ListUserDatasets)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/users/user@example.com/datasets"+query, nil))
	return recorder
}

func readEEUserDatasetPage(t *testing.T, recorder *httptest.ResponseRecorder) eeUserDatasetPage {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}
	var page eeUserDatasetPage
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode dataset page: %v; body = %s", err, recorder.Body.String())
	}
	if page.Code != common.CodeSuccess || page.Message != "success" || page.Total == nil || page.Data == nil {
		t.Fatalf("expected success envelope with a data array and total; body = %s", recorder.Body.String())
	}
	for _, dataset := range page.Data {
		if dataset.Permission != nil || dataset.Tenant != nil {
			t.Fatalf("dataset %q must not include permission or tenant; body = %s", dataset.Name, recorder.Body.String())
		}
		if dataset.TenantID == nil || dataset.TenantName == nil {
			t.Fatalf("dataset %q has no string tenant_id or tenant_name; body = %s", dataset.Name, recorder.Body.String())
		}
		if dataset.FileSize == nil {
			t.Fatalf("dataset %q has no numeric file_size field; body = %s", dataset.Name, recorder.Body.String())
		}
	}
	return page
}

func TestEEUserDatasetsReturnsOnlyCreatedDatasets(t *testing.T) {
	handler, db := setupEEUserDatasetsTest(t)
	if err := db.Migrator().DropTable(&entity.UserTenant{}); err != nil {
		t.Fatalf("drop membership table: %v", err)
	}
	for index, want := range []struct{ id, name, tenantID, tenantName string }{
		{id: "dataset-own", name: "Own dataset", tenantID: "user-1", tenantName: "Personal workspace"},
		{id: "dataset-team", name: "Team dataset", tenantID: "tenant-2", tenantName: "Team workspace"},
	} {
		page := readEEUserDatasetPage(t, requestEEUserDatasets(handler, fmt.Sprintf("?page=%d&page_size=1", index+1)))
		if *page.Total != 2 || len(page.Data) != 1 {
			t.Fatalf("page %d: total = %d, rows = %d, want 2 and 1", index+1, *page.Total, len(page.Data))
		}
		if dataset := page.Data[0]; dataset.ID != want.id || dataset.Name != want.name || *dataset.TenantID != want.tenantID || *dataset.TenantName != want.tenantName {
			t.Fatalf("page %d: dataset = %+v, want id %q, name %q, tenant_id %q and tenant_name %q", index+1, dataset, want.id, want.name, want.tenantID, want.tenantName)
		}
	}
}

func TestEEUserDatasetsWithoutCreatedDatasets(t *testing.T) {
	handler, db := setupEEUserDatasetsTest(t)
	if err := db.Model(&entity.Knowledgebase{}).Where("created_by = ?", "user-1").UpdateColumn("created_by", "other-user").Error; err != nil {
		t.Fatal(err)
	}
	page := readEEUserDatasetPage(t, requestEEUserDatasets(handler, ""))
	if *page.Total != 0 || len(page.Data) != 0 {
		t.Fatalf("total/rows = %d/%d, want 0/0", *page.Total, len(page.Data))
	}
}

func TestEEUserDatasetsDefaultPageSize(t *testing.T) {
	handler, db := setupEEUserDatasetsTest(t)
	for index := 0; index < 10; index++ {
		created := int64(300 + index)
		dataset := entity.Knowledgebase{
			ID: fmt.Sprintf("dataset-extra-%02d", index), Name: fmt.Sprintf("Extra dataset %d", index),
			TenantID: "user-1", EmbdID: "embedding", Permission: "me", CreatedBy: "user-1",
			BaseModel: entity.BaseModel{CreateTime: &created},
		}
		if err := db.Create(&dataset).Error; err != nil {
			t.Fatalf("create extra dataset: %v", err)
		}
	}
	first := readEEUserDatasetPage(t, requestEEUserDatasets(handler, ""))
	last := readEEUserDatasetPage(t, requestEEUserDatasets(handler, "?page=2"))
	if *first.Total != 12 || len(first.Data) != 10 || *last.Total != 12 || len(last.Data) != 2 {
		t.Fatalf("default pagination: first total/rows = %d/%d, last = %d/%d, want 12/10 and 12/2",
			*first.Total, len(first.Data), *last.Total, len(last.Data))
	}
}

func TestEEUserDatasetsStablePagination(t *testing.T) {
	handler, db := setupEEUserDatasetsTest(t)
	if err := db.Where("1 = 1").Delete(&entity.Knowledgebase{}).Error; err != nil {
		t.Fatalf("clear datasets: %v", err)
	}
	created := int64(100)
	for _, name := range []string{"c", "a", "b"} {
		dataset := entity.Knowledgebase{
			ID: "dataset-" + name, Name: name, TenantID: "user-1", EmbdID: "embedding",
			Permission: "me", CreatedBy: "user-1", BaseModel: entity.BaseModel{CreateTime: &created},
		}
		if err := db.Create(&dataset).Error; err != nil {
			t.Fatalf("create tied dataset: %v", err)
		}
	}
	first := readEEUserDatasetPage(t, requestEEUserDatasets(handler, "?page=1&page_size=2"))
	last := readEEUserDatasetPage(t, requestEEUserDatasets(handler, "?page=2&page_size=2"))
	if *first.Total != 3 || *last.Total != 3 || len(first.Data) != 2 || len(last.Data) != 1 {
		t.Fatalf("unexpected tied pages: first = %+v, last = %+v", first, last)
	}
	if first.Data[0].ID != "dataset-a" || first.Data[1].ID != "dataset-b" || last.Data[0].ID != "dataset-c" {
		t.Fatalf("tied page IDs = %q, %q, %q, want dataset-a, dataset-b, dataset-c", first.Data[0].ID, first.Data[1].ID, last.Data[0].ID)
	}
}

func TestEEUserDatasetsOutOfRangePage(t *testing.T) {
	handler, db := setupEEUserDatasetsTest(t)
	if err := db.Migrator().DropTable(&entity.Document{}); err != nil {
		t.Fatalf("drop document table: %v", err)
	}
	for _, pageNumber := range []string{"3", "1000000"} {
		t.Run(pageNumber, func(t *testing.T) {
			page := readEEUserDatasetPage(t, requestEEUserDatasets(handler, "?page="+pageNumber+"&page_size=2"))
			if *page.Total != 2 || len(page.Data) != 0 {
				t.Fatalf("out-of-range total/rows = %d/%d, want 2/0", *page.Total, len(page.Data))
			}
		})
	}
}

func TestEEUserDatasetsRejectsInvalidPagination(t *testing.T) {
	handler, _ := setupEEUserDatasetsTest(t)
	for _, query := range []string{
		"?page=abc", "?page=0", "?page=-1", "?page=1000001", "?page=999999999999999999999999",
		"?page_size=abc", "?page_size=0", "?page_size=-1", "?page_size=101",
	} {
		t.Run(query, func(t *testing.T) {
			recorder := requestEEUserDatasets(handler, query)
			var response struct {
				Code    common.ErrorCode `json:"code"`
				Message string           `json:"message"`
				Data    json.RawMessage  `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode invalid-pagination response: %v", err)
			}
			if recorder.Code != http.StatusOK || response.Code != common.CodeBadRequest || response.Message == "" || response.Data != nil {
				t.Fatalf("invalid pagination status/body = %d/%s, want HTTP 200 with business code 400 and an error message", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestEEUserDatasetsBatchesFileSizes(t *testing.T) {
	handler, db := setupEEUserDatasetsTest(t)
	if err := db.Create(&entity.Knowledgebase{
		ID: "dataset-empty", Name: "Empty dataset", TenantID: "user-1", CreatedBy: "user-1", EmbdID: "embedding",
	}).Error; err != nil {
		t.Fatal(err)
	}
	const largeSize int64 = 1 << 40
	documents := []entity.Document{
		{ID: "document-large", KbID: "dataset-own", Size: largeSize, ParserConfig: entity.JSONMap{}},
		{ID: "document-small", KbID: "dataset-own", Size: 1024, ParserConfig: entity.JSONMap{}},
		{ID: "document-team", KbID: "dataset-team", Size: 2048, ParserConfig: entity.JSONMap{}},
	}
	if err := db.Create(&documents).Error; err != nil {
		t.Fatal(err)
	}
	var sizeQueries int
	if err := db.Callback().Row().After("gorm:row").Register("test:count-document-size-queries", func(tx *gorm.DB) {
		if tx.Statement.Table == "document" {
			sizeQueries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	wantSizes := map[string]int64{"dataset-own": largeSize + 1024, "dataset-team": 2048, "dataset-empty": 0}
	for _, pageSize := range []int{1, 3} {
		sizeQueries = 0
		page := readEEUserDatasetPage(t, requestEEUserDatasets(handler, fmt.Sprintf("?page_size=%d", pageSize)))
		if *page.Total != 3 || len(page.Data) != pageSize || sizeQueries != 1 {
			t.Fatalf("page_size=%d: total/rows/size queries = %d/%d/%d, want 3/%d/1", pageSize, *page.Total, len(page.Data), sizeQueries, pageSize)
		}
		for _, dataset := range page.Data {
			wantSize, ok := wantSizes[dataset.ID]
			if !ok || *dataset.FileSize != wantSize {
				t.Fatalf("dataset %q size = %d, want %d", dataset.ID, *dataset.FileSize, wantSize)
			}
		}
	}
}

func TestEEUserDatasetsOnlySumsCurrentPage(t *testing.T) {
	handler, db := setupEEUserDatasetsTest(t)
	const largeSize int64 = 1 << 40
	documents := []entity.Document{
		{ID: "document-large", KbID: "dataset-own", Size: largeSize, ParserConfig: entity.JSONMap{}},
		{ID: "document-small", KbID: "dataset-own", Size: 1024, ParserConfig: entity.JSONMap{}},
		// An overflowing sum on another page must not prevent reading this page.
		{ID: "document-off-page-large", KbID: "dataset-team", Size: math.MaxInt64, ParserConfig: entity.JSONMap{}},
		{ID: "document-off-page-extra", KbID: "dataset-team", Size: 1, ParserConfig: entity.JSONMap{}},
	}
	if err := db.Create(&documents).Error; err != nil {
		t.Fatalf("create documents: %v", err)
	}
	page := readEEUserDatasetPage(t, requestEEUserDatasets(handler, "?page=1&page_size=1"))
	if *page.Total != 2 || len(page.Data) != 1 || page.Data[0].Name != "Own dataset" || *page.Data[0].FileSize != largeSize+1024 {
		t.Fatalf("unexpected page or truncated file size: %+v", page)
	}
}

func TestEEUserDatasetsReturnsSizeLookupError(t *testing.T) {
	handler, db := setupEEUserDatasetsTest(t)
	if err := db.Migrator().DropTable(&entity.Document{}); err != nil {
		t.Fatalf("drop document table: %v", err)
	}
	recorder := requestEEUserDatasets(handler, "?page=1&page_size=1")
	var response eeUserDatasetPage
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode size-lookup error: %v", err)
	}
	if recorder.Code != http.StatusOK || response.Code != common.CodeServerError || !strings.Contains(response.Message, "failed to get size") {
		t.Fatalf("size-lookup error status/body = %d/%s, want HTTP 200 with a size-query server error", recorder.Code, recorder.Body.String())
	}
}

func TestEEUserDatasetsWithoutTenantNames(t *testing.T) {
	for _, name := range []*string{nil, testutil.StrPtr("")} {
		label := "empty name"
		if name == nil {
			label = "null name"
		}
		t.Run(label, func(t *testing.T) {
			handler, db := setupEEUserDatasetsTest(t)
			if err := db.Model(&entity.Tenant{}).Where("id = ?", "user-1").UpdateColumn("name", name).Error; err != nil {
				t.Fatalf("clear tenant name: %v", err)
			}
			if err := db.Where("id = ?", "tenant-2").Delete(&entity.Tenant{}).Error; err != nil {
				t.Fatalf("delete team tenant: %v", err)
			}
			page := readEEUserDatasetPage(t, requestEEUserDatasets(handler, ""))
			if *page.Total != 2 || len(page.Data) != 2 {
				t.Fatalf("missing tenant names dropped datasets: %+v", page)
			}
			wantTenantIDs := map[string]string{"Own dataset": "user-1", "Team dataset": "tenant-2"}
			for _, dataset := range page.Data {
				if *dataset.TenantName != "" || *dataset.TenantID != wantTenantIDs[dataset.Name] {
					t.Fatalf("dataset %q tenant_id/name = %q/%q, want original ID and empty name", dataset.Name, *dataset.TenantID, *dataset.TenantName)
				}
			}
		})
	}
}

func TestUserAgentsRouteRequiresEE(_ *testing.T) {}

func TestUserDatasetsRouteRequiresEE(_ *testing.T) {}
