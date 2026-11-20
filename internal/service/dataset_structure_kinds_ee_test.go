//go:build ee

package service

import "testing"

func TestResolveDatasetStructureKindTimeline(t *testing.T) {
	if got := resolveDatasetStructureKind(" Timeline "); got != "timeline" {
		t.Fatalf("resolved kind = %q, want timeline", got)
	}
}
