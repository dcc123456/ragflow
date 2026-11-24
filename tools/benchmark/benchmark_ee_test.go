//go:build ee

package main

// Parity tests against the Python original's behaviour (the port's
// acceptance bar: identical verdicts and metrics on the same inputs).

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestParseGraderVerdict(t *testing.T) {
	text := "**extracted_final_answer:** The Compassionate Pugilist\n\n**correct:** yes\n\n**confidence:** 100%"
	verdict, err := parseGraderVerdict(text)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if verdict["correct"] != true {
		t.Errorf("correct = %v, want true", verdict["correct"])
	}
	if s, _ := verdict["extracted_final_answer"].(string); s != "The Compassionate Pugilist" {
		t.Errorf("extracted = %q", s)
	}
	if _, err := parseGraderVerdict("no verdict here"); err == nil {
		t.Error("expected an error when correct is missing")
	}
}

func TestParseGraderVerdictRealShape(t *testing.T) {
	text := "extracted_final_answer: Richard Larson\n\n[correct_answer]: Richard C. Larson\n\n" +
		"reasoning: The extracted answer names the same person; Larson is an acceptable short form.\n\n" +
		"correct: yes\n\nconfidence: 90%"
	verdict, err := parseGraderVerdict(text)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if verdict["correct"] != true {
		t.Errorf("correct = %v", verdict["correct"])
	}
	if s, _ := verdict["extracted_final_answer"].(string); s != "Richard Larson" {
		t.Errorf("extracted = %q", s)
	}
	if f, _ := verdict["confidence"].(float64); f != 90 {
		t.Errorf("confidence = %v", f)
	}
}

func TestCalibrationErrorStable(t *testing.T) {
	n := 830
	confidences := make([]float64, n)
	correctness := make([]bool, n)
	for i := range correctness {
		correctness[i] = i%4 != 0
	}
	first := calibrationError(confidences, correctness, 100)
	second := calibrationError(confidences, correctness, 100)
	if first != second {
		t.Errorf("unstable: %v vs %v", first, second)
	}
	if first == 0 {
		t.Error("expected a nonzero calibration error for ~75% correct at confidence 100")
	}
}

func TestSelectQuestionsIncludeWithExclude(t *testing.T) {
	questions := []*question{{questionID: "1"}, {questionID: "2"}, {questionID: "3"}}
	selection := map[string]any{"include": []any{"1", "2", "3"}, "exclude": []any{"2"}}
	selected, err := selectQuestions(questions, selection)
	if err != nil {
		t.Fatalf("select error: %v", err)
	}
	if len(selected) != 2 || selected[0].questionID != "1" || selected[1].questionID != "3" {
		t.Errorf("selected = %v", selected)
	}
}

func TestIsPersistentRagflowError(t *testing.T) {
	if !isPersistentRagflowError("minimax API error: input new_sensitive ...") {
		t.Error("content-policy rejection must be persistent")
	}
	if isPersistentRagflowError("已达到 Token Plan 用量上限") {
		t.Error("quota wall must stay transient")
	}
}

// End-to-end parity: rebuild the merged 830-question leaderboard with the Go
// builder and compare the headline metrics with the Python-built file.
func TestBuildLeaderboardMerged830(t *testing.T) {
	const mergedDir = "../../outputs/c2_smoke46_merged_830_20261001"
	answers := readJSONL(mergedDir + "/answers.jsonl")
	if len(answers) == 0 {
		t.Skip("merged run directory not available")
	}
	data, err := os.ReadFile(mergedDir + "/leaderboard.json")
	if err != nil {
		t.Skip("merged leaderboard not available")
	}
	var pyLeaderboard map[string]any
	json.Unmarshal(data, &pyLeaderboard)
	judgements := map[string]map[string]any{}
	for _, recordAny := range pyLeaderboard["per_query_judgements"].([]any) {
		record := recordAny.(map[string]any)
		judgements[record["run_key"].(string)] = record
	}
	cfg, err := loadJSONMap("../../scripts/c2_smoke46_rest_conf.json")
	if err != nil {
		t.Skip("config not available")
	}
	questions, _ := loadQuestions("/home/zhichyu/Downloads/data/browsecomp-plus/questions.jsonl")
	evidence := map[string][]string{}
	for _, q := range questions {
		evidence[q.questionID] = q.evidenceDocs
	}
	merged := mergeJudgements(dedupeLast(answers), judgements)
	goLeaderboard := buildLeaderboard(merged, cfg, evidence)

	goAcc := goLeaderboard["Accuracy (%)"].(float64)
	pyAcc := pyLeaderboard["Accuracy (%)"].(float64)
	if goAcc != pyAcc {
		t.Errorf("Accuracy: go=%v py=%v", goAcc, pyAcc)
	}
	goJudged := len(goLeaderboard["per_query_judgements"].([]map[string]any))
	pyJudged := len(pyLeaderboard["per_query_judgements"].([]any))
	if goJudged != pyJudged {
		t.Errorf("judgements: go=%d py=%d", goJudged, pyJudged)
	}
	goRecall := goLeaderboard["Recall (%)"]
	pyRecall := pyLeaderboard["Recall (%)"]
	if fmt.Sprintf("%v", goRecall) != fmt.Sprintf("%v", pyRecall) {
		t.Errorf("Recall: go=%v py=%v", goRecall, pyRecall)
	}
}

// TestAsDocIDListAcceptsTheShapeExtractRunStatsProduces pins the bug that zeroed
// gold_doc_served / gold_doc_cited on every freshly written row:
// extractRunStats stores uniqueDocIDs' OWN []string result back into the row, so
// the served-set builder later reads a []string - not the []any a JSON decode
// gives. An []any-only assertion fell through to normalizeDocID, which
// fmt.Sprintf'd the entire list into ONE scalar ("[5580 15715 ...]"), matching
// no expected document. The leaderboard path reads answers.jsonl back from disk
// (JSON decoding gives []any), so Recall stayed correct and only the in-run rows
// were wrong - which is exactly what the smoke47 re-run showed: 10 rows with
// gold_doc_served=[], Recall 61.3% from the same rows.
func TestAsDocIDListAcceptsTheShapeExtractRunStatsProduces(t *testing.T) {
	want := "[5580 15715]"

	// 1. What a JSON decode gives.
	if got := fmt.Sprintf("%v", asDocIDList([]any{"5580", "15715"})); got != want {
		t.Fatalf("asDocIDList([]any) = %s, want %s", got, want)
	}
	// 2. What extractRunStats produces (and stores back into the row).
	stored := uniqueDocIDs([]any{"5580.md", "15715", "5580"})
	if got := fmt.Sprintf("%v", asDocIDList(stored)); got != want {
		t.Fatalf("asDocIDList([]string) = %s, want %s (the list collapsed into one scalar: %v)", got, want, stored)
	}
	// 3. The served-set intersection the answer phase runs on that value.
	expected := map[string]bool{"5580": true, "15715": true, "37106": true}
	var served []string
	for _, id := range asDocIDList(stored) {
		if expected[id] {
			served = append(served, id)
		}
	}
	if got := fmt.Sprintf("%v", served); got != want {
		t.Fatalf("served = %s, want %s", got, want)
	}
	// 4. uniqueIDs (chunk ids) shares the contract.
	if got := fmt.Sprintf("%v", uniqueIDs([]string{"a", "b", "a"})); got != "[a b]" {
		t.Fatalf("uniqueIDs([]string) = %s, want [a b]", got)
	}
	// 5. A plain scalar still normalises to a one-element list.
	if got := fmt.Sprintf("%v", asDocIDList("5580.md")); got != "[5580]" {
		t.Fatalf("asDocIDList(scalar) = %s, want [5580]", got)
	}
}
