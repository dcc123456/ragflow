//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package component

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ragflow/internal/agent/runtime"
	"ragflow/internal/common"
	"ragflow/internal/ingestion/chunkcache"
	"ragflow/internal/ingestion/component/progress"
	"ragflow/internal/tokenizer"
	"ragflow/internal/utility"
)

// resumeNowFunc returns the current wall-clock time for computing ongoing
// ("至今"/"Present") tenure intervals. Overridden in unit tests for determinism.
var resumeNowFunc = time.Now

type resumeLaneSlot struct {
	lane resumeLane
	raw  string
	err  error
	// truncated marks a lane that ran on a context-trimmed resume: its reply
	// is a partial view that confirms nothing about the keys it did not see.
	truncated bool
}

type resumeChunkWork struct {
	chunkIndex  int
	ck          map[string]any
	rawText     string
	indexedText string
	lines       []string
	isEnglish   bool
	slots       []resumeLaneSlot
}

// resumeExtraction implements resumeHook: four-lane parallel resume
// extraction across extractorPool without nested submission
// (all 4*len(chunks) lane jobs are submitted flat from Run). Successful
// lanes are merged deterministically in Go; a lane that errored, returned a
// noncompliant reply, or ran on a context-trimmed resume contributes
// nothing, so previously stored values survive untouched; no lane producing
// a usable reply fails the run; embedding_text is always populated (falling
// back to raw text when the lanes produce nothing).
type resumeExtraction struct {
	c       *ExtractorComponent
	enabled bool
}

// attachResumeHook parses the resume mode config from the raw component
// params and attaches the hook. A missing or disabled sub-object attaches
// nothing, matching a template that does not run the mode.
func attachResumeHook(c *ExtractorComponent, params map[string]any) {
	r := &resumeExtraction{c: c}
	if resumeRaw, ok := params["resume"].(map[string]any); ok {
		if v, ok := resumeRaw["enabled"].(bool); ok {
			r.enabled = v
		} else if v, ok := resumeRaw["enabled"]; ok {
			r.enabled = mapInt(v) == 1
		}
	}
	if !r.enabled {
		return
	}
	c.resume = r
	// The resume mode owns the extractor: the other modular phases are not
	// supported alongside it. The four lanes already produce the keywords,
	// metadata and embedding text those phases would write, and the canvas
	// form hides their toggles while the mode is on; this gate is the
	// authoritative rule for any hand-edited DSL or operator override.
	c.Param.Keywords.Enabled = false
	c.Param.Questions.Enabled = false
	c.Param.Tags.Enabled = false
	c.Param.Summary.Enabled = false
	c.Param.Metadata.Enabled = false
}

func (r *resumeExtraction) Enabled() bool { return r.enabled }

func (r *resumeExtraction) Run(
	ctx context.Context,
	db *gorm.DB,
	in extractorInputs,
	w progress.Window,
) error {
	if !r.enabled || len(in.chunks) == 0 {
		return nil
	}
	// The four lanes must see the whole resume in one chunk. A chunker that
	// splits it would extract each fragment in isolation and the per-chunk
	// metadata merge would keep the last fragment's view of the document, so
	// a split resume fails loudly instead of producing fragment-level
	// metadata. The template wires the One chunker; this gate is the
	// authoritative rule for any hand-edited pipeline.
	usable := 0
	for _, ck := range in.chunks {
		if strings.TrimSpace(extractorChunkText(ck)) != "" {
			usable++
		}
	}
	if usable > 1 {
		return fmt.Errorf("extractor: resume extraction needs the whole resume in a single chunk, got %d; use the One chunker", usable)
	}

	works := make([]*resumeChunkWork, 0, len(in.chunks))
	futs := make([]utility.WorkerPoolFuture[extractorJob, struct{}], 0, len(in.chunks)*len(resumeLanes))

	temp := extractorTemperature
	laneIn := extractorInputs{
		llmID:       in.llmID,
		lang:        in.lang,
		temperature: &temp,
		cache:       in.cache,
	}
	modelID := in.modelID
	if modelID == "" {
		modelID = extractorCacheModelID(ctx, db, laneIn.llmID)
	}

	for i, ck := range in.chunks {
		rawText := extractorChunkText(ck)
		if strings.TrimSpace(rawText) == "" {
			continue
		}
		indexedText, lines := buildResumeIndexedText(rawText)
		if indexedText == "" {
			ck["embedding_text"] = rawText
			continue
		}

		isEnglish := detectResumeEnglish(rawText, in.lang)
		chunkID := chunkCacheID(ck)

		cw := &resumeChunkWork{
			chunkIndex:  i,
			ck:          ck,
			rawText:     rawText,
			indexedText: indexedText,
			lines:       lines,
			isEnglish:   isEnglish,
			slots:       make([]resumeLaneSlot, len(resumeLanes)),
		}
		works = append(works, cw)

		for laneIdx, lane := range resumeLanes {
			laneIdx, lane := laneIdx, lane
			cw.slots[laneIdx] = resumeLaneSlot{lane: lane}
			// The output contract rides in the system message and the indexed resume in
			// the user message, so context fitting trims resume text, never the schema.
			sysPrompt := resumeLaneInstructions(lane, isEnglish)
			cacheKey := chunkcache.Key("extractor:resume:"+string(lane), modelID, chunkID, sysPrompt, indexedText)

			fn := func() error {
				res, truncated, err := r.c.callResumeLane(ctx, db, laneIn, lane, cacheKey, sysPrompt, indexedText)
				cw.slots[laneIdx].raw = res
				cw.slots[laneIdx].truncated = truncated
				cw.slots[laneIdx].err = err
				// Never fail the pool future directly: sibling lanes must finish so
				// partial extraction results can still be merged and indexed.
				return nil
			}
			f, err := extractorPool.Submit(ctx, fn)
			if err != nil {
				return err
			}
			futs = append(futs, f)
		}
	}

	if err := awaitFutures(ctx, futs, w); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	now := resumeNowFunc()
	for _, cw := range works {
		if err := applyResumeChunkResult(ctx, cw, in.lang, now); err != nil {
			return err
		}
	}
	return nil
}

// callResumeLane returns the lane reply and whether it was produced from a
// context-trimmed resume. A trimmed or noncompliant reply is never cached.
func (c *ExtractorComponent) callResumeLane(
	ctx context.Context,
	db *gorm.DB,
	in extractorInputs,
	lane resumeLane,
	cacheKey, sysPrompt, userPrompt string,
) (string, bool, error) {
	// A cached reply is reused only if it still decodes and carries its
	// contract: a bad reply must never be served again on retry or resume.
	if cached, hit := chunkcache.Get(ctx, in.cache, cacheKey); hit && resumeLaneReplyValid(lane, cached) {
		return cached, false, nil
	}
	resp, err := c.callRaw(ctx, db, in, sysPrompt, userPrompt)
	if err != nil {
		return "", false, err
	}
	content := strings.TrimSpace(resp.Content)
	if content != "" && !strings.Contains(content, "**ERROR**") && !resp.UserTrimmed && resumeLaneReplyValid(lane, content) {
		chunkcache.Set(ctx, in.cache, cacheKey, content)
	}
	return content, resp.UserTrimmed, nil
}

// resumeLaneReplyValid reports whether a lane reply decodes into its payload
// and explicitly carries at least one contract key. A bare {} decodes but
// confirms nothing: it is a noncompliant reply that must neither be cached
// nor treated as the lane's view of the resume.
func resumeLaneReplyValid(lane resumeLane, raw string) bool {
	if !resumeLaneDecodes(lane, raw) {
		return false
	}
	return laneContractPresent(lane, decodeResumeLanePresence(raw))
}

func resumeLaneDecodes(lane resumeLane, raw string) bool {
	switch lane {
	case resumeLaneBasic:
		_, ok := decodeResumeLaneJSON[resumeBasicRaw](raw)
		return ok
	case resumeLaneWork:
		_, ok := decodeResumeLaneJSON[resumeWorkRaw](raw)
		return ok
	case resumeLaneEducation:
		_, ok := decodeResumeLaneJSON[resumeEducationRaw](raw)
		return ok
	case resumeLaneProject:
		_, ok := decodeResumeLaneJSON[resumeProjectRaw](raw)
		return ok
	}
	return false
}

func applyResumeChunkResult(ctx context.Context, cw *resumeChunkWork, lang string, now time.Time) error {
	outputs := resumeLaneOutputs{
		status:  make([]resumeLaneStatus, 0, len(cw.slots)),
		present: make(map[resumeLane]map[string]struct{}),
	}

	usable := 0
	var firstErr error
	for _, slot := range cw.slots {
		if slot.err != nil {
			if firstErr == nil {
				firstErr = slot.err
			}
			outputs.status = append(outputs.status, resumeLaneStatus{
				Lane:    slot.lane,
				Outcome: "call_error",
				Err:     slot.err.Error(),
			})
			continue
		}
		if strings.TrimSpace(slot.raw) == "" {
			outputs.status = append(outputs.status, resumeLaneStatus{
				Lane:    slot.lane,
				Outcome: "empty",
			})
			continue
		}
		presence := decodeResumeLanePresence(slot.raw)
		if !laneContractPresent(slot.lane, presence) {
			// A reply carrying none of the lane's contract keys is a parse
			// failure, not a view of the resume: it merges nothing and
			// confirms nothing.
			outputs.status = append(outputs.status, resumeLaneStatus{Lane: slot.lane, Outcome: "parse_error"})
			continue
		}
		if slot.truncated {
			// A trimmed lane saw less than the full resume, so its partial
			// view contributes nothing: merging it would overwrite complete
			// stored values with partial ones. The provider answered, so the
			// lane still counts as usable; only its outcome is recorded.
			usable++
			outputs.status = append(outputs.status, resumeLaneStatus{Lane: slot.lane, Outcome: "trimmed"})
			continue
		}
		payloadOK := false
		switch slot.lane {
		case resumeLaneBasic:
			if parsed, ok := decodeResumeLaneJSON[resumeBasicRaw](slot.raw); ok {
				outputs.basic = parsed
				payloadOK = true
			}
		case resumeLaneWork:
			if parsed, ok := decodeResumeLaneJSON[resumeWorkRaw](slot.raw); ok {
				outputs.work = parsed
				payloadOK = true
			}
		case resumeLaneEducation:
			if parsed, ok := decodeResumeLaneJSON[resumeEducationRaw](slot.raw); ok {
				outputs.edu = parsed
				payloadOK = true
			}
		case resumeLaneProject:
			if parsed, ok := decodeResumeLaneJSON[resumeProjectRaw](slot.raw); ok {
				outputs.project = parsed
				payloadOK = true
			}
		}
		if !payloadOK {
			outputs.status = append(outputs.status, resumeLaneStatus{Lane: slot.lane, Outcome: "parse_error"})
			continue
		}
		usable++
		outputs.present[slot.lane] = presence
		outputs.status = append(outputs.status, resumeLaneStatus{Lane: slot.lane, Outcome: "ok"})
	}

	// No lane produced a usable reply — every lane errored, returned
	// nothing, or failed its contract. The shared extraction phases fail
	// the task on any LLM error; a resume run that got nothing trustworthy
	// from the model must not index the raw text as a success. A compliant
	// empty reply (explicit []) is usable: the resume may genuinely lack
	// that section.
	if len(cw.slots) > 0 && usable == 0 {
		if firstErr != nil {
			return fmt.Errorf("resume extraction failed on all lanes: %w", firstErr)
		}
		return fmt.Errorf("resume extraction failed on all lanes: no lane returned a compliant reply")
	}

	profile := mergeResumeLanes(cw.rawText, cw.lines, cw.isEnglish, now, outputs)
	logResumeExtractionStatus(ctx, cw.chunkIndex, profile.LaneStatuses)

	// 1. Metadata: attach the document-level filter fields without mutating
	// text. Name, current location, degree labels, tenure, and the entry
	// fields pass source-text grounding in mergeResumeLanes; the basic
	// lane's list fields (skills, certificates, languages, expected
	// cities/positions) are model-curated and only deduplicated, not
	// source-verified.
	if meta := profile.Metadata(); len(meta) > 0 {
		var dst map[string]any
		if existing, ok := cw.ck["metadata"].(map[string]any); ok && existing != nil {
			dst = existing
		} else {
			dst = make(map[string]any, len(meta))
		}
		for k, v := range meta {
			dst[k] = v
		}
		cw.ck["metadata"] = dst
	}

	// 2. Keywords: populate important_kwd / important_tks directly (never via
	// comma-delimited ck["keywords"] so terms containing commas stay intact).
	if kwds := profile.ImportantKeywords(); len(kwds) > 0 {
		if _, exists := cw.ck["important_kwd"]; !exists {
			tokLang := lang
			if tokLang == "" {
				if cw.isEnglish {
					tokLang = "English"
				} else {
					tokLang = "Chinese"
				}
			}
			tok := tokenizer.New(tokLang)
			tks, tkErr := tok.Tokenize(strings.Join(kwds, " "))
			cw.ck["important_kwd"] = kwds
			if tkErr == nil {
				cw.ck["important_tks"] = tks
			}
		}
	}

	// 3. Embedding text: structured summary + full raw text (falls back to rawText
	// when all four lanes produced nothing). Original text stays untouched for
	// content_ltks full-text tokenization and display; summary is intentionally
	// NOT written so Tokenizer never shadows full-text tokens.
	cw.ck["embedding_text"] = profile.BuildEmbeddingText(cw.rawText)
	// Only keys the lanes explicitly reported may be cleared on re-parse; a
	// lane that failed, returned a noncompliant reply, or ran trimmed leaves
	// the stored value alone.
	if keys := confirmedManagedMetadataKeys(outputs); len(keys) > 0 {
		cw.ck["_managed_metadata_keys"] = keys
	}
	return nil
}

func logResumeExtractionStatus(ctx context.Context, chunkIndex int, statuses []resumeLaneStatus) {
	if len(statuses) == 0 {
		return
	}
	parts := make([]string, 0, len(statuses))
	hasIssue := false
	for _, st := range statuses {
		entry := fmt.Sprintf("%s=%s", st.Lane, st.Outcome)
		if st.OutOfBounds > 0 {
			entry += fmt.Sprintf("(oob=%d)", st.OutOfBounds)
			hasIssue = true
		}
		if st.Err != "" {
			entry += " err=" + st.Err
		}
		if st.Outcome != "ok" {
			hasIssue = true
		}
		parts = append(parts, entry)
	}
	summary := "resume lanes: " + strings.Join(parts, ", ")
	if hasIssue {
		common.Warn("extractor: resume extraction completed with partial issues",
			zap.String("component", componentNameExtractor),
			zap.Int("chunk_index", chunkIndex),
			zap.String("lanes", summary),
		)
	} else {
		common.Debug("extractor: resume extraction completed",
			zap.String("component", componentNameExtractor),
			zap.Int("chunk_index", chunkIndex),
			zap.String("lanes", summary),
		)
	}
	runtime.ReportIOProgress(ctx, componentNameExtractor, summary)
}

// confirmedManagedMetadataKeys lists the metadata keys the lanes explicitly
// reported. The basic lane confirms a key per contract field its reply
// carried; the aggregate lanes confirm their keys when their section key was
// carried; a trimmed lane confirms nothing.
func confirmedManagedMetadataKeys(outputs resumeLaneOutputs) []string {
	var keys []string
	if p := outputs.present[resumeLaneBasic]; p != nil {
		for _, src := range resumeBasicManagedKeySources {
			if _, ok := p[src.src]; ok {
				keys = append(keys, src.meta)
			}
		}
	}
	if _, ok := outputs.present[resumeLaneWork]["workExperience"]; ok {
		keys = append(keys, resumeLaneMetadataKeys["work_exp"]...)
	}
	if _, ok := outputs.present[resumeLaneEducation]["education"]; ok {
		keys = append(keys, resumeLaneMetadataKeys["education"]...)
	}
	return keys
}
