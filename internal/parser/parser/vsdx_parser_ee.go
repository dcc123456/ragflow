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

package parser

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

var (
	maxVSDXInputBytes              = 100 << 20 // 100 MiB compressed archive limit
	maxVSDXEntryBytes        int64 = 32 << 20  // 32 MiB single XML entry limit
	maxVSDXUncompressedBytes int64 = 128 << 20 // 128 MiB cumulative uncompressed XML budget
	maxVSDXPages                   = 200
)

// VSDXParser parses Microsoft Visio (.vsdx) OpenXML drawings in pure Go.
// Each non-empty foreground page becomes one TextItem carrying its explicit
// connection relations, unconnected shape texts, and a [displayPage, visioID]
// position tuple for PageChunker and frontend page navigation.
type VSDXParser struct{}

// NewVSDXParser constructs a VSDXParser.
func NewVSDXParser() *VSDXParser {
	return &VSDXParser{}
}

// String identifies the parser in logs and diagnostics.
func (p *VSDXParser) String() string {
	return "VSDXParser"
}

// ParseWithResult implements ParseResultProducer.
func (p *VSDXParser) ParseWithResult(ctx context.Context, filename string, data []byte) ParseResult {
	if p == nil {
		return ParseResult{Err: fmt.Errorf("VSDXParser is nil")}
	}
	if err := ctx.Err(); err != nil {
		return ParseResult{Err: err}
	}
	if len(data) == 0 {
		return ParseResult{Err: fmt.Errorf("vsdx: empty file %q", filename)}
	}
	if len(data) > maxVSDXInputBytes {
		return ParseResult{Err: fmt.Errorf("vsdx: file size %d exceeds %d byte limit", len(data), maxVSDXInputBytes)}
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ParseResult{Err: fmt.Errorf("vsdx: open zip: %w", err)}
	}

	pkg := newVSDXPackage(zr)
	doc, err := pkg.parseDocument(ctx)
	if err != nil {
		return ParseResult{Err: err}
	}

	items := make([]DocItem, 0, len(doc.ForegroundPages))
	for _, page := range doc.ForegroundPages {
		text := formatVSDXPageText(page)
		if text == "" {
			continue
		}
		items = append(items, TextItem{
			Text: text,
			Extra: map[string]any{
				"positions": [][]float64{{float64(page.DisplayNum), float64(page.VisioID), 0, 0, 0}},
			},
		})
	}

	return ParseResult{
		File: map[string]any{
			"name":       filename,
			"format":     "vsdx",
			"page_count": len(doc.ForegroundPages),
		},
		JSON: items,
	}
}

// vsdxDocument holds parsed foreground pages ready for text rendering.
type vsdxDocument struct {
	ForegroundPages []vsdxPage
}

type vsdxPage struct {
	VisioID     int
	DisplayNum  int
	Name        string
	Connections []vsdxConnection
	ExtraTexts  []string
}

type vsdxConnection struct {
	FromText string
	ToText   string
	Label    string
	Dir      vsdxDirection
}

type vsdxDirection int

const (
	vsdxDirUndirected vsdxDirection = iota
	vsdxDirForward
	vsdxDirBackward
	vsdxDirBidirectional
)

type vsdxMaster struct {
	ID         string
	RootShape  *vsdxShapeData
	ShapesByID map[string]*vsdxShapeData
}

type vsdxShapeData struct {
	ID          string
	ParentID    string
	Type        string
	Master      string
	MasterShape string
	Order       int
	Text        string
	HasText     bool
	BeginArrow  string
	EndArrow    string
	PinX        *float64
	PinY        *float64
	Children    []*vsdxShapeData
}

// --- ZIP & relationship reader ---

type vsdxPackage struct {
	files     map[string]*zip.File
	totalRead int64
}

func newVSDXPackage(zr *zip.Reader) *vsdxPackage {
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[cleanZipPath(f.Name)] = f
	}
	return &vsdxPackage{files: files}
}

func cleanZipPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	cleaned := path.Clean(p)
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func (p *vsdxPackage) readEntry(ctx context.Context, entryPath string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	entryPath = cleanZipPath(entryPath)
	zf, ok := p.files[entryPath]
	if !ok {
		return nil, false, nil
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, true, fmt.Errorf("vsdx: open %q: %w", entryPath, err)
	}
	defer rc.Close()

	remaining := maxVSDXUncompressedBytes - p.totalRead
	if remaining <= 0 {
		return nil, true, fmt.Errorf("vsdx: uncompressed content exceeds %d byte limit", maxVSDXUncompressedBytes)
	}
	limit := int64(maxVSDXEntryBytes)
	if remaining < limit {
		limit = remaining
	}

	raw, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, true, fmt.Errorf("vsdx: read %q: %w", entryPath, err)
	}
	if int64(len(raw)) > limit {
		return nil, true, fmt.Errorf("vsdx: entry %q exceeds size budget", entryPath)
	}
	p.totalRead += int64(len(raw))
	return raw, true, nil
}

type vsdxRelsXML struct {
	Relationships []vsdxRelXML `xml:"Relationship"`
}

type vsdxRelXML struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
}

func relsPathFor(sourcePath string) string {
	sourcePath = cleanZipPath(sourcePath)
	if sourcePath == "" {
		return "_rels/.rels"
	}
	dir, base := path.Split(sourcePath)
	return cleanZipPath(path.Join(dir, "_rels", base+".rels"))
}

func resolveRelTarget(sourcePath, target string) string {
	target = strings.ReplaceAll(strings.TrimSpace(target), "\\", "/")
	if target == "" {
		return ""
	}
	if strings.HasPrefix(target, "/") {
		return cleanZipPath(target)
	}
	dir := path.Dir(cleanZipPath(sourcePath))
	if dir == "." {
		dir = ""
	}
	return cleanZipPath(path.Join(dir, target))
}

func (p *vsdxPackage) readRels(ctx context.Context, sourcePath string) (map[string]string, map[string]string, error) {
	byID := map[string]string{}
	byTypeSuffix := map[string]string{}
	raw, exists, err := p.readEntry(ctx, relsPathFor(sourcePath))
	if err != nil || !exists {
		return byID, byTypeSuffix, err
	}
	var rels vsdxRelsXML
	if err := xml.Unmarshal(raw, &rels); err != nil {
		return nil, nil, fmt.Errorf("vsdx: decode rels for %q: %w", sourcePath, err)
	}
	for _, rel := range rels.Relationships {
		resolved := resolveRelTarget(sourcePath, rel.Target)
		if rel.ID != "" {
			byID[rel.ID] = resolved
		}
		if idx := strings.LastIndex(rel.Type, "/"); idx >= 0 {
			byTypeSuffix[strings.ToLower(rel.Type[idx+1:])] = resolved
		}
	}
	return byID, byTypeSuffix, nil
}

// --- Package parsing ---

func (p *vsdxPackage) parseDocument(ctx context.Context) (*vsdxDocument, error) {
	_, rootByType, err := p.readRels(ctx, "")
	if err != nil {
		return nil, err
	}
	docPath := rootByType["document"]
	if docPath == "" {
		docPath = "visio/document.xml"
	}
	if _, exists, err := p.readEntry(ctx, docPath); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("vsdx: missing document entry %q", docPath)
	}

	_, docByType, err := p.readRels(ctx, docPath)
	if err != nil {
		return nil, err
	}

	masters := map[string]*vsdxMaster{}
	if mastersPath := docByType["masters"]; mastersPath != "" {
		masters, err = p.parseMasters(ctx, mastersPath)
		if err != nil {
			return nil, err
		}
	}

	pagesPath := docByType["pages"]
	if pagesPath == "" {
		pagesPath = "visio/pages/pages.xml"
	}
	pages, err := p.parsePages(ctx, pagesPath, masters)
	if err != nil {
		return nil, err
	}
	return &vsdxDocument{ForegroundPages: pages}, nil
}

type vsdxMastersXML struct {
	Masters []vsdxMasterXML `xml:"Master"`
}

type vsdxMasterXML struct {
	ID  string     `xml:"ID,attr"`
	Rel vsdxRefXML `xml:"Rel"`
}

type vsdxRefXML struct {
	RID string `xml:"id,attr"`
}

func (p *vsdxPackage) parseMasters(ctx context.Context, mastersPath string) (map[string]*vsdxMaster, error) {
	raw, exists, err := p.readEntry(ctx, mastersPath)
	if err != nil || !exists {
		return map[string]*vsdxMaster{}, err
	}
	var mlist vsdxMastersXML
	if err := xml.Unmarshal(raw, &mlist); err != nil {
		return nil, fmt.Errorf("vsdx: decode %q: %w", mastersPath, err)
	}
	relsByID, _, err := p.readRels(ctx, mastersPath)
	if err != nil {
		return nil, err
	}

	out := make(map[string]*vsdxMaster, len(mlist.Masters))
	for _, m := range mlist.Masters {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		target := relsByID[m.Rel.RID]
		if target == "" {
			continue
		}
		mRaw, ok, err := p.readEntry(ctx, target)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		master, err := decodeVSDXMaster(m.ID, mRaw)
		if err != nil {
			return nil, fmt.Errorf("vsdx: decode master %q: %w", target, err)
		}
		out[m.ID] = master
	}
	return out, nil
}

type vsdxMasterContentsXML struct {
	Shapes []vsdxShapeXML `xml:"Shapes>Shape"`
}

func decodeVSDXMaster(masterID string, raw []byte) (*vsdxMaster, error) {
	var mc vsdxMasterContentsXML
	if err := xml.Unmarshal(raw, &mc); err != nil {
		return nil, err
	}
	byID := map[string]*vsdxShapeData{}
	order := 0
	var root *vsdxShapeData
	for _, s := range mc.Shapes {
		converted := convertVSDXShape(s, "", &order, byID)
		if root == nil {
			root = converted
		}
	}
	return &vsdxMaster{
		ID:         masterID,
		RootShape:  root,
		ShapesByID: byID,
	}, nil
}

type vsdxPagesXML struct {
	Pages []vsdxPageMetaXML `xml:"Page"`
}

type vsdxPageMetaXML struct {
	ID         string     `xml:"ID,attr"`
	Name       string     `xml:"Name,attr"`
	Background string     `xml:"Background,attr"`
	Rel        vsdxRefXML `xml:"Rel"`
}

func (p *vsdxPackage) parsePages(ctx context.Context, pagesPath string, masters map[string]*vsdxMaster) ([]vsdxPage, error) {
	raw, exists, err := p.readEntry(ctx, pagesPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("vsdx: missing pages index %q", pagesPath)
	}
	var plist vsdxPagesXML
	if err := xml.Unmarshal(raw, &plist); err != nil {
		return nil, fmt.Errorf("vsdx: decode %q: %w", pagesPath, err)
	}
	if len(plist.Pages) > maxVSDXPages {
		return nil, fmt.Errorf("vsdx: page count %d exceeds %d limit", len(plist.Pages), maxVSDXPages)
	}
	relsByID, _, err := p.readRels(ctx, pagesPath)
	if err != nil {
		return nil, err
	}

	var out []vsdxPage
	displayNum := 0
	for _, meta := range plist.Pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if isBackgroundPage(meta.Background) {
			continue
		}
		target := relsByID[meta.Rel.RID]
		if target == "" {
			continue
		}
		pageRaw, ok, err := p.readEntry(ctx, target)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		displayNum++
		visioID, _ := strconv.Atoi(strings.TrimSpace(meta.ID))
		page, err := decodeVSDXPage(visioID, displayNum, strings.TrimSpace(meta.Name), pageRaw, masters)
		if err != nil {
			return nil, fmt.Errorf("vsdx: decode page %q: %w", target, err)
		}
		out = append(out, page)
	}
	return out, nil
}

func isBackgroundPage(raw string) bool {
	v := strings.TrimSpace(strings.ToLower(raw))
	return v == "1" || v == "true"
}

// --- Page XML model ---

type vsdxPageContentsXML struct {
	Shapes   []vsdxShapeXML   `xml:"Shapes>Shape"`
	Connects []vsdxConnectXML `xml:"Connects>Connect"`
}

type vsdxShapeXML struct {
	ID          string           `xml:"ID,attr"`
	Type        string           `xml:"Type,attr"`
	Master      string           `xml:"Master,attr"`
	MasterShape string           `xml:"MasterShape,attr"`
	Cells       []vsdxCellXML    `xml:"Cell"`
	Sections    []vsdxSectionXML `xml:"Section"`
	Text        *vsdxTextXML     `xml:"Text"`
	Children    []vsdxShapeXML   `xml:"Shapes>Shape"`
}

type vsdxSectionXML struct {
	Name string       `xml:"N,attr"`
	Rows []vsdxRowXML `xml:"Row"`
}

type vsdxRowXML struct {
	Cells []vsdxCellXML `xml:"Cell"`
}

type vsdxCellXML struct {
	Name  string `xml:"N,attr"`
	Value string `xml:"V,attr"`
}

type vsdxTextXML struct {
	InnerXML string `xml:",innerxml"`
}

type vsdxConnectXML struct {
	FromSheet string `xml:"FromSheet,attr"`
	FromCell  string `xml:"FromCell,attr"`
	ToSheet   string `xml:"ToSheet,attr"`
}

func decodeVSDXPage(visioID, displayNum int, name string, raw []byte, masters map[string]*vsdxMaster) (vsdxPage, error) {
	var pc vsdxPageContentsXML
	if err := xml.Unmarshal(raw, &pc); err != nil {
		return vsdxPage{}, err
	}

	shapesByID := map[string]*vsdxShapeData{}
	orderedShapes := make([]*vsdxShapeData, 0, len(pc.Shapes))
	order := 0
	for _, s := range pc.Shapes {
		converted := convertVSDXShape(s, "", &order, shapesByID)
		collectShapesInOrder(converted, &orderedShapes)
	}
	applyMasterInheritance(orderedShapes, masters)

	connections, usedShapeIDs, connectorIDs, emittedConnectorIDs := buildVSDXConnections(pc.Connects, shapesByID)
	extraTexts := collectExtraShapeTexts(orderedShapes, usedShapeIDs, connectorIDs, emittedConnectorIDs)

	return vsdxPage{
		VisioID:     visioID,
		DisplayNum:  displayNum,
		Name:        name,
		Connections: connections,
		ExtraTexts:  extraTexts,
	}, nil
}

func convertVSDXShape(s vsdxShapeXML, parentID string, order *int, byID map[string]*vsdxShapeData) *vsdxShapeData {
	*order++
	shape := &vsdxShapeData{
		ID:          strings.TrimSpace(s.ID),
		ParentID:    parentID,
		Type:        strings.TrimSpace(s.Type),
		Master:      strings.TrimSpace(s.Master),
		MasterShape: strings.TrimSpace(s.MasterShape),
		Order:       *order,
	}
	if s.Text != nil {
		shape.HasText = true
		shape.Text = extractVisioText(s.Text.InnerXML)
	}
	readShapeCells(shape, s.Cells)
	for _, sec := range s.Sections {
		for _, row := range sec.Rows {
			readShapeCells(shape, row.Cells)
		}
	}
	if shape.ID != "" {
		byID[shape.ID] = shape
	}
	for _, child := range s.Children {
		convertedChild := convertVSDXShape(child, shape.ID, order, byID)
		shape.Children = append(shape.Children, convertedChild)
	}
	return shape
}

func readShapeCells(shape *vsdxShapeData, cells []vsdxCellXML) {
	for _, cell := range cells {
		switch cell.Name {
		case "BeginArrow":
			if shape.BeginArrow == "" {
				shape.BeginArrow = strings.TrimSpace(cell.Value)
			}
		case "EndArrow":
			if shape.EndArrow == "" {
				shape.EndArrow = strings.TrimSpace(cell.Value)
			}
		case "PinX":
			if shape.PinX == nil {
				if v, err := strconv.ParseFloat(strings.TrimSpace(cell.Value), 64); err == nil {
					shape.PinX = &v
				}
			}
		case "PinY":
			if shape.PinY == nil {
				if v, err := strconv.ParseFloat(strings.TrimSpace(cell.Value), 64); err == nil {
					shape.PinY = &v
				}
			}
		}
	}
}

func collectShapesInOrder(s *vsdxShapeData, out *[]*vsdxShapeData) {
	if s == nil {
		return
	}
	*out = append(*out, s)
	for _, child := range s.Children {
		collectShapesInOrder(child, out)
	}
}

func applyMasterInheritance(shapes []*vsdxShapeData, masters map[string]*vsdxMaster) {
	if len(masters) == 0 {
		return
	}
	byID := make(map[string]*vsdxShapeData, len(shapes))
	for _, s := range shapes {
		if s.ID != "" {
			byID[s.ID] = s
		}
	}
	for _, s := range shapes {
		masterID := s.Master
		if masterID == "" {
			masterID = ancestorMasterID(s, byID)
		}
		if masterID == "" {
			continue
		}
		master, ok := masters[masterID]
		if !ok || master == nil {
			continue
		}
		var proto *vsdxShapeData
		if s.MasterShape != "" {
			proto = master.ShapesByID[s.MasterShape]
		} else if s.Master != "" {
			proto = master.RootShape
		}
		if proto == nil {
			continue
		}
		if !s.HasText && proto.HasText {
			s.Text = proto.Text
			s.HasText = true
		}
		if s.BeginArrow == "" {
			s.BeginArrow = proto.BeginArrow
		}
		if s.EndArrow == "" {
			s.EndArrow = proto.EndArrow
		}
	}
}

func ancestorMasterID(s *vsdxShapeData, byID map[string]*vsdxShapeData) string {
	curr := s
	for curr != nil && curr.ParentID != "" {
		parent, ok := byID[curr.ParentID]
		if !ok {
			break
		}
		if parent.Master != "" {
			return parent.Master
		}
		curr = parent
	}
	return ""
}

// extractVisioText reads character data from <Text> XML, inserts a space at
// <pp> paragraph breaks, and collapses whitespace into a single-line label
// suitable for search and relation sentences.
func extractVisioText(innerXML string) string {
	if strings.TrimSpace(innerXML) == "" {
		return ""
	}
	dec := xml.NewDecoder(strings.NewReader("<t>" + innerXML + "</t>"))
	var buf strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch v := tok.(type) {
		case xml.StartElement:
			if v.Name.Local == "pp" && buf.Len() > 0 {
				buf.WriteByte(' ')
			}
		case xml.CharData:
			buf.Write(v)
		}
	}
	fields := strings.Fields(buf.String())
	return strings.Join(fields, " ")
}

// --- Explicit Connects resolution ---

type rawConnectorEndpoints struct {
	fromSheet string
	begins    []string
	beginSeen map[string]struct{}
	ends      []string
	endSeen   map[string]struct{}
}

func buildVSDXConnections(connects []vsdxConnectXML, shapesByID map[string]*vsdxShapeData) ([]vsdxConnection, map[string]struct{}, map[string]struct{}, map[string]struct{}) {
	byConnector := map[string]*rawConnectorEndpoints{}
	connectorOrder := make([]string, 0, len(connects)/2)
	connectorIDs := make(map[string]struct{}, len(connects)/2)
	emittedConnectorIDs := map[string]struct{}{}

	for _, c := range connects {
		fromSheet := strings.TrimSpace(c.FromSheet)
		toSheet := strings.TrimSpace(c.ToSheet)
		fromCell := strings.TrimSpace(c.FromCell)
		if fromSheet == "" || toSheet == "" {
			continue
		}
		connectorIDs[fromSheet] = struct{}{}
		ep, exists := byConnector[fromSheet]
		if !exists {
			ep = &rawConnectorEndpoints{
				fromSheet: fromSheet,
				beginSeen: map[string]struct{}{},
				endSeen:   map[string]struct{}{},
			}
			byConnector[fromSheet] = ep
			connectorOrder = append(connectorOrder, fromSheet)
		}
		switch {
		case strings.HasPrefix(fromCell, "Begin"):
			if _, dup := ep.beginSeen[toSheet]; !dup {
				ep.beginSeen[toSheet] = struct{}{}
				ep.begins = append(ep.begins, toSheet)
			}
		case strings.HasPrefix(fromCell, "End"):
			if _, dup := ep.endSeen[toSheet]; !dup {
				ep.endSeen[toSheet] = struct{}{}
				ep.ends = append(ep.ends, toSheet)
			}
		}
	}

	var connections []vsdxConnection
	usedShapeIDs := map[string]struct{}{}
	for _, connID := range connectorOrder {
		ep := byConnector[connID]
		// Only emit a relation when both endpoints are unambiguously glued to
		// exactly one target shape each.
		if len(ep.begins) != 1 || len(ep.ends) != 1 {
			continue
		}
		beginID := ep.begins[0]
		endID := ep.ends[0]
		if beginID == connID || endID == connID {
			continue
		}

		beginTarget, beginText := resolveEndpointText(beginID, shapesByID)
		endTarget, endText := resolveEndpointText(endID, shapesByID)
		if beginText == "" || endText == "" || beginTarget == endTarget {
			continue
		}

		connShape := shapesByID[connID]
		label := ""
		dir := vsdxDirUndirected
		if connShape != nil {
			label = connShape.Text
			dir = resolveArrowDirection(connShape.BeginArrow, connShape.EndArrow)
		}

		var fromText, toText string
		switch dir {
		case vsdxDirBackward:
			fromText, toText = endText, beginText
		default:
			fromText, toText = beginText, endText
		}

		connections = append(connections, vsdxConnection{
			FromText: fromText,
			ToText:   toText,
			Label:    label,
			Dir:      dir,
		})
		emittedConnectorIDs[connID] = struct{}{}
		markShapeFamilyUsed(beginTarget, shapesByID, usedShapeIDs)
		markShapeFamilyUsed(endTarget, shapesByID, usedShapeIDs)
	}

	return connections, usedShapeIDs, connectorIDs, emittedConnectorIDs
}

func resolveEndpointText(shapeID string, shapesByID map[string]*vsdxShapeData) (string, string) {
	s, ok := shapesByID[shapeID]
	if !ok || s == nil {
		return "", ""
	}
	if text := shapeEffectiveText(s); text != "" {
		return s.ID, text
	}
	// If the connector glued onto an unlabeled sub-shape inside a Group, walk up
	// to the nearest enclosing shape that carries a label.
	curr := s
	for curr.ParentID != "" {
		parent, ok := shapesByID[curr.ParentID]
		if !ok || parent == nil {
			break
		}
		if text := shapeEffectiveText(parent); text != "" {
			return parent.ID, text
		}
		curr = parent
	}
	return "", ""
}

func shapeEffectiveText(s *vsdxShapeData) string {
	if s == nil {
		return ""
	}
	if s.Text != "" {
		return s.Text
	}
	if strings.EqualFold(s.Type, "Group") {
		var parts []string
		collectChildTexts(s, &parts)
		return strings.Join(parts, " ")
	}
	return ""
}

func collectChildTexts(s *vsdxShapeData, parts *[]string) {
	for _, child := range s.Children {
		if child.Text != "" {
			*parts = append(*parts, child.Text)
		}
		if len(child.Children) > 0 {
			collectChildTexts(child, parts)
		}
	}
}

func markShapeFamilyUsed(shapeID string, shapesByID map[string]*vsdxShapeData, used map[string]struct{}) {
	s, ok := shapesByID[shapeID]
	if !ok || s == nil {
		return
	}
	used[s.ID] = struct{}{}
	if s.Text == "" && strings.EqualFold(s.Type, "Group") {
		var markChildren func(*vsdxShapeData)
		markChildren = func(node *vsdxShapeData) {
			for _, child := range node.Children {
				used[child.ID] = struct{}{}
				markChildren(child)
			}
		}
		markChildren(s)
	}
}

func resolveArrowDirection(beginArrow, endArrow string) vsdxDirection {
	hasBegin := hasVisibleArrow(beginArrow)
	hasEnd := hasVisibleArrow(endArrow)
	switch {
	case hasBegin && hasEnd:
		return vsdxDirBidirectional
	case hasEnd:
		return vsdxDirForward
	case hasBegin:
		return vsdxDirBackward
	default:
		return vsdxDirUndirected
	}
}

func hasVisibleArrow(val string) bool {
	v := strings.TrimSpace(strings.ToLower(val))
	return v != "" && v != "0" && v != "none"
}

func collectExtraShapeTexts(orderedShapes []*vsdxShapeData, usedShapeIDs, connectorIDs, emittedConnectorIDs map[string]struct{}) []string {
	var extras []*vsdxShapeData
	for _, s := range orderedShapes {
		if s.Text == "" {
			continue
		}
		if _, isConn := connectorIDs[s.ID]; isConn {
			if _, emitted := emittedConnectorIDs[s.ID]; emitted {
				continue
			}
		}
		if _, used := usedShapeIDs[s.ID]; used {
			continue
		}
		extras = append(extras, s)
	}

	// Sort unconnected texts top-to-bottom, left-to-right when coordinates exist.
	sort.SliceStable(extras, func(i, j int) bool {
		a, b := extras[i], extras[j]
		if a.PinY != nil && b.PinY != nil && *a.PinY != *b.PinY {
			return *a.PinY > *b.PinY
		}
		if a.PinX != nil && b.PinX != nil && *a.PinX != *b.PinX {
			return *a.PinX < *b.PinX
		}
		return a.Order < b.Order
	})

	out := make([]string, 0, len(extras))
	for _, s := range extras {
		out = append(out, s.Text)
	}
	return out
}

func formatVSDXPageText(page vsdxPage) string {
	if len(page.Connections) == 0 && len(page.ExtraTexts) == 0 {
		return ""
	}
	var b strings.Builder
	if page.Name != "" {
		fmt.Fprintf(&b, "[Page %d: %s]", page.DisplayNum, page.Name)
	} else {
		fmt.Fprintf(&b, "[Page %d]", page.DisplayNum)
	}

	if len(page.Connections) > 0 {
		b.WriteString("\nConnections:")
		for _, conn := range page.Connections {
			b.WriteString("\n- ")
			b.WriteString(formatVSDXConnectionSentence(conn))
		}
	}
	if len(page.ExtraTexts) > 0 {
		b.WriteString("\nOther shapes and notes:")
		for _, t := range page.ExtraTexts {
			b.WriteString("\n- ")
			b.WriteString(t)
		}
	}
	return b.String()
}

func formatVSDXConnectionSentence(c vsdxConnection) string {
	var base string
	switch c.Dir {
	case vsdxDirForward, vsdxDirBackward:
		base = fmt.Sprintf("%s points to %s", c.FromText, c.ToText)
	case vsdxDirBidirectional:
		base = fmt.Sprintf("%s is bidirectionally connected to %s", c.FromText, c.ToText)
	default:
		base = fmt.Sprintf("%s is connected to %s", c.FromText, c.ToText)
	}
	if c.Label != "" {
		return base + fmt.Sprintf(", labeled %q.", c.Label)
	}
	return base + "."
}
