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
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"ragflow/internal/common"
	datasetpkg "ragflow/internal/service/dataset"
	"ragflow/internal/service/datasetcontent"
)

// datasetVersionID parses the :dataset_id and :version path parameters. It
// answers the request itself when either is unusable, so the callers only
// handle a successful parse.
func datasetVersionID(c *gin.Context, withVersion bool) (string, int64, bool) {
	datasetID := strings.TrimSpace(c.Param("dataset_id"))
	if datasetID == "" {
		common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "dataset_id is required")
		return "", 0, false
	}
	if !withVersion {
		return datasetID, 0, true
	}
	version, err := strconv.ParseInt(strings.TrimSpace(c.Param("version")), 10, 64)
	if err != nil || version < 0 {
		common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "version must be a non-negative integer")
		return "", 0, false
	}
	return datasetID, version, true
}

// respondDatasetVersionError maps a coded service error onto its HTTP status.
// Permission failures arrive already normalized by the service, so an unknown
// error is the only case that becomes a 500.
func respondDatasetVersionError(c *gin.Context, err error) {
	var coded interface {
		Code() common.ErrorCode
	}
	if errors.As(err, &coded) {
		switch coded.Code() {
		case common.CodeArgumentError, common.CodeBadRequest, common.CodeDataError:
			common.Respond(c, http.StatusBadRequest, coded.Code(), nil, err.Error())
			return
		case common.CodeUnauthorized, common.CodeForbidden, common.CodeNotFound, common.CodeConflict:
			common.Respond(c, int(coded.Code()), coded.Code(), nil, err.Error())
			return
		}
	}
	// An unclassified error is the server's, and its text is the server's too: it
	// carries driver messages, table and column names, sometimes fragments of a
	// statement. Those go to the log, where an operator can read them; the client
	// gets the fact that the request failed and nothing else.
	common.Error("Dataset version request failed", err,
		zap.String("dataset_id", c.Param("dataset_id")),
		zap.String("version", c.Param("version")))
	common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, "dataset version request failed")
}

// ListDatasetVersions lists a dataset's configuration history.
// @Summary List Dataset Versions
// @Description List the configuration versions of a dataset, newest first.
// @Tags datasets
// @Success 200 {object} datasetpkg.ListDatasetVersionsResponse
// @Router /api/v1/datasets/{dataset_id}/versions [get]
func (h *DatasetsHandler) ListDatasetVersions(c *gin.Context) {
	datasetID, _, ok := datasetVersionID(c, false)
	if !ok {
		return
	}
	if h.datasetsService == nil {
		common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, "dataset version history is not available")
		return
	}

	req := &datasetpkg.ListDatasetVersionsRequest{DatasetID: datasetID}
	if raw := strings.TrimSpace(c.Query("before_version")); raw != "" {
		before, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 0 {
			common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "before_version must be a non-negative integer")
			return
		}
		req.BeforeVersion = &before
	}
	if raw := strings.TrimSpace(c.Query("kind")); raw != "" {
		req.Kinds = strings.Split(raw, ",")
	}
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "limit must be a positive integer")
			return
		}
		req.Limit = limit
	}

	resp, err := h.datasetsService.ListDatasetVersions(c.Request.Context(), req)
	if err != nil {
		respondDatasetVersionError(c, err)
		return
	}
	common.RespondSuccess(c, resp, "success")
}

// GetDatasetVersion returns one version of a dataset: the configuration it ran
// under, the document set it held and the documents and chunks it touched.
// @Summary Get Dataset Version
// @Description Retrieve one stored version of a dataset, with its configuration,
// @Description its document set and the change set that explains it.
// @Tags datasets
// @Success 200 {object} datasetpkg.GetDatasetVersionResponse
// @Router /api/v1/datasets/{dataset_id}/versions/{version} [get]
func (h *DatasetsHandler) GetDatasetVersion(c *gin.Context) {
	datasetID, version, ok := datasetVersionID(c, true)
	if !ok {
		return
	}
	if h.datasetsService == nil {
		common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, "dataset version history is not available")
		return
	}

	resp, err := h.datasetsService.GetDatasetVersion(c.Request.Context(), &datasetpkg.DatasetVersionRequest{
		DatasetID: datasetID,
		Version:   version,
	})
	if err != nil {
		respondDatasetVersionError(c, err)
		return
	}
	common.RespondSuccess(c, resp, "success")
}

// RestoreDatasetVersion restores a dataset's configuration to a stored version.
// @Summary Restore Dataset Version
// @Description Write a stored configuration back onto the dataset and append the
// @Description restored state as a new version, so the restore itself can be undone.
// @Tags datasets
// @Success 200 {object} datasetpkg.RestoreDatasetVersionResponse
// @Router /api/v1/datasets/{dataset_id}/versions/{version}/restore [post]
func (h *DatasetsHandler) RestoreDatasetVersion(c *gin.Context) {
	datasetID, version, ok := datasetVersionID(c, true)
	if !ok {
		return
	}
	if h.datasetsService == nil {
		common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, "dataset version history is not available")
		return
	}

	var body struct {
		ExpectedVersion *int64 `json:"expected_version"`
		DryRun          bool   `json:"dry_run"`
	}
	// An empty body is a valid restore to the named version; only a malformed
	// one is rejected.
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "invalid request body: "+err.Error())
			return
		}
	}

	ctx := c.Request.Context()
	restoreReq := &datasetpkg.RestoreDatasetVersionRequest{
		DatasetID:       datasetID,
		Version:         version,
		ExpectedVersion: body.ExpectedVersion,
		DryRun:          body.DryRun,
	}

	// A restore returns the dataset to the state a version recorded: its
	// configuration and its content. Putting documents back needs the document
	// service, which lives in its own package, so the whole restore is
	// coordinated there instead of in the dataset service.
	restored, err := datasetcontent.Restore(ctx, restoreReq)
	if err != nil {
		respondDatasetVersionError(c, err)
		return
	}
	common.RespondSuccess(c, restored, "success")
}

// DeleteDatasetVersion removes one version from a dataset's history.
// @Summary Delete Dataset Version
// @Description Drop one stored version. The current version and the baseline
// @Description cannot be deleted; deleting history never rewrites the dataset
// @Description itself.
// @Tags datasets
// @Success 200 {object} datasetpkg.DeleteDatasetVersionResponse
// @Router /api/v1/datasets/{dataset_id}/versions/{version} [delete]
func (h *DatasetsHandler) DeleteDatasetVersion(c *gin.Context) {
	datasetID, version, ok := datasetVersionID(c, true)
	if !ok {
		return
	}
	if h.datasetsService == nil {
		common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, "dataset version history is not available")
		return
	}

	resp, err := h.datasetsService.DeleteDatasetVersion(c.Request.Context(), &datasetpkg.DeleteDatasetVersionRequest{
		DatasetID: datasetID,
		Version:   version,
	})
	if err != nil {
		respondDatasetVersionError(c, err)
		return
	}
	common.RespondSuccess(c, resp, "success")
}
