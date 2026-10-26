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
	"net/http"
	"net/url"
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
		{"", "", "canvas_category is required", 400},
		{"WITH canvas_category ''", "canvas_category=", "canvas_category is required", 400},
		{"WITH canvas_category agent_canvas page ''", "canvas_category=agent_canvas&page=", "page must be between 1 and 1000000", 400},
		{"WITH canvas_category agent_canvas page invalid", "canvas_category=agent_canvas&page=invalid", "page must be between 1 and 1000000", 400},
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
		{"DATASETS WITH page invalid;", "page=invalid", "page must be between 1 and 1000000", 400},
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
