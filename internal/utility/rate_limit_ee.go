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

package utility

import (
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// rateLimit is the process-wide singleton global limiter.
var rateLimit *Limiter

// GetRateLimit returns the singleton global limiter, creating it on first use.
func GetRateLimit() *Limiter {
	if rateLimit == nil {
		rateLimit = New()
	}
	return rateLimit
}

// concurrencyLimiter bounds the number of in-flight requests using lock-free
// atomic CAS operations. A limit of 0 means "unlimited" (and disabled):
// tryAcquire returns true without counting, so toggling the limit on/off never
// leaves in-flight requests holding orphaned slots.
type concurrencyLimiter struct {
	current atomic.Uint64
	limit   atomic.Uint64
}

func newConcurrencyLimiter(limit uint64) *concurrencyLimiter {
	limiter := &concurrencyLimiter{}
	limiter.limit.Store(limit)
	return limiter
}

func (limiter *concurrencyLimiter) tryAcquire() bool {
	if limiter.limit.Load() == 0 {
		return true
	}

	for {
		current := limiter.current.Load()
		limit := limiter.limit.Load()

		if current >= limit {
			return false
		}

		if limiter.current.CompareAndSwap(current, current+1) {
			// Recheck after the CAS in case the limit was lowered concurrently.
			if current+1 <= limiter.limit.Load() {
				return true
			}

			limiter.release()
			return false
		}
	}
}

func (limiter *concurrencyLimiter) release() {
	for {
		current := limiter.current.Load()
		if current == 0 {
			return
		}

		if limiter.current.CompareAndSwap(current, current-1) {
			return
		}
	}
}

func (limiter *concurrencyLimiter) limitValue() uint64 {
	return limiter.limit.Load()
}

func (limiter *concurrencyLimiter) updateLimit(limit uint64) {
	limiter.limit.Store(limit)
}

func (limiter *concurrencyLimiter) currentValue() uint64 {
	return limiter.current.Load()
}

// Limiter is a single global rate/concurrency limiter applied to every URL.
//
// Both limits are OFF by default (no rate limiter configured, concurrency limit
// of 0). Configure them at runtime via UpdateGlobalRateLimit /
// UpdateGlobalConcurrencyLimit, and cancel them via DisableGlobalRateLimit /
// DisableGlobalConcurrencyLimit. Register Middleware on the Gin engine to
// enforce the limits for all requests.
type Limiter struct {
	// globalRateLimiter is nil when rate limiting is disabled.
	globalRateLimiter atomic.Pointer[rate.Limiter]
	// globalConcurrency is a long-lived instance; a limit of 0 means disabled.
	globalConcurrency *concurrencyLimiter
}

// New returns a global limiter with no limits active.
func New() *Limiter {
	return &Limiter{
		globalConcurrency: newConcurrencyLimiter(0),
	}
}

// Middleware returns a Gin middleware that applies the global rate and
// concurrency limits to every request, regardless of URL. Requests are allowed
// through unchanged when a limit is disabled.
func (limiter *Limiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limiter.checkGlobal(c) {
			return
		}
		defer limiter.releaseGlobal()

		c.Next()
	}
}

// UpdateGlobalRateLimit enables (or replaces) the global token-bucket rate
// limiter applied to all URLs. The burst defaults to the rate (with a minimum
// of 1), so short bursts of up to the rate are permitted. Call again to change
// the rate at runtime.
func (limiter *Limiter) UpdateGlobalRateLimit(limit rate.Limit) {
	burst := int(limit)
	if burst < 1 {
		burst = 1
	}
	limiter.globalRateLimiter.Store(rate.NewLimiter(limit, burst))
}

// DisableGlobalRateLimit turns off the global rate limiter. Requests are no
// longer throttled by rate until UpdateGlobalRateLimit is called again.
func (limiter *Limiter) DisableGlobalRateLimit() {
	limiter.globalRateLimiter.Store(nil)
}

// UpdateGlobalConcurrencyLimit sets (or changes) the global concurrency limit
// applied to all URLs. Pass 0 to disable it (same as DisableGlobalConcurrencyLimit).
func (limiter *Limiter) UpdateGlobalConcurrencyLimit(limit uint64) {
	limiter.globalConcurrency.updateLimit(limit)
}

// DisableGlobalConcurrencyLimit turns off the global concurrency limiter.
func (limiter *Limiter) DisableGlobalConcurrencyLimit() {
	limiter.globalConcurrency.updateLimit(0)
}

// GlobalRateLimitStatus returns the configured rate and burst. It returns zero
// values when rate limiting is disabled.
func (limiter *Limiter) GlobalRateLimitStatus() (rate.Limit, int) {
	if g := limiter.globalRateLimiter.Load(); g != nil {
		return g.Limit(), g.Burst()
	}
	return 0, 0
}

// GlobalConcurrencyLimitStatus returns the configured concurrency limit and the
// current in-flight request count. A limit of 0 means concurrency limiting is
// disabled.
func (limiter *Limiter) GlobalConcurrencyLimitStatus() (uint64, uint64) {
	return limiter.globalConcurrency.limitValue(), limiter.globalConcurrency.currentValue()
}

// checkGlobal enforces the global rate and concurrency limits. It aborts the
// request with 429 on the first limit exceeded and reports whether the request
// may proceed.
func (limiter *Limiter) checkGlobal(c *gin.Context) bool {
	if g := limiter.globalRateLimiter.Load(); g != nil && !g.Allow() {
		c.AbortWithStatus(http.StatusTooManyRequests)
		return false
	}

	if g := limiter.globalConcurrency; g != nil && !g.tryAcquire() {
		c.AbortWithStatus(http.StatusTooManyRequests)
		return false
	}

	return true
}

// releaseGlobal returns one in-flight slot to the global concurrency limiter.
// Safe to call unconditionally: it is a no-op when concurrency is disabled.
func (limiter *Limiter) releaseGlobal() {
	if g := limiter.globalConcurrency; g != nil {
		g.release()
	}
}
