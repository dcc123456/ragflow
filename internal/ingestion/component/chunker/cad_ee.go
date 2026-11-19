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
	Kind           cadChunkKind
	SheetID        string
	Layout         string
	Space          string
	RegionID       string
	SubjectID      string
	Content        string
	Context        string
	Evidence       []cadEvidence
	SourceIDs      []string
	Sources        []cadSourceFact
	ContextSources []cadSourceFact
	Keywords       []string
	Bounds         [4]float64
	SheetBounds    [4]float64
	Viewport       [4]float64
}

// cadSourceFact is one source text's stable provenance carried into the
// persisted chunk extra. It never replaces SourceIDs; it adds the fields the
// raw text stream exposes (kind/origin/layer/rotation/native bounds).
type cadSourceFact struct {
	ID       string     `json:"id"`
	Handle   uint64     `json:"handle,omitempty"`
	Kind     string     `json:"kind,omitempty"`
	Origin   string     `json:"origin,omitempty"`
	Layer    string     `json:"layer,omitempty"`
	Rotation float64    `json:"rotation,omitempty"`
	Bounds   [4]float64 `json:"bounds,omitzero"`
}

// cadDiagnostic is a coverage or quality signal reported through run progress.
type cadDiagnostic struct {
	Code   string
	Detail string
	Count  int
}

const (
	// Attribute chunks are independent of drawing geometry; these budgets keep
	// one block-attribute owner from becoming an unbounded chunk.
	cadAttributeMaxLines = 100
	cadAttributeMaxChars = 1200
	// cadChunkOversizeRunes warns when a block has no legal ink gutter and
	// grows past the point where embedding is likely to truncate it. It is a
	// diagnostic only: the block is never split through ink.
	cadChunkOversizeRunes = 12000
)

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
)

var (
	// cadTitleBlockLabels are the title-block field names the classifier looks
	// for. The list is derived from cadTitleFields so classification and
	// metadata extraction cannot drift apart.
	cadTitleBlockLabels = cadTitleFieldLabelList()

	// cadNotesHeaders mark the technical-notes region. A header is searched
	// across the ordered region and the header plus the lines below it are
	// split out as a notes chunk.
	cadNotesHeaders = []string{
		"技术要求", "技术条件", "技术说明", "施工说明", "设计说明", "说明",
		"注：", "注:", "注意",
		"notes", "general notes", "technical requirements",
	}
)

// decodeCADDrawing reads the parser's structured payload without touching the
// source binary. It reports payload problems through run progress.
func decodeCADDrawing(ctx context.Context, upstream schema.ChunkerFromUpstream) (*cadsource.Drawing, error) {
	raw, ok := cadPayload(upstream)
	if !ok {
		runtime.ReportProgressMessage(ctx, "CADChunker",
			"WARNING: cad_payload_missing: falling back to generic text chunking")
		return nil, nil
	}

	var doc cadsource.Drawing
	if err := json.Unmarshal(raw, &doc); err != nil {
		runtime.ReportProgressMessage(ctx, "CADChunker",
			"WARNING: cad_payload_invalid: falling back to generic text chunking: "+err.Error())
		return nil, nil
	}
	return &doc, nil
}

// chunkCADDrawing builds chunks for an already decoded drawing. ink is the
// rendered whitespace index; nil keeps one block per sheet rather than cutting
// on text counts.
func chunkCADDrawing(ctx context.Context, upstream schema.ChunkerFromUpstream, doc *cadsource.Drawing, ink *cadInkIndex) (map[string]any, *cadsource.Drawing, error) {
	chunks, diagnostics := buildCADChunks(doc, ink, cadDocumentName(upstream))
	for _, diagnostic := range diagnostics {
		runtime.ReportProgressMessage(ctx, "CADChunker", cadDiagnosticMessage(diagnostic))
	}
	docs, err := cadChunkDocs(chunks)
	if err != nil {
		runtime.ReportProgressMessage(ctx, "CADChunker", cadDiagnosticMessage(cadDiagnostic{Code: "cad_metadata_serialize_failed", Detail: err.Error()}))
		return nil, doc, fmt.Errorf("CADChunker: serialize provenance: %w", err)
	}
	return chunkOutputs(docs), doc, nil
}

// cadPayload returns the structured CAD text the parser put on File["cad"].
// Rendering needs the go-cad document, which is not part of that payload, so
// the preview path parses the source binary again from storage.
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
	return name
}

// buildCADChunks turns a parsed drawing into spatial chunks plus one
// attributes chunk per attribute owner. It returns the chunker-internal chunks
// and the diagnostics the caller reports to the run.
func buildCADChunks(doc *cadsource.Drawing, ink *cadInkIndex, docName string) ([]cadChunk, []cadDiagnostic) {
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
		sheetChunks, sheetDiagnostics, next := cadSheetChunks(group.sheet, group.texts, ink, docName, sequence)
		chunks = append(chunks, sheetChunks...)
		diagnostics = append(diagnostics, sheetDiagnostics...)
		sequence = next
	}
	attributeChunks, attributeDiagnostics := cadAttributeChunks(doc, docName)
	chunks = append(chunks, attributeChunks...)
	diagnostics = append(diagnostics, attributeDiagnostics...)
	chunks, diagnostics = cadValidateAndEmit(chunks, diagnostics, docName)

	emittedSources := 0
	for _, chunk := range chunks {
		emittedSources += len(chunk.SourceIDs)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "cad_sheet_grid_excluded" {
			expectedSources -= diagnostic.Count
		}
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
		chunks = append(chunks, cadGeometrySummaryChunks(doc, docName)...)
	}
	return chunks, diagnostics
}

// cadGeometrySummaryMaxDims caps the dimension lines of one geometry summary
// chunk; the remainder is reported as a count so the chunk stays bounded.
const cadGeometrySummaryMaxDims = 20

// cadGeometrySummaryChunks emits one region chunk per sheet when a drawing
// carries geometry but no searchable text (pure 3D solids, symbol-only
// sheets). The summary reports only decoded facts — entity counts, the sheet
// frame, dimension measurements, unsupported-table and special-entity
// counts — so a geometry-only drawing stays retrievable instead of
// producing zero chunks. Drawings with no decoded geometry at all return
// nil and keep the zero-chunk warning path in the component.
func cadGeometrySummaryChunks(doc *cadsource.Drawing, docName string) []cadChunk {
	if doc == nil || (doc.Source.EntityCount <= 0 && doc.Source.Skipped <= 0) {
		return nil
	}
	sheets := doc.Sheets
	if len(sheets) == 0 {
		sheets = []cadsource.Sheet{{ID: "s0", Name: "整图", Layout: "model", Space: "model"}}
	}
	var chunks []cadChunk
	for i, sheet := range sheets {
		sheetID := strings.TrimSpace(sheet.ID)
		if sheetID == "" {
			sheetID = "s0"
		}
		regionID := fmt.Sprintf("G%d", i+1)
		content := cadGeometrySummaryContent(doc, sheet)
		if strings.TrimSpace(content) == "" {
			continue
		}
		chunks = append(chunks, cadChunk{
			Kind:        cadKindRegion,
			SheetID:     sheetID,
			Layout:      strings.TrimSpace(sheet.Layout),
			Space:       strings.TrimSpace(sheet.Space),
			RegionID:    regionID,
			SubjectID:   fmt.Sprintf("%s:%s:%s", cadKindRegion, sheetID, regionID),
			Content:     content,
			Context:     cadHeader(docName, sheet, regionID, cadKindRegion),
			Evidence:    []cadEvidence{{Level: cadEvidenceDerived, Source: "layout", Method: "geometry_summary"}},
			Bounds:      sheet.Bounds,
			SheetBounds: sheet.Bounds,
			Viewport:    sheet.Viewport,
		})
	}
	return chunks
}

// cadGeometrySummaryCountNames maps entity-dump counter keys to retrievable
// labels. DIMENSION and ACAD_TABLE are reported from the decoded lists
// instead, so they are skipped here to avoid double counting.
var cadGeometrySummaryCountNames = map[string]string{
	"MULTILEADER":  "multileaders",
	"IMAGE":        "images",
	"PROXY_ENTITY": "proxy entities",
}

// cadGeometrySummaryContent renders the retrievable fact lines for one sheet
// of a textless drawing. Every line comes from the decoded intermediate
// representation; nothing is inferred about engineering intent.
func cadGeometrySummaryContent(doc *cadsource.Drawing, sheet cadsource.Sheet) string {
	lines := []string{"geometry summary (no text annotations in this sheet)"}
	lines = append(lines, fmt.Sprintf("entities: %d (%s %s); skipped during decode: %d",
		doc.Source.EntityCount, doc.Source.Format, doc.Source.Version, doc.Source.Skipped))
	if name := strings.TrimSpace(sheet.Name); name != "" {
		lines = append(lines, "sheet: "+name)
	}
	if cadValidBounds(sheet.Bounds) {
		lines = append(lines, "sheet bounds: ["+cadFormatNumbers(sheet.Bounds[:])+"]")
	}
	sheetID := strings.TrimSpace(sheet.ID)
	var dims []cadsource.Dimension
	for _, dim := range doc.Dimensions {
		if dim.SheetID != "" && sheetID != "" && dim.SheetID != sheetID {
			continue
		}
		dims = append(dims, dim)
	}
	for i, dim := range dims {
		if i >= cadGeometrySummaryMaxDims {
			lines = append(lines, fmt.Sprintf("dimensions: +%d more", len(dims)-i))
			break
		}
		part := "dimension"
		if dim.Measurement != nil {
			part += fmt.Sprintf(": %v", *dim.Measurement)
		} else if text := strings.TrimSpace(dim.UserText); text != "" {
			part += ": " + text
		}
		if layer := strings.TrimSpace(dim.Layer); layer != "" {
			part += " (layer " + layer + ")"
		}
		lines = append(lines, part)
	}
	if len(doc.Tables) > 0 {
		lines = append(lines, fmt.Sprintf("tables: %d (content not extracted)", len(doc.Tables)))
	}
	keys := make([]string, 0, len(doc.Diagnostics.Counts))
	for key, count := range doc.Diagnostics.Counts {
		if count <= 0 || key == "DIMENSION" || key == "ACAD_TABLE" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		label, ok := cadGeometrySummaryCountNames[key]
		if !ok {
			label = strings.ToLower(strings.ReplaceAll(key, "_", " "))
		}
		lines = append(lines, fmt.Sprintf("%s: %d", label, doc.Diagnostics.Counts[key]))
	}
	return strings.Join(lines, "\n")
}

// cadFormatNumbers renders world coordinates compactly for summary lines.
func cadFormatNumbers(values []float64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%v", value))
	}
	return strings.Join(parts, ", ")
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

// cadSheetChunks clusters one sheet's texts at rendered whitespace gutters.
// Without an ink index the sheet stays one block; text counts never invent a
// spatial boundary.
func cadSheetChunks(sheet cadsource.Sheet, raw []cadsource.Text, ink *cadInkIndex,
	docName string, sequence int) ([]cadChunk, []cadDiagnostic, int) {
	items := make([]cadTextItem, 0, len(raw))
	for _, text := range raw {
		trimmed := strings.TrimSpace(text.Text)
		if trimmed == "" {
			continue
		}
		var nativeBounds *cadBounds
		b := text.Bounds
		if b[2] >= b[0] && b[3] >= b[1] && (b[2] > b[0] || b[3] > b[1]) {
			nativeBounds = &cadBounds{minX: b[0], minY: b[1], maxX: b[2], maxY: b[3]}
		}
		items = append(items, cadTextItem{
			bounds:   nativeBounds,
			text:     trimmed,
			x:        cadFinite(text.X),
			y:        cadFinite(text.Y),
			height:   cadNonNegative(text.Height),
			handle:   text.Handle,
			kind:     text.Kind,
			origin:   text.Origin,
			layer:    text.Layer,
			rotation: text.Rotation,
		})
	}
	if len(items) == 0 {
		return nil, nil, sequence
	}
	sortCADTextItems(items)
	for i := range items {
		items[i].sourceID = fmt.Sprintf("text:%06d", sequence)
		items[i].sourceIDs = []string{items[i].sourceID}
		items[i].facts = []cadSourceFact{{
			ID:       items[i].sourceID,
			Handle:   items[i].handle,
			Kind:     string(items[i].kind),
			Origin:   string(items[i].origin),
			Layer:    items[i].layer,
			Rotation: items[i].rotation,
			Bounds:   sourceFactBounds(items[i]),
		}}
		sequence++
	}
	sheet, geometryDiagnostics := cadRepairSheetGeometry(sheet, items)
	items, excluded := cadFilterFrameGrid(items, sheet)
	items, legend := cadJoinLegendRows(items)
	chunks, diagnostics := cadSheetChunksFromItems(sheet, items, ink, docName)
	diagnostics = append(geometryDiagnostics, diagnostics...)
	cadAddLegendContext(chunks, legend)
	if excluded > 0 {
		diagnostics = append(diagnostics, cadDiagnostic{Code: "cad_sheet_grid_excluded", Detail: sheet.ID, Count: excluded})
	}
	return chunks, diagnostics, sequence
}

// cadSheetChunksFromItems partitions one sheet at rendered whitespace gutters.
// A sheet without a legal gutter stays one chunk, even when it is large; text
// counts and character budgets never decide a spatial boundary.
func cadSheetChunksFromItems(sheet cadsource.Sheet, items []cadTextItem, ink *cadInkIndex, docName string) ([]cadChunk, []cadDiagnostic) {
	scale, exact := cadTextScale(items)
	if scale <= 0 {
		scale = 1
	}
	var diagnostics []cadDiagnostic
	if !exact {
		diagnostics = append(diagnostics, cadDiagnostic{
			Code: "cad_scale_unreliable", Detail: sheetRef(sheet),
		})
	}

	allBoxes := cadItemsToBoxes(items, scale)
	bounds := cadBoundsForSheet(sheet, allBoxes)

	items, titleParts := cadExtractTitleFields(items, bounds)
	page := ink.pageForSheet(sheet)
	titleChunks := cadTitleChunks(docName, sheet, titleParts, page, scale)
	if len(items) == 0 {
		return titleChunks, diagnostics
	}
	if page == nil {
		diagnostics = append(diagnostics, cadDiagnostic{
			Code: "cad_ink_unavailable", Detail: sheetRef(sheet),
		})
		chunks := cadSerializedChunks(docName, sheet, items, scale)
		return append(chunks, titleChunks...), diagnostics
	}

	parts := splitCADInkGutters(sheet, items, scale, page)
	parts = mergeCADLeafParts(parts, page, sheet, scale)
	chunks := make([]cadChunk, 0, len(parts))
	for i, part := range parts {
		chunks = append(chunks, cadChunksFromPart(docName, sheet, fmt.Sprintf("R%d", i+1), part, page, scale)...)
	}
	return append(chunks, titleChunks...), diagnostics
}

// cadSerializedChunks is the no-ink path: one block per sheet, with notes split
// only when their own header establishes the boundary.
func cadSerializedChunks(docName string, sheet cadsource.Sheet, items []cadTextItem, scale float64) []cadChunk {
	boxes := cadItemsToBoxes(items, scale)
	return cadChunksFromPart(docName, sheet, "R1", cadInkPart{boxes: boxes, region: cadRegionForBoxes(boxes, scale)}, nil, scale)
}

func cadChunksFromPart(docName string, sheet cadsource.Sheet, baseID string, part cadInkPart, page *cadInkPage, scale float64) []cadChunk {
	before, notes, ok := cadSplitNotesPart(part.boxes)
	if !ok {
		return []cadChunk{cadChunkFromBoxes(
			docName, sheet, baseID, cadKindRegion,
			[]cadEvidence{{Level: cadEvidenceDerived, Source: "text_layout", Method: "ink_gutter"}},
			part.boxes, cadInkChunkBounds(part, page, scale),
		)}
	}
	var chunks []cadChunk
	if len(before) > 0 {
		chunks = append(chunks, cadChunkFromBoxes(
			docName, sheet, baseID, cadKindRegion,
			[]cadEvidence{{Level: cadEvidenceDerived, Source: "text_layout", Method: "ink_gutter"}},
			before, cadInkChunkBounds(cadInkPart{boxes: before, region: cadRegionForBoxes(before, scale)}, page, scale),
		))
		baseID += "N"
	}
	chunks = append(chunks, cadChunkFromBoxes(
		docName, sheet, baseID, cadKindNotes,
		[]cadEvidence{{Level: cadEvidenceDerived, Source: "layout", Method: "notes"}},
		notes, cadLocalTextBounds(notes, page),
	))
	return chunks
}

func cadRegionForBoxes(boxes []cadTextBox, scale float64) cadBounds {
	b := cadBoxesBounds(boxes)
	pad := math.Max(2*scale, 0.005*math.Max(b.width(), b.height()))
	return cadBounds{minX: b.minX - pad, minY: b.minY - pad, maxX: b.maxX + pad, maxY: b.maxY + pad}
}

// cadTextItem holds a text's content, position, scale, and source identity.
type cadTextItem struct {
	text      string
	x         float64
	y         float64
	height    float64
	handle    uint64
	kind      cadsource.Kind
	origin    cadsource.Origin
	layer     string
	rotation  float64
	sourceID  string
	sourceIDs []string
	facts     []cadSourceFact
	legendRow bool
	bounds    *cadBounds
}

// cadTextBox is a text's estimated world-space rectangle. The insertion point
// is the baseline-left corner for AutoCAD TEXT, so the box extends right and up.
type cadTextBox struct {
	item                   cadTextItem
	minX, maxX, minY, maxY float64
}

func cadTextBoxFor(item cadTextItem, height float64) cadTextBox {
	if item.bounds != nil {
		b := item.bounds
		return cadTextBox{item: item, minX: b.minX, minY: b.minY, maxX: b.maxX, maxY: b.maxY}
	}
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
// that is unreliable it returns 0 and the caller keeps the sheet whole.
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

// cadChunkFromBoxes serializes one complete block. bounds is the ink-aware crop;
// text provenance stays on each source item.
func cadChunkFromBoxes(docName string, sheet cadsource.Sheet, regionID string, kind cadChunkKind,
	evidence []cadEvidence, boxes []cadTextBox, bounds [4]float64) cadChunk {
	items := cadBoxesToItems(boxes)
	sortCADTextItems(items)
	sourceIDs := make([]string, 0, len(items))
	for _, item := range items {
		if len(item.sourceIDs) > 0 {
			sourceIDs = append(sourceIDs, item.sourceIDs...)
		} else {
			sourceIDs = append(sourceIDs, item.sourceID)
		}
	}
	if !cadValidBounds(bounds) {
		bounds = cadItemsBounds(items)
	}
	if cadValidBounds(sheet.Viewport) {
		bounds = clampCADBounds(bounds, sheet.Viewport)
	}
	var content string
	if kind == cadKindRegion {
		content = cadLayoutContent(boxes, 0)
	} else {
		lines := make([]string, len(items))
		for i, item := range items {
			lines[i] = item.text
		}
		content = strings.Join(lines, "\n")
	}
	return cadChunk{
		Kind:        kind,
		SheetID:     sheetRef(sheet),
		Layout:      sheet.Layout,
		Space:       sheet.Space,
		RegionID:    regionID,
		SubjectID:   fmt.Sprintf("%s:%s:%s", kind, sheetRef(sheet), regionID),
		Content:     content,
		Context:     cadHeader(docName, sheet, regionID, kind),
		SourceIDs:   sourceIDs,
		Sources:     cadItemFacts(items),
		Bounds:      bounds,
		SheetBounds: sheet.Bounds,
		Viewport:    sheet.Viewport,
		Evidence:    evidence,
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
func cadAttributeChunks(doc *cadsource.Drawing, docName string) ([]cadChunk, []cadDiagnostic) {
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
		if tag == "" || value == "" || (attribute.OwnerHandle == 0 && strings.EqualFold(tag, "ITEM") && strings.EqualFold(value, "XXX")) {
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
		budgetGroups := cadSplitAttributeLines(lines, cadAttributeMaxLines, cadAttributeMaxChars)
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

// cadSplitAttributeLines cuts one attribute owner's lines into bounded
// attribute chunks; block attributes have no drawing geometry to preserve.
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
		if runes := utf8.RuneCountInString(chunk.Content); runes > cadChunkOversizeRunes {
			diagnostics = append(diagnostics, cadDiagnostic{
				Code: "cad_chunk_oversize", Detail: chunk.SubjectID, Count: runes,
			})
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

func cadChunkDocs(chunks []cadChunk) ([]schema.ChunkDoc, error) {
	documentMeta, sheetMeta := cadChunkMetadata(chunks)
	docs := make([]schema.ChunkDoc, 0, len(chunks))
	for _, chunk := range chunks {
		doc := schema.ChunkDoc{Text: chunk.Content, DocType: "text", ContextAbove: chunk.Context, Keywords: strings.Join(chunk.Keywords, ",")}
		// extra is a JSON string in the chunk-store schema (ES text / Infinity
		// varchar). Keep CAD world coordinates inside that string, not PDF positions.
		cad := map[string]any{
			"kind": chunk.Kind, "sheet_id": chunk.SheetID, "region_id": chunk.RegionID,
			"bbox": chunk.Bounds, "sheet_bounds": chunk.SheetBounds, "viewport": chunk.Viewport, "source_ids": chunk.SourceIDs,
		}
		meta := sheetMeta[chunk.SheetID]
		// Attribute chunks carry no sheet id. When a drawing has exactly one
		// title block, its sheet-local fields describe that drawing; do not
		// copy them onto chunks that belong to a different sheet.
		if len(meta) == 0 && chunk.SheetID == "" && len(sheetMeta) == 1 {
			for _, single := range sheetMeta {
				meta = single
			}
		}
		for _, key := range []string{"scale", "sheet", "revision"} {
			if value := meta[key]; value != "" {
				cad[key] = value
			}
		}
		if chunk.Layout != "" {
			cad["layout_id"] = chunk.Layout
		}
		if chunk.Space != "" {
			cad["space"] = chunk.Space
		}
		if len(chunk.Sources) > 0 {
			cad["sources"] = chunk.Sources
		}
		if len(chunk.ContextSources) > 0 {
			cad["context_sources"] = chunk.ContextSources
		}
		raw, err := json.Marshal(map[string]any{"cad": cad})
		if err != nil {
			return nil, fmt.Errorf("sheet=%s region=%s: %w", chunk.SheetID, chunk.RegionID, err)
		}
		serialized, err := json.Marshal(string(raw))
		if err != nil {
			return nil, fmt.Errorf("sheet=%s region=%s extra: %w", chunk.SheetID, chunk.RegionID, err)
		}
		doc.Extra = map[string]json.RawMessage{"extra": serialized}
		if chunk.Kind == cadKindTitleBlock && len(documentMeta) > 0 {
			rawMeta, err := json.Marshal(documentMeta)
			if err != nil {
				return nil, fmt.Errorf("sheet=%s region=%s metadata: %w", chunk.SheetID, chunk.RegionID, err)
			}
			doc.Extra["metadata"] = rawMeta
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// --- spatial partitioning -------------------------------------------------

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

// mergeCADParts folds an undersized part into its nearest neighbour when the
// gap is within maxGap. A far part stays alone even when it is a single text:
// a valid annotation must not enlarge an unrelated region's bbox.
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

func cadTitleBlockLabelHits(part []cadTextBox) int {
	hits := 0
	for _, label := range cadTitleBlockLabels {
		for _, box := range part {
			if cadMatchesFieldLabel(box.item.text, label) {
				hits++
				break
			}
		}
	}
	return hits
}

// cadSplitNotesPart finds a notes header anywhere in a region and splits the
// notes off as their own chunk. The header must have at least one line after
// it; a table above the notes stays in the preceding region.
func cadSplitNotesPart(boxes []cadTextBox) (before, notes []cadTextBox, ok bool) {
	ordered := append([]cadTextBox(nil), boxes...)
	sort.SliceStable(ordered, func(i, j int) bool { return cadTextItemLess(ordered[i].item, ordered[j].item) })
	for i, header := range ordered {
		if !cadIsNotesHeader(header.item.text) {
			continue
		}
		height := math.Max(header.item.height, header.maxY-header.minY)
		if height <= 0 {
			height = 1
		}
		lastY := header.item.y
		ended := false
		for j, box := range ordered {
			inColumn := math.Abs(box.item.x-header.item.x) <= 4*height
			inNotes := j >= i && inColumn && !ended
			if inNotes && j > i && (lastY-box.item.y > 4*height || cadTitleBlockLabelHits([]cadTextBox{box}) > 0 || strings.HasPrefix(strings.ToUpper(strings.TrimSpace(box.item.text)), "SECTION ")) {
				ended = true
				inNotes = false
			}
			if inNotes {
				notes = append(notes, box)
				lastY = box.item.y
			} else {
				before = append(before, box)
			}
		}
		if len(notes) > 1 || strings.Contains(header.item.text, "\n") {
			return before, notes, true
		}
		return nil, nil, false
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

func (b cadBounds) valid() bool {
	for _, v := range []float64{b.minX, b.minY, b.maxX, b.maxY, b.width(), b.height()} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return b.maxX > b.minX && b.maxY > b.minY
}

func cadValidBounds(box [4]float64) bool {
	return (cadBounds{minX: box[0], minY: box[1], maxX: box[2], maxY: box[3]}).valid()
}

// cadRepairSheetGeometry validates whole rectangles before spatial processing.
// Missing or invalid geometry uses finite text extents; zero remains the
// unknown sentinel when no usable extent exists. Repairs are not frame evidence.
func cadRepairSheetGeometry(sheet cadsource.Sheet, items []cadTextItem) (cadsource.Sheet, []cadDiagnostic) {
	var diagnostics []cadDiagnostic
	var boxes []cadTextBox
	for _, item := range items {
		box := cadTextBoxFor(item, 1)
		if (cadBounds{minX: box.minX, minY: box.minY, maxX: box.maxX, maxY: box.maxY}).valid() {
			boxes = append(boxes, box)
		}
	}
	b := cadBoxesBounds(boxes)
	var derived [4]float64
	if b.valid() {
		derived = [4]float64{b.minX, b.minY, b.maxX, b.maxY}
	}
	if !cadValidBounds(sheet.Bounds) {
		if sheet.Bounds != [4]float64{} {
			diagnostics = append(diagnostics, cadDiagnostic{Code: "cad_sheet_bounds_invalid", Detail: sheetRef(sheet)})
		}
		sheet.Bounds = derived
		sheet.Source = "text_extents"
	}
	if !cadValidBounds(sheet.Viewport) {
		if sheet.Viewport != [4]float64{} {
			diagnostics = append(diagnostics, cadDiagnostic{Code: "cad_viewport_invalid", Detail: sheetRef(sheet)})
		}
		sheet.Viewport = sheet.Bounds
		if cadValidBounds(derived) && cadValidBounds(sheet.Bounds) {
			v := [4]float64{math.Min(sheet.Bounds[0], derived[0]), math.Min(sheet.Bounds[1], derived[1]), math.Max(sheet.Bounds[2], derived[2]), math.Max(sheet.Bounds[3], derived[3])}
			if cadValidBounds(v) {
				sheet.Viewport = v
			}
		}
	}
	return sheet, diagnostics
}

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
		minX: sheet.Bounds[0],
		minY: sheet.Bounds[1],
		maxX: sheet.Bounds[2],
		maxY: sheet.Bounds[3],
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

// cadMatchesFieldLabel reports whether text starts with label and continues
// with a field separator. CAD title blocks often append punctuation to the
// label ("DWG NO.", "SCALE:"), so the separator set includes periods and
// full-width punctuation.
func cadMatchesFieldLabel(text, label string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" || !strings.HasPrefix(text, label) {
		return false
	}
	rest := text[len(label):]
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	switch r {
	case ':', '：', ' ', '\t', '\n', '.', '。', '、':
		return true
	}
	return false
}

func cadItemsBounds(items []cadTextItem) [4]float64 {
	if len(items) == 0 {
		return [4]float64{}
	}
	boxes := make([]cadTextBox, len(items))
	for i, item := range items {
		boxes[i] = cadTextBoxFor(item, 1)
	}
	b := cadBoxesBounds(boxes)
	return [4]float64{b.minX, b.minY, b.maxX, b.maxY}
}

// sourceFactBounds keeps a text's native extent for provenance even when the
// text item has no explicit bounds (legacy synthetic items).
func sourceFactBounds(item cadTextItem) [4]float64 {
	if item.bounds != nil {
		return [4]float64{item.bounds.minX, item.bounds.minY, item.bounds.maxX, item.bounds.maxY}
	}
	b := cadTextBoxFor(item, 1)
	return [4]float64{b.minX, b.minY, b.maxX, b.maxY}
}

// cadItemFacts flattens the source facts of all items in one chunk. Legend
// rows carry two facts (tag and description), so deduplication is by ID.
func cadItemFacts(items []cadTextItem) []cadSourceFact {
	var out []cadSourceFact
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		facts := item.facts
		if len(facts) == 0 && item.sourceID != "" {
			facts = []cadSourceFact{{
				ID:       item.sourceID,
				Handle:   item.handle,
				Kind:     string(item.kind),
				Origin:   string(item.origin),
				Layer:    item.layer,
				Rotation: item.rotation,
				Bounds:   sourceFactBounds(item),
			}}
		}
		for _, fact := range facts {
			if fact.ID == "" || seen[fact.ID] {
				continue
			}
			seen[fact.ID] = true
			out = append(out, fact)
		}
	}
	return out
}
