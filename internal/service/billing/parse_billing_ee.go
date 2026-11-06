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
	"fmt"
	"path/filepath"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/entity"
)

// ParseQuote is one metered parse unit of a document.
type ParseQuote struct {
	ProductName string `json:"product_name"`
	Units       int64  `json:"units"`
	Points      int64  `json:"points"`
	PageRange   string `json:"page_range"`
	Unit        string `json:"unit"`
}

// PageCounter returns the page count of a document. It is injected so tests
// do not need to parse a real PDF.
type PageCounter func(filename string, blob []byte) (int, error)

// QuoteParse estimates the parse charge of a file.
// Only PDFs parsed by DeepDOC are metered, matching the product rules.
func (s *Service) QuoteParse(ctx context.Context, filename string, blob []byte, parserConfig map[string]interface{}, countPages PageCounter) ([]ParseQuote, error) {
	if s == nil || !s.cfg.Enabled {
		return nil, nil
	}
	if !isPDF(filename) || !isDeepdocParser(parserConfig) {
		return nil, nil
	}
	if countPages == nil {
		return nil, fmt.Errorf("page counter is required for PDF billing")
	}
	pages, err := countPages(filename, blob)
	if err != nil {
		return nil, err
	}
	if pages <= 0 {
		return nil, nil
	}

	pointAmount := int64(0)
	if point, err := s.productDAO.GetPricePointByName(ctx, s.db, "deepdoc"); err == nil && point != nil {
		pointAmount = point.ConsumingPointAmount
	}
	if pointAmount <= 0 {
		return nil, nil
	}
	return []ParseQuote{{
		ProductName: "deepdoc",
		Units:       int64(pages),
		Points:      int64(pages) * pointAmount,
		PageRange:   fmt.Sprintf("1-%d", pages),
		Unit:        "page",
	}}, nil
}

// HoldForParse reserves the quoted points and returns the hold row.
func (s *Service) HoldForParse(ctx context.Context, tenantID, docID string, quotes []ParseQuote) (*entity.BillingPointHold, error) {
	var total int64
	metadata := map[string]interface{}{"doc_id": docID}
	for _, quote := range quotes {
		total += quote.Points
		metadata["page_range"] = quote.PageRange
		metadata["units"] = quote.Units
	}
	if total <= 0 {
		return nil, nil
	}
	return s.Hold(ctx, tenantID, docID, total, ParseHoldKey(docID, common.GenerateUUID()), metadata)
}

// ParseHoldKey is the idempotency key of a document's parse hold for one run.
func ParseHoldKey(docID, runKey string) string {
	return "parse:" + docID + ":" + runKey
}

// FinalizeParseHold commits a successful parse and releases a failed one.
func (s *Service) FinalizeParseHold(ctx context.Context, holdID string, success bool) (bool, error) {
	if success {
		return s.CommitHold(ctx, holdID)
	}
	return s.ReleaseHold(ctx, holdID)
}

func isPDF(filename string) bool {
	return strings.EqualFold(filepath.Ext(filename), ".pdf")
}

func isDeepdocParser(parserConfig map[string]interface{}) bool {
	if parserConfig == nil {
		return false
	}
	value, ok := parserConfig["layout_recognize"]
	if !ok {
		return false
	}
	text, ok := value.(string)
	if !ok {
		return false
	}
	return strings.Contains(strings.ToUpper(text), "DEEPDOC")
}

// ChargeParsedDocument bills a completed parse. The hold is created and
// committed in one step because the page count is known only after parsing.
// runKey scopes the idempotency key to one ingestion attempt so independent
// reparses are charged separately.
func (s *Service) ChargeParsedDocument(ctx context.Context, tenantID, docID, runKey string, pageCount int64, deepdoc bool) error {
	if s == nil || !s.cfg.Enabled || !deepdoc || pageCount <= 0 {
		return nil
	}
	if tenantID == "" || docID == "" {
		return nil
	}
	pointAmount := int64(0)
	if point, err := s.productDAO.GetPricePointByName(ctx, s.db, "deepdoc"); err == nil && point != nil {
		pointAmount = point.ConsumingPointAmount
	}
	if pointAmount <= 0 {
		return nil
	}
	points := pageCount * pointAmount
	metadata := map[string]interface{}{
		"doc_id":     docID,
		"page_range": fmt.Sprintf("1-%d", pageCount),
		"units":      pageCount,
	}
	hold, err := s.Hold(ctx, tenantID, docID, points, ParseHoldKey(docID, runKey), metadata)
	if err != nil {
		return err
	}
	if hold == nil {
		return nil
	}
	_, err = s.CommitHold(ctx, hold.ID)
	return err
}
