//go:build ee

package config

import "github.com/spf13/viper"

// ParseIngestLogConfig reads the [ingestion_log] section of service_conf.yaml
// into the EE Config, reusing the shared IngestLogConfig type. The parsed Type
// is dispatched by server.wireIngestLogSink; only "clickhouse" currently has a
// sink implementation.
//
// The type defaults to "clickhouse" (on): the ingestion sink is wired unless an
// operator opts out. Only an explicit [ingestion_log].type key overrides it. A
// [ingestion_log] section present without a type key keeps the default-on value
// rather than flipping to the zero value.
func (c *Config) ParseIngestLogConfig(v *viper.Viper) error {
	c.ingestLog.Type = "clickhouse" // default on
	if sub := v.Sub("ingestion_log"); sub != nil {
		if err := sub.Unmarshal(&c.ingestLog); err != nil {
			return err
		}
	}
	return nil
}

// IngestLogType returns the configured [ingestion_log].type, the single source
// of truth for sink dispatch.
func (c *Config) IngestLogType() string {
	return c.ingestLog.Type
}

// IngestLogSettings returns the parsed [ingestion_log] section for sink
// installers (Path/MaxSize/...). Do NOT use GetIngestLogConfig() for sinks:
// that returns the separate [ingestor].log config.
func (c *Config) IngestLogSettings() *IngestLogConfig {
	return &c.ingestLog
}
