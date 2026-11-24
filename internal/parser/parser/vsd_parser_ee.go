// Copyright (c) 2026 InfiniFlow. All rights reserved.
//
// RAGFlow Enterprise Edition — Proprietary and Confidential.

//go:build ee

package parser

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"

	"github.com/richardlehane/mscfb"
)

const (
	vsdMagic             = "Visio (TM) Drawing\r\n\x00"
	vsdVersionOffset     = 0x1a
	vsdTrailerOffset     = 0x24
	vsdPointerSize       = 18
	vsdChunkHeaderSize   = 19
	vsdMaxPointers       = 100000
	vsdMaxDecodedBytes   = 128 << 20
	vsdMaxDirectoryDepth = 16

	vsdText        = 0x0e
	vsdTrailer     = 0x14
	vsdPageEntry   = 0x15
	vsdPages       = 0x27
	vsdNameList    = 0x32
	vsdName        = 0x33
	vsdGroup       = 0x47
	vsdShape       = 0x48
	vsdForeign     = 0x4e
	vsdTransform1D = 0x9d
	vsdMisc        = 0xa4
	vsdNameIndex   = 0xc9
)

func parseVSD(ctx context.Context, data []byte) ([]visioPage, error) {
	container, err := mscfb.New(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open CFB container: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry, err := container.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("missing VisioDocument stream")
		}
		if err != nil {
			return nil, fmt.Errorf("read CFB directory: %w", err)
		}
		if entry.Name != "VisioDocument" || len(entry.Path) != 0 || entry.FileInfo().IsDir() {
			continue
		}
		if entry.Size < vsdTrailerOffset+vsdPointerSize || entry.Size > int64(len(data)) {
			return nil, fmt.Errorf("invalid VisioDocument size: %d", entry.Size)
		}
		stream := make([]byte, int(entry.Size))
		if _, err := io.ReadFull(entry, stream); err != nil {
			return nil, fmt.Errorf("read VisioDocument: %w", err)
		}
		return decodeVSDDocument(ctx, stream)
	}
}

type vsdPointer struct {
	kind   uint32
	offset uint32
	size   uint32
	format uint16
}

func readVSDPointer(raw []byte) vsdPointer {
	return vsdPointer{
		kind:   binary.LittleEndian.Uint32(raw),
		offset: binary.LittleEndian.Uint32(raw[8:]),
		size:   binary.LittleEndian.Uint32(raw[12:]),
		format: binary.LittleEndian.Uint16(raw[16:]),
	}
}

type vsdDecoder struct {
	ctx              context.Context
	document         []byte
	decodedBytes     int
	pointerCount     int
	directories      map[uint32]bool
	pages            []visioPage
	hasPageDirectory bool
}

func decodeVSDDocument(ctx context.Context, stream []byte) ([]visioPage, error) {
	if len(stream) < vsdTrailerOffset+vsdPointerSize || !bytes.HasPrefix(stream, []byte(vsdMagic)) {
		return nil, fmt.Errorf("invalid VisioDocument header")
	}
	if version := stream[vsdVersionOffset]; version != 11 {
		return nil, fmt.Errorf("unsupported Visio binary version %d; save as .vsdx to upload", version)
	}
	root := readVSDPointer(stream[vsdTrailerOffset:])
	if root.kind != vsdTrailer {
		return nil, fmt.Errorf("invalid trailer directory")
	}
	decoder := vsdDecoder{ctx: ctx, document: stream, directories: make(map[uint32]bool)}
	if err := decoder.readDirectory(root, 0); err != nil {
		return nil, err
	}
	if !decoder.hasPageDirectory && len(decoder.pages) == 0 {
		return nil, fmt.Errorf("missing page directory")
	}
	return decoder.pages, nil
}

func (d *vsdDecoder) readStream(ptr vsdPointer) ([]byte, int, error) {
	if err := d.ctx.Err(); err != nil {
		return nil, 0, err
	}
	end := uint64(ptr.offset) + uint64(ptr.size)
	if end > uint64(len(d.document)) {
		return nil, 0, fmt.Errorf("stream at %#x exceeds VisioDocument bounds", ptr.offset)
	}
	raw := d.document[ptr.offset:end]
	if ptr.format&2 == 0 {
		if len(raw) > vsdMaxDecodedBytes-d.decodedBytes {
			return nil, 0, fmt.Errorf("decoded data exceeds size budget")
		}
		d.decodedBytes += len(raw)
		return raw, 0, nil
	}
	decoded, err := decompressVSD(d.ctx, raw, vsdMaxDecodedBytes-d.decodedBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("stream at %#x: %w", ptr.offset, err)
	}
	d.decodedBytes += len(decoded)
	return decoded, 4, nil
}

func (d *vsdDecoder) readDirectory(ptr vsdPointer, depth int) error {
	if depth >= vsdMaxDirectoryDepth || d.directories[ptr.offset] {
		return fmt.Errorf("cyclic or too deeply nested page directory at %#x", ptr.offset)
	}
	d.directories[ptr.offset] = true
	defer delete(d.directories, ptr.offset)

	raw, prefix, err := d.readStream(ptr)
	if err != nil {
		return err
	}
	if len(raw) < prefix+4 {
		return fmt.Errorf("truncated directory at %#x", ptr.offset)
	}
	table := int64(binary.LittleEndian.Uint32(raw[prefix:])) + int64(prefix) - 4
	if table < int64(prefix) || table+12 > int64(len(raw)) {
		return fmt.Errorf("invalid pointer table at %#x", ptr.offset)
	}
	orderCount := int64(binary.LittleEndian.Uint32(raw[table:]))
	count := int64(binary.LittleEndian.Uint32(raw[table+4:]))
	entries := raw[table+12:]
	if count > int64(len(entries)/vsdPointerSize) || count > int64(vsdMaxPointers-d.pointerCount) {
		return fmt.Errorf("invalid pointer count %d at %#x", count, ptr.offset)
	}
	d.pointerCount += int(count)
	children := make([]vsdPointer, count)
	for i := range children {
		children[i] = readVSDPointer(entries[i*vsdPointerSize:])
	}
	orderBytes := entries[count*vsdPointerSize:]
	// A count of one is also used for tables without an explicit order list.
	if orderCount <= 1 {
		orderCount = 0
	}
	if orderCount > int64(len(orderBytes)/4) {
		return fmt.Errorf("truncated pointer order at %#x", ptr.offset)
	}

	names, err := d.readPageNames(children)
	if err != nil {
		return err
	}
	readChild := func(index int) error {
		child := children[index]
		children[index].kind = 0
		switch child.kind {
		case vsdPages:
			d.hasPageDirectory = true
			return d.readDirectory(child, depth+1)
		case vsdPageEntry:
			if child.format&1 != 0 {
				return d.readPage(child, index, names[uint32(index)])
			}
		}
		return nil
	}
	for i := int64(0); i < orderCount; i++ {
		index := binary.LittleEndian.Uint32(orderBytes[i*4:])
		if uint64(index) < uint64(len(children)) {
			if err := readChild(int(index)); err != nil {
				return err
			}
		}
	}
	for i := range children {
		if err := readChild(i); err != nil {
			return err
		}
	}
	return nil
}

func (d *vsdDecoder) readPageNames(children []vsdPointer) (map[uint32]string, error) {
	names := make(map[uint32]string)
	for _, child := range children {
		if child.kind != vsdNameList {
			continue
		}
		if err := d.readRecords(child, func(kind, id uint32, _ uint16, data []byte) error {
			if kind == vsdName && len(data) >= 4 {
				name := data[4:]
				for i := 0; i+1 < len(name); i += 2 {
					if binary.LittleEndian.Uint16(name[i:]) == 0 {
						name = name[:i]
						break
					}
				}
				names[id] = decodeVSDText(name)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	pageNames := make(map[uint32]string)
	for _, child := range children {
		if child.kind != vsdNameIndex {
			continue
		}
		if err := d.readRecords(child, func(kind, _ uint32, _ uint16, data []byte) error {
			if kind != vsdNameIndex {
				return nil
			}
			if len(data) < 4 || uint64(binary.LittleEndian.Uint32(data)) > uint64((len(data)-4)/13) {
				return fmt.Errorf("truncated page name index")
			}
			count := int(binary.LittleEndian.Uint32(data))
			for i := 0; i < count; i++ {
				row := data[4+i*13:]
				pageNames[binary.LittleEndian.Uint32(row[8:])] = names[binary.LittleEndian.Uint32(row)]
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return pageNames, nil
}

type vsdPageShape struct {
	id               uint32
	level            uint16
	text             string
	connector        bool
	endpoints        [2]vsdEndpoint
	textInConnection bool
}

type vsdEndpoint struct {
	id        uint32
	seen      bool
	ambiguous bool
}

// readGlue reads the two explicit trigger cells in VSD_MISC. The cell tag,
// rather than storage order, determines which endpoint owns a formula.
// The formula token pattern is documented by libvisio's VSDParser::readMisc.
func (s *vsdPageShape) readGlue(raw []byte) {
	const (
		formulaOffset      = 45
		beginTriggerCell   = 11
		endTriggerCell     = 12
		glueReferenceBytes = 20
	)
	if len(raw) < formulaOffset {
		return
	}
	for raw = raw[formulaOffset:]; len(raw) >= 4; {
		size := binary.LittleEndian.Uint32(raw)
		if size == 0 {
			return
		}
		if size < 6 || uint64(size) > uint64(len(raw)) {
			// This record cannot establish complete and unambiguous endpoints.
			s.endpoints[0].ambiguous = true
			s.endpoints[1].ambiguous = true
			return
		}
		block := raw[:size]
		raw = raw[size:]
		if len(block) < glueReferenceBytes || block[4] != 2 ||
			(block[5] != beginTriggerCell && block[5] != endTriggerCell) ||
			block[6] != 0x74 || binary.LittleEndian.Uint32(block[7:]) != 0x6000004e ||
			block[15] != 0x7a || binary.LittleEndian.Uint32(block[16:]) != 0x40000073 {
			continue
		}
		endpoint := &s.endpoints[block[5]-beginTriggerCell]
		id := binary.LittleEndian.Uint32(block[11:])
		if endpoint.seen && endpoint.id != id {
			endpoint.ambiguous = true
		}
		endpoint.seen, endpoint.id = true, id
	}
}

func (d *vsdDecoder) readPage(ptr vsdPointer, id int, name string) error {
	switch ptr.format >> 4 {
	case 8, 12, 13:
	default:
		return fmt.Errorf("page %d (ID %d): unsupported page stream format %#x", len(d.pages)+1, id, ptr.format)
	}
	if len(d.pages) >= maxVisioPages {
		return fmt.Errorf("page count exceeds %d", maxVisioPages)
	}
	page := visioPage{VisioID: id, DisplayNum: len(d.pages) + 1, Name: name}
	shapes := make(map[uint32]*vsdPageShape)
	var connectors []*vsdPageShape
	type pageText struct {
		shape *vsdPageShape
		text  string
	}
	var texts []pageText
	var current *vsdPageShape
	err := d.readRecords(ptr, func(kind, id uint32, level uint16, data []byte) error {
		if current != nil && level <= current.level {
			current = nil
		}
		switch kind {
		case vsdGroup, vsdShape, vsdForeign:
			current = &vsdPageShape{id: id, level: level}
			if _, duplicate := shapes[id]; duplicate {
				shapes[id] = nil
			} else if id != ^uint32(0) {
				shapes[id] = current
			}
		case vsdTransform1D:
			if current != nil && !current.connector && len(data) >= 36 {
				current.connector = true
				connectors = append(connectors, current)
			}
		case vsdMisc:
			if current != nil {
				current.readGlue(data)
			}
		case vsdText:
			if len(data) < 8 || len(data)%2 != 0 {
				return fmt.Errorf("invalid text record on page %d", page.DisplayNum)
			}
			if text := decodeVSDText(data[8:]); text != "" {
				texts = append(texts, pageText{shape: current, text: text})
				if current != nil {
					current.text = text
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, connector := range connectors {
		begin, end := connector.endpoints[0], connector.endpoints[1]
		if shapes[connector.id] == nil || !begin.seen || !end.seen || begin.ambiguous || end.ambiguous ||
			begin.id == end.id || begin.id == connector.id || end.id == connector.id {
			continue
		}
		from, to := shapes[begin.id], shapes[end.id]
		if from == nil || to == nil || from.text == "" || to.text == "" {
			continue
		}
		page.Connections = append(page.Connections, visioConnection{
			FromText: from.text, ToText: to.text, Label: connector.text, Dir: visioDirUndirected,
		})
		from.textInConnection = true
		to.textInConnection = true
		connector.textInConnection = true
	}
	for _, entry := range texts {
		if entry.shape != nil && entry.shape.textInConnection && entry.text == entry.shape.text {
			continue
		}
		page.ExtraTexts = append(page.ExtraTexts, entry.text)
	}
	d.pages = append(d.pages, page)
	return nil
}

func (d *vsdDecoder) readRecords(ptr vsdPointer, visit func(kind, id uint32, level uint16, data []byte) error) error {
	raw, prefix, err := d.readStream(ptr)
	if err != nil {
		return err
	}
	switch ptr.format >> 4 {
	case 0, 4, 5:
		if len(raw) < prefix {
			return fmt.Errorf("truncated record at %#x", ptr.offset)
		}
		return visit(ptr.kind, 0, 0, raw[prefix:])
	case 8, 12, 13:
		// Compressed record streams begin with a record immediately; the
		// four-byte prefix belongs to the directory and blob encodings.
	default:
		return fmt.Errorf("unsupported record stream format %#x", ptr.format)
	}

	for pos := 0; pos < len(raw); {
		if err := d.ctx.Err(); err != nil {
			return err
		}
		for pos < len(raw) && raw[pos] == 0 {
			pos++
		}
		if pos == len(raw) {
			break
		}
		if len(raw)-pos < vsdChunkHeaderSize {
			return fmt.Errorf("truncated record header at %#x:%#x", ptr.offset, pos)
		}
		header := raw[pos : pos+vsdChunkHeaderSize]
		kind := binary.LittleEndian.Uint32(header)
		id := binary.LittleEndian.Uint32(header[4:])
		size := uint64(binary.LittleEndian.Uint32(header[12:]))
		level := binary.LittleEndian.Uint16(header[16:])
		padding := vsdRecordTrailer(kind, binary.LittleEndian.Uint32(header[8:]) != 0, level, header[18])
		pos += vsdChunkHeaderSize
		if size+uint64(padding) > uint64(len(raw)-pos) {
			return fmt.Errorf("truncated record %#x at %#x:%#x", kind, ptr.offset, pos)
		}
		if err := visit(kind, id, level, raw[pos:pos+int(size)]); err != nil {
			return err
		}
		pos += int(size) + padding
	}
	return nil
}

// V11 records use type- and level-dependent trailers. A few data
// records override the general trailer rules.
func vsdRecordTrailer(kind uint32, hasList bool, level uint16, flags byte) int {
	switch kind {
	case 0x1f, 0xc9, 0x2d, 0xd1:
		return 0
	}
	trailer := 0
	switch kind {
	case 0x71, 0x70, 0x6b, 0x6a, 0x69, 0x66, 0x65, 0x2c:
		trailer = 8
	}
	if hasList {
		return 12
	}
	if (level == 2 && (flags == 0x55 || flags == 0x54 && kind == 0xaa)) ||
		(level == 3 && flags != 0x50 && flags != 0x54) {
		return trailer + 4
	}
	switch kind {
	case 0x64, 0x65, 0x66, 0x69, 0x6a, 0x6b, 0x6f, 0x71, 0x92, 0xa9, 0xb4, 0xb6, 0xb9, 0xc7:
		trailer += 4
	}
	return trailer
}

func decodeVSDText(raw []byte) string {
	units := make([]uint16, 0, len(raw)/2)
	for len(raw) >= 2 {
		unit := binary.LittleEndian.Uint16(raw)
		raw = raw[2:]
		switch unit {
		case 0, 0xfffc, 0xfffe, 0xffff:
			// Terminators and placeholders for computed fields.
		default:
			units = append(units, unit)
		}
	}
	return strings.Join(strings.Fields(string(utf16.Decode(units))), " ")
}

// VSD compression uses a 4 KiB sliding window and flag bits for literals
// and back references. The encoded reference is biased by the lookahead size.
func decompressVSD(ctx context.Context, raw []byte, budget int) ([]byte, error) {
	const windowSize = 4096
	var window [windowSize]byte
	var out []byte
	for len(raw) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		flags := raw[0]
		raw = raw[1:]
		for bit := 0; bit < 8 && len(raw) != 0; bit++ {
			if flags&(1<<bit) != 0 {
				if len(out) >= budget {
					return nil, fmt.Errorf("decoded data exceeds size budget")
				}
				window[len(out)%windowSize] = raw[0]
				out = append(out, raw[0])
				raw = raw[1:]
				continue
			}
			if len(raw) < 2 {
				return nil, fmt.Errorf("truncated compression reference")
			}
			token := int(binary.LittleEndian.Uint16(raw))
			raw = raw[2:]
			length := (token>>8)&15 + 3
			from := ((token&255 | token>>4&0xf00) + 18) % windowSize
			if length > budget-len(out) {
				return nil, fmt.Errorf("decoded data exceeds size budget")
			}
			for i := 0; i < length; i++ {
				value := window[(from+i)%windowSize]
				window[len(out)%windowSize] = value
				out = append(out, value)
			}
		}
	}
	return out, nil
}
