//go:build ee

package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/utility"

	"github.com/gin-gonic/gin"
)

type smtpTestRequest struct {
	Host     string          `json:"host"`
	Port     json.RawMessage `json:"port"`
	Username string          `json:"username"`
	Password string          `json:"password"`
	UseSSL   json.RawMessage `json:"use_ssl"`
	UseTLS   json.RawMessage `json:"use_tls"`
	Timeout  json.RawMessage `json:"timeout"`
}

func smtpSettingText(raw json.RawMessage, fallback string) (string, error) {
	if len(raw) == 0 {
		return fallback, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text), nil
	}
	if string(raw) == "null" || strings.ContainsAny(string(raw), "{}[]") {
		return "", fmt.Errorf("expected a scalar SMTP setting")
	}
	return string(raw), nil
}

func (r smtpTestRequest) settings() (common.SMTPConfig, time.Duration, error) {
	cfg := common.SMTPConfig{
		MailServer:   strings.TrimSpace(r.Host),
		MailUsername: r.Username,
		MailPassword: r.Password,
	}
	if cfg.MailServer == "" {
		return cfg, 0, fmt.Errorf("SMTP host is required")
	}
	port, err := smtpSettingText(r.Port, "465")
	if err == nil {
		cfg.MailPort, err = strconv.Atoi(port)
	}
	if err != nil || cfg.MailPort < 1 || cfg.MailPort > 65535 {
		return cfg, 0, fmt.Errorf("SMTP port must be between 1 and 65535")
	}
	for _, flag := range []struct {
		name string
		raw  json.RawMessage
		out  *bool
	}{{"use_ssl", r.UseSSL, &cfg.MailUseSSL}, {"use_tls", r.UseTLS, &cfg.MailUseTLS}} {
		value, err := smtpSettingText(flag.raw, "false")
		if err == nil {
			*flag.out, err = strconv.ParseBool(value)
		}
		if err != nil {
			return cfg, 0, fmt.Errorf("SMTP %s must be a boolean", flag.name)
		}
	}
	value, err := smtpSettingText(r.Timeout, "30")
	if err != nil {
		return cfg, 0, err
	}
	timeout, err := utility.ParseSMTPTimeout(value)
	return cfg, timeout, err
}

func (h *Handler) validateMail(c *gin.Context) {
	common.StartOperationLog(c, "test_admin_smtp_connection")
	defer common.FinishOperationLog(c)

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var request smtpTestRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondSMTPTestError(c, "Invalid SMTP settings")
		return
	}
	cfg, timeout, err := request.settings()
	if err == nil {
		err = utility.CheckSMTPConnection(c.Request.Context(), cfg, timeout)
	}
	if err != nil {
		respondSMTPTestError(c, err.Error())
		return
	}
	common.Respond(c, http.StatusOK, common.CodeSuccess, json.RawMessage(`true`), "SMTP connection succeeded")
}

func respondSMTPTestError(c *gin.Context, message string) {
	// The admin UI reads the error from data; retain that response shape.
	data, _ := json.Marshal(message)
	common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, json.RawMessage(data), message)
}
