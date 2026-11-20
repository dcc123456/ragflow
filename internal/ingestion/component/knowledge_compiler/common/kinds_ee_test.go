//go:build ee

package common

import "testing"

func TestTimelineKind(t *testing.T) {
	if got, err := KindToVariant("timeline"); err != nil || got != VariantStructure {
		t.Fatalf("KindToVariant(timeline) = %q, %v", got, err)
	}
	if got, err := KindToTaskType("timeline"); err != nil || got != TaskTypeTimeline {
		t.Fatalf("KindToTaskType(timeline) = %q, %v", got, err)
	}
}
