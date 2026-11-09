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

package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (c *CLI) runAdminUserReport(commandCount int, cmd *Command) (ResponseIf, error) {
	path := "/admin/users"
	status, _ := cmd.Params["user_status"].(string)
	_, hasPlan := cmd.Params["plan"]
	_, hasQuota := cmd.Params["quota"]
	_, hasOrder := cmd.Params["order_by"]
	if strings.EqualFold(status, "inactive") || (hasPlan && !hasQuota && !hasOrder) {
		path += "/reports"
	}
	query := url.Values{}
	for name, value := range cmd.Params {
		switch name {
		case "order_by":
			name = "order"
		case "user_status":
			name = "status"
		}
		query.Set(name, fmt.Sprint(value))
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	response, err := c.AdminServerClient.Request(commandCount, http.MethodGet, path, "admin", nil, nil)
	if err != nil {
		return nil, err
	}
	result, err := HandleCommonResponse(response, "get user report")
	if err != nil {
		return nil, err
	}
	result.SetOutputFormat(c.Config.OutputFormat)
	return &OrderedCommonResponse{CommonResponse: *result.(*CommonResponse)}, nil
}
