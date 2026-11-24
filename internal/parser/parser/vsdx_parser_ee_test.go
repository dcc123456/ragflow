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
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"ragflow/internal/utility"
)

func buildTestVSDX(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func testVSDXBaseEntries() map[string]string {
	return map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.microsoft.com/visio/2010/relationships/document" Target="visio/document.xml"/>
</Relationships>`,
		"visio/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<VisioDocument xmlns="http://schemas.microsoft.com/office/visio/2012/main"/>`,
		"visio/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.microsoft.com/visio/2010/relationships/masters" Target="masters/masters.xml"/>
  <Relationship Id="rId2" Type="http://schemas.microsoft.com/visio/2010/relationships/pages" Target="pages/pages.xml"/>
</Relationships>`,
		"visio/masters/masters.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Masters xmlns="http://schemas.microsoft.com/office/visio/2012/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <Master ID="1" NameU="Dynamic connector"><Rel r:id="rId1"/></Master>
  <Master ID="2" NameU="Database"><Rel r:id="rId2"/></Master>
</Masters>`,
		"visio/masters/_rels/masters.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.microsoft.com/visio/2010/relationships/master" Target="master1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.microsoft.com/visio/2010/relationships/master" Target="master2.xml"/>
</Relationships>`,
		"visio/masters/master1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<MasterContents xmlns="http://schemas.microsoft.com/office/visio/2012/main">
  <Shapes>
    <Shape ID="5" Type="Shape">
      <Cell N="BeginArrow" V="0"/>
      <Cell N="EndArrow" V="4"/>
    </Shape>
  </Shapes>
</MasterContents>`,
		"visio/masters/master2.xml": `<?xml version="1.0" encoding="UTF-8"?>
<MasterContents xmlns="http://schemas.microsoft.com/office/visio/2012/main">
  <Shapes>
    <Shape ID="6" Type="Shape">
      <Text>MySQL 默认模板</Text>
    </Shape>
  </Shapes>
</MasterContents>`,
	}
}

func TestVSDXParser_MultiPageBackgroundMastersAndExplicitConnects(t *testing.T) {
	entries := testVSDXBaseEntries()
	entries["visio/pages/pages.xml"] = `<?xml version="1.0" encoding="UTF-8"?>
<Pages xmlns="http://schemas.microsoft.com/office/visio/2012/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <Page ID="99" Name="全局页眉背景" Background="1"><Rel r:id="rId1"/></Page>
  <Page ID="0" Name="核心架构" Background="0"><Rel r:id="rId2"/></Page>
  <Page ID="4" Name="无文字纯图页"><Rel r:id="rId3"/></Page>
  <Page ID="7" Name="数据流向"><Rel r:id="rId4"/></Page>
</Pages>`
	entries["visio/pages/_rels/pages.xml.rels"] = `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.microsoft.com/visio/2010/relationships/page" Target="page1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.microsoft.com/visio/2010/relationships/page" Target="page2.xml"/>
  <Relationship Id="rId3" Type="http://schemas.microsoft.com/visio/2010/relationships/page" Target="page3.xml"/>
  <Relationship Id="rId4" Type="http://schemas.microsoft.com/visio/2010/relationships/page" Target="page4.xml"/>
</Relationships>`

	// Background page text must be ignored from foreground chunks.
	entries["visio/pages/page1.xml"] = `<?xml version="1.0" encoding="UTF-8"?>
<PageContents xmlns="http://schemas.microsoft.com/office/visio/2012/main">
  <Shapes><Shape ID="1"><Text>机密水印页眉</Text></Shape></Shapes>
</PageContents>`

	// Foreground page 1 (VisioID=0):
	// - Shape 1 (用户) -> Shape 2 (Go 服务), Master=1 inherits EndArrow=4, label="HTTP 请求"
	// - Shape 2 -> Group 3 (child 31="MySQL", child 32="主库") via child 31, undirected (EndArrow=0 overrides master)
	// - Shape 4 (Redis) has an unglued line (Shape 11) with NO Connects -> must NOT infer relation!
	// - Shape 5 (独立注释) standalone note
	entries["visio/pages/page2.xml"] = `<?xml version="1.0" encoding="UTF-8"?>
<PageContents xmlns="http://schemas.microsoft.com/office/visio/2012/main">
  <Shapes>
    <Shape ID="1" Type="Shape"><Cell N="PinX" V="1"/><Cell N="PinY" V="5"/><Text><pp IX="0"/>用户</Text></Shape>
    <Shape ID="2" Type="Shape"><Cell N="PinX" V="3"/><Cell N="PinY" V="5"/><Text>Go<pp IX="1"/>服务</Text></Shape>
    <Shape ID="3" Type="Group">
      <Shapes>
        <Shape ID="31" Type="Shape"><Text/></Shape>
        <Shape ID="32" Type="Shape"><Text>MySQL</Text></Shape>
      </Shapes>
    </Shape>
    <Shape ID="4" Type="Shape" Master="2"><Cell N="PinX" V="6"/><Cell N="PinY" V="5"/><Text>Redis</Text></Shape>
    <Shape ID="5" Type="Shape"><Cell N="PinX" V="1"/><Cell N="PinY" V="1"/><Text>独立注释说明</Text></Shape>
    <Shape ID="10" Type="Shape" Master="1"><Text>HTTP 请求</Text></Shape>
    <Shape ID="11" Type="Shape" Master="1"><Text>未粘连线标注</Text></Shape>
    <Shape ID="12" Type="Shape" Master="1"><Cell N="BeginArrow" V="0"/><Cell N="EndArrow" V="0"/></Shape>
  </Shapes>
  <Connects>
    <Connect FromSheet="10" FromCell="BeginX" ToSheet="1"/>
    <Connect FromSheet="10" FromCell="BeginX" ToSheet="1"/>
    <Connect FromSheet="10" FromCell="EndX" ToSheet="2"/>
    <Connect FromSheet="12" FromCell="BeginX" ToSheet="2"/>
    <Connect FromSheet="12" FromCell="EndX" ToSheet="31"/>
    <Connect FromSheet="11" FromCell="BeginX" ToSheet="2"/>
  </Connects>
</PageContents>`

	// Foreground page 2 (VisioID=4): blank / no text -> omitted from JSON items, counted in page_count.
	entries["visio/pages/page3.xml"] = `<?xml version="1.0" encoding="UTF-8"?>
<PageContents xmlns="http://schemas.microsoft.com/office/visio/2012/main">
  <Shapes><Shape ID="1" Type="Shape"/></Shapes>
</PageContents>`

	// Foreground page 3 (VisioID=7): master text inheritance + backward arrow + bidirectional + ambiguous endpoint ignored.
	entries["visio/pages/page4.xml"] = `<?xml version="1.0" encoding="UTF-8"?>
<PageContents xmlns="http://schemas.microsoft.com/office/visio/2012/main">
  <Shapes>
    <Shape ID="1" Type="Shape"><Text>客户端</Text></Shape>
    <Shape ID="2" Type="Shape" Master="2"/>
    <Shape ID="3" Type="Shape"><Text>缓存</Text></Shape>
    <Shape ID="20" Type="Shape"><Cell N="BeginArrow" V="4"/><Cell N="EndArrow" V="0"/><Text>回传</Text></Shape>
    <Shape ID="21" Type="Shape"><Cell N="BeginArrow" V="4"/><Cell N="EndArrow" V="4"/></Shape>
    <Shape ID="22" Type="Shape"><Text>歧义连接</Text></Shape>
  </Shapes>
  <Connects>
    <Connect FromSheet="20" FromCell="BeginX" ToSheet="1"/>
    <Connect FromSheet="20" FromCell="EndX" ToSheet="2"/>
    <Connect FromSheet="21" FromCell="BeginX" ToSheet="1"/>
    <Connect FromSheet="21" FromCell="EndX" ToSheet="3"/>
    <Connect FromSheet="22" FromCell="BeginX" ToSheet="1"/>
    <Connect FromSheet="22" FromCell="BeginX" ToSheet="2"/>
    <Connect FromSheet="22" FromCell="EndX" ToSheet="3"/>
  </Connects>
</PageContents>`

	data := buildTestVSDX(t, entries)

	// Verify GetParser & TryParse routing.
	p, ft, err := GetParser(utility.FileTypeVisio, data)
	if err != nil {
		t.Fatalf("GetParser: %v", err)
	}
	if ft != utility.FileTypeVisio {
		t.Fatalf("detected type = %q, want %q", ft, utility.FileTypeVisio)
	}

	res := p.ParseWithResult(context.Background(), "arch.vsdx", data)
	if res.Err != nil {
		t.Fatalf("ParseWithResult error: %v", res.Err)
	}
	if got := res.File["page_count"]; got != 3 {
		t.Fatalf("page_count = %v, want 3 foreground pages", got)
	}
	if len(res.JSON) != 2 {
		t.Fatalf("len(JSON) = %d, want 2 non-empty foreground pages", len(res.JSON))
	}

	p1 := res.JSON[0].ToMap()
	p1Text, _ := p1["text"].(string)
	if strings.Contains(p1Text, "机密水印页眉") {
		t.Fatalf("background page text leaked into foreground output: %s", p1Text)
	}
	for _, want := range []string{
		"[Page 1: 核心架构]",
		"Connections:",
		`用户 points to Go 服务, labeled "HTTP 请求".`,
		"Go 服务 is connected to MySQL.",
		"Other shapes and notes:",
		"Redis",
		"独立注释说明",
		"未粘连线标注",
	} {
		if !strings.Contains(p1Text, want) {
			t.Errorf("page 1 text missing %q:\n%s", want, p1Text)
		}
	}
	if strings.Contains(p1Text, "Go 服务 points to Redis") || strings.Contains(p1Text, "Go 服务 is connected to Redis") {
		t.Errorf("unglued connector generated spurious relation:\n%s", p1Text)
	}
	pos1, err := json.Marshal(p1["positions"])
	if err != nil || string(pos1) != "[[1,0,0,0,0]]" {
		t.Errorf("page 1 positions = %s (err=%v), want [[1,0,0,0,0]]", string(pos1), err)
	}

	p3 := res.JSON[1].ToMap()
	p3Text, _ := p3["text"].(string)
	for _, want := range []string{
		"[Page 3: 数据流向]",
		`MySQL 默认模板 points to 客户端, labeled "回传".`,
		"客户端 is bidirectionally connected to 缓存.",
		"歧义连接",
	} {
		if !strings.Contains(p3Text, want) {
			t.Errorf("page 3 text missing %q:\n%s", want, p3Text)
		}
	}
	if strings.Contains(p3Text, "MySQL 默认模板 points to 缓存") {
		t.Errorf("ambiguous connector generated spurious relation:\n%s", p3Text)
	}
	pos3, err := json.Marshal(p3["positions"])
	if err != nil || string(pos3) != "[[3,7,0,0,0]]" {
		t.Errorf("page 3 positions = %s (err=%v), want [[3,7,0,0,0]]", string(pos3), err)
	}

	t.Run("macro-enabled drawing preserves page output", func(t *testing.T) {
		entries["[Content_Types].xml"] = `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Override PartName="/visio/document.xml" ContentType="application/vnd.ms-visio.drawing.macroEnabled.main+xml"/>
  <Override PartName="/visio/vbaProject.bin" ContentType="application/vnd.ms-office.vbaProject"/>
</Types>`
		entries["visio/_rels/document.xml.rels"] = strings.Replace(entries["visio/_rels/document.xml.rels"], "</Relationships>", `
  <Relationship Id="rId3" Type="http://schemas.microsoft.com/office/2006/relationships/vbaProject" Target="vbaProject.bin"/>
</Relationships>`, 1)
		entries["visio/vbaProject.bin"] = "opaque VBA data ignored during drawing extraction"
		data := buildTestVSDX(t, entries)
		p, _, err := GetParser(utility.GetFileType("arch.vsdm"), data)
		if err != nil {
			t.Fatal(err)
		}
		macro := p.ParseWithResult(t.Context(), "arch.vsdm", data)
		if macro.Err != nil {
			t.Fatal(macro.Err)
		}
		if !reflect.DeepEqual(macro.JSON, res.JSON) || macro.File["page_count"] != res.File["page_count"] {
			t.Fatalf("adding the VBA part changed drawing output: format metadata %v", macro.File)
		}
	})
}

func TestVSDXParser_InvalidInputs(t *testing.T) {
	p := visioParser{}
	if res := p.ParseWithResult(context.Background(), "empty.vsdx", nil); res.Err == nil {
		t.Fatal("expected error for empty data")
	}
	if res := p.ParseWithResult(context.Background(), "bad.vsdx", []byte("not a zip")); res.Err == nil {
		t.Fatal("expected error for invalid zip")
	}
}

func TestVSDXParser_MissingPageDoesNotAdvanceDisplayNum(t *testing.T) {
	entries := testVSDXBaseEntries()
	entries["visio/pages/pages.xml"] = `<?xml version="1.0" encoding="UTF-8"?>
<Pages xmlns="http://schemas.microsoft.com/office/visio/2012/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <Page ID="1" Name="BrokenRel"><Rel r:id="rIdMissing"/></Page>
  <Page ID="2" Name="MissingEntry"><Rel r:id="rId2"/></Page>
  <Page ID="5" Name="Healthy"><Rel r:id="rId3"/></Page>
</Pages>`
	entries["visio/pages/_rels/pages.xml.rels"] = `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId2" Type="http://schemas.microsoft.com/visio/2010/relationships/page" Target="missing.xml"/>
  <Relationship Id="rId3" Type="http://schemas.microsoft.com/visio/2010/relationships/page" Target="page3.xml"/>
</Relationships>`
	entries["visio/pages/page3.xml"] = `<PageContents xmlns="http://schemas.microsoft.com/office/visio/2012/main">
  <Shapes><Shape ID="1" Type="Shape"><Text>Service A</Text></Shape></Shapes>
</PageContents>`

	res := (visioParser{}).ParseWithResult(context.Background(), "skipped.vsdx", buildTestVSDX(t, entries))
	if res.Err != nil {
		t.Fatalf("ParseWithResult: %v", res.Err)
	}
	if got := res.File["page_count"]; got != 1 {
		t.Fatalf("page_count = %v, want 1", got)
	}
	if len(res.JSON) != 1 {
		t.Fatalf("len(JSON) = %d, want 1", len(res.JSON))
	}
	m := res.JSON[0].ToMap()
	if txt, _ := m["text"].(string); !strings.Contains(txt, "[Page 1: Healthy]") {
		t.Errorf("unexpected header in %q, want [Page 1: Healthy]", txt)
	}
	pos, _ := json.Marshal(m["positions"])
	if string(pos) != "[[1,5,0,0,0]]" {
		t.Errorf("positions = %s, want [[1,5,0,0,0]]", string(pos))
	}
}

func TestVSDXParser_ResourceGuards(t *testing.T) {
	p := visioParser{}

	t.Run("input size limit", func(t *testing.T) {
		orig := maxVisioInputBytes
		maxVisioInputBytes = 10
		t.Cleanup(func() { maxVisioInputBytes = orig })
		if res := p.ParseWithResult(context.Background(), "large.vsdx", make([]byte, 11)); res.Err == nil || !strings.Contains(res.Err.Error(), "exceeds") {
			t.Fatalf("expected input size limit error, got %v", res.Err)
		}
	})

	t.Run("single entry size limit", func(t *testing.T) {
		orig := maxVSDXEntryBytes
		maxVSDXEntryBytes = 64
		t.Cleanup(func() { maxVSDXEntryBytes = orig })
		entries := testVSDXBaseEntries()
		entries["_rels/.rels"] = strings.Repeat(" ", 128)
		if res := p.ParseWithResult(context.Background(), "entry.vsdx", buildTestVSDX(t, entries)); res.Err == nil || !strings.Contains(res.Err.Error(), "exceeds size budget") {
			t.Fatalf("expected entry budget error, got %v", res.Err)
		}
	})

	t.Run("cumulative uncompressed budget", func(t *testing.T) {
		orig := maxVSDXUncompressedBytes
		maxVSDXUncompressedBytes = 350
		t.Cleanup(func() { maxVSDXUncompressedBytes = orig })
		entries := testVSDXBaseEntries()
		entries["visio/pages/pages.xml"] = `<?xml version="1.0" encoding="UTF-8"?><Pages xmlns="http://schemas.microsoft.com/office/visio/2012/main"/>`
		if res := p.ParseWithResult(context.Background(), "total.vsdx", buildTestVSDX(t, entries)); res.Err == nil || !strings.Contains(res.Err.Error(), "exceeds") {
			t.Fatalf("expected cumulative uncompressed limit error, got %v", res.Err)
		}
	})

	t.Run("page count limit", func(t *testing.T) {
		orig := maxVisioPages
		maxVisioPages = 2
		t.Cleanup(func() { maxVisioPages = orig })
		entries := testVSDXBaseEntries()
		entries["visio/pages/pages.xml"] = `<?xml version="1.0" encoding="UTF-8"?>
<Pages xmlns="http://schemas.microsoft.com/office/visio/2012/main">
  <Page ID="1"/><Page ID="2"/><Page ID="3"/>
</Pages>`
		if res := p.ParseWithResult(context.Background(), "pages.vsdx", buildTestVSDX(t, entries)); res.Err == nil || !strings.Contains(res.Err.Error(), "page count") {
			t.Fatalf("expected page count limit error, got %v", res.Err)
		}
	})
}
