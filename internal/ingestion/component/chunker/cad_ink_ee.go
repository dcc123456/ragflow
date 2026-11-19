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
	"bytes"
	"image"
	"image/png"
	"math"

	xdraw "golang.org/x/image/draw"

	"ragflow/internal/parser/cadsource"
)

const (
	// cadInkMaskMaxWidth bounds the raster used for whitespace detection. A
	// lower resolution keeps the mask cheap while still exposing real gutters;
	// text-legible rendering stays with the chunk image path.
	cadInkMaskMaxWidth = 1024
	// cadInkThreshold treats near-white anti-aliased pixels as background.
	cadInkThreshold = 245
	// cadInkMinGutterPx is the smallest all-white band a cut may use.
	cadInkMinGutterPx = 4
	// cadInkFrameInsetRatio excludes the sheet frame and border grid from the
	// occupancy used for internal cuts.
	cadInkFrameInsetRatio = 0.02
	// cadInkMaxCutDepth prevents pathological recursion.
	cadInkMaxCutDepth = 32
)

type cadInkKey struct {
	LayoutID string
	SheetID  string
}

// cadInkPage is one rendered sheet reduced to an ink occupancy prefix sum.
// Coordinates in this type are raster pixels; world conversions go through the
// renderer viewport recorded on the page.
type cadInkPage struct {
	key      cadInkKey
	viewport [4]float64
	width    int
	height   int
	prefix   []uint32
}

// cadInkIndex maps a sheet to the rendered page that owns its cutting mask.
type cadInkIndex struct {
	pages         map[cadInkKey]*cadInkPage
	modelBySheet  map[string]*cadInkPage
	modelFallback *cadInkPage
}

type cadInkRun struct {
	start int
	end   int
	width int
}

// cadInkPart is one whitespace-delimited block: the texts assigned to it and
// the world-space region whose ink belongs to the same block.
type cadInkPart struct {
	boxes  []cadTextBox
	region cadBounds
}

// buildCADInk turns rendered preview pages into cut masks. Preview rendering is
// already part of ingestion; reusing it avoids a second sheet render.
func buildCADInk(pages []cadPreviewPage) *cadInkIndex {
	index := &cadInkIndex{
		pages:        make(map[cadInkKey]*cadInkPage, len(pages)),
		modelBySheet: make(map[string]*cadInkPage, len(pages)),
	}
	for _, page := range pages {
		ink := buildCADInkPage(page)
		if ink == nil {
			continue
		}
		index.pages[ink.key] = ink
		if ink.key.LayoutID == "model" {
			if ink.key.SheetID != "" {
				index.modelBySheet[ink.key.SheetID] = ink
			} else {
				index.modelFallback = ink
			}
		}
	}
	if len(index.pages) == 0 {
		return nil
	}
	return index
}

func buildCADInkPage(page cadPreviewPage) *cadInkPage {
	if len(page.data) == 0 || !cadValidBounds(page.viewport) || page.width <= 0 || page.height <= 0 {
		return nil
	}
	src, err := png.Decode(bytes.NewReader(page.data))
	if err != nil {
		return nil
	}
	width := page.width
	height := page.height
	if width > cadInkMaskMaxWidth {
		height = int(math.Round(float64(height) * cadInkMaskMaxWidth / float64(width)))
		width = cadInkMaskMaxWidth
	}
	if width <= 0 || height <= 0 {
		return nil
	}
	gray := image.NewGray(image.Rect(0, 0, width, height))
	xdraw.ApproxBiLinear.Scale(gray, gray.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	prefix := make([]uint32, (width+1)*(height+1))
	row := width + 1
	for y := 0; y < height; y++ {
		var rowSum uint32
		for x := 0; x < width; x++ {
			if gray.GrayAt(x, y).Y < cadInkThreshold {
				rowSum++
			}
			prefix[(y+1)*row+x+1] = prefix[y*row+x+1] + rowSum
		}
	}
	return &cadInkPage{
		key:      cadInkKey{LayoutID: page.layoutID, SheetID: page.sheetID},
		viewport: page.viewport,
		width:    width,
		height:   height,
		prefix:   prefix,
	}
}

func (p *cadInkPage) inkCount(x0, y0, x1, y1 int) uint32 {
	x0, y0, x1, y1 = p.clampRect(x0, y0, x1, y1)
	if x0 >= x1 || y0 >= y1 {
		return 0
	}
	row := p.width + 1
	return p.prefix[y1*row+x1] - p.prefix[y0*row+x1] - p.prefix[y1*row+x0] + p.prefix[y0*row+x0]
}

func (p *cadInkPage) clampRect(x0, y0, x1, y1 int) (int, int, int, int) {
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > p.width {
		x1 = p.width
	}
	if y1 > p.height {
		y1 = p.height
	}
	if x1 < x0 {
		x1 = x0
	}
	if y1 < y0 {
		y1 = y0
	}
	return x0, y0, x1, y1
}

// contentRegion is the drawable interior. The frame and border grid sit on the
// sheet edge and must not block every internal cut.
func (p *cadInkPage) contentRegion() cadBounds {
	v := p.viewport
	dx := (v[2] - v[0]) * cadInkFrameInsetRatio
	dy := (v[3] - v[1]) * cadInkFrameInsetRatio
	region := cadBounds{minX: v[0] + dx, minY: v[1] + dy, maxX: v[2] - dx, maxY: v[3] - dy}
	if !region.valid() {
		return cadBounds{minX: v[0], minY: v[1], maxX: v[2], maxY: v[3]}
	}
	return region
}

func (p *cadInkPage) worldToPixelRect(region cadBounds) (int, int, int, int, bool) {
	if !region.valid() || !cadValidBounds(p.viewport) {
		return 0, 0, 0, 0, false
	}
	v := p.viewport
	x0 := int(math.Floor((region.minX - v[0]) / (v[2] - v[0]) * float64(p.width)))
	x1 := int(math.Ceil((region.maxX - v[0]) / (v[2] - v[0]) * float64(p.width)))
	y0 := int(math.Floor((v[3] - region.maxY) / (v[3] - v[1]) * float64(p.height)))
	y1 := int(math.Ceil((v[3] - region.minY) / (v[3] - v[1]) * float64(p.height)))
	x0, y0, x1, y1 = p.clampRect(x0, y0, x1, y1)
	if x0 >= x1 || y0 >= y1 {
		return 0, 0, 0, 0, false
	}
	return x0, y0, x1, y1, true
}

func (p *cadInkPage) pixelXToWorld(x int) float64 {
	v := p.viewport
	return v[0] + float64(x)/float64(p.width)*(v[2]-v[0])
}

func (p *cadInkPage) pixelYToWorld(y int) float64 {
	v := p.viewport
	return v[3] - float64(y)/float64(p.height)*(v[3]-v[1])
}

func (p *cadInkPage) blankColumnRuns(x0, y0, x1, y1, minWidth int) []cadInkRun {
	x0, y0, x1, y1 = p.clampRect(x0, y0, x1, y1)
	var runs []cadInkRun
	start := -1
	for x := x0; x < x1; x++ {
		if p.inkCount(x, y0, x+1, y1) == 0 {
			if start < 0 {
				start = x
			}
			continue
		}
		if start >= 0 {
			if x-start >= minWidth {
				runs = append(runs, cadInkRun{start: start, end: x, width: x - start})
			}
			start = -1
		}
	}
	if start >= 0 && x1-start >= minWidth {
		runs = append(runs, cadInkRun{start: start, end: x1, width: x1 - start})
	}
	return runs
}

func (p *cadInkPage) blankRowRuns(x0, y0, x1, y1, minHeight int) []cadInkRun {
	x0, y0, x1, y1 = p.clampRect(x0, y0, x1, y1)
	var runs []cadInkRun
	start := -1
	for y := y0; y < y1; y++ {
		if p.inkCount(x0, y, x1, y+1) == 0 {
			if start < 0 {
				start = y
			}
			continue
		}
		if start >= 0 {
			if y-start >= minHeight {
				runs = append(runs, cadInkRun{start: start, end: y, width: y - start})
			}
			start = -1
		}
	}
	if start >= 0 && y1-start >= minHeight {
		runs = append(runs, cadInkRun{start: start, end: y1, width: y1 - start})
	}
	return runs
}

// inkBounds returns the world-space bounds of ink inside region.
func (p *cadInkPage) inkBounds(region cadBounds) ([4]float64, bool) {
	x0, y0, x1, y1, ok := p.worldToPixelRect(region)
	if !ok {
		return [4]float64{}, false
	}
	minX, minY := x1, y1
	maxX, maxY := x0, y0
	found := false
	for x := x0; x < x1; x++ {
		if p.inkCount(x, y0, x+1, y1) == 0 {
			continue
		}
		found = true
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
	}
	if !found {
		return [4]float64{}, false
	}
	for y := y0; y < y1; y++ {
		if p.inkCount(minX, y, maxX+1, y+1) == 0 {
			continue
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}
	return [4]float64{
		p.pixelXToWorld(minX),
		p.pixelYToWorld(maxY + 1),
		p.pixelXToWorld(maxX + 1),
		p.pixelYToWorld(minY),
	}, true
}

func (idx *cadInkIndex) pageForSheet(sheet cadsource.Sheet) *cadInkPage {
	if idx == nil {
		return nil
	}
	layout := sheet.Layout
	id := sheetRef(sheet)
	if layout != "" && layout != "model" {
		if page, ok := idx.pages[cadInkKey{LayoutID: layout, SheetID: id}]; ok {
			return page
		}
		if page, ok := idx.pages[cadInkKey{LayoutID: layout}]; ok {
			return page
		}
		return nil
	}
	if page, ok := idx.modelBySheet[id]; ok {
		return page
	}
	if len(idx.modelBySheet) == 0 && idx.modelFallback != nil {
		return idx.modelFallback
	}
	return nil
}

// splitCADInkGutters recursively cuts a sheet's text boxes at all-white bands.
// It never cuts when no band is wide enough; callers keep the whole block.
func splitCADInkGutters(sheet cadsource.Sheet, items []cadTextItem, scale float64, page *cadInkPage) []cadInkPart {
	if page == nil {
		return []cadInkPart{{boxes: cadItemsToBoxes(items, scale), region: cadBoundsForSheet(sheet, cadItemsToBoxes(items, scale))}}
	}
	boxes := cadItemsToBoxes(items, scale)
	if len(boxes) == 0 {
		return nil
	}
	region := page.contentRegion()
	return splitCADInkRegion(boxes, region, page, 0)
}

func cadItemsToBoxes(items []cadTextItem, scale float64) []cadTextBox {
	boxes := make([]cadTextBox, len(items))
	for i, item := range items {
		boxes[i] = cadTextBoxFor(item, scale)
	}
	return boxes
}

func splitCADInkRegion(boxes []cadTextBox, region cadBounds, page *cadInkPage, depth int) []cadInkPart {
	if len(boxes) <= 1 || depth >= cadInkMaxCutDepth {
		return []cadInkPart{{boxes: boxes, region: region}}
	}
	x0, y0, x1, y1, ok := page.worldToPixelRect(region)
	if !ok {
		return []cadInkPart{{boxes: boxes, region: region}}
	}
	cols := page.blankColumnRuns(x0, y0, x1, y1, cadInkMinGutterPx)
	rows := page.blankRowRuns(x0, y0, x1, y1, cadInkMinGutterPx)
	vertical := true
	best := cadInkRun{}
	for _, run := range cols {
		if run.width > best.width {
			best = run
		}
	}
	for _, run := range rows {
		if run.width > best.width {
			best = run
			vertical = false
		}
	}
	if best.width == 0 {
		return []cadInkPart{{boxes: boxes, region: region}}
	}
	var at float64
	if vertical {
		at = page.pixelXToWorld((best.start + best.end) / 2)
	} else {
		at = page.pixelYToWorld((best.start + best.end) / 2)
	}
	lower, upper := cadSplitAt(boxes, vertical, at)
	if len(lower) == 0 || len(upper) == 0 {
		return []cadInkPart{{boxes: boxes, region: region}}
	}
	leftRegion, rightRegion := region, region
	if vertical {
		leftRegion.maxX = at
		rightRegion.minX = at
	} else {
		leftRegion.maxY = at
		rightRegion.minY = at
	}
	parts := splitCADInkRegion(lower, leftRegion, page, depth+1)
	parts = append(parts, splitCADInkRegion(upper, rightRegion, page, depth+1)...)
	return parts
}

// mergeCADLeafParts folds isolated labels back into the nearest complete block.
// It never merges two large blocks; gutters between them are real boundaries.
func mergeCADLeafParts(parts []cadInkPart, page *cadInkPage, sheet cadsource.Sheet, scale float64) []cadInkPart {
	if len(parts) < 2 || page == nil {
		return parts
	}
	sheetW := sheet.Bounds[2] - sheet.Bounds[0]
	sheetH := sheet.Bounds[3] - sheet.Bounds[1]
	maxGap := math.Max(20*scale, 0.05*math.Max(sheetW, sheetH))
	large := 0
	for _, part := range parts {
		if len(part.boxes) >= 3 {
			large++
		}
	}
	if large == 1 {
		// A single complete view on the sheet leaves no ambiguity: every
		// remaining label belongs to it, even when the gutter is wide.
		maxGap = math.Inf(1)
	} else if large > 1 {
		maxGap = math.Max(maxGap, 0.5*math.Max(sheetW, sheetH))
	}
	for {
		changed := false
		for i, part := range parts {
			if len(part.boxes) > 2 {
				continue
			}
			best, bestGap := -1, math.Inf(1)
			for j, other := range parts {
				if i == j || len(other.boxes) < 3 {
					continue
				}
				if gap := cadPartsGap(part.boxes, other.boxes); gap < bestGap {
					best, bestGap = j, gap
				}
			}
			if best < 0 || bestGap > maxGap {
				continue
			}
			parts[best].boxes = append(parts[best].boxes, part.boxes...)
			parts[best].region = unionCADBounds(parts[best].region, part.region)
			parts = append(parts[:i], parts[i+1:]...)
			changed = true
			break
		}
		if !changed {
			break
		}
	}
	return parts
}

func unionCADBounds(a, b cadBounds) cadBounds {
	if !a.valid() {
		return b
	}
	if !b.valid() {
		return a
	}
	return cadBounds{
		minX: math.Min(a.minX, b.minX),
		minY: math.Min(a.minY, b.minY),
		maxX: math.Max(a.maxX, b.maxX),
		maxY: math.Max(a.maxY, b.maxY),
	}
}

// cadInkChunkBounds binds the chunk crop to text plus the block's ink, with a
// margin so neither glyphs nor strokes are clipped.
func cadInkChunkBounds(part cadInkPart, page *cadInkPage, scale float64) [4]float64 {
	textBounds := cadBoxesBounds(part.boxes)
	bounds := [4]float64{textBounds.minX, textBounds.minY, textBounds.maxX, textBounds.maxY}
	if page != nil {
		if ink, ok := page.inkBounds(part.region); ok {
			bounds[0] = math.Min(bounds[0], ink[0])
			bounds[1] = math.Min(bounds[1], ink[1])
			bounds[2] = math.Max(bounds[2], ink[2])
			bounds[3] = math.Max(bounds[3], ink[3])
		}
	}
	pad := math.Max(2*scale, 0.005*math.Max(bounds[2]-bounds[0], bounds[3]-bounds[1]))
	bounds[0] -= pad
	bounds[1] -= pad
	bounds[2] += pad
	bounds[3] += pad
	if page != nil {
		bounds = clampCADBounds(bounds, page.viewport)
	}
	return bounds
}

// cadLocalTextBounds is the crop for a self-contained text block such as notes
// or a title block. It deliberately does not expand to nearby ink: a sheet-wide
// scale and an ink search would pull the title block into a notes crop.
func cadLocalTextBounds(boxes []cadTextBox, page *cadInkPage) [4]float64 {
	if len(boxes) == 0 {
		return [4]float64{}
	}
	b := cadBoxesBounds(boxes)
	height := cadMedianItemHeight(cadBoxesToItems(boxes))
	if height <= 0 || math.IsNaN(height) || math.IsInf(height, 0) {
		height = math.Max(1, 0.01*math.Max(b.width(), b.height()))
	}
	pad := height
	bounds := [4]float64{b.minX - pad, b.minY - pad, b.maxX + pad, b.maxY + pad}
	if page != nil {
		bounds = clampCADBounds(bounds, page.viewport)
	}
	return bounds
}

func clampCADBounds(bounds, viewport [4]float64) [4]float64 {
	if cadValidBounds(viewport) {
		bounds[0] = math.Max(bounds[0], viewport[0])
		bounds[1] = math.Max(bounds[1], viewport[1])
		bounds[2] = math.Min(bounds[2], viewport[2])
		bounds[3] = math.Min(bounds[3], viewport[3])
	}
	return bounds
}
