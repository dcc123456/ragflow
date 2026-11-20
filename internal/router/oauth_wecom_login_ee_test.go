//go:build ee

package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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

// wecomStub fakes the WeCom hosts the login flow talks to: the wwlogin
// consent page (a browser lands there after the redirect and confirms the
// scan) and the three directory API endpoints the provider consumes. Every
// endpoint asserts the wire contract the real WeCom keeps — query-parameter
// credentials, error envelopes with errcode/errmsg — so a regression in the
// provider's requests, not just its parsing, shows up here.
type wecomStub struct {
	server *httptest.Server

	corpID    string
	appSecret string

	mu            sync.Mutex
	bizMail       string
	gettokenCalls int
	nextCode      int
	issuedCodes   map[string]bool
}

func newWecomStub(corpID, appSecret string) *wecomStub {
	stub := &wecomStub{corpID: corpID, appSecret: appSecret, issuedCodes: map[string]bool{}}
	stub.server = httptest.NewServer(stub)
	return stub
}

func (s *wecomStub) setBizMail(mail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bizMail = mail
}

func (s *wecomStub) gettokenCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gettokenCalls
}

func writeWecomJSON(w http.ResponseWriter, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *wecomStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/wwlogin/sso/login":
		// The consent page echoes the parameters the authorization URL
		// carried and, once the member confirms, bounces the code back to
		// the registered redirect URI.
		q := r.URL.Query()
		if q.Get("login_type") != "CorpApp" || q.Get("appid") == "" || q.Get("agentid") == "" || q.Get("redirect_uri") == "" || q.Get("state") == "" {
			http.Error(w, "missing wwlogin parameters", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.nextCode++
		code := fmt.Sprintf("wecom-code-%d", s.nextCode)
		s.issuedCodes[code] = true
		s.mu.Unlock()
		target := q.Get("redirect_uri") + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(q.Get("state"))
		http.Redirect(w, r, target, http.StatusFound)
	case "/cgi-bin/gettoken":
		q := r.URL.Query()
		if q.Get("corpid") != s.corpID || q.Get("corpsecret") != s.appSecret {
			writeWecomJSON(w, map[string]any{"errcode": 40001, "errmsg": "invalid corpid or corpsecret"})
			return
		}
		s.mu.Lock()
		s.gettokenCalls++
		s.mu.Unlock()
		writeWecomJSON(w, map[string]any{"errcode": 0, "errmsg": "ok", "access_token": "app-token", "expires_in": 7200})
	case "/cgi-bin/auth/getuserinfo":
		q := r.URL.Query()
		if q.Get("access_token") != "app-token" {
			writeWecomJSON(w, map[string]any{"errcode": 40014, "errmsg": "invalid access_token"})
			return
		}
		code := q.Get("code")
		s.mu.Lock()
		_, issued := s.issuedCodes[code]
		delete(s.issuedCodes, code)
		s.mu.Unlock()
		if !issued {
			writeWecomJSON(w, map[string]any{"errcode": 42061, "errmsg": "code has been used"})
			return
		}
		writeWecomJSON(w, map[string]any{"errcode": 0, "errmsg": "ok", "userid": "zhangsan"})
	case "/cgi-bin/user/get":
		q := r.URL.Query()
		if q.Get("access_token") != "app-token" || q.Get("userid") == "" {
			writeWecomJSON(w, map[string]any{"errcode": 60111, "errmsg": "userid not found"})
			return
		}
		s.mu.Lock()
		bizMail := s.bizMail
		s.mu.Unlock()
		writeWecomJSON(w, map[string]any{
			"errcode": 0, "errmsg": "ok", "userid": "zhangsan", "name": "张三",
			"avatar": "http://avatar.example/zhangsan.png", "biz_mail": bizMail,
		})
	default:
		http.NotFound(w, r)
	}
}

// TestOAuthWecomLoginFlow drives the WeCom SSO channel end to end: the real
// routes, handler, service, DAO layer and provider against a stub WeCom. The
// stub endpoints are injected through the wecom|sso.authorization_url /
// token_url / userinfo_url / directory_url settings, the same keys an operator
// uses to point the channel at a proxied deployment.
func TestOAuthWecomLoginFlow(t *testing.T) {
	// Server configuration and Kvrocks are process globals; isolate their initialization.
	const childEnv = "RAGFLOW_WECOM_LOGIN_TEST_CHILD"
	if os.Getenv(childEnv) != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "-test.run=^TestOAuthWecomLoginFlow$", "-test.v")
		command.Env = append(os.Environ(), childEnv+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("wecom login flow failed: %v\n%s", err, output)
		}
		return
	}
	if err := common.InitLogger("error", common.FileOutput{}, "wecom-login-test"); err != nil {
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
		corpID    = "ww-test-corp"
		appSecret = "wecom-test-secret"
		agentID   = "1000002"
		redirect  = "http://ragflow.test/api/v1/auth/oauth/wecom/callback"
		bizMail   = "zhangsan@corp.example"
	)
	stub := newWecomStub(corpID, appSecret)
	t.Cleanup(stub.server.Close)
	stub.setBizMail(bizMail)

	set := func(name, value string) {
		t.Helper()
		if err := db.Save(&entity.SystemSettings{Name: name, Value: value, DataType: "string"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range map[string]string{
		"enable_whitelist":            "false",
		"email_verification.enabled":  "false",
		"wecom|sso.enabled":           "true",
		"wecom|sso.client_id":         corpID,
		"wecom|sso.client_secret":     appSecret,
		"wecom|sso.agent_id":          agentID,
		"wecom|sso.redirect_uri":      redirect,
		"wecom|sso.authorization_url": stub.server.URL + "/wwlogin/sso/login",
		"wecom|sso.token_url":         stub.server.URL + "/cgi-bin/gettoken",
		"wecom|sso.userinfo_url":      stub.server.URL + "/cgi-bin/auth/getuserinfo",
		"wecom|sso.directory_url":     stub.server.URL + "/cgi-bin/user/get",
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
		req := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/auth/login/wecom", nil)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		if response.Code != http.StatusFound {
			t.Fatalf("GET /auth/login/wecom: status=%d body=%s", response.Code, response.Body.String())
		}
		state = ""
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == "ragflow_oauth_state_wecom" {
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
		if parsed.Path != "/wwlogin/sso/login" {
			t.Fatalf("authorization URL does not point at the stub consent page: %s", authURL)
		}
		q := parsed.Query()
		for key, want := range map[string]string{
			"login_type":   "CorpApp",
			"appid":        corpID,
			"agentid":      agentID,
			"redirect_uri": redirect,
			"state":        state,
		} {
			if q.Get(key) != want {
				t.Fatalf("authorization URL %s=%q, want %q", key, q.Get(key), want)
			}
		}
	}

	confirmScan := func(authURL, state string) string {
		t.Helper()
		// The browser lands on the consent page and confirms; WeCom answers
		// by bouncing the code back to the registered redirect URI.
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
		if target.Path != "/api/v1/auth/oauth/wecom/callback" {
			t.Fatalf("consent redirect target path=%q", target.Path)
		}
		q := target.Query()
		if q.Get("state") != state {
			t.Fatalf("consent redirect state=%q, want %q", q.Get("state"), state)
		}
		return q.Get("code")
	}

	finishLogin := func(code, state string) (token string, query url.Values, response *httptest.ResponseRecorder) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"http://localhost/api/v1/auth/oauth/wecom/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
		req.AddCookie(&http.Cookie{Name: "ragflow_oauth_state_wecom", Value: state})
		response = httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		if response.Code != http.StatusFound {
			t.Fatalf("GET /auth/oauth/wecom/callback: status=%d body=%s", response.Code, response.Body.String())
		}
		target, err := url.Parse(response.Header().Get("Location"))
		if err != nil {
			t.Fatalf("callback redirect is not a URL: %v", err)
		}
		return target.Query().Get("auth"), target.Query(), response
	}

	// 1. A member whose biz_mail matches no account is registered as a new
	// user with a fully-shaped tenant.
	authURL, state := startLogin()
	assertAuthURL(authURL, state)
	code := confirmScan(authURL, state)
	token, query, callbackResponse := finishLogin(code, state)
	if token == "" {
		t.Fatal("callback did not issue a login token")
	}
	cleared := false
	for _, cookie := range callbackResponse.Result().Cookies() {
		if cookie.Name == "ragflow_oauth_state_wecom" && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("callback did not clear the state cookie")
	}
	var user entity.User
	if err := db.Where("email = ?", bizMail).Take(&user).Error; err != nil {
		t.Fatalf("registered user is missing: %v", err)
	}
	if user.LoginChannel == nil || *user.LoginChannel != "wecom" {
		t.Fatalf("user login_channel=%v, want wecom", user.LoginChannel)
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
	_, query, _ = finishLogin(code, state)
	if query.Get("error") != "invalid_state" {
		t.Fatalf("replayed callback error=%q, want invalid_state", query.Get("error"))
	}

	// 3. A tampered cookie must not pair with a fresh state: the CSRF check.
	authURL, state = startLogin()
	code = confirmScan(authURL, state)
	_, query, _ = finishLogin(code, "tampered-state")
	if query.Get("error") != "invalid_state" {
		t.Fatalf("tampered-cookie callback error=%q, want invalid_state", query.Get("error"))
	}

	// 4. A member without the mailbox service cannot be bound: the callback
	// reports email_missing instead of registering an unidentifiable user.
	stub.setBizMail("")
	authURL, state = startLogin()
	code = confirmScan(authURL, state)
	_, query, _ = finishLogin(code, state)
	if query.Get("error") != "email_missing" {
		t.Fatalf("no-biz_mail callback error=%q, want email_missing", query.Get("error"))
	}

	// 5. An existing member logs in by matching the biz_mail instead of
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
	stub.setBizMail(existingEmail)
	authURL, state = startLogin()
	code = confirmScan(authURL, state)
	token, _, _ = finishLogin(code, state)
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

	// 6. WeCom issues one application token per corp+secret pair; the whole
	// flow must have hit gettoken exactly once, not once per login.
	if calls := stub.gettokenCallCount(); calls != 1 {
		t.Fatalf("gettoken called %d times, want 1 (application token must be cached)", calls)
	}
}
