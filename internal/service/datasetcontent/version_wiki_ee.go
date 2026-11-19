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

package datasetcontent

import (
	"context"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/service"
	"ragflow/internal/service/file"

	"go.uber.org/zap"
)

// restoreWikiPagesAtEE puts every compiled wiki page back to the body it held at
// the version's anchor.
//
// A wiki page is an artifact, not a document: it lives as a wiki_page entry in
// the document engine with no document row behind it, so the chunk replay that
// rebuilds documents cannot reach it. Its own history is the file commit chain,
// which is the source this replay reads - the same shape as the parse-settings
// and metadata replays beside it.
//
// What comes back is the set of pages that existed at the anchor: a page edited
// afterwards gets the body and title it held then, and a page created afterwards
// is left alone. A page whose history only begins after the anchor is recovered
// from the before-side of that first edit - see file.PageContentsAt - because
// such a page did exist and that edit is the one being undone. Removing a page
// the anchor never saw would mean deleting its commit chain too - the history
// that is the only way back to it - so a restore puts back the pages it knows
// about and leaves the rest.
//
// Best effort, like the settings and metadata replays: the documents have
// already moved, and one page that cannot be written must not fail a restore
// that is otherwise complete. Failures are logged, and restoring the same
// version again retries them.
func restoreWikiPagesAtEE(ctx context.Context, tenantID, datasetID string, atMillis int64) error {
	if tenantID == "" || datasetID == "" {
		return nil
	}
	// untilMillis bounds the interval this version describes. A run records its
	// version row when the pipeline reports the parse, and the pages it compiled
	// are merged a moment later; without the bound those pages look as if they
	// appeared after the version and a restore of it brings none of them back.
	pages, err := file.NewFileCommitService().PageContentsUntil(ctx, datasetID, atMillis, versionWindowEnd(ctx, datasetID, atMillis))
	if err != nil {
		return err
	}
	svc := service.NewDatasetArtifactService()
	if len(pages) == 0 {
		// A dataset that has wiki pages but no page history reaching this version
		// cannot have them put back: nothing describes them then. Say so, because a
		// silent restore reads as one that worked.
		if hasWiki, herr := svc.HasWiki(ctx, tenantID, datasetID); herr == nil && hasWiki {
			common.Warn("Content restore: the version holds wiki pages but no page history for them",
				zap.String("dataset_id", datasetID),
				zap.Int64("at", atMillis))
		}
		return nil
	}
	replayed, skipped := 0, 0
	for _, page := range pages {
		if page.Content == "" {
			skipped++
			continue
		}
		if _, err := svc.RestoreWikiPage(ctx, tenantID, datasetID, page.PageType, page.Slug, page.Page); err != nil {
			skipped++
			common.Warn("Content restore: failed to replay a wiki page",
				zap.String("dataset_id", datasetID),
				zap.String("page_type", page.PageType),
				zap.String("slug", page.Slug),
				zap.Error(err))
			continue
		}
		replayed++
	}
	common.Info("Content restore: replayed wiki pages",
		zap.String("dataset_id", datasetID),
		zap.Int64("at", atMillis),
		zap.Int("replayed", replayed),
		zap.Int("skipped", skipped))
	return nil
}

// versionWindowEnd returns the anchor of the version that follows atMillis, or 0
// when atMillis is the head of the history.
//
// It bounds the interval a version describes: a version written before the last of
// the rows its run produces (a parse version recorded at the end of the run, the
// pages compiled after it) is followed by those rows, not preceded by them. Read
// strictly, that version would describe a dataset without its own output.
func versionWindowEnd(ctx context.Context, datasetID string, atMillis int64) int64 {
	if dao.DB == nil || datasetID == "" || atMillis <= 0 {
		return 0
	}
	var next entity.DatasetVersion
	if err := dao.DB.WithContext(ctx).
		Select("create_time").
		Where("dataset_id = ? AND create_time > ?", datasetID, atMillis).
		Order("create_time ASC").
		Limit(1).
		Take(&next).Error; err != nil {
		return 0
	}
	if next.CreateTime == nil {
		return 0
	}
	return *next.CreateTime
}
