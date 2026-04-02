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

package service

import (
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
func (s *LicenseService) InsertWithCleanup(encryptedData string) (*entity.TimeRecord, error) {
	record := &entity.TimeRecord{
		Data: encryptedData,
	}

	// Insert new record
	if err := s.timeRecordDAO.Create(record); err != nil {
		return nil, err
	}

	// Cleanup old records if exceeds limit
	if err := s.CleanupOldRecords(); err != nil {
		// Log the error but don't fail the insert
		// The record is already inserted, cleanup can be retried later
		return record, err
	}

	return record, nil
}

// CleanupOldRecords removes old records, keeping only the latest MaxTimeRecords rows
func (s *LicenseService) CleanupOldRecords() error {
	// Get current total count
	count, err := s.timeRecordDAO.GetCount()
	if err != nil {
		return err
	}

	// Delete old records if count exceeds the limit
	if count > MaxTimeRecords {
		// Calculate how many records to delete
		toDelete := count - MaxTimeRecords
		return s.timeRecordDAO.DeleteOldest(toDelete)
	}

	return nil
}

// GetRecent retrieves the most recently inserted records
func (s *LicenseService) GetRecent(limit int) ([]*entity.TimeRecord, error) {
	return s.timeRecordDAO.GetRecent(limit)
}

// GetCount returns the total number of records
func (s *LicenseService) GetCount() (int64, error) {
	return s.timeRecordDAO.GetCount()
}

// GetByID retrieves a single record by its ID
func (s *LicenseService) GetByID(id int64) (*entity.TimeRecord, error) {
	return s.timeRecordDAO.GetByID(id)
}
