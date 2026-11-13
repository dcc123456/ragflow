//go:build ee

package server

import (
	"errors"
	"fmt"

	"ragflow/internal/common"
	"ragflow/internal/engine/clickhouse"
	"ragflow/internal/server/config"
)

// wireAuditLogSink connects the selected destination after ClickHouse initialization.
func wireAuditLogSink(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("audit configuration is nil")
	}
	switch typ := cfg.GetAuditConfig().Type; typ {
	case "database":
		driver := clickhouse.GetDriver()
		if driver == nil {
			return errors.New("audit database sink: ClickHouse driver is not initialized")
		}
		common.SetAuditLogSink(driver.SaveOperationLog)
		return nil
	case "file":
		// InitAudit already initialized the file destination before server startup.
		return nil
	default:
		return fmt.Errorf("unsupported audit.type %q", typ)
	}
}
