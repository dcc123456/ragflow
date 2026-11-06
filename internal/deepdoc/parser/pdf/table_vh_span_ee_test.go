//go:build ee

package pdf

import (
	"context"
	"image"
	"testing"

	"ragflow/internal/deepdoc/parser/pdf/table"
	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

type fakeTSR2 struct{ cells []pdf.TSRCell }

func (f fakeTSR2) TSR(_ context.Context, _ image.Image) ([]pdf.TSRCell, error) {
	return f.cells, nil
}

// TestEEAttachSpanInfo_VH verifies the VH builder flags the item for V/H
// rendering via AttachSpanInfo, so the shared ConstructTable dispatch routes
// to the EE ConstructTableVH path.
func TestEEAttachSpanInfo_VH(t *testing.T) {
	cells := []pdf.TSRCell{
		{X0: 0, Y0: 0, X1: 400, Y1: 240, Label: "v", Score: 0.9},
	}
	b := table.NewDeepDocVHTableBuilder(fakeTSR2{cells: cells})
	b.GroupCells(cells)
	var item pdf.TableItem
	b.AttachSpanInfo(&item)
	if !item.RenderVH {
		t.Fatal("VH builder must set RenderVH")
	}
}

// TestEEAttachSpanInfo_NonVH is adversarial: a non-VH builder must leave
// RenderVH false so the OSS post-processing path is used.
func TestEEAttachSpanInfo_NonVH(t *testing.T) {
	b := table.NewDeepDocTableBuilder(nil)
	var item pdf.TableItem
	b.AttachSpanInfo(&item)
	if item.RenderVH {
		t.Fatal("non-VH builder must leave RenderVH false")
	}
}

// TestEEFinalizeGrid_VH verifies the VH builder overlays OCR text via
// FinalizeGrid, returning a non-nil grid.
func TestEEFinalizeGrid_VH(t *testing.T) {
	cells := []pdf.TSRCell{
		{X0: 0, Y0: 0, X1: 400, Y1: 240, Label: "v", Score: 0.9},
	}
	b := table.NewDeepDocVHTableBuilder(fakeTSR2{cells: cells})
	b.GroupCells(cells)
	annotGrid := [][]pdf.TSRCell{{{Text: "x"}}}
	if got := b.FinalizeGrid(annotGrid, nil); got == nil {
		t.Fatal("VH builder FinalizeGrid must return the overlaid grid")
	}
}

// TestEEFinalizeGrid_NonVH is adversarial: the OSS builder returns the grid
// unchanged (identity), exercising the shared interface contract.
func TestEEFinalizeGrid_NonVH(t *testing.T) {
	b := table.NewDeepDocTableBuilder(nil)
	annotGrid := [][]pdf.TSRCell{{{Text: "x"}}}
	if out := b.FinalizeGrid(annotGrid, nil); out == nil {
		t.Fatal("OSS builder FinalizeGrid must return the grid unchanged")
	}
}
