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

package cadsource

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	cad "github.com/unitedrhino/go-cad"
)

// Extract decodes DWG/DXF bytes and builds the CAD intermediate
// representation. It never invents data: fields go-cad cannot provide stay
// empty and are reported in Diagnostics.
func Extract(ctx context.Context, filename string, data []byte) (*Drawing, error) {
	return extract(ctx, filename, data, true)
}

// extract is the test seam for the entity-dump supplement. Production always
// enables the dump; disabling it exists only to test the base text path.
func extract(ctx context.Context, filename string, data []byte, includeEntityDump bool) (*Drawing, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	format, isDXF := detectFormat(filename)
	var (
		doc *cad.Document
		err error
	)
	if isDXF {
		doc, err = cad.ParseDXF(data)
	} else {
		doc, err = cad.Parse(data)
	}
	if err != nil {
		return nil, fmt.Errorf("cad: parse: %w", err)
	}

	drawing := &Drawing{
		Source: Source{
			Format:      format,
			Version:     doc.Version(),
			Codepage:    doc.Codepage,
			EntityCount: doc.EntityCount(),
			Skipped:     doc.Skipped(),
		},
		Diagnostics: Diagnostics{
			Counts: map[string]int{},
		},
	}

	buildSheets(drawing, doc)
	drawing.Texts = buildTexts(doc, drawing)
	ensureUnassignedSheet(drawing)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	drawing.Attributes = buildAttributes(doc)

	if includeEntityDump {
		applyEntityDump(ctx, drawing, doc)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	drawing.finalize()
	return drawing, nil
}

// detectFormat derives the source format from the filename. Anything that is
// not a DXF variant is parsed through go-cad's DWG decoder; the library
// reports an error for unsupported containers instead of guessing.
func detectFormat(filename string) (format string, isDXF bool) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	switch ext {
	case "dxf", "dxfb":
		return ext, true
	default:
		return ext, false
	}
}

func buildSheets(d *Drawing, doc *cad.Document) {
	detected := cad.DetectSheets(doc)
	if len(detected) == 0 || (len(detected) == 1 && detected[0].Name == "整图") {
		var bounds [4]float64
		if len(detected) == 1 {
			bounds = detected[0].Box
		}
		d.Sheets = []Sheet{{
			ID:     "s0",
			Index:  0,
			Name:   "整图",
			Bounds: bounds,
		}}
		d.Diagnostics.SheetDetection = "fallback"
		return
	}

	d.Diagnostics.SheetDetection = "detected"
	for i, sheet := range detected {
		d.Sheets = append(d.Sheets, Sheet{
			ID:     fmt.Sprintf("s%d", i+1),
			Index:  i + 1,
			Name:   sheet.Name,
			Bounds: sheet.Box,
		})
	}
}

func buildTexts(doc *cad.Document, d *Drawing) []Text {
	infos := cad.SheetTexts(doc)
	if len(infos) == 0 {
		// SheetTexts runs the tessellator; when it yields nothing (budget,
		// malformed geometry, or a text-only model the tessellator skipped)
		// fall back to the flat stream so the document is not silently empty.
		flat := doc.Texts()
		out := make([]Text, 0, len(flat))
		for _, info := range flat {
			text := strings.TrimSpace(info.Text)
			if text == "" {
				continue
			}
			out = append(out, Text{
				Text:       text,
				Kind:       KindUnknown,
				Origin:     OriginTexts,
				SheetID:    sheetIDForIndex(d, 0),
				Layer:      layerName(doc, info.Layer),
				X:          info.X,
				Y:          info.Y,
				Confidence: 0.6,
			})
		}
		if len(out) > 0 {
			d.addWarning("texts_fallback")
		}
		return out
	}

	out := make([]Text, 0, len(infos))
	for _, info := range infos {
		text := strings.TrimSpace(info.Text)
		if text == "" {
			continue
		}
		out = append(out, Text{
			Text:       text,
			Kind:       KindUnknown,
			Origin:     OriginSheetTexts,
			SheetID:    sheetIDForIndex(d, info.Sheet),
			Layer:      info.Layer,
			X:          info.X,
			Y:          info.Y,
			Height:     info.Height,
			Rotation:   info.Rotation,
			Confidence: 1,
		})
	}
	return out
}

// ensureUnassignedSheet materializes the synthetic sheet for texts that fall
// outside every detected frame, so every Text.SheetID resolves to a Sheet.
func ensureUnassignedSheet(d *Drawing) {
	for _, text := range d.Texts {
		if text.SheetID == "s0" {
			d.ensureSheet("s0", "未归属", 0)
			return
		}
	}
}

func buildAttributes(doc *cad.Document) []Attribute {
	out := make([]Attribute, 0, len(doc.Attribs))
	for _, attrib := range doc.Attribs {
		if attrib == nil {
			continue
		}
		tag := strings.TrimSpace(attrib.Tag)
		value := strings.TrimSpace(attrib.Text)
		if tag == "" && value == "" {
			continue
		}
		out = append(out, Attribute{
			Tag:         tag,
			Value:       value,
			OwnerHandle: attrib.Owner,
			Layer:       layerName(doc, attrib.Layer),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OwnerHandle != out[j].OwnerHandle {
			return out[i].OwnerHandle < out[j].OwnerHandle
		}
		if out[i].Tag != out[j].Tag {
			return out[i].Tag < out[j].Tag
		}
		return out[i].Value < out[j].Value
	})
	return out
}

func layerName(doc *cad.Document, handle uint64) string {
	if doc == nil || handle == 0 {
		return ""
	}
	if layer, ok := doc.LayerColors[handle]; ok {
		return layer.Name
	}
	return ""
}

func sheetIDForIndex(d *Drawing, index int) string {
	if index <= 0 {
		return "s0"
	}
	for _, sheet := range d.Sheets {
		if sheet.Index == index {
			return sheet.ID
		}
	}
	return "s0"
}

// ---------------------------------------------------------------------------
// Entity dump supplement
// ---------------------------------------------------------------------------

type dumpRow struct {
	Handle uint64         `json:"handle"`
	Type   string         `json:"type"`
	DXF    map[string]any `json:"dxf"`
}

func parseDumpRows(raw string) ([]dumpRow, error) {
	var rows []dumpRow
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func applyEntityDump(ctx context.Context, d *Drawing, doc *cad.Document) {
	if err := ctx.Err(); err != nil {
		return
	}
	raw, err := cad.DumpEntitiesDoc(doc)
	if err != nil {
		d.addWarning("entity_dump_failed")
		return
	}
	rows, err := parseDumpRows(raw)
	if err != nil {
		d.addWarning("entity_dump_failed")
		return
	}
	applyDumpRows(d, doc, rows)
}

func applyDumpRows(d *Drawing, doc *cad.Document, rows []dumpRow) {
	if d.Diagnostics.Counts == nil {
		d.Diagnostics.Counts = map[string]int{}
	}
	mleaderTexts := 0
	for _, row := range rows {
		typeName := strings.ToUpper(strings.TrimSpace(row.Type))
		layer := layerName(doc, layerHandle(row.DXF))

		switch {
		case strings.Contains(typeName, "DIMENSION"):
			applyDimensionRow(d, row, layer)
		case strings.Contains(typeName, "MLEADER") || strings.Contains(typeName, "MULTILEADER"):
			d.Diagnostics.Counts["MULTILEADER"]++
			if applyMLeaderRow(d, row, layer) {
				mleaderTexts++
			}
		case isTableRow(row):
			d.Diagnostics.Counts["ACAD_TABLE"]++
			d.Tables = append(d.Tables, Table{
				Handle: row.Handle,
				Status: "unsupported",
			})
		case strings.Contains(typeName, "IMAGE"):
			d.Diagnostics.Counts["IMAGE"]++
		case strings.Contains(typeName, "PROXY"):
			d.Diagnostics.Counts["PROXY_ENTITY"]++
		}
	}

	if d.Diagnostics.Counts["MULTILEADER"] > 0 && mleaderTexts == 0 {
		d.addWarning("mleader_text_unavailable")
	}
	if d.Diagnostics.Counts["ACAD_TABLE"] > 0 {
		d.addWarning("table_unsupported")
	}
	if d.Diagnostics.Counts["IMAGE"] > 0 {
		d.addWarning("image_entities")
	}
	if d.Diagnostics.Counts["PROXY_ENTITY"] > 0 {
		d.addWarning("proxy_entities")
	}
}

func applyDimensionRow(d *Drawing, row dumpRow, layer string) {
	d.Diagnostics.Counts["DIMENSION"]++
	dimension := Dimension{
		Handle:      row.Handle,
		Layer:       layer,
		UserText:    strings.TrimSpace(stringFromAny(row.DXF["user_text"])),
		Measurement: floatPtrFromAny(row.DXF["act_measurement"]),
		Position:    pointFromAny(row.DXF["text_midpt"]),
		Confidence:  0.7,
	}

	if !matchDimensionText(d, &dimension) && dimension.UserText != "" {
		x, y := 0.0, 0.0
		if dimension.Position != nil {
			x, y = dimension.Position.X, dimension.Position.Y
		}
		dimension.SheetID = sheetForPoint(d, dimension.Position)
		d.Texts = append(d.Texts, Text{
			Text:       dimension.UserText,
			Kind:       KindDimension,
			Origin:     OriginDump,
			SheetID:    dimension.SheetID,
			Layer:      layer,
			X:          x,
			Y:          y,
			Handle:     row.Handle,
			Confidence: 0.7,
		})
		dimension.RenderedText = dimension.UserText
		d.ensureSheet(dimension.SheetID, "未归属", 0)
	}

	d.Dimensions = append(d.Dimensions, dimension)
}

// matchDimensionText links a dimension record to the text the tessellator
// already extracted from its anonymous block. The link is positional: the
// dimension's text midpoint is the insertion point of that text.
func matchDimensionText(d *Drawing, dimension *Dimension) bool {
	if dimension.Position != nil {
		best := -1
		bestDistance := math.Inf(1)
		for i := range d.Texts {
			text := &d.Texts[i]
			if text.Kind == KindMLeader || text.Kind == KindAttrib {
				continue
			}
			distance := math.Hypot(text.X-dimension.Position.X, text.Y-dimension.Position.Y)
			limit := math.Max(2*text.Height, 1)
			if distance <= limit && distance < bestDistance {
				best = i
				bestDistance = distance
			}
		}
		if best >= 0 {
			text := &d.Texts[best]
			dimension.RenderedText = text.Text
			dimension.SheetID = text.SheetID
			text.Kind = KindDimension
			if text.Handle == 0 {
				text.Handle = dimension.Handle
			}
			return true
		}
	}

	if dimension.UserText == "" {
		return false
	}
	for i := range d.Texts {
		text := &d.Texts[i]
		if text.Text != dimension.UserText {
			continue
		}
		dimension.RenderedText = text.Text
		dimension.SheetID = text.SheetID
		text.Kind = KindDimension
		if text.Handle == 0 {
			text.Handle = dimension.Handle
		}
		return true
	}
	return false
}

func applyMLeaderRow(d *Drawing, row dumpRow, layer string) bool {
	text := strings.TrimSpace(stringFromAny(lookup(row.DXF, "ctx.content.txt.default_text")))
	if text == "" {
		return false
	}
	position := pointFromAny(lookup(row.DXF, "ctx.content.txt.location"))
	rotation, _ := floatFromAny(lookup(row.DXF, "ctx.content.txt.rotation"))
	height, _ := floatFromAny(lookup(row.DXF, "ctx.content.txt.height"))

	x, y := 0.0, 0.0
	if position != nil {
		x, y = position.X, position.Y
	}
	sheetID := sheetForPoint(d, position)
	d.Texts = append(d.Texts, Text{
		Text:       text,
		Kind:       KindMLeader,
		Origin:     OriginDump,
		SheetID:    sheetID,
		Layer:      layer,
		X:          x,
		Y:          y,
		Height:     height,
		Rotation:   rotation,
		Handle:     row.Handle,
		Confidence: 0.7,
	})
	d.ensureSheet(sheetID, "未归属", 0)
	return true
}

func isTableRow(row dumpRow) bool {
	if row.DXF == nil {
		return false
	}
	if strings.EqualFold(stringFromAny(row.DXF["dxfname"]), "ACAD_TABLE") {
		return true
	}
	return strings.Contains(strings.ToUpper(row.Type), "TABLE")
}

func layerHandle(dxf map[string]any) uint64 {
	if dxf == nil {
		return 0
	}
	if handle := uint64FromAny(dxf["layer_handle"]); handle != 0 {
		return handle
	}
	return uint64FromAny(dxf["layer"])
}

func sheetForPoint(d *Drawing, point *Point) string {
	if d == nil {
		return ""
	}
	if d.Diagnostics.SheetDetection == "fallback" {
		return "s0"
	}
	if point != nil {
		for _, sheet := range d.Sheets {
			if sheet.Index == 0 {
				continue
			}
			bounds := sheet.Bounds
			if point.X >= bounds[0] && point.X <= bounds[2] && point.Y >= bounds[1] && point.Y <= bounds[3] {
				return sheet.ID
			}
		}
	}
	for _, sheet := range d.Sheets {
		if sheet.ID == "s0" {
			return "s0"
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Finalization
// ---------------------------------------------------------------------------

func (d *Drawing) finalize() {
	if d.Diagnostics.Counts == nil {
		d.Diagnostics.Counts = map[string]int{}
	}
	d.sortTexts()
	d.sortDimensions()

	if len(d.Texts) == 0 {
		d.addWarning("zero_text")
		if d.Source.EntityCount > 0 {
			d.addWarning("paper_space_suspected")
		}
	}
	if d.Source.Skipped > 0 {
		d.addWarning("decode_skipped")
	}
	if d.Source.EntityCount > 50 && len(d.Texts) < 5 {
		d.addWarning("low_coverage")
	}
}

func (d *Drawing) sortTexts() {
	order := make(map[string]int, len(d.Sheets))
	for _, sheet := range d.Sheets {
		if sheet.Index == 0 {
			order[sheet.ID] = math.MaxInt
			continue
		}
		order[sheet.ID] = sheet.Index
	}
	orderOf := func(sheetID string) int {
		if position, ok := order[sheetID]; ok {
			return position
		}
		return math.MaxInt
	}
	sort.SliceStable(d.Texts, func(i, j int) bool {
		left, right := d.Texts[i], d.Texts[j]
		if orderOf(left.SheetID) != orderOf(right.SheetID) {
			return orderOf(left.SheetID) < orderOf(right.SheetID)
		}
		if left.Y != right.Y {
			return left.Y > right.Y
		}
		if left.X != right.X {
			return left.X < right.X
		}
		return left.Text < right.Text
	})
}

func (d *Drawing) sortDimensions() {
	sort.SliceStable(d.Dimensions, func(i, j int) bool {
		if d.Dimensions[i].SheetID != d.Dimensions[j].SheetID {
			return d.Dimensions[i].SheetID < d.Dimensions[j].SheetID
		}
		return d.Dimensions[i].Handle < d.Dimensions[j].Handle
	})
}

// ---------------------------------------------------------------------------
// Scalar helpers for the entity dump
// ---------------------------------------------------------------------------

func lookup(m map[string]any, path string) any {
	if m == nil {
		return nil
	}
	// DumpEntities flattens nested entity fields into dotted keys, so try the
	// direct key before walking a nested map.
	if value, ok := m[path]; ok {
		return value
	}
	var current any = m
	for _, part := range strings.Split(path, ".") {
		next, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = next[part]
	}
	return current
}

func pointFromAny(value any) *Point {
	switch typed := value.(type) {
	case map[string]any:
		x, okX := floatFromAny(typed["X"])
		if !okX {
			x, okX = floatFromAny(typed["x"])
		}
		y, okY := floatFromAny(typed["Y"])
		if !okY {
			y, okY = floatFromAny(typed["y"])
		}
		if !okX || !okY {
			return nil
		}
		z, _ := floatFromAny(typed["Z"])
		if _, ok := typed["Z"]; !ok {
			z, _ = floatFromAny(typed["z"])
		}
		return &Point{X: x, Y: y, Z: z}
	case []any:
		if len(typed) < 2 {
			return nil
		}
		x, okX := floatFromAny(typed[0])
		y, okY := floatFromAny(typed[1])
		if !okX || !okY {
			return nil
		}
		z := 0.0
		if len(typed) > 2 {
			z, _ = floatFromAny(typed[2])
		}
		return &Point{X: x, Y: y, Z: z}
	default:
		return nil
	}
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func floatFromAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func floatPtrFromAny(value any) *float64 {
	parsed, ok := floatFromAny(value)
	if !ok {
		return nil
	}
	return &parsed
}

func uint64FromAny(value any) uint64 {
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return 0
		}
		return uint64(typed)
	case int:
		if typed < 0 {
			return 0
		}
		return uint64(typed)
	case int64:
		if typed < 0 {
			return 0
		}
		return uint64(typed)
	case json.Number:
		parsed, err := strconv.ParseUint(typed.String(), 10, 64)
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}
