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

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"math"
	"strconv"
	"strings"
)

// WordTextFilterOption is the parser-wide option passed to Word formats.
const WordTextFilterOption = "remove_low_contrast_text"

// WordTextFilterSetting reads an explicitly configured Word text filter.
func WordTextFilterSetting(params map[string]any) (enabled, specified bool) {
	enabled, specified = params[WordTextFilterOption].(bool)
	return enabled, specified
}

type wordParserOptions struct {
	removeLowContrastText bool
}

func (o *wordParserOptions) configure(setup map[string]any) {
	o.removeLowContrastText, _ = WordTextFilterSetting(setup)
}

func (o wordParserOptions) prepareIR(irJSON string, docxData []byte) (string, bool) {
	if !o.removeLowContrastText {
		return irJSON, false
	}
	var ir map[string]any
	decoder := json.NewDecoder(strings.NewReader(irJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&ir); err != nil || !cleanWordIR(ir["sections"], docxPageBackground(docxData)) {
		return irJSON, false
	}
	// The IR contains only values produced by the JSON decoder. Keep all
	// fields, including media and layout, for the normal Word renderer.
	cleaned, _ := json.Marshal(ir)
	return string(cleaned), true
}

type wordRGB [3]uint8

var wordWhite = wordRGB{255, 255, 255}

const wordLowContrastRatio = 1.10

// Walk only the structured content consumed by the Word renderer. Background
// inherits through the enclosing element and cell, with run highlight on top.
func cleanWordIR(value any, background wordRGB) (removed bool) {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			if cleanWordIR(child, background) {
				removed = true
			}
		}
	case map[string]any:
		if color, ok := parseWordRGB(node["background_color"]); ok {
			background = color
		}
		if color, ok := parseWordRGB(node["highlight"]); ok {
			background = color
		}
		if node["type"] == "text" {
			text, _ := node["text"].(string)
			if color, ok := parseWordRGB(node["color"]); text != "" && ok && lowWordContrast(color, background) {
				node["text"] = ""
				removed = true
			}
		}
		for _, key := range [...]string{"elements", "rows", "cells", "items", "nested", "content"} {
			if cleanWordIR(node[key], background) {
				removed = true
			}
		}
	}
	return removed
}

func parseWordRGB(value any) (wordRGB, bool) {
	channels, ok := value.([]any)
	if !ok || len(channels) != 3 {
		return wordRGB{}, false
	}
	var color wordRGB
	for i, channel := range channels {
		number, ok := channel.(json.Number)
		if !ok {
			return wordRGB{}, false
		}
		value, err := strconv.ParseUint(string(number), 10, 8)
		if err != nil {
			return wordRGB{}, false
		}
		color[i] = uint8(value)
	}
	return color, true
}

func lowWordContrast(foreground, background wordRGB) bool {
	fg, bg := wordLuminance(foreground), wordLuminance(background)
	return (max(fg, bg)+0.05)/(min(fg, bg)+0.05) <= wordLowContrastRatio
}

func wordLuminance(color wordRGB) float64 {
	linear := func(channel uint8) float64 {
		srgb := float64(channel) / 255
		if srgb <= 0.04045 {
			return srgb / 12.92
		}
		return math.Pow((srgb+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(color[0]) + 0.7152*linear(color[1]) + 0.0722*linear(color[2])
}

// DOCX page color is absent from the native IR. Read the document-level
// w:background before the body. Word documents without a known fill use white.
func docxPageBackground(data []byte) wordRGB {
	if len(data) == 0 {
		return wordWhite
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return wordWhite
	}
	document, err := archive.Open("word/document.xml")
	if err != nil {
		return wordWhite
	}
	defer document.Close()

	decoder := xml.NewDecoder(document)
	for {
		token, err := decoder.Token()
		if err != nil {
			return wordWhite
		}
		element, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch element.Name.Local {
		case "body":
			return wordWhite
		case "background":
			for _, attr := range element.Attr {
				if attr.Name.Space == element.Name.Space && attr.Name.Local == "color" && len(attr.Value) == 6 {
					rgb, err := strconv.ParseUint(attr.Value, 16, 24)
					if err == nil {
						return wordRGB{uint8(rgb >> 16), uint8(rgb >> 8), uint8(rgb)}
					}
				}
			}
			return wordWhite
		}
	}
}
