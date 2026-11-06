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
	"encoding/json"
	"fmt"
)

type adminIngestionTasksResponse struct {
	CommonResponse
	Total int64
}

func (r *adminIngestionTasksResponse) PrintOut() {
	if r.Code != 0 || r.OutputFormat != OutputFormatJSON {
		r.CommonResponse.PrintOut()
		if r.Code == 0 {
			fmt.Printf("Total %d\n", r.Total)
		}
		return
	}
	body, err := json.MarshalIndent(struct {
		Data  []map[string]interface{} `json:"data"`
		Total int64                    `json:"total"`
	}{Data: r.Data, Total: r.Total}, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling ingestion tasks: %v\n", err)
		return
	}
	fmt.Println(string(body))
}

func handleAdminIngestionTasksResponse(response *Response) (*adminIngestionTasksResponse, error) {
	result, err := HandleCommonResponse(response, "list admin ingestion tasks")
	if err != nil {
		return nil, err
	}
	var metadata struct {
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(response.Body, &metadata); err != nil {
		return nil, fmt.Errorf("failed to decode admin ingestion task total: %w", err)
	}
	commonResponse := result.(*CommonResponse)
	if commonResponse.Data == nil {
		commonResponse.Data = []map[string]interface{}{}
	}
	return &adminIngestionTasksResponse{CommonResponse: *commonResponse, Total: metadata.Total}, nil
}
