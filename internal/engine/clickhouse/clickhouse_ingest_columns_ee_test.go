//go:build ee

package clickhouse

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"

	"ragflow/internal/common"
)

// TestIngestionInsertSQLUsesSingleSource guards the single-source invariant:
// the INSERT column list must be generated from ingestionColumns, not written
// out by hand a second time in the PrepareBatch SQL.
func TestIngestionInsertSQLUsesSingleSource(t *testing.T) {
	src, err := os.ReadFile("clickhouse_ingest_ee.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "clickhouse_ingest_ee.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	var sqlArg ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "PrepareBatch" {
			return true
		}
		if len(call.Args) >= 2 {
			sqlArg = call.Args[1]
		}
		return true
	})
	if sqlArg == nil {
		t.Fatal("could not locate the PrepareBatch SQL argument")
	}
	if _, ok := sqlArg.(*ast.BasicLit); ok {
		t.Fatal("INSERT SQL is a hardcoded string literal; columns must come from ingestionColumns")
	}
	if !strings.Contains(string(src), "strings.Join(ingestionColumns") {
		t.Fatal("ingestionColumns must be the single source for the INSERT column list")
	}
}

// TestIngestionColumnsAreWellFormed covers duplicate / malformed / derived-column
// corner cases for the one source of truth.
func TestIngestionColumnsAreWellFormed(t *testing.T) {
	if len(ingestionColumns) != 27 {
		t.Fatalf("expected 27 inserted columns, got %d", len(ingestionColumns))
	}
	ident := regexp.MustCompile("^[a-z][a-z0-9_]*$")
	seen := make(map[string]bool, len(ingestionColumns))
	for _, c := range ingestionColumns {
		if !ident.MatchString(c) {
			t.Fatalf("column %q is not a plain lowercase identifier (SQL injection guard)", c)
		}
		if seen[c] {
			t.Fatalf("duplicate column %q", c)
		}
		seen[c] = true
		if c == "event_date" {
			t.Fatal("event_date is DEFAULT-derived and must not be inserted explicitly")
		}
	}
	if ingestionColumns[0] != "event_at" || ingestionColumns[len(ingestionColumns)-1] != "operation" {
		t.Fatalf("unexpected column order: first=%q last=%q", ingestionColumns[0], ingestionColumns[len(ingestionColumns)-1])
	}
}

// TestIngestionRowLengthMatchesColumns locks the value tuple to the column list
// so a future added column cannot silently desync the mapping.
func TestIngestionRowLengthMatchesColumns(t *testing.T) {
	row := ingestionRow(&common.IngestionLog{})
	if len(row) != len(ingestionColumns) {
		t.Fatalf("ingestionRow has %d values, want %d columns", len(row), len(ingestionColumns))
	}
}
