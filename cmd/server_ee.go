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

package main

import (
	"flag"
	"fmt"
	"strconv"

	"ragflow/internal/common"
	"ragflow/internal/server/bootstrap"
)

func registerFlags(fs *flag.FlagSet, args *bootstrap.ServerArgs) func() error {
	var modes []string
	var concurrency, cpuCores int
	var tier string
	fs.BoolVar(&args.InitSuperUser, "init-superuser", false, "")
	fs.IntVar(&concurrency, "deepdoc-inference-concurrency", 0, "")
	fs.IntVar(&cpuCores, "deepdoc-inference-cpu-cores", 0, "")
	fs.StringVar(&tier, "ingestor-tier", "", "")
	for _, mode := range []string{"admin", "api", "ingestor", "syncer", "chatter", "deepdoc", "migrate"} {
		fs.Var(modeFlag{name: mode, seen: &modes}, mode, "")
	}
	return func() error {
		seen := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })
		if len(modes) > 0 {
			args.Mode = &modes[len(modes)-1]
		}
		if seen["deepdoc-inference-concurrency"] {
			if concurrency <= 0 {
				return fmt.Errorf("invalid --deepdoc-inference-concurrency %d: must be positive", concurrency)
			}
			args.DeepdocInferenceConcurrency = &concurrency
		}
		if seen["deepdoc-inference-cpu-cores"] {
			if cpuCores < 0 {
				return fmt.Errorf("invalid --deepdoc-inference-cpu-cores %d: must be >= 0 (0 means all cores)", cpuCores)
			}
			args.DeepdocInferenceCPUCores = &cpuCores
		}
		if seen["ingestor-tier"] {
			if _, err := common.ParseIngestorTier(tier); err != nil {
				return err
			}
			args.IngestorTier = &tier
		}
		return nil
	}
}

// modeFlag is a boolean flag that records which mode flag was set and in what
// order, so the last mode on the command line wins (matching the previous
// hand-rolled parser). "=false" selects nothing.
type modeFlag struct {
	name string
	seen *[]string
}

func (m modeFlag) String() string   { return "" }
func (m modeFlag) IsBoolFlag() bool { return true }

func (m modeFlag) Set(value string) error {
	on, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	if on {
		*m.seen = append(*m.seen, m.name)
	}
	return nil
}
