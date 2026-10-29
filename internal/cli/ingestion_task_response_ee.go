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
