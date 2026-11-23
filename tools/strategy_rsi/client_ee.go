//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//

//go:build ee

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpClient is the minimal RAGFlow API client. Every call takes a context so a stop
// signal cancels in-flight requests instead of waiting out the 900s timeout.
type httpClient struct {
	base    string
	apiKey  string
	timeout time.Duration
	hc      *http.Client
}

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func newHTTPClient(cfg config) *httpClient {
	to := cfg.requestTimeout()
	return &httpClient{
		base:    cfg.baseURL(),
		apiKey:  cfg.RAGFlowAPIKey,
		timeout: to,
		hc:      &http.Client{Timeout: to},
	}
}

func (c *httpClient) client() *http.Client {
	if c.hc == nil {
		return &http.Client{Timeout: 900 * time.Second}
	}
	return c.hc
}

func (c *httpClient) do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{Status: resp.StatusCode, Body: string(data)}
	}
	var env apiEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("decode envelope: %w (body=%s)", err, truncate(string(data), 200))
	}
	if env.Code != 0 {
		return nil, &apiError{Code: env.Code, Message: env.Message}
	}
	return env.Data, nil
}

func (c *httpClient) post(ctx context.Context, path string, body any) (json.RawMessage, error) {
	return c.do(ctx, http.MethodPost, path, body)
}

func (c *httpClient) get(ctx context.Context, path string) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

// createSession creates a chat session and returns its id.
func (c *httpClient) createSession(ctx context.Context, chatID string) (string, error) {
	data, err := c.post(ctx, "/api/v1/chats/"+url.PathEscape(chatID)+"/sessions",
		map[string]any{"name": fmt.Sprintf("strategy-rsi-%s", time.Now().Format("20060102-150405"))})
	if err != nil {
		return "", err
	}
	var obj struct {
		SessionID string `json:"session_id"`
		ID        string `json:"id"`
	}
	_ = json.Unmarshal(data, &obj)
	if obj.SessionID != "" {
		return obj.SessionID, nil
	}
	if obj.ID != "" {
		return obj.ID, nil
	}
	return "", fmt.Errorf("could not extract session id: %s", truncate(string(data), 200))
}

// askResult is one chat-completion call's observable outcome.
type askResult struct {
	Answer    string
	SessionID string
	MessageID string
}

// ask sends one question; when sessionID is non-empty the turn continues that session.
func (c *httpClient) ask(ctx context.Context, chatID, question, sessionID string, reasoning int) (askResult, error) {
	body := map[string]any{
		"chat_id":   chatID,
		"question":  question,
		"stream":    false,
		"reasoning": reasoning,
	}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	data, err := c.post(ctx, "/api/v1/chat/completions", body)
	if err != nil {
		return askResult{}, err
	}
	var obj struct {
		Answer    string `json:"answer"`
		SessionID string `json:"session_id"`
		ID        string `json:"id"`
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return askResult{}, fmt.Errorf("decode chat completion: %w", err)
	}
	return askResult{Answer: obj.Answer, SessionID: obj.SessionID, MessageID: obj.MessageID}, nil
}

// sessionTurn is one stored turn, used for reconcile (§6.4).
type sessionTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// sessionHistory fetches the stored turns of ONE session so an "unknown" request can be
// reconciled before it is resent.
//
// The real shape is `GET /api/v1/chats/{chat_id}/sessions?id=<sid>` returning
// data = [ {id, message:[{role,content}, ...]} ] (handler/chat_session.go, entity/chat.go).
// The id filter is required: the unfiltered list is paginated (first page only), so the
// session of interest may be absent. Returns (nil,false) when the shape is unrecognised or
// the id is not present -- the caller must treat that as "cannot distinguish", never as
// proof of absence against an unrelated session.
func (c *httpClient) sessionHistory(ctx context.Context, chatID, sessionID string) ([]sessionTurn, bool) {
	data, err := c.get(ctx, "/api/v1/chats/"+url.PathEscape(chatID)+"/sessions?id="+url.QueryEscape(sessionID))
	if err != nil {
		return nil, false
	}
	var list []struct {
		ID      string        `json:"id"`
		Message []sessionTurn `json:"message"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, false
	}
	for _, s := range list {
		if s.ID == sessionID {
			return s.Message, true
		}
	}
	// No fallback to a lone unrelated session: id mismatch means "cannot distinguish".
	return nil, false
}

// ---- error types ----

type httpStatusError struct {
	Status int
	Body   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("http %d: %s", e.Status, truncate(e.Body, 200))
}

type apiError struct {
	Code    int
	Message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("api code=%d: %s", e.Code, e.Message)
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
