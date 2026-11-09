//go:build ee

package service

import (
	"context"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/utility"

	"gorm.io/gorm"
)

func loadMailSettings(ctx context.Context, db *gorm.DB) (common.SMTPConfig, time.Duration, error) {
	cfg := common.SMTPConfig{MailPort: 465, MailUseSSL: true, MailFromName: "RAGFlow"}
	timeout := 30 * time.Second
	settings, err := dao.NewSystemSettingsDAO().GetByNamePrefix(ctx, db, "mail.")
	if err != nil {
		return cfg, 0, err
	}
	for _, setting := range settings {
		value := strings.TrimSpace(setting.Value)
		switch setting.Name {
		case "mail.server":
			cfg.MailServer = value
		case "mail.username":
			cfg.MailUsername = value
		case "mail.password":
			cfg.MailPassword = setting.Value
		case "mail.default_sender":
			if value != "" {
				address, err := mail.ParseAddress(value)
				if err != nil {
					return cfg, 0, fmt.Errorf("invalid SMTP sender")
				}
				cfg.MailFromAddress = address.Address
				if address.Name != "" {
					cfg.MailFromName = address.Name
				}
			}
		case "mail.port":
			if value == "" {
				continue
			}
			number, err := strconv.Atoi(value)
			if err != nil || number < 1 || number > 65535 {
				return cfg, 0, fmt.Errorf("invalid %s", setting.Name)
			}
			cfg.MailPort = number
		case "mail.timeout":
			if value == "" {
				continue
			}
			timeout, err = utility.ParseSMTPTimeout(value)
			if err != nil {
				return cfg, 0, fmt.Errorf("invalid mail.timeout: %w", err)
			}
		case "mail.use_ssl", "mail.use_tls":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return cfg, 0, fmt.Errorf("invalid %s", setting.Name)
			}
			if setting.Name == "mail.use_ssl" {
				cfg.MailUseSSL = enabled
			} else {
				cfg.MailUseTLS = enabled
			}
		}
	}
	return cfg, timeout, nil
}
