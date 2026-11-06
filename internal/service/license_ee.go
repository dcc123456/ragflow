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
	"context"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

const (
	// MaxTimeRecords is the maximum number of records to keep (365 days * 24 hours = 8760)
	MaxTimeRecords = 8760
)

// LicenseService time record service
type LicenseService struct {
	timeRecordDAO *dao.TimeRecordDAO
}

// NewLicenseService create TimeRecord service
func NewLicenseService() *LicenseService {
	return &LicenseService{
		timeRecordDAO: dao.NewTimeRecordDAO(),
	}
}

// InsertWithCleanup inserts data and automatically deletes old records if count exceeds MaxTimeRecords
func (s *LicenseService) InsertWithCleanup(ctx context.Context, encryptedData string) (*entity.TimeRecord, error) {
	record := &entity.TimeRecord{
		Data: encryptedData,
	}

	// Insert new record
	if err := s.timeRecordDAO.Create(ctx, dao.DB, record); err != nil {
		return nil, err
	}

	// Cleanup old records if exceeds limit
	if err := s.CleanupOldRecords(ctx); err != nil {
		// Log the error but don't fail the insert
		// The record is already inserted, cleanup can be retried later
		return record, err
	}

	return record, nil
}

// CleanupOldRecords removes old records, keeping only the latest MaxTimeRecords rows
func (s *LicenseService) CleanupOldRecords(ctx context.Context) error {
	// Get current total count
	count, err := s.timeRecordDAO.GetCount(ctx, dao.DB)
	if err != nil {
		return err
	}

	// Delete old records if count exceeds the limit
	if count > MaxTimeRecords {
		// Calculate how many records to delete
		toDelete := count - MaxTimeRecords
		return s.timeRecordDAO.DeleteOldest(ctx, dao.DB, toDelete)
	}

	return nil
}

// GetRecent retrieves the most recently inserted records
func (s *LicenseService) GetRecent(ctx context.Context, limit int) ([]*entity.TimeRecord, error) {
	return s.timeRecordDAO.GetRecent(ctx, dao.DB, limit)
}

// GetCount returns the total number of records
func (s *LicenseService) GetCount(ctx context.Context) (int64, error) {
	return s.timeRecordDAO.GetCount(ctx, dao.DB)
}

// GetByID retrieves a single record by its ID
func (s *LicenseService) GetByID(ctx context.Context, id int64) (*entity.TimeRecord, error) {
	return s.timeRecordDAO.GetByID(ctx, dao.DB, id)
}
