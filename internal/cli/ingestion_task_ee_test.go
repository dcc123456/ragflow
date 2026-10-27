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

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseAdminIngestionTaskFilters(t *testing.T) {
	input := "LIST INGESTION TASKS WITH user_name 'reader@example.com' status running dataset_id 'dataset-1' dataset_name 'Research notes' document_name 'report.pdf' order_by update_time page '2' page_size 10 desc false;"
	cmd, err := NewParser(input).Parse(AdminMode)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Type != "list_admin_ingestion_tasks" {
		t.Fatalf("command type = %s", cmd.Type)
	}
	want := map[string]interface{}{
		"user_name": "reader@example.com", "dataset_id": "dataset-1", "dataset_name": "Research notes", "document_name": "report.pdf", "order_by": "update_time",
		"status": "running", "page": "2", "page_size": "10", "desc": "false",
	}
	if !reflect.DeepEqual(cmd.Params, want) {
		t.Fatalf("parameters = %#v, want %#v", cmd.Params, want)
	}
}

func TestParseAdminUserIngestionTasksUsesUserNameFilter(t *testing.T) {
	cmd, err := NewParser("LIST USER 'reader+jobs@example.com' INGESTION TASKS WITH dataset_id 'dataset-1' dataset_name 'Research notes' status 'FAILED';").Parse(AdminMode)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Type != "admin_list_user_ingestion_tasks" || cmd.Params["user_name"] != "reader+jobs@example.com" || cmd.Params["dataset_name"] != "Research notes" || cmd.Params["dataset_id"] != "dataset-1" {
		t.Fatalf("command = %#v", cmd)
	}
	if _, err := NewParser("LIST USER 'reader@example.com' INGESTION TASKS WITH user_name 'other@example.com';").Parse(AdminMode); err == nil {
		t.Fatal("expected duplicate user_name error")
	}
}

func TestParseAdminIngestionTasksRejectsInvalidSyntax(t *testing.T) {
	for _, suffix := range []string{
		"WITH", "WITH page", "WITH page ;", "WITH page 1 page 2",
		"WITH unknown 'x'", "WITH dataset_name", "WITH page 1; extra", "'RUNNING'",
	} {
		t.Run(suffix, func(t *testing.T) {
			cmd, err := NewParser("LIST INGESTION TASKS " + suffix + ";").Parse(AdminMode)
			if err == nil || cmd != nil {
				t.Fatalf("Parse() = (%#v, %v), want error", cmd, err)
			}
		})
	}
}

func TestParseAdminIngestionTasksRejectsRemovedParameters(t *testing.T) {
	for _, name := range []string{
		"id", "pipeline_log_id", "pipeline_id", "pipeline_name", "user_id", "user_email", "document_id",
		"create_time_from", "create_time_to",
	} {
		t.Run(name, func(t *testing.T) {
			cmd, err := NewParser("LIST INGESTION TASKS WITH " + name + " 'value';").Parse(AdminMode)
			if cmd != nil || err == nil || !strings.Contains(err.Error(), "unknown ingestion task parameter") {
				t.Fatalf("Parse() = (%#v, %v), want unknown parameter error", cmd, err)
			}
		})
	}
}

func TestAdminIngestionTaskCLIDefersFilterValidationToAPI(t *testing.T) {
	for _, test := range []struct{ name, value string }{
		{"page", "not-a-number"}, {"page", ""}, {"page_size", "101"}, {"status", "UNKNOWN"}, {"desc", "sideways"}, {"order_by", "pipeline_name"},
	} {
		t.Run(test.name+"/"+test.value, func(t *testing.T) {
			client := newTestHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if value := r.URL.Query().Get(test.name); value != test.value || !r.URL.Query().Has(test.name) {
					t.Errorf("query = %v, want %s=%q", r.URL.Query(), test.name, test.value)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `{"code":400,"message":"invalid filter"}`)
			}))
			token := "admin-token"
			client.LoginToken = &token
			c := &CLI{Config: &CommandLineConfig{CLIMode: AdminMode}, AdminServerClient: client}
			cmd, err := NewParser(fmt.Sprintf("LIST INGESTION TASKS WITH %s '%s';", test.name, test.value)).Parse(AdminMode)
			if err != nil {
				t.Fatalf("CLI should defer filter validation to the API: %v", err)
			}
			response, err := c.ExecuteCommand(1, cmd)
			if response != nil || err == nil || err.Error() != "invalid filter" {
				t.Fatalf("ExecuteCommand() = (%v, %v), want the API validation error", response, err)
			}
		})
	}
}

func TestAdminIngestionTaskCLIUsesQueryAndTotalResponse(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		query url.Values
	}{
		{
			name:  "all tasks",
			input: "LIST INGESTION TASKS WITH user_name 'reader+jobs@example.com' dataset_id 'dataset+&%_?/' dataset_name '知识库 +&%_?/' document_name '报告 +&%_?.pdf' status 'RUNNING' order_by update_time page '2' page_size '5' desc false;",
			query: url.Values{"user_name": {"reader+jobs@example.com"}, "dataset_id": {"dataset+&%_?/"}, "dataset_name": {"知识库 +&%_?/"}, "document_name": {"报告 +&%_?.pdf"}, "status": {"RUNNING"}, "order_by": {"update_time"}, "page": {"2"}, "page_size": {"5"}, "desc": {"false"}},
		},
		{
			name:  "user tasks",
			input: "LIST USER 'reader+jobs@example.com' INGESTION TASKS WITH dataset_id 'dataset-1' status 'RUNNING' page 2 page_size 5;",
			query: url.Values{"user_name": {"reader+jobs@example.com"}, "dataset_id": {"dataset-1"}, "status": {"RUNNING"}, "page": {"2"}, "page_size": {"5"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			requested := make(chan struct{}, 1)
			client := newTestHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requested <- struct{}{}
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/ingestion/tasks" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if !reflect.DeepEqual(r.URL.Query(), test.query) {
					t.Errorf("query = %v, want %v", r.URL.Query(), test.query)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || len(body) != 0 {
					t.Errorf("GET body = %q, error = %v; want no body", body, err)
				}
				if r.Header.Get("Authorization") != "admin-token" {
					t.Errorf("authorization = %q", r.Header.Get("Authorization"))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"code":0,"data":[{"id":"task-1","pipeline_log_id":"run-1","nickname":"Reader","email":"reader+jobs@example.com","status":"RUNNING"}],"total":11,"message":""}`)
			}))
			token := "admin-token"
			client.LoginToken = &token
			c := &CLI{Config: &CommandLineConfig{CLIMode: AdminMode, OutputFormat: OutputFormatJSON}, AdminServerClient: client}
			cmd, err := NewParser(test.input).Parse(AdminMode)
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.ExecuteCommand(1, cmd)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-requested:
			default:
				t.Fatal("task query was not sent")
			}
			page, ok := result.(*adminIngestionTasksResponse)
			if !ok || page.Total != 11 || len(page.Data) != 1 || page.OutputFormat != OutputFormatJSON || page.Data[0]["status"] != "RUNNING" ||
				page.Data[0]["nickname"] != "Reader" || page.Data[0]["email"] != "reader+jobs@example.com" {
				t.Fatalf("response = %#v", result)
			}
		})
	}
}

func TestAdminIngestionTaskResponseOutputPreservesTotal(t *testing.T) {
	response, err := handleAdminIngestionTasksResponse(&Response{StatusCode: http.StatusOK, Duration: 1.25, Body: []byte(`{"code":0,"data":null,"total":21}`)})
	if err != nil {
		t.Fatal(err)
	}
	if response.TimeCost() != 1.25 {
		t.Fatalf("duration = %v", response.TimeCost())
	}
	response.SetOutputFormat(OutputFormatJSON)
	output := captureIngestionTaskOutput(t, response.PrintOut)
	var page struct {
		Data  []map[string]interface{} `json:"data"`
		Total int64                    `json:"total"`
	}
	if err := json.Unmarshal([]byte(output), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 21 || page.Data == nil {
		t.Fatalf("JSON output = %s", output)
	}
	response.Data = []map[string]interface{}{{"id": "task-1"}}
	for _, format := range []OutputFormat{OutputFormatPlain, OutputFormatTable} {
		response.SetOutputFormat(format)
		output = captureIngestionTaskOutput(t, response.PrintOut)
		if !strings.Contains(output, "task-1") || !strings.Contains(output, "Total 21") {
			t.Fatalf("%s output = %s", format, output)
		}
	}
}

func TestAdminIngestionTaskResponseRejectsErrors(t *testing.T) {
	for _, response := range []Response{
		{StatusCode: http.StatusOK, Body: []byte(`{"code":400,"message":"invalid query"}`)},
		{StatusCode: http.StatusOK, Body: []byte(`{"code":500,"message":"query failed"}`)},
		{StatusCode: http.StatusOK, Body: []byte(`{"code":0,"data":{"tasks":[],"total":1}}`)},
		{StatusCode: http.StatusOK, Body: []byte(`{"code":0,"data":[],"total":"invalid"}`)},
		{StatusCode: http.StatusOK, Body: []byte(`invalid JSON`)},
	} {
		if _, err := handleAdminIngestionTasksResponse(&response); err == nil {
			t.Fatalf("expected error for response %s", response.Body)
		}
	}
}

func TestUserIngestionTaskCLIStillUsesUserAPI(t *testing.T) {
	cmd, err := NewParser("LIST INGESTION TASKS;").Parse(APIMode)
	if err != nil || cmd == nil || cmd.Type != "api_list_ingestion_tasks" {
		t.Fatalf("user task command = (%#v, %v)", cmd, err)
	}
}

func captureIngestionTaskOutput(t *testing.T, print func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "ingestion-output-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	previous := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = previous }()
	print()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}
