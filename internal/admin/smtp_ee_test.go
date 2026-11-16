//go:build ee

package admin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"

	"github.com/gin-gonic/gin"
)

func smtpTestRouter() *gin.Engine {
	engine := gin.New()
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(NewHandler(NewService())))
	return engine
}

func postSMTPTest(t *testing.T, engine *gin.Engine, body string) (int, common.ErrorCode) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/validate_mail", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	var result struct {
		Code common.ErrorCode `json:"code"`
		Data json.RawMessage  `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid test response: %s", response.Body.String())
	}
	if result.Code == common.CodeSuccess && string(result.Data) != "true" {
		t.Fatalf("SMTP success response must contain data=true: %s", response.Body.String())
	}
	if result.Code == common.CodeBadRequest {
		var message string
		if err := json.Unmarshal(result.Data, &message); err != nil || message == "" {
			t.Fatalf("SMTP error response must retain data message: %s", response.Body.String())
		}
	}
	return response.Code, result.Code
}

func TestSMTPTestAuditsSuccessfulConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		fmt.Fprint(conn, "220 localhost SMTP\r\n")
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "QUIT") {
				fmt.Fprint(conn, "221 bye\r\n")
				return
			}
			fmt.Fprint(conn, "250 localhost\r\n")
		}
	}()
	var logs []*common.OperationLog
	common.SetAuditLogSink(func(log *common.OperationLog) error { logs = append(logs, log); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("user_id", "audit-admin") })
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(NewHandler(NewService())))
	body, _ := json.Marshal(map[string]any{"host": "127.0.0.1", "port": listener.Addr().(*net.TCPAddr).Port})
	status, code := postSMTPTest(t, engine, string(body))
	if status != http.StatusOK || code != common.CodeSuccess || len(logs) != 1 ||
		logs[0].Operation != "test_admin_smtp_connection" || logs[0].Resources["outcome"] != "response_success" {
		t.Fatalf("status=%d code=%d logs=%+v", status, code, logs)
	}
}

func TestSMTPTestRejectsInvalidSettings(t *testing.T) {
	for name, body := range map[string]string{
		"empty host":       `{}`,
		"invalid port":     `{"host":"localhost","port":"abc"}`,
		"port range":       `{"host":"localhost","port":65536}`,
		"invalid boolean":  `{"host":"localhost","use_ssl":"invalid"}`,
		"both TLS modes":   `{"host":"localhost","use_ssl":true,"use_tls":true}`,
		"plaintext auth":   `{"host":"localhost","username":"sender@example.com","password":"test-password"}`,
		"invalid timeout":  `{"host":"localhost","timeout":"abc"}`,
		"negative timeout": `{"host":"localhost","timeout":-1}`,
		"object value":     `{"host":"localhost","port":{}}`,
		"malformed JSON":   `{"host":`,
	} {
		t.Run(name, func(t *testing.T) {
			status, code := postSMTPTest(t, smtpTestRouter(), body)
			if status != http.StatusBadRequest || code != common.CodeBadRequest {
				t.Fatalf("invalid settings accepted: status=%d code=%d", status, code)
			}
		})
	}
}

func TestSMTPSettingsTimeout(t *testing.T) {
	for _, test := range []struct {
		value string
		want  time.Duration
	}{
		{"0", 30 * time.Second},
		{"180", 180 * time.Second},
		{`"180"`, 180 * time.Second},
		{"-1", 0},
		{"1.5", 0},
		{"9223372037", 0},
	} {
		t.Run(test.value, func(t *testing.T) {
			_, timeout, err := (smtpTestRequest{Host: "localhost", Timeout: json.RawMessage(test.value)}).settings()
			if test.want == 0 {
				if err == nil {
					t.Fatal("invalid timeout accepted")
				}
			} else if err != nil || timeout != test.want {
				t.Fatalf("timeout=%v error=%v, want %v", timeout, err, test.want)
			}
		})
	}
}

func TestSMTPTestHonorsTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var value [1]byte
		conn.Read(value[:])
	}()
	body, _ := json.Marshal(map[string]any{"host": "127.0.0.1", "port": listener.Addr().(*net.TCPAddr).Port, "timeout": "1"})
	start := time.Now()
	status, code := postSMTPTest(t, smtpTestRouter(), string(body))
	if status != http.StatusBadRequest || code == common.CodeSuccess || time.Since(start) > 3*time.Second {
		t.Fatalf("timeout not enforced: status=%d code=%d duration=%s", status, code, time.Since(start))
	}
}

func TestSMTPTestRouteRequiresAdminAuthentication(t *testing.T) {
	handler := NewHandler(NewService())
	engine := gin.New()
	protected := engine.Group("/api/v1/admin")
	protected.Use(handler.AuthMiddleware())
	RegisterEERouter(protected, NewRouter(handler))
	_, code := postSMTPTest(t, engine, `{"host":"localhost"}`)
	if code != common.CodeUnauthorized {
		t.Fatalf("SMTP test route did not require authentication: %d", code)
	}
}
