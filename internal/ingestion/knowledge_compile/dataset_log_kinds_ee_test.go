//go:build ee

package knowledge_compile

import (
	"slices"
	"testing"

	kccommon "ragflow/internal/ingestion/component/knowledge_compiler/common"
)

func TestTimelineDatasetLogRouting(t *testing.T) {
	want := []string{kccommon.TaskTypeWiki, kccommon.TaskTypeTree, kccommon.TaskTypeGraph, kccommon.TaskTypeMindmap, kccommon.TaskTypeTimeline, kccommon.TaskTypePageIndex}
	if !slices.Equal(allDatasetTaskTypes, want) {
		t.Fatalf("dataset task types = %v, want %v", allDatasetTaskTypes, want)
	}

	for _, entry := range []BacklogEntry{
		{TaskTypes: []string{" timeline "}},
		{Variants: []string{"structure"}},
		{},
	} {
		if got := taskTypesForEntry(entry); !slices.Contains(got, kccommon.TaskTypeTimeline) {
			t.Fatalf("entry %+v task types = %v, want Timeline", entry, got)
		}
	}
}

func TestTimelineCompiledKind(t *testing.T) {
	variant, err := KwdToVariant("timeline")
	if err != nil || variant != kccommon.VariantStructure {
		t.Fatalf("KwdToVariant(timeline) = %q, %v", variant, err)
	}
}
