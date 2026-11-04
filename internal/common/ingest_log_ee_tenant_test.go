//go:build ee

package common

import (
	"encoding/json"
	"testing"
)

// TestIngestionLogTenantNameRoundTrips asserts the EE IngestionLog struct carries
// a TenantName field that serializes to the "tenant_name" JSON key. The OSS
// observability.BuildIngestLog (shared with the EE build) sets ev.TenantName, so
// the EE struct must expose the field or the package fails to compile.
func TestIngestionLogTenantNameRoundTrips(t *testing.T) {
	ev := IngestionLog{
		TenantID:   "tenant-1",
		TenantName: "Acme Corp",
		TaskID:     "task-1",
	}

	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	tn, ok := got["tenant_name"]
	if !ok {
		t.Fatalf("tenant_name key missing from serialized IngestionLog: %s", string(b))
	}
	if tn != "Acme Corp" {
		t.Errorf("tenant_name = %v, want %q", tn, "Acme Corp")
	}
	if got["tenant_id"] != "tenant-1" {
		t.Errorf("tenant_id = %v, want tenant-1", got["tenant_id"])
	}
}

// TestIngestionLogEmptyTenantNameOmitted asserts the EE struct's TenantName uses
// omitempty so an empty name does not emit a "tenant_name" key, matching the OSS
// struct contract (json:"tenant_name,omitempty"). This guards the JSON schema
// the EE sink/dashboards depend on.
func TestIngestionLogEmptyTenantNameOmitted(t *testing.T) {
	ev := IngestionLog{
		TenantID:   "tenant-1",
		TenantName: "",
		TaskID:     "task-1",
	}

	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := got["tenant_name"]; ok {
		t.Errorf("tenant_name should be omitted when empty, got: %s", string(b))
	}
	if got["tenant_id"] != "tenant-1" {
		t.Errorf("tenant_id = %v, want tenant-1", got["tenant_id"])
	}
}
