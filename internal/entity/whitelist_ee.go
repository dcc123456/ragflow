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

package entity

import (
	"errors"
	"fmt"
	"regexp"
	"unicode/utf8"
)

var (
	ErrWhitelistNotFound      = errors.New("whitelist entry not found")
	ErrWhitelistEmailConflict = errors.New("email conflicts with an existing whitelist entry")
	whitelistEmailPattern     = regexp.MustCompile(`^[\p{L}\p{N}_.-]+@([\p{L}\p{N}_-]+\.)+[\p{L}\p{N}_-]{2,}$`)
)

// WhiteList stores email addresses allowed to register when the whitelist is enabled.
type WhiteList struct {
	ID    int64  `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Email string `gorm:"column:email;size:255;not null;uniqueIndex:idx_white_list_email" json:"email"`
	BaseModel
}

func (WhiteList) TableName() string { return "white_list" }

// ValidateWhitelistEmail checks the whitelist address format without changing the input.
func ValidateWhitelistEmail(value string) error {
	if utf8.RuneCountInString(value) > 255 || !whitelistEmailPattern.MatchString(value) {
		return fmt.Errorf("invalid email address")
	}
	return nil
}
