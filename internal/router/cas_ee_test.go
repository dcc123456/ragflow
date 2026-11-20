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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ragflow/internal/admin"
	"ragflow/internal/common"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/entity"
	"ragflow/internal/handler"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/server"
	"ragflow/internal/service"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TestCASLoginFlow exercises the registered routes, settings, CAS client,
// one-time state, local accounts and signed credentials together.
func TestCASLoginFlow(t *testing.T) {
	const childEnv = "RAGFLOW_CAS_LOGIN_TEST_CHILD"
	if os.Getenv(childEnv) != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "-test.run=^TestCASLoginFlow$", "-test.v")
		command.Env = append(os.Environ(), childEnv+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("CAS login flow failed: %v\n%s", err, output)
		}
		return
	}
	common.Logger = zap.NewNop()
	common.Sugar = common.Logger.Sugar()
	gin.SetMode(gin.TestMode)
	cache := miniredis.RunT(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("kvrocks:\n  host: %q\n  password: ''\n  db: 0\n", cache.Addr())), 0600); err != nil {
		t.Fatal(err)
	}
	if err := server.Init(configPath); err != nil {
		t.Fatal(err)
	}
	if err := kvrocks.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { kvrocks.Close() })
	db := testutil.SetupTestDB(t, &entity.SystemSettings{}, &entity.WhiteList{}, &entity.User{}, &entity.Tenant{}, &entity.UserTenant{}, &entity.File{}, &entity.Role{}, &entity.RoleResource{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	settings := admin.NewService()
	set := func(key, value string) {
		t.Helper()
		if err := settings.SetVariable(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}
	var calls atomic.Int64
	var ticketMu sync.Mutex
	tickets := map[string]string{}
	casServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/cas-idp/application/serviceValidate" {
			t.Errorf("wrong validation path: %s", r.URL.Path)
		}
		ticketMu.Lock()
		expected, ok := tickets[r.URL.Query().Get("ticket")]
		delete(tickets, r.URL.Query().Get("ticket"))
		ticketMu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		if !ok || expected != r.URL.Query().Get("service") {
			fmt.Fprint(w, `<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationFailure code="INVALID_SERVICE"/></cas:serviceResponse>`)
			return
		}
		// Authing-style CAS 2.0 attributes; the mapping is selected in the admin configuration.
		fmt.Fprint(w, `<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationSuccess><cas:user>employee-42</cas:user><cas:attributes><cas:email>employee@example.com</cas:email><cas:nickname>Employee</cas:nickname></cas:attributes></cas:authenticationSuccess></cas:serviceResponse>`)
	}))
	defer casServer.Close()
	engine := gin.New()
	r := &Router{userHandler: handler.NewUserHandler(service.NewUserService(), service.NewUserTenantService()), systemHandler: handler.NewSystemHandler(service.NewSystemService())}
	RegisterEENoAuthRouter(engine, engine.Group("/api/v1"), r)
	request := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	channels := func() []service.LoginChannel {
		t.Helper()
		response := request("/api/v1/auth/login/channels", nil)
		var data struct {
			Code int                    `json:"code"`
			Data []service.LoginChannel `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil || data.Code != 0 {
			t.Fatalf("channels response: %s", response.Body.String())
		}
		return data.Data
	}
	if len(channels()) != 0 {
		t.Fatal("unconfigured CAS was advertised")
	}
	set("cas|sso.enabled", "true")
	if len(channels()) != 0 {
		t.Fatal("incomplete CAS was advertised")
	}
	const callbackPath = "/api/v1/auth/cas/callback"
	const callbackURL = "https://ragflow.example" + callbackPath
	for key, value := range map[string]string{
		"name": "Company login", "login_url": casServer.URL + "/cas-idp/application/login",
		"validation_url":   casServer.URL + "/cas-idp/application/serviceValidate",
		"redirect_uri":     callbackURL,
		"protocol_version": "2", "email_attribute": "email", "nickname_attribute": "nickname",
	} {
		set("cas|sso."+key, value)
	}
	if found := channels(); len(found) != 1 || found[0].Channel != "cas" || found[0].DisplayName != "Company login" {
		t.Fatalf("configured CAS missing: %+v", found)
	}
	type attempt struct {
		service string
		cookie  *http.Cookie
	}
	beginChannel := func(channel, loginEndpoint string) attempt {
		t.Helper()
		response := request("/api/v1/auth/login/"+channel, nil)
		login, err := url.Parse(response.Header().Get("Location"))
		if err != nil || response.Code != http.StatusFound {
			t.Fatalf("initiate: %d %s", response.Code, response.Body.String())
		}
		callback, err := url.Parse(login.Query().Get("service"))
		login.RawQuery = ""
		if login.String() != loginEndpoint {
			t.Fatalf("login endpoint = %s, want %s", login, loginEndpoint)
		}
		cookies := response.Result().Cookies()
		if err != nil || len(cookies) != 1 || callback.Path != callbackPath || len(callback.Query().Get("state")) != 64 {
			t.Fatal("missing browser state")
		}
		cookie := cookies[0]
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 300 {
			t.Fatalf("unsafe state cookie: %+v", cookie)
		}
		return attempt{service: callback.String(), cookie: cookie}
	}
	begin := func() attempt {
		t.Helper()
		return beginChannel("cas", casServer.URL+"/cas-idp/application/login")
	}
	complete := func(a attempt, ticket string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		response := request(a.service+"&ticket="+url.QueryEscape(ticket), cookie)
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("callback may cache or leak credentials")
		}
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != a.cookie.Name || cookies[0].MaxAge != -1 {
			t.Fatal("state cookie was not cleared")
		}
		return response
	}
	issue := func(a attempt, ticket string) { ticketMu.Lock(); tickets[ticket] = a.service; ticketMu.Unlock() }
	expectError := func(response *httptest.ResponseRecorder, code string) {
		t.Helper()
		target, _ := url.Parse(response.Header().Get("Location"))
		if response.Code != http.StatusFound || target.Query().Get("error") != code || target.Query().Get("auth") != "" {
			t.Fatalf("expected %s, got %d %s", code, response.Code, response.Header().Get("Location"))
		}
	}
	status := "1"
	token := "old-token"
	if err := db.Create(&entity.User{ID: "existing-user", Email: "employee@example.com", Nickname: "Existing name", AccessToken: &token, IsActive: "1", Status: &status}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.Tenant{ID: "company", Status: &status}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.UserTenant{ID: "member", UserID: "existing-user", TenantID: "company", Role: "normal", Status: &status}).Error; err != nil {
		t.Fatal(err)
	}

	t.Run("existing account and tenant", func(t *testing.T) {
		a := begin()
		issue(a, "ST-existing")
		response := complete(a, "ST-existing", a.cookie)
		target, _ := url.Parse(response.Header().Get("Location"))
		auth := target.Query().Get("auth")
		secret, err := server.GetSecretKey(t.Context(), kvrocks.Get())
		if err != nil {
			t.Fatal(err)
		}
		signed, err := common.ExtractAccessTokenContext(auth, secret)
		if err != nil || signed.TenantID != "company" || auth != response.Header().Get("Authorization") {
			t.Fatalf("invalid tenant credential: %+v, %v", signed, err)
		}
		var user entity.User
		if err := db.Where("id = ?", "existing-user").Take(&user).Error; err != nil {
			t.Fatal(err)
		}
		if user.AccessToken == nil || *user.AccessToken == token || user.LastLoginTime == nil || user.Nickname != "Existing name" {
			t.Fatal("existing account was not signed in correctly")
		}
		expectError(complete(a, "ST-existing", a.cookie), "invalid_state")
	})
	t.Run("browser binding", func(t *testing.T) {
		a := begin()
		before := calls.Load()
		expectError(complete(a, "ST-other", nil), "invalid_state")
		wrong := *a.cookie
		wrong.Value = strings.Repeat("0", 64)
		expectError(complete(a, "ST-other", &wrong), "invalid_state")
		if calls.Load() != before {
			t.Fatal("CAS was contacted before browser validation")
		}
	})
	t.Run("ticket service binding", func(t *testing.T) {
		a, b := begin(), begin()
		issue(a, "ST-swapped")
		expectError(complete(b, "ST-swapped", b.cookie), "invalid_ticket")
	})
	t.Run("expired state", func(t *testing.T) {
		a := begin()
		cache.FastForward(6 * time.Minute)
		expectError(complete(a, "ST-expired", a.cookie), "invalid_state")
	})
	t.Run("configuration change", func(t *testing.T) {
		a := begin()
		set("cas|sso.email_attribute", "otherEmail")
		expectError(complete(a, "ST-config", a.cookie), "invalid_state")
		set("cas|sso.email_attribute", "email")
	})
	t.Run("missing ticket consumes state", func(t *testing.T) {
		a := begin()
		expectError(complete(a, "", a.cookie), "missing_ticket")
		expectError(complete(a, "", a.cookie), "invalid_state")
	})
	t.Run("inactive account", func(t *testing.T) {
		db.Model(&entity.User{}).Where("id = ?", "existing-user").Update("is_active", "0")
		a := begin()
		issue(a, "ST-inactive")
		expectError(complete(a, "ST-inactive", a.cookie), "user_inactive")
		db.Model(&entity.User{}).Where("id = ?", "existing-user").Update("is_active", "1")
	})
	t.Run("membership required", func(t *testing.T) {
		db.Model(&entity.UserTenant{}).Where("id = ?", "member").Update("status", "2")
		a := begin()
		issue(a, "ST-invited")
		expectError(complete(a, "ST-invited", a.cookie), "No active tenant is available for login")
		db.Model(&entity.UserTenant{}).Where("id = ?", "member").Update("status", "1")
	})
	t.Run("missing email", func(t *testing.T) {
		set("cas|sso.email_attribute", "mail")
		a := begin()
		issue(a, "ST-no-email")
		expectError(complete(a, "ST-no-email", a.cookie), "email_missing")
		set("cas|sso.email_attribute", "email")
	})
	t.Run("new account provisions through SSO", func(t *testing.T) {
		if err := db.Where("id = ?", "existing-user").Delete(&entity.User{}).Error; err != nil {
			t.Fatal(err)
		}
		a := begin()
		issue(a, "ST-new")
		response := complete(a, "ST-new", a.cookie)
		if !strings.Contains(response.Header().Get("Location"), "?auth=") {
			t.Fatalf("new login: %s", response.Header().Get("Location"))
		}
		var user entity.User
		if err := db.Where("email = ?", "employee@example.com").Take(&user).Error; err != nil {
			t.Fatal(err)
		}
		if user.LoginChannel == nil || *user.LoginChannel != "cas" || user.Nickname != "Employee" {
			t.Fatalf("new account: %+v", user)
		}
		for _, model := range []any{&entity.Tenant{}, &entity.File{}, &entity.UserTenant{}} {
			var count int64
			query := db.Model(model)
			switch model.(type) {
			case *entity.Tenant:
				query = query.Where("id = ?", user.ID)
			default:
				query = query.Where("tenant_id = ?", user.ID)
			}
			if err := query.Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("missing SSO resources for %T: %d, %v", model, count, err)
			}
		}
	})
	t.Run("CAS 1.0 and 2.0 providers share the callback", func(t *testing.T) {
		const channel = "cas-11111111111111111111111111111111"
		var secondCalls atomic.Int64
		second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			secondCalls.Add(1)
			callback, err := url.Parse(r.URL.Query().Get("service"))
			if err != nil || callback.Path != callbackPath || r.URL.Path != "/cas/validate" ||
				r.URL.Query().Get("ticket") != "ST-second-provider" {
				t.Errorf("incorrect validation endpoint or service for the second provider")
				http.Error(w, "invalid ticket", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "yes\nsecond@example.com\n")
		}))
		defer second.Close()
		for key, value := range map[string]string{
			"enabled":          "true",
			"name":             "Second CAS provider",
			"login_url":        second.URL + "/cas/login",
			"validation_url":   second.URL + "/cas/validate",
			"redirect_uri":     callbackURL,
			"protocol_version": "1",
		} {
			set(channel+"|sso."+key, value)
		}
		if listed := channels(); len(listed) != 2 || listed[0].Channel != "cas" ||
			listed[1].Channel != channel || listed[1].DisplayName != "Second CAS provider" || listed[1].Icon != "cas" {
			t.Fatalf("expected both providers, got %+v", listed)
		}

		first := begin()
		other := beginChannel(channel, second.URL+"/cas/login")
		if first.cookie.Name == other.cookie.Name {
			t.Fatal("parallel CAS attempts share a browser cookie")
		}
		expectError(complete(other, "ST-second-provider", first.cookie), "invalid_state")
		if secondCalls.Load() != 0 {
			t.Fatal("mismatched provider reached ticket validation")
		}

		issue(first, "ST-first-provider")
		for _, response := range []*httptest.ResponseRecorder{
			complete(first, "ST-first-provider", first.cookie),
			complete(other, "ST-second-provider", other.cookie),
		} {
			if response.Header().Get("Authorization") == "" {
				t.Fatalf("missing app credential: %s", response.Header().Get("Location"))
			}
		}
		if secondCalls.Load() != 1 {
			t.Fatal("callback did not select the CAS provider from the login state")
		}
		var user entity.User
		if err := db.Where("email = ?", "second@example.com").Take(&user).Error; err != nil {
			t.Fatal(err)
		}
		if user.LoginChannel == nil || *user.LoginChannel != channel {
			t.Fatalf("user registered with the wrong provider")
		}
		set(channel+"|sso.enabled", "false")
		if listed := channels(); len(listed) != 1 || listed[0].Channel != "cas" {
			t.Fatalf("disabling the second provider affected the first: %+v", listed)
		}
	})
	t.Run("disabled provider", func(t *testing.T) {
		a := begin()
		set("cas|sso.enabled", "false")
		if len(channels()) != 0 {
			t.Fatal("disabled CAS advertised")
		}
		expectError(complete(a, "ST-disabled", a.cookie), "CAS is not configured")
	})
}
