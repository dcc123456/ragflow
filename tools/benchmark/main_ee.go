//go:build ee

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Command benchmark is the Go port of scripts/ragflow_benchmark.py: a
// dataset-agnostic QA benchmark runner for RAGFlow.
//
// How a run works:
//   - One question loader - .jsonl, JSON list, or JSON {id: {...}} mapping.
//   - An answer phase (a RAGFlow chat answers each question) followed by a judge
//     phase (a SEPARATE judge chat scores each answer), both resumable and both
//     parallelisable via concurrency.
//   - One judge scorer: the 0/2/4 rule book first, tolerant fallbacks after.
//   - One result artefact: leaderboard.json (the BrowseComp-Plus submission JSON
//     plus per-question judgement/usage extensions) carries everything a run
//     produces; answers.jsonl stays a pure record of what the agent returned.
//
// Reliability semantics carried over from the Python original:
//   - A question counts as finished when its row carries an answer and no
//     ragflow_error, OR when its error is PERSISTENT (the provider refused the
//     input itself - content policy, e.g. minimax's "input new_sensitive"; a
//     retry reproduces the refusal). Transient errors never write a damaged
//     row: the question parks ERROR_RETRY_WAIT_SEC (10 min) in process and
//     retries until it succeeds, so no external watcher is needed. (10 min, not
//     30: MiniMax now resets its 5-hour allowance on a SLIDING window, so a
//     slot reopens sooner and a long park only wastes wall clock.)
//   - A provider quota wall is ridden out in process: the phase stops
//     dispatching, waits --wall-wait seconds, then re-enters itself and
//     retries exactly the work still missing. Unbounded unless
//     --wall-max-waits N is given.
//   - Skipping finished work IS the resume: repeating the same command
//     continues the run directory; --overwrite discards it. Each run records
//     its git commit into _prompt_fingerprint.json once.
//
// Usage:
//
//	benchmark --config <conf.json> [--ids 11,33] [--dry-run]
//	benchmark --config <conf.json> --run outputs/<dir> --skip-answers
//	benchmark --config <conf.json>            # re-run a batch: same command

const (
	defaultConfigPath  = "qa_benchmark_conf.json"
	defaultOutputDir   = "outputs/qa_benchmark_<timestamp>"
	defaultAnswersName = "answers.jsonl"
	defaultLeaderboard = "leaderboard.json"
	fingerprintName    = "_prompt_fingerprint.json"
)

func main() {
	os.Exit(run())
}

func run() int {
	fs := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	configPath := fs.String("config", defaultConfigPath, "Path to the benchmark config JSON")
	concurrency := fs.Int("concurrency", 0, "Questions answered (and judged) in parallel; overrides the config's concurrency (default 1 = serial)")
	idsFlag := fs.String("ids", "", "Comma-separated question ids; overrides question_ids.include from the config")
	runDir := fs.String("run", "", "Reuse an existing output directory instead of creating a new one")
	overwrite := fs.Bool("overwrite", false, "Drop existing JSONL files instead of resuming")
	dryRun := fs.Bool("dry-run", false, "Print the plan without calling any API")
	skipAnswers := fs.Bool("skip-answers", false, "Judge an existing answers.jsonl")
	skipJudge := fs.Bool("skip-judge", false, "Only collect answers")
	wallWait := fs.Float64("wall-wait", wallRetryWaitSec, "Seconds to sleep when the provider Token Plan is exhausted, before retrying the pending work in process")
	wallMaxWaitsFlag := fs.Int("wall-max-waits", wallMaxWaits, "Give up after this many plan-wall waits instead of waiting indefinitely (0 = unlimited; a bounded run exits 1)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}

	cfg, err := loadJSONMap(*configPath)
	if err != nil {
		fmt.Println(err)
		return 2
	}
	baseDir := filepath.Dir(*configPath)

	outputCfg, _ := cfg["output"].(map[string]any)
	var outputDir string
	if *runDir != "" {
		outputDir = absPath(*runDir)
	} else {
		outputDir = resolveOutputDir(strAny(outputCfg, "output_dir", defaultOutputDir))
	}
	answersPath := filepath.Join(outputDir, strAny(outputCfg, "answers_jsonl", defaultAnswersName))
	leaderboardPath := filepath.Join(outputDir, strAny(outputCfg, "leaderboard_json", defaultLeaderboard))

	// CLI --ids replaces question_ids.include for this run; everything else in
	// the question_ids block (random is moot, exclude still applies) is kept.
	// Precedence: --ids > include_file > include.
	selection, _ := cfg["question_ids"].(map[string]any)
	if selection == nil {
		selection = map[string]any{}
	}
	includeFile := strings.TrimSpace(strAny(selection, "include_file", ""))
	if includeFile != "" && *idsFlag == "" {
		fileIDs := readIDList(resolvePath(includeFile, baseDir))
		if len(fileIDs) == 0 {
			fmt.Printf("question_ids.include_file is missing or empty: %s\n", resolvePath(includeFile, baseDir))
			return 2
		}
		selection["include"] = toAnySlice(fileIDs)
		fmt.Printf("[ids] %d id(s) from %s\n", len(fileIDs), resolvePath(includeFile, baseDir))
	}
	if *idsFlag != "" {
		var parts []any
		for _, part := range strings.Split(*idsFlag, ",") {
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
		selection["include"] = parts
	}

	if fileExists(answersPath) && !*overwrite {
		existing := dedupeLast(readJSONL(answersPath))
		answered, persistent, transient := resumeSplit(existing)
		judged := countSettledJudgements(readJudgements(leaderboardPath))
		fmt.Printf("[resume] %s: %d row(s) - %d answered + %d persistent-error (skipped), %d transient failed/aborted will be retried; %d verdict(s) already stored\n",
			filepath.Base(answersPath), len(existing), answered, persistent, transient, judged)
	}

	conc := *concurrency
	if conc == 0 {
		conc = asIntDefault(cfg["concurrency"], 1)
	}
	if conc < 1 {
		conc = 1
	}

	var selected []*question
	if *skipAnswers {
		if !fileExists(answersPath) {
			fmt.Printf("--skip-answers needs an existing answers file: %s\n", answersPath)
			return 2
		}
		fmt.Printf("Reusing answers file %s\n", answersPath)
	} else {
		questionsPath := resolvePath(questionsPathOf(cfg), baseDir)
		questions, err := loadQuestions(questionsPath)
		if err != nil {
			fmt.Println(err)
			return 2
		}
		selected, err = selectQuestions(questions, selection)
		if err != nil {
			fmt.Println(err)
			return 2
		}
		fmt.Printf("Loaded %d questions from %s\n", len(questions), questionsPath)
		if dataset, _ := cfg["dataset"].(map[string]any); dataset != nil {
			fmt.Printf("Dataset: %s - %s\n", strAny(dataset, "name", "(unnamed)"), strAny(dataset, "description", "(no description)"))
			if cp := strAny(dataset, "corpus_path", ""); cp != "" {
				fmt.Printf("Corpus: %s\n", cp)
			}
		}
		fmt.Printf("Selected %d question(s) (concurrency=%d)\n", len(selected), conc)
	}
	fmt.Printf("Output directory: %s\n", outputDir)

	if *dryRun {
		for i, q := range selected {
			if i >= 10 {
				break
			}
			fmt.Printf("- %s: %.100s\n", q.questionID, q.question)
		}
		if len(selected) > 10 {
			fmt.Printf("... %d more\n", len(selected)-10)
		}
		return 0
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		fmt.Println(err)
		return 1
	}

	// Provenance before any phase: the fingerprint carries the commit of the
	// run that produced the rows, so --overwrite must drop a stale one, while
	// a resume keeps the original.
	fingerprintPath := filepath.Join(outputDir, fingerprintName)
	if *overwrite && fileExists(fingerprintPath) {
		os.Remove(fingerprintPath)
	}
	recordRunProvenance(outputDir)

	if !*skipAnswers {
		if *overwrite && fileExists(answersPath) {
			os.Remove(answersPath)
		}
		answerAborted := runAnswerPhase(newClient(cfg), cfg, selected, answersPath, conc,
			time.Duration(*wallWait*float64(time.Second)), *wallMaxWaitsFlag)
		if answerAborted {
			fmt.Printf("Run gave up waiting for the plan at the answer phase; %s holds the completed rows.\n", answersPath)
			return 1
		}
		if *skipJudge {
			fmt.Printf("Answers written to %s\n", answersPath)
			return 0
		}
	}

	judgements, judgeAborted := runJudgePhase(newClient(cfg), cfg, answersPath, leaderboardPath, conc,
		time.Duration(*wallWait*float64(time.Second)), *wallMaxWaitsFlag)

	// The leaderboard export scores retrieval recall against each question's
	// EVIDENCE documents, which live in the questions file rather than in the
	// answers - an answers.jsonl from an older run predates the field, and
	// --skip-answers never re-reads the questions at all.
	allRows := dedupeLast(readJSONL(answersPath))
	evidenceByID := map[string][]string{}
	if p := questionsPathOf(cfg); p != "" {
		evidenceByID = loadEvidenceDocIDs(resolvePath(p, baseDir))
	}
	merged := mergeJudgements(allRows, judgements)
	leaderboard := buildLeaderboard(merged, cfg, evidenceByID)
	writeJSON(leaderboardPath, leaderboard)

	if judgeAborted {
		fmt.Println("Run gave up waiting for the plan at the judge phase; re-run the same command to continue.")
		return 1
	}
	fmt.Printf("Leaderboard submission written to %s\n", leaderboardPath)
	fmt.Println(leaderboardSummary(leaderboard))
	return 0
}

// Shared small helpers: dynamic-JSON accessors mirroring the Python
// original's dict handling, path resolution and the JSONL/JSON IO used by
// every phase. Field order in the written artefacts matters (the leaderboard
// is a submission document), so the ordered builders live in leaderboard.go.

// ---------------------------------------------------------------------------
// Dynamic JSON accessors (port of _as_float / _as_int / _as_string_list / ...)
// ---------------------------------------------------------------------------

func asFloat(v any) *float64 {
	// Usage rows and judgement records store their optional numbers as
	// *int/*float64 (the asInt/asFloat results). Unwrap one pointer level
	// here, or every reader of those rows reads zeros through the pointer.
	switch x := v.(type) {
	case *float64:
		if x == nil {
			return nil
		}
		f := *x
		return &f
	case *int:
		if x == nil {
			return nil
		}
		f := float64(*x)
		return &f
	case *json.Number:
		if x == nil {
			return nil
		}
		return asFloat(*x)
	case *bool:
		return nil
	}
	switch x := v.(type) {
	case nil:
		return nil
	case float64:
		return &x
	case int:
		f := float64(x)
		return &f
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return nil
		}
		return &f
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return nil
		}
		return &f
	case bool:
		return nil
	default:
		return nil
	}
}

func asInt(v any) *int {
	f := asFloat(v)
	if f == nil {
		return nil
	}
	n := int(*f)
	return &n
}

func asIntDefault(v any, def int) int {
	if n := asInt(v); n != nil {
		return *n
	}
	return def
}

func asStringList(v any) []string {
	if v == nil || v == "" {
		return nil
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	case float64:
		return []string{fmt.Sprintf("%v", x)}
	case bool:
		return []string{fmt.Sprintf("%v", x)}
	case []any:
		var out []string
		for _, item := range x {
			if item == nil {
				continue
			}
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	default:
		return nil
	}
}

func strAny(m map[string]any, key, def string) string {
	if m == nil {
		return def
	}
	switch x := m[key].(type) {
	case string:
		if x == "" {
			return def
		}
		return x
	case nil:
		return def
	case *int:
		if x == nil {
			return def
		}
		return fmt.Sprintf("%d", *x)
	case *float64:
		if x == nil {
			return def
		}
		return fmt.Sprintf("%v", *x)
	case *bool:
		if x == nil {
			return def
		}
		return fmt.Sprintf("%v", *x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func absPath(p string) string {
	abs, err := filepath.Abs(expandHome(p))
	if err != nil {
		return p
	}
	return abs
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func resolvePath(value, baseDir string) string {
	p := expandHome(value)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(baseDir, p))
}

func resolveOutputDir(value string) string {
	timestamp := time.Now().Format("20060102_150405")
	p := expandHome(strings.ReplaceAll(value, "<timestamp>", timestamp))
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	wd, _ := os.Getwd()
	return filepath.Clean(filepath.Join(wd, p))
}

func loadJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	return payload, nil
}

func writeJSON(path string, payload any) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Printf("[io] could not marshal %s: %v\n", path, err)
		return
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		fmt.Printf("[io] could not write %s: %v\n", path, err)
	}
}

func readJSONL(path string) []map[string]any {
	if !fileExists(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("[io] could not read %s: %v\n", path, err)
		return nil
	}
	var rows []map[string]any
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			fmt.Printf("[io] invalid JSONL at %s:%d: %v\n", path, i+1, err)
			return rows
		}
		rows = append(rows, row)
	}
	return rows
}

func toAnySlice(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

func compact(payload any) string {
	var text string
	if s, ok := payload.(string); ok {
		text = s
	} else {
		data, err := json.Marshal(payload)
		if err != nil {
			text = fmt.Sprintf("%v", payload)
		} else {
			text = string(data)
		}
	}
	if len(text) > 1000 {
		return text[:1000]
	}
	return text
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// naturalKey ports _natural_key: numeric ids sort numerically with a fixed
// width, everything else sorts lexically after them.
func naturalKey(value string) string {
	if n, err := strconv.Atoi(value); err == nil {
		return fmt.Sprintf("0:%012d", n)
	}
	return "1:" + value
}

// ---------------------------------------------------------------------------
// Resume accounting
// ---------------------------------------------------------------------------

// resumeSplit splits the deduped rows into (clean, persistent-error,
// transient-error) counts - the [resume] line's triple.
func resumeSplit(rows []map[string]any) (int, int, int) {
	answered, persistent := 0, 0
	for _, row := range rows {
		errText := strings.TrimSpace(strAny(row, "ragflow_error", ""))
		switch {
		case errText == "":
			answered++
		case isPersistentRagflowError(errText):
			persistent++
		}
	}
	return answered, persistent, len(rows) - answered - persistent
}

// persistentErrorMarkers: markers of an error that makes a retry pointless -
// the provider refused the INPUT itself (content policy), so the same
// question fails identically on every attempt (q744: five identical
// "input new_sensitive" failures across as many resumes).
//
// "context deadline exceeded" is terminal too: it is what mode 8 returns when
// one turn burns the whole codex.turn_timeout budget (1800s), and re-running
// the same question just spends another budget on the same question - q875,
// q1044 and q507 hit it on two consecutive attempts, each exactly at T+1800s.
// Left transient, they retry forever and hold every concurrency slot.
//
// Everything else (quota walls, connection aborts, retry exhaustion with a
// recoverable cause) stays transient and is retried; an unknown error also
// defaults to transient - one wasted retry beats one silently abandoned
// question.
var persistentErrorMarkers = []string{
	"new_sensitive",
	"sensitive content",
	"content_filter",
	"content filter",
	"context deadline exceeded",
}

// isPersistentRagflowError ports _is_persistent_ragflow_error.
func isPersistentRagflowError(err string) bool {
	text := strings.ToLower(err)
	for _, marker := range persistentErrorMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Run provenance (port of _record_run_provenance)
// ---------------------------------------------------------------------------

// recordRunProvenance stamps the run's git commit into
// _prompt_fingerprint.json, once. The archived rows must answer "what code
// produced them" without log archaeology (the #46 regression review needed
// exactly that, and had to reconstruct it from launch scripts). A fingerprint
// that already carries a commit is left untouched: a resumed run must not
// overwrite the commit of the run that produced the rows.
func recordRunProvenance(outputDir string) string {
	fingerprint := filepath.Join(outputDir, fingerprintName)
	existing := map[string]any{}
	if fileExists(fingerprint) {
		data, err := os.ReadFile(fingerprint)
		if err != nil {
			fmt.Printf("[run] could not read %s: %v; a fresh one will be written\n", fingerprintName, err)
		} else if err := json.Unmarshal(data, &existing); err != nil {
			fmt.Printf("[run] could not read %s: %v; a fresh one will be written\n", fingerprintName, err)
			existing = map[string]any{}
		}
	}
	if commit, _ := existing["git_commit"].(string); commit != "" {
		return commit
	}
	out, err := exec.Command("git", "rev-parse", "--short=9", "HEAD").Output()
	if err != nil {
		fmt.Printf("[run] git commit not recorded: %v\n", err)
		return ""
	}
	commit := strings.TrimSpace(string(out))
	existing["git_commit"] = commit
	if _, ok := existing["git_commit_recorded_at"]; !ok {
		existing["git_commit_recorded_at"] = time.Now().Format("2006-01-02 15:04:05")
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	tmp := fingerprint + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err == nil {
		os.Rename(tmp, fingerprint)
	}
	fmt.Printf("[run] git commit recorded: %s -> %s\n", commit, fingerprintName)
	return commit
}

// ---------------------------------------------------------------------------
// Sorting helper for the {id: {...}} question mapping
// ---------------------------------------------------------------------------

func sortIDs(ids []string) {
	sort.Slice(ids, func(i, j int) bool { return naturalKey(ids[i]) < naturalKey(ids[j]) })
}

// ---------------------------------------------------------------------------
// Doc-id normalisation (port of _normalize_benchmark_doc_id and friends)
// ---------------------------------------------------------------------------

// normalizeDocID ports _normalize_benchmark_doc_id: strip any directory path
// and a trailing ".md" - the corpus documents are addressed by their stem.
func normalizeDocID(value any) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if text == "" {
		return ""
	}
	filename := text
	if i := strings.LastIndexAny(filename, "/\\"); i >= 0 {
		filename = filename[i+1:]
	}
	if strings.HasSuffix(strings.ToLower(filename), ".md") {
		filename = filename[:len(filename)-3]
	}
	return strings.TrimSpace(filename)
}

// uniqueDocIDs ports _unique_doc_ids: first occurrence wins, empties dropped.
// asAnyList accepts the two shapes a JSON array reaches this file in: a freshly
// decoded []any, and the []string a previous uniqueDocIDs/uniqueIDs call
// produced. The second shape is the one that bites: extractRunStats stores its
// own result back into the row, so any value that passes through these helpers
// AGAIN is a []string - and an []any-only assertion fell through to
// normalizeDocID, which fmt.Sprintf'd the whole list into ONE scalar
// ("[5580 15715 ...]"). Measured on the smoke47 re-run: every freshly written
// row carried gold_doc_served=[], while the disk-decoded leaderboard path (JSON
// gives []any) computed Recall 61.3% from the same rows.
func asAnyList(values any) ([]any, bool) {
	if list, ok := values.([]any); ok {
		return list, true
	}
	if list, ok := values.([]string); ok {
		out := make([]any, len(list))
		for i, value := range list {
			out[i] = value
		}
		return out, true
	}
	return nil, false
}

func uniqueDocIDs(values any) []string {
	seen := map[string]bool{}
	var ids []string
	list, ok := asAnyList(values)
	if !ok {
		if s := normalizeDocID(values); s != "" {
			return []string{s}
		}
		return nil
	}
	for _, value := range list {
		normalized := normalizeDocID(value)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		ids = append(ids, normalized)
	}
	return ids
}

// asDocIDList ports _as_doc_id_list: accept a scalar, a list, or missing.
func asDocIDList(value any) []string {
	if value == nil || value == "" {
		return nil
	}
	list, ok := value.([]any)
	if !ok {
		return uniqueDocIDs(value)
	}
	return uniqueDocIDs(list)
}

// uniqueIDs ports _unique_ids: deduplicate opaque identifiers (chunk ids),
// preserving the backend's own order - NO document-name normalisation.
func uniqueIDs(values any) []string {
	list, ok := asAnyList(values)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	for _, value := range list {
		text := strings.TrimSpace(fmtAny(value))
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		ids = append(ids, text)
	}
	return ids
}

func fmtAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

func timeNow() time.Time { return time.Now() }

// The HTTP layer and the two chat entry points (port of JsonHttpClient,
// create_session, ask_ragflow, ask_judge / ask_judge_with_retry and the
// answer-text extraction). One *http.Client is shared: unlike
// requests.Session it is safe for concurrent use, so the Python client's
// clone() has no Go counterpart.

const (
	defaultTimeoutSeconds = 2400
	defaultMaxRetries     = 0
	defaultBackoffSeconds = 2.0
)

// GRADER_TEMPLATE is THE judge prompt, used for every dataset. Provenance:
// copied byte-for-byte from GRADER_TEMPLATE in the BrowseComp-Plus scoring
// script (scripts_evaluation/evaluate_run.py), because a submission is only
// comparable with the published numbers when the same judge prompt produced
// the verdicts. The verdict is BINARY (correct: yes/no) and the judge reports
// the confidence found in the answer (0-100, 100 when the answer states none).
const graderTemplate = `Judge whether the following [response] to [question] is correct or not based on the precise and unambiguous [correct_answer] below.

[question]: {question}

[response]: {response}

[correct_answer]: {correct_answer}

Your judgement must be in the format and criteria specified below:

extracted_final_answer: The final exact answer extracted from the [response].

[correct_answer]: Repeat the [correct_answer] given above.

reasoning: Explain why the extracted_final_answer is correct or incorrect based on [correct_answer], in the context of this [question]. You should judge whether the extracted_final_answer is semantically equivalent to [correct_answer], allowing the extracted_final_answer to be string variations of [correct_answer]. You should also allow the extracted_final_answer to be more precise or verbose than [correct_answer], as long as its additional details are correct. Do not comment on any background to the problem, do not attempt to solve the problem, do not argue for any answer different than [correct_answer], focus only on whether the answers are semantically equivalent.

correct: Answer 'yes' if extracted_final_answer matches the [correct_answer] given above, or is within a small margin of error for numerical problems. Answer 'no' otherwise, i.e. if there if there is any inconsistency, ambiguity, non-equivalency, or if the extracted answer is incorrect.


confidence: The extracted confidence score between 0|\%| and 100|\%| from [response]. Put 100 if there is no confidence score available.`

// judgeTransientMarkers: a judge call that comes back as a provider error is
// retried before the row is recorded as unjudged. The backend decorates
// provider failures into the ANSWER TEXT ("**ERROR**: minimax API error: ..."),
// so they never surface as a transport error - retrying has to inspect the
// text. Without this, one rate-limit burst silently produced rows with no
// verdict at all (and an unjudged row counts as incorrect).
var judgeTransientMarkers = []string{
	"速率限制", "用量上限", "rate limit", "429", "529", "status 5",
	"timed out", "timeout", "connection reset", "connection refused",
}

// benchmarkError ports BenchmarkError.
type benchmarkError struct{ msg string }

func (e *benchmarkError) Error() string { return e.msg }

func errf(format string, args ...any) error {
	return &benchmarkError{msg: fmt.Sprintf(format, args...)}
}

// httpClient ports JsonHttpClient.
type httpClient struct {
	baseURL    string
	apiKey     string
	maxRetries int
	backoffSec float64
	underlying *http.Client
}

func newClient(cfg map[string]any) *httpClient {
	apiKey := strAny(cfg, "ragflow_api_key", "")
	if apiKey == "" {
		apiKey = os.Getenv("RAGFLOW_API_KEY")
	}
	if apiKey == "" {
		// Mirror the Python ValueError; the caller reports it.
		panic("ragflow_api_key is required in the config (or set the RAGFLOW_API_KEY environment variable)")
	}
	backend, _ := cfg["backend"].(map[string]any)
	baseURL := strAny(backend, "base_url", "")
	if baseURL == "" {
		baseURL = fmt.Sprintf("%s://%s:%s",
			strAny(backend, "scheme", "http"), strAny(backend, "host", "127.0.0.1"), strAny(backend, "port", "80"))
	}
	return &httpClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		maxRetries: defaultMaxRetries,
		backoffSec: defaultBackoffSeconds,
		underlying: &http.Client{Timeout: defaultTimeoutSeconds * time.Second},
	}
}

func (c *httpClient) post(path string, body map[string]any) (any, error) {
	return c.request(path, body, false)
}

func (c *httpClient) postEventstream(path string, body map[string]any) (any, error) {
	stream := map[string]any{"stream": true}
	for k, v := range body {
		stream[k] = v
	}
	return c.request(path, stream, true)
}

func (c *httpClient) request(path string, body map[string]any, eventstream bool) (any, error) {
	target := c.baseURL + path
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	accept := "application/json"
	if eventstream {
		accept = "text/event-stream"
	}
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		backoff := time.Duration(c.backoffSec*float64(int64(1)<<uint(attempt))) * time.Second
		req, err := http.NewRequest("POST", target, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.underlying.Do(req)
		if err != nil {
			lastErr = err
			if attempt < c.maxRetries && shouldRetry(lastErr) {
				time.Sleep(backoff)
				continue
			}
			break
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if eventstream && resp.StatusCode < 400 && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			return collectEventstreamAnswer(string(data)), nil
		}
		decoded := decodeResponse(data)
		if resp.StatusCode >= 400 {
			lastErr = errf("HTTP %d from %s: %s", resp.StatusCode, target, compact(decoded))
		} else if m, ok := decoded.(map[string]any); ok {
			if code := asInt(m["code"]); code != nil && *code != 0 {
				lastErr = errf("RAGFlow code %d from %s: %s", *code, target, strAny(m, "message", compact(m)))
			} else if inner, ok := m["data"]; ok {
				return inner, nil
			} else {
				return decoded, nil
			}
		} else {
			return decoded, nil
		}
		if lastErr != nil && (attempt >= c.maxRetries || !shouldRetry(lastErr)) {
			break
		}
		if lastErr != nil {
			time.Sleep(backoff)
		}
	}
	return nil, lastErr
}

func decodeResponse(data []byte) any {
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return map[string]any{"raw": string(data)}
	}
	return payload
}

// shouldRetry ports _should_retry: transport errors and 5xx/timeout bubbles
// are worth another attempt; a 4xx is not.
func shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 5") || strings.Contains(msg, "timed out") || strings.Contains(msg, "Timeout")
}

// collectEventstreamAnswer ports _collect_eventstream_answer: assemble the
// answer from the SSE stream's data lines.
func collectEventstreamAnswer(raw string) string {
	answer := ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[5:])
		if data == "" || data == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			continue
		}
		if code := asInt(payload["code"]); code != nil && *code != 0 {
			// Python raises out of the collection loop; the error rides in the
			// answer text the caller already handles (**ERROR**).
			continue
		}
		answer = appendAnswerPart(answer, payload)
	}
	return answer
}

func appendAnswerPart(answer string, payload map[string]any) string {
	if data, ok := payload["data"].(map[string]any); ok {
		if s, ok := data["answer"].(string); ok {
			return answer + s
		}
		if _, has := data["reference"]; has {
			return answer
		}
	}
	if s, ok := payload["answer"].(string); ok {
		return answer + s
	}
	if choices, ok := payload["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if delta, ok := choice["delta"].(map[string]any); ok {
				if s, ok := delta["content"].(string); ok {
					return answer + s
				}
			}
		}
	}
	return answer
}

// extractAnswerText ports extract_answer_text: dig the answer text out of the
// many payload shapes the chat API and the agent modes return.
func extractAnswerText(payload any) string {
	switch p := payload.(type) {
	case nil:
		return ""
	case string:
		return p
	case map[string]any:
		for _, key := range []string{"answer", "content", "text", "message"} {
			switch value := p[key].(type) {
			case string:
				return value
			case map[string]any:
				if nested := extractAnswerText(value); nested != "" {
					return nested
				}
			}
		}
		if data, ok := p["data"]; ok {
			return extractAnswerText(data)
		}
		if choices, ok := p["choices"].([]any); ok && len(choices) > 0 {
			return extractAnswerText(choices[0])
		}
	}
	return ""
}

// extractSessionID ports _extract_session_id.
func extractSessionID(payload any) string {
	m, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"session_id", "conversation_id", "id"} {
		if s, ok := m[key].(string); ok && s != "" {
			return s
		}
	}
	if data, ok := m["data"].(map[string]any); ok {
		return extractSessionID(data)
	}
	return ""
}

// createSession ports create_session.
func createSession(c *httpClient, chatID string) (string, error) {
	payload, err := c.post("/api/v1/chats/"+url.PathEscape(chatID)+"/sessions",
		map[string]any{"name": fmt.Sprintf("qa-benchmark-%s", time.Now().Format("20060102-150405"))})
	if err != nil {
		return "", err
	}
	sessionID := extractSessionID(payload)
	if sessionID == "" {
		return "", errf("Could not create chat session: %s", compact(payload))
	}
	return sessionID, nil
}

// askRagflow ports ask_ragflow: one chat completion against the answer chat.
func askRagflow(c *httpClient, cfg map[string]any, question, sessionID string) (any, error) {
	chatCfg, _ := cfg["ragflow_chat"].(map[string]any)
	body := map[string]any{
		"chat_id":  strAny(cfg, "chat_id", ""),
		"question": question,
		"stream":   boolOf(chatCfg["stream"]),
	}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	if v := strAny(chatCfg, "llm_id", ""); v != "" {
		body["llm_id"] = v
	}
	for _, key := range []string{"quote", "refine_multiturn", "temperature", "top_p", "max_tokens"} {
		if v, ok := chatCfg[key]; ok {
			body[key] = v
		}
	}
	// `reasoning` is the engine selector and nothing else is: 0 off, 1..4 the
	// harness depths, 5 the agentic ReAct loop, 6 iterative synthesis. It is
	// pinned to 2 (a harness depth) unless the config names a level, so a config
	// that predates the levels still runs what it always did. There is no
	// agent_mode: the API removed it, and a config still carrying one is ignored.
	body["reasoning"] = asIntDefault(chatCfg["reasoning"], 2)
	if boolOf(body["stream"]) {
		return c.postEventstream("/api/v1/chat/completions", body)
	}
	return c.post("/api/v1/chat/completions", body)
}

func boolOf(v any) bool {
	// Judgement records store "correct" as *bool (the leaderboardCorrect
	// result); unwrap before the type assertion or every false reads as false
	// AND every pointer reads as non-nil.
	if p, ok := v.(*bool); ok {
		if p == nil {
			return false
		}
		return *p
	}
	b, _ := v.(bool)
	return b
}

// renderJudgePrompt ports render_judge_prompt. The template is not
// configurable: it is the BrowseComp-Plus scoring script's reference prompt,
// so letting a config rewrite it would silently break comparability with
// published numbers.
var thinkRE = regexp.MustCompile(`(?s)<think>.*?</think>`)

func renderJudgePrompt(row map[string]any) string {
	return strings.NewReplacer(
		"{question}", strAny(row, "question", ""),
		"{correct_answer}", strAny(row, "gold_answer", ""),
		"{response}", thinkRE.ReplaceAllString(strAny(row, "ragflow_answer", ""), ""),
	).Replace(graderTemplate)
}

// judgeInstanceOverride reports whether the config still names judge instances.
// It selects nothing: the judge chat carries its OWN failover group, the same
// mechanism the answering chat uses (RAGFlow chat-level model failover), so
// naming an instance would PIN the request to that one and defeat the group.
// The key is read only to warn that it is ignored.
func judgeInstanceOverride(cfg map[string]any) bool {
	if list, ok := cfg["judge_llm_ids"].([]any); ok && len(list) > 0 {
		return true
	}
	return strings.TrimSpace(strAny(cfg, "judge_llm_id", "")) != ""
}

// isTransientJudgeAnswer ports _is_transient_judge_answer: True when a judge
// reply is a provider failure rather than a verdict. A retry can only help
// when the failure is the transient kind, hence the marker test on top of the
// ERROR prefix.
func isTransientJudgeAnswer(answer string) bool {
	if !strings.HasPrefix(strings.TrimLeft(answer, " \t\n"), "**ERROR**") {
		return false
	}
	for _, marker := range judgeTransientMarkers {
		if strings.Contains(answer, marker) {
			return true
		}
	}
	return false
}

var judgeMaxRetries = 3
var judgeRetryBackoff = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}

// askJudgeWithRetry ports ask_judge_with_retry: ask_judge with backoff retries.
// ONE failure class is handled now - a transient provider burst, retried on the
// SAME chat with backoff. The instance rotation that used to sit on top of it is
// GONE: the judge chat's own failover group covers an exhausted instance (the
// same chat-level failover the answering chat uses), and naming an instance
// would pin the request to it and disable the group, turning one exhausted plan
// into a wall that no retry could pass.
func askJudgeWithRetry(c *httpClient, cfg map[string]any, row map[string]any) string {
	answer := ""
	for attempt := 0; attempt <= judgeMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(judgeRetryBackoff[min(attempt-1, len(judgeRetryBackoff)-1)])
		}
		payload, judgeErr := askJudge(c, cfg, row)
		if judgeErr != nil {
			answer = "**ERROR**: " + judgeErr.Error()
		} else {
			answer = extractAnswerText(payload)
		}
		if !isTransientJudgeAnswer(answer) {
			return answer
		}
	}
	return answer
}

// askJudge ports ask_judge. No `llm_id` is sent: the judge chat resolves its own
// failover group, exactly like the answering chat.
func askJudge(c *httpClient, cfg map[string]any, row map[string]any) (any, error) {
	body := map[string]any{
		"chat_id":  strAny(cfg, "judge_chat_id", ""),
		"question": renderJudgePrompt(row),
		"stream":   boolOf(cfg["judge_stream"]),
	}
	if boolOf(body["stream"]) {
		return c.postEventstream("/api/v1/chat/completions", body)
	}
	return c.post("/api/v1/chat/completions", body)
}

// Question loading, normalisation and selection (port of load_questions,
// _normalize_question, select_questions, load_evidence_doc_ids and the id
// helpers).

// Candidate keys per logical field, first hit wins. Keeps dataset-specific
// naming out of the code: add a key here instead of a new loader.
var (
	idKeys            = []string{"id", "question_id", "qid"}
	questionKeys      = []string{"question", "query", "prompt"}
	goldKeys          = []string{"gold_answer", "answer", "reference_answer", "gold"}
	reasoningTypeKeys = []string{"reasoning_types", "reasoning_type", "types"}
	expectedDocKeys   = []string{"expected_doc_ids", "expected_sources"}
	// The leaderboard scores RECALL against the qrel "evidence" set - every
	// document labelled as needed to answer the query - which is a superset of
	// the gold set. BrowseComp-Plus ships both as expected_doc_ids /
	// evidence_doc_ids, so they are read separately: the diagnostic report
	// keeps using expected_doc_ids, leaderboard.json uses evidence_doc_ids.
	evidenceDocKeys = []string{"evidence_doc_ids", "expected_evidence_doc_ids", "qrel_evidence"}
)

type question struct {
	questionID     string
	question       string
	goldAnswer     string
	reasoningTypes []string
	expectedDocs   []string
	evidenceDocs   []string
}

// loadQuestions ports load_questions: .jsonl, JSON list, or {id: {...}} map.
func loadQuestions(path string) ([]*question, error) {
	if strings.EqualFold(filepath.Ext(path), ".jsonl") {
		return loadQuestionsJSONL(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	switch payload := raw.(type) {
	case []any:
		out := make([]*question, 0, len(payload))
		for i, row := range payload {
			m, ok := row.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("question row %d must be a JSON object", i+1)
			}
			out = append(out, normalizeQuestion(m, i+1))
		}
		return out, nil
	case map[string]any:
		if list, ok := payload["questions"].([]any); ok {
			out := make([]*question, 0, len(list))
			for i, row := range list {
				m, ok := row.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("question row %d must be a JSON object", i+1)
				}
				out = append(out, normalizeQuestion(m, i+1))
			}
			return out, nil
		}
		ids := make([]string, 0, len(payload))
		for id := range payload {
			ids = append(ids, id)
		}
		sortIDs(ids)
		out := make([]*question, 0, len(ids))
		for _, id := range ids {
			m, ok := payload[id].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("question row %s must be a JSON object", id)
			}
			out = append(out, normalizeQuestion(m, id))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported question file format: %s", path)
	}
}

func loadQuestionsJSONL(path string) ([]*question, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []*question
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return nil, fmt.Errorf("invalid JSONL at %s:%d: %w", path, i+1, err)
		}
		out = append(out, normalizeQuestion(payload, i+1))
	}
	return out, nil
}

// loadEvidenceDocIDs ports load_evidence_doc_ids: question_id -> evidence
// document ids, read from the questions file. Returns an empty mapping (with
// a warning) when the file cannot be read: the leaderboard export then falls
// back to whatever the answer rows carry.
func loadEvidenceDocIDs(path string) map[string][]string {
	questions, err := loadQuestions(path)
	if err != nil {
		fmt.Printf("[leaderboard] could not read evidence documents from %s: %v\n", path, err)
		return map[string][]string{}
	}
	out := make(map[string][]string, len(questions))
	for _, q := range questions {
		out[q.questionID] = uniqueDocIDs(toAnySlice(q.evidenceDocs))
	}
	return out
}

func firstValue(payload map[string]any, keys []string) any {
	for _, key := range keys {
		if v, ok := payload[key]; ok && v != nil && v != "" {
			return v
		}
	}
	return nil
}

func firstDocList(payload map[string]any, keys []string) []string {
	for _, key := range keys {
		if docs := asDocIDList(payload[key]); len(docs) > 0 {
			return docs
		}
	}
	return nil
}

func normalizeQuestion(payload map[string]any, fallbackID any) *question {
	return &question{
		questionID:     strAny(payload, "question_id", firstNonEmpty(idValue(payload, idKeys), fmt.Sprintf("%v", fallbackID))),
		question:       idValue(payload, questionKeys),
		goldAnswer:     idValue(payload, goldKeys),
		reasoningTypes: asStringList(firstValue(payload, reasoningTypeKeys)),
		expectedDocs:   firstDocList(payload, expectedDocKeys),
		evidenceDocs:   firstNonEmptySlice(firstDocList(payload, evidenceDocKeys), firstDocList(payload, expectedDocKeys)),
	}
}

func idValue(payload map[string]any, keys []string) string {
	if v := firstValue(payload, keys); v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonEmptySlice(slices ...[]string) []string {
	for _, s := range slices {
		if len(s) > 0 {
			return s
		}
	}
	return nil
}

// selectQuestions ports select_questions: apply the config's question_ids
// block. include and random are alternative strategies (include wins); exclude
// is applied last in all cases; for the random strategy the sample is drawn
// from the pool after exclusion. The random seed is the current epoch second:
// every run draws a fresh sample by design, reproducibility comes from
// question_ids.include instead.
func selectQuestions(questions []*question, selection map[string]any) ([]*question, error) {
	var include []string
	if list, ok := selection["include"].([]any); ok {
		for _, id := range list {
			if s := strings.TrimSpace(fmt.Sprintf("%v", id)); s != "" {
				include = append(include, s)
			}
		}
	}
	randomCount := asIntDefault(selection["random"], 0)
	excluded := map[string]bool{}
	if list, ok := selection["exclude"].([]any); ok {
		for _, id := range list {
			excluded[strings.TrimSpace(fmt.Sprintf("%v", id))] = true
		}
	}

	if len(include) > 0 {
		byID := make(map[string]*question, len(questions))
		for _, q := range questions {
			byID[q.questionID] = q
		}
		var missing []string
		for _, qid := range include {
			if _, ok := byID[qid]; !ok {
				missing = append(missing, qid)
			}
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("question_ids.include contains unknown id(s): %s", strings.Join(missing, ", "))
		}
		var selected []*question
		for _, qid := range include {
			if !excluded[qid] {
				selected = append(selected, byID[qid])
			}
		}
		return selected, nil
	}

	var pool []*question
	for _, q := range questions {
		if !excluded[q.questionID] {
			pool = append(pool, q)
		}
	}
	if randomCount <= 0 || randomCount >= len(pool) {
		return pool, nil
	}
	rng := rand.New(rand.NewSource(time.Now().Unix()))
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	return pool[:randomCount], nil
}

func questionsPathOf(cfg map[string]any) string {
	dataset, _ := cfg["dataset"].(map[string]any)
	for _, value := range []any{
		dataset["questions_path"], cfg["questions_path"],
		cfg["frames_questions_path"], cfg["frames_mapping_path"],
	} {
		if s, ok := value.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// readIDList ports _read_id_list: ids separated by commas, whitespace or
// newlines; `#` starts a comment. Used by question_ids.include_file, so a
// generated batch lives in its own reviewable file instead of inline in the
// JSON config.
func readIDList(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var ids []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.SplitN(line, "#", 2)[0]
		for _, part := range strings.Fields(strings.ReplaceAll(line, ",", " ")) {
			if part = strings.TrimSpace(part); part != "" {
				ids = append(ids, part)
			}
		}
	}
	return ids
}

// ---------------------------------------------------------------------------
// Row identity / dedup helpers (port of _run_id / _row_run_key / _dedupe_last)
// ---------------------------------------------------------------------------

func rowRunKey(row map[string]any) string {
	if runID := strings.TrimSpace(strAny(row, "run_id", "")); runID != "" {
		return runID
	}
	return strings.TrimSpace(strAny(row, "question_id", ""))
}

func dedupeLast(rows []map[string]any) []map[string]any {
	// Collapse a JSONL file to one row per run_id (the last one wins) - a
	// re-run question appends a fresh row that shadows the earlier failure.
	// First-seen order is preserved; keyless rows are kept verbatim.
	index := map[string]int{}
	var out []map[string]any
	for _, row := range rows {
		key := rowRunKey(row)
		if key == "" {
			out = append(out, row)
			continue
		}
		if i, ok := index[key]; ok {
			out[i] = row
			continue
		}
		index[key] = len(out)
		out = append(out, row)
	}
	return out
}

func parseIntOrDefault(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}

// The two run phases (port of QuotaBreaker, run_answer_phase /
// _answer_phase_once, run_judge_phase / _judge_phase_once and the resume
// mechanics: strip damaged rows / damaged judgements, in-process parking and
// the plan-wall wait).

// Provider plan/quota circuit breaker. A WALL is durable - once the plan is
// exhausted every remaining question would fail the same way, so the run must
// stop with its completed rows intact. But the gateway reports short TPM/RPM
// bursts with the SAME wording ("已达到 Token Plan 用量上限"), and with N
// questions in flight one burst produces N failures within seconds: aborting
// an 830-question run on that is wrong. So the breaker only aborts when the
// failures are SPREAD OUT; failures clustered inside QUOTA_BURST_WINDOW_SEC
// are a burst - pause and probe again. QUOTA_ABORT_TOTAL bounds the pauses so
// a genuine wall still ends the run instead of looping.
const (
	quotaAbortAfter     = 3
	quotaBurstWindowSec = 120
	quotaBurstPauseSec  = 90
	quotaAbortTotal     = 12
	wallRetryWaitSec    = 600
	wallMaxWaits        = 0
	wallWaitSliceSec    = 30
	wallWaitLogEverySec = 300
	errorRetryWaitSec   = 600
)

type quotaBreaker struct {
	mu       sync.Mutex
	failedAt []time.Time
	total    int
}

// note ports QuotaBreaker.note: every phase outcome is fed to it; a non-quota
// outcome clears the history. Returns "" (keep going), "burst" (pause and
// probe again) or "wall" (the plan is exhausted: the caller waits and
// retries).
func (b *quotaBreaker) note(errText string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !strings.Contains(errText, "Token Plan") {
		b.failedAt = nil
		b.total = 0
		return ""
	}
	now := time.Now()
	b.total++
	// Keep EVERY failure since the last non-quota row (no age-based dropping:
	// filtering by the window would make the span test below unreachable).
	b.failedAt = append(b.failedAt, now)
	if b.total >= quotaAbortTotal {
		return "wall"
	}
	if len(b.failedAt) < quotaAbortAfter {
		return ""
	}
	if b.failedAt[len(b.failedAt)-1].Sub(b.failedAt[0]) >= quotaBurstWindowSec*time.Second {
		return "wall"
	}
	// Clustered failures: a burst, not a wall. Drop the history so the next
	// probe starts clean.
	b.failedAt = nil
	return "burst"
}

// waitForPlanRefill ports _wait_for_plan_refill: sleep out a provider plan
// wall before the next retry round, sliced so the log keeps proving the
// process is parked on purpose (a 30-minute silence is indistinguishable from
// a hang).
func waitForPlanRefill(label string, wait time.Duration, roundNo, maxWaits int) {
	limit := "unlimited"
	if maxWaits > 0 {
		limit = fmt.Sprintf("at most %d wait(s)", maxWaits)
	}
	fmt.Printf("[%s] provider plan exhausted - waiting %ds before retry round %d (%s); every finished row/verdict stays on disk\n",
		label, int(wait.Seconds()), roundNo, limit)
	remaining := wait
	slept := time.Duration(0)
	nextNotice := wallWaitLogEverySec * time.Second
	for remaining > 0 {
		slice := wallWaitSliceSec * time.Second
		if slice > remaining {
			slice = remaining
		}
		time.Sleep(slice)
		slept += slice
		remaining -= slice
		if remaining > 0 && slept >= nextNotice {
			fmt.Printf("[%s] still waiting: %ds left before retry round %d\n", label, int(remaining.Seconds()), roundNo)
			nextNotice += wallWaitLogEverySec * time.Second
		}
	}
}

// stripDamagedRows ports _strip_damaged_rows: drop rows a resume should
// re-run, keep the ones it must not. A row whose ragflow_error is TRANSIENT
// is dropped (the resume re-runs it and appends a fresh row). A row carrying
// a PERSISTENT error - the provider refused the input itself (content
// policy), so every retry fails identically - is KEPT and treated as settled
// (q744: 5+ identical failures across resumes). Unparseable rows are dropped.
// A timestamped backup is written beside the file before the rewrite; a file
// with nothing to strip is left untouched. Returns the number of rows removed.
func stripDamagedRows(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	rawLines := strings.Split(string(raw), "\n")
	var kept []string
	damaged := 0
	for _, line := range rawLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil || row == nil {
			damaged++
			continue
		}
		errText := strings.TrimSpace(strAny(row, "ragflow_error", ""))
		if errText != "" && !isPersistentRagflowError(errText) {
			damaged++
			continue
		}
		kept = append(kept, line)
	}
	if damaged == 0 {
		return 0
	}
	backup := path + fmt.Sprintf(".bak_strip_%s", time.Now().Format("0102_150405"))
	os.WriteFile(backup, []byte(strings.Join(rawLines, "\n")+"\n"), 0o644)
	os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o644)
	return damaged
}

// stripDamagedJudgements ports _strip_damaged_judgements: drop judgements
// standing on an answer row that carries a ragflow_error. The answer phase
// re-runs damaged rows, so a judgement recorded while the row was damaged is
// stale state. A timestamped backup is written beside the file first.
func stripDamagedJudgements(leaderboardPath string, rows []map[string]any) int {
	if !fileExists(leaderboardPath) {
		return 0
	}
	data, err := os.ReadFile(leaderboardPath)
	if err != nil {
		fmt.Printf("[judge] could not read %s: %v\n", leaderboardPath, err)
		return 0
	}
	var leaderboard map[string]any
	if err := json.Unmarshal(data, &leaderboard); err != nil {
		fmt.Printf("[judge] could not read %s: %v\n", leaderboardPath, err)
		return 0
	}
	records, _ := leaderboard["per_query_judgements"].([]any)
	if records == nil {
		return 0
	}
	damagedKeys := map[string]bool{}
	for _, row := range rows {
		if errText := strings.TrimSpace(strAny(row, "ragflow_error", "")); errText != "" {
			if key := rowRunKey(row); key != "" {
				damagedKeys[key] = true
			}
		}
	}
	var kept []any
	for _, recordAny := range records {
		record, ok := recordAny.(map[string]any)
		if !ok || damagedKeys[strAny(record, "run_key", "")] {
			continue
		}
		kept = append(kept, record)
	}
	removed := len(records) - len(kept)
	if removed == 0 {
		return 0
	}
	backup := leaderboardPath + fmt.Sprintf(".bak_strip_%s", time.Now().Format("0102_150405"))
	os.WriteFile(backup, data, 0o644)
	leaderboard["per_query_judgements"] = kept
	writeJSON(leaderboardPath, leaderboard)
	return removed
}

// runAnswerPhase ports run_answer_phase: answer every pending question,
// riding out an exhausted plan in process. Returns true only when a bounded
// wait budget gave up.
func runAnswerPhase(c *httpClient, cfg map[string]any, questions []*question, answersPath string, concurrency int, wallWait time.Duration, wallMaxWaits int) bool {
	roundNo, waits := 0, 0
	for {
		roundNo++
		if roundNo > 1 {
			fmt.Printf("[answers] retry round %d (after %d plan-wall wait(s))\n", roundNo, waits)
		}
		aborted := answerPhaseOnce(c, cfg, questions, answersPath, concurrency)
		if !aborted {
			if waits > 0 {
				fmt.Println("[answers] plan recovered after", waits, "wait(s); no questions left")
			}
			return false
		}
		if wallMaxWaits > 0 && waits >= wallMaxWaits {
			fmt.Printf("[answers] giving up after %d plan-wall wait(s) (--wall-max-waits %d); %s holds every finished row\n",
				waits, wallMaxWaits, answersPath)
			return true
		}
		waits++
		waitForPlanRefill("answers", wallWait, roundNo+1, wallMaxWaits)
	}
}

// answerPhaseOnce ports _answer_phase_once: one dispatch round. Returns true
// when the quota breaker saw a wall.
func answerPhaseOnce(c *httpClient, cfg map[string]any, questions []*question, answersPath string, concurrency int) bool {
	if stripped := stripDamagedRows(answersPath); stripped > 0 {
		fmt.Printf("[answers] resume: stripped %d damaged row(s) (transient ragflow_error); backup kept beside the file\n", stripped)
	}
	// A row counts as done when it is clean OR when its error is PERSISTENT -
	// the provider refused the input itself, and a retry would only reproduce
	// the refusal.
	completed := map[string]bool{}
	for key, row := range readJSONLByID(answersPath) {
		errText := strings.TrimSpace(strAny(row, "ragflow_error", ""))
		if errText == "" || isPersistentRagflowError(errText) {
			completed[key] = true
		}
	}

	chatCfg, _ := cfg["ragflow_chat"].(map[string]any)
	freshPerQuestion := true
	if v, ok := chatCfg["fresh_session_per_question"].(bool); ok {
		freshPerQuestion = v
	}
	sharedOK := !freshPerQuestion && concurrency == 1
	sharedSessionID := ""
	if sharedOK {
		id, err := createSession(c, strAny(cfg, "chat_id", ""))
		if err != nil {
			fmt.Printf("[answers] %v\n", err)
			return false
		}
		sharedSessionID = id
	} else if concurrency > 1 && !freshPerQuestion {
		fmt.Println("[answers] concurrency > 1 forces fresh_session_per_question (a shared session's history would interleave)")
	}

	// seq/total count the PENDING questions, not the batch: a resumed run must
	// not print "52/36" (batch position over pending total).
	type job struct {
		seq int
		q   *question
		rid string
	}
	var jobs []job
	for i, q := range questions {
		// The run id is UNIQUE PER SELECTION SLOT, not the question id. With
		// rid == questionID a selection that repeats a question (a repeat
		// experiment, a re-asked question) wrote several rows under ONE key and
		// dedupeLast collapsed them to the last one BEFORE judging - measured
		// 2026-10-05 on a q11 x5 run: five attempts ran, one row was judged, and
		// the leaderboard printed "Accuracy 100%" off a 1/1 denominator while the
		// true rate was 3/5. The number is the SELECTION POSITION, not the
		// pending-job counter, so a resumed run reproduces the same ids and the
		// resume check below still matches; question_id keeps the bare id for
		// scoring and grouping.
		rid := fmt.Sprintf("%s#%d", q.questionID, i+1)
		if !completed[rid] {
			jobs = append(jobs, job{len(jobs) + 1, q, rid})
		}
	}
	if skipped := len(questions) - len(jobs); skipped > 0 {
		fmt.Printf("[answers] skip %d existing row(s)\n", skipped)
	}
	if len(jobs) == 0 {
		return false
	}
	total := len(jobs)
	started := time.Now()
	breaker := &quotaBreaker{}
	var writeMu sync.Mutex

	answerOne := func(j job) map[string]any {
		row := map[string]any{
			"run_id":             j.rid,
			"question_id":        j.q.questionID,
			"question":           j.q.question,
			"gold_answer":        j.q.goldAnswer,
			"reasoning_types":    toAnySlice(j.q.reasoningTypes),
			"ragflow_answer":     "",
			"ragflow_error":      "",
			"ragflow_session_id": nil,
		}
		if len(j.q.expectedDocs) > 0 {
			row["expected_doc_ids"] = toAnySlice(uniqueDocIDs(toAnySlice(j.q.expectedDocs)))
		}
		if len(j.q.evidenceDocs) > 0 {
			row["evidence_doc_ids"] = toAnySlice(uniqueDocIDs(toAnySlice(j.q.evidenceDocs)))
		}
		payload, err := askRagflow(c, cfg, j.q.question, sharedSessionID)
		if err == nil {
			answer := extractAnswerText(payload)
			row["ragflow_answer"] = answer
			if sid := extractSessionID(payload); sid != "" {
				row["ragflow_session_id"] = sid
			}
			for k, v := range extractRunStats(payload) {
				row[k] = v
			}
			if answer == "" {
				row["ragflow_error"] = "empty_ragflow_answer"
			} else if strings.HasPrefix(strings.TrimLeft(answer, " \t\n"), "**ERROR**") {
				row["ragflow_error"] = strings.TrimSpace(answer)
			}
		} else {
			row["ragflow_error"] = err.Error()
		}
		// Whether the run ever SAW the documents that carry the answer, in the
		// three grades the backend records: `surfaced` (a retrieval result
		// named it, snippet-only locators included), `served`/opened (a
		// full-content tool pulled it: this is what Recall scores against),
		// and `cited` (the deliverable's own lines name it - a doc can surface
		// a dozen times and still never be opened). Without this triple a
		// retrieval miss is indistinguishable from a reasoning failure, and
		// the two need opposite fixes: measured 2026-10-05, q1093 rows read
		// "6 documents retrieved" while the same run had served 270, seven of
		// them carrying the answer's own series.
		if len(j.q.expectedDocs) > 0 {
			expected := map[string]bool{}
			for _, d := range j.q.expectedDocs {
				expected[d] = true
			}
			servedSet := map[string]bool{}
			for _, d := range asDocIDList(row["retrieved_docids"]) {
				servedSet[d] = true
			}
			surfacedSet := map[string]bool{}
			for _, d := range asDocIDList(row["served_docids"]) {
				surfacedSet[d] = true
			}
			var served, surfaced, cited []string
			for d := range expected {
				if servedSet[d] {
					served = append(served, d)
				}
				// A backend that predates served_docids reports only the
				// opened set; falling back keeps the field populated (and
				// understated) rather than empty and unreadable.
				if surfacedSet[d] || servedSet[d] {
					surfaced = append(surfaced, d)
				}
				if strings.Contains(strAny(row, "ragflow_answer", ""), d) {
					cited = append(cited, d)
				}
			}
			sortStrings(served)
			sortStrings(surfaced)
			sortStrings(cited)
			row["gold_doc_served"] = toAnySlice(served)
			row["gold_doc_surfaced"] = toAnySlice(surfaced)
			row["gold_doc_cited"] = toAnySlice(cited)
		}
		status := strAny(row, "ragflow_error", "")
		if status == "" {
			status = fmt.Sprintf("%d chars", len(strAny(row, "ragflow_answer", "")))
		}
		fmt.Printf("[answers] %d/%d %s: %s\n", j.seq, total, j.rid, status)
		return row
	}

	handle, err := os.OpenFile(answersPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Printf("[answers] could not open %s: %v\n", answersPath, err)
		return false
	}
	defer handle.Close()

	runOne := func(j job) bool {
		// A TRANSIENT failure does not produce a row: the question parks for
		// ERROR_RETRY_WAIT_SEC and retries in process, for as long as it
		// takes - the provider being down is a property of the clock, not of
		// the question. A PERSISTENT failure (content policy) is terminal and
		// recorded.
		startedOne := time.Now()
		attempt := 0
		var row map[string]any
		for {
			row = answerOne(j)
			errText := strings.TrimSpace(strAny(row, "ragflow_error", ""))
			if errText == "" || isPersistentRagflowError(errText) {
				break
			}
			attempt++
			fmt.Printf("[answers] %d/%d %s: retryable error (attempt %d) - %.160s; parking %ds, no row recorded\n",
				j.seq, total, j.rid, attempt, errText, errorRetryWaitSec)
			time.Sleep(errorRetryWaitSec * time.Second)
		}
		row["answer_elapsed_seconds"] = round3(time.Since(startedOne).Seconds())
		line, _ := json.Marshal(row)
		writeMu.Lock()
		handle.Write(append(line, '\n'))
		handle.Sync()
		verdict := breaker.note(strAny(row, "ragflow_error", ""))
		writeMu.Unlock()
		switch verdict {
		case "burst":
			fmt.Printf("[answers] Token Plan failures clustered inside %ds - rate-limit burst, not an exhausted plan: pausing %ds and probing again\n",
				quotaBurstWindowSec, quotaBurstPauseSec)
			time.Sleep(quotaBurstPauseSec * time.Second)
		case "wall":
			fmt.Printf("[answers] provider plan quota exhausted (%d Token Plan failures, spread over >= %ds) - this round stops here; the question(s) without an answer are retried after the plan-wall wait.\n",
				breaker.total, quotaBurstWindowSec)
		}
		return verdict == "wall"
	}

	aborted := false
	if concurrency <= 1 {
		for _, j := range jobs {
			if runOne(j) {
				aborted = true
				break
			}
		}
	} else {
		jobsCh := make(chan job)
		var wg sync.WaitGroup
		var mu sync.Mutex
		for w := 0; w < concurrency; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobsCh {
					if runOne(j) {
						mu.Lock()
						aborted = true
						mu.Unlock()
					}
				}
			}()
		}
		for _, j := range jobs {
			mu.Lock()
			stop := aborted
			mu.Unlock()
			if stop {
				break // queued-but-unstarted questions are not dispatched
			}
			jobsCh <- j
		}
		close(jobsCh)
		wg.Wait()
	}

	fmt.Printf("[answers] %d run(s) in %.1fs -> %s\n", total, time.Since(started).Seconds(), answersPath)
	return aborted
}

// runJudgePhase ports run_judge_phase: judge every row without a verdict,
// riding out an exhausted plan in process. Returns (judgements, gaveUp).
func runJudgePhase(c *httpClient, cfg map[string]any, answersPath, leaderboardPath string, concurrency int, wallWait time.Duration, wallMaxWaits int) (map[string]map[string]any, bool) {
	if judgeInstanceOverride(cfg) {
		fmt.Println("[judge] note: judge_llm_ids / judge_llm_id is IGNORED - the judge chat's own failover group picks the instance (naming one would pin the request and disable the group)")
	}
	roundNo, waits := 0, 0
	for {
		roundNo++
		if roundNo > 1 {
			fmt.Printf("[judge] retry round %d (after %d plan-wall wait(s))\n", roundNo, waits)
		}
		judgements, aborted := judgePhaseOnce(c, cfg, answersPath, leaderboardPath, concurrency)
		if !aborted {
			if waits > 0 {
				fmt.Println("[judge] plan recovered after", waits, "wait(s); no row left unjudged")
			}
			return judgements, false
		}
		if wallMaxWaits > 0 && waits >= wallMaxWaits {
			fmt.Printf("[judge] giving up after %d plan-wall wait(s) (--wall-max-waits %d); %s holds every stored verdict\n",
				waits, wallMaxWaits, leaderboardPath)
			return judgements, true
		}
		waits++
		waitForPlanRefill("judge", wallWait, roundNo+1, wallMaxWaits)
	}
}

// judgePhaseOnce ports _judge_phase_once: one judging round.
func judgePhaseOnce(c *httpClient, cfg map[string]any, answersPath, leaderboardPath string, concurrency int) (map[string]map[string]any, bool) {
	if !fileExists(answersPath) {
		fmt.Printf("[judge] missing answers file: %s\n", answersPath)
		return map[string]map[string]any{}, false
	}
	rows := dedupeLast(readJSONL(answersPath))
	if stripped := stripDamagedJudgements(leaderboardPath, rows); stripped > 0 {
		fmt.Printf("[judge] resume: stripped %d judgement(s) standing on damaged answer row(s); backup kept beside the file\n", stripped)
	}
	// Judging scope: the answer rows MINUS the rows still carrying a
	// ragflow_error (those are going to be re-run by the answer phase), MINUS
	// the questions already judged from an error-free row.
	var judgedRows []map[string]any
	for _, row := range rows {
		if strings.TrimSpace(strAny(row, "ragflow_error", "")) == "" {
			judgedRows = append(judgedRows, row)
		}
	}
	completed := map[string]bool{}
	for key, record := range readJudgements(leaderboardPath) {
		if !isBackendErrorVerdict(record) {
			completed[key] = true
		}
	}
	type jjob struct {
		seq int
		row map[string]any
	}
	var todo []jjob
	for _, row := range judgedRows {
		if !completed[rowRunKey(row)] {
			todo = append(todo, jjob{len(todo) + 1, row})
		}
	}
	if skipped := len(judgedRows) - len(todo); skipped > 0 {
		fmt.Printf("[judge] skip %d existing row(s)\n", skipped)
	}
	judgements := readJudgements(leaderboardPath)
	if len(todo) == 0 {
		return judgements, false
	}

	breaker := &quotaBreaker{}
	var writeMu sync.Mutex
	aborted := false

	finishJudge := func(j jjob) bool {
		// A TRANSIENT judge error does not persist a verdict: the row parks
		// for ERROR_RETRY_WAIT_SEC and re-judges in process. A PERSISTENT
		// error is recorded - it is re-judged after a resume.
		startedOne := time.Now()
		key := rowRunKey(j.row)
		attempt := 0
		var record map[string]any
		for {
			record = judgeOne(c, cfg, j.row)
			jerr := strings.TrimSpace(strAny(record, "judge_error", ""))
			if jerr == "" || isPersistentRagflowError(jerr) || jerr == "excluded_due_to_ragflow_error" {
				break
			}
			attempt++
			fmt.Printf("[judge] %d/%d %s: retryable judge error (attempt %d) - %.160s; parking %ds, no verdict recorded\n",
				j.seq, len(todo), key, attempt, jerr, errorRetryWaitSec)
			time.Sleep(errorRetryWaitSec * time.Second)
		}
		elapsed := round3(time.Since(startedOne).Seconds())
		record["query_id"] = strAny(j.row, "question_id", key)
		record["run_key"] = key
		record["judge_elapsed_seconds"] = elapsed

		writeMu.Lock()
		judgements[key] = record
		// One write per judged row: an aborted run keeps its completed
		// verdicts on disk, which is exactly what the resume logic reads.
		writeJSON(leaderboardPath, leaderboardWithJudgements(dedupeLast(readJSONL(answersPath)), judgements, cfg))
		verdict := breaker.note(strAny(record, "judge_error", ""))
		writeMu.Unlock()

		status := strAny(record, "judge_error", "")
		if status == "" {
			status = fmt.Sprintf("correct=%v", record["correct"])
		}
		fmt.Printf("[judge] %d/%d %s: %s (%.3fs)\n", j.seq, len(todo), key, status, elapsed)
		switch verdict {
		case "burst":
			fmt.Printf("[judge] Token Plan failures clustered inside %ds - rate-limit burst, not an exhausted plan: pausing %ds and probing again\n",
				quotaBurstWindowSec, quotaBurstPauseSec)
			time.Sleep(quotaBurstPauseSec * time.Second)
		case "wall":
			fmt.Printf("[judge] provider plan quota exhausted (%d Token Plan failures, spread over >= %ds) - this round stops here; the row(s) left without a verdict are re-judged after the plan-wall wait.\n",
				breaker.total, quotaBurstWindowSec)
		}
		return verdict == "wall"
	}

	if concurrency <= 1 {
		for _, j := range todo {
			if finishJudge(j) {
				aborted = true
				break
			}
		}
	} else {
		jobsCh := make(chan jjob)
		var wg sync.WaitGroup
		var mu sync.Mutex
		for w := 0; w < concurrency; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobsCh {
					if finishJudge(j) {
						mu.Lock()
						aborted = true
						mu.Unlock()
					}
				}
			}()
		}
		for _, j := range todo {
			mu.Lock()
			stop := aborted
			mu.Unlock()
			if stop {
				break
			}
			jobsCh <- j
		}
		close(jobsCh)
		wg.Wait()
	}

	fmt.Printf("[judge] %d judgement(s) -> %s\n", len(judgements), leaderboardPath)
	return judgements, aborted
}

// readJSONLByID ports _read_jsonl_by_id.
func readJSONLByID(path string) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, row := range readJSONL(path) {
		if key := rowRunKey(row); key != "" {
			out[key] = row
		}
	}
	return out
}

// countSettledJudgements ports the judged count of the [resume] line.
func countSettledJudgements(records map[string]map[string]any) int {
	n := 0
	for _, record := range records {
		if !isBackendErrorVerdict(record) {
			n++
		}
	}
	return n
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

var _ = filepath.Join // keep import if unused paths change

// Run-stats extraction (port of extract_run_stats and _llm_turn): read the
// run accounting the backend ships next to the answer. All of it is emitted
// by the Go backend on non-streaming agentic responses only; absent keys are
// left out rather than defaulted to zero, so a row can be flagged as "no
// accounting" instead of being scored as a run that made no calls.

func ioReadAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}

func llmTurn(turn map[string]any) map[string]any {
	prompt := asInt(turn["prompt_tokens"])
	completion := asInt(turn["completion_tokens"])
	total := asInt(turn["total_tokens"])
	if total == nil && prompt != nil && completion != nil {
		t := *prompt + *completion
		total = &t
	}
	model := ""
	if v, ok := turn["model"].(string); ok {
		model = v
	}
	return map[string]any{
		"seq":           asInt(turn["seq"]),
		"at_seconds":    asFloat(turn["at_seconds"]),
		"model":         nilIfEmpty(model),
		"input_tokens":  prompt,
		"output_tokens": completion,
		"total_tokens":  total,
	}
}

// extractRunStats ports extract_run_stats.
func extractRunStats(payload any) map[string]any {
	source, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	if _, hasDirect := source["tool_call_counts"]; !hasDirect {
		_, hasRetrieved := source["retrieved_docids"]
		_, hasServed := source["served_docids"]
		if !hasRetrieved && !hasServed {
			if data, ok := payload.(map[string]any)["data"].(map[string]any); ok {
				source = data
			}
		}
	}
	stats := map[string]any{}
	if counts, ok := source["tool_call_counts"].(map[string]any); ok {
		out := map[string]int{}
		for name, count := range counts {
			if n := asInt(count); n != nil {
				out[name] = *n
			}
		}
		stats["tool_call_counts"] = out
	}
	if errors, ok := source["tool_call_errors"].(map[string]any); ok {
		errCounts := map[string]int{}
		for name, count := range errors {
			if n := asInt(count); n != nil {
				errCounts[name] = *n
			}
		}
		if len(errCounts) > 0 {
			stats["tool_call_errors"] = errCounts
		}
	}
	if samples, ok := source["tool_error_samples"].(map[string]any); ok && len(samples) > 0 {
		out := map[string]string{}
		for name, text := range samples {
			out[name] = fmtAny(text)
		}
		stats["tool_error_samples"] = out
	}
	if docs, ok := source["retrieved_docids"].([]any); ok {
		stats["retrieved_docids"] = uniqueDocIDs(docs)
	}
	// Served = every document a retrieval result NAMED (snippet-only locators
	// included); retrieved = the subset the run opened with full content. Both
	// ride the response, and the row needs both: measured 2026-10-05, a run
	// reported 6 retrieved documents while its retrieval had served 270, and
	// every analysis of "was the answer's document ever in front of the model"
	// read the wrong one of the two.
	if docs, ok := source["served_docids"].([]any); ok {
		stats["served_docids"] = uniqueDocIDs(docs)
	}
	if usage, ok := source["usage"].(map[string]any); ok {
		out := map[string]any{}
		for _, key := range []string{"prompt_tokens", "completion_tokens", "total_tokens", "llm_calls"} {
			out[key] = asInt(usage[key])
		}
		stats["usage"] = out
		if turns, ok := usage["llm_turns"].([]any); ok && len(turns) > 0 {
			var ported []map[string]any
			for _, turnAny := range turns {
				if turn, ok := turnAny.(map[string]any); ok {
					ported = append(ported, llmTurn(turn))
				}
			}
			if len(ported) > 0 {
				stats["llm_turns"] = ported
			}
		}
	}
	// rounds is mode 6's per-round cost breakdown (kind/round, elapsed, tokens,
	// tool calls, documents, summary size, rubric verdict). Copied as a whole
	// rather than ported field by field: its shape belongs to the engine, no
	// other producer sets it, and a run-level total cannot answer "which round
	// spent the budget" — the question a run that hit its round cap raises.
	if rounds, ok := source["rounds"].([]any); ok && len(rounds) > 0 {
		stats["rounds"] = rounds
	}
	if elapsed := asFloat(source["elapsed_seconds"]); elapsed != nil {
		stats["server_elapsed_seconds"] = round3(*elapsed)
	}
	for _, key := range []string{"deep_read_chunks", "shallow_read_chunks", "compactions"} {
		if value := asInt(source[key]); value != nil {
			stats[key] = *value
		}
	}
	for _, key := range []string{"deep_read_chunk_ids", "shallow_read_chunk_ids"} {
		if value := uniqueIDs(source[key]); len(value) > 0 {
			stats[key] = value
		}
	}
	if gateAudit, ok := source["gate_audit"].(map[string]any); ok {
		suspects, hasSuspects := gateAudit["suspects"].([]any)
		rejections := asInt(gateAudit["rejections"])
		auditFailures := asInt(gateAudit["audit_failures"])
		verdicts, hasVerdicts := gateAudit["audit_verdicts"].([]any)
		citationGroundings := asInt(gateAudit["citation_groundings"])
		// Keep the record when ANY signal is present: a gate that refused
		// every deliverable before an audit could run reports no suspects at
		// all, and one whose auditor never returned a verdict reports nothing
		// but the failure - dropping either hid exactly that state
		// (q350/q784, and the audit-outage case).
		if hasSuspects || rejections != nil || auditFailures != nil || hasVerdicts || citationGroundings != nil {
			var verdictList []string
			if hasVerdicts {
				for _, v := range verdicts {
					verdictList = append(verdictList, fmtAny(v))
				}
			}
			stats["gate_audit"] = map[string]any{
				"suspects":            suspectList(suspects, hasSuspects),
				"passed":              boolOf(gateAudit["passed"]),
				"rejections":          rejections,
				"audit_failures":      auditFailures,
				"citation_groundings": citationGroundings,
				"audit_verdicts":      verdictListIf(hasVerdicts, verdictList),
			}
		}
	}
	return stats
}

func suspectList(list []any, ok bool) any {
	if !ok {
		return nil
	}
	out := make([]any, 0, len(list))
	for _, s := range list {
		out = append(out, asInt(s))
	}
	return out
}

func verdictListIf(ok bool, list []string) any {
	if !ok {
		return nil
	}
	return list
}

var _ = sort.Strings
var _ = fmt.Sprintf

// Structure metrics (port of _block_headers, _delivery_checks, structure_metrics,
// _candidate_keys, _prompt_signatures, _leakage_counts and friends): what the
// deliverable DECLARED, counted from its own markdown. The counts are
// reported, never scored.

const (
	earlyStopDocs = 10
	docTextLimit  = 400000
	// _DOC_TEXT_LIMIT caps how much of one corpus document is read for the
	// intermediate-node grounding check; a value's distinctive tokens are
	// almost always in the document's opening half.
)

var structureFields = []string{
	"blocks", "titles_anchored", "titles_use_vars", "slot", "kind", "op",
	"target_binds", "binds", "binds_unique", "from_lines", "constraints",
	"satisfied", "from_pointers", "searched", "tested", "eliminated",
	"retained", "derived", "failure", "reasoning_chain", "answer_label",
}

var deliveryTotalsFields = []string{
	"from_entries", "from_pointers", "from_entries_without_pointer",
	"from_pointer_mismatch", "from_unbound", "inlined_candidate",
	"depends_on_written", "searched_lines", "constraints_defined",
	"constraints_duplicated", "blocks_without_constraint",
	"blocks_without_searched", "blocks_with_by_name_query",
	"pivot_missing_blocks", "single_term_searches", "constraint_number_reused",
	"blocks_parsed", "blocks_total", "decomposition_detached_blocks",
	"decomposition_orphan_roots", "decomposition_from_none_after_first",
	"decomposition_prose_block_refs", "leaked_docnames", "leaked_values",
	"example_value_shipped",
}

// The whitespace runs are [^\S\n], never \s: \s* would swallow the newline
// before the heading too, so the slice of a block would begin with blank
// lines and its title would read as empty.
var (
	blockHeadRE    = regexp.MustCompile(`(?m)^[^\S\n]*#{2,4}[^\S\n]*Sub-question[^\S\n]+(\d+)[^\S\n]*:`)
	fromEntryRE    = regexp.MustCompile(`(\?[A-Za-z_][A-Za-z0-9_]*)\s*(?:\(block\s*(\d+)\))?`)
	candidateRE    = regexp.MustCompile(`(?m)^\s*-\s*(?:Tested|Eliminated|Retained):\s*(.*)$`)
	claimLineRE    = regexp.MustCompile(`^\s*-\s*(?:Tested|Eliminated|Retained|Failure):`)
	deemphasisRE   = regexp.MustCompile(`\*+`)
	proseBlockRE   = regexp.MustCompile(`(?i)\bblock\s*\d+\b|the above|previous sub-question|sub-question above`)
	dependsTailRE  = regexp.MustCompile(`(?i)[—\s-]+\s*depends_on\s*:.*$`)
	patternRE      = regexp.MustCompile("[(\"`]([^\"`)]+)")
	titleRE        = regexp.MustCompile(`(?m)^\s*#{2,4}\s*Sub-question\s+\d+\s*:(.*)$`)
	retainedLineRE = regexp.MustCompile(`(?m)^\s*-\s*Retained:\s*(.+)$`)
	derivedLineRE  = regexp.MustCompile(`(?m)^\s*-\s*Derived:\s*(.+)$`)
	failureTypeRE  = regexp.MustCompile(`(?m)^\s*-\s*Failure:\s*([A-Za-z_-]+)`)
	wordRE         = regexp.MustCompile(`[A-Za-z0-9'’-]+`)
	docNameRE      = regexp.MustCompile(`\b\d{3,}\.md\b`)
	exampleValueRE = regexp.MustCompile(`(?:Tested|Retained|Derived|Evidence):\s*([^—\n` + "`" + `/(]{4,80})`)
	valueTokenRE   = regexp.MustCompile(`[a-z0-9]+`)
	bindsNameRE    = regexp.MustCompile(`(?m)^\s*-\s*Binds:\s*([^\s(]+)`)
	slotTagRE      = regexp.MustCompile(`\s*[-–—]?\s*(?:slot|kind)\s*:.*$`)
	titlePrefixRE  = regexp.MustCompile(`^\s*#{2,4}\s*Sub-question\s+[0-9]+\s*:\s*`)
	answerWordRE   = regexp.MustCompile(`[A-Za-z][A-Za-z''’-]+`)
)

// fromEntry is one `From:` entry: its variable and its block pointer.
type fromEntry struct{ name, pointer string }

func splitBlocks(text string) []map[string]any {
	marks := blockHeadRE.FindAllStringSubmatchIndex(text, -1)
	var blocks []map[string]any
	for i, mark := range marks {
		end := len(text)
		if i+1 < len(marks) {
			end = marks[i+1][0]
		}
		body := text[mark[0]:end]
		lines := strings.Split(body, "\n")
		op, binds := "", ""
		hasFrom := false
		assertsCorpus := false
		var searched, constraints, constraintTexts []string
		var constraintDefs [][2]string
		var fromEntries []fromEntry
		for _, line := range lines[1:] {
			stripped := strings.TrimSpace(deemphasisRE.ReplaceAllString(line, ""))
			switch {
			case op == "" && strings.HasPrefix(stripped, "- Op:"):
				op = strings.TrimSpace(stripped[len("- Op:"):])
			case binds == "" && strings.HasPrefix(stripped, "- Binds:"):
				binds = strings.TrimSpace(stripped[len("- Binds:"):])
			case strings.HasPrefix(stripped, "- From:"):
				hasFrom = true
				for _, m := range fromEntryRE.FindAllStringSubmatch(stripped[len("- From:"):], -1) {
					fromEntries = append(fromEntries, fromEntry{m[1], m[2]})
				}
			case strings.HasPrefix(stripped, "- Constraints:"):
				constraintLine := dependsTailRE.ReplaceAllString(stripped, "")
				for _, m := range regexp.MustCompile(`\b(c\d+)\s*=\s*([^;]*)`).FindAllStringSubmatch(constraintLine, -1) {
					constraintDefs = append(constraintDefs, [2]string{m[1], strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(m[2], " "))})
				}
				for _, def := range constraintDefs {
					constraints = append(constraints, def[0])
				}
				constraintTexts = append(constraintTexts, constraintLine)
			case strings.HasPrefix(stripped, "- Searched:"):
				searched = append(searched, strings.TrimSpace(stripped[len("- Searched:"):]))
			case claimLineRE.MatchString(stripped):
				assertsCorpus = true
			}
		}
		var candidates []string
		for _, m := range candidateRE.FindAllStringSubmatch(deemphasisRE.ReplaceAllString(body, ""), -1) {
			if head := lineHead(m[1]); head != "" {
				candidates = append(candidates, head)
			}
		}
		title := ""
		if len(lines) > 0 {
			title = strings.TrimSpace(deemphasisRE.ReplaceAllString(lines[0], ""))
		}
		number := mark[1]
		blocks = append(blocks, map[string]any{
			"number": number, "title": title, "op": op, "binds": binds,
			"has_from_line": hasFrom, "from_entries": fromEntries,
			"candidates": candidates, "searched": searched,
			"constraints": constraints, "constraint_defs": constraintDefs,
			"constraint_texts": constraintTexts, "asserts_corpus": assertsCorpus,
		})
	}
	return blocks
}

// candidateKeys ports _candidate_keys: keys under which one candidate counts
// as searched by name. The head is reduced to its own tokens: the whole
// string when it is short, plus every two-word run inside it.
func candidateKeys(head string) []string {
	words := wordRE.FindAllString(strings.ToLower(head), -1)
	var keys []string
	if len(words) >= 2 && len(words) <= 4 {
		keys = append(keys, strings.Join(words, " "))
	}
	for i := 0; i+1 < len(words); i++ {
		keys = append(keys, words[i]+" "+words[i+1])
	}
	var out []string
	for _, key := range keys {
		if len(key) >= 6 {
			out = append(out, key)
		}
	}
	return out
}

// lineHead ports _line_head: the candidate name at the head of a matrix
// line, before its fields.
func lineHead(line string) string {
	cut := len(line)
	lowered := strings.ToLower(line)
	for _, separator := range []string{"—", " - ", "satisfied:", "(doc:", "doc:", "(source:"} {
		if index := strings.Index(lowered, strings.ToLower(separator)); index >= 0 && index < cut {
			cut = index
		}
	}
	head := strings.TrimSpace(line[:cut])
	return strings.Trim(head, "`*\" ")
}

var promptPathEnv = "BENCHMARK_PROMPT_PATH"

// promptSignatures ports _prompt_signatures: the names and document ids the
// PROMPT itself prints in its examples. A canary, not a judgement.
func promptSignatures(path string) map[string]any {
	docnames := map[string]bool{}
	values := map[string]bool{}
	loaded := false
	if path == "" {
		path = os.Getenv(promptPathEnv)
	}
	if path == "" {
		// Walk up from the working directory looking for conf/agentic_rag.yaml
		// (the Python original resolved it from the script's own location).
		wd, _ := os.Getwd()
		dir := wd
		for i := 0; i < 6; i++ {
			candidate := filepath.Join(dir, "conf", "agentic_rag.yaml")
			if fileExists(candidate) {
				path = candidate
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			loaded = true
			for _, name := range docNameRE.FindAllString(string(raw), -1) {
				docnames[name] = true
			}
			for _, match := range exampleValueRE.FindAllStringSubmatch(string(raw), -1) {
				value := strings.Trim(strings.TrimSpace(match[1]), "\"")
				// Only names can leak: a lowercase phrase is schema vocabulary
				// a delivery is SUPPOSED to repeat.
				uppercase := regexp.MustCompile(`[A-Z]`).FindAllString(value, -1)
				if len(value) >= 6 && !strings.HasPrefix(value, "<") && len(uppercase) >= 2 {
					values[value] = true
				}
			}
		}
	}
	return map[string]any{"docnames": docnames, "values": values, "loaded": loaded}
}

// leakageCounts ports _leakage_counts: whether the deliverable reused
// anything the PROMPT prints in its examples.
func leakageCounts(text string, signatures map[string]any) map[string]int {
	if signatures == nil {
		signatures = promptSignatures("")
	}
	docnames := signatures["docnames"].(map[string]bool)
	values := signatures["values"].(map[string]bool)
	answer := regexp.MustCompile(`(?im)^.*(?:Final|Guessed)\s+Answer.*$`).FindAllString(text, -1)
	answerLower := strings.ToLower(strings.Join(answer, "\n"))
	lowered := strings.ToLower(text)
	leakedValues, shipped := 0, 0
	for value := range values {
		needle := strings.ToLower(value)
		if count := strings.Count(lowered, needle); count > 0 {
			leakedValues += count
			if strings.Contains(answerLower, needle) {
				shipped = 1
			}
		}
	}
	leakedDocnames := 0
	for docname := range docnames {
		if strings.Contains(lowered, strings.ToLower(docname)) {
			leakedDocnames++
		}
	}
	return map[string]int{
		"leaked_docnames":       leakedDocnames,
		"leaked_values":         leakedValues,
		"example_value_shipped": shipped,
	}
}

// titleNamesAnchor ports _title_names_anchor: whether a sub-question title
// names an anchor of its own (a number, a capitalised name inside the
// sentence, or a distinctive term). A bare slot tag has none.
func titleNamesAnchor(title string) bool {
	text := titlePrefixRE.ReplaceAllString(title, "")
	text = strings.TrimSpace(slotTagRE.ReplaceAllString(text, ""))
	if regexp.MustCompile(`[0-9]`).MatchString(text) {
		return true
	}
	words := answerWordRE.FindAllString(text, -1)
	if len(words) < 3 {
		return false
	}
	for _, word := range words[1:] {
		if len(word) > 0 && word[:1] != strings.ToLower(word[:1]) {
			return true
		}
	}
	return false
}

// intermediateValues ports _intermediate_values: the entity values a run
// committed to mid-chain (retained candidates and the first clause of every
// Derived line), deduplicated.
func intermediateValues(text string) []string {
	var values []string
	for _, line := range retainedLineRE.FindAllStringSubmatch(text, -1) {
		candidate := lineHead(line[1])
		if candidate != "" && !strings.EqualFold(candidate, "none") {
			values = append(values, candidate)
		}
	}
	for _, line := range derivedLineRE.FindAllStringSubmatch(text, -1) {
		head := strings.TrimSpace(strings.Split(line[1], ";")[0])
		if len(head) >= 3 && len(head) <= 60 {
			values = append(values, head)
		}
	}
	seen := map[string]bool{}
	var unique []string
	for _, value := range values {
		key := strings.ToLower(value)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, value)
		}
	}
	return unique
}

// valueTokens ports _value_tokens: the distinctive tokens of a value (>=4
// chars, not a pure number).
func valueTokens(value string) []string {
	var tokens []string
	for _, token := range valueTokenRE.FindAllString(strings.ToLower(value), -1) {
		if len(token) >= 4 && !isAllDigits(token) {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// docText ports _doc_text: one corpus document's lower-cased text, cached
// across questions.
func docText(docID, corpusDir string, cache map[string]string) string {
	if text, ok := cache[docID]; ok {
		return text
	}
	name := docID
	if !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	text := ""
	if data, err := os.ReadFile(filepath.Join(corpusDir, name)); err == nil {
		if len(data) > docTextLimit {
			data = data[:docTextLimit]
		}
		text = strings.ToLower(string(data))
	}
	cache[docID] = text
	return text
}

// groundValues ports _ground_values: count how many committed values occur in
// the documents the run retrieved. Returns (grounded, checked).
func groundValues(values []string, docIDs []string, corpusDir string, cache map[string]string) (int, int) {
	if corpusDir == "" || len(values) == 0 || len(docIDs) == 0 {
		return 0, 0
	}
	var union strings.Builder
	for _, docID := range docIDs {
		union.WriteString(docText(docID, corpusDir, cache))
	}
	if union.Len() == 0 {
		return 0, 0
	}
	text := union.String()
	grounded, checked := 0, 0
	for _, value := range values {
		tokens := valueTokens(value)
		if len(tokens) == 0 {
			continue
		}
		checked++
		all := true
		for _, token := range tokens {
			if !strings.Contains(text, token) {
				all = false
				break
			}
		}
		if all {
			grounded++
		}
	}
	return grounded, checked
}

var _ = sort.Strings

// structureMetrics ports structure_metrics: count the structure a
// deliverable DECLARED, from its own text. Every counter is a regex over the
// markdown the producer shipped, so the result is reproducible from
// answers.jsonl alone.
func structureMetrics(answer, questionText string, signatures map[string]any) map[string]any {
	text := thinkRE.ReplaceAllString(answer, "")
	// Field prefixes are matched on a de-emphasised copy: the bolded-header
	// delivery of 2026-09-23 read as zero adopted fields otherwise.
	plain := deemphasisRE.ReplaceAllString(text, "")
	count := func(pattern string) int { return len(regexp.MustCompile(`(?m)`+pattern).FindAllString(plain, -1)) }
	titles := titleRE.FindAllStringSubmatch(text, -1)
	bindsNames := bindsNameRE.FindAllStringSubmatch(plain, -1)
	uniqueBinds := map[string]bool{}
	for _, m := range bindsNames {
		uniqueBinds[strings.ToLower(m[1])] = true
	}
	metrics := map[string]any{
		"blocks":             len(titles),
		"slot":               countPrefix(titles, `slot:\s*[A-Za-z_\[\]]`),
		"titles_anchored":    countTitles(titles, titleNamesAnchor),
		"titles_use_vars":    countTitles(titles, func(t string) bool { return strings.Contains(t, "?") }),
		"kind":               countPrefix(titles, `kind:\s*\S`),
		"op":                 count(`^\s*-\s*Op:\s*[A-Za-z_]`),
		"target_binds":       count(`^\s*-\s*Binds:\s*\?answer\b`),
		"binds":              count(`^\s*-\s*Binds:\s*`),
		"binds_unique":       boolToInt(len(uniqueBinds) == len(bindsNames) && len(bindsNames) > 0),
		"from_lines":         count(`^\s*-\s*From:\s*`),
		"constraints":        count(`^\s*-\s*Constraints:\s*`),
		"satisfied":          count(`satisfied:\s*\[`),
		"depends_on_written": count(`depends_on\s*:`),
		"searched":           count(`^\s*-\s*Searched:\s*`),
		"tested":             count(`^\s*-\s*Tested:\s*`),
		"eliminated":         count(`^\s*-\s*Eliminated:\s*`),
		"retained":           count(`^\s*-\s*Retained:\s*`),
		"derived":            count(`^\s*-\s*Derived:\s*`),
		"failure":            count(`^\s*-\s*Failure:\s*`),
		"reasoning_chain":    boolToInt(regexp.MustCompile(`(?m)^\s*#{1,4}\s*Reasoning Chain\b`).MatchString(text)),
		"answer_label":       boolToInt(regexp.MustCompile(`(?i)\b(?:Final|Guessed)\s+Answer\b`).MatchString(text)),
	}
	var failureTypes []string
	seenTypes := map[string]bool{}
	for _, m := range failureTypeRE.FindAllStringSubmatch(text, -1) {
		t := strings.ToLower(m[1])
		if !seenTypes[t] {
			seenTypes[t] = true
			failureTypes = append(failureTypes, t)
		}
	}
	sort.Strings(failureTypes)
	metrics["failure_types"] = failureTypes
	metrics["values"] = intermediateValues(text)
	for k, v := range deliveryChecks(text, questionText, signatures) {
		metrics[k] = v
	}
	return metrics
}

func countPrefix(titles [][]string, pattern string) int {
	re := regexp.MustCompile(pattern)
	n := 0
	for _, m := range titles {
		if re.MatchString(m[1]) {
			n++
		}
	}
	return n
}

func countTitles(titles [][]string, predicate func(string) bool) int {
	n := 0
	for _, m := range titles {
		if predicate(m[1]) {
			n++
		}
	}
	return n
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// deliveryChecks ports _delivery_checks: counts for the delivery questions
// that CAN be answered mechanically. Each is membership, not phrasing - and
// they are counted HERE, not enforced in the gate. Read blocks_parsed beside
// every count: a count taken over an unparsed block is not evidence of
// compliance.
func deliveryChecks(text, questionText string, signatures map[string]any) map[string]int {
	blocks := splitBlocks(text)
	bindings := map[string]int{}
	for _, block := range blocks {
		name := strings.ToLower(strings.TrimSpace(strAny(block, "binds", "")))
		if strings.HasPrefix(name, "?") {
			if _, seen := bindings[name]; !seen {
				bindings[name] = strToInt(block["number"])
			}
		}
	}
	// The decomposition is ONE chain: union the blocks each edge joins, then
	// read whether every block belongs to the component that carries ?answer.
	union := map[int]int{}
	for _, block := range blocks {
		union[strToInt(block["number"])] = strToInt(block["number"])
	}
	var unionRoot func(x int) int
	unionRoot = func(x int) int {
		for union[x] != x {
			union[x] = union[union[x]]
			x = union[x]
		}
		return x
	}
	pointers, withoutPointer, mismatch, unbound, inlined := 0, 0, 0, 0, 0
	parsed, searchedLines, withoutSearched, withByName := 0, 0, 0, 0
	pivotMissing, singleTerm, withoutConstraint, fromNoneAfterFirst := 0, 0, 0, 0
	proseRefs := 0
	defined := map[string]int{}
	definitions := map[string]map[string]bool{}
	questionLower := strings.ToLower(questionText)
	for _, block := range blocks {
		title := strAny(block, "title", "")
		number := strToInt(block["number"])
		op := strAny(block, "op", "")
		binds := strAny(block, "binds", "")
		hasFrom := boolOf(block["has_from_line"])
		searched := block["searched"].([]string)
		if op != "" && binds != "" && hasFrom && regexp.MustCompile(`slot:\s*\S`).MatchString(title) {
			parsed++
		}
		if number > 1 && len(block["from_entries"].([]fromEntry)) == 0 {
			// `From` names what a block consumes; the contract allows `none`
			// in the FIRST block only.
			fromNoneAfterFirst++
		}
		proseRefs += len(proseBlockRE.FindAllString(title, -1))
		for _, line := range block["constraint_texts"].([]string) {
			proseRefs += len(proseBlockRE.FindAllString(line, -1))
		}
		for _, entry := range block["from_entries"].([]fromEntry) {
			producer, bound := bindings[strings.ToLower(entry.name)]
			if bound {
				union[unionRoot(number)] = unionRoot(producer)
			}
			if entry.pointer == "" {
				withoutPointer++
				continue
			}
			pointers++
			if !bound {
				unbound++
			} else if producer != parseIntOrDefault(entry.pointer, -1) {
				mismatch++
			}
		}
		for _, def := range block["constraint_defs"].([][2]string) {
			defined[def[0]]++
			if definitions[def[0]] == nil {
				definitions[def[0]] = map[string]bool{}
			}
			definitions[def[0]][strings.ToLower(def[1])] = true
		}
		if len(block["constraints"].([]string)) == 0 {
			withoutConstraint++
		}
		searchedLines += len(searched)
		if boolOf(block["asserts_corpus"]) && len(searched) == 0 {
			withoutSearched++
		}
		recorded := strings.ToLower(strings.Join(searched, " || "))
		candidates := block["candidates"].([]string)
		hasByName := false
		for _, candidate := range candidates {
			for _, key := range candidateKeys(candidate) {
				if strings.Contains(recorded, key) {
					hasByName = true
				}
			}
		}
		switch {
		case hasByName:
			withByName++
		case len(candidates) > 0:
			// A block that tests or retains a candidate owes a query on that
			// candidate's own name; without it, it verified none of them.
			pivotMissing++
		}
		for _, raw := range searched {
			if m := patternRE.FindStringSubmatch(raw); m != nil {
				if words := regexp.MustCompile(`[A-Za-z0-9']+`).FindAllString(m[1], -1); len(words) > 0 && len(words) <= 3 {
					singleTerm++
				}
			}
		}
		for _, candidate := range candidates {
			lowered := strings.ToLower(candidate)
			// A name the QUESTION itself supplies is not an inlined finding.
			if len(lowered) >= 3 && strings.Contains(strings.ToLower(title), lowered) && !strings.Contains(questionLower, lowered) {
				inlined++
			}
		}
	}
	answerBlock, hasAnswerBlock := bindings["?answer"]
	detached := 0
	if hasAnswerBlock {
		main := unionRoot(answerBlock)
		for _, block := range blocks {
			if unionRoot(strToInt(block["number"])) != main {
				detached++
			}
		}
	}
	// A root (`From: none`) is lawful; an ORPHAN ROOT whose variable no block
	// consumes is a second, disconnected question. ?answer is exempt.
	consumedNames := map[string]bool{}
	for _, block := range blocks {
		for _, entry := range block["from_entries"].([]fromEntry) {
			consumedNames[strings.ToLower(entry.name)] = true
		}
	}
	orphanRoots := 0
	for _, block := range blocks {
		binds := strings.ToLower(strings.TrimSpace(strAny(block, "binds", "")))
		if len(block["from_entries"].([]fromEntry)) == 0 && binds != "" && !consumedNames[binds] && binds != "?answer" {
			orphanRoots++
		}
	}
	definedTotal := 0
	for _, count := range defined {
		definedTotal += count
	}
	duplicated := 0
	for _, count := range defined {
		if count > 1 {
			duplicated++
		}
	}
	reused := 0
	for _, texts := range definitions {
		if len(texts) > 1 {
			reused++
		}
	}
	out := map[string]int{
		"decomposition_detached_blocks":       detached,
		"decomposition_orphan_roots":          orphanRoots,
		"decomposition_from_none_after_first": fromNoneAfterFirst,
		"decomposition_prose_block_refs":      proseRefs,
		"from_pointers":                       pointers,
		"from_entries":                        pointers + withoutPointer,
		"from_entries_without_pointer":        withoutPointer,
		"from_pointer_mismatch":               mismatch,
		"from_unbound":                        unbound,
		"inlined_candidate":                   inlined,
		"searched_lines":                      searchedLines,
		"constraints_defined":                 definedTotal,
		"constraints_duplicated":              duplicated,
		"constraint_number_reused":            reused,
		"blocks_without_constraint":           withoutConstraint,
		"blocks_without_searched":             withoutSearched,
		"blocks_with_by_name_query":           withByName,
		"pivot_missing_blocks":                pivotMissing,
		"single_term_searches":                singleTerm,
		"blocks_parsed":                       parsed,
		"blocks_total":                        len(blocks),
	}
	for k, v := range leakageCounts(text, signatures) {
		out[k] = v
	}
	return out
}

func strToInt(v any) int { return asIntDefault(v, 0) }

// Delivery-gate accounting helpers (port of _gate_audit_* and
// _unrecorded_locate_tools).

var locateTools = []string{"grep_chunks", "search_bm25_chunks", "search_semantic_chunks", "search_chunks"}

var searchedLineRE = regexp.MustCompile(`(?im)Searched:\s*(grep_chunks|search_bm25_chunks|search_semantic_chunks|search_chunks)`)

func gateAuditSuspects(row map[string]any) any {
	audit, _ := row["gate_audit"].(map[string]any)
	if audit == nil {
		return nil
	}
	list, ok := audit["suspects"].([]any)
	if !ok {
		return nil
	}
	out := make([]any, 0, len(list))
	for _, s := range list {
		out = append(out, s)
	}
	return out
}

func gateAuditPassed(row map[string]any) any {
	audit, _ := row["gate_audit"].(map[string]any)
	if audit == nil {
		return nil
	}
	if _, ok := audit["passed"]; !ok {
		return nil
	}
	return boolOf(audit["passed"])
}

func gateAuditRejections(row map[string]any) any {
	audit, _ := row["gate_audit"].(map[string]any)
	if audit == nil {
		return nil
	}
	return audit["rejections"]
}

func gateAuditFailures(row map[string]any) any {
	audit, _ := row["gate_audit"].(map[string]any)
	if audit == nil {
		return nil
	}
	return audit["audit_failures"]
}

func gateAuditCitationGroundings(row map[string]any) any {
	audit, _ := row["gate_audit"].(map[string]any)
	if audit == nil {
		return nil
	}
	return audit["citation_groundings"]
}

func gateAuditVerdicts(row map[string]any) any {
	audit, _ := row["gate_audit"].(map[string]any)
	if audit == nil {
		return nil
	}
	verdicts, ok := audit["audit_verdicts"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, v := range verdicts {
		out = append(out, strings.TrimSpace(fmtAny(v)))
	}
	return out
}

// unrecordedLocateTools ports _unrecorded_locate_tools: locate tools the run
// called but the Candidate Matrix never credits on a `Searched:` line.
// Reported, never scored: measured on 703 scored rows, 69% of runs omit at
// least one, so it is the norm rather than an anomaly.
func unrecordedLocateTools(row map[string]any) any {
	counts, ok := row["tool_call_counts"].(map[string]any)
	if !ok {
		return nil
	}
	listed := map[string]bool{}
	for _, m := range searchedLineRE.FindAllStringSubmatch(strAny(row, "ragflow_answer", ""), -1) {
		listed[strings.ToLower(m[1])] = true
	}
	var missing []string
	for _, tool := range locateTools {
		if asIntDefault(counts[tool], 0) > 0 && !listed[tool] {
			missing = append(missing, tool)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return missing
}

// usageRow ports _usage_row: per-question token and time cost, straight from
// the backend's run accounting plus the client-side wall clock.
func usageRow(queryID string, row map[string]any, searchTools []string) map[string]any {
	usage, _ := row["usage"].(map[string]any)
	if usage == nil {
		usage = map[string]any{}
	}
	counts, _ := row["tool_call_counts"].(map[string]any)
	if counts == nil {
		counts = map[string]any{}
	}
	promptTokens := asInt(usage["prompt_tokens"])
	completionTokens := asInt(usage["completion_tokens"])
	totalTokens := asInt(usage["total_tokens"])
	if totalTokens == nil && promptTokens != nil && completionTokens != nil {
		t := *promptTokens + *completionTokens
		totalTokens = &t
	}
	searchCalls := 0
	for _, name := range searchTools {
		searchCalls += asIntDefault(counts[name], 0)
	}
	toolCalls := map[string]int{}
	for name, count := range counts {
		toolCalls[name] = asIntDefault(count, 0)
	}
	var retrievedCount any
	if retrieved, ok := row["retrieved_docids"].([]any); ok {
		retrievedCount = len(retrieved)
	}
	return map[string]any{
		"query_id":                queryID,
		"input_tokens":            promptTokens,
		"output_tokens":           completionTokens,
		"total_tokens":            totalTokens,
		"llm_calls":               asInt(usage["llm_calls"]),
		"llm_turns":               row["llm_turns"],
		"server_seconds":          asFloat(row["server_elapsed_seconds"]),
		"client_seconds":          asFloat(row["answer_elapsed_seconds"]),
		"search_calls":            searchCalls,
		"tool_calls":              toolCalls,
		"retrieved_doc_count":     retrievedCount,
		"audit_suspects":          gateAuditSuspects(row),
		"audit_passed":            gateAuditPassed(row),
		"audit_rejections":        gateAuditRejections(row),
		"audit_failures":          gateAuditFailures(row),
		"citation_groundings":     gateAuditCitationGroundings(row),
		"audit_verdicts":          gateAuditVerdicts(row),
		"unrecorded_locate_tools": unrecordedLocateTools(row),
		"deep_read_chunks":        asInt(row["deep_read_chunks"]),
		"shallow_read_chunks":     asInt(row["shallow_read_chunks"]),
		"compactions":             asInt(row["compactions"]),
	}
}

// Judge scoring, verdict parsing and the leaderboard builder (port of
// parse_grader_verdict, _judge_one, _merge_judgements, _usage_row,
// calibration_error, build_leaderboard and _leaderboard_summary).

// ---------------------------------------------------------------------------
// Grader verdict parsing (port of parse_grader_verdict)
// ---------------------------------------------------------------------------

// Patterns mirror parse_judge_response() in the reference evaluation script:
// a model may or may not bold the label, so each field is tried in its bold,
// colon-outside-bold, and plain forms before giving up. Go's RE2 has no
// lookahead, so the "up to end of line" semantics become (?m) + `.*`.
var (
	graderAnswerPatterns = []string{
		`(?im)\*\*extracted_final_answer:\*\*[ \t]*(.*)`,
		`(?im)\*\*extracted_final_answer\*\*:[ \t]*(.*)`,
		`(?im)extracted_final_answer:[ \t]*(.*)`,
	}
	graderCorrectPatterns = []string{
		`(?i)\*\*correct:\*\*\s*(yes|no)`,
		`(?i)\*\*correct\*\*:\s*(yes|no)`,
		`(?i)correct:\s*(yes|no)`,
	}
	graderConfidencePatterns = []string{
		`(?i)\*\*confidence:\*\*\s*(\d+(?:\.\d+)?)\s*%?`,
		`(?i)\*\*confidence\*\*:\s*(\d+(?:\.\d+)?)\s*%?`,
		`(?i)confidence:\s*(\d+(?:\.\d+)?)\s*%?`,
	}
)

var graderReasoningLabel = regexp.MustCompile(`(?is)\*\*reasoning:\*\*|\*\*reasoning\*\*:|reasoning:`)
var graderReasoningStop = regexp.MustCompile(`(?is)\n\*\*correct:\*\*|\n\*\*correct\*\*:|\ncorrect:`)

// graderField ports _grader_field for the line-scoped fields.
func graderField(text string, patterns []string) string {
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if m := re.FindStringSubmatch(text); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// graderReasoning ports the reasoning field: from the reasoning label to the
// next correct: line (or the end of the text).
func graderReasoning(text string) string {
	loc := graderReasoningLabel.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	rest := text[loc[1]:]
	if stop := graderReasoningStop.FindStringIndex(rest); stop != nil {
		rest = rest[:stop[0]]
	}
	return strings.TrimSpace(rest)
}

// parseGraderVerdict ports parse_grader_verdict. Raises (returns an error)
// when `correct` is missing: the leaderboard's accuracy is a boolean and a
// judge reply without one is a failed judgement, never an implicit "no".
func parseGraderVerdict(text string) (map[string]any, error) {
	cleaned := thinkRE.ReplaceAllString(text, "")
	rawCorrect := graderField(cleaned, graderCorrectPatterns)
	if rawCorrect == "" {
		snippet := cleaned
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return nil, fmt.Errorf("could not parse judge correctness from: %s", snippet)
	}
	// The reference script reads the confidence the ANSWER states and defaults
	// to 100 when there is none, so an agent that never states one is scored
	// as maximally confident - the calibration error then equals its miss rate.
	confidence := 100.0
	if raw := graderField(cleaned, graderConfidencePatterns); raw != "" {
		if f := asFloat(raw); f != nil && *f < 100.0 {
			confidence = *f
		}
	}
	return map[string]any{
		"correct":                strings.EqualFold(rawCorrect, "yes"),
		"confidence":             confidence,
		"extracted_final_answer": nilIfEmpty(graderField(cleaned, graderAnswerPatterns)),
		"reasoning":              nilIfEmpty(graderReasoning(cleaned)),
	}, nil
}

func nilIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// judgeOne ports _judge_one: run the judge for one answer row and shape the
// judgement record.
func judgeOne(c *httpClient, cfg map[string]any, row map[string]any) map[string]any {
	record := map[string]any{
		"correct":                nil,
		"confidence":             nil,
		"extracted_final_answer": nil,
		"verdict":                nil,
		"judge_error":            "",
	}
	if strings.TrimSpace(strAny(row, "ragflow_error", "")) != "" {
		record["judge_error"] = "excluded_due_to_ragflow_error"
	} else {
		judgeAnswer := askJudgeWithRetry(c, cfg, row)
		verdict, err := parseGraderVerdict(judgeAnswer)
		if err != nil {
			record["judge_error"] = err.Error()
		} else {
			record["verdict"] = verdict
			record["correct"] = verdict["correct"]
			record["confidence"] = verdict["confidence"]
			record["extracted_final_answer"] = verdict["extracted_final_answer"]
		}
	}
	return record
}

// isBackendErrorVerdict ports _is_backend_error_verdict: True when the judge
// never produced a verdict (backend error bubble or unparseable verdict).
func isBackendErrorVerdict(record map[string]any) bool {
	if strings.HasPrefix(strings.TrimLeft(strAny(record, "judge_answer", ""), " \t\n"), "**ERROR**") {
		return true
	}
	return asFloat(record["accuracy"]) == nil && strings.TrimSpace(strAny(record, "judge_error", "")) != ""
}

// readJudgements ports _read_judgements: the per_query_judgements already
// stored in leaderboard.json, keyed by run key.
func readJudgements(leaderboardPath string) map[string]map[string]any {
	if !fileExists(leaderboardPath) {
		return map[string]map[string]any{}
	}
	data, err := os.ReadFile(leaderboardPath)
	if err != nil {
		fmt.Printf("[judge] could not read %s: %v\n", leaderboardPath, err)
		return map[string]map[string]any{}
	}
	var leaderboard map[string]any
	if err := json.Unmarshal(data, &leaderboard); err != nil {
		fmt.Printf("[judge] could not read %s: %v\n", leaderboardPath, err)
		return map[string]map[string]any{}
	}
	records, _ := leaderboard["per_query_judgements"].([]any)
	out := map[string]map[string]any{}
	for _, recordAny := range records {
		record, ok := recordAny.(map[string]any)
		if !ok {
			continue
		}
		if key := strAny(record, "run_key", ""); key != "" {
			out[key] = record
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Merge + leaderboard build
// ---------------------------------------------------------------------------

// mergeJudgements ports _merge_judgements: attach each row's judge verdict to
// the answer row (in memory) so the leaderboard builder can score it.
func mergeJudgements(rows []map[string]any, judgements map[string]map[string]any) []map[string]any {
	var merged []map[string]any
	for _, row := range rows {
		out := map[string]any{}
		for k, v := range row {
			out[k] = v
		}
		if record, ok := judgements[rowRunKey(row)]; ok {
			out["judge_correct"] = record["correct"]
			out["judge_confidence"] = record["confidence"]
			out["judge_error"] = strAny(record, "judge_error", "")
			out["judge_extracted_answer"] = record["extracted_final_answer"]
			out["judge_parsed"] = record["verdict"]
		}
		merged = append(merged, out)
	}
	return merged
}

// leaderboardWithJudgements ports _leaderboard_with_judgements.
func leaderboardWithJudgements(rows []map[string]any, judgements map[string]map[string]any, cfg map[string]any) map[string]any {
	return buildLeaderboard(mergeJudgements(rows, judgements), cfg, nil)
}

// leaderboardCorrect ports _leaderboard_correct: the binary verdict of a row,
// or nil when the row was never judged (and counts as incorrect).
func leaderboardCorrect(row map[string]any) *bool {
	if v, ok := row["judge_correct"]; ok && v != nil {
		b := boolOf(v)
		return &b
	}
	return nil
}

// confidencePercent ports _confidence_percent: normalize a judge confidence
// onto the reference script's 0-100 scale. Legacy values at or below 1 are
// read as fractions of 1.
func confidencePercent(v any) *float64 {
	numeric := asFloat(v)
	if numeric == nil {
		return nil
	}
	out := *numeric
	if out >= 0 && out <= 1 {
		out *= 100
	}
	if out > 100 {
		out = 100
	}
	return &out
}

func searchToolsOf(lbCfg map[string]any) []string {
	if list, ok := lbCfg["search_tools"].([]any); ok {
		var tools []string
		for _, name := range list {
			if s := strings.TrimSpace(fmt.Sprintf("%v", name)); s != "" {
				tools = append(tools, s)
			}
		}
		if len(tools) > 0 {
			return tools
		}
	}
	return []string{"search_chunks", "search_semantic_chunks", "grep_chunks", "search_bm25_chunks"}
}

// calibrationMinSamples: the reference script needs at least this many
// scored confidences, otherwise the calibration error is reported as 0.0.
const calibrationMinSamples = 100

func calibrationBinSizeOf(lbCfg map[string]any) int {
	return asIntDefault(lbCfg["calibration_bin_size"], 100)
}

// calibrationError ports calibration_error, line for line from the reference
// script's calib_err(). The loop is `range(len(bins) - 1)`, so the final bin
// is never scored - a bug in the reference script, reproduced on purpose to
// stay comparable with the published leaderboard entries. One difference is
// unavoidable and deliberate: this port sorts STABLY, where the reference's
// numpy argsort permutes confidence ties arbitrarily.
func calibrationError(confidences []float64, correctness []bool, beta int) float64 {
	if len(confidences) == 0 || len(confidences) != len(correctness) {
		return 0
	}
	type pair struct {
		conf float64
		ok   float64
	}
	pairs := make([]pair, len(confidences))
	for i, c := range confidences {
		pairs[i] = pair{c / 100.0, boolToFloat(correctness[i])}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].conf < pairs[j].conf })
	total := len(pairs)
	if beta <= 0 {
		beta = 100
	}
	binCount := total / beta
	if binCount == 0 {
		return 0
	}
	cerr := 0.0
	for i := 0; i < binCount-1; i++ {
		lo, hi := i*beta, (i+1)*beta
		size := hi - lo
		if size <= 0 {
			continue
		}
		sumConf, sumOK := 0.0, 0.0
		for _, p := range pairs[lo:hi] {
			sumConf += p.conf
			sumOK += p.ok
		}
		difference := math.Abs(sumConf/float64(size) - sumOK/float64(size))
		cerr += float64(size) / float64(total) * difference * difference
	}
	return math.Sqrt(cerr) * 100
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// buildLeaderboard ports build_leaderboard: the JSON the BrowseComp-Plus
// leaderboard asks for. Every formula mirrors the reference script, including
// its denominators: Accuracy divides by ALL questions of the run (a question
// the agent failed on counts as incorrect); Recall averages only over
// questions that have evidence documents; calibration needs >= 100 scored
// confidences.
func buildLeaderboard(rows []map[string]any, cfg map[string]any, evidenceByID map[string][]string) map[string]any {
	lbCfg, _ := cfg["leaderboard"].(map[string]any)
	if lbCfg == nil {
		lbCfg = map[string]any{}
	}
	searchTools := searchToolsOf(lbCfg)
	beta := calibrationBinSizeOf(lbCfg)

	total := len(rows)
	var perQueryMetrics []map[string]any
	var perQueryUsage []map[string]any
	var perQueryStructure []map[string]any
	var confidences []float64
	var correctness []bool
	var recalls []float64
	var recallsSurfaced []float64
	toolTotals := map[string]float64{}
	toolErrorTotals := map[string]float64{}
	toolErrorSamples := map[string]string{}
	statsMissing := 0
	judgedCount := 0
	var perQueryJudgements []map[string]any
	structureSeen := map[string]int{}
	failureTypeCounts := map[string]int{}
	earlyStopHits, earlyStopRows := 0, 0
	intermediateGrounded, intermediateChecked := 0, 0
	var corpusDir string
	if dataset, _ := cfg["dataset"].(map[string]any); dataset != nil {
		corpusDir = strAny(dataset, "corpus_path", "")
	}
	docTextCache := map[string]string{}
	deliveryTotals := map[string]int{}
	inlineRows, pointerRows, retiredRows, pivotRows := 0, 0, 0, 0
	unrecordedRows, partitionRows, reuseRows, leakRows, decompositionRows := 0, 0, 0, 0, 0
	promptSigs := promptSignatures("")
	signatureTotal := len(promptSigs["docnames"].(map[string]bool)) + len(promptSigs["values"].(map[string]bool))

	for _, row := range rows {
		queryID := strAny(row, "question_id", rowRunKey(row))
		correctRaw := leaderboardCorrect(row)
		// Anything the judge did not affirm is a miss: the reference script
		// counts a result whose judge_result.correct is absent (error, empty
		// answer, unparseable verdict) as not correct.
		correct := correctRaw != nil && *correctRaw
		if correctRaw != nil {
			judgedCount++
			if confidence := confidencePercent(row["judge_confidence"]); confidence != nil {
				confidences = append(confidences, *confidence)
				correctness = append(correctness, correct)
			}
		}

		evidence := uniqueDocIDs(toAnySlice(firstNonEmptySlice(evidenceByID[queryID], asDocIDList(row["evidence_doc_ids"]))))
		recall := (*float64)(nil)
		recallSurfaced := (*float64)(nil)
		if len(evidence) > 0 {
			retrievedSet := map[string]bool{}
			for _, d := range asDocIDList(row["retrieved_docids"]) {
				retrievedSet[d] = true
			}
			// Surfaced = named by ANY retrieval result (snippet-only locators
			// included), i.e. everything that was ever in front of the model.
			// Recall above scores the OPENED set; the pair separates "the
			// retrieval never found it" from "the model had it and never
			// opened it" - opposite failures behind the same miss.
			surfacedSet := map[string]bool{}
			for _, d := range asDocIDList(row["served_docids"]) {
				surfacedSet[d] = true
			}
			for _, d := range asDocIDList(row["retrieved_docids"]) {
				surfacedSet[d] = true
			}
			hits, hitsSurfaced := 0, 0
			for _, d := range evidence {
				if retrievedSet[d] {
					hits++
				}
				if surfacedSet[d] {
					hitsSurfaced++
				}
			}
			r := float64(hits) / float64(len(evidence))
			recall = &r
			recalls = append(recalls, r)
			rs := float64(hitsSurfaced) / float64(len(evidence))
			recallSurfaced = &rs
			recallsSurfaced = append(recallsSurfaced, rs)
		}
		perQueryMetrics = append(perQueryMetrics, map[string]any{
			"query_id":        queryID,
			"correct":         correct,
			"recall":          roundPercentNil(recall),
			"recall_surfaced": roundPercentNil(recallSurfaced),
		})

		if row["judge_correct"] != nil || row["judge_error"] != nil {
			perQueryJudgements = append(perQueryJudgements, map[string]any{
				"query_id":               queryID,
				"run_key":                rowRunKey(row),
				"correct":                correctRaw,
				"confidence":             confidencePercent(row["judge_confidence"]),
				"extracted_final_answer": row["judge_extracted_answer"],
				"verdict":                row["judge_parsed"],
				"judge_error":            strAny(row, "judge_error", ""),
			})
		}

		if counts, ok := row["tool_call_counts"].(map[string]any); ok {
			for name, count := range counts {
				if numeric := asFloat(count); numeric != nil {
					toolTotals[name] += *numeric
				}
			}
		} else if strings.TrimSpace(strAny(row, "ragflow_error", "")) == "" {
			// A row the backend never reported stats for is not "zero calls":
			// streaming runs and pre-instrumentation backends never see them.
			statsMissing++
		}
		if rowErrors, ok := row["tool_call_errors"].(map[string]any); ok {
			for name, count := range rowErrors {
				if numeric := asFloat(count); numeric != nil {
					toolErrorTotals[name] += *numeric
				}
			}
		}
		if rowSamples, ok := row["tool_error_samples"].(map[string]any); ok {
			for name, text := range rowSamples {
				if _, exists := toolErrorSamples[name]; !exists {
					toolErrorSamples[name] = fmt.Sprintf("%v", text)
				}
			}
		}

		perQueryUsage = append(perQueryUsage, usageRow(queryID, row, searchTools))

		// Structure: what the deliverable DECLARED, counted rather than judged.
		structure := structureMetrics(strAny(row, "ragflow_answer", ""), strAny(row, "question", ""), promptSigs)
		committedValues, _ := structure["values"].([]string)
		failureTypes, _ := structure["failure_types"].([]string)
		delete(structure, "values")
		delete(structure, "failure_types")
		entry := map[string]any{"query_id": queryID, "run_key": rowRunKey(row)}
		for k, v := range structure {
			entry[k] = v
		}
		perQueryStructure = append(perQueryStructure, entry)
		for _, field := range structureFields {
			if truthy(structure[field]) {
				structureSeen[field]++
			}
		}
		for _, ft := range failureTypes {
			failureTypeCounts[ft]++
		}
		if truthy(structure["inlined_candidate"]) {
			inlineRows++
		}
		if truthy(structure["from_entries_without_pointer"]) || truthy(structure["from_pointer_mismatch"]) {
			pointerRows++
		}
		if truthy(structure["depends_on_written"]) {
			retiredRows++
		}
		if truthy(structure["pivot_missing_blocks"]) {
			pivotRows++
		}
		if truthy(structure["blocks_without_searched"]) {
			unrecordedRows++
		}
		if truthy(structure["constraints_duplicated"]) {
			partitionRows++
		}
		if truthy(structure["constraint_number_reused"]) {
			reuseRows++
		}
		if truthy(structure["leaked_docnames"]) || truthy(structure["leaked_values"]) || truthy(structure["example_value_shipped"]) {
			leakRows++
		}
		if truthy(structure["decomposition_detached_blocks"]) || truthy(structure["decomposition_orphan_roots"]) || truthy(structure["decomposition_prose_block_refs"]) {
			decompositionRows++
		}
		for _, key := range deliveryTotalsFields {
			deliveryTotals[key] += asIntDefault(structure[key], 0)
		}

		// Early stop: a run that concluded inside a handful of retrieved
		// documents. Symptom, not score.
		if row["retrieved_docids"] != nil {
			earlyStopRows++
			if len(uniqueDocIDs(row["retrieved_docids"])) <= earlyStopDocs {
				earlyStopHits++
			}
		}

		// Intermediate nodes: did the entities the deliverable committed to
		// mid-chain come from the documents it actually retrieved?
		retrievedNow := uniqueDocIDs(row["retrieved_docids"])
		grounded, checked := groundValues(committedValues, retrievedNow, corpusDir, docTextCache)
		intermediateGrounded += grounded
		intermediateChecked += checked
	}

	correctCount := 0
	for _, entry := range perQueryMetrics {
		if boolOf(entry["correct"]) {
			correctCount++
		}
	}
	accuracyPercent := 0.0
	if total > 0 {
		accuracyPercent = round2(float64(correctCount) / float64(total) * 100)
	}
	judgedTotal, judgedOK := 0, 0
	for _, row := range rows {
		if row["judge_correct"] != nil {
			judgedTotal++
			if boolOf(row["judge_correct"]) {
				judgedOK++
			}
		}
	}
	recallPercent := any(nil)
	if len(recalls) > 0 {
		sum := 0.0
		for _, r := range recalls {
			sum += r
		}
		recallPercent = round2(sum / float64(len(recalls)) * 100)
	}
	// Same average over the SURFACED set: how often the evidence documents
	// were in front of the model at all, opened or not. Recall (%) minus this
	// is the share of evidence the run had in hand and did not read.
	surfacedRecallPercent := any(nil)
	if len(recallsSurfaced) > 0 {
		sum := 0.0
		for _, r := range recallsSurfaced {
			sum += r
		}
		surfacedRecallPercent = round2(sum / float64(len(recallsSurfaced)) * 100)
	}

	avgToolStats := map[string]float64{}
	if total > 0 {
		for name, count := range toolTotals {
			avgToolStats[name] = count / float64(total)
		}
	}
	searchCalls := 0.0
	for _, name := range searchTools {
		searchCalls += avgToolStats[name]
	}

	calibrationErrorPercent := 0.0
	calibrationNote := fmt.Sprintf("not computed: only %d scored confidence(s), need %d", len(confidences), calibrationMinSamples)
	if len(confidences) >= calibrationMinSamples {
		calibrationErrorPercent = round2(calibrationError(confidences, correctness, beta))
		calibrationNote = fmt.Sprintf("%d scored confidence(s), bin size %d", len(confidences), beta)
	} else {
		fmt.Printf("[leaderboard] calibration error %s\n", calibrationNote)
	}

	structureAdoption := map[string]float64{}
	for _, field := range structureFields {
		if total > 0 {
			structureAdoption[field] = round1(float64(structureSeen[field]) / float64(total) * 100)
		}
	}
	intermediateNote := "not computed: dataset.corpus_path is not configured, so no corpus text can back the check."
	if corpusDir != "" {
		intermediateNote = "A committed value counts as grounded when every distinctive token of it (>=4 chars) occurs in the union of the documents the run retrieved: the offline reading of whether the mid-chain entities the deliverable committed to came from the corpus it read. Values with no distinctive token are skipped, and a value that is grounded may still be the wrong one."
	}

	return map[string]any{
		"LLM":                   orDefault(strAny(lbCfg, "llm", ""), "change me when submitting"),
		"Retriever":             orDefault(strAny(lbCfg, "retriever", ""), "change me when submitting"),
		"Accuracy (%)":          accuracyPercent,
		"Recall (%)":            recallPercent,
		"Surfaced Recall (%)":   surfacedRecallPercent,
		"Search Calls":          round2(searchCalls),
		"Calibration Error (%)": calibrationErrorPercent,
		"Link":                  orDefault(strAny(lbCfg, "link", ""), "change me when submitting"),
		"Evaluation Date":       orDefault(strAny(lbCfg, "evaluation_date", ""), timeNowDate()),
		"avg_tool_stats":        sortedCopy(avgToolStats),
		"tool_error_stats":      sortedCopyF(toolErrorTotals),
		"tool_error_samples":    sortedCopyS(toolErrorSamples),
		"per_query_metrics":     perQueryMetrics,
		"per_query_usage":       perQueryUsage,
		"per_query_judgements":  perQueryJudgements,
		"per_query_structure":   perQueryStructure,
		"_diagnostics":          diagnosticsBlock(total, judgedCount, judgedOK, len(recalls), searchTools, calibrationNote, statsMissing, structureAdoption, failureTypeCounts, earlyStopHits, earlyStopRows, intermediateGrounded, intermediateChecked, intermediateNote, deliveryTotals, inlineRows, pointerRows, retiredRows, pivotRows, unrecordedRows, partitionRows, reuseRows, leakRows, decompositionRows, signatureTotal, promptSigs),
	}
}

func sortedCopy(m map[string]float64) map[string]float64  { return m }
func sortedCopyF(m map[string]float64) map[string]float64 { return m }
func sortedCopyS(m map[string]string) map[string]string   { return m }

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func truthy(v any) bool {
	switch x := v.(type) {
	case int:
		return x != 0
	case float64:
		return x != 0
	case bool:
		return x
	case string:
		return x != ""
	case []string:
		return len(x) > 0
	case nil:
		return false
	default:
		return false
	}
}

func timeNowDate() string { return timeNow().Format("2006-01-02") }

func roundPercentNil(v *float64) any {
	if v == nil {
		return nil
	}
	return round2(*v * 100)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// leaderboardSummary ports _leaderboard_summary: one-line-per-metric digest
// of the submission.
func leaderboardSummary(leaderboard map[string]any) string {
	usage, _ := leaderboard["per_query_usage"].([]map[string]any)
	inputTokens, outputTokens := 0, 0
	clientSeconds := 0.0
	deepChunks, shallowChunks := 0, 0
	for _, row := range usage {
		inputTokens += asIntDefault(row["input_tokens"], 0)
		outputTokens += asIntDefault(row["output_tokens"], 0)
		if f := asFloat(row["client_seconds"]); f != nil {
			clientSeconds += *f
		}
		deepChunks += asIntDefault(row["deep_read_chunks"], 0)
		shallowChunks += asIntDefault(row["shallow_read_chunks"], 0)
	}
	judgements, _ := leaderboard["per_query_judgements"].([]map[string]any)
	scored, judgedOK := 0, 0
	for _, j := range judgements {
		if j["correct"] != nil {
			scored++
			if boolOf(j["correct"]) {
				judgedOK++
			}
		}
	}
	unjudged := len(judgements) - scored
	judgedOnly := "n/a"
	if scored > 0 {
		judgedOnly = fmt.Sprintf("%.2f%%", float64(judgedOK)/float64(scored)*100)
	}
	lines := []string{
		"leaderboard:",
		fmt.Sprintf("  LLM              : %v", leaderboard["LLM"]),
		fmt.Sprintf("  Retriever        : %v", leaderboard["Retriever"]),
		fmt.Sprintf("  Accuracy (%%)     : %v", leaderboard["Accuracy (%)"]),
	}
	if scored > 0 {
		lines = append(lines, fmt.Sprintf("  Accuracy (judged): %s  (%d/%d scored rows; %d unjudged counted incorrect)", judgedOnly, judgedOK, scored, unjudged))
	} else {
		lines = append(lines, "  Accuracy (judged): "+judgedOnly)
	}
	lines = append(lines,
		fmt.Sprintf("  Recall (%%)       : %v", leaderboard["Recall (%)"]),
		fmt.Sprintf("  Surfaced Recall  : %v", leaderboard["Surfaced Recall (%)"]),
		fmt.Sprintf("  Search Calls     : %v", leaderboard["Search Calls"]),
		fmt.Sprintf("  Calibration (%%)  : %v", leaderboard["Calibration Error (%)"]),
		fmt.Sprintf("  avg_tool_stats   : %v", leaderboard["avg_tool_stats"]),
		fmt.Sprintf("  tool_errors      : %v", orNonNil(leaderboard["tool_error_stats"], "none")),
		fmt.Sprintf("  chunk reads      : deep=%d shallow=%d", deepChunks, shallowChunks),
		fmt.Sprintf("  tokens           : input=%d output=%d", inputTokens, outputTokens),
		fmt.Sprintf("  wall clock (s)   : %.1f", clientSeconds),
	)
	if samples, ok := leaderboard["tool_error_samples"].(map[string]string); ok {
		for name, text := range samples {
			lines = append(lines, fmt.Sprintf("  tool_error[%s]: %s", name, text))
		}
	}
	return strings.Join(lines, "\n")
}

func orNonNil(v any, def string) string {
	if v == nil {
		return def
	}
	return fmt.Sprintf("%v", v)
}

// The _diagnostics block of leaderboard.json (port of the tail of
// build_leaderboard): adoption percentages, early-stop symptom, intermediate
// grounding, and the delivery checks with their measured-never-enforced
// notes.

func diagnosticsBlock(total, judged, judgedOK, recallScored int, searchTools []string,
	calibrationNote string, statsMissing int, structureAdoption map[string]float64,
	failureTypeCounts map[string]int, earlyStopHits, earlyStopRows, intermediateGrounded,
	intermediateChecked int, intermediateNote string, deliveryTotals map[string]int,
	inlineRows, pointerRows, retiredRows, pivotRows, unrecordedRows, partitionRows,
	reuseRows, leakRows, decompositionRows int, signatureTotal int, promptSigs map[string]any,
) map[string]any {
	note := "Accuracy counts every question of the run; failed and unjudged rows count as incorrect."
	if statsMissing > 0 {
		note = fmt.Sprintf("%s Recall and Search Calls are understated: %d row(s) were answered by a backend that did not report tool_call_counts / retrieved_docids, and rows without accounting are scored as 'retrieved nothing'.", note, statsMissing)
	}
	failureTypes := map[string]int{}
	for k, v := range failureTypeCounts {
		failureTypes[k] = v
	}
	earlyStopPercent := any(nil)
	if earlyStopRows > 0 {
		earlyStopPercent = round1(float64(earlyStopHits) / float64(earlyStopRows) * 100)
	}
	intermediatePercent := any(nil)
	if intermediateChecked > 0 {
		intermediatePercent = round1(float64(intermediateGrounded) / float64(intermediateChecked) * 100)
	}
	judgedOnlyAccuracy := any(nil)
	if judged > 0 {
		judgedOnlyAccuracy = round2(float64(judgedOK) / float64(judged) * 100)
	}
	return map[string]any{
		"questions": total, "judged": judged,
		"unjudged_counted_incorrect":   total - judged,
		"judged_only_accuracy_percent": judgedOnlyAccuracy,
		"judged_only_denominator":      judged,
		"recall_scored":                recallScored,
		"search_tools":                 searchTools,
		"calibration":                  calibrationNote,
		"run_stats_missing":            statsMissing,
		"structure_adoption_percent":   structureAdoption,
		"failure_types":                failureTypes,
		"early_stop": map[string]any{
			"threshold_docs": earlyStopDocs, "rows": earlyStopRows,
			"rows_at_or_below": earlyStopHits, "percent": earlyStopPercent,
			"note": "A run that concludes inside a handful of retrieved documents is a SYMPTOM, not a score: finding the document and giving up look identical here, so read this beside Accuracy.",
		},
		"intermediate_nodes": map[string]any{
			"values_checked": intermediateChecked, "values_grounded": intermediateGrounded,
			"percent": intermediatePercent, "note": intermediateNote,
		},
		"delivery_checks": deliveryChecksBlock(total, deliveryTotals, inlineRows, pointerRows,
			retiredRows, pivotRows, unrecordedRows, partitionRows, reuseRows, leakRows,
			decompositionRows, signatureTotal, promptSigs),
		"note": note,
	}
}

var _ = sort.Strings

// deliveryChecksBlock: the delivery_checks subtree of _diagnostics, with the
// measured-never-enforced notes carried over verbatim.

func deliveryChecksBlock(total int, deliveryTotals map[string]int, inlineRows, pointerRows,
	retiredRows, pivotRows, unrecordedRows, partitionRows, reuseRows, leakRows,
	decompositionRows, signatureTotal int, promptSigs map[string]any) map[string]any {
	percent := any(nil)
	if deliveryTotals["blocks_total"] > 0 {
		percent = round1(float64(deliveryTotals["blocks_parsed"]) / float64(deliveryTotals["blocks_total"]) * 100)
	}
	return map[string]any{
		"rows": total,
		"inlined_candidate": map[string]any{
			"rows": inlineRows, "occurrences": deliveryTotals["inlined_candidate"],
			"note": "A candidate name written into a block's own title freezes a finding into the plan, so the same question decomposes differently on another run. Membership, not phrasing: the name comes from the block's own Tested/Eliminated/Retained lines.",
		},
		"from_pointer": map[string]any{
			"entries": deliveryTotals["from_entries"], "with_pointer": deliveryTotals["from_pointers"],
			"without_pointer": deliveryTotals["from_entries_without_pointer"],
			"mismatch":        deliveryTotals["from_pointer_mismatch"], "unbound": deliveryTotals["from_unbound"],
			"rows_with_a_defect": pointerRows,
			"note":               "Each `From: ?x (block N)` should name the block that binds ?x - a DERIVED pointer, checkable against Binds. `without_pointer` counts entries that carry no pointer, `mismatch` counts pointers naming another block, `unbound` counts names no block binds.",
		},
		"depends_on_written": map[string]any{
			"rows": retiredRows, "occurrences": deliveryTotals["depends_on_written"],
			"note": "The field is RETIRED (the chain is stated by From); a nonzero count is the old contract still being written, not an adoption.",
		},
		"searched_record": map[string]any{
			"lines": deliveryTotals["searched_lines"], "blocks_without_searched": deliveryTotals["blocks_without_searched"],
			"rows": unrecordedRows,
			"note": "A block that asserts anything about the corpus owes at least one recorded `Searched:` line; an unrecorded run is unauditable rather than clean.",
		},
		"pivot": map[string]any{
			"blocks_with_by_name_query": deliveryTotals["blocks_with_by_name_query"],
			"pivot_missing_blocks":      deliveryTotals["pivot_missing_blocks"],
			"rows_with_a_missing_pivot": pivotRows,
			"single_term_searches":      deliveryTotals["single_term_searches"],
			"note":                      "The by-name query strategy, counted from the deliverable's own `Searched` patterns against its own candidate names: a block with candidates and no by-name query verified none of them. `single_term_searches` is the crude proxy for the clue-anchor query (<=3 words in the pattern).",
		},
		"decomposition_graph": map[string]any{
			"rows_with_a_defect":    decompositionRows,
			"detached_blocks":       deliveryTotals["decomposition_detached_blocks"],
			"orphan_roots":          deliveryTotals["decomposition_orphan_roots"],
			"roots_after_the_first": deliveryTotals["decomposition_from_none_after_first"],
			"prose_block_refs":      deliveryTotals["decomposition_prose_block_refs"],
			"note":                  "The decomposition must be ONE connected acyclic graph that carries `?answer`. The defects are the two that break the graph: an ORPHAN ROOT whose variable no block consumes and a block outside the `?answer` component; plus one that breaks resolution: an edge written in prose instead of the variable. Counted, not enforced.",
		},
		"constraint_partition": map[string]any{
			"defined": deliveryTotals["constraints_defined"], "duplicated": deliveryTotals["constraints_duplicated"],
			"blocks_without_constraint": deliveryTotals["blocks_without_constraint"],
			"rows_with_a_duplicate":     partitionRows,
			"number_reused":             deliveryTotals["constraint_number_reused"],
			"rows_with_a_reused_number": reuseRows,
			"note":                      "The numbered constraints form a PARTITION: each `c<k>` is defined by exactly one block. Measured 2026-09-23: 26 of 186 definitions were duplicates.",
		},
		"prompt_leakage": map[string]any{
			"signature_total": signatureTotal, "signatures_loaded": boolOf(promptSigs["loaded"]),
			"rows": leakRows, "leaked_docnames": deliveryTotals["leaked_docnames"],
			"leaked_values":         deliveryTotals["leaked_values"],
			"example_value_shipped": deliveryTotals["example_value_shipped"],
			"note":                  "A CANARY over the prompt's own examples, not a judgement. `signature_total` counts what was looked for - read it beside the zeros.",
		},
		"parse_coverage": map[string]any{
			"blocks_parsed": deliveryTotals["blocks_parsed"], "blocks_total": deliveryTotals["blocks_total"],
			"percent": percent,
			"note":    "A block counts as parsed when its title carries `slot:` and it carries `Op:`, `Binds:` and `From:`. READ THIS BESIDE EVERY COUNT ABOVE: a count taken over an unparsed block is not evidence of compliance.",
		},
		"note": "Measured, never enforced: these checks are membership, not phrasing, so the gate COULD carry them - but a gate opinion costs a whole repair turn on every run, and prevalence is not measured yet. Collect the prevalence here first.",
	}
}
