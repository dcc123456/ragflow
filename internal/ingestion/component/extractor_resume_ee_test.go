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
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ragflow/internal/ingestion/component/progress"
	"ragflow/internal/tokenizer"
)

// routingResumeChatInvoker dispatches canned responses by inspecting which of
// the four resume prompts was sent, so concurrent 4-lane execution stays
// deterministic regardless of worker scheduling order.
type routingResumeChatInvoker struct {
	mu       sync.Mutex
	handlers map[resumeLane]func(req extractorChatRequest) (*extractorChatResponse, error)
	calls    atomic.Int32
	seen     map[resumeLane]int
}

func newRoutingResumeInvoker(handlers map[resumeLane]func(req extractorChatRequest) (*extractorChatResponse, error)) *routingResumeChatInvoker {
	return &routingResumeChatInvoker{
		handlers: handlers,
		seen:     make(map[resumeLane]int),
	}
}

func (r *routingResumeChatInvoker) Chat(_ context.Context, req extractorChatRequest) (*extractorChatResponse, error) {
	r.calls.Add(1)
	// The lane contract rides in the system message (first message).
	userText := ""
	if len(req.Messages) > 0 {
		userText = req.Messages[0].Content
	}
	var lane resumeLane
	switch {
	case strings.Contains(userText, "workExperience"):
		lane = resumeLaneWork
	case strings.Contains(userText, `"education"`):
		lane = resumeLaneEducation
	case strings.Contains(userText, "projectExperience"):
		lane = resumeLaneProject
	default:
		lane = resumeLaneBasic
	}
	r.mu.Lock()
	r.seen[lane]++
	fn := r.handlers[lane]
	r.mu.Unlock()
	if fn == nil {
		return &extractorChatResponse{Content: "{}"}, nil
	}
	return fn(req)
}

func withRoutingResumeInvoker(t *testing.T, inv *routingResumeChatInvoker) {
	t.Helper()
	prev := defaultExtractorChatInvoker
	SetExtractorChatInvoker(inv)
	t.Cleanup(func() { SetExtractorChatInvoker(prev) })
}

func pinResumeNow(t *testing.T, ts time.Time) {
	t.Helper()
	prev := resumeNowFunc
	resumeNowFunc = func() time.Time { return ts }
	t.Cleanup(func() { resumeNowFunc = prev })
}

func TestBuildResumeIndexedText_StripsNoiseAndBlankLines(t *testing.T) {
	raw := "张三\r\n\r\nabcdef1234567890ABCDEF1234567890abcdef123456\n  资深后端工程师  \n"
	indexed, lines := buildResumeIndexedText(raw)
	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2: %#v", len(lines), lines)
	}
	if lines[0] != "张三" || lines[1] != "资深后端工程师" {
		t.Fatalf("lines = %#v", lines)
	}
	want := "[0]: 张三\n[1]: 资深后端工程师"
	if indexed != want {
		t.Fatalf("indexed = %q, want %q", indexed, want)
	}
}

func TestCalculateWorkExperienceYears_Rules(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	t.Run("overlapping intervals counted once", func(t *testing.T) {
		got := calculateWorkExperienceYears([]resumeWorkItemRaw{
			{Company: "A", StartDate: "2020.01", EndDate: "2023.01"},
			{Company: "B", StartDate: "2021.06", EndDate: "2022.06"},
		}, now)
		if got == nil || *got != 3.0 {
			t.Fatalf("got = %v, want 3.0", got)
		}
	})

	t.Run("explicit ongoing uses now while empty end date is skipped", func(t *testing.T) {
		gotOngoing := calculateWorkExperienceYears([]resumeWorkItemRaw{
			{Company: "A", StartDate: "2021.07", EndDate: "至今"},
		}, now)
		if gotOngoing == nil || *gotOngoing != 5.0 {
			t.Fatalf("ongoing got = %v, want 5.0", gotOngoing)
		}

		gotMissing := calculateWorkExperienceYears([]resumeWorkItemRaw{
			{Company: "A", StartDate: "2021.07", EndDate: ""},
		}, now)
		if gotMissing != nil {
			t.Fatalf("missing end_date must stay unknown (nil), got %v", *gotMissing)
		}
	})

	t.Run("internships excluded and unknown returns nil instead of zero", func(t *testing.T) {
		got := calculateWorkExperienceYears([]resumeWorkItemRaw{
			{Company: "InternCo", StartDate: "2023.01", EndDate: "2023.09", Internship: true},
		}, now)
		if got != nil {
			t.Fatalf("internship-only resume must return nil, got %v", *got)
		}
	})

	t.Run("year-only range avoids double counting boundaries", func(t *testing.T) {
		got := calculateWorkExperienceYears([]resumeWorkItemRaw{
			{Company: "A", StartDate: "2020", EndDate: "2025"},
		}, now)
		if got == nil || *got != 5.0 {
			t.Fatalf("year-only got = %v, want 5.0", got)
		}
	})
}

func TestSelectHighestDegree_ChineseAndEnglish(t *testing.T) {
	lines := []string{
		"清华大学 计算机科学 硕士",
		"浙江大学 软件工程 本科",
	}
	raw := strings.Join(lines, "\n")
	profile := mergeResumeLanes(raw, lines, false, time.Now(), resumeLaneOutputs{
		edu: &resumeEducationRaw{
			Education: []resumeEducationItemRaw{
				{School: "浙江大学", Major: "软件工程", Degree: "本科"},
				{School: "清华大学", Major: "计算机科学", Degree: "Master"},
			},
		},
	})
	if profile.HighestDegree != "硕士" || profile.HighestDegreeLevel != 5 {
		t.Fatalf("highest degree = (%q, %d), want (硕士, 5)", profile.HighestDegree, profile.HighestDegreeLevel)
	}
}

func TestExtractResumeLines_Validation(t *testing.T) {
	lines := []string{"L0", "L1", "L2"}

	if got, oob := extractResumeLines([]int{1, 2}, lines); oob || got != "L1\nL2" {
		t.Fatalf("valid range = (%q, %v), want (L1\\nL2, false)", got, oob)
	}
	if got, oob := extractResumeLines([]int{0}, lines); oob || got != "L0" {
		t.Fatalf("single line = (%q, %v), want (L0, false)", got, oob)
	}
	if got, oob := extractResumeLines([]int{2, 5}, lines); !oob || got != "" {
		t.Fatalf("oob range = (%q, %v), want (\"\", true)", got, oob)
	}
	if got, oob := extractResumeLines([]int{2, 1}, lines); !oob || got != "" {
		t.Fatalf("inverted range = (%q, %v), want (\"\", true)", got, oob)
	}
}

func TestExtractorComponent_ResumeMode_FourLaneParallelAndEnrichment(t *testing.T) {
	pinResumeNow(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))

	resumeText := strings.Join([]string{
		"李明",
		"现居地：上海",
		"技能：Go, Kafka, TiDB, 账务对账",
		"证书：CKA",
		"工作经历",
		"星河支付有限公司 2020.07 - 至今 高级后端工程师",
		"负责核心支付清结算系统从旧架构平滑迁移，实施双写校验与差异补偿机制。",
		"维护底层 PaxosFence 分布式锁组件。",
		"教育经历",
		"复旦大学 2017.09 - 2020.06 计算机技术 硕士",
		"项目经历",
		"新一代支付核心迁移 架构师 2023.01 - 2025.06",
		"设计双写校验与差异补偿，保障迁移期间资金零差错。",
	}, "\n")

	inv := newRoutingResumeInvoker(map[resumeLane]func(req extractorChatRequest) (*extractorChatResponse, error){
		resumeLaneBasic: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{
				"name_kwd": "李明",
				"current_location": "上海",
				"expect_position_name_tks": ["支付架构师"],
				"skill_tks": ["Go", "Kafka", "go", "TiDB"],
				"certificate_tks": ["CKA", "CKA"]
			}`}, nil
		},
		resumeLaneWork: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{
				"workExperience": [
					{
						"company": "星河支付有限公司",
						"position": "高级后端工程师",
						"internship": 0,
						"start_date": "2020.07",
						"end_date": "至今",
						"desc_lines": [6, 7]
					},
					{
						"company": "幻觉编造公司",
						"position": "不存在的职位",
						"internship": 0,
						"start_date": "2018.01",
						"end_date": "2019.01",
						"desc_lines": []
					}
				]
			}`}, nil
		},
		resumeLaneEducation: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{
				"education": [
					{"school": "复旦大学", "major": "计算机技术", "degree": "硕士", "start_date": "2017.09", "end_date": "2020.06", "desc_lines": []}
				]
			}`}, nil
		},
		resumeLaneProject: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{
				"projectExperience": [
					{"project_name": "新一代支付核心迁移", "role": "架构师", "start_date": "2023.01", "end_date": "2025.06", "desc_lines": [12, 12]}
				]
			}`}, nil
		},
	})
	withRoutingResumeInvoker(t, inv)

	comp, err := NewExtractorComponent(map[string]any{
		"llm_id": "gpt-4o-mini@openai",
		"resume": map[string]any{"enabled": true},
	})
	if err != nil {
		t.Fatalf("NewExtractorComponent: %v", err)
	}

	out, err := comp.Invoke(t.Context(), nil, map[string]any{
		"chunks": []map[string]any{{"id": "ck-resume-1", "text": resumeText}},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if inv.calls.Load() != 4 {
		t.Fatalf("chat calls = %d, want 4", inv.calls.Load())
	}

	chunks, ok := out["chunks"].([]map[string]any)
	if !ok || len(chunks) != 1 {
		t.Fatalf("chunks = %#v, want 1 chunk", out["chunks"])
	}
	ck := chunks[0]

	// Original text must remain untouched and summary must remain unset.
	if ck["text"] != resumeText {
		t.Fatal("original text must stay intact for full-text recall and display")
	}
	if _, hasSummary := ck["summary"]; hasSummary {
		t.Fatalf("summary must not be set in resume mode so Tokenizer keeps full-text content_ltks: %v", ck["summary"])
	}

	// Metadata verification (including anti-hallucination & numeric tenure).
	meta, ok := ck["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata = %T, want map[string]any", ck["metadata"])
	}
	if meta["name"] != "李明" {
		t.Errorf("name = %v, want 李明", meta["name"])
	}
	if meta["highest_degree"] != "硕士" || meta["highest_degree_level"] != 5 {
		t.Errorf("highest_degree = (%v, %v), want (硕士, 5)", meta["highest_degree"], meta["highest_degree_level"])
	}
	// 2020.07 - 2026.07 = 6.0 years; fabricated company 2018-2019 must be excluded.
	if years, ok := meta["work_experience_years"].(float64); !ok || years != 6.0 {
		t.Errorf("work_experience_years = %#v, want 6.0 (hallucinated entry must not count)", meta["work_experience_years"])
	}
	if companies, _ := meta["companies"].([]string); !reflect.DeepEqual(companies, []string{"星河支付有限公司"}) {
		t.Errorf("companies = %#v, want [星河支付有限公司]", meta["companies"])
	}
	if skills, _ := meta["skills"].([]string); !reflect.DeepEqual(skills, []string{"Go", "Kafka", "TiDB"}) {
		t.Errorf("skills = %#v, want deduped verbatim [Go Kafka TiDB]", meta["skills"])
	}
	if certs, _ := meta["certificates"].([]string); !reflect.DeepEqual(certs, []string{"CKA"}) {
		t.Errorf("certificates = %#v, want [CKA]", meta["certificates"])
	}
	// The basic reply carried name_kwd, current_location,
	// expect_position_name_tks, skill_tks, and certificate_tks — exactly those
	// five keys are confirmed; the fields it omitted (expect_city_names_tks,
	// language_tks) say nothing and their stored values stay untouched.
	wantManaged := append(append([]string(nil),
		"name", "current_location", "expected_positions", "skills", "certificates"),
		append(append([]string{}, resumeLaneMetadataKeys["work_exp"]...),
			resumeLaneMetadataKeys["education"]...)...)
	if managed, _ := ck["_managed_metadata_keys"].([]string); !reflect.DeepEqual(managed, wantManaged) {
		t.Errorf("_managed_metadata_keys = %#v, want %#v", managed, wantManaged)
	}

	// Keywords verification.
	kwds, ok := ck["important_kwd"].([]string)
	if !ok || !slices.Contains(kwds, "Go") || !slices.Contains(kwds, "新一代支付核心迁移") {
		t.Fatalf("important_kwd = %#v, want Go and 新一代支付核心迁移", ck["important_kwd"])
	}

	// Embedding text must prepend structured summary AND preserve full raw text.
	embText, ok := ck["embedding_text"].(string)
	if !ok || !strings.Contains(embText, "双写校验与差异补偿") || !strings.HasSuffix(embText, resumeText) {
		t.Fatalf("embedding_text must prepend summary and end with raw text, got: %q", embText)
	}
	if len(embText) <= len(resumeText) {
		t.Fatalf("embedding_text len = %d should be larger than rawText len = %d", len(embText), len(resumeText))
	}
}

func TestExtractorComponent_ResumeMode_PartialFailureAndAllEmptyFallback(t *testing.T) {
	resumeText := "王强\n熟悉 Rust 和 PaxosFence 引擎\n"

	t.Run("partial lane failure keeps remaining lanes and original text", func(t *testing.T) {
		inv := newRoutingResumeInvoker(map[resumeLane]func(req extractorChatRequest) (*extractorChatResponse, error){
			resumeLaneBasic: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return &extractorChatResponse{Content: `{"name_kwd": "王强", "skill_tks": ["Rust"]}`}, nil
			},
			resumeLaneWork: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return nil, context.DeadlineExceeded
			},
			resumeLaneEducation: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return &extractorChatResponse{Content: `not valid json {{{`}, nil
			},
			resumeLaneProject: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return &extractorChatResponse{Content: `{"projectExperience":[{"project_name":"Engine","desc_lines":[99,120]}]}`}, nil
			},
		})
		withRoutingResumeInvoker(t, inv)

		comp, err := NewExtractorComponent(map[string]any{
			"llm_id": "gpt-4o-mini@openai",
			"resume": map[string]any{"enabled": true},
		})
		if err != nil {
			t.Fatalf("NewExtractorComponent: %v", err)
		}
		out, err := comp.Invoke(t.Context(), nil, map[string]any{
			"chunks": []map[string]any{{"text": resumeText}},
		})
		if err != nil {
			t.Fatalf("partial failure must degrade cleanly, got err: %v", err)
		}
		ck := out["chunks"].([]map[string]any)[0]
		meta := ck["metadata"].(map[string]any)
		if meta["name"] != "王强" {
			t.Fatalf("name = %v, want 王强", meta["name"])
		}
		if _, hasYears := meta["work_experience_years"]; hasYears {
			t.Fatalf("work_experience_years must stay absent on work lane failure, got %v", meta["work_experience_years"])
		}
		if ck["text"] != resumeText {
			t.Fatal("raw text must survive partial failure")
		}
		// Only the fields the basic reply carried are confirmed — work timed
		// out, education returned unparsable output, and the reply holds
		// name_kwd and skill_tks, so exactly name and skills may be cleared;
		// every other stored value stays untouched.
		managed, _ := ck["_managed_metadata_keys"].([]string)
		if !reflect.DeepEqual(managed, []string{"name", "skills"}) {
			t.Fatalf("_managed_metadata_keys = %#v, want [name skills]", managed)
		}
		if emb, _ := ck["embedding_text"].(string); !strings.Contains(emb, resumeText) {
			t.Fatalf("embedding_text must contain raw text, got %q", emb)
		}
	})

	t.Run("compliant empty replies confirm absence and fall back to raw text", func(t *testing.T) {
		inv := newRoutingResumeInvoker(map[resumeLane]func(req extractorChatRequest) (*extractorChatResponse, error){
			resumeLaneBasic: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return &extractorChatResponse{Content: `{"name_kwd":"","skill_tks":[]}`}, nil
			},
			resumeLaneWork: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return &extractorChatResponse{Content: `{"workExperience":[]}`}, nil
			},
			resumeLaneEducation: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return &extractorChatResponse{Content: `{"education":[]}`}, nil
			},
			resumeLaneProject: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return &extractorChatResponse{Content: `{"projectExperience":[]}`}, nil
			},
		})
		withRoutingResumeInvoker(t, inv)

		comp, err := NewExtractorComponent(map[string]any{
			"llm_id": "gpt-4o-mini@openai",
			"resume": map[string]any{"enabled": true},
		})
		if err != nil {
			t.Fatalf("NewExtractorComponent: %v", err)
		}
		out, err := comp.Invoke(t.Context(), nil, map[string]any{
			"chunks": []map[string]any{{"text": resumeText}},
		})
		if err != nil {
			t.Fatalf("compliant empty lanes must succeed, got err: %v", err)
		}
		ck := out["chunks"].([]map[string]any)[0]
		if ck["embedding_text"] != resumeText {
			t.Fatalf("embedding_text = %q, want verbatim fallback %q", ck["embedding_text"], resumeText)
		}
		// Explicit empties confirm absence: the basic reply carried
		// name_kwd and skill_tks, so exactly name and skills may clear,
		// plus the work and education section keys.
		wantManaged := append(append([]string{"name", "skills"},
			resumeLaneMetadataKeys["work_exp"]...),
			resumeLaneMetadataKeys["education"]...)
		managed, _ := ck["_managed_metadata_keys"].([]string)
		if !reflect.DeepEqual(managed, wantManaged) {
			t.Fatalf("_managed_metadata_keys = %#v, want %#v", managed, wantManaged)
		}
	})

	t.Run("no usable lane fails the run", func(t *testing.T) {
		inv := newRoutingResumeInvoker(map[resumeLane]func(req extractorChatRequest) (*extractorChatResponse, error){
			resumeLaneBasic: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return &extractorChatResponse{Content: `{}`}, nil
			},
			resumeLaneWork: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return &extractorChatResponse{Content: `{}`}, nil
			},
			resumeLaneEducation: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return &extractorChatResponse{Content: `{}`}, nil
			},
			resumeLaneProject: func(_ extractorChatRequest) (*extractorChatResponse, error) {
				return &extractorChatResponse{Content: `{}`}, nil
			},
		})
		withRoutingResumeInvoker(t, inv)

		comp, err := NewExtractorComponent(map[string]any{
			"llm_id": "gpt-4o-mini@openai",
			"resume": map[string]any{"enabled": true},
		})
		if err != nil {
			t.Fatalf("NewExtractorComponent: %v", err)
		}
		// A bare {} carries none of the lane contract: no lane produced a
		// view of the resume, which must fail the run instead of silently
		// indexing the raw text.
		if _, err := comp.Invoke(t.Context(), nil, map[string]any{
			"chunks": []map[string]any{{"text": resumeText}},
		}); err == nil {
			t.Fatal("four noncompliant lanes must fail the run instead of indexing raw text")
		}
	})

	t.Run("all lanes failing with a config error fail the run", func(t *testing.T) {
		prevDelay := extractorRetryDelay
		extractorRetryDelay = time.Millisecond
		t.Cleanup(func() { extractorRetryDelay = prevDelay })

		authErr := errors.New("401 unauthorized: invalid api key")
		inv := newRoutingResumeInvoker(map[resumeLane]func(req extractorChatRequest) (*extractorChatResponse, error){
			resumeLaneBasic:     func(_ extractorChatRequest) (*extractorChatResponse, error) { return nil, authErr },
			resumeLaneWork:      func(_ extractorChatRequest) (*extractorChatResponse, error) { return nil, authErr },
			resumeLaneEducation: func(_ extractorChatRequest) (*extractorChatResponse, error) { return nil, authErr },
			resumeLaneProject:   func(_ extractorChatRequest) (*extractorChatResponse, error) { return nil, authErr },
		})
		withRoutingResumeInvoker(t, inv)

		comp, err := NewExtractorComponent(map[string]any{
			"llm_id": "gpt-4o-mini@openai",
			"resume": map[string]any{"enabled": true},
		})
		if err != nil {
			t.Fatalf("NewExtractorComponent: %v", err)
		}
		if _, err := comp.Invoke(t.Context(), nil, map[string]any{
			"chunks": []map[string]any{{"text": resumeText}},
		}); err == nil {
			t.Fatal("all-lane 401 auth failure must fail the run")
		}
	})

	t.Run("all lanes failing with a transient error fail the run", func(t *testing.T) {
		deadlineErr := context.DeadlineExceeded
		inv := newRoutingResumeInvoker(map[resumeLane]func(req extractorChatRequest) (*extractorChatResponse, error){
			resumeLaneBasic:     func(_ extractorChatRequest) (*extractorChatResponse, error) { return nil, deadlineErr },
			resumeLaneWork:      func(_ extractorChatRequest) (*extractorChatResponse, error) { return nil, deadlineErr },
			resumeLaneEducation: func(_ extractorChatRequest) (*extractorChatResponse, error) { return nil, deadlineErr },
			resumeLaneProject:   func(_ extractorChatRequest) (*extractorChatResponse, error) { return nil, deadlineErr },
		})
		withRoutingResumeInvoker(t, inv)

		comp, err := NewExtractorComponent(map[string]any{
			"llm_id": "gpt-4o-mini@openai",
			"resume": map[string]any{"enabled": true},
		})
		if err != nil {
			t.Fatalf("NewExtractorComponent: %v", err)
		}
		if _, err := comp.Invoke(t.Context(), nil, map[string]any{
			"chunks": []map[string]any{{"text": resumeText}},
		}); err == nil {
			t.Fatal("all-lane transient failure must fail the run instead of indexing an unenriched resume")
		}
	})
}

type recordingResumeEmbedder struct {
	encoded []string
}

func (e *recordingResumeEmbedder) MaxTokens() int { return 8192 }
func (e *recordingResumeEmbedder) BatchSize() int { return 16 }
func (e *recordingResumeEmbedder) Encode(_ context.Context, texts []string) ([]EmbeddingResult, error) {
	e.encoded = append(e.encoded, texts...)
	res := make([]EmbeddingResult, len(texts))
	for i := range texts {
		res[i] = EmbeddingResult{Vector: []float64{0.1, 0.2, 0.3, 0.4}, TokenCount: 10}
	}
	return res, nil
}

// TestExtractorResumeToTokenizer_FullTextFallbackAndEmbeddingField verifies the
// contract between Extractor (Resume mode) and Tokenizer(fields="embedding_text"):
//   - content_ltks comes from raw text (retaining niche terms missed by all 4 lanes),
//   - the embedding vector is computed from embedding_text (not raw text alone),
//   - important_kwd and _managed_metadata_keys survive Tokenizer intact.
func TestExtractorResumeToTokenizer_FullTextFallbackAndEmbeddingField(t *testing.T) {
	if err := tokenizer.Init(nil); err != nil {
		t.Skipf("tokenizer pool unavailable: %v", err)
	}
	resumeText := "赵敏\n熟悉 Go 与冷门自研协议 XyloProto99"
	inv := newRoutingResumeInvoker(map[resumeLane]func(req extractorChatRequest) (*extractorChatResponse, error){
		resumeLaneBasic: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{"name_kwd": "赵敏", "skill_tks": ["Go"]}`}, nil
		},
		resumeLaneWork: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{}`}, nil
		},
		resumeLaneEducation: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{}`}, nil
		},
		resumeLaneProject: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{}`}, nil
		},
	})
	withRoutingResumeInvoker(t, inv)

	ext, err := NewExtractorComponent(map[string]any{
		"llm_id": "gpt-4o-mini@openai",
		"resume": map[string]any{"enabled": true},
	})
	if err != nil {
		t.Fatalf("NewExtractorComponent: %v", err)
	}
	extOut, err := ext.Invoke(t.Context(), nil, map[string]any{
		"chunks": []map[string]any{{"id": "ck-1", "text": resumeText}},
	})
	if err != nil {
		t.Fatalf("Extractor.Invoke: %v", err)
	}

	embedder := &recordingResumeEmbedder{}
	tok, err := NewTokenizerComponentWithResolver(map[string]any{
		"fields":               "embedding_text",
		"filename_embd_weight": 0.0,
		"search_method":        []string{"embedding", "full_text"},
	}, func(_ context.Context, _, _ string) (Embedder, string, error) {
		return embedder, "test-embd", nil
	})
	if err != nil {
		t.Fatalf("NewTokenizerComponentWithResolver: %v", err)
	}

	tokOut, err := tok.Invoke(t.Context(), nil, map[string]any{
		"kb_id":         "kb-1",
		"name":          "zhaomin_resume.pdf",
		"output_format": "chunks",
		"chunks":        extOut["chunks"],
	})
	if err != nil {
		t.Fatalf("Tokenizer.Invoke: %v", err)
	}
	chunks := tokOut["chunks"].([]map[string]any)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	ck := chunks[0]

	// Niche term missed by all 4 lanes must stay searchable in content_ltks.
	ltks, _ := ck["content_ltks"].(string)
	if !strings.Contains(strings.ToLower(ltks), "xyloproto99") {
		t.Fatalf("content_ltks = %q, want niche term xyloproto99 preserved from raw text", ltks)
	}
	// Embedding input must be embedding_text (prepended summary + raw text).
	if len(embedder.encoded) < 2 {
		t.Fatalf("expected title + content encode calls, got %d: %#v", len(embedder.encoded), embedder.encoded)
	}
	contentEmbedded := embedder.encoded[len(embedder.encoded)-1]
	if !strings.Contains(contentEmbedded, "核心技能: Go") || !strings.Contains(contentEmbedded, "XyloProto99") {
		t.Fatalf("embedded content = %q, want summary + raw text", contentEmbedded)
	}
	if _, ok := ck["q_4_vec"]; !ok {
		t.Fatal("expected q_4_vec embedding vector attached")
	}
	if _, ok := ck["_managed_metadata_keys"]; !ok {
		t.Fatal("expected _managed_metadata_keys to survive Tokenizer into persist stage")
	}
}

func TestResumeProfile_ProjectWorkOverlapDeduplicatedInSummary(t *testing.T) {
	sharedDesc := "主导核心支付清结算系统从旧架构平滑迁移，实施双写校验与差异补偿机制确保资金一致。"
	profile := resumeProfile{
		WorkEntries: []resumeWorkEntry{
			{Company: "星河支付", Position: "资深后端工程师", Description: sharedDesc},
		},
		ProjectEntries: []resumeProjectEntry{
			{ProjectName: "核心支付迁移项目", Role: "架构师", Description: sharedDesc},
		},
	}
	summary := profile.EmbeddingSummary()
	if got := strings.Count(summary, "双写校验与差异补偿"); got != 1 {
		t.Fatalf("expected overlapping project/work description once in summary, got %d occurrences:\n%s", got, summary)
	}
	if !strings.Contains(summary, "核心支付迁移项目") {
		t.Fatalf("project name should still appear in summary even when description overlaps:\n%s", summary)
	}
}

func TestExtractorComponent_ResumeMode_PerLaneCacheHit(t *testing.T) {
	store := newFakeCacheStore()
	inv := newRoutingResumeInvoker(map[resumeLane]func(req extractorChatRequest) (*extractorChatResponse, error){
		resumeLaneBasic: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{"name_kwd":"陈晨","skill_tks":["Go"]}`}, nil
		},
		resumeLaneWork: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{"workExperience":[]}`}, nil
		},
		resumeLaneEducation: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{"education":[]}`}, nil
		},
		resumeLaneProject: func(_ extractorChatRequest) (*extractorChatResponse, error) {
			return &extractorChatResponse{Content: `{"projectExperience":[]}`}, nil
		},
	})
	withRoutingResumeInvoker(t, inv)

	comp, err := NewExtractorComponent(map[string]any{
		"llm_id": "gpt-4o-mini@openai",
		"resume": map[string]any{"enabled": true},
	})
	if err != nil {
		t.Fatalf("NewExtractorComponent: %v", err)
	}
	ext := comp.(*ExtractorComponent)
	in := extractorInputs{
		llmID:   "gpt-4o-mini@openai",
		modelID: "gpt-4o-mini@openai",
		chunks:  []map[string]any{{"id": "ck-cache-1", "text": "陈晨\n熟悉 Go"}},
		cache:   store,
	}
	if err := ext.resume.Run(t.Context(), nil, in, progress.Window{}); err != nil {
		t.Fatalf("first resume run: %v", err)
	}
	if inv.calls.Load() != 4 {
		t.Fatalf("first run calls = %d, want 4", inv.calls.Load())
	}
	in2 := extractorInputs{
		llmID:   "gpt-4o-mini@openai",
		modelID: "gpt-4o-mini@openai",
		chunks:  []map[string]any{{"id": "ck-cache-1", "text": "陈晨\n熟悉 Go"}},
		cache:   store,
	}
	if err := ext.resume.Run(t.Context(), nil, in2, progress.Window{}); err != nil {
		t.Fatalf("second resume run: %v", err)
	}
	if inv.calls.Load() != 4 {
		t.Fatalf("second run should hit per-lane cache with 0 extra LLM calls, total calls = %d", inv.calls.Load())
	}
}

func TestMergeResumeLanes_UngroundedDatesAndDegreeAreNotTrusted(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rawText := "李明\n星河支付有限公司 2018.03 - 2020.06 高级后端工程师\n复旦大学 计算机技术 本科"
	lines := strings.Split(rawText, "\n")

	t.Run("ongoing marker absent from the text makes tenure unknown", func(t *testing.T) {
		profile := mergeResumeLanes(rawText, lines, false, now, resumeLaneOutputs{
			work: &resumeWorkRaw{WorkExperience: []resumeWorkItemRaw{
				{Company: "星河支付有限公司", Position: "高级后端工程师", StartDate: "2018.03", EndDate: "至今"},
			}},
		})
		if profile.WorkExperienceYears != nil {
			t.Fatalf("work_experience_years = %v, want unknown: the text never says 至今", *profile.WorkExperienceYears)
		}
		if len(profile.Companies) != 1 {
			t.Fatalf("companies = %v, the grounded company must still be kept", profile.Companies)
		}
	})

	t.Run("misread month makes tenure unknown", func(t *testing.T) {
		profile := mergeResumeLanes(rawText, lines, false, now, resumeLaneOutputs{
			work: &resumeWorkRaw{WorkExperience: []resumeWorkItemRaw{
				{Company: "星河支付有限公司", StartDate: "2018.03", EndDate: "2023.06"},
			}},
		})
		if profile.WorkExperienceYears != nil {
			t.Fatalf("work_experience_years = %v, want unknown for an end date the text does not carry", *profile.WorkExperienceYears)
		}
	})

	t.Run("dates written in the text are accepted", func(t *testing.T) {
		profile := mergeResumeLanes(rawText, lines, false, now, resumeLaneOutputs{
			work: &resumeWorkRaw{WorkExperience: []resumeWorkItemRaw{
				{Company: "星河支付有限公司", StartDate: "2018.03", EndDate: "2020.06"},
			}},
		})
		if profile.WorkExperienceYears == nil || *profile.WorkExperienceYears != 2.3 {
			t.Fatalf("work_experience_years = %v, want 2.3", profile.WorkExperienceYears)
		}
	})

	t.Run("degree the text never names is dropped", func(t *testing.T) {
		profile := mergeResumeLanes(rawText, lines, false, now, resumeLaneOutputs{
			edu: &resumeEducationRaw{Education: []resumeEducationItemRaw{
				{School: "复旦大学", Degree: "博士"},
				{School: "复旦大学", Degree: "Bachelor"},
			}},
		})
		if profile.HighestDegree != "本科" || profile.HighestDegreeLevel != 4 {
			t.Fatalf("highest degree = (%q, %d), want (本科, 4): 博士 is not in the text", profile.HighestDegree, profile.HighestDegreeLevel)
		}
	})

	t.Run("exact-match degree spellings ground and survive", func(t *testing.T) {
		maText := "李明\n复旦大学\nM.A. in History, 2019 - 2021"
		profile := mergeResumeLanes(maText, strings.Split(maText, "\n"), true, now, resumeLaneOutputs{
			edu: &resumeEducationRaw{Education: []resumeEducationItemRaw{
				{School: "复旦大学", Degree: "MA"},
			}},
		})
		if profile.HighestDegreeLevel != 5 {
			t.Fatalf("highest degree level = %d, want 5: the text names M.A.", profile.HighestDegreeLevel)
		}

		baText := "Li Ming\nFudan University\nBA in Economics"
		profile = mergeResumeLanes(baText, strings.Split(baText, "\n"), true, now, resumeLaneOutputs{
			edu: &resumeEducationRaw{Education: []resumeEducationItemRaw{
				{School: "Fudan University", Degree: "B.A."},
			}},
		})
		if profile.HighestDegreeLevel != 4 {
			t.Fatalf("highest degree level = %d, want 4: the text names BA", profile.HighestDegreeLevel)
		}
	})

	t.Run("exact-match words inside longer words do not ground", func(t *testing.T) {
		noiseText := "Li Ming\nWorked as a Manager in Baltimore since 2020"
		profile := mergeResumeLanes(noiseText, strings.Split(noiseText, "\n"), true, now, resumeLaneOutputs{
			edu: &resumeEducationRaw{Education: []resumeEducationItemRaw{
				{School: "Fudan University", Degree: "MA"},
			}},
		})
		if profile.HighestDegree != "" || profile.HighestDegreeLevel != 0 {
			t.Fatalf("highest degree = (%q, %d), want none: Manager/Baltimore are not degrees", profile.HighestDegree, profile.HighestDegreeLevel)
		}
	})
}

func TestExtractorComponent_ResumeMode_ContractInSystemAndBadReplyNotCached(t *testing.T) {
	store := newFakeCacheStore()
	resumeText := "陈晨\n熟悉 Go"
	var systems, users []string
	var mu sync.Mutex
	handler := func(content string) func(req extractorChatRequest) (*extractorChatResponse, error) {
		return func(req extractorChatRequest) (*extractorChatResponse, error) {
			mu.Lock()
			systems = append(systems, req.Messages[0].Content)
			users = append(users, req.Messages[len(req.Messages)-1].Content)
			mu.Unlock()
			return &extractorChatResponse{Content: content}, nil
		}
	}
	inv := newRoutingResumeInvoker(map[resumeLane]func(req extractorChatRequest) (*extractorChatResponse, error){
		resumeLaneBasic:     handler(`not json at all {{{`),
		resumeLaneWork:      handler(`{"workExperience":[]}`),
		resumeLaneEducation: handler(`{"education":[]}`),
		resumeLaneProject:   handler(`{"projectExperience":[]}`),
	})
	withRoutingResumeInvoker(t, inv)

	comp, err := NewExtractorComponent(map[string]any{"llm_id": "gpt-4o-mini@openai", "resume": map[string]any{"enabled": true}})
	if err != nil {
		t.Fatalf("NewExtractorComponent: %v", err)
	}
	ext := comp.(*ExtractorComponent)
	run := func() {
		in := extractorInputs{
			llmID: "gpt-4o-mini@openai", modelID: "gpt-4o-mini@openai", cache: store,
			chunks: []map[string]any{{"id": "ck-bad-1", "text": resumeText}},
		}
		if err := ext.resume.Run(t.Context(), nil, in, progress.Window{}); err != nil {
			t.Fatalf("resume run: %v", err)
		}
	}
	run()
	run()

	for i, sys := range systems {
		if !strings.Contains(sys, "JSON") || strings.Contains(sys, "熟悉 Go") {
			t.Fatalf("system message %d must carry the output contract and not the resume: %q", i, sys)
		}
		if !strings.Contains(users[i], "[1]: 熟悉 Go") {
			t.Fatalf("user message %d must carry the indexed resume: %q", i, users[i])
		}
	}
	// The unparsable basic reply must not be cached: the second run asks again
	// (5 calls = 4 first run + 1 basic retry), while the three valid lanes hit the cache.
	if got := inv.calls.Load(); got != 5 {
		t.Fatalf("chat calls = %d, want 5 (bad basic reply re-requested, valid lanes cached)", got)
	}
}

// The resume mode owns the extractor: whatever else the params configure,
// the shared phases must not run alongside it.
func TestResumeModeDisablesOtherExtractionPhases(t *testing.T) {
	comp, err := NewExtractorComponent(map[string]any{
		"llm_id":   "gpt-4o-mini@openai",
		"resume":   map[string]any{"enabled": true},
		"keywords": map[string]any{"enabled": true, "top_n": 4},
		"summary":  map[string]any{"enabled": true},
	})
	if err != nil {
		t.Fatalf("NewExtractorComponent: %v", err)
	}
	ext := comp.(*ExtractorComponent)
	if ext.Param.Keywords.Enabled || ext.Param.Questions.Enabled ||
		ext.Param.Tags.Enabled || ext.Param.Summary.Enabled ||
		ext.Param.Metadata.Enabled {
		t.Fatalf("resume mode must disable the other phases, got %+v", ext.Param)
	}
	if ext.resume == nil || !ext.resume.Enabled() {
		t.Fatal("resume mode must stay enabled")
	}

	// Without resume the shared phases keep their configured state.
	comp, err = NewExtractorComponent(map[string]any{
		"llm_id":   "gpt-4o-mini@openai",
		"keywords": map[string]any{"enabled": true, "top_n": 4},
	})
	if err != nil {
		t.Fatalf("NewExtractorComponent without resume: %v", err)
	}
	ext = comp.(*ExtractorComponent)
	if !ext.Param.Keywords.Enabled {
		t.Fatal("keywords must stay enabled without resume")
	}
	if ext.resume != nil && ext.resume.Enabled() {
		t.Fatal("resume must be off when not configured")
	}
}

// A bare {} decodes into every lane payload yet is a noncompliant reply: it
// confirms nothing and must not be cached. Explicit empty arrays do confirm
// absence; a reply omitting a field says nothing about it.
func TestResumeLaneContractPresence(t *testing.T) {
	if resumeLaneReplyValid(resumeLaneWork, `{}`) {
		t.Fatal("a bare {} is noncompliant: it must not count as a lane view")
	}
	if !resumeLaneReplyValid(resumeLaneBasic, `{"skill_tks":[]}`) {
		t.Fatal("an explicit empty list is a compliant confirmation of absence")
	}
	if !resumeLaneReplyValid(resumeLaneWork, `{"workExperience":[]}`) {
		t.Fatal("an explicitly empty work section is compliant")
	}

	outputs := resumeLaneOutputs{present: map[resumeLane]map[string]struct{}{
		resumeLaneBasic: decodeResumeLanePresence(`{"name_kwd":"李四"}`),
	}}
	if got := confirmedManagedMetadataKeys(outputs); !reflect.DeepEqual(got, []string{"name"}) {
		t.Fatalf("confirmed = %#v, want [name]: omitted fields confirm nothing", got)
	}

	outputs.present[resumeLaneBasic] = decodeResumeLanePresence(`{"name_kwd":"李四","language_tks":[]}`)
	if got := confirmedManagedMetadataKeys(outputs); !reflect.DeepEqual(got, []string{"name", "languages"}) {
		t.Fatalf("confirmed = %#v, want [name languages]: an explicit empty list confirms its key", got)
	}

	// JSON null is not an explicit reply: the contract asks for [] / "", so
	// a null value counts for neither the contract nor the confirmation.
	if resumeLaneReplyValid(resumeLaneWork, `{"workExperience":null}`) {
		t.Fatal("JSON null must not count as a lane contract key")
	}
	if p := decodeResumeLanePresence(`{"name_kwd":"李四","skill_tks":null}`); len(p) != 1 {
		t.Fatalf("presence = %#v, want only name_kwd: null says nothing", p)
	}
	if _, ok := decodeResumeLanePresence(`{"name_kwd":"李四","skill_tks":null}`)["skill_tks"]; ok {
		t.Fatal("a null field must not be recorded as present")
	}
}

// A lane that ran on a context-trimmed resume contributes nothing: merging
// its partial view would overwrite complete stored values with partial ones.
func TestResumeTrimmedLaneContributesNothing(t *testing.T) {
	raw := "李明\n技能：Go"
	lines := []string{"李明", "技能：Go"}
	cw := &resumeChunkWork{
		chunkIndex:  0,
		ck:          map[string]any{"id": "ck-trim", "text": raw},
		rawText:     raw,
		indexedText: raw,
		lines:       lines,
		isEnglish:   false,
		slots:       make([]resumeLaneSlot, len(resumeLanes)),
	}
	cw.slots[0] = resumeLaneSlot{lane: resumeLaneBasic, raw: `{"name_kwd":"李明"}`, truncated: true}
	for i, lane := range []resumeLane{resumeLaneWork, resumeLaneEducation, resumeLaneProject} {
		cw.slots[i+1] = resumeLaneSlot{lane: lane, raw: `{}`}
	}
	if err := applyResumeChunkResult(t.Context(), cw, "", time.Now()); err != nil {
		t.Fatalf("applyResumeChunkResult: %v", err)
	}
	if _, has := cw.ck["metadata"]; has {
		t.Fatalf("a trimmed lane must not write metadata: %#v", cw.ck["metadata"])
	}
	if _, has := cw.ck["_managed_metadata_keys"]; has {
		t.Fatal("a trimmed lane must not confirm its keys")
	}
}

// A resume split across chunks would be extracted fragment by fragment, with
// the per-chunk metadata merge keeping the last fragment's view of the
// document: refuse it instead of silently producing fragment-level metadata.
func TestResumeRejectsSplitResume(t *testing.T) {
	comp, err := NewExtractorComponent(map[string]any{
		"llm_id": "gpt-4o-mini@openai",
		"resume": map[string]any{"enabled": true},
	})
	if err != nil {
		t.Fatalf("NewExtractorComponent: %v", err)
	}
	_, err = comp.Invoke(t.Context(), nil, map[string]any{
		"chunks": []map[string]any{{"text": "第一段简历"}, {"text": "第二段简历"}},
	})
	if err == nil {
		t.Fatal("a split resume must fail instead of extracting per fragment")
	}
	if !strings.Contains(err.Error(), "One chunker") {
		t.Fatalf("error should point the operator at the One chunker, got: %v", err)
	}
}
