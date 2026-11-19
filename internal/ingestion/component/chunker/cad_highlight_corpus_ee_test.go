//go:build ee && manual

package chunker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ragflow/internal/parser/cadsource"
)

// TestCADPreviewHighlightCorpus checks the real renderer and chunk metadata
// against a local, opt-in corpus; drawings are not redistributed in the repo.
func TestCADPreviewHighlightCorpus(t *testing.T) {
	root := os.Getenv("CAD_CORPUS_DIR")
	if root == "" {
		t.Skip("set CAD_CORPUS_DIR to a local CAD corpus")
	}
	files, total, mapped := 0, 0, 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".dwg", ".dxf", ".dxfb":
		default:
			return nil
		}
		files++
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			drawing, err := cadsource.Extract(t.Context(), entry.Name(), data)
			if err != nil {
				t.Fatal(err)
			}
			chunks, _ := buildCADChunks(drawing, nil, entry.Name())
			docs := cadTestChunkDocs(t, chunks)
			doc, err := parseCADDocument(entry.Name(), data)
			if err != nil {
				t.Fatal(err)
			}
			preview, err := buildCADPreview(t.Context(), doc, drawing.Sheets, 256)
			if err != nil {
				t.Fatal(err)
			}
			diagnostics := attachCADPreviewPositions(docs, preview)
			for _, chunk := range docs {
				if _, ok := cadChunkImageMeta(chunk); !ok {
					continue
				}
				total++
				var rows [][]float64
				if err := json.Unmarshal(chunk.Positions, &rows); err != nil || len(rows) == 0 {
					t.Errorf("chunk has no preview location: %q: %v diagnostics=%v", chunk.Text, err, diagnostics)
					continue
				}
				mapped++
				for _, row := range rows {
					if len(row) != 5 || int(row[0]) < 1 || int(row[0]) > len(preview.Pages) {
						t.Fatalf("bad page reference: %v", row)
					}
					w, h, _ := cadPreviewPlacement(preview.Pages[int(row[0])-1])
					if row[1] < 0 || row[3] < 0 || row[2] > w+1 || row[4] > h+1 || row[2] <= row[1] || row[4] <= row[3] {
						t.Errorf("position outside PDF page: %v size=%vx%v", row, w, h)
					}
				}
			}
			if dir := os.Getenv("CAD_HIGHLIGHT_OUTPUT"); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, entry.Name()+".pdf"), preview.PDF, 0600); err != nil {
					t.Fatal(err)
				}
				raw, err := json.MarshalIndent(docs, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, entry.Name()+".chunks.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("no CAD files")
	}
	t.Logf("files=%d searchable_chunks=%d mapped=%d", files, total, mapped)
}
