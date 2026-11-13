//go:build ee

package parser

import (
	"testing"

	"ragflow/internal/parser/cadsource"
)

func TestCADTextItemsProjectsAttributes(t *testing.T) {
	doc := &CADDocument{
		Texts: []cadsource.Text{{
			Text:       "hello dxf",
			Kind:       cadsource.KindUnknown,
			Origin:     cadsource.OriginSheetTexts,
			SheetID:    "s1",
			Layer:      "Wall",
			X:          1,
			Y:          2,
			Height:     3,
			Rotation:   15,
			Confidence: 1,
		}},
		Attributes: []cadsource.Attribute{{
			Tag:         "MATERIAL",
			Value:       "45钢",
			OwnerHandle: 99,
			Layer:       "TITLE",
		}},
	}

	items := CADTextItems(doc)
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}

	first := items[0].ToMap()
	if first["text"] != "hello dxf" {
		t.Errorf("text = %v", first["text"])
	}
	if first["cad_sheet_id"] != "s1" || first["cad_layer"] != "Wall" {
		t.Errorf("spatial metadata = %v/%v", first["cad_sheet_id"], first["cad_layer"])
	}

	second := items[1].ToMap()
	if second["text"] != "MATERIAL: 45钢" {
		t.Errorf("attribute text = %v", second["text"])
	}
	if second["cad_tag"] != "MATERIAL" || second["cad_value"] != "45钢" {
		t.Errorf("attribute tag/value = %v/%v", second["cad_tag"], second["cad_value"])
	}
	if second["cad_owner_handle"] != uint64(99) {
		t.Errorf("owner handle = %v", second["cad_owner_handle"])
	}
}
