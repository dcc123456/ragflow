//go:build ee

package server

import (
	"fmt"

	"ragflow/internal/common"
	"ragflow/internal/engine/clickhouse"
	"ragflow/internal/server/config"
)

// wireIngestLogSink installs the sink selected by [ingestion_log].type. All type
// handling lives here as a compile-time switch; StartServer calls it once after
// clickhouse.Init. A downstream PR adds a file sink as one case and does not
// touch StartServer. The clickhouse case guards against a not-yet-initialized
// driver so wiring never panics if it runs before clickhouse.Init.
func wireIngestLogSink(cfg *config.Config, serverName string) error {
	if cfg == nil {
		return nil
	}
	switch typ := cfg.IngestLogType(); typ {
	case "clickhouse":
		if d := clickhouse.GetDriver(); d != nil {
			d.RegisterIngestLog()
		}
		return nil
	default:
		common.Warn(fmt.Sprintf("ingestion_log.type %q has no sink implementation; lifecycle events will be dropped", typ))
		return nil
	}
}
