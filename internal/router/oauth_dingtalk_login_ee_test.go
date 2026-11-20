//go:build ee

package router

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/entity"
	"ragflow/internal/handler"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/permission/testsupport"
	"ragflow/internal/server"
	"ragflow/internal/service"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// dingtalkStub fakes the DingTalk hosts the login flow talks to: the
// login.dingtalk.com consent page (a browser lands there after the redirect
// and confirms the scan) and the two api.dingtalk.com endpoints the provider
// consumes. Every endpoint asserts the wire contract the real DingTalk keeps
// — a JSON token body with camelCase answers, the access token riding on the
// x-acs-dingtalk-access-token header, and the code coming back under the
// authCode name — so a regression in the provider's requests, not just its
// parsing, shows up here.
type dingtalkStub struct {
	server *httptest.Server

	clientID     string
	clientSecret string

	mu             sync.Mutex
	email          string
	tokenCalls     int
	userinfoCalls  int
	nextCode       int
	issuedCodes    map[string]bool
	redeemedTokens map[string]bool
}

func newDingtalkStub(clientID, clientSecret string) *dingtalkStub {
	stub := &dingtalkStub{
		clientID:       clientID,
		clientSecret:   clientSecret,
		issuedCodes:    map[string]bool{},
		redeemedTokens: map[string]bool{},
	}
	stub.server = httptest.NewServer(stub)
	return stub
}

func (s *dingtalkStub) setEmail(mail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.email = mail
}

func writeDingtalkJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *dingtalkStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/oauth2/auth":
		// The consent page echoes the parameters the authorization URL
		// carried and, once the member confirms, bounces the authCode back
		// to the registered redirect URI. DingTalk names the code authCode,
		// not code — the callback has to accept that spelling.
		q := r.URL.Query()
		if q.Get("client_id") == "" || q.Get("redirect_uri") == "" || q.Get("state") == "" || q.Get("response_type") != "code" {
			http.Error(w, "missing oauth2/auth parameters", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.nextCode++
		code := fmt.Sprintf("dingtalk-authcode-%d", s.nextCode)
		s.issuedCodes[code] = true
		s.mu.Unlock()
		target := q.Get("redirect_uri") + "?authCode=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(q.Get("state"))
		http.Redirect(w, r, target, http.StatusFound)

	case "/v1.0/oauth2/userAccessToken":
		// DingTalk exchanges the code through a JSON body and answers with
		// camelCase claims; it rejects a form-encoded body outright, which is
		// what a vanilla OAuth2 client would send.
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			writeDingtalkJSON(w, http.StatusBadRequest, map[string]any{
				"error": "InvalidAuthenticationCode", "error_description": fmt.Sprintf("content type %q is not JSON", ct),
			})
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			GrantType    string `json:"grant_type"`
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			Code         string `json:"code"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			writeDingtalkJSON(w, http.StatusBadRequest, map[string]any{
				"error": "InvalidAuthenticationCode", "error_description": "unparseable body",
			})
			return
		}
		s.mu.Lock()
		_, issued := s.issuedCodes[payload.Code]
		alreadyRedeemed := s.redeemedTokens[payload.Code]
		s.mu.Unlock()
		if payload.GrantType != "authorization_code" || payload.ClientID != s.clientID || payload.ClientSecret != s.clientSecret {
			writeDingtalkJSON(w, http.StatusBadRequest, map[string]any{
				"error": "InvalidClient", "error_description": "client credentials do not match",
			})
			return
		}
		if !issued || alreadyRedeemed {
			writeDingtalkJSON(w, http.StatusBadRequest, map[string]any{
				"error": "InvalidAuthenticationCode", "error_description": "authorization code has been used",
			})
			return
		}
		s.mu.Lock()
		s.redeemedTokens[payload.Code] = true
		s.tokenCalls++
		s.mu.Unlock()
		writeDingtalkJSON(w, http.StatusOK, map[string]any{
			"expireIn": 7200, "accessToken": "dingtalk-user-token-" + payload.Code,
			"refreshToken": "dingtalk-refresh", "corpId": "ding-org", "tokenType": "Bearer", "scope": "openid",
		})

	case "/v1.0/contact/users/me":
		// DingTalk authenticates the userinfo call through its own header,
		// not the Authorization one; a request that only carries the standard
		// header gets rejected, which is exactly the detail the provider has
		// to get right.
		token := r.Header.Get("x-acs-dingtalk-access-token")
		if token == "" {
			writeDingtalkJSON(w, http.StatusOK, map[string]any{
				"code": "Forbidden.AccessDenied.ResourceIsRestricted", "message": "missing access token header",
			})
			return
		}
		if !strings.HasPrefix(token, "dingtalk-user-token-") {
			writeDingtalkJSON(w, http.StatusOK, map[string]any{
				"code": "Forbidden.AccessDenied.ResourceIsRestricted", "message": "invalid access token",
			})
			return
		}
		s.mu.Lock()
		email := s.email
		s.userinfoCalls++
		s.mu.Unlock()
		writeDingtalkJSON(w, http.StatusOK, map[string]any{
			"nick": "张三", "email": email, "mobile": "+8613800000000",
			"avatarUrl": "http://avatar.example/zhangsan.png",
			"openId":    "dt-open-1", "unionId": "dt-union-1", "stateCode": "+86",
		})

	default:
		http.NotFound(w, r)
	}
}

// TestOAuthDingtalkLoginFlow drives the DingTalk SSO channel end to end: the
// real routes, handler, service, DAO layer and provider against a stub
// DingTalk. The stub endpoints are injected through the dingtalk|sso.
// authorization_url / token_url / userinfo_url settings, the same keys an
// operator uses to point the channel at a proxied deployment.
func TestOAuthDingtalkLoginFlow(t *testing.T) {
	// Server configuration and Kvrocks are process globals; isolate their initialization.
	const childEnv = "RAGFLOW_DINGTALK_LOGIN_TEST_CHILD"
	if os.Getenv(childEnv) != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "-test.run=^TestOAuthDingtalkLoginFlow$", "-test.v")
		command.Env = append(os.Environ(), childEnv+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("dingtalk login flow failed: %v\n%s", err, output)
		}
		return
	}
	if err := common.InitLogger("error", common.FileOutput{}, "dingtalk-login-test"); err != nil {
		t.Fatal(err)
	}
	common.Logger = zap.NewNop()
	common.Sugar = common.Logger.Sugar()
	gin.SetMode(gin.TestMode)
	t.Chdir("../..")

	cache := miniredis.RunT(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("kvrocks:\n  host: %q\n  password: ''\n  db: 0\nauthentication:\n  enable_register: true\n", cache.Addr())
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if err := server.Init(configPath); err != nil {
		t.Fatal(err)
	}
	if err := kvrocks.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { kvrocks.Close() })

	db := testutil.SetupTestDB(t, &entity.SystemSettings{}, &entity.WhiteList{}, &entity.User{}, &entity.Tenant{},
		&entity.UserTenant{}, &entity.File{}, &entity.Role{}, &entity.RoleResource{}, &entity.TenantModelProvider{}, &entity.TenantModelInstance{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	const (
		clientID    = "ding-app-key"
		appSecret   = "ding-app-secret"
		redirect    = "http://ragflow.test/api/v1/auth/oauth/dingtalk/callback"
		memberEmail = "zhangsan@corp.example"
	)
	stub := newDingtalkStub(clientID, appSecret)
	t.Cleanup(stub.server.Close)
	stub.setEmail(memberEmail)

	set := func(name, value string) {
		t.Helper()
		if err := db.Save(&entity.SystemSettings{Name: name, Value: value, DataType: "string"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range map[string]string{
		"enable_whitelist":               "false",
		"email_verification.enabled":     "false",
		"dingtalk|sso.enabled":           "true",
		"dingtalk|sso.client_id":         clientID,
		"dingtalk|sso.client_secret":     appSecret,
		"dingtalk|sso.redirect_uri":      redirect,
		"dingtalk|sso.scope":             "openid",
		"dingtalk|sso.authorization_url": stub.server.URL + "/oauth2/auth",
		"dingtalk|sso.token_url":         stub.server.URL + "/v1.0/oauth2/userAccessToken",
		"dingtalk|sso.userinfo_url":      stub.server.URL + "/v1.0/contact/users/me",
	} {
		set(name, value)
	}

	engine := gin.New()
	r := &Router{
		authHandler:   handler.NewAuthHandler(),
		userHandler:   handler.NewUserHandler(service.NewUserService(), service.NewUserTenantService()),
		systemHandler: handler.NewSystemHandler(service.NewSystemService()),
	}
	r.Setup(engine)

	startLogin := func() (authURL, state string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/auth/login/dingtalk", nil)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		if response.Code != http.StatusFound {
			t.Fatalf("GET /auth/login/dingtalk: status=%d body=%s", response.Code, response.Body.String())
		}
		state = ""
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == "ragflow_oauth_state_dingtalk" {
				state = cookie.Value
			}
		}
		if state == "" {
			t.Fatal("login redirect did not set the state cookie")
		}
		return response.Header().Get("Location"), state
	}

	assertAuthURL := func(authURL, state string) {
		t.Helper()
		parsed, err := url.Parse(authURL)
		if err != nil {
			t.Fatalf("authorization URL is not a URL: %v", err)
		}
		if parsed.Path != "/oauth2/auth" {
			t.Fatalf("authorization URL does not point at the stub consent page: %s", authURL)
		}
		q := parsed.Query()
		for key, want := range map[string]string{
			"client_id":     clientID,
			"response_type": "code",
			"scope":         "openid",
			"redirect_uri":  redirect,
			"state":         state,
		} {
			if q.Get(key) != want {
				t.Fatalf("authorization URL %s=%q, want %q", key, q.Get(key), want)
			}
		}
	}

	confirmScan := func(authURL, state string) string {
		t.Helper()
		// The browser lands on the consent page and confirms; DingTalk
		// answers by bouncing the authCode back to the registered redirect
		// URI.
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Get(authURL)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("consent page: status=%d", resp.StatusCode)
		}
		target, err := url.Parse(resp.Header.Get("Location"))
		if err != nil {
			t.Fatalf("consent redirect is not a URL: %v", err)
		}
		if target.Path != "/api/v1/auth/oauth/dingtalk/callback" {
			t.Fatalf("consent redirect target path=%q", target.Path)
		}
		q := target.Query()
		if q.Get("state") != state {
			t.Fatalf("consent redirect state=%q, want %q", q.Get("state"), state)
		}
		if q.Get("authCode") == "" {
			t.Fatal("consent redirect carries no authCode parameter")
		}
		return q.Get("authCode")
	}

	finishLogin := func(authCode, state string) (token string, query url.Values, response *httptest.ResponseRecorder) {
		t.Helper()
		// The callback is exercised exactly as DingTalk calls it: the code
		// under the authCode spelling.
		req := httptest.NewRequest(http.MethodGet,
			"http://localhost/api/v1/auth/oauth/dingtalk/callback?authCode="+url.QueryEscape(authCode)+"&state="+url.QueryEscape(state), nil)
		req.AddCookie(&http.Cookie{Name: "ragflow_oauth_state_dingtalk", Value: state})
		response = httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		if response.Code != http.StatusFound {
			t.Fatalf("GET /auth/oauth/dingtalk/callback: status=%d body=%s", response.Code, response.Body.String())
		}
		target, err := url.Parse(response.Header().Get("Location"))
		if err != nil {
			t.Fatalf("callback redirect is not a URL: %v", err)
		}
		return target.Query().Get("auth"), target.Query(), response
	}

	// 1. A member whose email matches no account is registered as a new user
	// with a fully-shaped tenant, and the union id is what the identity is
	// pinned to.
	authURL, state := startLogin()
	assertAuthURL(authURL, state)
	authCode := confirmScan(authURL, state)
	token, query, callbackResponse := finishLogin(authCode, state)
	if token == "" {
		t.Fatalf("callback did not issue a login token: %v", query)
	}
	cleared := false
	for _, cookie := range callbackResponse.Result().Cookies() {
		if cookie.Name == "ragflow_oauth_state_dingtalk" && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("callback did not clear the state cookie")
	}
	var user entity.User
	if err := db.Where("email = ?", memberEmail).Take(&user).Error; err != nil {
		t.Fatalf("registered user is missing: %v", err)
	}
	if user.LoginChannel == nil || *user.LoginChannel != "dingtalk" {
		t.Fatalf("user login_channel=%v, want dingtalk", user.LoginChannel)
	}
	var tenant entity.Tenant
	if err := db.Where("id = ?", user.ID).Take(&tenant).Error; err != nil {
		t.Fatalf("registered user has no tenant: %v", err)
	}
	var membership entity.UserTenant
	if err := db.Where("user_id = ?", user.ID).Take(&membership).Error; err != nil {
		t.Fatalf("registered user has no tenant membership: %v", err)
	}
	var root entity.File
	if err := db.Where("tenant_id = ?", user.ID).Take(&root).Error; err != nil {
		t.Fatalf("registered user has no root file: %v", err)
	}

	// 2. The state is single-use: replaying the callback must bounce with
	// invalid_state even with the original cookie still attached.
	_, query, _ = finishLogin(authCode, state)
	if query.Get("error") != "invalid_state" {
		t.Fatalf("replayed callback error=%q, want invalid_state", query.Get("error"))
	}

	// 3. A tampered cookie must not pair with a fresh state: the CSRF check.
	authURL, state = startLogin()
	authCode = confirmScan(authURL, state)
	_, query, _ = finishLogin(authCode, "tampered-state")
	if query.Get("error") != "invalid_state" {
		t.Fatalf("tampered-cookie callback error=%q, want invalid_state", query.Get("error"))
	}

	// 4. An authCode cannot be redeemed twice even with a fresh state:
	// DingTalk rejects the second exchange and the callback reports the
	// failure instead of logging the member in.
	authURL, state = startLogin()
	authCode = confirmScan(authURL, state)
	finishLogin(authCode, state)
	authURL, state = startLogin()
	_, query, _ = finishLogin(authCode, state)
	if query.Get("error") == "" {
		t.Fatal("replayed authCode with a fresh state logged the member in")
	}

	// 5. A member of an app without the contact permission has no email in
	// the userinfo answer: the callback reports email_missing instead of
	// registering an unidentifiable user.
	stub.setEmail("")
	authURL, state = startLogin()
	authCode = confirmScan(authURL, state)
	_, query, _ = finishLogin(authCode, state)
	if query.Get("error") != "email_missing" {
		t.Fatalf("no-email callback error=%q, want email_missing", query.Get("error"))
	}

	// 6. An existing member logs in by matching the email instead of
	// registering a second account.
	const existingEmail = "lisi@corp.example"
	const existingID = "lisi"
	if err := db.Create(&entity.User{ID: existingID, Email: existingEmail, Nickname: "李四"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "lisi-tenant", existingID); err != nil {
		t.Fatalf("prepare existing member tenant: %v", err)
	}
	if err := testsupport.PrepareEnterprisePermissionFixture(db, existingID, existingID); err != nil {
		t.Fatalf("prepare existing member personal tenant: %v", err)
	}
	stub.setEmail(existingEmail)
	authURL, state = startLogin()
	authCode = confirmScan(authURL, state)
	token, _, _ = finishLogin(authCode, state)
	if token == "" {
		t.Fatal("existing member callback did not issue a login token")
	}
	var matched entity.User
	if err := db.Where("email = ?", existingEmail).Take(&matched).Error; err != nil {
		t.Fatal(err)
	}
	if matched.ID != existingID {
		t.Fatalf("existing member login created user id=%q instead of matching %q", matched.ID, existingID)
	}

	// 7. Every login exchanges its own authCode, so the token endpoint must
	// have been hit once per login (5 logins above minus the two that never
	// reached the exchange).
	if calls := func() int {
		stub.mu.Lock()
		defer stub.mu.Unlock()
		return stub.tokenCalls
	}(); calls != 4 {
		t.Fatalf("userAccessToken called %d times, want 4", calls)
	}
}
