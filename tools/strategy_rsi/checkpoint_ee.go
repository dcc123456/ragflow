//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

//go:build ee

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// artifactPerm is the mode for every run artifact: they may contain model answers and
// (redacted) configuration, so they are owner-only.
const artifactPerm = 0o600

// phase mirrors the plan's state machine (§12).
const (
	phaseTrain        = "train"
	phaseEvaluate     = "evaluate"
	phasePassComplete = "pass_complete"
)

// evalResult is one committed frozen evaluation (the single source of truth for
// min_delta comparisons; passes.jsonl is only the audit copy).
type evalResult struct {
	Pass     int     `json:"pass"`
	Version  int     `json:"version"`
	EvalID   string  `json:"eval_id"`
	Accuracy float64 `json:"accuracy"`
	Scored   int     `json:"scored"`
	Skipped  int     `json:"skipped"`
	Coverage float64 `json:"coverage"`
}

// checkpoint is the atomic commit point. Recovery reads only this file.
type checkpoint struct {
	Phase             string         `json:"phase"`
	Version           int            `json:"version"`
	Items             []strategyItem `json:"items"`
	ActiveIDs         []string       `json:"active_ids"`
	ItemsSHA256       string         `json:"items_sha256"`
	Pass              int            `json:"pass"`
	NextBatchIndex    int            `json:"next_batch_index"`
	PassSample        []int          `json:"pass_sample"`
	CommittedAttempts []string       `json:"committed_attempts"`
	Eval              struct {
		EvalID        string      `json:"eval_id"`
		Version       int         `json:"version"`
		NextIndex     int         `json:"next_index"`
		LastCommitted *evalResult `json:"last_committed"`
		// Prev is the evaluation BEFORE LastCommitted, so `converged` can compare against
		// it even after LastCommitted has been overwritten by the current result.
		PrevEvalID   string   `json:"prev_eval_id"`
		PrevAccuracy *float64 `json:"prev_accuracy"`
	} `json:"eval"`
	Evaluated        bool   `json:"evaluated"`
	Seed             int64  `json:"seed"`
	NextID           int    `json:"next_id"`
	StrategiesFile   string `json:"strategies_file"`
	StrategiesSHA256 string `json:"strategies_sha256"`
	FinalVersion     int    `json:"final_version,omitempty"`

	// Run identity -- validated on resume so a changed parameter cannot silently
	// misalign a continued run (plan §8.4).
	TrainsetSHA256  string `json:"trainset_sha256"`
	SelectedIDsHash string `json:"selected_ids_hash"`
	BatchSize       int    `json:"batch_size"`
	Reasoning       int    `json:"reasoning"`
	ChatID          string `json:"chat_id"`
	JudgeChatID     string `json:"judge_chat_id"`
	MergeChatID     string `json:"merge_chat_id"`
	PromptFP        string `json:"prompt_fingerprint"`
}

// outputDir holds the run directory plus its exclusive lock.
type outputDir struct {
	dir string
	lf  *os.File
}

func (o *outputDir) path(name string) string { return filepath.Join(o.dir, name) }

// openOutputDir creates the run directory and takes an exclusive lock. A second training
// process on the same directory fails fast (plan §8.4).
func openOutputDir(dir string) (*outputDir, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lf, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lf.Close()
		return nil, fmt.Errorf("output dir %s is locked by another run (exclusive lock held)", dir)
	}
	return &outputDir{dir: dir, lf: lf}, nil
}

func (o *outputDir) release() {
	if o == nil || o.lf == nil {
		return
	}
	_ = syscall.Flock(int(o.lf.Fd()), syscall.LOCK_UN)
	_ = o.lf.Close()
}

func itemsSHA(items []strategyItem) string {
	b, _ := json.Marshal(items)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// idsHashOf is a stable fingerprint of the selected question order.
func idsHashOf(recs []trainsetRecord) string {
	h := sha256.New()
	for _, r := range recs {
		h.Write([]byte(r.ID))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeCheckpoint atomically persists the commit point (tmp + rename). It must NOT clear
// the strategy-file reference: recovery needs it to verify the injected set.
func (o *outputDir) writeCheckpoint(cp *checkpoint) error {
	cp.ItemsSHA256 = itemsSHA(cp.Items)
	b, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	tmp := o.path("checkpoint.json.tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), artifactPerm); err != nil {
		return err
	}
	return os.Rename(tmp, o.path("checkpoint.json"))
}

// loadCheckpoint reads the commit point; (nil, false, nil) when none exists. It verifies
// both the items hash and (when recorded) the referenced strategy file.
func loadCheckpoint(dir string) (*checkpoint, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, "checkpoint.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var cp checkpoint
	if err := json.Unmarshal(b, &cp); err != nil {
		return nil, false, fmt.Errorf("parse checkpoint: %w", err)
	}
	if cp.ItemsSHA256 != "" && cp.ItemsSHA256 != itemsSHA(cp.Items) {
		return nil, false, fmt.Errorf("checkpoint items sha mismatch (corrupt checkpoint)")
	}
	if cp.StrategiesFile != "" && cp.StrategiesSHA256 != "" {
		sha, err := fileSHA256(filepath.Join(dir, cp.StrategiesFile))
		if err != nil {
			return nil, false, fmt.Errorf("strategy file %s missing: %w", cp.StrategiesFile, err)
		}
		if sha != cp.StrategiesSHA256 {
			return nil, false, fmt.Errorf("strategy file %s changed since commit", cp.StrategiesFile)
		}
	}
	return &cp, true, nil
}

// appendJSONL appends one JSON object to a JSONL file and fsyncs it. A trailing partial
// line (left by a hard kill mid-write) is truncated first: a committed append always ends
// with '\n', so the tail is by definition uncommitted. Errors are returned so the
// coordinator can refuse to advance the cursor past a missing audit record.
func (o *outputDir) appendJSONL(name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	path := o.path(name)
	if err := truncatePartialLine(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, artifactPerm)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// truncatePartialLine drops a trailing line that has no terminating newline.
func truncatePartialLine(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	size := fi.Size()
	if size == 0 {
		return nil
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, size-1); err != nil {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	buf := make([]byte, size)
	if _, err := f.ReadAt(buf, 0); err != nil && err != io.EOF {
		return err
	}
	i := bytes.LastIndexByte(buf, '\n')
	return f.Truncate(int64(i + 1))
}

// validateResume refuses to continue when a run parameter that would misalign the resume
// has changed. Kept at 3 args for the review probe; the coordinator uses
// validateResumeWith to also check the selected-id set.
func validateResume(cp *checkpoint, cfg config, meta trainsetMeta) error {
	return validateResumeWith(cp, cfg, meta, "", "")
}

func validateResumeWith(cp *checkpoint, cfg config, meta trainsetMeta, idsHash, promptFP string) error {
	if cp.Seed != cfg.Seed {
		return fmt.Errorf("resume refused: seed changed (%d -> %d)", cp.Seed, cfg.Seed)
	}
	// The trainset binding is mandatory: an absent recorded hash is a mismatch, not a
	// free pass (a checkpoint that never bound a trainset must not silently continue).
	if meta.SHA256 != "" && cp.TrainsetSHA256 != meta.SHA256 {
		return fmt.Errorf("resume refused: trainset changed (%q -> %q)", cp.TrainsetSHA256, meta.SHA256)
	}
	if cp.BatchSize != 0 && cp.BatchSize != cfg.BatchSize {
		return fmt.Errorf("resume refused: batch_size changed (%d -> %d)", cp.BatchSize, cfg.BatchSize)
	}
	if cp.Reasoning != 0 && cp.Reasoning != cfg.Reasoning {
		return fmt.Errorf("resume refused: reasoning changed (%d -> %d)", cp.Reasoning, cfg.Reasoning)
	}
	if cp.ChatID != "" && cp.ChatID != cfg.ChatID {
		return fmt.Errorf("resume refused: chat_id changed")
	}
	if cp.JudgeChatID != "" && cp.JudgeChatID != cfg.JudgeChatID {
		return fmt.Errorf("resume refused: judge_chat_id changed")
	}
	if cp.MergeChatID != "" && cp.MergeChatID != cfg.MergeChatID {
		return fmt.Errorf("resume refused: merge_chat_id changed")
	}
	if idsHash != "" && cp.SelectedIDsHash != "" && cp.SelectedIDsHash != idsHash {
		return fmt.Errorf("resume refused: selected question set changed")
	}
	if promptFP != "" && cp.PromptFP != "" && cp.PromptFP != promptFP {
		return fmt.Errorf("resume refused: prompt fingerprint changed")
	}
	return nil
}

// bindRunIdentity records the run identity on a fresh checkpoint.
func bindRunIdentity(cp *checkpoint, cfg config, meta trainsetMeta, idsHash, promptFP string) {
	cp.TrainsetSHA256 = meta.SHA256
	cp.SelectedIDsHash = idsHash
	cp.BatchSize = cfg.BatchSize
	cp.Reasoning = cfg.Reasoning
	cp.ChatID = cfg.ChatID
	cp.JudgeChatID = cfg.JudgeChatID
	cp.MergeChatID = cfg.MergeChatID
	cp.PromptFP = promptFP
}
