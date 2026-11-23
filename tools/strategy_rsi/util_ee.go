//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

//go:build ee

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// nowISO returns the current time as an RFC3339 string with the local offset.
func nowISO() string {
	return time.Now().Format(time.RFC3339)
}

// newUUIDv4 returns a random RFC-4122 v4 UUID string. Used for attempt_id / eval_id,
// which must stay unique across crashes (never derived from the checkpoint cursor).
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is unrecoverable; fall back to a time-based value that is
		// still unique enough for the audit log.
		t := time.Now().UnixNano()
		return fmt.Sprintf("00000000-0000-4000-8000-%012x", uint64(t)&0xffffffffffff)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// countWords counts whitespace-separated tokens (used for the 30-word truncation guard).
func countWords(s string) int {
	n := 0
	inWord := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			inWord = false
			continue
		}
		if !inWord {
			n++
			inWord = true
		}
	}
	return n
}

// truncateWords caps a string to at most max whitespace-separated tokens (mechanical
// brevity guard; the "极简" judgement itself is the model's, §9.2).
func truncateWords(s string, max int) string {
	if max <= 0 {
		return s
	}
	fields := splitFields(s)
	if len(fields) <= max {
		return s
	}
	out := ""
	for i := 0; i < max; i++ {
		if i > 0 {
			out += " "
		}
		out += fields[i]
	}
	return out
}

func splitFields(s string) []string {
	var out []string
	cur := make([]rune, 0, 16)
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			flush()
			continue
		}
		cur = append(cur, r)
	}
	flush()
	return out
}
