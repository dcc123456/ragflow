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

//go:build ee

package agentic_rag

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"ragflow/internal/common"
)

// defaultIterativeRounds is the loop cap when neither the caller nor the config names
// one. It matches the shipped conf/iterative_synthesis.yaml and IterSynth's own
// `max_search_rounds = 30`.
const defaultIterativeRounds = 30

// iterativeTemplate is one role's configuration: the prompt it runs on and, when the
// operator pins one, its sampling temperature. One struct for every role rather than
// one type per role — the roles differ in what their prompt says, not in what the
// engine needs to know about them.
type iterativeTemplate struct {
	Temperature *float64 `yaml:"temperature"`
	Content     string   `yaml:"content"`
}

// iterativeRubricConfig is the online quality gate: its switch, its budget, and the
// prompt of the scoring call it makes.
//
// Enabled is a pointer so "absent" and "false" differ: an operator who omits the
// block gets the shipped default (on), while one who writes `enabled: false` gets a
// deterministic two-call loop with no judge traffic.
type iterativeRubricConfig struct {
	Enabled     *bool   `yaml:"enabled"`
	EveryRounds int     `yaml:"every_rounds"`
	Threshold   float64 `yaml:"threshold"`
	MaxRewrites int     `yaml:"max_rewrites"`
	// Temperature belongs to the scoring call. Its verdict is machine-parsed and
	// decides whether an extra rewrite is spent, so the shipped config pins it at 0.
	Temperature *float64 `yaml:"temperature"`
	Content     string   `yaml:"content"`
}

// iterativeConfig is the on-disk shape of conf/iterative_synthesis.yaml. It is
// separate from conf/agentic_rag.yaml on purpose: a Template there is one prompt and
// one tool list, while mode 6 is one loop over four role prompts plus a scoring
// prompt, and folding the latter into the former would give every mode-5 template
// five fields it cannot use.
type iterativeConfig struct {
	MaxSearchRounds int                   `yaml:"max_search_rounds"`
	Tools           []string              `yaml:"tools"`
	Rubric          iterativeRubricConfig `yaml:"rubric"`
	Planner         iterativeTemplate     `yaml:"planner"`
	Synthesizer     iterativeTemplate     `yaml:"synthesizer"`
	Commit          iterativeTemplate     `yaml:"commit"`
	Rewrite         iterativeTemplate     `yaml:"rewrite"`
}

// rubricEnabled reports whether the online gate runs. Absent means enabled: the
// rubric is the training-free half of this mode's benefit (inspiration 6), so an
// operator who wants it off has to say so.
func (c *iterativeConfig) rubricEnabled() bool {
	if c == nil || c.Rubric.Enabled == nil {
		return true
	}
	return *c.Rubric.Enabled
}

// applyDefaults fills the knobs a hand-written config may omit, so a partial file is
// a valid file. Prompt content is deliberately NOT defaulted: an empty role prompt is
// a misconfiguration and validate rejects it, because a silently-substituted embedded
// prompt is a different product from the one the operator edited.
func (c *iterativeConfig) applyDefaults() {
	if c.MaxSearchRounds <= 0 {
		c.MaxSearchRounds = defaultIterativeRounds
	}
	if c.Rubric.EveryRounds <= 0 {
		c.Rubric.EveryRounds = 1
	}
	if c.Rubric.Threshold <= 0 {
		c.Rubric.Threshold = 3.0
	}
	if c.Rubric.MaxRewrites <= 0 {
		c.Rubric.MaxRewrites = 5
	}
}

// validate rejects a config the engine cannot run. Each message names the role, so a
// typo in the YAML surfaces as "role \"planner\" has no content" rather than as a run
// that searches forever on an empty instruction. The rubric prompt is only required
// when the gate is on — a disabled gate is never invoked.
func (c *iterativeConfig) validate() error {
	roles := []struct {
		name     string
		content  string
		required bool
	}{
		{"planner", c.Planner.Content, true},
		{"synthesizer", c.Synthesizer.Content, true},
		{"commit", c.Commit.Content, true},
		{"rubric", c.Rubric.Content, c.rubricEnabled()},
		{"rewrite", c.Rewrite.Content, c.rubricEnabled()},
	}
	for _, r := range roles {
		if r.required && len(r.content) == 0 {
			return fmt.Errorf("iterative_synthesis config: role %q has no content", r.name)
		}
	}
	return nil
}

// iterativeConfigPath is the on-disk location of mode 6's config. It can be overridden
// with ITERATIVE_SYNTHESIS_CONFIG (e.g. to point at a scratch file while iterating on
// benchmarks) — the same escape hatch AGENTIC_RAG_CONFIG provides.
func iterativeConfigPath() string {
	if p := os.Getenv("ITERATIVE_SYNTHESIS_CONFIG"); p != "" {
		return p
	}
	return "conf/iterative_synthesis.yaml"
}

var (
	iterativeCfgMu       sync.Mutex
	cachedIterativeCfg   *iterativeConfig
	cachedIterativeMTime time.Time
	cachedIterativePath  string
)

// loadIterativeConfig reads and validates mode 6's config, reloading it when the file
// changes so prompt edits take effect without a restart.
//
// Unlike the mode-5 template loader there is NO last-good or embedded fallback: mode
// 6's prompts ARE the engine, and a missing file would otherwise run an empty Planner
// and a Synthesizer instructed by nothing. A misconfigured mode 6 must fail loudly,
// which is why the error is returned rather than logged and swallowed.
func loadIterativeConfig() (*iterativeConfig, error) {
	path := iterativeConfigPath()
	info, statErr := os.Stat(path)

	iterativeCfgMu.Lock()
	defer iterativeCfgMu.Unlock()

	if statErr == nil && cachedIterativeCfg != nil &&
		filepath.Clean(path) == cachedIterativePath &&
		!info.ModTime().After(cachedIterativeMTime) {
		return cachedIterativeCfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg iterativeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()

	cachedIterativeCfg = &cfg
	cachedIterativePath = filepath.Clean(path)
	if statErr == nil {
		cachedIterativeMTime = info.ModTime()
	}
	common.Info("iterative_rag: config (re)loaded",
		zap.String("path", path),
		zap.Int("max_search_rounds", cfg.MaxSearchRounds),
		zap.Int("tools", len(cfg.Tools)),
		zap.Bool("rubric", cfg.rubricEnabled()))
	return &cfg, nil
}

// IterativeRoleTemperatures carries the sampling each of mode 6's three model
// instances is pinned to. Any field may be nil, meaning the role declares no
// temperature and the model's own default applies.
type IterativeRoleTemperatures struct {
	// Planner pins the tool-bound producer instance (the Planner).
	Planner *float64
	// Synthesizer pins the instance the Synthesizer, commit and rewrite passes share:
	// they are synthesis-shaped, so they are one role for sampling purposes.
	Synthesizer *float64
	// Rubric pins the judge instance: its verdict is machine-parsed and decides
	// whether an extra rewrite is spent, so it must not inherit the producer's noise.
	Rubric *float64
}

// IterativeTemperatures resolves mode 6's per-role sampling. Exported for the service
// layer, which builds one model instance per pinned role the way agenticRag builds its
// producer / auditor / plan-auditor instances.
//
// A missing or invalid config yields all-nil: the caller then runs every role on the
// model's own default rather than failing the turn on a sampling preference —
// RunIterative itself is where a missing config becomes a hard error, and it says why.
func IterativeTemperatures() IterativeRoleTemperatures {
	cfg, err := loadIterativeConfig()
	if err != nil {
		return IterativeRoleTemperatures{}
	}
	return IterativeRoleTemperatures{
		Planner:     cfg.Planner.Temperature,
		Synthesizer: cfg.Synthesizer.Temperature,
		Rubric:      cfg.Rubric.Temperature,
	}
}
