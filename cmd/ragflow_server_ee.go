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
	"fmt"
	"os"

	"github.com/alecthomas/kingpin/v2"

	"ragflow/internal/common"
	"ragflow/internal/server/bootstrap"
)

func registerEEFlags(app *kingpin.Application, args *bootstrap.ServerArgs) func() error {
	var tierSet bool
	deepdoc := app.Flag("deepdoc", "Run only the DeepDoc inference server").Bool()
	app.Flag("init-superuser", "Initialize the superuser (automatic with --admin and default startup)").BoolVar(&args.InitSuperUser)
	args.IngestorTier = app.Flag("ingestor-tier", "Ingestor lane: low, medium or high (default: low)").IsSetByUser(&tierSet).String()

	return func() error {
		if *deepdoc {
			if args.Mode != nil {
				return fmt.Errorf("only one mode flag may be specified: --%s and --deepdoc", *args.Mode)
			}
			mode := "deepdoc"
			args.Mode = &mode
		}
		if tierSet {
			if _, err := common.ParseIngestorTier(*args.IngestorTier); err != nil {
				return err
			}
		} else {
			args.IngestorTier = nil
		}
		return nil
	}
}

func printEEHelp() {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Enterprise options:")
	fmt.Fprintln(os.Stderr, "  --deepdoc                             \tRun only the DeepDoc inference server (default port: 8001; override with --port)")
	fmt.Fprintln(os.Stderr, "  --init-superuser                      \tInitialize the superuser (automatic with --admin and default startup)")
	fmt.Fprintln(os.Stderr, "  --ingestor-tier string                \tIngestor lane: low, medium or high (default: low)")
}
