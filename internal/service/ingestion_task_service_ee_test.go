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
	"errors"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

func TestIngestionTaskServiceListForAdminRejectsInvalidFilters(t *testing.T) {
	service := NewIngestionTaskService()
	for _, test := range []struct {
		name   string
		filter dao.AdminIngestionTaskFilter
	}{
		{"unknown sort", dao.AdminIngestionTaskFilter{OrderBy: "status", Page: 1, PageSize: 20}},
		{"unknown status", dao.AdminIngestionTaskFilter{Status: "UNKNOWN", Page: 1, PageSize: 20}},
		{"zero page", dao.AdminIngestionTaskFilter{Page: 0, PageSize: 20}},
		{"negative page", dao.AdminIngestionTaskFilter{Page: -1, PageSize: 20}},
		{"zero page size", dao.AdminIngestionTaskFilter{Page: 1, PageSize: 0}},
		{"negative page size", dao.AdminIngestionTaskFilter{Page: 1, PageSize: -1}},
		{"page size above limit", dao.AdminIngestionTaskFilter{Page: 1, PageSize: 101}},
		{"page above limit", dao.AdminIngestionTaskFilter{Page: 1_000_001, PageSize: 20}},
	} {
		t.Run(test.name, func(t *testing.T) {
			tasks, total, err := service.ListForAdmin(t.Context(), test.filter)
			var coded *common.CodedError
			if !errors.As(err, &coded) || coded.Code != common.CodeBadRequest || tasks != nil || total != 0 {
				t.Fatalf("ListForAdmin() = (%+v, %d, %v), want a bad-request error", tasks, total, err)
			}
		})
	}
}

func TestIngestionTaskServiceListForAdminAcceptsMaximumPage(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	if err := db.Create(&entity.IngestionTask{
		ID: "task-1", UserID: "user-1", DocumentID: "doc-1", DatasetID: "dataset-1", Status: common.CREATED,
	}).Error; err != nil {
		t.Fatal(err)
	}
	tasks, total, err := NewIngestionTaskService().ListForAdmin(t.Context(), dao.AdminIngestionTaskFilter{
		Page: 1_000_000, PageSize: 100,
	})
	if err != nil || total != 1 || tasks == nil || len(tasks) != 0 {
		t.Fatalf("ListForAdmin() = (%+v, %d, %v), want an empty page and total 1", tasks, total, err)
	}
}

func TestIngestionTaskServiceListForAdminNormalizesFilters(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	for _, row := range []any{
		&entity.User{ID: "user-1", Nickname: "Reader", Email: "reader+jobs@example.test"},
		&entity.Knowledgebase{ID: "dataset-1", Name: "Legal Dataset", TenantID: "user-1", EmbdID: "embedding"},
		&entity.Document{ID: "doc-1", KbID: "dataset-1", Name: sptr("Current Contract.txt"), ParserID: "naive",
			Type: "txt", Suffix: "txt", SourceType: "local", CreatedBy: "user-1", ParserConfig: entity.JSONMap{}},
		&entity.IngestionTask{ID: "task-1", UserID: "user-1", DocumentID: "doc-1", DatasetID: "dataset-1", Status: common.RUNNING},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	tasks, total, err := NewIngestionTaskService().ListForAdmin(t.Context(), dao.AdminIngestionTaskFilter{
		UserEmail: " reader+jobs@example.test ", DatasetID: " dataset-1 ", DatasetName: " Legal ", DocumentName: " Contract ",
		Status: " running ", OrderBy: " UPDATE_TIME ", Page: 1, PageSize: 20,
	})
	if err != nil || total != 1 || len(tasks) != 1 || tasks[0].ID != "task-1" ||
		tasks[0].Nickname != "Reader" || tasks[0].Email != "reader+jobs@example.test" ||
		tasks[0].DocumentName != "Current Contract.txt" || tasks[0].DatasetName != "Legal Dataset" || tasks[0].PipelineName != "" {
		t.Fatalf("ListForAdmin() = (%+v, %d, %v), want task-1 after filter normalization", tasks, total, err)
	}
}
