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
