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

//go:build ee && integration

package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/handler"
	"ragflow/internal/ingestion/testutil"

	"github.com/gin-gonic/gin"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// RAGFLOW_WHITELIST_MYSQL_DSN must permit creating and dropping a temporary database.
func TestWhitelistMySQLHTTP(t *testing.T) {
	dsn := os.Getenv("RAGFLOW_WHITELIST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set RAGFLOW_WHITELIST_MYSQL_DSN to run MySQL integration tests")
	}
	config, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid MySQL integration DSN")
	}
	config.DBName = ""
	config.ParseTime = true
	connection, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	name := fmt.Sprintf("ragflow_whitelist_test_%d", time.Now().UnixNano())
	if _, err := connection.ExecContext(t.Context(), "CREATE DATABASE "+name+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := connection.Exec("DROP DATABASE " + name); err != nil {
			t.Errorf("remove temporary whitelist database: %v", err)
		}
	})
	config.DBName = name
	db, err := gorm.Open(mysql.Open(config.FormatDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := db.AutoMigrate(&entity.WhiteList{}, &entity.SystemSettings{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	mode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(mode) })
	engine := gin.New()
	engine.Use(handler.RegistrationWhitelist())
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(&Handler{service: &Service{}}))
	engine.POST("/api/v1/users", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	call := func(request *http.Request, want int) whitelistResponse {
		t.Helper()
		outgoing, err := http.NewRequest(request.Method, server.URL+request.URL.RequestURI(), request.Body)
		if err != nil {
			t.Fatal(err)
		}
		outgoing.Header = request.Header
		response, err := server.Client().Do(outgoing)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("%s %s status = %d, want %d", request.Method, request.URL.Path, response.StatusCode, want)
		}
		if want == http.StatusNoContent {
			return whitelistResponse{}
		}
		var result whitelistResponse
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if (want == http.StatusOK) != (result.Code == 0) {
			t.Fatalf("HTTP status and business result disagree: %+v", result)
		}
		return result
	}
	call(whitelistJSON(http.MethodPost, "", `{"email":"ALICE@Example.com"}`), http.StatusOK)
	call(whitelistJSON(http.MethodPost, "", `{"email":" ALICE@Example.com "}`), http.StatusBadRequest)
	call(whitelistJSON(http.MethodPost, "", `{"email":"alice@example.com"}`), http.StatusOK)
	call(whitelistJSON(http.MethodPost, "", `{"email":"álice@example.com"}`), http.StatusOK)
	call(whitelistUpload(t, "emails.xlsx", whitelistWorkbook(t, [][]string{{"email"}, {"alice@example.com"}, {"bob@example.com"}}), 1), http.StatusOK)
	call(whitelistUpload(t, "emails.xlsx", whitelistWorkbook(t, [][]string{{"email"}, {"new@example.com"}, {"invalid email"}}), 1), http.StatusBadRequest)
	store := dao.NewWhitelistDAO()
	if added, err := store.InsertEmails(t.Context(), db, []string{"álice@example.com"}); err != nil || added != 0 {
		t.Fatalf("database-equivalent insert was not skipped: added %d, error = %v", added, err)
	}
	if allowed, err := dao.WhitelistContains(t.Context(), db, "new@example.com"); err != nil || allowed {
		t.Fatalf("invalid workbook committed a partial import: allowed = %v, error = %v", allowed, err)
	}
	var alice entity.WhiteList
	if err := db.Where("email = ?", "alice@example.com").Take(&alice).Error; err != nil || alice.Email != "ALICE@Example.com" {
		t.Fatalf("original spelling was changed: %+v, error = %v", alice, err)
	}
	call(whitelistJSON(http.MethodPut, fmt.Sprintf("/%d", alice.ID), `{"email":"bob@example.com"}`), http.StatusOK)
	var unchanged entity.WhiteList
	if err := db.Where("id = ?", alice.ID).Take(&unchanged).Error; err != nil || unchanged.Email != alice.Email || *unchanged.UpdateTime != *alice.UpdateTime {
		t.Fatalf("existing-email update changed its source row: %+v, error = %v", unchanged, err)
	}
	var wait sync.WaitGroup
	service := &Service{}
	failures := make(chan error, 10)
	added := make(chan int64, 10)
	for index := 0; index < 10; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := service.AddWhiteList(t.Context(), "parallel@example.com")
			if err != nil {
				failures <- err
				return
			}
			added <- result.added
		}()
	}
	wait.Wait()
	close(failures)
	close(added)
	for err := range failures {
		t.Fatal(err)
	}
	var totalAdded int64
	for count := range added {
		totalAdded += count
	}
	if totalAdded != 1 {
		t.Fatalf("concurrent adds inserted %d rows, want 1", totalAdded)
	}
	result := call(whitelistJSON(http.MethodGet, "", ""), http.StatusOK)
	var list whitelistList
	if err := json.Unmarshal(result.Data, &list); err != nil || list.Total != 3 {
		t.Fatalf("whitelist = %s, error = %v", result.Data, err)
	}
	if err := db.Create(&entity.SystemSettings{Name: dao.WhitelistEnabledSettingName, DataType: "bool", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}
	for email, status := range map[string]int{"alice@example.com": http.StatusNoContent, "ALICE@EXAMPLE.COM": http.StatusNoContent, "outsider@example.com": http.StatusForbidden, "álice@example.com": http.StatusNoContent, " alice@example.com ": http.StatusForbidden, "alice+tag@example.com": http.StatusForbidden} {
		request := whitelistJSON(http.MethodPost, "", fmt.Sprintf(`{"email":%q}`, email))
		request.URL.Path = "/api/v1/users"
		call(request, status)
	}
	var accented entity.WhiteList
	if err := db.Where("email = ?", "álice@example.com").Take(&accented).Error; err != nil {
		t.Fatal(err)
	}
	result = call(whitelistJSON(http.MethodDelete, fmt.Sprintf("/%d", accented.ID), ""), http.StatusOK)
	var deletion whitelistDeleteResponse
	if err := json.Unmarshal(result.Data, &deletion); err != nil || deletion.Deleted != 1 {
		t.Fatalf("delete by ID = %s, error = %v", result.Data, err)
	}
	call(whitelistJSON(http.MethodDelete, fmt.Sprintf("/%d", alice.ID), ""), http.StatusOK)
}
