//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

//go:build ee

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// executor carries the run's shared dependencies. It is the single writer (§8.1): the
// coordinator runs in one goroutine, workers only read a frozen snapshot.
type executor struct {
	cfg    config
	client *httpClient
	retry  retryPolicy
	out    *outputDir
	quiet  bool
}

func (e *executor) logf(format string, args ...any) {
	if e.quiet {
		return
	}
	fmt.Fprintf(os.Stderr, "[strategy-rsi] "+format+"\n", args...)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("strategy-rsi", flag.ContinueOnError)
	configPath := fs.String("config", "", "config JSON path")
	buildTS := fs.Bool("build-trainset", false, "build the frozen trainset from benchmark outputs and exit")
	topN := fs.Int("top-n", 150, "top-N questions when building the trainset")
	trainset := fs.String("trainset", "", "frozen trainset path (overrides config)")
	ids := fs.String("ids", "", "comma-separated ids: train on a subset (smoke test)")
	batchSize := fs.Int("batch-size", 0, "questions per batch")
	maxPasses := fs.Int("max-passes", -1, "max passes (0=unlimited)")
	concurrency := fs.Int("concurrency", 0, "in-batch workers")
	reasoning := fs.Int("reasoning", 0, "reasoning level (engine)")
	baseline := fs.Bool("baseline", false, "run the no-strategy baseline over the trainset and exit")
	output := fs.String("output", "", "output dir (overrides config)")
	resume := fs.Bool("resume", true, "resume from checkpoint")
	requireMode8 := fs.Bool("require-mode8", true, "abort when mode 8 is not actually in use")
	target := fs.Float64("target-accuracy", -1, "convergence target %")
	minDelta := fs.Float64("min-delta", -1, "convergence min improvement (pct points)")
	minCov := fs.Float64("min-coverage", -1, "convergence min coverage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Only flags the user EXPLICITLY set override the config.
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if set["trainset"] {
		cfg.Trainset = *trainset
	}
	if set["batch-size"] {
		cfg.BatchSize = *batchSize
	}
	if set["max-passes"] {
		cfg.MaxPasses = *maxPasses
	}
	if set["concurrency"] {
		cfg.Concurrency = *concurrency
	}
	if set["reasoning"] {
		cfg.Reasoning = *reasoning
	}
	if set["output"] {
		cfg.OutputDir = *output
	}
	if set["resume"] {
		cfg.Resume = *resume
	}
	if set["require-mode8"] {
		cfg.RequireMode8 = *requireMode8
	}
	if set["target-accuracy"] {
		cfg.TargetAccuracy = *target
	}
	if set["min-delta"] {
		cfg.MinDelta = *minDelta
	}
	if set["min-coverage"] {
		cfg.MinCoverage = *minCov
	}

	if *buildTS {
		meta, err := buildTrainset(cfg.BenchmarkOutputsDir, cfg.Dataset.QuestionsPath, *topN, cfg.Trainset)
		if err != nil {
			return err
		}
		fmt.Printf("built trainset: n=%d cutoff_wrong>=%d cutoff_seen>=%d runs=%d rows=%d\nsha256=%s\n%s\n",
			meta.TopN, meta.CutoffWrong, meta.CutoffSeen, meta.NRuns, meta.NRows, meta.SHA256, cfg.Trainset)
		return nil
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	recs, meta, err := loadTrainset(cfg.Trainset)
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		return fmt.Errorf("trainset %s is empty", cfg.Trainset)
	}
	if idList := splitCSV(*ids); len(idList) > 0 {
		recs = subsetByID(recs, idList)
		if len(recs) == 0 {
			return fmt.Errorf("--ids selected no trainset questions")
		}
	}

	e := &executor{
		cfg:    cfg,
		client: newHTTPClient(cfg),
		retry: retryPolicy{
			initial:   time.Duration(cfg.RetryInitialSec * float64(time.Second)),
			max:       time.Duration(cfg.RetryMaxSec * float64(time.Second)),
			jitter:    cfg.RetryJitter,
			quota:     time.Duration(cfg.RetryQuotaSec) * time.Second,
			exhausted: cfg.quotaExhaustedWait(),
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.RequireMode8 {
		if err := e.preflightMode8(ctx); err != nil {
			return fmt.Errorf("mode-8 preflight failed (aborting): %w", err)
		}
	}

	if *baseline {
		return e.runBaseline(ctx, recs, meta)
	}
	return e.runTraining(ctx, recs, meta)
}

// ---- training ----

func (e *executor) runTraining(ctx context.Context, recs []trainsetRecord, meta trainsetMeta) error {
	out, err := openOutputDir(e.cfg.OutputDir)
	if err != nil {
		return err
	}
	defer out.release()
	e.out = out
	idsHash := idsHashOf(recs)
	promptFP := promptFingerprint()

	cp := &checkpoint{Phase: phaseTrain, Pass: 1, NextBatchIndex: 0, Seed: e.cfg.Seed}
	fresh := true
	if prev, ok, err := loadCheckpoint(out.dir); err != nil {
		return err
	} else if ok && e.cfg.Resume {
		if err := validateResumeWith(prev, e.cfg, meta, idsHash, promptFP); err != nil {
			return err
		}
		cp = prev
		fresh = false
		e.logf("resuming pass=%d batch=%d version=%d phase=%s", cp.Pass, cp.NextBatchIndex, cp.Version, cp.Phase)
	}
	if fresh {
		bindRunIdentity(cp, e.cfg, meta, idsHash, promptFP)
		// Persist the (redacted) identity ONLY for a fresh run, and never overwrite an
		// existing one -- a failed resume must not damage the original metadata.
		if err := writeRunIdentity(out, e.cfg, idsHash, promptFP); err != nil {
			return err
		}
	}

	set := &strategySet{Version: cp.Version, NextID: cp.NextID, Items: cloneItems(cp.Items), ActiveIDs: append([]string(nil), cp.ActiveIDs...)}
	if set.NextID == 0 {
		set.seedNextID()
	}
	set.rank(e.cfg.MaxStrategies)

	// Backfill the strategies.jsonl snapshot for the resumed version: idempotent, and it
	// covers a run that predates the snapshot (or one whose snapshot write raced a crash).
	if !fresh && cp.Version > 0 {
		origin := fmt.Sprintf("backfill pass=%d batch=%d", cp.Pass, cp.NextBatchIndex)
		if err := e.appendStrategySnapshot(set, origin, cp.StrategiesFile); err != nil {
			return err
		}
	}

	n := len(recs)

	// Mid-evaluation resume.
	if cp.Phase == phaseEvaluate {
		res, err := e.evaluatePass(ctx, cp, set, recs)
		if err != nil {
			if isStop(err) {
				return e.writeLeaderboard(cp, set, false)
			}
			return fmt.Errorf("resumed frozen eval: %w", err)
		}
		if _, err := e.commitEval(cp, set, res); err != nil {
			return err
		}
		e.logf("resumed frozen eval pass %d: accuracy=%.1f%%", res.Pass, res.Accuracy)
		if e.converged(cp, res) {
			return e.writeLeaderboard(cp, set, true)
		}
	}

	for {
		if ctx.Err() != nil {
			e.logf("interrupted; checkpoint at pass=%d batch=%d (no forced evaluation)", cp.Pass, cp.NextBatchIndex)
			return e.writeLeaderboard(cp, set, false)
		}
		if e.cfg.MaxPasses > 0 && cp.Pass > e.cfg.MaxPasses {
			return e.writeLeaderboard(cp, set, false)
		}

		if cp.Phase == phasePassComplete {
			if cp.Evaluated && e.convergedCommitted(cp) {
				e.logf("converged (resumed at pass_complete)")
				return e.writeLeaderboard(cp, set, true)
			}
			cp.Pass++
			cp.NextBatchIndex = 0
			cp.Phase = phaseTrain
			cp.Evaluated = false
			if err := e.out.writeCheckpoint(cp); err != nil {
				return err
			}
			continue
		}

		order := passBatches(e.cfg.Seed, cp.Pass, n, e.cfg.BatchSize)
		for bi := cp.NextBatchIndex; bi < len(order); bi++ {
			if ctx.Err() != nil {
				return e.writeLeaderboard(cp, set, false)
			}
			bq := order[bi]
			// One attempt identity for EVERY artifact of this batch execution.
			att := newAttemptID()
			snap := batchSnapshot{Pass: cp.Pass, Index: bi, Version: set.Version, Strategies: cloneSet(set), Questions: bq}
			outcomes, complete := e.runBatch(ctx, snap, recs)
			if !complete {
				// Never commit a partial batch: unstarted questions would be silently lost.
				if ctx.Err() != nil {
					e.logf("batch %d.%d interrupted before completion; not committed", cp.Pass, bi)
					return e.writeLeaderboard(cp, set, false)
				}
				return fmt.Errorf("batch %d.%d did not complete", cp.Pass, bi)
			}
			// Defense in depth: a batch that failed as a whole for TRANSPORT reasons is an
			// infrastructure abort, not a measurement. Committing one advances the cursor past
			// questions that were never really asked -- a 30s API restart once "completed" ten
			// passes with zero scored questions this way. Wait the transport out and re-run.
			if batchAbortedByTransport(outcomes) {
				e.logf("batch %d.%d: all %d question(s) failed on transport errors; not committing, retrying",
					cp.Pass, bi, len(outcomes))
				if serr := e.retry.sleepClass(ctx, failNetwork, 0); serr != nil {
					return e.writeLeaderboard(cp, set, false)
				}
				bi-- // undo the loop's bi++ so the SAME batch runs again
				continue
			}

			next := cloneSet(set)
			used := map[string]bool{}
			for _, oc := range outcomes {
				for _, r := range oc.Ratings {
					next.addScore(r.StrategyID, r.Score)
					used[oc.QID+"|"+r.StrategyID] = true
				}
			}
			var lessons []lesson
			for _, oc := range outcomes {
				lessons = append(lessons, oc.Lessons...)
			}

			var ns *strategySet
			switch {
			case len(lessons) == 0 && !scoresChanged(set, next):
				ns = set
			case len(lessons) == 0:
				next.Version = set.Version + 1
				next.rank(e.cfg.MaxStrategies)
				ns = next
			default:
				merged, merr := e.mergeBatch(ctx, next, set, lessons, cp.Pass, bi, att, used)
				if merr != nil {
					if errors.Is(merr, errAudit) {
						return merr // a mandatory audit record failed: stop, do not advance
					}
					e.logf("batch %d.%d merge failed, discarding this batch's scores: %v", cp.Pass, bi, merr)
					ns = set
				} else {
					ns = merged
				}
			}
			set = ns
			set.rank(e.cfg.MaxStrategies)

			for _, oc := range outcomes {
				row := map[string]any{
					"attempt_id": att, "id": oc.QID, "pass": cp.Pass, "batch_index": bi, "qpos": oc.Index,
					"version": snap.Version, "correct": oc.Correct, "skipped": oc.Skipped,
					"timed_out": oc.TimedOut, "err": oc.Err,
					"sent_question": oc.SentQuestion, "answer": oc.Answer, "session_id": oc.SessionID,
					"lessons": oc.Lessons, "ratings": oc.Ratings,
				}
				if oc.Verdict != nil {
					row["verdict"] = oc.Verdict
				}
				if err := out.appendJSONL("state.jsonl", row); err != nil {
					return fmt.Errorf("state log write (refusing to advance cursor): %w", err)
				}
			}
			if err := out.appendJSONL("batches.jsonl", map[string]any{
				"attempt_id": att, "pass": cp.Pass, "batch_index": bi,
				"version_before": snap.Version, "version_after": set.Version,
				"n_questions": len(bq), "n_correct": countCorrect(outcomes), "n_lessons": len(lessons),
			}); err != nil {
				return fmt.Errorf("batch log write (refusing to advance cursor): %w", err)
			}
			if err := e.persist(cp, set, att, cp.Pass, bi+1, phaseTrain); err != nil {
				return err
			}
			e.logf("pass %d batch %d/%d: %d/%d correct, version=%d", cp.Pass, bi+1, len(order),
				countCorrect(outcomes), len(bq), set.Version)
		}

		if cp.Pass%e.cfg.EvalEveryKPasses == 0 {
			res, err := e.evaluatePass(ctx, cp, set, recs)
			if err != nil {
				if isStop(err) {
					e.logf("frozen eval pass %d interrupted; stays pending", cp.Pass)
					return e.writeLeaderboard(cp, set, false)
				}
				return fmt.Errorf("frozen eval pass %d: %w", cp.Pass, err)
			}
			if _, err := e.commitEval(cp, set, res); err != nil {
				return err
			}
			e.logf("frozen eval pass %d: accuracy=%.1f%% scored=%d skipped=%d coverage=%.2f",
				res.Pass, res.Accuracy, res.Scored, res.Skipped, res.Coverage)
			if e.converged(cp, res) {
				e.logf("converged at pass %d", res.Pass)
				return e.writeLeaderboard(cp, set, true)
			}
		} else {
			cp.Phase = phasePassComplete
			cp.Evaluated = false
			cp.NextBatchIndex = 0
			// Log first (idempotent per pass), then commit: a crash cannot lose the row.
			if err := out.appendJSONL("passes.jsonl", map[string]any{"pass": cp.Pass, "evaluated": false, "version": set.Version}); err != nil {
				return fmt.Errorf("passes log write (refusing to commit pass): %w", err)
			}
			if err := e.out.writeCheckpoint(cp); err != nil {
				return err
			}
		}
	}
}

// isStop reports whether an error is a normal stop (context cancellation) rather than a
// real failure that must surface as a non-zero exit.
func isStop(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// runBatch runs one batch's questions and reports whether EVERY position completed. A
// position left unstarted by a cancellation is NOT a result: the caller must not commit.
func (e *executor) runBatch(ctx context.Context, snap batchSnapshot, recs []trainsetRecord) ([]questionOutcome, bool) {
	out := make([]questionOutcome, len(snap.Questions))
	done := make([]bool, len(snap.Questions))
	sem := make(chan struct{}, e.cfg.Concurrency)
	var wg sync.WaitGroup
	for qpos, qi := range snap.Questions {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		if ctx.Err() != nil { // canceled while waiting for a slot
			<-sem
			break
		}
		wg.Add(1)
		go func(qpos, qi int) {
			defer wg.Done()
			defer func() { <-sem }()
			out[qpos] = e.runQuestion(ctx, snap, recs[qi], qpos)
			done[qpos] = true
		}(qpos, qi)
	}
	wg.Wait()
	for _, d := range done {
		if !d {
			return out, false
		}
	}
	return out, true
}

func countCorrect(o []questionOutcome) int {
	n := 0
	for _, oc := range o {
		if oc.Correct {
			n++
		}
	}
	return n
}

// batchAbortedByTransport reports whether a batch failed as a WHOLE for transport reasons:
// every question skipped and every skip a quota/network error. That is the API or the
// provider being unreachable -- infrastructure, not a measurement -- so the batch must not
// be banked. A batch whose skips are timeouts is a normal outcome (per §6.4) and commits as
// usual, which is why the class is checked rather than just the skip count.
func batchAbortedByTransport(o []questionOutcome) bool {
	if len(o) == 0 {
		return false
	}
	for _, oc := range o {
		if !oc.Skipped || strings.TrimSpace(oc.Err) == "" {
			return false
		}
		switch classify(errors.New(oc.Err)) {
		case failQuota, failNetwork:
		default:
			return false
		}
	}
	return true
}

func scoresChanged(a, b *strategySet) bool {
	if len(a.Items) != len(b.Items) {
		return true
	}
	for _, it := range b.Items {
		o := a.byID(it.ID)
		if o == nil || o.TotalScore != it.TotalScore || o.RatingCount != it.RatingCount {
			return true
		}
	}
	return false
}

// persist writes the versioned strategy file (only when the version changed), then
// atomically commits the checkpoint (results already appended; the cursor is last).
func (e *executor) persist(cp *checkpoint, set *strategySet, att attemptID, pass, nextBatch int, phase string) error {
	if set.Version != cp.Version || cp.StrategiesFile == "" {
		sfile := e.cfg.strategiesFileFor(set.Version)
		origin := fmt.Sprintf("pass=%d batch=%d", pass, nextBatch)
		if err := writeStrategyFile(e.out.path(sfile), set.renderFile(origin)); err != nil {
			return err
		}
		sha, err := fileSHA256(e.out.path(sfile))
		if err != nil {
			return err
		}
		// The strategies.jsonl snapshot is the human-readable trace of the pool; it is
		// written with the versioned file, before the checkpoint cursor moves.
		if err := e.appendStrategySnapshot(set, origin, sfile); err != nil {
			return err
		}
		cp.StrategiesFile = sfile
		cp.StrategiesSHA256 = sha
	}
	cp.Phase = phase
	cp.Version = set.Version
	cp.NextID = set.NextID
	cp.Items = cloneItems(set.Items)
	cp.ActiveIDs = append([]string(nil), set.ActiveIDs...)
	cp.Pass = pass
	cp.NextBatchIndex = nextBatch
	if att != "" {
		cp.CommittedAttempts = append(cp.CommittedAttempts, string(att))
	}
	return e.out.writeCheckpoint(cp)
}

// commitEval commits a frozen evaluation: the passes.jsonl audit row FIRST (idempotent by
// eval_id), then the checkpoint.
func (e *executor) commitEval(cp *checkpoint, set *strategySet, res evalResult) (*checkpoint, error) {
	exists, err := passRowExists(e.out.path("passes.jsonl"), res.EvalID)
	if err != nil {
		return cp, err
	}
	if !exists {
		if err := e.out.appendJSONL("passes.jsonl", map[string]any{
			"pass": res.Pass, "eval_id": res.EvalID, "version": res.Version, "evaluated": true,
			"frozen_accuracy": res.Accuracy, "scored": res.Scored, "skipped": res.Skipped, "coverage": res.Coverage,
		}); err != nil {
			return cp, fmt.Errorf("passes log write (refusing to commit eval): %w", err)
		}
	}
	if lc := cp.Eval.LastCommitted; lc != nil {
		cp.Eval.PrevEvalID = lc.EvalID
		a := lc.Accuracy
		cp.Eval.PrevAccuracy = &a
	}
	cp.Phase = phasePassComplete
	cp.Evaluated = true
	cp.Eval.LastCommitted = &res
	cp.Version = set.Version
	cp.NextID = set.NextID
	cp.Items = cloneItems(set.Items)
	cp.ActiveIDs = append([]string(nil), set.ActiveIDs...)
	cp.FinalVersion = set.Version
	return cp, e.out.writeCheckpoint(cp)
}

func (e *executor) converged(cp *checkpoint, res evalResult) bool {
	if res.Scored == 0 || res.Coverage < e.cfg.MinCoverage {
		return false
	}
	if res.Accuracy >= e.cfg.TargetAccuracy {
		return true
	}
	var prev *float64
	if lc := cp.Eval.LastCommitted; lc != nil {
		if lc.EvalID == res.EvalID {
			prev = cp.Eval.PrevAccuracy
		} else {
			a := lc.Accuracy
			prev = &a
		}
	}
	if prev == nil {
		return false
	}
	return (res.Accuracy - *prev) < e.cfg.MinDelta
}

func (e *executor) convergedCommitted(cp *checkpoint) bool {
	if lc := cp.Eval.LastCommitted; lc != nil {
		return e.converged(cp, *lc)
	}
	return false
}

// evaluatePass runs (or resumes) the frozen evaluation. A NEW evaluation starts at each
// pass boundary; a pending one is reused only when genuinely resuming (phase=evaluate for
// the same version). An interrupted evaluation returns an error and stays pending.
func (e *executor) evaluatePass(ctx context.Context, cp *checkpoint, set *strategySet, recs []trainsetRecord) (evalResult, error) {
	resuming := cp.Phase == phaseEvaluate && cp.Eval.EvalID != "" && cp.Eval.Version == set.Version
	if !resuming {
		cp.Eval.EvalID = string(newEvalID())
		cp.Eval.Version = set.Version
		cp.Eval.NextIndex = 0
		cp.Phase = phaseEvaluate
		cp.Evaluated = false
		if err := e.out.writeCheckpoint(cp); err != nil {
			return evalResult{}, err
		}
	}
	done, err := evalDoneSet(e.out.path("eval_state.jsonl"), cp.Eval.EvalID)
	if err != nil {
		return evalResult{}, err
	}
	block := set.renderStrategyBlock()

	for i := cp.Eval.NextIndex; i < len(recs); i++ {
		if err := ctx.Err(); err != nil {
			return evalResult{}, err
		}
		rec := recs[i]
		if done[rec.ID] {
			continue
		}
		q := block
		if q != "" {
			q += "\n\n"
		}
		q += rec.Question
		status, correct := "skipped", false
		var sid string
		var cerr error
		if e.client != nil {
			sid, cerr = e.client.createSession(ctx, e.cfg.ChatID)
		} else {
			cerr = fmt.Errorf("no client")
		}
		if cerr == nil {
			if ans, _, aerr := e.askIsolated(ctx, sid, q); aerr == nil {
				if v, jerr := e.judgeAnswer(ctx, rec, ans); jerr == nil {
					status, correct = "scored", v.Correct
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return evalResult{}, err
		}
		if err := e.out.appendJSONL("eval_state.jsonl", map[string]any{
			"eval_id": cp.Eval.EvalID, "qid": rec.ID, "status": status, "correct": correct,
		}); err != nil {
			return evalResult{}, fmt.Errorf("eval log write (refusing to advance cursor): %w", err)
		}
		cp.Eval.NextIndex = i + 1
		if err := e.out.writeCheckpoint(cp); err != nil {
			return evalResult{}, err
		}
	}
	res, err := tallyEval(e.out.path("eval_state.jsonl"), cp.Eval.EvalID, len(recs))
	if err != nil {
		return evalResult{}, err
	}
	res.Pass = cp.Pass
	res.Version = set.Version
	res.EvalID = cp.Eval.EvalID
	return res, nil
}

func tallyEval(path, evalID string, n int) (evalResult, error) {
	res := evalResult{EvalID: evalID}
	rows, err := readJSONLStrict(path)
	if err != nil {
		return res, err
	}
	correct := 0
	for _, row := range rows {
		if row["eval_id"] != evalID {
			continue
		}
		if row["status"] == "scored" {
			res.Scored++
			if b, _ := row["correct"].(bool); b {
				correct++
			}
		} else {
			res.Skipped++
		}
	}
	if res.Scored > 0 {
		res.Accuracy = float64(correct) / float64(res.Scored) * 100
	}
	if n > 0 {
		res.Coverage = float64(res.Scored) / float64(n)
	}
	return res, nil
}

func evalDoneSet(path, evalID string) (map[string]bool, error) {
	done := map[string]bool{}
	rows, err := readJSONLStrict(path)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row["eval_id"] == evalID {
			if qid, _ := row["qid"].(string); qid != "" {
				done[qid] = true
			}
		}
	}
	return done, nil
}

func passRowExists(path, evalID string) (bool, error) {
	rows, err := readJSONLStrict(path)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row["eval_id"] == evalID {
			return true, nil
		}
	}
	return false, nil
}

// readJSONLStrict parses a JSONL file, failing on any unparsable line (a silently ignored
// line would let the cursor advance past missing data). It first drops an uncommitted
// trailing half-line left by a hard kill -- a committed append always ends with '\n', so
// recovery must self-heal BEFORE reading, otherwise a partial tail blocks every resume.
func readJSONLStrict(path string) ([]map[string]any, error) {
	if err := truncatePartialLine(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	line := 0
	for sc.Scan() {
		line++
		b := strings.TrimSpace(sc.Text())
		if b == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(b), &m); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		out = append(out, m)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

// runBaseline runs the no-strategy control over the trainset, resumable like training.
func (e *executor) runBaseline(ctx context.Context, recs []trainsetRecord, meta trainsetMeta) error {
	out, err := openOutputDir(e.cfg.OutputDir + "_baseline")
	if err != nil {
		return err
	}
	defer out.release()
	e.out = out
	idsHash := idsHashOf(recs)
	promptFP := promptFingerprint()

	cp := &checkpoint{Phase: phaseEvaluate, Seed: e.cfg.Seed}
	fresh := true
	if prev, ok, err := loadCheckpoint(out.dir); err != nil {
		return err
	} else if ok && e.cfg.Resume {
		if err := validateResumeWith(prev, e.cfg, meta, idsHash, promptFP); err != nil {
			return err
		}
		cp = prev
		fresh = false
	}
	if fresh {
		bindRunIdentity(cp, e.cfg, meta, idsHash, promptFP)
		if err := writeRunIdentity(out, e.cfg, idsHash, promptFP); err != nil {
			return err
		}
	}
	set := newStrategySet()
	res, err := e.evaluatePass(ctx, cp, set, recs)
	if err != nil {
		if isStop(err) {
			return nil
		}
		return err
	}
	lb := map[string]any{
		"baseline_accuracy": res.Accuracy, "scored": res.Scored, "skipped": res.Skipped,
		"coverage": res.Coverage, "reasoning": e.cfg.Reasoning, "chat_id": e.cfg.ChatID,
		"trainset": e.cfg.Trainset, "trainset_sha256": meta.SHA256,
	}
	b, _ := json.MarshalIndent(lb, "", "  ")
	_ = os.WriteFile(out.path("leaderboard.json"), append(b, '\n'), artifactPerm)
	fmt.Printf("baseline: accuracy=%.1f%% scored=%d skipped=%d coverage=%.2f\n", res.Accuracy, res.Scored, res.Skipped, res.Coverage)
	return nil
}

func (e *executor) writeLeaderboard(cp *checkpoint, set *strategySet, converged bool) error {
	if e.out == nil {
		return nil
	}
	lb := map[string]any{
		"final_version":   set.Version,
		"updates":         set.Version,
		"converged":       converged,
		"final_evaluated": cp.FinalVersion == set.Version && cp.Evaluated,
	}
	if lc := cp.Eval.LastCommitted; lc != nil {
		lb["after_accuracy"] = lc.Accuracy
		lb["after_accuracy_pass"] = lc.Pass
		lb["after_accuracy_version"] = lc.Version
	}
	b, _ := json.MarshalIndent(lb, "", "  ")
	return os.WriteFile(e.out.path("leaderboard.json"), append(b, '\n'), artifactPerm)
}

// ---- helpers ----

func cloneSet(s *strategySet) *strategySet {
	return &strategySet{Version: s.Version, NextID: s.NextID, Items: cloneItems(s.Items), ActiveIDs: append([]string(nil), s.ActiveIDs...)}
}

func cloneItems(items []strategyItem) []strategyItem {
	out := make([]strategyItem, len(items))
	for i, it := range items {
		out[i] = it
		out[i].SourceIDs = append([]string(nil), it.SourceIDs...)
	}
	return out
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func subsetByID(recs []trainsetRecord, ids []string) []trainsetRecord {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []trainsetRecord
	for _, r := range recs {
		if want[r.ID] {
			out = append(out, r)
		}
	}
	return out
}

// promptFingerprint hashes the FULL rendered prompts (with fixed dummy arguments), so any
// change to a prompt's body -- not just its head constant -- changes the fingerprint.
func promptFingerprint() string {
	g := graderPrompt("Q", "R", "A")
	r := reflectionPrompt(newStrategySet(), "GOLD", 2)
	m := mergePrompt(newStrategySet(), nil, 0, 0)
	h := sha256.Sum256([]byte(g + "\x00" + r + "\x00" + m))
	return hex.EncodeToString(h[:])
}

// writeRunIdentity persists the REDACTED config snapshot (never the API key) and the
// prompt fingerprint. It is immutable: an existing identity is never overwritten.
func writeRunIdentity(out *outputDir, cfg config, idsHash, promptFP string) error {
	if _, err := os.Stat(out.path("_config_snapshot.json")); err == nil {
		return nil
	}
	snap := cfg
	snap.RAGFlowAPIKey = "" // credentials are never persisted
	cfgSnap, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out.path("_config_snapshot.json"), append(cfgSnap, '\n'), artifactPerm); err != nil {
		return err
	}
	pf, _ := json.MarshalIndent(map[string]any{
		"prompt_fingerprint_sha256": promptFP,
		"selected_ids_hash":         idsHash,
	}, "", "  ")
	return os.WriteFile(out.path("_prompt_fingerprint.json"), append(pf, '\n'), artifactPerm)
}

// probeProviderError marks a probe whose ANSWER is a decorated provider error. It means
// the request DID reach Codex (the text comes from the codex agent), so the engine is up
// and only the provider is unhappy -- a transient condition, not engine degradation.
type probeProviderError struct{ answer string }

func (e *probeProviderError) Error() string {
	return "probe answer is a provider error: " + truncate(e.answer, 200)
}

// errNoProbeRollout means no rollout carrying the probe nonce appeared: the request never
// ran on Codex, so the engine is degraded (or the rollout is not attributable).
var errNoProbeRollout = errors.New("no rollout containing this probe's nonce appeared (engine likely degraded, or the rollout is not attributable to this request)")

// preflightMode8 confirms the experiment actually runs on the Codex engine: the probe turn
// must produce a rollout that CONTAINS a nonce unique to this probe. An unrelated rollout
// (or a degraded non-codex pipeline) cannot satisfy that.
//
// The probe verifies the ENGINE, not the provider. A rollout carrying our nonce proves
// Codex served the request even when the turn then died on a transient provider condition
// ("high demand"), so that case PASSES -- the batch loop's own retry policy then handles
// the provider. Only when no nonce rollout appears do we look at the failure: a
// provider-side error is waited out (the same class-aware policy the batch loop uses) and
// retried, anything else aborts.
func (e *executor) preflightMode8(ctx context.Context) error {
	home := envOr("CODEX_HOME", filepath.Join(os.Getenv("HOME"), ".codex"))
	before := rolloutSet(home)
	nonce := "rsi-probe-" + newUUIDv4()
	for attempt := 1; ; attempt++ {
		err := e.preflightProbeOnce(ctx, home, before, nonce)
		if err == nil {
			return nil
		}
		class, retryable := probeRetryClass(err)
		if !retryable {
			return err
		}
		e.logf("mode-8 preflight: transient %s failure on attempt %d (%v); retrying in %s",
			class, attempt, err, e.retry.delayFor(class, attempt-1))
		if serr := e.retry.sleepClass(ctx, class, attempt-1); serr != nil {
			return serr
		}
	}
}

// preflightProbeOnce runs one probe turn. Success is "a rollout carrying the nonce
// appeared", checked BEFORE the answer is inspected: a provider-errored turn still proves
// Codex ran it.
func (e *executor) preflightProbeOnce(ctx context.Context, home string, before map[string]bool, nonce string) error {
	sid, err := e.client.createSession(ctx, e.cfg.ChatID)
	if err != nil {
		return err
	}
	res, aerr := e.client.ask(ctx, e.cfg.ChatID, "Reply with the single word: "+nonce, sid, e.cfg.Reasoning)
	if nonceRollout(home, before, nonce, 8*time.Second) {
		return nil // this probe's turn really ran on Codex
	}
	if aerr != nil {
		return aerr
	}
	if answerLooksLikeError(res.Answer) {
		return &probeProviderError{answer: res.Answer}
	}
	return errNoProbeRollout
}

// probeRetryClass decides whether a failed preflight probe is worth waiting out.
func probeRetryClass(err error) (failClass, bool) {
	var pe *probeProviderError
	if errors.As(err, &pe) {
		// Reached Codex, provider unhappy. Classify from the answer body so the wait
		// matches the class ("high demand" -> the flat quota interval).
		if c := classify(errors.New(pe.answer)); c == failQuota || c == failNetwork {
			return c, true
		}
		return failQuota, true
	}
	switch classify(err) {
	case failQuota, failNetwork:
		return classify(err), true
	default:
		return failPermanent, false
	}
}

// nonceRollout waits up to `wait` for a rollout created after `before` that contains nonce.
func nonceRollout(home string, before map[string]bool, nonce string, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		for p := range rolloutSet(home) {
			if before[p] {
				continue
			}
			if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), nonce) {
				return true
			}
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(time.Second)
	}
}

func rolloutSet(codexHome string) map[string]bool {
	root := filepath.Join(codexHome, "sessions")
	set := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
			set[path] = true
		}
		return nil
	})
	return set
}
