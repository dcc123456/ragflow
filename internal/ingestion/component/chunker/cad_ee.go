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

// CAD chunking groups structured drawing text by sheet and spatial layout.
package chunker

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"ragflow/internal/agent/runtime"
	"ragflow/internal/ingestion/component/schema"
	"ragflow/internal/parser/cadsource"
)

// cadChunkKind identifies a chunk's content category.
type cadChunkKind string

const (
	cadKindTitleBlock cadChunkKind = "title_block"
	cadKindNotes      cadChunkKind = "notes"
	cadKindRegion     cadChunkKind = "region"
	cadKindAttributes cadChunkKind = "attributes"
	cadKindObject     cadChunkKind = "object"
	cadKindTable      cadChunkKind = "table"
)

// cadEvidenceLevel distinguishes explicit CAD data, deterministic rules, and
// geometric inference.
type cadEvidenceLevel string

const (
	// cadEvidenceDeclared is a relation the CAD file itself states: a text's
	// sheet membership, an ATTRIB's owner handle, a native table cell.
	cadEvidenceDeclared cadEvidenceLevel = "declared"
	// cadEvidenceDerived is a fact read from CAD data with a deterministic
	// rule: title-block labels plus position, a notes header, XY-cut bounds.
	cadEvidenceDerived cadEvidenceLevel = "derived"
	// cadEvidenceInferred is a geometric guess, such as a leader pointing at
	// a nearby object.
	cadEvidenceInferred cadEvidenceLevel = "inferred"
)

// cadEvidence is one reason a chunk exists.
type cadEvidence struct {
	Level  cadEvidenceLevel
	Source string
	Method string
}

// cadChunk holds content, drawing context, and internal validation metadata.
type cadChunk struct {
	Kind      cadChunkKind
	SheetID   string
	RegionID  string
	SubjectID string
	Content   string
	Context   string
	Evidence  []cadEvidence
	SourceIDs []string
}

// cadDiagnostic is a coverage or quality signal reported through run progress.
type cadDiagnostic struct {
	Code   string
	Detail string
	Count  int
}

// cadChunkerConfig controls spatial partitioning and chunk size limits.
type cadChunkerConfig struct {
	// BandHeightRatio splits a sheet's texts into independent scale bands.
	// A title block in paper units (height 2.5) and a plan in model units
	// (height 125) must not share one median: the paper band would otherwise
	// set the gap for the whole drawing.
	BandHeightRatio float64
	// TextsPerRegion is the target number of texts per region. The XY-cut
	// stops at ceil(texts/TextsPerRegion) parts (at least one), which keeps a
	// ten-text drawing whole while still splitting a dense floor plan.
	TextsPerRegion int
	// MinGapFactor is the smallest whitespace gap, in text heights, the
	// target-count XY-cut may split at. It keeps a table's rows together.
	MinGapFactor float64
	// SoloSplitGapFactor lets a band whose target is one region still split
	// once when a single very large gap separates two multi-text groups, so a
	// small title block does not absorb its view.
	SoloSplitGapFactor float64
	// MaxRegions caps the target region count of one scale band.
	MaxRegions int
	// MaxLines and MaxChars cap one chunk body. Oversized regions are split at
	// their next-largest whitespace before any line-budget fallback.
	MaxLines int
	MaxChars int
	// MinTextsPerRegion folds an undersized region into its nearest neighbour
	// when the gap between them is small enough. A value of two keeps a
	// label/value pair together while still folding a lone drawing title.
	MinTextsPerRegion int
	// MaxSubChunks caps the parts produced by one oversize split.
	MaxSubChunks int
}

func defaultCADChunkerConfig() cadChunkerConfig {
	return cadChunkerConfig{
		BandHeightRatio:    4,
		TextsPerRegion:     12,
		MinGapFactor:       2,
		SoloSplitGapFactor: 10,
		MaxRegions:         16,
		MaxLines:           100,
		MaxChars:           1200,
		MinTextsPerRegion:  2,
		MaxSubChunks:       32,
	}
}

// cadEvidenceSatisfies checks the minimum evidence for a chunk category.
// cadValidateAndEmit downgrades categories with insufficient evidence to region.
func cadEvidenceSatisfies(kind cadChunkKind, evidence []cadEvidence) bool {
	switch kind {
	case cadKindRegion:
		return true
	case cadKindTitleBlock:
		return cadHasEvidence(evidence, "layout", "title_block", cadEvidenceDerived)
	case cadKindNotes:
		return cadHasEvidence(evidence, "layout", "notes", cadEvidenceDerived)
	case cadKindAttributes:
		return cadHasEvidence(evidence, "attrib", "tag_value", cadEvidenceDerived)
	case cadKindObject:
		// An object requires an explicit owner relation and attribute data.
		return cadHasEvidence(evidence, "insert", "owner_handle", cadEvidenceDeclared) &&
			cadHasEvidence(evidence, "attrib", "tag_value", cadEvidenceDeclared)
	case cadKindTable:
		return cadHasEvidence(evidence, "table", "native_cells", cadEvidenceDeclared) ||
			cadHasEvidence(evidence, "table", "text_grid", cadEvidenceDerived)
	default:
		return false
	}
}

// cadHasEvidence matches a source/method pair at or above the minimum level.
func cadHasEvidence(evidence []cadEvidence, source, method string, minLevel cadEvidenceLevel) bool {
	for _, item := range evidence {
		if item.Source == source && item.Method == method && cadEvidenceRank(item.Level) >= cadEvidenceRank(minLevel) {
			return true
		}
	}
	return false
}

func cadEvidenceRank(level cadEvidenceLevel) int {
	switch level {
	case cadEvidenceDeclared:
		return 2
	case cadEvidenceDerived:
		return 1
	case cadEvidenceInferred:
		return 0
	default:
		return -1
	}
}

const (
	// cadTextWidthFactor estimates the width of one drawing text from its rune
	// count and height. The parser exposes insertion points, not text boxes, so
	// gap detection needs an approximation; 0.7 em per rune sits between Latin
	// (~0.6) and CJK (~1.0) text.
	cadTextWidthFactor = 0.7

	// cadSubSplitGapFactor shrinks the gap threshold at each oversize split
	// level, so a long region is first cut at its next-largest whitespace and
	// only falls back to a hard line split when no gap is left.
	cadSubSplitGapFactor = 0.5

	// cadMaxSubSplitDepth bounds the recursive oversize split.
	cadMaxSubSplitDepth = 4
)

var (
	// cadTitleBlockLabels are the title-block field names the classifier looks
	// for. Matching is case-insensitive substring matching per label.
	cadTitleBlockLabels = []string{
		"图号", "图名", "比例", "材料", "设计", "制图", "审核", "校对",
		"日期", "版本", "重量", "单位名称",
		"dwg no", "drawing no", "title", "scale", "material", "drawn",
		"checked", "approved", "date", "rev", "weight",
	}

	// cadNotesHeaders mark the technical-notes region. A header is searched
	// across the ordered region and the header plus the lines below it are
	// split out as a notes chunk.
	cadNotesHeaders = []string{
		"技术要求", "技术条件", "技术说明", "施工说明", "设计说明", "说明",
		"注：", "注:", "注意",
		"notes", "general notes", "technical requirements",
	}
)

// chunkCAD consumes the parser's structured CAD payload. The handled result is
// false when the payload is absent or invalid.
func chunkCAD(ctx context.Context, upstream schema.ChunkerFromUpstream) (map[string]any, bool, error) {
	raw, ok := cadPayload(upstream)
	if !ok {
		runtime.ReportProgressMessage(ctx, "CADChunker",
			"WARNING: cad_payload_missing: falling back to generic text chunking")
		return nil, false, nil
	}

	var doc cadsource.Drawing
	if err := json.Unmarshal(raw, &doc); err != nil {
		runtime.ReportProgressMessage(ctx, "CADChunker",
			"WARNING: cad_payload_invalid: falling back to generic text chunking: "+err.Error())
		return nil, false, nil
	}

	chunks, diagnostics := buildCADChunks(&doc, defaultCADChunkerConfig(), cadDocumentName(upstream))
	for _, diagnostic := range diagnostics {
		runtime.ReportProgressMessage(ctx, "CADChunker", cadDiagnosticMessage(diagnostic))
	}
	return chunkOutputs(cadChunkDocs(chunks)), true, nil
}

// cadPayload returns the structured CAD document the parser put on
// File["cad"]. The chunker never re-parses the source binary.
func cadPayload(upstream schema.ChunkerFromUpstream) (json.RawMessage, bool) {
	if upstream.File == nil {
		return nil, false
	}
	raw, ok := upstream.File.Extra["cad"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	return raw, true
}

// cadDocumentName is the drawing locator placed in every chunk header. The
// file name is the only drawing identifier available on every engine; the
// title block is a separate chunk, not a field.
func cadDocumentName(upstream schema.ChunkerFromUpstream) string {
	name := strings.TrimSpace(upstream.Name)
	if name == "" && upstream.File != nil {
		name = strings.TrimSpace(upstream.File.Name)
	}
	if name == "" {
		return "CAD"
	}
	name = filepath.Base(name)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		return "CAD"
	}
	name = strings.ReplaceAll(name, "]", ")")
	runes := []rune(name)
	if len(runes) > 60 {
		name = string(runes[:60])
	}
	return name
}

// buildCADChunks turns a parsed drawing into spatial chunks plus one
// attributes chunk per attribute owner. It returns the chunker-internal chunks
// and the diagnostics the caller reports to the run.
func buildCADChunks(doc *cadsource.Drawing, cfg cadChunkerConfig, docName string) ([]cadChunk, []cadDiagnostic) {
	if doc == nil {
		return nil, nil
	}
	expectedSources := 0
	for _, text := range doc.Texts {
		if strings.TrimSpace(text.Text) != "" {
			expectedSources++
		}
	}

	var chunks []cadChunk
	var diagnostics []cadDiagnostic
	sequence := 0
	for _, group := range cadSheetGroups(doc) {
		sheetChunks, sheetDiagnostics, next := cadSheetChunks(group.sheet, group.texts, cfg, docName, sequence)
		chunks = append(chunks, sheetChunks...)
		diagnostics = append(diagnostics, sheetDiagnostics...)
		sequence = next
	}
	attributeChunks, attributeDiagnostics := cadAttributeChunks(doc, docName, cfg)
	chunks = append(chunks, attributeChunks...)
	diagnostics = append(diagnostics, attributeDiagnostics...)
	chunks, diagnostics = cadValidateAndEmit(chunks, diagnostics, docName)

	emittedSources := 0
	for _, chunk := range chunks {
		emittedSources += len(chunk.SourceIDs)
	}
	if emittedSources < expectedSources {
		diagnostics = append(diagnostics, cadDiagnostic{
			Code: "cad_text_unassigned", Count: expectedSources - emittedSources,
		})
	}
	if len(chunks) == 0 {
		diagnostics = append(diagnostics, cadDiagnostic{
			Code:   "cad_no_text",
			Detail: fmt.Sprintf("texts=%d attributes=%d", len(doc.Texts), len(doc.Attributes)),
		})
	}
	return chunks, diagnostics
}

func cadDiagnosticMessage(diagnostic cadDiagnostic) string {
	message := "WARNING: " + diagnostic.Code
	if diagnostic.Detail != "" {
		message += ": " + diagnostic.Detail
	}
	if diagnostic.Count > 0 {
		message += fmt.Sprintf(" (count=%d)", diagnostic.Count)
	}
	return message
}

type cadSheetGroup struct {
	sheet cadsource.Sheet
	texts []cadsource.Text
}

// cadSheetGroups partitions texts by sheet. Sheet order follows the document;
// ids the document did not declare are appended in sorted order (empty id last)
// so a shuffled input still produces the same chunk sequence.
func cadSheetGroups(doc *cadsource.Drawing) []cadSheetGroup {
	textsBySheet := make(map[string][]cadsource.Text, len(doc.Sheets))
	for _, text := range doc.Texts {
		textsBySheet[text.SheetID] = append(textsBySheet[text.SheetID], text)
	}

	groups := make([]cadSheetGroup, 0, len(doc.Sheets)+1)
	seen := make(map[string]bool, len(doc.Sheets))
	for _, sheet := range doc.Sheets {
		if sheet.ID == "" || seen[sheet.ID] {
			continue
		}
		seen[sheet.ID] = true
		groups = append(groups, cadSheetGroup{sheet: sheet, texts: textsBySheet[sheet.ID]})
		delete(textsBySheet, sheet.ID)
	}

	extra := make([]string, 0, len(textsBySheet))
	for id := range textsBySheet {
		extra = append(extra, id)
	}
	sort.Slice(extra, func(i, j int) bool {
		if (extra[i] == "") != (extra[j] == "") {
			return extra[i] != ""
		}
		return extra[i] < extra[j]
	})
	for _, id := range extra {
		groups = append(groups, cadSheetGroup{
			sheet: cadsource.Sheet{ID: id},
			texts: textsBySheet[id],
		})
	}
	return groups
}

// cadSheetChunks clusters one sheet's texts into regions. A sheet without a
// usable text scale becomes one or more budget-sized regions instead of
// invented spatial regions.
func cadSheetChunks(sheet cadsource.Sheet, raw []cadsource.Text, cfg cadChunkerConfig,
	docName string, sequence int) ([]cadChunk, []cadDiagnostic, int) {
	items := make([]cadTextItem, 0, len(raw))
	for _, text := range raw {
		trimmed := strings.TrimSpace(text.Text)
		if trimmed == "" {
			continue
		}
		items = append(items, cadTextItem{
			text:   trimmed,
			x:      cadFinite(text.X),
			y:      cadFinite(text.Y),
			height: cadNonNegative(text.Height),
			handle: text.Handle,
		})
	}
	if len(items) == 0 {
		return nil, nil, sequence
	}
	sortCADTextItems(items)
	for i := range items {
		items[i].sourceID = fmt.Sprintf("text:%06d", sequence)
		sequence++
	}
	chunks, diagnostics := cadSheetChunksFromItems(sheet, items, cfg, docName)
	return chunks, diagnostics, sequence
}

// cadScaledPart is one spatial part together with the text scale it was
// computed at. Parts from different height bands stay separate.
type cadScaledPart struct {
	boxes []cadTextBox
	scale float64
}

// cadSheetChunksFromItems partitions one sheet by text scale before it cuts
// space. A sheet can mix a title block in paper units with a plan in model
// units; one sheet-wide median would let the smaller text set the gap for the
// whole drawing. Each band is then cut with a target region count, so a small
// drawing stays whole while a dense one is split.
func cadSheetChunksFromItems(sheet cadsource.Sheet, items []cadTextItem, cfg cadChunkerConfig, docName string) ([]cadChunk, []cadDiagnostic) {
	height, exact := cadTextScale(items)
	if height <= 0 {
		boxes := make([]cadTextBox, len(items))
		for i, item := range items {
			boxes[i] = cadTextBoxFor(item, 1)
		}
		evidence := []cadEvidence{{
			Level: cadEvidenceInferred, Source: "text_layout",
			Method: "whole_sheet_fallback",
		}}
		chunks := cadRegionChunks(docName, sheet, "R1", cadKindRegion, evidence, boxes, 0, 0, cfg)
		return chunks, []cadDiagnostic{{
			Code: "cad_scale_unreliable", Detail: sheetRef(sheet),
		}}
	}

	allBoxes := make([]cadTextBox, len(items))
	for i, item := range items {
		allBoxes[i] = cadTextBoxFor(item, height)
	}
	bounds := cadBoundsForSheet(sheet, allBoxes)

	bands := cadHeightBands(items, cfg.BandHeightRatio)
	parts := make([]cadScaledPart, 0, len(items))
	var diagnostics []cadDiagnostic
	for _, band := range bands {
		if len(band) == 0 {
			continue
		}
		bandScale := cadBandScale(band, height)
		target := cadTargetRegions(len(band), cfg)
		boxes := make([]cadTextBox, len(band))
		for i, item := range band {
			boxes[i] = cadTextBoxFor(item, bandScale)
		}

		bandParts := splitCADBoxesTarget(boxes, cfg.MinGapFactor*bandScale, target)
		if target <= 1 && len(band) > 1 {
			// A small sheet still splits once when a single very large gap
			// separates two multi-text groups. Without this a small title
			// block would be absorbed by its view.
			if solo := splitCADBoxesTarget(boxes, cfg.SoloSplitGapFactor*bandScale, 2); len(solo) == 2 && len(solo[0]) >= 2 && len(solo[1]) >= 2 {
				bandParts = solo
			}
		}
		bandParts = mergeCADParts(bandParts, cfg.MinTextsPerRegion, 3*bandScale)
		for _, part := range bandParts {
			parts = append(parts, cadScaledPart{boxes: part, scale: bandScale})
		}
		if cfg.MaxRegions > 0 && cadNaturalRegions(len(band), cfg) > cfg.MaxRegions {
			diagnostics = append(diagnostics, cadDiagnostic{
				Code: "cad_region_cap", Detail: sheetRef(sheet), Count: len(bandParts),
			})
		}
	}
	if !exact {
		diagnostics = append(diagnostics, cadDiagnostic{
			Code: "cad_scale_unreliable", Detail: sheetRef(sheet),
		})
	}
	if len(bands) > 1 {
		diagnostics = append(diagnostics, cadDiagnostic{
			Code: "cad_scale_mixed", Detail: sheetRef(sheet), Count: len(bands),
		})
	}
	sortCADScaledParts(parts)

	titleIndex := cadTitleBlockIndex(parts, bounds)
	chunks := make([]cadChunk, 0, len(parts))
	for i, part := range parts {
		baseID := fmt.Sprintf("R%d", i+1)
		gap := cfg.MinGapFactor * part.scale
		if i == titleIndex {
			chunks = append(chunks, cadRegionChunks(docName, sheet, baseID, cadKindTitleBlock,
				[]cadEvidence{{Level: cadEvidenceDerived, Source: "layout", Method: "title_block"}},
				part.boxes, gap, 0, cfg)...)
			continue
		}
		if before, notes, ok := cadSplitNotesPart(part.boxes); ok {
			if len(before) > 0 {
				chunks = append(chunks, cadRegionChunks(docName, sheet, baseID, cadKindRegion,
					[]cadEvidence{{Level: cadEvidenceDerived, Source: "text_layout", Method: "xy_cut"}},
					before, gap, 0, cfg)...)
				baseID += "N"
			}
			chunks = append(chunks, cadRegionChunks(docName, sheet, baseID, cadKindNotes,
				[]cadEvidence{{Level: cadEvidenceDerived, Source: "layout", Method: "notes"}},
				notes, gap, 0, cfg)...)
			continue
		}
		chunks = append(chunks, cadRegionChunks(docName, sheet, baseID, cadKindRegion,
			[]cadEvidence{{Level: cadEvidenceDerived, Source: "text_layout", Method: "xy_cut"}},
			part.boxes, gap, 0, cfg)...)
	}
	return chunks, diagnostics
}

// cadNaturalRegions is a band's target region count before MaxRegions clips
// it. The cap diagnostic compares against this value so a band that merely
// reaches its natural target is not reported as capped.
func cadNaturalRegions(texts int, cfg cadChunkerConfig) int {
	if texts <= 0 || cfg.TextsPerRegion <= 0 {
		return 1
	}
	return (texts + cfg.TextsPerRegion - 1) / cfg.TextsPerRegion
}

// cadTargetRegions turns a band's text count into a target region count. The
// target is a count, not a gap threshold: a ten-text drawing stays one chunk,
// while a dense floor plan splits at its largest gaps until the target is
// reached.
func cadTargetRegions(texts int, cfg cadChunkerConfig) int {
	target := cadNaturalRegions(texts, cfg)
	if target < 1 {
		target = 1
	}
	if cfg.MaxRegions > 0 && target > cfg.MaxRegions {
		target = cfg.MaxRegions
	}
	return target
}

// cadTextItem holds a text's content, position, scale, and source identity.
type cadTextItem struct {
	text     string
	x        float64
	y        float64
	height   float64
	handle   uint64
	sourceID string
}

// cadTextBox is a text's estimated world-space rectangle. The insertion point
// is the baseline-left corner for AutoCAD TEXT, so the box extends right and up.
type cadTextBox struct {
	item                   cadTextItem
	minX, maxX, minY, maxY float64
}

func cadTextBoxFor(item cadTextItem, height float64) cadTextBox {
	if item.height > 0 {
		height = item.height
	}
	width := float64(utf8.RuneCountInString(item.text)) * cadTextWidthFactor * height
	if width < height {
		width = height
	}
	return cadTextBox{
		item: item,
		minX: item.x,
		maxX: item.x + width,
		minY: item.y,
		maxY: item.y + height,
	}
}

// cadTextScale returns the drawing's text height and whether it came from the
// parser. Without a height the chunker estimates one from line spacing; when
// that is unreliable it returns 0 and the caller budget-splits the sheet
// without inventing spatial regions.
func cadTextScale(items []cadTextItem) (float64, bool) {
	heights := make([]float64, 0, len(items))
	for _, item := range items {
		if item.height > 0 {
			heights = append(heights, item.height)
		}
	}
	if len(heights) > 0 {
		if height := cadMedian(heights); height > 0 {
			return height, true
		}
	}
	if height, ok := cadScaleFromLineSpacing(items); ok {
		return height, false
	}
	return 0, false
}

// cadScaleFromLineSpacing estimates text height from the median line pitch.
// It requires at least four lines and a mostly regular pitch: scattered text
// would otherwise produce a meaningless scale and wrong regions.
func cadScaleFromLineSpacing(items []cadTextItem) (float64, bool) {
	if len(items) < 4 {
		return 0, false
	}
	ys := make([]float64, 0, len(items))
	for _, item := range items {
		ys = append(ys, item.y)
	}
	sort.Float64s(ys)

	deltas := make([]float64, 0, len(ys))
	for i := 1; i < len(ys); i++ {
		if delta := ys[i] - ys[i-1]; delta > 1e-9 {
			deltas = append(deltas, delta)
		}
	}
	if len(deltas) < 3 {
		return 0, false
	}
	median := cadMedian(deltas)
	if median <= 0 {
		return 0, false
	}
	regular := 0
	for _, delta := range deltas {
		if delta <= median*2 {
			regular++
		}
	}
	if float64(regular)/float64(len(deltas)) < 0.7 {
		return 0, false
	}
	height := median / 2
	if height <= 0 || math.IsNaN(height) || math.IsInf(height, 0) {
		return 0, false
	}
	return height, true
}

// cadRegionChunks builds one region's chunks. A region that exceeds the line or
// character budget is split at its next-largest whitespace first; if no gap is
// left it is cut between lines, which keeps every text exactly once.
func cadRegionChunks(docName string, sheet cadsource.Sheet, regionID string, kind cadChunkKind,
	evidence []cadEvidence, boxes []cadTextBox, gap float64, depth int, cfg cadChunkerConfig) []cadChunk {
	items := cadBoxesToItems(boxes)
	if len(items) <= cfg.MaxLines && cadTextLen(items) <= cfg.MaxChars {
		chunk := cadChunkFromItems(docName, sheet, regionID, kind, items)
		chunk.Evidence = evidence
		return []cadChunk{chunk}
	}

	if depth < cadMaxSubSplitDepth && gap > 0 {
		subGap := gap * math.Pow(cadSubSplitGapFactor, float64(depth+1))
		parts := splitCADBoxesTarget(boxes, subGap, cfg.MaxSubChunks)
		if len(parts) > 1 {
			parts = mergeCADParts(parts, cfg.MinTextsPerRegion, 3*gap)
			chunks := make([]cadChunk, 0, len(parts))
			for i, part := range parts {
				subID := fmt.Sprintf("%s.%d", regionID, i+1)
				chunks = append(chunks, cadRegionChunks(
					docName, sheet, subID, kind, evidence, part, gap, depth+1, cfg)...)
			}
			return chunks
		}
	}

	groups := splitCADItemsByBudget(items, cfg.MaxLines, cfg.MaxChars)
	chunks := make([]cadChunk, 0, len(groups))
	for i, group := range groups {
		id := regionID
		if len(groups) > 1 {
			id = fmt.Sprintf("%s.%d", regionID, i+1)
		}
		chunk := cadChunkFromItems(docName, sheet, id, kind, group)
		chunk.Evidence = evidence
		chunks = append(chunks, chunk)
	}
	return chunks
}

// splitCADItemsByBudget cuts an ordered text list into budget-sized runs. A
// single over-budget line is kept whole rather than truncated.
func splitCADItemsByBudget(items []cadTextItem, maxLines, maxChars int) [][]cadTextItem {
	if len(items) == 0 {
		return nil
	}
	if maxLines <= 0 {
		maxLines = len(items)
	}
	if maxChars <= 0 {
		maxChars = math.MaxInt32
	}

	groups := make([][]cadTextItem, 0, 4)
	start, lines, chars := 0, 0, 0
	for i, item := range items {
		itemChars := utf8.RuneCountInString(item.text) + 1
		if lines > 0 && (lines+1 > maxLines || chars+itemChars > maxChars) {
			groups = append(groups, items[start:i])
			start, lines, chars = i, 0, 0
		}
		lines++
		chars += itemChars
	}
	groups = append(groups, items[start:])
	return groups
}

// cadChunkFromItems assembles text and source IDs in reading order.
func cadChunkFromItems(docName string, sheet cadsource.Sheet, regionID string,
	kind cadChunkKind, items []cadTextItem) cadChunk {
	sortCADTextItems(items)
	lines := make([]string, len(items))
	sourceIDs := make([]string, len(items))
	for i, item := range items {
		lines[i] = item.text
		sourceIDs[i] = item.sourceID
	}
	return cadChunk{
		Kind:      kind,
		SheetID:   sheetRef(sheet),
		RegionID:  regionID,
		SubjectID: fmt.Sprintf("%s:%s:%s", kind, sheetRef(sheet), regionID),
		Content:   strings.Join(lines, "\n"),
		Context:   cadHeader(docName, sheet, regionID, kind),
		SourceIDs: sourceIDs,
	}
}

// cadHeader keeps drawing context in text because chunk stores do not uniformly
// persist CAD metadata from Extra.
func cadHeader(docName string, sheet cadsource.Sheet, regionID string, kind cadChunkKind) string {
	return fmt.Sprintf("[%s · %s/%s · %s]\n", docName, sheetRef(sheet), regionID, kind)
}

func sheetRef(sheet cadsource.Sheet) string {
	id := strings.TrimSpace(sheet.ID)
	if id == "" {
		return "s0"
	}
	return id
}

// cadAttributeChunks emits one chunk per attribute owner. An attribute's owner
// handle is a declared relation, so attributes from different blocks are never
// mixed. An attribute with OwnerHandle 0 has no declared owner; it is grouped
// as "unowned" with derived evidence and a diagnostic instead of pretending
// the handle proves a block relation. The raw value also remains in the region
// text: without a source-level join there is no safe way to prove the two
// strings are the same fact.
func cadAttributeChunks(doc *cadsource.Drawing, docName string, cfg cadChunkerConfig) ([]cadChunk, []cadDiagnostic) {
	type group struct {
		owner uint64
		items []cadsource.Attribute
	}
	index := make(map[uint64]int)
	groups := make([]group, 0, 4)
	unowned := 0
	for _, attribute := range doc.Attributes {
		tag := strings.TrimSpace(attribute.Tag)
		value := strings.TrimSpace(attribute.Value)
		if tag == "" || value == "" {
			continue
		}
		if attribute.OwnerHandle == 0 {
			unowned++
		}
		position, ok := index[attribute.OwnerHandle]
		if !ok {
			position = len(groups)
			index[attribute.OwnerHandle] = position
			groups = append(groups, group{owner: attribute.OwnerHandle})
		}
		groups[position].items = append(groups[position].items, attribute)
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].owner < groups[j].owner })

	chunks := make([]cadChunk, 0, len(groups))
	var diagnostics []cadDiagnostic
	for _, attributeGroup := range groups {
		seen := make(map[string]bool, len(attributeGroup.items))
		lines := make([]string, 0, len(attributeGroup.items))
		for _, attribute := range attributeGroup.items {
			line := strings.TrimSpace(attribute.Tag) + ": " + strings.TrimSpace(attribute.Value)
			if seen[line] {
				continue
			}
			seen[line] = true
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			continue
		}

		level := cadEvidenceDeclared
		ownerLabel := fmt.Sprintf("%d", attributeGroup.owner)
		if attributeGroup.owner == 0 {
			level = cadEvidenceDerived
			ownerLabel = "unowned"
		}
		budgetGroups := cadSplitAttributeLines(lines, cfg.MaxLines, cfg.MaxChars)
		for i, part := range budgetGroups {
			suffix := ""
			if len(budgetGroups) > 1 {
				suffix = fmt.Sprintf(" %d/%d", i+1, len(budgetGroups))
			}
			chunks = append(chunks, cadChunk{
				Kind:      cadKindAttributes,
				SubjectID: fmt.Sprintf("attributes:%d:%d", attributeGroup.owner, i+1),
				Content:   strings.Join(part, "\n"),
				Context:   fmt.Sprintf("[%s · attributes %s%s]\n", docName, ownerLabel, suffix),
				Evidence: []cadEvidence{{
					Level: level, Source: "attrib", Method: "tag_value",
				}},
			})
		}
	}
	if unowned > 0 {
		diagnostics = append(diagnostics, cadDiagnostic{
			Code: "cad_attrib_owner_missing", Detail: "0", Count: unowned,
		})
	}
	return chunks, diagnostics
}

// cadSplitAttributeLines cuts one attribute owner's lines into the same
// line/character budget as a spatial chunk. A block with hundreds of
// attributes must not become one oversized chunk.
func cadSplitAttributeLines(lines []string, maxLines, maxChars int) [][]string {
	if len(lines) == 0 {
		return nil
	}
	if maxLines <= 0 {
		maxLines = len(lines)
	}
	if maxChars <= 0 {
		maxChars = math.MaxInt32
	}
	groups := make([][]string, 0, 4)
	start, count, chars := 0, 0, 0
	for i, line := range lines {
		lineChars := utf8.RuneCountInString(line) + 1
		if count > 0 && (count+1 > maxLines || chars+lineChars > maxChars) {
			groups = append(groups, lines[start:i])
			start, count, chars = i, 0, 0
		}
		count++
		chars += lineChars
	}
	return append(groups, lines[start:])
}

// cadValidateAndEmit is the last quality gate: empty chunks are dropped, a kind
// whose evidence is missing is downgraded to a region, duplicate source text is
// reported, and identical context+content pairs are emitted once.
func cadValidateAndEmit(chunks []cadChunk, diagnostics []cadDiagnostic, docName string) ([]cadChunk, []cadDiagnostic) {
	out := make([]cadChunk, 0, len(chunks))
	seenSource := make(map[string]bool)
	seenChunk := make(map[string]bool)

	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.Content) == "" {
			continue
		}
		if !cadEvidenceSatisfies(chunk.Kind, chunk.Evidence) {
			diagnostics = append(diagnostics, cadDiagnostic{
				Code: "cad_chunk_evidence_missing", Detail: string(chunk.Kind),
			})
			chunk.Kind = cadKindRegion
			chunk.SubjectID = fmt.Sprintf("%s:%s:%s", chunk.Kind, chunk.SheetID, chunk.RegionID)
			chunk.Context = cadHeader(docName, cadsource.Sheet{ID: chunk.SheetID}, chunk.RegionID, chunk.Kind)
		}

		duplicateSource := false
		for _, id := range chunk.SourceIDs {
			if id == "" {
				continue
			}
			if seenSource[id] {
				duplicateSource = true
				continue
			}
			seenSource[id] = true
		}
		if duplicateSource {
			diagnostics = append(diagnostics, cadDiagnostic{
				Code: "cad_source_duplicate", Detail: chunk.SubjectID,
			})
		}

		key := chunk.Context + "\x00" + chunk.Content
		if seenChunk[key] {
			diagnostics = append(diagnostics, cadDiagnostic{
				Code: "cad_chunk_duplicate", Detail: chunk.SubjectID,
			})
			continue
		}
		seenChunk[key] = true
		out = append(out, chunk)
	}
	return out, diagnostics
}

func cadChunkDocs(chunks []cadChunk) []schema.ChunkDoc {
	docs := make([]schema.ChunkDoc, 0, len(chunks))
	for _, chunk := range chunks {
		docs = append(docs, schema.ChunkDoc{
			Text:         chunk.Content,
			DocType:      "text",
			ContextAbove: chunk.Context,
		})
	}
	return docs
}

// --- spatial partitioning -------------------------------------------------

// cadHeightBands groups texts into independent scale bands. Neighbouring
// heights within ratio stay together; a title block in paper units and a plan
// in model units end up in different bands, each with its own scale. Texts
// without a height join the band that holds most of the sheet.
func cadHeightBands(items []cadTextItem, ratio float64) [][]cadTextItem {
	if ratio <= 1 {
		return [][]cadTextItem{items}
	}
	heights := make([]float64, 0, len(items))
	for _, item := range items {
		if item.height > 0 {
			heights = append(heights, item.height)
		}
	}
	if len(heights) == 0 {
		return [][]cadTextItem{items}
	}
	sort.Float64s(heights)
	var boundaries []float64
	for i := 1; i < len(heights); i++ {
		if heights[i] > heights[i-1]*ratio {
			boundaries = append(boundaries, heights[i])
		}
	}
	if len(boundaries) == 0 {
		return [][]cadTextItem{items}
	}

	bands := make([][]cadTextItem, len(boundaries)+1)
	for _, item := range items {
		if item.height <= 0 {
			continue
		}
		index := 0
		for index < len(boundaries) && item.height >= boundaries[index] {
			index++
		}
		bands[index] = append(bands[index], item)
	}
	largest := 0
	for i := range bands {
		if len(bands[i]) > len(bands[largest]) {
			largest = i
		}
	}
	for _, item := range items {
		if item.height <= 0 {
			bands[largest] = append(bands[largest], item)
		}
	}
	return bands
}

// cadBandScale is one band's median text height. The whole-sheet median is the
// fallback for a band that carries only height-less texts.
func cadBandScale(band []cadTextItem, fallback float64) float64 {
	heights := make([]float64, 0, len(band))
	for _, item := range band {
		if item.height > 0 {
			heights = append(heights, item.height)
		}
	}
	if len(heights) == 0 {
		return fallback
	}
	if scale := cadMedian(heights); scale > 0 {
		return scale
	}
	return fallback
}

// splitCADBoxesTarget performs the largest-gap-first XY-cut until the part
// count reaches target or no gap reaches minGap. Unlike the threshold-first
// split it never exceeds target, so a pathological drawing cannot explode and
// a small sheet is not cut into single-text fragments.
func splitCADBoxesTarget(boxes []cadTextBox, minGap float64, target int) [][]cadTextBox {
	if len(boxes) <= 1 || target <= 1 {
		return [][]cadTextBox{boxes}
	}
	parts := [][]cadTextBox{boxes}
	for len(parts) < target {
		bestIndex := -1
		bestGap := 0.0
		bestXAxis := true
		bestAt := 0.0
		for i, part := range parts {
			if len(part) <= 1 {
				continue
			}
			xAxis, at, gap, ok := cadPartBestGap(part, minGap)
			if !ok || gap <= bestGap {
				continue
			}
			bestIndex, bestGap, bestXAxis, bestAt = i, gap, xAxis, at
		}
		if bestIndex < 0 {
			break
		}
		lower, upper := cadSplitAt(parts[bestIndex], bestXAxis, bestAt)
		if len(lower) == 0 || len(upper) == 0 {
			break
		}
		parts[bestIndex] = lower
		parts = append(parts, upper)
	}
	return parts
}

// cadPartBestGap returns a part's widest separating gap across both axes when
// it reaches threshold. Ties prefer the X axis so the split order is
// deterministic.
func cadPartBestGap(boxes []cadTextBox, threshold float64) (xAxis bool, at, gap float64, ok bool) {
	gapX, atX, okX := cadAxisGap(boxes, true)
	gapY, atY, okY := cadAxisGap(boxes, false)
	if okX && okY {
		if gapY > gapX {
			okX = false
		} else {
			okY = false
		}
	}
	if okX && gapX >= threshold {
		return true, atX, gapX, true
	}
	if okY && gapY >= threshold {
		return false, atY, gapY, true
	}
	return false, 0, 0, false
}

// sortCADScaledParts orders the bands' parts together, top-to-bottom then
// left-to-right, so region ids follow reading order regardless of band.
func sortCADScaledParts(parts []cadScaledPart) {
	sort.SliceStable(parts, func(i, j int) bool {
		a, b := cadBoxesBounds(parts[i].boxes), cadBoxesBounds(parts[j].boxes)
		if a.maxY != b.maxY {
			return a.maxY > b.maxY
		}
		if a.minX != b.minX {
			return a.minX < b.minX
		}
		return len(parts[i].boxes) > len(parts[j].boxes)
	})
}

// cadAxisGap finds the widest whitespace between merged intervals on one axis.
func cadAxisGap(boxes []cadTextBox, xAxis bool) (gap float64, at float64, ok bool) {
	type interval struct{ lo, hi float64 }
	intervals := make([]interval, len(boxes))
	for i, box := range boxes {
		if xAxis {
			intervals[i] = interval{box.minX, box.maxX}
		} else {
			intervals[i] = interval{box.minY, box.maxY}
		}
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].lo != intervals[j].lo {
			return intervals[i].lo < intervals[j].lo
		}
		return intervals[i].hi < intervals[j].hi
	})

	best, bestAt := 0.0, 0.0
	curHi := intervals[0].hi
	for _, iv := range intervals[1:] {
		if iv.lo <= curHi {
			if iv.hi > curHi {
				curHi = iv.hi
			}
			continue
		}
		if space := iv.lo - curHi; space > best {
			best, bestAt = space, (curHi+iv.lo)/2
		}
		curHi = iv.hi
	}
	return best, bestAt, best > 0
}

// cadSplitAt divides boxes at a coordinate that sits inside a whitespace gap.
func cadSplitAt(boxes []cadTextBox, xAxis bool, at float64) ([]cadTextBox, []cadTextBox) {
	lower := make([]cadTextBox, 0, len(boxes))
	upper := make([]cadTextBox, 0, len(boxes))
	for _, box := range boxes {
		lo, hi := box.minY, box.maxY
		if xAxis {
			lo, hi = box.minX, box.maxX
		}
		switch {
		case hi <= at:
			lower = append(lower, box)
		case lo >= at:
			upper = append(upper, box)
		default:
			// Retain text if its box unexpectedly straddles the split.
			lower = append(lower, box)
		}
	}
	return lower, upper
}

// mergeCADParts folds an undersized part into its nearest neighbour. A lone
// text has no useful standalone chunk, so it is folded regardless of distance;
// a larger undersized part only merges when the gap is within maxGap. A far
// part stays alone: losing text coverage is worse than a small region.
func mergeCADParts(parts [][]cadTextBox, minTexts int, maxGap float64) [][]cadTextBox {
	if len(parts) < 2 || minTexts <= 1 {
		return parts
	}
	for {
		merged := false
		for i := range parts {
			if len(parts[i]) >= minTexts {
				continue
			}
			best, bestGap := -1, math.Inf(1)
			for j := range parts {
				if i == j {
					continue
				}
				if space := cadPartsGap(parts[i], parts[j]); space < bestGap {
					best, bestGap = j, space
				}
			}
			if best < 0 {
				continue
			}
			if len(parts[i]) > 1 && bestGap > maxGap {
				continue
			}
			parts[best] = append(parts[best], parts[i]...)
			parts = append(parts[:i], parts[i+1:]...)
			merged = true
			break
		}
		if !merged {
			return parts
		}
	}
}

// cadPartsGap is the axis-aligned distance between two parts' bounding boxes.
func cadPartsGap(a, b []cadTextBox) float64 {
	ab, bb := cadBoxesBounds(a), cadBoxesBounds(b)
	dx := math.Max(0, math.Max(ab.minX-bb.maxX, bb.minX-ab.maxX))
	dy := math.Max(0, math.Max(ab.minY-bb.maxY, bb.minY-ab.maxY))
	return math.Hypot(dx, dy)
}

// cadTextItemLess is the reading order the chunk body preserves: top-to-bottom,
// then left-to-right, with text and handle as deterministic tie-breakers.
func cadTextItemLess(a, b cadTextItem) bool {
	if a.y != b.y {
		return a.y > b.y
	}
	if a.x != b.x {
		return a.x < b.x
	}
	if a.text != b.text {
		return a.text < b.text
	}
	return a.handle < b.handle
}

// sortCADTextItems fixes the reading order that the chunk body preserves.
func sortCADTextItems(items []cadTextItem) {
	sort.SliceStable(items, func(i, j int) bool {
		return cadTextItemLess(items[i], items[j])
	})
}

// --- region classification ------------------------------------------------

// cadTitleBlockIndex chooses the title-block part among a sheet's parts. A
// labelled bottom-right block wins on labels; a locale-independent structural
// rule also recognises a bottom short-line block whose labels are not in the
// known list (for example an Italian title block). The bottom-most candidate
// wins so a scale bar above the title block is not mistaken for it.
func cadTitleBlockIndex(parts []cadScaledPart, bounds cadBounds) int {
	if len(parts) == 0 || !bounds.valid() {
		return -1
	}
	// A known labelled title block is the stronger signal; only fall back to
	// the structural rule when no part carries the known field labels.
	if index := cadBottomMostPart(parts, func(part cadScaledPart) bool {
		return cadIsTitleBlock(part.boxes, bounds)
	}); index >= 0 {
		return index
	}
	return cadBottomMostPart(parts, func(part cadScaledPart) bool {
		return cadIsTitleBlockStructural(part.boxes, bounds)
	})
}

func cadBottomMostPart(parts []cadScaledPart, match func(cadScaledPart) bool) int {
	best := -1
	bestY := math.Inf(1)
	bestHits := -1
	for i, part := range parts {
		if !match(part) {
			continue
		}
		box := cadBoxesBounds(part.boxes)
		hits := cadTitleBlockLabelHits(part.boxes)
		if box.maxY < bestY || (box.maxY == bestY && hits > bestHits) {
			best, bestY, bestHits = i, box.maxY, hits
		}
	}
	return best
}

// cadIsTitleBlock requires both a bottom-right position and at least two
// distinct field labels. One label alone is too weak to assert a title block.
func cadIsTitleBlock(part []cadTextBox, bounds cadBounds) bool {
	if len(part) < 2 || !bounds.valid() {
		return false
	}
	box := cadBoxesBounds(part)
	centerX := (box.minX + box.maxX) / 2
	centerY := (box.minY + box.maxY) / 2
	if centerX < bounds.minX+0.6*bounds.width() {
		return false
	}
	if centerY > bounds.minY+0.4*bounds.height() {
		return false
	}
	return cadTitleBlockLabelHits(part) >= 2
}

// cadIsTitleBlockStructural recognises a title block by shape rather than
// language: a bottom short-line block that is small relative to the sheet.
// The known-label rule stays the stronger signal; this rule exists because a
// title block's field names are locale-specific. A part that carries a notes
// header is excluded: a bottom block of short lines is then a notes list, and
// treating it as a title block would drop the notes chunk.
func cadIsTitleBlockStructural(part []cadTextBox, bounds cadBounds) bool {
	if len(part) < 8 || !bounds.valid() || cadPartHasNotesHeader(part) {
		return false
	}
	box := cadBoxesBounds(part)
	if box.maxY > bounds.minY+0.25*bounds.height() {
		return false
	}
	if box.height() > 0.15*bounds.height() {
		return false
	}
	short := 0
	for _, item := range part {
		if utf8.RuneCountInString(item.item.text) <= 30 {
			short++
		}
	}
	return short*100 >= len(part)*70
}

func cadTitleBlockLabelHits(part []cadTextBox) int {
	hits := 0
	for _, label := range cadTitleBlockLabels {
		for _, box := range part {
			if strings.Contains(strings.ToLower(box.item.text), label) {
				hits++
				break
			}
		}
	}
	return hits
}

// cadPartHasNotesHeader reports whether a part carries a notes header. Such a
// part is a notes list wherever it sits, so the structural title-block rule
// must not claim it.
func cadPartHasNotesHeader(part []cadTextBox) bool {
	for _, box := range part {
		if cadIsNotesHeader(box.item.text) {
			return true
		}
	}
	return false
}

// cadSplitNotesPart finds a notes header anywhere in a region and splits the
// notes off as their own chunk. The header must have at least one line after
// it; a table above the notes stays in the preceding region.
func cadSplitNotesPart(boxes []cadTextBox) (before, notes []cadTextBox, ok bool) {
	ordered := append([]cadTextBox(nil), boxes...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return cadTextItemLess(ordered[i].item, ordered[j].item)
	})
	for i, box := range ordered {
		if !cadIsNotesHeader(box.item.text) {
			continue
		}
		if i == len(ordered)-1 {
			return nil, nil, false
		}
		return ordered[:i], ordered[i:], true
	}
	return nil, nil, false
}

// cadIsNotesHeader matches the configured notes headers and a bare NOTE line.
// The bare form accepts "NOTE", "Notes:", "note 1:" and "note", but not an
// embedded "footnote" or an unrelated word such as "notebook".
func cadIsNotesHeader(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, header := range cadNotesHeaders {
		if strings.Contains(lower, header) {
			return true
		}
	}
	return lower == "note" ||
		strings.HasPrefix(lower, "note:") ||
		strings.HasPrefix(lower, "notes") ||
		strings.HasPrefix(lower, "note ")
}

// --- geometry helpers -----------------------------------------------------

type cadBounds struct {
	minX, minY, maxX, maxY float64
}

func (b cadBounds) valid() bool { return b.maxX > b.minX && b.maxY > b.minY }

func (b cadBounds) width() float64 { return b.maxX - b.minX }

func (b cadBounds) height() float64 { return b.maxY - b.minY }

func cadBoxesBounds(boxes []cadTextBox) cadBounds {
	if len(boxes) == 0 {
		return cadBounds{}
	}
	bounds := cadBounds{
		minX: boxes[0].minX, minY: boxes[0].minY,
		maxX: boxes[0].maxX, maxY: boxes[0].maxY,
	}
	for _, box := range boxes[1:] {
		bounds.minX = math.Min(bounds.minX, box.minX)
		bounds.minY = math.Min(bounds.minY, box.minY)
		bounds.maxX = math.Max(bounds.maxX, box.maxX)
		bounds.maxY = math.Max(bounds.maxY, box.maxY)
	}
	return bounds
}

// cadBoundsForSheet prefers the detected frame. A frame that holds less than
// half of the sheet's texts is treated as stale (or as a paper-space box that
// does not match the text coordinates) and replaced by the text extent, so the
// title-block quadrant is never computed against the wrong origin.
func cadBoundsForSheet(sheet cadsource.Sheet, boxes []cadTextBox) cadBounds {
	bounds := cadBounds{
		minX: cadFinite(sheet.Bounds[0]),
		minY: cadFinite(sheet.Bounds[1]),
		maxX: cadFinite(sheet.Bounds[2]),
		maxY: cadFinite(sheet.Bounds[3]),
	}
	derived := cadBoxesBounds(boxes)
	if !bounds.valid() || len(boxes) == 0 {
		if bounds.valid() {
			return bounds
		}
		return derived
	}
	inside := 0
	for _, box := range boxes {
		centerX := (box.minX + box.maxX) / 2
		centerY := (box.minY + box.maxY) / 2
		if centerX >= bounds.minX && centerX <= bounds.maxX &&
			centerY >= bounds.minY && centerY <= bounds.maxY {
			inside++
		}
	}
	if inside*2 < len(boxes) {
		return derived
	}
	return bounds
}

func cadBoxesToItems(boxes []cadTextBox) []cadTextItem {
	items := make([]cadTextItem, len(boxes))
	for i, box := range boxes {
		items[i] = box.item
	}
	return items
}

func cadTextLen(items []cadTextItem) int {
	total := 0
	for _, item := range items {
		total += utf8.RuneCountInString(item.text) + 1
	}
	return total
}

func cadMedian(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// cadFinite maps NaN/Inf coordinates to zero; go-cad should never emit them,
// and one bad value must not poison the whole sheet's bounds.
func cadFinite(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func cadNonNegative(value float64) float64 {
	value = cadFinite(value)
	if value < 0 {
		return 0
	}
	return value
}
