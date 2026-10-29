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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// AdminListUserAgentsCommand lists agents or pipelines created by the target user.
func (c *CLI) AdminListUserAgentsCommand(commandCount int, cmd *Command) (ResponseIf, error) {
	if c.Config.CLIMode != AdminMode || c.AdminServerClient.LoginToken == nil {
		return nil, fmt.Errorf("this command is only allowed in ADMIN mode or already login")
	}

	userName, ok := cmd.Params["user_name"].(string)
	if !ok {
		return nil, fmt.Errorf("user_name not provided")
	}

	query := url.Values{}
	for _, name := range []string{"canvas_category", "page", "page_size", "order_by", "desc"} {
		if value, ok := cmd.Params[name]; ok {
			query.Set(name, fmt.Sprint(value))
		}
	}
	apiURL := fmt.Sprintf("/admin/users/%s/agents", url.PathEscape(userName))
	if len(query) > 0 {
		apiURL += "?" + query.Encode()
	}
	resp, err := c.AdminServerClient.Request(commandCount, "GET", apiURL, "admin", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents created by user %s: %w", userName, err)
	}
	return HandleCommonResponse(resp, fmt.Sprintf("list agents created by user %s", userName))
}

// LIST USER 'user@example.com' AGENTS WITH canvas_category 'agent_canvas' order_by update_time desc true;
func (p *Parser) parseAdminListUserAgentsEE(userName string) (*Command, error) {
	p.nextToken()
	cmd := NewCommand("admin_list_user_agents")
	if p.curToken.Type == TokenWith {
		p.nextToken()
		if p.curToken.Type == TokenEOF || p.curToken.Type == TokenSemicolon {
			return nil, fmt.Errorf("expected an AGENTS parameter after WITH")
		}
		for p.curToken.Type != TokenEOF && p.curToken.Type != TokenSemicolon {
			name := strings.ToLower(p.curToken.Value)
			switch name {
			case "canvas_category", "page", "page_size", "order_by", "desc":
			default:
				return nil, fmt.Errorf("unknown AGENTS parameter %q", name)
			}
			if _, exists := cmd.Params[name]; exists {
				return nil, fmt.Errorf("duplicate AGENTS parameter %q", name)
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
	if p.curToken.Type == TokenSemicolon {
		p.nextToken()
	}
	if p.curToken.Type != TokenEOF {
		return nil, fmt.Errorf("expected WITH or end of AGENTS command, got %s", p.curToken.Value)
	}
	cmd.Params["user_name"] = userName
	return cmd, nil
}

func (c *CLI) AdminListUserDatasetsCommand(commandCount int, cmd *Command) (ResponseIf, error) {
	if c.Config == nil || c.Config.CLIMode != AdminMode || c.AdminServerClient == nil || c.AdminServerClient.LoginToken == nil {
		return nil, fmt.Errorf("this command requires ADMIN mode and an authenticated admin client")
	}
	userName, ok := cmd.Params["user_name"].(string)
	if !ok {
		return nil, fmt.Errorf("user_name not provided")
	}

	query := url.Values{}
	for _, name := range []string{"page", "page_size", "order_by", "desc"} {
		if value, ok := cmd.Params[name]; ok {
			query.Set(name, fmt.Sprint(value))
		}
	}
	apiURL := fmt.Sprintf("/admin/users/%s/datasets", url.PathEscape(userName))
	if len(query) > 0 {
		apiURL += "?" + query.Encode()
	}
	resp, err := c.AdminServerClient.Request(commandCount, "GET", apiURL, "admin", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list user %s datasets: %w", userName, err)
	}
	return HandleCommonResponse(resp, fmt.Sprintf("list user %s datasets", userName))
}

// LIST USER 'user@example.com' DATASETS WITH page 2 page_size 5 order_by create_time desc true;
func (p *Parser) parseAdminListUserDatasetsEE(userName string) (*Command, error) {
	p.nextToken()
	cmd := NewCommand("admin_list_user_datasets")
	if p.curToken.Type == TokenWith {
		p.nextToken()
		if p.curToken.Type == TokenEOF || p.curToken.Type == TokenSemicolon {
			return nil, fmt.Errorf("expected a DATASETS parameter after WITH")
		}
		for p.curToken.Type != TokenEOF && p.curToken.Type != TokenSemicolon {
			name := strings.ToLower(p.curToken.Value)
			switch name {
			case "page", "page_size", "order_by", "desc":
			default:
				return nil, fmt.Errorf("unknown DATASETS parameter %q", name)
			}
			if _, exists := cmd.Params[name]; exists {
				return nil, fmt.Errorf("duplicate DATASETS parameter %q", name)
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
	if p.curToken.Type == TokenSemicolon {
		p.nextToken()
	}
	if p.curToken.Type != TokenEOF {
		return nil, fmt.Errorf("expected WITH or end of DATASETS command, got %s", p.curToken.Value)
	}
	cmd.Params["user_name"] = userName
	return cmd, nil
}

func (c *CLI) ListAdminIngestionTasks(commandCount int, cmd *Command) (ResponseIf, error) {
	return c.listAdminIngestionTasks(commandCount, cmd)
}

func (p *Parser) parseAdminListIngestionTasks() (*Command, error) {
	return p.parseAdminIngestionTaskList("list_admin_ingestion_tasks", "")
}

func (p *Parser) parseAdminShowIngestionLog() (*Command, error) {
	return nil, errors.New("ingestion logs are only available in the enterprise edition")
}

func (c *CLI) AdminShowIngestionLogCommand(commandCount int, cmd *Command) (ResponseIf, error) {
	return nil, errors.New("ingestion logs are only available in the enterprise edition")
}

func (c *CLI) AdminShowIngestionMessagesCommand(commandCount int, cmd *Command) (ResponseIf, error) {
	return nil, errors.New("ingestion logs are only available in the enterprise edition")
}

func (c *CLI) AdminListUserIngestionTasksCommand(commandCount int, cmd *Command) (ResponseIf, error) {
	if userName, ok := cmd.Params["user_name"].(string); !ok || strings.TrimSpace(userName) == "" {
		return nil, errors.New("user_name not provided")
	}
	return c.listAdminIngestionTasks(commandCount, cmd)
}

func (p *Parser) parseAdminListUserIngestionTasks(userName string) (*Command, error) {
	if strings.TrimSpace(userName) == "" {
		return nil, errors.New("user_name is required")
	}
	return p.parseAdminIngestionTaskList("admin_list_user_ingestion_tasks", userName)
}

func (c *CLI) listAdminIngestionTasks(commandCount int, cmd *Command) (ResponseIf, error) {
	if c.Config == nil || c.Config.CLIMode != AdminMode || c.AdminServerClient == nil || c.AdminServerClient.LoginToken == nil {
		return nil, errors.New("listing admin ingestion tasks requires admin mode and login")
	}
	query := url.Values{}
	for name, value := range cmd.Params {
		if !isAdminIngestionTaskParameter(name) {
			return nil, fmt.Errorf("unknown ingestion task parameter %q", name)
		}
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("invalid value for %s", name)
		}
		query.Set(name, text)
	}
	path := "/admin/ingestion/tasks"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	response, err := c.AdminServerClient.Request(commandCount, http.MethodGet, path, "admin", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list admin ingestion tasks: %w", err)
	}
	result, err := handleAdminIngestionTasksResponse(response)
	if err != nil {
		return nil, err
	}
	result.SetOutputFormat(c.Config.OutputFormat)
	return result, nil
}

// LIST [USER 'email'] INGESTION TASKS [WITH parameter value ...].
// USER and the user_name filter both take an account email address.
func (p *Parser) parseAdminIngestionTaskList(commandType, userName string) (*Command, error) {
	p.nextToken() // INGESTION
	if p.curToken.Type != TokenTasks {
		return nil, errors.New("expected TASKS after INGESTION")
	}
	p.nextToken()
	cmd := NewCommand(commandType)
	if userName != "" {
		cmd.Params["user_name"] = userName
	}
	if p.curToken.Type == TokenWith {
		p.nextToken()
		if p.curToken.Type == TokenEOF || p.curToken.Type == TokenSemicolon {
			return nil, errors.New("expected an ingestion task parameter after WITH")
		}
		for p.curToken.Type != TokenEOF && p.curToken.Type != TokenSemicolon {
			name := strings.ToLower(p.curToken.Value)
			if !isAdminIngestionTaskParameter(name) {
				return nil, fmt.Errorf("unknown ingestion task parameter %q", name)
			}
			if _, exists := cmd.Params[name]; exists {
				return nil, fmt.Errorf("duplicate ingestion task parameter %q", name)
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
	if p.curToken.Type == TokenSemicolon {
		p.nextToken()
	}
	if p.curToken.Type != TokenEOF {
		return nil, errors.New("expected WITH or end of ingestion task command")
	}
	return cmd, nil
}

func isAdminIngestionTaskParameter(name string) bool {
	switch name {
	case "user_name", "status", "dataset_id", "dataset_name", "document_name", "order_by", "page", "page_size", "desc":
		return true
	default:
		return false
	}
}

func (c *CLI) AdminStopIngestionCommand(commandCount int, cmd *Command) (ResponseIf, error) {
	return nil, errors.New("stopping admin ingestion tasks is not supported in the open-source edition")
}

func (c *CLI) AdminRemoveIngestionCommand(commandCount int, cmd *Command) (ResponseIf, error) {
	return nil, errors.New("removing admin ingestion tasks is not supported in the open-source edition")
}

func (c *CLI) AdminStopUserIngestionTasksCommand(commandCount int, cmd *Command) (ResponseIf, error) {
	return nil, errors.New("stopping user ingestion tasks in admin mode is not supported in the open-source edition")
}

func (c *CLI) AdminRemoveUserIngestionTasksCommand(commandCount int, cmd *Command) (ResponseIf, error) {
	return nil, errors.New("removing user ingestion tasks in admin mode is not supported in the open-source edition")
}

func (p *Parser) parseAdminStopIngestionTasks() (*Command, error) {
	return nil, errors.New("stopping admin ingestion tasks is not supported in the open-source edition")
}

func (p *Parser) parseAdminRemoveIngestionTasks() (*Command, error) {
	return nil, errors.New("removing admin ingestion tasks is not supported in the open-source edition")
}

func (p *Parser) parseAdminStopUserIngestionTasks(userName string) (*Command, error) {
	return nil, errors.New("stopping user ingestion tasks in admin mode is not supported in the open-source edition")
}

func (p *Parser) parseAdminRemoveUserIngestionTasks(userName string) (*Command, error) {
	return nil, errors.New("removing user ingestion tasks in admin mode is not supported in the open-source edition")
}
