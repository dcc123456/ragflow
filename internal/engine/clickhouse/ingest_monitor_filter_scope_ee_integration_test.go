//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee && integration

package clickhouse

import "testing"

// TestQueryFilterOptions_TenantScopeNarrowsQueueIngestorStatus guards that a
// tenant-scoped caller sees only its own queue/ingestor/status candidates, not
// global enumerations that leak other tenants' operational vocabulary.
func TestQueryFilterOptions_TenantScopeNarrowsQueueIngestorStatus(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	// Precondition: the foreign-tenant vocabulary must actually exist globally,
	// otherwise the tenant-scope assertions below would pass vacuously.
	global, err := d.QueryFilterOptions(monitorAllTimeQuery())
	if err != nil {
		t.Fatalf("QueryFilterOptions (admin): %v", err)
	}
	assertContains(t, "global queues", global.Queues, "low")
	assertContains(t, "global ingestors", global.Ingestors, "parser-b")
	assertContains(t, "global statuses", global.Statuses, "FAILED")

	q := monitorAllTimeQuery()
	q.Scope = monitorScopeTenant
	q.ScopeTenant = "tenant-A"

	got, err := d.QueryFilterOptions(q)
	if err != nil {
		t.Fatalf("QueryFilterOptions (tenant): %v", err)
	}
	assertNotContains(t, "queues", got.Queues, "low")
	assertNotContains(t, "ingestors", got.Ingestors, "parser-b")
	assertNotContains(t, "statuses", got.Statuses, "FAILED")
}

func assertContains(t *testing.T, name string, list []string, want string) {
	t.Helper()
	for _, v := range list {
		if v == want {
			return
		}
	}
	t.Fatalf("%s must contain %q for the precondition, got %v", name, want, list)
}

func assertNotContains(t *testing.T, name string, list []string, forbidden string) {
	t.Helper()
	for _, v := range list {
		if v == forbidden {
			t.Errorf("%s must not contain %q for a tenant-scoped caller, got %v", name, forbidden, list)
		}
	}
}
