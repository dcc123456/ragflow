//go:build ee

package clickhouse

import "testing"

func TestAuditResource(t *testing.T) {
	for _, tt := range []struct {
		name             string
		resources        map[string]any
		wantType, wantID string
	}{
		{"dataset", map[string]any{"dataset_id": "ds"}, "dataset", "ds"},
		{"document", map[string]any{"dataset_id": "ds", "document_id": "doc"}, "document", "doc"},
		{"chunk", map[string]any{"document_id": "doc", "chunk_id": "chunk"}, "chunk", "chunk"},
		{"agent", map[string]any{"agent_id": "agent"}, "agent", "agent"},
		{"session", map[string]any{"agent_id": "agent", "session_id": "session"}, "session", "session"},
		{"version", map[string]any{"agent_id": "agent", "version_id": "version"}, "version", "version"},
		{"search", map[string]any{"search_id": "search"}, "search", "search"},
		{"search over dataset", map[string]any{"search_id": "search", "dataset_id": "ds"}, "search", "search"},
		{"memory", map[string]any{"memory_id": "memory"}, "memory", "memory"},
		{"memory over dataset", map[string]any{"memory_id": "memory", "dataset_id": "ds"}, "memory", "memory"},
		{"bulk", map[string]any{"document_ids": []string{"a", "b"}}, "", ""},
		{"absent", nil, "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotID := auditResource(tt.resources)
			if gotType != tt.wantType || gotID != tt.wantID {
				t.Fatalf("got (%q, %q), want (%q, %q)", gotType, gotID, tt.wantType, tt.wantID)
			}
		})
	}
}
