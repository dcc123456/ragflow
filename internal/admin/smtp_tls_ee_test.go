//go:build ee

package admin

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/utility"
)

const smtpTLSChildEnv = "RAGFLOW_SMTP_TLS_TEST_CHILD"

type smtpTLSExchange struct {
	commands      []string
	message       string
	upgraded      bool
	authenticated bool
	authAttempted bool
	authOverTLS   bool
	version       uint16
}

func smtpTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "SMTP test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, key
}

func smtpTestCertificate(t *testing.T, ca *x509.Certificate, signer *ecdsa.PrivateKey, matchingHost bool) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "SMTP test server"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"other.example"},
	}
	if matchingHost {
		template.DNSNames = []string{"localhost"}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key}
}

func startSMTPTestTLS(t *testing.T, certificate tls.Certificate, implicitTLS, offerSTARTTLS bool) (int, <-chan smtpTLSExchange) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	exchanges := make(chan smtpTLSExchange, 1)
	go func() {
		var exchange smtpTLSExchange
		defer func() { exchanges <- exchange }()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { conn.Close() }()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		config := &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
		secure := false
		upgrade := func() error {
			encrypted := tls.Server(conn, config)
			if err := encrypted.Handshake(); err != nil {
				return err
			}
			conn = encrypted
			secure = encrypted.ConnectionState().HandshakeComplete
			exchange.version = encrypted.ConnectionState().Version
			return nil
		}
		if implicitTLS {
			if err := upgrade(); err != nil {
				return
			}
		}
		wire := textproto.NewConn(conn)
		if err := wire.PrintfLine("220 SMTP TLS test"); err != nil {
			return
		}
		for {
			line, err := wire.ReadLine()
			if err != nil {
				return
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				return
			}
			exchange.commands = append(exchange.commands, fields[0])
			switch fields[0] {
			case "EHLO":
				if secure {
					wire.PrintfLine("250-SMTP TLS test")
					wire.PrintfLine("250 AUTH PLAIN")
				} else if offerSTARTTLS {
					wire.PrintfLine("250-SMTP TLS test")
					wire.PrintfLine("250 STARTTLS")
				} else {
					wire.PrintfLine("250 SMTP test")
				}
			case "STARTTLS":
				if !offerSTARTTLS || secure {
					wire.PrintfLine("454 STARTTLS unavailable")
					continue
				}
				wire.PrintfLine("220 Begin TLS")
				if err := upgrade(); err != nil {
					return
				}
				exchange.upgraded = true
				wire = textproto.NewConn(conn)
			case "AUTH":
				exchange.authAttempted = true
				exchange.authOverTLS = secure
				if !secure || len(fields) != 3 || fields[1] != "PLAIN" {
					wire.PrintfLine("538 Encrypted authentication required")
					continue
				}
				credentials, err := base64.StdEncoding.DecodeString(fields[2])
				if err != nil || !bytes.Equal(credentials, []byte("\x00sender@example.com\x00test-password")) {
					wire.PrintfLine("535 Authentication failed")
					continue
				}
				exchange.authenticated = true
				wire.PrintfLine("235 Authentication succeeded")
			case "MAIL", "RCPT":
				wire.PrintfLine("250 OK")
			case "DATA":
				wire.PrintfLine("354 Send message")
				message, err := wire.ReadDotBytes()
				if err != nil {
					return
				}
				exchange.message = string(message)
				wire.PrintfLine("250 Accepted")
			case "QUIT":
				wire.PrintfLine("221 Bye")
				return
			default:
				wire.PrintfLine("501 Unexpected command")
			}
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, exchanges
}

func TestSMTPTestEncryptedConnections(t *testing.T) {
	// Root certificates are process-wide. Isolate the test CA from other tests.
	if os.Getenv(smtpTLSChildEnv) != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "-test.run=^TestSMTPTestEncryptedConnections$", "-test.v")
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "GODEBUG=") && !strings.HasPrefix(value, smtpTLSChildEnv+"=") {
				command.Env = append(command.Env, value)
			}
		}
		command.Env = append(command.Env, smtpTLSChildEnv+"=1", "GODEBUG="+os.Getenv("GODEBUG")+",x509usefallbackroots=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("encrypted SMTP tests failed: %v\n%s", err, output)
		}
		t.Log(string(output))
		return
	}

	ca, signer := smtpTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	x509.SetFallbackRoots(roots)
	valid := smtpTestCertificate(t, ca, signer, true)
	wrongHost := smtpTestCertificate(t, ca, signer, false)
	untrustedCA, untrustedSigner := smtpTestCA(t)
	untrusted := smtpTestCertificate(t, untrustedCA, untrustedSigner, true)

	for _, test := range []struct {
		name        string
		ssl         bool
		startTLS    bool
		offerTLS    bool
		certificate tls.Certificate
		password    string
		wantSuccess bool
		wantAuth    bool
		send        bool
	}{
		{"SSL string settings", true, false, false, valid, "test-password", true, true, false},
		{"STARTTLS native settings", false, true, true, valid, "test-password", true, true, false},
		{"STARTTLS incorrect password", false, true, true, valid, "incorrect", false, true, false},
		{"SSL untrusted certificate", true, false, false, untrusted, "test-password", false, false, false},
		{"STARTTLS wrong hostname", false, true, true, wrongHost, "test-password", false, false, false},
		{"STARTTLS unavailable", false, true, false, valid, "test-password", false, false, false},
		{"SSL email delivery", true, false, false, valid, "test-password", true, true, true},
		{"STARTTLS email delivery", false, true, true, valid, "test-password", true, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			port, exchanges := startSMTPTestTLS(t, test.certificate, test.ssl, test.offerTLS)
			if test.send {
				cfg := common.SMTPConfig{
					MailServer: "127.0.0.1", MailPort: port,
					MailUsername: "sender@example.com", MailPassword: test.password,
					MailUseSSL: test.ssl, MailUseTLS: test.startTLS,
				}
				err := utility.SendSMTPEmail(t.Context(), cfg, 3*time.Second, "recipient@example.com", "SMTP delivery test", "Test email body")
				if (err == nil) != test.wantSuccess {
					t.Fatalf("encrypted delivery: %v, want success=%v", err, test.wantSuccess)
				}
			} else {
				settings := map[string]any{
					"host": "127.0.0.1", "port": port, "username": "sender@example.com", "password": test.password,
					"use_ssl": test.ssl, "use_tls": test.startTLS, "timeout": 3,
				}
				if test.ssl {
					for _, key := range []string{"port", "use_ssl", "use_tls", "timeout"} {
						settings[key] = fmt.Sprint(settings[key])
					}
				}
				body, err := json.Marshal(settings)
				if err != nil {
					t.Fatal(err)
				}
				status, code := postSMTPTest(t, smtpTestRouter(), string(body))
				if test.wantSuccess {
					if status != http.StatusOK || code != common.CodeSuccess {
						t.Fatalf("encrypted connection failed: HTTP %d, code %d", status, code)
					}
				} else if status != http.StatusBadRequest || code == common.CodeSuccess {
					t.Fatalf("invalid connection accepted: HTTP %d, code %d", status, code)
				}
			}
			select {
			case exchange := <-exchanges:
				if exchange.authAttempted != test.wantAuth || exchange.authenticated != test.wantSuccess {
					t.Fatalf("unexpected authentication result: %#v", exchange)
				}
				if test.wantAuth && (!exchange.authOverTLS || exchange.version < tls.VersionTLS12) {
					t.Fatalf("authentication did not use TLS 1.2 or later: %#v", exchange)
				}
				if test.wantSuccess && exchange.upgraded != test.startTLS {
					t.Fatalf("wrong encryption mode: %#v", exchange)
				}
				for _, verb := range exchange.commands {
					if !test.send && (verb == "MAIL" || verb == "RCPT" || verb == "DATA") {
						t.Fatalf("connection test attempted email delivery: %s", verb)
					}
				}
				if test.send && test.wantSuccess && (!strings.Contains(exchange.message, "To: recipient@example.com") || !strings.Contains(exchange.message, "Subject: SMTP delivery test") || !strings.Contains(exchange.message, "Test email body")) {
					t.Fatalf("unexpected email: %s", exchange.message)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("encrypted SMTP connection was not closed")
			}
		})
	}
}
