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
