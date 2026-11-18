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

// CAD preview rendering lives with the CAD chunker, which owns CAD parsing and
// ingestion-time artifact generation.
package chunker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"path/filepath"
	"strings"

	"ragflow/internal/ingestion/component/schema"
	"ragflow/internal/parser/cadsource"

	"github.com/signintech/gopdf"

	cad "github.com/unitedrhino/go-cad"
)

const (
	// cadPreviewPageWidth is the pixel width used for every rendered page.
	cadPreviewPageWidth = 2048
	// cadPreviewMaxPages bounds the page count of one preview PDF.
	cadPreviewMaxPages = 64
	// cadPreviewMaxPixels bounds the pixel count of a single rendered page.
	cadPreviewMaxPixels = 400_000_000

	cadPreviewPDFVersion = "v1"

	cadPreviewMillimetresPerInch = 25.4
	cadPreviewPointsPerInch      = 72.0
)

// cadPreviewResult is the generated preview bundle for one CAD document.
type cadPreviewResult struct {
	PDF       []byte
	Thumbnail []byte
	Warnings  []string
	Pages     []cadPreviewPage
	Omitted   map[cadPreviewPageKey]string
}

// isCADSourceFile reports whether name uses a CAD extension handled by the
// preview renderer.
func isCADSourceFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".dwg", ".dxf", ".dxfb":
		return true
	}
	return false
}

// parseCADDocument parses a DWG or DXF payload into the go-cad document model.
func parseCADDocument(name string, data []byte) (*cad.Document, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".dxf", ".dxfb":
		return cad.ParseDXF(data)
	default:
		return cad.Parse(data)
	}
}

// cadPreviewKey returns the object-storage key of the generated PDF. Keep the
// format in sync with the document preview reader.
func cadPreviewKey(docID string) string {
	return "cad-preview/" + docID + "/preview-" + cadPreviewPDFVersion + ".pdf"
}

// cadThumbnailKey returns the object-storage key of the generated thumbnail.
// Keep the format in sync with the document preview reader.
func cadThumbnailKey(docID string) string {
	return "cad-preview/" + docID + "/thumb.png"
}

// buildCADPreview renders the CAD document into the PDF and thumbnail stored for its
// document row. Each detected sheet or paper layout becomes one PDF page.
func buildCADPreview(ctx context.Context, doc *cad.Document, sheets []cadsource.Sheet, width int) (*cadPreviewResult, error) {
	if doc == nil {
		return nil, fmt.Errorf("CAD document is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if width <= 0 {
		width = cadPreviewPageWidth
	}
	preview, err := renderCADPreviewPages(ctx, doc, sheets, width)
	if err != nil {
		return nil, err
	}
	if len(preview.Pages) == 0 {
		return nil, fmt.Errorf("CAD has no renderable pages: %v", preview.Warnings)
	}
	preview.PDF, err = writeCADPreviewPDF(ctx, preview.Pages)
	if err != nil {
		return nil, err
	}
	preview.Thumbnail = preview.Pages[0].data
	return preview, nil
}

type cadPreviewPageKey struct{ layoutID, sheetID string }

func previewPageKey(layoutID, sheetID string) cadPreviewPageKey {
	if layoutID != "model" {
		sheetID = ""
	}
	return cadPreviewPageKey{layoutID, sheetID}
}

type cadPreviewPage struct {
	data      []byte
	width     int
	height    int
	paperSize [2]float64
	layoutID  string
	sheetID   string
	viewport  [4]float64
}

// renderCADPreviewPages keeps paper layouts and model sheets in their native
// spaces. Every successful render retains the actual viewport used by go-cad.
func renderCADPreviewPages(ctx context.Context, doc *cad.Document, sheets []cadsource.Sheet, width int) (*cadPreviewResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	preview := &cadPreviewResult{Omitted: make(map[cadPreviewPageKey]string)}
	omit := func(key cadPreviewPageKey, reason string) {
		preview.Omitted[key] = reason
		preview.Warnings = append(preview.Warnings, fmt.Sprintf("layout=%q sheet=%q: %s", key.layoutID, key.sheetID, reason))
	}
	appendPage := func(page cadPreviewPage) {
		if page.width <= 0 || page.height <= 0 || page.width*page.height > cadPreviewMaxPixels {
			omit(previewPageKey(page.layoutID, page.sheetID), "page exceeds the pixel budget")
			return
		}
		preview.Pages = append(preview.Pages, page)
	}
	paperLayouts := make(map[string]bool)
	limitReason := fmt.Sprintf("preview truncated to %d pages", cadPreviewMaxPages)
	for _, layout := range cad.Layouts(doc) {
		if layout.Space != cad.SpacePaper || layout.Entities == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		paperLayouts[layout.ID] = true
		key := previewPageKey(layout.ID, "")
		if len(preview.Pages) >= cadPreviewMaxPages {
			omit(key, limitReason)
			continue
		}
		res, err := cad.RenderLayoutPNG(doc, layout.ID, cad.RenderOptions{Width: width})
		if err != nil {
			omit(key, "render failed: "+err.Error())
			continue
		}
		appendPage(cadPreviewPage{
			data: res.Data, width: res.Width, height: res.Height, paperSize: layout.PaperSize,
			layoutID: layout.ID, viewport: res.Viewport,
		})
		preview.Warnings = append(preview.Warnings, res.Warnings...)
	}
	model := *doc
	model.PspaceSpace = nil
	for _, sheet := range sheets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := previewPageKey(sheet.Layout, sheet.ID)
		if sheet.Layout != "model" {
			if !paperLayouts[sheet.Layout] {
				omit(key, "parser layout not found in the renderer")
			}
			continue
		}
		if len(preview.Pages) >= cadPreviewMaxPages {
			omit(key, limitReason)
			continue
		}
		page, err := renderCADModelPreviewPage(&model, sheet, width)
		if err != nil {
			omit(key, "render failed: "+err.Error())
			continue
		}
		appendPage(page)
	}
	return preview, nil
}

func renderCADModelPreviewPage(doc *cad.Document, sheet cadsource.Sheet, width int) (cadPreviewPage, error) {
	page := cadPreviewPage{layoutID: "model", sheetID: sheet.ID, viewport: sheet.Viewport}
	if !cadValidBounds(page.viewport) && cadValidBounds(sheet.Bounds) {
		page.viewport = cad.SheetViewport(sheet.Bounds)
	}
	if !cadValidBounds(page.viewport) {
		if sheet.Source != "fallback" {
			return cadPreviewPage{}, fmt.Errorf("sheet has no valid viewport")
		}
		res, err := cad.RenderLayoutPNG(doc, "model", cad.RenderOptions{Width: width})
		if err != nil {
			return cadPreviewPage{}, err
		}
		page.data, page.width, page.height, page.viewport = res.Data, res.Width, res.Height, res.Viewport
		return page, nil
	}

	// The parser viewport already covers the sheet's glyphs and margin.
	// RenderLayoutBoxPNG would expand it again.
	data, err := cad.RenderSheetPNG(doc, cad.Sheet{Box: sheet.Bounds, Viewport: page.viewport}, cad.RenderOptions{Width: width})
	if err != nil {
		return cadPreviewPage{}, err
	}
	size, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return cadPreviewPage{}, fmt.Errorf("read PNG dimensions: %w", err)
	}
	page.data, page.width, page.height = data, size.Width, size.Height
	return page, nil
}

func writeCADPreviewPDF(ctx context.Context, pages []cadPreviewPage) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("CAD preview has no pages")
	}
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: 842, H: 595}})
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		img, err := png.Decode(bytes.NewReader(page.data))
		if err != nil {
			return nil, fmt.Errorf("decode CAD preview page %d: %w", i+1, err)
		}
		pageW, pageH, rect := cadPreviewPlacement(page)
		pdf.AddPageWithOption(gopdf.PageOption{PageSize: &gopdf.Rect{W: pageW, H: pageH}})
		if err := pdf.ImageFrom(img, rect[0], rect[1], &gopdf.Rect{W: rect[2] - rect[0], H: rect[3] - rect[1]}); err != nil {
			return nil, fmt.Errorf("embed CAD preview page %d: %w", i+1, err)
		}
	}
	var buf bytes.Buffer
	if _, err := pdf.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func cadPreviewPointSize(page cadPreviewPage) (float64, float64) {
	if page.paperSize[0] > 0 && page.paperSize[1] > 0 {
		return page.paperSize[0] * cadPreviewPointsPerInch / cadPreviewMillimetresPerInch,
			page.paperSize[1] * cadPreviewPointsPerInch / cadPreviewMillimetresPerInch
	}
	const widthPt = 842.0 // A3 width in points, used only for the aspect ratio
	if page.width <= 0 || page.height <= 0 {
		return widthPt, widthPt
	}
	return widthPt, widthPt * float64(page.height) / float64(page.width)
}

// cadPreviewPlacement is shared by PDF writing and chunk projection, including
// the centering offset when a raster does not fill its physical paper size.
func cadPreviewPlacement(page cadPreviewPage) (float64, float64, [4]float64) {
	w, h := cadPreviewPointSize(page)
	dw, dh := cadPreviewFit(w, h, float64(page.width), float64(page.height))
	x, y := (w-dw)/2, (h-dh)/2
	return w, h, [4]float64{x, y, x + dw, y + dh}
}

func cadPreviewFit(pageW, pageH, imgW, imgH float64) (float64, float64) {
	if imgW <= 0 || imgH <= 0 || pageW <= 0 || pageH <= 0 {
		return pageW, pageH
	}
	scale := pageW / imgW
	if pageH/imgH < scale {
		scale = pageH / imgH
	}
	return imgW * scale, imgH * scale
}

// cadPreviewPosition projects native CAD coordinates into PDF points measured
// from the top left. The viewport and image placement belong to the same render.
func cadPreviewPosition(page cadPreviewPage, pageNumber int, bbox [4]float64) ([]float64, bool) {
	if !cadValidBounds(page.viewport) || !cadValidBounds(bbox) {
		return nil, false
	}
	v := page.viewport
	b := [4]float64{math.Max(bbox[0], v[0]), math.Max(bbox[1], v[1]), math.Min(bbox[2], v[2]), math.Min(bbox[3], v[3])}
	if !cadValidBounds(b) {
		return nil, false
	}
	_, _, r := cadPreviewPlacement(page)
	if !cadValidBounds(r) {
		return nil, false
	}
	// go-cad uses the width-derived scale on both axes, then rounds raster
	// height. Independent Y normalization would stretch the rendered content.
	scale := (r[2] - r[0]) / (v[2] - v[0])
	left, right := r[0]+(b[0]-v[0])*scale, r[0]+(b[2]-v[0])*scale
	top, bottom := r[1]+(v[3]-b[3])*scale, r[1]+(v[3]-b[1])*scale
	top, bottom = math.Max(r[1], top), math.Min(r[3], bottom)
	if !cadValidBounds([4]float64{left, top, right, bottom}) {
		return nil, false
	}
	// The index stores integer positions. Round outward so a thin annotation
	// still has a nonzero highlight after that conversion.
	return []float64{float64(pageNumber), math.Floor(left), math.Ceil(right), math.Floor(top), math.Ceil(bottom)}, true
}

// attachCADPreviewPositions is called only after the matching PDF was stored.
// Raw CAD provenance stays in extra; positions use the existing PDF protocol.
func attachCADPreviewPositions(chunks []schema.ChunkDoc, preview *cadPreviewResult) []cadDiagnostic {
	var diagnostics []cadDiagnostic
	for i := range chunks {
		chunks[i].Positions = nil
		meta, ok := cadChunkImageMeta(chunks[i])
		if !ok {
			diagnostics = append(diagnostics, cadDiagnostic{Code: "cad_preview_unmapped", Detail: fmt.Sprintf("chunk=%d: missing layout or valid bounds", i+1)})
			continue
		}
		var rows [][]float64
		key := previewPageKey(meta.layoutID, meta.sheetID)
		matched := false
		for n, page := range preview.Pages {
			if previewPageKey(page.layoutID, page.sheetID) != key {
				continue
			}
			matched = true
			row, ok := cadPreviewPosition(page, n+1, meta.bbox)
			if !ok {
				continue
			}
			rows = append(rows, row)
		}
		if len(rows) == 0 {
			reason := preview.Omitted[key]
			if matched {
				reason = "chunk bounds outside the visible page"
			} else if reason == "" {
				reason = "no preview page for layout and sheet"
			}
			detail := fmt.Sprintf("chunk=%d layout=%q sheet=%q: %s", i+1, meta.layoutID, meta.sheetID, reason)
			diagnostics = append(diagnostics, cadDiagnostic{Code: "cad_preview_unmapped", Detail: detail})
			continue
		}
		positions, err := json.Marshal(rows)
		if err != nil {
			diagnostics = append(diagnostics, cadDiagnostic{Code: "cad_preview_metadata_failed", Detail: err.Error()})
			continue
		}
		chunks[i].Positions = positions
	}
	return diagnostics
}
