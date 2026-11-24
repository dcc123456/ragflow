//go:build ee

//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

package main

import (
	"fmt"
	"sort"
	"strings"
)

// lesson is one self-reflection lesson (a tentative candidate), before batch mapping.
type lesson struct {
	TentativeID string // t<pass>.<batch>.<qpos>.<k>
	QID         string // the question it came from (provenance / same-question rules)
	Text        string
	Score       int // predicted usefulness (-2..+2)
	Reason      string
	Replaces    string // optional target strategy id
}

// llmItem is one merged item as returned by the merge model. The four quality verdicts
// are the MODEL's (plan §9.2); the program only applies them.
type llmItem struct {
	ID             string   `json:"id"`
	Text           string   `json:"text"`
	SourceIDs      []string `json:"source_ids"`
	Terse          *bool    `json:"terse"`
	CorpusSpecific *bool    `json:"corpus_specific"`
	DuplicateOf    string   `json:"duplicate_of"`
	Contradicts    []string `json:"contradicts"`
	Reason         string   `json:"reason"`
}

type llmRejection struct {
	SourceID string `json:"source_id"`
	Reason   string `json:"reason"`
}

// llmMergeOutput is the merge model's JSON reply (plan §9.1).
type llmMergeOutput struct {
	Items    []llmItem      `json:"items"`
	Rejected []llmRejection `json:"rejected"`
}

// mergeCtx carries optional cross-cutting inputs without changing the call arity.
type mergeCtx struct {
	// usedPairs marks (qid|strategyID) that already received a `used` rating this batch;
	// a mapped (predicted) score for the same pair is skipped so `used` wins.
	usedPairs map[string]bool
	// base is the pre-accumulation committed set. When an update is NOT applied (empty
	// result / no change), it is returned instead of the accumulated input, so the batch's
	// ratings are discarded with it.
	base *strategySet
}

type mergeOption func(*mergeCtx)

func withUsedPairs(m map[string]bool) mergeOption { return func(c *mergeCtx) { c.usedPairs = m } }

func withBase(s *strategySet) mergeOption { return func(c *mergeCtx) { c.base = s } }

// normalizeStrategyText applies the ONLY mechanical rules (plan §9.2).
func normalizeStrategyText(s string, maxWords int) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "-•*0123456789.) \t")
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'“”‘’")
	s = strings.TrimRight(s, "。.;；,，")
	s = strings.Join(strings.Fields(s), " ")
	s = truncateWords(s, maxWords)
	return strings.TrimSpace(s)
}

// applyMerge takes the current set, this batch's lessons and the model's merge output and
// produces the NEXT set (the committed set is never mutated).
//
// Invariants enforced over the WHOLE batch update:
//   - every lesson is mapped into exactly one item or explicitly rejected;
//   - each input id is referenced at most once; empty/unknown ids reject the merge;
//   - a rewrite must own its id: an item may only claim an existing id that is one of its
//     own source_ids, and two items may not claim the same id;
//   - consumed/rejected/conflict-losing sources are retired -- a conflict loser is NOT
//     resurrected by its own rewrite item;
//   - contradictions are decided against the IMMUTABLE scores (deterministic tie-break);
//   - a missing terse/corpus_specific verdict is a contract violation;
//   - an update that would empty the pool keeps the previous pool and version (§9.3).
func applyMerge(set *strategySet, out llmMergeOutput, lessons []lesson, maxStrategies, maxWords int, opts ...mergeOption) (*strategySet, []string, error) {
	mctx := mergeCtx{}
	for _, o := range opts {
		o(&mctx)
	}
	known := map[string]*strategyItem{}
	for i := range set.Items {
		known[set.Items[i].ID] = &set.Items[i]
	}
	lessonByID := map[string]lesson{}
	for _, l := range lessons {
		lessonByID[l.TentativeID] = l
	}
	knownInput := func(id string) bool {
		if _, ok := known[id]; ok {
			return true
		}
		_, ok := lessonByID[id]
		return ok
	}

	// ---- 1. source accounting (disjoint + complete coverage of lessons) ----
	used := map[string]int{}
	usedBy := map[string]string{}
	note := func(id, where string) error {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("empty source id in %s", where)
		}
		if !knownInput(id) {
			return fmt.Errorf("unknown source id %q in %s", id, where)
		}
		used[id]++
		if used[id] > 1 {
			return fmt.Errorf("source id %q referenced more than once (%s and %s)", id, usedBy[id], where)
		}
		usedBy[id] = where
		return nil
	}
	for i, it := range out.Items {
		if len(it.SourceIDs) == 0 {
			return nil, nil, fmt.Errorf("item[%d] has no source_ids", i)
		}
		for _, sid := range it.SourceIDs {
			if err := note(sid, fmt.Sprintf("item[%d]", i)); err != nil {
				return nil, nil, err
			}
		}
	}
	rejectedIDs := map[string]bool{}
	for _, r := range out.Rejected {
		if err := note(r.SourceID, "rejected"); err != nil {
			return nil, nil, err
		}
		rejectedIDs[r.SourceID] = true
	}
	for _, l := range lessons {
		if used[l.TentativeID] == 0 {
			return nil, nil, fmt.Errorf("lesson %q was neither mapped nor rejected", l.TentativeID)
		}
	}

	// ---- 2. decide every item against the immutable pool ----
	type planned struct {
		add       *strategyItem
		ownID     string
		retire    []string
		hardLoser bool
		reason    string
	}
	var plans []planned
	pendingWin := map[string]bool{}
	claimed := map[string]int{} // output rewrite id -> first item index
	dupDelta := map[string]int{}
	dupCount := map[string]int{}
	predSeen := map[string]bool{} // (qid|target) already given a predicted score this batch

	for i, it := range out.Items {
		if it.Terse == nil || it.CorpusSpecific == nil {
			return nil, nil, fmt.Errorf("item[%d] missing terse/corpus_specific verdict", i)
		}
		text := normalizeStrategyText(it.Text, maxWords)
		// Keeping an existing strategy needs NO text. Requiring the model to echo every
		// kept strategy verbatim put the whole batch update on a token-exact copy: a slip
		// in the copy either breaks the reply outright or silently rewrites the strategy.
		// An item that claims an existing id may therefore send "text":"" and the stored
		// wording is used. The stored text already passed these checks when it entered the
		// pool, so the verdicts below apply only to text the model actually wrote.
		keepStored := text == "" && it.ID != "" && known[it.ID] != nil && containsStr(it.SourceIDs, it.ID)
		switch {
		case text == "" && !keepStored:
			plans = append(plans, planned{retire: it.SourceIDs, reason: fmt.Sprintf("item[%d]: empty text", i)})
			continue
		case !keepStored && !*it.Terse:
			plans = append(plans, planned{retire: it.SourceIDs, reason: fmt.Sprintf("item[%d]: not terse: %s", i, truncate(text, 60))})
			continue
		case !keepStored && *it.CorpusSpecific:
			plans = append(plans, planned{retire: it.SourceIDs, reason: fmt.Sprintf("item[%d]: corpus-specific: %s", i, truncate(text, 60))})
			continue
		}
		// duplicate_of: restatement of an existing strategy -> map the lesson's predicted
		// score onto it, unless that (question, strategy) already got a `used` rating or a
		// prior lesson. The MAPPED TARGET is never consumed by this item.
		if it.DuplicateOf != "" {
			target := known[it.DuplicateOf]
			if target == nil {
				return nil, nil, fmt.Errorf("item[%d] duplicate_of unknown id %q", i, it.DuplicateOf)
			}
			var retire []string
			for _, sid := range it.SourceIDs {
				if sid == it.DuplicateOf {
					continue // the mapping target stays in the pool
				}
				retire = append(retire, sid)
				l, ok := lessonByID[sid]
				if !ok {
					continue
				}
				pair := l.QID + "|" + target.ID
				if mctx.usedPairs[pair] || predSeen[pair] {
					continue // used wins, and a repeat prediction counts once
				}
				predSeen[pair] = true
				dupDelta[target.ID] += l.Score
				dupCount[target.ID]++
			}
			plans = append(plans, planned{retire: retire})
			continue
		}
		// Identity: an item may only claim an existing id that is one of its own sources.
		ownID := ""
		if it.ID != "" {
			if known[it.ID] == nil {
				ownID = "" // unknown id -> the uniform "create new" contract
			} else if !containsStr(it.SourceIDs, it.ID) {
				return nil, nil, fmt.Errorf("item[%d] claims existing id %q that is not in its source_ids (unconsumed identity overwrite)", i, it.ID)
			} else {
				if prev, dup := claimed[it.ID]; dup {
					return nil, nil, fmt.Errorf("item[%d] and item[%d] both claim id %q", prev, i, it.ID)
				}
				claimed[it.ID] = i
				ownID = it.ID
			}
		}
		if keepStored {
			// The guard above only sets keepStored when the claim is legal, so ownID is set;
			// substitute the stored wording for the omitted text.
			if ownID == "" {
				plans = append(plans, planned{retire: it.SourceIDs, reason: fmt.Sprintf("item[%d]: omitted text with no claimable id", i)})
				continue
			}
			text = known[ownID].Text
		}
		score, count := inheritFrom(known, lessonByID, it.SourceIDs)
		// Contradiction decided against the immutable scores, with a deterministic tie-break.
		losesTo := ""
		for _, cid := range it.Contradicts {
			other := known[cid]
			if other == nil {
				return nil, nil, fmt.Errorf("item[%d] contradicts unknown id %q", i, cid)
			}
			loser := score < other.TotalScore
			if !loser && score == other.TotalScore {
				// new items yield to existing ones; between existing ids the larger loses
				loser = ownID == "" || ownID > other.ID
			}
			if loser {
				losesTo = cid
				break
			}
		}
		if losesTo != "" {
			plans = append(plans, planned{ownID: ownID, hardLoser: ownID != "", retire: it.SourceIDs,
				reason: fmt.Sprintf("item[%d]: contradiction lost to %s", i, losesTo)})
			continue
		}
		for _, cid := range it.Contradicts {
			pendingWin[cid] = true
		}
		plans = append(plans, planned{
			add: &strategyItem{
				ID: ownID, Text: text, CreatedBy: "merge", TS: nowISO(),
				SourceIDs: append([]string(nil), it.SourceIDs...), TotalScore: score, RatingCount: count,
			},
			ownID:  ownID,
			retire: it.SourceIDs,
		})
	}

	// ---- 3. rebuild the pool from the immutable input ----
	hardRetire := map[string]bool{}
	for id := range rejectedIDs {
		hardRetire[id] = true
	}
	for id := range pendingWin {
		hardRetire[id] = true
	}
	consumed := map[string]bool{}
	ownRewrite := map[string]bool{}
	var survivors []planned
	for _, p := range plans {
		if p.hardLoser {
			if p.ownID != "" {
				hardRetire[p.ownID] = true
			}
			// A merged group that loses retires EVERY source it consumed, not just the
			// representative id.
			for _, sid := range p.retire {
				if sid != p.ownID {
					consumed[sid] = true
				}
			}
			continue
		}
		survivors = append(survivors, p)
		if p.ownID != "" {
			ownRewrite[p.ownID] = true
		}
		for _, sid := range p.retire {
			if sid != p.ownID {
				consumed[sid] = true
			}
		}
	}

	ns := &strategySet{Version: set.Version, NextID: set.NextID}
	for _, it := range set.Items {
		if hardRetire[it.ID] {
			continue
		}
		if consumed[it.ID] && !ownRewrite[it.ID] {
			continue
		}
		ns.Items = append(ns.Items, it)
	}
	changed := len(ns.Items) != len(set.Items)

	for _, p := range survivors {
		if p.add == nil {
			continue
		}
		if p.ownID != "" {
			for i := range ns.Items {
				if ns.Items[i].ID == p.ownID {
					ns.Items[i] = *p.add
					changed = true
					break
				}
			}
			continue
		}
		item := *p.add
		item.ID = ns.allocID()
		if item.RatingCount == 0 {
			item.RatingCount = 1
		}
		ns.Items = append(ns.Items, item)
		changed = true
	}
	// Mapped (predicted) scores are applied AFTER rewrites, so a rewrite of the same
	// strategy can never overwrite them.
	for id, d := range dupDelta {
		for i := range ns.Items {
			if ns.Items[i].ID == id {
				ns.Items[i].TotalScore += d
				ns.Items[i].RatingCount += dupCount[id]
				changed = true
			}
		}
	}
	// Whole-pool duplicate collapse: per-call validity is not pool validity (§9.1).
	if deduped, dc := dedupePool(ns.Items); dc {
		ns.Items = deduped
		changed = true
	}

	var rej []string
	for _, p := range plans {
		if p.reason != "" {
			rej = append(rej, p.reason)
		}
	}

	// §9.3: an update that would empty the pool keeps the previous pool (and version).
	// Distinguish two "not applied" cases from a REAL update:
	//   - empty pool  -> keep the previous pool (§9.3);
	//   - nothing at all changed (structure AND scores) -> keep the previous pool.
	// A merge that only CHANGES SCORES (e.g. a used rating with the structure unchanged)
	// IS applied: the scores live in `set` (= the accumulated input), so returning the
	// pre-accumulation base would silently discard this batch's ratings.
	base := set
	if mctx.base != nil {
		base = mctx.base
	}
	fallback := func() *strategySet {
		return &strategySet{Version: base.Version, NextID: ns.NextID,
			Items: cloneItems(base.Items), ActiveIDs: append([]string(nil), base.ActiveIDs...)}
	}
	if len(ns.Items) == 0 {
		return fallback(), rej, nil
	}
	if !changed && !scoresChanged(base, ns) {
		return fallback(), rej, nil
	}
	ns.Version = base.Version + 1
	ns.rank(maxStrategies)
	return ns, rej, nil
}

// dedupePool collapses duplicate/near-duplicate strategies across the WHOLE pool,
// deterministically: candidates are examined in (score desc, id asc) order and each is
// compared against EVERY kept item, so the survivors are pairwise distinct and a score
// tie is resolved by the stable id (not by iteration order). Exact text plus a lexical
// near-duplicate net (token Jaccard) catch the common case; a full semantic adjudication
// would need a second model pass and is a documented limitation.
func dedupePool(items []strategyItem) ([]strategyItem, bool) {
	ordered := append([]strategyItem(nil), items...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].TotalScore != ordered[j].TotalScore {
			return ordered[i].TotalScore > ordered[j].TotalScore
		}
		return ordered[i].ID < ordered[j].ID
	})
	var out []strategyItem
	changed := false
	for _, it := range ordered {
		dup := false
		for j := range out {
			if nearDuplicate(out[j].Text, it.Text) {
				dup = true
				break
			}
		}
		if dup {
			changed = true
			continue
		}
		out = append(out, it)
	}
	return out, changed
}

func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range splitFields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,;:()[]\"'`")
		if w != "" {
			set[w] = true
		}
	}
	return set
}

// nearDuplicate reports lexical near-identity (either text empty -> exact only).
func nearDuplicate(a, b string) bool {
	sa, sb := tokenSet(a), tokenSet(b)
	if len(sa) == 0 || len(sb) == 0 {
		return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	inter := 0
	for w := range sa {
		if sb[w] {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return true
	}
	return float64(inter)/float64(union) >= 0.8
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// inheritFrom returns the score and rating count of the SINGLE winning source (max
// score, tie by id) -- never a mix of one source's score with another's count.
func inheritFrom(known map[string]*strategyItem, lessons map[string]lesson, sources []string) (int, int) {
	type cand struct {
		id    string
		score int
		count int
	}
	var best *cand
	for _, sid := range sources {
		var c cand
		if it, ok := known[sid]; ok {
			c = cand{sid, it.TotalScore, it.RatingCount}
		} else if l, ok := lessons[sid]; ok {
			c = cand{sid, l.Score, 1}
		} else {
			continue
		}
		if best == nil || c.score > best.score || (c.score == best.score && c.id < best.id) {
			cc := c
			best = &cc
		}
	}
	if best == nil {
		return 0, 0
	}
	return best.score, best.count
}

// rankByIDSorted is a tiny helper used by tests.
func rankByIDSorted(ids []string) []string {
	cp := append([]string(nil), ids...)
	sort.Strings(cp)
	return cp
}
