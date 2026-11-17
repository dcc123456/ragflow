//go:build ee

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

package document

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/storage"
)

// preparePreviewPresentationEE serves the CAD preview PDF generated during
// ingestion. It never parses or renders CAD in the API server.
func preparePreviewPresentationEE(ctx context.Context, doc *entity.Document, fileName string) ([]byte, string, bool, error) {
	if doc == nil {
		return nil, "", false, nil
	}
	name := fileName
	if strings.TrimSpace(name) == "" {
		name = cadDocumentName(doc)
	}
	if !isCADPreviewFile(name) {
		return nil, "", false, nil
	}
	store := storage.GetStorageFactory().GetStorage()
	if store == nil {
		return nil, "", true, fmt.Errorf("storage not initialized")
	}
	pdf, err := store.Get(ctx, doc.KbID, cadPreviewObjectKey(doc.ID))
	if err != nil {
		if storage.IsNotFound(err) {
			return nil, "", true, ErrPreviewNotReady
		}
		return nil, "", true, fmt.Errorf("load CAD preview: %w", err)
	}
	if len(pdf) == 0 {
		return nil, "", true, ErrPreviewNotReady
	}
	return pdf, "application/pdf", true, nil
}

// cleanupPreviewArtifactsEE removes the doc-scoped preview artifacts during
// document deletion or a re-parse. It intentionally keeps the object keys
// doc-scoped so one document never removes another document's preview.
func cleanupPreviewArtifactsEE(ctx context.Context, doc *entity.Document) {
	if doc == nil || !isCADPreviewFile(cadDocumentName(doc)) {
		return
	}
	store := storage.GetStorageFactory().GetStorage()
	if store == nil {
		common.Warn("cleanup CAD preview: storage not initialized")
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for _, key := range []string{cadPreviewObjectKey(doc.ID), cadThumbnailObjectKey(doc.ID)} {
		if err := store.Remove(cleanupCtx, doc.KbID, key); err != nil && !storage.IsNotFound(err) {
			common.Warn(fmt.Sprintf("cleanup CAD preview %s/%s: %v", doc.KbID, key, err))
		}
	}
	if dao.DB != nil && doc.Thumbnail != nil && strings.HasPrefix(*doc.Thumbnail, cadPreviewPrefix(doc.ID)) {
		if err := dao.DB.Model(&entity.Document{}).Where("id = ?", doc.ID).Update("thumbnail", nil).Error; err != nil {
			common.Warn("clear CAD thumbnail: " + err.Error())
		}
	}
}

func cadDocumentName(doc *entity.Document) string {
	if doc == nil {
		return ""
	}
	if doc.Name != nil && strings.TrimSpace(*doc.Name) != "" {
		return *doc.Name
	}
	if doc.Suffix != "" {
		return "document." + strings.TrimPrefix(strings.ToLower(doc.Suffix), ".")
	}
	return ""
}

func cadPreviewPrefix(docID string) string {
	return "cad-preview/" + docID + "/"
}

func isCADPreviewFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".dwg", ".dxf", ".dxfb":
		return true
	}
	return false
}

// cadPreviewObjectKey and cadThumbnailObjectKey are the storage contract shared
// with the CAD ingestion decorator.
func cadPreviewObjectKey(docID string) string {
	return "cad-preview/" + docID + "/preview-v1.pdf"
}

func cadThumbnailObjectKey(docID string) string {
	return "cad-preview/" + docID + "/thumb.png"
}
