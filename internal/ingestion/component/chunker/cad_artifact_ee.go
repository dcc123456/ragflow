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
	cadChunkImageMaxPerDoc  = 200
	cadChunkImageMaxPixels  = 60_000_000
	cadChunkImageDataPrefix = "data:image/png;base64,"
)

// cadArtifactDecorator attaches a rendered B image to every chunk that still
// has stable layout provenance, and generates the document-level preview PDF
// and thumbnail during ingestion. The outer imageUploadDecorator uploads chunk
// images through the existing img_id contract, so no new image route is
// introduced.
type cadArtifactDecorator struct {
	inner runtime.Component
}

func (d *cadArtifactDecorator) Invoke(ctx context.Context, db *gorm.DB, inputs map[string]any) (map[string]any, error) {
	out, err := d.inner.Invoke(ctx, db, inputs)
	if err != nil || out == nil {
		return out, err
	}
	// The chunker reports input failures through _ERROR. The outer
	// imageUploadDecorator promotes it to an error; skip artifact generation
	// here so a failed chunker does not pay for CAD parsing or rendering.
	if msg, _ := out["_ERROR"].(string); strings.TrimSpace(msg) != "" {
		return out, nil
	}
	upstream, err := decodeChunkerFromUpstream(inputs)
	if err != nil {
		common.Warn("CADChunker: decode image upstream: " + err.Error())
		return out, nil
	}
	data, name, err := fetchCADSource(ctx, db, upstream)
	if err != nil {
		common.Warn("CADChunker: fetch source: " + err.Error())
		return out, nil
	}
	if len(data) == 0 || !isCADSourceFile(sourceCADName(name, upstream)) {
		return out, nil
	}
	docCAD, err := parseCADDocument(sourceCADName(name, upstream), data)
	if err != nil {
		common.Warn("CADChunker: parse source: " + err.Error())
		return out, nil
	}
	chunks, hasChunks := schema.GetChunks(out)
	if hasChunks && len(chunks) > 0 {
		if err := renderCADChunkImages(ctx, docCAD, chunks); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			common.Warn("CADChunker: render chunk images: " + err.Error())
		}
		out["chunks"] = chunks
	}
	if err := generateCADPreviewArtifacts(ctx, db, inputs, upstream, docCAD); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		common.Warn("CADChunker: generate preview: " + err.Error())
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
	if h := float64(width) * boxH / boxW; float64(width)*h > cadChunkImageMaxPixels {
		width = int(math.Sqrt(cadChunkImageMaxPixels * boxW / boxH))
		if width < 64 {
			return "", nil
		}
	}
	res, err := cad.RenderLayoutBoxPNG(doc, meta.layoutID, meta.bbox, cad.RenderOptions{Width: width})
	if err != nil || len(res.Data) == 0 {
		return "", err
	}
	return cadChunkImageDataPrefix + base64.StdEncoding.EncodeToString(res.Data), nil
}

// generateCADPreviewArtifacts writes the document-scoped preview PDF and
// thumbnail during ingestion, before the document is marked complete. Preview
// rendering is best-effort: a failure or panic must not fail the ingestion
// task that already produced the chunks.
func generateCADPreviewArtifacts(ctx context.Context, db *gorm.DB, inputs map[string]any, upstream schema.ChunkerFromUpstream, docCAD *cad.Document) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("CAD preview panic: %v", recovered)
		}
	}()
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
	result, err := buildCADPreview(ctx, docCAD, cadPreviewPageWidth)
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		common.Warn("CAD preview: " + warning)
	}
	previewKey := cadPreviewKey(docID)
	thumbKey := cadThumbnailKey(docID)
	if err := store.Put(ctx, kbID, previewKey, result.PDF); err != nil {
		return fmt.Errorf("store preview PDF: %w", err)
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
			BBox     [4]float64 `json:"bbox"`
		} `json:"cad"`
	}
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		return cadChunkImageProvenance{}, false
	}
	meta := cadChunkImageProvenance{
		layoutID: strings.TrimSpace(payload.CAD.LayoutID),
		bbox:     payload.CAD.BBox,
	}
	if meta.layoutID == "" || meta.bbox[2] <= meta.bbox[0] || meta.bbox[3] <= meta.bbox[1] {
		return cadChunkImageProvenance{}, false
	}
	return meta, true
}
