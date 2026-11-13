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

import "time"

// HandlerPullTimeout bounds the admin manual-pull (PullMessageFromQueue). It
// also drains the three lanes (high -> medium -> default) under one outer
// deadline, so it must cover all three lane windows (>= 3s) or the lower lanes
// are starved the same way the worker pull is.
const HandlerPullTimeout = 3 * time.Second
