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

package service

import (
	"context"
	"fmt"
	"time"

	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/mcp"
)

// kvrocksCodexKV adapts the shared Kvrocks client to mcp.KV, so the mode 8 handle/binding
// store is visible to every process that serves /mcp/codex (the API that binds and the
// bridge that resolves may be different processes, or different API replicas).
type kvrocksCodexKV struct{}

// Compile-time contract check.
var _ mcp.KV = kvrocksCodexKV{}

func (kvrocksCodexKV) Get(ctx context.Context, key string) (string, bool, error) {
	client := kvrocks.Get()
	if client == nil {
		return "", false, nil
	}
	// Exist first: an empty value is still a live key (a one-shot handle stores "" as its
	// session index), so the value alone cannot distinguish miss from empty.
	exists, err := client.Exist(ctx, key)
	if err != nil {
		return "", false, err
	}
	if !exists {
		return "", false, nil
	}
	value, err := client.Get(ctx, key)
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (kvrocksCodexKV) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	client := kvrocks.Get()
	if client == nil {
		return fmt.Errorf("codex: kvrocks is not initialized")
	}
	if !client.Set(ctx, key, value, ttl) {
		return fmt.Errorf("codex: kvrocks set %s failed", key)
	}
	return nil
}

func (kvrocksCodexKV) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	client := kvrocks.Get()
	if client == nil {
		return false, fmt.Errorf("codex: kvrocks is not initialized")
	}
	return client.SetNX(ctx, key, value, ttl)
}

func (kvrocksCodexKV) Del(ctx context.Context, keys ...string) error {
	client := kvrocks.Get()
	if client == nil {
		return nil
	}
	for _, key := range keys {
		if !client.Delete(ctx, key) {
			// Delete reports false on a missing key too; a real failure is logged by the
			// kvrocks client itself, so treat this as best-effort.
			continue
		}
	}
	return nil
}

func (kvrocksCodexKV) Expire(ctx context.Context, key string, ttl time.Duration) error {
	client := kvrocks.Get()
	if client == nil {
		return nil
	}
	client.Expire(ctx, key, ttl)
	return nil
}

// InstallCodexKVStore wires the mode 8 handle/binding store onto the shared Kvrocks so
// the API process (which mints handles and binds scopes) and any process serving
// /mcp/codex (which resolves them) agree on state. Call once at boot, after kvrocks.Init.
// The per-turn keep-alive lives in the binder (codex_pipeline.go), not here.
func InstallCodexKVStore() {
	mcp.SetCodexKV(kvrocksCodexKV{})
}
