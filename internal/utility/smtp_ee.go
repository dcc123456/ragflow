//go:build ee

package utility

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"ragflow/internal/common"
)

// ParseSMTPTimeout converts a non-negative number of seconds to a duration.
func ParseSMTPTimeout(value string) (time.Duration, error) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || seconds < 0 || seconds > int64((1<<63-1)/time.Second) {
		return 0, fmt.Errorf("SMTP timeout must be a non-negative whole number of seconds within the duration range")
	}
	if seconds == 0 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second, nil
}

// CheckSMTPConnection completes the SMTP handshake and authentication without sending mail.
func CheckSMTPConnection(ctx context.Context, cfg common.SMTPConfig, timeout time.Duration) error {
	if err := validateSMTPConfig(cfg); err != nil {
		return err
	}
	return withSMTPClient(ctx, cfg, timeout, func(client *smtp.Client) error {
		return client.Quit()
	})
}

// SendSMTPEmail delivers a plain-text email using the supplied SMTP settings.
func SendSMTPEmail(ctx context.Context, cfg common.SMTPConfig, timeout time.Duration, toEmail, subject, body string) error {
	if err := validateSMTPConfig(cfg); err != nil {
		return err
	}
	fromAddr := cfg.MailFromAddress
	if fromAddr == "" {
		fromAddr = cfg.MailUsername
	}
	fromName := cfg.MailFromName
	if fromName == "" {
		fromName = "RAGFlow"
	}
	sender, err := mail.ParseAddress(fromAddr)
	if err != nil {
		return fmt.Errorf("invalid SMTP sender: %w", err)
	}
	recipient, err := mail.ParseAddress(toEmail)
	if err != nil {
		return fmt.Errorf("invalid SMTP recipient: %w", err)
	}
	fromHeader := (&mail.Address{Name: fromName, Address: sender.Address}).String()
	message := buildPlainEmail(fromHeader, recipient.Address, subject, body)
	return withSMTPClient(ctx, cfg, timeout, func(client *smtp.Client) error {
		if err := deliverMail(client, sender.Address, recipient.Address, message); err != nil {
			return err
		}
		// The server has accepted DATA; a failed QUIT cannot undo it.
		client.Quit()
		return nil
	})
}

func validateSMTPConfig(cfg common.SMTPConfig) error {
	if cfg.MailServer == "" || cfg.MailPort == 0 {
		return SMTPNotConfiguredError{}
	}
	if cfg.MailUseSSL && cfg.MailUseTLS {
		return fmt.Errorf("SMTP SSL and STARTTLS cannot both be enabled")
	}
	if cfg.MailUsername != "" && !cfg.MailUseSSL && !cfg.MailUseTLS {
		return SMTPInsecureAuthError{}
	}
	return nil
}

func withSMTPClient(ctx context.Context, cfg common.SMTPConfig, timeout time.Duration, action func(*smtp.Client) error) error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	addr := net.JoinHostPort(cfg.MailServer, strconv.Itoa(cfg.MailPort))
	tlsCfg := &tls.Config{ServerName: cfg.MailServer, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	var err error
	if cfg.MailUseSSL {
		conn, err = (&tls.Dialer{Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("SMTP dial: %w", err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	client, err := smtp.NewClient(conn, cfg.MailServer)
	if err != nil {
		return fmt.Errorf("SMTP greeting: %w", err)
	}
	defer client.Close()
	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("SMTP handshake: %w", err)
	}
	if cfg.MailUseTLS {
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("SMTP starttls: %w", err)
		}
	}
	if cfg.MailUsername != "" {
		if err := client.Auth(smtp.PlainAuth("", cfg.MailUsername, cfg.MailPassword, cfg.MailServer)); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}
	return action(client)
}
