//go:build ee

package parser

import (
	"encoding/binary"
	"os"
	"reflect"
	"strings"
	"testing"

	"ragflow/internal/utility"
)

func TestVSDParserReadsV11Fixture(t *testing.T) {
	// Fixture from LibreOffice/libvisio's src/test/data (MPL-2.0).
	data, err := os.ReadFile("testdata/Visio11FormatLine.vsd")
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := GetParser(utility.GetFileType("lines.vsd"), data)
	if err != nil {
		t.Fatal(err)
	}
	result := p.ParseWithResult(t.Context(), "lines.vsd", data)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result.File["format"] != "vsd" || result.File["page_count"] != 1 || len(result.JSON) != 1 {
		t.Fatalf("unexpected VSD result: file=%v, items=%d", result.File, len(result.JSON))
	}
	item := result.JSON[0].ToMap()
	text, _ := item["text"].(string)
	for _, want := range []string{"Very small", "Extra large", "Round"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in %q", want, text)
		}
	}
}

func TestVSDParserRejectsTextAndTruncatedContainers(t *testing.T) {
	drawing, err := os.ReadFile("testdata/Visio11FormatLine.vsd")
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := GetParser(utility.GetFileType("bad.vsd"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{nil, []byte("plain text"), drawing[:len(drawing)/2]} {
		if result := p.ParseWithResult(t.Context(), "bad.vsd", data); result.Produced() || result.Err == nil {
			t.Fatalf("expected a parse error for %d bytes", len(data))
		}
	}
}

func TestVSDDecoderKeepsPageOrderAndUnicodeText(t *testing.T) {
	stream := make([]byte, 640)
	copy(stream, "Visio (TM) Drawing\r\n\x00")
	stream[26] = 11
	setPointer := func(slot, kind, offset, length int, format uint16) {
		binary.LittleEndian.PutUint32(stream[slot:], uint32(kind))
		binary.LittleEndian.PutUint32(stream[slot+8:], uint32(offset))
		binary.LittleEndian.PutUint32(stream[slot+12:], uint32(length))
		binary.LittleEndian.PutUint16(stream[slot+16:], format)
	}
	setPointer(36, 0x14, 64, 118, 0x50)
	binary.LittleEndian.PutUint32(stream[64:], 8) // Pointer metadata starts four bytes into the directory.
	binary.LittleEndian.PutUint32(stream[68:], 3)
	binary.LittleEndian.PutUint32(stream[72:], 5)
	// Page pointers in storage order: second, background, first.
	setPointer(80, 0x15, 200, 33, 0xd1)
	setPointer(98, 0x15, 300, 31, 0xd0)
	setPointer(116, 0x15, 400, 33, 0xd1)
	setPointer(134, 0x32, 500, 29, 0xd0)
	setPointer(152, 0xc9, 550, 17, 0x40)
	for i, page := range []uint32{2, 1, 0} {
		binary.LittleEndian.PutUint32(stream[170+i*4:], page)
	}

	writeText := func(offset int, units ...uint16) {
		binary.LittleEndian.PutUint32(stream[offset:], 0x0e)
		binary.LittleEndian.PutUint32(stream[offset+12:], uint32(8+len(units)*2))
		for i, unit := range units {
			binary.LittleEndian.PutUint16(stream[offset+27+i*2:], unit)
		}
	}
	writeText(200, 'G', 'o', 0)
	writeText(300, 'X', 0)
	writeText(400, '用', '户', 0)
	// A UTF-16 name record and an index associating it with page ID 2.
	binary.LittleEndian.PutUint32(stream[500:], 0x33)
	binary.LittleEndian.PutUint32(stream[504:], 7)
	binary.LittleEndian.PutUint32(stream[512:], 10)
	binary.LittleEndian.PutUint16(stream[523:], '首')
	binary.LittleEndian.PutUint16(stream[525:], '页')
	binary.LittleEndian.PutUint32(stream[550:], 1)
	binary.LittleEndian.PutUint32(stream[554:], 7)
	binary.LittleEndian.PutUint32(stream[558:], 7)
	binary.LittleEndian.PutUint32(stream[562:], 2)

	pages, err := decodeVSDDocument(t.Context(), stream)
	if err != nil {
		t.Fatal(err)
	}
	result := visioParseResult("example.vsd", "vsd", pages)
	if len(result.JSON) != 2 {
		t.Fatalf("expected two foreground items, got %d", len(result.JSON))
	}
	for i, want := range []struct {
		text string
		id   float64
	}{{"[Page 1: 首页]\nOther shapes and notes:\n- 用户", 2}, {"Go", 0}} {
		item := result.JSON[i].ToMap()
		if text, _ := item["text"].(string); !strings.Contains(text, want.text) {
			t.Errorf("page %d: expected %q in %q", i+1, want.text, text)
		}
		wantPositions := [][]float64{{float64(i + 1), want.id, 0, 0, 0}}
		if !reflect.DeepEqual(item["positions"], wantPositions) {
			t.Errorf("page %d: positions = %v, want %v", i+1, item["positions"], wantPositions)
		}
	}

	stream[26] = 6
	if _, err := decodeVSDDocument(t.Context(), stream); err == nil || !strings.Contains(err.Error(), "version 6") {
		t.Fatalf("expected version error, got %v", err)
	}
}

func TestVSDDecoderRejectsUnsupportedPageStream(t *testing.T) {
	decoder := vsdDecoder{ctx: t.Context(), document: make([]byte, 16)}
	err := decoder.readPage(vsdPointer{kind: 0x15, size: 16, format: 0x51}, 9, "Architecture")
	if err == nil || !strings.Contains(err.Error(), "page 1") || !strings.Contains(err.Error(), "0x51") {
		t.Fatalf("expected error identifying the page and stream format, got %v", err)
	}
	if len(decoder.pages) != 0 {
		t.Fatal("unsupported page was recorded as parsed")
	}
}

func TestVSDDecoderExtractsOnlyExplicitConnections(t *testing.T) {
	var stream []byte
	addRecord := func(kind, id uint32, level uint16, data []byte) {
		header := make([]byte, 19)
		binary.LittleEndian.PutUint32(header, kind)
		binary.LittleEndian.PutUint32(header[4:], id)
		binary.LittleEndian.PutUint32(header[12:], uint32(len(data)))
		binary.LittleEndian.PutUint16(header[16:], level)
		header[18] = 0x54
		stream = append(stream, header...)
		stream = append(stream, data...)
	}
	addText := func(label string) {
		text := make([]byte, 8)
		for _, char := range label {
			text = binary.LittleEndian.AppendUint16(text, uint16(char))
		}
		addRecord(0x0e, 0, 2, text)
	}
	addFormula := func(payload []byte, cell byte, target uint32) []byte {
		block := []byte{20, 0, 0, 0, 2, cell, 0x74, 0x4e, 0, 0, 0x60, 0, 0, 0, 0, 0x7a, 0x73, 0, 0, 0x40}
		binary.LittleEndian.PutUint32(block[11:], target)
		return append(payload, block...)
	}
	addLine := func(id uint32, label string, payload []byte) {
		addRecord(0x48, id, 1, nil)
		addText(label)
		addRecord(0x9d, 0, 2, make([]byte, 36))
		addRecord(0xa4, 0, 3, payload)
	}
	addRecord(0x48, 1, 1, nil)
	addText("Service note")
	addText("Go 服务")
	addRecord(0x48, 2, 1, nil)
	addText("MySQL")
	addRecord(0x48, 3, 1, nil)
	addText("Redis")
	addRecord(0x48, 4, 1, nil)
	addText("MySQL") // A separate, unconnected shape with the same text as shape 2.

	// Formula order is deliberately reversed. The cell tag identifies the endpoint.
	glued := addFormula(addFormula(make([]byte, 45), 12, 2), 11, 1)
	addLine(10, "TCP", glued)
	addLine(11, "touches both shapes", make([]byte, 45))
	addLine(12, "only one attached end", addFormula(make([]byte, 45), 11, 1))
	ambiguous := addFormula(addFormula(addFormula(make([]byte, 45), 11, 1), 11, 3), 12, 2)
	addLine(13, "conflicting begin targets", ambiguous)
	missing := addFormula(addFormula(make([]byte, 45), 11, 1), 12, 99)
	addLine(14, "unresolved target", missing)
	addRecord(0xa4, 0, 1, glued) // Same-level formula is not owned by shape 14.

	decoder := vsdDecoder{ctx: t.Context(), document: stream}
	if err := decoder.readPage(vsdPointer{kind: 0x15, size: uint32(len(stream)), format: 0xd1}, 7, "System"); err != nil {
		t.Fatal(err)
	}
	page := decoder.pages[0]
	want := []visioConnection{{FromText: "Go 服务", ToText: "MySQL", Label: "TCP", Dir: visioDirUndirected}}
	if !reflect.DeepEqual(page.Connections, want) {
		t.Fatalf("connections = %#v, want %#v", page.Connections, want)
	}
	wantExtras := []string{"Service note", "Redis", "MySQL", "touches both shapes", "only one attached end", "conflicting begin targets", "unresolved target"}
	if !reflect.DeepEqual(page.ExtraTexts, wantExtras) {
		t.Errorf("extra texts = %q, want %q", page.ExtraTexts, wantExtras)
	}
}
