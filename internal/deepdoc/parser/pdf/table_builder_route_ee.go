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
