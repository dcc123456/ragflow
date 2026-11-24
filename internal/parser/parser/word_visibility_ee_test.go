//go:build cgo && ee

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
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

func TestDOCXParserRemovesLowContrastText(t *testing.T) {
	body := `<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr>
		<w:r><w:t>Visible heading</w:t></w:r>
		<w:r><w:rPr><w:color w:val="F9F9F9"/></w:rPr><w:t>Hidden heading</w:t></w:r>
	</w:p>
	<w:p><w:r><w:t xml:space="preserve">Visible before </w:t></w:r>
		<w:r><w:rPr><w:color w:val="F9F9F9"/></w:rPr><w:t>Hidden inline</w:t></w:r>
		<w:r><w:t>visible after</w:t></w:r>
		<w:r><w:rPr><w:color w:val="FFFFFF"/><w:highlight w:val="darkBlue"/></w:rPr><w:t>Visible highlighted text</w:t></w:r>
	</w:p>
	<w:tbl><w:tr>
		<w:tc><w:p><w:r><w:t>Visible cell</w:t></w:r>
			<w:r><w:rPr><w:color w:val="FFFFFF"/></w:rPr><w:t>Hidden cell</w:t></w:r>
		</w:p></w:tc>
		<w:tc><w:tcPr><w:shd w:val="clear" w:fill="202020"/></w:tcPr>
			<w:p><w:r><w:rPr><w:color w:val="FFFFFF"/></w:rPr><w:t>Visible on dark cell</w:t></w:r></w:p>
		</w:tc>
	</w:tr></w:tbl>`
	p := NewDOCXParser()
	p.ConfigureFromSetup(map[string]any{"remove_low_contrast_text": true})
	res := p.ParseWithResult(t.Context(), "colors.docx", docxCaptionTestDOCX(t, body))
	output := wordVisibilityOutput(t, res)
	for _, want := range []string{"Visible before visible after", "Visible highlighted text", "Visible cell", "Visible on dark cell"} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in %s", want, output)
		}
	}
	if strings.Contains(output, "Hidden") {
		t.Errorf("low-contrast text stayed in output: %s", output)
	}
	table := docxCaptionFindTable(t, res)
	if table.Caption != "Table Location: Visible heading" {
		t.Errorf("table caption = %q", table.Caption)
	}
}

func TestDOCParserFullyFilteredTextDoesNotFallBack(t *testing.T) {
	const hidden = "Low contrast sample"
	data := docWithTextProperties(t, hidden, []byte{0x70, 0x68, 249, 249, 249, 0})
	p := NewDOCParser()
	p.ConfigureFromSetup(map[string]any{"remove_low_contrast_text": true})
	res := p.ParseWithResult(t.Context(), "colors.doc", data)
	if output := wordVisibilityOutput(t, res); strings.Contains(output, hidden) {
		t.Errorf("low-contrast text stayed in output: %s", output)
	}
}

func TestDOCParserPreservesWhiteTextOnDarkHighlight(t *testing.T) {
	const visible = "White text on dark highlight"
	// sprmCCv sets white text; sprmCHighlight sets dark blue.
	data := docWithTextProperties(t, visible, []byte{0x70, 0x68, 255, 255, 255, 0, 0x0C, 0x2A, 9})
	p := NewDOCParser()
	p.ConfigureFromSetup(map[string]any{"remove_low_contrast_text": true})
	res := p.ParseWithResult(t.Context(), "highlight.doc", data)
	if output := wordVisibilityOutput(t, res); !strings.Contains(output, visible) {
		t.Errorf("highlighted text was removed: %s", output)
	}
}

func wordVisibilityOutput(t *testing.T, res ParseResult) string {
	t.Helper()
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	items := make([]map[string]any, 0, len(res.JSON))
	for _, item := range res.JSON {
		items = append(items, item.ToMap())
	}
	output, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

// Add Word 97 character and paragraph property pages to the synthetic OLE
// document. One CHPX range applies the supplied properties to all text.
func docWithTextProperties(t *testing.T, text string, properties []byte) []byte {
	t.Helper()
	const sector = 512
	const textStart = 426
	data := append(buildSyntheticDoc(t, text), make([]byte, 2*sector)...)
	fat := data[sector : 2*sector]
	binary.LittleEndian.PutUint32(fat[2*4:], 4)
	binary.LittleEndian.PutUint32(fat[4*4:], 5)
	binary.LittleEndian.PutUint32(fat[5*4:], 0xFFFFFFFE)
	directory := data[2*sector : 3*sector]
	binary.LittleEndian.PutUint32(directory[128+0x78:], 3*sector)
	wordDocument := data[3*sector : 4*sector]
	binary.LittleEndian.PutUint32(wordDocument[0xFA:], 21)
	binary.LittleEndian.PutUint32(wordDocument[0xFE:], 12)
	binary.LittleEndian.PutUint32(wordDocument[0x102:], 33)
	binary.LittleEndian.PutUint32(wordDocument[0x106:], 12)
	table := data[4*sector : 5*sector]
	binary.LittleEndian.PutUint32(directory[2*128+0x78:], 45)
	binary.LittleEndian.PutUint32(table[21:], textStart)
	binary.LittleEndian.PutUint32(table[25:], textStart+uint32(len(text)))
	binary.LittleEndian.PutUint32(table[29:], 1)
	copy(table[33:], table[21:29])
	binary.LittleEndian.PutUint32(table[41:], 2)
	chpx := data[5*sector : 6*sector]
	papx := data[6*sector:]
	copy(papx, table[33:41])
	papx[sector-1] = 1
	binary.LittleEndian.PutUint32(chpx, textStart)
	binary.LittleEndian.PutUint32(chpx[4:], textStart+uint32(len(text)))
	chpx[8] = 16
	chpx[32] = byte(len(properties))
	copy(chpx[33:], properties)
	chpx[sector-1] = 1
	return data
}

func TestDOCXParserUsesExplicitPageBackground(t *testing.T) {
	document := docxCaptionBody(`<w:p>
		<w:r><w:rPr><w:color w:val="FFFFFF"/></w:rPr><w:t>Visible white text</w:t></w:r>
		<w:r><w:rPr><w:color w:val="202020"/></w:rPr><w:t>Hidden in dark background</w:t></w:r>
	</w:p>`)
	document = strings.Replace(document, "<w:body>", `<w:background w:color="202020"/><w:body>`, 1)
	data := docxCaptionZip(t, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   document,
	})
	p := NewDOCXParser()
	p.ConfigureFromSetup(map[string]any{"remove_low_contrast_text": true})
	res := p.ParseWithResult(t.Context(), "dark.docx", data)
	output := wordVisibilityOutput(t, res)
	if !strings.Contains(output, "Visible white text") || strings.Contains(output, "Hidden in dark background") {
		t.Errorf("dark-page text: %s", output)
	}
}

func TestWordParsersKeepLowContrastTextWithoutOptIn(t *testing.T) {
	const pale = "Low contrast opt-in sample"
	tests := []struct {
		name   string
		parser ParseResultProducer
		data   []byte
	}{
		{"sample.doc", NewDOCParser(), docWithTextProperties(t, pale, []byte{0x70, 0x68, 249, 249, 249, 0})},
		{"sample.docx", NewDOCXParser(), docxCaptionTestDOCX(t, `<w:p><w:r><w:rPr><w:color w:val="F9F9F9"/></w:rPr><w:t>`+pale+`</w:t></w:r></w:p>`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if output := wordVisibilityOutput(t, tc.parser.ParseWithResult(t.Context(), tc.name, tc.data)); !strings.Contains(output, pale) {
				t.Errorf("low-contrast text was filtered without opting in: %s", output)
			}
		})
	}
}
