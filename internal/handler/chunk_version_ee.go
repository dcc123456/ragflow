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

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"ragflow/internal/common"
	"ragflow/internal/service"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// chunkVersionService is the chunk history the shared chunk service exposes in
// this edition. The shared chunkService interface stays free of it, so the
// community build of this tree needs no counterpart.
type chunkVersionService interface {
	ListVersions(ctx context.Context, req *service.ChunkVersionRequest) (*service.ListChunkVersionsResponse, error)
	GetVersion(ctx context.Context, req *service.ChunkVersionRequest) (*service.GetChunkVersionResponse, error)
	DiffVersions(ctx context.Context, req *service.ChunkVersionDiffRequest) (*service.ChunkVersionDiffResponse, error)
	RestoreVersion(ctx context.Context, req *service.ChunkVersionRequest) (*service.RestoreChunkVersionResponse, error)
	DeleteVersion(ctx context.Context, req *service.ChunkVersionRequest) (*service.DeleteChunkVersionResponse, error)
	VersionImage(ctx context.Context, req *service.ChunkVersionRequest) ([]byte, error)
}

// chunkVersionParams binds the path parameters shared by the chunk version
// endpoints. It answers the request itself when a parameter is missing or
// malformed, so the callers only handle a successful parse.
func chunkVersionParams(c *gin.Context, withVersion bool) (*service.ChunkVersionRequest, bool) {
	datasetID := strings.TrimSpace(c.Param("dataset_id"))
	documentID := strings.TrimSpace(c.Param("document_id"))
	chunkID := strings.TrimSpace(c.Param("chunk_id"))
	if datasetID == "" || documentID == "" || chunkID == "" {
		common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "dataset_id, document_id and chunk_id are required")
		return nil, false
	}

	req := &service.ChunkVersionRequest{
		DatasetID:  datasetID,
		DocumentID: documentID,
		ChunkID:    chunkID,
	}
	if !withVersion {
		return req, true
	}
	version, err := strconv.Atoi(strings.TrimSpace(c.Param("version")))
	if err != nil || version < 0 {
		common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "version must be a non-negative integer")
		return nil, false
	}
	req.Version = version
	return req, true
}

// versions answers with the chunk history and reports whether it could.
func (h *ChunkHandler) versions(c *gin.Context) (chunkVersionService, bool) {
	versioned, ok := h.chunkService.(chunkVersionService)
	if !ok {
		common.Respond(c, http.StatusInternalServerError, 500, nil, "chunk version history is not available")
		return nil, false
	}
	return versioned, true
}

// ListChunkVersions lists a chunk's version history.
// @Summary List Chunk Versions
// @Description List every stored version of a chunk, newest first.
// @Tags chunks
// @Success 200 {object} service.ListChunkVersionsResponse
// @Router /api/v1/datasets/{dataset_id}/documents/{document_id}/chunks/{chunk_id}/versions [get]
func (h *ChunkHandler) ListChunkVersions(c *gin.Context) {
	req, ok := chunkVersionParams(c, false)
	if !ok {
		return
	}
	svc, ok := h.versions(c)
	if !ok {
		return
	}

	resp, err := svc.ListVersions(c.Request.Context(), req)
	if err != nil {
		if respondPermissionErrorIf(c, err, false) {
			return
		}
		common.Respond(c, http.StatusInternalServerError, 500, nil, err.Error())
		return
	}

	common.RespondSuccess(c, resp, "success")
}

// GetChunkVersion returns one stored chunk version.
// @Summary Get Chunk Version
// @Description Retrieve one stored version of a chunk, including its content.
// @Tags chunks
// @Success 200 {object} service.GetChunkVersionResponse
// @Router /api/v1/datasets/{dataset_id}/documents/{document_id}/chunks/{chunk_id}/versions/{version} [get]
func (h *ChunkHandler) GetChunkVersion(c *gin.Context) {
	req, ok := chunkVersionParams(c, true)
	if !ok {
		return
	}
	svc, ok := h.versions(c)
	if !ok {
		return
	}

	resp, err := svc.GetVersion(c.Request.Context(), req)
	if err != nil {
		if respondPermissionErrorIf(c, err, false) {
			return
		}
		common.Respond(c, http.StatusInternalServerError, 500, nil, err.Error())
		return
	}

	common.RespondSuccess(c, resp, "success")
}

// DiffChunkVersions compares two stored versions of a chunk and reports the
// versioned fields that changed between them.
// @Summary Diff Chunk Versions
// @Description Compare two stored versions of a chunk field by field. `to`
// defaults to the latest version, so `?from=0` compares the parser's output
// with the newest edit.
// @Tags chunks
// @Success 200 {object} service.ChunkVersionDiffResponse
// @Router /api/v1/datasets/{dataset_id}/documents/{document_id}/chunks/{chunk_id}/versions/diff [get]
func (h *ChunkHandler) DiffChunkVersions(c *gin.Context) {
	req, ok := chunkVersionParams(c, false)
	if !ok {
		return
	}
	svc, ok := h.versions(c)
	if !ok {
		return
	}

	from, err := strconv.Atoi(strings.TrimSpace(c.Query("from")))
	if err != nil || from < 0 {
		common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "`from` must be a non-negative integer")
		return
	}
	diffReq := &service.ChunkVersionDiffRequest{
		DatasetID:  req.DatasetID,
		DocumentID: req.DocumentID,
		ChunkID:    req.ChunkID,
		From:       from,
	}
	if rawTo := strings.TrimSpace(c.Query("to")); rawTo != "" {
		to, err := strconv.Atoi(rawTo)
		if err != nil || to < 0 {
			common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "`to` must be a non-negative integer")
			return
		}
		diffReq.To = &to
	}

	resp, err := svc.DiffVersions(c.Request.Context(), diffReq)
	if err != nil {
		if respondPermissionErrorIf(c, err, false) {
			return
		}
		if errors.Is(err, service.ErrChunkVersionNotFound) {
			common.Respond(c, http.StatusNotFound, common.CodeNotFound, nil, err.Error())
			return
		}
		common.Respond(c, http.StatusInternalServerError, 500, nil, err.Error())
		return
	}

	common.RespondSuccess(c, resp, "success")
}

// RestoreChunkVersion restores a chunk to a stored version.
// @Summary Restore Chunk Version
// @Description Write a stored version back onto the chunk and record it as a new version.
// @Tags chunks
// @Success 200 {object} service.RestoreChunkVersionResponse
// @Router /api/v1/datasets/{dataset_id}/documents/{document_id}/chunks/{chunk_id}/versions/{version}/restore [post]
func (h *ChunkHandler) RestoreChunkVersion(c *gin.Context) {
	req, ok := chunkVersionParams(c, true)
	if !ok {
		return
	}
	svc, ok := h.versions(c)
	if !ok {
		return
	}

	// The body is optional; when present it may carry the optimistic-lock
	// token of the version the caller read as current.
	if c.Request.Body != nil {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "invalid request body: "+err.Error())
			return
		}
		if len(body) > 0 {
			var rawBody map[string]interface{}
			if err := json.Unmarshal(body, &rawBody); err != nil {
				common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "invalid JSON body: "+err.Error())
				return
			}
			expectedVersion, err := optionalBodyVersion(rawBody)
			if err != nil {
				common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, err.Error())
				return
			}
			req.ExpectedVersion = expectedVersion
		}
	}

	resp, err := svc.RestoreVersion(c.Request.Context(), req)
	if err != nil {
		if respondPermissionErrorIf(c, err, false) {
			return
		}
		var coded interface {
			Code() common.ErrorCode
		}
		if errors.As(err, &coded) {
			switch coded.Code() {
			case common.CodeArgumentError, common.CodeBadRequest, common.CodeDataError:
				common.Respond(c, http.StatusBadRequest, coded.Code(), nil, err.Error())
				return
			case common.CodeConflict:
				common.Respond(c, http.StatusConflict, coded.Code(), nil, err.Error())
				return
			}
		}
		common.Respond(c, http.StatusInternalServerError, 500, nil, err.Error())
		return
	}

	common.RespondSuccess(c, resp, "chunk version restored successfully")
}

// DeleteChunkVersion drops one version from a chunk's history.
// @Summary Delete Chunk Version
// @Description Remove one stored version. The latest version, the parse baseline
// @Description and a delete marker cannot be removed, and the chunk itself is not
// @Description rewritten: the remaining versions keep their numbers.
// @Tags chunks
// @Success 200 {object} service.DeleteChunkVersionResponse
// @Router /api/v1/datasets/{dataset_id}/documents/{document_id}/chunks/{chunk_id}/versions/{version} [delete]
func (h *ChunkHandler) DeleteChunkVersion(c *gin.Context) {
	req, ok := chunkVersionParams(c, true)
	if !ok {
		return
	}
	svc, ok := h.versions(c)
	if !ok {
		return
	}

	resp, err := svc.DeleteVersion(c.Request.Context(), req)
	if err != nil {
		if respondPermissionErrorIf(c, err, false) {
			return
		}
		var coded interface {
			Code() common.ErrorCode
		}
		if errors.As(err, &coded) {
			switch coded.Code() {
			case common.CodeArgumentError, common.CodeBadRequest, common.CodeDataError:
				common.Respond(c, http.StatusBadRequest, coded.Code(), nil, err.Error())
				return
			case common.CodeNotFound:
				common.Respond(c, http.StatusNotFound, coded.Code(), nil, err.Error())
				return
			case common.CodeConflict:
				common.Respond(c, http.StatusConflict, coded.Code(), nil, err.Error())
				return
			}
		}
		common.Respond(c, http.StatusInternalServerError, 500, nil, err.Error())
		return
	}

	common.RespondSuccess(c, resp, "chunk version deleted successfully")
}

// GetChunkVersionImage streams the picture a stored version recorded.
// @Summary Get Chunk Version Image
// @Description Retrieve the picture a stored chunk version recorded. The bytes
// are content-addressed, so a picture that the live chunk no longer references
// is still readable through the version that recorded it.
// @Tags chunks
// @Produce image/png
// @Success 200 {file} binary
// @Router /api/v1/datasets/{dataset_id}/documents/{document_id}/chunks/{chunk_id}/versions/{version}/image [get]
func (h *ChunkHandler) GetChunkVersionImage(c *gin.Context) {
	req, ok := chunkVersionParams(c, true)
	if !ok {
		return
	}
	svc, ok := h.versions(c)
	if !ok {
		return
	}

	data, err := svc.VersionImage(c.Request.Context(), req)
	if err != nil {
		if respondPermissionErrorIf(c, err, false) {
			return
		}
		// A version that never had a picture and one whose bytes are gone
		// answer the same way: the caller asked for something that is not
		// there, and no retry will produce it.
		if errors.Is(err, service.ErrChunkVersionNotFound) || errors.Is(err, service.ErrChunkVersionImageNotFound) {
			common.Respond(c, http.StatusNotFound, common.CodeNotFound, nil, err.Error())
			return
		}
		common.Respond(c, http.StatusInternalServerError, 500, nil, err.Error())
		return
	}

	contentType := documentImageContentType(data)
	if contentType == "" {
		common.Respond(c, http.StatusNotFound, common.CodeNotFound, nil, "chunk version image not found")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, contentType, data)
}
