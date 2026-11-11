//go:build ee

package clickhouse

import (
	"testing"

	"ragflow/internal/common"
)

func indexOfColumn(cols []string, c string) int {
	for i, v := range cols {
		if v == c {
			return i
		}
	}
	return -1
}

// TestIngestionColumnsIncludeIdentityRouting asserts the two EE routing identity
// columns are present in the single source of truth. They let the ClickHouse
// sink attribute each event to the ingestor instance and NATS subject that
// carried it.
func TestIngestionColumnsIncludeIdentityRouting(t *testing.T) {
	if got := indexOfColumn(ingestionColumns, "ingestor_name"); got < 0 {
		t.Fatalf("ingestionColumns missing ingestor_name")
	}
	if got := indexOfColumn(ingestionColumns, "nats_queue"); got < 0 {
		t.Fatalf("ingestionColumns missing nats_queue")
	}
}

// TestIngestionColumnsCountIncludesRouting locks the column count now that the
// two routing columns are present (was 27, now 29).
func TestIngestionColumnsCountIncludesRouting(t *testing.T) {
	if len(ingestionColumns) != 29 {
		t.Fatalf("expected 29 inserted columns after adding ingestor_name/nats_queue, got %d", len(ingestionColumns))
	}
}

// TestIngestionRowMapsIdentityRouting asserts the new columns are mapped from
// the IngestLog struct at the same position as their column, so a value set on
// the struct actually reaches ClickHouse.
func TestIngestionRowMapsIdentityRouting(t *testing.T) {
	log := &common.IngestionLog{IngestorName: "ing-A", NatsQueue: "tasks.RAGFLOW"}
	row := ingestionRow(log)
	ci := indexOfColumn(ingestionColumns, "ingestor_name")
	nqi := indexOfColumn(ingestionColumns, "nats_queue")
	if ci < 0 || nqi < 0 {
		t.Fatal("routing columns not present in ingestionColumns")
	}
	if row[ci] != "ing-A" {
		t.Fatalf("ingestor_name column index %d maps to %v, want ing-A", ci, row[ci])
	}
	if row[nqi] != "tasks.RAGFLOW" {
		t.Fatalf("nats_queue column index %d maps to %v, want tasks.RAGFLOW", nqi, row[nqi])
	}
}
