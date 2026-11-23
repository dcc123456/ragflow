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

package chunker

import (
	"archive/zip"
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"ragflow/internal/ingestion/component"
	"ragflow/internal/ingestion/task/indexdoc"
)

func buildChunkerTestVSDX(t *testing.T) []byte {
	t.Helper()
	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.microsoft.com/visio/2010/relationships/document" Target="visio/document.xml"/>
</Relationships>`,
		"visio/document.xml": `<?xml version="1.0" encoding="UTF-8"?><VisioDocument xmlns="http://schemas.microsoft.com/office/visio/2012/main"/>`,
		"visio/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.microsoft.com/visio/2010/relationships/pages" Target="pages/pages.xml"/>
</Relationships>`,
		"visio/pages/pages.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Pages xmlns="http://schemas.microsoft.com/office/visio/2012/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <Page ID="9" Name="Bg" Background="1"><Rel r:id="rId1"/></Page>
  <Page ID="0" Name="架构总览"><Rel r:id="rId2"/></Page>
  <Page ID="7" Name="订单链路"><Rel r:id="rId3"/></Page>
</Pages>`,
		"visio/pages/_rels/pages.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.microsoft.com/visio/2010/relationships/page" Target="page1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.microsoft.com/visio/2010/relationships/page" Target="page2.xml"/>
  <Relationship Id="rId3" Type="http://schemas.microsoft.com/visio/2010/relationships/page" Target="page3.xml"/>
</Relationships>`,
		"visio/pages/page1.xml": `<PageContents xmlns="http://schemas.microsoft.com/office/visio/2012/main"><Shapes><Shape ID="1"><Text>BG</Text></Shape></Shapes></PageContents>`,
		"visio/pages/page2.xml": `<PageContents xmlns="http://schemas.microsoft.com/office/visio/2012/main">
  <Shapes>
    <Shape ID="1"><Text>用户</Text></Shape>
    <Shape ID="2"><Text>Go 服务</Text></Shape>
    <Shape ID="3"><Cell N="EndArrow" V="4"/><Text>HTTP 请求</Text></Shape>
  </Shapes>
  <Connects>
    <Connect FromSheet="3" FromCell="BeginX" ToSheet="1"/>
    <Connect FromSheet="3" FromCell="EndX" ToSheet="2"/>
  </Connects>
</PageContents>`,
		"visio/pages/page3.xml": `<PageContents xmlns="http://schemas.microsoft.com/office/visio/2012/main">
  <Shapes>
    <Shape ID="1"><Text>Go 服务</Text></Shape>
    <Shape ID="2"><Text>MySQL</Text></Shape>
    <Shape ID="3"><Cell N="EndArrow" V="4"/></Shape>
  </Shapes>
  <Connects>
    <Connect FromSheet="3" FromCell="BeginX" ToSheet="1"/>
    <Connect FromSheet="3" FromCell="EndX" ToSheet="2"/>
  </Connects>
</PageContents>`,
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPageChunkerVSDXEndToEnd(t *testing.T) {
	pComp, err := component.NewParserComponent(nil)
	if err != nil {
		t.Fatalf("NewParserComponent: %v", err)
	}
	parserOut, err := pComp.Invoke(t.Context(), nil, map[string]any{
		"name":   "system.vsdx",
		"binary": buildChunkerTestVSDX(t),
	})
	if err != nil {
		t.Fatalf("ParserComponent.Invoke: %v", err)
	}
	if got := parserOut["file_type"]; got != "visio" {
		t.Fatalf("file_type = %v, want visio", got)
	}

	chunks := slideChunksOf(t, parserOut)
	if len(chunks) != 2 {
		t.Fatalf("chunks len = %d, want 2: %#v", len(chunks), chunks)
	}
	if txt, _ := chunks[0]["text"].(string); !strings.Contains(txt, "用户 points to Go 服务") {
		t.Fatalf("chunk[0] text unexpected: %s", txt)
	}
	if txt, _ := chunks[1]["text"].(string); !strings.Contains(txt, "Go 服务 points to MySQL") {
		t.Fatalf("chunk[1] text unexpected: %s", txt)
	}

	if _, err := indexdoc.ProcessChunksForPipeline(chunks, "doc-visio-1", "system.vsdx", time.Now()); err != nil {
		t.Fatalf("ProcessChunksForPipeline: %v", err)
	}
	if got := chunks[0]["page_num_int"]; !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("chunks[0].page_num_int = %#v, want [1]", got)
	}
	if got := chunks[0]["position_int"]; !reflect.DeepEqual(got, [][]int{{1, 0, 0, 0, 0}}) {
		t.Errorf("chunks[0].position_int = %#v, want [[1 0 0 0 0]]", got)
	}
	if got := chunks[1]["page_num_int"]; !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("chunks[1].page_num_int = %#v, want [2]", got)
	}
	if got := chunks[1]["position_int"]; !reflect.DeepEqual(got, [][]int{{2, 7, 0, 0, 0}}) {
		t.Errorf("chunks[1].position_int = %#v, want [[2 7 0 0 0]]", got)
	}
}
