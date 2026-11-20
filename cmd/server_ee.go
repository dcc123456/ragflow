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
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	"ragflow/internal/common"
	"ragflow/internal/server/admin"
	apipkg "ragflow/internal/server/api"
	"ragflow/internal/server/bootstrap"
	chatterpkg "ragflow/internal/server/chatter"
	ingestorpkg "ragflow/internal/server/ingestor"
	syncerpkg "ragflow/internal/server/syncer"
)

func registerFlags(fs *flag.FlagSet, args *bootstrap.ServerArgs) func() error {
	var modes []string
	var port, concurrency, cpuCores int
	var tier string
	fs.IntVar(&port, "port", 0, "")
	fs.IntVar(&port, "p", 0, "")
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
		if seen["port"] || seen["p"] {
			if port <= 0 || port > 65535 {
				return fmt.Errorf("invalid port: %d", port)
			}
			args.Port = &port
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

func printHelp(args *bootstrap.ServerArgs) {
	if args.Mode == nil {
		fmt.Fprintf(os.Stderr, "Usage: %s --api|--admin|--ingestor|--syncer|--chatter|--deepdoc [OPTIONS]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s --migrate [OPTIONS]\n\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Modes: --admin, --api, --ingestor, --syncer, --chatter, --deepdoc")
		fmt.Fprintln(os.Stderr, "  --migrate          \tRun database migrations and exit")
	} else {
		fmt.Fprintf(os.Stderr, "Usage: %s --%s [OPTIONS]\n\n", os.Args[0], *args.Mode)
	}
	printCommonHelp()
	fmt.Fprintln(os.Stderr, "  -p, --port int     \tServer port (overrides configuration)")
	if args.Mode == nil || *args.Mode == "admin" {
		fmt.Fprintln(os.Stderr, "  --init-superuser   \tInitialize superuser account (admin)")
	}
	if args.Mode == nil || *args.Mode == "ingestor" {
		fmt.Fprintln(os.Stderr, "  --ingestor-tier string\tIngestion lanes to consume")
		fmt.Fprintln(os.Stderr, "  --deepdoc-inference-concurrency int\tIn-process inference concurrency")
		fmt.Fprintln(os.Stderr, "  --deepdoc-inference-cpu-cores int\tInference CPU budget (0: all cores)")
	}
}

func validateServerMode(args *bootstrap.ServerArgs) error {
	if args.Mode == nil {
		printHelp(args)
		return errors.New("select a server mode")
	}
	return nil
}

func runEEServices(ctx context.Context, cancel context.CancelFunc, args *bootstrap.ServerArgs, services *bootstrap.AgentServices) error {
	switch *args.Mode {
	case "admin":
		return admin.Run(ctx, args)
	case "api":
		return apipkg.Run(ctx, *args.Name, args, services)
	case "ingestor":
		return ingestorpkg.Run(ctx, cancel, args, services)
	case "syncer":
		return syncerpkg.Run(ctx, cancel, args)
	case "chatter":
		return chatterpkg.Run(ctx, args, services)
	default:
		return fmt.Errorf("invalid server mode: %s", *args.Mode)
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
