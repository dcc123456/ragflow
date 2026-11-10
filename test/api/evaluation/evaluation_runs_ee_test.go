//go:build integration && ee

// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package evaluation exercises a real API without the parent suite's model
// provisioning. It creates and removes only its own collection/chat fixtures.
package evaluation

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func (c apiClient) draftRun(t *testing.T) (string, string) {
	t.Helper()
	chat := requireOK(t, c.json(t, http.MethodPost, "/api/v1/chats", object{"name": fmt.Sprintf("evaluation_contract_%d", time.Now().UnixNano()), "dataset_ids": []string{}}))
	chatID := id(t, chat)
	t.Cleanup(func() { requireOK(t, c.json(t, http.MethodDelete, "/api/v1/chats/"+chatID, nil)) })
	collectionID := c.collection(t, "cases.csv", []byte("question,reference_answer\nReply with exactly YES.,YES\nReply with exactly NO.,NO\n"))
	run := requireOK(t, c.json(t, http.MethodPost, evaluationPath+"/runs", object{"collection_id": collectionID, "target_id": chatID, "target_type": "chat", "name": "saved configuration"}))
	runID := id(t, run)
	t.Cleanup(func() {
		current := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+runID, nil))
		if current["status"] == "RUNNING" || current["status"] == "PENDING" && current["task_id"] != nil && current["task_id"] != "" {
			requireOK(t, c.json(t, http.MethodPatch, evaluationPath+"/runs/"+runID, object{"status": "CANCEL"}))
		}
	})
	return runID, chatID
}

func TestEvaluationSavingRunLeavesItPendingWithoutResults(t *testing.T) {
	c := client(t)
	runID, _ := c.draftRun(t)
	run := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+runID, nil))
	if run["status"] != "PENDING" || run["task_id"] != nil && run["task_id"] != "" {
		t.Fatalf("saving queued execution: status=%v task_id=%v", run["status"], run["task_id"])
	}
	results := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+runID+"/results", nil))
	if len(items(t, results, "results")) != 0 {
		t.Fatal("saving created execution results")
	}
}

func TestEvaluationDuplicateCopiesConfigurationWithoutResults(t *testing.T) {
	c := client(t)
	runID, _ := c.draftRun(t)
	original := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+runID, nil))
	duplicate := requireOK(t, c.json(t, http.MethodPost, evaluationPath+"/runs", object{"source_run_id": runID, "name": "duplicate"}))
	copyID := id(t, duplicate)
	if copyID == runID {
		t.Fatal("duplicate reused original run ID")
	}
	copy := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+copyID, nil))
	if !reflect.DeepEqual(original["config_snapshot"], copy["config_snapshot"]) || copy["status"] != "PENDING" {
		t.Fatal("duplicate did not preserve a fresh copy of the configuration")
	}
	results := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+copyID+"/results", nil))
	if len(items(t, results, "results")) != 0 {
		t.Fatal("duplicate contains results")
	}
}

func TestEvaluationRejectsUnknownMetricBeforeQueuing(t *testing.T) {
	c := client(t)
	runID, _ := c.draftRun(t)
	requireValidationError(t, c.json(t, http.MethodPost, evaluationPath+"/runs/"+runID+"/executions", object{"mode": "generate_and_score", "metrics": []string{"unknown_metric"}}))
	run := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+runID, nil))
	if run["status"] != "PENDING" || run["task_id"] != nil && run["task_id"] != "" {
		t.Fatal("invalid metric request queued an execution")
	}
}

func TestEvaluationRejectsInvalidPagination(t *testing.T) {
	c := client(t)
	for _, path := range []string{"/collections", "/runs"} {
		t.Run(path, func(t *testing.T) {
			q := url.Values{"page": {"0"}, "page_size": {"-1"}}
			requireValidationError(t, c.json(t, http.MethodGet, evaluationPath+path+"?"+q.Encode(), nil))
		})
	}
}

func (c apiClient) execute(t *testing.T, runID, mode string, caseIDs, metrics []string) []any {
	t.Helper()
	body := object{"mode": mode, "case_ids": caseIDs}
	if metrics != nil {
		body["metrics"] = metrics
	}
	r := c.json(t, http.MethodPost, evaluationPath+"/runs/"+runID+"/executions", body)
	requireOK(t, r)
	if r.status != http.StatusAccepted {
		t.Fatalf("queued execution HTTP=%d, want 202", r.status)
	}
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	for {
		data := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+runID+"/results", nil))
		run := data["run"].(object)
		switch run["status"] {
		case "COMPLETED":
			return items(t, data, "results")
		case "FAILED", "CANCEL":
			t.Fatalf("execution ended in %v: %s", run["status"], strings.ReplaceAll(fmt.Sprint(run["error"]), c.key, "[redacted]"))
		}
		select {
		case <-t.Context().Done():
			t.Fatal("test cancelled while waiting for execution")
		case <-deadline.C:
			t.Fatal("execution did not finish within two minutes")
		case <-poll.C:
		}
	}
}

func resultForCase(t *testing.T, rows []any, caseID string) object {
	t.Helper()
	for _, row := range rows {
		result := row.(object)
		if result["case_id"] == caseID {
			return result
		}
	}
	t.Fatal("execution result missing selected case")
	return nil
}

func (c apiClient) runCases(t *testing.T, runID string) []any {
	t.Helper()
	run := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+runID, nil))
	return c.cases(t, run["collection_id"].(string))
}

func TestEvaluationSelectedGenerationRetainsUnselectedResult(t *testing.T) {
	c := client(t)
	runID, _ := c.draftRun(t)
	cases := c.runCases(t, runID)
	first, second := id(t, cases[0].(object)), id(t, cases[1].(object))
	initial := c.execute(t, runID, "generate", []string{first}, nil)
	if len(initial) != 1 {
		t.Fatal("selected generation executed unselected cases")
	}
	saved := resultForCase(t, initial, first)
	if strings.TrimSpace(fmt.Sprint(saved["generated_answer"])) == "" {
		t.Fatal("generation did not persist an answer")
	}
	if metrics, ok := saved["metrics"].(object); ok && len(metrics) != 0 {
		t.Fatal("generation-only execution computed metrics")
	}
	updated := c.execute(t, runID, "generate", []string{second}, nil)
	if len(updated) != 2 || !reflect.DeepEqual(saved, resultForCase(t, updated, first)) {
		t.Fatal("selected generation changed the unselected case result")
	}
}

func TestEvaluationScoreOnlyPreservesGeneratedAnswer(t *testing.T) {
	c := client(t)
	runID, _ := c.draftRun(t)
	caseID := id(t, c.runCases(t, runID)[0].(object))
	generated := resultForCase(t, c.execute(t, runID, "generate", []string{caseID}, nil), caseID)
	scored := resultForCase(t, c.execute(t, runID, "score", []string{caseID}, []string{"semantic_similarity"}), caseID)
	if generated["generated_answer"] != scored["generated_answer"] || !reflect.DeepEqual(generated["retrieved_chunks"], scored["retrieved_chunks"]) {
		t.Fatal("score-only execution regenerated its answer or retrieval")
	}
	metrics := scored["metrics"].(object)
	score, ok := metrics["semantic_similarity"].(float64)
	if !ok || score < 0 || score > 1 {
		t.Fatal("judge did not return a normalized numeric score")
	}
	if _, ok := metrics["semantic_similarity_reason"].(string); !ok {
		t.Fatal("judge explanation was not persisted")
	}
}

func TestEvaluationExecutionDoesNotModifyChatOrCreateSessions(t *testing.T) {
	c := client(t)
	runID, chatID := c.draftRun(t)
	chatPath := "/api/v1/chats/" + chatID
	before := requireOK(t, c.json(t, http.MethodGet, chatPath, nil))
	sessionsBefore := requireOK(t, c.json(t, http.MethodGet, chatPath+"/sessions", nil))
	caseID := id(t, c.runCases(t, runID)[0].(object))
	c.execute(t, runID, "generate", []string{caseID}, nil)
	after := requireOK(t, c.json(t, http.MethodGet, chatPath, nil))
	sessionsAfter := requireOK(t, c.json(t, http.MethodGet, chatPath+"/sessions", nil))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("evaluation changed the saved chat")
	}
	if !reflect.DeepEqual(sessionsBefore, sessionsAfter) {
		t.Fatal("evaluation created persistent chat sessions")
	}
}

func TestEvaluationClearsInvalidateScoresAndSummary(t *testing.T) {
	c := client(t)
	runID, _ := c.draftRun(t)
	caseID := id(t, c.runCases(t, runID)[0].(object))
	c.execute(t, runID, "generate_and_score", []string{caseID}, nil)
	base := evaluationPath + "/runs/" + runID + "/results/" + caseID
	requireOK(t, c.json(t, http.MethodDelete, base+"/metrics/faithfulness", nil))
	data := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+runID+"/results", nil))
	result := resultForCase(t, items(t, data, "results"), caseID)
	metrics := result["metrics"].(object)
	if _, ok := metrics["faithfulness"]; ok {
		t.Fatal("cleared metric survived")
	}
	if _, ok := metrics["faithfulness_reason"]; ok {
		t.Fatal("cleared metric explanation survived")
	}
	if _, ok := data["run"].(object)["metrics_summary"].(object)["faithfulness"]; ok {
		t.Fatal("cleared metric summary survived")
	}
	requireOK(t, c.json(t, http.MethodDelete, base+"/answer", nil))
	data = requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+runID+"/results", nil))
	result = resultForCase(t, items(t, data, "results"), caseID)
	if result["generated_answer"] != "" || len(result["metrics"].(object)) != 0 {
		t.Fatal("cleared answer retained stale scores")
	}
	requireValidationError(t, c.json(t, http.MethodPost, evaluationPath+"/runs/"+runID+"/executions", object{"mode": "score", "case_ids": []string{caseID}}))
}

func TestEvaluationDuplicateExecutionAndCancellation(t *testing.T) {
	c := client(t)
	runID, _ := c.draftRun(t)
	path := evaluationPath + "/runs/" + runID
	first := c.json(t, http.MethodPost, path+"/executions", object{"mode": "generate_and_score"})
	requireOK(t, first)
	second := c.json(t, http.MethodPost, path+"/executions", object{"mode": "generate_and_score"})
	if second.status != http.StatusConflict || second.code == nil || *second.code == 0 {
		t.Fatalf("duplicate execution accepted: HTTP=%d", second.status)
	}
	requireOK(t, c.json(t, http.MethodPatch, path, object{"status": "CANCEL"}))
	before := requireOK(t, c.json(t, http.MethodGet, path+"/results", nil))
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal("cancelled test")
	}
	after := requireOK(t, c.json(t, http.MethodGet, path+"/results", nil))
	if after["run"].(object)["status"] != "CANCEL" || !reflect.DeepEqual(before["results"], after["results"]) {
		t.Fatal("cancelled worker overwrote persisted results")
	}
}

func TestEvaluationRetrievalUsesChatPipelineAndScoresRelevantDocuments(t *testing.T) {
	c := client(t)
	dataset := requireOK(t, c.json(t, http.MethodPost, "/api/v1/datasets", object{"name": fmt.Sprintf("evaluation_retrieval_%d", time.Now().UnixNano()), "parse_type": 1, "parser_id": "general"}))
	datasetID := id(t, dataset)
	t.Cleanup(func() {
		requireOK(t, c.json(t, http.MethodDelete, "/api/v1/datasets", object{"ids": []string{datasetID}}))
	})
	document := requireOK(t, c.json(t, http.MethodPost, "/api/v1/datasets/"+datasetID+"/documents?type=empty", object{"name": "evaluation-herb.txt"}))
	docID := id(t, document)
	requireOK(t, c.json(t, http.MethodPost, "/api/v1/datasets/"+datasetID+"/documents/"+docID+"/chunks", object{"content": "Coriander botanical archive records: the herb in this archive is coriander."}))
	deadline := time.Now().Add(30 * time.Second)
	for {
		retrieval := requireOK(t, c.json(t, http.MethodPost, "/api/v1/retrieval", object{"dataset_ids": []string{datasetID}, "question": "coriander botanical archive", "similarity_threshold": 0, "vector_similarity_weight": 0, "include_knowledge_compilation": false}))
		if len(items(t, retrieval, "chunks")) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("new chunk did not become retrievable")
		}
		time.Sleep(250 * time.Millisecond)
	}
	chat := requireOK(t, c.json(t, http.MethodPost, "/api/v1/chats", object{
		"name": fmt.Sprintf("evaluation_retrieval_chat_%d", time.Now().UnixNano()), "dataset_ids": []string{datasetID},
		"similarity_threshold": 0, "vector_similarity_weight": 0,
		"prompt_config": object{"system": "Answer using this knowledge: {knowledge}", "parameters": []object{{"key": "knowledge", "optional": false}}, "empty_response": "", "keyword": false, "quote": true, "tts": false, "refine_multiturn": false},
	}))
	chatID := id(t, chat)
	t.Cleanup(func() { requireOK(t, c.json(t, http.MethodDelete, "/api/v1/chats/"+chatID, nil)) })
	collectionID := c.collection(t, "retrieval.csv", []byte("question,reference_answer\nWhat herb does the coriander botanical archive record?,coriander\n"))
	caseID := id(t, c.cases(t, collectionID)[0].(object))
	requireOK(t, c.json(t, http.MethodPatch, evaluationPath+"/collections/"+collectionID+"/cases/"+caseID, object{"relevant_doc_ids": []string{docID}}))
	run := requireOK(t, c.json(t, http.MethodPost, evaluationPath+"/runs", object{"collection_id": collectionID, "target_id": chatID, "target_type": "chat"}))
	t.Cleanup(func() {
		current := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/runs/"+id(t, run), nil))
		if current["status"] == "RUNNING" || current["status"] == "PENDING" && current["task_id"] != "" {
			requireOK(t, c.json(t, http.MethodPatch, evaluationPath+"/runs/"+id(t, run), object{"status": "CANCEL"}))
		}
	})
	result := resultForCase(t, c.execute(t, id(t, run), "generate_and_score", []string{caseID}, []string{}), caseID)
	references := result["retrieved_chunks"].(object)
	chunks := items(t, references, "chunks")
	if len(chunks) == 0 || chunks[0].(object)["document_id"] != docID {
		t.Fatalf("evaluation did not persist the chat pipeline's retrieved document: references=%v metrics=%v", references, result["metrics"])
	}
	metrics := result["metrics"].(object)
	for _, metric := range []string{"precision", "recall", "f1_score", "hit_rate", "mrr"} {
		if metrics[metric] != float64(1) {
			t.Fatalf("%s=%v, want 1 for the sole relevant retrieved document", metric, metrics[metric])
		}
	}
}
