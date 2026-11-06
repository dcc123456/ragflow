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

package billing

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/price"
)

// apiVersionTransport rewrites the Stripe-Version header so the pinned
// configuration wins over the SDK constant. stripe-go v82 ships
// 2025-08-27.basil, while service_conf.yaml pins a newer version; the
// header override keeps the SDK wire contract and the configuration in sync
// without forking the SDK.
type apiVersionTransport struct {
	base    http.RoundTripper
	version string
}

func (t *apiVersionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.version != "" {
		req = req.Clone(req.Context())
		req.Header.Set("Stripe-Version", t.version)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

type priceCacheEntry struct {
	usd     float64
	ok      bool
	expires time.Time
}

var priceCache = struct {
	sync.Mutex
	entries map[string]priceCacheEntry
}{entries: map[string]priceCacheEntry{}}

const priceCacheTTL = 60 * time.Second

// initStripe configures the stripe-go global key and backend.
func (s *Service) initStripe() {
	if s == nil || s.cfg.StripeAPIKey == "" {
		return
	}
	s.stripeOnce.Do(func() {
		stripe.Key = s.cfg.StripeAPIKey
		backend := stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
			HTTPClient: &http.Client{
				Transport: &apiVersionTransport{base: http.DefaultTransport, version: strings.TrimSpace(s.cfg.StripeAPIVersion)},
				Timeout:   30 * time.Second,
			},
		})
		stripe.SetBackend(stripe.APIBackend, backend)
	})
}

// PriceUSD returns the unit price in USD for a Stripe price id.
// It returns (-1, false) when the price cannot be resolved, matching the
// Python fallback where an unavailable price is rendered as -1.
func (s *Service) PriceUSD(priceID string) (float64, bool) {
	priceID = strings.TrimSpace(priceID)
	if priceID == "" || priceID == "price_xxx" || strings.HasPrefix(priceID, "price_trail") {
		return -1, false
	}
	priceCache.Lock()
	if entry, ok := priceCache.entries[priceID]; ok && time.Now().Before(entry.expires) {
		priceCache.Unlock()
		return entry.usd, entry.ok
	}
	priceCache.Unlock()

	if s == nil || s.cfg.StripeAPIKey == "" {
		return -1, false
	}
	s.initStripe()
	object, err := price.Get(priceID, nil)
	if err != nil || object == nil || object.UnitAmount == 0 {
		priceCache.Lock()
		priceCache.entries[priceID] = priceCacheEntry{usd: -1, ok: false, expires: time.Now().Add(priceCacheTTL)}
		priceCache.Unlock()
		return -1, false
	}
	usd := float64(object.UnitAmount) / 100
	priceCache.Lock()
	priceCache.entries[priceID] = priceCacheEntry{usd: usd, ok: true, expires: time.Now().Add(priceCacheTTL)}
	priceCache.Unlock()
	return usd, true
}
