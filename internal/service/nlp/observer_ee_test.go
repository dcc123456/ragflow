//go:build ee

package nlp

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"ragflow/internal/entity/models"
	"ragflow/internal/observability"
)

func retrievalMetricCounts(t *testing.T) map[string]float64 {
	t.Helper()
	recorder := httptest.NewRecorder()
	observability.MetricsHandler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	counts := make(map[string]float64)
	for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
		name, _, ok := strings.Cut(line, "{")
		if !ok || (!strings.HasSuffix(name, "_requests_total") && !strings.HasSuffix(name, "_failures_total")) {
			continue
		}
		fields := strings.Fields(line)
		value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			t.Fatal(err)
		}
		counts[name] = value
	}
	return counts
}

func TestRetrievalObserverCountsServiceOperations(t *testing.T) {
	before := retrievalMetricCounts(t)
	svc := &RetrievalService{}
	if _, err := svc.Retrieval(t.Context(), &RetrievalRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Retrieval(t.Context(), &RetrievalRequest{
		Question: "query", Page: 2, PageSize: 1, RerankCandidatesCount: new(1),
	}); err == nil {
		t.Fatal("invalid retrieval pagination was accepted")
	}
	t.Setenv("TOKENIZER_EMBEDDING_TOKENIZER", "")
	t.Setenv("TOKENIZER_EMBEDDING_MAX_TOKENS", "8192")
	key := t.Name()
	model := models.NewEmbeddingModel(&emptyEmbeddingResponseDriver{}, nil, &models.APIConfig{ApiKey: &key}, 8192)
	if _, err := svc.GetVector(t.Context(), "query", model, 10, 10, .1); err == nil {
		t.Fatal("invalid embedding response was accepted")
	}
	if _, _, _, err := RerankByModel(t.Context(), nil, nil, nil, nil, "query", .3, .7, "content_ltks", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	after := retrievalMetricCounts(t)
	for name, want := range map[string]float64{
		"retrieval_requests_total":           2,
		"retrieval_failures_total":           1,
		"retrieval_embedding_requests_total": 1,
		"retrieval_embedding_failures_total": 1,
		"retrieval_rerank_requests_total":    1,
		"retrieval_rerank_failures_total":    0,
	} {
		if got := after[name] - before[name]; got != want {
			t.Errorf("%s changed by %v, want %v", name, got, want)
		}
	}
}
