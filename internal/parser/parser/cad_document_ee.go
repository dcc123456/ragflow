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

package parser

import "ragflow/internal/parser/cadsource"

// CADDocument is the structured CAD payload carried on ParseResult.File under
// the "cad" key. The CAD chunker consumes it directly; ParseResult.JSON only
// carries a flat text projection for preview and for pipelines that have no
// CAD chunker.
type CADDocument = cadsource.Drawing

// CADTextItems projects the CAD document's text facts onto the parser's
// standard DocItem wire contract. CAD-specific fields ride in Extra so the
// shared DocItem/ChunkDoc types stay untouched.
func CADTextItems(doc *CADDocument) []DocItem {
	if doc == nil {
		return nil
	}
	items := make([]DocItem, 0, len(doc.Texts)+len(doc.Attributes))
	for _, text := range doc.Texts {
		if text.Text == "" {
			continue
		}
		extra := map[string]any{
			"cad_kind":       string(text.Kind),
			"cad_source":     string(text.Origin),
			"cad_sheet_id":   text.SheetID,
			"cad_layer":      text.Layer,
			"cad_x":          text.X,
			"cad_y":          text.Y,
			"cad_height":     text.Height,
			"cad_rotation":   text.Rotation,
			"cad_confidence": text.Confidence,
		}
		if text.Handle != 0 {
			extra["cad_entity_handle"] = text.Handle
		}
		items = append(items, TextItem{Text: text.Text, Extra: extra})
	}
	// Attribute values already appear in Texts through SheetTexts. This
	// projection exists for preview consumers that only understand DocItem;
	// the CAD chunker must read doc.Attributes for tag/value pairs instead.
	for _, attribute := range doc.Attributes {
		text := attribute.Value
		if attribute.Tag != "" {
			if text != "" {
				text = attribute.Tag + ": " + text
			} else {
				text = attribute.Tag
			}
		}
		if text == "" {
			continue
		}
		extra := map[string]any{
			"cad_kind":   string(cadsource.KindAttrib),
			"cad_source": string(cadsource.OriginAttrib),
			"cad_tag":    attribute.Tag,
			"cad_value":  attribute.Value,
			"cad_layer":  attribute.Layer,
		}
		if attribute.OwnerHandle != 0 {
			extra["cad_owner_handle"] = attribute.OwnerHandle
		}
		items = append(items, TextItem{Text: text, Extra: extra})
	}
	return items
}
