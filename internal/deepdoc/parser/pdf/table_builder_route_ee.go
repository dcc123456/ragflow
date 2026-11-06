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

package pdf

import (
	"ragflow/internal/deepdoc/client"
	tbl "ragflow/internal/deepdoc/parser/pdf/table"
	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

// init routes the EE inference-service analyzers (deepdoc_cpu_ee et al.) to the
// VH table builder while leaving every other analyzer on the OSS
// DeepDocTableBuilder. RegisterTableBuilder is the public injection point, so
// this ee file needs no change to the shared parser.go fallback.
func init() {
	RegisterTableBuilder(func(doc pdf.DocAnalyzer) pdf.TableBuilder {
		if c, ok := doc.(*client.Client); ok {
			return tbl.NewDeepDocVHTableBuilder(c)
		}
		return tbl.NewDeepDocTableBuilder(doc)
	})
}
