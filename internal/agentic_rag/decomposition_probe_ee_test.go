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

import "github.com/cloudwego/eino/schema"

// Independent stage-one probe: run ONLY the plan stage -
// planner, check_decomposition, and the review round - against a live model.
// No explorer, no auditor, no corpus, no retrieval: the plan and the review's
// verdict lines are the entire output, so decomposition quality (clause
// coverage, root richness, pair clauses) can be iterated on without paying for
// an end-to-end smoke run.
//
// Guarded by DECOMP_PROBE=1: ordinary `go test ./...` never reaches the wire.
//
//	cd internal/agentic_rag && \
//	AGENTIC_RAG_CONFIG=../../conf/agentic_rag.yaml \
//	DECOMP_PROBE=1 \
//	DECOMP_API_KEY="<provider key>" \
//	DECOMP_TEMPERATURE=0.5 \
//	DECOMP_QUESTION="Two individuals from different industries ..." \
//	go test -run TestPlanStageProbe -v -timeout 12m
//
// Environment:
//
//	AGENTIC_RAG_CONFIG   required in practice: `go test` runs in the package
//	                     directory, where the default relative config path
//	                     (conf/agentic_rag.yaml) does not resolve - point it
//	                     at ../../conf/agentic_rag.yaml.
//	DECOMP_QUESTION      one question, or:
//	DECOMP_QUESTIONS_FILE a file with one question per line.
//	DECOMP_PROVIDER      provider name as spelled in conf/models/*.json
//	                     (default "MiniMax"). The driver, base URL and URL
//	                     suffix come from that provider's own file via
//	                     InitProviderManager - nothing is hardcoded here.
//	DECOMP_API_KEY       the provider key (for MiniMax: the tenant_model_instance
//	                     row's api_key in MySQL - the API never returns keys).
//	DECOMP_BASE_URL      optional override of the provider file's default URL
//	                     (e.g. the global-region key needs the .io endpoint).
//	DECOMP_MODEL         model name (default "MiniMax-M3").
//	DECOMP_TEMPERATURE   sampling temperature (default 0.5, the benchmark's).

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity/models"
)

func TestPlanStageProbe(t *testing.T) {
	if os.Getenv("DECOMP_PROBE") != "1" {
		t.Skip("stage-one live probe: set DECOMP_PROBE=1 to run")
	}
	// The probe is where decomposition is debugged: the pipeline's logger is
	// nil in a test binary until initialized, and WITHOUT it every
	// common.InfoCtx/DebugCtx - the plan auditor's output, its findings, the
	// verification lines - is a silent no-op. Debug level, straight to the
	// test log.
	if err := common.InitLogger("debug", common.FileOutput{}, "decomp-probe"); err != nil {
		t.Fatalf("init logger: %v", err)
	}
	apiKey := os.Getenv("DECOMP_API_KEY")
	if apiKey == "" {
		t.Fatal("DECOMP_API_KEY is required")
	}
	questions := probeQuestions(t)

	providerName := envOr("DECOMP_PROVIDER", "MiniMax")
	driver := probeDriver(t, providerName)
	modelName := envOr("DECOMP_MODEL", "MiniMax-M3")
	temp := 0.5
	if s := os.Getenv("DECOMP_TEMPERATURE"); s != "" {
		temp = parseProbeFloat(t, s)
	}

	name := modelName
	apiCfg := &models.APIConfig{ApiKey: &apiKey}
	if override := os.Getenv("DECOMP_BASE_URL"); override != "" {
		apiCfg.BaseURL = &override
	}
	eino := models.NewEinoChatModel(models.NewChatModel(driver, &name, apiCfg),
		&models.ChatConfig{Temperature: &temp})

	for i, question := range questions {
		question = strings.TrimSpace(question)
		if question == "" {
			continue
		}
		t.Run(qLabel(i, question), func(t *testing.T) {
			// The audit loop is deliberately generous - up to eight
			// audit-driven repair rounds, each two cheap turns - so the
			// per-question budget is sized for the loop, not for a single
			// write. 20m covers the full budget with headroom; a provider
			// outage still fails fast (failover cooldown).
			perQuestion := 20 * time.Minute
			if s := os.Getenv("DECOMP_TIMEOUT"); s != "" {
				d, err := time.ParseDuration(s)
				if err != nil {
					t.Fatalf("DECOMP_TIMEOUT: %v", err)
				}
				perQuestion = d
			}
			ctx, cancel := context.WithTimeout(context.Background(), perQuestion)
			defer cancel()
			in := Input{
				Model:             eino,
				TenantID:          "probe",
				Messages:          []*schema.Message{schema.UserMessage(question)},
				ToolCallCounts:    map[string]int{},
				ToolCallDurations: NewDurationAccumulator(),
			}
			plan, conversational, psErr := runPlanStage(ctx, in)
			if psErr != nil {
				t.Fatalf("plan stage error: %v", psErr)
			}
			if conversational {
				t.Logf("stage one ruled the message conversational; no plan")
				return
			}
			if plan == "" {
				t.Fatal("stage one returned no plan (check the server-side warnings above)")
			}
			t.Logf("PLAN (%d bytes):\n%s", len(plan), plan)
			if findings := checkDecomposition(plan); len(findings) > 0 {
				t.Errorf("plan carries %d mechanical findings:", len(findings))
				for _, f := range findings {
					t.Errorf("  - %s", f)
				}
			} else {
				t.Logf("mechanical check: clean")
			}
		})
	}
}

// probeDriver resolves the provider by name through the provider manager -
// the same conf/models/*.json files the server loads - so the driver, its
// base URLs and the URL suffixes are the provider file's own bytes, never
// duplicated here.
func probeDriver(t *testing.T, providerName string) models.ModelDriver {
	t.Helper()
	dir := envOr("DECOMP_MODELS_DIR", "../../conf/models")
	if err := models.InitProviderManager(dir); err != nil {
		t.Fatalf("InitProviderManager(%s): %v", dir, err)
	}
	pm := models.GetProviderManager()
	for i := range pm.Providers {
		p := &pm.Providers[i]
		if strings.EqualFold(p.Name, providerName) {
			if p.ModelDriver == nil {
				t.Fatalf("provider %s has no driver", providerName)
			}
			return p.ModelDriver
		}
	}
	t.Fatalf("provider %q not found in %s", providerName, dir)
	return nil
}

// probeQuestions assembles the probe's questions: DECOMP_QUESTION first, then
// every non-empty line of DECOMP_QUESTIONS_FILE.
func probeQuestions(t *testing.T) []string {
	t.Helper()
	var questions []string
	if q := os.Getenv("DECOMP_QUESTION"); q != "" {
		questions = append(questions, q)
	}
	if path := os.Getenv("DECOMP_QUESTIONS_FILE"); path != "" {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("DECOMP_QUESTIONS_FILE: %v", err)
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if q := strings.TrimSpace(scanner.Text()); q != "" {
				questions = append(questions, q)
			}
		}
	}
	if len(questions) == 0 {
		t.Fatal("set DECOMP_QUESTION or DECOMP_QUESTIONS_FILE")
	}
	return questions
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseProbeFloat(t *testing.T, s string) float64 {
	t.Helper()
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("DECOMP_TEMPERATURE: %v", err)
	}
	return f
}

func qLabel(i int, question string) string {
	r := []rune(question)
	if len(r) > 40 {
		r = r[:40]
	}
	return strings.Join([]string{"q#", strconv.Itoa(i + 1), ":", string(r)}, " ")
}
