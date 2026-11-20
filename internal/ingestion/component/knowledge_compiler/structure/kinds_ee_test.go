//go:build ee

package structure

import (
	"testing"

	"ragflow/internal/ingestion/component/knowledge_compiler/common"
)

func TestDropIsolatedTimelineEntities(t *testing.T) {
	rows := []common.Product{
		entRow("e-a", "Alpha"), entRow("e-b", "Beta"), entRow("e-o", "Orphan"),
		relRow("r-ab", "Alpha", "Beta"),
	}
	out := dropIsolatedTimelineEntities(rows)
	if len(out) != 3 {
		t.Fatalf("expected 3 survivors (2 entities + 1 relation), got %d", len(out))
	}
	for _, row := range out {
		if row.ID == "e-o" {
			t.Fatalf("orphan timeline entity must be dropped")
		}
	}
}

// TestStructureRunTimelineKind exercises the full timeline pipeline end to
// end: extraction → LLM dedup → chain correction (fan-out dropped) →
// orphan-entity cleanup (the dropped relation's target becomes isolated).
func TestStructureRunTimelineKind(t *testing.T) {
	deps := common.Deps{Chat: &graphChat{}, Embed: hashEmbedder{dim: 16}, TenantID: "t1", DatasetID: "d1"}
	param := testParam()
	inputs := common.Inputs{
		DocID: "doc1",
		Chunks: []common.Chunk{
			{ID: "c1", Text: "Alpha is a Beta."},
			{ID: "c2", Text: "Alpha also links Gamma."},
		},
		VariantSpecific: map[string]any{"parser_config": map[string]any{
			"kind": "timeline",
			"entity": map[string]any{"fields": []any{
				map[string]any{"type": "letter", "description": "a single letter entity"},
			}},
			"relation": map[string]any{"fields": []any{
				map[string]any{"type": "linked", "description": "a link between letters"},
			}},
		}},
	}
	out, err := Run(t.Context(), deps, param, inputs)
	if err != nil {
		t.Fatalf("Run timeline: %v", err)
	}
	var entities, relations int
	for _, p := range out.Products {
		switch p.Meta["kind"] {
		case "entity":
			entities++
			if p.Meta["name"] == "Gamma" {
				t.Errorf("Gamma must be dropped: its only relation was corrected away, leaving it isolated")
			}
		case "relation":
			relations++
			if p.Meta["to"] == "Gamma" {
				t.Errorf("fan-out relation Alpha→Gamma must be dropped by chain correction")
			}
		}
	}
	if entities != 2 || relations != 1 {
		t.Fatalf("timeline survivors = %d entities + %d relations, want 2+1 (Alpha, Beta, Alpha→Beta)", entities, relations)
	}
}
