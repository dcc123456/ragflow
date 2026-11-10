//go:build ee

package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestIngestionLogConfigDefaultsOn asserts the EE ingestion-log sink is ON by
// default: with no [ingestion_log] section present, Type defaults to "clickhouse".
func TestIngestionLogConfigDefaultsOn(t *testing.T) {
	c := &Config{}
	if err := c.ParseIngestLogConfig(viper.New()); err != nil {
		t.Fatalf("ParseIngestLogConfig: %v", err)
	}
	if got := c.IngestLogType(); got != "clickhouse" {
		t.Fatalf("IngestLogType = %q, want clickhouse", got)
	}
}

// TestIngestionLogConfigClickHouseOn is the explicit on-value.
func TestIngestionLogConfigClickHouseOn(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("ingestion_log:\n  type: clickhouse\n")); err != nil {
		t.Fatalf("read config: %v", err)
	}
	c := &Config{}
	if err := c.ParseIngestLogConfig(v); err != nil {
		t.Fatalf("ParseIngestLogConfig: %v", err)
	}
	if got := c.IngestLogType(); got != "clickhouse" {
		t.Fatalf("IngestLogType = %q, want clickhouse", got)
	}
}

// TestIngestionLogConfigNoneIsOff covers the explicit no-op alias. Operators use
// "none" to document "do not stream ingestion events".
func TestIngestionLogConfigNoneIsOff(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("ingestion_log:\n  type: none\n")); err != nil {
		t.Fatalf("read config: %v", err)
	}
	c := &Config{}
	if err := c.ParseIngestLogConfig(v); err != nil {
		t.Fatalf("ParseIngestLogConfig: %v", err)
	}
	if got := c.IngestLogType(); got != "none" {
		t.Fatalf("IngestLogType = %q, want none", got)
	}
}

// TestIngestionLogConfigDatabaseIsOff: the shared IngestLogConfig type keeps its
// pre-existing "database" value, but the EE ingestion-event sink does not handle
// it, so it must reach the dispatcher as "database" (an unhandled type).
func TestIngestionLogConfigDatabaseIsOff(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("ingestion_log:\n  type: database\n")); err != nil {
		t.Fatalf("read config: %v", err)
	}
	c := &Config{}
	if err := c.ParseIngestLogConfig(v); err != nil {
		t.Fatalf("ParseIngestLogConfig: %v", err)
	}
	if got := c.IngestLogType(); got != "database" {
		t.Fatalf("IngestLogType = %q, want database", got)
	}
}

// TestIngestionLogConfigSectionWithoutTypeKey is the adversarial case: a
// [ingestion_log] section exists but carries no "type" key. The sink must keep
// its default-on value (Type="clickhouse") rather than flipping off via the zero
// value "".
func TestIngestionLogConfigSectionWithoutTypeKey(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("ingestion_log:\n  other_key: whatever\n")); err != nil {
		t.Fatalf("read config: %v", err)
	}
	c := &Config{}
	if err := c.ParseIngestLogConfig(v); err != nil {
		t.Fatalf("ParseIngestLogConfig: %v", err)
	}
	if got := c.IngestLogType(); got != "clickhouse" {
		t.Fatalf("section present without type key must keep default, got %q", got)
	}
}

// TestIngestLogConfigFieldPopulated asserts the parser writes the parsed value
// into the Config.ingestLog field (the structured sink config).
func TestIngestLogConfigFieldPopulated(t *testing.T) {
	c := &Config{}
	if err := c.ParseIngestLogConfig(viper.New()); err != nil {
		t.Fatalf("ParseIngestLogConfig: %v", err)
	}
	if c.ingestLog.Type != "clickhouse" {
		t.Fatalf("ingestLog.Type must default to clickhouse, got %q", c.ingestLog.Type)
	}
	if c.IngestLogSettings() != &c.ingestLog {
		t.Fatal("IngestLogSettings must return &c.ingestLog")
	}
}
