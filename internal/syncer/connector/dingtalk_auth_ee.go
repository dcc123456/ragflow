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

package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	dingTalkAccessTokenPath   = "/v1.0/oauth2/accessToken"
	dingTalkRequestTimeout    = 60 * time.Second
	dingTalkRetryTries        = 3
	dingTalkRetryBaseDelay    = 500 * time.Millisecond
	dingTalkRetryMaxDelay     = 8 * time.Second
	dingTalkMaxResponseBody   = 4 * 1024 * 1024
	dingTalkAccessTokenMargin = 60 * time.Second
)

// dingTalkAPIClient is the shared request pipeline of the DingTalk connectors:
// one cached access token and one retrying JSON GET.
type dingTalkAPIClient struct {
	appKey      string
	appSecret   string
	operatorID  string
	baseURL     string
	httpClient  *http.Client
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// getAccessToken returns a cached access token, refreshing it before it expires.
func (c *dingTalkAPIClient) getAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.expiresAt) {
		return c.accessToken, nil
	}
	body, err := json.Marshal(map[string]string{"appKey": c.appKey, "appSecret": c.appSecret})
	if err != nil {
		return "", err
	}
	resp, err := c.dingTalkSend(ctx, http.MethodPost, c.baseURL+dingTalkAccessTokenPath, body)
	if err != nil {
		return "", err
	}
	payload, err := dingTalkDecodeResponse(resp, "authenticate")
	if err != nil {
		return "", err
	}
	token := stringConfig(payload["accessToken"])
	if token == "" {
		return "", &ConnectorValidationError{Message: "DingTalk authentication response did not contain an access token"}
	}
	expiresIn := configInt(payload["expireIn"], 7200)
	if expiresIn < 60 {
		expiresIn = 7200
	}
	c.accessToken = token
	c.expiresAt = time.Now().Add(time.Duration(expiresIn)*time.Second - dingTalkAccessTokenMargin)
	return token, nil
}

// dingTalkGet performs one authenticated GET with query parameters and decodes
// the JSON body.
func (c *dingTalkAPIClient) dingTalkGet(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.dingTalkDoRequest(ctx, http.MethodGet, endpoint, nil, token)
	if err != nil {
		return nil, err
	}
	return dingTalkDecodeResponse(resp, path)
}

// dingTalkPost performs one authenticated POST, with a JSON body when the caller
// has one; a nil body sends no payload at all.
func (c *dingTalkAPIClient) dingTalkPost(ctx context.Context, path string, query url.Values, body map[string]any) (map[string]any, error) {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = encoded
	}
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.dingTalkDoRequest(ctx, http.MethodPost, endpoint, payload, token)
	if err != nil {
		return nil, err
	}
	return dingTalkDecodeResponse(resp, path)
}

// operatorQuery adds the caller identity the doc APIs require: doc_1.0 and
// doc_2.0 name the caller's unionId operatorId.
func (c *dingTalkAPIClient) operatorQuery() url.Values {
	return url.Values{"operatorId": []string{c.operatorID}}
}

// unionIDQuery adds the caller identity the storage APIs require: storage_1.0
// names the same unionId unionId.
func (c *dingTalkAPIClient) unionIDQuery() url.Values {
	return url.Values{"unionId": []string{c.operatorID}}
}

// dingTalkDoRequest performs one authenticated request with bounded retry on
// transport errors, 429 and 5xx.
func (c *dingTalkAPIClient) dingTalkDoRequest(ctx context.Context, method, endpoint string, body []byte, token string) (*http.Response, error) {
	delay := dingTalkRetryBaseDelay
	var lastErr error
	for attempt := 0; attempt < dingTalkRetryTries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-acs-dingtalk-access-token", token)
		if len(body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.httpClient.Do(req)
		if err == nil && resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return resp, nil
		}
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		} else {
			resp.Body.Close()
			lastErr = fmt.Errorf("DingTalk HTTP %d while requesting %s", resp.StatusCode, endpoint)
		}
		if attempt == dingTalkRetryTries-1 {
			break
		}
		wait := delay + time.Duration(rand.Float64()*float64(delay))
		if !dingTalkSleep(ctx, wait) {
			return nil, ctx.Err()
		}
		delay *= 2
		if delay > dingTalkRetryMaxDelay {
			delay = dingTalkRetryMaxDelay
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("DingTalk request failed: %s", endpoint)
	}
	return nil, lastErr
}

// dingTalkSend performs one unauthenticated request (the token endpoint).
func (c *dingTalkAPIClient) dingTalkSend(ctx context.Context, method, endpoint string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// dingTalkDecodeResponse decodes one JSON response and turns DingTalk's error
// envelope into a connector error.
func dingTalkDecodeResponse(resp *http.Response, operation string) (map[string]any, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, dingTalkMaxResponseBody+1))
	if err != nil {
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("Failed to read DingTalk response while attempting to %s", operation)}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk permission denied (HTTP %d) while attempting to %s", resp.StatusCode, operation)}
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		if resp.StatusCode >= 400 {
			return nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk HTTP %d while attempting to %s", resp.StatusCode, operation)}
		}
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk returned an invalid response while attempting to %s", operation)}
	}
	if resp.StatusCode >= 400 {
		code := firstNonEmpty(stringConfig(payload["code"]), stringConfig(payload["errCode"]))
		message := firstNonEmpty(stringConfig(payload["message"]), stringConfig(payload["errmsg"]))
		if code == "" && message == "" {
			return nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk HTTP %d while attempting to %s", resp.StatusCode, operation)}
		}
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk API error %s (HTTP %d) while attempting to %s: %s", code, resp.StatusCode, operation, message)}
	}
	return payload, nil
}

func dingTalkSleep(ctx context.Context, duration time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(duration):
		return true
	}
}

// dingTalkParseTimestamp accepts the epoch seconds, epoch milliseconds and
// RFC3339 forms DingTalk uses for node timestamps.
func dingTalkParseTimestamp(value any) (time.Time, bool) {
	text := strings.TrimSpace(stringConfig(value))
	if text == "" {
		return time.Time{}, false
	}
	if number, err := strconv.ParseFloat(text, 64); err == nil {
		if number > 1e12 {
			return time.UnixMilli(int64(number)).UTC(), true
		}
		return time.Unix(int64(number), 0).UTC(), true
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}
