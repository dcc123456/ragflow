//go:build ee

package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"ragflow/internal/entity"
	kccommon "ragflow/internal/ingestion/component/knowledge_compiler/common"
)

func TestEvaluationBlueScoreSourceGolden(t *testing.T) {
	for _, test := range []struct {
		candidate, reference string
		want                 float64
	}{
		{"The cat sat", "the cat sat", 1},
		{"cat cat cat", "cat dog", 1.0 / 3},
		{"cat", "cat dog", math.Exp(-1)},
		{"你好，世界！", "你好 世界", 1},
		{"ÉCOLE ②", "école ②", 1},
		{"!!!", "cat", 0},
		{"cat", "", 0},
	} {
		t.Run(test.candidate+"/"+test.reference, func(t *testing.T) {
			if got := computeEvaluationBlueScore(test.candidate, test.reference); math.Abs(got-test.want) > 1e-12 {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}

func TestEvaluationDeterministicMetrics(t *testing.T) {
	input := EvaluationMetricInput{
		Answer: "中文🙂", ReferenceAnswer: "中文🙂", RelevantDocIDs: []string{"a", "b", "b"},
		References: entity.JSONMap{"chunks": []any{
			map[string]any{"doc_id": "x"}, map[string]any{"doc_id": "a"}, map[string]any{"doc_id": "a"},
		}},
	}
	metrics := ComputeEvaluationMetrics(input)
	for key, want := range map[string]float64{"precision": .5, "recall": .5, "f1_score": .5, "hit_rate": 1, "mrr": .5, "has_answer": 1, "blue_score": 1} {
		if metrics[key] != want {
			t.Errorf("%s = %v, want %v", key, metrics[key], want)
		}
	}
	if metrics["answer_length"] != 3 {
		t.Errorf("answer length = %v, want Unicode character count 3", metrics["answer_length"])
	}
	if metrics := ComputeEvaluationMetrics(EvaluationMetricInput{}); len(metrics) != 0 {
		t.Fatalf("empty input metrics: %v", metrics)
	}
	input.References = entity.JSONMap{"chunks": []any{
		map[string]any{"document_id": "x"}, map[string]any{"document_id": "a"}, map[string]any{"document_id": "a"},
	}}
	if got := ComputeEvaluationMetrics(input); got["recall"] != .5 || got["mrr"] != .5 {
		t.Fatalf("normalized chat references lost retrieval scores: %v", got)
	}
	metrics = ComputeEvaluationMetrics(EvaluationMetricInput{Answer: "  "})
	if metrics["has_answer"] != 0.0 || metrics["answer_length"] != 2 {
		t.Fatalf("whitespace metrics: %v", metrics)
	}
}

func TestEvaluationJudgeContextBounds(t *testing.T) {
	chunks := []any{map[string]any{"content_ltks": "( hello ) world !"}}
	if got := buildEvaluationContext(entity.JSONMap{"chunks": chunks}); got != "(hello) world!" {
		t.Fatalf("detokenized context = %q", got)
	}
	chunks = nil
	for range 7 {
		chunks = append(chunks, map[string]any{"content": strings.Repeat("中", 1100)})
	}
	contextText := buildEvaluationContext(entity.JSONMap{"chunks": chunks})
	if got := len([]rune(contextText)); got != 6*1024+10 {
		t.Fatalf("context characters = %d", got)
	}
	if got := truncateEvaluationText("  中文🙂  ", 2); got != "中文" {
		t.Fatalf("truncation = %q", got)
	}
}

type evaluationJudgeFake struct {
	requests []kccommon.ChatRequest
	response string
	err      error
}

func (f *evaluationJudgeFake) Chat(_ context.Context, req kccommon.ChatRequest) (*kccommon.ChatResponse, error) {
	f.requests = append(f.requests, req)
	return &kccommon.ChatResponse{Content: f.response}, f.err
}

func TestEvaluationJudgePromptAndNormalization(t *testing.T) {
	judge := &evaluationJudgeFake{response: `{"score": 1.5, "reason":"supported"}`}
	input := EvaluationMetricInput{Question: strings.Repeat("问", 300), Answer: strings.Repeat("答", 600)}
	metrics, err := judgeEvaluationMetrics(t.Context(), judge, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(judge.requests) != 3 {
		t.Fatalf("judge calls = %d", len(judge.requests))
	}
	for _, name := range evaluationMetricNames {
		if metrics[name] != 1.0 || metrics[name+"_reason"] != "supported" {
			t.Errorf("%s metric = %v", name, metrics)
		}
	}
	faithfulness := judge.requests[0]
	if !strings.Contains(faithfulness.UserPrompt, strings.Repeat("问", 256)) ||
		strings.Contains(faithfulness.UserPrompt, strings.Repeat("问", 257)) ||
		strings.Contains(faithfulness.UserPrompt, strings.Repeat("答", 513)) ||
		!strings.Contains(faithfulness.UserPrompt, "Context:\nNone") {
		t.Fatalf("judge input limits or empty context violated")
	}
	if !faithfulness.JSONMode || !faithfulness.DisableRetry {
		t.Fatalf("JSON transport options missing")
	}
	if !strings.Contains(judge.requests[2].UserPrompt, "Reference Answer:\nNone") {
		t.Fatalf("missing reference default")
	}
}

func TestEvaluationJudgeSelectedAndInvalidResponses(t *testing.T) {
	input := EvaluationMetricInput{Question: "q", Answer: "a"}
	judge := &evaluationJudgeFake{response: `{"score":"-1","reason":""}`}
	metrics, err := judgeEvaluationMetrics(t.Context(), judge, input, []string{"context_relevance", "context_relevance"})
	if err != nil || len(judge.requests) != 1 || metrics["context_relevance"] != 0.0 {
		t.Fatalf("selected metrics=%v calls=%d err=%v", metrics, len(judge.requests), err)
	}
	for _, response := range []string{`{}`, `{"score":"NaN"}`, `{"score":true}`, `{"score":0.5,"reason":{}}`} {
		judge := &evaluationJudgeFake{response: response}
		if _, err := judgeEvaluationMetrics(t.Context(), judge, input, []string{"faithfulness"}); err == nil {
			t.Errorf("accepted invalid judge result %s", response)
		}
	}
	judge = &evaluationJudgeFake{err: errors.New("provider failed")}
	if _, err := judgeEvaluationMetrics(t.Context(), judge, input, []string{"faithfulness"}); err == nil {
		t.Fatal("provider error discarded")
	}
	if _, err := judgeEvaluationMetrics(t.Context(), judge, input, []string{"made_up"}); err == nil {
		t.Fatal("unknown metric accepted")
	}
	judge = &evaluationJudgeFake{response: `not json`}
	ctx := kccommon.WithRetryDelay(t.Context(), time.Nanosecond)
	if _, err := judgeEvaluationMetrics(ctx, judge, input, []string{"faithfulness"}); err == nil || len(judge.requests) != 2 {
		t.Fatalf("malformed JSON attempts = %d, err = %v", len(judge.requests), err)
	}
}

func TestEvaluationDisabledMetricsDoNotResolveModels(t *testing.T) {
	metrics, err := EvaluateEvaluationMetrics(t.Context(), "tenant",
		entity.JSONMap{"faithfulness": entity.JSONMap{"enable": false}},
		&entity.EvaluationCase{Variable: entity.JSONMap{"question": "q"}},
		&entity.EvaluationResult{GeneratedAnswer: "a"}, []string{"faithfulness"})
	if err != nil || len(metrics) != 0 {
		t.Fatalf("disabled metrics = %v, err = %v", metrics, err)
	}
}

func TestEvaluationSummaryAvailableScoresOnly(t *testing.T) {
	results := []entity.EvaluationResult{
		{Metrics: entity.JSONMap{"faithfulness": .5, "faithfulness_reason": "a", "bad": math.NaN()}, ExecutionTime: 2},
		{Metrics: entity.JSONMap{"faithfulness": 1.0, "blue_score": .8}, ExecutionTime: 4},
		{Metrics: entity.JSONMap{}, ExecutionTime: -1},
	}
	summary := ComputeEvaluationSummary(results)
	for key, want := range map[string]float64{"faithfulness": .75, "blue_score": .8, "execution_time": 3} {
		if got := summary[key].(entity.JSONMap)["summary"]; got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	if len(summary) != 3 {
		t.Fatalf("unexpected summaries: %v", summary)
	}
}
