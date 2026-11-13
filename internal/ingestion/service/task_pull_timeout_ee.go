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

package service

import "time"

// taskPullRequestTimeout bounds a single dispatch pull. The enterprise build
// drains three lanes (high -> medium -> default), and each lane waits up to the
// engine's 1s tier window before giving up, so the outer budget must cover all
// three lanes (>= 3s); otherwise an empty high lane consumes the whole budget
// and the lower lanes are never reached, starving normal ingestion.
const taskPullRequestTimeout = 3 * time.Second
