//go:build ee

package config

import "github.com/spf13/viper"

// ParseIngestLogConfig is the EE-build counterpart of the OSS/non-ee stub in
// stub.go (//go:build !ee). The shared config loader (internal/server/config.go)
// calls globalConfig.ParseIngestLogConfig(v) once the OSS ingestion-log
// configuration lands in EE via the OSS->EE sync.
//
// This is a no-op placeholder so the EE build links whether or not the caller is
// present. It is temporary: the ingestion-log feature branch provides the real
// parser that wires ingestion lifecycle events into ClickHouse. Both branches add
// this same file name, so merging that branch must resolve the conflict in favor
// of the real implementation (replacing this no-op).
func (c *Config) ParseIngestLogConfig(v *viper.Viper) error { return nil }
