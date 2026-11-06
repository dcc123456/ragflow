//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package config

import (
	"ragflow/internal/common"

	"github.com/spf13/viper"
)

func (c *IngestLogConfig) ParseIngestLogConfig(v *viper.Viper) error {
	c.Type = "database"
	c.Format = "json"
	c.Path = "logs"
	c.MaxSize = common.DefaultLogMaxSizeMB
	c.MaxBackups = common.DefaultLogMaxBackups
	c.MaxAge = common.DefaultLogMaxAgeDays

	if v.IsSet("type") {
		c.Type = v.GetString("type")
	}

	if v.IsSet("format") {
		c.Format = v.GetString("format")
	}

	if v.IsSet("path") {
		c.Path = v.GetString("path")
	}

	if v.IsSet("max_size") {
		c.MaxSize = v.GetInt("max_size")
	}

	if v.IsSet("max_backups") {
		c.MaxBackups = v.GetInt("max_backups")
	}

	if v.IsSet("max_age") {
		c.MaxAge = v.GetInt("max_age")
	}

	return nil
}

func (c *IngestLogConfig) GetFileOutput() common.FileOutput {
	return common.FileOutput{
		Path:       c.Path,
		MaxSize:    c.MaxSize,
		MaxBackups: c.MaxBackups,
		MaxAge:     c.MaxAge,
	}
}
