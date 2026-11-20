//go:build ee

package structure

import (
	"strings"

	"ragflow/internal/ingestion/component/knowledge_compiler/common"
)

func isChainKind(kind Type) bool {
	return kind == TypeList || kind == Type("timeline")
}

func postprocessAdditionalKind(kind Type, rows []common.Product) []common.Product {
	if kind == Type("timeline") {
		return dropIsolatedTimelineEntities(rows)
	}
	return rows
}

// Drop orphan entities after relation correction so only connected events remain.
func dropIsolatedTimelineEntities(rows []common.Product) []common.Product {
	connected := map[string]bool{}
	for _, row := range rows {
		if e, ok := chainExtractEdge(row); ok {
			connected[strings.ToLower(e.From)] = true
			connected[strings.ToLower(e.To)] = true
		}
	}
	out := make([]common.Product, 0, len(rows))
	for _, row := range rows {
		if kind, _ := row.Meta["kind"].(string); kind == "entity" {
			if !connected[strings.ToLower(entityNameValue(row))] {
				continue
			}
		}
		out = append(out, row)
	}
	return out
}
