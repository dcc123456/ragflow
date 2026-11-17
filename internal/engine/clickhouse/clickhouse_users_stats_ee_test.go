//go:build ee

package clickhouse

import (
	"testing"
	"time"
)

// TestParseStatsDate verifies the two accepted forms and the rejection of
// garbage, including the handler-default " 00:00:00" suffix that used to fail
// the strict date-only parse in UserStats.
func TestParseStatsDate(t *testing.T) {
	datetime, err := parseStatsDate("2026-11-16 23:59:59", "end")
	if err != nil {
		t.Fatalf("datetime form rejected: %v", err)
	}
	if got := datetime.Format("2006-01-02 15:04:05"); got != "2026-11-16 23:59:59" {
		t.Fatalf("datetime form = %q", got)
	}

	dateOnly, err := parseStatsDate("2026-11-16", "start")
	if err != nil {
		t.Fatalf("date-only form rejected: %v", err)
	}
	if !dateOnly.Equal(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("date-only form = %v", dateOnly)
	}

	if _, err := parseStatsDate("not-a-date", "start"); err == nil {
		t.Fatal("garbage date must be rejected")
	}
}
