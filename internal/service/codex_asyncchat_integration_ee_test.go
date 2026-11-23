//go:build integration && ee

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

package service_test

import (
	"context"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	codexgo "github.com/yuzhichang/codex-app-server-go"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	modelModule "ragflow/internal/entity/models"
	"ragflow/internal/mcp"
	"ragflow/internal/permission"
	serverconfig "ragflow/internal/server/config"
	"ragflow/internal/service"
)

// codexTestKV is an in-test mcp.KV so the mode 8 handle/binding store needs no Kvrocks.
type codexTestKV struct {
	mu   sync.Mutex
	data map[string]string
}

func newCodexTestKV() *codexTestKV { return &codexTestKV{data: map[string]string{}} }

func (k *codexTestKV) Get(_ context.Context, key string) (string, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.data[key]
	return v, ok, nil
}

func (k *codexTestKV) Set(_ context.Context, key, value string, _ time.Duration) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	return nil
}

func (k *codexTestKV) SetNX(_ context.Context, key, value string, _ time.Duration) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.data[key]; ok {
		return false, nil
	}
	k.data[key] = value
	return true, nil
}

func (k *codexTestKV) Del(_ context.Context, keys ...string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, key := range keys {
		delete(k.data, key)
	}
	return nil
}

func (k *codexTestKV) Expire(_ context.Context, _ string, _ time.Duration) error { return nil }

func (k *codexTestKV) Keys(_ context.Context, prefix string) ([]string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for key := range k.data {
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	return out, nil
}

// TestCodexAsyncChatEndToEnd drives the real mode 8 path through AsyncChat: dialog
// model resolution -> codexAgent -> the codexagent runner -> a local codex WS app-server
// -> the process's own MCP bridge.
//
//	RAGFLOW_CODEX_IT=1 OPENAI_BASE_URL=... OPENAI_API_KEY=... OPENAI_MODEL=... \
//	  go test -tags integration -run TestCodexAsyncChatEndToEnd ./internal/service/
func TestCodexAsyncChatEndToEnd(t *testing.T) {
	if os.Getenv("RAGFLOW_CODEX_IT") != "1" {
		t.Skip("set RAGFLOW_CODEX_IT=1 to run against the local codex app-server")
	}
	bin, err := codexgo.FindBinary()
	if err != nil {
		t.Skipf("codex binary not found: %v", err)
	}
	baseURL, apiKey, modelName := os.Getenv("OPENAI_BASE_URL"), os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_MODEL")
	if baseURL == "" || apiKey == "" || modelName == "" {
		t.Skip("set OPENAI_BASE_URL / OPENAI_API_KEY / OPENAI_MODEL (a Responses-API provider)")
	}

	// --- DB: in-memory sqlite + the model tables the resolver reads ---
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.Tenant{}, &entity.Chat{}, &entity.TenantModelProvider{},
		&entity.TenantModelInstance{}, &entity.TenantModel{}, &entity.CodexThreadMap{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	dao.DB = db

	// The model provider manager is loaded from conf/models (repo root).
	_, thisFile, _, _ := runtime.Caller(0)
	modelsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "conf", "models")
	if err := modelModule.InitProviderManager(modelsDir); err != nil {
		t.Fatalf("init provider manager: %v", err)
	}

	const tenantID = "it-tenant"
	mustCreate(t, db, &entity.Tenant{ID: tenantID})
	mustCreate(t, db, &entity.TenantModelProvider{ID: "prov-1", ProviderName: "minimax", TenantID: tenantID})
	mustCreate(t, db, &entity.TenantModelInstance{
		ID: "inst-1", InstanceName: "it", ProviderID: "prov-1", APIKey: apiKey, Status: "active",
		Extra: `{"base_url":"` + baseURL + `"}`,
	})
	mustCreate(t, db, &entity.TenantModel{
		ID: "model-1", ModelName: modelName, ProviderID: "prov-1", InstanceID: "inst-1",
		ModelType: int(entity.ModelTypeChat), Status: "active",
	})

	// --- local codex WS app-server (a "shared Codex") ---
	addr, token, stopCodex := startCodexWS(t, bin)
	defer stopCodex()

	// The handle/binding store must be KV-backed; give it an in-test store so the API
	// side (which mints + binds) and the bridge (which resolves) share it.
	mcp.SetCodexKV(newCodexTestKV())
	mcpSrv := httptest.NewServer(mcp.NewCodexHandler(mcp.DefaultCodexTickets()))
	defer mcpSrv.Close()

	service.SetCodexConfig(&serverconfig.CodexConfig{
		Endpoint:           "ws://" + addr,
		BearerToken:        token,
		MCPPublicBase:      mcpSrv.URL,
		ResponsesProviders: []string{"minimax"},
		SandboxMode:        "read-only",
		ApprovalPolicy:     "untrusted",
		TurnTimeout:        3 * time.Minute,
	})

	chat := &entity.Chat{
		ID: "chat-1", TenantID: tenantID, LLMID: "model-1",
		KBIDs: entity.JSONSlice{}, PromptConfig: entity.JSONMap{}, LLMSetting: entity.JSONMap{},
	}
	messages := []map[string]interface{}{{"role": "user", "content": "Reply with exactly: E2EOK"}}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// AsyncChat reads the caller's identity from ctx (the userID parameter was
	// removed upstream), so attach it the same way the HTTP layer does.
	ctx = permission.WithIdentity(ctx, "user-1", tenantID)
	ch, err := service.NewChatPipelineService().AsyncChat(ctx, chat, messages, false, map[string]interface{}{"reasoning": 8})
	if err != nil {
		t.Fatalf("AsyncChat: %v", err)
	}
	final := ""
	for res := range ch {
		if res.Final {
			final = res.Answer
		}
	}
	t.Logf("final answer: %q", final)
	if final == "" {
		t.Fatal("empty final answer from the mode 8 path")
	}
}

func mustCreate(t *testing.T, db *gorm.DB, v interface{}) {
	t.Helper()
	if err := db.Create(v).Error; err != nil {
		t.Fatalf("seed %T: %v", v, err)
	}
}

// startCodexWS starts `codex app-server --listen ws://...` with a capability token and
// returns the address, the token, and a stop func.
func startCodexWS(t *testing.T, bin string) (addr, token string, stop func()) {
	t.Helper()
	token = "it-token-" + time.Now().Format("150405.000000")
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	// Grab a free port.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick port: %v", err)
	}
	addr = l.Addr().String()
	l.Close()

	cmd := exec.Command(bin, "app-server", "--listen", "ws://"+addr, "--ws-auth", "capability-token", "--ws-token-file", tokenFile)
	cmd.Env = append(os.Environ(), "RUST_LOG=error")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start codex: %v", err)
	}
	for i := 0; i < 100; i++ {
		c, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if dialErr == nil {
			c.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return addr, token, func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }
}
