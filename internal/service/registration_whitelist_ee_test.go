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

package service

import (
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/service/oauth"
)

func TestWhitelistBlocksNewOAuthAccounts(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.WhiteList{}, &entity.SystemSettings{}, &entity.User{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	if err := db.Create(&entity.SystemSettings{Name: dao.WhitelistEnabledSettingName, DataType: "bool", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}
	user, code, err := (&UserService{}).registerOAuthUser(t.Context(), "github", &oauth.UserInfo{Email: "outsider@example.com"})
	if err == nil || code != common.CodeForbidden || user != nil {
		t.Fatalf("OAuth provision = (%+v, %d, %v), want forbidden", user, code, err)
	}
	var count int64
	if err := db.Model(&entity.User{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("blocked OAuth account was saved: count %d, error %v", count, err)
	}
}
