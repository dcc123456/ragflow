//go:build ee

package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestIngestLogConfigEEStubIsNoOp verifies the EE-build placeholder for
// (*Config).ParseIngestLogConfig is a no-op: it must return nil, so the EE build
// links whether or not the shared config loader (config.go) calls it after the
// OSS->EE sync.
func TestIngestLogConfigEEStubIsNoOp(t *testing.T) {
	c := &Config{}
	if err := c.ParseIngestLogConfig(viper.New()); err != nil {
		t.Fatalf("ee ParseIngestLogConfig must be a no-op: %v", err)
	}
}

// TestIngestLogConfigEEStubIgnoresSection proves the EE placeholder stays a
// no-op even with an explicit [ingestion_log].type: clickhouse section.
func TestIngestLogConfigEEStubIgnoresSection(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("ingestion_log:\n  type: clickhouse\n")); err != nil {
		t.Fatalf("read config: %v", err)
	}
	c := &Config{}
	if err := c.ParseIngestLogConfig(v); err != nil {
		t.Fatalf("ee ParseIngestLogConfig must ignore [ingestion_log]: %v", err)
	}
}
