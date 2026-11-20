//go:build ee

package dataset

import (
	"testing"

	"ragflow/internal/entity"
)

func TestTimelineCompilationStatusRouting(t *testing.T) {
	if got := normalizeCompilationKind(" Timeline "); got != "Timeline" {
		t.Fatalf("normalized kind = %q, want Timeline", got)
	}
	for _, entry := range []entity.KnowledgeCompileEntry{
		{TaskTypes: []string{"Timeline"}},
		{Variants: []string{"structure"}},
	} {
		if !compilationStatusEntryMatches(entry, "Timeline") {
			t.Fatalf("Timeline status did not match entry: %+v", entry)
		}
		if len(entry.TaskTypes) > 0 && compilationStatusEntryMatches(entry, "Graph") {
			t.Fatalf("Timeline task matched Graph status: %+v", entry)
		}
	}
}
