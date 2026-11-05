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

package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"ragflow/internal/common"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

// RegistrationWhitelist applies the EE admission policy to the public signup route.
func RegistrationWhitelist() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.FullPath() != "/api/v1/users" {
			c.Next()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, common.GetMaxJSONRequestBodyBytes())
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			status := http.StatusBadRequest
			var limitError *http.MaxBytesError
			if errors.As(err, &limitError) {
				status = http.StatusRequestEntityTooLarge
			}
			common.Respond(c, status, common.CodeBadRequest, nil, "Invalid registration request")
			c.Abort()
			return
		}
		// Preserve the body for the shared registration handler and its validation.
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		var request struct {
			Email string `json:"email"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, nil, "Invalid registration request")
			c.Abort()
			return
		}
		if err := service.CheckRegistrationWhitelist(c.Request.Context(), request.Email); err != nil {
			var coded *common.CodedError
			if errors.As(err, &coded) {
				common.Respond(c, int(coded.Code), coded.Code, nil, coded.Message)
			} else {
				common.Error("Registration whitelist check failed", err)
				common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, "Registration is temporarily unavailable")
			}
			c.Abort()
			return
		}
		c.Next()
	}
}
