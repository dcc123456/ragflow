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
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

// Package main implements the query-strategy RSI tool (docs/develop/query-strategy-rsi.md).
//
// It trains a shared set of *query strategies* by a mini-batch loop over a frozen
// question set: each question is answered with the current strategy block, a wrong
// answer triggers a same-session reflection that yields <=2 "lessons", and once a
// batch of questions completes the batch's lessons update the global strategy set.

//go:build ee

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// config mirrors the plan's §11 config JSON. Values are filled from defaults, then
// the -config file (if any), then CLI flags.
type config struct {
	RAGFlowAPIKey string `json:"ragflow_api_key"`
	Backend       struct {
		Scheme string `json:"scheme"`
		Host   string `json:"host"`
		Port   int    `json:"port"`
	} `json:"backend"`

	ChatID      string `json:"chat_id"`
	JudgeChatID string `json:"judge_chat_id"`
	MergeChatID string `json:"merge_chat_id"`

	Reasoning int `json:"reasoning"`

	// UtilityReasoning is the engine for the tool's OWN one-shot calls: the grader
	// (judge) and the merger. 0 = the regular pipeline, which is what the benchmark's
	// grader uses (it sends no "reasoning" at all). They must NOT inherit `reasoning`:
	// both are stateless JSON transformations on their own dialogs, and running them on
	// the agentic engine both wraps a deterministic transform in an agent loop and makes
	// the frozen evaluation use a DIFFERENT grader than the baseline it is compared to.
	// The self-reflection turn is deliberately excluded: it continues the answering
	// thread and must stay on that thread's engine.
	UtilityReasoning int `json:"utility_reasoning"`

	BatchSize        int     `json:"batch_size"`
	MaxPasses        int     `json:"max_passes"` // 0 = unlimited (run to convergence)
	Concurrency      int     `json:"concurrency"`
	TargetAccuracy   float64 `json:"target_accuracy"`
	MinDelta         float64 `json:"min_delta"`
	MinCoverage      float64 `json:"min_coverage"`
	EvalEveryKPasses int     `json:"eval_every_k_passes"`
	Seed             int64   `json:"seed"`

	RequestTimeoutSec int     `json:"request_timeout"`
	RetryInitialSec   float64 `json:"retry_initial_seconds"`
	RetryMaxSec       float64 `json:"retry_max_seconds"`
	RetryJitter       float64 `json:"retry_jitter"`
	// TimeoutReflectSec bounds the ONE post-mortem call made after a test turn exhausted its
	// time budget. The diagnosis deserves a short turn of its own, but it must NOT inherit the
	// answering budget: a model that just spent twenty minutes looping would spend twenty more
	// explaining it.
	TimeoutReflectSec int `json:"timeout_reflect_seconds"`

	// QuotaExhaustedWaitSec is how long to wait after an EXHAUSTED account (Token Plan limit /
	// balance) is detected before the single retry. The provider rarely advertises a reset
	// time, so this is the fallback; after that one retry the question is skipped rather than
	// retried forever.
	QuotaExhaustedWaitSec int `json:"quota_exhausted_wait_seconds"`

	// RetryQuotaSec is the FLAT wait before retrying a MODEL-side temporary failure
	// (high demand / allowance exhausted). MiniMax resets its 5-hour allowance on a
	// sliding window, so 10 min (not 30) reopens a slot sooner.
	RetryQuotaSec int  `json:"retry_quota_seconds"`
	RequireMode8  bool `json:"require_mode8"`

	MaxStrategies    int `json:"max_strategies"`
	MaxLessons       int `json:"max_lessons"`
	MaxStrategyWords int `json:"max_strategy_words"`

	Dataset struct {
		QuestionsPath string `json:"questions_path"`
		CorpusPath    string `json:"corpus_path"`
	} `json:"dataset"`

	BenchmarkOutputsDir string `json:"benchmark_outputs_dir"`
	Trainset            string `json:"trainset"`
	StrategiesFile      string `json:"strategies_file"`
	OutputDir           string `json:"output_dir"`
	Resume              bool   `json:"resume"`
}

func defaultConfig() config {
	c := config{
		ChatID:                "",
		JudgeChatID:           "",
		MergeChatID:           "",
		Reasoning:             8,
		UtilityReasoning:      0, // judge + merge run on the regular pipeline (see the field doc)
		BatchSize:             10,
		MaxPasses:             0,
		Concurrency:           4,
		TargetAccuracy:        100,
		MinDelta:              0.5,
		MinCoverage:           0.9,
		EvalEveryKPasses:      10,
		Seed:                  20261015,
		RequestTimeoutSec:     900,
		RetryInitialSec:       1,
		RetryMaxSec:           60,
		RetryJitter:           0.3,
		RetryQuotaSec:         600,
		TimeoutReflectSec:     180,
		QuotaExhaustedWaitSec: 1800,
		RequireMode8:          true,
		MaxStrategies:         10,
		MaxLessons:            2,
		MaxStrategyWords:      30,
		StrategiesFile:        "strategies.v%04d.txt",
		OutputDir:             "outputs/strategy_rsi",
		Resume:                true,
	}
	c.Backend.Scheme = "http"
	c.Backend.Host = "127.0.0.1"
	c.Backend.Port = 9380
	return c
}

// loadConfig reads the config file (if given), then applies env fallbacks. CLI flags
// are applied by main after this so they win.
func loadConfig(path string) (config, error) {
	c := defaultConfig()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("read config: %w", err)
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return c, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	if c.RAGFlowAPIKey == "" {
		c.RAGFlowAPIKey = os.Getenv("RAGFLOW_API_KEY")
	}
	if c.ChatID == "" {
		c.ChatID = os.Getenv("RAGFLOW_CHAT_ID")
	}
	if v := os.Getenv("RAGFLOW_JUDGE_CHAT_ID"); v != "" && c.JudgeChatID == "" {
		c.JudgeChatID = v
	}
	if c.MergeChatID == "" {
		c.MergeChatID = c.JudgeChatID
	}
	return c, nil
}

func (c config) baseURL() string {
	scheme := c.Backend.Scheme
	if scheme == "" {
		scheme = "http"
	}
	host := c.Backend.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := c.Backend.Port
	if port == 0 {
		port = 9380
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

// quotaExhaustedWait is the single wait allowed before a question is abandoned because the
// account is out of credit.
func (c config) quotaExhaustedWait() time.Duration {
	if c.QuotaExhaustedWaitSec <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(c.QuotaExhaustedWaitSec) * time.Second
}

// timeoutReflectBudget bounds the post-mortem call a timed-out question is allowed.
func (c config) timeoutReflectBudget() time.Duration {
	if c.TimeoutReflectSec <= 0 {
		return 180 * time.Second
	}
	return time.Duration(c.TimeoutReflectSec) * time.Second
}

func (c config) requestTimeout() time.Duration {
	if c.RequestTimeoutSec <= 0 {
		return 900 * time.Second
	}
	return time.Duration(c.RequestTimeoutSec) * time.Second
}

// validate returns an error when the run cannot start.
func (c config) validate() error {
	if strings.TrimSpace(c.RAGFlowAPIKey) == "" {
		return fmt.Errorf("ragflow_api_key (or RAGFLOW_API_KEY) is required")
	}
	if strings.TrimSpace(c.ChatID) == "" {
		return fmt.Errorf("chat_id (or RAGFLOW_CHAT_ID) is required")
	}
	if strings.TrimSpace(c.JudgeChatID) == "" {
		return fmt.Errorf("judge_chat_id is required")
	}
	if c.BatchSize <= 0 {
		return fmt.Errorf("batch_size must be > 0")
	}
	if c.Concurrency <= 0 {
		return fmt.Errorf("concurrency must be > 0")
	}
	if c.MaxStrategies <= 0 {
		return fmt.Errorf("max_strategies must be > 0")
	}
	if c.MaxStrategyWords <= 0 {
		return fmt.Errorf("max_strategy_words must be > 0")
	}
	if c.EvalEveryKPasses <= 0 {
		return fmt.Errorf("eval_every_k_passes must be > 0")
	}
	if strings.TrimSpace(c.Trainset) == "" {
		return fmt.Errorf("trainset is required")
	}
	return nil
}

// strategiesFileFor renders the versioned strategy-file name (e.g. strategies.v0030.txt).
func (c config) strategiesFileFor(version int) string {
	tmpl := c.StrategiesFile
	if tmpl == "" {
		tmpl = "strategies.v%04d.txt"
	}
	if strings.Contains(tmpl, "%") {
		return fmt.Sprintf(tmpl, version)
	}
	return tmpl
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func atoiDefault(s string, d int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return v
	}
	return d
}
