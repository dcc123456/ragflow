//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

//go:build ee

package main

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// failClass is how a failed call is handled (plan §6.4): quota/network -> retry
// (after reconcile), timeout/permanent -> skip or abandon the batch update.
type failClass int

const (
	failTimeout failClass = iota
	failQuota
	failNetwork
	failPermanent
	// failQuotaExhausted means the ACCOUNT is out of credit (Token Plan limit / balance),
	// not that a single request was throttled. Retrying immediately cannot help: the only
	// sensible options are to wait for the advertised reset (if the provider states one) or
	// to stop. Kept separate from failQuota so it never enters the unbounded retry loop.
	failQuotaExhausted
)

func (f failClass) String() string {
	switch f {
	case failTimeout:
		return "timeout"
	case failQuota:
		return "quota"
	case failQuotaExhausted:
		return "quota-exhausted"
	case failNetwork:
		return "network"
	default:
		return "permanent"
	}
}

// quotaMarkers / networkMarkers are matched (case-insensitive) against *transport error*
// text (never against a model answer's body -- see answerErrorPhrases).
var quotaMarkers = []string{
	"rate limit", "429", "529", "quota", "用量上限", "额度", "速率限制",
	"high demand", "overloaded", "too many requests", "token plan",
}

var networkMarkers = []string{
	"connection reset", "connection refused", "no such host", "eof", "broken pipe",
	"i/o timeout", "tls", "unexpected eof", "network is unreachable",
}

var timeoutMarkers = []string{
	"timed out", "timeout", "deadline exceeded", "context deadline",
}

// answerErrorPhrases are the ONLY signals that a returned ANSWER is really a decorated
// provider error. They must be multi-word / explicit: matching short substrings against
// arbitrary model text produces catastrophic false positives -- "Geoffrey Chaucer"
// contains "eof", and any date/number can contain "429"/"529".
var answerErrorPhrases = []string{
	"**error**", "api error", "rate limit exceeded", "用量上限", "额度不足", "quota exceeded",
	"connection reset by peer", "connection refused", "no such host",
	"context deadline exceeded", "request timed out", "i/o timeout", "too many requests",
}

// quotaExhaustedMarkers identify an EXHAUSTED ACCOUNT rather than a throttled request. They
// are checked before the generic quota markers, because a Token Plan limit is terminal until
// the plan resets, while "rate limit"/429 usually clears in seconds.
var quotaExhaustedMarkers = []string{
	"token plan", "用量上限", "insufficient balance", "insufficient_balance",
	"quota exceeded", "额度不足", "余额不足", "(1008)", "(2056)",
}

var statusCodeRE = regexp.MustCompile(`\b(429|529)\b`)

// classify inspects a TRANSPORT error and returns its class.
func classify(err error) failClass {
	if err == nil {
		return failPermanent
	}
	// Explicit Go timeout signals win over text heuristics.
	if errors.Is(err, context.DeadlineExceeded) {
		return failTimeout
	}
	if os.IsTimeout(err) {
		return failTimeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return failTimeout
	}
	// An exhausted account outranks the status code: a 500 whose body says "Token Plan limit"
	// is not a transient server fault, and treating it as one is what produced the endless
	// retry storm.
	if containsAny(strings.ToLower(err.Error()), quotaExhaustedMarkers) {
		return failQuotaExhausted
	}
	var hs *httpStatusError
	if errors.As(err, &hs) {
		switch {
		case hs.Status == 429:
			return failQuota
		case hs.Status >= 500:
			return failNetwork
		default:
			return failPermanent
		}
	}
	msg := strings.ToLower(err.Error())
	if containsAny(msg, timeoutMarkers) {
		return failTimeout
	}
	if containsAny(msg, quotaMarkers) {
		return failQuota
	}
	if containsAny(msg, networkMarkers) {
		return failNetwork
	}
	return failPermanent
}

// answerLooksLikeError reports whether a returned answer is really a decorated provider
// error rather than a genuine model answer. Conservative by construction: an ordinary
// answer must never be misread as a failure (that would retry forever).
func answerLooksLikeError(answer string) bool {
	m := strings.ToLower(answer)
	if containsAny(m, answerErrorPhrases) {
		return true
	}
	if statusCodeRE.MatchString(m) && containsAny(m, []string{"error", "limit", "rate", "quota"}) {
		return true
	}
	return false
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// retryPolicy controls waits between retries. Transport hiccups (failNetwork) use an
// exponential backoff; MODEL-side temporary failures (failQuota: high demand / allowance
// exhausted) wait a flat `quota` interval, because the provider resets its allowance on a
// sliding window and a long park only wastes wall clock (default 10 min).
type retryPolicy struct {
	initial time.Duration
	max     time.Duration
	jitter  float64
	quota   time.Duration
	// exhausted is the wait between attempts while the ACCOUNT is out of credit. It is much
	// longer than quota's, because a plan limit cannot clear in seconds.
	exhausted time.Duration
}

// delayFor returns the wait before retry `attempt` for a given failure class.
func (p retryPolicy) delayFor(class failClass, attempt int) time.Duration {
	if class == failQuota {
		q := p.quota
		if q <= 0 {
			q = 10 * time.Minute
		}
		return jittered(q, p.jitter)
	}
	return jittered(p.backoff(attempt), p.jitter)
}

func (p retryPolicy) backoff(attempt int) time.Duration {
	if p.initial <= 0 {
		p.initial = time.Second
	}
	if p.max <= 0 {
		p.max = 60 * time.Second
	}
	d := p.initial
	for i := 0; i < attempt && d < p.max; i++ {
		d *= 2
	}
	if d > p.max {
		d = p.max
	}
	return d
}

func jittered(d time.Duration, jitter float64) time.Duration {
	if jitter > 0 {
		factor := 1 + (rand.Float64()*2-1)*jitter
		d = time.Duration(float64(d) * factor)
	}
	if d < 0 {
		d = 0
	}
	return d
}

// resetHint extracts the moment an exhausted plan recovers, when the provider states one.
// Providers differ: some put an RFC 3339 timestamp in the body, some a "retry after N" hint.
// Absent any of those, callers fall back to a fixed wait — and then stop instead of looping.
func resetHint(err error) (time.Time, bool) {
	if err == nil {
		return time.Time{}, false
	}
	msg := err.Error()
	if m := rfc3339RE.FindString(msg); m != "" {
		if at, perr := time.Parse(time.RFC3339, m); perr == nil {
			return at, true
		}
	}
	if m := retryAfterRE.FindStringSubmatch(strings.ToLower(msg)); len(m) == 2 {
		if n, perr := strconv.Atoi(m[1]); perr == nil && n > 0 {
			return time.Now().Add(time.Duration(n) * time.Second), true
		}
	}
	return time.Time{}, false
}

// sleepFor waits d, returning early when ctx is cancelled.
func sleepFor(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

var (
	rfc3339RE    = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`)
	retryAfterRE = regexp.MustCompile(`retry[ _-]?after[:\s]*(\d+)`)
)

// sleepClass waits (cancellably) before the next retry of `class`.
func (p retryPolicy) sleepClass(ctx context.Context, class failClass, attempt int) error {
	t := time.NewTimer(p.delayFor(class, attempt))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// callWithRetry repeatedly runs fn until it succeeds. A timeout or permanent failure is
// returned immediately (the caller decides skip/abandon); quota/network failures are
// retried endlessly after a class-appropriate wait, as required by the plan.
//
// NOTE: the caller must make fn side-effect-safe across retries -- resending a chat turn
// duplicates it (the server persists the user turn before calling the model), so fn is
// expected to reconcile first (see rsi.go).
func callWithRetry(ctx context.Context, p retryPolicy, fn func(attempt int) error) error {
	for attempt := 0; ; attempt++ {
		err := fn(attempt)
		if err == nil {
			return nil
		}
		class := classify(err)
		switch class {
		case failQuotaExhausted:
			// An exhausted account is a billing condition, not a failed call: wait for the plan
			// (the advertised reset when there is one) and retry indefinitely. Only the caller's
			// context ends this.
			wait := p.exhausted
			if wait <= 0 {
				wait = 30 * time.Minute
			}
			if at, ok := resetHint(err); ok {
				if d := time.Until(at); d > 0 && d < wait {
					wait = d
				}
			}
			if serr := sleepFor(ctx, wait); serr != nil {
				return serr
			}
			continue
		case failQuota, failNetwork:
			if serr := p.sleepClass(ctx, class, attempt); serr != nil {
				return serr
			}
			continue
		default:
			return err
		}
	}
}
