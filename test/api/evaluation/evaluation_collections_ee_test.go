//go:build integration && ee

// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package evaluation exercises a real API without the parent suite's model
// provisioning. It creates and removes only its own collection/chat fixtures.
package evaluation

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestEvaluationRequiresAuthentication(t *testing.T) {
	c := client(t)
	for _, key := range []string{"", "invalid-evaluation-api-key"} {
		t.Run(fmt.Sprintf("missing_%t", key == ""), func(t *testing.T) {
			unauthorized := c
			unauthorized.key = key
			r := unauthorized.json(t, http.MethodGet, evaluationPath+"/collections", nil)
			if r.status != http.StatusUnauthorized {
				t.Fatalf("HTTP=%d, want 401", r.status)
			}
			requireRejected(t, r)
		})
	}
}

func TestEvaluationImportPreservesUnicodeCases(t *testing.T) {
	book := excelize.NewFile()
	t.Cleanup(func() { _ = book.Close() })
	for _, sheet := range []string{"Sheet1", "Second"} {
		if sheet != "Sheet1" {
			if _, err := book.NewSheet(sheet); err != nil {
				t.Fatal(err)
			}
		}
		for cell, value := range map[string]string{"A1": "question", "B1": "reference_answer", "A2": "世界是什么？", "B2": "一个测试答案"} {
			if err := book.SetCellStr(sheet, cell, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	xlsx, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		body []byte
		want int
	}{
		{"cases.csv", []byte("question,reference_answer\n世界是什么？,一个测试答案\n"), 1},
		{"cases.xlsx", xlsx.Bytes(), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := client(t)
			collectionID := c.collection(t, tc.name, tc.body)
			cases := c.cases(t, collectionID)
			if len(cases) != tc.want {
				t.Fatalf("imported %d cases, want %d", len(cases), tc.want)
			}
			for _, row := range cases {
				variable := row.(object)["variable"].(object)
				if variable["question"] != "世界是什么？" || variable["reference_answer"] != "一个测试答案" {
					t.Fatalf("Unicode case changed: %v", variable)
				}
			}
		})
	}
}

func TestEvaluationRejectsMissingSpreadsheetHeaders(t *testing.T) {
	c := client(t)
	requireValidationError(t, c.upload(t, "invalid.csv", []byte("question,wrong_header\nquestion,answer\n")))
}

func TestEvaluationImportsGenuineBIFFWorkbook(t *testing.T) {
	// Adapted from the parser's genuineBIFF8FixtureBase64: only the four
	// header strings and consequent worksheet offsets differ.
	content, err := os.ReadFile("testdata/cases.xls")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(content, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
		t.Fatal("XLS fixture must be a BIFF workbook, not renamed OOXML")
	}
	c := client(t)
	collectionID := c.collection(t, "cases.xls", content)
	got := map[string]string{}
	for _, row := range c.cases(t, collectionID) {
		variable := row.(object)["variable"].(object)
		got[variable["question"].(string)] = variable["reference_answer"].(string)
	}
	if !reflect.DeepEqual(got, map[string]string{"Apples": "3", "Pears": "7", "Legacy": "BIFF8"}) {
		t.Fatalf("BIFF sheets/cells changed: %v", got)
	}
}

func TestEvaluationCaseUpdatesPersistAndDeletionRemovesOnlySelectedCase(t *testing.T) {
	c := client(t)
	collectionID := c.collection(t, "cases.csv", []byte("question,reference_answer\noriginal,answer\n"))
	path := evaluationPath + "/collections/" + collectionID + "/cases"
	created := requireOK(t, c.json(t, http.MethodPost, path, object{"question": "second", "reference_answer": "second answer", "metadata": object{"tag": "fixture"}}))
	caseID := id(t, created)
	requireOK(t, c.json(t, http.MethodPatch, path+"/"+caseID, object{"variable": object{"question": "edited", "reference_answer": "edited answer"}}))
	found := false
	for _, row := range c.cases(t, collectionID) {
		entry := row.(object)
		if entry["id"] == caseID {
			found = entry["variable"].(object)["question"] == "edited"
		}
	}
	if !found {
		t.Fatal("case update was not persisted")
	}
	requireOK(t, c.json(t, http.MethodDelete, path+"/"+caseID, nil))
	cases := c.cases(t, collectionID)
	if len(cases) != 1 || cases[0].(object)["variable"].(object)["question"] != "original" {
		t.Fatal("case deletion affected a different case")
	}
}

func TestEvaluationCaseCannotBeMutatedThroughAnotherCollection(t *testing.T) {
	c := client(t)
	first := c.collection(t, "first.csv", []byte("question,reference_answer\nfirst,answer\n"))
	second := c.collection(t, "second.csv", []byte("question,reference_answer\nsecond,answer\n"))
	caseID := id(t, c.cases(t, first)[0].(object))
	path := evaluationPath + "/collections/" + second + "/cases/" + caseID
	requireRejected(t, c.json(t, http.MethodPatch, path, object{"variable": object{"question": "stolen"}}))
	requireRejected(t, c.json(t, http.MethodDelete, path, nil))
	if c.cases(t, first)[0].(object)["variable"].(object)["question"] != "first" {
		t.Fatal("foreign collection request changed its case")
	}
}

func TestEvaluationJSONImportReportsRejectedCases(t *testing.T) {
	c := client(t)
	collectionID := c.collection(t, "cases.csv", []byte("question,reference_answer\noriginal,answer\n"))
	data := requireOK(t, c.json(t, http.MethodPost, evaluationPath+"/collections/"+collectionID+"/imports", object{"cases": []any{
		object{"question": "new", "reference_answer": "new answer"},
		object{"question": "", "reference_answer": "invalid"},
	}}))
	if data["success_count"] != float64(1) || data["failure_count"] != float64(1) {
		t.Fatalf("unexpected import counts: %v", data)
	}
	if len(c.cases(t, collectionID)) != 2 {
		t.Fatal("import did not preserve existing and accepted cases")
	}
}

func TestEvaluationExportIsWorkbookWithCaseContent(t *testing.T) {
	c := client(t)
	collectionID := c.collection(t, "cases.csv", []byte("question,reference_answer\nexport question,export answer\n"))
	r := c.json(t, http.MethodGet, evaluationPath+"/collections/"+collectionID+"/export", nil)
	if r.status != http.StatusOK || !strings.Contains(r.header.Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("export is not XLSX: HTTP=%d type=%q", r.status, r.header.Get("Content-Type"))
	}
	book, err := excelize.OpenReader(bytes.NewReader(r.body))
	if err != nil {
		t.Fatal("export is not a readable workbook:", err)
	}
	defer book.Close()
	rows, err := book.GetRows(book.GetSheetName(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !strings.Contains(strings.Join(rows[1], "|"), "export question|export answer") {
		t.Fatalf("export did not preserve case content: %v", rows)
	}
}
