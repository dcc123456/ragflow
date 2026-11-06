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

package admin

import "time"

func formatLicenseTime(t time.Time) string {
	if t.IsZero() {
		return "N/A (Perpetual)"
	}
	return t.Format("2006-01-02 15:04:05")
}

func formatTime(t *int64) string {
	if t == nil {
		return "N/A"
	}
	return time.UnixMilli(*t).Format("2006-01-02 15:04:05")
}
