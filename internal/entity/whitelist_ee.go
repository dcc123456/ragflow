//
// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
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
