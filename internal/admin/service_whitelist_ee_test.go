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

package admin

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/ingestion/testutil"
)

func requireWhitelistCode(t *testing.T, err error, code common.ErrorCode) {
	t.Helper()
	var coded *common.CodedError
	if !errors.As(err, &coded) || coded.Code != code {
		t.Fatalf("error = %v, want code %v", err, code)
	}
}

func TestWhitelistServiceValidatesBeforePersistence(t *testing.T) {
	t.Cleanup(testutil.ReplaceDBForTest(t, nil))
	service := &Service{}
	_, err := service.AddWhiteList(t.Context(), "invalid")
	requireWhitelistCode(t, err, common.CodeBadRequest)
	_, err = service.UpdateWhiteList(t.Context(), 0, "alice@example.com")
	requireWhitelistCode(t, err, common.CodeBadRequest)
	_, err = service.UpdateWhiteList(t.Context(), 42, "invalid")
	requireWhitelistCode(t, err, common.CodeBadRequest)
	for _, ids := range [][]int64{nil, {0}, {-1}, make([]int64, maxWhitelistRows+1)} {
		_, err = service.BatchDeleteWhiteList(t.Context(), ids)
		requireWhitelistCode(t, err, common.CodeBadRequest)
	}
	invalid := whitelistWorkbook(t, [][]string{{"email"}, {"alice@example.com"}, {"invalid"}})
	_, err = service.BatchAddWhiteList(t.Context(), "emails.xlsx", bytes.NewReader(invalid))
	requireWhitelistCode(t, err, common.CodeBadRequest)
	_, err = service.BatchAddWhiteList(t.Context(), "emails.csv", bytes.NewReader(invalid))
	requireWhitelistCode(t, err, common.CodeBadRequest)
	_, err = service.BatchAddWhiteList(t.Context(), "emails.xlsx", nil)
	requireWhitelistCode(t, err, common.CodeBadRequest)
	_, err = service.BatchAddWhiteList(t.Context(), "emails.xlsx", bytes.NewReader(make([]byte, maxWhitelistFileBytes+1)))
	requireWhitelistCode(t, err, common.CodeBadRequest)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	valid := whitelistWorkbook(t, [][]string{{"email"}, {"alice@example.com"}})
	_, err = service.BatchAddWhiteList(ctx, "emails.xlsx", bytes.NewReader(valid))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled import = %v", err)
	}
}
