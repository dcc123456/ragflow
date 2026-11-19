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

package dao

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"ragflow/internal/entity"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WhitelistDAO owns whitelist persistence and transaction boundaries.
type WhitelistDAO struct{}

func NewWhitelistDAO() *WhitelistDAO {
	return &WhitelistDAO{}
}

func (d *WhitelistDAO) List(ctx context.Context, db *gorm.DB) ([]entity.WhiteList, error) {
	entries := make([]entity.WhiteList, 0)
	if err := db.WithContext(ctx).Order("id ASC").Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("list whitelist: %w", err)
	}
	return entries, nil
}

// InsertEmails atomically inserts addresses, skipping existing database matches.
func (d *WhitelistDAO) InsertEmails(ctx context.Context, db *gorm.DB, emails []string) (int64, error) {
	if len(emails) == 0 {
		return 0, nil
	}
	entries := make([]entity.WhiteList, len(emails))
	for i, email := range emails {
		entries[i].Email = email
	}
	// Acquire unique-index locks in the same order for concurrent imports.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Email < entries[j].Email })
	var added int64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "email"}}, DoNothing: true}).CreateInBatches(&entries, 200)
		added = result.RowsAffected
		if result.Error != nil {
			return result.Error
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("insert whitelist emails: %w", whitelistStorageError(err))
	}
	return added, nil
}

// UpdateEmail returns the previous address and whether a row was updated.
// An existing database match is a successful no-op.
func (d *WhitelistDAO) UpdateEmail(ctx context.Context, db *gorm.DB, id int64, email string) (string, bool, error) {
	var previous string
	var updated bool
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing entity.WhiteList
		err := tx.Select("email").Where("email = ?", email).Take(&existing).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var entry entity.WhiteList
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&entry).Error; err != nil {
			return err
		}
		previous = entry.Email
		if err := tx.Model(&entry).Update("email", email).Error; err != nil {
			return err
		}
		updated = true
		return nil
	})
	if err != nil {
		return "", false, fmt.Errorf("update whitelist email: %w", whitelistStorageError(err))
	}
	return previous, updated, nil
}

func (d *WhitelistDAO) DeleteByIDs(ctx context.Context, db *gorm.DB, ids []int64) (int64, error) {
	result := db.WithContext(ctx).Where("id IN ?", ids).Delete(&entity.WhiteList{})
	if result.Error != nil {
		return 0, fmt.Errorf("delete whitelist entries: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func whitelistStorageError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return entity.ErrWhitelistNotFound
	}
	var mysqlError *mysqldriver.MySQLError
	if errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &mysqlError) && mysqlError.Number == 1062) {
		return entity.ErrWhitelistEmailConflict
	}
	return err
}
