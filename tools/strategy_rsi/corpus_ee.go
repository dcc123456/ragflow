//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

//go:build ee

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// question is one BrowseComp-Plus record from questions.jsonl.
type question struct {
	ID             string
	Question       string
	GoldAnswer     string
	ExpectedDocIDs []string
	EvidenceDocIDs []string
}

type rawQuestion struct {
	ID             string   `json:"id"`
	Question       string   `json:"question"`
	GoldAnswer     string   `json:"gold_answer"`
	ExpectedDocIDs []string `json:"expected_doc_ids"`
	EvidenceDocIDs []string `json:"evidence_doc_ids"`
}

// loadQuestions reads questions.jsonl and returns the ordered slice plus an id index.
func loadQuestions(path string) ([]question, map[string]question, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open questions %s: %w", path, err)
	}
	defer f.Close()

	var out []question
	idx := map[string]question{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var rq rawQuestion
		if err := json.Unmarshal(raw, &rq); err != nil {
			return nil, nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if rq.ID == "" {
			continue
		}
		q := question{
			ID:             rq.ID,
			Question:       rq.Question,
			GoldAnswer:     rq.GoldAnswer,
			ExpectedDocIDs: rq.ExpectedDocIDs,
			EvidenceDocIDs: rq.EvidenceDocIDs,
		}
		out = append(out, q)
		idx[rq.ID] = q
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan questions: %w", err)
	}
	return out, idx, nil
}
