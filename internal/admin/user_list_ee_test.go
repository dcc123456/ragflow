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
	"testing"
	"time"

	"ragflow/internal/dao"
	"ragflow/internal/entity"

	"github.com/gin-gonic/gin"
)

func init() {
	adminUserDetailTestModels = append(adminUserDetailTestModels, &entity.BillingSubscription{})
}

func TestListUsersEEPlanAndStatus(t *testing.T) {
	handler := setupAdminUserDetailTest(t)
	if err := dao.DB.Create(&entity.BillingSubscription{ID: "sub-enabled", TenantID: "user-1", PlanName: "Starter"}).Error; err != nil {
		t.Fatal(err)
	}
	for i, row := range []struct{ id, plan string }{
		{"disabled-a", "Starter"}, {"disabled-b", "Starter"}, {"disabled-pro", "Pro"}, {"disabled-trial", ""},
	} {
		created := time.Date(2026, time.October, 1, i, 0, 0, 0, time.UTC).UnixMilli()
		user := entity.User{ID: row.id, Email: row.id, Nickname: row.id, IsActive: "0", BaseModel: entity.BaseModel{CreateTime: &created}}
		if i == 1 {
			oldLogin := time.Now().AddDate(0, 0, -60)
			user.LastLoginTime = &oldLogin
		}
		if err := dao.DB.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		if row.plan != "" {
			if err := dao.DB.Create(&entity.BillingSubscription{ID: "sub-" + row.id, TenantID: row.id, PlanName: row.plan}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tt := range []struct {
		query  string
		emails []string
		total  int64
	}{
		{"?page=1&page_size=10&status=inactive&plan=Starter", []string{"disabled-b", "disabled-a"}, 2},
		{"?page=2&page_size=1&status=inactive&plan=Starter&order=create_time&sort=asc", []string{"disabled-b"}, 2},
		{"?keyword=DISABLED-A&status=inactive&plan=Starter", []string{"disabled-a"}, 1},
		{"?status=active&plan=Starter", []string{"user@example.com"}, 1},
		{"?status=inactive&plan=Trial", []string{"disabled-trial"}, 1},
		{"?status=inactive", []string{"disabled-trial", "disabled-pro", "disabled-b", "disabled-a"}, 4},
		{"?status=inactive&plan=Starter&order=last_login_time&sort=desc", []string{"disabled-b", "disabled-a"}, 2},
		{"?plan=missing", []string{}, 0},
	} {
		t.Run(tt.query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users"+tt.query, nil)
			handler.ListUsers(ctx)
			var response adminTestResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			var users []map[string]interface{}
			if err := json.Unmarshal(response.Data, &users); err != nil {
				t.Fatal(err)
			}
			emails := make([]string, 0, len(users))
			for _, user := range users {
				emails = append(emails, user["email"].(string))
				if user["plan"] == nil {
					t.Fatalf("missing plan in %s", recorder.Body.String())
				}
			}
			if response.Code != 0 || response.Total == nil || *response.Total != tt.total || users == nil || !reflect.DeepEqual(emails, tt.emails) {
				t.Fatalf("response=%s; want emails=%v total=%d", recorder.Body.String(), tt.emails, tt.total)
			}
		})
	}
}
