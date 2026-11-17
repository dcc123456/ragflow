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

// Package cadsource turns DWG/DXF bytes into the parser's CAD intermediate
// representation. It is the only package in RAGFlow that imports go-cad, so a
// future engine swap replaces one adapter instead of touching the parser or
// the CAD chunker.
//
// The model records facts, not engineering semantics: text, position, layer,
// sheet attribution, block attribute tags, dimension overrides and the
// diagnostics for what the library could not extract. Title-block schemas,
// callout parsing and BOM assembly belong to the semantic layer downstream.
package cadsource

// Kind identifies the source entity family of one drawing text.
type Kind string

const (
	KindAttrib    Kind = "attrib"
	KindDimension Kind = "dimension"
	KindMLeader   Kind = "mleader"
	KindUnknown   Kind = "unknown"
)

// Origin records which extraction path produced a fact.
type Origin string

const (
	// OriginSheetTexts is the world-coordinate text stream built from
	// sheet-scoped tessellation (INSERT transforms, attributes, dimension
	// anonymous blocks).
	OriginSheetTexts Origin = "sheet_texts"
	// OriginTexts is the legacy flat text stream, used only as a fallback.
	OriginTexts Origin = "texts"
	// OriginDump is the entity dump path used for fields the text stream
	// does not expose (dimension overrides, MLEADER content, table counts).
	OriginDump Origin = "dump"
	// OriginAttrib is a block attribute value taken from the document's
	// attribute map rather than from the text stream.
	OriginAttrib Origin = "attrib"
)

// Source is the document-level metadata taken from the decoded drawing.
type Source struct {
	Format      string `json:"format"` // dwg / dxf / dxfb
	Version     string `json:"version,omitempty"`
	Codepage    uint16 `json:"codepage,omitempty"`
	EntityCount int    `json:"entity_count"`
	Skipped     int    `json:"skipped"`
}

// Sheet is one detected sheet frame or the fallback whole-drawing sheet.
// Layout identifies the go-cad layout stable ID ("model" or paper:<handle>);
// Space is "model" or "paper" so downstream code never mixes coordinates.
type Sheet struct {
	ID         string     `json:"id"`
	Index      int        `json:"index"` // 1-based; 0 = unassigned or fallback
	Name       string     `json:"name"`
	Layout     string     `json:"layout,omitempty"`
	Space      string     `json:"space,omitempty"`
	Bounds     [4]float64 `json:"bounds"`   // {minX, minY, maxX, maxY} in Layout coordinates
	Viewport   [4]float64 `json:"viewport"` // Render bounds including owned glyphs and margin.
	Source     string     `json:"source,omitempty"`
	Confidence float64    `json:"confidence,omitempty"`
}

// Text is one drawing text with the context the CAD chunker needs.
type Text struct {
	Text     string     `json:"text"`
	Kind     Kind       `json:"kind"`
	Origin   Origin     `json:"origin"`
	SheetID  string     `json:"sheet_id,omitempty"`
	Layer    string     `json:"layer,omitempty"`
	X        float64    `json:"x"`
	Y        float64    `json:"y"`
	Height   float64    `json:"height,omitempty"`
	Rotation float64    `json:"rotation,omitempty"`
	Bounds   [4]float64 `json:"bounds,omitempty"`
	// Handle identifies the source entity; flat-stream fallback texts may lack it.
	Handle     uint64  `json:"handle,omitempty"`
	Confidence float64 `json:"confidence"`
}

// Attribute is one block attribute value. go-cad exposes only the block-local
// insertion point, so no world position or sheet attribution is carried here.
type Attribute struct {
	Tag         string `json:"tag"`
	Value       string `json:"value"`
	OwnerHandle uint64 `json:"owner_handle,omitempty"`
	Layer       string `json:"layer,omitempty"`
}

// Dimension is one dimension entity. RenderedText is the text the drawing
// actually displays (from the anonymous block); UserText and Measurement are
// the entity-level overrides. Position is the text midpoint when the entity
// dump exposes it.
type Dimension struct {
	Handle       uint64   `json:"handle,omitempty"`
	SheetID      string   `json:"sheet_id,omitempty"`
	Layer        string   `json:"layer,omitempty"`
	RenderedText string   `json:"rendered_text,omitempty"`
	UserText     string   `json:"user_text,omitempty"`
	Measurement  *float64 `json:"measurement,omitempty"`
	Position     *Point   `json:"position,omitempty"`
	Confidence   float64  `json:"confidence"`
}

// Table reports a table entity. Status is "unsupported" while go-cad exposes
// ACAD_TABLE only as raw bits: the chunker must not present an empty table as
// if it had been parsed.
type Table struct {
	Handle uint64 `json:"handle,omitempty"`
	Status string `json:"status"` // unsupported
}

// Point is a world-space drawing coordinate.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z,omitempty"`
}

// Diagnostics records what the extraction could not deliver. It is the
// parser's honesty channel: an empty text list, unsupported tables and
// decode failures are reported here instead of being silently treated as a
// successful empty document.
type Diagnostics struct {
	Warnings       []string       `json:"warnings,omitempty"`
	Counts         map[string]int `json:"counts,omitempty"`
	SheetDetection string         `json:"sheet_detection"` // detected / fallback
}

// Drawing is the CAD intermediate representation handed from the parser to
// the CAD chunker.
type Drawing struct {
	Source      Source      `json:"source"`
	Sheets      []Sheet     `json:"sheets"`
	Texts       []Text      `json:"texts"`
	Attributes  []Attribute `json:"attributes,omitempty"`
	Dimensions  []Dimension `json:"dimensions,omitempty"`
	Tables      []Table     `json:"tables,omitempty"`
	Diagnostics Diagnostics `json:"diagnostics"`
}

// addWarning appends a warning once, preserving order.
func (d *Drawing) addWarning(w string) {
	if d == nil || w == "" {
		return
	}
	for _, existing := range d.Diagnostics.Warnings {
		if existing == w {
			return
		}
	}
	d.Diagnostics.Warnings = append(d.Diagnostics.Warnings, w)
}

// ensureSheet adds a synthetic sheet once. It is used for texts that fall
// outside every detected frame and for the fallback whole-drawing sheet.
func (d *Drawing) ensureSheet(id, name string, index int) {
	if d == nil || id == "" {
		return
	}
	for _, s := range d.Sheets {
		if s.ID == id {
			return
		}
	}
	d.Sheets = append(d.Sheets, Sheet{
		ID:     id,
		Index:  index,
		Name:   name,
		Layout: "model",
		Space:  "model",
		Source: "unassigned",
	})
}
