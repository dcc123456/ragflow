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

package chunker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	cad "github.com/unitedrhino/go-cad"
	"gorm.io/gorm"

	"ragflow/internal/agent/runtime"
	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/component"
	"ragflow/internal/ingestion/component/schema"
	"ragflow/internal/storage"
)

const (
	cadChunkImageWidth      = 1200
	cadChunkImageMaxSide    = 4096
	cadChunkImageMaxPerDoc  = 200
	cadChunkImageMaxPixels  = 60_000_000
	cadChunkImageDataPrefix = "data:image/png;base64,"
)

// loadCADDocument fetches and parses the source binary once. It is called
// before chunking so the sheet ink can drive cut boundaries.
func loadCADDocument(ctx context.Context, db *gorm.DB, upstream schema.ChunkerFromUpstream) (*cad.Document, string, error) {
	data, name, err := fetchCADSource(ctx, db, upstream)
	if err != nil {
		return nil, name, err
	}
	if len(data) == 0 || !isCADSourceFile(sourceCADName(name, upstream)) {
		return nil, name, nil
	}
	doc, err := parseCADDocument(sourceCADName(name, upstream), data)
	if err != nil {
		return nil, name, err
	}
	return doc, name, nil
}

// attachCADArtifacts renders chunk images and stores the preview that was
// already rendered for ink detection.
func attachCADArtifacts(ctx context.Context, db *gorm.DB, inputs map[string]any, upstream schema.ChunkerFromUpstream, out map[string]any, chunks []schema.ChunkDoc, docCAD *cad.Document, preview *cadPreviewResult) (map[string]any, error) {
	if docCAD == nil {
		return out, nil
	}
	if err := renderCADChunkImages(ctx, docCAD, chunks); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		common.Warn("CADChunker: render chunk images: " + err.Error())
	}
	out["chunks"] = chunks
	if preview == nil {
		return out, nil
	}
	if err := storeCADPreviewArtifacts(ctx, db, inputs, upstream, preview, chunks); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		common.Warn("CADChunker: store preview: " + err.Error())
	}
	return out, nil
}

func sourceCADName(name string, upstream schema.ChunkerFromUpstream) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	if fileType := strings.TrimSpace(strings.ToLower(upstream.FileType)); fileType != "" {
		return "document." + strings.TrimPrefix(fileType, ".")
	}
	return ""
}

func renderCADChunkImages(ctx context.Context, doc *cad.Document, chunks []schema.ChunkDoc) error {
	rendered := 0
	for i := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}
		ck := &chunks[i]
		if ck.Image != "" || ck.ImgID != "" || strings.TrimSpace(ck.Text) == "" {
			continue
		}
		meta, ok := cadChunkImageMeta(*ck)
		if !ok {
			continue
		}
		image, err := renderCADChunkImage(doc, meta)
		if err != nil || image == "" {
			continue
		}
		ck.Image = image
		rendered++
		if rendered >= cadChunkImageMaxPerDoc {
			break
		}
	}
	return nil
}

func renderCADChunkImage(doc *cad.Document, meta cadChunkImageProvenance) (string, error) {
	width := cadChunkImageWidth
	boxW, boxH := meta.bbox[2]-meta.bbox[0], meta.bbox[3]-meta.bbox[1]
	if boxW <= 0 || boxH <= 0 {
		return "", nil
	}
	// The crop is one complete block; keep it whole and clamp its longest side.
	// VLM tiling, when needed, happens on this image without changing the chunk.
	if height := float64(width) * boxH / boxW; height > cadChunkImageMaxSide {
		width = int(math.Round(cadChunkImageMaxSide * boxW / boxH))
	}
	if width < 1 {
		return "", nil
	}
	if h := float64(width) * boxH / boxW; float64(width)*h > cadChunkImageMaxPixels {
		width = int(math.Sqrt(cadChunkImageMaxPixels * boxW / boxH))
	}
	if width < 1 {
		return "", nil
	}
	res, err := cad.RenderLayoutBoxPNG(doc, meta.layoutID, meta.bbox, cad.RenderOptions{Width: width})
	if err != nil || len(res.Data) == 0 {
		return "", err
	}
	return cadChunkImageDataPrefix + base64.StdEncoding.EncodeToString(res.Data), nil
}

// storeCADPreviewArtifacts persists the preview that was rendered before
// chunking. Storage is best-effort: a failure or panic must not fail the
// ingestion task that already produced the chunks.
func storeCADPreviewArtifacts(ctx context.Context, db *gorm.DB, inputs map[string]any, upstream schema.ChunkerFromUpstream, result *cadPreviewResult, chunks []schema.ChunkDoc) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("CAD preview panic: %v", recovered)
		}
	}()
	if result == nil {
		return nil
	}
	kbID, docID := resolveImageUploadContext(ctx, inputs)
	if docID == "" {
		docID = strings.TrimSpace(upstream.DocID)
	}
	if db != nil && docID != "" && kbID == "" {
		var doc entity.Document
		if err := db.WithContext(ctx).Select("id", "kb_id").Where("id = ?", docID).Take(&doc).Error; err == nil {
			kbID = doc.KbID
		}
	}
	if kbID == "" || docID == "" {
		return fmt.Errorf("missing kb_id/doc_id for preview")
	}
	store := storage.GetStorageFactory().GetStorage()
	if store == nil {
		return fmt.Errorf("storage not initialized")
	}
	for _, warning := range result.Warnings {
		common.Warn("CAD preview: " + warning)
	}
	previewKey := cadPreviewKey(docID)
	thumbKey := cadThumbnailKey(docID)
	if err := store.Put(ctx, kbID, previewKey, result.PDF); err != nil {
		return fmt.Errorf("store preview PDF: %w", err)
	}
	for _, diagnostic := range attachCADPreviewPositions(chunks, result) {
		runtime.ReportProgressMessage(ctx, "CADChunker", cadDiagnosticMessage(diagnostic))
	}
	if len(result.Thumbnail) > 0 {
		if err := store.Put(ctx, kbID, thumbKey, result.Thumbnail); err != nil {
			return fmt.Errorf("store preview thumbnail: %w", err)
		}
	}
	if db != nil {
		if err := db.WithContext(ctx).Model(&entity.Document{}).Where("id = ?", docID).Update("thumbnail", thumbKey).Error; err != nil {
			return fmt.Errorf("persist preview thumbnail: %w", err)
		}
	}
	return nil
}

func fetchCADSource(ctx context.Context, db *gorm.DB, upstream schema.ChunkerFromUpstream) ([]byte, string, error) {
	name := strings.TrimSpace(upstream.Name)
	if name == "" && upstream.File != nil {
		name = strings.TrimSpace(upstream.File.Name)
	}
	switch {
	case upstream.Bucket != "" && upstream.Path != "":
		data, err := component.FetchBinary(ctx, upstream.Bucket, upstream.Path)
		return data, name, err
	case upstream.DocID != "":
		ref, err := component.ResolveDocumentStorage(ctx, db, upstream.DocID)
		if err != nil {
			return nil, name, err
		}
		if ref == nil {
			return nil, name, nil
		}
		if name == "" {
			name = ref.Name
		}
		data, err := component.FetchBinary(ctx, ref.Bucket, ref.Path)
		return data, name, err
	default:
		return nil, name, nil
	}
}

type cadChunkImageProvenance struct {
	layoutID string
	sheetID  string
	bbox     [4]float64
}

func cadChunkImageMeta(ck schema.ChunkDoc) (cadChunkImageProvenance, bool) {
	raw, ok := ck.Extra["extra"]
	if !ok || len(raw) == 0 {
		return cadChunkImageProvenance{}, false
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return cadChunkImageProvenance{}, false
	}
	var payload struct {
		CAD struct {
			LayoutID string     `json:"layout_id"`
			SheetID  string     `json:"sheet_id"`
			BBox     [4]float64 `json:"bbox"`
		} `json:"cad"`
	}
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		return cadChunkImageProvenance{}, false
	}
	meta := cadChunkImageProvenance{
		layoutID: strings.TrimSpace(payload.CAD.LayoutID),
		sheetID:  payload.CAD.SheetID,
		bbox:     payload.CAD.BBox,
	}
	if meta.layoutID == "" || !cadValidBounds(meta.bbox) {
		return cadChunkImageProvenance{}, false
	}
	return meta, true
}
