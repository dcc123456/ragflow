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

package common

import (
	"go.uber.org/zap"
)

func InitIngestLog(logType string, file FileOutput, serviceName string) error {
	return nil
}

// SyncIngestLog satisfies the shared shutdown hook; the EE sink has no local buffer.
func SyncIngestLog() {}

func LogIngest(_ *IngestionLog, fields ...zap.Field) {

}

func LogIngestEvent(log IngestionLog) {
}
