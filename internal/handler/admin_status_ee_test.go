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

package handler

import "ragflow/internal/server/local"

// The enterprise auth middleware refuses every authenticated request while the
// admin connection status is unavailable, and that status only becomes
// available when the server bootstrap reaches the admin service. Unit tests
// never run the bootstrap, so mark it available once for the whole test binary;
// the community stub answers true without consulting the status at all.
func init() {
	local.InitAdminStatus()
	local.SetAdminStatus(local.Available, "unit tests")
}
