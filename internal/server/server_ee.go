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

package server

import (
	"context"
	"errors"
	"ragflow/internal/common"
	"ragflow/internal/engine/clickhouse"
	"ragflow/internal/engine/otel"
	"ragflow/internal/utility"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var serverEE *EEServer

func init() {
	serverEE = newEEServer()
}

type EEServer struct {
	tracerProvider *sdktrace.TracerProvider
}

func newEEServer() *EEServer {
	return &EEServer{}
}

func StartServer(ctx context.Context, cancel context.CancelFunc, serverName string, serverType common.ServerType) error {
	if serverEE == nil {
		return errors.New("server EE is nil")
	}

	config := GetConfig()

	oTel := config.GetOpenTelemetryConfig()

	var err error
	serverEE.tracerProvider, err = tracer.NewTracerProvider(ctx, serverName, oTel.Host, oTel.Port, oTel.Secure, oTel.Stdout, oTel.Enable, oTel.SampleRatio)
	if err != nil {
		return err
	}

	ckConfig := config.GetClickhouseConfig()

	err = clickhouse.Init(ctx, cancel, ckConfig.Host, ckConfig.Port, ckConfig.User, ckConfig.Password, ckConfig.Database)
	if err != nil {
		common.Error("Fail to connect to clickhouse", err)
		return err
	}
	if err = wireAuditLogSink(config); err != nil {
		return err
	}

	// Install the ingestion lifecycle sink selected by [ingestion_log].type.
	// The dispatcher owns all type handling; unhandled types are a no-op. This
	// runs after clickhouse.Init succeeds so the driver is always available.
	if err = wireIngestLogSink(config, serverName); err != nil {
		return err
	}

	// Init service quota instance
	utility.InitServiceQuota(serverType)

	return nil
}

func ShutdownServer(ctx context.Context) error {
	if serverEE == nil {
		return errors.New("server EE is nil")
	}

	err := serverEE.tracerProvider.Shutdown(ctx)
	if err != nil {
		return err
	}
	err = clickhouse.Close()
	if err != nil {
		return err
	}

	return nil
}
