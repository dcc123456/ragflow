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
	"math"
	"net/http"
	"net/http/httptest"
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

func TestUserActivityReportsEE(t *testing.T) {
	handler := setupAdminUserDetailTest(t)
	for _, fixture := range []struct {
		id, plan string
		days     int
		active   string
	}{
		{"user-1", "Starter", 2, "1"},
		{"disabled-recent", "Starter", 5, "0"},
		{"enabled-stale", "Starter", 60, "1"},
		{"pro-stale", "Pro", 60, "1"},
		{"trial-never", "", -1, "1"},
	} {
		var login *time.Time
		if fixture.days >= 0 {
			value := time.Now().AddDate(0, 0, -fixture.days)
			login = &value
		}
		if fixture.id == "user-1" {
			if err := dao.DB.Model(&entity.User{}).Where("id = ?", fixture.id).Update("last_login_time", login).Error; err != nil {
				t.Fatal(err)
			}
		} else if err := dao.DB.Create(&entity.User{ID: fixture.id, Email: fixture.id, Nickname: fixture.id, IsActive: fixture.active, LastLoginTime: login}).Error; err != nil {
			t.Fatal(err)
		}
		if fixture.plan != "" {
			if err := dao.DB.Create(&entity.BillingSubscription{ID: fixture.id, TenantID: fixture.id, PlanName: fixture.plan, SubscriptionStatus: "active"}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, user := range []entity.User{
		{ID: "anonymous", Email: "anonymous", IsAnonymous: "1"},
		{ID: "deleted", Email: "deleted", Status: testutil.StrPtr("0")},
	} {
		if err := dao.DB.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		query, field string
		values       []string
		total        int64
	}{
		{"?status=inactive&days=30", "plan", []string{"Pro", "Starter", "Trial", "Total"}, 4},
		{"?status=inactive&days=30&page=2&page_size=1", "plan", []string{"Starter"}, 4},
		{"?status=inactive&plan=Starter&days=30", "plan", []string{"Starter", "Total"}, 2},
		{"?plan=Starter&days=30", "user", []string{"user@example.com", "disabled-recent"}, 2},
		{"?plan=Starter&days=0", "user", []string{"user@example.com", "disabled-recent"}, 2},
		{"?plan=Starter&page=2&page_size=1", "user", []string{"disabled-recent"}, 2},
		{"?plan=Starter&page=99&page_size=1", "user", []string{}, 2},
		{"?plan=missing", "user", []string{}, 0},
	} {
		t.Run(tt.query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/reports"+tt.query, nil)
			handler.ListUsersReports(ctx)
			var response adminTestResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			var rows []map[string]interface{}
			if err := json.Unmarshal(response.Data, &rows); err != nil {
				t.Fatalf("response=%s error=%v", recorder.Body.String(), err)
			}
			values := make([]string, 0, len(rows))
			for _, row := range rows {
				values = append(values, row[tt.field].(string))
			}
			if response.Code != 0 || rows == nil || response.Total == nil || *response.Total != tt.total || !reflect.DeepEqual(values, tt.values) {
				t.Fatalf("response=%s; want values=%v total=%d", recorder.Body.String(), tt.values, tt.total)
			}
			if tt.query == "?plan=Starter&page=2&page_size=1" && rows[0]["rank"] != float64(2) {
				t.Fatalf("rank=%v", rows[0]["rank"])
			}
			if tt.field == "plan" && len(rows) == 4 && rows[3]["inactive_users"] != "3" {
				t.Fatalf("summary=%v", rows[3])
			}
		})
	}
}

func TestUserActivityReportsUseCurrentPlanEE(t *testing.T) {
	for _, subscription := range []struct{ status, plan string }{
		{"active", "Pro"},
		{"past_due", "Pro"},
		{"unpaid", "Starter"},
		{"incomplete", "Pro"},
		{"trialing", "Starter"},
		{"", "Pro"},
		{"canceled", "Trial"},
	} {
		name := subscription.status
		if name == "" {
			name = "without_status"
		}
		t.Run(name, func(t *testing.T) {
			handler := setupAdminUserDetailTest(t)
			login := time.Now().AddDate(0, 0, -60)
			if err := dao.DB.Model(&entity.User{}).Where("id = ?", "user-1").Update("last_login_time", login).Error; err != nil {
				t.Fatal(err)
			}
			if err := dao.DB.Create(&entity.BillingSubscription{
				ID: "subscription-1", TenantID: "user-1", PlanName: subscription.plan, SubscriptionStatus: subscription.status,
			}).Error; err != nil {
				t.Fatal(err)
			}
			for _, request := range []struct {
				handle func(*gin.Context)
				path   string
				field  string
				values []string
				total  int64
			}{
				{handler.ListUsers, "/api/v1/admin/users?plan=" + subscription.plan, "plan", []string{subscription.plan}, 1},
				{handler.ListUsersReports, "/api/v1/admin/users/reports?plan=" + subscription.plan + "&days=365", "user", []string{"user@example.com"}, 1},
				{handler.ListUsersReports, "/api/v1/admin/users/reports?status=inactive&days=30", "plan", []string{subscription.plan, "Total"}, 2},
				{handler.ListUsersReports, "/api/v1/admin/users/reports?status=inactive&days=30&plan=" + subscription.plan, "plan", []string{subscription.plan, "Total"}, 2},
			} {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodGet, request.path, nil)
				request.handle(ctx)
				var response adminTestResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				var rows []map[string]interface{}
				if err := json.Unmarshal(response.Data, &rows); err != nil {
					t.Fatalf("response=%s error=%v", recorder.Body.String(), err)
				}
				values := make([]string, 0, len(rows))
				for _, row := range rows {
					values = append(values, row[request.field].(string))
					if request.field == "plan" && row["plan"] != "Total" && row["inactive_users"] != nil && row["inactive_users"] != "1" {
						t.Fatalf("incorrect inactivity count: %v", row)
					}
				}
				if response.Code != 0 || response.Total == nil || *response.Total != request.total || !reflect.DeepEqual(values, request.values) {
					t.Errorf("%s: response=%s; want values=%v total=%d", request.path, recorder.Body.String(), request.values, request.total)
				}
			}
		})
	}
}

func TestUserActivityReportValidationEE(t *testing.T) {
	handler := setupAdminUserDetailTest(t)
	for _, query := range []string{
		"", "?status=active", "?status=invalid", "?plan=", "?plan=Starter&days=-1", "?plan=Starter&days=abc",
		"?plan=Starter&days=1&days=2", "?plan=Starter&page=0", "?plan=Starter&page_size=0", "?plan=Starter&page_size=101",
		"?plan=Starter&quota=80", "?plan=Starter&order=storage", "?plan=Starter&page=" + strconv.Itoa(math.MaxInt) + "&page_size=100",
	} {
		t.Run(query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/reports"+query, nil)
			handler.ListUsersReports(ctx)
			var response adminTestResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Code != int(common.CodeBadRequest) {
				t.Fatalf("response=%s", recorder.Body.String())
			}
		})
	}
}
