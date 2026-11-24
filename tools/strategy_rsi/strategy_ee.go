//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

//go:build ee

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// strategyItem is one candidate strategy with its accumulated (heuristic) score.
type strategyItem struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	CreatedBy   string   `json:"created_by"`
	TS          string   `json:"ts"`
	TotalScore  int      `json:"total_score"`
	RatingCount int      `json:"rating_count"`
	SourceIDs   []string `json:"source_ids,omitempty"`
}

// strategySet is the global candidate pool + the ≤max active ids. Version increments on
// every applied batch update (even a score-only change), so each ranking is traceable.
// NextID is a MONOTONIC allocator: ids are never reused after a deletion.
type strategySet struct {
	Version   int            `json:"version"`
	NextID    int            `json:"next_id"`
	Items     []strategyItem `json:"items"`
	ActiveIDs []string       `json:"active_ids"`
}

func newStrategySet() *strategySet { return &strategySet{Version: 0} }

// allocID hands out the next monotonic strategy id. It never reuses a retired id, so a
// deletion cannot cause an unrelated new strategy to inherit an old identity.
func (s *strategySet) allocID() string {
	if s.NextID < 0 {
		s.NextID = 0
	}
	s.NextID++
	return fmt.Sprintf("s%04d", s.NextID)
}

// seedNextID initialises the allocator from the pool's current maximum (used after
// loading a checkpoint that predates the NextID field).
func (s *strategySet) seedNextID() {
	max := s.NextID
	for _, it := range s.Items {
		if strings.HasPrefix(it.ID, "s") {
			if n, err := strconv.Atoi(strings.TrimPrefix(it.ID, "s")); err == nil && n > max {
				max = n
			}
		}
	}
	s.NextID = max
}

func (s *strategySet) byID(id string) *strategyItem {
	for i := range s.Items {
		if s.Items[i].ID == id {
			return &s.Items[i]
		}
	}
	return nil
}

func (s *strategySet) hasID(id string) bool { return s.byID(id) != nil }

// nextID is retained for compatibility but delegates to the monotonic allocator.
func (s *strategySet) nextID() string { return s.allocID() }

// addScore accumulates one self-reflection rating onto an existing strategy.
func (s *strategySet) addScore(id string, delta int) {
	if it := s.byID(id); it != nil {
		it.TotalScore += delta
		it.RatingCount++
	}
}

// rank orders the pool by (total_score desc, id asc) and sets ActiveIDs to the top n.
// The full pool is always retained; only the active prefix is injected.
func (s *strategySet) rank(maxStrategies int) {
	sort.SliceStable(s.Items, func(i, j int) bool {
		if s.Items[i].TotalScore != s.Items[j].TotalScore {
			return s.Items[i].TotalScore > s.Items[j].TotalScore
		}
		return s.Items[i].ID < s.Items[j].ID
	})
	n := maxStrategies
	if n > len(s.Items) {
		n = len(s.Items)
	}
	if n < 0 {
		n = 0
	}
	s.ActiveIDs = s.ActiveIDs[:0]
	for i := 0; i < n; i++ {
		s.ActiveIDs = append(s.ActiveIDs, s.Items[i].ID)
	}
}

// activeItems returns the active strategies in rank order.
func (s *strategySet) activeItems() []strategyItem {
	var out []strategyItem
	for _, id := range s.ActiveIDs {
		if it := s.byID(id); it != nil {
			out = append(out, *it)
		}
	}
	return out
}

// renderStrategyBlock renders the block prepended to the question (§7). Empty set -> "".
func (s *strategySet) renderStrategyBlock() string {
	items := s.activeItems()
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Follow the following search strategy:\n")
	for _, it := range items {
		b.WriteString("- ")
		b.WriteString(it.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// strategyListForPrompt renders the in-force list for the reflection prompt (id + text).
func (s *strategySet) strategyListForPrompt() string {
	items := s.activeItems()
	if len(items) == 0 {
		return "(none yet)"
	}
	var b strings.Builder
	for _, it := range items {
		b.WriteString("- ")
		b.WriteString(it.ID)
		b.WriteString(": ")
		b.WriteString(it.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// renderFile renders the versioned strategy text file (§12.1).
func (s *strategySet) renderFile(origin string) string {
	var body strings.Builder
	for _, it := range s.activeItems() {
		body.WriteString("- ")
		body.WriteString(it.Text)
		body.WriteString("\n")
	}
	sum := sha256.Sum256([]byte(body.String()))
	var b strings.Builder
	b.WriteString("# ragflow query-strategy set\n")
	fmt.Fprintf(&b, "# version: %d\n", s.Version)
	fmt.Fprintf(&b, "# updated: %s\n", nowISO())
	if origin != "" {
		fmt.Fprintf(&b, "# origin: %s\n", origin)
	}
	fmt.Fprintf(&b, "# sha256: %s\n", hex.EncodeToString(sum[:]))
	b.WriteString(body.String())
	return b.String()
}

var versionLineRE = regexp.MustCompile(`(?m)^#\s*version:\s*(\d+)\s*$`)

// parseStrategyFile reads a versioned strategy file back into (version, texts).
func parseStrategyFile(text string) (int, []string) {
	version := 0
	if m := versionLineRE.FindStringSubmatch(text); m != nil {
		version, _ = strconv.Atoi(m[1])
	}
	var texts []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			texts = append(texts, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
		}
	}
	return version, texts
}

// writeStrategyFile atomically writes the versioned strategy file (tmp + rename).
func writeStrategyFile(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// appendStrategySnapshot records one line per strategy VERSION in strategies.jsonl: the
// whole pool with each item's accumulated score, rating count and active flag. The
// versioned strategies.vNNNN.txt files carry only the rendered text (what the model is
// given) and the checkpoint is a single mutable state file, so this is the append-only
// trace of how the pool evolved.
//
// Idempotent by version: a re-persist after a crash between the file write and the
// checkpoint commit must not duplicate the line.
func (e *executor) appendStrategySnapshot(set *strategySet, origin, sfile string) error {
	if e.out == nil {
		return nil
	}
	seen, err := snapshotVersions(e.out.path("strategies.jsonl"))
	if err != nil {
		return err
	}
	if seen[set.Version] {
		return nil
	}
	active := make(map[string]bool, len(set.ActiveIDs))
	for _, id := range set.ActiveIDs {
		active[id] = true
	}
	items := make([]map[string]any, 0, len(set.Items))
	for _, it := range set.Items {
		items = append(items, map[string]any{
			"id":           it.ID,
			"text":         it.Text,
			"total_score":  it.TotalScore,
			"rating_count": it.RatingCount,
			"active":       active[it.ID],
			"created_by":   it.CreatedBy,
			"source_ids":   it.SourceIDs,
		})
	}
	return e.out.appendJSONL("strategies.jsonl", map[string]any{
		"version":         set.Version,
		"ts":              nowISO(),
		"origin":          origin,
		"strategies_file": sfile,
		"next_id":         set.NextID,
		"active_ids":      append([]string(nil), set.ActiveIDs...),
		"items":           items,
	})
}

// snapshotVersions returns the versions already present in a strategies.jsonl snapshot.
func snapshotVersions(path string) (map[int]bool, error) {
	rows, err := readJSONLStrict(path)
	if err != nil {
		return nil, err
	}
	out := make(map[int]bool, len(rows))
	for _, r := range rows {
		if v, ok := r["version"].(float64); ok {
			out[int(v)] = true
		}
	}
	return out, nil
}
