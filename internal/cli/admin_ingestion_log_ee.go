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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// SHOW INGESTION LOG '<log_id>' [MESSAGES [WITH limit N before_id N|after_id N]];
func (p *Parser) parseAdminIngestionLog() (*Command, error) {
	p.nextToken() // consume INGESTION
	if p.curToken.Type != TokenLog {
		return nil, fmt.Errorf("expected LOG after INGESTION")
	}
	p.nextToken()
	logID, err := p.parseQuotedString()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(logID) == "" {
		return nil, fmt.Errorf("log_id must not be empty")
	}
	cmd := NewCommand("admin_show_ingestion_log")
	cmd.Params["log_id"] = logID
	p.nextToken()
	if p.curToken.Type == TokenIdentifier && strings.EqualFold(p.curToken.Value, "MESSAGES") {
		cmd.Type = "admin_show_ingestion_messages"
		p.nextToken()
		if p.curToken.Type == TokenWith {
			p.nextToken()
			if p.curToken.Type == TokenEOF || p.curToken.Type == TokenSemicolon {
				return nil, fmt.Errorf("expected limit, before_id or after_id after WITH")
			}
			for p.curToken.Type != TokenEOF && p.curToken.Type != TokenSemicolon {
				name := strings.ToLower(p.curToken.Value)
				switch name {
				case "limit", "before_id", "after_id":
				default:
					return nil, fmt.Errorf("unknown MESSAGES parameter %q", name)
				}
				if _, exists := cmd.Params[name]; exists {
					return nil, fmt.Errorf("duplicate MESSAGES parameter %q", name)
				}
				p.nextToken()
				value, err := p.parseVariableValue()
				if err != nil {
					return nil, fmt.Errorf("invalid value for %s: %w", name, err)
				}
				cmd.Params[name] = value
				p.nextToken()
			}
		}
	}
	if p.curToken.Type == TokenSemicolon {
		p.nextToken()
	}
	if p.curToken.Type != TokenEOF {
		return nil, fmt.Errorf("unexpected token after INGESTION LOG command: %s", p.curToken.Value)
	}
	return cmd, nil
}

func (c *CLI) showAdminIngestionLog(commandCount int, cmd *Command, messages bool) (ResponseIf, error) {
	if c.Config == nil || c.Config.CLIMode != AdminMode || c.AdminServerClient == nil || c.AdminServerClient.LoginToken == nil {
		return nil, fmt.Errorf("this command requires ADMIN mode and an authenticated admin client")
	}
	logID, ok := cmd.Params["log_id"].(string)
	if !ok || strings.TrimSpace(logID) == "" {
		return nil, fmt.Errorf("log_id must not be empty")
	}
	apiURL := "/admin/ingestion/logs/" + url.PathEscape(logID)
	command := "show ingestion log"
	if messages {
		apiURL += "/messages"
		command = "show ingestion messages"
		query := url.Values{}
		for _, name := range []string{"limit", "before_id", "after_id"} {
			if value, ok := cmd.Params[name]; ok {
				query.Set(name, fmt.Sprint(value))
			}
		}
		if len(query) > 0 {
			apiURL += "?" + query.Encode()
		}
	}
	response, err := c.AdminServerClient.Request(commandCount, http.MethodGet, apiURL, "admin", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to %s: %w", command, err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to %s: HTTP %d, body: %s", command, response.StatusCode, response.Body)
	}
	var result adminIngestionLogResponse
	decoder := json.NewDecoder(bytes.NewReader(response.Body))
	// Event IDs are pagination cursors; decoding through float64 can change them.
	decoder.UseNumber()
	if err := decoder.Decode(&result.CommonDataResponse); err != nil {
		return nil, fmt.Errorf("%s failed: invalid JSON (%w)", command, err)
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return nil, fmt.Errorf("%s failed: invalid JSON after response object", command)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("%s", result.Message)
	}
	result.Duration = response.Duration
	result.SetOutputFormat(c.Config.OutputFormat)
	return &result, nil
}

type adminIngestionLogResponse struct {
	CommonDataResponse
}

func (r *adminIngestionLogResponse) PrintOut() {
	if r.OutputFormat == OutputFormatJSON {
		data, err := json.MarshalIndent(r.Data, "", "  ")
		if err != nil {
			fmt.Printf("Error marshaling JSON: %v\n", err)
			return
		}
		fmt.Println(string(data))
		return
	}
	fields := make([]string, 0, len(r.Data))
	for name := range r.Data {
		if name != "items" {
			fields = append(fields, name)
		}
	}
	sort.Strings(fields)
	metadata := make([]map[string]interface{}, 0, len(fields))
	for _, name := range fields {
		metadata = append(metadata, map[string]interface{}{"field": name, "value": r.Data[name]})
	}
	printAdminIngestionLogRows(metadata, r.OutputFormat, "field", "value")
	if items, ok := r.Data["items"].([]interface{}); ok {
		rows := make([]map[string]interface{}, 0, len(items))
		for _, item := range items {
			if row, ok := item.(map[string]interface{}); ok {
				rows = append(rows, row)
			}
		}
		fmt.Println()
		printAdminIngestionLogRows(rows, r.OutputFormat, "id", "ts", "event_type", "component", "phase", "message")
	}
}

func printAdminIngestionLogRows(rows []map[string]interface{}, format OutputFormat, columns ...string) {
	if format != OutputFormatPlain || len(rows) == 0 {
		PrintTableSimpleByFormat(rows, format)
		return
	}
	fmt.Println(strings.Join(columns, "  "))
	for _, row := range rows {
		values := make([]string, 0, len(columns))
		for _, column := range columns {
			values = append(values, formatValue(row[column]))
		}
		fmt.Println(strings.Join(values, "  "))
	}
}
