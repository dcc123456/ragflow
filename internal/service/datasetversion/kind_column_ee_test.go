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

//go:build ee

package datasetversion

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"ragflow/internal/entity"
)

// TestEveryVersionKindFitsItsColumn pins the assumption every writer makes: a
// kind has to fit the column that stores it.
//
// The column was 16 characters and document_metadata is 17, so every metadata
// edit was rejected on insert - and because a version write is best effort, the
// request still answered success and only a warning on the timeline said the row
// never landed. The kinds are spelled out for the reader, so the length has to
// be checked against the names rather than the other way round.
func TestEveryVersionKindFitsItsColumn(t *testing.T) {
	field, ok := reflect.TypeOf(entity.DatasetVersion{}).FieldByName("Kind")
	if !ok {
		t.Fatal("DatasetVersion has no Kind field")
	}
	size := 0
	for _, part := range strings.Split(field.Tag.Get("gorm"), ";") {
		if value, found := strings.CutPrefix(part, "size:"); found {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				t.Fatalf("parse the kind column size %q: %v", value, err)
			}
			size = parsed
		}
	}
	if size <= 0 {
		t.Fatal("the Kind field declares no column size to check the kinds against")
	}
	if len(recordedVersionKinds) == 0 {
		t.Fatal("no version kinds are registered to check")
	}
	for kind := range recordedVersionKinds {
		if len(kind) > size {
			t.Errorf("kind %q is %d characters, longer than the kind column (%d)", kind, len(kind), size)
		}
	}
}
