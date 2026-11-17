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

package models

import (
	"context"
	"testing"
	"time"

	"ragflow/internal/common"
)

// usageCaptureDriver records the usage pointer each modality method forwards
// to the driver, so tests can assert what a nil-usage call site ends up with.
type usageCaptureDriver struct {
	ModelDriver
	chatUsage   *common.ModelUsage
	embedUsage  *common.ModelUsage
	rerankUsage *common.ModelUsage
}

func (d *usageCaptureDriver) ChatWithMessages(_ context.Context, _ string, _ []Message, _ *APIConfig, _ *ChatConfig, usage *common.ModelUsage) (*ChatResponse, error) {
	d.chatUsage = usage
	return &ChatResponse{}, nil
}

func (d *usageCaptureDriver) Embed(_ context.Context, _ *string, req EmbedRequest, _ *APIConfig, _ *EmbeddingConfig, usage *common.ModelUsage) ([]EmbeddingData, error) {
	d.embedUsage = usage
	out := make([]EmbeddingData, len(req.Texts))
	for i := range out {
		out[i] = EmbeddingData{Index: i, Embedding: []float64{0.1}}
	}
	return out, nil
}

func (d *usageCaptureDriver) Rerank(_ context.Context, _ *string, _ RerankRequest, _ *APIConfig, _ *RerankConfig, usage *common.ModelUsage) (*RerankResponse, error) {
	d.rerankUsage = usage
	return &RerankResponse{}, nil
}

func TestChatModelUsageTemplateFillsNilUsage(t *testing.T) {
	startAt := time.Now()
	template := &common.ModelUsage{
		UserID:         "user-1",
		TenantID:       "tenant-1",
		ProviderName:   "OpenAI",
		InstanceID:     "instance-1",
		ModelName:      "gpt-test",
		GroupID:        "group-1",
		GroupName:      "failover",
		InputTokens:    11,
		OutputTokens:   7,
		TotalTokens:    18,
		RequestID:      "req-1",
		ResponseTimeMS: 42,
		StartAt:        startAt,
	}
	info := &ModelInfo{GroupID: "group-1", GroupName: "failover", UsageTemplate: template}
	driver := &usageCaptureDriver{}
	model := NewChatModelWithInfo(driver, strPtr("gpt-test"), &APIConfig{}, info)

	if _, err := model.ChatWithMessages(context.Background(), []Message{{Content: "hi"}}, nil, nil); err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	usage := driver.chatUsage
	if usage == nil {
		t.Fatal("driver received nil usage; expected template fallback")
	}
	if usage.UserID != "user-1" || usage.TenantID != "tenant-1" || usage.GroupID != "group-1" || usage.GroupName != "failover" {
		t.Fatalf("fallback usage lost identity: %+v", usage)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.TotalTokens != 0 || usage.RequestID != "" || usage.ResponseTimeMS != 0 || !usage.StartAt.IsZero() {
		t.Fatalf("fallback usage carried stale call-scoped fields: %+v", usage)
	}
	if template.InputTokens != 11 || template.OutputTokens != 7 || template.TotalTokens != 18 || template.RequestID != "req-1" || template.StartAt != startAt {
		t.Fatalf("template was mutated by the call: %+v", template)
	}
}

func TestChatModelExplicitUsageWinsAndNilTemplateKeepsNil(t *testing.T) {
	driver := &usageCaptureDriver{}

	explicit := &common.ModelUsage{UserID: "explicit"}
	model := NewChatModelWithInfo(driver, strPtr("gpt-test"), &APIConfig{}, &ModelInfo{UsageTemplate: &common.ModelUsage{UserID: "template"}})
	if _, err := model.ChatWithMessages(context.Background(), nil, nil, explicit); err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if driver.chatUsage != explicit {
		t.Fatal("explicit usage was replaced by the fallback")
	}

	plain := NewChatModelWithInfo(driver, strPtr("gpt-test"), &APIConfig{}, nil)
	if _, err := plain.ChatWithMessages(context.Background(), nil, nil, nil); err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if driver.chatUsage != nil {
		t.Fatalf("nil template should keep the nil behaviour, got %+v", driver.chatUsage)
	}
}

func TestChatModelUsageResolverTopsUpGroupOnExplicitUsage(t *testing.T) {
	driver := &usageCaptureDriver{}

	groupInfo := &ModelInfo{GroupID: "group-1", GroupName: "failover", UsageTemplate: &common.ModelUsage{UserID: "user-1"}}
	model := NewChatModelWithInfo(driver, strPtr("gpt-test"), &APIConfig{}, groupInfo)
	explicit := &common.ModelUsage{UserID: "explicit"}
	if _, err := model.ChatWithMessages(context.Background(), nil, nil, explicit); err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if driver.chatUsage.GroupID != "group-1" || driver.chatUsage.GroupName != "failover" || driver.chatUsage.UserID != "explicit" {
		t.Fatalf("explicit usage group attribution = %q/%q user %q, want group-1/failover/explicit", driver.chatUsage.GroupID, driver.chatUsage.GroupName, driver.chatUsage.UserID)
	}

	directInfo := &ModelInfo{UsageTemplate: &common.ModelUsage{UserID: "user-1"}}
	direct := NewChatModelWithInfo(driver, strPtr("gpt-test"), &APIConfig{}, directInfo)
	directExplicit := &common.ModelUsage{UserID: "explicit"}
	if _, err := direct.ChatWithMessages(context.Background(), nil, nil, directExplicit); err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if driver.chatUsage != directExplicit || driver.chatUsage.GroupID != "" {
		t.Fatalf("direct model must not stamp group identity: %+v", driver.chatUsage)
	}

	carried := &common.ModelUsage{UserID: "explicit", GroupID: "group-9", GroupName: "own"}
	if _, err := model.ChatWithMessages(context.Background(), nil, nil, carried); err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if driver.chatUsage.GroupID != "group-9" || driver.chatUsage.GroupName != "own" {
		t.Fatalf("caller-provided group attribution must win: %+v", driver.chatUsage)
	}
}

func TestEmbeddingModelUsageTemplateFillsNilUsage(t *testing.T) {
	template := &common.ModelUsage{
		UserID:       "user-1",
		TenantID:     "tenant-1",
		ModelName:    "embed-test",
		ProviderName: "OpenAI",
	}
	driver := &usageCaptureDriver{}
	info := &ModelInfo{UsageTemplate: template}
	model := NewEmbeddingModelWithInfo(driver, strPtr("embed-test"), &APIConfig{}, 0, info)

	if _, err := model.Embed(context.Background(), EmbedRequest{Texts: []string{"a", "b"}}, nil, nil); err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	usage := driver.embedUsage
	if usage == nil {
		t.Fatal("driver received nil usage; expected template fallback")
	}
	if usage.UserID != "user-1" || usage.TenantID != "tenant-1" {
		t.Fatalf("fallback embedding usage lost identity: %+v", usage)
	}
}

func TestRerankModelUsageTemplateFillsNilUsage(t *testing.T) {
	template := &common.ModelUsage{TenantID: "tenant-1", ModelName: "rerank-test"}
	driver := &usageCaptureDriver{}
	info := &ModelInfo{UsageTemplate: template}
	model := NewRerankModelWithInfo(driver, strPtr("rerank-test"), &APIConfig{}, 0, info)

	if _, err := model.Rerank(context.Background(), RerankRequest{Query: "q", Documents: []string{"d"}}, nil, nil); err != nil {
		t.Fatalf("rerank failed: %v", err)
	}
	if driver.rerankUsage == nil || driver.rerankUsage.TenantID != "tenant-1" {
		t.Fatalf("fallback rerank usage lost identity: %+v", driver.rerankUsage)
	}
}
