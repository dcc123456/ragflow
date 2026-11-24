//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

//go:build ee

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// errAudit marks a mandatory-audit write failure. It must abort the run, unlike a model
// failure which merely abandons the batch update.
var errAudit = errors.New("mandatory audit write failed")

// mergePromptTemplate is the fixed head of the merge prompt (fingerprinted at run start).
const mergePromptTemplate = "You maintain a GLOBAL set of reusable SEARCH STRATEGIES"

// contentTokens extracts lowercase content words (>=4 chars) for relatedness checks.
func contentTokens(s string) []string {
	var out []string
	for _, w := range splitFields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,;:()[]\"'`")
		if len(w) >= 4 {
			out = append(out, w)
		}
	}
	return out
}

// poolPromptBudget bounds how many candidates go into one merge prompt. Below it the
// WHOLE pool is sent (so the model's verdicts cover the whole pool); above it only the
// relevant subset is sent and the whole-pool near-duplicate net is the safety net.
const poolPromptBudget = 60

// boundedPool is the subset put into the merge prompt (see poolPromptBudget).
func boundedPool(set *strategySet, lessons []lesson) []strategyItem {
	if len(set.Items) <= poolPromptBudget {
		return append([]strategyItem(nil), set.Items...)
	}
	seen := map[string]bool{}
	var out []strategyItem
	add := func(it strategyItem) {
		if !seen[it.ID] {
			out = append(out, it)
			seen[it.ID] = true
		}
	}
	for _, it := range set.activeItems() {
		add(it)
	}
	for _, l := range lessons {
		if l.Replaces != "" {
			if it := set.byID(l.Replaces); it != nil {
				add(*it)
			}
		}
	}
	lessonToks := map[string]bool{}
	for _, l := range lessons {
		for _, t := range contentTokens(l.Text) {
			lessonToks[t] = true
		}
	}
	if len(lessonToks) > 0 {
		for _, it := range set.Items { // set.Items is score-ranked
			if seen[it.ID] {
				continue
			}
			for _, t := range contentTokens(it.Text) {
				if lessonToks[t] {
					add(it)
					break
				}
			}
		}
	}
	if len(out) > poolPromptBudget {
		out = out[:poolPromptBudget]
	}
	return out
}

// mergePrompt builds the batch-level merge prompt (plan §9.1). The four quality
// requirements are the model's to judge; the program only applies the verdicts.
func mergePrompt(set *strategySet, lessons []lesson, pass, batchIndex int) string {
	pool := boundedPool(set, lessons)
	var b strings.Builder
	b.WriteString(mergePromptTemplate + " for an agentic retrieval system.\n\n")
	b.WriteString("Current strategies in scope (id: text):\n")
	if len(pool) == 0 {
		b.WriteString("(none yet)\n")
	} else {
		for _, it := range pool {
			fmt.Fprintf(&b, "- %s: %s\n", it.ID, it.Text)
		}
	}
	b.WriteString("\nThis batch's lessons (candidate improvements; each has a predicted score -2..+2):\n")
	if len(lessons) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, l := range lessons {
			fmt.Fprintf(&b, "- %s: %s (predicted %d", l.TentativeID, l.Text, l.Score)
			if l.Replaces != "" {
				fmt.Fprintf(&b, ", replaces %s", l.Replaces)
			}
			b.WriteString(")\n")
		}
	}
	fmt.Fprintf(&b, "\n(batch: pass=%d index=%d)\n", pass, batchIndex)
	b.WriteString(`
Merge these into the global set. HARD rules (judge each yourself and report the verdict):
- TERSE: one imperative sentence, <= 30 words. Report "terse": true|false.
- CORPUS-AGNOSTIC: no specific high-value retrievable items (person/org/place names, dates,
  numbers, or question-specific facts/phrases) -- a strategy must be a GENERAL METHOD, not a
  specific clue. Report "corpus_specific": true|false and a "reason".
- NON-OVERLAPPING: if an item merely restates an existing strategy, "duplicate_of":"<id>"
  (do NOT create a new item for it).
- NON-CONTRADICTORY: list conflicting ids in "contradicts":["<id>"].

Every input id (in-scope strategy id or lesson id) MUST appear EXACTLY ONCE, either in some
item's "source_ids" or in "rejected". Never duplicate a source id, never invent one, and
never leave a lesson unmapped. To keep an existing strategy unchanged, emit an item whose
"id" is that strategy id, INCLUDING it in "source_ids", and leave "text" EMPTY -- do NOT
retype the strategy; an empty text keeps the stored wording exactly as it is. Fill "text"
only when you are actually CHANGING the wording. An item may NOT set "id" to a strategy
that is not among its own "source_ids".

Output ONLY JSON:
{"items":[{"id":"","text":"","source_ids":["..."],"terse":true,"corpus_specific":false,"duplicate_of":"","contradicts":[],"reason":""}],
 "rejected":[{"source_id":"...","reason":"..."}]}`)
	return b.String()
}

// parseMergeOutput extracts the JSON object from a model reply. An empty/null object is a
// failure, not a silent no-op.
func parseMergeOutput(text string) (llmMergeOutput, error) {
	cleaned := strings.TrimSpace(thinkRE.ReplaceAllString(text, ""))
	var out llmMergeOutput
	try := func(s string) bool {
		var o llmMergeOutput
		if err := json.Unmarshal([]byte(s), &o); err != nil {
			return false
		}
		if o.Items == nil && o.Rejected == nil {
			return false
		}
		out = o
		return true
	}
	if try(cleaned) {
		return out, nil
	}
	if lo, hi := strings.Index(cleaned, "{"), strings.LastIndex(cleaned, "}"); lo >= 0 && hi > lo {
		if try(cleaned[lo : hi+1]) {
			return out, nil
		}
	}
	return llmMergeOutput{}, fmt.Errorf("could not parse merge JSON from: %s", truncate(cleaned, 300))
}

// mergeBatch runs the batch update under a single attempt identity: call the merge model,
// parse it, apply the verdicts, then append the full audit row. A model/parse failure
// keeps the current set; an AUDIT write failure is returned wrapped in errAudit so the
// caller aborts (a mandatory record must never be silently dropped).
func (e *executor) mergeBatch(ctx context.Context, set, base *strategySet, lessons []lesson, pass, batchIndex int, att attemptID, usedPairs map[string]bool) (*strategySet, error) {
	prompt := mergePrompt(set, lessons, pass, batchIndex)
	var out llmMergeOutput
	raw := ""
	err := callWithRetry(ctx, e.retry, func(attempt int) error {
		// UtilityReasoning, not the answering engine: the merge is a stateless JSON
		// transformation on its own dialog, and the agentic engine wraps it in an agent
		// loop that contributes nothing and adds a way to corrupt the whole batch.
		res, err := e.client.ask(ctx, e.cfg.MergeChatID, prompt, "", e.cfg.UtilityReasoning)
		if err != nil {
			return err
		}
		if answerLooksLikeError(res.Answer) {
			return fmt.Errorf("merge reply is a provider error: %s", truncate(res.Answer, 120))
		}
		raw = res.Answer
		parsed, perr := parseMergeOutput(res.Answer)
		if perr != nil {
			return perr
		}
		out = parsed
		return nil
	})
	if err != nil {
		// The model/parse failed: audit the failure (a write failure here is fatal too).
		if werr := e.out.appendJSONL("merge_history.jsonl", map[string]any{
			"attempt_id": att, "pass": pass, "batch_index": batchIndex,
			"before_version": set.Version, "failed": true, "raw": truncate(raw, 2000), "error": err.Error(),
		}); werr != nil {
			return set, fmt.Errorf("%w: %v", errAudit, werr)
		}
		return set, fmt.Errorf("merge call: %w", err)
	}
	ns, rejected, aerr := applyMerge(set, out, lessons, e.cfg.MaxStrategies, e.cfg.MaxStrategyWords,
		withUsedPairs(usedPairs), withBase(base))
	if aerr != nil {
		if werr := e.out.appendJSONL("merge_history.jsonl", map[string]any{
			"attempt_id": att, "pass": pass, "batch_index": batchIndex,
			"before_version": set.Version, "invariant_violation": aerr.Error(), "llm_out": out,
		}); werr != nil {
			return set, fmt.Errorf("%w: %v", errAudit, werr)
		}
		return set, fmt.Errorf("merge invariant: %w", aerr)
	}
	if err := e.out.appendJSONL("merge_history.jsonl", map[string]any{
		"attempt_id": att, "pass": pass, "batch_index": batchIndex,
		"before_version": set.Version, "after_version": ns.Version,
		"before_items": set.Items, "after_items": ns.Items,
		"llm_out": out, "rejected": rejected,
	}); err != nil {
		return set, fmt.Errorf("%w: %v", errAudit, err)
	}
	if len(rejected) > 0 {
		e.logf("merge: %d rejected: %s", len(rejected), truncate(strings.Join(rejected, "; "), 400))
	}
	return ns, nil
}
