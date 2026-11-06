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

package billing

import (
	"context"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"
)

func seedDeepdocPrice(t *testing.T, service *Service, points int64) {
	t.Helper()
	point := &entity.BillingPricePoint{
		ID:                   common.GenerateUUID(),
		ProductName:          "deepdoc",
		PriceType:            "usage_based",
		BillingFrequency:     "one_time",
		ConsumingPointAmount: points,
		EffectiveTime:        time.Now().UTC(),
	}
	if err := service.productDAO.UpsertPricePoint(context.Background(), service.db, point); err != nil {
		t.Fatalf("seed price point: %v", err)
	}
}

func TestQuoteParseSkipsNonDeepdoc(t *testing.T) {
	service := newTestService(t)
	seedDeepdocPrice(t, service, 3)
	called := false
	quotes, err := service.QuoteParse(context.Background(), "demo.pdf", []byte("%PDF"), map[string]interface{}{"layout_recognize": "Plain Text"}, func(string, []byte) (int, error) {
		called = true
		return 8, nil
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if len(quotes) != 0 || called {
		t.Fatalf("non-deepdoc pdf must not be metered: quotes=%v called=%v", quotes, called)
	}
}

func TestQuoteParseMetersDeepdocPages(t *testing.T) {
	service := newTestService(t)
	seedDeepdocPrice(t, service, 3)
	quotes, err := service.QuoteParse(context.Background(), "demo.pdf", []byte("%PDF"), map[string]interface{}{"layout_recognize": "DeepDOC"}, func(string, []byte) (int, error) {
		return 8, nil
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("quotes = %v", quotes)
	}
	if quotes[0].Units != 8 || quotes[0].Points != 24 || quotes[0].PageRange != "1-8" {
		t.Fatalf("quote = %+v", quotes[0])
	}
}

func TestHoldForParseAndFinalize(t *testing.T) {
	service := newTestService(t)
	seedDeepdocPrice(t, service, 3)
	ctx := context.Background()

	quotes, err := service.QuoteParse(ctx, "demo.pdf", []byte("%PDF"), map[string]interface{}{"layout_recognize": "DeepDOC"}, func(string, []byte) (int, error) {
		return 2, nil
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	hold, err := service.HoldForParse(ctx, "tenant-p", "doc-p", quotes)
	if err != nil || hold == nil {
		t.Fatalf("hold: %v %v", hold, err)
	}
	balance, _ := service.GetBalance(ctx, "tenant-p")
	if balance.ConsumedPlanPoints != 6 || balance.HeldPoints != 6 {
		t.Fatalf("balance after hold = %+v", balance)
	}
	if _, err := service.FinalizeParseHold(ctx, hold.ID, true); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	balance, _ = service.GetBalance(ctx, "tenant-p")
	if balance.HeldPoints != 0 || balance.ConsumedPlanPoints != 6 {
		t.Fatalf("balance after commit = %+v", balance)
	}
}

func TestHoldForParseSkipsNonDeepdoc(t *testing.T) {
	service := newTestService(t)
	seedDeepdocPrice(t, service, 3)
	hold, err := service.HoldForParse(context.Background(), "tenant-q", "doc-q", nil)
	if err != nil || hold != nil {
		t.Fatalf("expected no hold, got %v %v", hold, err)
	}
}

func TestParseChargesIndependentRuns(t *testing.T) {
	service := newTestService(t)
	seedDeepdocPrice(t, service, 2)
	ctx := context.Background()
	docID := "doc-run"

	for _, runKey := range []string{"run-a", "run-b"} {
		if err := service.ChargeParsedDocument(ctx, "tenant-r", docID, runKey, 2, true); err != nil {
			t.Fatalf("charge run %s: %v", runKey, err)
		}
	}
	balance, _ := service.GetBalance(ctx, "tenant-r")
	if balance.ConsumedPlanPoints != 8 {
		t.Fatalf("consumed plan points = %d, want 8", balance.ConsumedPlanPoints)
	}
}
