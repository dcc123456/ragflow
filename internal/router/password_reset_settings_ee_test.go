//go:build ee

package router

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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
	"ragflow/internal/utility"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func emailSettingsSMTP(t *testing.T, cancelSend <-chan context.CancelFunc) (int, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	messages := make(chan string, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				wire := textproto.NewConn(conn)
				wire.PrintfLine("220 settings test SMTP")
				for {
					line, err := wire.ReadLine()
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
						wire.PrintfLine("250 OK")
					case line == "DATA":
						select {
						case cancel := <-cancelSend:
							cancel()
							return
						default:
						}
						wire.PrintfLine("354 send message")
						message, err := wire.ReadDotBytes()
						if err != nil {
							return
						}
						messages <- string(message)
						wire.PrintfLine("250 accepted")
					case line == "QUIT":
						wire.PrintfLine("221 bye")
						return
					default:
						wire.PrintfLine("500 unexpected command")
					}
				}
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, messages
}

func TestPasswordResetSMTPSettingsFlow(t *testing.T) {
	// Server configuration and Kvrocks are process globals; isolate their initialization.
	const childEnv = "RAGFLOW_PASSWORD_RESET_SETTINGS_TEST_CHILD"
	if os.Getenv(childEnv) != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "-test.run=^TestPasswordResetSMTPSettingsFlow$", "-test.v")
		command.Env = append(os.Environ(), childEnv+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("password reset settings flow failed: %v\n%s", err, output)
		}
		return
	}
	if err := common.InitLogger("error", common.FileOutput{}, "password-reset-settings-test"); err != nil {
		t.Fatal(err)
	}
	common.Logger = zap.NewNop()
	common.Sugar = common.Logger.Sugar()
	gin.SetMode(gin.TestMode)
	t.Chdir("../..")
	privateKey, err := common.LoadPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	encryptPassword := func(password string) string {
		t.Helper()
		encoded := base64.StdEncoding.EncodeToString([]byte(password))
		ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, &privateKey.PublicKey, []byte(encoded))
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(ciphertext)
	}
	cache := miniredis.RunT(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("kvrocks:\n  host: %q\n  password: ''\n  db: 0\nauthentication:\n  enable_register: true\nsmtp:\n  mail_server: '127.0.0.1'\n  mail_port: 1\n  mail_use_ssl: false\n  mail_from_address: 'startup@example.com'\n", cache.Addr())
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
	db := testutil.SetupTestDB(t, &entity.SystemSettings{}, &entity.WhiteList{}, &entity.User{}, &entity.Tenant{}, &entity.File{}, &entity.Role{}, &entity.RoleResource{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	set := func(name, value string) {
		t.Helper()
		if err := db.Save(&entity.SystemSettings{Name: name, Value: value, DataType: "string"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	cancelSend := make(chan context.CancelFunc, 1)
	port, messages := emailSettingsSMTP(t, cancelSend)
	for name, value := range map[string]string{
		"email_verification.enabled": "true", "enable_whitelist": "false",
		"mail.server": "127.0.0.1", "mail.port": fmt.Sprint(port), "mail.timeout": "2",
		"mail.use_ssl": "false", "mail.use_tls": "false", "mail.default_sender": "sender@example.com",
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
	cancelNextSMTP := false
	request := func(method, path string, payload any, want common.ErrorCode, wantMessage ...string) any {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "http://localhost/api/v1"+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cancelNextSMTP {
			cancelNextSMTP = false
			ctx, cancel := context.WithCancel(req.Context())
			defer cancel()
			req = req.WithContext(ctx)
			cancelSend <- cancel
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		if strings.HasSuffix(path, "/captcha") && want == common.CodeSuccess {
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s did not return an uncached PNG image: status=%d headers=%v", path, response.Code, response.Header())
			}
			if _, err := png.Decode(response.Body); err != nil {
				t.Fatalf("invalid captcha image: %v", err)
			}
			return nil
		}
		var result struct {
			Code    common.ErrorCode `json:"code"`
			Data    any              `json:"data"`
			Message string           `json:"message"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Code != want {
			t.Fatalf("%s: status=%d body=%s, want code=%d", path, response.Code, response.Body.String(), want)
		}
		if len(wantMessage) != 0 && result.Message != wantMessage[0] {
			t.Fatalf("%s: message=%q, want %q", path, result.Message, wantMessage[0])
		}
		if strings.HasPrefix(path, "/auth/password/") && want != common.CodeSuccess && (result.Data != false || response.Header().Get("Content-Type") != "application/json") {
			t.Fatalf("%s did not return the JSON error expected by the frontend: %s", path, response.Body.String())
		}
		if path == "/auth/password/reset" && want == common.CodeSuccess && response.Header().Get("Authorization") == "" {
			t.Fatal("password reset did not issue a login token")
		}
		return result.Data
	}
	readCode := func(subject, sender string) string {
		t.Helper()
		select {
		case message := <-messages:
			match := regexp.MustCompile(`code is: ([A-Z]{4})`).FindStringSubmatch(message)
			if len(match) != 2 || !strings.Contains(message, "Subject: "+subject) || !strings.Contains(message, sender) {
				t.Fatalf("unexpected verification email: %s", message)
			}
			return match[1]
		case <-time.After(3 * time.Second):
			t.Fatal("SMTP settings did not deliver an email")
			return ""
		}
	}
	verifySuccess := func(path string, payload map[string]string) {
		t.Helper()
		if data := request(http.MethodPost, path, payload, common.CodeSuccess); data != true {
			t.Fatalf("%s returned data=%v, want true", path, data)
		}
	}
	const email = "new@example.com"
	set("mail.server", "")
	request(http.MethodPost, "/auth/register/captcha", map[string]string{"email": email}, common.CodeSuccess)
	registrationAnswer, err := cache.Get("reg_captcha:" + email)
	if err != nil {
		t.Fatal(err)
	}
	request(http.MethodPost, "/auth/register/otp", map[string]string{"email": email, "captcha": registrationAnswer}, common.CodeServerError, "SMTP is not configured")
	set("mail.server", "127.0.0.1")
	hashed, err := common.GenerateWerkzeugPasswordHash("password!")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.User{ID: "reset-user", Email: email, Nickname: "Test User", Password: &hashed}).Error; err != nil {
		t.Fatal(err)
	}
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "reset-tenant", "reset-user"); err != nil {
		t.Fatalf("prepare reset user tenant: %v", err)
	}
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "reset-user", "reset-user"); err != nil {
		t.Fatalf("prepare reset user personal tenant: %v", err)
	}
	set("email_verification.enabled", "false")
	set("mail.default_sender", "reset@example.com")
	request(http.MethodPost, "/auth/password/forgot/captcha", map[string]string{"email": "unknown@example.com"}, common.CodeDataError)
	request(http.MethodPost, "/auth/password/forgot/otp", map[string]string{"email": email}, common.CodeArgumentError)
	request(http.MethodPost, "/auth/password/forgot/captcha", map[string]string{"email": email}, common.CodeSuccess)
	answer, err := cache.Get("captcha:" + email)
	if err != nil {
		t.Fatal(err)
	}
	if cache.TTL("captcha:"+email) != time.Minute {
		t.Fatal("forgot-password captcha does not expire after one minute")
	}
	set("mail.server", "")
	request(http.MethodPost, "/auth/password/forgot/otp", map[string]string{"email": email, "captcha": answer}, common.CodeServerError, "SMTP is not configured")
	set("mail.server", "127.0.0.1")
	request(http.MethodPost, "/auth/password/forgot/captcha", map[string]string{"email": email}, common.CodeSuccess)
	answer, err = cache.Get("captcha:" + email)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.User{ID: "other-user", Email: "other@example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	request(http.MethodPost, "/auth/password/forgot/otp", map[string]string{"email": "other@example.com", "captcha": answer}, common.CodeNotEffective)
	request(http.MethodPost, "/auth/password/forgot/otp", map[string]string{"email": email, "captcha": answer + "!"}, common.CodeAuthenticationError)
	cache.FastForward(time.Minute)
	request(http.MethodPost, "/auth/password/forgot/otp", map[string]string{"email": email, "captcha": answer}, common.CodeNotEffective)
	request(http.MethodPost, "/auth/password/forgot/captcha", map[string]string{"email": email}, common.CodeSuccess)
	answer, err = cache.Get("captcha:" + email)
	if err != nil {
		t.Fatal(err)
	}
	verifySuccess("/auth/password/forgot/otp", map[string]string{"email": email, "captcha": strings.ToLower(answer)})
	request(http.MethodPost, "/auth/password/forgot/otp", map[string]string{"email": email, "captcha": answer}, common.CodeNotEffective)
	request(http.MethodPost, "/auth/password/forgot/captcha", map[string]string{"email": email}, common.CodeSuccess)
	resendAnswer, err := cache.Get("captcha:" + email)
	if err != nil {
		t.Fatal(err)
	}
	request(http.MethodPost, "/auth/password/forgot/otp", map[string]string{"email": email, "captcha": resendAnswer}, common.CodeNotEffective)
	otp := readCode("Your Password Reset Code", "reset@example.com")
	reset := map[string]string{"email": email, "new_password": encryptPassword("new-password!"), "confirm_new_password": encryptPassword("new-password!")}
	request(http.MethodPost, "/auth/password/reset", reset, common.CodeAuthenticationError)
	request(http.MethodPost, "/auth/password/forgot/otp/verify", map[string]string{"email": email, "otp": "WRONG"}, common.CodeAuthenticationError)
	verifySuccess("/auth/password/forgot/otp/verify", map[string]string{"email": email, "otp": otp})
	request(http.MethodPost, "/auth/password/reset", reset, common.CodeSuccess)
	request(http.MethodPost, "/auth/password/reset", reset, common.CodeAuthenticationError)
	users := service.NewUserService()
	if _, code, err := users.LoginByEmail(t.Context(), &service.EmailLoginRequest{Email: email, Password: encryptPassword("new-password!")}); err != nil || code != common.CodeSuccess {
		t.Fatalf("reset password cannot log in: code=%d error=%v", code, err)
	}
	if _, code, err := users.LoginByEmail(t.Context(), &service.EmailLoginRequest{Email: email, Password: encryptPassword("password!")}); err == nil || code != common.CodeAuthenticationError {
		t.Fatalf("old password can still log in: code=%d error=%v", code, err)
	}
	request(http.MethodPost, "/auth/password/forgot/captcha", map[string]string{"email": email}, common.CodeSuccess)
	answer, err = cache.Get("captcha:" + email)
	if err != nil {
		t.Fatal(err)
	}
	salt := []byte("previous-code-salt")
	previousCode := utility.EncodeOTPStorageValue(utility.HashOTPCode("ABCD", salt), salt)
	cache.Set("otp:"+email, previousCode)
	cache.Set("otp_attempts:"+email, "2")
	cancelNextSMTP = true
	request(http.MethodPost, "/auth/password/forgot/otp", map[string]string{"email": email, "captcha": answer}, common.CodeServerError)
	retainedCode, _ := cache.Get("otp:" + email)
	retainedAttempts, _ := cache.Get("otp_attempts:" + email)
	if retainedCode != previousCode || retainedAttempts != "2" || cache.Exists("otp_last_sent:"+email) {
		t.Fatal("canceled SMTP request did not preserve the previous OTP state")
	}
	verifySuccess("/auth/password/forgot/otp/verify", map[string]string{"email": email, "otp": "ABCD"})
	request(http.MethodPost, "/auth/password/forgot/captcha", map[string]string{"email": email}, common.CodeSuccess)
	answer, err = cache.Get("captcha:" + email)
	if err != nil {
		t.Fatal(err)
	}
	verifySuccess("/auth/password/forgot/otp", map[string]string{"email": email, "captcha": answer})
	for attempt := 1; attempt <= utility.OTPAttemptLimit; attempt++ {
		want := common.CodeAuthenticationError
		if attempt == utility.OTPAttemptLimit {
			want = common.CodeNotEffective
		}
		request(http.MethodPost, "/auth/password/forgot/otp/verify", map[string]string{"email": email, "otp": "WRONG"}, want)
	}
	cache.FastForward(utility.OTPResendCooldown)
	request(http.MethodPost, "/auth/password/forgot/captcha", map[string]string{"email": email}, common.CodeSuccess)
	answer, err = cache.Get("captcha:" + email)
	if err != nil {
		t.Fatal(err)
	}
	request(http.MethodPost, "/auth/password/forgot/otp", map[string]string{"email": email, "captcha": answer}, common.CodeNotEffective)
}
