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

//go:build ee

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAdminListUserAgentsCommandPassesOptions(t *testing.T) {
	tests := []struct {
		options, query, message string
		code                    int
	}{
		{"WITH CaNvAs_CaTeGoRy 'dataflow_canvas' PaGe 2 PAGE_SIZE 5", "canvas_category=dataflow_canvas&page=2&page_size=5", "success", 0},
		{"WITH canvas_category 'agent_canvas'", "canvas_category=agent_canvas", "success", 0},
		{"WITH CaNvAs_CaTeGoRy dataflow_canvas OrDeR_By 'update_time' DeSc false PaGe 2 PAGE_SIZE 5", "canvas_category=dataflow_canvas&desc=false&order_by=update_time&page=2&page_size=5", "success", 0},
		{"WITH canvas_category agent_canvas order_by create_time", "canvas_category=agent_canvas&order_by=create_time", "success", 0},
		{"WITH canvas_category agent_canvas desc true", "canvas_category=agent_canvas&desc=true", "success", 0},
		{"WITH canvas_category agent_canvas order_by ' UPDATE_TIME ' desc ' false '", "canvas_category=agent_canvas&desc=+false+&order_by=+UPDATE_TIME+", "success", 0},
		{"", "", "canvas_category is required", 400},
		{"WITH canvas_category ''", "canvas_category=", "canvas_category is required", 400},
		{"WITH canvas_category agent_canvas page ''", "canvas_category=agent_canvas&page=", "page must be between 1 and 1000000", 400},
		{"WITH canvas_category agent_canvas page invalid", "canvas_category=agent_canvas&page=invalid", "page must be between 1 and 1000000", 400},
		{"WITH canvas_category agent_canvas order_by title", "canvas_category=agent_canvas&order_by=title", "order_by must be create_time or update_time", 400},
		{"WITH canvas_category agent_canvas order_by ''", "canvas_category=agent_canvas&order_by=", "order_by must be create_time or update_time", 400},
		{"WITH canvas_category agent_canvas desc sideways", "canvas_category=agent_canvas&desc=sideways", "desc must be a boolean", 400},
		{"WITH canvas_category agent_canvas desc ''", "canvas_category=agent_canvas&desc=", "desc must be a boolean", 400},
		{"WITH canvas_category 'Custom_/画布 +&?=%'", "canvas_category=" + url.QueryEscape("Custom_/画布 +&?=%"), "success", 0},
		{"WITH canvas_category agent_canvas", "canvas_category=agent_canvas", "User not found", 404},
	}
	for _, tt := range tests {
		t.Run(tt.options+"/"+tt.message, func(t *testing.T) {
			cmd, err := NewParser("LIST USER 'user@example.com' AGENTS " + tt.options + ";").Parse(AdminMode)
			if err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			client := newTestHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) != 1 {
					http.Error(w, "unexpected extra request", http.StatusBadRequest)
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/users/user@example.com/agents" || r.URL.RawQuery != tt.query {
					t.Errorf("request = %s %s, want GET user agents with query %q", r.Method, r.URL, tt.query)
				}
				body := map[string]interface{}{"code": tt.code, "message": tt.message}
				if tt.code == 0 {
					body["data"] = []map[string]interface{}{{"id": "canvas-1", "title": "current page"}}
					body["total"] = 101
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(body); err != nil {
					t.Error(err)
				}
			}))
			token := "admin-token"
			client.LoginToken = &token
			c := &CLI{Config: &CommandLineConfig{CLIMode: AdminMode}, AdminServerClient: client}
			response, err := c.ExecuteCommand(1, cmd)
			if requests.Load() != 1 {
				t.Fatalf("requests = %d, want 1", requests.Load())
			}
			if tt.code != 0 {
				if err == nil || !strings.Contains(err.Error(), tt.message) || response != nil {
					t.Fatalf("response = %v, error = %v; want nil and %q", response, err, tt.message)
				}
				return
			}
			result, ok := response.(*CommonResponse)
			if err != nil || !ok || result.Message != "success" || len(result.Data) != 1 || result.Data[0]["id"] != "canvas-1" || result.Data[0]["title"] != "current page" {
				t.Fatalf("response = %+v, error = %v; want only the current page", response, err)
			}
		})
	}
}

func TestParseAdminListUserAgentsRejectsOptions(t *testing.T) {
	for _, options := range []string{
		"WITH unknown 1", "WITH page 1 PAGE 2", "WITH page", "WITH",
		"WITH order_by create_time ORDER_BY update_time", "WITH desc true DESC false", "WITH order_by", "WITH desc",
		"canvas_category agent_canvas", "; page 2",
	} {
		t.Run(options, func(t *testing.T) {
			if _, err := NewParser("LIST USER 'user@example.com' AGENTS " + options + ";").Parse(AdminMode); err == nil {
				t.Fatal("expected invalid options to fail parsing")
			}
		})
	}
}

func TestParseAPIListDatasets(t *testing.T) {
	cmd, err := NewParser("LIST DATASETS;").Parse(APIMode)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cmd == nil || cmd.Type != "api_list_datasets" {
		t.Fatalf("Parse() command = %v, want api_list_datasets", cmd)
	}
}
func TestAdminListUserDatasetsCommandPassesOptions(t *testing.T) {
	tests := []struct {
		command, query, message string
		code                    int
	}{
		{"DATASETS", "", "success", 0},
		{"DATASETS WITH PaGe 2 PAGE_SIZE 5;", "page=2&page_size=5", "success", 0},
		{"DATASETS WITH page_size 1;", "page_size=1", "success", 0},
		{"DATASETS WITH OrDeR_By 'update_time' DeSc false PaGe 2 PAGE_SIZE 5;", "desc=false&order_by=update_time&page=2&page_size=5", "success", 0},
		{"DATASETS WITH order_by create_time;", "order_by=create_time", "success", 0},
		{"DATASETS WITH desc true;", "desc=true", "success", 0},
		{"DATASETS WITH order_by ' UPDATE_TIME ' desc ' false ';", "desc=+false+&order_by=+UPDATE_TIME+", "success", 0},
		{"DATASETS WITH page invalid;", "page=invalid", "page must be between 1 and 1000000", 400},
		{"DATASETS WITH order_by 'name';", "order_by=name", "order_by must be create_time or update_time", 400},
		{"DATASETS WITH order_by '';", "order_by=", "order_by must be create_time or update_time", 400},
		{"DATASETS WITH desc sideways;", "desc=sideways", "desc must be a boolean", 400},
		{"DATASETS WITH desc '';", "desc=", "desc must be a boolean", 400},
		{"DATASETS;", "", "User not found", 404},
	}
	for _, tt := range tests {
		t.Run(tt.command+"/"+tt.message, func(t *testing.T) {
			cmd, err := NewParser("LIST USER 'user@example.com' " + tt.command).Parse(AdminMode)
			if err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			client := newTestHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) != 1 {
					http.Error(w, "unexpected extra request", http.StatusBadRequest)
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/users/user@example.com/datasets" || r.URL.RawQuery != tt.query {
					t.Errorf("request = %s %s, want GET user datasets with query %q", r.Method, r.URL, tt.query)
				}
				body := map[string]interface{}{"code": tt.code, "message": tt.message}
				if tt.code == 0 {
					body["data"] = []map[string]interface{}{{"id": "dataset-1", "tenant_id": "tenant-2", "tenant_name": "Team workspace", "file_size": 123}}
					body["total"] = 101
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(body); err != nil {
					t.Error(err)
				}
			}))
			token := "admin-token"
			client.LoginToken = &token
			c := &CLI{Config: &CommandLineConfig{CLIMode: AdminMode}, AdminServerClient: client}
			response, err := c.ExecuteCommand(1, cmd)
			if requests.Load() != 1 {
				t.Fatalf("requests = %d, want 1", requests.Load())
			}
			if tt.code != 0 {
				if err == nil || response != nil || !strings.Contains(err.Error(), tt.message) {
					t.Fatalf("response = %v, error = %v; want nil and %q", response, err, tt.message)
				}
				return
			}
			result, ok := response.(*CommonResponse)
			if err != nil || !ok || result.Code != 0 || result.Message != "success" || len(result.Data) != 1 || result.Data[0]["id"] != "dataset-1" || result.Data[0]["tenant_id"] != "tenant-2" || result.Data[0]["tenant_name"] != "Team workspace" || result.Data[0]["file_size"] != float64(123) {
				t.Fatalf("response = %+v, error = %v; want only the current page with tenant and file size", response, err)
			}
		})
	}
}

func TestParseAdminListUserDatasetsRejectsOptions(t *testing.T) {
	for _, options := range []string{
		"WITH unknown 1", "WITH page 1 PAGE 2", "WITH page", "WITH", "; page 2",
		"WITH order_by create_time ORDER_BY update_time", "WITH desc true DESC false", "WITH order_by", "WITH desc",
	} {
		t.Run(options, func(t *testing.T) {
			if _, err := NewParser("LIST USER 'user@example.com' DATASETS " + options + ";").Parse(AdminMode); err == nil {
				t.Fatal("expected invalid options to fail parsing")
			}
		})
	}
}

func TestParseAdminListUserDatasetsRequiresEE(_ *testing.T) {}

func TestAdminListUserDatasetsRequiresEE(_ *testing.T) {}

func TestParseAdminListUserAgentsRequiresEE(_ *testing.T) {}

func TestAdminListUserAgentsRequiresEE(_ *testing.T) {}

func TestParseAPIListAgents(_ *testing.T) {}

func TestAdminIngestionLogCommands(t *testing.T) {
	const detail = `{"id":"run-1","name":"example.txt","chunk_count":0,"nickname":null}`
	longMessage := "Parser Started " + strings.Repeat("detail ", 80) + "last diagnostic line"
	messages := `{"run_count":1,"items":[{"id":9007199254740993,"ts":null,"event_type":0,"component":"Parser","phase":0,"message":"` + longMessage + `"}],"oldest_id":9007199254740993,"newest_id":9007199254740993,"has_more_before":true,"has_more_after":false,"terminal":false}`
	const empty = `{"run_count":1,"items":[],"oldest_id":0,"newest_id":0,"has_more_before":true,"has_more_after":false,"terminal":true}`
	for _, tt := range []struct {
		name, suffix, query, data string
		format                    OutputFormat
	}{
		{"detail", "", "", detail, OutputFormatJSON},
		{"messages", " MESSAGES", "", messages, OutputFormatJSON},
		{"older", " messages WITH LiMiT 2 BEFORE_ID 9007199254740993", "before_id=9007199254740993&limit=2", messages, OutputFormatPlain},
		{"newer", " MESSAGES WITH after_id 9007199254740993", "after_id=9007199254740993", empty, OutputFormatJSON},
		{"table", " MESSAGES WITH limit 500", "limit=500", messages, OutputFormatTable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const logID = "run /+&?=日志"
			cmd, err := NewParser("SHOW INGESTION LOG '" + logID + "'" + tt.suffix + ";").Parse(AdminMode)
			if err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/admin/ingestion/logs/" + url.PathEscape(logID)
			if tt.suffix != "" {
				path += "/messages"
			}
			var requests atomic.Int32
			client := newTestHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				body, err := io.ReadAll(r.Body)
				if err != nil || len(body) != 0 || r.Method != http.MethodGet || r.URL.EscapedPath() != path || r.URL.RawQuery != tt.query {
					t.Errorf("request = %s %s, body = %q, error = %v", r.Method, r.URL, body, err)
				}
				if r.Header.Get("Authorization") != "admin-token" {
					t.Error("admin login token was not forwarded")
				}
				_, _ = io.WriteString(w, `{"code":0,"message":"success","data":`+tt.data+`}`)
			}))
			token := "admin-token"
			client.LoginToken = &token
			c := &CLI{Config: &CommandLineConfig{CLIMode: AdminMode, OutputFormat: tt.format}, AdminServerClient: client}
			response, err := c.ExecuteCommand(1, cmd)
			if err != nil || response == nil || requests.Load() != 1 {
				t.Fatalf("response = %v, error = %v, requests = %d", response, err, requests.Load())
			}
			output := captureAdminIngestionLogOutput(t, response)
			if tt.format == OutputFormatJSON {
				decode := func(input string) any {
					var value any
					decoder := json.NewDecoder(strings.NewReader(input))
					decoder.UseNumber()
					if err := decoder.Decode(&value); err != nil {
						t.Fatalf("invalid JSON output %q: %v", input, err)
					}
					return value
				}
				if !reflect.DeepEqual(decode(output), decode(tt.data)) {
					t.Fatalf("output = %s, want data object %s", output, tt.data)
				}
			} else if !strings.Contains(output, longMessage) || !strings.Contains(output, "has_more_before") || !strings.Contains(output, "9007199254740993") {
				t.Fatalf("output must include messages and pagination metadata: %s", output)
			}
		})
	}
}

func TestParseAdminIngestionLogRejectsInvalidOptions(t *testing.T) {
	for _, input := range []string{
		"SHOW INGESTION LOG '';", "SHOW INGESTION LOG '   ';", "SHOW INGESTION TASK 'id';",
		"SHOW INGESTION LOG 'id' WITH limit 1;", "SHOW INGESTION LOG 'id'; unexpected",
		"SHOW INGESTION LOG 'id' MESSAGES WITH;", "SHOW INGESTION LOG 'id' MESSAGES WITH after_id;",
		"SHOW INGESTION LOG 'id' MESSAGES WITH unknown 1;", "SHOW INGESTION LOG 'id' MESSAGES WITH limit 1 LIMIT 2;",
		"SHOW INGESTION LOG 'id' MESSAGES WITH before_id 1 BEFORE_ID 2;",
	} {
		t.Run(input, func(t *testing.T) {
			if cmd, err := NewParser(input).Parse(AdminMode); cmd != nil || err == nil {
				t.Fatalf("Parse() = (%v, %v), want invalid command", cmd, err)
			}
		})
	}
}

func TestAdminIngestionLogErrors(t *testing.T) {
	for _, tt := range []struct {
		status                        int
		options, query, body, message string
	}{
		{200, "", "", `{"code":102,"message":"log not found"}`, "log not found"},
		{200, "", "", `{"code":500,"message":"database unavailable"}`, "database unavailable"},
		{403, "", "", `{"code":403,"message":"Permission denied"}`, "HTTP 403"},
		{200, "", "", `{"code":0,"data":`, "invalid JSON"},
		{200, "", "", `{"code":0,"data":{}} {}`, "invalid JSON after response object"},
		{200, " MESSAGES WITH limit 0", "limit=0", `{"code":101,"message":"limit must be a positive integer"}`, "limit must be a positive integer"},
		{200, " MESSAGES WITH limit 501", "limit=501", `{"code":101,"message":"limit must be between 1 and 500"}`, "limit must be between 1 and 500"},
		{200, " MESSAGES WITH before_id 1 after_id 2", "after_id=2&before_id=1", `{"code":101,"message":"after_id and before_id are mutually exclusive"}`, "after_id and before_id are mutually exclusive"},
		{200, " MESSAGES WITH after_id 1.5", "after_id=1.5", `{"code":101,"message":"after_id must be a positive integer"}`, "after_id must be a positive integer"},
	} {
		t.Run(tt.message, func(t *testing.T) {
			client := newTestHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RawQuery != tt.query {
					t.Errorf("query = %q, want %q", r.URL.RawQuery, tt.query)
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			token := "admin-token"
			client.LoginToken = &token
			c := &CLI{Config: &CommandLineConfig{CLIMode: AdminMode}, AdminServerClient: client}
			cmd, err := NewParser("SHOW INGESTION LOG 'id'" + tt.options + ";").Parse(AdminMode)
			if err != nil {
				t.Fatal(err)
			}
			if response, err := c.ExecuteCommand(1, cmd); response != nil || err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("response = %v, error = %v, want %s", response, err, tt.message)
			}
		})
	}
}

func captureAdminIngestionLogOutput(t *testing.T, response ResponseIf) string {
	t.Helper()
	outputFile, err := os.CreateTemp(t.TempDir(), "ingestion-log-output-*")
	if err != nil {
		t.Fatal(err)
	}
	defer outputFile.Close()
	stdout := os.Stdout
	os.Stdout = outputFile
	defer func() { os.Stdout = stdout }()
	response.PrintOut()
	if _, err := outputFile.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(outputFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}
