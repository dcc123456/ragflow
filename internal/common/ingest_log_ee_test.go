//go:build ee

package common

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestSetIngestLogSinkForwards verifies that an installed sink receives every
// event emitted through LogIngest, with no extra/duplicate delivery.
func TestSetIngestLogSinkForwards(t *testing.T) {
	defer SetIngestLogSink(nil)

	var mu sync.Mutex
	var got []IngestionLog
	SetIngestLogSink(func(log IngestionLog) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, log)
		return nil
	})

	in := IngestionLog{
		EventTime: time.Now().UTC(),
		TenantID:  "t-1",
		TaskID:    "task-1",
		EventType: "created",
		Status:    "CREATED",
	}
	LogIngest(&in)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected sink to receive exactly 1 log, got %d", len(got))
	}
	if got[0].TaskID != "task-1" || got[0].TenantID != "t-1" {
		t.Fatalf("sink received unexpected log: %+v", got[0])
	}
}

// TestLogIngestNoSinkNoPanic verifies the seam is safe when no sink is
// registered (the default no-op state), so a missing ClickHouse registration
// can never break the ingestion pipeline.
func TestLogIngestNoSinkNoPanic(t *testing.T) {
	defer SetIngestLogSink(nil)
	SetIngestLogSink(nil)

	LogIngest(&IngestionLog{
		EventTime: time.Now().UTC(),
		TaskID:    "task-x",
		EventType: "created",
		Status:    "CREATED",
	})
}

// TestSetIngestLogSinkReplace verifies re-installing a sink replaces the
// previous one rather than stacking handlers.
func TestSetIngestLogSinkReplace(t *testing.T) {
	defer SetIngestLogSink(nil)

	var firstCount, secondCount int
	SetIngestLogSink(func(log IngestionLog) error { firstCount++; return nil })
	SetIngestLogSink(func(log IngestionLog) error { secondCount++; return nil })

	LogIngest(&IngestionLog{TaskID: "t", EventType: "created"})

	if firstCount != 0 {
		t.Fatalf("old sink should not fire after replacement, got %d", firstCount)
	}
	if secondCount != 1 {
		t.Fatalf("new sink should fire exactly once, got %d", secondCount)
	}
}

// recordingSink returns a sink that records every delivered event and a getter
// returning a snapshot of the recorded slice.
func recordingSink() (func(IngestionLog) error, func() []IngestionLog) {
	var mu sync.Mutex
	var got []IngestionLog
	sink := func(log IngestionLog) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, log)
		return nil
	}
	get := func() []IngestionLog {
		mu.Lock()
		defer mu.Unlock()
		return append([]IngestionLog(nil), got...)
	}
	return sink, get
}

// TestLogIngestCopiesOperationBeforeEnriching is the core guard for the
// copy-before-enrich contract: the caller's Operation map must never be
// mutated, and the sink must receive an independent, fully enriched copy.
func TestLogIngestCopiesOperationBeforeEnriching(t *testing.T) {
	defer SetIngestLogSink(nil)
	sink, got := recordingSink()
	SetIngestLogSink(sink)

	orig := map[string]any{"custom": "keep"}
	log := &IngestionLog{
		EventTime:  time.Now().UTC(),
		TaskID:     "t1",
		EventType:  "completed",
		Status:     "COMPLETED",
		UserName:   "alice",
		TenantName: "Acme",
		Operation:  orig,
	}
	LogIngest(log)

	if len(orig) != 1 || orig["custom"] != "keep" {
		t.Fatalf("caller Operation was mutated: %v", orig)
	}
	events := got()
	if len(events) != 1 {
		t.Fatalf("expected 1 delivered event, got %d", len(events))
	}
	op := events[0].Operation
	want := map[string]any{
		"custom": "keep", "event_label": "Completed", "terminal": true,
		"has_error": false, "user_name": "alice", "tenant_name": "Acme",
	}
	if len(op) != len(want) {
		t.Fatalf("enriched operation has %d keys, want %d: %v", len(op), len(want), op)
	}
	for k, v := range want {
		if op[k] != v {
			t.Fatalf("operation[%q] = %v, want %v", k, op[k], v)
		}
	}
	// Prove the maps are distinct: mutating the caller after delivery must not
	// be observable through the delivered copy.
	orig["custom"] = "changed"
	if op["custom"] != "keep" {
		t.Fatal("sink Operation aliases the caller's map")
	}
}

// TestLogIngestRepeatedCallDoesNotAccumulate is the adversarial fixture: a
// future caller reusing one IngestionLog across emits must not accumulate
// derived keys from earlier calls.
func TestLogIngestRepeatedCallDoesNotAccumulate(t *testing.T) {
	defer SetIngestLogSink(nil)
	sink, got := recordingSink()
	SetIngestLogSink(sink)

	log := &IngestionLog{
		TaskID: "t", EventType: "created", Status: "CREATED",
		Operation: map[string]any{"k": "v"},
	}
	LogIngest(log)
	LogIngest(log)

	events := got()
	if len(events) != 2 {
		t.Fatalf("expected 2 delivered events, got %d", len(events))
	}
	for i, e := range events {
		// k + event_label + terminal + has_error; no name fields for this log.
		if len(e.Operation) != 4 {
			t.Fatalf("emit %d accumulated derived keys: %v", i, e.Operation)
		}
		if e.Operation["event_label"] != "Created" {
			t.Fatalf("emit %d lost event_label: %v", i, e.Operation)
		}
	}
	if len(log.Operation) != 1 || log.Operation["k"] != "v" {
		t.Fatalf("caller mutated across emits: %v", log.Operation)
	}
}

// TestLogIngestIgnoresSinkError covers the error path: a sink signalling
// backpressure (full channel) must not panic or break the caller.
func TestLogIngestIgnoresSinkError(t *testing.T) {
	defer SetIngestLogSink(nil)
	SetIngestLogSink(func(IngestionLog) error { return errors.New("channel full") })
	LogIngest(&IngestionLog{TaskID: "t", EventType: "created"})
}

// TestLogIngestDerivedDimensions is table-driven coverage for boundary and
// adversarial inputs: nil/empty Operation, empty event type, both has_error
// triggers, omitting one of the coupled name fields, and Unicode names.
func TestLogIngestDerivedDimensions(t *testing.T) {
	cases := []struct {
		name string
		log  IngestionLog
		want map[string]any
	}{
		{
			name: "nil operation map",
			log:  IngestionLog{EventType: "running", Status: "RUNNING"},
			want: map[string]any{"event_label": "Running", "terminal": false, "has_error": false},
		},
		{
			name: "empty operation map",
			log:  IngestionLog{EventType: "running", Status: "RUNNING", Operation: map[string]any{}},
			want: map[string]any{"event_label": "Running", "terminal": false, "has_error": false},
		},
		{
			name: "empty event type falls through to raw value",
			log:  IngestionLog{EventType: "", Status: ""},
			want: map[string]any{"event_label": "", "terminal": false, "has_error": false},
		},
		{
			name: "failed status triggers has_error and terminal",
			log:  IngestionLog{EventType: "failed", Status: "FAILED"},
			want: map[string]any{"event_label": "Failed", "terminal": true, "has_error": true},
		},
		{
			name: "error info alone triggers has_error",
			log:  IngestionLog{EventType: "completed", Status: "COMPLETED", ErrorInfo: "boom"},
			want: map[string]any{"event_label": "Completed", "terminal": true, "has_error": true},
		},
		{
			name: "only user name (tenant omitted)",
			log:  IngestionLog{EventType: "created", UserName: "张三"},
			want: map[string]any{"event_label": "Created", "terminal": false, "has_error": false, "user_name": "张三"},
		},
		{
			name: "only tenant name (user omitted)",
			log:  IngestionLog{EventType: "created", TenantName: "Acme"},
			want: map[string]any{"event_label": "Created", "terminal": false, "has_error": false, "tenant_name": "Acme"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer SetIngestLogSink(nil)
			sink, got := recordingSink()
			SetIngestLogSink(sink)

			orig := tc.log.Operation
			origLen := len(orig)
			origKeys := make(map[string]any, origLen)
			for k, v := range orig {
				origKeys[k] = v
			}
			LogIngest(&tc.log)

			events := got()
			if len(events) != 1 {
				t.Fatalf("expected 1 delivered event, got %d", len(events))
			}
			op := events[0].Operation
			if len(op) != len(tc.want) {
				t.Fatalf("operation has %d keys, want %d: %v", len(op), len(tc.want), op)
			}
			for k, v := range tc.want {
				if op[k] != v {
					t.Fatalf("operation[%q] = %v, want %v", k, op[k], v)
				}
			}
			// The caller's Operation must be untouched: nil stays nil, and a
			// non-nil map keeps exactly its original key set and values.
			if orig == nil {
				if tc.log.Operation != nil {
					t.Fatalf("caller nil Operation was replaced: %v", tc.log.Operation)
				}
			} else {
				if len(tc.log.Operation) != origLen {
					t.Fatalf("caller Operation gained/lost keys: %v", tc.log.Operation)
				}
				for k, v := range origKeys {
					if cur, ok := tc.log.Operation[k]; !ok || cur != v {
						t.Fatalf("caller Operation[%q] changed: %v", k, tc.log.Operation)
					}
				}
			}
		})
	}
}
