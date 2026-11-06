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

package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"

	"github.com/xuri/excelize/v2"
)

const (
	maxWhitelistFileBytes = 5 << 20
	maxWhitelistRows      = 10000
	maxWhitelistXMLBytes  = 32 << 20
)

type whitelistInsertResult struct {
	added   int64
	skipped int64
}

func (s *Service) ShowWhiteList(ctx context.Context) ([]entity.WhiteList, error) {
	return dao.NewWhitelistDAO().List(ctx, dao.DB)
}

func whitelistEmail(value string) (string, error) {
	if err := entity.ValidateWhitelistEmail(value); err != nil {
		return "", common.NewCodedError(common.CodeBadRequest, "Invalid email address")
	}
	return value, nil
}

func (s *Service) AddWhiteList(ctx context.Context, value string) (whitelistInsertResult, error) {
	email, err := whitelistEmail(value)
	if err != nil {
		return whitelistInsertResult{}, err
	}
	return s.insertWhitelistEmails(ctx, []string{email})
}

func (s *Service) insertWhitelistEmails(ctx context.Context, emails []string) (whitelistInsertResult, error) {
	unique := make([]string, 0, len(emails))
	seen := make(map[string]bool, len(emails))
	for _, email := range emails {
		if !seen[email] {
			unique = append(unique, email)
			seen[email] = true
		}
	}
	added, err := dao.NewWhitelistDAO().InsertEmails(ctx, dao.DB, unique)
	if err != nil {
		return whitelistInsertResult{}, whitelistOperationError(err)
	}
	return whitelistInsertResult{added: added, skipped: int64(len(emails)) - added}, nil
}

func (s *Service) UpdateWhiteList(ctx context.Context, id int64, value string) (string, error) {
	if id <= 0 {
		return "", common.NewCodedError(common.CodeBadRequest, "Invalid whitelist ID")
	}
	email, err := whitelistEmail(value)
	if err != nil {
		return "", err
	}
	previous, updated, err := dao.NewWhitelistDAO().UpdateEmail(ctx, dao.DB, id, email)
	if err != nil {
		return "", whitelistOperationError(err)
	}
	if !updated {
		return fmt.Sprintf("Email %s is already in whitelist.", email), nil
	}
	return fmt.Sprintf("Successfully updated the email from %s to %s.", previous, email), nil
}

func (s *Service) DeleteWhiteList(ctx context.Context, value string) (int64, error) {
	return dao.NewWhitelistDAO().DeleteByEmail(ctx, dao.DB, value)
}

func (s *Service) BatchDeleteWhiteList(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 || len(ids) > maxWhitelistRows {
		return 0, common.NewCodedError(common.CodeBadRequest, "Provide between 1 and 10000 whitelist IDs")
	}
	for _, id := range ids {
		if id <= 0 {
			return 0, common.NewCodedError(common.CodeBadRequest, "Invalid whitelist ID")
		}
	}
	return dao.NewWhitelistDAO().DeleteByIDs(ctx, dao.DB, ids)
}

func (s *Service) BatchAddWhiteList(ctx context.Context, filename string, reader io.Reader) (whitelistInsertResult, error) {
	if reader == nil || strings.ToLower(filepath.Ext(filename)) != ".xlsx" {
		return whitelistInsertResult{}, common.NewCodedError(common.CodeBadRequest, "Upload an .xlsx file")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxWhitelistFileBytes+1))
	if err != nil {
		return whitelistInsertResult{}, fmt.Errorf("read whitelist upload: %w", err)
	}
	if len(data) > maxWhitelistFileBytes {
		return whitelistInsertResult{}, common.NewCodedError(common.CodeBadRequest, "Whitelist file exceeds 5 MiB")
	}
	emails, err := readWhitelistXLSX(ctx, bytes.NewReader(data))
	if err != nil {
		return whitelistInsertResult{}, err
	}
	return s.insertWhitelistEmails(ctx, emails)
}

func whitelistOperationError(err error) error {
	switch {
	case errors.Is(err, entity.ErrWhitelistNotFound):
		return common.NewCodedError(common.CodeNotFound, "Whitelist entry not found")
	case errors.Is(err, entity.ErrWhitelistEmailConflict):
		return common.NewCodedError(common.CodeConflict, "Email conflicts with an existing whitelist entry")
	default:
		return err
	}
}

func readWhitelistXLSX(ctx context.Context, reader io.Reader) ([]string, error) {
	book, err := excelize.OpenReader(reader, excelize.Options{UnzipSizeLimit: maxWhitelistXMLBytes, UnzipXMLSizeLimit: 1 << 20})
	if err != nil {
		return nil, common.NewCodedError(common.CodeBadRequest, "Invalid or oversized .xlsx workbook")
	}
	defer book.Close()
	sheets := book.GetSheetList()
	if len(sheets) == 0 {
		return nil, common.NewCodedError(common.CodeBadRequest, "Workbook has no worksheets")
	}
	rows, err := book.Rows(sheets[0])
	if err != nil {
		return nil, common.NewCodedError(common.CodeBadRequest, "Cannot read worksheet")
	}
	defer rows.Close()
	column, rowNumber := -1, 0
	emails := make([]string, 0)
	for rows.Next() {
		rowNumber++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if rowNumber > maxWhitelistRows+1 {
			return nil, common.NewCodedError(common.CodeBadRequest, "Workbook exceeds 10000 data rows")
		}
		values, err := rows.Columns()
		if err != nil {
			return nil, common.NewCodedError(common.CodeBadRequest, fmt.Sprintf("Cannot read row %d", rowNumber))
		}
		if rowNumber == 1 {
			for index, value := range values {
				if strings.EqualFold(strings.TrimSpace(value), "email") {
					if column != -1 {
						return nil, common.NewCodedError(common.CodeBadRequest, "Worksheet has duplicate email columns")
					}
					column = index
				}
			}
			if column == -1 {
				return nil, common.NewCodedError(common.CodeBadRequest, "Worksheet requires an email column")
			}
			continue
		}
		blank := true
		for _, value := range values {
			blank = blank && strings.TrimSpace(value) == ""
		}
		if blank {
			continue
		}
		if column >= len(values) {
			return nil, common.NewCodedError(common.CodeBadRequest, fmt.Sprintf("Missing email at row %d", rowNumber))
		}
		email, err := whitelistEmail(values[column])
		if err != nil {
			return nil, common.NewCodedError(common.CodeBadRequest, fmt.Sprintf("Invalid email at row %d", rowNumber))
		}
		cell, err := excelize.CoordinatesToCellName(column+1, rowNumber)
		if err != nil {
			return nil, common.NewCodedError(common.CodeBadRequest, "Invalid email column")
		}
		if formula, err := book.GetCellFormula(sheets[0], cell); err != nil || formula != "" {
			return nil, common.NewCodedError(common.CodeBadRequest, fmt.Sprintf("Email at row %d must be a literal value", rowNumber))
		}
		emails = append(emails, email)
	}
	if err := rows.Error(); err != nil {
		return nil, common.NewCodedError(common.CodeBadRequest, "Cannot read worksheet rows")
	}
	if len(emails) == 0 {
		return nil, common.NewCodedError(common.CodeBadRequest, "Worksheet contains no emails")
	}
	return emails, nil
}
