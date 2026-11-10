//go:build ee

package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// configMethod looks up a method on *Config without referencing the symbol at
// compile time, so this test can express the desired API before it exists.
func configMethod(t *testing.T, c *Config, name string) reflect.Value {
	t.Helper()
	m := reflect.ValueOf(c).MethodByName(name)
	if !m.IsValid() {
		t.Fatalf("(*Config).%s must exist", name)
	}
	return m
}

func parseIngestLogConfig(t *testing.T, yaml string) *Config {
	t.Helper()
	c := &Config{}
	v := viper.New()
	if yaml != "" {
		v.SetConfigType("yaml")
		if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
			t.Fatalf("read config: %v", err)
		}
	}
	if err := c.ParseIngestLogConfig(v); err != nil {
		t.Fatalf("ParseIngestLogConfig: %v", err)
	}
	return c
}

func ingestLogType(t *testing.T, c *Config) string {
	t.Helper()
	return configMethod(t, c, "IngestLogType").Call(nil)[0].String()
}

func TestIngestLogTypeDefaultsToClickHouse(t *testing.T) {
	c := parseIngestLogConfig(t, "")
	if got := ingestLogType(t, c); got != "clickhouse" {
		t.Fatalf("default IngestLogType = %q, want clickhouse", got)
	}
}

func TestIngestLogTypeFollowsConfiguredValue(t *testing.T) {
	cases := []struct {
		yaml string
		want string
	}{
		{"ingestion_log:\n  type: clickhouse\n", "clickhouse"},
		{"ingestion_log:\n  type: none\n", "none"},
		{"ingestion_log:\n  type: database\n", "database"},
		{"ingestion_log:\n  type: file\n", "file"},
		{"ingestion_log:\n  type: 未知类型\n", "未知类型"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			c := parseIngestLogConfig(t, tc.yaml)
			if got := ingestLogType(t, c); got != tc.want {
				t.Fatalf("IngestLogType = %q, want %q", got, tc.want)
			}
		})
	}
}

// Adversarial: a section present without a type key must keep the default-on
// value rather than flipping to the zero value "".
func TestIngestLogTypeSectionWithoutTypeKey(t *testing.T) {
	c := parseIngestLogConfig(t, "ingestion_log:\n  other_key: whatever\n")
	if got := ingestLogType(t, c); got != "clickhouse" {
		t.Fatalf("section without type key must keep default, got %q", got)
	}
}

// Adversarial: Config owns two IngestLogConfig instances. IngestLogSettings must
// expose c.ingestLog ([ingestion_log]), NOT c.ingestor.logConfig ([ingestor].log)
// which GetIngestLogConfig returns.
func TestIngestLogSettingsIsTheIngestionLogSection(t *testing.T) {
	c := &Config{}
	c.ingestLog.Type = "clickhouse"
	c.ingestLog.Path = "ingestion-log-path"
	c.ingestor.logConfig.Type = "file"
	c.ingestor.logConfig.Path = "ingestor-log-path"

	out := configMethod(t, c, "IngestLogSettings").Call(nil)[0]
	if out.IsNil() {
		t.Fatal("IngestLogSettings must not return nil")
	}
	settings, ok := out.Interface().(*IngestLogConfig)
	if !ok {
		t.Fatalf("IngestLogSettings must return *IngestLogConfig, got %T", out.Interface())
	}
	if settings.Path != "ingestion-log-path" || settings.Type != "clickhouse" {
		t.Fatalf("IngestLogSettings returned the wrong config: %+v", settings)
	}
	if settings.Path == c.GetIngestLogConfig().Path {
		t.Fatal("IngestLogSettings must not alias c.ingestor.logConfig")
	}
	// Downstream file-sink installers read the file settings through this getter.
	if got := settings.GetFileOutput().Path; got != "ingestion-log-path" {
		t.Fatalf("GetFileOutput().Path = %q, want ingestion-log-path", got)
	}
}
