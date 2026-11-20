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

package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"ragflow/internal/agent/canvas"
	"ragflow/internal/compliance"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/storage"
	"ragflow/internal/tokenizer"
)

func TestSensitiveQuestionsSkipChatSearchAndAgentExecution(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.SystemSettings{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	factory := storage.GetStorageFactory()
	previous := factory.GetStorage()
	store := storage.NewMemoryStorage()
	factory.SetStorage(store)
	t.Cleanup(func() { factory.SetStorage(previous) })
	tokenizer.SetEngineType("infinity")
	t.Cleanup(func() { tokenizer.SetEngineType("") })
	for _, setting := range []entity.SystemSettings{
		{Name: compliance.EnabledSetting, Source: "variable", DataType: "bool", Value: "true"},
		{Name: compliance.ResponseSetting, Source: "variable", DataType: "string", Value: "Restricted question"},
	} {
		if err := db.Create(&setting).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Put(t.Context(), compliance.Bucket, compliance.ObjectKey, []byte("restricted")); err != nil {
		t.Fatal(err)
	}
	for _, content := range []any{
		"restricted question",
		[]any{map[string]any{"type": "text", "text": "restricted question"}},
	} {
		for _, stream := range []bool{false, true} {
			results, err := (&ChatPipelineService{}).AsyncChat(t.Context(), &entity.Chat{ID: "chat"}, []map[string]interface{}{{"role": "user", "content": content}}, stream, nil)
			if err != nil {
				t.Fatalf("chat error = %v", err)
			}
			var answers []AsyncChatResult
			for result := range results {
				answers = append(answers, result)
			}
			wantCount := 1
			if stream {
				wantCount = 2
			}
			if len(answers) != wantCount || answers[len(answers)-1].Answer != "Restricted question" || !answers[len(answers)-1].Final {
				t.Fatalf("chat results = %+v; want normal filter response", answers)
			}
			if stream && (answers[0].Answer != "Restricted question" || answers[0].Final) {
				t.Fatalf("chat stream missing response delta: %+v", answers)
			}
		}
	}
	var deltas []AskDelta
	for delta := range (&AskService{}).Stream(t.Context(), nil, "restricted question", []string{"dataset"}) {
		deltas = append(deltas, delta)
	}
	if len(deltas) != 1 || deltas[0].Kind != AskDeltaFinal || deltas[0].Value != "" || deltas[0].Message != "Restricted question" {
		t.Fatalf("search events = %+v", deltas)
	}
	if !reflect.DeepEqual(deltas[0].Refs, map[string]interface{}{"chunks": []map[string]interface{}{}, "doc_aggs": []map[string]interface{}{}}) {
		t.Fatalf("search references = %+v", deltas[0].Refs)
	}
	input := map[string]any{"query": "restricted question"}
	events := canvas.NewRunner().Run(t.Context(), (&AgentService{}).buildRunFunc("agent", nil, nil), "agent", "session", input, map[string]any{
		"__message_id__": "message",
		"user_input":     input,
	})
	var types []string
	for event := range events {
		types = append(types, event.Type)
		if event.Type == "message" {
			var message canvas.MessageEvent
			if err := json.Unmarshal([]byte(event.Data), &message); err != nil || message.Content != "Restricted question" {
				t.Fatalf("message = %s, %v", event.Data, err)
			}
		}
	}
	if !reflect.DeepEqual(types, []string{"workflow_started", "message", "message_end", "workflow_finished"}) {
		t.Fatalf("agent event types = %v", types)
	}
}
