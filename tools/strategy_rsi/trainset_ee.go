//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

//go:build ee

package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// trainsetRecord is one line of the frozen trainset file (plan §3.3).
type trainsetRecord struct {
	ID             string   `json:"id"`
	Question       string   `json:"question"`
	GoldAnswer     string   `json:"gold_answer"`
	ExpectedDocIDs []string `json:"expected_doc_ids"`
	EvidenceDocIDs []string `json:"evidence_doc_ids"`
	Stats          struct {
		Wrong   int      `json:"wrong"`
		Seen    int      `json:"seen"`
		Sources []string `json:"sources"`
	} `json:"stats"`
}

// trainsetMeta is the sidecar written next to the trainset for provenance + integrity.
type trainsetMeta struct {
	BuiltAt             string   `json:"built_at"`
	BenchmarkOutputsDir string   `json:"benchmark_outputs_dir"`
	RunFilter           string   `json:"run_filter"`
	SortRule            string   `json:"sort_rule"`
	NRuns               int      `json:"n_runs"`
	NRows               int      `json:"n_rows"`
	NQuestionIDs        int      `json:"n_question_ids"`
	TopN                int      `json:"top_n"`
	CutoffWrong         int      `json:"cutoff_wrong"`
	CutoffSeen          int      `json:"cutoff_seen"`
	QuestionsPath       string   `json:"questions_path"`
	Trainset            string   `json:"trainset"`
	SHA256              string   `json:"sha256"`
	Runs                []string `json:"runs"`
	ExcludedRuns        []string `json:"excluded_runs"`
}

// strictRunFilter selects the runs counted toward the train set: the `browsecomp_*`
// and `c2_*` benchmark families only (the strict scope chosen for this experiment).
func strictRunFilter(run string) bool {
	return strings.HasPrefix(run, "browsecomp") || strings.HasPrefix(run, "c2_")
}

// buildTrainset aggregates wrong-answer counts across the benchmark outputs under
// benchDir and writes the topN highest-wrong-frequency questions to outPath, plus a
// meta sidecar. Only `*/leaderboard.json` files carrying per_query_judgements count.
func buildTrainset(benchDir, questionsPath string, topN int, outPath string) (trainsetMeta, error) {
	paths, err := filepath.Glob(filepath.Join(benchDir, "*", "leaderboard.json"))
	if err != nil {
		return trainsetMeta{}, err
	}
	sort.Strings(paths)

	wrong := map[string]int{}
	seen := map[string]int{}
	sources := map[string]map[string]bool{}
	var runs, excluded []string
	rows := 0

	for _, p := range paths {
		run := filepath.Base(filepath.Dir(p))
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var lb struct {
			PerQueryJudgements []struct {
				QueryID string `json:"query_id"`
				Correct *bool  `json:"correct"`
			} `json:"per_query_judgements"`
		}
		if err := json.Unmarshal(raw, &lb); err != nil || len(lb.PerQueryJudgements) == 0 {
			continue
		}
		if !strictRunFilter(run) {
			excluded = append(excluded, run)
			continue
		}
		runs = append(runs, run)
		for _, j := range lb.PerQueryJudgements {
			qid := strings.TrimSpace(j.QueryID)
			if qid == "" || qid == "None" {
				continue
			}
			// Only JUDGED rows count: `correct` is nil for unjudged / judge_error rows
			// (service or judge failures), which must not inflate the wrong count.
			if j.Correct == nil {
				continue
			}
			rows++
			seen[qid]++
			if !*j.Correct {
				wrong[qid]++
				if sources[qid] == nil {
					sources[qid] = map[string]bool{}
				}
				sources[qid][run] = true
			}
		}
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	// Rank by WRONG RATE first (wrong/seen), then by absolute wrong count, then by id.
	// The rate surfaces questions that fail consistently even on few exposures; the
	// absolute count breaks ties toward the more-witnessed failures. Compared by cross
	// multiplication to keep it exact (seen is always > 0 here).
	rateGreater := func(a, b string) bool {
		return int64(wrong[a])*int64(seen[b]) > int64(wrong[b])*int64(seen[a])
	}
	sort.Slice(ids, func(i, j int) bool {
		if rateGreater(ids[i], ids[j]) {
			return true
		}
		if rateGreater(ids[j], ids[i]) {
			return false
		}
		if wrong[ids[i]] != wrong[ids[j]] {
			return wrong[ids[i]] > wrong[ids[j]]
		}
		return ids[i] < ids[j]
	})
	if topN > 0 && len(ids) > topN {
		ids = ids[:topN]
	}

	_, qidx, err := loadQuestions(questionsPath)
	if err != nil {
		return trainsetMeta{}, err
	}
	var missing []string
	for _, id := range ids {
		if _, ok := qidx[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return trainsetMeta{}, fmt.Errorf("trainset ids missing from corpus: %v", missing)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return trainsetMeta{}, err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return trainsetMeta{}, err
	}
	for _, id := range ids {
		q := qidx[id]
		srcs := make([]string, 0, len(sources[id]))
		for s := range sources[id] {
			srcs = append(srcs, s)
		}
		sort.Strings(srcs)
		rec := trainsetRecord{
			ID: id, Question: q.Question, GoldAnswer: q.GoldAnswer,
			ExpectedDocIDs: q.ExpectedDocIDs, EvidenceDocIDs: q.EvidenceDocIDs,
		}
		rec.Stats.Wrong = wrong[id]
		rec.Stats.Seen = seen[id]
		rec.Stats.Sources = srcs
		line, _ := json.Marshal(rec)
		if _, err := f.Write(append(line, '\n')); err != nil {
			f.Close()
			return trainsetMeta{}, err
		}
	}
	if err := f.Close(); err != nil {
		return trainsetMeta{}, err
	}

	sha, err := fileSHA256(outPath)
	if err != nil {
		return trainsetMeta{}, err
	}
	meta := trainsetMeta{
		BuiltAt:             nowISO(),
		BenchmarkOutputsDir: benchDir,
		RunFilter:           "dirname starts with 'browsecomp' or 'c2_' (strict)",
		SortRule:            "wrong/seen desc, wrong desc, id asc",
		NRuns:               len(runs), NRows: rows, NQuestionIDs: len(seen), TopN: len(ids),
		QuestionsPath: questionsPath, Trainset: filepath.Base(outPath), SHA256: sha,
		Runs: runs, ExcludedRuns: excluded,
	}
	if meta.TopN > 0 {
		meta.CutoffWrong = wrong[ids[len(ids)-1]]
		meta.CutoffSeen = seen[ids[len(ids)-1]]
	}
	mb, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metaPathFor(outPath), append(mb, '\n'), 0o644); err != nil {
		return trainsetMeta{}, err
	}
	return meta, nil
}

// loadTrainset reads the frozen trainset and verifies its sha256 against the sidecar.
// A mismatch is fatal: the run must not proceed on a changed question set.
func loadTrainset(path string) ([]trainsetRecord, trainsetMeta, error) {
	sha, err := fileSHA256(path)
	if err != nil {
		return nil, trainsetMeta{}, err
	}
	var meta trainsetMeta
	mb, err := os.ReadFile(metaPathFor(path))
	if err != nil {
		return nil, meta, fmt.Errorf("trainset meta %s missing (rebuild with --build-trainset): %w", metaPathFor(path), err)
	}
	if err := json.Unmarshal(mb, &meta); err != nil {
		return nil, meta, fmt.Errorf("parse trainset meta: %w", err)
	}
	if meta.SHA256 == "" {
		return nil, meta, fmt.Errorf("trainset meta %s has no sha256; refusing to run unbound", metaPathFor(path))
	}
	if meta.SHA256 != sha {
		return nil, meta, fmt.Errorf("trainset sha256 mismatch: file=%s meta=%s (rebuild with --build-trainset)", sha, meta.SHA256)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, meta, err
	}
	defer f.Close()
	var recs []trainsetRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	line := 0
	for sc.Scan() {
		line++
		if len(sc.Bytes()) == 0 {
			continue
		}
		var rec trainsetRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, meta, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		recs = append(recs, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, meta, err
	}
	return recs, meta, nil
}

func metaPathFor(trainsetPath string) string {
	return strings.TrimSuffix(trainsetPath, filepath.Ext(trainsetPath)) + ".meta.json"
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
