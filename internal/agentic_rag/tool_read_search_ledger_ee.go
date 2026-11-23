//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the specific language governing permissions and limitations under
//  the License.

//go:build ee

package agentic_rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

// readSearchLedgerTool exposes the run's append-only search ledger to the
// delivery gate's auditor. It answers one question with recorded facts instead
// of the deliverable's own reconstruction: WHAT WAS ACTUALLY SEARCHED.
//
// The Candidate Matrix's Searched lines are the explorer's end-of-run account
// of its behavior, and that account measurably underreports - a #30 run
// executed 59 distinct searches while its shipped matrix carried 8 Searched
// lines, which made a thoroughly-searched anchor read as "never queried" for
// five audit passes. Coverage judgements ("this clue anchor was never
// queried", "no co-occurrence scan ran", "the run opened on a guess") must
// therefore be verified against this ledger before they are asserted, and a
// matrix whose Searched lines omit ledger-recorded searches is itself a
// suspect.
//
// The ledger is append-only and ordered by execution: entry 1 is the run's
// actual first search, which is what the candidate-free opening rule is judged
// against. The optional `after` cursor returns only the delta - what the
// explorer did since the seq the auditor last read - so a re-audit after a
// repair turn can verify repair compliance directly.

const readSearchLedgerToolName = "read_search_ledger"

const readSearchLedgerToolDescription = "Read the run's append-only search ledger: EVERY retrieval action " +
	"the explorer actually executed (tool + verbatim arguments, in execution order), recorded mechanically at " +
	"tool execution - unlike the deliverable's Searched lines, which are the model's end-of-run reconstruction " +
	"and omit most of them. Use it BEFORE asserting any coverage judgement: whether a clue anchor was ever " +
	"queried, whether the run's first search was candidate-free (entry 1 is the actual first search), whether " +
	"a co-occurrence regex over two constraints ever ran, and whether the matrix's Searched lines match what " +
	"really happened. Pass `after` (the last seq you read) to see only the delta since then."

// readSearchLedgerArgs is the JSON the auditor sends into InvokableRun.
type readSearchLedgerArgs struct {
	After int `json:"after,omitempty"`
}

// ReadSearchLedgerTool reads the run's search ledger. It is read-only and
// uncounted: it records nothing itself, so re-reads never pollute the facts
// they report.
type ReadSearchLedgerTool struct {
	ledger *searchLedger
}

// NewReadSearchLedgerTool returns the ledger reader bound to one run's
// ledger. A nil ledger yields a tool that reports itself unavailable - the
// audit can proceed without coverage facts, it just cannot assert them.
func NewReadSearchLedgerTool(ledger *searchLedger) *ReadSearchLedgerTool {
	return &ReadSearchLedgerTool{ledger: ledger}
}

// Info returns the tool's metadata for the chat model.
func (t *ReadSearchLedgerTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	schemaJSON := `{
  "type": "object",
  "properties": {
    "after": {
      "type": "integer",
      "description": "Optional: return only entries with a sequence number greater than this (your previous read's last seq). Omit for the full ledger from entry 1."
    }
  }
}`
	s := &jsonschema.Schema{}
	if err := json.Unmarshal([]byte(schemaJSON), s); err != nil {
		return nil, fmt.Errorf("read_search_ledger: parse schema: %w", err)
	}
	return &schema.ToolInfo{
		Name:        readSearchLedgerToolName,
		Desc:        readSearchLedgerToolDescription,
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(s),
	}, nil
}

// InvokableRun renders the requested ledger slice, one entry per line:
// "seq. tool: args". Entries are the run's recorded facts - verbatim tool
// arguments in execution order.
func (t *ReadSearchLedgerTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	after := 0
	if strings.TrimSpace(argumentsInJSON) != "" {
		var args readSearchLedgerArgs
		if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
			return fmt.Sprintf("<tool_error tool=%q severity=\"warn\">read_search_ledger: unparseable arguments, returning the full ledger: %v</tool_error>", readSearchLedgerToolName, err), nil
		}
		after = args.After
	}
	if t.ledger == nil {
		return "no search ledger is available for this run", nil
	}
	entries := t.ledger.Snapshot(after, 0)
	if len(entries) == 0 {
		return fmt.Sprintf("no retrieval actions after seq %d", after), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "search ledger (%d entries after seq %d, total %d):\n", len(entries), after, t.ledger.Count())
	for _, e := range entries {
		fmt.Fprintf(&b, "%d. %s: %s\n", e.seq, e.tool, e.args)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
