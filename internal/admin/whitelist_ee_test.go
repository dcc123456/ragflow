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

package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

type whitelistResponse struct {
	Code common.ErrorCode `json:"code"`
	Data json.RawMessage  `json:"data"`
}

func setupWhitelistTest(t *testing.T) *gin.Engine {
	t.Helper()
	db := testutil.SetupTestDB(t, &entity.WhiteList{}, &entity.SystemSettings{})
	db.Config.TranslateError = true
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	mode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(mode) })
	engine := gin.New()
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(&Handler{service: &Service{}}))
	return engine
}

func callWhitelist(t *testing.T, engine *gin.Engine, request *http.Request, status int) whitelistResponse {
	t.Helper()
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != status {
		t.Fatalf("%s %s status = %d, body = %s, want %d", request.Method, request.URL.Path, response.Code, response.Body.String(), status)
	}
	var result whitelistResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if status == http.StatusOK && result.Code != common.CodeSuccess {
		t.Fatalf("unexpected business error: %s", response.Body.String())
	}
	if status != http.StatusOK && result.Code == common.CodeSuccess {
		t.Fatalf("failure returned a success code: %s", response.Body.String())
	}
	return result
}

func whitelistJSON(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, "/api/v1/admin/whitelist"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestWhitelistHTTPAdd(t *testing.T) {
	engine := setupWhitelistTest(t)
	callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", `{"email":"Alice@Example.com"}`), http.StatusOK)
	var alice entity.WhiteList
	if err := dao.DB.Where("email = ?", "Alice@Example.com").Take(&alice).Error; err != nil {
		t.Fatal(err)
	}
	if alice.CreateTime == nil || alice.CreateDate == nil || alice.UpdateTime == nil || alice.UpdateDate == nil {
		t.Fatalf("missing timestamps: %+v", alice)
	}
	result := callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", `{"email":"Alice@Example.com"}`), http.StatusOK)
	var counts struct{ Added, Skipped int }
	if err := json.Unmarshal(result.Data, &counts); err != nil || counts.Added != 0 || counts.Skipped != 1 {
		t.Fatalf("duplicate add = %s, error = %v", result.Data, err)
	}
	var original entity.WhiteList
	if err := dao.DB.Where("id = ?", alice.ID).Take(&original).Error; err != nil || *original.UpdateTime != *alice.UpdateTime {
		t.Fatalf("duplicate changed the entry: %+v, error = %v", original, err)
	}
}

func TestWhitelistHTTPAddRejectsInvalidInput(t *testing.T) {
	engine := setupWhitelistTest(t)
	for _, body := range []string{`{`, `null`, `{}`, `{"email":123}`, `{"email":"bad"}`, `{"email":"Alice <alice@example.com>"}`, `{"email":"x@example.com' OR 1=1--"}`, `{"email":"a@b.c"}`, `{"email":"alice+tag@example.com"}`, `{"email":" alice@example.com "}`} {
		callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", body), http.StatusBadRequest)
	}
	if err := dao.DB.Migrator().DropTable(&entity.WhiteList{}); err != nil {
		t.Fatal(err)
	}
	callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", `{"email":"alice@example.com"}`), http.StatusInternalServerError)
}

func TestWhitelistHTTPPreservesAddresses(t *testing.T) {
	for _, email := range []string{"Alice@Example.COM", "álice@example.com", "用户@例子.公司", "a..b@example.com", ".alice@example.com", "alice@exam_ple.com"} {
		t.Run(email, func(t *testing.T) {
			engine := setupWhitelistTest(t)
			callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", fmt.Sprintf(`{"email":%q}`, email)), http.StatusOK)
			result := callWhitelist(t, engine, whitelistJSON(http.MethodGet, "", ""), http.StatusOK)
			var list whitelistList
			if err := json.Unmarshal(result.Data, &list); err != nil || len(list.Entries) != 1 || list.Entries[0].Email != email {
				t.Fatalf("address was changed: %s, error = %v", result.Data, err)
			}
		})
	}
}

func TestWhitelistHTTPList(t *testing.T) {
	engine := setupWhitelistTest(t)
	result := callWhitelist(t, engine, whitelistJSON(http.MethodGet, "", ""), http.StatusOK)
	var list whitelistList
	if err := json.Unmarshal(result.Data, &list); err != nil || list.Entries == nil || list.Total != 0 {
		t.Fatalf("empty list = %s, error = %v", result.Data, err)
	}
	callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", `{"email":"alice@example.com"}`), http.StatusOK)
	callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", `{"email":"bob@example.com"}`), http.StatusOK)
	result = callWhitelist(t, engine, whitelistJSON(http.MethodGet, "", ""), http.StatusOK)
	if err := json.Unmarshal(result.Data, &list); err != nil || list.Total != 2 || len(list.Entries) != 2 {
		t.Fatalf("list = %s, error = %v", result.Data, err)
	}
	if list.Entries[0].Email != "alice@example.com" || list.Entries[1].Email != "bob@example.com" || list.Entries[0].ID >= list.Entries[1].ID {
		t.Fatalf("list order = %+v", list.Entries)
	}
	for index := 0; index < 15; index++ {
		if err := dao.DB.Create(&entity.WhiteList{Email: fmt.Sprintf("user%d@example.com", index)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	result = callWhitelist(t, engine, whitelistJSON(http.MethodGet, "", ""), http.StatusOK)
	if err := json.Unmarshal(result.Data, &list); err != nil || list.Total != 17 || len(list.Entries) != 17 {
		t.Fatalf("full list = %s, error = %v", result.Data, err)
	}
	if err := dao.DB.Migrator().DropTable(&entity.WhiteList{}); err != nil {
		t.Fatal(err)
	}
	callWhitelist(t, engine, whitelistJSON(http.MethodGet, "", ""), http.StatusInternalServerError)
}

func TestWhitelistHTTPUpdate(t *testing.T) {
	engine := setupWhitelistTest(t)
	callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", `{"email":"alice@example.com"}`), http.StatusOK)
	callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", `{"email":"bob@example.com"}`), http.StatusOK)
	var alice entity.WhiteList
	if err := dao.DB.Where("email = ?", "alice@example.com").Take(&alice).Error; err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/%d", alice.ID)
	for _, email := range []string{"a@b.c", "alice+tag@example.com", " alice@example.com "} {
		callWhitelist(t, engine, whitelistJSON(http.MethodPut, path, fmt.Sprintf(`{"email":%q}`, email)), http.StatusBadRequest)
	}
	result := callWhitelist(t, engine, whitelistJSON(http.MethodPut, path, `{"email":"bob@example.com"}`), http.StatusOK)
	var response whitelistUpdateResponse
	if err := json.Unmarshal(result.Data, &response); err != nil || !response.Success || response.Message != "Email bob@example.com is already in whitelist." {
		t.Fatalf("existing email was not a successful no-op: %s, error = %v", result.Data, err)
	}
	var unchanged entity.WhiteList
	if err := dao.DB.Where("id = ?", alice.ID).Take(&unchanged).Error; err != nil || unchanged.Email != alice.Email || *unchanged.UpdateTime != *alice.UpdateTime {
		t.Fatalf("existing email update changed the row: %+v, error = %v", unchanged, err)
	}
	callWhitelist(t, engine, whitelistJSON(http.MethodPut, "/999999", `{"email":"bob@example.com"}`), http.StatusOK)
	callWhitelist(t, engine, whitelistJSON(http.MethodPut, "/999999", `{"email":"carol@example.com"}`), http.StatusNotFound)
	callWhitelist(t, engine, whitelistJSON(http.MethodPut, "/invalid", `{"email":"carol@example.com"}`), http.StatusBadRequest)
	result = callWhitelist(t, engine, whitelistJSON(http.MethodPut, path, `{"email":"Carol@Example.com"}`), http.StatusOK)
	if err := json.Unmarshal(result.Data, &response); err != nil || response.Message != "Successfully updated the email from alice@example.com to Carol@Example.com." {
		t.Fatalf("updated email message = %s, error = %v", result.Data, err)
	}
	var updated entity.WhiteList
	if err := dao.DB.Where("id = ?", alice.ID).Take(&updated).Error; err != nil || updated.Email != "Carol@Example.com" || *updated.CreateTime != *alice.CreateTime {
		t.Fatalf("update changed identity or creation time: %+v, error = %v", updated, err)
	}
}

func TestWhitelistHTTPDeletesInvalidHistoricalAddress(t *testing.T) {
	engine := setupWhitelistTest(t)
	if err := dao.DB.Create(&entity.WhiteList{Email: "legacy invalid"}).Error; err != nil {
		t.Fatal(err)
	}
	callWhitelist(t, engine, whitelistJSON(http.MethodDelete, "/legacy%20invalid", ""), http.StatusOK)
	var count int64
	if err := dao.DB.Model(&entity.WhiteList{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("historical address could not be removed: count %d, error = %v", count, err)
	}
}

func TestWhitelistHTTPDelete(t *testing.T) {
	engine := setupWhitelistTest(t)
	callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", `{"email":"carol@example.com"}`), http.StatusOK)
	callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", `{"email":"bob@example.com"}`), http.StatusOK)
	callWhitelist(t, engine, whitelistJSON(http.MethodDelete, "/carol%40example.com", ""), http.StatusOK)
	callWhitelist(t, engine, whitelistJSON(http.MethodDelete, "/carol%40example.com", ""), http.StatusOK)
	var bob entity.WhiteList
	if err := dao.DB.Where("email = ?", "bob@example.com").Take(&bob).Error; err != nil {
		t.Fatal(err)
	}
	callWhitelist(t, engine, whitelistJSON(http.MethodDelete, "/batch", fmt.Sprintf(`{"ids":[%d,0]}`, bob.ID)), http.StatusBadRequest)
	callWhitelist(t, engine, whitelistJSON(http.MethodDelete, "/batch", `{"ids":[]}`), http.StatusBadRequest)
	callWhitelist(t, engine, whitelistJSON(http.MethodDelete, "/batch", `{"ids":[9223372036854775808]}`), http.StatusBadRequest)
	var count int64
	if err := dao.DB.Model(&entity.WhiteList{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("invalid batch delete changed data: count %d, error %v", count, err)
	}
	callWhitelist(t, engine, whitelistJSON(http.MethodDelete, "/batch", fmt.Sprintf(`{"ids":[%d,%d]}`, bob.ID, bob.ID)), http.StatusOK)
	if err := dao.DB.Model(&entity.WhiteList{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("list after deletions has %d rows, error = %v", count, err)
	}
}

func whitelistWorkbook(t *testing.T, rows [][]string) []byte {
	t.Helper()
	book := excelize.NewFile()
	defer book.Close()
	for index, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, index+1)
		if err != nil {
			t.Fatal(err)
		}
		if err := book.SetSheetRow("Sheet1", cell, &row); err != nil {
			t.Fatal(err)
		}
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func whitelistUpload(t *testing.T, name string, data []byte, copies int) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for index := 0; index < copies; index++ {
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/whitelist/batch", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestWhitelistXLSXImport(t *testing.T) {
	engine := setupWhitelistTest(t)
	data := whitelistWorkbook(t, [][]string{{"name", " EMAIL "}, {"Alice", "Alice@Example.com"}, {"Bob", "bob@example.com"}, {}, {"Alice again", "Alice@Example.com"}})
	for run := 0; run < 2; run++ {
		result := callWhitelist(t, engine, whitelistUpload(t, "emails.xlsx", data, 1), http.StatusOK)
		var counts struct{ Added, Skipped int }
		if err := json.Unmarshal(result.Data, &counts); err != nil || counts.Added != 2*(1-run) || counts.Skipped != 1+2*run {
			t.Fatalf("import %d = %s, error = %v", run, result.Data, err)
		}
	}
	var count int64
	if err := dao.DB.Model(&entity.WhiteList{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("imported count = %d, error = %v", count, err)
	}
	var alice entity.WhiteList
	if err := dao.DB.Where("email = ?", "Alice@Example.com").Take(&alice).Error; err != nil || alice.Email != "Alice@Example.com" {
		t.Fatalf("import changed the email spelling: %+v, error = %v", alice, err)
	}
}

func TestWhitelistXLSXRejectsInvalidFilesAtomically(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows [][]string
	}{
		{"missing header", [][]string{{"address"}, {"alice@example.com"}}},
		{"duplicate header", [][]string{{"email", "EMAIL"}, {"alice@example.com", "bob@example.com"}}},
		{"empty workbook", nil},
		{"header only", [][]string{{"email"}}},
		{"invalid later row", [][]string{{"email"}, {"alice@example.com"}, {"invalid"}}},
		{"single character TLD", [][]string{{"email"}, {"alice@example.com"}, {"a@b.c"}}},
		{"plus address", [][]string{{"email"}, {"alice@example.com"}, {"alice+tag@example.com"}}},
		{"surrounding whitespace", [][]string{{"email"}, {"alice@example.com"}, {" Alice@Example.com "}}},
		{"missing email cell", [][]string{{"name", "email"}, {"Alice", "alice@example.com"}, {"Bob"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := setupWhitelistTest(t)
			callWhitelist(t, engine, whitelistUpload(t, "emails.xlsx", whitelistWorkbook(t, tc.rows), 1), http.StatusBadRequest)
			var count int64
			if err := dao.DB.Model(&entity.WhiteList{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("failed import left %d rows, error = %v", count, err)
			}
		})
	}
	engine := setupWhitelistTest(t)
	data := whitelistWorkbook(t, [][]string{{"email"}, {"alice@example.com"}})
	callWhitelist(t, engine, whitelistUpload(t, "emails.csv", data, 1), http.StatusBadRequest)
	callWhitelist(t, engine, whitelistUpload(t, "emails.xlsx", []byte("not an xlsx"), 1), http.StatusBadRequest)
	callWhitelist(t, engine, whitelistUpload(t, "emails.xlsx", data, 0), http.StatusBadRequest)
	callWhitelist(t, engine, whitelistUpload(t, "emails.xlsx", data, 2), http.StatusBadRequest)
	callWhitelist(t, engine, whitelistUpload(t, "emails.xlsx", make([]byte, maxWhitelistFileBytes+1), 1), http.StatusBadRequest)
	callWhitelist(t, engine, whitelistUpload(t, "emails.xlsx", make([]byte, maxWhitelistFileBytes+(128<<10)), 1), http.StatusRequestEntityTooLarge)
}

func TestWhitelistXLSXRollsBackDatabaseFailure(t *testing.T) {
	engine := setupWhitelistTest(t)
	if err := dao.DB.Exec(`CREATE TRIGGER reject_whitelist_entry BEFORE INSERT ON white_list WHEN NEW.email = 'reject@example.com' BEGIN SELECT RAISE(ABORT, 'test storage failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	rows := [][]string{{"email"}}
	for index := 0; index < 250; index++ {
		rows = append(rows, []string{fmt.Sprintf("user%d@example.com", index)})
	}
	rows = append(rows, []string{"reject@example.com"})
	callWhitelist(t, engine, whitelistUpload(t, "emails.xlsx", whitelistWorkbook(t, rows), 1), http.StatusInternalServerError)
	var count int64
	if err := dao.DB.Model(&entity.WhiteList{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed import committed a partial batch: %d rows, error = %v", count, err)
	}
}

func TestWhitelistXLSXRowLimitAndFormulas(t *testing.T) {
	engine := setupWhitelistTest(t)
	rows := make([][]string, maxWhitelistRows+2)
	rows[0] = []string{"email"}
	for index := 1; index < len(rows); index++ {
		rows[index] = []string{fmt.Sprintf("user%d@example.com", index)}
	}
	callWhitelist(t, engine, whitelistUpload(t, "emails.xlsx", whitelistWorkbook(t, rows), 1), http.StatusBadRequest)
	book := excelize.NewFile()
	defer book.Close()
	if err := book.SetCellValue("Sheet1", "A1", "email"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetCellValue("Sheet1", "A2", "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetCellFormula("Sheet1", "A2", `"alice@example.com"`); err != nil {
		t.Fatal(err)
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	callWhitelist(t, engine, whitelistUpload(t, "emails.xlsx", buffer.Bytes(), 1), http.StatusBadRequest)
}
