//go:build ee

package config

import (
	"ragflow/internal/common"

	"github.com/spf13/viper"
)

func (c *Config) ParseAuditConfig(v *viper.Viper) error {
	c.audit.Type = "database"
	c.audit.Format = "json"
	c.audit.Path = "logs"
	c.audit.MaxSize = common.DefaultLogMaxSizeMB
	c.audit.MaxBackups = common.DefaultLogMaxBackups
	c.audit.MaxAge = common.DefaultLogMaxAgeDays

	if v.IsSet("audit.type") {
		c.audit.Type = v.GetString("audit.type")
	}

	if v.IsSet("audit.format") {
		c.audit.Format = v.GetString("audit.format")
	}

	if v.IsSet("audit.path") {
		c.audit.Path = v.GetString("audit.path")
	}

	if v.IsSet("audit.max_size") {
		c.audit.MaxSize = v.GetInt("audit.max_size")
	}

	if v.IsSet("audit.max_backups") {
		c.audit.MaxBackups = v.GetInt("audit.max_backups")
	}

	if v.IsSet("audit.max_age") {
		c.audit.MaxAge = v.GetInt("audit.max_age")
	}

	return nil
}

func (c *Config) GetAuditConfig() AuditConfig {
	return c.audit
}

func (c *AuditConfig) GetFileOutput() common.FileOutput {
	return common.FileOutput{
		Path:       c.Path,
		MaxSize:    c.MaxSize,
		MaxBackups: c.MaxBackups,
		MaxAge:     c.MaxAge,
	}
}
