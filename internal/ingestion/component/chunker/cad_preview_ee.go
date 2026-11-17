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
	"fmt"
	"image"
	"image/png"
	"path/filepath"
	"strings"

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
func buildCADPreview(ctx context.Context, doc *cad.Document, width int) (*cadPreviewResult, error) {
	if doc == nil {
		return nil, fmt.Errorf("CAD document is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if width <= 0 {
		width = cadPreviewPageWidth
	}
	pages, warnings, err := renderCADPreviewPages(ctx, doc, width)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("CAD has no renderable pages")
	}
	pdf, err := writeCADPreviewPDF(ctx, pages)
	if err != nil {
		return nil, err
	}
	return &cadPreviewResult{
		PDF:       pdf,
		Thumbnail: pages[0].data,
		Warnings:  warnings,
	}, nil
}

type cadPreviewPage struct {
	data      []byte
	width     int
	height    int
	paperSize [2]float64
}

// renderCADPreviewPages chooses the stable page set for a CAD document. Paper layouts
// with real content win; otherwise model-space detected sheets are rendered one
// page each; a single-model drawing remains one page.
func renderCADPreviewPages(ctx context.Context, doc *cad.Document, width int) ([]cadPreviewPage, []string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	layouts := cad.Layouts(doc)
	var paper []cad.Layout
	for _, layout := range layouts {
		if layout.Space == cad.SpacePaper && layout.Entities > 8 {
			paper = append(paper, layout)
		}
	}
	var pages []cadPreviewPage
	var warnings []string
	if len(paper) > 0 {
		for _, layout := range paper {
			if len(pages) >= cadPreviewMaxPages {
				warnings = append(warnings, fmt.Sprintf("preview truncated to %d pages", cadPreviewMaxPages))
				break
			}
			if err := ctx.Err(); err != nil {
				return nil, warnings, err
			}
			res, err := cad.RenderLayoutPNG(doc, layout.ID, cad.RenderOptions{Width: width})
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("layout %s: %v", layout.ID, err))
				continue
			}
			pages = append(pages, cadPreviewPage{
				data: res.Data, width: res.Width, height: res.Height, paperSize: layout.PaperSize,
			})
			warnings = append(warnings, res.Warnings...)
		}
	}
	if len(pages) == 0 {
		det := cad.DetectSheetsDetailed(doc)
		if !det.Fallback && len(det.Sheets) > 0 {
			for _, sheet := range det.Sheets {
				if len(pages) >= cadPreviewMaxPages {
					warnings = append(warnings, fmt.Sprintf("preview truncated to %d pages", cadPreviewMaxPages))
					break
				}
				if err := ctx.Err(); err != nil {
					return nil, warnings, err
				}
				res, err := cad.RenderLayoutBoxPNG(doc, "model", sheet.Box, cad.RenderOptions{Width: width})
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("sheet %s: %v", sheet.Name, err))
					continue
				}
				pages = append(pages, cadPreviewPage{
					data: res.Data, width: res.Width, height: res.Height,
				})
				warnings = append(warnings, res.Warnings...)
			}
		}
	}
	if len(pages) == 0 {
		if err := ctx.Err(); err != nil {
			return nil, warnings, err
		}
		res, err := cad.RenderLayoutPNG(doc, "model", cad.RenderOptions{Width: width})
		if err != nil {
			return nil, warnings, err
		}
		pages = append(pages, cadPreviewPage{
			data: res.Data, width: res.Width, height: res.Height,
		})
		warnings = append(warnings, res.Warnings...)
	}
	for i := range pages {
		if pages[i].width <= 0 || pages[i].height <= 0 || pages[i].width*pages[i].height > cadPreviewMaxPixels {
			return nil, warnings, fmt.Errorf("CAD preview page %d exceeds the pixel budget", i+1)
		}
	}
	return pages, warnings, nil
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
		pageW, pageH := cadPreviewPointSize(page, img)
		pdf.AddPageWithOption(gopdf.PageOption{PageSize: &gopdf.Rect{W: pageW, H: pageH}})
		b := img.Bounds()
		drawW, drawH := cadPreviewFit(pageW, pageH, float64(b.Dx()), float64(b.Dy()))
		x, y := (pageW-drawW)/2, (pageH-drawH)/2
		if err := pdf.ImageFrom(img, x, y, &gopdf.Rect{W: drawW, H: drawH}); err != nil {
			return nil, fmt.Errorf("embed CAD preview page %d: %w", i+1, err)
		}
	}
	var buf bytes.Buffer
	if _, err := pdf.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func cadPreviewPointSize(page cadPreviewPage, img image.Image) (float64, float64) {
	if page.paperSize[0] > 0 && page.paperSize[1] > 0 {
		return page.paperSize[0] * cadPreviewPointsPerInch / cadPreviewMillimetresPerInch,
			page.paperSize[1] * cadPreviewPointsPerInch / cadPreviewMillimetresPerInch
	}
	const widthPt = 842.0 // A3 width in points, used only for the aspect ratio
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return widthPt, widthPt
	}
	return widthPt, widthPt * float64(b.Dy()) / float64(b.Dx())
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
