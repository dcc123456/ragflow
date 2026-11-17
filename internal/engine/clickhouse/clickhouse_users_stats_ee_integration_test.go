//go:build ee && integration

package clickhouse

import (
	"testing"
	"time"
)

// TestUsersStatsRanksAndLimitsTopUsers seeds model_usage_fact with rows for
// dedicated users and verifies UsersStats: per-user aggregation across all
// tenants, ranking by total_tokens with request_count as tiebreak, the top
// limit, the date-range filter, and both accepted date formats.
func TestUsersStatsRanksAndLimitsTopUsers(t *testing.T) {
	d := newIntegrationDriver(t)
	defer d.conn.Close()

	uid := time.Now().UTC().Format("20060102150405.000000000")
	prefix := "utest-usrstats-" + uid
	cleanup := func() {
		_ = d.conn.Exec(d.ctx, "ALTER TABLE model_usage_fact DELETE WHERE user_id LIKE ? SETTINGS mutations_sync = 1", prefix+"%")
	}
	cleanup()
	t.Cleanup(cleanup)

	now := time.Now().UTC()
	insert := func(userID string, startAt time.Time, in, out uint64, fail bool) {
		t.Helper()
		errMsg := ""
		if fail {
			errMsg = "boom"
		}
		if err := d.conn.Exec(d.ctx, `
			INSERT INTO model_usage_fact
				(start_at, user_id, user_email, tenant_id, provider_name, model_name,
				 response_time_ms, error_message, input_tokens, output_tokens, total_tokens)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			startAt, userID, userID+"@test.com", "utest-tenant-"+uid, "OpenAI", "gpt-test",
			uint64(120), errMsg, in, out, in+out); err != nil {
			t.Fatalf("insert %s: %v", userID, err)
		}
	}

	// Outside the date range: must never appear.
	insert(prefix+"-d", now.AddDate(0, 0, -100), 5, 5, false)
	// Highest usage: 500 tokens, single request.
	insert(prefix+"-c", now, 300, 200, false)
	// Ties on tokens with -b (100), wins the tiebreak via request_count=2; one failed request.
	insert(prefix+"-a", now, 30, 20, false)
	insert(prefix+"-a", now, 30, 20, true)
	// Tie loser: 100 tokens, single request.
	insert(prefix+"-b", now, 60, 40, false)

	assertRanking := func(from, to string) {
		t.Helper()
		stats, err := d.UsersStats(from, to, 1000)
		if err != nil {
			t.Fatalf("UsersStats(%s, %s): %v", from, to, err)
		}

		var gotA, gotB, gotC, gotD bool
		var order []string
		for _, r := range stats {
			switch r.UserID {
			case prefix + "-a":
				gotA = true
				order = append(order, "a")
				if r.RequestCount != 2 || r.TotalTokens != 100 || r.ErrorCount != 1 {
					t.Fatalf("user a = rc %d tokens %d errors %d, want 2/100/1", r.RequestCount, r.TotalTokens, r.ErrorCount)
				}
				if r.SuccessRate != 0.5 {
					t.Fatalf("user a success_rate = %v, want 0.5", r.SuccessRate)
				}
				if r.UserEmail != prefix+"-a@test.com" {
					t.Fatalf("user a email = %q", r.UserEmail)
				}
			case prefix + "-b":
				gotB = true
				order = append(order, "b")
				if r.RequestCount != 1 || r.TotalTokens != 100 || r.ErrorCount != 0 {
					t.Fatalf("user b = rc %d tokens %d errors %d, want 1/100/0", r.RequestCount, r.TotalTokens, r.ErrorCount)
				}
			case prefix + "-c":
				gotC = true
				order = append(order, "c")
				if r.RequestCount != 1 || r.TotalTokens != 500 {
					t.Fatalf("user c = rc %d tokens %d, want 1/500", r.RequestCount, r.TotalTokens)
				}
			case prefix + "-d":
				gotD = true
			}
		}
		if !gotA || !gotB || !gotC {
			t.Fatalf("missing users: a=%v b=%v c=%v", gotA, gotB, gotC)
		}
		if gotD {
			t.Fatal("user d (outside date range) leaked into results")
		}
		want := []string{"c", "a", "b"}
		if len(order) != len(want) {
			t.Fatalf("ranking = %v, want %v", order, want)
		}
		for i := range want {
			if order[i] != want[i] {
				t.Fatalf("ranking = %v, want %v (total_tokens desc, request_count tiebreak)", order, want)
			}
		}
	}

	// Datetime form (what the admin handler sends by default).
	assertRanking(now.AddDate(0, 0, -1).Format("2006-01-02 00:00:00"), now.Format("2006-01-02 23:59:59"))
	// Date-only form.
	assertRanking(now.AddDate(0, 0, -1).Format("2006-01-02"), now.AddDate(0, 0, 1).Format("2006-01-02"))

	// Top limit: only the two highest-usage users come back.
	stats, err := d.UsersStats(now.AddDate(0, 0, -1).Format("2006-01-02"), now.Format("2006-01-02 23:59:59"), 2)
	if err != nil {
		t.Fatalf("UsersStats top=2: %v", err)
	}
	if len(stats) != 2 || stats[0].UserID != prefix+"-c" || stats[1].UserID != prefix+"-a" {
		t.Fatalf("top=2 returned %d rows, want [c a], got first=%q second=%q",
			len(stats), stats[0].UserID, stats[1].UserID)
	}
}

// TestUsersStatsRejectsInvalidInput covers the guard rails: non-positive top
// and malformed dates.
func TestUsersStatsRejectsInvalidInput(t *testing.T) {
	d := newIntegrationDriver(t)
	defer d.conn.Close()

	if _, err := d.UsersStats("2026-01-01", "2026-01-02", 0); err == nil {
		t.Fatal("top=0 must be rejected")
	}
	if _, err := d.UsersStats("2026-01-01", "2026-01-02", -3); err == nil {
		t.Fatal("negative top must be rejected")
	}
	if _, err := d.UsersStats("not-a-date", "2026-01-02", 5); err == nil {
		t.Fatal("malformed start date must be rejected")
	}
	if _, err := d.UsersStats("2026-01-01", "not-a-date", 5); err == nil {
		t.Fatal("malformed end date must be rejected")
	}
}

// TestStatsSummaryTotalsAllUsers verifies StatsSummary: one row totalling
// request count, token sums, error count and success rate across all users
// and tenants in the range, with both date formats accepted.
func TestStatsSummaryTotalsAllUsers(t *testing.T) {
	d := newIntegrationDriver(t)
	defer d.conn.Close()

	uid := time.Now().UTC().Format("20060102150405.000000000")
	prefix := "utest-summary-" + uid
	cleanup := func() {
		_ = d.conn.Exec(d.ctx, "ALTER TABLE model_usage_fact DELETE WHERE user_id LIKE ? SETTINGS mutations_sync = 1", prefix+"%")
	}
	cleanup()
	t.Cleanup(cleanup)

	now := time.Now().UTC()
	insert := func(userID string, startAt time.Time, in, out uint64, fail bool) {
		t.Helper()
		errMsg := ""
		if fail {
			errMsg = "boom"
		}
		if err := d.conn.Exec(d.ctx, `
			INSERT INTO model_usage_fact
				(start_at, user_id, user_email, tenant_id, provider_name, model_name,
				 response_time_ms, error_message, input_tokens, output_tokens, total_tokens)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			startAt, userID, userID+"@test.com", "utest-tenant-"+uid, "OpenAI", "gpt-test",
			uint64(120), errMsg, in, out, in+out); err != nil {
			t.Fatalf("insert %s: %v", userID, err)
		}
	}

	insert(prefix+"-x", now, 40, 20, false)                         // 60 tokens
	insert(prefix+"-y", now, 40, 20, true)                          // 60 tokens, failed
	insert(prefix+"-z", now.AddDate(0, 0, -100), 1000, 1000, false) // outside range

	assertSummary := func(from, to string) {
		t.Helper()
		summary, err := d.StatsSummary(from, to)
		if err != nil {
			t.Fatalf("StatsSummary(%s, %s): %v", from, to, err)
		}
		if summary.RequestCount != 2 || summary.TotalTokens != 120 {
			t.Fatalf("summary = rc %d tokens %d, want 2/120", summary.RequestCount, summary.TotalTokens)
		}
		if summary.TotalInputTokens != 80 || summary.TotalOutputTokens != 40 {
			t.Fatalf("summary tokens = in %d out %d, want 80/40", summary.TotalInputTokens, summary.TotalOutputTokens)
		}
		if summary.ErrorCount != 1 || summary.SuccessRate != 0.5 {
			t.Fatalf("summary errors = %d rate %v, want 1/0.5", summary.ErrorCount, summary.SuccessRate)
		}
	}

	// Datetime form (what the admin handler sends by default).
	assertSummary(now.AddDate(0, 0, -1).Format("2006-01-02 00:00:00"), now.Format("2006-01-02 23:59:59"))
	// Date-only form.
	assertSummary(now.AddDate(0, 0, -1).Format("2006-01-02"), now.AddDate(0, 0, 1).Format("2006-01-02"))

	// Guard rails: malformed dates are rejected.
	if _, err := d.StatsSummary("not-a-date", "2026-01-02"); err == nil {
		t.Fatal("malformed start date must be rejected")
	}
	if _, err := d.StatsSummary("2026-01-01", "not-a-date"); err == nil {
		t.Fatal("malformed end date must be rejected")
	}
}
