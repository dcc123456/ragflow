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
	"context"
	"fmt"

	"gorm.io/gorm"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

// stateKey carries a flow's pending per-document states.
type stateKey struct{}

// stateSink collects the states a flow writes while the version that owns them
// has no number yet.
//
// A flow that changes documents and then records one version for the change
// (an upload, a parse, a delete) knows the number only at the end: the number
// is allocated in the same transaction that stores the version row, and the
// version row is written last so its snapshot shows the finished state. The
// per-document states it moved have to be addressed by that number all the same
// - that is what lets a replay read them by version instead of by clock - so
// they are parked in a sink and written once the number exists.
//
// suppressed drops the states instead of collecting them: the caller records
// them itself, under a number it allocates after the write (the restore replay).
type stateSink struct {
	rows       []*entity.DocumentVersion
	suppressed bool
}

// WithStateSink opens a state sink for the flow started with the returned
// context. A nested flow reuses the sink it inherited, so the outermost version
// takes every state the flow wrote.
func WithStateSink(ctx context.Context) context.Context {
	if sink, ok := ctx.Value(stateKey{}).(*stateSink); ok && sink != nil {
		return ctx
	}
	return context.WithValue(ctx, stateKey{}, &stateSink{})
}

// WithStateWritesSuppressed opens a sink that drops what it is offered. It is
// for a caller that records the states itself after the change, with the number
// of the version it records (see document.RecordReplayedDocumentStatesAtVersionEE).
func WithStateWritesSuppressed(ctx context.Context) context.Context {
	return context.WithValue(ctx, stateKey{}, &stateSink{suppressed: true})
}

// EnqueueStates offers the states to the flow's sink.
//
// It reports whether the caller may stop: true when the states are handled here
// (collected, or dropped by design), false when the flow opened no sink and the
// caller has to record the version that describes them.
func EnqueueStates(ctx context.Context, rows []*entity.DocumentVersion) bool {
	if len(rows) == 0 {
		return false
	}
	sink, ok := ctx.Value(stateKey{}).(*stateSink)
	if !ok || sink == nil {
		return false
	}
	if sink.suppressed {
		return true
	}
	sink.rows = append(sink.rows, rows...)
	return true
}

// FlushStates writes the states the flow parked for this dataset under the
// version it just recorded. States of other datasets stay in the sink: a flow
// that spans datasets (a folder linked into several of them) flushes each one
// with its own number.
//
// A failure is returned, not logged: the states are what makes the version
// restorable, so a version whose states could not be written is not one. The
// caller passes the database handle it recorded the version with, which is the
// transaction the two should share.
func FlushStates(ctx context.Context, db *gorm.DB, datasetID string, version int64) error {
	if db == nil || datasetID == "" || version < 0 {
		return nil
	}
	sink, ok := ctx.Value(stateKey{}).(*stateSink)
	if !ok || sink == nil || sink.suppressed {
		return nil
	}
	rows := sink.take(datasetID)
	if len(rows) == 0 {
		return nil
	}
	for _, row := range rows {
		row.DatasetVersion = version
	}
	if err := dao.NewDocumentVersionDAO().CreateBatch(ctx, db, rows); err != nil {
		return fmt.Errorf("datasetversion: failed to record the document states of version %d of dataset %s: %w",
			version, datasetID, err)
	}
	return nil
}

// PendingStates is how many states the flow has parked for the dataset. It lets
// a caller decide whether a change it would otherwise skip still owes a version
// to the states it wrote.
func PendingStates(ctx context.Context, datasetID string) int {
	sink, ok := ctx.Value(stateKey{}).(*stateSink)
	if !ok || sink == nil || sink.suppressed {
		return 0
	}
	count := 0
	for _, row := range sink.rows {
		if row != nil && row.DatasetID == datasetID {
			count++
		}
	}
	return count
}

// take removes and returns the parked rows of one dataset, in the order they
// were offered.
func (s *stateSink) take(datasetID string) []*entity.DocumentVersion {
	if s == nil || len(s.rows) == 0 {
		return nil
	}
	kept := make([]*entity.DocumentVersion, 0, len(s.rows))
	taken := make([]*entity.DocumentVersion, 0, len(s.rows))
	for _, row := range s.rows {
		if row == nil {
			continue
		}
		if row.DatasetID == datasetID {
			taken = append(taken, row)
			continue
		}
		kept = append(kept, row)
	}
	s.rows = kept
	return taken
}
