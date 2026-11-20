//go:build ee

package knowledge_compiler

import (
	"testing"

	"ragflow/internal/ingestion/component/knowledge_compiler/common"
)

func TestProductsToChunkDocsTimelineKind(t *testing.T) {
	docs, err := productsToChunkDocs([]common.Product{{ID: "row", Kind: "timeline", Variant: common.VariantStructure,
		Content: "summary", Meta: map[string]any{"compile_kwd": "list", "kind": "entity"}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := docs[0].ToMap()["compile_kwd"]; got != "timeline" {
		t.Fatalf("compile_kwd = %v, want timeline", got)
	}
}
