//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

//go:build ee

package main

import (
	"fmt"
	"math/rand"
)

// samplePass returns a deterministic, seed+pass-keyed permutation of [0,n). It is the
// in-pass without-replacement draw (plan §6.2/§12): the same (seed, pass) always yields
// the same order, so a resumed run replays an identical sample.
func samplePass(seed int64, pass, n int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	if n <= 1 {
		return order
	}
	r := rand.New(rand.NewSource(passSeed(seed, pass)))
	r.Shuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })
	return order
}

// passSeed mixes the run seed with the pass number so each pass draws a fresh
// permutation while remaining reproducible.
func passSeed(seed int64, pass int) int64 {
	return seed*1000003 + int64(pass)*0x9E3779B1
}

// passBatches chops a pass permutation into consecutive batches of at most `size`.
// Batches within a pass are disjoint and necessarily different; the last batch may be
// short (residual batch).
func passBatches(seed int64, pass, n, size int) [][]int {
	if size <= 0 {
		size = 1
	}
	order := samplePass(seed, pass, n)
	var out [][]int
	for i := 0; i < len(order); i += size {
		end := i + size
		if end > len(order) {
			end = len(order)
		}
		chunk := make([]int, end-i)
		copy(chunk, order[i:end])
		out = append(out, chunk)
	}
	return out
}

// batchSnapshot is the immutable input handed to the workers (plan §8.1). Workers only
// read it; the coordinator builds the next state from the returned results.
type batchSnapshot struct {
	Pass       int
	Index      int
	Version    int
	Strategies *strategySet // frozen snapshot (never mutated by workers)
	Questions  []int        // indices into the trainset
}

// strategyBlock renders the block prepended to each question in this batch.
func (b batchSnapshot) strategyBlock() string {
	if b.Strategies == nil {
		return ""
	}
	return b.Strategies.renderStrategyBlock()
}

// attemptID is a batch execution identity. It must be globally unique across crashes, so
// it is a random UUID rather than a value derived from the checkpoint cursor (a cursor-
// derived #n would be reused by uncommitted attempts).
type attemptID string

func newAttemptID() attemptID {
	return attemptID(newUUIDv4())
}

// evalID identifies one frozen-evaluation job (same uniqueness rule as attemptID).
type evalID string

func newEvalID() evalID {
	return evalID(newUUIDv4())
}

// batchKey is the (pass, batch index) coordinate a snapshot belongs to.
func batchKey(pass, index int) string {
	return fmt.Sprintf("%d.%d", pass, index)
}
