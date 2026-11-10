//go:build ee && integration

package clickhouse

import (
	"testing"
)

// TestInitDBRecreatesIngestionEventsFactOnUpgrade reproduces the upgrade
// scenario: an existing deployment already has model_usage_fact, but a newer
// analytics table (ingestion_events_fact) is absent. InitDB must converge the
// missing table instead of short-circuiting on the presence of model_usage_fact.
//
// Before the fix, InitDB returned early whenever model_usage_fact existed, so
// ingestion_events_fact was never created on upgraded databases and the ingest
// events were silently dropped.
func TestInitDBRecreatesIngestionEventsFactOnUpgrade(t *testing.T) {
	d := newIntegrationDriver(t)
	defer d.conn.Close()

	// Record the state we found so we restore it for the other tests in this
	// package (the table may already exist from a prior run).
	existedBefore, err := d.tableExists("ingestion_events_fact")
	if err != nil {
		t.Fatalf("tableExists before: %v", err)
	}
	defer func() {
		if existedBefore {
			_ = d.InitDB()
		}
	}()

	// Simulate the upgrade: model_usage_fact stays, ingestion_events_fact is gone.
	if err := d.conn.Exec(d.ctx, "DROP TABLE IF EXISTS ingestion_events_fact"); err != nil {
		t.Fatalf("drop ingestion_events_fact: %v", err)
	}
	gone, err := d.tableExists("ingestion_events_fact")
	if err != nil {
		t.Fatalf("tableExists after drop: %v", err)
	}
	if gone {
		t.Fatalf("precondition failed: ingestion_events_fact should have been dropped")
	}

	// Drive a second schema convergence. This must recreate the missing table.
	if err := d.InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	recreated, err := d.tableExists("ingestion_events_fact")
	if err != nil {
		t.Fatalf("tableExists after InitDB: %v", err)
	}
	if !recreated {
		t.Fatalf("ingestion_events_fact not created when model_usage_fact already exists (upgrade path)")
	}
}

// TestInitDBIsIdempotentOnExistingTables ensures a second InitDB run does not
// error and leaves the analytics tables present (retry/idempotency corner case).
func TestInitDBIsIdempotentOnExistingTables(t *testing.T) {
	d := newIntegrationDriver(t)
	defer d.conn.Close()

	// First call creates everything; the second must be a clean no-op.
	if err := d.InitDB(); err != nil {
		t.Fatalf("first InitDB: %v", err)
	}
	if err := d.InitDB(); err != nil {
		t.Fatalf("second InitDB (idempotency): %v", err)
	}

	for _, name := range []string{"model_usage_fact", "ingestion_events_fact"} {
		exists, err := d.tableExists(name)
		if err != nil {
			t.Fatalf("tableExists %s: %v", name, err)
		}
		if !exists {
			t.Fatalf("table %s missing after idempotent InitDB", name)
		}
	}
}
